package tbtc_test

// The batch-6 node seam. Refs tuxxy/keep-core#26, #38, #39.
//
// DECISIONS.md "D5 revised (tux, 2026-11-03)": FROST duties are dispatched
// from base's own node files in pkg/tbtc (node.go, coordination.go, the DKG
// loop, proposals, registry restore at startup), on base coordination windows
// with base's leader, like the legacy ECDSA duties. keep-client start keeps
// refusing a FROST configuration (ValidateNode), so the tests enter through
// the same node initialisation cmd/start.go makes: tbtc.Initialize.
//
// tbtc.Initialize has no FROST input yet, and a test cannot call it with one
// without inventing its signature. As batch 4 did (frostNodeStartup), the
// tests reach it through frostNodeStart. While it is nil, every FROST case
// fails at its named "frostNodeStart is unbound" assertion.
//
// Binding (the implementer's, reviewed): from a new file in this package,
//
//	func init() { frostNodeStart = func(ctx context.Context, h frostNodeHost) error { ... } }
//
// calling tbtc.Initialize (or its FROST-configured form, the function
// cmd/start.go calls) with h's values, and nothing else: no scheduler, no
// goroutine of its own, no duty or executor call, no build tag. Everything
// the node does afterwards must come from tbtc.Initialize. This file and the
// tests stay unchanged.

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	commonethereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	"github.com/keep-network/keep-common/pkg/persistence"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/generator"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// frostNodeHost is what cmd/start.go builds before tbtc.Initialize, plus the
// FROST settings a node cannot read from chain. Nothing in it is test logic.
type frostNodeHost struct {
	// The tbtc.Initialize arguments, built as cmd/start.go builds them:
	// the TbtcChain from ethereum.Connect, the Bitcoin chain, the libp2p
	// provider with the base firewall, the tbtc keystore and work persistence
	// from storage.Initialize, the scheduler, the tbtcpg proposal generator,
	// the tbtc config and the Ethereum network.
	Chain           tbtc.Chain
	BitcoinChain    bitcoin.Chain
	Network         net.Provider
	KeyStore        persistence.ProtectedHandle
	Work            persistence.BasicHandle
	Scheduler       *generator.Scheduler
	Proposals       tbtc.CoordinationProposalGenerator
	Config          tbtc.Config
	EthereumNetwork commonethereum.Network
	// EthereumConfig is the client's Ethereum configuration (endpoint,
	// operator key file, contract addresses) that ethereum.Connect was given.
	EthereumConfig commonethereum.Config

	// FROST settings. All zero means FROST is not configured: the node must
	// then behave as base (shared rule 5).
	// FrostRegistry is the FROST wallet registry's address.
	FrostRegistry common.Address
	// Worker is the pinned SNOWFALL worker.
	Worker snowfallengine.WorkerConfig
	// Store is the FROST key store: its directory, storage ID, key and
	// external fence. It is the same on every restart of one operator.
	Store store.Config
}

// frostNodeStart starts a base node with FROST configured on h, as
// cmd/start.go's tbtc.Initialize call starts a legacy node, and returns once
// the node initialisation returned. See the file comment.
var frostNodeStart func(ctx context.Context, h frostNodeHost) error
