package redemption

// Acceptance tests for tuxxy/keep-core#176, the redemption twin of #168
// (raised from REVIEW-TEST-168 note 2 and REVIEW-FIX-168 note 3).
// Refs #176
//
// Invariant: redemptionTxMaxTotalFee is any uint64 above zero, and under every
// such cap the FROST redemption planner builds a plan whose signed fee bound
// (Intent.Fees.Maximum, part of the intent preimage and so of the admitted
// intent commitment) is min(cap, bitcoin.MaxSatoshi); the follower's
// validation, the attester and a rebuilding member accept that plan. No
// conversion of the cap yields a negative bound. For caps at or below
// MaxSatoshi nothing changes: the bound is the cap, a fee above the cap is
// refused with ErrFeeDomain, and a plan whose bound exceeds the live cap is
// refused with ErrFeeDomain.
//
// Oracle, stock behaviour:
//   - tbtc-v2 requires only redemptionTxMaxTotalFee > 0, stored as uint64
//     (BridgeState.sol updateRedemptionParameters);
//   - stock keep-core compares the fee against the uint64 cap directly, with
//     no upper bound (origin/main pkg/tbtcpg/redemptions.go:552
//     `uint64(totalFee) > txMaxTotalFee`, and applyWalletTxFeeFloor in
//     pkg/tbtcpg/fee.go); pkg/tbtc only carries the value (parameters.go), and
//     the follower check is on chain (WalletProposalValidator, uint64 <=). So a
//     cap such as type(uint64).max ("no cap") still lets stock redemptions run.
//
// The MaxSatoshi ceiling on the signed bound is the signing package's own
// guard (pkg/frost/signing/plan.go NewPlan), not a KC-SF-V3 rule. Per the
// issue, checkedAmount stays as it is for request amounts.
//
// Fixtures: newWorld (redemption_test.go) over the in-memory fakeChain
// (fake_test.go): a fixed test wallet key, a synthetic 1,000,000 sat main
// outpoint and two funded P2WPKH requests (100,000 sat gross, 500 sat treasury
// fee, 2,000 sat captured fee cap each). Only the live redemptionTxMaxTotalFee
// (a governance value, fakeChain.maxTotalFee) and each operator's local fee
// policy are changed; no state recorded on chain (requests, custody) is
// edited. Planner.Build, Planner.Validate, Planner.Rebuild and Attester.Attest
// are the real code.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// supply176 is bitcoin.MaxSatoshi as a cap value.
const supply176 = uint64(bitcoin.MaxSatoshi)

// capsAboveSupply176 are governance caps above MaxSatoshi, on both sides of
// MaxInt64, up to type(uint64).max.
var capsAboveSupply176 = []uint64{
	supply176 + 1,
	2 * supply176,
	math.MaxInt64,
	math.MaxInt64 + 1,
	1<<63 + 5_000,
	math.MaxUint64 - 1,
	math.MaxUint64,
}

// capsAboveMaxInt64176 are the caps whose int64 conversion wraps negative.
var capsAboveMaxInt64176 = []uint64{math.MaxInt64 + 1, 1<<63 + 5_000, math.MaxUint64 - 1, math.MaxUint64}

// bound176 is the fee bound a plan must carry for cap: min(cap, MaxSatoshi).
func bound176(cap uint64) int64 {
	if cap > supply176 {
		return bitcoin.MaxSatoshi
	}
	return int64(cap)
}

// worldUnderCap176 is the unit world with the live redemptionTxMaxTotalFee
// set to cap (a governance change).
func worldUnderCap176(t *testing.T, cap uint64) *world {
	t.Helper()
	w := newWorld(t)
	w.chain.maxTotalFee = cap
	return w
}

// supplyPlan176 is the plan the real Build makes under a live cap of exactly
// MaxSatoshi, the largest cap base accepts, with the given coordinated fee
// (0: FeeRate sizing). Its bound is MaxSatoshi = min(cap, MaxSatoshi) for
// every cap above supply, so it is the plan a correct planner builds under
// any such cap (fixture: the leader's plan, built where base and a fix agree).
func supplyPlan176(t *testing.T, fee int64) *signing.Plan {
	t.Helper()
	w := worldUnderCap176(t, supply176)
	w.planner.Fee = fee
	plan, _, err := w.planner.Build(context.Background(), w.chain.head, w.ids)
	if err != nil {
		t.Fatal("fixture: plan under a cap of MaxSatoshi:", err)
	}
	if plan.Fees().Maximum != bitcoin.MaxSatoshi {
		t.Fatalf("fixture: plan under a cap of MaxSatoshi has bound %d", plan.Fees().Maximum)
	}
	return plan
}

