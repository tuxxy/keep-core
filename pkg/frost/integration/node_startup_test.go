package integration

// Binds the batch-4 node-startup seam (startup_test.go) to the production
// FROST node startup, tbtc.StartFrostNode. Wiring only: every decision is in
// production code. Refs tuxxy/keep-core#46, tuxxy/keep-core#52.

import (
	"context"

	"github.com/keep-network/keep-core/pkg/tbtc"
)

func init() {
	frostNodeStartup = func(ctx context.Context, h startupHost) error {
		_, _, err := tbtc.StartFrostNode(ctx, tbtc.FrostNodeStartup{Chain: h.Chain, Journal: h.Journal, Worker: h.Worker, Network: h.Network, Membership: h.Membership, Signer: h.Signer})
		return err
	}
}
