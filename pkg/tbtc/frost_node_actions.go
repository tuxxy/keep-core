package tbtc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ipfs/go-log/v2"
	"go.uber.org/zap"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// executeFrostCoordinationProcedure executes the base coordination procedure
// for a FROST wallet in the given window: base's leader proposes from chain
// state, the followers validate.
func executeFrostCoordinationProcedure(
	node *node,
	window *coordinationWindow,
	wallet *frostWallet,
) (*coordinationResult, bool) {
	procedureLogger := logger.With(
		zap.Uint64("coordinationBlock", window.coordinationBlock),
		zap.String("frostWallet", fmt.Sprintf("0x%x", wallet.id)),
	)

	procedureLogger.Infof("starting coordination procedure")

	result, err := wallet.coordinationExecutor.coordinate(window)
	if err != nil {
		procedureLogger.Errorf("coordination procedure failed: [%v]", err)
		return nil, false
	}

	procedureLogger.Infof(
		"coordination procedure finished successfully with result [%s]",
		result,
	)

	result.frostWallet = wallet

	return result, true
}

// processFrostCoordinationResult processes a FROST wallet's coordination
// result, as processCoordinationResult does a legacy wallet's. One action
// runs at a time per wallet; other wallets are never blocked by it.
func processFrostCoordinationResult(
	node *node,
	result *coordinationResult,
	startBlock uint64,
	expiryBlock uint64,
) {
	wallet := result.frostWallet
	actionLogger := logger.With(
		zap.String("frostWallet", fmt.Sprintf("0x%x", wallet.id)),
		zap.String("action", result.proposal.ActionType().String()),
		zap.Uint64("coordinationBlock", result.window.coordinationBlock),
	)

	if !wallet.actionMutex.TryLock() {
		actionLogger.Warnf("FROST wallet is busy with another action")
		return
	}
	defer wallet.actionMutex.Unlock()

	ctx, cancel := withCancelOnBlock(
		context.Background(),
		expiryBlock,
		node.waitForBlockHeight,
	)
	defer cancel()

	var err error
	switch proposal := result.proposal.(type) {
	case *MovedFundsSweepProposal:
		err = wallet.sweepIncoming(ctx, actionLogger, proposal, startBlock)
	case *RedemptionProposal:
		err = wallet.redeem(
			ctx,
			actionLogger,
			proposal,
			result.window.coordinationBlock,
		)
	default:
		err = fmt.Errorf("no FROST handler for action")
	}
	if err != nil {
		actionLogger.Errorf("FROST wallet action failed: [%v]", err)
		return
	}

	actionLogger.Infof("FROST wallet action completed")
}

// sweepIncoming signs and broadcasts the sweep of the wallet's pending
// incoming output (K-07, C-06) that the window's leader proposed. Every
// member plans it from chain state with the proposal's exact fee (D19), so
// all of them sign the same transaction. Signing rounds start at the end of
// the window's active phase, as a legacy wallet's action starts.
func (fw *frostWallet) sweepIncoming(
	ctx context.Context,
	actionLogger log.StandardLogger,
	proposal *MovedFundsSweepProposal,
	startBlock uint64,
) error {
	fn := fw.node
	if err := ValidateMovedFundsSweepProposal(
		actionLogger,
		fw.label,
		proposal,
		fn.node.chain,
	); err != nil {
		return err
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
		Fee:                proposal.SweepTxFee.Int64(),
	}
	plan, err := planner.IncomingSweep(ctx)
	if err != nil {
		return fmt.Errorf("cannot plan incoming sweep: [%w]", err)
	}
	if !spendsOutpoint(plan, bitcoin.TransactionOutpoint{
		TransactionHash: bitcoin.Hash(proposal.MovingFundsTxHash),
		OutputIndex:     proposal.MovingFundsTxOutputIndex,
	}) {
		return fmt.Errorf("incoming sweep plan does not spend the proposed output")
	}

	schedule := redemption.Schedule{
		FinalityBlocks: frostFinalityBlocks,
		RoundBlocks:    frostMovementRoundBlocks,
	}
	mover := &movement.Mover{
		Chain:     fn.bridge,
		Authority: &movement.Authority{View: view},
		Lock:      &deposit.MainLock{Journal: fw.journal},
		Ledger:    redemption.Ledger{Journal: fw.journal},
		Schedule:  schedule,
		Wallet:    fw.descriptor,
		Base:      fw.signingConfig(),
	}

	// Rounds are derived from block numbers, so every member joins the same
	// round; a member outside a round's selection waits for the next one.
	for round := (startBlock + schedule.RoundBlocks - 1) / schedule.RoundBlocks; ctx.Err() == nil; round++ {
		roundStart := round*schedule.RoundBlocks + schedule.FinalityBlocks
		if err := fn.node.waitForBlockHeight(ctx, roundStart); err != nil {
			return err
		}

		eligible, err := mover.Eligible(ctx, round)
		if err != nil {
			return err
		}
		if !eligible {
			continue
		}

		roundCtx, cancelRound := fn.roundContext(ctx, roundStart+schedule.RoundBlocks)
		_, err = mover.Sign(roundCtx, plan, round)
		cancelRound()
		if err != nil {
			actionLogger.Warnf("incoming sweep round [%d] failed: [%v]", round, err)
			if fw.planSpent(ctx, plan) {
				actionLogger.Infof("incoming sweep inputs are spent; nothing left to sign")
				return nil
			}
			continue
		}

		if err := mover.Broadcast(ctx, plan); err != nil {
			return fmt.Errorf("cannot broadcast incoming sweep: [%w]", err)
		}

		actionLogger.Infof(
			"incoming sweep [%s] broadcast",
			bitcoin.Hash(plan.BitcoinTransactionHash()).Hex(bitcoin.ReversedByteOrder),
		)
		return nil
	}

	return ctx.Err()
}

