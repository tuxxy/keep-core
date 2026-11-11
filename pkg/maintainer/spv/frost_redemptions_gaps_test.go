package spv

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// Mutation batch M8 (FrostRedemptionMaintainer). Fixtures are in
// frost_gaps_fixture_test.go.

// redemptionWorld: one wallet's admissions, intents, alternatives and
// requests. Confirmed transactions have six confirmations; an unconfirmed
// one is unknown to Bitcoin.
type redemptionWorld struct {
	f      *faults
	bridge *fakeBridge
	btc    *proofBitcoin
	m      FrostRedemptionMaintainer
}

func newRedemptionWorld(t *testing.T) *redemptionWorld {
	w := &redemptionWorld{f: &faults{}}
	w.bridge = newFakeBridge(w.f)
	w.btc = newProofBitcoin(t, w.f)
	w.m = FrostRedemptionMaintainer{Chain: w.bridge, Bitcoin: w.btc, Confirmations: 2, Wallet: [32]byte{0x3e}}
	return w
}

func id(tag byte) [32]byte { return [32]byte{tag} }

// actionSet renders actions or submissions as a sorted set.
func actionSet(actions []FrostAction) []string {
	var out []string
	for _, a := range actions {
		out = append(out, fmt.Sprintf("%s r%x q%x t%x", a.Kind, a.Reservation[:1], a.Request[:1], a.Transaction[:1]))
	}
	sort.Strings(out)
	return out
}

func sentSet(sent []submission) []string {
	kinds := map[string]string{
		"payment": "payment-proof", "cancellation": "cancellation-proof", "timeout": "timeout",
		"unreserved-refund": "unreserved-refund", "resolved-refund": "resolved-refund", "budget-claim": "budget-claim",
	}
	var out []string
	for _, s := range sent {
		out = append(out, fmt.Sprintf("%s r%x q%x t%x", kinds[s.kind], s.reservation[:1], s.request[:1], s.transaction[:1]))
	}
	sort.Strings(out)
	return out
}

