package movement

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// terminal reports authorization refusals after which this plan can never be
// signed: plan again (a new period, new incoming outputs) instead.
func terminal(err error) bool {
	for _, t := range []error{ErrNotFound, ErrPlanMismatch, ErrWalletState, ErrSettled, deposit.ErrMainBusy} {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// Status is the mover's view of one plan after a step.
type Status string

const (
	StatusWaiting   Status = "waiting"
	StatusSigned    Status = "signed"
	StatusBroadcast Status = "broadcast"
	StatusSettled   Status = "settled"
	// StatusAbandoned means the plan can no longer be signed and nothing was
	// signed under it: plan again.
	StatusAbandoned Status = "abandoned"
)

// Round is one agreed signing round, derived from the canonical chain alone,
// as for K-06 sweeps.
type Round struct {
	Number, Anchor, StartBlock uint64
	AnchorHash, Session        [32]byte
}

// ID is the ledger and executor subject of one movement plan.
func ID(plan *signing.Plan) [32]byte {
	c := plan.Commitment()
	return sha256.Sum256(append([]byte("keep-core/frost/movement-id/v1/"), c[:]...))
}

// Mover drives one operator's side of a movement or an incoming sweep.
// Decisions read the chain; local progress is in the K-05 signed-intent
// ledger under the movement ID.
type Mover struct {
	Chain     Reader
	Authority *Authority
	Lock      *deposit.MainLock
	Ledger    redemption.Ledger
	Schedule  redemption.Schedule
	Wallet    dkg.Descriptor
	// Base carries the operator's journal, Bitcoin view, worker, transport,
	// confirmations and LocalRegtest; the rest is filled per attempt.
	Base     signing.Config
	boundary func(string) error
}

func (m *Mover) step(name string) error {
	if m.boundary != nil {
		return m.boundary(name)
	}
	return nil
}

func (m *Mover) roundAt(ctx context.Context, number uint64, plan *signing.Plan) (Round, error) {
	if m.Schedule.FinalityBlocks == 0 || m.Schedule.RoundBlocks == 0 {
		return Round{}, errors.New("incomplete movement schedule")
	}
	anchor := number * m.Schedule.RoundBlocks
	hash, err := m.Chain.HeaderHash(ctx, anchor)
	if err != nil {
		return Round{}, err
	}
	r := Round{Number: number, Anchor: anchor, AnchorHash: hash, StartBlock: anchor + m.Schedule.FinalityBlocks}
	if plan != nil {
		wallet, commitment := plan.WalletID(), plan.Commitment()
		bridge := m.Chain.Bridge()
		raw := append([]byte("keep-core/frost/movement-session/v1/"), wallet[:]...)
		raw = binary.BigEndian.AppendUint64(raw, m.Chain.ChainID())
		raw = append(raw, bridge[:]...)
		raw = append(raw, hash[:]...)
		raw = binary.BigEndian.AppendUint64(raw, number)
		raw = append(raw, commitment[:]...)
		r.Session = sha256.Sum256(raw)
	}
	return r, nil
}

// Current returns the round whose final anchor is the latest one at head.
func (m *Mover) Current(ctx context.Context) (Round, error) {
	if m.Schedule.FinalityBlocks == 0 || m.Schedule.RoundBlocks == 0 {
		return Round{}, errors.New("incomplete movement schedule")
	}
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return Round{}, err
	}
	if head.Number < m.Schedule.FinalityBlocks {
		return Round{}, errors.New("chain shorter than finality")
	}
	return m.roundAt(ctx, (head.Number-m.Schedule.FinalityBlocks)/m.Schedule.RoundBlocks, nil)
}

func (m *Mover) executor(plan *signing.Plan, r Round) (*signing.Executor, error) {
	c := m.Base
	c.Plan = plan
	c.Reservation = ID(plan)
	c.Session = r.Session
	c.StartBlock = r.StartBlock
	c.FinalityBlocks = m.Schedule.FinalityBlocks
	c.Selected = seats(m.Wallet)
	c.Reservations = nil
	c.Sweeps = m.Authority
	c.Guard = deposit.Guard{Lock: m.Lock}
	return signing.NewLocalExecutor(c)
}

// seats is every live seat of the descriptor; rotation is OPEN (D-02/D-11).
func seats(d dkg.Descriptor) []uint16 {
	var out []uint16
	for i, member := range d.Members {
		if member != 0 {
			out = append(out, uint16(i+1))
		}
	}
	return out
}

// Sign runs one agreed attempt of round for plan. The main lock is taken
// before the durable claim; the executor rechecks it and the C-06 records at
// every grant. A movement first passes the authority, whose verdict is the
// evidence that lets it take a main an aborted K-06 sweep still holds.
func (m *Mover) Sign(ctx context.Context, plan *signing.Plan, number uint64) (*signing.Result, error) {
	r, err := m.roundAt(ctx, number, plan)
	if err != nil {
		return nil, err
	}
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	if head.Number < r.StartBlock {
		return nil, errors.New("movement round anchor not yet final")
	}
	if err = m.step("agreement-derived"); err != nil {
		return nil, err
	}
	if main := plan.MovementMain(); main != nil {
		if plan.IsMovement() {
			var committed deposit.CommittedMovement
			if committed, err = deposit.AuthorizeMovement(ctx, m.Authority, plan); err == nil {
				err = m.Lock.AcquireMovement(ctx, committed)
			}
		} else {
			err = m.Lock.AcquireSweep(ctx, plan.WalletID(), main.Outpoint, plan.Commitment())
		}
		if err != nil {
			return nil, err
		}
	}
	e, err := m.executor(plan, r)
	if err != nil {
		return nil, err
	}
	if err = m.step("before-sign"); err != nil {
		return nil, err
	}
	id, tx := ID(plan), plan.BitcoinTransactionHash()
	if err = m.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindMovementAttempt, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); err != nil {
		return nil, err
	}
	result, err := e.Run(ctx)
	if released, releaseErr := e.PossibleRelease(ctx); releaseErr == nil && released {
		_ = m.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindPossibleRelease, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)})
	}
	if err != nil {
		return nil, err
	}
	if err = m.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindMovementSigned, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); err != nil {
		return nil, err
	}
	return result, m.step("after-sign")
}