// roundContext bounds one signing round: it ends at the round's end block
// and, since a FROST transport needs a deadline, after frostRoundTimeout at
// the latest.
func (fn *frostNode) roundContext(
	ctx context.Context,
	endBlock uint64,
) (context.Context, context.CancelFunc) {
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, frostRoundTimeout)
	blockCtx, cancelBlock := withCancelOnBlock(
		timeoutCtx,
		endBlock,
		fn.node.waitForBlockHeight,
	)
	return blockCtx, func() {
		cancelBlock()
		cancelTimeout()
	}
}

// planSpent reports whether an input of the plan is already spent, in a
// block or the mempool: another member broadcast the transaction, or the
// input went elsewhere. Signing the plan again cannot help then.
func (fw *frostWallet) planSpent(ctx context.Context, plan *signing.Plan) bool {
	view := &frostSigningBitcoin{chain: fw.node.bitcoin}
	for _, previous := range plan.Snapshot().PreviousOutputs() {
		unspent, err := view.Unspent(ctx, previous.Outpoint)
		if err == nil && !unspent {
			return true
		}
	}
	return false
}

func spendsOutpoint(plan *signing.Plan, outpoint bitcoin.TransactionOutpoint) bool {
	for _, previous := range plan.Snapshot().PreviousOutputs() {
		if previous.Outpoint == outpoint {
			return true
		}
	}
	return false
}

// redeem reserves (C-04) and pays the pending FROST redemption requests the
// window's leader proposed (K-05). Every member builds the plan from chain
// state at the coordination block, checks it pays exactly the proposed
// requests, attests it with its certified seats, and exchanges attestations
// with the other members; once the Bridge admits the reservation, the
// members sign the payment in rounds derived from the admission block.
func (fw *frostWallet) redeem(
	ctx context.Context,
	actionLogger log.StandardLogger,
	proposal *RedemptionProposal,
	coordinationBlock uint64,
) error {
	fn := fw.node
	planner := redemption.Planner{
		Chain:   fn.bridge,
		Bitcoin: fn.bitcoin,
		Wallet:  fw.descriptor,
		Genesis: fn.genesis,
		Fees: redemption.FeePolicy{
			FeeRate:            1,
			MaximumSatPerVByte: frostMaximumSatPerVByte,
			Minimum:            frostMinimumFee,
		},
		Fee:            proposal.RedemptionTxFee.Int64(),
		DeadlineMargin: frostRedemptionDeadlineMargin,
	}

	from := uint64(0)
	if coordinationBlock > frostRedemptionLookBackBlocks {
		from = coordinationBlock - frostRedemptionLookBackBlocks
	}
	ids, err := planner.Pending(ctx, from, coordinationBlock, len(proposal.RedeemersOutputScripts))
	if err != nil {
		return fmt.Errorf("cannot find pending FROST redemptions: [%w]", err)
	}
	if len(ids) == 0 {
		return fmt.Errorf("no pending FROST redemptions")
	}
	plan, _, err := planner.Build(ctx, coordinationBlock, ids)
	if err != nil {
		return fmt.Errorf("cannot plan FROST redemption: [%w]", err)
	}
	if err := validateFrostRedemptionPlan(plan, proposal); err != nil {
		return err
	}

	configuration, err := fn.bridge.Configuration(ctx, coordinationBlock)
	if err != nil {
		return err
	}
	// Every member derives the same certificate deadline from the window.
	validUntil := coordinationBlock + configuration.MaximumCertificateBlocks - 1
	certificate, err := redemption.CertificateFor(plan, validUntil)
	if err != nil {
		return err
	}
	chainID, bridge := fn.bridge.ChainID(), fn.bridge.Bridge()
	reservation := redemption.ReservationID(chainID, bridge, certificate)
	digest := redemption.ReservationDigest(chainID, bridge, certificate)

	if _, err := fn.bridge.Admission(ctx, reservation); err != nil {
		if err := fw.reserve(ctx, actionLogger, planner, plan, certificate, reservation, digest, validUntil); err != nil {
			return err
		}
	}

	// Once admitted, the payment is resolved on chain, not within the
	// proposal's validity.
	payCtx := context.WithoutCancel(ctx)
	admission, err := fn.bridge.Admission(payCtx, reservation)
	if err != nil {
		return err
	}

	actionLogger.Infof(
		"FROST redemption reservation [0x%x] admitted at block [%d]",
		reservation,
		admission.Block,
	)

	return fw.pay(payCtx, actionLogger, plan, reservation, admission)
}

