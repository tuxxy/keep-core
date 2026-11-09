package tbtc_test

// Remediation batch 15 acceptance tests: a Terminated FROST wallet's funds
// are moved by the hosts as a MovingFunds wallet's are.
// Refs tuxxy/keep-core#141 (FSM analysis M-1, P2) and DECISIONS.md D30 (a).
//
// Invariant (D30 (a), P-K07-NO-STRANDED-MOVEMENT for a terminated source):
// whenever the Bridge would accept a FROST source's movement (tbtc-v2
// FrostMovement.commit/prove: the source is MovingFunds or Terminated, its
// current-period commitment is absent or unproven, no dust is recorded, and
// it holds a positive main), every coordination window's leader proposes the
// movement (tbtcpg.MovingFundsTask, through ProposalGenerator.Generate) and
// every follower plans and authorizes the proposed movement (the K-07
// movement.Planner and movement.Authority, as frostWallet.moveFunds runs
// them). When the Bridge refuses it (a Terminated source whose movement is
// already proven, a Terminated source with recorded dust, a source in any
// other state), no leader proposes it or tries to commit it.
//
// Terminated sub-cases (fsm-analysis/contracts.md, "Terminated has hidden
// sub-states"), against FrostMovement at tbtc-v2 39412825:
//   - awaiting movement: commit and prove accept it (FrostMovement.sol:86-89,
//     176-179). Case "terminated" (Live, then Terminated, then committed)
//     and case "terminated-committed" (committed while MovingFunds, then
//     Terminated).
//   - moved (current commitment provenAt != 0, custody 0): commit refuses
//     ("FROST movement committed"), prove refuses ("FROST movement
//     commitment"). Checked after every settle.
//   - dust (lifecycle.dust): commit refuses ("FROST movement committed").
//     Case "terminated-dust".
// The refusals are asserted on the real Bridge as preconditions, so the
// tests expect of the host exactly what the contract accepts.
//
// World: the frozen legacy world on the real upgraded Bridge (tbtc-v2
// integration/frost-base, LEGACY_UPGRADED_DIR) with Bitcoin Core regtest,
// configured by setUpFrostDrain, whose FROST wallet is the movement's only
// eligible target. The source S is a second FROST wallet: the legacy
// MovingFunds wallet drains into it (as batch 3's world does), S sweeps that
// incoming output and the sweep is proven, so S is Live with a main. S is
// then put into the case's state by public calls only: age (governance
// wallet max age, notifyFrostWalletCloseable by a stranger) for MovingFunds;
// an undefeated fraud challenge over a digest S signed (governance defeat
// timeout 1 s, notifyFrostFraudChallengeDefeatTimeout by a stranger) for
// Terminated; notifyFrostMovingFundsBelowDust by a stranger for dust. The
// movable cases' commitment is submitted in setup by S's seat-0 operator
// (see commit for why not by the leader). A movement is carried to Bitcoin
// (Core mines it) and proven on the real Bridge.
// FIXTURES: the FROST registry fixture behind both wallets (no FROST DKG);
// the legacy source's test key (the drain); S's test key (scalar 1002), which
// signs in place of S's FROST signers (incoming sweep, fraud digest,
// movement); and the Core-backed script query b15Bitcoin (ADAPTER).
// RPC: every leader and follower runs a production client (chainethereum.Connect)
// behind batch 1's recorder ADAPTER, on both empty-revert encodings.
// No build tag (tuxxy/keep-core#64).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// b15Prefix names the invariant and the issue in every assertion.
const b15Prefix = "D30 (tuxxy/keep-core#141): "

// b15SweepFee is the fee of the source's incoming sweep (setup).
const b15SweepFee = int64(1_000)

// b15MaximumSatPerVByte is the follower's fee-rate ceiling, the node's
// frostMaximumSatPerVByte (pkg/tbtc/frost_node.go).
const b15MaximumSatPerVByte = 20

// b15Bound bounds one leader or follower routine.
const b15Bound = 30 * time.Second

// b15Start is the source's state when the windows run.
type b15Start int

const (
	b15Live b15Start = iota
	b15MovingFunds
	b15Terminated
	b15TerminatedCommitted
	b15TerminatedDust
)

func (s b15Start) String() string {
	return [...]string{"live", "moving-funds", "terminated", "terminated-committed", "terminated-dust"}[s]
}

