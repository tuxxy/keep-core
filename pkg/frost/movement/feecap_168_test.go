package movement

// Acceptance tests for tuxxy/keep-core#168 (raised from REVIEW-TEST-M7, note 5).
// Refs #168
//
// Invariant: a governance fee cap is any uint64 above zero, and the FROST
// planner plans a movement and an incoming sweep under every such cap, with
// the plan's fee bound equal to min(cap, bitcoin.MaxSatoshi) and the fee
// within it. The bound is never negative, and it equals the cap when the cap
// is at most MaxSatoshi (behaviour unchanged). The Authority, which compares
// the uint64 fee against the raw cap, authorizes that plan.
//
// Oracle, stock behaviour:
//   - tbtc-v2 requires only movingFundsTxMaxTotalFee > 0 and
//     movedFundsSweepTxMaxTotalFee > 0, both uint64 (BridgeState.sol);
//   - stock keep-core compares the fee against the uint64 cap directly
//     (pkg/tbtcpg/fee.go applyWalletTxFeeFloor, moved_funds_sweep.go), so a
//     cap such as type(uint64).max ("no cap") still moves funds.
//
// The MaxSatoshi ceiling is the signing planners' own guard
// (pkg/frost/signing/movement.go; pinned by the frozen M7 tests), not a
// KC-SF-V3 rule. Per the issue, the clamp belongs in pkg/frost/movement and the
// signing bound stays.
//
// Fixtures: the in-memory chain fake (movement_test.go) wrapped by gapWorld
// (gaps_fixture_test.go), with fixed test keys and synthetic outpoints. The
// fixture chain records a commitment's MaxFee from movingFundsTxMaxTotalFee at
// submission, and addIncoming records an output's SweepFeeCap from
// movedFundsSweepTxMaxTotalFee. The Planner, Commitments and Authority are the
// real code.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// supply168 is bitcoin.MaxSatoshi as a cap value.
const supply168 = uint64(bitcoin.MaxSatoshi)

// capsAboveSupply168 are governance caps above MaxSatoshi, on both sides of
// MaxInt64, up to type(uint64).max.
var capsAboveSupply168 = []uint64{
	supply168 + 1,
	2 * supply168,
	math.MaxInt64,
	math.MaxInt64 + 1,
	1<<63 + 5_000,
	math.MaxUint64 - 1,
	math.MaxUint64,
}

// capsAboveMaxInt64168 are the caps whose int64 conversion wraps negative.
var capsAboveMaxInt64168 = []uint64{math.MaxInt64 + 1, 1<<63 + 5_000, math.MaxUint64 - 1, math.MaxUint64}

// capsWithinSupply168 are caps at or below MaxSatoshi, the boundary included;
// 1 and 300 sit below the FeeRate fee, so they clamp the fee.
var capsWithinSupply168 = []uint64{1, 300, 5_000, 1_000_000, supply168 - 1, supply168}

// bound168 is the fee bound a plan must carry for cap: min(cap, MaxSatoshi).
func bound168(cap uint64) int64 {
	if cap > supply168 {
		return bitcoin.MaxSatoshi
	}
	return int64(cap)
}

func min168(a int64, cap uint64) int64 {
	if b := bound168(cap); b < a {
		return b
	}
	return a
}

// commitUnderCap168 is a gapWorld whose source commitment was recorded while
// movingFundsTxMaxTotalFee was cap, finalized. It plans nothing, so a refused
// plan fails the caller's own assertion, not this fixture.
func commitUnderCap168(t *testing.T, cap uint64) *gapWorld {
	t.Helper()
	g := newGapWorld(t)
	g.f.params.MovingFundsTxMaxTotalFee = cap
	targets, err := g.commitments().Commit(context.Background())
	if err != nil {
		t.Fatal("fixture commitment:", err)
	}
	c := g.f.commitments[g.source.ID]
	if c.MaxFee != cap {
		t.Fatalf("fixture: commitment recorded cap %d, want %d", c.MaxFee, cap)
	}
	c.DescriptorHashes = nil
	for _, label := range targets {
		c.DescriptorHashes = append(c.DescriptorHashes, g.f.wallets[label].DescriptorHash)
	}
	g.f.commitments[g.source.ID] = c
	g.f.head += 2
	return g
}

