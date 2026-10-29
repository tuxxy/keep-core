package movement

// Remediation batch 3 acceptance tests: the incoming sweep and its signing
// release. Refs tuxxy/keep-core#45, #50, #56, #61, #63, #64, #66, #70, #71.
//
// The incoming-sweep coordination is a missing K-07 feature on the integration
// branch (K09-HARNESS-PLAN.md section 5). These tests fix its contract first:
// the API shape is in TESTS-BATCH3.md ("API shape for review"). They reach the
// entry point through a runtime interface check, so they compile today and
// fail at "no incoming coordination entry point" until it exists.
//
// They assert behaviour only: what is signed and offered to Bitcoin, what
// the chain settles, what an operator reports as settled, and who leads.
// The world is in incoming_world_b3_test.go (real DKG, worker, store and
// libp2p; FIXTURE chain, Bitcoin and maintainer; ADAPTER recipe channel).
// No build tag (tuxxy/keep-core#64).

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// incomingCoordination is the K-07 incoming-sweep entry point under review.
type incomingCoordination interface {
	// IncomingLeader returns the seat that leads the incoming recipe of a
	// round. Every operator computes the same seat from public data.
	IncomingLeader(ctx context.Context, round uint64) (uint16, error)
	// IncomingStep runs one step of this operator's incoming-sweep duty for
	// the planner's wallet in the current round. Recipes reach the other
	// operators over recipes. When the step finds the duty's inputs settled
	// on chain, it returns StatusSettled and the Bitcoin hash of the proven
	// transaction that settled them, as the operator records it.
	IncomingStep(ctx context.Context, planner Planner, recipes frost.Transport) (Status, [32]byte, error)
}

func coordination(t *testing.T, m *Mover) incomingCoordination {
	t.Helper()
	c, ok := any(m).(incomingCoordination)
	if !ok {
		t.Fatal("no incoming coordination entry point: *movement.Mover has no IncomingLeader(ctx, round) (uint16, error) and IncomingStep(ctx, Planner, frost.Transport) (Status, [32]byte, error) (TESTS-BATCH3.md, API shape for review)")
	}
	return c
}

// requireEntryPoint fails fast, before the world is built.
func requireEntryPoint(t *testing.T) {
	t.Helper()
	coordination(t, &Mover{})
}

// ---------------------------------------------------------------- driver

type stepReport struct {
	status  Status
	settled [32]byte
	err     error
}

type tickOptions struct {
	// crashAtPublish crashes these operators right after their first recipe
	// message of the tick (the message is delivered; the process dies).
	crashAtPublish map[int]bool
	// crashAtSigning crashes these operators at their first signing protocol
	// message: after their durable claim and grant, before any signature.
	crashAtSigning map[int]bool
	// stagger runs operator 0's step first, then calls between, then starts
	// the others in the same round.
	stagger bool
	between func()
}

