package signing

import (
	"context"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Review P1: a certified cancellation must be minable now. A non-final
// alternative at the top of the fee envelope would block every later
// alternative (which must pay strictly more) and strand recovery.
func TestCancellationPlanRequiresImmediateFinality(t *testing.T) {
	p, _, _ := planFixture(t)
	payment, err := NewPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	change := p.Prevouts[0].PublicKeyScript
	cancellation := func(sequence, locktime uint32) *bitcoin.Transaction {
		point := p.Prevouts[0].Outpoint
		return &bitcoin.Transaction{Version: 2, Locktime: locktime, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: sequence}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100000 - 1000, PublicKeyScript: change}}}
	}
	for _, tc := range []struct {
		name               string
		sequence, locktime uint32
		ok                 bool
	}{
		{"final now", 0xfffffffd, 0, true},
		{"locktime disabled by final sequences", 0xffffffff, 0xffffffff, true},
		{"far-future absolute locktime", 0xfffffffd, 0xffffffff, false},
		{"any nonzero absolute locktime (conservative)", 0xfffffffd, 1, false},
		{"relative locktime enabled", 0x00000001, 0, false},
		{"relative time lock enabled", 0x00400001, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCancellationPlan(payment, cancellation(tc.sequence, tc.locktime))
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v err=%v", tc.ok, err)
			}
		})
	}
}

type confirmedStub struct {
	*bitcoinStub
	confirmedSpent bool
}

func (c confirmedStub) ConfirmedUnspent(context.Context, bitcoin.TransactionOutpoint) (bool, error) {
	return !c.confirmedSpent, nil
}

// A cancellation replaces a payment that may sit unconfirmed in the mempool
// spending the same inputs: its prevout check ignores that mempool spend, and
// refuses a provider that cannot exclude the mempool.
func TestCancellationPrevoutIgnoresMempoolPayment(t *testing.T) {
	e, _, btc, _ := executorFixture(t)
	payment := e.config.Plan
	tx := payment.Snapshot().Unsigned()
	script, _ := bitcoin.PayToTaproot(payment.Snapshot().OutputKey())
	cancellation, err := NewCancellationPlan(payment, &bitcoin.Transaction{Version: 2, Inputs: tx.Inputs, Outputs: []*bitcoin.TransactionOutput{{Value: 99_000, PublicKeyScript: script}}})
	if err != nil {
		t.Fatal(err)
	}
	outpoint := *tx.Inputs[0].Outpoint
	btc.spent = true // the payment is in the mempool
	e.config.Plan = cancellation
	if _, err = e.unspent(context.Background(), outpoint); err == nil {
		t.Fatal("cancellation signed without a confirmed-state provider")
	}
	e.config.Bitcoin = confirmedStub{bitcoinStub: btc}
	if ok, err := e.unspent(context.Background(), outpoint); err != nil || !ok {
		t.Fatal("mempool payment blocked its own cancellation", err)
	}
	e.config.Bitcoin = confirmedStub{bitcoinStub: btc, confirmedSpent: true}
	if ok, _ := e.unspent(context.Background(), outpoint); ok {
		t.Fatal("cancellation of a confirmed spend")
	}
	e.config.Plan = payment
	if ok, _ := e.unspent(context.Background(), outpoint); ok {
		t.Fatal("payment ignored a mempool spend")
	}
}
