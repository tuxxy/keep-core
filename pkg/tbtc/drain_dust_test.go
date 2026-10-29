package tbtc_test

// tuxxy/keep-core#55 (K09-40): CanDrain ignored the dust threshold, so the host
// proposed a drain the contract rejects ("FROST drain below dust") every
// window.
//
// Invariant: with the source's main below movingFundsDustThreshold and every
// other drain condition met, the production client attempts no moving-funds
// commitment in any window: no drain (refused below dust), and after the
// cutover no legacy commitment either. The below-dust recovery stays
// reachable: notifyMovingFundsBelowDust is accepted from an account that is
// neither a wallet member nor the host (D8: permissionless), and moves the
// source to Closing.
//
// Spec: KC-SF-V3 line 448 and line 450 ("Reproduce the current
// stranded-moved-funds and under-dust failure modes, then show their specified
// recovery"). Review 5671041158 (K09-40 criterion: "a host test with a
// below-dust main proposes no drain and performs the specified recovery").
// D8: below dust, the call is permissionless and "we run a keeper as a
// backstop".
//
// Not covered (pending, DECISIONS.md D17): whether the keep-core host itself
// is that keeper, i.e. must send notifyMovingFundsBelowDust.

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// A third party: Anvil's development account 3, neither member nor host.
const drainStrangerKey = "5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a"

// TestDrainProducer_BelowDust: see the file comment. Refs tuxxy/keep-core#55.
func TestDrainProducer_BelowDust(t *testing.T) {
	for _, setup := range []drainSetup{
		{mode: modeFrostOnly, live: true, route: routeBelowDust, encoding: revertCode3},
		{mode: modeFrostOnly, live: false, route: routeBelowDust, encoding: revertCode3},
		{mode: modeLegacyOpen, live: false, route: routeBelowDust, encoding: revertCode3},
		{mode: modeLegacyDraining, live: false, route: routeBelowDust, encoding: revertCode3},
	} {
		setup := setup
		t.Run(modeName(setup.mode)+"-"+liveName(setup.live), func(t *testing.T) {
			drainDustRound(t, setup)
		})
	}
}

func drainDustRound(t *testing.T, setup drainSetup) {
	dw := newDrainWorld(t, setup)
	_, requestedAt, _ := dw.sourceWallet()
	for _, at := range []uint64{0, uint64(requestedAt) + uint64(drainResetDelay) + 60} {
		dw.elapseTo(at)
		_, err := dw.window("moving-funds window below dust")
		if legacy := dw.committedLegacy(); legacy != nil && setup.mode == modeFrostOnly {
			t.Fatalf("below dust (%s): the host committed legacy targets %s after the cutover", setup, dw.labels(legacy))
		}
		if n := dw.commitmentAttempts(); n > 0 {
			t.Fatalf("below dust (%s): the host attempted %d moving-funds commitments for a below-dust main (window error: %v)",
				setup, n, err)
		}
	}

	// The specified recovery is reachable by anyone (D8).
	strangerKey, err := crypto.HexToECDSA(drainStrangerKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := dw.admin.Client().Call(nil, "anvil_setBalance", crypto.PubkeyToAddress(strangerKey.PublicKey), "0x3635c9adc5dea00000"); err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(strangerKey, dw.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := dw.legacy.NotifyMovingFundsBelowDust(opts, dw.source.pkh, legacyUTXO(dw.main))
	if err != nil {
		t.Fatalf("below dust (%s): a third party's notifyMovingFundsBelowDust is refused: %v", setup, err)
	}
	if receipt, err := legacyWaitMined(context.Background(), dw.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("below dust (%s): a third party's notifyMovingFundsBelowDust did not succeed: %v", setup, err)
	}
	if state, _, _ := dw.sourceWallet(); state != 3 {
		t.Errorf("below dust (%s): source state %d after the below-dust notification, want Closing", setup, state)
	}
}
