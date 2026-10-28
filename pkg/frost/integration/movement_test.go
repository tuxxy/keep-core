package integration

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

func (w *k07World) incomingSweep(h *walletHost) *signing.Plan {
	return w.plans(h, func(p movement.Planner) (*signing.Plan, error) { return p.IncomingSweep(w.f.ctx) })
}

func (w *k07World) movementPlan(h *walletHost) *signing.Plan {
	return w.plans(h, func(p movement.Planner) (*signing.Plan, error) { return p.Movement(w.f.ctx) })
}

// closeable notifies C-09 that h is closeable (old or low custody).
func (w *k07World) closeable(h *walletHost) {
	w.t.Helper()
	if err := w.chain.NotifyCloseable(w.f.ctx, h.id); err != nil {
		w.t.Fatal("notify closeable:", err)
	}
}

func (w *k07World) commit(h *walletHost) [][20]byte {
	w.t.Helper()
	targets, err := h.commits[0].Commit(w.f.ctx)
	if err != nil {
		w.t.Fatal("commitment:", err)
	}
	w.mine(int(k06Finality))
	return targets
}

// warp advances Ethereum time.
func (w *k07World) warp(seconds uint64) {
	w.rpc("evm_increaseTime", seconds)
	w.mine(1)
}

// moveAToB funds A by a K-06 sweep, moves it into MovingFunds through C-09,
// commits to B and settles the movement. It returns the movement plan.
func (w *k07World) moveAToB() *signing.Plan {
	t := w.t
	t.Helper()
	w.fundBySweep()
	w.closeable(w.a)
	if s := w.state(w.a.id); s != movement.StateMovingFunds {
		t.Fatalf("A state %d, want MovingFunds", s)
	}
	if targets := w.commit(w.a); len(targets) != 1 || targets[0] != w.b.label() {
		t.Fatalf("commitment targets %x", targets)
	}
	plan := w.movementPlan(w.a)
	w.settle(w.a, plan, w.proofs.ProveMovement)
	return plan
}