// tick runs one round: every online operator's step concurrently (bounded),
// restarts crashed operators from their stores, mines and proves what was
// offered, and moves the head to the start of the next round.
func (w *b3World) tick(o tickOptions) map[int]stepReport {
	w.t.Helper()
	reports := map[int]stepReport{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	crashed := map[int]bool{}
	run := func(i int, op *b3Operator) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(w.ctx, b3StepBound)
		defer cancel()
		crash := func() {
			mu.Lock()
			crashed[i] = true
			mu.Unlock()
			cancel()
		}
		w.hub.mu.Lock()
		if o.crashAtPublish[i] {
			w.hub.onSend[i] = crash
		} else {
			delete(w.hub.onSend, i)
		}
		w.hub.mu.Unlock()
		op.mu.Lock()
		if o.crashAtSigning[i] {
			op.onSigningSend = crash
		} else {
			op.onSigningSend = nil
		}
		op.mu.Unlock()
		c := coordination(w.t, op.mover)
		status, settled, err := c.IncomingStep(ctx, op.planner, op.recipes)
		mu.Lock()
		reports[i] = stepReport{status, settled, err}
		mu.Unlock()
	}
	var started []int
	for i, op := range w.ops {
		if !op.online {
			continue
		}
		if o.stagger && len(started) == 1 {
			// let operator 0 plan at the earlier head first
			time.Sleep(b3StepBound / 5)
			if o.between != nil {
				o.between()
			}
		}
		wg.Add(1)
		started = append(started, i)
		go run(i, op)
	}
	wg.Wait()
	for i, op := range w.ops {
		w.hub.mu.Lock()
		delete(w.hub.onSend, i)
		w.hub.mu.Unlock()
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

func (h *recipeHub) sentBy(op int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sent[op]
}

func (op *b3Operator) sends() int {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.signingSends
}

// settleWithin ticks until no incoming output of the target is pending, at
// most b3Ticks rounds, and returns every report.
func (w *b3World) settleWithin(first tickOptions) []map[int]stepReport {
	var all []map[int]stepReport
	o := first
	for i := 0; i < b3Ticks && len(w.pending()) > 0; i++ {
		all = append(all, w.tick(o))
		o = tickOptions{}
	}
	if len(w.pending()) > 0 {
		w.t.Fatalf("incoming duty stalled: %d incoming outputs still pending after %d rounds (last reports %s)", len(w.pending()), b3Ticks, describe(all))
	}
	// one more round so every operator can observe the final proof
	all = append(all, w.tick(tickOptions{}))
	return all
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

// requireSafeSettlement (tuxxy/keep-core#71, K09-58; KC-SF-V3 line 452): no
// two different transactions spending a common incoming output were signed
// and offered, and every settlement an operator reported names a proven
// transaction.
func (w *b3World) requireSafeSettlement(all []map[int]stepReport) {
	w.t.Helper()
	if c := w.conflicts(); len(c) > 0 {
		w.t.Fatalf("two different signed sweeps spend a common incoming output: %x and %x", c[0][0], c[0][1])
	}
	for _, reports := range all {
		for i, r := range reports {
			if r.status == StatusSettled && r.settled != ([32]byte{}) && !w.isProven(r.settled) {
				w.t.Fatalf("operator %d recorded %x as the settled incoming sweep, but no such transaction was proven", i, r.settled)
			}
		}
	}
}

// settledBy returns the proven transaction that swept the incoming output.
func (w *b3World) settledBy(key [32]byte) *bitcoin.Transaction {
	w.btc.mu.Lock()
	defer w.btc.mu.Unlock()
	for h, tx := range w.btc.broadcasts {
		if !w.isProven([32]byte(h)) || !w.btc.confirmed[h] {
			continue
		}
		for _, in := range tx.Inputs {
			if OutpointKey(*in.Outpoint) == key {
				return tx
			}
		}
	}
	return nil
}

func spends(tx *bitcoin.Transaction, key [32]byte) bool {
	for _, in := range tx.Inputs {
		if OutpointKey(*in.Outpoint) == key {
			return true
		}
	}
	return false
}

func operatorsInRound(w *b3World, round uint64) (leader uint16, op int) {
	w.t.Helper()
	l, err := coordination(w.t, w.ops[0].mover).IncomingLeader(w.ctx, round)
	if err != nil {
		w.t.Fatal("IncomingLeader:", err)
	}
	return l, operatorOf(l)
}

// ---------------------------------------------------------------- cases

// TestIncomingSweepSettles (guard; harness check): with every operator online,
// pending incoming outputs, with or without a target main, are swept by one
// signed transaction that the chain proves.
func TestIncomingSweepSettles(t *testing.T) {
	requireEntryPoint(t)
	for _, withMain := range []bool{false, true} {
		withMain := withMain
		t.Run(map[bool]string{false: "mainless", true: "with-main"}[withMain], func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			if withMain {
				w.setMain(150_000)
			}
			w.addIncoming(99_000, 30)
			w.addIncoming(88_000, 31)
			w.chain.head = 42
			w.requireSafeSettlement(w.settleWithin(tickOptions{}))
		})
	}
}

// TestIncomingOutputFinalBetweenTicks (tuxxy/keep-core#45, K09-29): a second
// incoming output that becomes final between two operators' steps in the
// same round never stalls the wallet's duty, in one process or across a
// restart of every operator.
func TestIncomingOutputFinalBetweenTicks(t *testing.T) {
	requireEntryPoint(t)
	for _, restart := range []bool{false, true} {
		restart := restart
		t.Run(map[bool]string{false: "same-process", true: "restart-all"}[restart], func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			w.setMain(150_000)
			w.addIncoming(99_000, 30)
			w.chain.head = 42
			var reports []map[int]stepReport
			reports = append(reports, w.tick(tickOptions{stagger: true, between: func() {
				// final at head 43 (same round 10), not at 42
				w.addIncoming(77_000, 41)
				w.advance(1)
			}}))
			if restart {
				for i := range w.ops {
					w.restart(i)
				}
			}
			reports = append(reports, w.settleWithin(tickOptions{})...)
			w.requireSafeSettlement(reports)
		})
	}
}

// TestIncomingLeaderCrashAtPublish (tuxxy/keep-core#56 K09-41, #63 K09-48,
// #70 K09-57): the round's leader dies the moment its recipe leaves (the
// followers have it; the leader never signed), and a new incoming output
// becomes final before it restarts from its store. The sweep still settles,
// with no second signed recipe for the same inputs. Run for three successive
// rounds, so leaders other than seat 1 are covered.
func TestIncomingLeaderCrashAtPublish(t *testing.T) {
	requireEntryPoint(t)
	for offset := uint64(0); offset < 3; offset++ {
		offset := offset
		t.Run(fmt.Sprintf("round-plus-%d", offset), func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			w.setMain(150_000)
			w.addIncoming(99_000, 30)
			w.chain.head = 42 + offset*b3Round
			leader, op := operatorsInRound(w, w.round())
			reports := []map[int]stepReport{w.tick(tickOptions{crashAtPublish: map[int]bool{op: true}})}
			if w.hub.sentBy(op) == 0 {
				t.Fatalf("precondition: the round's leader (seat %d, operator %d) published no recipe", leader, op)
			}
			w.addIncoming(66_000, w.head()-b3Finality)
			reports = append(reports, w.settleWithin(tickOptions{})...)
			w.requireSafeSettlement(reports)
		})
	}
}

