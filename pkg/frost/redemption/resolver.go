package redemption

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// TimeoutPolicy is the honest host's action when a reservation leaves
// Reserved while it holds a durable signed payment. Spec issue #3 is OPEN; no
// production default is chosen.
type TimeoutPolicy int

const (
	// RebroadcastThenCancel rebroadcasts an accepted durable payment result;
	// cancellation is still available as a separate authorized act.
	RebroadcastThenCancel TimeoutPolicy = iota + 1
	// AlwaysCancel never sends the payment after timeout.
	AlwaysCancel
)

// Status is the resolver's view of one reservation after a step.
type Status string

const (
	StatusWaiting           Status = "waiting"
	StatusSigned            Status = "signed"
	StatusBroadcast         Status = "broadcast"
	StatusNeedsCancellation Status = "needs-cancellation"
	StatusSettled           Status = "settled"
)

// Resolver drives one operator's side of a reservation. All decisions read
// the authoritative chain record; all local progress is in the durable ledger.
type Resolver struct {
	Chain    Reader
	Ledger   Ledger
	Schedule Schedule
	Policy   TimeoutPolicy
	// Base carries the operator's journal, Bitcoin view, worker, transport,
	// confirmations and LocalRegtest. Plan, Reservation, Session, StartBlock,
	// FinalityBlocks, Selected and Reservations are filled per attempt.
	Base     signing.Config
	boundary func(string) error
}

func (r *Resolver) step(name string) error {
	if r.boundary != nil {
		return r.boundary(name)
	}
	return nil
}

func (r *Resolver) executor(plan *signing.Plan, reservation [32]byte, a Agreement) (*signing.Executor, error) {
	c := r.Base
	c.Plan = plan
	c.Reservation = reservation
	c.Session = a.Session
	c.StartBlock = a.StartBlock
	c.FinalityBlocks = r.Schedule.FinalityBlocks
	c.Selected = a.Selected
	c.Reservations = ChainReservations{Chain: r.Chain}
	return signing.NewLocalExecutor(c)
}

func purposeOf(plan *signing.Plan) string {
	if plan.IsCancellation() {
		return signing.Cancellation
	}
	return signing.Redemption
}

// agreement derives the agreed config from the admission log of the payment
// or of the cancellation alternative. With round nil it uses the round that
// contains head; otherwise the given round.
func (r *Resolver) agreement(ctx context.Context, plan *signing.Plan, reservation [32]byte, head Head, round *uint64) (Agreement, error) {
	var admission Admission
	var seats []uint16
	purpose := purposeOf(plan)
	if plan.IsCancellation() {
		var err error
		if admission, err = r.Chain.CancellationAdmission(ctx, reservation, plan.BitcoinTransactionHash()); err != nil {
			return Agreement{}, err
		}
		alternatives, err := r.Chain.Alternatives(ctx, head.Number, reservation)
		if err != nil {
			return Agreement{}, err
		}
		for _, alt := range alternatives {
			if alt.TransactionHash == plan.BitcoinTransactionHash() {
				seats = alt.Seats
			}
		}
		if seats == nil {
			return Agreement{}, ErrNotFound
		}
	} else {
		var err error
		if admission, err = r.Chain.Admission(ctx, reservation); err != nil {
			return Agreement{}, err
		}
		intent, err := r.Chain.Intent(ctx, head.Number, reservation)
		if err != nil {
			return Agreement{}, err
		}
		seats = intent.Seats
	}
	if round != nil {
		return AgreeRound(r.Schedule, r.Chain.ChainID(), r.Chain.Bridge(), purpose, reservation, admission, seats, *round)
	}
	return Agree(r.Schedule, r.Chain.ChainID(), r.Chain.Bridge(), purpose, reservation, admission, seats, head.Number)
}

