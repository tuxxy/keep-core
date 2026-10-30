package movement

// Remediation batch 3 acceptance tests: the signing side of a FROST incoming
// sweep, driven the way base keep-core drives a wallet action. Refs
// tuxxy/keep-core#45, #61, #66, #71 (TESTS-BATCH3.md maps #50, #56, #63 and
// #70; the coordination side is pkg/tbtc/frost_incoming_b3_test.go).
//
// Base model (DECISIONS.md D2, D19, tux 2026-10-29): every coordination window
// starts from chain state; nothing is retained between windows; the wallet's
// signers sign with whichever members are available, as base tECDSA signing
// retries with member subsets. Here one round is one window: every online
// operator runs the existing K-07 Planner.IncomingSweep and Mover.Resolve.
//
// The world is in incoming_world_b3_test.go (real DKG, worker, store and
// libp2p; FIXTURE chain, Bitcoin and maintainer). No build tag
// (tuxxy/keep-core#64).

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

func operatorOf(seat uint16) int {
	for op, seats := range b3Topology {
		for _, s := range seats {
			if s == seat {
				return op
			}
		}
	}
	return -1
}

type stepReport struct {
	status Status
	err    error
}

type tickOptions struct {
	// crashAtSigning crashes these operators at their first signing protocol
	// message: after their durable claim and grant, before any signature.
	crashAtSigning map[int]bool
}

func (op *b3Operator) sends() int {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.signingSends
}

// tick is one window: every online operator plans from chain state and
// resolves the plan (sign, broadcast or observe the outcome), bounded. Then
// crashed operators restart from their stores, the miner and maintainer run,
// and the head moves to the next round.
func (w *b3World) tick(o tickOptions) map[int]stepReport {
	w.t.Helper()
	reports := map[int]stepReport{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	crashed := map[int]bool{}
	for i, op := range w.ops {
		if !op.online {
			continue
		}
		wg.Add(1)
		go func(i int, op *b3Operator) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(w.ctx, b3StepBound)
			defer cancel()
			op.mu.Lock()
			op.onSigningSend = nil
			if o.crashAtSigning[i] {
				op.onSigningSend = func() {
					mu.Lock()
					crashed[i] = true
					mu.Unlock()
					cancel()
				}
			}
			op.mu.Unlock()
			var status Status
			plan, err := op.planner.IncomingSweep(ctx)
			if err == nil {
				status, err = op.mover.Resolve(ctx, plan)
			}
			mu.Lock()
			reports[i] = stepReport{status, err}
			mu.Unlock()
		}(i, op)
	}
	wg.Wait()
	for i, op := range w.ops {
		op.mu.Lock()
		op.onSigningSend = nil
		op.mu.Unlock()
		if crashed[i] {
			w.restart(i)
		}
	}
	w.mine()
	r := w.round()
	w.chain.mu.Lock()
	w.chain.head = (r+1)*b3Round + b3Finality
	w.chain.mu.Unlock()
	return reports
}

// settleWithin ticks until no incoming output of the target is pending, at
// most b3Ticks windows.
func (w *b3World) settleWithin() {
	w.t.Helper()
	var all []map[int]stepReport
	for i := 0; i < b3Ticks && len(w.pending()) > 0; i++ {
		all = append(all, w.tick(tickOptions{}))
	}
	if len(w.pending()) > 0 {
		w.t.Fatalf("incoming duty stalled: %d incoming outputs still pending after %d windows (last reports %s)", len(w.pending()), b3Ticks, describe(all))
	}
}

func describe(all []map[int]stepReport) string {
	if len(all) == 0 {
		return "none"
	}
	last := all[len(all)-1]
	s := ""
	for i := 0; i < len(b3Topology); i++ {
		if r, ok := last[i]; ok {
			s += fmt.Sprintf("[op%d %s %v] ", i, r.status, r.err)
		}
	}
	return s
}

// crashAllAfterGrant runs one window in which every operator that reaches
// signing dies at its first signing message: it has claimed and granted the
// window's plan and its share may be on the wire, but no signature exists.
// An operator the window did not select may not sign at all.
func crashAllAfterGrant(t *testing.T, w *b3World) {
	t.Helper()
	w.tick(tickOptions{crashAtSigning: map[int]bool{0: true, 1: true, 2: true}})
	granted := 0
	for _, op := range w.ops {
		if op.sends() > 0 {
			granted++
		}
	}
	if granted == 0 {
		t.Fatal("precondition: no operator reached signing (no grant)")
	}
	if len(w.pending()) == 0 || len(w.btc.broadcasts) > 0 {
		t.Fatal("precondition: the crashed window must leave the output pending and nothing signed")
	}
}

