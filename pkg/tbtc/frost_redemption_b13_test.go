package tbtc_test

// Remediation batch 13 acceptance tests: the FROST redemption leader selects
// exactly the requests its followers accept.
// Refs tuxxy/keep-core#142 (FSM analysis 2026-11-09, R-1, P2).
//
// Invariant (R-1): in every coordination window, the requests the leader's
// production proposal generator (pkg/tbtcpg, ActionRedemption for a FROST
// wallet) names are exactly the first requests, oldest first, that every
// follower's own handling of the proposal (tbtc frostWallet.redeem: plan
// from chain state at the coordination block, then check it pays exactly the
// proposed requests) accepts. So a request the followers refuse, because it
// is still delayed by the watchtower (now < NotBefore) or too close to or
// past its deadline (Deadline - now < the members' margin, or Deadline <=
// now) while nobody has observed its timeout yet, is never proposed, and it
// does not hold back the wallet's other requests. Once such a request
// becomes acceptable (NotBefore reached), it is proposed.
//
// Why it matters: the follower refuses any proposal whose requests differ
// from its own plan. A leader that names a request the followers leave out
// gets every window refused, so all of the wallet's redemptions stall for
// the length of the delay, or until someone observes the timeout.
//
// World: batch 3's world, rebuilt here (b13SetUpFrost, drainToTarget) on
// the real upgraded Bridge (tbtc-v2 integration/frost-base,
// LEGACY_UPGRADED_DIR) with Bitcoin Core regtest. Its one change: the FROST
// target's descriptor has three distinct operators, so members can plan for
// it (batch 3's maps two seats to one operator, which keep-core's descriptor
// validation refuses). The legacy source drains into the target through the
// real movement commitment and proof.
// The target's incoming output is swept and proven on the real Bridge, which
// makes the target Live with a recognized main; FROST redemptions are then
// configured and enabled by governance, and the requests are real
// requestFrostRedemption calls escrowing the depositor's real Bank balance.
// The chain clock moves with Anvil evm_setNextBlockTimestamp only.
// FIXTURES (labelled where used): the FROST registry fixture behind the
// target (batch 3, no FROST DKG); the source's test key, which signs the
// drain (batch 3); the target's test key (scalar 1000, as
// setUpFrostDrain derives it), which signs the incoming sweep in place of the
// FROST signers; the tbtc-v2 test contract FrostCapturedWatchtowerFixture as
// the FROST redemption watchtower (it returns the delay the test sets); the
// member's closed FROST store in tbtc.B13FollowerRedeem; and
// b6LiveBitcoin (batch 6) as the member's Bitcoin view with its script query.
// RPC: every leader window and follower check goes through batch 1's
// recorder adapter; leaders alternate the two empty-revert encodings (code 3
// / "0x" and -32000 / no data) and every follower check runs on both.
// Unreachable through public calls (asserted or recorded, not tested as a
// leader property): a request paying the wallet's own change script (the
// Bridge refuses it, asserted in the guard case) and a request with another
// descriptor hash (the Bridge captures the wallet's own descriptor hash at
// request time, asserted for every request).
// Entry points: the leader is tbtcpg.NewProposalGenerator(...).Generate with
// the CoordinationProposalRequest base's coordination executor builds for a
// FROST wallet (FrostWallet set); the follower is tbtc.B13FollowerRedeem.
// No build tag (tuxxy/keep-core#64).

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// b13Prefix names the invariant in every assertion.
const b13Prefix = "R-1 (tuxxy/keep-core#142): "

// b13Delay is the watchtower delay of a delayed request, in seconds.
const b13Delay = uint64(3600)

// b13Gross is each request's amount in satoshis.
const b13Gross = uint64(1_500_000)

// b13SweepFee is the fee of the target's incoming sweep.
const b13SweepFee = int64(1_000)

// b13Bound bounds every client step (a case runs in under 10 s).
const b13Bound = 8 * time.Second

