package movement

// Mutation batch M6-movement: the movement authority (authority.go).
// Invariants (KC-SF-V3 K-07; V-08 P-K07-COMMITTED-TARGET-ONLY,
// P-K07-INCOMING-ONCE; the Authority doc comments):
//   - a movement is authorized only when every term of the plan equals the
//     recorded commitment: the main (its outpoint, the custody main, no
//     reservation, its value), the target count, and each target's label,
//     descriptor hash and key. Each term differing alone refuses;
//   - an incoming sweep is authorized only for a target in an allowed state
//     (a quarantined one under a cancel directive), for incoming records of
//     this wallet that are pending or escalated, final, of the planned value,
//     with the unreserved custody main exactly as planned; the receipt anchors
//     the latest record.
// Unit level on the real Authority over the fixture chain.

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// otherMainPlan builds a movement plan of the same commitment that spends a
// different main of the same value. The world's records are restored after.
func otherMainPlan(t *testing.T, g *gapWorld) *signing.Plan {
	t.Helper()
	ctx := context.Background()
	other := g.main
	other.Outpoint = bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x78}, OutputIndex: 1}
	key, otherKey := OutpointKey(g.main.Outpoint), OutpointKey(other.Outpoint)
	custody, commitment := g.f.custody[g.source.ID], g.f.commitments[g.source.ID]
	moved := custody
	moved.MainOutpoint = otherKey
	g.f.custody[g.source.ID] = moved
	c := commitment
	c.MainOutpoint = otherKey
	g.f.commitments[g.source.ID] = c
	g.f.outputs[otherKey] = g.f.outputs[key]
	p := g.planner()
	p.Bitcoin = unspent{[]bitcoin.PreviousOutput{other}}
	plan, err := p.Movement(ctx)
	if err != nil {
		t.Fatal("fixture plan of another main:", err)
	}
	g.f.custody[g.source.ID], g.f.commitments[g.source.ID] = custody, commitment
	return plan
}

// planFrom builds a movement plan while change is applied, then undoes it:
// a stale plan (or one from a buggy host) checked against a consistent chain.
// mainDelta adjusts the value of the main the fixture Bitcoin view offers.
func planFrom(t *testing.T, g *gapWorld, change func() (undo func()), mainDelta int64) *signing.Plan {
	t.Helper()
	undo := change()
	p := g.planner()
	m := g.main
	m.Value += mainDelta
	p.Bitcoin = unspent{[]bitcoin.PreviousOutput{m}}
	plan, err := p.Movement(context.Background())
	undo()
	if err != nil {
		t.Fatal("fixture stale plan:", err)
	}
	return plan
}