type b15World struct {
	*legacyWorld
	frost *bind.BoundContract
	// script is the ADAPTER Bitcoin view the leader and followers use.
	script *b15Bitcoin
	// src is the FROST source; dest is setUpFrostDrain's FROST wallet, the
	// movement's only eligible target.
	src, dest *frostTarget
	// operators are the source descriptor's operators in seat order.
	operators []chain.Address
	// drain is the legacy drain into the source; sweep is the source's
	// incoming sweep, whose output 0 is its main.
	drain, sweep *bitcoin.Transaction
	// reader is the test's own client for state reads; leaders and
	// followers always start their own.
	reader *chainethereum.TbtcChain
}

// b15SourceKey is the FIXTURE source key's scalar; it signs in place of the
// source's FROST signers.
const b15SourceKey = 1002

// b15Bitcoin is an ADAPTER: the world's Core-backed Bitcoin chain plus the
// Electrum script query (redemption.ScriptUnspent) the FROST leader and
// planner use, answered from the blocks Core mined for this world.
type b15Bitcoin struct {
	*legacyRegtest
}

func (b *b15Bitcoin) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	c := b.legacyRegtest
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var out []bitcoin.PreviousOutput
	for _, block := range c.blocks {
		for _, tx := range block.txs {
			for i, output := range tx.Outputs {
				outpoint := bitcoin.TransactionOutpoint{TransactionHash: tx.Hash(), OutputIndex: uint32(i)}
				if !bytes.Equal(output.PublicKeyScript, script) || c.spentLocked(&outpoint, true) {
					continue
				}
				out = append(out, bitcoin.PreviousOutput{Outpoint: outpoint, Value: output.Value, PublicKeyScript: append(bitcoin.Script(nil), script...)})
			}
		}
	}
	return out, nil
}

// newB15World builds the upgraded system with setUpFrostDrain's
// configuration and FROST wallet (the movement target), registers the
// source, drains the legacy source into it and sweeps that incoming output
// (the source becomes Live), then puts the source into the start state.
func newB15World(t *testing.T, start b15Start) *b15World {
	t.Helper()
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	w := &b15World{src: &frostTarget{}, dest: &frostTarget{}}
	w.legacyWorld = newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), func(lw *legacyWorld) {
		setUpFrostDrain(w.dest)(lw)
		w.legacyWorld = lw
		w.registerSource(t)
	})
	w.frost = bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
	w.script = &b15Bitcoin{w.btc}
	w.drainIntoSource(t)
	w.sweepIncoming(t)

	switch start {
	case b15MovingFunds:
		w.ageOut(t)
		w.commit(t)
	case b15Terminated:
		w.terminate(t)
		w.commit(t)
	case b15TerminatedCommitted:
		w.ageOut(t)
		w.commit(t)
		w.terminate(t)
	case b15TerminatedDust:
		w.terminate(t)
		w.recordDust(t)
	}

	// Preconditions every case relies on.
	s := w.readSource(t)
	want := map[b15Start]movement.State{b15Live: movement.StateLive, b15MovingFunds: movement.StateMovingFunds,
		b15Terminated: movement.StateTerminated, b15TerminatedCommitted: movement.StateTerminated,
		b15TerminatedDust: movement.StateTerminated}[start]
	if s.Wallet.State != want {
		t.Fatalf("setup (%s): source state %d, want %d", start, s.Wallet.State, want)
	}
	if s.MainValue == 0 || s.Custody.MainOutpoint != deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: w.sweep.Hash()}) {
		t.Fatalf("setup (%s): the source holds no main (value %d)", start, s.MainValue)
	}
	if s.Lifecycle.Dust != (start == b15TerminatedDust) {
		t.Fatalf("setup (%s): source dust %v", start, s.Lifecycle.Dust)
	}
	movable := start == b15MovingFunds || start == b15Terminated || start == b15TerminatedCommitted
	if (s.Log != nil) != movable || s.Commitment.ProvenAt != 0 {
		t.Fatalf("setup (%s): source commitment recorded %v, proven at %d", start, s.Log != nil, s.Commitment.ProvenAt)
	}
	return w
}

