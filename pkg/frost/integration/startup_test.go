package integration

// The FROST node-startup seam for the batch-4 restart tests.
// Refs tuxxy/keep-core#46, tuxxy/keep-core#52.
//
// Base keep-core restores keys at node startup, not by rerunning the DKG duty:
// dkgExecutor.registerSigner persists each signer right after key generation,
// and newWalletRegistry loads every stored signer when the node starts
// (walletStorage.loadSigners). The FROST counterpart does not exist yet: per
// D5 the FROST node runtime is test-only, and the K-09 frosttest runner calls
// K-03's "saved-key reload" owner entry point at startup. Until that entry
// point exists, frostNodeStartup is nil and the restart tests fail at their
// named "no FROST node startup" assertion.
//
// Binding: the owner entry point is bound from a new file in this package, as
//
//	func init() { frostNodeStartup = func(ctx context.Context, h startupHost) error { ... } }
//
// calling production code only. This file and the tests stay unchanged.

import (
	"context"
	"crypto/ecdsa"

	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// startupHost is what a restarted operator process holds when its node
// starts: its reopened real store, worker, chain adapter, operator keys and
// authenticated network. Nothing in it is test state.
type startupHost struct {
	Store      *store.Store
	Journal    *store.Scoped
	Domain     frost.Domain
	Worker     snowfallengine.WorkerConfig
	Chain      *eth.FrostChain
	Signer     *ecdsa.PrivateKey
	Operator   *operator.PrivateKey
	Network    net.Provider
	Membership *group.MembershipValidator
	Roster     []uint16
	LocalSeats []uint16
}

// frostNodeStartup starts a FROST node on h, the counterpart of base node
// startup. It returns once every local seat of the epoch's wallet is either
// restored (its durable key reloaded and, after the wallet is certified, the
// wallet saved ReadyUnfunded) or reported lost (no durable key). Following
// chain state, including taking part in readiness when the wallet is not yet
// certified, is its job; a restored seat attests readiness only after its
// completed key reloaded in a fresh run of h.Worker (KC-SF-V3 4.2, C-03). It
// returns before ctx's deadline in both cases. Afterwards the host's store
// and worker must sign for the certified seats.
var frostNodeStartup func(ctx context.Context, h startupHost) error