func contains(set []string, s string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- ProveSettlements ---------------------------------------------------
//
// Invariant (KC-SF-V3 K-05 public maintainer path; ProveSettlements doc):
// for every unresolved admitted reservation, exactly one inclusion proof is
// submitted once a transaction is confirmed to the Bridge's proof depth: the
// payment if it is confirmed, otherwise the first confirmed admitted
// cancellation alternative. A resolved reservation and an unconfirmed
// transaction get none. A failed read or refused submission is an error.

// settlementWorld: admissions in order
//   - 0x01 resolved (Paid), its payment confirmed: skipped;
//   - 0x02 unresolved, payment confirmed: payment proof;
//   - 0x03 unresolved, payment unconfirmed; alternatives: unconfirmed,
//     confirmed (proven), confirmed (not proven, the first one wins);
//   - 0x04 unresolved, nothing confirmed: nothing.
func settlementWorld(t *testing.T) (*redemptionWorld, []string, []string) {
	w := newRedemptionWorld(t)
	paid := w.btc.confirm(t, gapsTx(0x21), 6)
	payment := w.btc.confirm(t, gapsTx(0x22), 6)
	alt2 := w.btc.confirm(t, gapsTx(0x32), 6)
	alt3 := w.btc.confirm(t, gapsTx(0x33), 6)
	unconfirmed := [32]byte(gapsTx(0x99).Hash())
	w.bridge.admissions = []redemption.Admission{{ReservationID: id(1)}, {ReservationID: id(2)}, {ReservationID: id(3)}, {ReservationID: id(4)}}
	w.bridge.intents[id(1)] = redemption.Intent{ID: id(1), State: redemption.IntentPaid, TransactionHash: paid}
	w.bridge.intents[id(2)] = redemption.Intent{ID: id(2), State: redemption.IntentReserved, TransactionHash: payment}
	w.bridge.intents[id(3)] = redemption.Intent{ID: id(3), State: redemption.IntentTimeoutPending, TransactionHash: unconfirmed}
	w.bridge.intents[id(4)] = redemption.Intent{ID: id(4), State: redemption.IntentQuarantined, TransactionHash: unconfirmed}
	w.bridge.alternatives[id(3)] = []redemption.Alternative{{TransactionHash: unconfirmed}, {TransactionHash: alt2}, {TransactionHash: alt3}}
	w.bridge.alternatives[id(4)] = []redemption.Alternative{{TransactionHash: unconfirmed}}
	want := actionSet([]FrostAction{
		{Kind: "payment-proof", Reservation: id(2), Transaction: payment},
		{Kind: "cancellation-proof", Reservation: id(3), Transaction: alt2},
	})
	// With a confirmations read failing, an item may wait or fall to the
	// next confirmed alternative, but nothing else may be proven and each
	// reservation gets at most one proof.
	allowed := append(append([]string{}, want...), actionSet([]FrostAction{{Kind: "cancellation-proof", Reservation: id(3), Transaction: alt3}})...)
	return w, want, allowed
}

func TestFrostSettlementProofs(t *testing.T) {
	t.Run("one proof per unresolved confirmed reservation", func(t *testing.T) {
		w, want, _ := settlementWorld(t)
		var actions []FrostAction
		err := recovered(func() error {
			var err error
			actions, err = w.m.ProveSettlements(context.Background())
			return err
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := actionSet(actions); !sameSet(got, want) {
			t.Fatalf("actions %v, expected %v", got, want)
		}
		if got := sentSet(w.bridge.sent); !sameSet(got, want) {
			t.Fatalf("submissions %v, expected %v", got, want)
		}
	})
	t.Run("zero confirmations is refused", func(t *testing.T) {
		w, _, _ := settlementWorld(t)
		w.m.Confirmations = 0
		if _, err := w.m.ProveSettlements(context.Background()); err == nil || len(w.bridge.sent) != 0 {
			t.Fatalf("error %v, %d submissions; expected an error and none", err, len(w.bridge.sent))
		}
	})
	for _, kind := range []string{"payment", "cancellation"} {
		t.Run("refused "+kind+" proof is an error", func(t *testing.T) {
			w, _, _ := settlementWorld(t)
			w.bridge.submit = func(s submission) error {
				if s.kind == kind {
					return errors.New("fixture: refused")
				}
				return nil
			}
			if _, err := w.m.ProveSettlements(context.Background()); err == nil {
				t.Fatalf("a refused %s proof was not reported", kind)
			}
		})
	}
	t.Run("Bitcoin returning another transaction is refused", func(t *testing.T) {
		w, _, _ := settlementWorld(t)
		for h := range map[[32]byte]bool{w.bridge.intents[id(2)].TransactionHash: true, w.bridge.alternatives[id(3)][1].TransactionHash: true} {
			w.btc.substitute[[32]byte(h)] = gapsTx(0x77)
		}
		if _, err := w.m.ProveSettlements(context.Background()); err == nil || len(w.bridge.sent) != 0 {
			t.Fatalf("error %v, %d submissions; expected an error and none", err, len(w.bridge.sent))
		}
	})
	t.Run("every failed read is an error", func(t *testing.T) {
		for n := 1; n < 500; n++ {
			w, _, allowed := settlementWorld(t)
			w.f.failAt = n
			err := recovered(func() error { _, err := w.m.ProveSettlements(context.Background()); return err })
			if !w.f.fired {
				if n == 1 {
					t.Fatal("fixture: no read was made")
				}
				return
			}
			// A failed confirmations read is a transaction Bitcoin does not
			// know yet (frostProofDepth): that item waits, so the run may
			// succeed, but it never proves anything outside the expected set.
			if w.f.firedOp == "confirmations" {
				perReservation := map[byte]int{}
				for _, s := range w.bridge.sent {
					perReservation[s.reservation[0]]++
				}
				for _, s := range sentSet(w.bridge.sent) {
					if !contains(allowed, s) {
						t.Fatalf("read %d (confirmations) failed, and %s was submitted", n, s)
					}
				}
				for r, k := range perReservation {
					if k > 1 {
						t.Fatalf("read %d (confirmations) failed, and reservation %x got %d proofs", n, r, k)
					}
				}
				continue
			}
			if err == nil {
				t.Fatalf("read %d failed, but ProveSettlements reported success", n)
			}
		}
		t.Fatal("fixture: reads never ended")
	})
}

// --- ProcessRequests ----------------------------------------------------
//
// Invariant (KC-SF-V3 K-05; ProcessRequests doc): a requested or reserved
// request is noticed as timed out at or after its deadline; an unreserved
// TimeoutPending request is refunded at or after its captured finality
// boundary (TimeoutBlock + FinalityBlocks); a Refundable request's refund is
// collected; nothing else is touched, and a reserved timed-out request is
// never refunded here.

func TestFrostRequestProcessing(t *testing.T) {
	const now, head = 1_000, 100
	r := func(tag byte, state redemption.RequestState, deadline uint64, reservation [32]byte, timeoutBlock, finality uint64) redemption.Request {
		return redemption.Request{ID: id(tag), State: state, Deadline: deadline, ReservationID: reservation, TimeoutBlock: timeoutBlock, FinalityBlocks: finality}
	}
	requests := []redemption.Request{
		r(0x01, redemption.RequestRequested, now, [32]byte{}, 0, 0),          // at the deadline: timeout
		r(0x02, redemption.RequestReserved, now-1, id(0xa2), 0, 0),           // past the deadline: timeout
		r(0x03, redemption.RequestRequested, now+1, [32]byte{}, 0, 0),        // before the deadline: nothing
		r(0x04, redemption.RequestReserved, now+1, id(0xa4), 0, 0),           // before the deadline: nothing
		r(0x05, redemption.RequestTimeoutPending, now-1, [32]byte{}, 80, 20), // at the boundary: refund
		r(0x06, redemption.RequestTimeoutPending, now-1, [32]byte{}, 90, 20), // before the boundary: nothing
		r(0x07, redemption.RequestTimeoutPending, now-1, id(0xa7), 10, 20),   // reserved: never refunded here
		r(0x08, redemption.RequestRefundable, now-1, id(0xa8), 10, 20),       // refundable: collected
		r(0x09, redemption.RequestState(99), now-1, [32]byte{}, 0, 0),        // any other state: nothing
	}
	world := func(t *testing.T) *redemptionWorld {
		w := newRedemptionWorld(t)
		w.bridge.now, w.bridge.head = now, head
		for _, q := range requests {
			w.bridge.events = append(w.bridge.events, redemption.RequestEvent{RequestID: q.ID})
			w.bridge.requests[q.ID] = q
		}
		return w
	}
	want := actionSet([]FrostAction{
		{Kind: "timeout", Request: id(1)},
		{Kind: "timeout", Request: id(2), Reservation: id(0xa2)},
		{Kind: "unreserved-refund", Request: id(5)},
		{Kind: "resolved-refund", Request: id(8), Reservation: id(0xa8)},
	})
	wantSent := actionSet([]FrostAction{
		{Kind: "timeout", Request: id(1)},
		{Kind: "timeout", Request: id(2)},
		{Kind: "unreserved-refund", Request: id(5)},
		{Kind: "resolved-refund", Request: id(8)},
	})
	t.Run("each request gets only its own action", func(t *testing.T) {
		w := world(t)
		actions, err := w.m.ProcessRequests(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := actionSet(actions); !sameSet(got, want) {
			t.Fatalf("actions %v, expected %v", got, want)
		}
		if got := sentSet(w.bridge.sent); !sameSet(got, wantSent) {
			t.Fatalf("submissions %v, expected %v", got, wantSent)
		}
	})
	for _, kind := range []string{"timeout", "unreserved-refund", "resolved-refund"} {
		t.Run("refused "+kind+" is an error", func(t *testing.T) {
			w := world(t)
			w.bridge.submit = func(s submission) error {
				if s.kind == kind {
					return errors.New("fixture: refused")
				}
				return nil
			}
			if _, err := w.m.ProcessRequests(context.Background()); err == nil {
				t.Fatalf("a refused %s was not reported", kind)
			}
		})
	}
	t.Run("every failed read is an error", func(t *testing.T) {
		for n := 1; n < 200; n++ {
			w := world(t)
			w.f.failAt = n
			_, err := w.m.ProcessRequests(context.Background())
			if !w.f.fired {
				if n == 1 {
					t.Fatal("fixture: no read was made")
				}
				return
			}
			if err == nil {
				t.Fatalf("read %d failed, but ProcessRequests reported success", n)
			}
		}
		t.Fatal("fixture: reads never ended")
	})
}

// --- ClaimBudgets -------------------------------------------------------
//
// Invariant (KC-SF-V3 K-05; ClaimBudgets doc): every unsettled cancellation
// budget of every terminal reservation is claimed once; an unresolved
// reservation and a settled budget are skipped; a refused claim (the burned
// winner) is skipped without stopping the others. A failed read is an error.

func TestFrostBudgetClaims(t *testing.T) {
	world := func(t *testing.T) *redemptionWorld {
		w := newRedemptionWorld(t)
		w.bridge.admissions = []redemption.Admission{{ReservationID: id(1)}, {ReservationID: id(2)}, {ReservationID: id(3)}}
		w.bridge.intents[id(1)] = redemption.Intent{State: redemption.IntentReserved}
		w.bridge.intents[id(2)] = redemption.Intent{State: redemption.IntentCancelled}
		w.bridge.intents[id(3)] = redemption.Intent{State: redemption.IntentPaid}
		w.bridge.alternatives[id(1)] = []redemption.Alternative{{TransactionHash: id(0x10)}}
		w.bridge.alternatives[id(2)] = []redemption.Alternative{
			{TransactionHash: id(0x20), BudgetSettled: true},
			{TransactionHash: id(0x21)}, // the burned winner: refused
			{TransactionHash: id(0x22)},
		}
		w.bridge.alternatives[id(3)] = []redemption.Alternative{{TransactionHash: id(0x30)}}
		w.bridge.submit = func(s submission) error {
			if s.transaction == id(0x21) {
				return errors.New("fixture: winner budget burned")
			}
			return nil
		}
		return w
	}
	t.Run("claims every unsettled losing budget", func(t *testing.T) {
		w := world(t)
		actions, err := w.m.ClaimBudgets(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := actionSet([]FrostAction{
			{Kind: "budget-claim", Reservation: id(2), Transaction: id(0x22)},
			{Kind: "budget-claim", Reservation: id(3), Transaction: id(0x30)},
		})
		if got := actionSet(actions); !sameSet(got, want) {
			t.Fatalf("actions %v, expected %v", got, want)
		}
		attempted := actionSet([]FrostAction{
			{Kind: "budget-claim", Reservation: id(2), Transaction: id(0x21)},
			{Kind: "budget-claim", Reservation: id(2), Transaction: id(0x22)},
			{Kind: "budget-claim", Reservation: id(3), Transaction: id(0x30)},
		})
		if got := sentSet(w.bridge.sent); !sameSet(got, attempted) {
			t.Fatalf("claims sent %v, expected %v", got, attempted)
		}
	})
	t.Run("every failed read is an error", func(t *testing.T) {
		for n := 1; n < 200; n++ {
			w := world(t)
			w.f.failAt = n
			_, err := w.m.ClaimBudgets(context.Background())
			if !w.f.fired {
				if n == 1 {
					t.Fatal("fixture: no read was made")
				}
				return
			}
			if err == nil {
				t.Fatalf("read %d failed, but ClaimBudgets reported success", n)
			}
		}
		t.Fatal("fixture: reads never ended")
	})
}