// registerSource registers and readies the source through the registry
// FIXTURE's callbacks, as setUpFrostDrain registers its wallet, with the
// FIXTURE source key and a descriptor the K-07 planner accepts: the world's
// two operators, the first holding seats 1 and 3 under one member ID.
func (w *b15World) registerSource(t *testing.T) {
	t.Helper()
	frost := bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
	var out []interface{}
	if err := frost.Call(&bind.CallOpts{}, &out, "frostRegistry"); err != nil || len(out) != 1 {
		t.Fatalf("setup: frostRegistry: %v", err)
	}
	registryAddress := out[0].(common.Address)
	artifact := w.baseline.artifact("contracts/test/FrostRegistryFixture.sol", "FrostRegistryFixture")
	registryABI, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		t.Fatal(err)
	}
	registry := bind.NewBoundContract(registryAddress, registryABI, w.admin, w.admin, w.admin)
	key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(b15SourceKey).Bytes(), 32))
	if err != nil {
		t.Fatal(err)
	}
	copy(w.src.q[:], crypto.CompressPubkey(&key.PublicKey)[1:])
	head, err := w.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	descriptorType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "scheme", Type: "uint8"}, {Name: "profile", Type: "uint8"},
		{Name: "chainId", Type: "uint256"}, {Name: "registry", Type: "address"},
		{Name: "epoch", Type: "uint64"}, {Name: "members", Type: "uint32[]"},
		{Name: "operators", Type: "address[]"}, {Name: "threshold", Type: "uint16"},
		{Name: "snowfallDescriptor", Type: "bytes32"}, {Name: "outputKey", Type: "bytes32"},
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	operators := []common.Address{w.operator, admin, w.operator}
	descriptor := struct {
		Scheme             uint8
		Profile            uint8
		ChainId            *big.Int
		Registry           common.Address
		Epoch              uint64
		Members            []uint32
		Operators          []common.Address
		Threshold          uint16
		SnowfallDescriptor [32]byte
		OutputKey          [32]byte
	}{2, 1, w.chainID, registryAddress, head, []uint32{1, 2, 1}, operators,
		2, crypto.Keccak256Hash([]byte("batch-15 FROST source")), w.src.q}
	encoded, err := abi.Arguments{{Type: descriptorType}}.Pack(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	tagged, err := abi.Arguments{{Type: bytes32Type}, {Type: descriptorType}}.Pack(crypto.Keccak256Hash([]byte("tbtc-v2/frost-descriptor/v1")), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	w.src.id = crypto.Keccak256Hash(crypto.Keccak256([]byte("tbtc-v2/frost-wallet-id/v1")), w.src.q[:])
	copy(w.src.label[:], w.src.id[:20])
	w.send("setup: FROST source created", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return registry.Transact(o, "created", encoded, head+100000)
	})
	w.send("setup: FROST source ready", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return registry.Transact(o, "ready", w.src.id, crypto.Keccak256Hash(tagged))
	})
	for _, operator := range operators {
		w.operators = append(w.operators, chain.Address(operator.Hex()))
	}
}

// drainIntoSource: setup, as batch 3's newFrostIncomingWorld. The legacy
// MovingFunds wallet commits its drain to the source, the drain (signed with
// the legacy source's test key) is mined by Core and proven on the Bridge,
// so the source holds one pending FROST incoming output.
func (w *b15World) drainIntoSource(t *testing.T) {
	t.Helper()
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(operatorKey, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.legacy.SubmitMovingFundsCommitment(opts, w.source.pkh, legacyUTXO(w.main), []uint32{1}, big.NewInt(1), [][20]byte{w.src.label})
	if err != nil {
		t.Fatalf("setup: drain commitment: %v", err)
	}
	if receipt, err := legacyWaitMined(context.Background(), w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: drain commitment not mined: %v", err)
	}
	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddPublicKeyHashInput(w.main); err != nil {
		t.Fatal(err)
	}
	script, err := bitcoin.PayToTaproot(w.src.q)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: w.main.Value - frostDrainFee, PublicKeyScript: script})
	w.drain = legacySign(t, builder, w.source.key)
	w.btc.mine(t, w.drain)
	w.send("setup: drain proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsProof(o, legacyTxInfo(w.drain), w.btc.proof(t, w.drain.Hash()), legacyUTXO(w.main), w.source.pkh)
	})
}

// readSource is one production read of the source (movement.View.Source),
// through the test's own reader client (started once).
func (w *b15World) readSource(t *testing.T) movement.Source {
	t.Helper()
	if w.reader == nil {
		w.reader = w.clientWith(t, revertCode3, nil)
	}
	bridge, err := w.reader.FrostBridge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	view := &movement.View{Chain: bridge, Policy: movement.Policy{FinalityBlocks: tbtc.FrostFinalityBlocks}}
	s, err := view.Source(context.Background(), w.src.id)
	if err != nil {
		t.Fatalf("source read: %v", err)
	}
	return s
}

