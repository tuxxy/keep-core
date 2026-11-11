package redemption

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// TestDirectiveContainment (spec issue #3): a contained reservation is acted
// on only under a well-formed governance directive for that reservation that
// names its class and the matching action: rebroadcast for a payment, cancel
// for a cancellation. A missing, failed or malformed directive never allows
// the act.
func TestDirectiveContainment(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	cw := newCancelWorld(t, IntentQuarantined)
	payment, cancellation := cw.payment, cw.cancel(t, cw.payment, 400)
	good := func(a DirectiveAction) fixedDirectives {
		return fixedDirectives{d: Directive{Reservation: cw.id, Class: "fixture-class", Action: a}, ok: true}
	}
	for _, tc := range []struct {
		name    string
		source  Directives
		plan    *signing.Plan
		allowed bool
	}{
		{"payment, rebroadcast directive", good(DirectiveRebroadcast), payment, true},
		{"payment, cancel directive", good(DirectiveCancel), payment, false},
		{"cancellation, cancel directive", good(DirectiveCancel), cancellation, true},
		{"cancellation, rebroadcast directive", good(DirectiveRebroadcast), cancellation, false},
		{"no directive source", nil, payment, false},
		{"no directive", fixedDirectives{}, payment, false},
		{"directive source fails", fixedDirectives{err: errInjected}, payment, false},
		{"directive for another reservation", fixedDirectives{d: Directive{Reservation: [32]byte{1}, Class: "fixture-class", Action: DirectiveRebroadcast}, ok: true}, payment, false},
		{"directive naming no class", fixedDirectives{d: Directive{Reservation: cw.id, Action: DirectiveRebroadcast}, ok: true}, payment, false},
		{"directive of no action", fixedDirectives{d: Directive{Reservation: cw.id, Class: "fixture-class"}, ok: true}, payment, false},
		{"directive of an unknown action", fixedDirectives{d: Directive{Reservation: cw.id, Class: "fixture-class", Action: DirectiveCancel + 1}, ok: true}, cancellation, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			err := DirectiveContainment{Directives: tc.source}.AllowContained(ctx, tc.plan, cw.id)
			if tc.allowed && err != nil {
				t.Fatalf("valid directive refused: %v", err)
			}
			if !tc.allowed && err == nil {
				t.Fatal("contained reservation allowed without a matching directive")
			}
		})
	}
}

// TestResolveContainedReservation (spec issue #3): a Quarantined
// reservation is neither rebroadcast nor cancelled on the host's own
// initiative. Without a directive the host records an escalation and waits;
// a failed or malformed directive is an error; a directive acts only as it
// says (rebroadcast only a payment the host signed).
func TestResolveContainedReservation(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		source Directives
		signed bool
		want   Status
		fails  bool
	}{
		{"no directive source", nil, false, StatusAwaitingDirective, false},
		{"no directive", fixedDirectives{}, false, StatusAwaitingDirective, false},
		{"directive source fails", fixedDirectives{err: errInjected}, false, "", true},
		{"malformed directive", fixedDirectives{d: Directive{Reservation: [32]byte{0x5e}, Class: "fixture-class", Action: DirectiveCancel + 1}, ok: true}, false, "", true},
		{"rebroadcast directive, nothing signed", fixedDirectives{d: Directive{Reservation: [32]byte{0x5e}, Class: "fixture-class", Action: DirectiveRebroadcast}, ok: true}, false, StatusNeedsCancellation, false},
		{"cancel directive, payment signed", fixedDirectives{d: Directive{Reservation: [32]byte{0x5e}, Class: "fixture-class", Action: DirectiveCancel}, ok: true}, true, StatusNeedsCancellation, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, IntentQuarantined)
			rw.r.Directives = tc.source
			if tc.signed {
				rw.mark(t, KindSigned, "0") // planted marker, no durable result (fixture)
			}
			var status Status
			var err error
			noPanic(t, "Resolve", func() { status, err = rw.r.Resolve(ctx, rw.plan, rw.id) })
			if tc.fails {
				if err == nil {
					t.Fatalf("directive failure hidden: status %q", status)
				}
				return
			}
			if err != nil || status != tc.want {
				t.Fatalf("status %q %v, want %q", status, err, tc.want)
			}
			if tc.want == StatusAwaitingDirective && rw.count(t, KindEscalation, "containment: awaiting governance directive") != 1 {
				t.Fatal("waiting for a directive was not escalated")
			}
		})
	}
}

