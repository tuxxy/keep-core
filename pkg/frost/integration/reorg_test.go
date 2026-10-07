package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

type reorgControl struct {
	mu        sync.Mutex
	snapshot  string
	triggered bool
	fixture   *fixture
	observed  chan struct{}
	changed   chan struct{}
}
type reorgChain struct {
	dkg.Chain
	control *reorgControl
}

func (c reorgChain) Approve(ctx context.Context) error {
	c.control.mu.Lock()
	if c.control.snapshot == "" {
		if err := c.control.fixture.rpc.CallContext(ctx, &c.control.snapshot, "evm_snapshot"); err != nil {
			c.control.mu.Unlock()
			return err
		}
	}
	c.control.mu.Unlock()
	return c.Chain.Approve(ctx)
}
func (c reorgChain) View(ctx context.Context, epoch uint64) (dkg.View, error) {
	v, err := c.Chain.View(ctx, epoch)
	if err != nil {
		return v, err
	}
	if v.ApprovedID != ([32]byte{}) {
		c.control.mu.Lock()
		if !c.control.triggered && c.control.snapshot != "" {
			c.control.triggered = true
			close(c.control.observed)
			go func() {
				// Let the executor observe the approval, then remove the actual block.
				timer := time.NewTimer(200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
				c.control.fixture.stopMining()
				c.control.fixture.chainMu.Lock()
				defer c.control.fixture.chainMu.Unlock()
				var ok bool
				if e := c.control.fixture.rpc.CallContext(c.control.fixture.ctx, &ok, "evm_revert", c.control.snapshot); e == nil && ok {
					var out interface{}
					_ = c.control.fixture.rpc.CallContext(c.control.fixture.ctx, &out, "evm_mine")
					close(c.control.changed)
				}
			}()
		}
		c.control.mu.Unlock()
	}
	return v, nil
}
func TestApprovalReorg(t *testing.T) {
	f := setup(t, "1,2,3")
	control := &reorgControl{fixture: f, observed: make(chan struct{}), changed: make(chan struct{})}
	for _, e := range f.executors {
		e.FinalityBlocks = 100
		e.Chain = reorgChain{e.Chain, control}
	}
	results := make(chan error, len(f.executors))
	var workers sync.WaitGroup
	t.Cleanup(func() {
		f.cancel()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("reorg executor cleanup timed out")
		}
	})
	for _, e := range f.executors {
		workers.Add(1)
		go func(e *dkg.Executor) { defer workers.Done(); _, err := e.Run(f.ctx); results <- err }(e)
	}
	select {
	case <-control.changed:
	case <-time.After(20 * time.Second):
		f.cancel()
		t.Fatal("canonical approval block was not removed")
	}
	quarantined := false
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for !quarantined {
		select {
		case err := <-results:
			if errors.Is(err, dkg.ErrQuarantined) {
				quarantined = true
			}
		case <-timeout.C:
			f.cancel()
			t.Fatal("removed approval was not quarantined")
		}
	}
	for _, j := range f.journals {
		if _, err := j.LoadKey(f.ctx); err == nil {
			t.Fatal("orphaned approval installed a key")
		}
	}
	f.cancel()
}