// crashAllAfterGrant runs one round in which every operator dies at its first
// signing message: each has claimed and granted the round's recipe and its
// share may be on the wire, but no signature exists. It returns the incoming
// output that recipe sweeps.
func crashAllAfterGrant(t *testing.T, w *b3World) ([32]byte, []map[int]stepReport) {
	t.Helper()
	w.setMain(150_000)
	first := w.addIncoming(99_000, 30)
	w.chain.head = 42
	all := map[int]bool{0: true, 1: true, 2: true}
	reports := []map[int]stepReport{w.tick(tickOptions{crashAtSigning: all})}
	for i, op := range w.ops {
		if op.sends() == 0 {
			t.Fatalf("precondition: operator %d never reached signing (no grant)", i)
		}
	}
	if len(w.pending()) == 0 || len(w.conflicts()) > 0 {
		t.Fatal("precondition: the crashed round must leave its recipe unsigned")
	}
	return first, reports
}

// TestIncomingGrantPinsRecipe (tuxxy/keep-core#50 K09-34, #56, #70; KC-SF-V3
// line 258): after every operator granted a recipe for output I and died, a
// new output J becomes final. On restart no operator retires the recipe on
// its own word: I is swept by a transaction that does not also spend J (the
// granted recipe, never a replanned superset), and the duty still settles.
func TestIncomingGrantPinsRecipe(t *testing.T) {
	requireEntryPoint(t)
	t.Parallel()
	w := newB3World(t)
	first, reports := crashAllAfterGrant(t, w)
	second := w.addIncoming(77_000, w.head()-b3Finality)
	reports = append(reports, w.settleWithin(tickOptions{})...)
	w.requireSafeSettlement(reports)
	tx := w.settledBy(first)
	if tx == nil {
		t.Fatal("the first incoming output has no proven sweep")
	}
	if spends(tx, second) {
		t.Fatal("a possibly released recipe was replaced: the first output was swept together with an output that became final only after every seat granted the earlier recipe")
	}
}

