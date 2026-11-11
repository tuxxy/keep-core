package redemption

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// TestIntentStateUnresolved mirrors C-04: payment or cancellation proofs may
// still settle exactly in Reserved, TimeoutPending and Quarantined.
func TestIntentStateUnresolved(t *testing.T) {
	defer guard(t)
	want := map[IntentState]bool{IntentReserved: true, IntentTimeoutPending: true, IntentQuarantined: true}
	for s := IntentUnknown; s <= IntentConflictResolved+1; s++ {
		if s.Unresolved() != want[s] {
			t.Fatalf("state %d (%s): Unresolved %v", s, s.ExecutorState(), s.Unresolved())
		}
	}
}

// TestLedgerReserveEqualToCapacity: a reserve as large as the capacity is
// honoured, so no ordinary progress record is accepted at all while recovery
// records still are.
func TestLedgerReserveEqualToCapacity(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	l := Ledger{Journal: newMemJournal(), Capacity: 600, Reserve: 600}
	id := [32]byte{2}
	if err := l.Append(ctx, Entry{Reservation: id, Kind: KindSignAttempt, Detail: "0"}); err == nil {
		t.Fatal("ordinary record consumed a reserve equal to the capacity")
	}
	if err := l.Append(ctx, Entry{Reservation: id, Kind: KindTimeoutSeen}); err != nil {
		t.Fatalf("recovery record refused: %v", err)
	}
}

// TestLedgerRefusesMisplacedRecords: a record stored under a reservation's
// sequence slot is used only if it names that reservation and sequence, and
// an unreadable or undecodable record fails the read, the lookup and the
// append (fail closed, nothing skipped).
func TestLedgerRefusesMisplacedRecords(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	id := [32]byte{3}
	for _, tc := range []struct {
		name string
		raw  func() []byte
	}{
		{"another reservation", func() []byte { b, _ := json.Marshal(Entry{Reservation: [32]byte{4}, Kind: KindOutcome}); return b }},
		{"another sequence", func() []byte { b, _ := json.Marshal(Entry{Reservation: id, Kind: KindOutcome, Sequence: 1}); return b }},
		{"undecodable", func() []byte { return []byte("{") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			j := newMemJournal()
			if err := j.RetainSigningRecord(ctx, entryID(id, 0), tc.raw()); err != nil {
				t.Fatal(err)
			}
			l := Ledger{Journal: j}
			if e, err := l.Entries(ctx, id); err == nil {
				t.Fatalf("misplaced record read as %+v", e)
			}
			if ok, err := l.Has(ctx, id, KindOutcome, [32]byte{}); err == nil {
				t.Fatalf("lookup over a misplaced record answered %v", ok)
			}
			if err := l.Append(ctx, Entry{Reservation: id, Kind: KindTimeoutSeen}); err == nil {
				t.Fatal("append over a misplaced record accepted")
			}
		})
	}
	t.Run("read failure", func(t *testing.T) {
		defer guard(t)
		l := Ledger{Journal: faultJournal{RecordJournal: newMemJournal(), readErr: errInjected}}
		if _, err := l.Entries(ctx, id); !errors.Is(err, errInjected) {
			t.Fatalf("read failure hidden: %v", err)
		}
		if _, err := l.Has(ctx, id, KindOutcome, [32]byte{}); !errors.Is(err, errInjected) {
			t.Fatalf("lookup hid a read failure: %v", err)
		}
		if err := l.Append(ctx, Entry{Reservation: id, Kind: KindOutcome}); !errors.Is(err, errInjected) {
			t.Fatalf("append hid a read failure: %v", err)
		}
	})
	t.Run("write failure", func(t *testing.T) {
		defer guard(t)
		l := Ledger{Journal: faultJournal{RecordJournal: newMemJournal(), writeErr: errInjected}}
		if err := l.Append(ctx, Entry{Reservation: id, Kind: KindOutcome}); !errors.Is(err, errInjected) {
			t.Fatalf("append hid a write failure: %v", err)
		}
	})
}

