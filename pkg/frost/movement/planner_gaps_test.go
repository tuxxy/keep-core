package movement

// Mutation batch M6-movement: the planner and commitment submitter
// (planner.go). Invariants (KC-SF-V3 K-07, C-06; the Planner doc comments):
//   - Targets names min(eligibleTargets, ceil(value / walletMaxBtcTransfer))
//     distinct eligible wallets, lowest labels first, never the source; it
//     refuses when nothing can move or fewer eligible wallets are seen than
//     counted; any failed read refuses;
//   - Commit submits once: a recorded commitment of the period is success
//     when it names the same targets and a refusal otherwise; a failed
//     submission is success only when the reread shows the same targets;
//   - Movement and IncomingSweep plan only under a valid fee policy, from a
//     readable snapshot, with the custody main found by exact outpoint and
//     value, and a coordinated fee within the cap;
//   - IncomingSweep includes exactly the final, pending or escalated records
//     of this wallet, at most MaxSweepInputs-1 of them, so the main fits.
//
// The fee-policy cases with a coordinated Fee ("zero fee rate, coordinated
// fee", "rate above ceiling, coordinated") pin this code's fail-closed
// configuration check (planner.go:182, :238): an invalid FeeRate refuses
// even when Fee replaces it. Neither the spec nor stock keep-core states
// that rule (stock moving-funds members use the proposal's fee and keep no
// local rate), and production sets FeeRate 1, so it never triggers there.
//
// Unit level on the real Planner and Commitments over the fixture chain.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// TestTargetsCountAndOrder: distinct eligible labels, lowest first, as many
// as the value needs and the counters allow; duplicates in the ready list and
// the source itself are never named twice or at all.
func TestTargetsCountAndOrder(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	// Every ready wallet appears twice, the source among them.
	g.f.ready = append(append([][32]byte(nil), g.f.ready...), g.f.ready...)
	s, err := g.view.Source(ctx, g.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	var labels [][20]byte
	for _, w := range g.targets {
		labels = append(labels, Label(w.ID))
	}
	sort.Slice(labels, func(i, j int) bool { return bytes.Compare(labels[i][:], labels[j][:]) < 0 })
	for _, c := range []struct {
		value, max, eligible uint64
		want                 int
	}{{300_000, 100_000, 4, 3}, {300_001, 100_000, 4, 4}, {100_000, 100_000, 4, 1}, {300_000, 100_000, 2, 2}, {1, 100_000, 4, 1}} {
		s.Custody.Value, s.Parameters.WalletMaxBtcTransfer = c.value, c.max
		g.f.counters.EligibleTargets = c.eligible
		got, err := g.planner().Targets(ctx, s)
		if err != nil {
			t.Fatalf("value %d, cap %d, %d eligible: %v", c.value, c.max, c.eligible, err)
		}
		if !sameLabels(got, labels[:c.want]) {
			t.Fatalf("value %d, cap %d, %d eligible: targets %x, want the %d lowest distinct eligible labels", c.value, c.max, c.eligible, got, c.want)
		}
	}
}

// TestTargetsRefusals: nothing to move, no counted target, fewer eligible
// wallets seen than counted, and any failed read refuse.
func TestTargetsRefusals(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	s, err := g.view.Source(ctx, g.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(s *Source){
		"zero value":         func(s *Source) { s.Custody.Value = 0 },
		"zero transfer cap":  func(s *Source) { s.Parameters.WalletMaxBtcTransfer = 0 },
		"zero value and cap": func(s *Source) { s.Custody.Value, s.Parameters.WalletMaxBtcTransfer = 0, 0 },
		"no eligible target": func(*Source) { g.f.counters.EligibleTargets = 0 },
	} {
		g.f.counters.EligibleTargets = 4
		c := s
		mutate(&c)
		var got [][20]byte
		err := recovered(func() error { var err error; got, err = g.planner().Targets(ctx, c); return err })
		if err == nil || errors.Is(err, errPanic) {
			t.Errorf("%s: targets %x chosen: %v", name, got, err)
		}
	}
	// The log scan misses ready wallets (Policy.From after their WalletReady
	// events): two of the four counted eligible wallets are seen, and
	// 300,000 sat needs three.
	g.f.counters.EligibleTargets = 4
	ready := g.f.ready
	g.f.ready = [][32]byte{g.source.ID, g.targets[0].ID, g.targets[1].ID}
	if got, err := g.planner().Targets(ctx, s); !errors.Is(err, ErrNoTarget) {
		t.Errorf("three targets counted but two seen: chose %x: %v", got, err)
	}
	g.f.ready = ready
	for _, method := range []string{"Counters", "ReadyWallets", "Wallet"} {
		g.fc.failOn(method)
		if _, err := g.planner().Targets(ctx, s); !errors.Is(err, errInjected) {
			t.Errorf("a failed %s read did not refuse the targets: %v", method, err)
		}
		g.fc.failOn()
	}
}

// TestEligibleRule: C-06 isMovementTarget: ReadyUnfunded or Live, not
// quarantined, capability version above zero.
func TestEligibleRule(t *testing.T) {
	for state := StateUnknown; state <= StateTerminated; state++ {
		for _, quarantined := range []bool{false, true} {
			for _, capability := range []uint32{0, 1} {
				w := Wallet{State: state, Quarantined: quarantined, CapabilityVersion: capability}
				want := (state == StateReadyUnfunded || state == StateLive) && !quarantined && capability > 0
				if w.Eligible() != want {
					t.Errorf("state %d quarantined %v capability %d: eligible %v, want %v", state, quarantined, capability, w.Eligible(), want)
				}
			}
		}
	}
}

// TestCommitSourceState: MovingFunds and Terminated sources commit; any
// other state refuses before anything is submitted; a failed read refuses.
func TestCommitSourceState(t *testing.T) {
	ctx := context.Background()
	for state := StateUnknown; state <= StateTerminated; state++ {
		g := newGapWorld(t)
		src := g.f.wallets[Label(g.source.ID)]
		src.State = state
		g.f.wallets[Label(g.source.ID)] = src
		targets, err := g.commitments().Commit(ctx)
		switch state {
		case StateMovingFunds, StateTerminated:
			if err != nil || len(targets) != 3 || !sameLabels(g.f.submitted, targets) {
				t.Errorf("source state %d must commit: %v", state, err)
			}
		default:
			if !errors.Is(err, ErrWalletState) || g.f.submitted != nil {
				t.Errorf("source state %d committed: %v", state, err)
			}
		}
	}
	g := newGapWorld(t)
	g.fc.failOn("Head")
	if _, err := g.commitments().Commit(ctx); !errors.Is(err, errInjected) || g.f.submitted != nil {
		t.Errorf("a failed source read did not refuse the commitment: %v", err)
	}
}

// TestCommitRecordedCommitment: a recorded commitment is success only when
// it names the same targets, in order; another length or another label
// refuses. Nothing is submitted either way.
func TestCommitRecordedCommitment(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		recorded func(w [][20]byte, spare [20]byte) [][20]byte
		ok       bool
	}{
		"same targets":     {func(w [][20]byte, _ [20]byte) [][20]byte { return w }, true},
		"one target fewer": {func(w [][20]byte, _ [20]byte) [][20]byte { return w[:2] }, false},
		"one target more": {func(w [][20]byte, spare [20]byte) [][20]byte {
			return append(append([][20]byte(nil), w...), spare)
		}, false},
		"one target differs": {func(w [][20]byte, spare [20]byte) [][20]byte {
			o := append([][20]byte(nil), w...)
			o[2] = spare
			return o
		}, false},
		"order differs": {func(w [][20]byte, _ [20]byte) [][20]byte { return [][20]byte{w[1], w[0], w[2]} }, false},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGapWorld(t)
			derived, err := g.commitments().Commit(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// spare is the one eligible wallet the commitment does not name.
			spare := Label(g.targets[3].ID)
			cm := g.f.commitments[g.source.ID]
			cm.Targets = c.recorded(derived, spare)
			g.f.commitments[g.source.ID] = cm
			g.f.submitted = nil
			got, err := g.commitments().Commit(ctx)
			if g.f.submitted != nil {
				t.Fatal("a recorded commitment was submitted again")
			}
			if c.ok && (err != nil || !sameLabels(got, derived)) {
				t.Fatalf("the recorded commitment of the same targets was refused: %v", err)
			}
			if !c.ok && !errors.Is(err, ErrPlanMismatch) {
				t.Fatalf("a recorded commitment of other targets was accepted: %x, %v", got, err)
			}
		})
	}
}

