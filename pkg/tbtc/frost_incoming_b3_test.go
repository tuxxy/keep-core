package tbtc_test

// Remediation batch 3 acceptance tests: the coordination side of a FROST
// incoming sweep, bound to base keep-core's wallet-action machinery.
// Refs tuxxy/keep-core#45, #66 (as in base), and DECISIONS.md D19.
//
// Base model (DECISIONS.md D2, D19, tux 2026-10-29): a target wallet's incoming
// funds are swept by the base moved-funds-sweep action. In every coordination
// window the leader runs the production proposal generator over the window's
// checklist (pkg/tbtcpg), estimates the fee and puts it in the proposal; each
// follower validates the proposal against the chain only (no local fee
// configuration), as base WalletProposalValidator does for deposit and moved
// funds sweeps. Nothing is retained between windows.
//
// World: the frozen legacy world on the real upgraded Bridge (tbtc-v2
// integration/frost-base, LEGACY_UPGRADED_DIR) with Bitcoin Core regtest. The
// FROST incoming output is created by public calls only: a drain commitment of
// the legacy MovingFunds source to a ready FROST wallet, the drain transaction
// signed with the source's test key, mined by Core, and the Bridge's own
// movement proof. FIXTURE: the FROST registry fixture behind the target (no
// FROST DKG) and the source's test key (as in setUpFrostDrain).
// No build tag (tuxxy/keep-core#64).

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	golog "github.com/ipfs/go-log/v2"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

const frostDrainFee = int64(2_000)

// windowChecklist is base's checklist for a window after the deposit-sweep
// activation block, without the frequency-gated moving funds and heartbeat.
var windowChecklist = []tbtc.WalletActionType{tbtc.ActionRedemption, tbtc.ActionDepositSweep, tbtc.ActionMovedFundsSweep}

// frostIncomingRecord mirrors FrostMovementState.Incoming.
type frostIncomingRecord struct {
	Target, Source   [32]byte
	Value, CreatedAt uint64
	State            uint8
	SweepFeeCap      uint64
}

type frostIncomingWorld struct {
	*legacyWorld
	target    *frostTarget
	operators []chain.Address
	drain     *bitcoin.Transaction
	sweepCap  uint64
}

// newFrostIncomingWorld drains the legacy source into the FROST target and
// proves the drain, so the target holds one pending FROST incoming output.
func newFrostIncomingWorld(t *testing.T) *frostIncomingWorld {
	t.Helper()
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	target := &frostTarget{}
	w := &frostIncomingWorld{legacyWorld: newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), setUpFrostDrain(target)), target: target}
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	// The target's operators, as setUpFrostDrain registers them.
	w.operators = []chain.Address{chain.Address(w.operator.Hex()), chain.Address(admin.Hex()), chain.Address(common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8").Hex())}

	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(operatorKey, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.legacy.SubmitMovingFundsCommitment(opts, w.source.pkh, legacyUTXO(w.main), []uint32{1}, big.NewInt(1), [][20]byte{target.label})
	if err != nil {
		t.Fatalf("setup: drain commitment: %v", err)
	}
	if receipt, err := legacyWaitMined(context.Background(), w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: drain commitment not mined: %v", err)
	}

	// FIXTURE signer: the drain the K-07 producer would build, paying the
	// target's Taproot output key.
	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddPublicKeyHashInput(w.main); err != nil {
		t.Fatal(err)
	}
	script, err := bitcoin.PayToTaproot(target.q)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: w.main.Value - frostDrainFee, PublicKeyScript: script})
	w.drain = legacySign(t, builder, w.source.key)
	w.btc.mine(t, w.drain)
	w.send("drain proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsProof(o, legacyTxInfo(w.drain), w.btc.proof(t, w.drain.Hash()), legacyUTXO(w.main), w.source.pkh)
	})
	// Precondition: the Bridge recorded a pending FROST incoming output for
	// the target (C-06), keyed as keep-core keys it.
	key := deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: w.drain.Hash(), OutputIndex: 0})
	var out []interface{}
	if err := bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin).Call(&bind.CallOpts{}, &out, "frostIncoming", key); err != nil || len(out) != 1 {
		t.Fatalf("precondition: frostIncoming: %v", err)
	}
	record := *abi.ConvertType(out[0], new(frostIncomingRecord)).(*frostIncomingRecord)
	if record.Target != target.id || record.State != 1 || record.Value != uint64(w.main.Value-frostDrainFee) {
		t.Fatalf("precondition: the drain did not record a pending FROST incoming output for the target: %+v", record)
	}
	params, err := w.legacy.MovingFundsParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.sweepCap = params.MovedFundsSweepTxMaxTotalFee
	return w
}

