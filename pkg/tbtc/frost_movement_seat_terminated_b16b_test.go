package tbtc_test

// Remediation batch 16b acceptance tests: a FROST movement leader's own
// commitment for a Terminated source names a seat the Bridge maps to the
// leader. This is the Terminated half of tuxxy/keep-core#146 (P1), deferred by
// batch 16 (keep-core#147, MovingFunds only) until tuxxy/keep-core#141 let
// the leader reach the commitment for a Terminated source (keep-core#148).
// Refs tuxxy/keep-core#146; DECISIONS.md D30 (a).
//
// Invariant: when a Terminated FROST source has nothing committed in the
// current period (and no dust recorded, a positive main), every coordination
// window's leader, whichever of the wallet's operators it is (any seat, the
// last seat, an operator holding several seats), submits the movement
// commitment itself (tbtcpg.MovingFundsTask through ProposalGenerator.Generate,
// then movement.Commitments.Commit) and the real Bridge accepts it: the seat
// the host sends is one FrostMovement.commit maps to msg.sender (0-based
// descriptor index at tbtc-v2 39412825, FrostMovement.sol:92-95), whatever
// the source's state. An executing operator that holds no seat makes no
// commitment attempt.
//
// Review gap closed (REVIEW-TEST-B16.md follow-up): a host that converts the
// seat only for a MovingFunds source passes batch 16; here it fails, because
// the source is Terminated. Two routes reach Terminated with nothing
// committed, so a fix keyed on how the source got there fails one of them:
//   - "from-live": Live, then Terminated (no moving-funds request recorded);
//   - "from-moving-funds": aged out to MovingFunds, then Terminated before
//     anyone commits (a moving-funds request recorded).
//
// The contract is pinned as it is, for the Terminated source: before any
// window, each case dry-runs FrostMovement.commit on the real Bridge from
// every seat's operator at that seat (accepted), and from each operator at
// every seat it does not hold and one past the end (refused "FROST movement
// operator"); the upgraded build must be tbtc-v2 39412825.
//
// World: batch 16's world (keep-core#147, pkg/tbtc/frost_movement_seat_b16_test.go,
// frozen and not on this base, so its setup is repeated here under b16b
// names) on the frozen legacy world with the real upgraded Bridge
// (LEGACY_UPGRADED_DIR) and Bitcoin Core regtest, configured by
// setUpFrostDrain, whose FROST wallet is the movement's only eligible target.
// The source S is a second FROST wallet: the legacy MovingFunds wallet drains
// into it, S sweeps that incoming output and the sweep is proven, so S is Live
// with a main. S reaches Terminated by public calls only, as batch 15
// (pkg/tbtc/frost_movement_terminated_b15_test.go) does: governance shortens
// the fraud defeat timeout, a stranger challenges a digest S signed, nobody
// defeats it, and the stranger notifies the timeout. Nothing is committed in
// setup: the leader's own commitment is the subject.
//
// S's descriptor is members [1,2,1,3], operators [A,B,A,C] (threshold 3): A
// holds seats 0 and 2 under one member ID, B seat 1, C the last seat 3 alone.
// Batch 15's descriptor ([op,op,deployer]) is not used: its first leader's
// 1-based seat names itself, which hides #146.
//
// FIXTURES: the FROST registry fixture behind both wallets (no FROST DKG);
// the legacy source's test key (the drain); S's test key (scalar 1004), which
// signs in place of S's FROST signers (the incoming sweep and the fraud
// digest); operator keys B and C and the stranger's key (fixed test scalars,
// funded on Anvil); operator A is the world's operator key. ADAPTER: the
// Core-backed script query b16bBitcoin.
// RPC: every leader runs a production client (chainethereum.Connect) with its
// own operator key, behind batch 1's recorder ADAPTER, on both empty-revert
// encodings. No build tag (tuxxy/keep-core#64).

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
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
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// b16bPrefix names the invariant's issue in every assertion.
const b16bPrefix = "tuxxy/keep-core#146 (Terminated): "

