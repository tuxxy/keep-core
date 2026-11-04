package tbtc_test

// Regression test RV116-1 (review of tuxxy/keep-core#116; reviewer probe): a FROST drain target must not
// be classified as a legacy target because the endpoint's log index is short.
//
// The moving-funds action of a legacy (ECDSA) source resolves its committed
// targets with tbtc.FrostTargetKeys; a target it does not resolve as FROST is
// paid P2WPKH(label). A FROST label is keccak-derived, so nobody can spend that
// output. The Bridge state (walletScheme == 2) is readable at the head; only
// eth_getLogs lags (drainFaults.SetLogHorizon, the batch-2 R6 fault model).

import (
	"context"
	"testing"

	"github.com/keep-network/keep-core/pkg/tbtc"
)

func TestRV116_LaggingLogIndexKeepsFrostTarget(t *testing.T) {
	setup := drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertCode3,
		targets: 1, faults: true}
	dw := newDrainWorld(t, setup)
	horizon, err := dw.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	late := dw.createFrostTarget()
	labels := [][20]byte{late.label}

	keys, err := tbtc.FrostTargetKeys(dw.client, labels)
	if err != nil || keys[late.label] != late.q {
		t.Fatalf("complete index: keys %x, err %v; want the target's output key", keys, err)
	}

	dw.faults.SetLogHorizon(horizon)
	keys, err = tbtc.FrostTargetKeys(dw.client, labels)
	t.Logf("lagging index: withheld %d logs; keys %x, err %v", dw.faults.Hidden(), keys, err)
	if err == nil && keys[late.label] != late.q {
		t.Fatalf("lagging log index: FROST target %x resolved as a legacy target (nil error); "+
			"the moving-funds transaction would pay P2WPKH(label)", late.label)
	}
}