// client is a production client (default configuration, public Connect),
// started fresh: nothing survives from an earlier client.
func (w *frostIncomingWorld) client(t *testing.T) *chainethereum.TbtcChain {
	t.Helper()
	c, err := w.connect(newLegacyRecorder(t, w.anvilURL, revertCode3))
	if err != nil {
		t.Fatalf("client startup: %v", err)
	}
	return c
}

// propose is one window's leader routine: the production proposal generator
// over the window's checklist for the FROST target.
func (w *frostIncomingWorld) propose(t *testing.T, c *chainethereum.TbtcChain, btc bitcoin.Chain, leader chain.Address) (tbtc.CoordinationProposal, error) {
	t.Helper()
	var proposal tbtc.CoordinationProposal
	err := legacyBounded(t, "proposal generation", func() error {
		var err error
		proposal, err = tbtcpg.NewProposalGenerator(c, btc).Generate(&tbtc.CoordinationProposalRequest{
			WalletPublicKeyHash: w.target.label,
			WalletOperators:     w.operators,
			ExecutingOperator:   leader,
			ActionsChecklist:    windowChecklist,
		})
		return err
	})
	return proposal, err
}

// requireIncomingProposal: the proposal is a moved funds sweep of the target's
// pending incoming output (the proven drain output) with a fee the leader put
// in, within the on-chain cap.
func (w *frostIncomingWorld) requireIncomingProposal(t *testing.T, what string, proposal tbtc.CoordinationProposal, err error) *tbtc.MovedFundsSweepProposal {
	t.Helper()
	p, ok := proposal.(*tbtc.MovedFundsSweepProposal)
	if err != nil || !ok {
		t.Fatalf("%s: no moved funds sweep proposal for the FROST target's pending incoming output (got %T, %v)", what, proposal, err)
	}
	if p.MovingFundsTxHash != [32]byte(w.drain.Hash()) || p.MovingFundsTxOutputIndex != 0 {
		t.Fatalf("%s: the proposal sweeps %x:%d, not the pending incoming output %x:0", what, p.MovingFundsTxHash, p.MovingFundsTxOutputIndex, [32]byte(w.drain.Hash()))
	}
	if p.SweepTxFee == nil || p.SweepTxFee.Sign() <= 0 || p.SweepTxFee.Uint64() > w.sweepCap {
		t.Fatalf("%s: proposal fee %v is not within (0, %d], the on-chain cap", what, p.SweepTxFee, w.sweepCap)
	}
	return p
}

func (w *frostIncomingWorld) validate(c *chainethereum.TbtcChain, p *tbtc.MovedFundsSweepProposal) error {
	return tbtc.ValidateMovedFundsSweepProposal(golog.Logger("b3-follower"), w.target.label, p, c)
}

// scaledFees is an ADAPTER Bitcoin view whose fee estimate is factor times
// Core's: another operator's local fee configuration.
type scaledFees struct {
	bitcoin.Chain
	factor int64
}

func (s scaledFees) EstimateSatPerVByteFee(blocks uint32) (int64, error) {
	fee, err := s.Chain.EstimateSatPerVByteFee(blocks)
	return fee * s.factor, err
}

