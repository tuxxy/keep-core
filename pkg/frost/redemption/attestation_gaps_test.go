package redemption

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// TestAttestCertificateWindowAndFinality (C-04 certificate window): a
// certificate deadline is admissible from the current head through head plus
// MaximumCertificateBlocks inclusive and nowhere else, and a chain exactly as
// long as the finality depth can attest (its finalized block is genesis).
func TestAttestCertificateWindowAndFinality(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	build := func(t *testing.T, w *world) *signing.Plan {
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	for _, tc := range []struct {
		name  string
		delta func(c Configuration) int64
		ok    bool
	}{
		{"deadline at the head", func(Configuration) int64 { return 0 }, true},
		{"deadline at the window end", func(c Configuration) int64 { return int64(c.MaximumCertificateBlocks) }, true},
		{"deadline before the head", func(Configuration) int64 { return -1 }, false},
		{"deadline past the window", func(c Configuration) int64 { return int64(c.MaximumCertificateBlocks) + 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			a := attester(w, newMemJournal())
			validUntil := uint64(int64(w.chain.head) + tc.delta(w.chain.config))
			sigs, err := a.Attest(ctx, build(t, w), validUntil)
			if tc.ok && (err != nil || len(sigs) != 1) {
				t.Fatalf("admissible deadline refused: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("inadmissible deadline attested")
			}
		})
	}
	t.Run("chain exactly the finality depth long", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.chain.head = w.chain.config.FinalityBlocks
		if _, err := attester(w, newMemJournal()).Attest(ctx, build(t, w), w.chain.head+5); err != nil {
			t.Fatalf("attestation at the finality depth refused: %v", err)
		}
	})
	t.Run("chain shorter than the finality depth", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		plan := build(t, w)
		w.chain.head = w.chain.config.FinalityBlocks - 1
		if sigs, err := attester(w, newMemJournal()).Attest(ctx, plan, w.chain.head+5); err == nil {
			t.Fatalf("attested without a finalized block: %d signatures", len(sigs))
		}
	})
}

