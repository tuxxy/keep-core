package node

import (
	"context"
	"time"

	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// A peer may finish and prove while this worker still waits for its final
// packet. Finalized settlement ends that wait even when no further blocks
// arrive to advance the signing round. Reconciliation runs on the parent
// operation context after the worker has stopped; claims remain immutable.
func (n *Node) cancelSettledSigning(ctx context.Context, cancel context.CancelFunc, chain movement.Reader, plan *signing.Plan) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			head, err := chain.Head(ctx)
			if err != nil || head.Number < n.cfg.FinalityBlocks {
				continue
			}
			settled := true
			for _, input := range plan.IncomingOutputs() {
				record, err := chain.Incoming(ctx, head.Number-n.cfg.FinalityBlocks, movement.OutpointKey(input.Outpoint))
				if err != nil || record.State != movement.IncomingSwept || record.Target != plan.WalletID() {
					settled = false
					break
				}
			}
			if !settled {
				continue
			}
			hash, err := chain.HeaderHash(ctx, head.Number)
			if err != nil || hash != head.Hash {
				continue
			}
			cancel()
			return
		}
	}
}