// ECDSA → FROST drain into A, A's incoming sweep, then FROST → FROST from A to
// B and B's incoming sweep, all mined on Bitcoin Core and proven once.
func TestCrossSchemeDrainAndSweep(t *testing.T) {
	w := newK07World(t)
	ctx := w.f.ctx
	lw := w.legacyWallet(k07DrainValue)
	labels := w.commitDrain(lw, []*walletHost{w.a})
	// The legacy host path: pkg/tbtcpg's commitment hash and the real
	// WalletProposalValidator accept the drain proposal.
	opts := &bind.CallOpts{Context: ctx}
	stored, err := w.legacy.Wallets(opts, lw.pkh)
	if err != nil {
		t.Fatal(err)
	}
	// The helper ignores its receiver.
	if hash := (&eth.TbtcChain{}).ComputeMovingFundsCommitmentHash(labels); stored.MovingFundsTargetWalletsCommitmentHash != hash {
		t.Fatalf("legacy commitment hash %x, the legacy host computes %x", stored.MovingFundsTargetWalletsCommitmentHash, hash)
	}
	validator, err := legacyabi.NewWalletProposalValidator(w.f.deployment.Legacy.Validator, w.f.client)
	if err != nil {
		t.Fatal(err)
	}
	proposal := legacyabi.WalletProposalValidatorMovingFundsProposal{WalletPubKeyHash: lw.pkh, TargetWallets: labels, MovingFundsTxFee: big.NewInt(k07DrainFee)}
	if valid, err := validator.ValidateMovingFundsProposal(opts, proposal, legacyabi.BitcoinTxUTXO3(lw.utxo)); err != nil || !valid {
		t.Fatal("legacy validator refused the drain proposal:", valid, err)
	}
	drain := w.drain(lw, []*walletHost{w.a})
	key := movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: drain.Hash()})
	incoming, err := w.chain.Incoming(ctx, w.head(), key)
	if err != nil || incoming.State != movement.IncomingPending || incoming.Target != w.a.id || incoming.Value != k07DrainValue-k07DrainFee {
		t.Fatalf("drain incoming: %+v %v", incoming, err)
	}
	var source [32]byte
	copy(source[:], lw.pkh[:])
	if incoming.Source != source {
		t.Fatal("a drain records its legacy source label")
	}
	w.mine(int(k06Finality))
	sweepA := w.incomingSweep(w.a)
	w.settle(w.a, sweepA, w.proofs.ProveIncomingSweep)
	if s := w.state(w.a.id); s != movement.StateLive {
		t.Fatalf("A state %d after its first incoming sweep", s)
	}
	valueA := uint64(sweepA.Snapshot().Unsigned().Outputs[0].Value)
	if c := w.custody(w.a.id); c.Value != valueA || valueA != k07DrainValue-k07DrainFee-uint64(sweepA.Snapshot().Fee()) {
		t.Fatalf("A custody %d, want %d", c.Value, valueA)
	}
	if again, _ := w.chain.Incoming(ctx, w.head(), key); again.State != movement.IncomingSwept {
		t.Fatal("drain incoming not consumed")
	}
	t.Logf("drain: %d sats legacy -> %d sats in A (drain fee %d, sweep fee %d)", k07DrainValue, valueA, k07DrainFee, sweepA.Snapshot().Fee())

	w.closeable(w.a)
	if targets := w.commit(w.a); len(targets) != 1 || targets[0] != w.b.label() {
		t.Fatalf("commitment targets %x", targets)
	}
	move := w.movementPlan(w.a)
	w.settle(w.a, move, w.proofs.ProveMovement)
	if s := w.state(w.a.id); s != movement.StateClosing || w.custody(w.a.id).Value != 0 {
		t.Fatal("A must be Closing with no custody after its movement")
	}
	w.mine(int(k06Finality))
	sweepB := w.incomingSweep(w.b)
	// B's proof reply is lost after it was mined: the maintainer settles it
	// by rereading, never by resubmitting.
	lossy := w.proofs
	lossy.Chain = movementLostReply{w.chain}
	w.settle(w.b, sweepB, lossy.ProveIncomingSweep)
	moved := uint64(move.Snapshot().Unsigned().Outputs[0].Value)
	if c := w.custody(w.b.id); c.Value != moved-uint64(sweepB.Snapshot().Fee()) || w.state(w.b.id) != movement.StateLive {
		t.Fatalf("B custody %d after %d moved", c.Value, moved)
	}
	if valueA != moved+uint64(move.Snapshot().Fee()) {
		t.Fatal("movement value is not conserved apart from its fee")
	}
	t.Logf("witness: drain %x, A sweep %x, movement %x, B sweep %x", [32]byte(drain.Hash()), sweepA.BitcoinTransactionHash(), move.BitcoinTransactionHash(), sweepB.BitcoinTransactionHash())
}

// The committed target leaves eligibility inside the movement's first signing
// claim: the movement still pays it exactly as committed, and the target, now
// in MovingFunds, still sweeps the incoming output.
func TestMovementTargetStateRace(t *testing.T) {
	w := newK07World(t)
	ctx := w.f.ctx
	w.fundBySweep()
	w.closeable(w.a)
	w.commit(w.a)
	// B must be old to be closeable by a third party.
	params, err := w.legacy.WalletParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	w.warp(uint64(params.WalletMaxAge))
	plan := w.movementPlan(w.a)
	raced := false
	hook := &claimHook{hookedJournal: w.a.journals[0], onClaim: func() {
		raced = true
		if err := w.chain.NotifyCloseable(ctx, w.b.id); err != nil {
			t.Error("target closeable inside the claim:", err)
		}
	}}
	w.a.movers[0].Base.Journal = hook
	w.settle(w.a, plan, w.proofs.ProveMovement)
	if !raced {
		t.Fatal("the claim hook did not run")
	}
	b, err := w.chain.Wallet(ctx, w.head(), w.b.label())
	if err != nil || b.State != movement.StateMovingFunds {
		t.Fatalf("B, owed a committed transfer, must go to MovingFunds, not Closing: %+v %v", b, err)
	}
	if plan.MovementTargets()[0].OutputKey != w.b.desc.OutputKey {
		t.Fatal("movement paid a key the commitment did not bind")
	}
	if open := w.obligations(w.b.id); open&movement.OpenPendingIncoming == 0 {
		t.Fatal("B's incoming must be pending")
	}
	w.mine(int(k06Finality))
	sweep := w.incomingSweep(w.b)
	w.settle(w.b, sweep, w.proofs.ProveIncomingSweep)
	if open := w.obligations(w.b.id); open&(movement.OpenPendingIncoming|movement.OpenCommittedIncoming) != 0 {
		t.Fatalf("B incoming obligations left: %08b", open)
	}
	if w.state(w.b.id) != movement.StateMovingFunds {
		t.Fatal("B keeps its MovingFunds duty with its new main")
	}
}