// movementRateFee168 is the FeeRate fee of the fixture movement, read from a
// plan under a normal cap that does not clamp it.
func movementRateFee168(t *testing.T) int64 {
	t.Helper()
	g := commitUnderCap168(t, 5_000)
	plan, err := g.planner().Movement(context.Background())
	if err != nil {
		t.Fatal("fixture reference movement:", err)
	}
	return plan.Snapshot().Fee()
}

// sweepUnderCaps168 is a gapWorld with target's sweep inputs recorded: one
// pending incoming output per record cap (live movedFundsSweepTxMaxTotalFee
// at the time of each record), then the live cap set to live. With a target
// main when withMain.
func sweepUnderCaps168(t *testing.T, withMain bool, live uint64, records ...uint64) (*gapWorld, Planner) {
	t.Helper()
	g := newGapWorld(t)
	target := g.targets[0]
	var main *bitcoin.PreviousOutput
	if withMain {
		m := g.setTargetMain(target, 50_000)
		main = &m
	}
	for i, rc := range records {
		g.f.params.MovedFundsSweepTxMaxTotalFee = rc
		key := g.addIncoming(target, 0x55+byte(i), 99_000, 90)
		if g.f.incoming[key].SweepFeeCap != rc {
			t.Fatalf("fixture: record cap %d, want %d", g.f.incoming[key].SweepFeeCap, rc)
		}
	}
	g.f.params.MovedFundsSweepTxMaxTotalFee = live
	return g, g.targetPlanner(target, main)
}

// sweepRateFee168 is the FeeRate fee of the fixture sweep of n records.
func sweepRateFee168(t *testing.T, withMain bool, n int) int64 {
	t.Helper()
	caps := make([]uint64, n)
	for i := range caps {
		caps[i] = 50_000
	}
	_, p := sweepUnderCaps168(t, withMain, 50_000, caps...)
	plan, err := p.IncomingSweep(context.Background())
	if err != nil {
		t.Fatal("fixture reference sweep:", err)
	}
	return plan.Snapshot().Fee()
}

// TestMovementFeeCapAboveSupplyStillPlans: with movingFundsTxMaxTotalFee above
// MaxSatoshi, up to type(uint64).max, the planner still builds the movement;
// its fee bound is min(cap, MaxSatoshi) and its fee the FeeRate fee, or the
// coordinated fee exactly. Refs #168.
func TestMovementFeeCapAboveSupplyStillPlans(t *testing.T) {
	t.Parallel()
	rate := movementRateFee168(t)
	for _, cap := range capsAboveSupply168 {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			g := commitUnderCap168(t, cap)
			plan, err := g.planner().Movement(ctx)
			if err != nil || plan == nil {
				t.Fatalf("movingFundsTxMaxTotalFee %d: no movement plan (%v); stock moving funds runs under any cap above zero", cap, err)
			}
			if got, want := plan.Fees().Maximum, bound168(cap); got != want {
				t.Fatalf("movingFundsTxMaxTotalFee %d: plan fee bound %d, want min(cap, MaxSatoshi) = %d", cap, got, want)
			}
			if got := plan.Snapshot().Fee(); got != rate {
				t.Fatalf("movingFundsTxMaxTotalFee %d: planned fee %d, want the FeeRate fee %d", cap, got, rate)
			}
			p := g.planner()
			p.Fee, p.MaximumSatPerVByte = 10_000, 1_000
			plan, err = p.Movement(ctx)
			if err != nil || plan == nil || plan.Snapshot().Fee() != 10_000 || plan.Fees().Maximum != bound168(cap) {
				t.Fatalf("movingFundsTxMaxTotalFee %d: coordinated fee 10000 not planned exactly within min(cap, MaxSatoshi): %v", cap, err)
			}
		})
	}
}