// TestIncomingPinnedRecipeWaitsWithoutChainEvidence (D3; #50, #61 negative
// side): after every operator granted a recipe, governance lowers the moved
// funds sweep fee cap below its fee. That makes the recipe unprovable but is
// no evidence that it can never confirm on Bitcoin, so no operator signs a
// replacement for the same input. The input waits for the operational path.
func TestIncomingPinnedRecipeWaitsWithoutChainEvidence(t *testing.T) {
	requireEntryPoint(t)
	t.Parallel()
	w := newB3World(t)
	first, reports := crashAllAfterGrant(t, w)
	w.chain.mu.Lock()
	w.chain.params.MovedFundsSweepTxMaxTotalFee = 10
	w.chain.mu.Unlock()
	for i := 0; i < b3Ticks; i++ {
		reports = append(reports, w.tick(tickOptions{}))
	}
	w.requireSafeSettlement(reports)
	if tx := w.settledBy(first); tx != nil {
		t.Fatalf("a possibly released recipe was replaced without chain evidence: %x swept its input after every seat granted the earlier recipe", [32]byte(tx.Hash()))
	}
}

// TestIncomingRecipeRetiredAfterMainSpent (tuxxy/keep-core#61 K09-46, #71
// K09-58): a recipe that spends the target's main is retired once a proven,
// final transaction spends that main elsewhere, whether nobody granted it
// (the followers were down when the leader published) or every operator
// did. A fresh recipe settles the input, and every operator that reports the
// settlement names that proven transaction, never the retired recipe.
func TestIncomingRecipeRetiredAfterMainSpent(t *testing.T) {
	requireEntryPoint(t)
	for _, granted := range []bool{false, true} {
		granted := granted
		t.Run(map[bool]string{false: "leader-only", true: "all-granted"}[granted], func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			var reports []map[int]stepReport
			var first [32]byte
			if granted {
				first, reports = crashAllAfterGrant(t, w)
			} else {
				w.setMain(150_000)
				first = w.addIncoming(99_000, 30)
				w.chain.head = 42
				_, op := operatorsInRound(w, w.round())
				for i := range w.ops {
					if i != op {
						w.setOnline(i, false)
					}
				}
				reports = append(reports, w.tick(tickOptions{}))
				if w.hub.sentBy(op) == 0 {
					t.Fatal("precondition: the leader published no recipe")
				}
				for i := range w.ops {
					w.setOnline(i, true)
				}
			}
			w.spendMain(120_000)
			w.advance(b3Round)
			reports = append(reports, w.settleWithin(tickOptions{})...)
			w.requireSafeSettlement(reports)
			tx := w.settledBy(first)
			if tx == nil {
				t.Fatal("the incoming output has no proven sweep")
			}
			for i := range w.ops {
				var named [32]byte
				for _, r := range reports {
					if rep, ok := r[i]; ok && rep.status == StatusSettled && rep.settled != ([32]byte{}) {
						named = rep.settled
					}
				}
				if named != [32]byte(tx.Hash()) {
					t.Fatalf("operator %d settlement record names %x, want the proven sweep %x", i, named, [32]byte(tx.Hash()))
				}
			}
		})
	}
}

// TestIncomingSettlesWithOperatorDown (tuxxy/keep-core#66 K09-52; SC-08): with
// any one operator down for the whole case, including the one holding the
// first round's leader seat, the remaining seats (at least the threshold)
// settle the sweep.
func TestIncomingSettlesWithOperatorDown(t *testing.T) {
	requireEntryPoint(t)
	for down := range b3Topology {
		down := down
		t.Run(fmt.Sprintf("operator-%d-down", down), func(t *testing.T) {
			t.Parallel()
			w := newB3World(t)
			w.setMain(150_000)
			w.addIncoming(99_000, 30)
			w.chain.head = 42
			w.setOnline(down, false)
			w.requireSafeSettlement(w.settleWithin(tickOptions{}))
		})
	}
}