// rateFee176 is the FeeRate fee of the fixture redemption, read from a plan
// under the default cap of 5,000, which does not bind it.
func rateFee176(t *testing.T) int64 {
	t.Helper()
	w := newWorld(t)
	plan, _, err := w.planner.Build(context.Background(), w.chain.head, w.ids)
	if err != nil {
		t.Fatal("fixture reference redemption:", err)
	}
	return plan.Snapshot().Fee()
}

// coordinatedFee176 is an exact leader fee (D19) inside the fixture's fee-rate
// envelope (10 sat/vB) and the requests' captured caps (2 x 2,000), and other
// than the FeeRate fee.
const coordinatedFee176 = 1_000

// TestRedemptionFeeCapAboveSupplyStillBuilds: with redemptionTxMaxTotalFee
// above MaxSatoshi, up to type(uint64).max, the planner builds the
// redemption. Its signed fee bound is min(cap, MaxSatoshi), its fee the
// FeeRate fee or the coordinated fee exactly, and the plan, commitment
// included, is the one built under a cap of MaxSatoshi. A follower's
// Validate accepts it. A coordinated fee above MaxSatoshi is still refused
// with ErrFeeDomain. Refs #176.
func TestRedemptionFeeCapAboveSupplyStillBuilds(t *testing.T) {
	t.Parallel()
	rate := rateFee176(t)
	reference := supplyPlan176(t, 0)
	coordinated := supplyPlan176(t, coordinatedFee176)
	for _, cap := range capsAboveSupply176 {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			w := worldUnderCap176(t, cap)
			plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
			if err != nil || plan == nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: no redemption plan (%v); stock redemptions run under any cap above zero", cap, err)
			}
			if got, want := plan.Fees().Maximum, bound176(cap); got != want {
				t.Fatalf("redemptionTxMaxTotalFee %d: signed fee bound %d, want min(cap, MaxSatoshi) = %d", cap, got, want)
			}
			if got := plan.Snapshot().Fee(); got != rate {
				t.Fatalf("redemptionTxMaxTotalFee %d: planned fee %d, want the FeeRate fee %d", cap, got, rate)
			}
			if plan.Commitment() != reference.Commitment() {
				t.Fatalf("redemptionTxMaxTotalFee %d: plan differs from the plan under a cap of MaxSatoshi", cap)
			}
			if err := w.planner.Validate(ctx, w.chain.head, plan); err != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: follower refused the plan built under it: %v", cap, err)
			}

			p := w.planner
			p.Fee = coordinatedFee176
			plan, _, err = p.Build(ctx, w.chain.head, w.ids)
			if err != nil || plan == nil || plan.Snapshot().Fee() != coordinatedFee176 || plan.Fees().Maximum != bound176(cap) || plan.Commitment() != coordinated.Commitment() {
				t.Fatalf("redemptionTxMaxTotalFee %d: coordinated fee %d not planned exactly within min(cap, MaxSatoshi): %v", cap, coordinatedFee176, err)
			}

			p.Fee = bitcoin.MaxSatoshi + 1
			if plan, _, err := p.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrFeeDomain) || plan != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: coordinated fee MaxSatoshi+1: plan %v, err %v, want ErrFeeDomain", cap, plan != nil, err)
			}
		})
	}
}