// TestMovementFeeCapWithinSupplyUnchanged (control, passes on base): for a cap
// at or below MaxSatoshi the plan's bound is the cap and the fee
// min(FeeRate fee, cap); a coordinated fee one above a normal cap, and a fee
// above MaxSatoshi under an unbounded cap, are still refused. Refs #168.
func TestMovementFeeCapWithinSupplyUnchanged(t *testing.T) {
	t.Parallel()
	rate := movementRateFee168(t)
	for _, cap := range capsWithinSupply168 {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			t.Parallel()
			g := commitUnderCap168(t, cap)
			plan, err := g.planner().Movement(context.Background())
			if err != nil {
				t.Fatalf("movingFundsTxMaxTotalFee %d: no movement plan: %v", cap, err)
			}
			if got := plan.Fees().Maximum; got != int64(cap) {
				t.Fatalf("movingFundsTxMaxTotalFee %d: plan fee bound %d, want the cap", cap, got)
			}
			if got, want := plan.Snapshot().Fee(), min168(rate, cap); got != want {
				t.Fatalf("movingFundsTxMaxTotalFee %d: planned fee %d, want %d", cap, got, want)
			}
		})
	}
	for name, c := range map[string]struct {
		cap uint64
		fee int64
	}{
		"coordinated fee one above cap 300":  {300, 301},
		"coordinated fee one above cap 5000": {5_000, 5_001},
		// Refused because the fee exceeds the 300,000 sat main (the
		// transaction cannot pay it), not because of a supply bound.
		"fee above the main (MaxSatoshi+1), unbounded cap": {math.MaxUint64, bitcoin.MaxSatoshi + 1},
		"fee above the main, cap above supply":             {supply168 + 1, 300_001},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := commitUnderCap168(t, c.cap)
			p := g.planner()
			p.Fee, p.MaximumSatPerVByte = c.fee, bitcoin.MaxSatoshi
			var plan *signing.Plan
			err := recovered(func() error { var err error; plan, err = p.Movement(context.Background()); return err })
			if err == nil || errors.Is(err, errPanic) || plan != nil {
				t.Fatalf("cap %d: fee %d planned (%v) or crashed: %v", c.cap, c.fee, plan != nil, err)
			}
		})
	}
}

// TestIncomingSweepFeeCapAboveSupplyStillPlans: with
// movedFundsSweepTxMaxTotalFee above MaxSatoshi, live and recorded, up to
// type(uint64).max, the planner still builds the incoming sweep (with and
// without a target main); its fee bound is min(every cap, MaxSatoshi) and its
// fee the FeeRate fee, or the coordinated fee exactly. Refs #168.
func TestIncomingSweepFeeCapAboveSupplyStillPlans(t *testing.T) {
	t.Parallel()
	type sweepCase struct {
		live    uint64
		records []uint64
	}
	cases := map[string]sweepCase{
		"live max, records supply+1 and 2*supply": {math.MaxUint64, []uint64{supply168 + 1, 2 * supply168}},
		"live supply+1, record max":               {supply168 + 1, []uint64{math.MaxUint64}},
	}
	for _, cap := range capsAboveSupply168 {
		cases[fmt.Sprint("live and record ", cap)] = sweepCase{cap, []uint64{cap}}
	}
	for _, withMain := range []bool{false, true} {
		rates := map[int]int64{1: sweepRateFee168(t, withMain, 1), 2: sweepRateFee168(t, withMain, 2)}
		for name, c := range cases {
			t.Run(fmt.Sprintf("main %v, %s", withMain, name), func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				least := c.live
				for _, r := range c.records {
					if r < least {
						least = r
					}
				}
				_, p := sweepUnderCaps168(t, withMain, c.live, c.records...)
				plan, err := p.IncomingSweep(ctx)
				if err != nil || plan == nil {
					t.Fatalf("movedFundsSweepTxMaxTotalFee %d (records %v): no incoming sweep plan (%v); stock moved funds sweep runs under any cap above zero", c.live, c.records, err)
				}
				if got, want := plan.Fees().Maximum, bound168(least); got != want {
					t.Fatalf("movedFundsSweepTxMaxTotalFee %d (records %v): plan fee bound %d, want min(caps, MaxSatoshi) = %d", c.live, c.records, got, want)
				}
				if got, want := plan.Snapshot().Fee(), rates[len(c.records)]; got != want {
					t.Fatalf("movedFundsSweepTxMaxTotalFee %d (records %v): planned fee %d, want the FeeRate fee %d", c.live, c.records, got, want)
				}
				p.Fee, p.MaximumSatPerVByte = 10_000, 1_000
				plan, err = p.IncomingSweep(ctx)
				if err != nil || plan == nil || plan.Snapshot().Fee() != 10_000 || plan.Fees().Maximum != bound168(least) {
					t.Fatalf("movedFundsSweepTxMaxTotalFee %d (records %v): coordinated fee 10000 not planned exactly within min(caps, MaxSatoshi): %v", c.live, c.records, err)
				}
			})
		}
	}
}