// clientWith is a production client started fresh behind a recorder ADAPTER
// with the given empty-revert encoding; the recorder is returned through rec.
func (w *b15World) clientWith(t *testing.T, encoding legacyRevertEncoding, rec **legacyRecorder) *chainethereum.TbtcChain {
	t.Helper()
	recorder := newLegacyRecorder(t, w.anvilURL, encoding)
	c, err := w.connect(recorder)
	if err != nil {
		t.Fatalf("client startup (%v): %v", encoding, err)
	}
	if rec != nil {
		*rec = recorder
	}
	return c
}

// signAsSource signs every digest of a key-path spend with the FIXTURE
// source key.
func signAsSource(t *testing.T, snapshot *bitcoin.TaprootTransaction) *bitcoin.Transaction {
	t.Helper()
	private, _ := btcec.PrivKeyFromBytes(common.LeftPadBytes(big.NewInt(b15SourceKey).Bytes(), 32))
	var signatures []bitcoin.Signature
	for _, digest := range snapshot.Digests() {
		signature, err := schnorr.Sign(private, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		typed, err := bitcoin.NewSchnorrSignature(signature.Serialize())
		if err != nil {
			t.Fatal(err)
		}
		signatures = append(signatures, typed)
	}
	tx, err := snapshot.AddSignatures(signatures)
	if err != nil {
		t.Fatalf("signatures: %v", err)
	}
	return tx
}

// sweepIncoming: setup. The source sweeps its pending incoming output into
// its first main, proven on the real Bridge; the source becomes Live.
func (w *b15World) sweepIncoming(t *testing.T) {
	t.Helper()
	input := bitcoin.TransactionOutpoint{TransactionHash: w.drain.Hash(), OutputIndex: 0}
	value := w.main.Value - frostDrainFee
	script, err := bitcoin.PayToTaproot(w.src.q)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := &bitcoin.Transaction{
		Version: 2,
		Inputs:  []*bitcoin.TransactionInput{{Outpoint: &input, Sequence: 0xffffffff}},
		Outputs: []*bitcoin.TransactionOutput{{Value: value - b15SweepFee, PublicKeyScript: script}},
	}
	snapshot, err := bitcoin.NewTaprootTransaction(unsigned, []bitcoin.PreviousOutput{{Outpoint: input, Value: value, PublicKeyScript: script}}, w.src.q)
	if err != nil {
		t.Fatalf("setup: sweep snapshot: %v", err)
	}
	w.sweep = signAsSource(t, snapshot)
	w.btc.mine(t, w.sweep)
	w.send("setup: incoming sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostMovedFundsSweepProof", w.src.id, legacyTxInfo(w.sweep), w.btc.proof(t, w.sweep.Hash()))
	})
	if s := w.readSource(t); s.Wallet.State != movement.StateLive {
		t.Fatalf("setup: the swept source is in state %d, not Live", s.Wallet.State)
	}
}

// stranger is a fresh funded account that is neither an operator nor the
// deployer: the permissionless notifications are made by anyone.
func (w *b15World) stranger(t *testing.T) *bind.TransactOpts {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	if address == crypto.PubkeyToAddress(w.adminKey.PublicKey) {
		t.Fatal("setup: the stranger is the deployer")
	}
	for _, operator := range w.operators {
		if common.HexToAddress(string(operator)) == address {
			t.Fatal("setup: the stranger is an operator")
		}
	}
	// Enough for gas and any bond (the fraud challenge deposit).
	balance := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000))
	if err := w.admin.Client().Call(nil, "anvil_setBalance", address, hexutil.EncodeBig(balance)); err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(key, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	opts.Context = context.Background()
	return opts
}

// public sends a permissionless Bridge call from a stranger.
func (w *b15World) public(t *testing.T, what, method string, args ...interface{}) {
	t.Helper()
	opts := w.stranger(t)
	tx, err := w.frost.Transact(opts, method, args...)
	if err != nil {
		t.Fatalf("setup: %s: %v", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), b15Bound)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: %s not mined: %v", what, err)
	}
}

// ageOut: setup. Governance shortens the wallet max age to one hour (the
// chain clock stays legacyChainLag behind the wall clock, so the wall-clock
// moving-funds safety margin has passed), the hour passes on the chain
// clock, and a stranger notifies the source closeable: MovingFunds.
func (w *b15World) ageOut(t *testing.T) {
	t.Helper()
	const maxAge = 3600
	p, err := w.legacy.WalletParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("setup: wallet max age", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateWalletParameters(o, p.WalletCreationPeriod, p.WalletCreationMinBtcBalance,
			p.WalletCreationMaxBtcBalance, p.WalletClosureMinBtcBalance, maxAge, p.WalletMaxBtcTransfer, p.WalletClosingPeriod)
	})
	w.elapse(maxAge + 1)
	w.public(t, "notifyFrostWalletCloseable", "notifyFrostWalletCloseable", w.src.id)
}

