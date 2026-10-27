package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	keepnet "github.com/keep-network/keep-core/pkg/net"
)

// The lowest selected seat proposes public transaction bytes, never worker
// material. Followers authenticate that seat and independently authorize every
// input/output and fee before retaining or signing the recipe.
type incomingProposal struct {
	Epoch, Anchor uint64
	Intent        []byte
}

func (*incomingProposal) Type() string               { return "frost-incoming-recipe-v1" }
func (r *incomingProposal) Marshal() ([]byte, error) { return json.Marshal(r) }
func (r *incomingProposal) Unmarshal(raw []byte) error {
	if len(raw) > 1<<20 {
		return errors.New("incoming recipe too large")
	}
	return json.Unmarshal(raw, r)
}
func validateIncomingCeiling(plan *signing.Plan, ceiling int64) error {
	snapshot := plan.Snapshot()
	if ceiling <= 0 || snapshot.Fee()/snapshot.VirtualSize() > ceiling || (snapshot.Fee()/snapshot.VirtualSize() == ceiling && snapshot.Fee()%snapshot.VirtualSize() != 0) {
		return errors.New("incoming recipe exceeds local fee ceiling")
	}
	return nil
}
func (n *Node) agreedIncoming(ctx context.Context, w *wallet, planner movement.Planner, anchor uint64, retained *signing.Plan) (*signing.Plan, error) {
	// Do not wait for a leader on an empty historical round. Every peer
	// first establishes that the shared anchor actually has incoming work.
	plan := retained
	var err error
	if plan == nil {
		plan, err = planner.IncomingSweep(ctx)
		if err != nil {
			return nil, err
		}
	}
	ch, err := n.network.BroadcastChannelFor(fmt.Sprintf("frost-incoming-recipe-v1-%x-%d", w.journal.Domain().ID(), anchor))
	if err != nil {
		return nil, err
	}
	if err = ch.SetFilter(w.membership.IsInGroup); err != nil {
		return nil, err
	}
	ch.SetUnmarshaler(func() keepnet.TaggedUnmarshaler { return &incomingProposal{} })
	received := make(chan *incomingProposal, 1)
	ch.Recv(ctx, func(message keepnet.Message) {
		recipe, ok := message.Payload().(*incomingProposal)
		if !ok || recipe.Epoch != w.record.Descriptor.Epoch || recipe.Anchor != anchor || !w.membership.IsValidMembership(1, message.SenderPublicKey()) {
			return
		}
		copy := *recipe
		copy.Intent = append([]byte(nil), recipe.Intent...)
		select {
		case received <- &copy:
		default:
		}
	})
	if w.request.LocalSeats[0] == 1 {
		// A peer may durably grant as soon as this packet is sent. Persist first,
		// and never replace an advertised recipe using only our local grant state.
		if w.plan == nil {
			w.plan = plan
			if err := w.retainIncoming(ctx); err != nil {
				w.plan = nil
				return nil, err
			}
		}

		recipe := &incomingProposal{Epoch: w.record.Descriptor.Epoch, Anchor: anchor, Intent: plan.IntentBytes()}
		if err = ch.Send(ctx, recipe); err != nil {
			return nil, err
		}
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = ch.Send(ctx, recipe)
				}
			}
		}()
		return plan, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case recipe := <-received:
		return n.acceptIncomingRecipe(w.record.Descriptor, recipe.Intent, func(plan *signing.Plan) error {
			_, err := w.mover.Authority.AuthorizeSweep(ctx, plan)
			return err
		})
	}
}

func (n *Node) acceptIncomingRecipe(descriptor dkg.Descriptor, intent []byte, authorize func(*signing.Plan) error) (*signing.Plan, error) {
	plan, err := signing.RestoreIncomingSweepPlan(descriptor, intent)
	if err != nil {
		return nil, err
	}
	if err = validateIncomingCeiling(plan, n.cfg.MaximumSatPerVByte); err != nil {
		return nil, err
	}
	if err = authorize(plan); err != nil {
		return nil, err
	}
	return plan, nil
}
