package spv

import (
	"context"
	"errors"
	"math/big"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

type proofDifficultySettings interface{ TxProofDifficultyFactor() (*big.Int, error) }
type proofEpochDifficulties interface {
	GetCurrentAndPrevEpochDifficulty() (*big.Int, *big.Int, error)
}

// FrostProofDepth derives the header count from the Bridge factor and the
// relay's current/previous difficulties, including retargets and DIFF1 spans.
type FrostProofDepth func(context.Context, bitcoin.Hash) (uint, error)

var ErrFrostProofNotReady = errors.New("FROST proof has insufficient work or is outside relay range")

// DifficultyProofDepth uses exactly the legacy maintainer's Bridge-matching
// accumulation algorithm. minimum is an additional caller floor, never a
// replacement for the contract's accumulated-work requirement.
func DifficultyProofDepth(btc bitcoin.Chain, settings proofDifficultySettings, epochs proofEpochDifficulties, minimum uint) FrostProofDepth {
	return func(ctx context.Context, hash bitcoin.Hash) (uint, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		within, available, required, err := getProofInfo(hash, btc, settings, epochs)
		if err != nil {
			return 0, err
		}
		if required < minimum {
			required = minimum
		}
		if !within || required == 0 || available < required {
			return 0, ErrFrostProofNotReady
		}
		return required, nil
	}
}
func frostProofDepth(ctx context.Context, hash bitcoin.Hash, localCount uint, derive FrostProofDepth) (uint, error) {
	if derive != nil {
		depth, err := derive(ctx, hash)
		if err == nil && depth == 0 {
			return 0, ErrFrostProofNotReady
		}
		return depth, err
	}
	// Explicit component/regtest callers retain a fixed count. The executable
	// FROST CLI always installs DifficultyProofDepth; no default production
	// policy may be inferred from this local integration compatibility path.
	if localCount == 0 {
		return 0, errors.New("confirmations must be positive")
	}
	return localCount, nil
}
