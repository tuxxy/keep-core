package deposit

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// staticAuthority is a fixture movement authority with a fixed answer.
type staticAuthority struct{ err error }

func (a staticAuthority) AuthorizeSweep(context.Context, *signing.Plan) (signing.ReservationReceipt, error) {
	if a.err != nil {
		return signing.ReservationReceipt{}, a.err
	}
	return signing.ReservationReceipt{Block: 1, Hash: [32]byte{1}}, nil
}

// lockWorld is a funded wallet whose main lock uses a fault journal.
func lockWorld(t *testing.T) (*world, *faultJournal, bitcoin.PreviousOutput) {
	t.Helper()
	w := newWorld(t)
	j := newFaultJournal()
	w.lock = &MainLock{Journal: j}
	w.planner.Lock = w.lock
	return w, j, w.fund(1_000_000)
}

func history(t *testing.T, w *world, main bitcoin.TransactionOutpoint) []Entry {
	t.Helper()
	h, err := (&MainLock{Journal: w.lock.Journal.(*faultJournal).memoryJournal}).History(ctx, w.id, OutpointKey(main))
	if err != nil {
		t.Fatal("fixture lock read:", err)
	}
	return h
}

// TestMainLockFailsClosedOnStorage: the VD-07 main lock is a durable
// history; a lock that cannot be read or written cannot grant anything.
// Invariant: an unreadable or corrupt history, a failed insert and a stop at
// any lock boundary fail every lock operation and leave the history as it
// was; nothing reads a failed history as a free main. The journal is the
// fault fixture. Refs M5-deposit mutation batch (KC-SF-V3 K-06, VD-07).
func TestMainLockFailsClosedOnStorage(t *testing.T) {
	r := [32]byte{0x99}
	type op struct {
		name string
		run  func(w *world, main bitcoin.PreviousOutput) error
	}
	ops := []op{
		{"history", func(w *world, m bitcoin.PreviousOutput) error {
			_, err := w.lock.History(ctx, w.id, OutpointKey(m.Outpoint))
			return err
		}},
		{"current", func(w *world, m bitcoin.PreviousOutput) error {
			_, err := w.lock.Current(ctx, w.id, OutpointKey(m.Outpoint))
			return err
		}},
		{"acquire redemption", func(w *world, m bitcoin.PreviousOutput) error {
			return w.lock.AcquireRedemption(ctx, w.id, m.Outpoint, r, 120)
		}},
		{"acquire sweep", func(w *world, m bitcoin.PreviousOutput) error {
			return w.lock.AcquireSweep(ctx, w.id, m.Outpoint, [32]byte{1})
		}},
		{"resolve", func(w *world, m bitcoin.PreviousOutput) error {
			return w.lock.Resolve(ctx, w.id, m.Outpoint, r, [32]byte{2})
		}},
		{"guard permit", func(w *world, m bitcoin.PreviousOutput) error {
			return Guard{Lock: w.lock}.Permit(ctx, paymentPlanOn(w.t, w, m), r)
		}},
		{"guard grant", func(w *world, m bitcoin.PreviousOutput) error {
			return Guard{Lock: w.lock}.Grant(ctx, paymentPlanOn(w.t, w, m), r, 0)
		}},
	}
	for _, fault := range []struct {
		name   string
		inject func(*testing.T, *world, *faultJournal, bitcoin.PreviousOutput)
		writes bool // only operations that write can fail on it
	}{
		{"unreadable", func(_ *testing.T, _ *world, j *faultJournal, _ bitcoin.PreviousOutput) { j.failReadsAfter(0) }, false},
		{"corrupt record", func(t *testing.T, w *world, j *faultJournal, m bitcoin.PreviousOutput) {
			// The next record of the history is not a lock entry (fixture
			// corruption).
			next := uint32(len(history(t, w, m.Outpoint)))
			if err := j.memoryJournal.RetainSigningRecord(ctx, mainLockID(w.id, OutpointKey(m.Outpoint), next), []byte("{not json")); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"insert refused", func(_ *testing.T, _ *world, j *faultJournal, _ bitcoin.PreviousOutput) {
			j.mu.Lock()
			j.failWrites = true
			j.mu.Unlock()
		}, true},
		{"stop at a lock boundary", func(_ *testing.T, w *world, _ *faultJournal, _ bitcoin.PreviousOutput) {
			w.lock.boundary = func(name string) error {
				if name == "before-main-lock" || name == "before-main-lock-transfer" {
					return errInjected
				}
				return nil
			}
		}, true},
	} {
		for _, o := range ops {
			if fault.writes && (o.name == "history" || o.name == "current" || o.name == "guard permit") {
				continue // read-only
			}
			if fault.name == "insert refused" && o.name == "acquire redemption" {
				continue // idempotent here, writes nothing; see "first attestation insert refused"
			}
			if fault.name == "stop at a lock boundary" && (o.name == "guard permit" || o.name == "guard grant") {
				continue // the guard has no boundaries of its own
			}
			t.Run(fault.name+"/"+o.name, func(t *testing.T) {
				w, j, main := lockWorld(t)
				// A redemption(r) record first, so resolve and the payment
				// grant would succeed on a healthy lock.
				if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 120); err != nil {
					t.Fatal(err)
				}
				if o.name == "acquire sweep" {
					if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 130, w.id, main.Outpoint, r); err != nil {
						t.Fatal(err)
					}
				}
				before := len(history(t, w, main.Outpoint))
				fault.inject(t, w, j, main)
				if err := recovered(func() error { return o.run(w, main) }); err == nil {
					t.Fatalf("%s succeeded on a %s main lock", o.name, fault.name)
				}
				j.mu.Lock()
				j.failReadsFrom, j.failWrites = 0, false
				j.mu.Unlock()
				w.lock.boundary = nil
				if fault.name != "corrupt record" {
					if after := len(history(t, w, main.Outpoint)); after != before {
						t.Fatalf("%s on a %s main lock changed the history: %d -> %d records", o.name, fault.name, before, after)
					}
				}
			})
		}
	}
	t.Run("first attestation insert refused", func(t *testing.T) {
		w, j, main := lockWorld(t)
		j.mu.Lock()
		j.failWrites = true
		j.mu.Unlock()
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 120); err == nil {
			t.Fatal("an attestation that was not recorded succeeded")
		}
	})
	t.Run("renewal insert refused", func(t *testing.T) {
		w, j, main := lockWorld(t)
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 120); err != nil {
			t.Fatal(err)
		}
		j.mu.Lock()
		j.failWrites = true
		j.mu.Unlock()
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 200); err == nil {
			t.Fatal("a renewal whose deadline was not recorded succeeded")
		}
	})
	t.Run("close with an unreadable custody output", func(t *testing.T) {
		w, _, main := lockWorld(t)
		c := newFaultChain(w.chain)
		c.set("CustodyOutput", true)
		if err := w.lock.Close(ctx, c, 130, w.id, main.Outpoint, "sweep proven"); err == nil {
			t.Fatal("a main was closed without reading its custody output")
		}
	})
}

