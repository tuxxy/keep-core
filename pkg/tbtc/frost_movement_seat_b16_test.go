package tbtc_test

// Remediation batch 16 acceptance tests: a FROST movement leader's own
// commitment names a seat the Bridge maps to the leader.
// Refs tuxxy/keep-core#146 (P1; found as batch 15 note N1).
//
// Invariant: when a MovingFunds FROST source has nothing committed in the
// current period, every coordination window's leader, whichever of the
// wallet's operators it is (any seat, the last seat, an operator holding
// several seats), submits the movement commitment itself
// (tbtcpg.MovingFundsTask through ProposalGenerator.Generate, then
// movement.Commitments.Commit) and the real Bridge accepts it: the seat the
// host sends is one FrostMovement.commit maps to msg.sender, so the numbering
// the host uses is the contract's (0-based descriptor index at tbtc-v2
// 39412825, FrostMovement.sol:92-95). Any seat the leader holds is valid;
// a seat held by another operator, or past the end, is not. An executing
// operator that holds no seat makes no commitment attempt.
//
// The contract is pinned as it is (contract changes are out of scope for
// #146): before any window, each case dry-runs FrostMovement.commit on the
// real Bridge from every seat's operator at that seat (accepted), from each
// operator at every seat it does not hold and one past the end (refused
// "FROST movement operator"), and the upgraded build must be tbtc-v2
// 39412825. A host whose numbering only matches a changed contract fails
// there.
//
// Only a MovingFunds source is covered. #146's Terminated half (D30) needs
// #141 first: at this base the leader never reaches the commitment for a
// Terminated source (batch 15, keep-core#145).
//
// World: batch 15's world (keep-core#145, pkg/tbtc/frost_movement_terminated_b15_test.go,
// not on this base, so its setup is repeated here under b16 names) on the
// frozen legacy world with the real upgraded Bridge (LEGACY_UPGRADED_DIR) and
// Bitcoin Core regtest, configured by setUpFrostDrain, whose FROST wallet is
// the movement's only eligible target. The source S is a second FROST wallet:
// the legacy MovingFunds wallet drains into it (batch 3's drain), S sweeps
// that incoming output and the sweep is proven, so S is Live with a main;
// governance shortens the wallet max age and a stranger notifies S closeable,
// so S is MovingFunds with nothing committed. Unlike batch 15, nothing is
// committed in setup: the leader's own commitment is the subject.
//
// S's descriptor is members [1,2,1,3], operators [A,B,A,C] (threshold 3): A
// holds seats 0 and 2 under one member ID, B seat 1, C the last seat 3 alone.
// Batch 3's descriptor (one operator under two member IDs) is refused by
// dkg.ValidateDescriptor ("inconsistent FROST operator mapping"); this one
// passes it, as batch 15's [1,2,1] does. The leader's commitment path itself
// (frostExecutingSeat, movement.Commitments) does not validate the descriptor.
//
// FIXTURES: the FROST registry fixture behind both wallets (no FROST DKG);
// the legacy source's test key (the drain); S's test key (scalar 1003), which
// signs in place of S's FROST signers (the incoming sweep); operator keys B
// and C and the stranger's key (fixed test scalars, funded on Anvil);
// operator A is the world's operator key. ADAPTER: the Core-backed script
// query b16Bitcoin.
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

// b16Prefix names the invariant's issue in every assertion.
const b16Prefix = "tuxxy/keep-core#146: "

// b16PinnedContract is the tbtc-v2 commit whose FrostMovement.commit seat
// numbering (0-based descriptor index) these tests pin.
const b16PinnedContract = "39412825454ae9d7d56811ce48cd826c92b821c2"

// b16SweepFee is the fee of the source's incoming sweep (setup).
const b16SweepFee = int64(1_000)

// b16Bound bounds one leader routine or one wait.
const b16Bound = 30 * time.Second

// b16SourceKey is the FIXTURE source key's scalar; it signs in place of the
// source's FROST signers.
const b16SourceKey = 1003

// FIXTURE operator and stranger key scalars (test values only).
const (
	b16OperatorBKey = 0xb16b
	b16OperatorCKey = 0xb16c
	b16StrangerKey  = 0xb16d
)

// b16Operator is one of the source's operators: its key and the descriptor
// indexes (seats, 0-based as FrostMovement.commit reads them) it holds.
type b16Operator struct {
	name  string
	key   *bind.TransactOpts
	pk    []byte // FIXTURE private key bytes, for the operator's own client
	addr  common.Address
	seats []uint16
}

