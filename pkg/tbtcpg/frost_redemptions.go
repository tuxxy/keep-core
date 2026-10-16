package tbtcpg

import (
	"context"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// ProposeFrostRedemption builds a K-05 FROST redemption plan from the oldest
// redeemable requests of the planner's wallet, using chain state at the given
// finalized block. It never touches legacy ECDSA requests (D-08 separation).
// The plan still needs threshold attestation and C-04 admission before any
// worker may sign.
func ProposeFrostRedemption(ctx context.Context, planner redemption.Planner, from, at uint64, limit int) (*signing.Plan, []redemption.Request, error) {
	ids, err := planner.Pending(ctx, from, at, limit)
	if err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("no redeemable FROST requests")
	}
	return planner.Build(ctx, at, ids)
}