// signedRound returns the round whose session produced a durable accepted
// result. The signed marker is a cache: when it is missing (a crash or I/O
// error after the executor stored its result), every round recorded by a
// sign-attempt marker is reopened and its durable result record is checked.
// A found result is reconciled into the ledger before it is used.
func (r *Resolver) signedRound(ctx context.Context, plan *signing.Plan, reservation [32]byte) (uint64, bool, error) {
	kind, attemptKind := KindSigned, KindSignAttempt
	if plan.IsCancellation() {
		kind, attemptKind = KindCancellationSigned, KindCancellationAttempt
	}
	entries, err := r.Ledger.Entries(ctx, reservation)
	if err != nil {
		return 0, false, err
	}
	tx := plan.BitcoinTransactionHash()
	var attempted []uint64
	seen := map[uint64]bool{}
	for _, e := range entries {
		if e.Transaction != tx {
			continue
		}
		round, parseErr := strconv.ParseUint(e.Detail, 10, 64)
		if e.Kind == kind {
			return round, parseErr == nil, parseErr
		}
		if e.Kind == attemptKind && parseErr == nil && !seen[round] {
			seen[round] = true
			attempted = append(attempted, round)
		}
	}
	for _, round := range attempted {
		a, err := r.roundAgreement(ctx, plan, reservation, round)
		if err != nil {
			return 0, false, err
		}
		e, err := r.executor(plan, reservation, a)
		if err != nil {
			return 0, false, err
		}
		if _, err = e.Result(ctx); err != nil {
			continue
		}
		head, _ := r.Chain.Head(ctx)
		if err = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: kind, Block: head.Number, Transaction: tx, Detail: strconv.FormatUint(round, 10)}); err != nil {
			return 0, false, err
		}
		return round, true, nil
	}
	return 0, false, nil
}

func (r *Resolver) roundAgreement(ctx context.Context, plan *signing.Plan, reservation [32]byte, round uint64) (Agreement, error) {
	head, err := r.Chain.Head(ctx)
	if err != nil {
		return Agreement{}, err
	}
	return r.agreement(ctx, plan, reservation, head, &round)
}

// Sign runs one agreed attempt for the current round. A failed or crashed
// attempt is terminal (the store claim is never reused); the next round has a
// fresh session. A signature that completes after the reservation changed is
// recorded as quarantined and is never broadcast.
func (r *Resolver) Sign(ctx context.Context, plan *signing.Plan, reservation [32]byte) (*signing.Result, error) {
	head, err := r.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	a, err := r.agreement(ctx, plan, reservation, head, nil)
	if err != nil {
		return nil, err
	}
	if err = r.step("agreement-derived"); err != nil {
		return nil, err
	}
	e, err := r.executor(plan, reservation, a)
	if err != nil {
		return nil, err
	}
	if err = r.step("before-sign"); err != nil {
		return nil, err
	}
	// Record the round before any durable signing state exists, so recovery
	// can always find this session's result even if the signed marker is lost.
	attemptKind := KindSignAttempt
	if plan.IsCancellation() {
		attemptKind = KindCancellationAttempt
	}
	if err = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: attemptKind, Block: head.Number, Transaction: plan.BitcoinTransactionHash(), Detail: strconv.FormatUint(a.Round, 10)}); err != nil {
		return nil, err
	}
	result, err := e.Run(ctx)
	if released, releaseErr := e.PossibleRelease(ctx); releaseErr == nil && released {
		_ = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindPossibleRelease, Block: head.Number, Transaction: plan.BitcoinTransactionHash(), Detail: strconv.FormatUint(a.Round, 10)})
	}
	if errors.Is(err, signing.ErrLateSignature) {
		if appendErr := r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindLateQuarantined, Block: head.Number, Transaction: plan.BitcoinTransactionHash(), Detail: strconv.FormatUint(a.Round, 10)}); appendErr != nil {
			return nil, appendErr
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	kind := KindSigned
	if plan.IsCancellation() {
		kind = KindCancellationSigned
	}
	if err = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: kind, Block: head.Number, Transaction: plan.BitcoinTransactionHash(), Detail: strconv.FormatUint(a.Round, 10)}); err != nil {
		return nil, err
	}
	return result, r.step("after-sign")
}

