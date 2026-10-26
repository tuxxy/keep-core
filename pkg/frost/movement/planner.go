package movement

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// ErrNoTarget reports that no eligible target exists: the source keeps its
// funds and the moving funds clock resets (C-06).
var ErrNoTarget = errors.New("no eligible FROST movement target")

// ErrNothingIncoming reports that a target has no sweepable incoming output.
var ErrNothingIncoming = errors.New("no pending FROST incoming output")

// Planner derives commitments and transactions from finalized chain state,
// so every honest operator derives the same plan without coordination.
type Planner struct {
	View    *View
	Bitcoin redemption.ScriptUnspent
	Wallet  dkg.Descriptor
	Genesis [32]byte
	// FeeRate sizes transactions; MaximumSatPerVByte is the ceiling. Both are
	// D-09 inputs (local test values, OPEN).
	FeeRate, MaximumSatPerVByte int64
	boundary                    func(string) error
}

func (p Planner) step(name string) error {
	if p.boundary != nil {
		return p.boundary(name)
	}
	return nil
}

func (p Planner) walletID() [32]byte { return dkg.WalletID(p.Wallet.OutputKey) }

// Targets chooses the targets of a new commitment for the source snapshot s:
// the eligible FROST wallets in ascending label order, the C-06 count
// min(eligibleTargets, ceil(value / walletMaxBtcTransfer)), lowest labels
// first. Every operator reads the same finalized logs and records.
func (p Planner) Targets(ctx context.Context, s Source) ([][20]byte, error) {
	if s.Custody.Value == 0 || s.Parameters.WalletMaxBtcTransfer == 0 {
		return nil, errors.New("nothing to move")
	}
	counters, err := p.View.Chain.Counters(ctx, s.Head.Number, s.Wallet.ID)
	if err != nil {
		return nil, err
	}
	needed := (s.Custody.Value + s.Parameters.WalletMaxBtcTransfer - 1) / s.Parameters.WalletMaxBtcTransfer
	if counters.EligibleTargets < needed {
		needed = counters.EligibleTargets
	}
	if needed == 0 {
		return nil, ErrNoTarget
	}
	ids, err := p.View.Chain.ReadyWallets(ctx, p.View.Policy.From, s.Head.Number)
	if err != nil {
		return nil, err
	}
	var eligible [][20]byte
	seen := map[[20]byte]bool{}
	for _, id := range ids {
		label := Label(id)
		if seen[label] || id == s.Wallet.ID {
			continue
		}
		seen[label] = true
		w, err := p.View.Chain.Wallet(ctx, s.Head.Number, label)
		if err != nil {
			return nil, err
		}
		if w.Eligible() {
			eligible = append(eligible, label)
		}
	}
	if uint64(len(eligible)) < needed {
		return nil, fmt.Errorf("%w: %d eligible wallets seen, %d counted", ErrNoTarget, len(eligible), needed)
	}
	sort.Slice(eligible, func(i, j int) bool { return bytes.Compare(eligible[i][:], eligible[j][:]) < 0 })
	return eligible[:needed], nil
}

// Commitments submits this operator's C-06 commitment for the source once:
// a recorded commitment of the current period is success when it names the
// same targets, and a refusal otherwise.
type Commitments struct {
	Chain    Chain
	Planner  Planner
	Seat     uint16
	boundary func(string) error
}

func (c Commitments) step(name string) error {
	if c.boundary != nil {
		return c.boundary(name)
	}
	return nil
}