// TestCommitFailedSubmission: a failed submission is success only when the
// reread shows a commitment of the same targets (another operator's, or a
// lost reply); nothing recorded, other targets, or a failed reread refuse.
func TestCommitFailedSubmission(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		// records gets spare, an eligible wallet the commitment does not
		// name (newWorld's highest label), so other targets stay readable.
		records func(targets [][20]byte, spare [20]byte) [][20]byte
		reread  bool
		ok      bool
	}{
		"nothing recorded":       {nil, false, false},
		"same targets recorded":  {func(t [][20]byte, _ [20]byte) [][20]byte { return t }, false, true},
		"other targets recorded": {func(t [][20]byte, spare [20]byte) [][20]byte { return [][20]byte{t[0], t[1], spare} }, false, false},
		"reread fails":           {func(t [][20]byte, _ [20]byte) [][20]byte { return t }, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGapWorld(t)
			spare := Label(g.targets[3].ID)
			g.fc.submitErr = errInjected
			// The first source read succeeds; with reread, every read after
			// the submission fails.
			recorded := false
			if c.records != nil {
				g.fc.submitRecords = func(_ [32]byte, tg [][20]byte) [][20]byte {
					recorded = true
					return c.records(tg, spare)
				}
			}
			if c.reread {
				g.fc.failWhen = func(m string, _ uint64) bool { return recorded && m == "Head" }
			}
			got, err := g.commitments().Commit(ctx)
			if c.ok && (err != nil || len(got) != 3) {
				t.Fatalf("a submission whose commitment is recorded with the same targets was refused: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("a failed submission was reported as committed (%s): %x", name, got)
			}
		})
	}
	t.Run("stop before the submission", func(t *testing.T) {
		g := newGapWorld(t)
		c := g.commitments()
		c.boundary = func(name string) error {
			if name == "before-commitment" {
				return errBoundary
			}
			return nil
		}
		if _, err := c.Commit(ctx); !errors.Is(err, errBoundary) || g.f.submitted != nil {
			t.Fatalf("a stop before the commitment did not stop it: %v", err)
		}
	})
}