// TestResolveTimedOutReservation (spec issue #3, K-05 timeout policy): after
// the timeout the host records it; without a durable signed payment, or under
// AlwaysCancel, it moves to cancellation; under RebroadcastThenCancel with a
// signed payment it rebroadcasts until the chain-derived window (latest
// request timeout block + finality + CancelAfterBlocks) has passed, then
// escalates and cancels. A missing window, timeout block or request record
// is an error, never a rebroadcast.
func TestResolveTimedOutReservation(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	// Requests time out at blocks 97 and 95; finality 2, window 5: the
	// window has passed from block 104 on.
	for _, tc := range []struct {
		name   string
		head   uint64
		edit   func(*resolverWorld)
		signed bool
		want   Status
		window bool // escalated because the rebroadcast window elapsed
	}{
		{"nothing signed", 100, nil, false, StatusNeedsCancellation, false},
		{"signed, AlwaysCancel", 100, func(rw *resolverWorld) { rw.r.Policy = AlwaysCancel }, true, StatusNeedsCancellation, false},
		{"nothing signed, AlwaysCancel, no window configured", 100, func(rw *resolverWorld) {
			rw.r.Policy, rw.r.Schedule.CancelAfterBlocks = AlwaysCancel, 0
		}, false, StatusNeedsCancellation, false},
		{"signed, window just passed", 104, nil, true, StatusNeedsCancellation, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, IntentTimeoutPending)
			rw.setHead(tc.head)
			if tc.edit != nil {
				tc.edit(rw)
			}
			if tc.signed {
				rw.mark(t, KindSigned, "0") // planted marker, no durable result (fixture)
			}
			status, err := rw.r.Resolve(ctx, rw.plan, rw.id)
			if err != nil || status != tc.want {
				t.Fatalf("status %q %v, want %q", status, err, tc.want)
			}
			if rw.count(t, KindTimeoutSeen, "TimeoutPending") != 1 {
				t.Fatal("timeout not recorded")
			}
			if got := rw.count(t, KindEscalation, "rebroadcast window elapsed") == 1; got != tc.window {
				t.Fatalf("window escalation recorded %v, want %v", got, tc.window)
			}
			if !tc.window && rw.count(t, KindEscalation, "cancellation required") != 1 {
				t.Fatal("cancellation not escalated")
			}
		})
	}
	t.Run("signed, last block inside the window", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentTimeoutPending)
		rw.setHead(103)
		rw.mark(t, KindSigned, "0") // planted marker, no durable result (fixture)
		status, err := rw.r.Resolve(ctx, rw.plan, rw.id)
		// The rebroadcast is attempted; the executor refuses it because the
		// fixture holds no durable result.
		if !errors.Is(err, signing.ErrUnauthorized) {
			t.Fatalf("inside the window the payment was not rebroadcast: %q %v", status, err)
		}
		if rw.count(t, KindEscalation, "") != 0 {
			t.Fatal("escalated inside the rebroadcast window")
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*resolverWorld)
	}{
		{"no window configured", func(rw *resolverWorld) { rw.r.Schedule.CancelAfterBlocks = 0 }},
		{"no timeout block", func(rw *resolverWorld) { setRequests(rw.world, func(r *Request) { r.TimeoutBlock = 0 }) }},
		{"request read fails", func(rw *resolverWorld) { rw.fc.requestErr = errInjected }},
	} {
		t.Run("signed, "+tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, IntentTimeoutPending)
			tc.edit(rw)
			rw.mark(t, KindSigned, "0") // planted marker, no durable result (fixture)
			status, err := rw.r.Resolve(ctx, rw.plan, rw.id)
			if err == nil || errors.Is(err, signing.ErrUnauthorized) {
				t.Fatalf("%s: status %q, %v (a rebroadcast must not be attempted)", tc.name, status, err)
			}
		})
	}
}

