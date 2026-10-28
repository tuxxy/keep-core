package tbtc_test

// The no-route recovery and the post-cutover legacy fence, host side.
//
// tuxxy/keep-core#34 (K09-15, second half): after the cutover the honest
// producer keeps creating legacy obligations. tuxxy/keep-core#51 (K09-35): no
// host submits the reset the contract's no-route recovery relies on.
// tuxxy/keep-core#54 (K09-39): before the cutover, with zero Live wallets and a
// partial custody budget (or any other unusable drain), the source can neither
// drain nor reset and is terminated. tuxxy/keep-core#62 (K09-47): a
// default-config host against FrostOnly with a Live legacy wallet and a
// blocked drain must reset when due and never commit legacy targets.
// tuxxy/tbtc-v2#18 (K09-16) and tuxxy/tbtc-v2#20 (K09-28) are the contract
// halves; this test runs against the integration contracts, so a case can
// also fail because the contract still refuses the reset (#20) or still
// accepts the legacy commitment (#18).
//
// Invariant: a legacy MovingFunds source with no commitment and no admissible
// route (FrostOnly with the drain contained, disabled, without an eligible
// target or without budget, whatever the legacy Live count; or LegacyOpen and
// LegacyDraining with zero Live wallets and the drain unusable) is never
// terminated while the production client runs its moving-funds duty:
//   - the host makes no commitment attempt (no new legacy obligation after the
//     cutover, no doomed drain);
//   - each time the reset delay has passed, the host resets the timeout, so a
//     third party's notifyMovingFundsTimeout is refused past the deadline it
//     would otherwise have had, for two consecutive periods;
//   - once the route opens, the host commits the drain to FROST targets.
//
// Spec: KC-SF-V3 line 448, line 616, line 729 (SC-01: "Existing ECDSA
// obligations retain reachable settlement, recovery and defense transitions";
// "New routing cannot assign new legacy obligations that defeat drain");
// KC-VER P-K11-CONTAINMENT-KEEPS-EXITS. Reviews 5667802743 (K09-35 criterion:
// "under containment, a witness without harness calls shows the host issuing
// the reset and no termination across two timeout periods"), 5671041158
// (K09-39 criterion: modes 0 and 1 x {partial budget, exhausted, contained,
// drain off, zero eligible} with zero Live), 5674230284 (K09-47 criterion),
// 5655963449 and 5667803422 (K09-28 criterion).
//
// The host is driven only through tbtcpg.MovingFundsTask.Run, once per
// coordination window. Which reset entrypoint it uses is its choice.

import (
	"strings"
	"testing"
)

type drainRecoveryCase struct {
	name  string
	setup drainSetup
}

func drainRecoveryCases() []drainRecoveryCase {
	var cases []drainRecoveryCase
	for _, route := range []drainRoute{routeContained, routeDrainOff, routeZeroEligible, routeBudgetExhausted} {
		for _, live := range []bool{true, false} {
			setup := drainSetup{mode: modeFrostOnly, live: live, route: route, encoding: revertCode3}
			cases = append(cases, drainRecoveryCase{"FrostOnly-" + liveName(live) + "-" + route.String(), setup})
		}
	}
	cases = append(cases, drainRecoveryCase{"FrostOnly-live1-contained-geth-32000-nodata",
		drainSetup{mode: modeFrostOnly, live: true, route: routeContained, encoding: revertGeth}})
	for _, mode := range []uint8{modeLegacyOpen, modeLegacyDraining} {
		for _, route := range []drainRoute{routeBudgetPartial, routeBudgetExhausted, routeContained, routeDrainOff, routeZeroEligible} {
			setup := drainSetup{mode: mode, live: false, route: route, encoding: revertCode3}
			cases = append(cases, drainRecoveryCase{modeName(mode) + "-live0-" + route.String(), setup})
		}
	}
	return cases
}

func liveName(live bool) string {
	if live {
		return "live1"
	}
	return "live0"
}