// terminate: setup. Governance shortens the fraud defeat timeout to 1 s; a
// stranger challenges a digest the source signed (FIXTURE key) with the
// bond; nobody defeats it, and the stranger notifies the timeout: Terminated.
func (w *b15World) terminate(t *testing.T) {
	t.Helper()
	p, err := w.legacy.FraudParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("setup: fraud defeat timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateFraudParameters(o, p.FraudChallengeDepositAmount, 1, p.FraudSlashingAmount, p.FraudNotifierRewardMultiplier)
	})
	digest := crypto.Keccak256Hash([]byte("batch-15 digest the source never authorized"))
	private, _ := btcec.PrivKeyFromBytes(common.LeftPadBytes(big.NewInt(b15SourceKey).Bytes(), 32))
	signature, err := schnorr.Sign(private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	opts := w.stranger(t)
	opts.Value = p.FraudChallengeDepositAmount
	tx, err := w.frost.Transact(opts, "submitFrostFraudChallenge", w.src.id, digest, signature.Serialize())
	if err != nil {
		t.Fatalf("setup: fraud challenge: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), b15Bound)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: fraud challenge not mined: %v", err)
	}
	w.elapse(2)
	w.public(t, "notifyFrostFraudChallengeDefeatTimeout", "notifyFrostFraudChallengeDefeatTimeout", w.src.id, digest)
}

// recordDust: setup. Governance raises the moving-funds dust threshold above
// the source's main (the redemption dust threshold must stay above it), and a
// stranger records the dust; a Terminated source stays Terminated.
func (w *b15World) recordDust(t *testing.T) {
	t.Helper()
	dust := uint64(w.sweep.Outputs[0].Value) + 1
	r, err := w.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("setup: redemption dust", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateRedemptionParameters(o, 2*dust, r.RedemptionTreasuryFeeDivisor,
			r.RedemptionTxMaxFee, r.RedemptionTxMaxTotalFee, r.RedemptionTimeout,
			r.RedemptionTimeoutSlashingAmount, r.RedemptionTimeoutNotifierRewardMultiplier)
	})
	p, err := w.legacy.MovingFundsParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("setup: moving funds dust", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateMovingFundsParameters(o, p.MovingFundsTxMaxTotalFee, dust,
			p.MovingFundsTimeoutResetDelay, p.MovingFundsTimeout, p.MovingFundsTimeoutSlashingAmount,
			p.MovingFundsTimeoutNotifierRewardMultiplier, p.MovingFundsCommitmentGasOffset,
			p.MovedFundsSweepTxMaxTotalFee, p.MovedFundsSweepTimeout,
			p.MovedFundsSweepTimeoutSlashingAmount, p.MovedFundsSweepTimeoutNotifierRewardMultiplier)
	})
	w.public(t, "notifyFrostMovingFundsBelowDust", "notifyFrostMovingFundsBelowDust", w.src.id)
}