// TestResolveFailsClosed: every failed read, write, derivation or crash
// boundary while resolving a reservation is returned as an error, never as
// a status with no error.
func TestResolveFailsClosed(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	invalidBase := func(rw *resolverWorld) { rw.r.Base.MinimumConfirmations = 0 }
	shrunk := func(rw *resolverWorld) { rw.r.Wallet, rw.fc.certified = rw.wallet, []uint16{1} }
	full := func(capacity int, marks ...Kind) func(*resolverWorld) {
		return func(rw *resolverWorld) {
			rw.r.Ledger = Ledger{Journal: rw.journal, Capacity: capacity, Reserve: capacity}
			// The timeout record is the one Resolve itself writes (no
			// transaction), so it is idempotent and the next write is the
			// one that meets the full ledger.
			for _, k := range marks {
				switch k {
				case KindTimeoutSeen:
					if err := rw.r.Ledger.Append(ctx, Entry{Reservation: rw.id, Kind: k, Block: 100, Detail: "TimeoutPending"}); err != nil {
						rw.t.Fatal(err)
					}
				case KindOutcome:
					rw.mark(nil, k, "fixture")
				default:
					rw.mark(nil, k, "0")
				}
			}
		}
	}
	for _, tc := range []struct {
		name  string
		state IntentState
		head  uint64
		edit  func(*resolverWorld)
	}{
		{"head read fails", IntentReserved, 100, func(rw *resolverWorld) { rw.fc.headErr = errInjected }},
		{"record read fails", IntentReserved, 100, func(rw *resolverWorld) { rw.fc.intentErr = errInjected }},
		{"record admits another plan", IntentReserved, 100, func(rw *resolverWorld) {
			i := rw.chain.intents[rw.id]
			i.Certificate.IntentCommitment[0] ^= 1
			rw.chain.intents[rw.id] = i
		}},
		{"crash at the outcome", IntentPaid, 100, func(rw *resolverWorld) { rw.r.boundary = stopAt("outcome-seen") }},
		{"reserved, recorded round cannot be reopened", IntentReserved, 100, func(rw *resolverWorld) { rw.mark(nil, KindSignAttempt, "0"); invalidBase(rw) }},
		{"reserved, certified seats below the threshold", IntentReserved, 100, shrunk},
		{"reserved, signed marker without a durable result", IntentReserved, 100, func(rw *resolverWorld) { rw.mark(nil, KindSigned, "0") }},
		{"timed out, crash at the timeout", IntentTimeoutPending, 100, func(rw *resolverWorld) { rw.r.boundary = stopAt("timeout-seen") }},
		{"timed out, ledger full", IntentTimeoutPending, 100, full(1, KindOutcome)},
		{"timed out, recorded round cannot be reopened", IntentTimeoutPending, 100, func(rw *resolverWorld) { rw.mark(nil, KindSignAttempt, "0"); invalidBase(rw) }},
		{"timed out, recorded round has no certified signers", IntentTimeoutPending, 100, func(rw *resolverWorld) { rw.mark(nil, KindSignAttempt, "0"); shrunk(rw) }},
		{"timed out, window passed, ledger full", IntentTimeoutPending, 104, full(2, KindSigned, KindTimeoutSeen)},
		{"timed out, nothing signed, ledger full", IntentTimeoutPending, 100, full(1, KindTimeoutSeen)},
		{"timed out, rebroadcast round has no admission log", IntentTimeoutPending, 103, func(rw *resolverWorld) {
			rw.mark(nil, KindSigned, "0")
			delete(rw.chain.admissions, rw.id)
		}},
		{"timed out, rebroadcast executor cannot be built", IntentTimeoutPending, 103, func(rw *resolverWorld) { rw.mark(nil, KindSigned, "0"); invalidBase(rw) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, tc.state)
			rw.setHead(tc.head)
			tc.edit(rw)
			var status Status
			var err error
			noPanic(t, "Resolve", func() { status, err = rw.r.Resolve(ctx, rw.plan, rw.id) })
			if err == nil {
				t.Fatalf("failure hidden behind status %q", status)
			}
		})
	}
	t.Run("settled reservation records its outcome", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentPaid)
		status, err := rw.r.Resolve(ctx, rw.plan, rw.id)
		if err != nil || status != StatusSettled || rw.count(t, KindOutcome, "Paid") != 1 {
			t.Fatalf("status %q %v", status, err)
		}
	})
}