// C-04 FrostRedemptionLedger.State.
const (
	b13Requested      = uint8(1)
	b13TimeoutPending = uint8(3)
)

// b13RequestRecord mirrors FrostRedemptionLedger.Request.
type b13RequestRecord struct {
	WalletId          [32]byte
	DescriptorHash    [32]byte
	ReservationId     [32]byte
	Redeemer          common.Address
	Gross             uint64
	TreasuryFee       uint64
	MaximumBitcoinFee uint64
	Deadline          uint64
	FinalityBlocks    uint64
	TimeoutBlock      uint64
	State             uint8
	Script            []byte
}

// b13PolicyRecord mirrors FrostRedemptionState.RequestPolicy.
type b13PolicyRecord struct {
	Watchtower common.Address
	PolicyId   [32]byte
	NotBefore  uint64
}

// b13Request is one request the test made, with its captured timing.
type b13Request struct {
	name      string
	id        [32]byte
	script    bitcoin.Script
	notBefore uint64
	deadline  uint64
}

type b13World struct {
	*legacyWorld
	target     *frostTarget
	operators  []chain.Address
	drain      *bitcoin.Transaction
	frost      *bind.BoundContract
	watchtower *bind.BoundContract
	policy     [32]byte
	descriptor dkg.Descriptor
	genesis    [32]byte
	encodings  int
	// followers are the members' production clients, one per encoding,
	// each behind its own recorder adapter; a member reads at the
	// coordination block it is given. Every leader is a fresh client, so
	// its head is the window's coordination block.
	followers map[legacyRevertEncoding]*chainethereum.TbtcChain
	names     map[string]string
	next      byte
}

func b13Bounded(t *testing.T, what string, fn func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- fn() }()
	select {
	case err := <-result:
		return err
	case <-time.After(b13Bound):
		t.Fatalf("%s%s did not finish within %v", b13Prefix, what, b13Bound)
		return nil
	}
}

