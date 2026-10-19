package movement

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// Authority authorizes K-07 movement and incoming sweep plans
// (P-K07-COMMITTED-TARGET-ONLY, P-K07-INCOMING-ONCE). The executor calls it at
// start, after the durable claim right before every grant, after every
// signature and before broadcast; every call rereads one consistent snapshot.
type Authority struct {
	View *View
	// Directives are governance instructions for a contained wallet. A
	// quarantined target sweeps incoming only under a cancel directive for its
	// containment (V-08 reading 2); nil refuses.
	Directives redemption.Directives
}

var _ signing.SweepAuthority = (*Authority)(nil)

// AuthorizeSweep implements signing.SweepAuthority for movement plans.
func (a *Authority) AuthorizeSweep(ctx context.Context, plan *signing.Plan) (signing.ReservationReceipt, error) {
	switch {
	case plan.IsMovement():
		return a.movement(ctx, plan)
	case plan.IsIncomingSweep():
		return a.incoming(ctx, plan)
	}
	return signing.ReservationReceipt{}, errors.New("not a movement plan")
}

// movement requires the recorded, finalized, unproven commitment of the
// plan's period to name exactly the plan's main and targets. A target's key
// comes from its immutable descriptor, checked against the committed
// descriptor hash; its current state is never consulted. The fee is checked
// against the cap recorded in the commitment, as the proof and the C-08
// defense check it, so a later parameter change cannot pass an unprovable fee.
func (a *Authority) movement(ctx context.Context, plan *signing.Plan) (signing.ReservationReceipt, error) {
	s, err := a.View.Source(ctx, plan.WalletID())
	if err != nil {
		return signing.ReservationReceipt{}, err
	}
	if s.Wallet.State != StateMovingFunds && s.Wallet.State != StateTerminated {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: source state %d", ErrWalletState, s.Wallet.State)
	}
	c := s.Commitment
	if s.Lifecycle.Period != plan.MovementPeriod() || s.Log == nil {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: no commitment in period %d", ErrPlanMismatch, plan.MovementPeriod())
	}
	if c.ProvenAt != 0 {
		return signing.ReservationReceipt{}, ErrSettled
	}
	if !a.View.Final(s.Head, s.Log.Log) {
		return signing.ReservationReceipt{}, ErrNotFinal
	}
	main := plan.MovementMain()
	if main == nil || OutpointKey(main.Outpoint) != c.MainOutpoint || s.Custody.MainOutpoint != c.MainOutpoint || s.Custody.ActiveReservation != ([32]byte{}) || uint64(main.Value) != s.MainValue {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: main", ErrPlanMismatch)
	}
	targets := plan.MovementTargets()
	if len(targets) != len(c.Targets) || len(s.Targets) != len(c.Targets) {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: target count", ErrPlanMismatch)
	}
	for i, t := range targets {
		w := s.Targets[i]
		if t.Label != c.Targets[i] || t.DescriptorHash != c.DescriptorHashes[i] || w.DescriptorHash != c.DescriptorHashes[i] || w.Descriptor.OutputKey != t.OutputKey {
			return signing.ReservationReceipt{}, fmt.Errorf("%w: target %d", ErrPlanMismatch, i)
		}
	}
	if fee := plan.Snapshot().Fee(); fee <= 0 || uint64(fee) > c.MaxFee {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: fee above the committed cap", ErrPlanMismatch)
	}
	return signing.ReservationReceipt{Block: s.Log.Block, Hash: s.Log.BlockHash}, nil
}

// incoming requires every incoming input to be recorded for this wallet at a
// finalized canonical block, pending or escalated, with the planned value; the
// main to be the unreserved custody main (or absent); and the fee within the
// moved funds sweep cap. The plan lists inputs in key order and the view
// returns records in log order, so records are matched by outpoint key.
func (a *Authority) incoming(ctx context.Context, plan *signing.Plan) (signing.ReservationReceipt, error) {
	inputs := plan.IncomingOutputs()
	keys := make([][32]byte, len(inputs))
	for i, in := range inputs {
		keys[i] = OutpointKey(in.Outpoint)
	}
	wallet := plan.WalletID()
	t, err := a.View.Target(ctx, wallet, keys)
	if err != nil {
		return signing.ReservationReceipt{}, err
	}
	switch t.Wallet.State {
	case StateReadyUnfunded, StateLive, StateMovingFunds, StateClosing, StateTerminated:
	default:
		return signing.ReservationReceipt{}, fmt.Errorf("%w: target state %d", ErrWalletState, t.Wallet.State)
	}
	if t.Wallet.Quarantined {
		if a.Directives == nil {
			return signing.ReservationReceipt{}, fmt.Errorf("%w: quarantined target needs a directive", redemption.ErrContained)
		}
		// The directive is keyed by the wallet ID for a contained wallet.
		d, ok, err := a.Directives.Directive(ctx, wallet)
		if err != nil || !ok || d.Action != redemption.DirectiveCancel {
			return signing.ReservationReceipt{}, fmt.Errorf("%w: quarantined target needs a directive", redemption.ErrContained)
		}
	}
	recorded := make(map[[32]byte]IncomingOutput, len(t.Incoming))
	for _, in := range t.Incoming {
		recorded[in.Log.Outpoint] = in
	}
	var anchor Log
	capFee := t.Parameters.MovedFundsSweepTxMaxTotalFee
	for i, key := range keys {
		in, ok := recorded[key]
		if !ok {
			return signing.ReservationReceipt{}, fmt.Errorf("%w: incoming %d", ErrPlanMismatch, i)
		}
		// A repeated input finds no record the second time.
		delete(recorded, key)
		if in.Record.Target != wallet || (in.Record.State != IncomingPending && in.Record.State != IncomingEscalated) {
			if in.Record.State == IncomingSwept {
				return signing.ReservationReceipt{}, ErrSettled
			}
			return signing.ReservationReceipt{}, fmt.Errorf("%w: incoming %d", ErrPlanMismatch, i)
		}
		if !a.View.Final(t.Head, in.Log.Log) {
			return signing.ReservationReceipt{}, ErrNotFinal
		}
		if uint64(inputs[i].Value) != in.Record.Value {
			return signing.ReservationReceipt{}, fmt.Errorf("%w: incoming %d value", ErrPlanMismatch, i)
		}
		if in.Log.Block > anchor.Block {
			anchor = in.Log.Log
		}
		if in.Record.SweepFeeCap < capFee {
			capFee = in.Record.SweepFeeCap
		}
	}
	if t.Custody.ActiveReservation != ([32]byte{}) {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: target main reserved", ErrPlanMismatch)
	}
	main := plan.MovementMain()
	if (main == nil) != (t.Custody.MainOutpoint == ([32]byte{})) || (main != nil && OutpointKey(main.Outpoint) != t.Custody.MainOutpoint) {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: target main", ErrPlanMismatch)
	}
	if fee := plan.Snapshot().Fee(); fee <= 0 || uint64(fee) > capFee {
		return signing.ReservationReceipt{}, fmt.Errorf("%w: fee above the moved funds sweep cap or a recorded cap", ErrPlanMismatch)
	}
	return signing.ReservationReceipt{Block: anchor.Block, Hash: anchor.BlockHash}, nil
}