// TestResolveReservedSignsOncePerRound (K-05 terminal attempts): a Reserved
// reservation with no durable signature records the current round and runs
// one attempt for it; a failed attempt is reported and is terminal, so the
// same round waits instead of retrying, and the next round makes one fresh
// attempt. A failed attempt leaves no signed, late or possible-release
// record. A durable signed marker routes to broadcast, never to signing
// again; a late signature routes to cancellation. The fixture executor has
// no key, so every attempt fails.
func TestResolveReservedSignsOncePerRound(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	rw := newResolverWorld(t, IntentReserved)
	var status Status
	var err error
	noPanic(t, "Resolve", func() { status, err = rw.r.Resolve(ctx, rw.plan, rw.id) })
	if err == nil {
		t.Fatalf("a failed signing attempt was reported as %q", status)
	}
	if rw.count(t, KindSignAttempt, "0") != 1 || rw.count(t, KindCancellationAttempt, "") != 0 {
		t.Fatalf("round 0 attempt not recorded as a payment attempt: %+v", rw.entries(t))
	}
	for _, k := range []Kind{KindSigned, KindLateQuarantined, KindPossibleRelease} {
		if rw.count(t, k, "") != 0 {
			t.Fatalf("a failed attempt with nothing retained recorded %s", k)
		}
	}
	if status, err = rw.r.Resolve(ctx, rw.plan, rw.id); err != nil || status != StatusWaiting {
		t.Fatalf("same round: %q %v, want waiting", status, err)
	}
	if rw.count(t, KindSignAttempt, "") != 1 {
		t.Fatal("the failed round was retried")
	}
	rw.setHead(102) // round 1
	if _, err = rw.r.Resolve(ctx, rw.plan, rw.id); err == nil {
		t.Fatal("round 1 attempt failure hidden")
	}
	if rw.count(t, KindSignAttempt, "1") != 1 || rw.count(t, KindSignAttempt, "") != 2 {
		t.Fatalf("round 1 did not make one fresh attempt: %+v", rw.entries(t))
	}

	t.Run("signed marker", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentReserved)
		// A record of another transaction comes first.
		if err := rw.r.Ledger.Append(ctx, Entry{Reservation: rw.id, Kind: KindSignAttempt, Transaction: [32]byte{0xee}, Detail: "0"}); err != nil {
			t.Fatal(err)
		}
		rw.mark(t, KindSigned, "0") // planted marker, no durable result (fixture)
		if _, err := rw.r.Resolve(ctx, rw.plan, rw.id); err == nil {
			t.Fatal("broadcast without a durable result reported success")
		}
		if rw.count(t, KindSignAttempt, "") != 0 {
			t.Fatal("a signed payment was signed again")
		}
	})
	t.Run("signed marker of another transaction", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentReserved)
		if err := rw.r.Ledger.Append(ctx, Entry{Reservation: rw.id, Kind: KindSigned, Transaction: [32]byte{0xee}, Detail: "0"}); err != nil {
			t.Fatal(err)
		}
		_, _ = rw.r.Resolve(ctx, rw.plan, rw.id)
		if rw.count(t, KindSignAttempt, "0") != 1 {
			t.Fatal("another transaction's signature was taken for this plan's")
		}
	})
	t.Run("late signature", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentReserved)
		rw.mark(t, KindLateQuarantined, "0")
		if status, err := rw.r.Resolve(ctx, rw.plan, rw.id); err != nil || status != StatusNeedsCancellation {
			t.Fatalf("late signature: %q %v", status, err)
		}
		if rw.count(t, KindSignAttempt, "") != 0 {
			t.Fatal("signed again after a late signature")
		}
	})
}