// TestMainLockTransferRules: each transition of the VD-07 lock names its
// holder. Invariant: Resolve moves only redemption(r) or resolution(r) of
// the same r; ReleaseUnadmitted frees only redemption(r) of the same r; a
// free main can be neither resolved nor released; Close is idempotent and
// leaves the main Closed. Refs M5-deposit mutation batch (VD-07).
func TestMainLockTransferRules(t *testing.T) {
	r, other := [32]byte{0x99}, [32]byte{0x98}
	t.Run("resolve of a free main", func(t *testing.T) {
		w, _, main := lockWorld(t)
		if err := w.lock.Resolve(ctx, w.id, main.Outpoint, r, [32]byte{2}); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a free main was resolved:", err)
		}
	})
	t.Run("resolve of another reservation", func(t *testing.T) {
		w, _, main := lockWorld(t)
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 120); err != nil {
			t.Fatal(err)
		}
		if err := w.lock.Resolve(ctx, w.id, main.Outpoint, other, [32]byte{2}); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a main attested for one reservation was resolved for another:", err)
		}
		if err := w.lock.Resolve(ctx, w.id, main.Outpoint, r, [32]byte{2}); err != nil {
			t.Fatal(err)
		}
		if err := w.lock.Resolve(ctx, w.id, main.Outpoint, other, [32]byte{3}); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a main in resolution for one reservation was resolved for another:", err)
		}
		if cur, _ := w.lock.Current(ctx, w.id, OutpointKey(main.Outpoint)); cur.Purpose != Resolution || cur.Subject != r {
			t.Fatalf("lock left resolution(r): %+v", cur)
		}
	})
	t.Run("release of another reservation", func(t *testing.T) {
		w, _, main := lockWorld(t)
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 120); err != nil {
			t.Fatal(err)
		}
		if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 300, w.id, main.Outpoint, other); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a main attested for one reservation was released for another:", err)
		}
		if cur, _ := w.lock.Current(ctx, w.id, OutpointKey(main.Outpoint)); cur.Purpose != Redemption {
			t.Fatalf("a refused release moved the lock: %+v", cur)
		}
	})
	t.Run("close is idempotent", func(t *testing.T) {
		w, _, main := lockWorld(t)
		editMainOutput(w, func(o *redemption.CustodyOutput) { o.Spent = true })
		for i := 0; i < 2; i++ {
			if err := w.lock.Close(ctx, w.chain, 130, w.id, main.Outpoint, "sweep proven"); err != nil {
				t.Fatal(err)
			}
		}
		h := history(t, w, main.Outpoint)
		if len(h) != 1 || state(h).Purpose != Closed {
			t.Fatalf("closing a closed main changed it: %d records, state %q", len(h), state(h).Purpose)
		}
	})
}