// b16bPinnedContract is the tbtc-v2 commit whose FrostMovement.commit seat
// numbering (0-based descriptor index) these tests pin.
const b16bPinnedContract = "39412825454ae9d7d56811ce48cd826c92b821c2"

// b16bSweepFee is the fee of the source's incoming sweep (setup).
const b16bSweepFee = int64(1_000)

// b16bBound bounds one leader routine or one wait.
const b16bBound = 30 * time.Second

// b16bSourceKey is the FIXTURE source key's scalar; it signs in place of the
// source's FROST signers.
const b16bSourceKey = 1004

// FIXTURE operator and stranger key scalars (test values only).
const (
	b16bOperatorBKey = 0xb16bb
	b16bOperatorCKey = 0xb16bc
	b16bStrangerKey  = 0xb16bd
)

// b16bRoute is how the source reached Terminated with nothing committed.
type b16bRoute int

const (
	// b16bFromLive: Live, then Terminated.
	b16bFromLive b16bRoute = iota
	// b16bFromMovingFunds: aged out to MovingFunds, then Terminated before
	// anyone commits.
	b16bFromMovingFunds
)

func (r b16bRoute) String() string {
	return [...]string{"from-live", "from-moving-funds"}[r]
}

// b16bOperator is one of the source's operators: its key and the descriptor
// indexes (seats, 0-based as FrostMovement.commit reads them) it holds.
type b16bOperator struct {
	name  string
	key   *bind.TransactOpts
	pk    []byte // FIXTURE private key bytes, for the operator's own client
	addr  common.Address
	seats []uint16
}

type b16bWorld struct {
	*legacyWorld
	frost *bind.BoundContract
	// script is the ADAPTER Bitcoin view the leader uses.
	script *b16bBitcoin
	// src is the FROST source; dest is setUpFrostDrain's FROST wallet, the
	// movement's only eligible target.
	src, dest *frostTarget
	// descriptor is the source descriptor's operator list in seat order;
	// operators are its distinct operators A, B, C.
	descriptor []common.Address
	operators  []*b16bOperator
	stranger   *b16bOperator
	drain      *bitcoin.Transaction
	sweep      *bitcoin.Transaction
	reader     *chainethereum.TbtcChain
}

// b16bBitcoin is an ADAPTER: the world's Core-backed Bitcoin chain plus the
// Electrum script query (redemption.ScriptUnspent) the FROST leader uses,
// answered from the blocks Core mined for this world.
type b16bBitcoin struct {
	*legacyRegtest
}

func (b *b16bBitcoin) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
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

// newB16bWorld builds the upgraded system with setUpFrostDrain's
// configuration and FROST wallet (the movement target), registers the
// source, drains the legacy source into it, sweeps that incoming output (the
// source becomes Live), and terminates it by the route (Terminated, nothing
// committed).
func newB16bWorld(t *testing.T, route b16bRoute) *b16bWorld {
	t.Helper()
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	w := &b16bWorld{src: &frostTarget{}, dest: &frostTarget{}}
	w.legacyWorld = newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), func(lw *legacyWorld) {
		setUpFrostDrain(w.dest)(lw)
		w.legacyWorld = lw
		w.makeOperators(t)
		w.registerSource(t)
	})
	w.frost = bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
	w.script = &b16bBitcoin{w.btc}
	w.drainIntoSource(t)
	w.sweepIncoming(t)
	if route == b16bFromMovingFunds {
		w.ageOut(t)
		if s := w.readSource(t); s.Wallet.State != movement.StateMovingFunds || s.Lifecycle.MovingFundsRequestedAt == 0 {
			t.Fatalf("setup (%s): source state %d (moving funds requested at %d), want MovingFunds", route, s.Wallet.State, s.Lifecycle.MovingFundsRequestedAt)
		}
	}
	w.terminate(t)

	s := w.readSource(t)
	if s.Wallet.State != movement.StateTerminated || s.Lifecycle.TerminatedAt == 0 {
		t.Fatalf("setup (%s): source state %d (terminated at %d), want Terminated", route, s.Wallet.State, s.Lifecycle.TerminatedAt)
	}
	if (s.Lifecycle.MovingFundsRequestedAt != 0) != (route == b16bFromMovingFunds) {
		t.Fatalf("setup (%s): moving funds requested at %d", route, s.Lifecycle.MovingFundsRequestedAt)
	}
	if s.Lifecycle.Dust {
		t.Fatalf("setup (%s): the source's funds are recorded as dust", route)
	}
	if s.MainValue == 0 || s.Custody.MainOutpoint != deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: w.sweep.Hash()}) {
		t.Fatalf("setup (%s): the source holds no main (value %d)", route, s.MainValue)
	}
	if s.Log != nil || s.Commitment.ProvenAt != 0 {
		t.Fatalf("setup (%s): the source already has a commitment (recorded %v)", route, s.Log != nil)
	}
	// The host's request is built from the on-chain descriptor (as
	// frostNode.newFrostWallet builds the coordination executor's operators).
	if !slices.Equal(s.Wallet.Descriptor.Operators, w.descriptor) {
		t.Fatalf("setup: the Bridge's source descriptor operators %v, want %v", s.Wallet.Descriptor.Operators, w.descriptor)
	}
	return w
}

