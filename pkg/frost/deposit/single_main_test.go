package deposit

import (
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// Single-main invariant (D13 refinement, tux 2026-10-30): while a FROST
// wallet's main is reserved for a redemption (C-04 activeReservation != 0),
// the honest K-06 host neither plans nor authorizes an ordinary deposit sweep;
// deposits wait until the reservation resolves, as an ECDSA wallet's deposits
// wait for its main. The only sweep during a reservation is the C-04
// sweep-resolution route for that exact reservation (KC-SF-V3 §4.6). So
// recognized custody is always exactly the main, and a movement can spend it
// alone. Supports KC-VER P-K06-ONE-MAIN-PURPOSE and P-K06-CREDIT-ONCE (no
// credit outside the main).
// Refs tuxxy/tbtc-v2#24
//
// All chain state here is the in-memory fake C-05/C-04 reader of
// fake_test.go (a fixture); Planner, Authority and Sweeper are the real code.

// unresolvedStates are every C-04 intent state in which the main stays
// reserved (redemption.IntentState.Unresolved).
var unresolvedStates = []struct {
	name  string
	state redemption.IntentState
}{
	{"Reserved", redemption.IntentReserved},
	{"TimeoutPending", redemption.IntentTimeoutPending},
	{"Quarantined", redemption.IntentQuarantined},
}

// reserve gives the wallet's current main an active reservation r in state,
// with a C-04 certificate naming that main.
func (w *world) reserve(r [32]byte, state redemption.IntentState, main bitcoin.PreviousOutput) {
	w.t.Helper()
	w.chain.custody.ActiveReservation = r
	w.chain.intents[r] = redemption.Intent{ID: r, State: state, Certificate: redemption.Certificate{WalletID: w.id, ConflictTxHash: [32]byte(main.Outpoint.TransactionHash), ConflictOutputIndex: main.Outpoint.OutputIndex}}
}

// TestPlannerRefusesOrdinarySweepWhileReserved: Planner.Build and the host's
// round planning (Sweeper.Plan, the honest host's only ordinary-sweep
// decision) refuse with ErrMainBusy and plan nothing while the main is
// reserved, in every unresolved reservation state. The previous rule planned a
// deposit-only sweep, whose output C-05 credits beside the reserved main.
// Invariant: no ordinary sweep while reserved (single main). Supports
// P-K06-ONE-MAIN-PURPOSE. Refs tuxxy/tbtc-v2#24
func TestPlannerRefusesOrdinarySweepWhileReserved(t *testing.T) {
	for _, s := range unresolvedStates {
		t.Run(s.name, func(t *testing.T) {
			w := newWorld(t)
			main := w.fund(1_000_000)
			w.reserve([32]byte{0x99}, s.state, main)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
			if !errors.Is(err, ErrMainBusy) || plan != nil {
				t.Fatalf("single main: ordinary sweep planned while the main is reserved (%s): plan %v, err %v", s.name, plan != nil, err)
			}
			sweeper := &Sweeper{Chain: w.chain, Planner: w.planner, Authority: &Authority{View: w.view}, Lock: w.lock, Ledger: redemption.Ledger{Journal: newJournal()}, Schedule: redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}}
			plan, err = sweeper.Plan(ctx, 0, Round{Number: 10, Anchor: 100})
			if !errors.Is(err, ErrMainBusy) || plan != nil {
				t.Fatalf("single main: host round planned an ordinary sweep while the main is reserved (%s): plan %v, err %v", s.name, plan != nil, err)
			}
		})
	}
}

// TestAuthorityRefusesNoMainPlanWhileReserved: a plan without main, built
// while the wallet had no main, is refused at authorization once the wallet
// has a reserved main. The executor calls the
// authority before every grant and before broadcast, so a deposit-only sweep
// that races a new main and its reservation never reaches a worker. The
// refusal must be terminal (ErrMainBusy or ErrPlanMismatch) so the sweeper
// abandons the plan and replans after the reservation resolves.
// Invariant: no ordinary sweep while reserved (single main). Supports
// P-K06-ONE-MAIN-PURPOSE, P-K06-SWEEP-AUTHORIZED-ONLY. Refs tuxxy/tbtc-v2#24
func TestAuthorityRefusesNoMainPlanWhileReserved(t *testing.T) {
	refused := func(err error) bool {
		return (errors.Is(err, ErrMainBusy) || errors.Is(err, ErrPlanMismatch)) && terminal(err)
	}
	for _, s := range unresolvedStates {
		t.Run(s.name, func(t *testing.T) {
			w := newWorld(t)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			plan := planned(t, w, key)
			if plan.SweepMain() != nil {
				t.Fatal("fixture: the first sweep of an unfunded wallet has no main")
			}
			main := w.fund(1_000_000)
			w.reserve([32]byte{0x99}, s.state, main)
			if _, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, plan); !refused(err) {
				t.Fatalf("single main: sweep without main authorized while the main is reserved (%s): %v", s.name, err)
			}
		})
	}
}

