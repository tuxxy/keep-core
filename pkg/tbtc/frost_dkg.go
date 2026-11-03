package tbtc

import (
	"context"
	"crypto/ecdsa"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// frostDkgExecutor is the explicit, inactive FROST node path. It uses its own
// validator parameters, publication format and watchers, never ECDSA globals.
type frostDkgExecutor struct{ execution *dkg.Executor }

// ExecuteFrostDKG is for an explicitly configured local FROST deployment. The
// legacy node dispatch does not call it and no production flag enables it.
func ExecuteFrostDKG(ctx context.Context, execution *dkg.Executor) (dkg.WalletRecord, error) {
	executor := frostDkgExecutor{execution: execution}
	return executor.execution.Run(ctx)
}

// FrostNodeStartup is what a restarted FROST operator holds for one epoch:
// its reopened store scope, pinned worker, registry adapter, operator key and
// authenticated network.
type FrostNodeStartup struct {
	Chain      dkg.Chain
	Journal    dkg.Journal
	Worker     snowfallengine.WorkerConfig
	Network    net.Provider
	Membership *group.MembershipValidator
	Signer     *ecdsa.PrivateKey
}

// StartFrostNode restores the operator's seats of the journal's epoch at node
// startup, as base newWalletRegistry loads every stored signer
// (walletStorage.loadSigners) instead of rerunning the DKG duty. It returns
// once every local seat is restored (key reloaded in a fresh worker, wallet
// ReadyUnfunded after certification) or reported lost. Like ExecuteFrostDKG it
// is for an explicitly configured local FROST deployment; the legacy node
// dispatch does not call it.
func StartFrostNode(ctx context.Context, s FrostNodeStartup) (frost.DKGStatus, dkg.WalletRecord, error) {
	engine, err := snowfallengine.NewGuarded(s.Worker, s.Journal)
	if err != nil {
		return frost.DKGStatus{}, dkg.WalletRecord{}, err
	}
	domain := s.Journal.Domain()
	bus, err := dkg.NewNetworkBus(ctx, s.Network, s.Membership, domain.ID(), domain.Epoch)
	if err != nil {
		return frost.DKGStatus{}, dkg.WalletRecord{}, err
	}
	executor := frostDkgExecutor{execution: &dkg.Executor{Chain: s.Chain, Engine: engine, Journal: s.Journal, Bus: bus, Signer: s.Signer, PollInterval: 50 * time.Millisecond}}
	return executor.execution.Restore(ctx)
}
