package signing

import (
	"fmt"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Mutation batch M7-signing-movement: tests for the K-07 movement and
// incoming-sweep planners (pkg/frost/signing/movement.go). Every case goes
// through the public constructors NewMovementPlan and NewIncomingSweepPlan.
// Proposals start from the labelled test fixtures movementFixture and
// incomingFixture (movement_test.go): fixed test keys and synthetic
// outpoints, not chain data.

// buildPlan calls a planner and turns a panic into a value, so a malformed
// proposal that would crash the host fails at a named assertion. The
// malformed shapes used here (no transaction, no inputs, a nil input or an
// input without an outpoint) cannot occur in production today: proposals come
// only from the node's own planner (movement.Planner.Movement and
// Planner.IncomingSweep), which builds the transaction itself. These cases
// guard the exported constructors' contract, not a live attack path.
func buildPlan(build func() (*Plan, error)) (plan *Plan, err error, recovered any) {
	defer func() { recovered = recover() }()
	plan, err = build()
	return plan, err, nil
}

// requireRefused asserts the planner refuses the proposal with an error,
// without a plan and without panicking.
func requireRefused(t *testing.T, what string, build func() (*Plan, error)) {
	t.Helper()
	plan, err, recovered := buildPlan(build)
	if recovered != nil {
		t.Fatalf("%s: planner panicked instead of refusing: %v", what, recovered)
	}
	if err == nil || plan != nil {
		t.Fatalf("%s: proposal was not refused (err=%v, plan returned=%v)", what, err, plan != nil)
	}
}

// requireAccepted asserts the planner accepts the proposal.
func requireAccepted(t *testing.T, what string, build func() (*Plan, error)) *Plan {
	t.Helper()
	plan, err, recovered := buildPlan(build)
	if recovered != nil {
		t.Fatalf("%s: planner panicked on a valid proposal: %v", what, recovered)
	}
	if err != nil || plan == nil {
		t.Fatalf("%s: valid proposal was refused: %v", what, err)
	}
	return plan
}

// setMovementFee rebuilds the fixture's outputs as the equal split of the
// main less fee, so only the fee changes.
func setMovementFee(p *MovementProposal, fee int64) {
	for i, v := range MovementSplit(p.Main.Value-fee, len(p.Targets)) {
		p.Transaction.Outputs[i].Value = v
	}
}

// setIncomingFee sets the single output to the sum of the inputs less fee.
func setIncomingFee(p *IncomingSweepProposal, fee int64) {
	var total int64
	if p.Main != nil {
		total += p.Main.Value
	}
	for _, in := range p.Incoming {
		total += in.Value
	}
	p.Transaction.Outputs[0].Value = total - fee
}

// foreignScript is a P2TR script of a key that is not the fixture wallet's.
func foreignScript(t *testing.T) bitcoin.Script {
	t.Helper()
	script, err := bitcoin.PayToTaproot(movementTargets(t, 1)[0].OutputKey)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

// Invariant: a movement's and an incoming sweep's fee rate never exceeds the
// committed sat/vB ceiling, compared exactly (fee <= ceiling * vsize) with no
// rounding either way. Checked on both sides of the boundary for both
// planners: a fee of exactly ceiling*vsize is accepted, one satoshi more and
// a whole vbyte-step more are refused. Refs M7 rows at movement.go:92.
func TestMovementFeeRateBoundaryIsExact(t *testing.T) {
	const ceiling = 10
	t.Run("movement", func(t *testing.T) {
		base := movementFixture(t)
		vsize := requireAccepted(t, "baseline", func() (*Plan, error) { return NewMovementPlan(base) }).Snapshot().VirtualSize()
		for _, c := range []struct {
			fee    int64
			accept bool
		}{
			{ceiling*vsize - 1, true},
			{ceiling * vsize, true},
			{ceiling*vsize + 1, false},
			{(ceiling + 1) * vsize, false},
		} {
			p := movementFixture(t)
			p.MaximumFee, p.MaximumSatPerVByte = p.Main.Value, ceiling
			setMovementFee(&p, c.fee)
			what := fmt.Sprintf("fee %d at vsize %d and ceiling %d sat/vB", c.fee, vsize, ceiling)
			if c.accept {
				requireAccepted(t, what, func() (*Plan, error) { return NewMovementPlan(p) })
			} else {
				requireRefused(t, what, func() (*Plan, error) { return NewMovementPlan(p) })
			}
		}
	})
	t.Run("incoming sweep", func(t *testing.T) {
		base := incomingFixture(t, true)
		vsize := requireAccepted(t, "baseline", func() (*Plan, error) { return NewIncomingSweepPlan(base) }).Snapshot().VirtualSize()
		for _, c := range []struct {
			fee    int64
			accept bool
		}{
			{ceiling*vsize - 1, true},
			{ceiling * vsize, true},
			{ceiling*vsize + 1, false},
			{(ceiling + 1) * vsize, false},
		} {
			p := incomingFixture(t, true)
			p.MaximumFee, p.MaximumSatPerVByte = 100_000, ceiling
			setIncomingFee(&p, c.fee)
			what := fmt.Sprintf("fee %d at vsize %d and ceiling %d sat/vB", c.fee, vsize, ceiling)
			if c.accept {
				requireAccepted(t, what, func() (*Plan, error) { return NewIncomingSweepPlan(p) })
			} else {
				requireRefused(t, what, func() (*Plan, error) { return NewIncomingSweepPlan(p) })
			}
		}
	})
}

// Invariant: the absolute fee cap is inclusive. A fee equal to the committed
// maximum is accepted and a fee one satoshi above the cap is refused, for
// both planners. Refs M7 rows at movement.go:129 and :176.
func TestMovementFeeCapIsInclusive(t *testing.T) {
	t.Run("movement", func(t *testing.T) {
		p := movementFixture(t) // fixture fee: 3,000 sat
		p.MaximumFee = 3_000
		requireAccepted(t, "fee equal to the cap", func() (*Plan, error) { return NewMovementPlan(p) })
		p.MaximumFee = 2_999
		requireRefused(t, "fee one above the cap", func() (*Plan, error) { return NewMovementPlan(p) })
	})
	t.Run("incoming sweep", func(t *testing.T) {
		p := incomingFixture(t, true) // fixture fee: 500 sat
		p.MaximumFee = 500
		requireAccepted(t, "fee equal to the cap", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
		p.MaximumFee = 499
		requireRefused(t, "fee one above the cap", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
}

// Invariant: a planner accepts the committed caps only inside (0, MaxSatoshi]
// and refuses a proposal without a valid wallet descriptor, a Bitcoin genesis
// or a transaction, each on its own with every other term valid. The upper
// bounds are inclusive. The MaxSatoshi bound comes from the code's own int64
// guard (as in NewPlan), not from KC-SF-V3, which states fee caps but no
// numeric ceiling. Shared by both planners. Refs M7 rows at
// movement.go:98 and :140.
func TestMovementProposalTermsEachRefusedAlone(t *testing.T) {
	type terms struct {
		scheme    *uint8
		genesis   *[32]byte
		fee, rate *int64
		clearTx   func()
	}
	cases := []struct {
		name   string
		mutate func(terms)
		accept bool
	}{
		{"invalid wallet descriptor", func(x terms) { *x.scheme = 3 }, false},
		{"zero bitcoin genesis", func(x terms) { *x.genesis = [32]byte{} }, false},
		{"no transaction", func(x terms) { x.clearTx() }, false},
		{"zero fee cap", func(x terms) { *x.fee = 0 }, false},
		{"negative fee cap", func(x terms) { *x.fee = -1 }, false},
		{"fee cap above the supply", func(x terms) { *x.fee = bitcoin.MaxSatoshi + 1 }, false},
		{"fee cap equal to the supply", func(x terms) { *x.fee = bitcoin.MaxSatoshi }, true},
		{"zero rate cap", func(x terms) { *x.rate = 0 }, false},
		{"negative rate cap", func(x terms) { *x.rate = -1 }, false},
		{"rate cap above the supply", func(x terms) { *x.rate = bitcoin.MaxSatoshi + 1 }, false},
		{"rate cap equal to the supply", func(x terms) { *x.rate = bitcoin.MaxSatoshi }, true},
	}
	for _, c := range cases {
		t.Run("movement/"+c.name, func(t *testing.T) {
			p := movementFixture(t)
			c.mutate(terms{scheme: &p.Wallet.Scheme, genesis: &p.BitcoinGenesis, fee: &p.MaximumFee, rate: &p.MaximumSatPerVByte, clearTx: func() { p.Transaction = nil }})
			build := func() (*Plan, error) { return NewMovementPlan(p) }
			if c.accept {
				requireAccepted(t, c.name, build)
			} else {
				requireRefused(t, c.name, build)
			}
		})
		t.Run("incoming sweep/"+c.name, func(t *testing.T) {
			p := incomingFixture(t, true)
			c.mutate(terms{scheme: &p.Wallet.Scheme, genesis: &p.BitcoinGenesis, fee: &p.MaximumFee, rate: &p.MaximumSatPerVByte, clearTx: func() { p.Transaction = nil }})
			build := func() (*Plan, error) { return NewIncomingSweepPlan(p) }
			if c.accept {
				requireAccepted(t, c.name, build)
			} else {
				requireRefused(t, c.name, build)
			}
		})
	}
}

// Invariant: a movement spends exactly one input, the committed main, and the
// main must be the wallet's own output. A missing, nil or outpoint-less input
// is refused without a panic, and so is a main whose script is not P2TR(Q).
// Refs M7 rows at movement.go:101 and :106.
func TestMovementSpendsOnlyTheCommittedMain(t *testing.T) {
	for name, mutate := range map[string]func(*MovementProposal){
		"no inputs": func(p *MovementProposal) { p.Transaction.Inputs = nil },
		"nil input": func(p *MovementProposal) { p.Transaction.Inputs[0] = nil },
		"input without outpoint": func(p *MovementProposal) {
			p.Transaction.Inputs[0].Outpoint = nil
		},
		"second input": func(p *MovementProposal) {
			o := bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{77}}
			p.Transaction.Inputs = append(p.Transaction.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
		},
		"main pays another key": func(p *MovementProposal) { p.Main.PublicKeyScript = foreignScript(t) },
	} {
		t.Run(name, func(t *testing.T) {
			p := movementFixture(t)
			mutate(&p)
			requireRefused(t, name, func() (*Plan, error) { return NewMovementPlan(p) })
		})
	}
}

// Invariant: every movement output is a committed target with a non-zero
// label and a non-zero descriptor hash; either one zero alone refuses the
// movement even when the output script matches. Refs M7 rows at
// movement.go:118.
func TestMovementTargetIdentityEachRequired(t *testing.T) {
	for name, mutate := range map[string]func(*MovementTarget){
		"zero label":           func(m *MovementTarget) { m.Label = [20]byte{} },
		"zero descriptor hash": func(m *MovementTarget) { m.DescriptorHash = [32]byte{} },
	} {
		for i := 0; i < 3; i++ {
			t.Run(fmt.Sprintf("%s/target %d", name, i), func(t *testing.T) {
				p := movementFixture(t)
				mutate(&p.Targets[i])
				requireRefused(t, name, func() (*Plan, error) { return NewMovementPlan(p) })
			})
		}
	}
}

// Invariant: the plan reports the C-06 period it was built for, and a plan
// that is not a movement reports none. Refs M7 row at movement.go:269.
func TestMovementPeriodReported(t *testing.T) {
	p := movementFixture(t)
	p.Period = 7
	plan := requireAccepted(t, "movement in period 7", func() (*Plan, error) { return NewMovementPlan(p) })
	if got := plan.MovementPeriod(); got != 7 {
		t.Fatalf("movement plan reports period %d, want 7", got)
	}
	payment, _, _ := planFixture(t)
	other := requireAccepted(t, "redemption plan", func() (*Plan, error) { return NewPlan(payment) })
	if got := other.MovementPeriod(); got != 0 {
		t.Fatalf("non-movement plan reports period %d, want 0", got)
	}
}

// incomingWithCount is incomingFixture with the main and n-1 incoming
// outputs, all paying the wallet: n inputs in total.
func incomingWithCount(t *testing.T, n int) IncomingSweepProposal {
	t.Helper()
	p := incomingFixture(t, true)
	script := p.Main.PublicKeyScript
	p.Incoming = nil
	main := p.Main.Outpoint
	p.Transaction.Inputs = []*bitcoin.TransactionInput{{Outpoint: &main, Sequence: 0xffffffff}}
	for i := 1; i < n; i++ {
		in := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xa0, byte(i >> 8), byte(i)}}, Value: 10_000, PublicKeyScript: script}
		p.Incoming = append(p.Incoming, in)
		o := in.Outpoint
		p.Transaction.Inputs = append(p.Transaction.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
	}
	p.MaximumFee, p.MaximumSatPerVByte = 100_000, 50
	setIncomingFee(&p, 20_000)
	return p
}

// Invariant: an incoming sweep spends at most MaxSweepInputs inputs (main
// included), and its transaction spends exactly the planned inputs. The cap
// is inclusive. Refs M7 rows at movement.go:155 and :156.
func TestIncomingSweepInputCount(t *testing.T) {
	t.Run("exactly the cap", func(t *testing.T) {
		p := incomingWithCount(t, MaxSweepInputs)
		requireAccepted(t, "MaxSweepInputs inputs", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
	t.Run("one above the cap", func(t *testing.T) {
		p := incomingWithCount(t, MaxSweepInputs+1)
		requireRefused(t, "MaxSweepInputs+1 inputs", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
	t.Run("transaction spends an extra input", func(t *testing.T) {
		p := incomingFixture(t, true)
		o := bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{78}}
		p.Transaction.Inputs = append(p.Transaction.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
		requireRefused(t, "unplanned extra input", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
	t.Run("transaction omits a planned input", func(t *testing.T) {
		p := incomingFixture(t, true)
		p.Transaction.Inputs = p.Transaction.Inputs[:len(p.Transaction.Inputs)-1]
		requireRefused(t, "missing planned input", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
}

// Invariant: an incoming sweep moves received outputs. Its own main alone is
// not an incoming sweep, and every transaction input must be a planned
// outpoint: nil inputs and inputs without an outpoint are refused without a
// panic, at every input index. Refs M7 rows at movement.go:140 and :159.
func TestIncomingSweepInputsArePlanned(t *testing.T) {
	t.Run("main without incoming", func(t *testing.T) {
		p := incomingFixture(t, true)
		p.Incoming = nil
		p.Transaction.Inputs = p.Transaction.Inputs[:1]
		setIncomingFee(&p, 500)
		requireRefused(t, "main-only sweep", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("nil input %d", i), func(t *testing.T) {
			p := incomingFixture(t, true)
			p.Transaction.Inputs[i] = nil
			requireRefused(t, "nil input", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
		})
		t.Run(fmt.Sprintf("input %d without outpoint", i), func(t *testing.T) {
			p := incomingFixture(t, true)
			p.Transaction.Inputs[i].Outpoint = nil
			requireRefused(t, "input without outpoint", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
		})
	}
	t.Run("incoming repeated in the transaction", func(t *testing.T) {
		p := incomingFixture(t, true)
		p.Incoming[1] = p.Incoming[0]
		o := p.Incoming[0].Outpoint
		p.Transaction.Inputs[2].Outpoint = &o
		requireRefused(t, "duplicate incoming", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
	t.Run("incoming equal to the main", func(t *testing.T) {
		p := incomingFixture(t, true)
		p.Incoming[0] = *p.Main
		o := p.Main.Outpoint
		p.Transaction.Inputs[1].Outpoint = &o
		requireRefused(t, "incoming repeats the main", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
}

// Invariant: an incoming sweep spends only outputs of its own wallet and is
// immediately final. Refs M7 rows at movement.go:165 and :169.
func TestIncomingSweepSnapshotRefusals(t *testing.T) {
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("input %d pays another key", i), func(t *testing.T) {
			p := incomingFixture(t, true)
			if i == 0 {
				p.Main.PublicKeyScript = foreignScript(t)
			} else {
				p.Incoming[i-1].PublicKeyScript = foreignScript(t)
			}
			requireRefused(t, "foreign prevout", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
		})
	}
	t.Run("absolute locktime in the future", func(t *testing.T) {
		p := incomingFixture(t, true)
		p.Transaction.Locktime = 1 << 30
		p.Transaction.Inputs[0].Sequence = 0xfffffffe
		requireRefused(t, "non-final sweep", func() (*Plan, error) { return NewIncomingSweepPlan(p) })
	})
}