// Commit derives and submits the commitment. It returns the targets recorded.
func (c Commitments) Commit(ctx context.Context) ([][20]byte, error) {
	wallet := c.Planner.walletID()
	s, err := c.Planner.View.Source(ctx, wallet)
	if err != nil {
		return nil, err
	}
	if s.Wallet.State != StateMovingFunds && s.Wallet.State != StateTerminated {
		return nil, fmt.Errorf("%w: source state %d", ErrWalletState, s.Wallet.State)
	}
	targets, err := c.Planner.Targets(ctx, s)
	if err != nil && s.Log == nil {
		return nil, err
	}
	if s.Log != nil {
		if err == nil && !sameLabels(targets, s.Commitment.Targets) {
			return nil, fmt.Errorf("%w: recorded commitment names other targets", ErrPlanMismatch)
		}
		return s.Commitment.Targets, nil
	}
	if err = c.step("before-commitment"); err != nil {
		return nil, err
	}
	if err = c.Chain.SubmitCommitment(ctx, wallet, c.Seat, targets); err != nil {
		// Another operator may have committed first, or the reply was lost.
		again, readErr := c.Planner.View.Source(ctx, wallet)
		if readErr != nil || again.Log == nil || !sameLabels(targets, again.Commitment.Targets) {
			return nil, fmt.Errorf("movement commitment: %w", err)
		}
	}
	return targets, c.step("commitment-submitted")
}

func sameLabels(a, b [][20]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// main discovers the wallet's custody main by exact script on Bitcoin.
func (p Planner) main(ctx context.Context, custody redemption.Custody, value uint64) (bitcoin.PreviousOutput, error) {
	script, err := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if err != nil {
		return bitcoin.PreviousOutput{}, err
	}
	candidates, err := p.Bitcoin.UnspentByScript(ctx, script)
	if err != nil {
		return bitcoin.PreviousOutput{}, err
	}
	for _, c := range candidates {
		if OutpointKey(c.Outpoint) == custody.MainOutpoint && uint64(c.Value) == value {
			return c, nil
		}
	}
	return bitcoin.PreviousOutput{}, redemption.ErrNoCustody
}

// Movement builds the exact movement of the recorded commitment: the main to
// each committed target in order, the equal split of main - fee, the fee at
// FeeRate within the cap recorded in the commitment.
func (p Planner) Movement(ctx context.Context) (*signing.Plan, error) {
	if err := p.step("before-movement-plan"); err != nil {
		return nil, err
	}
	if p.FeeRate <= 0 || p.MaximumSatPerVByte < p.FeeRate {
		return nil, errors.New("movement fee policy")
	}
	s, err := p.View.Source(ctx, p.walletID())
	if err != nil {
		return nil, err
	}
	if s.Log == nil {
		return nil, fmt.Errorf("%w: nothing committed", ErrPlanMismatch)
	}
	if s.Commitment.ProvenAt != 0 {
		return nil, ErrSettled
	}
	main, err := p.main(ctx, s.Custody, s.MainValue)
	if err != nil {
		return nil, err
	}
	var targets []signing.MovementTarget
	for i, w := range s.Targets {
		if w.DescriptorHash != s.Commitment.DescriptorHashes[i] {
			return nil, fmt.Errorf("%w: target %d descriptor", ErrPlanMismatch, i)
		}
		targets = append(targets, signing.MovementTarget{Label: s.Commitment.Targets[i], DescriptorHash: w.DescriptorHash, OutputKey: w.Descriptor.OutputKey})
	}
	o := main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &o, Sequence: 0xffffffff}}}
	for _, t := range targets {
		script, _ := bitcoin.PayToTaproot(t.OutputKey)
		tx.Outputs = append(tx.Outputs, &bitcoin.TransactionOutput{Value: 1, PublicKeyScript: script})
	}
	sizing, err := bitcoin.NewTaprootTransaction(tx, []bitcoin.PreviousOutput{main}, p.Wallet.OutputKey)
	if err != nil {
		return nil, err
	}
	fee := p.FeeRate * sizing.VirtualSize()
	if uint64(fee) > s.Commitment.MaxFee {
		fee = int64(s.Commitment.MaxFee)
	}
	for i, v := range signing.MovementSplit(main.Value-fee, len(targets)) {
		tx.Outputs[i].Value = v
	}
	return signing.NewMovementPlan(signing.MovementProposal{Wallet: p.Wallet, BitcoinGenesis: p.Genesis, Generation: s.Custody.Generation, StateVersion: s.Custody.StateVersion, Period: s.Lifecycle.Period, Main: main, Targets: targets, MaximumFee: int64(s.Commitment.MaxFee), MaximumSatPerVByte: p.MaximumSatPerVByte, Transaction: tx})
}