// TestIncomingSweepSettles (guard; harness check): every operator online, a
// pending incoming output with or without a target main is swept by a proven
// transaction.
func TestIncomingSweepSettles(t *testing.T) {
	for _, withMain := range []bool{false, true} {
		withMain := withMain
		t.Run(map[bool]string{false: "mainless", true: "with-main"}[withMain], func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			if withMain {
				w.setMain(150_000)
			}
			w.addIncoming(99_000, 30)
			w.chain.head = 42
			w.settleWithin()
		})
	}
}

// TestIncomingSweepSignsWithOperatorDown (tuxxy/keep-core#66 K09-52, as in
// base): with any one operator down for every window, the seats still
// available (at least the threshold) sign and the sweep settles. Base tECDSA
// signing retries with subsets of the wallet's members; it never needs every
// member.
func TestIncomingSweepSignsWithOperatorDown(t *testing.T) {
	for down := range b3Topology {
		down := down
		t.Run(fmt.Sprintf("operator-%d-down", down), func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			w.setMain(150_000)
			w.addIncoming(99_000, 30)
			w.chain.head = 42
			w.setOnline(down, false)
			w.settleWithin()
		})
	}
}

// TestIncomingSweepNextWindowAfterFailedWindow (tuxxy/keep-core#45 K09-29, as
// in base; also the liveness side of #50 and #56): every operator dies after
// granting a window's plan, a second output becomes final, and every operator
// restarts from its store. The next windows start from chain state again and
// the duty settles; no earlier plan stalls it.
func TestIncomingSweepNextWindowAfterFailedWindow(t *testing.T) {
	t.Parallel()
	w := newB3World(t)
	w.setMain(150_000)
	w.addIncoming(99_000, 30)
	w.chain.head = 42
	crashAllAfterGrant(t, w)
	w.addIncoming(77_000, w.head()-b3Finality)
	w.settleWithin()
}

// TestIncomingSweepAfterMainSpent (tuxxy/keep-core#61 K09-46, as in base):
// every operator granted a plan that spends the target's main, then a proven,
// final transaction spends that main elsewhere. The next windows plan from the
// new main and settle the output.
func TestIncomingSweepAfterMainSpent(t *testing.T) {
	t.Parallel()
	w := newB3World(t)
	w.setMain(150_000)
	w.addIncoming(99_000, 30)
	w.chain.head = 42
	crashAllAfterGrant(t, w)
	w.spendMain(120_000)
	w.advance(b3Round)
	w.settleWithin()
}

// provenFake adds the C-06 proven-transaction read to the fake chain.
type provenFake struct {
	*fake
	proven map[[32]byte][32]byte
}

func (p *provenFake) ProvenTransaction(_ context.Context, _ uint64, h [32]byte) ([32]byte, error) {
	return p.proven[h], nil
}

// TestResolveRecordsOnlyTheProvenSweep (tuxxy/keep-core#71 K09-58): when a
// different, proven transaction swept a plan's inputs, Resolve never records
// that plan's own transaction as the movement outcome. Unit level on the
// existing Mover.Resolve; FIXTURE chain (fake) and journal.
func TestResolveRecordsOnlyTheProvenSweep(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	target := w.targets[0]
	movementTx := [32]byte{0x71}
	key := OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(movementTx)})
	w.f.incoming[key] = Incoming{Target: target.ID, Source: w.source.ID, Value: 99_000, State: IncomingPending, SweepFeeCap: 5_000}
	w.f.incomingLog = []IncomingLog{{Log: Log{Block: 90, BlockHash: w.f.hash(90)}, Outpoint: key, Target: target.ID, Source: w.source.ID, Value: 99_000, TransactionHash: movementTx}}
	p := w.planner()
	p.Wallet = target.Descriptor
	p.Bitcoin = unspent{}
	plan, err := p.IncomingSweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Another transaction, not this plan, swept the input and was proven.
	other := [32]byte{0x72}
	w.f.incoming[key] = Incoming{Target: target.ID, Source: w.source.ID, Value: 99_000, State: IncomingSwept, SweepFeeCap: 5_000}
	chain := &provenFake{fake: w.f, proven: map[[32]byte][32]byte{other: target.ID}}
	ledger := redemption.Ledger{Journal: &journal{records: map[[32]byte][]byte{}}}
	m := &Mover{Chain: chain, Authority: &Authority{View: &View{Chain: chain, Policy: Policy{FinalityBlocks: 2}}}, Ledger: ledger, Schedule: redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 4}, Wallet: target.Descriptor}
	_, _ = m.Resolve(ctx, plan)
	recorded, err := ledger.Has(ctx, ID(plan), redemption.KindMovementOutcome, plan.BitcoinTransactionHash())
	if err != nil {
		t.Fatal(err)
	}
	if recorded {
		t.Fatalf("settlement records the node's own plan %x as the movement outcome, but the chain proved a different transaction", plan.BitcoinTransactionHash())
	}
}