// Broadcast sends the durable result of the recorded round through the K-04
// executor's validated path (reservation still Reserved for a payment;
// unresolved plus finalized admission for a cancellation).
func (r *Resolver) Broadcast(ctx context.Context, plan *signing.Plan, reservation [32]byte) error {
	round, ok, err := r.signedRound(ctx, plan, reservation)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no durable signed result")
	}
	a, err := r.roundAgreement(ctx, plan, reservation, round)
	if err != nil {
		return err
	}
	e, err := r.executor(plan, reservation, a)
	if err != nil {
		return err
	}
	result, err := e.Result(ctx)
	if err != nil {
		return err
	}
	name, kind := "before-payment-broadcast", KindBroadcast
	if plan.IsCancellation() {
		name, kind = "before-cancel-broadcast", KindCancellationBroadcast
	}
	if err = r.step(name); err != nil {
		return err
	}
	if err = e.Broadcast(ctx, result); err != nil {
		return err
	}
	head, _ := r.Chain.Head(ctx)
	return r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: kind, Block: head.Number, Transaction: plan.BitcoinTransactionHash()})
}

// Resolve advances one payment reservation by one step.
func (r *Resolver) Resolve(ctx context.Context, plan *signing.Plan, reservation [32]byte) (Status, error) {
	head, err := r.Chain.Head(ctx)
	if err != nil {
		return "", err
	}
	intent, err := r.Chain.Intent(ctx, head.Number, reservation)
	if err != nil {
		return "", err
	}
	if intent.Certificate.IntentCommitment != plan.Commitment() {
		return "", errors.New("reservation does not admit this plan")
	}
	switch intent.State {
	case IntentPaid, IntentCancelled, IntentConflictResolved:
		if err = r.step("outcome-seen"); err != nil {
			return "", err
		}
		return StatusSettled, r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindOutcome, Block: head.Number, Detail: intent.State.ExecutorState()})
	case IntentReserved:
		_, signed, err := r.signedRound(ctx, plan, reservation)
		if err != nil {
			return "", err
		}
		if !signed {
			late, err := r.Ledger.Has(ctx, reservation, KindLateQuarantined, [32]byte{})
			if err != nil || late {
				return StatusNeedsCancellation, err
			}
			// A failed or crashed attempt of the current round is terminal;
			// wait for the next round's fresh session instead of retrying it.
			current, err := r.agreement(ctx, plan, reservation, head, nil)
			if err != nil {
				return "", err
			}
			attempted, err := r.Ledger.Has(ctx, reservation, KindSignAttempt, plan.BitcoinTransactionHash())
			if err != nil {
				return "", err
			}
			if attempted {
				entries, err := r.Ledger.Entries(ctx, reservation)
				if err != nil {
					return "", err
				}
				for _, e := range entries {
					if e.Kind == KindSignAttempt && e.Transaction == plan.BitcoinTransactionHash() && e.Detail == strconv.FormatUint(current.Round, 10) {
						return StatusWaiting, nil
					}
				}
			}
			if _, err = r.Sign(ctx, plan, reservation); err != nil {
				return "", err
			}
			return StatusSigned, nil
		}
		if err = r.Broadcast(ctx, plan, reservation); err != nil {
			return "", err
		}
		return StatusBroadcast, nil
	case IntentTimeoutPending, IntentQuarantined:
		if err = r.step("timeout-seen"); err != nil {
			return "", err
		}
		if err = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindTimeoutSeen, Block: head.Number, Detail: intent.State.ExecutorState()}); err != nil {
			return "", err
		}
		round, signed, err := r.signedRound(ctx, plan, reservation)
		if err != nil {
			return "", err
		}
		if signed && r.Policy == RebroadcastThenCancel {
			a, err := r.roundAgreement(ctx, plan, reservation, round)
			if err != nil {
				return "", err
			}
			e, err := r.executor(plan, reservation, a)
			if err != nil {
				return "", err
			}
			if err = e.Rebroadcast(ctx); err != nil {
				return "", fmt.Errorf("rebroadcast: %w", err)
			}
			if err = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindBroadcast, Block: head.Number, Transaction: plan.BitcoinTransactionHash(), Detail: "rebroadcast"}); err != nil {
				return "", err
			}
			return StatusBroadcast, nil
		}
		if err = r.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindEscalation, Block: head.Number, Detail: "cancellation required"}); err != nil {
			return "", err
		}
		return StatusNeedsCancellation, nil
	}
	return StatusWaiting, nil
}
