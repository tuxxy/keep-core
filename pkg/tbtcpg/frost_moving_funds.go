package tbtcpg

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ipfs/go-log/v2"

	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// runFrost commits and proposes the movement of a FROST wallet's funds to
// FROST targets (K-07, C-06) as base commits and proposes a legacy wallet's:
// only a MovingFunds wallet past base's wall-clock safety margin, with no
// pending redemption, incoming output or unswept deposit, and a positive
// main. If nothing is committed in the current period, the leader commits
// the targets the K-07 planner chooses from chain state with its own seat
// ("any seat" of the wallet); the proposal then names the committed targets
// and the fee the leader estimates within the committed cap (D19). With
// fewer eligible targets than required, no proposal is made and the next
// window retries (D6). Every member plans the movement from the recorded
// commitment and checks it against the proposal.
func (mft *MovingFundsTask) runFrost(
	taskLogger log.StandardLogger,
	request *tbtc.CoordinationProposalRequest,
) (tbtc.CoordinationProposal, bool, error) {
	walletPublicKeyHash := request.WalletPublicKeyHash

	frostChain, ok := mft.chain.(tbtc.FrostBridgeConnector)
	if !ok {
		return nil, false, fmt.Errorf("chain cannot read FROST movement")
	}
	outputKey, frost, err := frostChain.FrostTargetOutputKey(walletPublicKeyHash)
	if err != nil {
		return nil, false, fmt.Errorf("cannot resolve FROST wallet: [%w]", err)
	}
	if !frost {
		return nil, false, fmt.Errorf("wallet is not a FROST wallet")
	}

	ctx := context.Background()
	bridge, err := frostChain.FrostBridge(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("cannot bind the FROST Bridge: [%w]", err)
	}
	walletID := dkg.WalletID(outputKey)
	view := &movement.View{
		Chain:  bridge,
		Policy: movement.Policy{FinalityBlocks: tbtc.FrostFinalityBlocks},
	}

	source, err := view.Source(ctx, walletID)
	if err != nil {
		return nil, false, fmt.Errorf("cannot read FROST source wallet: [%w]", err)
	}
	if source.Wallet.State != movement.StateMovingFunds {
		taskLogger.Infof("source wallet not in MoveFunds state")
		return nil, false, nil
	}
	if source.Commitment.ProvenAt != 0 {
		taskLogger.Infof("source wallet's movement is already proven")
		return nil, false, nil
	}

	counters, err := bridge.Counters(ctx, source.Head.Number, walletID)
	if err != nil {
		return nil, false, fmt.Errorf("cannot read FROST movement counters: [%w]", err)
	}

	if err := validateFrostMovingFundsSafetyMargin(
		source.Lifecycle.MovingFundsRequestedAt,
		counters.InboundCommitments > 0,
		mft.chain,
	); err != nil {
		taskLogger.Infof("source wallet moving funds safety margin validation failed: [%v]", err)
		return nil, false, nil
	}

	if source.Custody.ActiveReservation != ([32]byte{}) {
		taskLogger.Infof("source wallet has pending redemptions")
		return nil, false, nil
	}
	if requests, ok := mft.chain.(tbtc.FrostRedemptionRequestChain); ok {
		filterStartBlock := uint64(0)
		if source.Head.Number > DepositSweepLookBackBlocks {
			filterStartBlock = source.Head.Number - DepositSweepLookBackBlocks
		}
		pending, err := requests.PendingFrostRedemptions(outputKey, filterStartBlock, 1)
		if err != nil {
			return nil, false, fmt.Errorf("cannot check FROST redemptions: [%w]", err)
		}
		if len(pending) > 0 {
			taskLogger.Infof("source wallet has pending redemptions")
			return nil, false, nil
		}
	}

	if counters.PendingIncoming > 0 {
		taskLogger.Infof("source wallet has pending moved funds sweep requests")
		return nil, false, nil
	}

	unswept, err := mft.hasUnsweptFrostDeposit(ctx, bridge, walletID, source.Head.Number)
	if err != nil {
		return nil, false, fmt.Errorf("cannot check wallet's unswept deposits: [%w]", err)
	}
	if unswept {
		taskLogger.Infof("source wallet has unswept deposits")
		return nil, false, nil
	}

	if source.MainValue == 0 {
		taskLogger.Infof("source wallet does not have a positive balance")
		return nil, false, nil
	}

	planner := movement.Planner{View: view, Wallet: dkg.Descriptor{OutputKey: outputKey}}
	targets := source.Commitment.Targets
	maxFee := source.Commitment.MaxFee
	if source.Log == nil {
		if _, err := planner.Targets(ctx, source); err != nil {
			if errors.Is(err, movement.ErrNoTarget) {
				return nil, false, fmt.Errorf("%w: [%v]", ErrNotEnoughTargetWallets, err)
			}
			return nil, false, fmt.Errorf("cannot find FROST target wallets: [%w]", err)
		}

		seat, err := frostExecutingSeat(request)
		if err != nil {
			return nil, false, err
		}

		taskLogger.Infof("commitment not submitted yet; committing FROST target wallets")

		commitments := movement.Commitments{Chain: bridge, Planner: planner, Seat: seat}
		if targets, err = commitments.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf(
				"error while submitting moving funds commitment: [%w]",
				err,
			)
		}
		maxFee = source.Parameters.MovingFundsTxMaxTotalFee
	}

	fee, err := EstimateMovingFundsFee(mft.btcChain, len(targets), maxFee)
	if err != nil {
		return nil, false, fmt.Errorf(
			"cannot estimate moving funds transaction fee: [%w]",
			err,
		)
	}

	taskLogger.Infof("moving funds transaction fee: [%d]", fee)

	return &tbtc.MovingFundsProposal{
		TargetWallets:    targets,
		MovingFundsTxFee: big.NewInt(fee),
	}, true, nil
}

