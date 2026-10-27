package node

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	keepnet "github.com/keep-network/keep-core/pkg/net"
)

// This bounded local availability schedule covers all three 2-of-3 cohorts,
// with one seat per operator. General D-02/D-11 scheduling stays OPEN; duplicate-
// seat and other topologies retain the existing full-roster schedule.
func localIncomingCohort(d dkg.Descriptor) func(uint64) []uint16 {
	if len(d.Members) != 3 || len(d.Operators) != 3 || d.Threshold != 2 {
		return nil
	}
	for i := range d.Members {
		if d.Members[i] == 0 {
			return nil
		}
		for j := 0; j < i; j++ {
			if d.Operators[i] == d.Operators[j] {
				return nil
			}
		}
	}
	return func(round uint64) []uint16 {
		cohorts := [][]uint16{{1, 2}, {1, 3}, {2, 3}}
		return append([]uint16(nil), cohorts[round%3]...)
	}
}

// Before choosing bytes, every selected seat advertises its retained recipe.
// An absent pin on the leader is never permission to disregard a follower's
// interrupted grant. Membership authenticates the original descriptor seats;
// all cohort/leader choices come from the canonical round, not a local peer list.
func (n *Node) cohortIncoming(ctx context.Context, w *wallet, cohort []uint16, anchor uint64, fresh *signing.Plan, ch keepnet.BroadcastChannel, received <-chan *incomingProposal) (*signing.Plan, bool, error) {
	local := w.request.LocalSeats[0]
	state := &incomingProposal{Epoch: w.record.Descriptor.Epoch, Anchor: anchor, Seat: local, Announcement: true}
	if w.plan != nil {
		state.Intent = w.plan.IntentBytes()
	}
	states := map[uint16][]byte{local: state.Intent}
	if err := ch.Send(ctx, state); err != nil {
		return nil, false, err
	}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = ch.Send(ctx, state)
			}
		}
	}()
	for len(states) < len(cohort) {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case message := <-received:
			if prior, ok := states[message.Seat]; ok && !bytes.Equal(prior, message.Intent) {
				return nil, false, errors.New("cohort seat changed its pinned recipe")
			}
			states[message.Seat] = message.Intent
		}
	}
	// Prefer a retained recipe in canonical seat order. A locally irreversible
	// different pin refuses below, rather than being silently overwritten.
	for _, seat := range cohort {
		if len(states[seat]) != 0 {
			plan, err := n.acceptIncomingRecipe(w.record.Descriptor, states[seat], func(p *signing.Plan) error { _, err := w.mover.Authority.AuthorizeSweep(ctx, p); return err })
			if err != nil {
				return nil, false, err
			}
			if w.plan != nil && w.plan.Commitment() != plan.Commitment() {
				safe, err := w.canAbandonIncoming(ctx)
				if err != nil {
					return nil, false, err
				}
				if !safe {
					return nil, false, errors.New("cohort recipe conflicts with released or advertised recipe")
				}
				if abandoned, err := w.abandonIncoming(ctx); err != nil || !abandoned {
					return nil, false, errors.New("cannot change cohort recipe")
				}
			}
			return plan, true, nil
		}
	}
	return fresh, false, nil
}