// terminate: setup, as batch 15's b15World.terminate. Governance shortens the
// fraud defeat timeout to 1 s; the stranger challenges a digest the source
// signed (FIXTURE key) with the bond; nobody defeats it, and the stranger
// notifies the timeout: Terminated.
func (w *b16bWorld) terminate(t *testing.T) {
	t.Helper()
	p, err := w.legacy.FraudParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("setup: fraud defeat timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateFraudParameters(o, p.FraudChallengeDepositAmount, 1, p.FraudSlashingAmount, p.FraudNotifierRewardMultiplier)
	})
	digest := crypto.Keccak256Hash([]byte("batch-16b digest the source never authorized"))
	private, _ := btcec.PrivKeyFromBytes(b16bKey(b16bSourceKey))
	signature, err := schnorr.Sign(private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	opts := *w.stranger.key
	opts.Value = p.FraudChallengeDepositAmount
	w.public(t, "fraud challenge", &opts, "submitFrostFraudChallenge", w.src.id, digest, signature.Serialize())
	w.elapse(2)
	w.public(t, "notifyFrostFraudChallengeDefeatTimeout", w.stranger.key, "notifyFrostFraudChallengeDefeatTimeout", w.src.id, digest)
}

// public sends a permissionless Bridge call (setup) and waits for it, bounded.
func (w *b16bWorld) public(t *testing.T, what string, opts *bind.TransactOpts, method string, args ...interface{}) {
	t.Helper()
	tx, err := w.frost.Transact(opts, method, args...)
	if err != nil {
		t.Fatalf("setup: %s: %v", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), b16bBound)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: %s not mined: %v", what, err)
	}
}

// b16bKey is a FIXTURE private key from a test scalar.
func b16bKey(scalar int64) []byte {
	return common.LeftPadBytes(big.NewInt(scalar).Bytes(), 32)
}