// TestResolveEveryLedgerReadFault (K-05 F2 recovery rule): any single
// failed ledger read before the round's attempt record is written stops
// Resolve with an error and writes no new attempt record; a transient fault
// never makes the host skip checking an earlier round for a durable result.
// Round 0 was attempted already and the head is in round 1. The fixture
// fails exactly the n-th ledger read, for every n until the fault falls
// after the first ledger write.
func TestResolveEveryLedgerReadFault(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	covered := 0
	for n := 1; ; n++ {
		rw := newResolverWorld(t, IntentReserved)
		rw.mark(t, KindSignAttempt, "0")
		rw.setHead(102)
		j := &countingJournal{RecordJournal: rw.journal, n: n}
		rw.r.Ledger = Ledger{Journal: j}
		var err error
		noPanic(t, "Resolve", func() { _, err = rw.r.Resolve(ctx, rw.plan, rw.id) })
		if !j.hit {
			break
		}
		covered = n
		rw.r.Ledger = Ledger{Journal: rw.journal}
		if err == nil {
			t.Fatalf("ledger read %d failed but Resolve returned no error", n)
		}
		if got := rw.count(t, KindSignAttempt, ""); got != 1 {
			t.Fatalf("ledger read %d failed but Resolve wrote an attempt (%d attempt records)", n, got)
		}
	}
	if covered < 3 {
		t.Fatalf("only %d ledger reads were faulted before the first write", covered)
	}
}

// countingChain is a C-04 reader (fixture) that fails exactly the n-th read
// of one agreement input: the admission log, the reservation record or the
// readiness certificate.
type countingChain struct {
	*faultChain
	target   string
	n, calls int
}

func (c *countingChain) tick(name string) bool {
	if name != c.target {
		return false
	}
	c.calls++
	return c.calls == c.n
}

func (c *countingChain) Admission(ctx context.Context, id [32]byte) (Admission, error) {
	if c.tick("admission") {
		return Admission{}, errInjected
	}
	return c.faultChain.Admission(ctx, id)
}

func (c *countingChain) Intent(ctx context.Context, at uint64, id [32]byte) (Intent, error) {
	if c.tick("intent") {
		return Intent{}, errInjected
	}
	return c.faultChain.Intent(ctx, at, id)
}

func (c *countingChain) CertifiedSeats(ctx context.Context, w [32]byte) ([]uint16, error) {
	if c.tick("certified") {
		return nil, errInjected
	}
	return c.faultChain.CertifiedSeats(ctx, w)
}

// TestResolveEveryAgreementReadFault (A-K04-AGREED-EXECUTOR-CONFIG): any
// single failed read of an agreement input (admission log, reservation
// record, readiness certificate) stops Resolve with an error; it is never
// hidden behind a waiting status. The Reserved branch derives the current
// round without building an executor, so only the read error itself can
// stop it. Round 0 was attempted, the head is still in round 0 and the
// signers rotate over the certified seats. The fixture fails exactly the
// n-th read of one input, for every n until that read no longer happens.
func TestResolveEveryAgreementReadFault(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	for _, target := range []string{"admission", "intent", "certified"} {
		faulted := 0
		for n := 1; ; n++ {
			rw := newResolverWorld(t, IntentReserved)
			rw.r.Wallet = rw.wallet
			rw.mark(t, KindSignAttempt, "0")
			c := &countingChain{faultChain: rw.fc, target: target, n: n}
			rw.r.Chain = c
			var status Status
			var err error
			noPanic(t, "Resolve", func() { status, err = rw.r.Resolve(ctx, rw.plan, rw.id) })
			if c.calls < n {
				break
			}
			faulted = n
			if err == nil {
				t.Fatalf("%s read %d failed but Resolve returned %q and no error", target, n, status)
			}
		}
		if faulted < 2 {
			t.Fatalf("only %d %s reads were faulted", faulted, target)
		}
	}
}

