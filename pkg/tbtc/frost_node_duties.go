package tbtc

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ipfs/go-log/v2"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/enforcement"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/net"
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
	syncFrom := uint64(0)
	if coordinationBlock > frostRedemptionLookBackBlocks {
		syncFrom = coordinationBlock - frostRedemptionLookBackBlocks
	}
	planner := deposit.Planner{
		Chain:   fn.bridge,
		Bitcoin: fn.bitcoin,
		Funding: signingBitcoin,
		// Base's EnsureWalletSyncedBetweenChains: no sweep while a sweep of
		// the wallet is on Bitcoin and unproven (review B1).
		Clock:              signingBitcoin,
		SyncFrom:           syncFrom,
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

// moveFunds signs and broadcasts the movement of the wallet's main to the
// targets of its recorded commitment (K-07, C-06) that the window's leader
// proposed, as a legacy wallet's members execute moving funds. Every member
// plans the movement from the recorded commitment with the proposal's exact
// fee (D19) and checks that it pays the proposed targets in order.
func (fw *frostWallet) moveFunds(
	ctx context.Context,
	actionLogger log.StandardLogger,
	proposal *MovingFundsProposal,
	startBlock uint64,
) error {
	fn := fw.node
	if proposal.MovingFundsTxFee == nil || proposal.MovingFundsTxFee.Sign() <= 0 ||
		!proposal.MovingFundsTxFee.IsInt64() {
		return fmt.Errorf("proposed moving funds transaction fee must be positive")
	}

	view := &movement.View{
		Chain:  fn.bridge,
		Policy: movement.Policy{FinalityBlocks: frostFinalityBlocks},
	}
	planner := movement.Planner{
		View:               view,
		Bitcoin:            fn.bitcoin,
		Wallet:             fw.descriptor,
		Genesis:            fn.genesis,
		FeeRate:            1,
		MaximumSatPerVByte: frostMaximumSatPerVByte,
		Fee:                proposal.MovingFundsTxFee.Int64(),
	}
	plan, err := planner.Movement(ctx)
	if err != nil {
		return fmt.Errorf("cannot plan movement: [%w]", err)
	}
	targets := plan.MovementTargets()
	if len(targets) != len(proposal.TargetWallets) {
		return fmt.Errorf("movement plan does not pay the proposed targets")
	}
	for i, target := range targets {
		if target.Label != proposal.TargetWallets[i] {
			return fmt.Errorf("movement plan does not pay the proposed targets")
		}
	}

	schedule := redemption.Schedule{
		FinalityBlocks: frostFinalityBlocks,
		RoundBlocks:    frostMovementRoundBlocks,
	}
	mover := fw.mover(view, schedule)

	return fw.signInRounds(
		ctx,
		actionLogger,
		"movement",
		plan,
		startBlock,
		schedule,
		mover.Eligible,
		func(ctx context.Context, round uint64) error {
			_, err := mover.Sign(ctx, plan, round)
			return err
		},
		func(ctx context.Context) error { return mover.Broadcast(ctx, plan) },
	)
}

// mover is the wallet's K-07 mover over the given view.
func (fw *frostWallet) mover(
	view *movement.View,
	schedule redemption.Schedule,
) *movement.Mover {
	return &movement.Mover{
		Chain:     fw.node.bridge,
		Authority: &movement.Authority{View: view},
		Lock:      &deposit.MainLock{Journal: fw.journal},
		Ledger:    redemption.Ledger{Journal: fw.journal},
		Schedule:  schedule,
		Wallet:    fw.descriptor,
		Base:      fw.signingConfig(),
	}
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

// heartbeat runs the wallet's heartbeat (K-08, C-09) that the window's
// leader proposed, as base's heartbeat action does: every member signs the
// heartbeat of the window, whose nonce is the coordination block, with the
// wallet's certified seats, and records which seats were authenticated
// present. A heartbeat with fewer than 70% of the wallet's seats present is
// a failure (base's activity rule; DECISIONS.md 2026-11-04); after
// heartbeatConsecutiveFailureThreshold consecutive failures the active
// members claim inactivity of the seats absent from all of them, with
// heartbeatFailed set.
func (fw *frostWallet) heartbeat(
	ctx context.Context,
	actionLogger log.StandardLogger,
	proposal *HeartbeatProposal,
	coordinationBlock uint64,
	startBlock uint64,
	expiryBlock uint64,
) error {
	fn := fw.node
	if err := fn.node.chain.ValidateHeartbeatProposal(fw.label, proposal); err != nil {
		return fmt.Errorf("heartbeat proposal is invalid: [%v]", err)
	}

	certified, err := fn.bridge.CertifiedSeats(ctx, fw.id)
	if err != nil {
		return fmt.Errorf("cannot read readiness-certified seats: [%w]", err)
	}
	base := fw.signingConfig()
	runner := &enforcement.Runner{
		Chain: fn.bridge,
		Schedule: redemption.Schedule{
			FinalityBlocks: frostFinalityBlocks,
			RoundBlocks:    coordinationFrequencyBlocks,
		},
		Wallet: fw.descriptor,
		Base: signing.HeartbeatConfig{
			Journal:      fw.journal,
			Worker:       fn.config.Worker,
			Selected:     certified,
			Transport:    base.Transport,
			LocalRegtest: base.LocalRegtest,
			HoldPresence: true,
		},
	}

	if err := fn.node.waitForBlockHeight(ctx, startBlock); err != nil {
		return err
	}
	round, err := runner.Current(ctx)
	if err != nil {
		return err
	}
	if round.Anchor != coordinationBlock {
		return fmt.Errorf(
			"heartbeat round [%d] is not the window's [%d]",
			round.Anchor,
			coordinationBlock,
		)
	}

	signingCtx, cancelSigning := fn.roundContext(
		ctx,
		startBlock+frostHeartbeatSigningBlocks,
	)
	outcome, err := runner.Run(signingCtx, round)
	cancelSigning()
	if outcome.Nonce == 0 {
		return fmt.Errorf("heartbeat recorded no outcome: [%v]", err)
	}
	actionLogger.Infof(
		"heartbeat [%d] signed [%v] with seats [%v] present",
		outcome.Nonce,
		outcome.Signed,
		outcome.Present,
	)

	claimer := &enforcement.Claimer{
		Chain:                fn.bridge,
		Runner:               runner,
		Records:              fw.journal,
		LocalSeats:           fw.localSeats,
		Key:                  fn.signer,
		Failures:             heartbeatConsecutiveFailureThreshold,
		Lookback:             frostHeartbeatLookBackWindows,
		MinimumActivePercent: frostHeartbeatMinimumActivePercent,
	}
	claim, _, err := claimer.Derive(ctx)
	if errors.Is(err, enforcement.ErrNoClaim) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot derive inactivity claim: [%w]", err)
	}

	if expiryBlock < heartbeatTimeoutSafetyMarginBlocks {
		return fmt.Errorf("invalid proposal expiry block")
	}
	claimCtx, cancelClaim := withCancelOnBlock(
		ctx,
		expiryBlock-heartbeatTimeoutSafetyMarginBlocks,
		fn.node.waitForBlockHeight,
	)
	defer cancelClaim()

	return fw.claimInactivity(claimCtx, actionLogger, claimer, claim)
}

// claimInactivity signs the claim with the local seats outside it, exchanges
// the signatures with the other members and submits the claim once the
// threshold is reached. Members submit in order of their lowest signing
// seat, a few blocks apart, so the Bridge usually sees one submission.
func (fw *frostWallet) claimInactivity(
	ctx context.Context,
	actionLogger log.StandardLogger,
	claimer *enforcement.Claimer,
	claim enforcement.Claim,
) error {
	fn := fw.node
	digest, err := enforcement.ClaimDigest(fn.bridge.ChainID(), fn.bridge.Bridge(), claim)
	if err != nil {
		return err
	}
	own, err := claimer.Sign(ctx, claim)
	if err != nil {
		return fmt.Errorf("cannot sign inactivity claim: [%w]", err)
	}
	if len(own) == 0 {
		return nil
	}

	actionLogger.Infof(
		"claiming inactivity of seats [%v] at nonce [%d]",
		claim.Inactive,
		claim.Nonce,
	)

	message := &frostInactivityMessage{}
	message.Digest = digest
	var signingSeats []uint16
	for seat := range own {
		signingSeats = append(signingSeats, seat)
	}
	sort.Slice(signingSeats, func(i, j int) bool { return signingSeats[i] < signingSeats[j] })
	for _, seat := range signingSeats {
		message.Seats = append(message.Seats, seat)
		message.Signatures = append(message.Signatures, own[seat])
	}
	fw.inactivity.add(&message.frostReservationMessage)
	if err := fw.inactivity.channel.Send(
		ctx,
		message,
		net.BackoffRetransmissionStrategy,
	); err != nil {
		return fmt.Errorf("cannot send inactivity claim attestation: [%w]", err)
	}

	blockCounter, err := fn.node.chain.BlockCounter()
	if err != nil {
		return err
	}
	submitBlock := uint64(0)
	for {
		nonce, err := fn.bridge.InactivityNonce(ctx, fw.id)
		if err == nil && nonce > claim.Nonce {
			actionLogger.Infof("inactivity claim at nonce [%d] submitted", claim.Nonce)
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("inactivity claim not submitted in time")
		}

		head, err := blockCounter.CurrentBlock()
		if err != nil {
			return err
		}
		collected := fw.inactivity.collected(digest)
		if len(collected) >= int(fw.descriptor.Threshold) {
			if submitBlock == 0 {
				submitBlock = head + 2*uint64(signingSeats[0]-1)
			}
			if head >= submitBlock {
				seats, signatures, err := redemption.Collect(fw.descriptor.Threshold, collected)
				if err == nil {
					err = claimer.Submit(ctx, claim, seats, signatures)
				}
				if err != nil {
					actionLogger.Warnf(
						"inactivity claim submission failed (another member may have submitted): [%v]",
						err,
					)
				}
				submitBlock = head + 2*uint64(len(fw.descriptor.Operators))
			}
		}

		if err := fn.node.waitForBlockHeight(ctx, head+1); err != nil {
			return err
		}
	}
}

// MedianTimePast implements deposit.Clock: Bitcoin's median-time-past
// (BIP113) from the node's Bitcoin chain, which a deposit's timestamp refund
// is checked against.
func (fsb *frostSigningBitcoin) MedianTimePast(ctx context.Context) (uint64, error) {
	return deposit.HeaderClock{Chain: fsb.chain}.MedianTimePast(ctx)
}