// TestDrainProducer_NoRouteRecovery: see the file comment. Refs
// tuxxy/keep-core#34, tuxxy/keep-core#51, tuxxy/keep-core#54,
// tuxxy/keep-core#62, tuxxy/keep-core#67, tuxxy/tbtc-v2#18, tuxxy/tbtc-v2#20.
func TestDrainProducer_NoRouteRecovery(t *testing.T) {
	for _, c := range drainRecoveryCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			drainRecoveryRound(t, c.setup)
		})
	}
}

// timedOut is the revert reason that attributes a refused
// notifyMovingFundsTimeout to the deadline, not to anything else.
const timedOut = "Moving funds has not timed out yet"

func drainRecoveryRound(t *testing.T, setup drainSetup) {
	dw := newDrainWorld(t, setup)

	// A window before the reset is due: nothing to do but wait.
	_, err := dw.window("moving-funds window before the reset is due")
	dw.assertNoObligation("before the reset is due", err)

	_, requestedAt, _ := dw.sourceWallet()
	for period := 1; period <= 2; period++ {
		dw.elapseTo(uint64(requestedAt) + uint64(drainResetDelay) + 60)
		_, err := dw.window("moving-funds window with the reset due")
		dw.assertNoObligation("with the reset due", err)
		state, next, _ := dw.sourceWallet()
		if state != 2 {
			t.Fatalf("no-route recovery (%s): source state %d in period %d, want MovingFunds", setup, state, period)
		}
		if next == requestedAt {
			t.Fatalf("no-route recovery (%s): period %d: the host did not reset the moving-funds timeout when it was due "+
				"(no admissible route; window error: %v)", setup, period, err)
		}
		// The deadline the source had before this reset has now passed: a
		// third party's timeout notification must be refused for the
		// deadline, and for nothing else.
		dw.elapseTo(uint64(requestedAt) + uint64(drainTimeout) + 60)
		if refusal := dw.notifyTimeoutRefusal(); !strings.Contains(refusal, timedOut) {
			t.Fatalf("no-route recovery (%s): period %d: past the source's previous deadline, "+
				"notifyMovingFundsTimeout is not refused for the deadline (refusal %q): the source can be terminated",
				setup, period, refusal)
		}
		requestedAt = next
	}

	// The route opens: the host drains.
	dw.openRoute()
	_, err = dw.window("moving-funds window after the route opened")
	if legacy := dw.committedLegacy(); legacy != nil {
		t.Fatalf("no-route recovery (%s): after the route opened the host committed legacy targets %s", setup, dw.labels(legacy))
	}
	commitments := dw.commitments()
	if len(commitments) != 1 {
		t.Fatalf("no-route recovery (%s): after the route opened, the host made no drain commitment (commitments %d, window error %v)",
			setup, len(commitments), err)
	}
	t.Logf("no-route recovery (%s): two resets, no termination, then the drain to %s", setup, dw.labels(commitments[0]))
}

// assertNoObligation: while no route is admissible the host creates no
// obligation. After the cutover a legacy commitment is the K09-15/K09-16
// defect; any other attempt is a commitment the Bridge must refuse.
func (dw *drainWorld) assertNoObligation(when string, windowErr error) {
	t := dw.t
	t.Helper()
	if legacy := dw.committedLegacy(); legacy != nil {
		if dw.setup.mode == modeFrostOnly {
			t.Fatalf("no-route recovery (%s): %s: the host committed legacy targets %s after the cutover",
				dw.setup, when, dw.labels(legacy))
		}
		t.Fatalf("no-route recovery (%s): %s: the host committed legacy targets %s with no legacy Live wallet",
			dw.setup, when, dw.labels(legacy))
	}
	if n := dw.commitmentAttempts(); n > 0 {
		t.Fatalf("no-route recovery (%s): %s: the host attempted %d moving-funds commitments while no route was admissible (window error: %v)",
			dw.setup, when, n, windowErr)
	}
}