// validateFrostRedemptionPlan checks that the plan pays exactly the requests
// the leader proposed, in order, with the proposed fee.
func validateFrostRedemptionPlan(
	plan *signing.Plan,
	proposal *RedemptionProposal,
) error {
	requests := plan.Requests()
	if len(requests) != len(proposal.RedeemersOutputScripts) {
		return fmt.Errorf("FROST redemption plan does not match the proposal")
	}
	for i, request := range requests {
		if !bytes.Equal(request.Script, proposal.RedeemersOutputScripts[i]) {
			return fmt.Errorf("FROST redemption plan does not match the proposal")
		}
	}
	if plan.Snapshot().Fee() != proposal.RedemptionTxFee.Int64() {
		return fmt.Errorf("FROST redemption plan fee does not match the proposal")
	}
	return nil
}

// reserve attests the plan with the local seats, collects the other members'
// attestations and submits the reservation once the threshold is reached.
// Members submit in order of their lowest seat, a few blocks apart, so the
// Bridge usually sees one submission.
func (fw *frostWallet) reserve(
	ctx context.Context,
	actionLogger log.StandardLogger,
	planner redemption.Planner,
	plan *signing.Plan,
	certificate redemption.Certificate,
	reservation [32]byte,
	digest [32]byte,
	validUntil uint64,
) error {
	fn := fw.node
	attester := &redemption.Attester{
		Planner: planner,
		Journal: fw.journal,
		Ledger:  redemption.Ledger{Journal: fw.journal},
		Key:     fn.signer,
		Seats:   fw.localSeats,
		Reload: redemption.FreshWorkerReload(
			fn.config.Worker,
			fw.journal,
			func(ctx context.Context) (uint64, error) {
				head, err := fn.bridge.Head(ctx)
				return head.Number, err
			},
		),
		MainLock: &deposit.MainLock{Journal: fw.journal},
	}
	own, err := attester.Attest(ctx, plan, validUntil)
	if err != nil {
		return fmt.Errorf("cannot attest FROST redemption: [%w]", err)
	}

	message := &frostReservationMessage{Digest: digest}
	for seat, signature := range own {
		message.Seats = append(message.Seats, seat)
		message.Signatures = append(message.Signatures, signature)
	}
	fw.reservations.add(message)

	attestCtx, cancel := withCancelOnBlock(ctx, validUntil, fn.node.waitForBlockHeight)
	defer cancel()
	if err := fw.reservations.channel.Send(
		attestCtx,
		message,
		net.BackoffRetransmissionStrategy,
	); err != nil {
		return fmt.Errorf("cannot send FROST attestation: [%w]", err)
	}

	blockCounter, err := fn.node.chain.BlockCounter()
	if err != nil {
		return err
	}
	threshold := fw.descriptor.Threshold
	submitBlock := uint64(0)
	for {
		if _, err := fn.bridge.Admission(context.WithoutCancel(ctx), reservation); err == nil {
			return nil
		}
		if attestCtx.Err() != nil {
			return fmt.Errorf("FROST reservation not admitted by block [%d]", validUntil)
		}

		collected := fw.reservations.collected(digest)
		head, err := blockCounter.CurrentBlock()
		if err != nil {
			return err
		}
		if len(collected) >= int(threshold) {
			if submitBlock == 0 {
				submitBlock = head + 2*uint64(minSeat(fw.localSeats)-1)
			}
			if head >= submitBlock {
				if err := fw.submitReservation(attestCtx, plan, certificate, collected); err != nil {
					actionLogger.Warnf(
						"FROST reservation submission failed (another member may have reserved): [%v]",
						err,
					)
				}
				submitBlock = head + 2*uint64(len(fw.descriptor.Operators))
			}
		}

		if err := fn.node.waitForBlockHeight(attestCtx, head+1); err != nil {
			return err
		}
	}
}