// TestIncomingSweepFeeCapWithinSupplyUnchanged (control, passes on base): when
// the least of the live and recorded caps is at most MaxSatoshi, the bound is
// that cap and the fee min(FeeRate fee, cap), even if another cap is above
// MaxSatoshi; a coordinated fee one above that cap is still refused, and so
// is a fee above MaxSatoshi under unbounded caps. Refs #168.
func TestIncomingSweepFeeCapWithinSupplyUnchanged(t *testing.T) {
	t.Parallel()
	rate := sweepRateFee168(t, false, 1)
	type sweepCase struct{ live, record, least uint64 }
	cases := map[string]sweepCase{
		"live max, record 5000":   {math.MaxUint64, 5_000, 5_000},
		"live 5000, record max":   {5_000, math.MaxUint64, 5_000},
		"live supply, record max": {supply168, math.MaxUint64, supply168},
		"live max, record 300":    {math.MaxUint64, 300, 300},
		"live supply+1, record 1": {supply168 + 1, 1, 1},
	}
	for _, cap := range capsWithinSupply168 {
		cases[fmt.Sprint("live and record ", cap)] = sweepCase{cap, cap, cap}
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, p := sweepUnderCaps168(t, false, c.live, c.record)
			plan, err := p.IncomingSweep(context.Background())
			if err != nil {
				t.Fatalf("live %d, record %d: no incoming sweep plan: %v", c.live, c.record, err)
			}
			if got := plan.Fees().Maximum; got != int64(c.least) {
				t.Fatalf("live %d, record %d: plan fee bound %d, want the least cap %d", c.live, c.record, got, c.least)
			}
			if got, want := plan.Snapshot().Fee(), min168(rate, c.least); got != want {
				t.Fatalf("live %d, record %d: planned fee %d, want %d", c.live, c.record, got, want)
			}
		})
	}
	for name, c := range map[string]struct {
		live, record uint64
		fee          int64
	}{
		"coordinated fee one above the live cap": {5_000, math.MaxUint64, 5_001},
		"coordinated fee one above a record cap": {math.MaxUint64, 300, 301},
		// Refused because the fee exceeds the 99,000 sat input (the
		// transaction cannot pay it), not because of a supply bound.
		"fee above the inputs (MaxSatoshi+1), unbounded caps": {math.MaxUint64, math.MaxUint64, bitcoin.MaxSatoshi + 1},
		"fee above the inputs, caps above supply":             {supply168 + 1, supply168 + 1, 99_001},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, p := sweepUnderCaps168(t, false, c.live, c.record)
			p.Fee, p.MaximumSatPerVByte = c.fee, bitcoin.MaxSatoshi
			var plan *signing.Plan
			err := recovered(func() error { var err error; plan, err = p.IncomingSweep(context.Background()); return err })
			if err == nil || errors.Is(err, errPanic) || plan != nil {
				t.Fatalf("live %d, record %d: fee %d planned (%v) or crashed: %v", c.live, c.record, c.fee, plan != nil, err)
			}
		})
	}
}

// TestFeeCapAboveMaxInt64NeverNegative: a cap of MaxInt64+1 or more never
// becomes a negative fee bound: both planners still plan, with a bound in
// (0, MaxSatoshi] and a fee in (0, bound]. Refs #168.
func TestFeeCapAboveMaxInt64NeverNegative(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, what string, cap uint64, plan *signing.Plan, err error) {
		t.Helper()
		if err != nil || plan == nil {
			t.Fatalf("%s cap %d (int64 %d): no plan (%v); the cap must not wrap to a negative bound", what, cap, int64(cap), err)
		}
		bound, fee := plan.Fees().Maximum, plan.Snapshot().Fee()
		if bound <= 0 || bound > bitcoin.MaxSatoshi || fee <= 0 || fee > bound {
			t.Fatalf("%s cap %d: fee bound %d, fee %d; want 0 < fee <= bound <= MaxSatoshi", what, cap, bound, fee)
		}
	}
	for _, cap := range capsAboveMaxInt64168 {
		t.Run(fmt.Sprint("movement ", cap), func(t *testing.T) {
			t.Parallel()
			plan, err := commitUnderCap168(t, cap).planner().Movement(context.Background())
			check(t, "movingFundsTxMaxTotalFee", cap, plan, err)
		})
		t.Run(fmt.Sprint("incoming sweep ", cap), func(t *testing.T) {
			t.Parallel()
			_, p := sweepUnderCaps168(t, true, cap, cap)
			plan, err := p.IncomingSweep(context.Background())
			check(t, "movedFundsSweepTxMaxTotalFee", cap, plan, err)
		})
	}
}