// IncomingSweep builds the target's sweep of every finalized pending or
// escalated incoming output (ascending key order, at most 127) with its main.
func (p Planner) IncomingSweep(ctx context.Context) (*signing.Plan, error) {
	if err := p.step("before-movement-plan"); err != nil {
		return nil, err
	}
	if p.FeeRate <= 0 || p.MaximumSatPerVByte < p.FeeRate {
		return nil, errors.New("incoming sweep fee policy")
	}
	wallet := p.walletID()
	t, err := p.View.Target(ctx, wallet, nil)
	if err != nil {
		return nil, err
	}
	if t.Custody.ActiveReservation != ([32]byte{}) {
		return nil, ErrMainReserved
	}
	script, err := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if err != nil {
		return nil, err
	}
	var incoming []bitcoin.PreviousOutput
	// The fee stays within the live proof cap and every included output's
	// recorded cap, so the sweep is provable now and defensible later.
	capFee := t.Parameters.MovedFundsSweepTxMaxTotalFee
	sort.Slice(t.Incoming, func(i, j int) bool {
		return bytes.Compare(t.Incoming[i].Log.Outpoint[:], t.Incoming[j].Log.Outpoint[:]) < 0
	})
	for _, in := range t.Incoming {
		if in.Record.Target != wallet || (in.Record.State != IncomingPending && in.Record.State != IncomingEscalated) || !p.View.Final(t.Head, in.Log.Log) || len(incoming) == signing.MaxSweepInputs-1 {
			continue
		}
		incoming = append(incoming, bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(in.Log.TransactionHash), OutputIndex: in.Log.OutputIndex}, Value: int64(in.Record.Value), PublicKeyScript: script})
		if in.Record.SweepFeeCap < capFee {
			capFee = in.Record.SweepFeeCap
		}
	}
	if len(incoming) == 0 {
		return nil, ErrNothingIncoming
	}
	var main *bitcoin.PreviousOutput
	tx := &bitcoin.Transaction{Version: 2}
	prevouts := []bitcoin.PreviousOutput{}
	if t.Custody.MainOutpoint != ([32]byte{}) {
		out, err := p.View.Chain.CustodyOutput(ctx, t.Head.Number, t.Custody.MainOutpoint)
		if err != nil {
			return nil, err
		}
		m, err := p.main(ctx, t.Custody, out.Value)
		if err != nil {
			return nil, err
		}
		main = &m
		prevouts = append(prevouts, m)
	}
	prevouts = append(prevouts, incoming...)
	var total int64
	for _, prev := range prevouts {
		o := prev.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
		total += prev.Value
	}
	tx.Outputs = []*bitcoin.TransactionOutput{{Value: 1, PublicKeyScript: script}}
	sizing, err := bitcoin.NewTaprootTransaction(tx, prevouts, p.Wallet.OutputKey)
	if err != nil {
		return nil, err
	}
	fee := p.FeeRate * sizing.VirtualSize()
	if uint64(fee) > capFee {
		fee = int64(capFee)
	}
	tx.Outputs[0].Value = total - fee
	return signing.NewIncomingSweepPlan(signing.IncomingSweepProposal{Wallet: p.Wallet, BitcoinGenesis: p.Genesis, Generation: t.Custody.Generation, StateVersion: t.Custody.StateVersion, Main: main, Incoming: incoming, MaximumFee: int64(capFee), MaximumSatPerVByte: p.MaximumSatPerVByte, Transaction: tx})
}