// TestRedemptionFeeCapAboveSupplyRebuilds: a member that did not attest
// rebuilds the admitted plan under a cap above MaxSatoshi. Rebuild runs Build
// with the admitted fee, so it must reproduce the signed bound
// min(cap, MaxSatoshi) and with it the admitted intent commitment.
// Refs #176.
func TestRedemptionFeeCapAboveSupplyRebuilds(t *testing.T) {
	t.Parallel()
	admittedPlan := supplyPlan176(t, 0)
	for _, cap := range capsAboveSupply176 {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			t.Parallel()
			w := worldUnderCap176(t, cap)
			// Fixture: the reservation admitted the leader's plan (supplyPlan176)
			// at the block after the head; the requests and custody are as
			// recorded at the head.
			intent := Intent{AdmittedBlock: w.chain.head + 1, RequestIDs: w.ids, Certificate: Certificate{IntentCommitment: admittedPlan.Commitment()}}
			for _, r := range admittedPlan.Requests() {
				intent.Payouts = append(intent.Payouts, uint64(r.Value))
			}
			plan, err := w.planner.Rebuild(context.Background(), intent)
			if err != nil || plan == nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: admitted plan not rebuilt: %v", cap, err)
			}
			if plan.Fees().Maximum != bound176(cap) {
				t.Fatalf("redemptionTxMaxTotalFee %d: rebuilt bound %d, want %d", cap, plan.Fees().Maximum, bound176(cap))
			}
		})
	}
}

// TestRedemptionFeeCapAboveMaxInt64NeverRefusesValidPlan: with
// redemptionTxMaxTotalFee at MaxInt64+1 or more, the follower's Validate and
// the attester never refuse an otherwise valid plan because of the cap: the
// cap is never read as a negative bound. Both the plan bounded at MaxSatoshi
// and a plan bounded by an earlier normal cap (built before governance raised
// it) are accepted. Refs #176.
func TestRedemptionFeeCapAboveMaxInt64NeverRefusesValidPlan(t *testing.T) {
	t.Parallel()
	for _, cap := range capsAboveMaxInt64176 {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			// The leader's plan under this cap: bound MaxSatoshi.
			plan := supplyPlan176(t, 0)
			w := worldUnderCap176(t, cap)
			if err := w.planner.Validate(ctx, w.chain.head, plan); err != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: valid plan bounded at MaxSatoshi refused: %v", cap, err)
			}
			if _, err := attester(w, newMemJournal()).Attest(ctx, plan, w.chain.head+1); err != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: attester refused a valid plan bounded at MaxSatoshi: %v", cap, err)
			}

			// A plan built under the default cap of 5,000, then governance
			// raised the cap to this value before the follower validated.
			v := newWorld(t)
			small, _, err := v.planner.Build(ctx, v.chain.head, v.ids)
			if err != nil {
				t.Fatal("fixture plan under a cap of 5000:", err)
			}
			v.chain.maxTotalFee = cap
			if err := v.planner.Validate(ctx, v.chain.head, small); err != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: valid plan bounded at 5000 refused: %v", cap, err)
			}
		})
	}
}

// TestRedemptionFeeCapAboveSupplyKeepsOtherFeeChecks (control, passes on
// base): under a cap above MaxSatoshi the follower still refuses a plan whose
// fee-rate ceiling is above its own or whose minimum fee is below its own,
// with ErrFeeDomain. The cap clamp must not skip the rest of the fee check.
// The plans are what a leader with another local fee policy proposes
// (fixture: a supplied proposal). Refs #176.
func TestRedemptionFeeCapAboveSupplyKeepsOtherFeeChecks(t *testing.T) {
	t.Parallel()
	for _, cap := range append([]uint64{supply176}, capsAboveSupply176...) {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			for _, tc := range []struct {
				name   string
				leader func(*FeePolicy)
			}{
				{"fee-rate ceiling above the follower's", func(f *FeePolicy) { f.MaximumSatPerVByte = 11 }},
				{"minimum fee below the follower's", func(f *FeePolicy) { f.Minimum = 99 }},
			} {
				// Built under the MaxSatoshi cap, the bound every cap above
				// supply allows; validated under cap.
				w := worldUnderCap176(t, supply176)
				tc.leader(&w.planner.Fees)
				plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
				if err != nil {
					t.Fatalf("fixture leader plan (%s): %v", tc.name, err)
				}
				follower := worldUnderCap176(t, cap)
				if err := follower.planner.Validate(ctx, follower.chain.head, plan); !errors.Is(err, ErrFeeDomain) {
					t.Fatalf("redemptionTxMaxTotalFee %d: plan with %s: %v, want ErrFeeDomain", cap, tc.name, err)
				}
			}
		})
	}
}