func minSeat(seats []uint16) uint16 {
	minimum := seats[0]
	for _, seat := range seats {
		if seat < minimum {
			minimum = seat
		}
	}
	return minimum
}

// frostTransactionInfo is the Bridge's BitcoinTx.Info of a transaction.
func frostTransactionInfo(transaction *bitcoin.Transaction) redemption.BitcoinInfo {
	return redemption.BitcoinInfo{
		Version:      transaction.SerializeVersion(),
		InputVector:  transaction.SerializeInputs(),
		OutputVector: transaction.SerializeOutputs(),
		Locktime:     transaction.SerializeLocktime(),
	}
}

func (fw *frostWallet) submitReservation(
	ctx context.Context,
	plan *signing.Plan,
	certificate redemption.Certificate,
	collected map[uint16][]byte,
) error {
	seats, signatures, err := redemption.Collect(fw.descriptor.Threshold, collected)
	if err != nil {
		return err
	}
	proposal := redemption.ReservationProposal{
		Certificate: certificate,
		Transaction: frostTransactionInfo(plan.Snapshot().Unsigned()),
		Seats:       seats,
		Signatures:  signatures,
	}
	for _, previous := range plan.Snapshot().PreviousOutputs() {
		proposal.Inputs = append(proposal.Inputs, redemption.UTXO{
			TransactionHash: previous.Outpoint.TransactionHash,
			OutputIndex:     previous.Outpoint.OutputIndex,
			Value:           uint64(previous.Value),
		})
	}
	for _, request := range plan.Requests() {
		proposal.Requests = append(proposal.Requests, redemption.ProposalRequest{
			ID:     request.ID,
			Value:  uint64(request.Value),
			Script: request.Script,
		})
	}
	return fw.node.bridge.Reserve(ctx, proposal)
}

// pay signs the admitted reservation's payment and broadcasts it. Rounds are
// derived from the admission block, so every member joins the same round; a
// member keeps joining the current round until the payment is signed.
func (fw *frostWallet) pay(
	ctx context.Context,
	actionLogger log.StandardLogger,
	plan *signing.Plan,
	reservation [32]byte,
	admission redemption.Admission,
) error {
	fn := fw.node
	schedule := redemption.Schedule{
		FinalityBlocks:    frostFinalityBlocks,
		RoundBlocks:       frostRedemptionRoundBlocks,
		CancelAfterBlocks: frostRedemptionCancelAfterBlocks,
	}
	resolver := &redemption.Resolver{
		Chain:    fn.bridge,
		Ledger:   redemption.Ledger{Journal: fw.journal},
		Schedule: schedule,
		Policy:   redemption.RebroadcastThenCancel,
		Base:     fw.signingConfig(),
	}

	first := admission.Block + schedule.FinalityBlocks
	if err := fn.node.waitForBlockHeight(ctx, first); err != nil {
		return err
	}
	blockCounter, err := fn.node.chain.BlockCounter()
	if err != nil {
		return err
	}
	for {
		head, err := blockCounter.CurrentBlock()
		if err != nil {
			return err
		}
		intent, err := fn.bridge.Intent(ctx, head, reservation)
		if err != nil {
			return err
		}
		if !intent.State.Unresolved() {
			return fmt.Errorf("FROST reservation resolved before payment")
		}

		round := (head - first) / schedule.RoundBlocks
		roundStart := first + round*schedule.RoundBlocks
		if head > roundStart+schedule.RoundBlocks/3 {
			// Too late to join this round; join the next one.
			if err := fn.node.waitForBlockHeight(ctx, roundStart+schedule.RoundBlocks); err != nil {
				return err
			}
			continue
		}

		roundCtx, cancelRound := fn.roundContext(ctx, roundStart+schedule.RoundBlocks)
		_, err = resolver.Sign(roundCtx, plan, reservation)
		cancelRound()
		if err != nil {
			actionLogger.Warnf("FROST payment round [%d] failed: [%v]", round, err)
			if fw.planSpent(ctx, plan) {
				actionLogger.Infof("FROST payment inputs are spent; nothing left to sign")
				return nil
			}
			if err := fn.node.waitForBlockHeight(ctx, roundStart+schedule.RoundBlocks); err != nil {
				return err
			}
			continue
		}

		if err := resolver.Broadcast(ctx, plan, reservation); err != nil {
			return fmt.Errorf("cannot broadcast FROST payment: [%w]", err)
		}

		actionLogger.Infof(
			"FROST payment [%s] broadcast",
			bitcoin.Hash(plan.BitcoinTransactionHash()).Hex(bitcoin.ReversedByteOrder),
		)
		return nil
	}
}

