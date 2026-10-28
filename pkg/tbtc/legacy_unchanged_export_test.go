package tbtc

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

// LegacySignBatchFn signs a batch of sighashes the way a wallet's signing
// executor does. The legacy-unchanged suite passes a FIXTURE signer that holds
// the wallet's test key; no threshold signing takes place.
type LegacySignBatchFn func(
	ctx context.Context,
	messages []*big.Int,
	startBlock uint64,
) ([]*tecdsa.Signature, error)

func (fn LegacySignBatchFn) signBatch(
	ctx context.Context,
	messages []*big.Int,
	startBlock uint64,
) ([]*tecdsa.Signature, error) {
	return fn(ctx, messages, startBlock)
}

// ExecuteMovingFundsActionForTest runs one moving-funds action exactly as a
// wallet member runs it after accepting a coordination proposal: proposal
// validation, commitment confirmation, assembly, signing and broadcast. Only
// the broadcast check delay is shortened. Exposed for the external
// legacy-unchanged suite (legacy_unchanged_test.go); test-only.
func ExecuteMovingFundsActionForTest(
	hostChain Chain,
	btcChain bitcoin.Chain,
	walletPublicKey *ecdsa.PublicKey,
	walletOperators []chain.Address,
	sign LegacySignBatchFn,
	proposal *MovingFundsProposal,
	proposalProcessingStartBlock uint64,
	waitForBlock func(context.Context, uint64) error,
) error {
	action := newMovingFundsAction(
		logger.With(),
		hostChain,
		btcChain,
		wallet{
			publicKey:             walletPublicKey,
			signingGroupOperators: walletOperators,
		},
		sign,
		proposal,
		proposalProcessingStartBlock,
		proposalProcessingStartBlock+proposal.ValidityBlocks(),
		waitForBlock,
		newTransactionMonitor(btcChain),
	)
	action.broadcastCheckDelay = 10 * time.Millisecond

	return action.execute()
}
