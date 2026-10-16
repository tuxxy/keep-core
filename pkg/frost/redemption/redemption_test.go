package redemption

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// TestZeroNetAndFeeBoundaries is the K-05 named check that zero or negative
// nets, invalid fee domains and overflow are refused before any plan (and so
// before any signature request) exists. The legacy half is in pkg/tbtc.
func TestZeroNetAndFeeBoundaries(t *testing.T) {
	ctx := context.Background()
	base := func() Request {
		recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{1})
		return Request{Gross: 10_000, TreasuryFee: 100, MaximumBitcoinFee: 1000, Script: recipient}
	}
	for _, tc := range []struct {
		name  string
		edit  func(*Request)
		share int64
		want  error
	}{
		{"treasury above gross wraps", func(r *Request) { r.TreasuryFee = r.Gross + 1 }, 0, ErrZeroNet},
		{"zero net exactly", func(r *Request) { r.MaximumBitcoinFee = 1_000_000 }, 9_900, ErrZeroNet},
		{"negative net", func(r *Request) { r.MaximumBitcoinFee = 1_000_000 }, 9_901, ErrZeroNet},
		{"share above captured cap", func(*Request) {}, 1001, ErrFeeDomain},
		{"negative share", func(*Request) {}, -1, ErrFeeDomain},
		{"dust output", func(r *Request) { r.Gross = 393 }, 0, ErrDustOutput},
		{"gross beyond satoshi domain", func(r *Request) { r.Gross = uint64(bitcoin.MaxSatoshi) + 1 }, 0, ErrAmountDomain},
		{"cap beyond satoshi domain", func(r *Request) { r.MaximumBitcoinFee = ^uint64(0) }, 0, ErrAmountDomain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.edit(&r)
			if _, err := netValue(r, tc.share); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("positive control", func(t *testing.T) {
		if v, err := netValue(base(), 1000); err != nil || v != 8900 {
			t.Fatalf("got %d %v", v, err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*world)
		want error
	}{
		{"zero fee rate", func(w *world) { w.planner.Fees.FeeRate = 0 }, ErrFeeDomain},
		{"fee rate above envelope", func(w *world) { w.planner.Fees.FeeRate = 11 }, ErrFeeDomain},
		{"fee rate overflow", func(w *world) {
			w.planner.Fees.FeeRate = bitcoin.MaxSatoshi
			w.planner.Fees.MaximumSatPerVByte = bitcoin.MaxSatoshi
		}, ErrFeeDomain},
		{"fee above governance total", func(w *world) { w.chain.maxTotalFee = 50 }, ErrFeeDomain},
		{"fee below certificate minimum", func(w *world) { w.planner.Fees.Minimum = 100_000 }, ErrFeeDomain},
		{"share above captured cap", func(w *world) {
			for id, r := range w.chain.requests {
				r.MaximumBitcoinFee = 1
				w.chain.requests[id] = r
			}
		}, ErrFeeDomain},
		{"zero net after fee", func(w *world) {
			for id, r := range w.chain.requests {
				r.Gross, r.TreasuryFee, r.MaximumBitcoinFee = 700, 600, 1000
				w.chain.requests[id] = r
			}
		}, ErrZeroNet},
		{"custody cannot cover change", func(w *world) {
			w.main.Value = 150_000
			w.planner.Bitcoin = fakeUnspent{w.main}
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Value = 150_000
			w.chain.outputs[k] = o
		}, ErrInsufficient},
		{"main output not discoverable by script", func(w *world) { w.planner.Bitcoin = fakeUnspent{} }, ErrNoCustody},
		{"custody value mismatch", func(w *world) {
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Value++
			w.chain.outputs[k] = o
		}, ErrNoCustody},
		{"request past deadline margin", func(w *world) { w.chain.time += 3600 - 599 }, ErrNotRedeemable},
		{"request not yet allowed", func(w *world) {
			for id, r := range w.chain.requests {
				r.NotBefore = w.chain.time + 1
				w.chain.requests[id] = r
			}
		}, ErrNotRedeemable},
		{"request already reserved", func(w *world) {
			for id, r := range w.chain.requests {
				r.State = RequestReserved
				w.chain.requests[id] = r
			}
		}, ErrNotRedeemable},
	} {
		t.Run("plan/"+tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.edit(w)
			if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPlannerBuildsAndValidatesExactEntitlements(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	plan, requests, err := w.planner.Build(ctx, w.chain.head, w.ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || plan.Generation() != 1 || plan.StateVersion() != 1 || OutpointKey(plan.ConflictInput()) != OutpointKey(w.main.Outpoint) {
		t.Fatal("plan slot or conflict input")
	}
	fee := plan.Snapshot().Fee()
	var shareTotal int64
	for i, r := range plan.Requests() {
		share := int64(requests[i].Gross-requests[i].TreasuryFee) - r.Value
		if share < 0 || share > int64(requests[i].MaximumBitcoinFee) {
			t.Fatal("share outside cap")
		}
		shareTotal += share
	}
	if shareTotal != fee || fee != w.planner.Fees.FeeRate*plan.Snapshot().VirtualSize() {
		t.Fatalf("shares %d fee %d", shareTotal, fee)
	}
	if err = w.planner.Validate(ctx, w.chain.head, plan); err != nil {
		t.Fatal(err)
	}
	pending, err := w.planner.Pending(ctx, 0, w.chain.head, 0)
	if err != nil || len(pending) != 2 || pending[0] != w.ids[0] {
		t.Fatalf("pending %v %v", pending, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*world)
	}{
		{"request reserved meanwhile", func(w *world) {
			r := w.chain.requests[w.ids[0]]
			r.State = RequestReserved
			w.chain.requests[w.ids[0]] = r
		}},
		{"captured cap lower than share", func(w *world) {
			r := w.chain.requests[w.ids[1]]
			r.MaximumBitcoinFee = 1
			w.chain.requests[w.ids[1]] = r
		}},
		{"slot already admitted", func(w *world) {
			c := w.chain.custody[plan.WalletID()]
			c.Generation = 1
			w.chain.custody[plan.WalletID()] = c
		}},
		{"state version advanced", func(w *world) {
			c := w.chain.custody[plan.WalletID()]
			c.StateVersion = 2
			w.chain.custody[plan.WalletID()] = c
		}},
		{"main output spent", func(w *world) {
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Spent = true
			w.chain.outputs[k] = o
		}},
		{"governance fee cap lowered", func(w *world) { w.chain.maxTotalFee = 100 }},
		{"redemptions disabled", func(w *world) { w.chain.config.Enabled = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w2 := newWorld(t)
			tc.edit(w2)
			if err := w2.planner.Validate(ctx, w2.chain.head, plan); err == nil {
				t.Fatal("stale or invalid plan accepted")
			}
		})
	}
}

func attester(w *world, j *memJournal) *Attester {
	key, _ := crypto.ToECDSA(append(make([]byte, 31), 7))
	return &Attester{Planner: w.planner, Journal: j, Ledger: Ledger{Journal: j}, Key: key, Seats: []uint16{1}}
}

func TestAttestationSlotLock(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	j := newMemJournal()
	a := attester(w, j)
	plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Attest(ctx, plan, w.chain.head+10)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.Attest(ctx, plan, w.chain.head+10)
	if err != nil || string(again[1]) != string(first[1]) {
		t.Fatal("identical attestation is not idempotent")
	}
	if _, err = a.Attest(ctx, plan, w.chain.head+20); err != nil {
		t.Fatalf("same-intent renewal refused: %v", err)
	}
	if _, err = a.Attest(ctx, plan, w.chain.head+15); !errors.Is(err, ErrConflictingAttestation) {
		t.Fatal("renewal with an earlier deadline accepted")
	}
	other, _, err := w.planner.Build(ctx, w.chain.head, w.ids[:1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Attest(ctx, other, w.chain.head+10); !errors.Is(err, ErrConflictingAttestation) {
		t.Fatalf("conflicting intent before deadlines finalized: %v", err)
	}
	// Both deadlines (head+10 and the renewal head+20) finalize: head-2 >= head+20.
	w.chain.head += 22
	w.chain.custody[plan.WalletID()] = Custody{Generation: 1, StateVersion: 1, Value: 1_000_000, MainOutpoint: OutpointKey(w.main.Outpoint)}
	if _, err = a.Attest(ctx, other, w.chain.head+10); err == nil {
		t.Fatal("conflicting intent accepted for an admitted slot")
	}
	w.chain.custody[plan.WalletID()] = Custody{Generation: 0, StateVersion: 1, Value: 1_000_000, MainOutpoint: OutpointKey(w.main.Outpoint)}
	if _, err = a.Attest(ctx, other, w.chain.head+10); err != nil {
		t.Fatalf("replacement after finalized non-admission refused: %v", err)
	}
	history, err := a.History(ctx, plan.WalletID(), 1, 1)
	if err != nil || len(history) != 3 || history[0].Plan != plan.Commitment() || history[2].Plan != other.Commitment() {
		t.Fatalf("history not retained: %d %v", len(history), err)
	}
	// Signature recovers to the operator for the C-04 digest.
	certificate, _ := CertificateFor(plan, w.chain.head-22+10)
	digest := ReservationDigest(w.chain.ChainID(), w.chain.Bridge(), certificate)
	prefixed := crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n32"), digest[:])
	sig := append([]byte(nil), first[1]...)
	sig[64] -= 27
	pub, err := crypto.SigToPub(prefixed[:], sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != crypto.PubkeyToAddress(a.Key.PublicKey) {
		t.Fatal("certificate signature does not recover to the operator")
	}
	if _, err = a.Attest(ctx, plan, w.chain.head+1000); err == nil {
		t.Fatal("deadline beyond the certificate window accepted")
	}
}

func TestChainReservationsReadsOneCanonicalSnapshot(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	id := [32]byte{0x77}
	w.chain.intents[id] = Intent{ID: id, Certificate: Certificate{IntentCommitment: [32]byte{9}, Generation: 1, StateVersion: 1}, AdmittedBlock: 90, TransactionHash: [32]byte{5}, State: IntentTimeoutPending}
	w.chain.admissions[id] = Admission{ReservationID: id, TransactionHash: [32]byte{5}, Block: 90, BlockHash: w.chain.hash(90)}
	r := ChainReservations{Chain: w.chain}
	v, err := r.ReadReservation(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != "TimeoutPending" || v.Block != 90 || v.BlockHash != v.CanonicalBlockHash || v.Head != 100 || v.Commitment != ([32]byte{9}) {
		t.Fatalf("view %+v", v)
	}
	w.chain.flipAfter = 2
	if _, err = r.ReadReservation(ctx, id); !errors.Is(err, ErrInconsistentRead) {
		t.Fatalf("head change during read not detected: %v", err)
	}
	adm := w.chain.admissions[id]
	adm.BlockHash = [32]byte{1}
	w.chain.admissions[id] = adm
	v, err = r.ReadReservation(ctx, id)
	if err != nil || v.BlockHash == v.CanonicalBlockHash {
		t.Fatal("reorganized admission not visible to the executor check")
	}
	adm.Block = 91
	w.chain.admissions[id] = adm
	if _, err = r.ReadReservation(ctx, id); err == nil {
		t.Fatal("admission log differing from the record accepted")
	}
}

func TestLedgerIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	j := newMemJournal()
	l := Ledger{Journal: j}
	id := [32]byte{1}
	for _, e := range []Entry{{Reservation: id, Kind: KindSigned, Detail: "0"}, {Reservation: id, Kind: KindSigned, Detail: "0"}, {Reservation: id, Kind: KindLateQuarantined}} {
		if err := l.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := l.Entries(ctx, id)
	if err != nil || len(entries) != 2 || entries[1].Kind != KindLateQuarantined {
		t.Fatalf("%v %v", entries, err)
	}
	// A conflicting rewrite of an existing sequence is refused by the store.
	if err = j.RetainSigningRecord(ctx, entryID(id, 0), []byte(`{}`)); err == nil {
		t.Fatal("ledger record overwritten")
	}
}

func TestCollectCountsSeatsOnce(t *testing.T) {
	sig := make([]byte, 65)
	seats, sigs, err := Collect(2, map[uint16][]byte{3: sig}, map[uint16][]byte{1: sig, 3: sig})
	if err != nil || len(seats) != 2 || seats[0] != 1 || seats[1] != 3 || len(sigs) != 130 {
		t.Fatal(seats, err)
	}
	if _, _, err = Collect(3, map[uint16][]byte{1: sig, 2: sig}); err == nil {
		t.Fatal("below threshold accepted")
	}
}

func TestDigestsMatchSolidityEncoding(t *testing.T) {
	ids := [][32]byte{{1}, {2}}
	got, err := RequestIDsHash(ids)
	if err != nil {
		t.Fatal(err)
	}
	// abi.encode(bytes32[]) = offset 0x20, length 2, elements.
	raw := make([]byte, 64)
	raw[31] = 0x20
	raw[63] = 2
	raw = append(raw, ids[0][:]...)
	raw = append(raw, ids[1][:]...)
	if got != crypto.Keccak256Hash(raw) {
		t.Fatal("request IDs hash encoding")
	}
	_ = dkg.WalletID
}