// TestRedemptionFeeCapWithinSupplyUnchanged (control, passes on base): for a
// cap at or below MaxSatoshi the signed bound is the cap and the follower
// accepts the plan; a cap below the fee refuses the plan with ErrFeeDomain;
// a coordinated fee one above a normal cap is refused with ErrFeeDomain and
// a coordinated fee equal to it is planned exactly; and a follower refuses,
// with ErrFeeDomain, a plan whose fee or bound is above the live normal cap
// (governance lowered it after the leader planned), while it accepts a plan
// whose bound is below the live cap. Refs #176.
func TestRedemptionFeeCapWithinSupplyUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rate := rateFee176(t)
	for _, cap := range []uint64{uint64(rate), 5_000, 1_000_000, supply176 - 1, supply176} {
		t.Run(fmt.Sprint("cap ", cap), func(t *testing.T) {
			t.Parallel()
			w := worldUnderCap176(t, cap)
			plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
			if err != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: no plan: %v", cap, err)
			}
			if plan.Fees().Maximum != int64(cap) || plan.Snapshot().Fee() != rate {
				t.Fatalf("redemptionTxMaxTotalFee %d: bound %d fee %d, want bound = cap and fee %d", cap, plan.Fees().Maximum, plan.Snapshot().Fee(), rate)
			}
			if err := w.planner.Validate(ctx, w.chain.head, plan); err != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d: follower refused: %v", cap, err)
			}
		})
	}
	for _, cap := range []uint64{1, 100, uint64(rate) - 1} {
		t.Run(fmt.Sprint("cap below the fee ", cap), func(t *testing.T) {
			t.Parallel()
			w := worldUnderCap176(t, cap)
			if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrFeeDomain) || plan != nil {
				t.Fatalf("redemptionTxMaxTotalFee %d below the fee %d: plan %v, err %v, want ErrFeeDomain", cap, rate, plan != nil, err)
			}
		})
	}
	t.Run("coordinated fee against a normal cap", func(t *testing.T) {
		t.Parallel()
		const cap = 3_000
		w := worldUnderCap176(t, cap)
		w.planner.Fees.MaximumSatPerVByte = 1_000
		w.planner.Fee = cap + 1
		if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrFeeDomain) || plan != nil {
			t.Fatalf("coordinated fee %d above cap %d: plan %v, err %v, want ErrFeeDomain", cap+1, cap, plan != nil, err)
		}
		w.planner.Fee = cap
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil || plan.Snapshot().Fee() != cap || plan.Fees().Maximum != cap {
			t.Fatalf("coordinated fee equal to cap %d not planned exactly: %v", cap, err)
		}
		// Governance lowers the cap below the planned fee before the follower
		// validates: the fee is now above the live cap.
		w.chain.maxTotalFee = cap - 1
		if err := w.planner.Validate(ctx, w.chain.head, plan); !errors.Is(err, ErrFeeDomain) {
			t.Fatalf("plan with fee %d under live cap %d: %v, want ErrFeeDomain", cap, cap-1, err)
		}
		if _, err := attester(w, newMemJournal()).Attest(ctx, plan, w.chain.head+1); !errors.Is(err, ErrFeeDomain) {
			t.Fatalf("attester: plan with fee %d under live cap %d: %v, want ErrFeeDomain", cap, cap-1, err)
		}
	})
	t.Run("bound above the live normal cap", func(t *testing.T) {
		t.Parallel()
		plan := supplyPlan176(t, 0)
		for _, live := range []uint64{5_000, supply176 - 1} {
			w := worldUnderCap176(t, live)
			if err := w.planner.Validate(ctx, w.chain.head, plan); !errors.Is(err, ErrFeeDomain) {
				t.Fatalf("plan bounded at MaxSatoshi under live cap %d: %v, want ErrFeeDomain", live, err)
			}
		}
	})
	t.Run("bound below the live normal cap", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal("fixture plan under a cap of 5000:", err)
		}
		for _, live := range []uint64{5_001, 1_000_000, supply176} {
			w.chain.maxTotalFee = live
			if err := w.planner.Validate(ctx, w.chain.head, plan); err != nil {
				t.Fatalf("plan bounded at 5000 under live cap %d refused: %v", live, err)
			}
		}
	})
}