// signedRound returns the round whose session produced a durable accepted
// result, reconciling a lost signed marker from the executor's records.
func (m *Mover) signedRound(ctx context.Context, plan *signing.Plan) (uint64, bool, error) {
	id, tx := ID(plan), plan.BitcoinTransactionHash()
	entries, err := m.Ledger.Entries(ctx, id)
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
		if e.Kind == redemption.KindMovementSigned {
			return round, parseErr == nil, parseErr
		}
		if e.Kind == redemption.KindMovementAttempt && parseErr == nil && !seen[round] {
			seen[round] = true
			attempted = append(attempted, round)
		}
	}
	for _, number := range attempted {
		r, err := m.roundAt(ctx, number, plan)
		if err != nil {
			return 0, false, err
		}
		e, err := m.executor(plan, r)
		if err != nil {
			return 0, false, err
		}
		if _, err = e.Result(ctx); err != nil {
			continue
		}
		head, _ := m.Chain.Head(ctx)
		if err = m.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindMovementSigned, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(number, 10)}); err != nil {
			return 0, false, err
		}
		return number, true, nil
	}
	return 0, false, nil
}

// Signed checks durable executor results before a caller starts a new round.
// A restarted host can rebroadcast the exact accepted transaction without
// producing another signature or trusting a process-local completion flag.
func (m *Mover) Signed(ctx context.Context, plan *signing.Plan) (bool, error) {
	_, signed, err := m.signedRound(ctx, plan)
	return signed, err
}

