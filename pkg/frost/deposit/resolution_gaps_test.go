package deposit

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// attestWorld is a funded wallet whose main is attested for reservation
// 0x99, active and Reserved on the fake C-04 reader (a fixture), with a
// resolution sweep planned for it. The attester signs with the fixed test
// key of resolution_test.go.
type attestWorld struct {
	*world
	fc       *faultChain
	plan     *signing.Plan
	attester *ResolutionAttester
	r        [32]byte
}

func newAttestWorld(t *testing.T, state redemption.IntentState) *attestWorld {
	t.Helper()
	w := newWorld(t)
	fc := newFaultChain(w.chain)
	main := w.fund(1_000_000)
	r := [32]byte{0x99}
	w.chain.custody.ActiveReservation = r
	key := w.deposit(1, 100_000, 90, [20]byte{})
	plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
	if err != nil {
		t.Fatal(err)
	}
	w.chain.intents[r] = redemption.Intent{ID: r, State: state, Certificate: redemption.Certificate{WalletID: w.id, DescriptorHash: plan.DescriptorHash(), ConflictTxHash: [32]byte(main.Outpoint.TransactionHash), ConflictOutputIndex: main.Outpoint.OutputIndex}}
	w.chain.maxCerts = 10
	if err = w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 200); err != nil {
		t.Fatal(err)
	}
	a := &ResolutionAttester{Chain: fc, Lock: w.lock, Authority: &Authority{View: w.view}, Key: testECDSA(t), Seats: []uint16{1}}
	return &attestWorld{world: w, fc: fc, plan: plan, attester: a, r: r}
}

func (aw *attestWorld) purpose(t *testing.T) Purpose {
	t.Helper()
	cur, err := aw.lock.Current(ctx, aw.id, OutpointKey(aw.plan.SweepMain().Outpoint))
	if err != nil {
		t.Fatal(err)
	}
	return cur.Purpose
}

func (aw *attestWorld) editIntent(f func(*redemption.Intent)) {
	i := aw.chain.intents[aw.r]
	f(&i)
	aw.chain.intents[aw.r] = i
}

// TestResolutionAttestWindowAndBinding: the C-04 sweep-resolution
// certificate (VD-07 reading 3) is attested only for a deadline in
// [head, head+MaximumCertificateBlocks], only for the active, unresolved
// reservation of this wallet whose certificate names this descriptor and
// exactly this main, and never for a plan that is not a resolution sweep.
// Invariant: each single fault is refused and the lock stays redemption(r);
// both window ends are accepted and move the lock to resolution(r).
// Refs M5-deposit mutation batch (KC-SF-V3 4.6).
func TestResolutionAttestWindowAndBinding(t *testing.T) {
	for _, tc := range []struct {
		name       string
		validUntil uint64
		change     func(*attestWorld)
		ok         bool
	}{
		{"deadline at the head", 100, nil, true},
		{"deadline at the window end", 110, nil, true},
		{"deadline before the head", 99, nil, false},
		{"deadline past the window", 111, nil, false},
		{"certificate of another descriptor", 105, func(aw *attestWorld) {
			aw.editIntent(func(i *redemption.Intent) { i.Certificate.DescriptorHash[0] ^= 1 })
		}, false},
		{"certificate of another conflict transaction", 105, func(aw *attestWorld) {
			aw.editIntent(func(i *redemption.Intent) { i.Certificate.ConflictTxHash[0] ^= 1 })
		}, false},
		{"certificate of another conflict output", 105, func(aw *attestWorld) {
			aw.editIntent(func(i *redemption.Intent) { i.Certificate.ConflictOutputIndex++ })
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aw := newAttestWorld(t, redemption.IntentReserved)
			if tc.change != nil {
				tc.change(aw)
			}
			sigs, auth, err := aw.attester.Attest(ctx, aw.plan, tc.validUntil)
			if tc.ok {
				if err != nil || len(sigs) != 1 || auth.ValidUntilBlock != tc.validUntil || aw.purpose(t) != Resolution {
					t.Fatalf("a valid resolution was not attested (%s): %d signatures, %v", tc.name, len(sigs), err)
				}
				return
			}
			if err == nil || sigs != nil {
				t.Fatalf("a resolution was attested (%s)", tc.name)
			}
			if p := aw.purpose(t); p != Redemption {
				t.Fatalf("a refused resolution (%s) moved the lock to %q", tc.name, p)
			}
		})
	}
	t.Run("ordinary sweep", func(t *testing.T) {
		w := newWorld(t)
		w.fund(1_000_000)
		plan := planned(t, w, w.deposit(1, 100_000, 90, [20]byte{}))
		a := &ResolutionAttester{Chain: w.chain, Lock: w.lock, Authority: &Authority{View: w.view}, Key: testECDSA(t), Seats: []uint16{1}}
		if sigs, _, err := a.Attest(ctx, plan, 105); err == nil || sigs != nil {
			t.Fatal("a resolution certificate was attested for an ordinary sweep")
		}
	})
}