// makeOperators: A is the world's operator key; B, C and the stranger are
// FIXTURE keys, funded on Anvil. The descriptor is [A, B, A, C].
func (w *b16bWorld) makeOperators(t *testing.T) {
	t.Helper()
	a, err := hexutil.Decode("0x" + legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, raw []byte) *b16bOperator {
		key, err := crypto.ToECDSA(raw)
		if err != nil {
			t.Fatal(err)
		}
		opts, err := bind.NewKeyedTransactorWithChainID(key, w.chainID)
		if err != nil {
			t.Fatal(err)
		}
		opts.Context = context.Background()
		address := crypto.PubkeyToAddress(key.PublicKey)
		balance := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000))
		if err := w.admin.Client().Call(nil, "anvil_setBalance", address, hexutil.EncodeBig(balance)); err != nil {
			t.Fatal(err)
		}
		return &b16bOperator{name: name, key: opts, pk: raw, addr: address}
	}
	opA, opB, opC := mk("A", a), mk("B", b16bKey(b16bOperatorBKey)), mk("C", b16bKey(b16bOperatorCKey))
	w.stranger = mk("stranger", b16bKey(b16bStrangerKey))
	if opA.addr != w.operator {
		t.Fatal("setup: operator A is not the world's operator")
	}
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	for _, o := range []*b16bOperator{opA, opB, opC, w.stranger} {
		if o != opA && (o.addr == admin || o.addr == w.operator) {
			t.Fatalf("setup: FIXTURE key %s is the deployer or the world's operator", o.name)
		}
	}
	w.descriptor = []common.Address{opA.addr, opB.addr, opA.addr, opC.addr}
	w.operators = []*b16bOperator{opA, opB, opC}
	for i, address := range w.descriptor {
		for _, o := range w.operators {
			if o.addr == address {
				o.seats = append(o.seats, uint16(i))
			}
		}
	}
}

// registerSource registers and readies the source through the registry
// FIXTURE's callbacks, as setUpFrostDrain registers its wallet, with the
// FIXTURE source key and the [A,B,A,C] descriptor (members [1,2,1,3]).
func (w *b16bWorld) registerSource(t *testing.T) {
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
	key, err := crypto.ToECDSA(b16bKey(b16bSourceKey))
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
	}{2, 1, w.chainID, registryAddress, head, []uint32{1, 2, 1, 3}, w.descriptor,
		3, crypto.Keccak256Hash([]byte("batch-16b FROST source")), w.src.q}
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
}

// drainIntoSource: setup, as batch 3's newFrostIncomingWorld. The legacy
// MovingFunds wallet commits its drain to the source, the drain (signed with
// the legacy source's test key) is mined by Core and proven on the Bridge.
func (w *b16bWorld) drainIntoSource(t *testing.T) {
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

// sweepIncoming: setup. The source sweeps its pending incoming output into
// its first main (FIXTURE source key), proven on the real Bridge.
func (w *b16bWorld) sweepIncoming(t *testing.T) {
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
		Outputs: []*bitcoin.TransactionOutput{{Value: value - b16bSweepFee, PublicKeyScript: script}},
	}
	snapshot, err := bitcoin.NewTaprootTransaction(unsigned, []bitcoin.PreviousOutput{{Outpoint: input, Value: value, PublicKeyScript: script}}, w.src.q)
	if err != nil {
		t.Fatalf("setup: sweep snapshot: %v", err)
	}
	private, _ := btcec.PrivKeyFromBytes(b16bKey(b16bSourceKey))
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
	if w.sweep, err = snapshot.AddSignatures(signatures); err != nil {
		t.Fatalf("setup: sweep signatures: %v", err)
	}
	w.btc.mine(t, w.sweep)
	w.send("setup: incoming sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostMovedFundsSweepProof", w.src.id, legacyTxInfo(w.sweep), w.btc.proof(t, w.sweep.Hash()))
	})
	if s := w.readSource(t); s.Wallet.State != movement.StateLive {
		t.Fatalf("setup: the swept source is in state %d, not Live", s.Wallet.State)
	}
}

// ageOut: setup. Governance shortens the wallet max age to one hour, the hour
// passes on the chain clock (legacyChainLag keeps the wall-clock safety
// margin passed), and the stranger notifies the source closeable.
func (w *b16bWorld) ageOut(t *testing.T) {
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
	tx, err := w.frost.Transact(w.stranger.key, "notifyFrostWalletCloseable", w.src.id)
	if err != nil {
		t.Fatalf("setup: notifyFrostWalletCloseable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), b16bBound)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: notifyFrostWalletCloseable not mined: %v", err)
	}
}