// setMovingFundsFeeCap sets movingFundsTxMaxTotalFee, keeping the other
// moving funds parameters.
func (w *k07World) setMovingFundsFeeCap(cap uint64) {
	w.t.Helper()
	m, err := w.legacy.MovingFundsParameters(&bind.CallOpts{Context: w.f.ctx})
	if err != nil {
		w.t.Fatal(err)
	}
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateMovingFundsParameters(o, cap, m.MovingFundsDustThreshold, m.MovingFundsTimeoutResetDelay, m.MovingFundsTimeout, m.MovingFundsTimeoutSlashingAmount, m.MovingFundsTimeoutNotifierRewardMultiplier, m.MovingFundsCommitmentGasOffset, m.MovedFundsSweepTxMaxTotalFee, m.MovedFundsSweepTimeout, m.MovedFundsSweepTimeoutSlashingAmount, m.MovedFundsSweepTimeoutNotifierRewardMultiplier)
	})
}

// The cap is raised after the commitment: the host still plans and
// authorizes within the committed cap, so C-06 accepts the proof. At the
// current cap the fee rate alone (about 220 sat) would exceed the 150-sat
// commitment and the proof would revert.
func TestMovementCommittedFeeCap(t *testing.T) {
	w := newK07World(t)
	ctx := w.f.ctx
	w.fundBySweep()
	w.closeable(w.a)
	w.setMovingFundsFeeCap(150)
	w.commit(w.a)
	w.setMovingFundsFeeCap(100_000)
	lifecycle, err := w.chain.Lifecycle(ctx, w.head(), w.a.id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.chain.Commitment(ctx, w.head(), w.a.id, lifecycle.Period)
	if err != nil || c.MaxFee != 150 {
		t.Fatalf("committed cap %d, want 150: %v", c.MaxFee, err)
	}
	plan := w.movementPlan(w.a)
	if fee := plan.Snapshot().Fee(); fee != 150 {
		t.Fatalf("movement fee %d, want the committed 150", fee)
	}
	w.settle(w.a, plan, w.proofs.ProveMovement)
	if c, err = w.chain.Commitment(ctx, w.head(), w.a.id, lifecycle.Period); err != nil || c.ProvenAt == 0 {
		t.Fatalf("movement not proven: %+v %v", c, err)
	}
}

// With no eligible target the commitment is refused and the clock resets; a
// below-dust main is recorded as dust and the wallet closes on the ledger
// rule with its key still loadable.
func TestNoTargetAndDustRecovery(t *testing.T) {
	w := newK07World(t)
	ctx := w.f.ctx
	w.fundBySweep()
	params, err := w.legacy.WalletParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	// B, old and empty, owes nothing: it goes straight to Closing.
	w.warp(uint64(params.WalletMaxAge))
	w.closeable(w.b)
	if s := w.state(w.b.id); s != movement.StateClosing {
		t.Fatalf("B state %d, want Closing", s)
	}
	w.closeable(w.a)
	if _, err = w.a.commits[0].Commit(ctx); !errors.Is(err, movement.ErrNoTarget) {
		t.Fatal("commitment with no eligible target:", err)
	}
	moving, err := w.legacy.MovingFundsParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.chain.ResetMovingFundsTimeout(ctx, w.a.id); err == nil {
		t.Fatal("reset before its delay")
	}
	w.warp(uint64(moving.MovingFundsTimeoutResetDelay) + 1)
	if err = w.chain.ResetMovingFundsTimeout(ctx, w.a.id); err != nil {
		t.Fatal("reset with no eligible target:", err)
	}
	// Raise the dust threshold above A's main (D-07 test value, OPEN).
	redemption, err := w.legacy.RedemptionParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateRedemptionParameters(o, 5_000_000, redemption.RedemptionTreasuryFeeDivisor, redemption.RedemptionTxMaxFee, redemption.RedemptionTxMaxTotalFee, redemption.RedemptionTimeout, redemption.RedemptionTimeoutSlashingAmount, redemption.RedemptionTimeoutNotifierRewardMultiplier)
	})
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateMovingFundsParameters(o, moving.MovingFundsTxMaxTotalFee, 2_000_000, moving.MovingFundsTimeoutResetDelay, moving.MovingFundsTimeout, moving.MovingFundsTimeoutSlashingAmount, moving.MovingFundsTimeoutNotifierRewardMultiplier, moving.MovingFundsCommitmentGasOffset, moving.MovedFundsSweepTxMaxTotalFee, moving.MovedFundsSweepTimeout, moving.MovedFundsSweepTimeoutSlashingAmount, moving.MovedFundsSweepTimeoutNotifierRewardMultiplier)
	})
	if err = w.chain.NotifyBelowDust(ctx, w.a.id); err != nil {
		t.Fatal("dust:", err)
	}
	lifecycle, err := w.chain.Lifecycle(ctx, w.head(), w.a.id)
	if err != nil || !lifecycle.Dust || w.state(w.a.id) != movement.StateClosing {
		t.Fatalf("dust not recorded: %+v %v", lifecycle, err)
	}
	if err = w.a.closures[0].Close(ctx, w.a.id); err == nil {
		t.Fatal("closed before the closing period")
	}
	w.warp(uint64(params.WalletClosingPeriod) + 1)
	w.mine(int(k06Finality))
	if err = w.a.closures[0].Close(ctx, w.a.id); err != nil {
		t.Fatal("close:", err)
	}
	if w.state(w.a.id) != movement.StateClosed || w.custody(w.a.id).Value == 0 {
		t.Fatal("A closed with its dust custody still recorded")
	}
	for i, j := range w.a.journals {
		if _, err := j.LoadKey(ctx); err != nil {
			t.Fatalf("operator %d lost A's key at closure: %v", i, err)
		}
		if archived, err := movement.IsArchived(ctx, j, w.a.id); err != nil || (i == 0 && !archived) {
			t.Fatal("closure not archived", err)
		}
	}
}