// TestResolverSignAndBroadcastFailClosed: Sign and Broadcast return an
// error, never a missing result with no error, when any read, derivation,
// executor configuration, ledger write or crash boundary fails, and
// Broadcast needs a durable signed result.
func TestResolverSignAndBroadcastFailClosed(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		edit func(*resolverWorld)
	}{
		{"head read fails", func(rw *resolverWorld) { rw.fc.headErr = errInjected }},
		{"no admission log", func(rw *resolverWorld) { delete(rw.chain.admissions, rw.id) }},
		{"crash after the agreement", func(rw *resolverWorld) { rw.r.boundary = stopAt("agreement-derived") }},
		{"crash before signing", func(rw *resolverWorld) { rw.r.boundary = stopAt("before-sign") }},
		{"executor cannot be built", func(rw *resolverWorld) { rw.r.Base.MinimumConfirmations = 0 }},
		{"ledger full", func(rw *resolverWorld) { rw.r.Ledger = Ledger{Journal: rw.journal, Capacity: 1} }},
	} {
		t.Run("sign/"+tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, IntentReserved)
			tc.edit(rw)
			if result, err := rw.r.Sign(ctx, rw.plan, rw.id); err == nil {
				t.Fatalf("Sign reported success: %v", result != nil)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*resolverWorld)
	}{
		{"ledger unreadable", func(rw *resolverWorld) { _ = rw.journal.RetainSigningRecord(ctx, entryID(rw.id, 0), []byte("{")) }},
		{"nothing signed", func(*resolverWorld) {}},
		{"signed, no admission log", func(rw *resolverWorld) { rw.mark(nil, KindSigned, "0"); delete(rw.chain.admissions, rw.id) }},
		{"signed, executor cannot be built", func(rw *resolverWorld) { rw.mark(nil, KindSigned, "0"); rw.r.Base.MinimumConfirmations = 0 }},
		{"signed marker, no durable result", func(rw *resolverWorld) { rw.mark(nil, KindSigned, "0") }},
	} {
		t.Run("broadcast/"+tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, IntentReserved)
			tc.edit(rw)
			if err := rw.r.Broadcast(ctx, rw.plan, rw.id); err == nil {
				t.Fatal("Broadcast reported success")
			}
		})
	}
}