// TestMovementAuthorityRefusesEachTermAlone: each term of the plan or the
// records that differs from the commitment alone refuses the movement.
func TestMovementAuthorityRefusesEachTermAlone(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]func(t *testing.T, g *gapWorld, plan *signing.Plan) *signing.Plan{
		"plan spends another main": func(t *testing.T, g *gapWorld, _ *signing.Plan) *signing.Plan {
			return otherMainPlan(t, g)
		},
		"custody main moved, same value": func(_ *testing.T, g *gapWorld, plan *signing.Plan) *signing.Plan {
			key := OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x79}})
			cu := g.f.custody[g.source.ID]
			cu.MainOutpoint = key
			g.f.custody[g.source.ID] = cu
			g.f.outputs[key] = redemption.CustodyOutput{WalletID: g.source.ID, Value: uint64(g.main.Value)}
			return plan
		},
		"main reserved": func(_ *testing.T, g *gapWorld, plan *signing.Plan) *signing.Plan {
			cu := g.f.custody[g.source.ID]
			cu.ActiveReservation = [32]byte{9}
			g.f.custody[g.source.ID] = cu
			return plan
		},
		// The cases below change no recorded data: each builds a stale plan
		// while the chain briefly differed (planFrom), then authorizes it
		// against the restored, consistent chain.
		"plan main value differs": func(t *testing.T, g *gapWorld, _ *signing.Plan) *signing.Plan {
			return planFrom(t, g, func() func() {
				key := OutpointKey(g.main.Outpoint)
				out := g.f.outputs[key]
				lower := out
				lower.Value--
				g.f.outputs[key] = lower
				return func() { g.f.outputs[key] = out }
			}, -1)
		},
		"plan names one target fewer": func(t *testing.T, g *gapWorld, _ *signing.Plan) *signing.Plan {
			return planFrom(t, g, func() func() {
				c := g.f.commitments[g.source.ID]
				fewer := c
				fewer.Targets = c.Targets[:len(c.Targets)-1]
				fewer.DescriptorHashes = c.DescriptorHashes[:len(c.DescriptorHashes)-1]
				g.f.commitments[g.source.ID] = fewer
				return func() { g.f.commitments[g.source.ID] = c }
			}, 0)
		},
		"plan target label differs": func(t *testing.T, g *gapWorld, _ *signing.Plan) *signing.Plan {
			return planFrom(t, g, func() func() {
				c := g.f.commitments[g.source.ID]
				alias := [20]byte{0xa1, 0x1a}
				g.f.wallets[alias] = g.f.wallets[c.Targets[1]]
				m := c
				m.Targets = append([][20]byte(nil), c.Targets...)
				m.Targets[1] = alias
				g.f.commitments[g.source.ID] = m
				return func() { g.f.commitments[g.source.ID] = c; delete(g.f.wallets, alias) }
			}, 0)
		},
		"plan target descriptor hash differs": func(t *testing.T, g *gapWorld, _ *signing.Plan) *signing.Plan {
			return planFrom(t, g, func() func() {
				c := g.f.commitments[g.source.ID]
				l := c.Targets[1]
				w := g.f.wallets[l]
				m := c
				m.DescriptorHashes = append([][32]byte(nil), c.DescriptorHashes...)
				m.DescriptorHashes[1] = [32]byte{0xd1}
				g.f.commitments[g.source.ID] = m
				other := w
				other.DescriptorHash = [32]byte{0xd1}
				g.f.wallets[l] = other
				return func() { g.f.commitments[g.source.ID] = c; g.f.wallets[l] = w }
			}, 0)
		},
		"plan target key differs": func(t *testing.T, g *gapWorld, _ *signing.Plan) *signing.Plan {
			return planFrom(t, g, func() func() {
				l := g.f.commitments[g.source.ID].Targets[1]
				w := g.f.wallets[l]
				other := w
				other.Descriptor.OutputKey = g.targets[3].Descriptor.OutputKey
				if other.Descriptor.OutputKey == w.Descriptor.OutputKey {
					other.Descriptor.OutputKey = g.targets[2].Descriptor.OutputKey
				}
				g.f.wallets[l] = other
				return func() { g.f.wallets[l] = w }
			}, 0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGapWorld(t)
			plan := g.committedPlan(t)
			a := &Authority{View: g.view}
			if _, err := a.AuthorizeSweep(ctx, plan); err != nil {
				t.Fatal("control: the committed movement must be authorized:", err)
			}
			plan = c(t, g, plan)
			err := recovered(func() error { _, err := a.AuthorizeSweep(ctx, plan); return err })
			if !errors.Is(err, ErrPlanMismatch) {
				t.Fatalf("authorized a movement whose %s: %v", name, err)
			}
		})
	}
}

// fixtureDirectives is a fixture directive source with one answer.
type fixtureDirectives struct {
	d   redemption.Directive
	ok  bool
	err error
}

func (f fixtureDirectives) Directive(context.Context, [32]byte) (redemption.Directive, bool, error) {
	return f.d, f.ok, f.err
}