type b16World struct {
	*legacyWorld
	frost *bind.BoundContract
	// script is the ADAPTER Bitcoin view the leader uses.
	script *b16Bitcoin
	// src is the FROST source; dest is setUpFrostDrain's FROST wallet, the
	// movement's only eligible target.
	src, dest *frostTarget
	// descriptor is the source descriptor's operator list in seat order;
	// operators are its distinct operators A, B, C.
	descriptor []common.Address
	operators  []*b16Operator
	stranger   *b16Operator
	drain      *bitcoin.Transaction
	sweep      *bitcoin.Transaction
	reader     *chainethereum.TbtcChain
}

// b16Bitcoin is an ADAPTER: the world's Core-backed Bitcoin chain plus the
// Electrum script query (redemption.ScriptUnspent) the FROST leader uses,
// answered from the blocks Core mined for this world.
type b16Bitcoin struct {
	*legacyRegtest
}

func (b *b16Bitcoin) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
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

// newB16World builds the upgraded system with setUpFrostDrain's
// configuration and FROST wallet (the movement target), registers the
// source, drains the legacy source into it, sweeps that incoming output (the
// source becomes Live) and ages it out (MovingFunds, nothing committed).
func newB16World(t *testing.T) *b16World {
	t.Helper()
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	w := &b16World{src: &frostTarget{}, dest: &frostTarget{}}
	w.legacyWorld = newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), func(lw *legacyWorld) {
		setUpFrostDrain(w.dest)(lw)
		w.legacyWorld = lw
		w.makeOperators(t)
		w.registerSource(t)
	})
	w.frost = bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
	w.script = &b16Bitcoin{w.btc}
	w.drainIntoSource(t)
	w.sweepIncoming(t)
	w.ageOut(t)

	s := w.readSource(t)
	if s.Wallet.State != movement.StateMovingFunds {
		t.Fatalf("setup: source state %d, want MovingFunds", s.Wallet.State)
	}
	if s.MainValue == 0 || s.Custody.MainOutpoint != deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: w.sweep.Hash()}) {
		t.Fatalf("setup: the source holds no main (value %d)", s.MainValue)
	}
	if s.Log != nil || s.Commitment.ProvenAt != 0 {
		t.Fatalf("setup: the source already has a commitment (recorded %v)", s.Log != nil)
	}
	// The host's request is built from the on-chain descriptor (as
	// frostNode.newFrostWallet builds the coordination executor's operators).
	if !slices.Equal(s.Wallet.Descriptor.Operators, w.descriptor) {
		t.Fatalf("setup: the Bridge's source descriptor operators %v, want %v", s.Wallet.Descriptor.Operators, w.descriptor)
	}
	return w
}

// b16Key is a FIXTURE private key from a test scalar.
func b16Key(scalar int64) []byte {
	return common.LeftPadBytes(big.NewInt(scalar).Bytes(), 32)
}

// makeOperators: A is the world's operator key; B, C and the stranger are
// FIXTURE keys, funded on Anvil. The descriptor is [A, B, A, C].
func (w *b16World) makeOperators(t *testing.T) {
	t.Helper()
	a, err := hexutil.Decode("0x" + legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, raw []byte) *b16Operator {
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
		return &b16Operator{name: name, key: opts, pk: raw, addr: address}
	}
	opA, opB, opC := mk("A", a), mk("B", b16Key(b16OperatorBKey)), mk("C", b16Key(b16OperatorCKey))
	w.stranger = mk("stranger", b16Key(b16StrangerKey))
	if opA.addr != w.operator {
		t.Fatal("setup: operator A is not the world's operator")
	}
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	for _, o := range []*b16Operator{opA, opB, opC, w.stranger} {
		if o != opA && (o.addr == admin || o.addr == w.operator) {
			t.Fatalf("setup: FIXTURE key %s is the deployer or the world's operator", o.name)
		}
	}
	w.descriptor = []common.Address{opA.addr, opB.addr, opA.addr, opC.addr}
	w.operators = []*b16Operator{opA, opB, opC}
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
func (w *b16World) registerSource(t *testing.T) {
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
	key, err := crypto.ToECDSA(b16Key(b16SourceKey))
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
		3, crypto.Keccak256Hash([]byte("batch-16 FROST source")), w.src.q}
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
func (w *b16World) drainIntoSource(t *testing.T) {
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
func (w *b16World) sweepIncoming(t *testing.T) {
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
		Outputs: []*bitcoin.TransactionOutput{{Value: value - b16SweepFee, PublicKeyScript: script}},
	}
	snapshot, err := bitcoin.NewTaprootTransaction(unsigned, []bitcoin.PreviousOutput{{Outpoint: input, Value: value, PublicKeyScript: script}}, w.src.q)
	if err != nil {
		t.Fatalf("setup: sweep snapshot: %v", err)
	}
	private, _ := btcec.PrivKeyFromBytes(b16Key(b16SourceKey))
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
func (w *b16World) ageOut(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), b16Bound)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: notifyFrostWalletCloseable not mined: %v", err)
	}
}

