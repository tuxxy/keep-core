package redemption

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// directPlan builds a payment through signing.NewPlan, bypassing Build, as an
// externally supplied proposal would.
func directPlan(t *testing.T, w *world, values []int64, change int64, locktime uint32) *signing.Plan {
	t.Helper()
	walletScript, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	point := w.main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Locktime: locktime, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}}
	var requests []signing.Request
	for i, id := range w.ids {
		r := w.chain.requests[id]
		tx.Outputs = append(tx.Outputs, &bitcoin.TransactionOutput{Value: values[i], PublicKeyScript: r.Script})
		requests = append(requests, signing.Request{ID: id, Value: values[i], Script: r.Script})
	}
	tx.Outputs = append(tx.Outputs, &bitcoin.TransactionOutput{Value: change, PublicKeyScript: walletScript})
	custody := w.chain.custody[dkg.WalletID(w.wallet.OutputKey)]
	plan, err := signing.NewPlan(signing.Proposal{Wallet: w.wallet, BitcoinGenesis: w.planner.Genesis, Purpose: signing.Redemption, Generation: custody.Generation + 1, StateVersion: custody.StateVersion, Requests: requests, ConflictInput: w.main.Outpoint, Fees: signing.FeePolicy{Minimum: 100, Maximum: int64(w.chain.maxTotalFee), MaximumSatPerVByte: 10}, Transaction: tx, Prevouts: []bitcoin.PreviousOutput{w.main}})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func setMain(w *world, value int64) {
	w.main.Value = value
	w.planner.Bitcoin = fakeUnspent{w.main}
	k := OutpointKey(w.main.Outpoint)
	o := w.chain.outputs[k]
	o.Value = uint64(value)
	w.chain.outputs[k] = o
	c := w.chain.custody[o.WalletID]
	c.Value = uint64(value)
	w.chain.custody[o.WalletID] = c
}

// Review P2: independent validation enforces the change dust floor and
// immediate finality for supplied plans, not only for Build.
func TestValidateRefusesDustChangeAndNonFinalPlans(t *testing.T) {
	ctx := context.Background()
	t.Run("one-sat change", func(t *testing.T) {
		w := newWorld(t)
		setMain(w, 199_001)
		// Gross 100,000 - treasury 500 = 99,500 each; 200-sat shares sum to
		// the 400-sat fee and leave exactly 1 sat of P2TR change.
		plan := directPlan(t, w, []int64{99_300, 99_300}, 1, 0)
		if err := w.planner.Validate(ctx, w.chain.head, plan); !errors.Is(err, ErrDustOutput) {
			t.Fatalf("dust change accepted: %v", err)
		}
		a := attester(w, newMemJournal())
		if _, err := a.Attest(ctx, plan, w.chain.head+10); err == nil {
			t.Fatal("dust change attested")
		}
	})
	t.Run("non-final payment", func(t *testing.T) {
		w := newWorld(t)
		plan := directPlan(t, w, []int64{99_300, 99_300}, 1_000_000-199_000, 0xffffffff)
		if err := w.planner.Validate(ctx, w.chain.head, plan); err == nil {
			t.Fatal("non-final payment accepted")
		}
	})
	t.Run("positive control", func(t *testing.T) {
		w := newWorld(t)
		plan := directPlan(t, w, []int64{99_300, 99_300}, 1_000_000-199_000, 0)
		if err := w.planner.Validate(ctx, w.chain.head, plan); err != nil {
			t.Fatal(err)
		}
	})
}

// Review P2 (cancellation side): a cancellation whose single output is below
// the P2TR dust floor is never attested.
func TestAttestCancellationRefusesDustOutput(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	setMain(w, 1_400)
	walletScript, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	point := w.main.Outpoint
	recipient := w.chain.requests[w.ids[0]].Script
	payment, err := signing.NewPlan(signing.Proposal{Wallet: w.wallet, BitcoinGenesis: w.planner.Genesis, Purpose: signing.Redemption, Generation: 1, StateVersion: 1, Requests: []signing.Request{{ID: w.ids[0], Value: 300, Script: recipient}}, ConflictInput: point, Fees: signing.FeePolicy{Minimum: 100, Maximum: 2000, MaximumSatPerVByte: 10}, Transaction: &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: 300, PublicKeyScript: recipient}, {Value: 500, PublicKeyScript: walletScript}}}, Prevouts: []bitcoin.PreviousOutput{w.main}})
	if err != nil {
		t.Fatal(err)
	}
	id := [32]byte{0x99}
	w.chain.intents[id] = Intent{ID: id, Certificate: Certificate{WalletID: payment.WalletID(), IntentCommitment: payment.Commitment()}, State: IntentTimeoutPending}
	a := attester(w, newMemJournal())
	cancel := func(value int64) *signing.Plan {
		plan, err := signing.NewCancellationPlan(payment, &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: value, PublicKeyScript: walletScript}}})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	if _, _, err = a.AttestCancellation(ctx, payment, cancel(300), id, w.chain.head+10); !errors.Is(err, ErrDustOutput) {
		t.Fatalf("dust cancellation attested: %v", err)
	}
	if _, _, err = a.AttestCancellation(ctx, payment, cancel(400), id, w.chain.head+10); err != nil {
		t.Fatalf("positive control: %v", err)
	}
}

// Review P2 (re-review): the ledger stays readable at capacity, ordinary
// retries stop before the recovery reserve, and every record kind the
// timeout/cancellation recovery path writes is accepted inside the reserve.
func TestLedgerCapacityKeepsHistoryReadable(t *testing.T) {
	ctx := context.Background()
	l := Ledger{Journal: newMemJournal()}
	capacity, reserve := l.limits()
	if capacity != defaultCapacity || reserve != defaultReserve {
		t.Fatal("defaults")
	}
	id := [32]byte{1}
	for i := 0; i < capacity-reserve; i++ {
		if err := l.Append(ctx, Entry{Reservation: id, Kind: KindSignAttempt, Detail: fmt.Sprint(i)}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	for _, k := range []Kind{KindSignAttempt, KindPossibleRelease, KindAttested, KindAdmitted} {
		if err := l.Append(ctx, Entry{Reservation: id, Kind: k, Detail: "overflow"}); err == nil {
			t.Fatalf("ordinary %s consumed the recovery reserve", k)
		}
	}
	if err := l.Append(ctx, Entry{Reservation: id, Kind: KindSignAttempt, Detail: "0"}); err != nil {
		t.Fatal("identical retry not idempotent at capacity:", err)
	}
	recovery := []Kind{KindTimeoutSeen, KindEscalation, KindCancellationAttest, KindCancellationAttempt, KindCancellationSigned, KindCancellationBroadcast, KindSigned, KindBroadcast, KindLateQuarantined, KindOutcome}
	for i := 0; i < reserve; i++ {
		if err := l.Append(ctx, Entry{Reservation: id, Kind: recovery[i%len(recovery)], Detail: fmt.Sprint(i)}); err != nil {
			t.Fatalf("recovery append %d refused: %v", i, err)
		}
	}
	if err := l.Append(ctx, Entry{Reservation: id, Kind: KindOutcome, Detail: "full"}); err == nil {
		t.Fatal("append beyond capacity")
	}
	entries, err := l.Entries(ctx, id)
	if err != nil || len(entries) != capacity {
		t.Fatalf("full ledger unreadable: %d %v", len(entries), err)
	}
	if ok, err := l.Has(ctx, id, KindOutcome, [32]byte{}); err != nil || !ok {
		t.Fatal("full ledger lookup", err)
	}
}