// frostExecutingSeat is the executing operator's lowest seat in the wallet:
// the seat a FROST commitment is submitted with.
func frostExecutingSeat(request *tbtc.CoordinationProposalRequest) (uint16, error) {
	for i, operator := range request.WalletOperators {
		if operator == request.ExecutingOperator {
			return uint16(i + 1), nil
		}
	}
	return 0, ErrNoExecutingOperator
}

// validateFrostMovingFundsSafetyMargin is base's moving funds safety margin
// (tbtc.ValidateMovingFundsSafetyMargin) for a FROST wallet: 24 hours of
// wall-clock time after the moving-funds request, 14 days for a wallet that
// is itself a pending movement target, at most half the moving funds timeout.
func validateFrostMovingFundsSafetyMargin(
	movingFundsRequestedAt uint64,
	pendingTarget bool,
	chain Chain,
) error {
	safetyMargin := 24 * time.Hour
	if pendingTarget {
		safetyMargin = 14 * 24 * time.Hour
	}

	movingFundsParameters, err := chain.GetMovingFundsParameters()
	if err != nil {
		return fmt.Errorf("cannot get moving funds parameters: [%w]", err)
	}
	maxAllowedSafetyMargin := time.Duration(movingFundsParameters.Timeout/2) * time.Second
	if safetyMargin > maxAllowedSafetyMargin {
		safetyMargin = maxAllowedSafetyMargin
	}

	requestedAt := time.Unix(int64(movingFundsRequestedAt), 0)
	if time.Now().Before(requestedAt.Add(safetyMargin)) {
		return fmt.Errorf("safety margin in force")
	}

	return nil
}

// hasUnsweptFrostDeposit reports whether the wallet has a revealed deposit,
// within the deposit look-back period, that is unswept and whose funding
// output is still unspent: as base, a single one means the wallet should not
// move funds yet.
func (mft *MovingFundsTask) hasUnsweptFrostDeposit(
	ctx context.Context,
	bridge tbtc.FrostBridgeChain,
	walletID [32]byte,
	at uint64,
) (bool, error) {
	funding, ok := mft.btcChain.(redemption.ScriptUnspent)
	if !ok {
		return false, fmt.Errorf("bitcoin chain has no script query")
	}
	from := uint64(0)
	if at > DepositSweepLookBackBlocks {
		from = at - DepositSweepLookBackBlocks
	}
	reveals, err := bridge.Reveals(ctx, walletID, from, at)
	if err != nil {
		return false, err
	}
	for _, reveal := range reveals {
		record, err := bridge.Deposit(ctx, at, reveal.Outpoint)
		if errors.Is(err, deposit.ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if record.SweptAt != 0 {
			continue
		}
		unspent, err := frostFunding{chain: mft.btcChain, unspent: funding}.Unspent(
			ctx,
			fundingOutpoint(record),
		)
		if err != nil {
			return false, err
		}
		if unspent {
			return true, nil
		}
	}
	return false, nil
}