// readSource is one production read of the source (movement.View.Source).
func (w *b16World) readSource(t *testing.T) movement.Source {
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
func (w *b16World) clientAs(t *testing.T, o *b16Operator, encoding legacyRevertEncoding, rec **legacyRecorder) *chainethereum.TbtcChain {
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
func (w *b16World) commitRefusal(o *b16Operator, seat uint16) string {
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
func (w *b16World) pinContract(t *testing.T) {
	t.Helper()
	n := uint16(len(w.descriptor))
	for _, o := range w.operators {
		for seat := uint16(0); seat <= n; seat++ {
			holds := slices.Contains(o.seats, seat)
			reason := w.commitRefusal(o, seat)
			if holds && reason != "" {
				t.Fatalf("%scontract pin: the Bridge refuses operator %s (%s) committing at its seat %d (0-based descriptor index, FrostMovement.commit at %s): %s",
					b16Prefix, o.name, o.addr, seat, b16PinnedContract[:8], reason)
			}
			if !holds && !strings.Contains(reason, "FROST movement operator") {
				t.Fatalf("%scontract pin: the Bridge does not refuse operator %s (%s) committing at seat %d it does not hold with %q (got %q)",
					b16Prefix, o.name, o.addr, seat, "FROST movement operator", reason)
			}
		}
	}
	// The environment's commit only (upgradedBuild returns
	// LEGACY_UPGRADED_COMMIT); the dry-runs above are the real guard.
	if _, commit := upgradedBuild(t); commit != b16PinnedContract {
		t.Fatalf("%scontract pin: the upgraded Bridge is built from %s, not tbtc-v2 %s", b16Prefix, commit, b16PinnedContract)
	}
}

// request is the window's coordination request for the source, with the
// wallet operators in descriptor order as the node builds them.
func (w *b16World) request(leader common.Address) *tbtc.CoordinationProposalRequest {
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
func (w *b16World) window(t *testing.T, o *b16Operator, encoding legacyRevertEncoding) (tbtc.CoordinationProposal, *legacyRecorder, error) {
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
	case <-time.After(b16Bound):
		t.Fatalf("leader %s (%v): proposal generation did not finish within %v", o.name, encoding, b16Bound)
		return nil, nil, nil
	}
}

// commitmentAttempts counts the recorder's eth_estimateGas and
// eth_sendRawTransaction requests carrying submitFrostMovingFundsCommitment.
func (w *b16World) commitmentAttempts(rec *legacyRecorder) int {
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
func (w *b16World) recordedCommitment(t *testing.T, s movement.Source) (common.Address, uint16) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), b16Bound)
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
// background miner must be stopped).
func (w *b16World) snapshot(t *testing.T) string {
	t.Helper()
	var id string
	if err := w.admin.Client().Call(&id, "evm_snapshot"); err != nil {
		t.Fatalf("setup: evm_snapshot: %v", err)
	}
	return id
}

// revertTo restores the snapshot (Anvil consumes its id), takes a new one for
// the next case, and checks the source again has nothing committed.
func (w *b16World) revertTo(t *testing.T, id string) string {
	t.Helper()
	var reverted bool
	if err := w.admin.Client().Call(&reverted, "evm_revert", id); err != nil || !reverted {
		t.Fatalf("setup: evm_revert %s: %v", id, err)
	}
	next := w.snapshot(t)
	if s := w.readSource(t); s.Wallet.State != movement.StateMovingFunds || s.Log != nil {
		t.Fatalf("setup: after the revert the source is in state %d with a commitment recorded %v", s.Wallet.State, s.Log != nil)
	}
	return next
}

// TestFrostMovementLeaderOwnCommitment (tuxxy/keep-core#146): for a
// MovingFunds source with nothing committed, the window's leader, for every
// operator of the wallet (A: seats 0 and 2; B: seat 1; C: the last seat 3
// alone), on either empty-revert encoding, submits the commitment itself and
// the real Bridge accepts it: the commitment is recorded to the only eligible
// target, sent by the leader, at a seat the descriptor maps to the leader,
// and the window proposes the movement. Before that, an executing operator
// with no seat proposes nothing and makes no commitment attempt, and the
// contract's numbering is pinned (pinContract).
//
// One world per encoding: after setup the Anvil state is snapshotted, and
// each leader's case starts from evm_revert to it (as k09World.seedWindow
// does), so every leader faces the same source with nothing committed. The
// leader path writes nothing to Core (b16Bitcoin only reads mined blocks),
// and every window starts fresh clients, so the revert restores the case's
// whole starting state.
func TestFrostMovementLeaderOwnCommitment(t *testing.T) {
	for _, encoding := range legacyEncodings {
		t.Run(encoding.String(), func(t *testing.T) {
			t.Parallel()
			world := newB16World(t)
			snapshot := world.snapshot(t)
			for _, leader := range []string{"A", "B", "C"} {
				t.Run(leader, func(t *testing.T) {
					w := world
					snapshot = w.revertTo(t, snapshot)
					stop := w.mineInBackground()
					defer stop()
					w.pinContract(t)

					var o *b16Operator
					for _, candidate := range w.operators {
						if candidate.name == leader {
							o = candidate
						}
					}
					what := fmt.Sprintf("leader %s %s (seats %v of %d, %v)", o.name, o.addr, o.seats, len(w.descriptor), encoding)

					// Negative: an executing operator that holds no seat
					// commits nothing and gets no proposal.
					proposal, rec, err := w.window(t, w.stranger, encoding)
					if _, ok := proposal.(*tbtc.MovingFundsProposal); ok {
						t.Fatalf("%san executing operator with no seat proposed a movement (error %v)", b16Prefix, err)
					}
					if n := w.commitmentAttempts(rec); n != 0 {
						t.Fatalf("%san executing operator with no seat attempted %d movement commitments", b16Prefix, n)
					}
					if s := w.readSource(t); s.Log != nil {
						t.Fatalf("%sa commitment was recorded for an executing operator with no seat", b16Prefix)
					}

					// The leader's own commitment.
					proposal, rec, err = w.window(t, o, encoding)
					p, ok := proposal.(*tbtc.MovingFundsProposal)
					if err != nil || !ok {
						t.Fatalf("%s%s: the leader's own movement commitment was not accepted by the Bridge (proposal %T, error %v)", b16Prefix, what, proposal, err)
					}
					if w.commitmentAttempts(rec) == 0 {
						t.Fatalf("%s%s: the leader proposed without submitting the commitment itself", b16Prefix, what)
					}
					s := w.readSource(t)
					if s.Log == nil || len(s.Commitment.Targets) != 1 || s.Commitment.Targets[0] != w.dest.label {
						t.Fatalf("%s%s: the Bridge holds no commitment of the source to the only eligible target (recorded %v, targets %x)", b16Prefix, what, s.Log != nil, s.Commitment.Targets)
					}
					sender, seat := w.recordedCommitment(t, s)
					if sender != o.addr {
						t.Fatalf("%s%s: the recorded commitment was sent by %s, not the leader", b16Prefix, what, sender)
					}
					if !slices.Contains(o.seats, seat) || w.descriptor[seat] != o.addr {
						t.Fatalf("%s%s: the commitment names seat %d, which the descriptor maps to another operator", b16Prefix, what, seat)
					}
					if len(p.TargetWallets) != 1 || p.TargetWallets[0] != w.dest.label {
						t.Fatalf("%s%s: the proposal moves to %x, not to the committed target %x", b16Prefix, what, p.TargetWallets, w.dest.label)
					}
					if p.MovingFundsTxFee == nil || p.MovingFundsTxFee.Sign() <= 0 || p.MovingFundsTxFee.Uint64() > s.Commitment.MaxFee {
						t.Fatalf("%s%s: proposal fee %v is not within (0, %d], the committed cap", b16Prefix, what, p.MovingFundsTxFee, s.Commitment.MaxFee)
					}
				})
			}
		})
	}
}
