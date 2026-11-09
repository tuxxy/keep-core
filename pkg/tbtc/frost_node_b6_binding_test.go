package tbtc_test

// The batch-6 seam binding (frost_node_b6_seam_test.go): the node
// initialisation cmd/start.go calls, tbtc.Initialize, with the host's FROST
// settings in its tbtc configuration. Everything the node does afterwards
// comes from tbtc.Initialize.

import (
	"context"

	"github.com/keep-network/keep-core/pkg/tbtc"
)

func init() {
	frostNodeStart = func(ctx context.Context, h frostNodeHost) error {
		config := h.Config
		config.Frost = tbtc.FrostConfig{Registry: h.FrostRegistry, Worker: h.Worker, Store: h.Store}
		return tbtc.Initialize(ctx, h.Chain, h.BitcoinChain, h.Network, h.KeyStore, h.Work, h.Scheduler, h.Proposals, config, nil, nil, h.EthereumNetwork)
	}
}