// A closing wallet with an open fraud challenge on its own public movement
// signature does not close until the C-08 defense resolves it.
func TestCloseWithPendingObligations(t *testing.T) {
	w := newK07World(t)
	ctx := w.f.ctx
	move := w.moveAToB()
	if open := w.obligations(w.a.id); open != 0 {
		t.Fatalf("A obligations after its proven movement: %08b", open)
	}
	if open := w.obligations(w.b.id); open&movement.OpenPendingIncoming == 0 {
		t.Fatal("B must owe its incoming sweep")
	}
	// The movement witness is public on Bitcoin: challenge its signature.
	mined := w.coreTx(bitcoin.Hash(move.BitcoinTransactionHash()))
	signature := mined.Inputs[0].Witness[0]
	digest := move.Snapshot().Digests()[0]
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		o.Value = new(big.Int).Mul(big.NewInt(5), big.NewInt(1e18))
		return w.bridge.SubmitFrostFraudChallenge(o, w.a.id, digest, signature)
	})
	if open := w.obligations(w.a.id); open != movement.OpenChallenge {
		t.Fatalf("A obligations with a challenge: %08b", open)
	}
	params, err := w.legacy.WalletParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	w.warp(uint64(params.WalletClosingPeriod) + 1)
	w.mine(int(k06Finality))
	if err = w.a.closures[0].Close(ctx, w.a.id); !errors.Is(err, movement.ErrObligations) {
		t.Fatal("closed with an open challenge:", err)
	}
	info := spv.TransactionInfo(mined)
	script := move.MovementMain().PublicKeyScript
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.DefeatFrostFraudChallenge(o, frostbridgeabi.FrostFraudDefense{WalletId: w.a.id, Transaction: frostbridgeabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, Prevouts: []frostbridgeabi.FrostFraudPrevout{{Value: uint64(move.MovementMain().Value), Script: script}}, InputIndex: 0, Authorization: 1})
	})
	w.mine(int(k06Finality))
	if err = w.a.closures[0].Close(ctx, w.a.id); err != nil {
		t.Fatal("close after the defense:", err)
	}
	if w.state(w.a.id) != movement.StateClosed {
		t.Fatal("A not closed")
	}
	for i, j := range w.a.journals {
		if _, err := j.LoadKey(ctx); err != nil {
			t.Fatalf("operator %d lost A's key: %v", i, err)
		}
	}
	// B still owes its incoming sweep and is not closable.
	if err = w.b.closures[0].Close(context.Background(), w.b.id); err == nil {
		t.Fatal("B closed with incoming pending")
	}
}
