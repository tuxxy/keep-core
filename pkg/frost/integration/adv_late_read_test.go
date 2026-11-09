package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// flakyIntentChain is a test adapter over the real Bridge reader: once armed,
// the next Intent read fails with a provider error (a dropped RPC
// connection), then reads pass through again. The chain state is untouched.
type flakyIntentChain struct {
	*eth.FrostRedemptionChain
	armed atomic.Bool
	fired atomic.Int32
}

func (c *flakyIntentChain) Intent(ctx context.Context, block uint64, id [32]byte) (redemption.Intent, error) {
	if c.armed.CompareAndSwap(true, false) {
		c.fired.Add(1)
		return redemption.Intent{}, errors.New("injected provider error: connection reset by peer")
	}
	return c.FrostRedemptionChain.Intent(ctx, block, id)
}

// TestTransientReadAfterSignatureDoesNotStrandPayment (adversarial review
// ADV-signing S1; KC-VER P-K05-LATE-SIGNATURE-QUARANTINE): a single failed
// Bridge read right after every operator's worker returned its payment
// signature, while the reservation stays Reserved, must not quarantine the
// payment for good. The real K-05 resolver must still pay the redeemer in a
// later round: signed, broadcast, accepted by Bitcoin Core and proven.
func TestTransientReadAfterSignatureDoesNotStrandPayment(t *testing.T) {
	w := newK05WorldWith(t, certifyAllSeats)
	ctx := w.f.ctx
	id := w.request(60_000, p2wpkh(0x71))
	payment, reservation := w.admit()

	flaky := make([]*flakyIntentChain, len(w.resolvers))
	for i, r := range w.resolvers {
		flaky[i] = &flakyIntentChain{FrostRedemptionChain: w.chain}
		r.Chain = flaky[i]
		f := flaky[i]
		// Arm right after this operator's done record is durable, i.e.
		// after the signature returned and before the acceptance read.
		w.journals[i].setOnDone(func([]byte) { f.armed.Store(true) })
	}
	errs := w.signAll(payment, reservation)
	for i, j := range w.journals {
		j.setOnDone(nil)
		flaky[i].armed.Store(false)
		if flaky[i].fired.Load() != 1 {
			t.Fatalf("operator %d: the injected read failure did not fire once (%d)", i, flaky[i].fired.Load())
		}
		if errs[i] == nil {
			t.Fatalf("operator %d: signing succeeded although its acceptance read failed", i)
		}
	}
	intent, err := w.chain.Intent(ctx, w.head(), reservation)
	if err != nil || intent.State != redemption.IntentReserved {
		t.Fatalf("precondition: reservation must still be Reserved: %v %v", intent.State, err)
	}
	for i, r := range w.resolvers {
		status, err := r.Resolve(ctx, payment, reservation)
		if status == redemption.StatusNeedsCancellation {
			t.Fatalf("ADV-signing S1: operator %d: one failed read after the signature quarantined a still-Reserved payment for good (Resolve: %s, sign error: %v)", i, status, errs[i])
		}
		if err != nil {
			t.Fatalf("operator %d: %s %v", i, status, err)
		}
	}

	// Next round: the same plan is signed again and paid.
	w.rpc("anvil_mine", fmt.Sprintf("0x%x", k05RoundBlocks))
	statuses := make([]redemption.Status, len(w.resolvers))
	resolveErrs := make([]error, len(w.resolvers))
	var wg sync.WaitGroup
	for i, r := range w.resolvers {
		wg.Add(1)
		go func(i int, r *redemption.Resolver) {
			defer wg.Done()
			statuses[i], resolveErrs[i] = r.Resolve(ctx, payment, reservation)
		}(i, r)
	}
	wg.Wait()
	for i := range resolveErrs {
		if resolveErrs[i] != nil || statuses[i] != redemption.StatusSigned {
			t.Fatalf("operator %d: next round did not sign: %s %v", i, statuses[i], resolveErrs[i])
		}
	}
	if status, err := w.resolvers[0].Resolve(ctx, payment, reservation); err != nil || status != redemption.StatusBroadcast {
		t.Fatalf("signed payment not broadcast: %s %v", status, err)
	}
	if _, err := w.core.Transaction(ctx, bitcoin.Hash(payment.BitcoinTransactionHash())); err != nil {
		t.Fatal("payment did not reach Bitcoin Core:", err)
	}
	w.coreMine(1)
	if actions, err := w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 || actions[0].Kind != "payment-proof" {
		t.Fatalf("%+v %v", actions, err)
	}
	r, err := w.chain.Request(ctx, w.head(), id)
	if err != nil || r.State != redemption.RequestPaid {
		t.Fatalf("redeemer not paid: %v %v", r.State, err)
	}
}