// b13Send submits one admin transaction, requires it to succeed and returns
// its receipt.
func (w *b13World) b13Send(t *testing.T, what string, fn func(*bind.TransactOpts) (*types.Transaction, error)) *types.Receipt {
	t.Helper()
	tx, err := fn(w.opts())
	if err != nil {
		t.Fatalf("setup: %s: %v", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	receipt, err := legacyWaitMined(ctx, w.admin, tx)
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: %s not mined: %v", what, err)
	}
	return receipt
}

// newB13World builds batch 3's world with a FROST target whose descriptor
// members can plan with (b13SetUpFrost), drains the legacy source into it,
// makes the target Live by sweeping and proving its incoming output, and
// configures and enables FROST redemptions with the watchtower fixture.
func newB13World(t *testing.T) *b13World {
	t.Helper()
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	target := &frostTarget{}
	w := &b13World{legacyWorld: newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), b13SetUpFrost(target)), target: target, names: map[string]string{},
		followers: map[legacyRevertEncoding]*chainethereum.TbtcChain{}}
	w.frost = bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
	ctx := context.Background()
	w.drainToTarget(t)

	sweep := w.sweepIncoming(t)

	// FIXTURE watchtower: tbtc-v2's FrostCapturedWatchtowerFixture returns
	// the delay the test sets for the next request.
	w.policy = crypto.Keccak256Hash([]byte("batch-13 watchtower policy"))
	address, parsed := w.deployBaseline(map[string]common.Address{}, "contracts/test/FrostVaultCallbackFixture.sol", "FrostCapturedWatchtowerFixture", w.policy, uint64(0))
	w.watchtower = bind.NewBoundContract(address, parsed, w.admin, w.admin, w.admin)

	// The FROST funding genesis setUpFrostDrain configured; redemptions
	// must use the same network.
	genesis := crypto.Keccak256Hash([]byte("legacy-unchanged synthetic regtest domain"))
	w.b13Send(t, "configure FROST redemptions", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "configureFrostRedemptions", genesis, uint64(1), uint64(100), address,
			crypto.Keccak256Hash([]byte("batch-13 local test; no legacy transition")))
	})
	w.b13Send(t, "enable FROST redemptions", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "setFrostRedemptionsEnabled", true)
	})

	// The depositor (admin) holds the Bank balance of the legacy deposit.
	bankABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"approveBalance","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"}]`))
	if err != nil {
		t.Fatal(err)
	}
	bank := bind.NewBoundContract(w.bank, bankABI, w.admin, w.admin, w.admin)
	w.b13Send(t, "approve Bank balance", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return bank.Transact(o, "approveBalance", w.bridge, new(big.Int).SetUint64(8*b13Gross))
	})

	// The members' wallet descriptor and network, as the Bridge records
	// them for the target.
	c := w.clientWith(t, revertCode3)
	bridge, err := c.FrostBridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head := w.head(t)
	wallet, err := bridge.Wallet(ctx, head, w.target.label)
	if err != nil {
		t.Fatal(err)
	}
	w.descriptor = wallet.Descriptor
	custody, err := bridge.Custody(ctx, head, w.target.id)
	if err != nil || custody.MainOutpoint != deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: sweep, OutputIndex: 0}) || custody.ActiveReservation != ([32]byte{}) {
		t.Fatalf("precondition: the target's custody main is not the proven sweep output: %+v %v", custody, err)
	}
	hash, err := dkg.DescriptorHash(w.descriptor)
	if err != nil || hash != wallet.DescriptorHash || w.descriptor.OutputKey != w.target.q {
		t.Fatalf("precondition: the Bridge's descriptor of the target does not hash to its descriptor hash (%v)", err)
	}
	if wallet.State != 4 { // FrostWallets.State.Live
		t.Fatalf("precondition: the target is not Live after its incoming sweep (state %d)", wallet.State)
	}
	configuration, err := bridge.Configuration(ctx, head)
	if err != nil || !configuration.Enabled || configuration.BitcoinGenesis != genesis {
		t.Fatalf("precondition: FROST redemptions are not enabled on the funding network: %+v %v", configuration, err)
	}
	w.genesis = configuration.BitcoinGenesis

	maxSize, err := c.GetRedemptionMaxSize()
	if err != nil || maxSize < 3 {
		t.Fatalf("precondition: redemption max size %d < 3 (%v)", maxSize, err)
	}
	params, err := w.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if b13Gross < params.RedemptionDustThreshold || uint64(params.RedemptionTimeout) <= b13Delay+2000 {
		t.Fatalf("precondition: redemption parameters do not fit the cases: %+v", params)
	}
	return w
}

// b13Operators are the target's three seats' operators: Anvil's public
// development accounts 1 (the legacy operator), 0 (the deployer) and 2, one
// seat each, so the descriptor passes keep-core's descriptor validation
// (batch 3's target maps two members to one operator, which members refuse
// to plan for).
func (w *b13World) b13Operators() []common.Address {
	return []common.Address{w.operator, crypto.PubkeyToAddress(w.adminKey.PublicKey), common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC")}
}

// b13SetUpFrost is setUpFrostDrain (legacy_upgraded_test.go) with one
// change: the target's descriptor names three distinct operators
// (b13Operators). It configures the upgraded Bridge's FROST extension as
// tbtc-v2's movementWorld does (C-10 test ceilings, D-10 values OPEN),
// registers the target through the FIXTURE registry and enables the drain.
func b13SetUpFrost(target *frostTarget) func(*legacyWorld) {
	return func(lw *legacyWorld) {
		w := &b13World{legacyWorld: lw}
		t := w.t
		t.Helper()
		admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
		frost := bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
		call := func(what string, contract *bind.BoundContract, method string, args ...interface{}) {
			w.send(what, func(o *bind.TransactOpts) (*types.Transaction, error) {
				return contract.Transact(o, method, args...)
			})
		}
		libraries := map[string]common.Address{}
		registryAddress, registryABI := w.deployBaseline(libraries, "contracts/test/FrostRegistryFixture.sol", "FrostRegistryFixture", w.bridge)
		registry := bind.NewBoundContract(registryAddress, registryABI, w.admin, w.admin, w.admin)
		call("FROST registry", frost, "configureFrostRegistry", registryAddress)
		verifier, _ := w.deployBaseline(libraries, "contracts/frost/FrostVerifier.sol", "FrostVerifier")
		code, err := w.admin.CodeAt(context.Background(), verifier, nil)
		if err != nil {
			t.Fatal(err)
		}
		call("FROST funding", frost, "configureFrostFunding", verifier, crypto.Keccak256Hash(code),
			crypto.Keccak256Hash([]byte("UNQUALIFIED legacy-unchanged local test")),
			crypto.Keccak256Hash([]byte("legacy-unchanged synthetic regtest domain")))
		exposureAddress, exposureABI := w.deployBaseline(libraries, "contracts/frost/FrostExposureController.sol", "FrostExposureController", w.bridge, admin)
		exposure := bind.NewBoundContract(exposureAddress, exposureABI, w.admin, w.admin, w.admin)
		maxUint64 := new(big.Int).SetUint64(^uint64(0))
		call("C-10 budget", exposure, "openBudgetPeriod", maxUint64.Uint64(), maxUint64.Uint64(),
			crypto.Keccak256Hash([]byte("C-10 test budget; D-10 OPEN")))
		call("C-10 controller", frost, "installFrostExposureController", exposureAddress)
		call("FROST gates", frost, "setFrostFundingGates", true, true)

		key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(1000).Bytes(), 32))
		if err != nil {
			t.Fatal(err)
		}
		copy(target.q[:], crypto.CompressPubkey(&key.PublicKey)[1:])
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
		}{2, 1, w.chainID, registryAddress, head, []uint32{1, 2, 3}, w.b13Operators(),
			2, crypto.Keccak256Hash([]byte("batch-13 FROST target")), target.q}
		encoded, err := abi.Arguments{{Type: descriptorType}}.Pack(descriptor)
		if err != nil {
			t.Fatal(err)
		}
		bytes32Type, _ := abi.NewType("bytes32", "", nil)
		tagged, err := abi.Arguments{{Type: bytes32Type}, {Type: descriptorType}}.Pack(
			crypto.Keccak256Hash([]byte("tbtc-v2/frost-descriptor/v1")), descriptor)
		if err != nil {
			t.Fatal(err)
		}
		target.id = crypto.Keccak256Hash(crypto.Keccak256([]byte("tbtc-v2/frost-wallet-id/v1")), target.q[:])
		copy(target.label[:], target.id[:20])
		call("FROST wallet created", registry, "created", encoded, head+100000)
		call("FROST wallet ready", registry, "ready", target.id, crypto.Keccak256Hash(tagged))
		call("FROST drain enabled", frost, "setFrostDrainEnabled", true)
	}
}

// drainToTarget drains the legacy MovingFunds source into the target and
// proves the drain, as newFrostIncomingWorld does: the target then holds one
// pending FROST incoming output. FIXTURE signer: the source's test key.
func (w *b13World) drainToTarget(t *testing.T) {
	t.Helper()
	for _, operator := range w.b13Operators() {
		w.operators = append(w.operators, chain.Address(operator.Hex()))
	}
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(operatorKey, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.legacy.SubmitMovingFundsCommitment(opts, w.source.pkh, legacyUTXO(w.main), []uint32{1}, big.NewInt(1), [][20]byte{w.target.label})
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
	script, err := bitcoin.PayToTaproot(w.target.q)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: w.main.Value - frostDrainFee, PublicKeyScript: script})
	w.drain = legacySign(t, builder, w.source.key)
	w.btc.mine(t, w.drain)
	w.send("drain proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsProof(o, legacyTxInfo(w.drain), w.btc.proof(t, w.drain.Hash()), legacyUTXO(w.main), w.source.pkh)
	})
}

// clientWith is a production client started fresh, behind an RPC adapter
// with the given empty-revert encoding.
func (w *b13World) clientWith(t *testing.T, encoding legacyRevertEncoding) *chainethereum.TbtcChain {
	t.Helper()
	c, err := w.connect(newLegacyRecorder(t, w.anvilURL, encoding))
	if err != nil {
		t.Fatalf("client startup (%v): %v", encoding, err)
	}
	return c
}

// client is the role's production client for the encoding, started on
// first use.
func (w *b13World) client(t *testing.T, role map[legacyRevertEncoding]*chainethereum.TbtcChain, encoding legacyRevertEncoding) *chainethereum.TbtcChain {
	t.Helper()
	if c, ok := role[encoding]; ok {
		return c
	}
	c := w.clientWith(t, encoding)
	role[encoding] = c
	return c
}

func (w *b13World) head(t *testing.T) uint64 {
	t.Helper()
	head, err := w.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return head
}

func (w *b13World) now(t *testing.T) uint64 {
	t.Helper()
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return header.Time
}

// at mines the next block with the given timestamp: the window's
// coordination block. Only the chain clock moves.
func (w *b13World) at(t *testing.T, timestamp uint64) uint64 {
	t.Helper()
	if now := w.now(t); timestamp <= now {
		t.Fatalf("setup: cannot move the chain clock back from %d to %d", now, timestamp)
	}
	if err := w.admin.Client().Call(nil, "evm_setNextBlockTimestamp", timestamp); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.Client().Call(nil, "evm_mine"); err != nil {
		t.Fatal(err)
	}
	if now := w.now(t); now != timestamp {
		t.Fatalf("setup: chain clock %d, want %d", now, timestamp)
	}
	return w.head(t)
}

// sweepIncoming carries the target's pending incoming output to Bitcoin and
// the Bridge: its sweep, signed by the FIXTURE target key, mined by Core and
// proven on the real Bridge, which makes the target Live with that main.
func (w *b13World) sweepIncoming(t *testing.T) bitcoin.Hash {
	t.Helper()
	input := bitcoin.TransactionOutpoint{TransactionHash: w.drain.Hash(), OutputIndex: 0}
	value := w.main.Value - frostDrainFee
	script, err := bitcoin.PayToTaproot(w.target.q)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := &bitcoin.Transaction{
		Version: 2,
		Inputs:  []*bitcoin.TransactionInput{{Outpoint: &input, Sequence: 0xffffffff}},
		Outputs: []*bitcoin.TransactionOutput{{Value: value - b13SweepFee, PublicKeyScript: script}},
	}
	snapshot, err := bitcoin.NewTaprootTransaction(unsigned, []bitcoin.PreviousOutput{{Outpoint: input, Value: value, PublicKeyScript: script}}, w.target.q)
	if err != nil {
		t.Fatalf("setup: sweep snapshot: %v", err)
	}
	private, _ := btcec.PrivKeyFromBytes(common.LeftPadBytes(big.NewInt(1000).Bytes(), 32))
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
	sweep, err := snapshot.AddSignatures(signatures)
	if err != nil {
		t.Fatalf("setup: sweep signatures: %v", err)
	}
	w.btc.mine(t, sweep)
	w.b13Send(t, "incoming sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostMovedFundsSweepProof", w.target.id, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()))
	})
	return sweep.Hash()
}

// request makes a FROST redemption request paying a fresh P2WPKH script,
// with the given watchtower delay, and returns it with its captured timing.
func (w *b13World) request(t *testing.T, name string, delay uint64) b13Request {
	t.Helper()
	w.b13Send(t, "watchtower delay", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.watchtower.Transact(o, "changeRequestPolicy", w.policy, delay)
	})
	w.next++
	script, err := bitcoin.PayToWitnessPublicKeyHash([20]byte{0xb1, 0x3, w.next})
	if err != nil {
		t.Fatal(err)
	}
	receipt := w.b13Send(t, "request "+name, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "requestFrostRedemption", w.target.id, []byte(script), b13Gross, crypto.PubkeyToAddress(w.adminKey.PublicKey))
	})
	topic := crypto.Keccak256Hash([]byte("FrostRedemptionRequested(bytes32,bytes32,address,uint64,uint64,uint64,uint64)"))
	var id [32]byte
	for _, l := range receipt.Logs {
		if len(l.Topics) == 3 && l.Topics[0] == topic && l.Topics[2] == w.target.id {
			id = l.Topics[1]
		}
	}
	if id == ([32]byte{}) {
		t.Fatalf("setup: request %s emitted no FrostRedemptionRequested", name)
	}
	record, policy := w.read(t, id)
	block, err := w.admin.HeaderByNumber(context.Background(), receipt.BlockNumber)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != b13Requested || policy.NotBefore != block.Time+delay || !bytes.Equal(record.Script, script) {
		t.Fatalf("precondition: request %s not recorded as Requested with NotBefore = request time + %d: %+v %+v", name, delay, record, policy)
	}
	descriptorHash, _ := dkg.DescriptorHash(w.descriptor)
	if record.DescriptorHash != descriptorHash {
		t.Fatalf("precondition: request %s did not capture the wallet's descriptor hash", name)
	}
	w.names[string(script)] = name
	return b13Request{name: name, id: id, script: script, notBefore: policy.NotBefore, deadline: record.Deadline}
}

func (w *b13World) read(t *testing.T, id [32]byte) (b13RequestRecord, b13PolicyRecord) {
	t.Helper()
	var out []interface{}
	if err := w.frost.Call(&bind.CallOpts{}, &out, "frostRedemptionRequest", id); err != nil || len(out) != 1 {
		t.Fatalf("frostRedemptionRequest: %v", err)
	}
	record := *abi.ConvertType(out[0], new(b13RequestRecord)).(*b13RequestRecord)
	out = nil
	if err := w.frost.Call(&bind.CallOpts{}, &out, "frostRedemptionPolicy", id); err != nil || len(out) != 1 {
		t.Fatalf("frostRedemptionPolicy: %v", err)
	}
	return record, *abi.ConvertType(out[0], new(b13PolicyRecord)).(*b13PolicyRecord)
}

func (w *b13World) named(scripts []bitcoin.Script) string {
	names := make([]string, len(scripts))
	for i, s := range scripts {
		if name, ok := w.names[string(s)]; ok {
			names[i] = name
		} else {
			names[i] = fmt.Sprintf("%x", []byte(s))
		}
	}
	return "[" + strings.Join(names, " ") + "]"
}

func b13Scripts(requests ...b13Request) []bitcoin.Script {
	out := make([]bitcoin.Script, len(requests))
	for i, r := range requests {
		out[i] = r.script
	}
	return out
}

// window runs one coordination window at the current head: the leader's
// production proposal generator (encodings alternate between windows) must
// name exactly want, in order; with want empty it must propose no
// redemption. Every follower, on both encodings, must accept the proposal.
func (w *b13World) window(t *testing.T, what string, want ...b13Request) {
	t.Helper()
	encoding := []legacyRevertEncoding{revertCode3, revertGeth}[w.encodings%2]
	w.encodings++
	coordinationBlock := w.head(t)
	c := w.clientWith(t, encoding)
	var proposal tbtc.CoordinationProposal
	err := b13Bounded(t, what+": proposal generation", func() error {
		var err error
		proposal, err = tbtcpg.NewProposalGenerator(c, w.btc).Generate(&tbtc.CoordinationProposalRequest{
			WalletPublicKeyHash: w.target.label,
			WalletOperators:     w.operators,
			ExecutingOperator:   w.operators[w.encodings%len(w.operators)],
			ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionRedemption},
			FrostWallet:         true,
		})
		return err
	})
	if err != nil {
		t.Fatalf("%s%s: leader (%v): proposal generation failed: %v", b13Prefix, what, encoding, err)
	}
	p, ok := proposal.(*tbtc.RedemptionProposal)
	wantScripts := b13Scripts(want...)
	if len(want) == 0 {
		if ok {
			t.Fatalf("%s%s: leader (%v) proposed a redemption of %s, but no request is one a follower accepts", b13Prefix, what, encoding, w.named(p.RedeemersOutputScripts))
		}
		return
	}
	if !ok {
		t.Fatalf("%s%s: leader (%v) proposed no redemption (got %T), want exactly the requests a follower accepts %s", b13Prefix, what, encoding, proposal, w.named(wantScripts))
	}
	same := len(p.RedeemersOutputScripts) == len(wantScripts)
	for i := 0; same && i < len(wantScripts); i++ {
		same = bytes.Equal(p.RedeemersOutputScripts[i], wantScripts[i])
	}
	if !same {
		t.Fatalf("%s%s: leader (%v) proposed %s, want exactly the requests a follower accepts, oldest first: %s", b13Prefix, what, encoding, w.named(p.RedeemersOutputScripts), w.named(wantScripts))
	}
	for _, followerEncoding := range legacyEncodings {
		if err := w.follow(t, what, followerEncoding, p, coordinationBlock); err != nil {
			t.Fatalf("%s%s: a follower (%v) refused the leader's proposal %s: %v", b13Prefix, what, followerEncoding, w.named(p.RedeemersOutputScripts), err)
		}
	}
}

// follow is one member's handling of the proposal at the coordination block
// (tbtc.B13FollowerRedeem); nil means it accepted it.
func (w *b13World) follow(t *testing.T, what string, encoding legacyRevertEncoding, p *tbtc.RedemptionProposal, coordinationBlock uint64) error {
	t.Helper()
	c := w.client(t, w.followers, encoding)
	ctx := context.Background()
	bridge, err := c.FrostBridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	return b13Bounded(t, what+": follower", func() error {
		return tbtc.B13FollowerRedeem(ctx, dir, bridge, &b6LiveBitcoin{inner: w.btc}, w.descriptor, w.genesis, p, coordinationBlock)
	})
}

// requireRefused is a precondition: a proposal naming the requests is
// refused by every follower, while the same proposal without the refused
// request is accepted. It shows that the base leader's choice stalls the
// window, whatever the leader does.
func (w *b13World) requireRefused(t *testing.T, what string, refused b13Request, all ...b13Request) {
	t.Helper()
	coordinationBlock := w.head(t)
	var rest []b13Request
	for _, r := range all {
		if r.id != refused.id {
			rest = append(rest, r)
		}
	}
	fee := big.NewInt(1_000)
	for _, encoding := range legacyEncodings {
		with := &tbtc.RedemptionProposal{RedeemersOutputScripts: b13Scripts(all...), RedemptionTxFee: fee}
		if err := w.follow(t, what, encoding, with, coordinationBlock); err == nil {
			t.Fatalf("%sprecondition, %s: a follower (%v) accepted a proposal naming %s", b13Prefix, what, encoding, refused.name)
		}
		if len(rest) == 0 {
			continue
		}
		without := &tbtc.RedemptionProposal{RedeemersOutputScripts: b13Scripts(rest...), RedemptionTxFee: fee}
		if err := w.follow(t, what, encoding, without, coordinationBlock); err != nil {
			t.Fatalf("%sprecondition, %s: a follower (%v) refused the proposal without %s: %v", b13Prefix, what, encoding, refused.name, err)
		}
	}
}

// TestFrostRedemptionLeaderMatchesFollower (tuxxy/keep-core#142): see the
// file comment. Each case is its own world, run in parallel.
func TestFrostRedemptionLeaderMatchesFollower(t *testing.T) {
	margin := tbtc.B13FrostRedemptionDeadlineMargin

	// Guard (base behaviour): ordinary requests are proposed, oldest first,
	// and followers accept them. A request paying the wallet's own change
	// script cannot exist (the Bridge refuses it).
	t.Run("ready", func(t *testing.T) {
		t.Parallel()
		w := newB13World(t)
		change, err := bitcoin.PayToTaproot(w.target.q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.frost.Transact(w.opts(), "requestFrostRedemption", w.target.id, []byte(change), b13Gross, crypto.PubkeyToAddress(w.adminKey.PublicKey)); err == nil || !strings.Contains(err.Error(), "FROST") {
			t.Fatalf("precondition: the Bridge did not refuse a request paying the wallet's change script: %v", err)
		}
		r1 := w.request(t, "R1", 0)
		r2 := w.request(t, "R2", 0)
		r3 := w.request(t, "R3", 0)
		w.window(t, "three ready requests", r1, r2, r3)
		w.window(t, "three ready requests, next window", r1, r2, r3)
	})

	// A watchtower-delayed request, the oldest, next to ready ones: it is
	// left out until NotBefore (both sides of the boundary), then proposed.
	t.Run("delayed", func(t *testing.T) {
		t.Parallel()
		w := newB13World(t)
		r1 := w.request(t, "R1(delayed)", b13Delay)
		r2 := w.request(t, "R2", 0)
		r3 := w.request(t, "R3", 0)
		w.at(t, w.now(t)+1)
		w.requireRefused(t, "R1 before NotBefore", r1, r1, r2, r3)
		w.window(t, "R1 delayed by the watchtower", r2, r3)
		w.at(t, r1.notBefore-1)
		w.window(t, "one second before R1's NotBefore", r2, r3)
		w.at(t, r1.notBefore)
		w.window(t, "at R1's NotBefore", r1, r2, r3)
	})

	// A delayed request alone: no redemption is proposed until NotBefore.
	t.Run("delayed-alone", func(t *testing.T) {
		t.Parallel()
		w := newB13World(t)
		r1 := w.request(t, "R1(delayed)", b13Delay)
		w.at(t, w.now(t)+1)
		w.requireRefused(t, "R1 alone before NotBefore", r1, r1)
		w.window(t, "only R1, delayed by the watchtower")
		w.at(t, r1.notBefore)
		w.window(t, "only R1, at its NotBefore", r1)
	})

	// An expiring request, the oldest, next to ready ones: proposed while
	// Deadline - now >= the members' margin, left out from one second
	// later, at its deadline while nobody observed the timeout, and after
	// the timeout is observed.
	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		w := newB13World(t)
		r1 := w.request(t, "R1(expiring)", 0)
		w.at(t, w.now(t)+2*margin)
		r2 := w.request(t, "R2", 0)
		r3 := w.request(t, "R3", 0)
		if r2.deadline < r1.deadline+2*margin {
			t.Fatal("setup: R2's deadline is not after R1's by twice the margin")
		}
		w.at(t, r1.deadline-margin)
		w.window(t, "R1 exactly the margin before its deadline", r1, r2, r3)
		w.at(t, r1.deadline-margin+1)
		w.requireRefused(t, "R1 inside the deadline margin", r1, r1, r2, r3)
		w.window(t, "R1 one second inside the deadline margin", r2, r3)
		w.at(t, r1.deadline)
		if record, _ := w.read(t, r1.id); record.State != b13Requested {
			t.Fatalf("precondition: R1 is not still Requested at its deadline (state %d)", record.State)
		}
		w.window(t, "R1 at its deadline, timeout not observed", r2, r3)
		w.b13Send(t, "observe R1's timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.frost.Transact(o, "notifyFrostRedemptionTimeout", r1.id)
		})
		if record, _ := w.read(t, r1.id); record.State != b13TimeoutPending {
			t.Fatalf("precondition: R1 is not TimeoutPending after its observation (state %d)", record.State)
		}
		w.window(t, "R1's timeout observed", r2, r3)
	})
}