// TestIncomingAuthorityTargetStateAndContainment: the allowed target states
// sweep; every other state refuses; a quarantined target sweeps only under a
// readable cancel directive.
func TestIncomingAuthorityTargetStateAndContainment(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	plan, target, _ := g.incomingPlan(t, false)
	a := &Authority{View: g.view}
	set := func(state State, quarantined bool) {
		w := g.f.wallets[Label(target.ID)]
		w.State, w.Quarantined = state, quarantined
		g.f.wallets[Label(target.ID)] = w
	}
	for state := StateUnknown; state <= StateTerminated; state++ {
		set(state, false)
		_, err := a.AuthorizeSweep(ctx, plan)
		switch state {
		case StateReadyUnfunded, StateLive, StateMovingFunds, StateClosing, StateTerminated:
			if err != nil {
				t.Errorf("target state %d must sweep its incoming: %v", state, err)
			}
		default:
			if !errors.Is(err, ErrWalletState) {
				t.Errorf("target state %d swept its incoming: %v", state, err)
			}
		}
	}
	cancel := redemption.Directive{Action: redemption.DirectiveCancel}
	for name, c := range map[string]struct {
		dirs redemption.Directives
		want bool
	}{
		"no directive source":   {nil, false},
		"directive read fails":  {fixtureDirectives{cancel, true, errInjected}, false},
		"no directive":          {fixtureDirectives{cancel, false, nil}, false},
		"rebroadcast directive": {fixtureDirectives{redemption.Directive{Action: redemption.DirectiveRebroadcast}, true, nil}, false},
		"cancel directive":      {fixtureDirectives{cancel, true, nil}, true},
	} {
		set(StateLive, true)
		_, err := (&Authority{View: g.view, Directives: c.dirs}).AuthorizeSweep(ctx, plan)
		if c.want && err != nil {
			t.Errorf("%s: quarantined target refused: %v", name, err)
		}
		if !c.want && !errors.Is(err, redemption.ErrContained) {
			t.Errorf("%s: quarantined target swept without a cancel directive: %v", name, err)
		}
	}
}

// TestIncomingAuthorityRecordRules: each incoming record fault refuses;
// escalated records sweep; a failed read refuses; a stale plan whose input
// value differs from the record refuses.
func TestIncomingAuthorityRecordRules(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		mutate func(g *gapWorld, key [32]byte, target Wallet)
		want   error
	}{
		"escalated": {func(g *gapWorld, key [32]byte, _ Wallet) {
			r := g.f.incoming[key]
			r.State = IncomingEscalated
			g.f.incoming[key] = r
		}, nil},
		"record of another": {func(g *gapWorld, key [32]byte, _ Wallet) {
			r := g.f.incoming[key]
			r.Target = g.targets[1].ID
			g.f.incoming[key] = r
		}, ErrPlanMismatch},
		"record unknown": {func(g *gapWorld, key [32]byte, _ Wallet) { g.f.incoming[key] = Incoming{} }, ErrPlanMismatch},
		"swept": {func(g *gapWorld, key [32]byte, _ Wallet) {
			r := g.f.incoming[key]
			r.State = IncomingSwept
			g.f.incoming[key] = r
		}, ErrSettled},
		"not final": {func(g *gapWorld, _ [32]byte, _ Wallet) { g.f.head = 91 }, ErrNotFinal},
		"incoming read fails": {func(g *gapWorld, _ [32]byte, _ Wallet) {
			g.fc.failOn("IncomingLogs")
		}, errInjected},
		"main reserved": {func(g *gapWorld, _ [32]byte, target Wallet) {
			cu := g.f.custody[target.ID]
			cu.ActiveReservation = [32]byte{3}
			g.f.custody[target.ID] = cu
		}, ErrPlanMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGapWorld(t)
			plan, target, key := g.incomingPlan(t, false)
			c.mutate(g, key, target)
			_, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan)
			if c.want == nil && err != nil {
				t.Fatalf("%s: refused: %v", name, err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("%s: authorized or refused for another reason: %v, want %v", name, err, c.want)
			}
		})
	}
	// A stale plan of another value: planned while the record briefly read
	// one sat more, authorized against the restored record.
	g := newGapWorld(t)
	target := g.targets[0]
	key := g.addIncoming(target, 0x55, 99_000, 90)
	r := g.f.incoming[key]
	more := r
	more.Value++
	g.f.incoming[key] = more
	plan, err := g.targetPlanner(target, nil).IncomingSweep(ctx)
	g.f.incoming[key] = r
	if err != nil {
		t.Fatal("fixture stale plan:", err)
	}
	if _, err = (&Authority{View: g.view}).AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
		t.Fatalf("a plan of another value than its record was authorized: %v", err)
	}
}

