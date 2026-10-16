package deposit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// terminal reports authorization refusals after which this plan can never be
// signed: replan without it.
func terminal(err error) bool {
	for _, t := range []error{ErrNotFound, ErrForeignWallet, ErrNotFinal, ErrSwept, ErrRefundMargin, ErrPlanMismatch, ErrMainBusy} {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// Status is the sweeper's view of one sweep after a step.
type Status string

const (
	StatusWaiting     Status = "waiting"
	StatusSigned      Status = "signed"
	StatusBroadcast   Status = "broadcast"
	StatusQuarantined Status = "quarantined"
	StatusSwept       Status = "swept"
	// StatusAbandoned means a deposit of the plan can no longer be swept
	// (refunded, swept elsewhere or past its margin) and nothing was signed;
	// the next round plans again without it.
	StatusAbandoned Status = "abandoned"
)

// Round is one agreed sweep round. Every honest operator derives it from the
// canonical chain alone: the anchor block is the round's first block, its
// hash binds the session, and signing starts once the anchor is final. A
// failed or crashed attempt is terminal; the next round has a fresh session.
type Round struct {
	Number, Anchor, StartBlock uint64
	AnchorHash, Session        [32]byte
}

// SweepID is the ledger and executor subject of one sweep plan.
func SweepID(plan *signing.Plan) [32]byte {
	c := plan.Commitment()
	return sha256.Sum256(append([]byte("keep-core/frost/sweep-id/v1/"), c[:]...))
}

// Sweeper drives one operator's side of a sweep. Decisions read the chain;
// local progress is in the K-05 signed-intent ledger under the sweep ID.
type Sweeper struct {
	Chain     Reader
	Planner   Planner
	Authority *Authority
	Lock      *MainLock
	Ledger    redemption.Ledger
	Schedule  redemption.Schedule
	// Base carries the operator's journal, Bitcoin view, worker, transport,
	// confirmations and LocalRegtest; the rest is filled per attempt.
	Base     signing.Config
	boundary func(string) error
}

func (s *Sweeper) step(name string) error {
	if s.boundary != nil {
		return s.boundary(name)
	}
	return nil
}

func (s *Sweeper) roundAt(ctx context.Context, number uint64, plan *signing.Plan) (Round, error) {
	if s.Schedule.FinalityBlocks == 0 || s.Schedule.RoundBlocks == 0 {
		return Round{}, errors.New("incomplete sweep schedule")
	}
	anchor := number * s.Schedule.RoundBlocks
	hash, err := s.Chain.HeaderHash(ctx, anchor)
	if err != nil {
		return Round{}, err
	}
	r := Round{Number: number, Anchor: anchor, AnchorHash: hash, StartBlock: anchor + s.Schedule.FinalityBlocks}
	if plan != nil {
		wallet, commitment := plan.WalletID(), plan.Commitment()
		bridge := s.Chain.Bridge()
		raw := append([]byte("keep-core/frost/sweep-session/v1/"), wallet[:]...)
		raw = binary.BigEndian.AppendUint64(raw, s.Chain.ChainID())
		raw = append(raw, bridge[:]...)
		raw = append(raw, hash[:]...)
		raw = binary.BigEndian.AppendUint64(raw, number)
		raw = append(raw, commitment[:]...)
		r.Session = sha256.Sum256(raw)
	}
	return r, nil
}

// Current returns the round whose final anchor is the latest one at head.
func (s *Sweeper) Current(ctx context.Context) (Round, error) {
	if s.Schedule.FinalityBlocks == 0 || s.Schedule.RoundBlocks == 0 {
		return Round{}, errors.New("incomplete sweep schedule")
	}
	head, err := s.Chain.Head(ctx)
	if err != nil {
		return Round{}, err
	}
	if head.Number < s.Schedule.FinalityBlocks {
		return Round{}, errors.New("chain shorter than finality")
	}
	return s.roundAt(ctx, (head.Number-s.Schedule.FinalityBlocks)/s.Schedule.RoundBlocks, nil)
}

// Plan derives the round's ordinary sweep of every pending deposit.
func (s *Sweeper) Plan(ctx context.Context, from uint64, round Round) (*signing.Plan, error) {
	keys, err := s.Planner.Pending(ctx, from, round.Anchor, 0)
	if err != nil {
		return nil, err
	}
	return s.Planner.Build(ctx, round.Anchor, keys, [32]byte{})
}

func (s *Sweeper) executor(plan *signing.Plan, r Round) (*signing.Executor, error) {
	c := s.Base
	c.Plan = plan
	c.Reservation = SweepID(plan)
	c.Session = r.Session
	c.StartBlock = r.StartBlock
	c.FinalityBlocks = s.Schedule.FinalityBlocks
	c.Selected = seats(s.Planner.Wallet)
	c.Reservations = nil
	c.Sweeps = s.Authority
	c.Guard = Guard{Lock: s.Lock}
	return signing.NewLocalExecutor(c)
}

// seats is every live seat of the descriptor in increasing order. Rotating a
// threshold-sized selection when an operator is offline is a liveness choice
// left OPEN; the local fixture runs every operator.
func seats(d dkg.Descriptor) []uint16 {
	var out []uint16
	for i, m := range d.Members {
		if m != 0 {
			out = append(out, uint16(i+1))
		}
	}
	return out
}

// Sign runs one agreed attempt of round for plan. The main lock is taken for
// a main-spending sweep before the durable claim; the executor rechecks it,
// the deposits and the margin at every grant. A signature that completes
// after the authorization lapsed is recorded as quarantined and is never
// broadcast.
func (s *Sweeper) Sign(ctx context.Context, plan *signing.Plan, number uint64) (*signing.Result, error) {
	r, err := s.roundAt(ctx, number, plan)
	if err != nil {
		return nil, err
	}
	head, err := s.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	if head.Number < r.StartBlock {
		return nil, errors.New("sweep round anchor not yet final")
	}
	if err = s.step("agreement-derived"); err != nil {
		return nil, err
	}
	if main := plan.SweepMain(); main != nil && plan.SweepResolution() == ([32]byte{}) {
		if err = s.Lock.AcquireSweep(ctx, plan.WalletID(), main.Outpoint, plan.Commitment()); err != nil {
			return nil, err
		}
	}
	e, err := s.executor(plan, r)
	if err != nil {
		return nil, err
	}
	if err = s.step("before-sign"); err != nil {
		return nil, err
	}
	id, tx := SweepID(plan), plan.BitcoinTransactionHash()
	// Record the round first, so a lost signed marker can always be found
	// again from the executor's durable result (the K-05 F2 rule).
	if err = s.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindSweepAttempt, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); err != nil {
		return nil, err
	}
	result, err := e.Run(ctx)
	if released, releaseErr := e.PossibleRelease(ctx); releaseErr == nil && released {
		_ = s.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindPossibleRelease, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)})
	}
	if errors.Is(err, signing.ErrLateSignature) {
		if err = s.step("before-late-quarantine"); err != nil {
			return nil, err
		}
		if appendErr := s.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindSweepLateQuarantined, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); appendErr != nil {
			return nil, appendErr
		}
		return nil, signing.ErrLateSignature
	}
	if err != nil {
		return nil, err
	}
	if err = s.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindSweepSigned, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); err != nil {
		return nil, err
	}
	return result, s.step("after-sign")
}