// TestMainLockPlanRecords: plans recorded under a holder are a set of
// alternatives. Invariant: each distinct sweep plan on the main is recorded
// once, and acquiring an already-recorded plan adds nothing. Refs M5-deposit
// mutation batch (VD-07).
func TestMainLockPlanRecords(t *testing.T) {
	w, _, main := lockWorld(t)
	p1, p2 := [32]byte{1}, [32]byte{2}
	for _, p := range [][32]byte{p1, p2, p1, p2} {
		if err := w.lock.AcquireSweep(ctx, w.id, main.Outpoint, p); err != nil {
			t.Fatal(err)
		}
	}
	h := history(t, w, main.Outpoint)
	got := map[[32]byte]int{}
	for _, e := range h {
		got[e.Subject]++
	}
	if len(h) != 2 || got[p1] != 1 || got[p2] != 1 {
		t.Fatalf("sweep alternatives must be recorded once each, got %d records %v", len(h), got)
	}
}

// TestMovementLockAndEvidence: a K-07 movement takes the main only with the
// movement authority's evidence for a movement plan. Invariant:
// AuthorizeMovement accepts exactly a movement plan that the authority
// authorizes; it refuses a missing authority, an authority refusal, a K-06
// sweep and an incoming sweep (which is not a movement even when it spends
// a main). AcquireMovement refuses empty evidence; it supersedes a K-06
// sweep holder but never a redemption, and a sweep never takes a movement
// main. Refs M5-deposit mutation batch (VD-07, K-07).
func TestMovementLockAndEvidence(t *testing.T) {
	t.Run("authorization", func(t *testing.T) {
		w, _, main := lockWorld(t)
		movement := movementPlanOn(t, w, main)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		for _, tc := range []struct {
			name      string
			authority signing.SweepAuthority
			plan      *signing.Plan
			ok        bool
		}{
			{"movement authorized", staticAuthority{}, movement, true},
			{"no authority", nil, movement, false},
			{"authority refuses", staticAuthority{err: errInjected}, movement, false},
			{"deposit sweep", staticAuthority{}, planned(t, w, key), false},
			{"incoming sweep with a main", staticAuthority{}, incomingSweepPlanOn(t, w, &main), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var evidence CommittedMovement
				err := recovered(func() error {
					var err error
					evidence, err = AuthorizeMovement(ctx, tc.authority, tc.plan)
					return err
				})
				if tc.ok && (err != nil || evidence.plan != movement.Commitment() || evidence.main != OutpointKey(main.Outpoint)) {
					t.Fatalf("an authorized movement got no evidence: %+v %v", evidence, err)
				}
				if !tc.ok && err == nil {
					t.Fatalf("movement evidence issued for %s", tc.name)
				}
			})
		}
	})
	evidence := func(t *testing.T, w *world, main bitcoin.PreviousOutput) CommittedMovement {
		t.Helper()
		e, err := AuthorizeMovement(ctx, staticAuthority{}, movementPlanOn(t, w, main))
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	t.Run("empty evidence", func(t *testing.T) {
		w, _, _ := lockWorld(t)
		if err := w.lock.AcquireMovement(ctx, CommittedMovement{}); err == nil {
			t.Fatal("a movement took a main without evidence")
		}
	})
	t.Run("free main", func(t *testing.T) {
		w, _, main := lockWorld(t)
		if err := w.lock.AcquireMovement(ctx, evidence(t, w, main)); err != nil {
			t.Fatal("a committed movement was refused a free main:", err)
		}
		if err := w.lock.AcquireSweep(ctx, w.id, main.Outpoint, [32]byte{1}); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a sweep took a movement main:", err)
		}
	})
	t.Run("supersedes a sweep", func(t *testing.T) {
		w, _, main := lockWorld(t)
		if err := w.lock.AcquireSweep(ctx, w.id, main.Outpoint, [32]byte{1}); err != nil {
			t.Fatal(err)
		}
		if err := w.lock.AcquireMovement(ctx, evidence(t, w, main)); err != nil {
			t.Fatal("a committed movement did not supersede an aborted sweep:", err)
		}
		if cur, _ := w.lock.Current(ctx, w.id, OutpointKey(main.Outpoint)); cur.Purpose != MovementMain {
			t.Fatalf("lock is %q after the movement superseded the sweep", cur.Purpose)
		}
	})
	t.Run("never supersedes a redemption", func(t *testing.T) {
		w, _, main := lockWorld(t)
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, [32]byte{0x99}, 120); err != nil {
			t.Fatal(err)
		}
		if err := w.lock.AcquireMovement(ctx, evidence(t, w, main)); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a movement took a main attested for a redemption:", err)
		}
	})
}