// frostReservationMessage carries a member's seat attestations of a FROST
// redemption reservation digest to the other members of the wallet.
type frostReservationMessage struct {
	Digest     [32]byte
	Seats      []uint16
	Signatures [][]byte
}

func (frm *frostReservationMessage) Type() string {
	return "tbtc/frost_reservation_attestation"
}

func (frm *frostReservationMessage) Marshal() ([]byte, error) {
	if err := frm.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(frm)
}

func (frm *frostReservationMessage) Unmarshal(raw []byte) error {
	if len(raw) > 64*1024 {
		return errors.New("oversized FROST reservation attestation")
	}
	if err := json.Unmarshal(raw, frm); err != nil {
		return err
	}
	return frm.validate()
}

func (frm *frostReservationMessage) validate() error {
	if len(frm.Seats) == 0 || len(frm.Seats) != len(frm.Signatures) || len(frm.Seats) > 100 {
		return errors.New("invalid FROST reservation attestation")
	}
	for i, seat := range frm.Seats {
		if seat == 0 || seat > 100 || len(frm.Signatures[i]) != 65 {
			return errors.New("invalid FROST reservation attestation")
		}
	}
	return nil
}

// frostReservationChannel exchanges reservation attestations among the
// members of one FROST wallet. A seat's attestation is kept only if it comes
// from the seat's operator and recovers to it.
type frostReservationChannel struct {
	channel    net.BroadcastChannel
	descriptor dkg.Descriptor

	mutex sync.Mutex
	parts map[[32]byte]map[uint16][]byte
}

func newFrostReservationChannel(
	provider net.Provider,
	label [20]byte,
	descriptor dkg.Descriptor,
	membership *group.MembershipValidator,
) (*frostReservationChannel, error) {
	channel, err := provider.BroadcastChannelFor(
		fmt.Sprintf("%s-frost-%x-reservation", ProtocolName, label),
	)
	if err != nil {
		return nil, err
	}
	channel.SetUnmarshaler(func() net.TaggedUnmarshaler {
		return &frostReservationMessage{}
	})
	if err := channel.SetFilter(membership.IsInGroup); err != nil {
		return nil, err
	}

	frc := &frostReservationChannel{
		channel:    channel,
		descriptor: descriptor,
		parts:      make(map[[32]byte]map[uint16][]byte),
	}
	channel.Recv(context.Background(), func(message net.Message) {
		payload, ok := message.Payload().(*frostReservationMessage)
		if !ok {
			return
		}
		for _, seat := range payload.Seats {
			if !membership.IsValidMembership(
				group.MemberIndex(seat),
				message.SenderPublicKey(),
			) {
				return
			}
		}
		frc.add(payload)
	})

	return frc, nil
}

// add keeps the message's seat attestations that recover to their seat's
// operator.
func (frc *frostReservationChannel) add(message *frostReservationMessage) {
	prefixed := crypto.Keccak256Hash(
		[]byte("\x19Ethereum Signed Message:\n32"),
		message.Digest[:],
	)

	frc.mutex.Lock()
	defer frc.mutex.Unlock()

	if frc.parts[message.Digest] == nil {
		frc.parts[message.Digest] = make(map[uint16][]byte)
	}
	for i, seat := range message.Seats {
		if int(seat) > len(frc.descriptor.Operators) {
			continue
		}
		signature := append([]byte{}, message.Signatures[i]...)
		signature[64] -= 27
		publicKey, err := crypto.SigToPub(prefixed[:], signature)
		if err != nil ||
			crypto.PubkeyToAddress(*publicKey) != frc.descriptor.Operators[seat-1] {
			continue
		}
		frc.parts[message.Digest][seat] = message.Signatures[i]
	}
}

func (frc *frostReservationChannel) collected(digest [32]byte) map[uint16][]byte {
	frc.mutex.Lock()
	defer frc.mutex.Unlock()

	collected := make(map[uint16][]byte, len(frc.parts[digest]))
	for seat, signature := range frc.parts[digest] {
		collected[seat] = signature
	}
	return collected
}
