package deposit

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// The sweeper tests run one operator's Sweeper over the fault chain and the
// fixture host journal of gaps_fixture_test.go. The executor is the real
// signing executor; the fixture journal holds no wallet record, so every Run
// stops at its first wallet read (errNoWallet) before any grant or worker.
// What is checked is the sweeper's own bookkeeping around that run: rounds,
// the main lock, and the K-05 ledger records.

func notPanic(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil && strings.HasPrefix(err.Error(), "panic:") {
		t.Fatalf("%s crashed: %v", what, err)
	}
}

// TestSweeperCurrentRound: the round whose anchor is final at the head is
// number (head - FinalityBlocks) / RoundBlocks, anchored at number *
// RoundBlocks, starting FinalityBlocks after the anchor, bound to the
// anchor's hash. Invariant: exact for a mid-round head and at head ==
// FinalityBlocks (round 0); refused for a shorter chain, an incomplete
// schedule and failed head or anchor reads, never a crash. Refs M5-deposit
// mutation batch (KC-SF-V3 K-06 rounds).
func TestSweeperCurrentRound(t *testing.T) {
	for _, tc := range []struct {
		name string
		head uint64
		want Round
	}{
		{"mid round", 100, Round{Number: 9, Anchor: 90, StartBlock: 92}},
		{"head at the finality depth", 2, Round{Number: 0, Anchor: 0, StartBlock: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSweepHost(t)
			h.chain.head = tc.head
			var got Round
			err := recovered(func() error { var err error; got, err = h.sweeper.Current(ctx); return err })
			tc.want.AnchorHash = h.chain.hash(tc.want.Anchor)
			if err != nil || got != tc.want {
				t.Fatalf("current round at head %d: %+v (%v), want %+v", tc.head, got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		set  func(*sweepHost)
	}{
		{"chain shorter than finality", func(h *sweepHost) { h.chain.head = 1 }},
		{"no finality depth", func(h *sweepHost) { h.sweeper.Schedule.FinalityBlocks = 0 }},
		{"no round length", func(h *sweepHost) { h.sweeper.Schedule.RoundBlocks = 0 }},
		{"head unreadable", func(h *sweepHost) { h.fc.set("Head", true) }},
		{"anchor hash unreadable", func(h *sweepHost) { h.fc.hashFail = func(n uint64) bool { return n == 90 } }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSweepHost(t)
			tc.set(h)
			err := recovered(func() error { _, err := h.sweeper.Current(ctx); return err })
			notPanic(t, err, tc.name)
			if err == nil {
				t.Fatalf("a current round was derived with %s", tc.name)
			}
		})
	}
}

// TestSweeperPlanFailsOnPendingFailure: a round's plan is built from a
// complete pending read. Invariant: a failed reveal read fails Plan; it does
// not plan an empty or partial sweep. Refs M5-deposit mutation batch.
func TestSweeperPlanFailsOnPendingFailure(t *testing.T) {
	h := newSweepHost(t)
	h.deposit(1, 100_000, 90, [20]byte{})
	h.fc.set("Reveals", true)
	if plan, err := h.sweeper.Plan(ctx, 0, Round{Number: 10, Anchor: 100}); err == nil || plan != nil {
		t.Fatal("a round was planned from a failed reveal read")
	}
}

// TestSweeperSelectedSeats: an operator signs only with seats in the
// wallet's accepted readiness certificate (KC-SF-V3 4.2). Invariant: the
// selection is exactly the descriptor's live seats that are certified, in
// increasing order; a host none of whose local seats is certified refuses
// with ErrUncertifiedSeat; failed status or certificate reads refuse. The
// descriptor and certificate are fixtures. Refs M5-deposit mutation batch.
func TestSweeperSelectedSeats(t *testing.T) {
	h := newSweepHost(t)
	plan := planned(t, h.world, h.deposit(1, 100_000, 90, [20]byte{}))
	// Seats 1, 3 and 4 are live; 1 and 3 are certified.
	h.sweeper.Planner.Wallet = dkg.Descriptor{Members: []uint32{11, 0, 13, 14}}
	h.fc.seats = []uint16{1, 3}
	got, err := h.sweeper.selected(ctx, plan)
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("selected seats %v (%v), want the certified live seats [1 3]", got, err)
	}
	h.host.seats = []uint16{4}
	if _, err = h.sweeper.selected(ctx, plan); !errors.Is(err, redemption.ErrUncertifiedSeat) {
		t.Fatal("a host without a certified local seat was selected:", err)
	}
	h.host.seats, h.host.failStatus = []uint16{1}, true
	if _, err = h.sweeper.selected(ctx, plan); err == nil {
		t.Fatal("seats were selected without the local DKG status")
	}
	h.host.failStatus = false
	h.fc.set("CertifiedSeats", true)
	if _, err = h.sweeper.selected(ctx, plan); err == nil {
		t.Fatal("seats were selected without the readiness certificate")
	}
}

// signOutcome is what one Sign call left behind.
type signOutcome struct {
	err     error
	entries map[redemption.Kind]int
	lock    []Entry
}

func (h *sweepHost) sign(t *testing.T, plan *signing.Plan, number uint64) signOutcome {
	t.Helper()
	err := recovered(func() error { _, err := h.sweeper.Sign(ctx, plan, number); return err })
	out := signOutcome{err: err, entries: kinds(h.entries(t, plan))}
	if m := plan.SweepMain(); m != nil {
		out.lock, _ = (&MainLock{Journal: h.journal}).History(ctx, h.id, OutpointKey(m.Outpoint))
	}
	return out
}

// TestSweeperSignStartsAnAgreedAttempt: Sign starts a round's attempt only
// when the round is derivable and its anchor final, the host holds a
// certified seat, the executor can be built and the main lock admits the
// plan; it records the attempt before running and returns the run's own
// failure. Invariant: on every refusal nothing is recorded and the lock is
// unchanged; on a start the attempt is recorded once, an ordinary main sweep
// holds the lock for its plan, a deposit-only or resolution sweep takes no
// sweep record, no signed or possible-release record is written when the run
// failed before any intent was retained, and the error is the run's failure.
// Refs M5-deposit mutation batch (KC-SF-V3 K-06, VD-07).
func TestSweeperSignStartsAnAgreedAttempt(t *testing.T) {
	started := func(t *testing.T, o signOutcome, what string) {
		t.Helper()
		notPanic(t, o.err, what)
		if !errors.Is(o.err, errNoWallet) {
			t.Fatalf("%s: Sign did not return the run's own failure: %v", what, o.err)
		}
		if o.entries[redemption.KindSweepAttempt] != 1 || o.entries[redemption.KindSweepSigned] != 0 || o.entries[redemption.KindPossibleRelease] != 0 || len(o.entries) != 1 {
			t.Fatalf("%s: ledger after a failed run %v, want exactly one attempt", what, o.entries)
		}
	}
	refused := func(t *testing.T, o signOutcome, what string, lockBefore int) {
		t.Helper()
		notPanic(t, o.err, what)
		if o.err == nil {
			t.Fatalf("%s: Sign succeeded", what)
		}
		if len(o.entries) != 0 || len(o.lock) != lockBefore {
			t.Fatalf("%s: a refused Sign left records: ledger %v, lock %d -> %d", what, o.entries, lockBefore, len(o.lock))
		}
	}
	t.Run("ordinary main sweep", func(t *testing.T) {
		h := newSweepHost(t)
		h.fund(1_000_000)
		plan := planned(t, h.world, h.deposit(1, 100_000, 90, [20]byte{}))
		o := h.sign(t, plan, 9)
		started(t, o, "ordinary main sweep")
		if len(o.lock) != 1 || o.lock[0].Purpose != SweepMain || o.lock[0].Subject != plan.Commitment() {
			t.Fatalf("an ordinary main sweep did not hold the main for its plan: %+v", o.lock)
		}
	})
	t.Run("anchor exactly final", func(t *testing.T) {
		h := newSweepHost(t)
		h.fund(1_000_000)
		plan := planned(t, h.world, h.deposit(1, 100_000, 80, [20]byte{}))
		h.chain.head = 92 // round 9: anchor 90, start 92
		started(t, h.sign(t, plan, 9), "head at the round's start block")
	})
	t.Run("deposit-only sweep", func(t *testing.T) {
		h := newSweepHost(t)
		plan := planned(t, h.world, h.deposit(1, 100_000, 90, [20]byte{}))
		started(t, h.sign(t, plan, 9), "deposit-only sweep")
	})
	t.Run("resolution sweep", func(t *testing.T) {
		h := newSweepHost(t)
		main := h.fund(1_000_000)
		r := [32]byte{0x99}
		h.chain.custody.ActiveReservation = r
		plan, err := h.planner.Build(ctx, 100, [][32]byte{h.deposit(1, 100_000, 90, [20]byte{})}, r)
		if err != nil {
			t.Fatal(err)
		}
		if err = h.lock.AcquireRedemption(ctx, h.id, main.Outpoint, r, 200); err != nil {
			t.Fatal(err)
		}
		if err = h.lock.Resolve(ctx, h.id, main.Outpoint, r, plan.BitcoinTransactionHash()); err != nil {
			t.Fatal(err)
		}
		o := h.sign(t, plan, 9)
		started(t, o, "resolution sweep")
		if len(o.lock) != 2 || state(o.lock).Purpose != Resolution {
			t.Fatalf("a resolution sweep changed its resolution lock: %+v", o.lock)
		}
	})
	for _, tc := range []struct {
		name string
		set  func(*testing.T, *sweepHost, *signing.Plan)
	}{
		{"anchor not yet final", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.chain.head = 91 }},
		{"no round length", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.sweeper.Schedule.RoundBlocks = 0 }},
		{"no finality depth", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.sweeper.Schedule.FinalityBlocks = 0 }},
		{"anchor hash unreadable", func(_ *testing.T, h *sweepHost, _ *signing.Plan) {
			h.fc.hashFail = func(n uint64) bool { return n == 90 }
		}},
		{"head unreadable", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.fc.set("Head", true) }},
		{"stop after the agreement", func(_ *testing.T, h *sweepHost, _ *signing.Plan) {
			h.sweeper.boundary = stopAtName("agreement-derived")
		}},
		{"no certified local seat", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.host.seats = []uint16{9} }},
		{"local DKG status unreadable", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.host.failStatus = true }},
		{"executor incomplete", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.sweeper.Base.Transport = nil }},
		{"main held by a redemption", func(t *testing.T, h *sweepHost, p *signing.Plan) {
			if err := h.lock.AcquireRedemption(ctx, h.id, p.SweepMain().Outpoint, [32]byte{0x99}, 200); err != nil {
				t.Fatal(err)
			}
		}},
		{"stop before signing", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.sweeper.boundary = stopAtName("before-sign") }},
		{"ledger unwritable", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.ledgerJ.failWrites = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSweepHost(t)
			h.fund(1_000_000)
			plan := planned(t, h.world, h.deposit(1, 100_000, 80, [20]byte{}))
			tc.set(t, h, plan)
			lock, _ := (&MainLock{Journal: h.journal}).History(ctx, h.id, OutpointKey(plan.SweepMain().Outpoint))
			o := h.sign(t, plan, 9)
			if tc.name == "stop before signing" || tc.name == "ledger unwritable" {
				// The main lock is taken before these points by design.
				notPanic(t, o.err, tc.name)
				if o.err == nil || len(o.entries) != 0 {
					t.Fatalf("%s: Sign recorded an attempt or succeeded: %v %v", tc.name, o.entries, o.err)
				}
				return
			}
			refused(t, o, tc.name, len(lock))
			if tc.name == "no certified local seat" && !errors.Is(o.err, redemption.ErrUncertifiedSeat) {
				t.Fatal("an uncertified host was not refused as one:", o.err)
			}
		})
	}
}