// TestGuardPurposeDispatch: every grant is checked against the holder of the
// main it spends (VD-07). Invariant: a movement grant needs movement with
// that plan recorded; an incoming sweep with a main needs sweep with that
// plan recorded, and one without a main spends no main and is not gated; a
// payment grant needs redemption of its reservation; a resolution sweep
// needs resolution of its own reservation; an ordinary main sweep needs
// sweep with that plan recorded (a grant record of another plan does not
// count); a deposit-only sweep spends no main and is not gated. Grant
// refuses whatever Permit refuses and records nothing. Refs M5-deposit
// mutation batch (VD-07).
func TestGuardPurposeDispatch(t *testing.T) {
	r := [32]byte{0x99}
	guard := func(w *world) Guard { return Guard{Lock: w.lock} }
	check := func(t *testing.T, w *world, plan *signing.Plan, reservation [32]byte, ok bool, what string) {
		t.Helper()
		err := recovered(func() error { return guard(w).Permit(ctx, plan, reservation) })
		if ok && err != nil {
			t.Fatalf("%s refused: %v", what, err)
		}
		if !ok && !errors.Is(err, ErrMainBusy) {
			t.Fatalf("%s permitted: %v", what, err)
		}
	}
	t.Run("movement", func(t *testing.T) {
		w, _, main := lockWorld(t)
		movement := movementPlanOn(t, w, main)
		check(t, w, movement, [32]byte{}, false, "a movement on a free main")
		e, err := AuthorizeMovement(ctx, staticAuthority{}, movement)
		if err != nil {
			t.Fatal(err)
		}
		if err = w.lock.AcquireMovement(ctx, e); err != nil {
			t.Fatal(err)
		}
		check(t, w, movement, [32]byte{}, true, "the recorded movement")
		other := movementPlanOnFee(t, w, main, 2_000)
		check(t, w, other, [32]byte{}, false, "a movement not recorded under the lock")
	})
	t.Run("incoming sweep with a main", func(t *testing.T) {
		w, _, main := lockWorld(t)
		incoming := incomingSweepPlanOn(t, w, &main)
		e, err := AuthorizeMovement(ctx, staticAuthority{}, movementPlanOn(t, w, main))
		if err != nil {
			t.Fatal(err)
		}
		if err = w.lock.AcquireMovement(ctx, e); err != nil {
			t.Fatal(err)
		}
		check(t, w, incoming, [32]byte{}, false, "an incoming sweep of a movement main")
		w2, _, main2 := lockWorld(t)
		incoming2 := incomingSweepPlanOn(t, w2, &main2)
		if err = w2.lock.AcquireSweep(ctx, w2.id, main2.Outpoint, incoming2.Commitment()); err != nil {
			t.Fatal(err)
		}
		check(t, w2, incoming2, [32]byte{}, true, "the recorded incoming sweep")
	})
	t.Run("incoming sweep without a main", func(t *testing.T) {
		w, _, _ := lockWorld(t)
		check(t, w, incomingSweepPlanOn(t, w, nil), [32]byte{}, true, "an incoming sweep that spends no main")
	})
	t.Run("payment", func(t *testing.T) {
		w, _, main := lockWorld(t)
		payment := paymentPlanOn(t, w, main)
		check(t, w, payment, r, false, "a payment on a free main")
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 120); err != nil {
			t.Fatal(err)
		}
		check(t, w, payment, r, true, "a payment under its redemption")
		check(t, w, payment, [32]byte{0x98}, false, "a payment under another reservation's redemption")
	})
	t.Run("resolution of another reservation", func(t *testing.T) {
		w, _, main := lockWorld(t)
		w.chain.custody.ActiveReservation = r
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
		if err != nil {
			t.Fatal(err)
		}
		other := [32]byte{0x98}
		if err = w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, other, 120); err != nil {
			t.Fatal(err)
		}
		if err = w.lock.Resolve(ctx, w.id, main.Outpoint, other, plan.BitcoinTransactionHash()); err != nil {
			t.Fatal(err)
		}
		check(t, w, plan, SweepID(plan), false, "a resolution sweep under another reservation's resolution")
	})
	t.Run("ordinary main sweep", func(t *testing.T) {
		w, _, main := lockWorld(t)
		a := w.deposit(1, 100_000, 90, [20]byte{})
		b := w.deposit(2, 100_000, 90, [20]byte{})
		planA, planB := planned(t, w, a), planned(t, w, a, b)
		check(t, w, planA, SweepID(planA), false, "a main sweep on a free main")
		if err := w.lock.AcquireSweep(ctx, w.id, main.Outpoint, planA.Commitment()); err != nil {
			t.Fatal(err)
		}
		if err := guard(w).Grant(ctx, planA, SweepID(planA), 0); err != nil {
			t.Fatal(err)
		}
		check(t, w, planA, SweepID(planA), true, "the recorded main sweep")
		check(t, w, planB, SweepID(planB), false, "a main sweep recorded only as another plan's grant")
		before := len(history(t, w, main.Outpoint))
		if err := guard(w).Grant(ctx, planB, SweepID(planB), 0); !errors.Is(err, ErrMainBusy) {
			t.Fatal("a grant was issued for an unrecorded sweep:", err)
		}
		if after := len(history(t, w, main.Outpoint)); after != before {
			t.Fatalf("a refused grant was recorded: %d -> %d", before, after)
		}
	})
	t.Run("deposit-only sweep", func(t *testing.T) {
		w := newWorld(t)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan := planned(t, w, key)
		if plan.SweepMain() != nil {
			t.Fatal("fixture: an unfunded wallet's sweep has no main")
		}
		check(t, w, plan, SweepID(plan), true, "a deposit-only sweep")
	})
}

// movementPlanOnFee is movementPlanOn with another fee: a distinct
// alternative of the same movement.
func movementPlanOnFee(t *testing.T, w *world, main bitcoin.PreviousOutput, fee int64) *signing.Plan {
	t.Helper()
	target := xonly(testKey(33))
	script, _ := bitcoin.PayToTaproot(target)
	o := main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &o, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: main.Value - fee, PublicKeyScript: script}}}
	plan, err := signing.NewMovementPlan(signing.MovementProposal{Wallet: w.wallet, BitcoinGenesis: w.planner.Genesis, Generation: 1, StateVersion: 1, Period: 1, Main: main, Targets: []signing.MovementTarget{{Label: [20]byte{1}, DescriptorHash: [32]byte{2}, OutputKey: target}}, MaximumFee: 10_000, MaximumSatPerVByte: 100, Transaction: tx})
	if err != nil {
		t.Fatal("fixture movement plan:", err)
	}
	return plan
}