// signedRound returns the round whose session produced a durable accepted
// result, reconciling a lost signed marker from the executor's records.
func (s *Sweeper) signedRound(ctx context.Context, plan *signing.Plan) (uint64, bool, error) {
	id, tx := SweepID(plan), plan.BitcoinTransactionHash()
	entries, err := s.Ledger.Entries(ctx, id)
	if err != nil {
		return 0, false, err
	}
	var attempted []uint64
	seen := map[uint64]bool{}
	for _, e := range entries {
		if e.Transaction != tx {
			continue
		}
		round, parseErr := strconv.ParseUint(e.Detail, 10, 64)
		if e.Kind == redemption.KindSweepSigned {
			return round, parseErr == nil, parseErr
		}
		if e.Kind == redemption.KindSweepAttempt && parseErr == nil && !seen[round] {
			seen[round] = true
			attempted = append(attempted, round)
		}
	}
	for _, number := range attempted {
		r, err := s.roundAt(ctx, number, plan)
		if err != nil {
			return 0, false, err
		}
		e, err := s.executor(plan, r)
		if err != nil {
			return 0, false, err
		}
		if _, err = e.Result(ctx); err != nil {
			continue
		}
		head, _ := s.Chain.Head(ctx)
		if err = s.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindSweepSigned, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); err != nil {
			return 0, false, err
		}
		return number, true, nil
	}
	return 0, false, nil
}

