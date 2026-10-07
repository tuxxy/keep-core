package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

type candidateGate struct {
	once sync.Once
	hit  chan struct{}
}

func (g *candidateGate) WaitCandidateAcceptance(ctx context.Context, _ frost.Candidate) (frost.Receipt, error) {
	g.once.Do(func() { close(g.hit) })
	<-ctx.Done()
	return frost.Receipt{}, ctx.Err()
}

type gatedEngine struct {
	dkg.Engine
	gate *candidateGate
}

func (e gatedEngine) DKG(ctx context.Context, r frost.DKGRequest, p frost.Providers) (frost.KeyReady, error) {
	p.Acceptance = e.gate
	return e.Engine.DKG(ctx, r, p)
}
func TestFrostDKGCandidateCrashRequiresFreshEpoch(t *testing.T) {
	f := setup(t, "1,2,3")
	gate := &candidateGate{hit: make(chan struct{})}
	f.executors[0].Engine = gatedEngine{f.executors[0].Engine, gate}
	done := f.start()
	select {
	case <-gate.hit:
	case <-f.ctx.Done():
		t.Fatal("public candidate not reached")
	}
	if err := killWorker(f.workers[0]); err != nil {
		t.Fatal(err)
	}
	view, err := f.executors[1].Chain.View(f.ctx, f.deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	if view.ApprovedID != ([32]byte{}) {
		t.Fatal("candidate crash already approved")
	}
	f.mineTo(view.ResultDeadline)
	r := f.await(done)
	for i, err := range r.errs {
		if err == nil {
			t.Fatal("incomplete candidate completed", i)
		}
		status, err := f.journals[i].DKGStatus(f.ctx)
		if err != nil || status.State != frost.DKGSeatsLost {
			t.Fatal("candidate crash lost-seat report", i, err)
		}
	}
	f.nextEpoch()
	_, errs := f.run()
	for _, err := range errs {
		if err != nil {
			t.Fatal("fresh epoch failed", err)
		}
	}
}