// TestSweeperSignedRoundAndBroadcast: the signed round of a plan is read
// from the ledger records of that plan's transaction only, and Broadcast
// sends only a durable accepted result. Invariant: the signed marker of the
// plan's own transaction is found behind a marker of another transaction;
// without any marker, with an unreadable ledger, with an underivable round
// or executor, or without the durable result, Broadcast fails and records
// no broadcast. Refs M5-deposit mutation batch.
func TestSweeperSignedRoundAndBroadcast(t *testing.T) {
	setup := func(t *testing.T) (*sweepHost, *signing.Plan) {
		h := newSweepHost(t)
		h.fund(1_000_000)
		return h, planned(t, h.world, h.deposit(1, 100_000, 90, [20]byte{}))
	}
	appendEntry := func(t *testing.T, h *sweepHost, plan *signing.Plan, kind redemption.Kind, tx [32]byte, round uint64) {
		t.Helper()
		if err := h.sweeper.Ledger.Append(ctx, redemption.Entry{Reservation: SweepID(plan), Kind: kind, Block: 100, Transaction: tx, Detail: strconv.FormatUint(round, 10)}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("own marker behind another transaction's", func(t *testing.T) {
		h, plan := setup(t)
		appendEntry(t, h, plan, redemption.KindSweepSigned, [32]byte{0x0e}, 3)
		appendEntry(t, h, plan, redemption.KindSweepSigned, plan.BitcoinTransactionHash(), 7)
		round, ok, err := h.sweeper.signedRound(ctx, plan)
		if err != nil || !ok || round != 7 {
			t.Fatalf("signed round %d %v (%v), want 7", round, ok, err)
		}
	})
	t.Run("reconcile with an underivable round", func(t *testing.T) {
		h, plan := setup(t)
		appendEntry(t, h, plan, redemption.KindSweepAttempt, plan.BitcoinTransactionHash(), 7)
		h.sweeper.Schedule.RoundBlocks = 0
		if _, ok, err := h.sweeper.signedRound(ctx, plan); err == nil || ok {
			t.Fatal("an attempt of an underivable round was reconciled without error:", ok, err)
		}
	})
	t.Run("reconcile with an incomplete executor", func(t *testing.T) {
		h, plan := setup(t)
		appendEntry(t, h, plan, redemption.KindSweepAttempt, plan.BitcoinTransactionHash(), 7)
		h.sweeper.Base.Transport = nil
		if _, ok, err := h.sweeper.signedRound(ctx, plan); err == nil || ok {
			t.Fatal("an attempt was reconciled without an executor:", ok, err)
		}
	})
	for _, tc := range []struct {
		name string
		set  func(*testing.T, *sweepHost, *signing.Plan)
	}{
		{"nothing signed", func(*testing.T, *sweepHost, *signing.Plan) {}},
		{"ledger unreadable", func(_ *testing.T, h *sweepHost, _ *signing.Plan) { h.ledgerJ.failReadsAfter(0) }},
		{"marker without a durable result", func(t *testing.T, h *sweepHost, p *signing.Plan) {
			appendEntry(t, h, p, redemption.KindSweepSigned, p.BitcoinTransactionHash(), 9)
		}},
		{"marker of an underivable round", func(t *testing.T, h *sweepHost, p *signing.Plan) {
			appendEntry(t, h, p, redemption.KindSweepSigned, p.BitcoinTransactionHash(), 9)
			h.sweeper.Schedule.RoundBlocks = 0
		}},
		{"marker without an executor", func(t *testing.T, h *sweepHost, p *signing.Plan) {
			appendEntry(t, h, p, redemption.KindSweepSigned, p.BitcoinTransactionHash(), 9)
			h.sweeper.Base.Transport = nil
		}},
	} {
		t.Run("broadcast/"+tc.name, func(t *testing.T) {
			h, plan := setup(t)
			tc.set(t, h, plan)
			err := recovered(func() error { return h.sweeper.Broadcast(ctx, plan) })
			notPanic(t, err, tc.name)
			h.ledgerJ.failReadsFrom = 0
			if err == nil || kinds(h.entries(t, plan))[redemption.KindSweepBroadcast] != 0 {
				t.Fatalf("a sweep was broadcast with %s (%v)", tc.name, err)
			}
		})
	}
}

// TestSweeperResolveSteps: Resolve advances one sweep by one step from
// finalized chain state and the ledger (Sweeper.Resolve). Invariant: every
// deposit swept at the finalized head (head - FinalityBlocks, including
// head == FinalityBlocks) is the outcome and is recorded; a sweep visible
// only above the finalized head is not; a late-quarantine record makes the
// sweep quarantined; a signed marker without a durable result fails; an
// attempt of the current round waits without signing again; an attempt of
// an earlier round does not; every failed read or stop fails Resolve without
// starting a round. Refs M5-deposit mutation batch.
func TestSweeperResolveSteps(t *testing.T) {
	setup := func(t *testing.T) (*sweepHost, *signing.Plan, [32]byte) {
		h := newSweepHost(t)
		h.fund(1_000_000)
		key := h.deposit(1, 100_000, 85, [20]byte{})
		return h, planned(t, h.world, key), key
	}
	resolve := func(h *sweepHost, plan *signing.Plan) (Status, error) {
		var status Status
		err := recovered(func() error { var err error; status, err = h.sweeper.Resolve(ctx, plan); return err })
		return status, err
	}
	sweep := func(h *sweepHost, key [32]byte, at uint64) {
		r := h.chain.records[key]
		r.SweptAt = at
		h.chain.records[key] = r
	}
	t.Run("swept at the finalized head", func(t *testing.T) {
		h, plan, key := setup(t)
		sweep(h, key, 1)
		h.chain.head = 2 // head == FinalityBlocks: finalized head is block 0
		status, err := resolve(h, plan)
		notPanic(t, err, "outcome")
		if err != nil || status != StatusSwept || kinds(h.entries(t, plan))[redemption.KindSweepOutcome] != 1 {
			t.Fatalf("a finalized sweep was not the outcome: %s %v", status, err)
		}
	})
	t.Run("swept above the finalized head", func(t *testing.T) {
		h, plan, key := setup(t)
		h.fc.sweptFrom = map[[32]byte]uint64{key: 99} // head 100, finalized 98
		status, err := resolve(h, plan)
		notPanic(t, err, "unfinalized sweep")
		if status == StatusSwept || kinds(h.entries(t, plan))[redemption.KindSweepOutcome] != 0 {
			t.Fatalf("a sweep not yet final was recorded as the outcome: %s %v", status, err)
		}
	})
	t.Run("late quarantine", func(t *testing.T) {
		h, plan, _ := setup(t)
		if err := h.sweeper.Ledger.Append(ctx, redemption.Entry{Reservation: SweepID(plan), Kind: redemption.KindSweepLateQuarantined, Block: 100, Transaction: plan.BitcoinTransactionHash(), Detail: "9"}); err != nil {
			t.Fatal(err)
		}
		if status, err := resolve(h, plan); err != nil || status != StatusQuarantined {
			t.Fatalf("a late-quarantined sweep resolved as %s (%v)", status, err)
		}
	})
	t.Run("signed marker without a durable result", func(t *testing.T) {
		h, plan, _ := setup(t)
		if err := h.sweeper.Ledger.Append(ctx, redemption.Entry{Reservation: SweepID(plan), Kind: redemption.KindSweepSigned, Block: 100, Transaction: plan.BitcoinTransactionHash(), Detail: "9"}); err != nil {
			t.Fatal(err)
		}
		if status, err := resolve(h, plan); err == nil {
			t.Fatalf("a sweep with no durable result resolved as %s", status)
		}
	})
	t.Run("attempt of the current round waits", func(t *testing.T) {
		h, plan, _ := setup(t)
		if err := h.sweeper.Ledger.Append(ctx, redemption.Entry{Reservation: SweepID(plan), Kind: redemption.KindSweepAttempt, Block: 100, Transaction: plan.BitcoinTransactionHash(), Detail: "9"}); err != nil {
			t.Fatal(err)
		}
		status, err := resolve(h, plan)
		if err != nil || status != StatusWaiting || kinds(h.entries(t, plan))[redemption.KindSweepAttempt] != 1 {
			t.Fatalf("the current round's attempt was retried: %s %v %v", status, err, kinds(h.entries(t, plan)))
		}
	})
	t.Run("attempt of an earlier round signs again", func(t *testing.T) {
		h, plan, _ := setup(t)
		if err := h.sweeper.Ledger.Append(ctx, redemption.Entry{Reservation: SweepID(plan), Kind: redemption.KindSweepAttempt, Block: 90, Transaction: plan.BitcoinTransactionHash(), Detail: "8"}); err != nil {
			t.Fatal(err)
		}
		status, err := resolve(h, plan)
		notPanic(t, err, "next round")
		if !errors.Is(err, errNoWallet) || status == StatusWaiting || kinds(h.entries(t, plan))[redemption.KindSweepAttempt] != 2 {
			t.Fatalf("the next round did not start after an earlier round's attempt: %s %v %v", status, err, kinds(h.entries(t, plan)))
		}
	})
	for _, tc := range []struct {
		name string
		set  func(*sweepHost, [32]byte)
	}{
		{"head unreadable", func(h *sweepHost, _ [32]byte) { h.fc.set("Head", true) }},
		{"record unreadable", func(h *sweepHost, _ [32]byte) { h.fc.set("Deposit", true) }},
		{"stop at the outcome", func(h *sweepHost, k [32]byte) {
			r := h.chain.records[k]
			r.SweptAt = 1
			h.chain.records[k] = r
			h.sweeper.boundary = stopAtName("outcome-seen")
		}},
		{"ledger unreadable", func(h *sweepHost, _ [32]byte) { h.ledgerJ.failReadsAfter(0) }},
		{"signed round unreadable", func(h *sweepHost, _ [32]byte) { h.ledgerJ.failOnlyRead(2) }},
		{"current round unreadable", func(h *sweepHost, _ [32]byte) { h.fc.hashFail = func(n uint64) bool { return n == 90 } }},
		{"round attempts unreadable", func(h *sweepHost, _ [32]byte) { h.ledgerJ.failOnlyRead(3) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, plan, key := setup(t)
			tc.set(h, key)
			status, err := resolve(h, plan)
			notPanic(t, err, tc.name)
			h.ledgerJ.failReadsFrom, h.ledgerJ.failReadAt = 0, 0
			if err == nil {
				t.Fatalf("Resolve reported %q with %s", status, tc.name)
			}
			if got := kinds(h.entries(t, plan)); got[redemption.KindSweepAttempt] != 0 || got[redemption.KindSweepOutcome] != 0 {
				t.Fatalf("Resolve with %s recorded progress: %v", tc.name, got)
			}
		})
	}
	t.Run("sign failure is reported", func(t *testing.T) {
		h, plan, _ := setup(t)
		status, err := resolve(h, plan)
		notPanic(t, err, "sign failure")
		if !errors.Is(err, errNoWallet) || status == StatusSigned {
			t.Fatalf("a failed round was reported as %q (%v)", status, err)
		}
	})
}