// TestLedgerHasMatchesKindAndTransaction: Has finds a record only of the
// asked kind, of the asked transaction when one is given, and of any
// transaction when none is given.
func TestLedgerHasMatchesKindAndTransaction(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	l := Ledger{Journal: newMemJournal()}
	id, tx := [32]byte{5}, [32]byte{6}
	if err := l.Append(ctx, Entry{Reservation: id, Kind: KindSigned, Transaction: tx, Detail: "0"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		kind Kind
		tx   [32]byte
		want bool
	}{
		{"kind and transaction", KindSigned, tx, true},
		{"kind, any transaction", KindSigned, [32]byte{}, true},
		{"kind, another transaction", KindSigned, [32]byte{7}, false},
		{"another kind, same transaction", KindBroadcast, tx, false},
		{"another kind, any transaction", KindBroadcast, [32]byte{}, false},
	} {
		if ok, err := l.Has(ctx, id, tc.kind, tc.tx); err != nil || ok != tc.want {
			t.Fatalf("%s: Has %v %v, want %v", tc.name, ok, err, tc.want)
		}
	}
	if ok, err := l.Has(ctx, [32]byte{8}, KindSigned, [32]byte{}); err != nil || ok {
		t.Fatalf("empty ledger: Has %v %v", ok, err)
	}
}

// TestReloadTransportCarriesNoProtocol (C-04 honest-host rule): the
// key-reload transport binds one attempt and refuses every protocol message
// and every DKG admission, so a reload can never advance a signing protocol.
func TestReloadTransportCarriesNoProtocol(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	tr := reloadOnly{frost.Attempt{StartBlock: 9}}
	if a, _ := tr.Binding(); a.StartBlock != 9 {
		t.Fatal("binding")
	}
	if err := tr.Broadcast(ctx, 1, []byte{1}); err == nil {
		t.Fatal("reload broadcast a protocol message")
	}
	if err := tr.SendPrivate(ctx, 1, 2, []byte{1}); err == nil {
		t.Fatal("reload sent a private protocol message")
	}
	if _, err := tr.Receive(ctx); err == nil {
		t.Fatal("reload received a protocol message")
	}
	if _, err := tr.WaitCandidateAcceptance(ctx, frost.Candidate{}); err == nil {
		t.Fatal("reload admitted a DKG candidate")
	}
}

// TestFreshWorkerReloadFailsClosed (C-04 honest-host rule): the reload
// before an attestation never reports success without reloading the
// completed key: an unpinned worker configuration, or a host store that
// holds no completed key, is an error, so Attest cannot sign after it.
func TestFreshWorkerReloadFailsClosed(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	head := func(context.Context) (uint64, error) { return 100, nil }
	journal := openJournal(t, [32]byte{0x31})
	for _, tc := range []struct {
		name   string
		worker snowfallengine.WorkerConfig
	}{
		{"unpinned worker", snowfallengine.WorkerConfig{}},
		{"no completed key", fixtureWorker},
	} {
		noPanic(t, "FreshWorkerReload", func() {
			if err := FreshWorkerReload(tc.worker, journal, head)(ctx); err == nil {
				t.Fatalf("%s: reload reported success", tc.name)
			}
		})
	}
}

// TestChainReservationsFailClosed: a reservation or cancellation view is
// returned only from one consistent snapshot in which the admission log
// matches the record and the admission block's canonical header was read;
// any failed read or mismatch refuses the view.
func TestChainReservationsFailClosed(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	id := [32]byte{0x77}
	setup := func(t *testing.T) (*world, *faultChain) {
		w := newWorld(t)
		w.chain.intents[id] = Intent{ID: id, Certificate: Certificate{IntentCommitment: [32]byte{9}, Generation: 1, StateVersion: 1}, AdmittedBlock: 90, TransactionHash: [32]byte{5}, State: IntentReserved}
		w.chain.admissions[id] = Admission{ReservationID: id, TransactionHash: [32]byte{5}, Block: 90, BlockHash: w.chain.hash(90)}
		return w, &faultChain{fakeChain: w.chain, headerErrAt: map[uint64]bool{}}
	}
	t.Run("reservation/admission at the head", func(t *testing.T) {
		defer guard(t)
		w, f := setup(t)
		i := w.chain.intents[id]
		i.AdmittedBlock = 100
		w.chain.intents[id] = i
		w.chain.admissions[id] = Admission{ReservationID: id, TransactionHash: [32]byte{5}, Block: 100, BlockHash: w.chain.hash(100)}
		v, err := ChainReservations{Chain: f}.ReadReservation(ctx, id)
		if err != nil || v.Block != 100 || v.CanonicalBlockHash != v.BlockHash {
			t.Fatalf("admission at the head refused: %+v %v", v, err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*world, *faultChain)
	}{
		{"head read fails", func(_ *world, f *faultChain) { f.headErr = errInjected }},
		{"head re-read fails", func(_ *world, f *faultChain) { f.headerErrAt[100] = true }},
		{"admission header read fails", func(_ *world, f *faultChain) { f.headerErrAt[90] = true }},
		{"record read fails", func(_ *world, f *faultChain) { f.intentErr = errInjected }},
		{"no admission log", func(w *world, _ *faultChain) { delete(w.chain.admissions, id) }},
		{"log of another transaction", func(w *world, _ *faultChain) {
			a := w.chain.admissions[id]
			a.TransactionHash[0] ^= 1
			w.chain.admissions[id] = a
		}},
	} {
		t.Run("reservation/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w, f := setup(t)
			tc.edit(w, f)
			if v, err := (ChainReservations{Chain: f}).ReadReservation(ctx, id); err == nil {
				t.Fatalf("view returned: %+v", v)
			}
		})
	}

	hash := [32]byte{0xc2}
	key := func() [64]byte {
		var k [64]byte
		copy(k[:32], id[:])
		copy(k[32:], hash[:])
		return k
	}()
	cancellation := func(t *testing.T) (*world, *faultChain) {
		w, f := setup(t)
		w.chain.alternatives[id] = []Alternative{{TransactionHash: [32]byte{0xc1}, Snapshot: [32]byte{1}, Fee: 600, AdmittedBlock: 80}, {TransactionHash: hash, Snapshot: [32]byte{2}, Fee: 700, AdmittedBlock: 90}, {TransactionHash: [32]byte{0xc3}, Snapshot: [32]byte{3}, Fee: 800, AdmittedBlock: 95}}
		w.chain.cancellation[key] = Admission{ReservationID: id, TransactionHash: hash, Block: 90, BlockHash: w.chain.hash(90)}
		return w, f
	}
	t.Run("cancellation/the named alternative", func(t *testing.T) {
		defer guard(t)
		_, f := cancellation(t)
		v, err := ChainReservations{Chain: f}.ReadCancellation(ctx, id, hash)
		if err != nil || v.Snapshot != ([32]byte{2}) || v.Fee != 700 || v.Block != 90 || v.BlockHash != v.CanonicalBlockHash {
			t.Fatalf("view %+v %v", v, err)
		}
	})
	t.Run("cancellation/admission at the head", func(t *testing.T) {
		defer guard(t)
		w, f := cancellation(t)
		w.chain.alternatives[id][1].AdmittedBlock = 100
		w.chain.cancellation[key] = Admission{ReservationID: id, TransactionHash: hash, Block: 100, BlockHash: w.chain.hash(100)}
		if v, err := (ChainReservations{Chain: f}).ReadCancellation(ctx, id, hash); err != nil || v.Block != 100 {
			t.Fatalf("admission at the head refused: %+v %v", v, err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*world, *faultChain)
	}{
		{"head read fails", func(_ *world, f *faultChain) { f.headErr = errInjected }},
		{"alternatives read fails", func(_ *world, f *faultChain) { f.alternativesErr = errInjected }},
		{"no such alternative", func(w *world, _ *faultChain) { w.chain.alternatives[id] = w.chain.alternatives[id][:1] }},
		{"no cancellation log", func(w *world, _ *faultChain) { delete(w.chain.cancellation, key) }},
		{"log block differs", func(w *world, _ *faultChain) { w.chain.alternatives[id][1].AdmittedBlock = 91 }},
		{"admission after the head", func(w *world, _ *faultChain) {
			w.chain.alternatives[id][1].AdmittedBlock = 101
			w.chain.cancellation[key] = Admission{ReservationID: id, TransactionHash: hash, Block: 101, BlockHash: w.chain.hash(101)}
		}},
		{"admission header read fails", func(_ *world, f *faultChain) { f.headerErrAt[90] = true }},
		{"head changed during the read", func(w *world, _ *faultChain) { w.chain.flipAfter = 2 }},
	} {
		t.Run("cancellation/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w, f := cancellation(t)
			tc.edit(w, f)
			noPanic(t, "ReadCancellation", func() {
				if v, err := (ChainReservations{Chain: f}).ReadCancellation(ctx, id, hash); err == nil {
					t.Fatalf("view returned: %+v", v)
				}
			})
		})
	}
}