// TestResolutionRouteSurvivesSingleMain: the C-04 sweep-resolution route is
// unchanged by the single-main rule. While the main is reserved, a plan naming
// the active reservation is built with exactly the reserved main and is
// authorized, in every unresolved state the authority accepts (Quarantined
// with a governance cancel directive). A plan naming another reservation is
// still refused. Positive control against an over-broad fix.
// Invariant: the one sweep allowed during a reservation is its resolution
// (KC-SF-V3 §4.6). Supports P-K06-ONE-MAIN-PURPOSE. Refs tuxxy/tbtc-v2#24
func TestResolutionRouteSurvivesSingleMain(t *testing.T) {
	for _, s := range unresolvedStates {
		t.Run(s.name, func(t *testing.T) {
			w := newWorld(t)
			main := w.fund(1_000_000)
			r := [32]byte{0x99}
			w.reserve(r, s.state, main)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
			if err != nil {
				t.Fatalf("single main: resolution sweep of the active reservation not planned (%s): %v", s.name, err)
			}
			if m := plan.SweepMain(); m == nil || m.Outpoint != main.Outpoint || plan.SweepResolution() != r {
				t.Fatalf("single main: resolution sweep must spend exactly the reserved main (%s)", s.name)
			}
			authority := &Authority{View: w.view}
			if s.state == redemption.IntentQuarantined {
				authority.Directives = directives{r: {Reservation: r, Class: "signer-integrity", Action: redemption.DirectiveCancel}}
			}
			if _, err = authority.AuthorizeSweep(ctx, plan); err != nil {
				t.Fatalf("single main: resolution sweep of the active reservation refused (%s): %v", s.name, err)
			}
			if _, err = w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{0x98}); err == nil {
				t.Fatalf("resolution of a reservation that is not active was planned (%s)", s.name)
			}
		})
	}
}

// TestSingleMainBoundaries: the sides of the rule. A wallet with no main and
// no reservation sweeps deposit-only (the first sweep creates the main), and
// the authority accepts it. An unreserved main is always included. While the
// main is reserved the deposit waits; once the reservation resolves (here a
// payment proof: C-04 records the payment's change as the new, unreserved
// main) the next plan spends the new main with the waiting deposit and is
// authorized, so the output is again the single main.
// Invariant: single main after every sweep. Supports P-K06-ONE-MAIN-PURPOSE,
// P-K06-CREDIT-ONCE. Refs tuxxy/tbtc-v2#24
func TestSingleMainBoundaries(t *testing.T) {
	authorize := func(t *testing.T, w *world, plan *signing.Plan, what string) {
		t.Helper()
		if _, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, plan); err != nil {
			t.Fatalf("single main: %s refused: %v", what, err)
		}
	}
	t.Run("no main, no reservation: deposit-only", func(t *testing.T) {
		w := newWorld(t)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if err != nil || plan.SweepMain() != nil {
			t.Fatal("single main: the first sweep of an unfunded wallet must be deposit-only:", err)
		}
		authorize(t, w, plan, "first deposit-only sweep")
	})
	t.Run("unreserved main is included", func(t *testing.T) {
		w := newWorld(t)
		main := w.fund(1_000_000)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if err != nil || plan.SweepMain() == nil || plan.SweepMain().Outpoint != main.Outpoint {
			t.Fatal("single main: an unreserved main must be included:", err)
		}
		authorize(t, w, plan, "sweep of the unreserved main")
	})
	t.Run("deposit waits for the reservation, then sweeps with the new main", func(t *testing.T) {
		w := newWorld(t)
		old := w.fund(1_000_000)
		r := [32]byte{0x99}
		w.reserve(r, redemption.IntentReserved, old)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		if plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{}); !errors.Is(err, ErrMainBusy) || plan != nil {
			t.Fatalf("single main: ordinary sweep planned while the main is reserved: plan %v, err %v", plan != nil, err)
		}
		// Payment proven: the reserved main is spent and the payment's change
		// is the new unreserved main (fixture of the C-04 payment-proof state).
		spent := w.chain.outputs[OutpointKey(old.Outpoint)]
		spent.Spent = true
		w.chain.outputs[OutpointKey(old.Outpoint)] = spent
		w.btc.outputs = nil
		script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
		change := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xbb}, OutputIndex: 1}, Value: 900_000, PublicKeyScript: script}
		changeKey := OutpointKey(change.Outpoint)
		w.chain.custody = redemption.Custody{Generation: 1, StateVersion: 2, Value: 900_000, MainOutpoint: changeKey}
		w.chain.outputs[changeKey] = redemption.CustodyOutput{WalletID: w.id, Value: 900_000}
		w.btc.outputs = append(w.btc.outputs, change)
		w.chain.intents[r] = redemption.Intent{ID: r, State: redemption.IntentPaid}
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if err != nil || plan.SweepMain() == nil || plan.SweepMain().Outpoint != change.Outpoint {
			t.Fatal("single main: after the reservation resolves the waiting deposit must be swept with the new main:", err)
		}
		authorize(t, w, plan, "sweep of the new main after the payment")
	})
}
