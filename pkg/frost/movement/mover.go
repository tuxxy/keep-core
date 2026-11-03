package movement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"

	"github.com/ethereum/go-ethereum/common"

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
	c.Selected = selected(m.Wallet, r.Number)
	c.Reservations = nil
	c.Sweeps = m.Authority
	c.Guard = deposit.Guard{Lock: m.Lock}
	return signing.NewLocalExecutor(c)
}

// selected is the round's signing seats, chosen as base keep-core chooses
// the participants of a signing attempt (pkg/tbtc signingRetryLoop,
// tecdsa/retry EvaluateRetryParticipantsForSigning): operators sorted by
// address and shuffled with a seed shared by every operator, then whole
// operators taken until their seats reach the threshold (tuxxy/keep-core#66).
// Base shuffles afresh for every attempt. Here the shuffle is redrawn once per
// rotation cycle of len(operators) rounds and the start operator rotates
// within the cycle, so one absent operator is left out of some round of every
// cycle, and the reshuffles reach every set of live operators whose seats
// reach the threshold. The seed is the wallet ID and the retry count the
// cycle, so every operator derives the same seats from the round alone.
func selected(d dkg.Descriptor, round uint64) []uint16 {
	seatsOf := map[common.Address][]uint16{}
	var operators []common.Address
	for i, member := range d.Members {
		if member == 0 {
			continue
		}
		operator := d.Operators[i]
		if _, ok := seatsOf[operator]; !ok {
			operators = append(operators, operator)
		}
		seatsOf[operator] = append(seatsOf[operator], uint16(i+1))
	}
	if len(operators) == 0 {
		return nil
	}
	sort.Slice(operators, func(i, j int) bool {
		return bytes.Compare(operators[i][:], operators[j][:]) < 0
	})
	id := dkg.WalletID(d.OutputKey)
	n := uint64(len(operators))
	// #nosec G404 (insecure random number source (rand))
	// As in base, shuffling operators for retries needs no secure randomness.
	rng := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(id[:8])) + int64(round/n)))
	rng.Shuffle(len(operators), func(i, j int) {
		operators[i], operators[j] = operators[j], operators[i]
	})
	var out []uint16
	for i := uint64(0); i < n && len(out) < int(d.Threshold); i++ {
		out = append(out, seatsOf[operators[(round%n+i)%n]]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Eligible reports whether this operator holds a seat selected in round; an
// operator outside the selection does not sign that round, as a base member
// excluded from an attempt does not.
func (m *Mover) Eligible(ctx context.Context, round uint64) (bool, error) {
	status, err := m.Base.Journal.DKGStatus(ctx)
	if err != nil {
		return false, err
	}
	for _, seat := range selected(m.Wallet, round) {
		for _, local := range status.LocalSeats {
			if seat == local {
				return true, nil
			}
		}
	}
	return false, nil
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
// commitment, or the plan's own incoming sweep proven by the wallet. Inputs
// swept by another proven transaction do not settle this plan, so its own
// hash is never recorded as that outcome (tuxxy/keep-core#71); it is
// abandoned once its inputs can no longer be authorized.
func (m *Mover) settled(ctx context.Context, plan *signing.Plan, finalized uint64) (bool, error) {
	if plan.IsMovement() {
		c, err := m.Chain.Commitment(ctx, finalized, plan.WalletID(), plan.MovementPeriod())
		if err != nil {
			return false, err
		}
		return c.ProvenAt != 0, nil
	}
	wallet, err := m.Chain.ProvenTransaction(ctx, finalized, plan.BitcoinTransactionHash())
	if err != nil {
		return false, err
	}
	return wallet == plan.WalletID(), nil
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
	if eligible, err := m.Eligible(ctx, current.Number); err != nil || !eligible {
		return StatusWaiting, err
	}
	if _, err = m.Sign(ctx, plan, current.Number); err != nil {
		return "", fmt.Errorf("movement round %d: %w", current.Number, err)
	}
	return StatusSigned, nil
}
