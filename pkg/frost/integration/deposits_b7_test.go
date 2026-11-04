package integration

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// flakyClock is a TEST median-time-past clock that fails its next read once
// armed: a flaky Bitcoin provider at the moment after a sweep signature.
type flakyClock struct {
	inner deposit.Clock
	mu    sync.Mutex
	fail  int
}

func (c *flakyClock) MedianTimePast(ctx context.Context) (uint64, error) {
	c.mu.Lock()
	fail := c.fail > 0
	if fail {
		c.fail--
	}
	c.mu.Unlock()
	if fail {
		return 0, errors.New("test: transient Bitcoin provider error")
	}
	return c.inner.MedianTimePast(ctx)
}

func (c *flakyClock) arm(n int) {
	c.mu.Lock()
	c.fail = n
	c.mu.Unlock()
}

// TestSweepTransientReadAfterSignatureRetries (ADV-signing S2, batch 7):
// a sweep whose authorization read fails once right after the signature is
// not quarantined. Nothing is accepted or broadcast in that round, and the
// next round signs, broadcasts and proves the sweep once. A refusal the chain
// still shows stays quarantined (TestLateDepositRecovery).
func TestSweepTransientReadAfterSignatureRetries(t *testing.T) {
	w := newK06WorldWith(t, k06Options{mode: "k05", beforeDKG: certifyAllSeats})
	ctx := w.f.ctx
	// Each operator's authority reads its own flaky clock.
	clocks := make([]*flakyClock, len(w.sweepers))
	for i, s := range w.sweepers {
		clocks[i] = &flakyClock{inner: w.view.Clock}
		s.Authority = &deposit.Authority{View: &deposit.View{Chain: w.view.Chain, Clock: clocks[i], Policy: w.view.Policy}}
	}

	g := w.deposit(1_000_000, w.locktime(k06RefundDelay), [20]byte{})
	r := w.round(g.readyBlock + k06Finality)
	plan := w.plan(r)
	// Every operator's first post-signature authorization read fails once.
	for i, j := range w.journals {
		clock := clocks[i]
		j.setOnDone(func([]byte) { clock.arm(1) })
	}
	for i, err := range w.signSweep(plan, r.Number) {
		if err == nil {
			t.Fatalf("operator %d accepted a signature whose authorization was unread", i)
		}
		if errors.Is(err, signing.ErrLateSignature) {
			t.Fatalf("ADV-S2: operator %d quarantined the sweep on one transient read error: %v", i, err)
		}
	}
	for _, j := range w.journals {
		j.setOnDone(nil)
	}
	for i, s := range w.sweepers {
		if err := s.Broadcast(ctx, plan); err == nil {
			t.Fatalf("operator %d broadcast a sweep with no accepted signature", i)
		}
	}
	if unspent, err := w.core.Unspent(ctx, g.funding); err != nil || !unspent {
		t.Fatal("the unaccepted sweep reached Bitcoin", err)
	}

	// The next round signs again; the sweep is broadcast and proven once.
	next := w.round(r.Anchor + 1)
	w.sweepAndProve(plan, next.Number, w.sweeps)
	for i, s := range w.sweepers {
		status, err := s.Resolve(ctx, plan)
		if err != nil || status != deposit.StatusSwept {
			w.mine(k06Finality)
			if status, err = s.Resolve(ctx, plan); err != nil || status != deposit.StatusSwept {
				t.Fatalf("operator %d: %s %v", i, status, err)
			}
		}
	}
}
