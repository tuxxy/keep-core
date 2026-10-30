package tbtc_test

// Legacy-only guards of the drain producer, on the pre-FROST Bridge.
//
// tuxxy/keep-core#49 (K09-33): with FROST unconfigured, zero Live wallets and no
// commitment, the producer returns no result (as at base), not an error.
// tuxxy/keep-core#62 (K09-47, pre-cutover half) and tuxxy/keep-core#69
// (K09-56): the legacy-only guard needs an owning test with the due, live=0
// state seeded: the client must not send the reset (or anything else) on the
// pre-FROST path. tuxxy/keep-core#68 (K09-55): the legacy safety-margin check
// makes no RPC beyond base and uses the client's clock.
//
// Invariant: against the pre-FROST Bridge built from tbtc-v2 40a11d1d, with
// zero Live wallets and the source's reset delay passed, the moving-funds
// task returns no proposal and no error, the client sends no transaction and
// calls only base functions, and the source's timeout is untouched (base
// behaviour: recovery is left to third parties). The safety-margin check
// succeeds on the wall clock (the chain runs legacyChainLag behind) and makes
// only the JSON-RPC requests base makes for it.
//
// Spec: KC-SF-V3 line 362, SC-01, D4(c), D14 (base calls only), shared rule 4.
// Reviews 5655962724 (K09-33), 5674230284 (K09-47), 5681389770 (K09-55,
// K09-56). These pass on the integration branch (no such dispatch exists);
// they guard the #34 fix. Controls: test/remediation-b2/controls/.

import (
	"testing"

	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// drainBaseValidationMethods are the JSON-RPC methods base's
// ValidateMovingFundsSafetyMargin makes through the production chain (a
// contract read, the commitment events, the block height for the event
// look-back). Measured on keep-core base 4c9b65ec4 with these files.
var drainBaseValidationMethods = map[string]bool{
	"eth_call":        true,
	"eth_getLogs":     true,
	"eth_blockNumber": true,
}

// TestDrainProducer_PreFrostUnchanged: see the file comment. Refs
// tuxxy/keep-core#49, tuxxy/keep-core#62, tuxxy/keep-core#68,
// tuxxy/keep-core#69.
func TestDrainProducer_PreFrostUnchanged(t *testing.T) {
	for _, encoding := range legacyEncodings {
		encoding := encoding
		t.Run(encoding.String(), func(t *testing.T) {
			drainPreFrostRound(t, encoding)
		})
	}
}

func drainPreFrostRound(t *testing.T, encoding legacyRevertEncoding) {
	w := newLegacyWorld(t)
	dw := &drainWorld{legacyWorld: w, setup: drainSetup{encoding: encoding}}
	dw.shortenTimeouts()
	w.heartbeatFailed(w.target)
	live, err := w.legacy.LiveWalletsCount(nil)
	if err != nil || live != 0 {
		t.Fatalf("setup: liveWalletsCount %d (%v), want 0", live, err)
	}
	_, requestedAt, _ := dw.sourceWallet()
	// Seed the state a reset needs: the reset delay has passed.
	dw.elapseTo(uint64(requestedAt) + uint64(drainResetDelay) + 60)

	dw.recorder = newLegacyRecorder(t, w.anvilURL, encoding)
	dw.client, err = w.connect(dw.recorder)
	if err != nil {
		t.Fatalf("pre-FROST startup: %v", err)
	}

	// The safety-margin check, alone.
	before := len(dw.recorder.Calls())
	if err := tbtc.ValidateMovingFundsSafetyMargin(w.source.pkh, dw.client); err != nil {
		t.Errorf("pre-FROST safety margin: the check fails on the client's clock: %v", err)
	}
	for _, call := range dw.recorder.Calls()[before:] {
		if !drainBaseValidationMethods[call.Method] {
			t.Errorf("pre-FROST safety margin: the check made a %s request, which base does not make", call.Method)
		}
	}

	stop := w.mineInBackground()
	var proposed bool
	runErr := legacyBounded(t, "pre-FROST moving-funds window", func() error {
		_, ok, err := tbtcpg.NewMovingFundsTask(dw.client, w.btc).Run(dw.request())
		proposed = ok
		return err
	})
	stop()
	if runErr != nil {
		t.Errorf("pre-FROST, zero Live wallets: the moving-funds task returned an error instead of no result: %v", runErr)
	}
	if proposed {
		t.Errorf("pre-FROST, zero Live wallets: the moving-funds task made a proposal")
	}
	for _, call := range dw.recorder.Calls() {
		if call.Method == "eth_sendRawTransaction" {
			t.Errorf("pre-FROST, zero Live wallets: the client sent a transaction to %s (selector 0x%s); base sends none",
				call.To.Hex(), call.Selector)
		}
	}
	if _, next, _ := dw.sourceWallet(); next != requestedAt {
		t.Errorf("pre-FROST, zero Live wallets: the source's moving-funds timeout was reset; base never resets it")
	}
	w.assertBaselineSurface(t, "pre-FROST zero-Live window", dw.recorder.Calls())
}
