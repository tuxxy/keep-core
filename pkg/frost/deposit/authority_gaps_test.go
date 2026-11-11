package deposit

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// TestAuthorityRefusesEachRecordDivergence: P-K06-SWEEP-AUTHORIZED-ONLY
// requires every planned deposit input to be exactly its C-05 record and the
// D-04 construction for Q. Invariant: a record that differs from the frozen
// plan in any one field, or whose terms rebuild to a different deposit
// script, is refused with ErrPlanMismatch; a record whose terms no longer
// build is refused. Each case changes one field only, so a check that
// requires two fields to differ together is caught. Chain state is the fake
// C-05 reader of fake_test.go (a fixture). Refs M5-deposit mutation batch
// (KC-SF-V3 K-06, README rule 1).
func TestAuthorityRefusesEachRecordDivergence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*world, *Record)
		any    bool // any refusal (the terms no longer build)
	}{
		{"funding transaction hash", func(_ *world, r *Record) { r.FundingTransactionHash[5] ^= 1 }, false},
		{"funding output index", func(_ *world, r *Record) { r.FundingOutputIndex++ }, false},
		{"wallet key", func(_ *world, r *Record) { r.WalletKey = xonly(testKey(44)) }, false},
		{"merkle root", func(_ *world, r *Record) { r.MerkleRoot[0] ^= 1 }, false},
		{"output parity", func(_ *world, r *Record) { r.OutputParity ^= 1 }, false},
		{"receipt", func(_ *world, r *Record) { r.Receipt[0] ^= 1 }, false},
		{"deposit output key", func(_ *world, r *Record) { r.DepositOutputKey[0] ^= 1 }, false},
		// The record is internally consistent for other terms: its receipt,
		// root, parity and output key are those of the new terms, so only
		// the rebuilt deposit script and wallet leaf differ from the plan.
		{"consistent record of other terms", func(w *world, r *Record) {
			r.Terms.ExtraData[0] ^= 1
			c, err := Build(r.Terms, w.wallet.OutputKey)
			if err != nil {
				w.t.Fatal(err)
			}
			r.Receipt, r.DepositOutputKey, r.MerkleRoot, r.OutputParity = c.Receipt, c.OutputKey, c.Root, c.Parity
		}, false},
		{"terms that no longer build", func(_ *world, r *Record) { r.Terms.Depositor = [20]byte{} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.fund(1_000_000)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			plan := planned(t, w, key)
			r := w.chain.records[key]
			tc.change(w, &r)
			w.chain.records[key] = r
			_, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, plan)
			if tc.any {
				if err == nil {
					t.Fatalf("authorized a sweep whose record no longer builds (%s)", tc.name)
				}
				return
			}
			if !errors.Is(err, ErrPlanMismatch) {
				t.Fatalf("authorized a sweep whose record differs in %s: %v", tc.name, err)
			}
		})
	}
}

// TestAuthorityRefusesNonSweepPlan: the sweep authority authorizes only K-06
// sweep plans. Invariant: a plan of another purpose (here a K-07 movement) is
// refused, even for a wallet with no main and no reservation, where every
// sweep-only check would pass vacuously. Refs M5-deposit mutation batch.
func TestAuthorityRefusesNonSweepPlan(t *testing.T) {
	w := newWorld(t)
	script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	main := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xaa}}, Value: 1_000_000, PublicKeyScript: script}
	err := recovered(func() error {
		_, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, movementPlanOn(t, w, main))
		return err
	})
	if err == nil {
		t.Fatal("the sweep authority authorized a movement plan")
	}
}

// TestAuthorityResolutionStateRefusals: a resolution sweep is authorized
// only while its reservation is the active, unresolved reservation of this
// wallet (KC-SF-V3 4.6) and, while Quarantined, under a governance cancel
// directive for exactly that reservation (spec issue #3). Invariant: an
// unreadable or absent reservation, a reservation that is no longer active,
// an unreadable directive store, and a directive that fails any one of
// present / same reservation / nonempty class / cancel action are all
// refused; the complete directive is accepted. Refs M5-deposit mutation batch.
func TestAuthorityResolutionStateRefusals(t *testing.T) {
	r := [32]byte{0x99}
	setup := func(t *testing.T, state redemption.IntentState) (*world, *Authority, func() error) {
		w := newWorld(t)
		main := w.fund(1_000_000)
		w.reserve(r, state, main)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
		if err != nil {
			t.Fatal(err)
		}
		a := &Authority{View: w.view}
		return w, a, func() error { _, err := a.AuthorizeSweep(ctx, plan); return err }
	}
	t.Run("reservation absent", func(t *testing.T) {
		w, _, authorize := setup(t, redemption.IntentReserved)
		delete(w.chain.intents, r)
		if err := authorize(); err == nil {
			t.Fatal("a resolution sweep was authorized for an unreadable reservation")
		}
	})
	t.Run("reservation no longer active", func(t *testing.T) {
		w, _, authorize := setup(t, redemption.IntentReserved)
		w.chain.custody.ActiveReservation = [32]byte{0x77}
		if err := authorize(); !errors.Is(err, ErrPlanMismatch) {
			t.Fatal("a resolution sweep was authorized for a reservation that is not active:", err)
		}
	})
	good := redemption.Directive{Reservation: r, Class: "signer-integrity", Action: redemption.DirectiveCancel}
	for _, tc := range []struct {
		name       string
		directives redemption.Directives
		want       error // nil: accepted; errInjected: any error
	}{
		{"directive store unreadable", failingDirectives{}, errInjected},
		{"no directive", directives{}, redemption.ErrContained},
		{"directive for another reservation", directives{r: {Reservation: [32]byte{0x98}, Class: good.Class, Action: good.Action}}, redemption.ErrContained},
		{"directive without class", directives{r: {Reservation: r, Action: good.Action}}, redemption.ErrContained},
		{"rebroadcast directive", directives{r: {Reservation: r, Class: good.Class, Action: redemption.DirectiveRebroadcast}}, redemption.ErrContained},
		{"cancel directive", directives{r: good}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, a, authorize := setup(t, redemption.IntentQuarantined)
			a.Directives = tc.directives
			err := authorize()
			switch {
			case tc.want == nil && err != nil:
				t.Fatal("a directed resolution sweep was refused:", err)
			case tc.want == errInjected && err == nil:
				t.Fatal("a contained resolution sweep was authorized without a readable directive")
			case tc.want != nil && tc.want != errInjected && !errors.Is(err, tc.want):
				t.Fatalf("a contained resolution sweep was authorized under an incomplete directive (%s): %v", tc.name, err)
			}
		})
	}
}

// failingDirectives is a fixture directive store that cannot be read.
type failingDirectives struct{}

func (failingDirectives) Directive(context.Context, [32]byte) (redemption.Directive, bool, error) {
	return redemption.Directive{}, false, errInjected
}