// operatorOpts is the transactor of the source's seat-0 operator (the
// world's operator key).
func (w *b15World) operatorOpts(t *testing.T) *bind.TransactOpts {
	t.Helper()
	key, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(key, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	opts.Context = context.Background()
	return opts
}

// commit: setup for every movable case, and the precondition that the
// Bridge accepts this source's movement: the source's seat-0 operator
// commits the movement to the target, as the contract allows any seat
// operator to (FrostMovement.commit, seat = descriptor index). SETUP, not
// the leader: the leader's own commitment (tbtcpg frostExecutingSeat) sends
// the 1-based seat that FrostMovement.commit reads as a 0-based index, so it
// reverts "FROST movement operator" in this world (seat 1 is another
// operator) for any source, MovingFunds included; that is a separate defect
// outside #141, so the windows here start from a recorded commitment.
func (w *b15World) commit(t *testing.T) {
	t.Helper()
	tx, err := w.frost.Transact(w.operatorOpts(t), "submitFrostMovingFundsCommitment", w.src.id, uint16(0), [][20]byte{w.dest.label})
	if err != nil {
		t.Fatalf("precondition: the Bridge refuses the source's movement commitment: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), b15Bound)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("precondition: the Bridge did not record the source's movement commitment: %v", err)
	}
}

// commitRefusal dry-runs the source's commitment as its seat-0 operator and
// returns the Bridge's refusal ("" if the Bridge would accept it).
func (w *b15World) commitRefusal(t *testing.T) string {
	t.Helper()
	opts := w.operatorOpts(t)
	opts.NoSend = true
	if _, err := w.frost.Transact(opts, "submitFrostMovingFundsCommitment", w.src.id, uint16(0), [][20]byte{w.dest.label}); err != nil {
		return err.Error()
	}
	return ""
}

// request is the window's coordination request for the FROST source with
// moving funds on its checklist.
func (w *b15World) request(leader chain.Address) *tbtc.CoordinationProposalRequest {
	return &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.src.label,
		WalletOperators:     w.operators,
		ExecutingOperator:   leader,
		ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
		FrostWallet:         true,
	}
}

// window runs one window's leader routine: the production proposal generator
// for the source, on a fresh client, bounded.
func (w *b15World) window(t *testing.T, leader chain.Address, encoding legacyRevertEncoding) (tbtc.CoordinationProposal, *legacyRecorder, error) {
	t.Helper()
	var rec *legacyRecorder
	c := w.clientWith(t, encoding, &rec)
	type result struct {
		p   tbtc.CoordinationProposal
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := tbtcpg.NewProposalGenerator(c, w.script).Generate(w.request(leader))
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		return r.p, rec, r.err
	case <-time.After(b15Bound):
		t.Fatalf("leader %s (%v): proposal generation did not finish within %v", leader, encoding, b15Bound)
		return nil, nil, nil
	}
}

// commitmentAttempts counts the recorder's eth_estimateGas and
// eth_sendRawTransaction requests carrying submitFrostMovingFundsCommitment:
// each is an attempt to create a movement obligation.
func (w *b15World) commitmentAttempts(rec *legacyRecorder) int {
	selector := fmt.Sprintf("%x", w.frostABI().Methods["submitFrostMovingFundsCommitment"].ID)
	n := 0
	for _, call := range rec.Calls() {
		if (call.Method == "eth_estimateGas" || call.Method == "eth_sendRawTransaction") &&
			call.To == w.bridge && call.Selector == selector {
			n++
		}
	}
	return n
}

// follow is one follower's handling of a movement proposal, as
// frostWallet.moveFunds runs it: the K-07 planner builds the movement of the
// recorded commitment with the proposal's exact fee, the plan must pay the
// proposed targets in order, and the signing authority authorizes it.
func (w *b15World) follow(t *testing.T, encoding legacyRevertEncoding, p *tbtc.MovingFundsProposal) (*signing.Plan, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), b15Bound)
	defer cancel()
	c := w.clientWith(t, encoding, nil)
	bridge, err := c.FrostBridge(ctx)
	if err != nil {
		return nil, err
	}
	view := &movement.View{Chain: bridge, Policy: movement.Policy{FinalityBlocks: tbtc.FrostFinalityBlocks}}
	s, err := view.Source(ctx, w.src.id)
	if err != nil {
		return nil, err
	}
	if p.MovingFundsTxFee == nil || p.MovingFundsTxFee.Sign() <= 0 || !p.MovingFundsTxFee.IsInt64() {
		return nil, errors.New("proposed moving funds transaction fee must be positive")
	}
	planner := movement.Planner{
		View: view, Bitcoin: w.script, Wallet: s.Wallet.Descriptor,
		// The node's configured Bitcoin genesis: regtest here.
		Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash),
		FeeRate: 1, MaximumSatPerVByte: b15MaximumSatPerVByte, Fee: p.MovingFundsTxFee.Int64(),
	}
	plan, err := planner.Movement(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot plan movement: [%w]", err)
	}
	targets := plan.MovementTargets()
	if len(targets) != len(p.TargetWallets) {
		return nil, errors.New("movement plan does not pay the proposed targets")
	}
	for i, target := range targets {
		if target.Label != p.TargetWallets[i] {
			return nil, errors.New("movement plan does not pay the proposed targets")
		}
	}
	if _, err := (&movement.Authority{View: view}).AuthorizeSweep(ctx, plan); err != nil {
		return nil, fmt.Errorf("authority refused the movement: [%w]", err)
	}
	return plan, nil
}