// ---------------------------------------------------------------- rotation

// weighted is a 100-seat wallet (D2 topology) with uneven operator weights,
// no operator at the 51-seat threshold. Seats are dealt in a fixed shuffle.
func weighted(t *testing.T) (*Mover, []int) {
	t.Helper()
	weights := []int{30, 25, 20, 15, 10}
	owner := make([]int, 0, 100)
	for op, n := range weights {
		for i := 0; i < n; i++ {
			owner = append(owner, op)
		}
	}
	for i := range owner {
		j := (i*37 + 11) % len(owner)
		owner[i], owner[j] = owner[j], owner[i]
	}
	d := descriptor(t, 9)
	d.Members = make([]uint32, 100)
	d.Operators = make([]common.Address, 100)
	for i := range owner {
		d.Members[i] = uint32(owner[i] + 1)
		d.Operators[i] = common.BigToAddress(big.NewInt(int64(owner[i] + 1)))
	}
	d.Threshold = 51
	if err := dkg.ValidateDescriptor(d); err != nil {
		t.Fatal(err)
	}
	chain := &chainFixture{head: 100_000}
	return &Mover{Chain: chain, Wallet: d}, owner
}

// TestIncomingLeaderRotates (tuxxy/keep-core#66, D2 mechanism b: rotation):
// the leader is a seat of the wallet, the same for every operator, and it
// changes from round to round; every operator leads within 200 rounds.
func TestIncomingLeaderRotates(t *testing.T) {
	requireEntryPoint(t)
	m, owner := weighted(t)
	other := *m
	led := map[int]int{}
	seats := map[uint16]bool{}
	for r := uint64(1000); r < 1200; r++ {
		a, err := coordination(t, m).IncomingLeader(context.Background(), r)
		if err != nil {
			t.Fatal("IncomingLeader:", err)
		}
		b, err := coordination(t, &other).IncomingLeader(context.Background(), r)
		if err != nil || a != b {
			t.Fatalf("round %d: operators disagree on the leader (%d, %d, %v)", r, a, b, err)
		}
		if a == 0 || int(a) > len(owner) {
			t.Fatalf("round %d: leader %d is not a seat of the wallet", r, a)
		}
		seats[a] = true
		led[owner[a-1]]++
	}
	if len(seats) < 2 {
		t.Fatal("the incoming leader never rotates: one seat leads every round")
	}
	for op := 0; op < 5; op++ {
		if led[op] == 0 {
			t.Fatalf("operator %d never leads in 200 rounds", op)
		}
	}
}

// TestIncomingLeaderShareBySeat_Tentative (tuxxy/keep-core#66; D2 rotation
// "by seat", TENTATIVE, tux 2026-10-29). Only this test changes if D2 changes:
// an operator leads in proportion to its seats, so heavier operators lead
// more often than lighter ones.
func TestIncomingLeaderShareBySeat_Tentative(t *testing.T) {
	requireEntryPoint(t)
	m, owner := weighted(t)
	const rounds = 4000
	led := map[int]int{}
	for r := uint64(5000); r < 5000+rounds; r++ {
		seat, err := coordination(t, m).IncomingLeader(context.Background(), r)
		if err != nil || seat == 0 || int(seat) > len(owner) {
			t.Fatalf("IncomingLeader(%d) = %d, %v", r, seat, err)
		}
		led[owner[seat-1]]++
	}
	weights := []int{30, 25, 20, 15, 10}
	for op, w := range weights {
		share := float64(led[op]) / rounds
		want := float64(w) / 100
		if share < want-0.03 || share > want+0.03 {
			t.Errorf("TENTATIVE (D2 by seat): operator %d with %d of 100 seats led %.3f of rounds, want %.2f ± 0.03", op, w, share, want)
		}
	}
	if led[0] <= led[4] {
		t.Errorf("TENTATIVE (D2 by seat): the 30-seat operator led %d rounds, not more than the 10-seat operator's %d", led[0], led[4])
	}
}
