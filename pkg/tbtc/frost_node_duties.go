package tbtc

import (
	"context"
	"fmt"
	"sort"

	"github.com/ipfs/go-log/v2"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// sweepDeposits signs and broadcasts the sweep of the wallet's revealed
// deposits (K-06, C-05) that the window's leader proposed, as a legacy
// wallet's members execute a deposit sweep. Every member checks the
// proposal as base's WalletProposalValidator does (each deposit's age in
// chain time and its funding confirmations), then plans the sweep from
// chain state at the coordination block with the proposal's exact fee
// (D19): the planner refuses any deposit that is not reserved, final,
// unswept, before its refund margin and of one vault, and includes the
// wallet's main. While the main is reserved for a redemption the deposits
// wait (D13). Signing rounds start at the end of the window's active phase.
func (fw *frostWallet) sweepDeposits(
	ctx context.Context,
	actionLogger log.StandardLogger,
	proposal *DepositSweepProposal,
	coordinationBlock uint64,
	startBlock uint64,
) error {
	fn := fw.node
	if len(proposal.DepositsKeys) == 0 ||
		len(proposal.DepositsKeys) != len(proposal.DepositsRevealBlocks) {
		return fmt.Errorf("invalid FROST deposit sweep proposal")
	}
	if proposal.SweepTxFee == nil || proposal.SweepTxFee.Sign() <= 0 ||
		!proposal.SweepTxFee.IsInt64() {
		return fmt.Errorf("proposed sweep transaction fee must be positive")
	}

	custody, err := fn.bridge.Custody(ctx, coordinationBlock, fw.id)
	if err != nil {
		return err
	}
	if custody.ActiveReservation != ([32]byte{}) {
		return fmt.Errorf("the wallet's main is reserved for a redemption; deposits wait")
	}

	minAgeChain, ok := fn.node.chain.(interface {
		GetDepositMinAge() (uint32, error)
	})
	if !ok {
		return fmt.Errorf("chain cannot read the deposit minimum age")
	}
	minAge, err := minAgeChain.GetDepositMinAge()
	if err != nil {
		return fmt.Errorf("cannot read the deposit minimum age: [%w]", err)
	}
	timestamp, err := fn.bridge.Timestamp(ctx, coordinationBlock)
	if err != nil {
		return err
	}

	keys := make([][32]byte, len(proposal.DepositsKeys))
	for i, key := range proposal.DepositsKeys {
		outpoint := bitcoin.TransactionOutpoint{
			TransactionHash: key.FundingTxHash,
			OutputIndex:     key.FundingOutputIndex,
		}
		keys[i] = deposit.OutpointKey(outpoint)

		record, err := fn.bridge.Deposit(ctx, coordinationBlock, keys[i])
		if err != nil {
			return fmt.Errorf("cannot read FROST deposit [%d]: [%w]", i, err)
		}
		reveal, err := fn.bridge.Reveal(ctx, keys[i])
		if err != nil {
			return fmt.Errorf("cannot read FROST deposit [%d] reveal: [%w]", i, err)
		}
		if !proposal.DepositsRevealBlocks[i].IsUint64() ||
			proposal.DepositsRevealBlocks[i].Uint64() != reveal.Block {
			return fmt.Errorf("deposit [%d] reveal block does not match", i)
		}
		// Base: the deposit is older than DEPOSIT_MIN_AGE in chain time.
		if timestamp <= record.RevealedAt+uint64(minAge) {
			return fmt.Errorf("deposit [%d] is not old enough", i)
		}
		confirmations, err := fn.bitcoin.GetTransactionConfirmations(
			ctx,
			key.FundingTxHash,
		)
		if err != nil {
			return fmt.Errorf("cannot read deposit [%d] confirmations: [%w]", i, err)
		}
		if confirmations < DepositSweepRequiredFundingTxConfirmations {
			return fmt.Errorf("deposit [%d] funding has too few confirmations", i)
		}
	}

	lock := &deposit.MainLock{Journal: fw.journal}
	signingBitcoin := &frostSigningBitcoin{chain: fn.bitcoin, genesis: fn.genesis}
	planner := deposit.Planner{
		Chain:              fn.bridge,
		Bitcoin:            fn.bitcoin,
		Funding:            signingBitcoin,
		Lock:               lock,
		Wallet:             fw.descriptor,
		Genesis:            fn.genesis,
		Policy:             FrostDepositPolicy,
		FeeRate:            1,
		MaximumSatPerVByte: frostMaximumSatPerVByte,
		Fee:                proposal.SweepTxFee.Int64(),
	}
	plan, err := planner.Build(ctx, coordinationBlock, keys, [32]byte{})
	if err != nil {
		return fmt.Errorf("cannot plan deposit sweep: [%w]", err)
	}
	if custody.MainOutpoint != ([32]byte{}) && plan.SweepMain() == nil {
		return fmt.Errorf("deposit sweep plan does not include the wallet's main")
	}

	schedule := redemption.Schedule{
		FinalityBlocks: frostFinalityBlocks,
		RoundBlocks:    frostDepositSweepRoundBlocks,
	}
	sweeper := &deposit.Sweeper{
		Chain:   fn.bridge,
		Planner: planner,
		Authority: &deposit.Authority{View: &deposit.View{
			Chain:  fn.bridge,
			Clock:  signingBitcoin,
			Policy: FrostDepositPolicy,
		}},
		Lock:     lock,
		Ledger:   redemption.Ledger{Journal: fw.journal},
		Schedule: schedule,
		Base:     fw.signingConfig(),
	}

	return fw.signInRounds(
		ctx,
		actionLogger,
		"deposit sweep",
		plan,
		startBlock,
		schedule,
		func(context.Context, uint64) (bool, error) { return true, nil },
		func(ctx context.Context, round uint64) error {
			_, err := sweeper.Sign(ctx, plan, round)
			return err
		},
		func(ctx context.Context) error { return sweeper.Broadcast(ctx, plan) },
	)
}

// signInRounds signs a plan in rounds derived from block numbers, from the
// first round starting at or after startBlock, so every member joins the
// same round; a member outside a round's selection waits for the next one.
// Once a round signs, the transaction is broadcast. A failed round is
// retried in the next one unless the plan's inputs are already spent.
func (fw *frostWallet) signInRounds(
	ctx context.Context,
	actionLogger log.StandardLogger,
	name string,
	plan *signing.Plan,
	startBlock uint64,
	schedule redemption.Schedule,
	eligible func(ctx context.Context, round uint64) (bool, error),
	sign func(ctx context.Context, round uint64) error,
	broadcast func(ctx context.Context) error,
) error {
	fn := fw.node
	for round := (startBlock + schedule.RoundBlocks - 1) / schedule.RoundBlocks; ctx.Err() == nil; round++ {
		roundStart := round*schedule.RoundBlocks + schedule.FinalityBlocks
		if err := fn.node.waitForBlockHeight(ctx, roundStart); err != nil {
			return err
		}

		ok, err := eligible(ctx, round)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}

		roundCtx, cancelRound := fn.roundContext(ctx, roundStart+schedule.RoundBlocks)
		err = sign(roundCtx, round)
		cancelRound()
		if err != nil {
			actionLogger.Warnf("%s round [%d] failed: [%v]", name, round, err)
			if fw.planSpent(ctx, plan) {
				actionLogger.Infof("%s inputs are spent; nothing left to sign", name)
				return nil
			}
			continue
		}

		if err := broadcast(ctx); err != nil {
			return fmt.Errorf("cannot broadcast %s: [%w]", name, err)
		}

		actionLogger.Infof(
			"%s [%s] broadcast",
			name,
			bitcoin.Hash(plan.BitcoinTransactionHash()).Hex(bitcoin.ReversedByteOrder),
		)
		return nil
	}

	return ctx.Err()
}

// MedianTimePast implements deposit.Clock: Bitcoin's median-time-past
// (BIP113), the median of the last 11 block times, which a deposit's
// timestamp refund is checked against.
func (fsb *frostSigningBitcoin) MedianTimePast(context.Context) (uint64, error) {
	tip, err := fsb.chain.GetLatestBlockHeight()
	if err != nil {
		return 0, err
	}
	var times []uint32
	for height := tip; len(times) < 11; height-- {
		header, err := fsb.chain.GetBlockHeader(height)
		if err != nil {
			return 0, err
		}
		times = append(times, header.Time)
		if height == 0 {
			break
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return uint64(times[len(times)/2]), nil
}