// Broadcast sends the durable result through the executor, which rechecks the
// deposits, the margin and the lock first. A quarantined sweep never reaches
// this path: it has no accepted result.
func (s *Sweeper) Broadcast(ctx context.Context, plan *signing.Plan) error {
	number, ok, err := s.signedRound(ctx, plan)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no durable signed sweep")
	}
	r, err := s.roundAt(ctx, number, plan)
	if err != nil {
		return err
	}
	e, err := s.executor(plan, r)
	if err != nil {
		return err
	}
	result, err := e.Result(ctx)
	if err != nil {
		return err
	}
	if err = e.Broadcast(ctx, result); err != nil {
		return err
	}
	head, _ := s.Chain.Head(ctx)
	return s.Ledger.Append(ctx, redemption.Entry{Reservation: SweepID(plan), Kind: redemption.KindSweepBroadcast, Block: head.Number, Transaction: plan.BitcoinTransactionHash()})
}

// Resolve advances one sweep by one step: outcome when every deposit is swept
// at the finalized head; quarantined when a late signature exists; abandoned
// when a deposit is no longer sweepable and nothing was signed; otherwise
// sign in the current round or broadcast the signed result.
func (s *Sweeper) Resolve(ctx context.Context, plan *signing.Plan) (Status, error) {
	id := SweepID(plan)
	head, err := s.Chain.Head(ctx)
	if err != nil {
		return "", err
	}
	if head.Number < s.Schedule.FinalityBlocks {
		return StatusWaiting, nil
	}
	finalized := head.Number - s.Schedule.FinalityBlocks
	swept := true
	for _, d := range plan.SweepDeposits() {
		r, err := s.Chain.Deposit(ctx, finalized, d.Key)
		if err != nil {
			return "", err
		}
		swept = swept && r.SweptAt != 0
	}
	if swept {
		if err = s.step("outcome-seen"); err != nil {
			return "", err
		}
		return StatusSwept, s.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindSweepOutcome, Block: head.Number, Transaction: plan.BitcoinTransactionHash()})
	}
	late, err := s.Ledger.Has(ctx, id, redemption.KindSweepLateQuarantined, [32]byte{})
	if err != nil || late {
		return StatusQuarantined, err
	}
	_, signed, err := s.signedRound(ctx, plan)
	if err != nil {
		return "", err
	}
	if signed {
		if err = s.Broadcast(ctx, plan); err != nil {
			return "", err
		}
		return StatusBroadcast, nil
	}
	if _, err = s.Authority.AuthorizeSweep(ctx, plan); err != nil {
		if terminal(err) {
			return StatusAbandoned, nil
		}
		// A provider, context or read error is not a refusal: keep the plan.
		return "", err
	}
	current, err := s.Current(ctx)
	if err != nil {
		return "", err
	}
	entries, err := s.Ledger.Entries(ctx, id)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Kind == redemption.KindSweepAttempt && e.Detail == strconv.FormatUint(current.Number, 10) {
			// The current round's attempt is terminal; wait for the next round.
			return StatusWaiting, nil
		}
	}
	if _, err = s.Sign(ctx, plan, current.Number); err != nil {
		return "", fmt.Errorf("sweep round %d: %w", current.Number, err)
	}
	return StatusSigned, nil
}