// movementReady is a world with a finalized commitment, ready to plan.
func movementReady(t *testing.T) *gapWorld {
	t.Helper()
	g := newGapWorld(t)
	g.committedPlan(t)
	return g
}

// TestMovementPlanRefusals: every refusal of Movement, each alone.
func TestMovementPlanRefusals(t *testing.T) {
	ctx := context.Background()
	other := func(g *gapWorld) bitcoin.PreviousOutput {
		o := g.main
		o.Outpoint.OutputIndex = 5
		return o
	}
	for name, c := range map[string]struct {
		mutate func(g *gapWorld, p *Planner)
		want   error
	}{
		"zero fee rate, coordinated fee":  {func(_ *gapWorld, p *Planner) { p.FeeRate, p.Fee = 0, 1_000 }, nil},
		"negative fee rate":               {func(_ *gapWorld, p *Planner) { p.FeeRate = -1 }, nil},
		"rate above ceiling, coordinated": {func(_ *gapWorld, p *Planner) { p.FeeRate, p.MaximumSatPerVByte, p.Fee = 30, 20, 1_000 }, nil},
		"source read fails":               {func(g *gapWorld, _ *Planner) { g.fc.failOn("Head") }, errInjected},
		"nothing committed": {func(g *gapWorld, _ *Planner) {
			delete(g.f.logs, g.source.ID)
			g.f.commitments[g.source.ID] = Commitment{}
		}, ErrPlanMismatch},
		"commitment proven": {func(g *gapWorld, _ *Planner) {
			c := g.f.commitments[g.source.ID]
			c.ProvenAt = 7
			g.f.commitments[g.source.ID] = c
		}, ErrSettled},
		"main not on Bitcoin":   {func(_ *gapWorld, p *Planner) { p.Bitcoin = unspent{} }, redemption.ErrNoCustody},
		"only another outpoint": {func(g *gapWorld, p *Planner) { p.Bitcoin = unspent{[]bitcoin.PreviousOutput{other(g)}} }, redemption.ErrNoCustody},
		"main of another value": {func(g *gapWorld, p *Planner) {
			m := g.main
			m.Value--
			p.Bitcoin = unspent{[]bitcoin.PreviousOutput{m}}
		}, redemption.ErrNoCustody},
		"main with a foreign script": {func(g *gapWorld, p *Planner) {
			m := g.main
			m.PublicKeyScript, _ = bitcoin.PayToTaproot(g.targets[0].Descriptor.OutputKey)
			p.Bitcoin = unspent{[]bitcoin.PreviousOutput{m}}
		}, nil},
		"unspent read fails": {func(_ *gapWorld, p *Planner) { p.Bitcoin = failingUnspent{} }, errInjected},
		"target descriptor changed": {func(g *gapWorld, _ *Planner) {
			l := g.f.commitments[g.source.ID].Targets[1]
			w := g.f.wallets[l]
			w.DescriptorHash = [32]byte{0xd3}
			g.f.wallets[l] = w
		}, ErrPlanMismatch},
		"coordinated fee above the cap":    {func(g *gapWorld, p *Planner) { p.Fee = int64(g.f.commitments[g.source.ID].MaxFee) + 1 }, nil},
		"stop before the movement is read": {func(_ *gapWorld, p *Planner) { p.boundary = stopAt("before-movement-plan") }, errBoundary},
	} {
		t.Run(name, func(t *testing.T) {
			g := movementReady(t)
			p := g.planner()
			c.mutate(g, &p)
			var plan *signing.Plan
			err := recovered(func() error { var err error; plan, err = p.Movement(ctx); return err })
			if err == nil || errors.Is(err, errPanic) || plan != nil {
				t.Fatalf("planned a movement (%v) or crashed: %v", plan != nil, err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("refused for another reason: %v, want %v", err, c.want)
			}
		})
	}
	// Both custody and Bitcoin carry the main once more, among candidates
	// that differ in one term each: the exact main is chosen.
	g := movementReady(t)
	p := g.planner()
	m := g.main
	m.Value--
	p.Bitcoin = unspent{[]bitcoin.PreviousOutput{other(g), m, g.main}}
	plan, err := p.Movement(ctx)
	if err != nil || plan.MovementMain().Outpoint != g.main.Outpoint {
		t.Fatalf("the exact custody main was not chosen among near candidates: %v", err)
	}
}

// TestMovementCoordinatedFee: a coordinated fee is the exact fee when it is
// at most the committed cap, the cap itself included.
func TestMovementCoordinatedFee(t *testing.T) {
	ctx := context.Background()
	g := movementReady(t)
	cap := int64(g.f.commitments[g.source.ID].MaxFee)
	for _, fee := range []int64{1_000, cap} {
		p := g.planner()
		p.Fee, p.MaximumSatPerVByte = fee, 1_000
		plan, err := p.Movement(ctx)
		if err != nil || plan.Snapshot().Fee() != fee {
			t.Fatalf("coordinated fee %d (cap %d) was not planned exactly: %v", fee, cap, err)
		}
	}
}

func stopAt(step string) func(string) error {
	return func(name string) error {
		if name == step {
			return errBoundary
		}
		return nil
	}
}

type failingUnspent struct{}

func (failingUnspent) UnspentByScript(context.Context, bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	return nil, errInjected
}

// TestIncomingSweepSelection: exactly the final pending or escalated records
// of this wallet, wherever the excluded ones sort.
func TestIncomingSweepSelection(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	target := g.targets[0]
	cases := []struct {
		state   IncomingState
		other   bool
		block   uint64
		include bool
	}{
		// In key order: excluded records come first and between the
		// included ones.
		{IncomingSwept, false, 90, false},
		{IncomingPending, false, 90, true},
		{IncomingPending, true, 90, false},
		{IncomingEscalated, false, 90, true},
		{IncomingUnknown, false, 90, false},
		{IncomingPending, false, 99, false},
		{IncomingPending, false, 98, true},
		{IncomingEscalated, true, 90, false},
	}
	var keys [][32]byte
	for i := range cases {
		keys = append(keys, g.addIncoming(target, byte(0x60+i), 40_000+uint64(i), 90))
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
	want := map[[32]byte]bool{}
	for i, c := range cases {
		key := keys[i]
		r := g.f.incoming[key]
		r.State = c.state
		if c.other {
			r.Target = g.targets[1].ID
		}
		g.f.incoming[key] = r
		for j, l := range g.f.incomingLog {
			if l.Outpoint == key {
				g.f.incomingLog[j].Log = Log{Block: c.block, BlockHash: g.f.hash(c.block)}
			}
		}
		if c.include {
			want[key] = true
		}
	}
	plan, err := g.targetPlanner(target, nil).IncomingSweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[[32]byte]bool{}
	for _, in := range plan.IncomingOutputs() {
		got[OutpointKey(in.Outpoint)] = true
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("swept %d records, want exactly the %d final pending or escalated records of this wallet", len(got), len(want))
	}
}

// TestIncomingSweepInputCap: at most MaxSweepInputs-1 incoming outputs, the
// lowest keys, so the main always fits; with a main the plan is still valid.
func TestIncomingSweepInputCap(t *testing.T) {
	ctx := context.Background()
	for _, withMain := range []bool{false, true} {
		g := newGapWorld(t)
		target := g.targets[0]
		var main *bitcoin.PreviousOutput
		if withMain {
			m := g.setTargetMain(target, 50_000)
			main = &m
		}
		g.f.params.MovedFundsSweepTxMaxTotalFee = 50_000
		for i := 0; i < signing.MaxSweepInputs+2; i++ {
			o := bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{byte(i), byte(i >> 8), 0x17}, OutputIndex: 0}
			key := OutpointKey(o)
			g.f.incoming[key] = Incoming{Target: target.ID, Source: g.source.ID, Value: 10_000, State: IncomingPending, SweepFeeCap: 50_000}
			g.f.incomingLog = append(g.f.incomingLog, IncomingLog{Log: Log{Block: 90, BlockHash: g.f.hash(90)}, Outpoint: key, Target: target.ID, Source: g.source.ID, Value: 10_000, TransactionHash: [32]byte(o.TransactionHash)})
		}
		p := g.targetPlanner(target, main)
		p.FeeRate = 1
		plan, err := p.IncomingSweep(ctx)
		if err != nil {
			t.Fatalf("main %v: %v", withMain, err)
		}
		if n := len(plan.IncomingOutputs()); n != signing.MaxSweepInputs-1 {
			t.Fatalf("main %v: %d incoming inputs, want %d", withMain, n, signing.MaxSweepInputs-1)
		}
	}
}

// TestIncomingSweepRefusals: every refusal of IncomingSweep, each alone.
func TestIncomingSweepRefusals(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		withMain bool
		mutate   func(g *gapWorld, p *Planner, target Wallet)
		want     error
	}{
		"zero fee rate, coordinated fee":  {false, func(_ *gapWorld, p *Planner, _ Wallet) { p.FeeRate, p.Fee = 0, 1_000 }, nil},
		"negative fee rate":               {false, func(_ *gapWorld, p *Planner, _ Wallet) { p.FeeRate = -1 }, nil},
		"rate above ceiling, coordinated": {false, func(_ *gapWorld, p *Planner, _ Wallet) { p.FeeRate, p.MaximumSatPerVByte, p.Fee = 30, 20, 1_000 }, nil},
		"target read fails":               {false, func(g *gapWorld, _ *Planner, _ Wallet) { g.fc.failOn("IncomingLogs") }, errInjected},
		"main reserved": {true, func(g *gapWorld, _ *Planner, target Wallet) {
			cu := g.f.custody[target.ID]
			cu.ActiveReservation = [32]byte{4}
			g.f.custody[target.ID] = cu
		}, ErrPlanMismatch},
		"nothing pending": {false, func(g *gapWorld, _ *Planner, _ Wallet) {
			for k, r := range g.f.incoming {
				r.State = IncomingSwept
				g.f.incoming[k] = r
			}
		}, ErrNothingIncoming},
		"custody output read fails": {true, func(g *gapWorld, _ *Planner, _ Wallet) { g.fc.failOn("CustodyOutput") }, errInjected},
		"main not on Bitcoin":       {true, func(_ *gapWorld, p *Planner, _ Wallet) { p.Bitcoin = unspent{} }, redemption.ErrNoCustody},
		"main with a foreign script": {true, func(g *gapWorld, p *Planner, _ Wallet) {
			m := p.Bitcoin.(unspent).outs[0]
			m.PublicKeyScript, _ = bitcoin.PayToTaproot(g.source.Descriptor.OutputKey)
			p.Bitcoin = unspent{[]bitcoin.PreviousOutput{m}}
		}, nil},
		"coordinated fee above the cap": {false, func(g *gapWorld, p *Planner, _ Wallet) { p.Fee = int64(g.f.params.MovedFundsSweepTxMaxTotalFee) + 1 }, nil},
		"stop before the sweep is read": {false, func(_ *gapWorld, p *Planner, _ Wallet) { p.boundary = stopAt("before-movement-plan") }, errBoundary},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGapWorld(t)
			target := g.targets[0]
			var main *bitcoin.PreviousOutput
			if c.withMain {
				m := g.setTargetMain(target, 50_000)
				main = &m
			}
			g.addIncoming(target, 0x55, 99_000, 90)
			p := g.targetPlanner(target, main)
			if _, err := p.IncomingSweep(ctx); err != nil {
				t.Fatal("control: the sweep must plan:", err)
			}
			c.mutate(g, &p, target)
			var plan *signing.Plan
			err := recovered(func() error { var err error; plan, err = p.IncomingSweep(ctx); return err })
			if err == nil || errors.Is(err, errPanic) || plan != nil {
				t.Fatalf("planned a sweep (%v) or crashed: %v", plan != nil, err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("refused for another reason: %v, want %v", err, c.want)
			}
		})
	}
}

// TestIncomingSweepCoordinatedFee: a coordinated fee at most the cap, the
// cap itself included, is the exact fee.
func TestIncomingSweepCoordinatedFee(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	target := g.targets[0]
	g.addIncoming(target, 0x55, 99_000, 90)
	cap := int64(g.f.params.MovedFundsSweepTxMaxTotalFee)
	for _, fee := range []int64{1_000, cap} {
		p := g.targetPlanner(target, nil)
		p.Fee, p.MaximumSatPerVByte = fee, 1_000
		plan, err := p.IncomingSweep(ctx)
		if err != nil || plan.Snapshot().Fee() != fee {
			t.Fatalf("coordinated fee %d (cap %d) was not planned exactly: %v", fee, cap, err)
		}
	}
}