// TestFeeCapAboveSupplyPlanIsAuthorized: the Authority compares the uint64
// fee against the raw cap (the commitment's MaxFee; the least of the live and
// recorded sweep caps), so the plan built under a cap above MaxSatoshi is
// authorized, movement and incoming sweep alike. Refs #168.
func TestFeeCapAboveSupplyPlanIsAuthorized(t *testing.T) {
	t.Parallel()
	for _, cap := range []uint64{supply168 + 1, math.MaxInt64 + 1, math.MaxUint64} {
		t.Run(fmt.Sprint("movement ", cap), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			g := commitUnderCap168(t, cap)
			plan, err := g.planner().Movement(ctx)
			if err != nil || plan == nil {
				t.Fatalf("movingFundsTxMaxTotalFee %d: no movement plan to authorize (%v)", cap, err)
			}
			if _, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan); err != nil {
				t.Fatalf("movingFundsTxMaxTotalFee %d: the planned movement (fee %d) was not authorized: %v", cap, plan.Snapshot().Fee(), err)
			}
		})
		t.Run(fmt.Sprint("incoming sweep ", cap), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			g, p := sweepUnderCaps168(t, true, cap, cap)
			plan, err := p.IncomingSweep(ctx)
			if err != nil || plan == nil {
				t.Fatalf("movedFundsSweepTxMaxTotalFee %d: no incoming sweep plan to authorize (%v)", cap, err)
			}
			if _, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan); err != nil {
				t.Fatalf("movedFundsSweepTxMaxTotalFee %d: the planned sweep (fee %d) was not authorized: %v", cap, plan.Snapshot().Fee(), err)
			}
		})
	}
}

// TestNormalFeeCapAuthorityStillRefusesAboveCap (control, passes on base): a
// plan whose fee is one above a normal cap is refused by the Authority,
// movement and incoming sweep alike. Refs #168.
func TestNormalFeeCapAuthorityStillRefusesAboveCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("movement", func(t *testing.T) {
		t.Parallel()
		g := commitUnderCap168(t, 5_000)
		plan, err := g.planner().Movement(ctx)
		if err != nil {
			t.Fatal("fixture movement:", err)
		}
		// Injected state (fixture): a recorded commitment's MaxFee cannot
		// change on chain (FrostMovement.sol freezes it at commit), and this
		// planner refuses a fee above it. Lowering it stands for a plan this
		// planner did not build; the Authority's fee check is defense in depth.
		c := g.f.commitments[g.source.ID]
		c.MaxFee = uint64(plan.Snapshot().Fee()) - 1
		g.f.commitments[g.source.ID] = c
		if _, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
			t.Fatalf("a movement fee one above the committed cap was authorized: %v", err)
		}
	})
	t.Run("incoming sweep", func(t *testing.T) {
		t.Parallel()
		g, p := sweepUnderCaps168(t, true, 5_000, 5_000)
		plan, err := p.IncomingSweep(ctx)
		if err != nil {
			t.Fatal("fixture sweep:", err)
		}
		// Reachable: governance lowers the live movedFundsSweepTxMaxTotalFee
		// after the plan was built; the recorded caps stay as recorded.
		g.f.params.MovedFundsSweepTxMaxTotalFee = uint64(plan.Snapshot().Fee()) - 1
		if _, err := (&Authority{View: g.view}).AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
			t.Fatalf("a sweep fee one above the live moved funds sweep cap was authorized: %v", err)
		}
	})
}
