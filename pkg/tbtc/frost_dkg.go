package tbtc

import (
	"context"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
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