// TestAttestReplacementReadsFinalizedSlot (INTENT-V2 / ADMISSION-SEMANTICS):
// a different intent for a slot is attested only if finalized canonical
// state shows the slot still free at the same state version. The finalized
// read is a controlled reader (fixture) that answers the finalized block
// differently from the head:
//   - the head already shows the slot's state version, but the finalized
//     block is one version behind (a consistent chain whose latest custody
//     change is not yet final): refused;
//   - the finalized record is already at the slot's generation while the
//     head shows it free. No consistent provider answers this way (the head
//     extends the finalized block); the case is defence in depth against an
//     inconsistent reader, for example head and finalized reads served by
//     different nodes: refused.
func TestAttestReplacementReadsFinalizedSlot(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	w := newWorld(t)
	f := &faultChain{fakeChain: w.chain}
	w.planner.Chain = f
	a := attester(w, newMemJournal())
	plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := w.planner.Build(ctx, w.chain.head, w.ids[:1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Attest(ctx, plan, w.chain.head+10); err != nil {
		t.Fatal(err)
	}
	w.chain.head += 12 // the deadline head+10 is now finalized
	finalized := w.chain.head - w.chain.config.FinalityBlocks
	walletID := dkg.WalletID(w.wallet.OutputKey)
	free := w.chain.custody[walletID]
	f.custodyAt = func(at uint64, wallet [32]byte) (Custody, bool) {
		if wallet != walletID || at > finalized {
			return Custody{}, false
		}
		behind := free
		behind.StateVersion--
		return behind, true
	}
	if _, err = a.Attest(ctx, other, w.chain.head+10); !errors.Is(err, ErrConflictingAttestation) {
		t.Fatalf("replacement attested before the state version was final: %v", err)
	}
	f.custodyAt = func(at uint64, wallet [32]byte) (Custody, bool) {
		if wallet != walletID || at > finalized {
			return Custody{}, false
		}
		admitted := free
		admitted.Generation = plan.Generation()
		return admitted, true
	}
	if _, err = a.Attest(ctx, other, w.chain.head+10); !errors.Is(err, ErrConflictingAttestation) {
		t.Fatalf("replacement attested over a finalized admission (inconsistent reader): %v", err)
	}
	f.custodyAt = nil
	if _, err = a.Attest(ctx, other, w.chain.head+10); err != nil {
		t.Fatalf("positive control: %v", err)
	}
}

// TestAttestHistoryCapacity: a slot's attestation history holds at most
// 4,096 statements; at capacity even a valid renewal is refused, one below
// it is accepted. The history is written directly (fixture): reaching it
// through Attest takes 4,096 renewals.
func TestAttestHistoryCapacity(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	for _, tc := range []struct {
		stored int
		ok     bool
	}{{defaultCapacity - 1, true}, {defaultCapacity, false}} {
		t.Run(fmt.Sprint(tc.stored), func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			w.chain.head = 5000
			j := newMemJournal()
			a := attester(w, j)
			plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < tc.stored; i++ {
				c, err := CertificateFor(plan, uint64(i+1))
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(Statement{Certificate: c, Plan: plan.Commitment()})
				if err = j.RetainSigningRecord(ctx, lockID(c.WalletID, c.Generation, c.StateVersion, uint32(i)), raw); err != nil {
					t.Fatal(err)
				}
			}
			_, err = a.Attest(ctx, plan, w.chain.head+10)
			if tc.ok && err != nil {
				t.Fatalf("renewal below capacity refused: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrConflictingAttestation) {
				t.Fatalf("renewal beyond capacity: %v", err)
			}
		})
	}
}

// TestAttestFailsClosed: no certificate is signed unless every read, the
// readiness certificate, the key reload, the durable slot lock and the main
// purpose lock all succeed; each failure is returned, never an empty or
// partial signature set without an error.
func TestAttestFailsClosed(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		edit func(*world, *faultChain, *Attester, *memJournal, *signing.Plan)
	}{
		{"head read fails", func(_ *world, f *faultChain, _ *Attester, _ *memJournal, _ *signing.Plan) { f.headErr = errInjected }},
		{"configuration read fails", func(_ *world, f *faultChain, _ *Attester, _ *memJournal, _ *signing.Plan) { f.configErr = errInjected }},
		{"finalized header read fails", func(w *world, f *faultChain, _ *Attester, _ *memJournal, _ *signing.Plan) {
			f.headerErrAt = map[uint64]bool{w.chain.head - w.chain.config.FinalityBlocks: true}
		}},
		{"readiness certificate read fails", func(_ *world, f *faultChain, _ *Attester, _ *memJournal, _ *signing.Plan) {
			f.certifiedErr = errInjected
		}},
		{"no local seat certified", func(_ *world, _ *faultChain, a *Attester, _ *memJournal, _ *signing.Plan) { a.Seats = []uint16{4} }},
		{"key reload fails", func(_ *world, _ *faultChain, a *Attester, _ *memJournal, _ *signing.Plan) {
			a.Reload = func(context.Context) error { return errInjected }
		}},
		{"slot history unreadable", func(_ *world, _ *faultChain, a *Attester, j *memJournal, _ *signing.Plan) {
			a.Journal = faultJournal{RecordJournal: j, readErr: errInjected}
		}},
		{"slot history undecodable after an identical statement", func(w *world, _ *faultChain, _ *Attester, j *memJournal, plan *signing.Plan) {
			c, _ := CertificateFor(plan, w.chain.head+10)
			raw, _ := json.Marshal(Statement{Certificate: c, Plan: plan.Commitment()})
			_ = j.RetainSigningRecord(ctx, lockID(c.WalletID, c.Generation, c.StateVersion, 0), raw)
			_ = j.RetainSigningRecord(ctx, lockID(c.WalletID, c.Generation, c.StateVersion, 1), []byte("{"))
		}},
		{"crash before the slot lock", func(_ *world, _ *faultChain, a *Attester, _ *memJournal, _ *signing.Plan) {
			a.boundary = stopAt("before-attest-lock")
		}},
		{"crash after the slot lock", func(_ *world, _ *faultChain, a *Attester, _ *memJournal, _ *signing.Plan) {
			a.boundary = stopAt("after-attest-lock")
		}},
		{"main purpose lock refuses", func(_ *world, _ *faultChain, a *Attester, _ *memJournal, _ *signing.Plan) {
			a.MainLock = failingMainLock{}
		}},
		{"operator key cannot sign", func(_ *world, _ *faultChain, a *Attester, _ *memJournal, _ *signing.Plan) {
			a.Key = invalidOperatorKey()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			f := &faultChain{fakeChain: w.chain}
			w.planner.Chain = f
			j := newMemJournal()
			a := attester(w, j)
			plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(w, f, a, j, plan)
			if sigs, err := a.Attest(ctx, plan, w.chain.head+10); err == nil {
				t.Fatalf("attested with %d signatures", len(sigs))
			}
		})
	}
}

// TestCertifiedLocalSeats (KC-SF-V3 4.2): only local seats in the wallet's
// readiness certificate sign, in local order; none certified, or an
// unreadable certificate, is a refusal.
func TestCertifiedLocalSeats(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	w := newWorld(t)
	f := &faultChain{fakeChain: w.chain, certified: []uint16{2, 3}}
	if seats, err := CertifiedLocalSeats(ctx, f, [32]byte{1}, []uint16{3, 1, 2}); err != nil || fmt.Sprint(seats) != "[3 2]" {
		t.Fatalf("seats %v %v", seats, err)
	}
	if seats, err := CertifiedLocalSeats(ctx, f, [32]byte{1}, []uint16{1, 4}); !errors.Is(err, ErrUncertifiedSeat) {
		t.Fatalf("uncertified seats: %v %v", seats, err)
	}
	f.certifiedErr = errInjected
	if seats, err := CertifiedLocalSeats(ctx, f, [32]byte{1}, []uint16{2}); !errors.Is(err, errInjected) {
		t.Fatalf("certificate read failure: %v %v", seats, err)
	}
}

// TestSignDigestRefusesInvalidKey: an invalid operator key yields an error,
// never an empty signature. Attester has no constructor that validates its
// Key, so Attest and AttestCancellation must fail on such a key as well
// (TestAttestFailsClosed, TestAttestCancellationEachCondition).
func TestSignDigestRefusesInvalidKey(t *testing.T) {
	defer guard(t)
	noPanic(t, "SignDigest", func() {
		if sig, err := SignDigest(invalidOperatorKey(), [32]byte{1}); err == nil {
			t.Fatalf("zero key signed: %x", sig)
		}
	})
}

// TestCollectSeats: seats are 1-based; a seat-0 signature is never counted,
// and a signature of the wrong length refuses the collection.
func TestCollectSeats(t *testing.T) {
	defer guard(t)
	sig := make([]byte, 65)
	seats, sigs, err := Collect(1, map[uint16][]byte{0: sig, 1: sig})
	if err != nil || fmt.Sprint(seats) != "[1]" || len(sigs) != 65 {
		t.Fatalf("seat 0 counted: %v %d %v", seats, len(sigs), err)
	}
	if _, _, err = Collect(1, map[uint16][]byte{2: sig[:64]}); err == nil {
		t.Fatal("malformed seat signature collected")
	}
}

// cancelWorld is one admitted payment of a 1,400-sat main (400 to the
// request, 500 change, 500 fee) and its reservation record.
type cancelWorld struct {
	*world
	fc      *faultChain
	payment *signing.Plan
	id      [32]byte
	a       *Attester
}

func newCancelWorld(t *testing.T, state IntentState) *cancelWorld {
	t.Helper()
	w := newWorld(t)
	setMain(w, 1_400)
	f := &faultChain{fakeChain: w.chain}
	w.planner.Chain = f
	cw := &cancelWorld{world: w, fc: f, id: [32]byte{0x99}}
	cw.payment = cw.paymentPlan(t, 400)
	w.chain.intents[cw.id] = Intent{ID: cw.id, Certificate: Certificate{WalletID: cw.payment.WalletID(), IntentCommitment: cw.payment.Commitment()}, State: state}
	cw.a = attester(w, newMemJournal())
	return cw
}

func (cw *cancelWorld) paymentPlan(t *testing.T, value int64) *signing.Plan {
	t.Helper()
	walletScript, _ := bitcoin.PayToTaproot(cw.wallet.OutputKey)
	point := cw.main.Outpoint
	recipient := cw.chain.requests[cw.ids[0]].Script
	p, err := signing.NewPlan(signing.Proposal{Wallet: cw.wallet, BitcoinGenesis: cw.planner.Genesis, Purpose: signing.Redemption, Generation: 1, StateVersion: 1, Requests: []signing.Request{{ID: cw.ids[0], Value: value, Script: recipient}}, ConflictInput: point, Fees: signing.FeePolicy{Minimum: 100, Maximum: 2000, MaximumSatPerVByte: 10}, Transaction: &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: value, PublicKeyScript: recipient}, {Value: 900 - value, PublicKeyScript: walletScript}}}, Prevouts: []bitcoin.PreviousOutput{cw.main}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (cw *cancelWorld) cancel(t *testing.T, payment *signing.Plan, value int64) *signing.Plan {
	t.Helper()
	walletScript, _ := bitcoin.PayToTaproot(cw.wallet.OutputKey)
	point := cw.main.Outpoint
	p, err := signing.NewCancellationPlan(payment, &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: value, PublicKeyScript: walletScript}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAttestCancellationEachCondition (C-04 cancellation, spec issue #3): a
// cancellation is signed only for an unresolved admission of this wallet's
// exact payment, with a directive to cancel when the reservation is
// contained, a standard output, a fee above every admitted alternative and a
// deadline not yet passed; each failed condition alone refuses it, each
// boundary that is still valid is accepted.
func TestAttestCancellationEachCondition(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	type tcase struct {
		name string
		edit func(*testing.T, *cancelWorld) (payment, cancellation *signing.Plan, validUntil uint64)
		ok   bool
	}
	plain := func(t *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
		return cw.payment, cw.cancel(t, cw.payment, 400), cw.chain.head + 10
	}
	with := func(f func(*cancelWorld)) func(*testing.T, *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
		return func(t *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
			f(cw)
			return plain(t, cw)
		}
	}
	setIntent := func(f func(*Intent)) func(*cancelWorld) {
		return func(cw *cancelWorld) {
			i := cw.chain.intents[cw.id]
			f(&i)
			cw.chain.intents[cw.id] = i
		}
	}
	for _, tc := range []tcase{
		{"positive control", plain, true},
		{"the payment offered as its own cancellation", func(_ *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
			return cw.payment, cw.payment, cw.chain.head + 10
		}, false},
		{"a cancellation of another payment", func(t *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
			return cw.payment, cw.cancel(t, cw.paymentPlan(t, 450), 400), cw.chain.head + 10
		}, false},
		{"head read fails", with(func(cw *cancelWorld) { cw.fc.headErr = errInjected }), false},
		{"no reservation record", with(func(cw *cancelWorld) { delete(cw.chain.intents, cw.id) }), false},
		{"reservation resolved", with(setIntent(func(i *Intent) { i.State = IntentPaid })), false},
		{"reservation admits another payment", with(setIntent(func(i *Intent) { i.Certificate.IntentCommitment[0] ^= 1 })), false},
		{"reservation of another wallet", with(setIntent(func(i *Intent) { i.Certificate.WalletID[0] ^= 1 })), false},
		{"contained, no directive", with(setIntent(func(i *Intent) { i.State = IntentQuarantined })), false},
		{"contained, directive source fails", with(func(cw *cancelWorld) {
			setIntent(func(i *Intent) { i.State = IntentQuarantined })(cw)
			cw.a.Directives = fixedDirectives{err: errInjected}
		}), false},
		{"contained, directive to rebroadcast", with(func(cw *cancelWorld) {
			setIntent(func(i *Intent) { i.State = IntentQuarantined })(cw)
			cw.a.Directives = fixedDirectives{d: Directive{Reservation: cw.id, Class: "fixture", Action: DirectiveRebroadcast}, ok: true}
		}), false},
		{"contained, directive to cancel", with(func(cw *cancelWorld) {
			setIntent(func(i *Intent) { i.State = IntentQuarantined })(cw)
			cw.a.Directives = fixedDirectives{d: Directive{Reservation: cw.id, Class: "fixture", Action: DirectiveCancel}, ok: true}
		}), true},
		{"alternatives read fails", with(func(cw *cancelWorld) { cw.fc.alternativesErr = errInjected }), false},
		{"output exactly at the dust floor", func(t *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
			return cw.payment, cw.cancel(t, cw.payment, taprootDust), cw.chain.head + 10
		}, true},
		{"fee equal to an admitted alternative", with(func(cw *cancelWorld) { cw.chain.alternatives[cw.id] = []Alternative{{Fee: 1_000}} }), false},
		{"fee above every admitted alternative", with(func(cw *cancelWorld) { cw.chain.alternatives[cw.id] = []Alternative{{Fee: 600}, {Fee: 999}} }), true},
		{"deadline at the head", func(t *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
			return cw.payment, cw.cancel(t, cw.payment, 400), cw.chain.head
		}, true},
		{"deadline passed", func(t *testing.T, cw *cancelWorld) (*signing.Plan, *signing.Plan, uint64) {
			return cw.payment, cw.cancel(t, cw.payment, 400), cw.chain.head - 1
		}, false},
		{"readiness certificate read fails", with(func(cw *cancelWorld) { cw.fc.certifiedErr = errInjected }), false},
		{"crash before attesting", with(func(cw *cancelWorld) { cw.a.boundary = stopAt("before-cancel-attest") }), false},
		{"operator key cannot sign", with(func(cw *cancelWorld) { cw.a.Key = invalidOperatorKey() }), false},
		{"ledger full", with(func(cw *cancelWorld) {
			cw.a.Ledger = Ledger{Journal: newMemJournal(), Capacity: 1, Reserve: 1}
			_ = cw.a.Ledger.Append(ctx, Entry{Reservation: cw.id, Kind: KindTimeoutSeen})
		}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			cw := newCancelWorld(t, IntentTimeoutPending)
			payment, cancellation, validUntil := tc.edit(t, cw)
			var sigs map[uint16][]byte
			var err error
			noPanic(t, "AttestCancellation", func() { sigs, _, err = cw.a.AttestCancellation(ctx, payment, cancellation, cw.id, validUntil) })
			if tc.ok && (err != nil || len(sigs) != 1) {
				t.Fatalf("valid cancellation refused: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("cancellation attested with %d signatures", len(sigs))
			}
		})
	}
}