// CanAbandon checks every attempted session, including an interrupted attempt
// whose ledger lacks its result marker. A possible released share pins the exact
// intent until chain settlement; read failures are never permission to replan.
func (m *Mover) CanAbandon(ctx context.Context, plan *signing.Plan) (bool, error) {
	entries, err := m.Ledger.Entries(ctx, ID(plan))
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Kind == redemption.KindPossibleRelease || entry.Kind == redemption.KindMovementSigned {
			return false, nil
		}
		if entry.Kind != redemption.KindMovementAttempt {
			continue
		}
		number, err := strconv.ParseUint(entry.Detail, 10, 64)
		if err != nil {
			return false, err
		}
		round, err := m.roundAt(ctx, number, plan)
		if err != nil {
			return false, err
		}
		executor, err := m.executor(plan, round)
		if err != nil {
			return false, err
		}
		released, err := executor.PossibleRelease(ctx)
		if err != nil || released {
			return false, err
		}
	}
	return true, nil
}

// Broadcast sends the durable result through the executor, which rechecks the
// C-06 records and the lock first.
func (m *Mover) Broadcast(ctx context.Context, plan *signing.Plan) error {
	number, ok, err := m.signedRound(ctx, plan)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no durable signed movement")
	}
	r, err := m.roundAt(ctx, number, plan)
	if err != nil {
		return err
	}
	e, err := m.executor(plan, r)
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
	head, _ := m.Chain.Head(ctx)
	return m.Ledger.Append(ctx, redemption.Entry{Reservation: ID(plan), Kind: redemption.KindMovementBroadcast, Block: head.Number, Transaction: plan.BitcoinTransactionHash()})
}

// settled reads the plan's outcome at the finalized head: a proven movement
// commitment, or every incoming input swept.
func (m *Mover) settled(ctx context.Context, plan *signing.Plan, finalized uint64) (bool, error) {
	if plan.IsMovement() {
		c, err := m.Chain.Commitment(ctx, finalized, plan.WalletID(), plan.MovementPeriod())
		if err != nil {
			return false, err
		}
		return c.ProvenAt != 0, nil
	}
	for _, in := range plan.IncomingOutputs() {
		record, err := m.Chain.Incoming(ctx, finalized, OutpointKey(in.Outpoint))
		if err != nil {
			return false, err
		}
		if record.State != IncomingSwept {
			return false, nil
		}
	}
	return true, nil
}

// Resolve advances one plan by one step: settled when its proof is final;
// otherwise broadcast the signed result; abandoned when it can no longer be
// signed; otherwise sign once in the current round.
func (m *Mover) Resolve(ctx context.Context, plan *signing.Plan) (Status, error) {
	id := ID(plan)
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return "", err
	}
	if head.Number < m.Schedule.FinalityBlocks {
		return StatusWaiting, nil
	}
	done, err := m.settled(ctx, plan, head.Number-m.Schedule.FinalityBlocks)
	if err != nil {
		return "", err
	}
	if done {
		if err = m.step("outcome-seen"); err != nil {
			return "", err
		}
		return StatusSettled, m.Ledger.Append(ctx, redemption.Entry{Reservation: id, Kind: redemption.KindMovementOutcome, Block: head.Number, Transaction: plan.BitcoinTransactionHash()})
	}
	_, signed, err := m.signedRound(ctx, plan)
	if err != nil {
		return "", err
	}
	if signed {
		if err = m.Broadcast(ctx, plan); err != nil {
			return "", err
		}
		return StatusBroadcast, nil
	}
	if _, err = m.Authority.AuthorizeSweep(ctx, plan); err != nil {
		if terminal(err) {
			return StatusAbandoned, nil
		}
		return "", err
	}
	current, err := m.Current(ctx)
	if err != nil {
		return "", err
	}
	entries, err := m.Ledger.Entries(ctx, id)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Kind == redemption.KindMovementAttempt && e.Detail == strconv.FormatUint(current.Number, 10) {
			return StatusWaiting, nil
		}
	}
	if _, err = m.Sign(ctx, plan, current.Number); err != nil {
		return "", fmt.Errorf("movement round %d: %w", current.Number, err)
	}
	return StatusSigned, nil
}