// awaitFinal waits (bounded) until the source's commitment log is final for
// the K-07 view, as a member's signing round starts FinalityBlocks after it.
func (w *b15World) awaitFinal(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(b15Bound)
	for {
		s := w.readSource(t)
		if s.Log == nil {
			return
		}
		if s.Head.Number >= s.Log.Block+tbtc.FrostFinalityBlocks {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup: the commitment did not become final within %v", b15Bound)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// settle carries the accepted movement to Bitcoin (FIXTURE source key, Core
// mines it) and proves it on the real Bridge, then checks the Bridge's
// record: the commitment is proven, the source holds nothing, the target
// holds the pending incoming output, and the source is Closing (from
// MovingFunds) or still Terminated.
func (w *b15World) settle(t *testing.T, plan *signing.Plan, start b15Start) {
	t.Helper()
	tx := signAsSource(t, plan.Snapshot())
	w.btc.mine(t, tx)
	w.send("movement proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostMovingFundsProof", w.src.id, legacyTxInfo(tx), w.btc.proof(t, tx.Hash()))
	})
	s := w.readSource(t)
	if s.Commitment.ProvenAt == 0 || s.Custody.Value != 0 {
		t.Fatalf("%sthe Bridge did not record the proven movement of the %s source (proven at %d, custody %d)", b15Prefix, start, s.Commitment.ProvenAt, s.Custody.Value)
	}
	want := movement.StateTerminated
	if start == b15MovingFunds {
		want = movement.StateClosing
	}
	if s.Wallet.State != want {
		t.Fatalf("%sthe %s source is in state %d after its proven movement, want %d", b15Prefix, start, s.Wallet.State, want)
	}
	var out []interface{}
	if err := w.frost.Call(&bind.CallOpts{}, &out, "frostIncoming", deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: tx.Hash()})); err != nil || len(out) != 1 {
		t.Fatalf("frostIncoming: %v", err)
	}
	record := *abi.ConvertType(out[0], new(frostIncomingRecord)).(*frostIncomingRecord)
	if record.Target != w.dest.id || record.State != 1 || record.Value != uint64(tx.Outputs[0].Value) {
		t.Fatalf("%sthe target holds no pending incoming output for the proven movement: %+v", b15Prefix, record)
	}
}

// requireMovement: the window's proposal moves the source's funds to the
// committed (or to be committed) target with a fee within the movement cap.
func (w *b15World) requireMovement(t *testing.T, what string, proposal tbtc.CoordinationProposal, err error) *tbtc.MovingFundsProposal {
	t.Helper()
	p, ok := proposal.(*tbtc.MovingFundsProposal)
	if err != nil || !ok {
		t.Fatalf("%s%s: no movement proposal for the source (got %T, %v)", b15Prefix, what, proposal, err)
	}
	if len(p.TargetWallets) != 1 || p.TargetWallets[0] != w.dest.label {
		t.Fatalf("%s%s: the proposal moves to %x, not to the only eligible FROST target %x", b15Prefix, what, p.TargetWallets, w.dest.label)
	}
	s := w.readSource(t)
	if s.Log == nil {
		t.Fatalf("%s%s: the leader proposed a movement with no commitment recorded", b15Prefix, what)
	}
	if p.MovingFundsTxFee == nil || p.MovingFundsTxFee.Sign() <= 0 || p.MovingFundsTxFee.Uint64() > s.Commitment.MaxFee {
		t.Fatalf("%s%s: proposal fee %v is not within (0, %d], the committed cap", b15Prefix, what, p.MovingFundsTxFee, s.Commitment.MaxFee)
	}
	return p
}

// requireNoMovement: the window ends as base ends a window with nothing to
// move: no error, no movement proposal, and no attempt to commit.
func (w *b15World) requireNoMovement(t *testing.T, what string, proposal tbtc.CoordinationProposal, rec *legacyRecorder, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s%s: proposal generation failed: %v", b15Prefix, what, err)
	}
	if p, ok := proposal.(*tbtc.MovingFundsProposal); ok {
		t.Fatalf("%s%s: the leader proposed a movement to %x the Bridge refuses", b15Prefix, what, p.TargetWallets)
	}
	if n := w.commitmentAttempts(rec); n != 0 {
		t.Fatalf("%s%s: the leader attempted %d movement commitments the Bridge refuses", b15Prefix, what, n)
	}
}

