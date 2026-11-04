package tbtc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

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
	case *DepositSweepProposal:
		err = wallet.sweepDeposits(
			ctx,
			actionLogger,
			proposal,
			result.window.coordinationBlock,
			startBlock,
		)
	case *MovingFundsProposal:
		err = wallet.moveFunds(ctx, actionLogger, proposal, startBlock)
	case *HeartbeatProposal:
		err = wallet.heartbeat(
			ctx,
			actionLogger,
			proposal,
			result.window.coordinationBlock,
			startBlock,
			expiryBlock,
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
	mover := fw.mover(view, schedule)

	return fw.signInRounds(
		ctx,
		actionLogger,
		"incoming sweep",
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

	// A reservation the wallet already holds is resumed, never planned
	// again: the leader re-proposes its requests until it is resolved.
	custody, err := fn.bridge.Custody(ctx, coordinationBlock, fw.id)
	if err != nil {
		return err
	}
	if custody.ActiveReservation != ([32]byte{}) {
		return fw.resume(ctx, actionLogger, proposal, custody.ActiveReservation)
	}

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

	if err := fw.retainPlan(ctx, reservation, plan); err != nil {
		return err
	}
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

// resume continues the wallet's admitted, unresolved reservation after a
// restart or an interrupted action: the plan this operator attested is read
// back from the FROST store and must be the one the Bridge admitted, and the
// leader's proposal must name its requests. The same reservation is then
// paid; nothing is planned or spent anew.
func (fw *frostWallet) resume(
	ctx context.Context,
	actionLogger log.StandardLogger,
	proposal *RedemptionProposal,
	reservation [32]byte,
) error {
	fn := fw.node
	payCtx := context.WithoutCancel(ctx)

	head, err := fn.bridge.Head(ctx)
	if err != nil {
		return err
	}
	intent, err := fn.bridge.Intent(ctx, head.Number, reservation)
	if err != nil {
		return err
	}
	if !intent.State.Unresolved() {
		return nil
	}
	plan, err := fw.loadPlan(ctx, reservation)
	if err != nil {
		return fmt.Errorf(
			"cannot resume FROST reservation [0x%x]: [%w]",
			reservation,
			err,
		)
	}
	if plan.Commitment() != intent.Certificate.IntentCommitment {
		return fmt.Errorf("retained FROST plan is not the admitted one")
	}
	requests := plan.Requests()
	if len(requests) != len(proposal.RedeemersOutputScripts) {
		return fmt.Errorf("FROST reservation does not match the proposal")
	}
	for i, request := range requests {
		if !bytes.Equal(request.Script, proposal.RedeemersOutputScripts[i]) {
			return fmt.Errorf("FROST reservation does not match the proposal")
		}
	}
	admission, err := fn.bridge.Admission(payCtx, reservation)
	if err != nil {
		return err
	}

	actionLogger.Infof(
		"resuming FROST redemption reservation [0x%x] admitted at block [%d]",
		reservation,
		admission.Block,
	)

	return fw.pay(payCtx, actionLogger, plan, reservation, admission)
}

// frostPlanRecordID is the FROST store record of the plan attested for a
// reservation.
func frostPlanRecordID(reservation [32]byte) [32]byte {
	return sha256.Sum256(append([]byte("tbtc/frost-redemption-plan/v1/"), reservation[:]...))
}

// frostPlanRecord is the public recovery data of an attested redemption
// plan: everything signing.NewPlan needs besides the wallet.
type frostPlanRecord struct {
	Generation, StateVersion uint64
	Requests                 []signing.Request
	ConflictInput            bitcoin.TransactionOutpoint
	Fees                     signing.FeePolicy
	Transaction              []byte
	Prevouts                 []bitcoin.PreviousOutput
}

// retainPlan keeps the plan of a reservation in the FROST store before it is
// attested, so that the reservation can be paid after a restart.
func (fw *frostWallet) retainPlan(
	ctx context.Context,
	reservation [32]byte,
	plan *signing.Plan,
) error {
	record := frostPlanRecord{
		Generation:    plan.Generation(),
		StateVersion:  plan.StateVersion(),
		Requests:      plan.Requests(),
		ConflictInput: plan.ConflictInput(),
		Fees:          plan.Fees(),
		Transaction:   plan.Snapshot().Unsigned().Serialize(bitcoin.Standard),
		Prevouts:      plan.Snapshot().PreviousOutputs(),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return fw.journal.RetainSigningRecord(ctx, frostPlanRecordID(reservation), payload)
}

// loadPlan rebuilds the plan retained for a reservation.
func (fw *frostWallet) loadPlan(
	ctx context.Context,
	reservation [32]byte,
) (*signing.Plan, error) {
	payload, err := fw.journal.ReadSigningRecord(ctx, frostPlanRecordID(reservation))
	if err != nil {
		return nil, err
	}
	var record frostPlanRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, err
	}
	transaction := new(bitcoin.Transaction)
	if err := transaction.Deserialize(record.Transaction); err != nil {
		return nil, err
	}
	return signing.NewPlan(signing.Proposal{
		Wallet:         fw.descriptor,
		BitcoinGenesis: fw.node.genesis,
		Purpose:        signing.Redemption,
		Generation:     record.Generation,
		StateVersion:   record.StateVersion,
		Requests:       record.Requests,
		ConflictInput:  record.ConflictInput,
		Fees:           record.Fees,
		Transaction:    transaction,
		Prevouts:       record.Prevouts,
	})
}

// pay resolves the admitted reservation with K-05's resolver until its
// payment is broadcast or the reservation is resolved: it signs in rounds
// derived from the admission block, so every member joins the same round,
// broadcasts a signed payment (again, after a restart), and rebroadcasts it
// after the timeout. A failed read or round is retried on the next block or
// round; the reservation is never dropped for a transient error.
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
			actionLogger.Warnf("cannot read the current block: [%v]", err)
			time.Sleep(frostPollInterval)
			continue
		}
		round := (head - first) / schedule.RoundBlocks
		roundStart := first + round*schedule.RoundBlocks
		roundEnd := roundStart + schedule.RoundBlocks
		if head > roundStart+schedule.RoundBlocks/3 {
			// Too late to join this round's signing; join the next one.
			if err := fn.node.waitForBlockHeight(ctx, roundEnd); err != nil {
				return err
			}
			continue
		}

		roundCtx, cancelRound := fn.roundContext(ctx, roundEnd)
		status, err := resolver.Resolve(roundCtx, plan, reservation)
		cancelRound()
		if err == nil && status == redemption.StatusSigned {
			// Broadcast the signed payment right away.
			status, err = resolver.Resolve(ctx, plan, reservation)
		}
		if err != nil {
			actionLogger.Warnf("FROST payment round [%d] failed: [%v]", round, err)
			if err := fn.node.waitForBlockHeight(ctx, roundEnd); err != nil {
				return err
			}
			continue
		}

		switch status {
		case redemption.StatusBroadcast:
			actionLogger.Infof(
				"FROST payment [%s] broadcast",
				bitcoin.Hash(plan.BitcoinTransactionHash()).Hex(bitcoin.ReversedByteOrder),
			)
			return nil
		case redemption.StatusSettled:
			return nil
		case redemption.StatusNeedsCancellation, redemption.StatusAwaitingDirective:
			return fmt.Errorf("FROST reservation needs [%s]", status)
		default:
			// Waiting for the next round's fresh session.
			if err := fn.node.waitForBlockHeight(ctx, roundEnd); err != nil {
				return err
			}
		}
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

func (frm *frostReservationMessage) attestation() *frostReservationMessage {
	return frm
}

// frostInactivityMessage carries a member's seat signatures of a FROST
// inactivity claim digest (C-09) to the other members of the wallet.
type frostInactivityMessage struct {
	frostReservationMessage
}

func (fim *frostInactivityMessage) Type() string {
	return "tbtc/frost_inactivity_attestation"
}

// frostAttestation is a message of seat signatures over one digest.
type frostAttestation interface {
	net.TaggedUnmarshaler
	attestation() *frostReservationMessage
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

// frostReservationChannel exchanges seat attestations of one kind
// (reservation digests, inactivity claim digests) among the members of one
// FROST wallet. A seat's attestation is kept only if it comes from the
// seat's operator and recovers to it.
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
	return newFrostAttestationChannel(
		provider,
		fmt.Sprintf("%s-frost-%x-reservation", ProtocolName, label),
		descriptor,
		membership,
		func() net.TaggedUnmarshaler { return &frostReservationMessage{} },
	)
}

// newFrostInactivityChannel exchanges inactivity claim signatures.
func newFrostInactivityChannel(
	provider net.Provider,
	label [20]byte,
	descriptor dkg.Descriptor,
	membership *group.MembershipValidator,
) (*frostReservationChannel, error) {
	return newFrostAttestationChannel(
		provider,
		fmt.Sprintf("%s-frost-%x-inactivity", ProtocolName, label),
		descriptor,
		membership,
		func() net.TaggedUnmarshaler { return &frostInactivityMessage{} },
	)
}

func newFrostAttestationChannel(
	provider net.Provider,
	name string,
	descriptor dkg.Descriptor,
	membership *group.MembershipValidator,
	unmarshaler func() net.TaggedUnmarshaler,
) (*frostReservationChannel, error) {
	channel, err := provider.BroadcastChannelFor(name)
	if err != nil {
		return nil, err
	}
	channel.SetUnmarshaler(unmarshaler)
	if err := channel.SetFilter(membership.IsInGroup); err != nil {
		return nil, err
	}

	frc := &frostReservationChannel{
		channel:    channel,
		descriptor: descriptor,
		parts:      make(map[[32]byte]map[uint16][]byte),
	}
	channel.Recv(context.Background(), func(message net.Message) {
		attestation, ok := message.Payload().(frostAttestation)
		if !ok {
			return
		}
		payload := attestation.attestation()
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