// readSource is one production read of the source (movement.View.Source).
func (w *b16bWorld) readSource(t *testing.T) movement.Source {
	t.Helper()
	if w.reader == nil {
		w.reader = w.clientAs(t, w.operators[0], revertCode3, nil)
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

// clientAs is a production client started fresh with the operator's own key,
// behind a recorder ADAPTER with the given empty-revert encoding.
func (w *b16bWorld) clientAs(t *testing.T, o *b16bOperator, encoding legacyRevertEncoding, rec **legacyRecorder) *chainethereum.TbtcChain {
	t.Helper()
	key, err := crypto.ToECDSA(o.pk)
	if err != nil {
		t.Fatal(err)
	}
	recorder := newLegacyRecorder(t, w.anvilURL, encoding)
	config := w.clientConfig(recorder.URL())
	config.Account.KeyFile = legacyKeyFile(t, key)
	_, c, _, _, _, err := chainethereum.Connect(context.Background(), config)
	if err != nil {
		t.Fatalf("client startup (%s, %v): %v", o.name, encoding, err)
	}
	if rec != nil {
		*rec = recorder
	}
	return c
}

// commitRefusal dry-runs FrostMovement.commit from o at the given seat on the
// real Bridge and returns the refusal ("" if the Bridge would accept it).
func (w *b16bWorld) commitRefusal(o *b16bOperator, seat uint16) string {
	opts := *o.key
	opts.NoSend = true
	if _, err := w.frost.Transact(&opts, "submitFrostMovingFundsCommitment", w.src.id, seat, [][20]byte{w.dest.label}); err != nil {
		return err.Error()
	}
	return ""
}

// pinContract: the Bridge's seat numbering is FrostMovement.commit's at
// tbtc-v2 39412825, checked on the real Bridge for this source: each seat's
// operator may commit at that seat (0-based descriptor index); no operator
// may commit at a seat it does not hold or one past the end.
func (w *b16bWorld) pinContract(t *testing.T) {
	t.Helper()
	n := uint16(len(w.descriptor))
	for _, o := range w.operators {
		for seat := uint16(0); seat <= n; seat++ {
			holds := slices.Contains(o.seats, seat)
			reason := w.commitRefusal(o, seat)
			if holds && reason != "" {
				t.Fatalf("%scontract pin: the Bridge refuses operator %s (%s) committing at its seat %d (0-based descriptor index, FrostMovement.commit at %s): %s",
					b16bPrefix, o.name, o.addr, seat, b16bPinnedContract[:8], reason)
			}
			if !holds && !strings.Contains(reason, "FROST movement operator") {
				t.Fatalf("%scontract pin: the Bridge does not refuse operator %s (%s) committing at seat %d it does not hold with %q (got %q)",
					b16bPrefix, o.name, o.addr, seat, "FROST movement operator", reason)
			}
		}
	}
	// The environment's commit only (upgradedBuild returns
	// LEGACY_UPGRADED_COMMIT); the dry-runs above are the real guard.
	if _, commit := upgradedBuild(t); commit != b16bPinnedContract {
		t.Fatalf("%scontract pin: the upgraded Bridge is built from %s, not tbtc-v2 %s", b16bPrefix, commit, b16bPinnedContract)
	}
}

// request is the window's coordination request for the source, with the
// wallet operators in descriptor order as the node builds them.
func (w *b16bWorld) request(leader common.Address) *tbtc.CoordinationProposalRequest {
	operators := make([]chain.Address, len(w.descriptor))
	for i, operator := range w.descriptor {
		operators[i] = chain.Address(operator.Hex())
	}
	return &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.src.label,
		WalletOperators:     operators,
		ExecutingOperator:   chain.Address(leader.Hex()),
		ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
		FrostWallet:         true,
	}
}

// window runs one window's leader routine as o: the production proposal
// generator on o's own fresh client, bounded.
func (w *b16bWorld) window(t *testing.T, o *b16bOperator, encoding legacyRevertEncoding) (tbtc.CoordinationProposal, *legacyRecorder, error) {
	t.Helper()
	var rec *legacyRecorder
	c := w.clientAs(t, o, encoding, &rec)
	type result struct {
		p   tbtc.CoordinationProposal
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := tbtcpg.NewProposalGenerator(c, w.script).Generate(w.request(o.addr))
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		return r.p, rec, r.err
	case <-time.After(b16bBound):
		t.Fatalf("leader %s (%v): proposal generation did not finish within %v", o.name, encoding, b16bBound)
		return nil, nil, nil
	}
}

// commitmentAttempts counts the recorder's eth_estimateGas and
// eth_sendRawTransaction requests carrying submitFrostMovingFundsCommitment.
func (w *b16bWorld) commitmentAttempts(rec *legacyRecorder) int {
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

// recordedCommitment finds the source's recorded commitment transaction in
// the block of its FrostMovingFundsCommitted log and returns its sender and
// the seat it named.
func (w *b16bWorld) recordedCommitment(t *testing.T, s movement.Source) (common.Address, uint16) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), b16bBound)
	defer cancel()
	block, err := w.admin.BlockByHash(ctx, s.Log.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	method := w.frostABI().Methods["submitFrostMovingFundsCommitment"]
	signer := types.LatestSignerForChainID(w.chainID)
	for _, tx := range block.Transactions() {
		if tx.To() == nil || *tx.To() != w.bridge || len(tx.Data()) < 4 || !bytes.Equal(tx.Data()[:4], method.ID) {
			continue
		}
		receipt, err := w.admin.TransactionReceipt(ctx, tx.Hash())
		if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}
		args, err := method.Inputs.Unpack(tx.Data()[4:])
		if err != nil {
			t.Fatal(err)
		}
		if args[0].([32]byte) != w.src.id {
			continue
		}
		sender, err := types.Sender(signer, tx)
		if err != nil {
			t.Fatal(err)
		}
		return sender, args[1].(uint16)
	}
	t.Fatalf("no successful submitFrostMovingFundsCommitment for the source in block %d", s.Log.Block)
	return common.Address{}, 0
}