// proposedEveryWindow runs one window per operator seat (each seat's
// operator as the leader, encodings alternating) and returns the last
// proposal and plan; each window must propose the movement, and followers
// on both encodings must accept each proposal.
func (w *b15World) proposedEveryWindow(t *testing.T, start b15Start) (*tbtc.MovingFundsProposal, *signing.Plan) {
	t.Helper()
	var accepted *tbtc.MovingFundsProposal
	var plan *signing.Plan
	for i, leader := range w.operators {
		encoding := legacyEncodings[i%len(legacyEncodings)]
		proposal, _, err := w.window(t, leader, encoding)
		p := w.requireMovement(t, fmt.Sprintf("window with leader %s (%v), %s source", leader, encoding, start), proposal, err)
		w.awaitFinal(t)
		for _, follower := range legacyEncodings {
			got, err := w.follow(t, follower, p)
			if err != nil {
				t.Fatalf("%sa follower (%v) refused the leader's movement of the %s source (fee %v): %v", b15Prefix, follower, start, p.MovingFundsTxFee, err)
			}
			plan = got
		}
		accepted = p
	}
	return accepted, plan
}

// requireRefusedEveryWindow: no window's leader proposes or commits, and the
// leader's routine ends cleanly; where a proposal is given, every follower
// refuses it.
func (w *b15World) requireRefusedEveryWindow(t *testing.T, what string, stale *tbtc.MovingFundsProposal) {
	t.Helper()
	for i, leader := range w.operators {
		encoding := legacyEncodings[i%len(legacyEncodings)]
		proposal, rec, err := w.window(t, leader, encoding)
		w.requireNoMovement(t, fmt.Sprintf("window with leader %s (%v), %s", leader, encoding, what), proposal, rec, err)
	}
	if stale == nil {
		return
	}
	for _, follower := range legacyEncodings {
		if _, err := w.follow(t, follower, stale); err == nil {
			t.Fatalf("%sa follower (%v) accepted a movement of the source after %s", b15Prefix, follower, what)
		}
	}
}

// TestFrostMovementTerminatedSourceProposed (tuxxy/keep-core#141, D30 (a)):
// for a source the Bridge lets move, MovingFunds (guard, base behaviour) or
// Terminated with nothing committed or with an unproven commitment, every
// window's leader, whichever seat's operator and on either empty-revert
// encoding, proposes the movement to the eligible FROST target from chain
// state; every follower plans and authorizes it; the movement carried to
// Bitcoin is accepted by the real Bridge. After that the movement is proven
// ("moved" sub-case): the Bridge refuses another commitment, no leader
// proposes or commits, and every follower refuses the stale proposal.
func TestFrostMovementTerminatedSourceProposed(t *testing.T) {
	for _, start := range []b15Start{b15MovingFunds, b15Terminated, b15TerminatedCommitted} {
		start := start
		t.Run(start.String(), func(t *testing.T) {
			t.Parallel()
			// The setup's commitment is the Bridge accepting this source's
			// movement (commit; prove is the settle step).
			w := newB15World(t, start)
			stop := w.mineInBackground()
			defer stop()

			accepted, plan := w.proposedEveryWindow(t, start)
			w.settle(t, plan, start)

			// Moved: the Bridge refuses another commitment, a Terminated
			// source because this period's commitment is proven, a former
			// MovingFunds source because it is now Closing.
			want := "FROST movement committed"
			if start == b15MovingFunds {
				want = "FROST movement source"
			}
			if reason := w.commitRefusal(t); !strings.Contains(reason, want) {
				t.Fatalf("precondition: the Bridge does not refuse a moved %s source's commitment with %q (%q)", start, want, reason)
			}
			w.requireRefusedEveryWindow(t, fmt.Sprintf("%s source whose movement is proven", start), accepted)
		})
	}
}

// TestFrostMovementTerminatedSourceRefused (tuxxy/keep-core#141, D30 (a)):
// a source the Bridge does not let move is never proposed and never
// committed: a Live wallet (not a movement source), and a Terminated wallet
// with recorded dust (FrostMovement.commit refuses "FROST movement
// committed"). Each window's leader routine ends cleanly, as base's does for a
// wallet with nothing to move.
func TestFrostMovementTerminatedSourceRefused(t *testing.T) {
	for _, c := range []struct {
		start  b15Start
		reason string
	}{
		{b15Live, "FROST movement source"},
		{b15TerminatedDust, "FROST movement committed"},
	} {
		c := c
		t.Run(c.start.String(), func(t *testing.T) {
			t.Parallel()
			w := newB15World(t, c.start)
			if reason := w.commitRefusal(t); !strings.Contains(reason, c.reason) {
				t.Fatalf("precondition: the Bridge does not refuse the %s source's commitment with %q (%q)", c.start, c.reason, reason)
			}
			stop := w.mineInBackground()
			defer stop()
			w.requireRefusedEveryWindow(t, fmt.Sprintf("%s source", c.start), nil)
		})
	}
}