// TestResolverAgreement (A-K04-AGREED-EXECUTOR-CONFIG, ADV-D2): every
// operator derives the executor configuration from the admission log alone:
// a payment from the reservation's admission and seats, a cancellation from
// its own admission and the seats of that exact alternative, each under its
// own purpose; with the wallet set, payment signers rotate per round over the
// readiness-certified seats. The derived session, start block and seats have
// no public observable without a worker (Run fails before using them), so
// those cases call the unexported derivation; every refusal goes through
// Sign, which must fail and record no attempt.
func TestResolverAgreement(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	t.Run("payment", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentReserved)
		adm := rw.chain.admissions[rw.id]
		head := Head{Number: 100, Hash: rw.chain.hash(100)}
		got, err := rw.r.agreement(ctx, rw.plan, rw.id, head, nil)
		want, _ := Agree(rw.r.Schedule, rw.chain.ChainID(), rw.chain.Bridge(), signing.Redemption, rw.id, adm, []uint16{1, 2}, 100)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("agreement %+v %v, want %+v", got, err, want)
		}
		round := uint64(3)
		got, err = rw.r.agreement(ctx, rw.plan, rw.id, head, &round)
		want, _ = AgreeRound(rw.r.Schedule, rw.chain.ChainID(), rw.chain.Bridge(), signing.Redemption, rw.id, adm, []uint16{1, 2}, 3)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("round 3 agreement %+v %v, want %+v", got, err, want)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentTimeoutPending)
		walletScript, _ := bitcoin.PayToTaproot(rw.wallet.OutputKey)
		point := rw.main.Outpoint
		cancel, err := signing.NewCancellationPlan(rw.plan, &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: 999_000, PublicKeyScript: walletScript}}})
		if err != nil {
			t.Fatal(err)
		}
		hash := cancel.BitcoinTransactionHash()
		rw.chain.alternatives[rw.id] = []Alternative{{TransactionHash: [32]byte{1}, Seats: []uint16{9}}, {TransactionHash: hash, Seats: []uint16{1, 3}}, {TransactionHash: [32]byte{2}, Seats: []uint16{7}}}
		var key [64]byte
		copy(key[:32], rw.id[:])
		copy(key[32:], hash[:])
		adm := Admission{ReservationID: rw.id, TransactionHash: hash, Block: 95, BlockHash: rw.chain.hash(95)}
		rw.chain.cancellation[key] = adm
		head := Head{Number: 100, Hash: rw.chain.hash(100)}
		got, err := rw.r.agreement(ctx, cancel, rw.id, head, nil)
		want, _ := Agree(rw.r.Schedule, rw.chain.ChainID(), rw.chain.Bridge(), signing.Cancellation, rw.id, adm, []uint16{1, 3}, 100)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("cancellation agreement %+v %v, want %+v", got, err, want)
		}
		for _, tc := range []struct {
			name string
			edit func()
			undo func()
		}{
			{"no such alternative", func() { rw.chain.alternatives[rw.id] = rw.chain.alternatives[rw.id][:1] }, nil},
			{"alternatives read fails", func() { rw.fc.alternativesErr = errInjected }, func() { rw.fc.alternativesErr = nil }},
			{"no cancellation log", func() { delete(rw.chain.cancellation, key) }, nil},
		} {
			tc.edit()
			before := len(rw.entries(t))
			noPanic(t, "Sign", func() {
				if result, err := rw.r.Sign(ctx, cancel, rw.id); err == nil {
					t.Fatalf("%s: cancellation signed: %v", tc.name, result != nil)
				}
			})
			if len(rw.entries(t)) != before {
				t.Fatalf("%s: an attempt was recorded without an agreement", tc.name)
			}
			if tc.undo != nil {
				tc.undo()
			}
		}
	})
	t.Run("payment without its records", func(t *testing.T) {
		defer guard(t)
		for _, tc := range []struct {
			name string
			edit func(*resolverWorld)
		}{
			{"no admission log", func(rw *resolverWorld) { delete(rw.chain.admissions, rw.id) }},
			{"record read fails", func(rw *resolverWorld) { rw.fc.intentErr = errInjected }},
		} {
			rw := newResolverWorld(t, IntentReserved)
			tc.edit(rw)
			noPanic(t, "Sign", func() {
				if result, err := rw.r.Sign(ctx, rw.plan, rw.id); err == nil {
					t.Fatalf("%s: payment signed: %v", tc.name, result != nil)
				}
			})
			if n := len(rw.entries(t)); n != 0 {
				t.Fatalf("%s: %d ledger records written without an agreement", tc.name, n)
			}
		}
	})
	t.Run("rotating payment signers", func(t *testing.T) {
		defer guard(t)
		rw := newResolverWorld(t, IntentReserved)
		rw.r.Wallet = rw.wallet
		certified := []uint16{1, 2, 3}
		adm := rw.chain.admissions[rw.id]
		seen := map[string]bool{}
		for _, number := range []uint64{92, 100, 105, 112, 125, 133} {
			head := Head{Number: number, Hash: rw.chain.hash(number)}
			round := (number - 92) / 10
			got, err := rw.r.agreement(ctx, rw.plan, rw.id, head, nil)
			want, _ := Agree(rw.r.Schedule, rw.chain.ChainID(), rw.chain.Bridge(), signing.Redemption, rw.id, adm, SelectSeats(rw.wallet, certified, round), number)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("head %d: agreement %+v %v, want %+v", number, got, err, want)
			}
			got, err = rw.r.agreement(ctx, rw.plan, rw.id, Head{Number: 100}, &round)
			want, _ = AgreeRound(rw.r.Schedule, rw.chain.ChainID(), rw.chain.Bridge(), signing.Redemption, rw.id, adm, SelectSeats(rw.wallet, certified, round), round)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("explicit round %d: agreement %+v %v, want %+v", round, got, err, want)
			}
			seen[describeSeats(got.Selected)] = true
		}
		if len(seen) < 3 {
			t.Fatalf("signers did not rotate: %v", seen)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*resolverWorld)
	}{
		{"certificate read fails", func(rw *resolverWorld) { rw.fc.certifiedErr = errInjected }},
		{"certificate below the threshold", func(rw *resolverWorld) { rw.fc.certified = []uint16{2} }},
		{"no round length", func(rw *resolverWorld) { rw.r.Schedule.RoundBlocks = 0 }},
	} {
		t.Run("rotating payment signers/"+tc.name, func(t *testing.T) {
			defer guard(t)
			rw := newResolverWorld(t, IntentReserved)
			rw.r.Wallet = rw.wallet
			rw.setHead(105)
			tc.edit(rw)
			noPanic(t, "Sign", func() {
				if result, err := rw.r.Sign(ctx, rw.plan, rw.id); err == nil {
					t.Fatalf("payment signed: %v", result != nil)
				}
			})
			if n := len(rw.entries(t)); n != 0 {
				t.Fatalf("%d ledger records written without signers", n)
			}
		})
	}
}

func describeSeats(s []uint16) string {
	out := ""
	for _, v := range s {
		out += string(rune('0' + v))
	}
	return out
}