// snapshot takes an Anvil snapshot of the world's state (setup: the

// snapshot takes an Anvil snapshot of the world's state (setup: the
// background miner must be stopped).
func (w *b16bWorld) snapshot(t *testing.T) string {
	t.Helper()
	var id string
	if err := w.admin.Client().Call(&id, "evm_snapshot"); err != nil {
		t.Fatalf("setup: evm_snapshot: %v", err)
	}
	return id
}

// revertTo restores the snapshot (Anvil consumes its id), takes a new one for
// the next case, and checks the source is again Terminated with nothing
// committed.
func (w *b16bWorld) revertTo(t *testing.T, id string) string {
	t.Helper()
	var reverted bool
	if err := w.admin.Client().Call(&reverted, "evm_revert", id); err != nil || !reverted {
		t.Fatalf("setup: evm_revert %s: %v", id, err)
	}
	next := w.snapshot(t)
	if s := w.readSource(t); s.Wallet.State != movement.StateTerminated || s.Log != nil {
		t.Fatalf("setup: after the revert the source is in state %d with a commitment recorded %v", s.Wallet.State, s.Log != nil)
	}
	return next
}

// TestFrostMovementTerminatedLeaderOwnCommitment (tuxxy/keep-core#146,
// Terminated half): for a Terminated source with nothing committed, reached
// from Live or from MovingFunds, the window's leader, for every operator of
// the wallet (A: seats 0 and 2; B: seat 1; C: the last seat 3 alone), on
// either empty-revert encoding, submits the commitment itself and the real
// Bridge accepts it: the commitment is recorded to the only eligible target,
// sent by the leader, at a seat the descriptor maps to the leader, the source
// stays Terminated, and the window proposes the movement. Before that, an
// executing operator with no seat proposes nothing and makes no commitment
// attempt, and the contract's numbering is pinned for the Terminated source
// (pinContract).
//
// One world per route and encoding: after setup the Anvil state is
// snapshotted, and each leader's case starts from evm_revert to it (as batch
// 16 does), so every leader faces the same Terminated source with nothing
// committed. The leader path writes nothing to Core (b16bBitcoin only reads
// mined blocks), and every window starts fresh clients.
func TestFrostMovementTerminatedLeaderOwnCommitment(t *testing.T) {
	for _, route := range []b16bRoute{b16bFromLive, b16bFromMovingFunds} {
		for _, encoding := range legacyEncodings {
			t.Run(route.String()+"/"+encoding.String(), func(t *testing.T) {
				t.Parallel()
				world := newB16bWorld(t, route)
				snapshot := world.snapshot(t)
				for _, leader := range []string{"A", "B", "C"} {
					t.Run(leader, func(t *testing.T) {
						w := world
						snapshot = w.revertTo(t, snapshot)
						stop := w.mineInBackground()
						defer stop()
						w.pinContract(t)

						var o *b16bOperator
						for _, candidate := range w.operators {
							if candidate.name == leader {
								o = candidate
							}
						}
						what := fmt.Sprintf("leader %s %s (seats %v of %d, %v), Terminated source %s", o.name, o.addr, o.seats, len(w.descriptor), encoding, route)

						// Negative: an executing operator that holds no seat
						// commits nothing and gets no proposal.
						proposal, rec, err := w.window(t, w.stranger, encoding)
						if _, ok := proposal.(*tbtc.MovingFundsProposal); ok {
							t.Fatalf("%san executing operator with no seat proposed a movement (error %v)", b16bPrefix, err)
						}
						if n := w.commitmentAttempts(rec); n != 0 {
							t.Fatalf("%san executing operator with no seat attempted %d movement commitments", b16bPrefix, n)
						}
						if s := w.readSource(t); s.Log != nil {
							t.Fatalf("%sa commitment was recorded for an executing operator with no seat", b16bPrefix)
						}

						// The leader's own commitment.
						proposal, rec, err = w.window(t, o, encoding)
						p, ok := proposal.(*tbtc.MovingFundsProposal)
						if err != nil || !ok {
							t.Fatalf("%s%s: the leader's own movement commitment was not accepted by the Bridge (proposal %T, error %v)", b16bPrefix, what, proposal, err)
						}
						if w.commitmentAttempts(rec) == 0 {
							t.Fatalf("%s%s: the leader proposed without submitting the commitment itself", b16bPrefix, what)
						}
						s := w.readSource(t)
						if s.Log == nil || len(s.Commitment.Targets) != 1 || s.Commitment.Targets[0] != w.dest.label {
							t.Fatalf("%s%s: the Bridge holds no commitment of the source to the only eligible target (recorded %v, targets %x)", b16bPrefix, what, s.Log != nil, s.Commitment.Targets)
						}
						if s.Wallet.State != movement.StateTerminated {
							t.Fatalf("%s%s: the source is in state %d after the commitment, not Terminated", b16bPrefix, what, s.Wallet.State)
						}
						sender, seat := w.recordedCommitment(t, s)
						if sender != o.addr {
							t.Fatalf("%s%s: the recorded commitment was sent by %s, not the leader", b16bPrefix, what, sender)
						}
						if !slices.Contains(o.seats, seat) || w.descriptor[seat] != o.addr {
							t.Fatalf("%s%s: the commitment names seat %d, which the descriptor maps to another operator", b16bPrefix, what, seat)
						}
						if len(p.TargetWallets) != 1 || p.TargetWallets[0] != w.dest.label {
							t.Fatalf("%s%s: the proposal moves to %x, not to the committed target %x", b16bPrefix, what, p.TargetWallets, w.dest.label)
						}
						if p.MovingFundsTxFee == nil || p.MovingFundsTxFee.Sign() <= 0 || p.MovingFundsTxFee.Uint64() > s.Commitment.MaxFee {
							t.Fatalf("%s%s: proposal fee %v is not within (0, %d], the committed cap", b16bPrefix, what, p.MovingFundsTxFee, s.Commitment.MaxFee)
						}
					})
				}
			})
		}
	}
}