// TestResolutionAttestFailsClosed: the attester signs only after every read
// and the lock transition succeed. Invariant: a failed head, configuration,
// reservation or custody read, a failed key reload, a missing sweep
// authority, a lock that does not hold redemption(r), and a stop before or
// after the lock moves all return an error and no signature. Refs
// M5-deposit mutation batch.
func TestResolutionAttestFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *attestWorld)
	}{
		{"head unreadable", func(_ *testing.T, aw *attestWorld) { aw.fc.set("Head", true) }},
		{"configuration unreadable", func(_ *testing.T, aw *attestWorld) { aw.fc.set("Configuration", true) }},
		{"reservation unreadable", func(_ *testing.T, aw *attestWorld) { aw.fc.set("Intent", true) }},
		{"custody unreadable", func(_ *testing.T, aw *attestWorld) { aw.fc.set("Custody", true) }},
		{"key reload fails", func(_ *testing.T, aw *attestWorld) {
			aw.attester.Reload = func(context.Context) error { return errInjected }
		}},
		{"no sweep authority", func(_ *testing.T, aw *attestWorld) { aw.attester.Authority = nil }},
		{"lock not attested", func(_ *testing.T, aw *attestWorld) {
			aw.attester.Lock = &MainLock{Journal: newJournal()} // this host never attested r: the main is free
		}},
		{"stop before the lock moves", func(_ *testing.T, aw *attestWorld) { aw.attester.boundary = stopAtName("before-resolution-attest") }},
		{"stop after the lock moves", func(_ *testing.T, aw *attestWorld) { aw.attester.boundary = stopAtName("after-resolution-attest") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aw := newAttestWorld(t, redemption.IntentReserved)
			tc.change(t, aw)
			if sigs, _, err := aw.attester.Attest(ctx, aw.plan, 105); err == nil || sigs != nil {
				t.Fatalf("a resolution was attested with %s", tc.name)
			}
		})
	}
}

func stopAtName(name string) func(string) error {
	return func(at string) error {
		if at == name {
			return errInjected
		}
		return nil
	}
}

// TestResolutionAttesterNeedsItsOwnDirective: a resolution sweep of a
// contained (Quarantined) reservation is a cancellation-class act; the
// attester itself requires a governance cancel directive for exactly that
// reservation before the lock moves (spec issue #3), independently of the
// directives its sweep authority holds. Invariant: with the authority fully
// directed, an attester with no directive store, an unreadable store, no
// directive, a directive for another reservation, without class, or with
// another action refuses and leaves the lock at redemption(r); the complete
// cancel directive attests. Refs M5-deposit mutation batch.
func TestResolutionAttesterNeedsItsOwnDirective(t *testing.T) {
	r := [32]byte{0x99}
	good := redemption.Directive{Reservation: r, Class: "signer-integrity", Action: redemption.DirectiveCancel}
	for _, tc := range []struct {
		name       string
		directives redemption.Directives
		ok         bool
	}{
		{"no directive store", nil, false},
		{"directive store unreadable", failingDirectives{}, false},
		{"no directive", directives{}, false},
		{"directive for another reservation", directives{r: {Reservation: [32]byte{0x98}, Class: good.Class, Action: good.Action}}, false},
		{"directive without class", directives{r: {Reservation: r, Action: good.Action}}, false},
		{"rebroadcast directive", directives{r: {Reservation: r, Class: good.Class, Action: redemption.DirectiveRebroadcast}}, false},
		{"cancel directive", directives{r: good}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aw := newAttestWorld(t, redemption.IntentQuarantined)
			aw.attester.Authority.Directives = directives{r: good}
			aw.attester.Directives = tc.directives
			sigs, _, err := aw.attester.Attest(ctx, aw.plan, 105)
			if tc.ok {
				if err != nil || len(sigs) != 1 {
					t.Fatal("a directed resolution was refused:", err)
				}
				return
			}
			if err == nil || sigs != nil {
				t.Fatalf("a contained resolution was attested by an attester with %s", tc.name)
			}
			if p := aw.purpose(t); p != Redemption {
				t.Fatalf("a refused contained resolution moved the lock to %q", p)
			}
			if tc.directives != nil && tc.name != "directive store unreadable" && !errors.Is(err, redemption.ErrContained) {
				t.Fatalf("an incomplete directive was not refused as containment: %v", err)
			}
		})
	}
}
