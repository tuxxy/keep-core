package deposit

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// Authority is the K-06 sweep authorization (P-K06-SWEEP-AUTHORIZED-ONLY).
// The executor calls it at start, after the durable claim right before every
// worker grant, after every signature and before broadcast. Each call rereads
// one consistent snapshot and refuses unless every deposit input is revealed
// for this wallet at a finalized canonical block, unswept, before its refund
// margin, and exactly the input the plan froze, built from the D-04 template
// for this Q; and the main input follows the C-05 inclusion rule.
type Authority struct {
	View *View
	// Directives are governance instructions for a contained reservation. A
	// resolution sweep of a Quarantined reservation needs a cancel directive
	// at every authorization, not only when it was attested (spec issue #3).
	Directives redemption.Directives
}

var _ signing.SweepAuthority = (*Authority)(nil)

// ErrPlanMismatch reports a plan input that differs from the C-05 record.
var ErrPlanMismatch = errors.New("FROST sweep plan differs from the C-05 deposit record")

func (a *Authority) AuthorizeSweep(ctx context.Context, plan *signing.Plan) (signing.ReservationReceipt, error) {
	if !plan.IsSweep() {
		return signing.ReservationReceipt{}, errors.New("not a sweep plan")
	}
	planned := plan.SweepDeposits()
	keys := make([][32]byte, len(planned))
	for i, d := range planned {
		keys[i] = d.Key
	}
	wallet := plan.WalletID()
	s, err := a.View.Read(ctx, wallet, keys)
	if err != nil {
		return signing.ReservationReceipt{}, err
	}
	var anchor Reveal
	for i, d := range s.Deposits {
		if err = a.View.Sweepable(s, d, wallet); err != nil {
			return signing.ReservationReceipt{}, err
		}
		if err = matches(plan, planned[i], d.Record); err != nil {
			return signing.ReservationReceipt{}, err
		}
		if d.Reveal.Block > anchor.Block {
			anchor = d.Reveal
		}
	}
	if err = mainRule(plan, s); err != nil {
		return signing.ReservationReceipt{}, err
	}
	if r := plan.SweepResolution(); r != ([32]byte{}) {
		if err = a.resolutionState(ctx, s.Head.Number, plan, r); err != nil {
			return signing.ReservationReceipt{}, err
		}
	}
	return signing.ReservationReceipt{Block: anchor.Block, Hash: anchor.BlockHash}, nil
}

// resolutionState requires the resolved reservation to be unresolved at the
// snapshot head and, while it is Quarantined, a governance cancel directive.
func (a *Authority) resolutionState(ctx context.Context, at uint64, plan *signing.Plan, reservation [32]byte) error {
	intent, err := a.View.Chain.Intent(ctx, at, reservation)
	if err != nil {
		return err
	}
	if !intent.State.Unresolved() || intent.Certificate.WalletID != plan.WalletID() {
		return fmt.Errorf("%w: resolved reservation is no longer unresolved", ErrPlanMismatch)
	}
	if intent.State != redemption.IntentQuarantined {
		return nil
	}
	if a.Directives == nil {
		return redemption.ErrContained
	}
	d, ok, err := a.Directives.Directive(ctx, reservation)
	if err != nil {
		return err
	}
	if !ok || d.Reservation != reservation || d.Class == "" || d.Action != redemption.DirectiveCancel {
		return redemption.ErrContained
	}
	return nil
}

// matches compares one planned deposit input with its C-05 record and with
// the D-04 construction rebuilt for the plan's Q.
func matches(plan *signing.Plan, d signing.SweepDeposit, r Record) error {
	q := plan.Snapshot().OutputKey()
	funding := d.Prevout.Outpoint
	if r.Terms.Amount != uint64(d.Prevout.Value) || r.FundingTransactionHash != [32]byte(funding.TransactionHash) || r.FundingOutputIndex != funding.OutputIndex || OutpointKey(funding) != d.Key {
		return fmt.Errorf("%w: funding outpoint or value", ErrPlanMismatch)
	}
	if r.TreasuryFee != d.TreasuryFee || r.MaximumSweepFee != d.MaximumSweepFee || r.Terms.RefundLocktime != d.RefundLocktime {
		return fmt.Errorf("%w: captured fees or refund time", ErrPlanMismatch)
	}
	if r.Terms.Vault != plan.SweepVault() || r.Terms.DescriptorHash != plan.DescriptorHash() || r.WalletKey != q {
		return fmt.Errorf("%w: vault, descriptor or wallet key", ErrPlanMismatch)
	}
	c, err := Build(r.Terms, q)
	if err != nil {
		return err
	}
	if c.OutputKey != r.DepositOutputKey || c.Root != r.MerkleRoot || c.Parity != r.OutputParity || c.Receipt != r.Receipt {
		return fmt.Errorf("%w: C-05 output relation differs from the D-04 template", ErrPlanMismatch)
	}
	if !bytes.Equal(d.Prevout.PublicKeyScript, c.Script()) || !bytes.Equal(d.Spend.Leaf, c.WalletLeaf) || !bytes.Equal(d.Spend.Control, c.WalletControl) {
		return fmt.Errorf("%w: deposit script, leaf or control block", ErrPlanMismatch)
	}
	return nil
}

// mainRule is C-05's inclusion rule. An ordinary sweep must include an
// unreserved main and must not include a reserved one; a resolution sweep
// spends exactly the reserved main of its reservation.
func mainRule(plan *signing.Plan, s Snapshot) error {
	c := s.Custody
	main := plan.SweepMain()
	resolution := plan.SweepResolution()
	switch {
	case main == nil:
		if c.MainOutpoint != ([32]byte{}) && c.ActiveReservation == ([32]byte{}) {
			return fmt.Errorf("%w: an unreserved main must be included", ErrPlanMismatch)
		}
	case OutpointKey(main.Outpoint) != c.MainOutpoint:
		return fmt.Errorf("%w: main is not the current custody main", ErrPlanMismatch)
	case resolution != ([32]byte{}):
		if c.ActiveReservation != resolution {
			return fmt.Errorf("%w: resolution reservation is not active", ErrPlanMismatch)
		}
	case c.ActiveReservation != ([32]byte{}):
		return fmt.Errorf("%w: main is reserved for a redemption", ErrMainBusy)
	}
	return nil
}