// TestFrostIncomingSweepProposedEveryWindow (tuxxy/keep-core#45, #66, as in
// base): while the target's incoming output is pending, every window's leader,
// whichever operator it is, proposes its sweep from chain state, including a
// client started after an earlier window failed. No window depends on an
// earlier one.
func TestFrostIncomingSweepProposedEveryWindow(t *testing.T) {
	w := newFrostIncomingWorld(t)
	c := w.client(t)
	stop := w.mineInBackground()
	defer stop()
	for i, leader := range w.operators {
		proposal, err := w.propose(t, c, w.btc, leader)
		w.requireIncomingProposal(t, "window with leader "+string(leader), proposal, err)
		if i == 0 {
			// A window passes with nothing executed; the next client starts
			// from scratch (a restart).
			c = w.client(t)
		}
	}
}

// TestFrostIncomingSweepFollowerValidation (D19, as in base; K09-29): a
// follower accepts any leader fee within the on-chain cap, whatever its own
// local fee configuration, and refuses a fee above the cap, a zero fee, and a
// proposal naming an output that is not the target's pending incoming output.
func TestFrostIncomingSweepFollowerValidation(t *testing.T) {
	w := newFrostIncomingWorld(t)
	c := w.client(t)
	stop := w.mineInBackground()
	defer stop()
	good := func(fee uint64) *tbtc.MovedFundsSweepProposal {
		return &tbtc.MovedFundsSweepProposal{MovingFundsTxHash: [32]byte(w.drain.Hash()), MovingFundsTxOutputIndex: 0, SweepTxFee: new(big.Int).SetUint64(fee)}
	}
	for _, fee := range []uint64{1, w.sweepCap / 2, w.sweepCap} {
		if err := w.validate(c, good(fee)); err != nil {
			t.Fatalf("a follower refused a leader proposal with fee %d within the on-chain cap %d: %v", fee, w.sweepCap, err)
		}
	}
	refusals := map[string]*tbtc.MovedFundsSweepProposal{
		"fee above the on-chain cap": good(w.sweepCap + 1),
		"zero fee":                   good(0),
		"wrong output index":         {MovingFundsTxHash: [32]byte(w.drain.Hash()), MovingFundsTxOutputIndex: 1, SweepTxFee: big.NewInt(1)},
		"unrecorded output":          {MovingFundsTxHash: [32]byte{0xb3}, MovingFundsTxOutputIndex: 0, SweepTxFee: big.NewInt(1)},
	}
	for name, p := range refusals {
		if err := w.validate(c, p); err == nil {
			t.Errorf("a follower accepted a proposal with %s", name)
		}
	}
}

// TestFrostIncomingSweepDivergentLocalFees (D19, as in base; K09-29): two
// leaders with different local fee estimates both propose within the cap, and
// each proposal passes the other operator's validation, so operators never
// diverge on their own fee settings. A leader whose estimate exceeds the cap
// proposes no sweep above it.
func TestFrostIncomingSweepDivergentLocalFees(t *testing.T) {
	w := newFrostIncomingWorld(t)
	c := w.client(t)
	stop := w.mineInBackground()
	defer stop()
	low, err := w.propose(t, c, w.btc, w.operators[0])
	pLow := w.requireIncomingProposal(t, "leader with Core's fee estimate", low, err)
	high, err := w.propose(t, c, scaledFees{w.btc, 3}, w.operators[1])
	pHigh := w.requireIncomingProposal(t, "leader with a three times higher fee estimate", high, err)
	for name, p := range map[string]*tbtc.MovedFundsSweepProposal{"low-fee": pLow, "high-fee": pHigh} {
		if err := w.validate(c, p); err != nil {
			t.Fatalf("a follower refused the %s leader's proposal (fee %v, cap %d): %v", name, p.SweepTxFee, w.sweepCap, err)
		}
	}
	over, err := w.propose(t, c, scaledFees{w.btc, 1_000_000}, w.operators[2])
	if p, ok := over.(*tbtc.MovedFundsSweepProposal); err == nil && ok && p.SweepTxFee.Uint64() > w.sweepCap {
		t.Fatalf("a leader proposed fee %v above the on-chain cap %d", p.SweepTxFee, w.sweepCap)
	}
}