// TestIncomingAuthorityMainRule: the plan's main must be the target's
// custody main exactly: absent when there is none, that outpoint when there
// is one.
func TestIncomingAuthorityMainRule(t *testing.T) {
	ctx := context.Background()
	t.Run("with main", func(t *testing.T) {
		g := newGapWorld(t)
		plan, target, _ := g.incomingPlan(t, true)
		a := &Authority{View: g.view}
		if _, err := a.AuthorizeSweep(ctx, plan); err != nil {
			t.Fatal("an incoming sweep with the custody main was refused:", err)
		}
		cu := g.f.custody[target.ID]
		moved := cu
		moved.MainOutpoint = OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x4e}})
		g.f.custody[target.ID] = moved
		if _, err := a.AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
			t.Fatalf("an incoming sweep of a main that is no longer the custody main was authorized: %v", err)
		}
		moved.MainOutpoint = [32]byte{}
		g.f.custody[target.ID] = moved
		if _, err := a.AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
			t.Fatalf("an incoming sweep with a main was authorized for a target without one: %v", err)
		}
	})
	t.Run("without main", func(t *testing.T) {
		g := newGapWorld(t)
		plan, target, _ := g.incomingPlan(t, false)
		g.setTargetMain(target, 50_000)
		if _, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
			t.Fatalf("an incoming sweep that leaves out the custody main was authorized: %v", err)
		}
	})
}

// TestIncomingAuthorityAnchorsTheLatestRecord: the receipt anchors the
// latest incoming record (its block and canonical hash), whatever the log
// order, so a reorg of any record is seen at the next recheck.
func TestIncomingAuthorityAnchorsTheLatestRecord(t *testing.T) {
	ctx := context.Background()
	for _, blocks := range [][2]uint64{{95, 90}, {90, 95}} {
		g := newGapWorld(t)
		target := g.targets[0]
		g.addIncoming(target, 0x41, 60_000, blocks[0])
		g.addIncoming(target, 0x42, 70_000, blocks[1])
		plan, err := g.targetPlanner(target, nil).IncomingSweep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan)
		if err != nil {
			t.Fatal(err)
		}
		if want := (signing.ReservationReceipt{Block: 95, Hash: g.f.hash(95)}); receipt != want {
			t.Fatalf("logs at blocks %v: receipt %+v, want the latest record %+v", blocks, receipt, want)
		}
	}
}

// TestAuthorityRefusesOtherPurposes: only movement and incoming sweep plans.
func TestAuthorityRefusesOtherPurposes(t *testing.T) {
	g := newGapWorld(t)
	script, _ := bitcoin.PayToTaproot(g.source.Descriptor.OutputKey)
	recipient := append(bitcoin.Script{0x00, 0x14}, make([]byte, 20)...)
	o := g.main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &o, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100_000, PublicKeyScript: recipient}, {Value: g.main.Value - 101_000, PublicKeyScript: script}}}
	plan, err := signing.NewPlan(signing.Proposal{Wallet: g.source.Descriptor, BitcoinGenesis: g.planner().Genesis, Purpose: signing.Redemption, Generation: 1, StateVersion: 1, Requests: []signing.Request{{ID: [32]byte{1}, Value: 100_000, Script: recipient}}, ConflictInput: o, Fees: signing.FeePolicy{Minimum: 1, Maximum: 10_000, MaximumSatPerVByte: 100}, Transaction: tx, Prevouts: []bitcoin.PreviousOutput{g.main}})
	if err != nil {
		t.Fatal("fixture payment plan:", err)
	}
	if _, err = (&Authority{View: g.view}).AuthorizeSweep(context.Background(), plan); err == nil {
		t.Fatalf("a %s plan was authorized as a movement", plan.Purpose())
	}
}
