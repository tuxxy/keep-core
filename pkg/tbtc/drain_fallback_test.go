package tbtc_test

// tuxxy/keep-core#47 (K09-31): before the cutover, the producer falls back to
// legacy targets when the drain is enabled but unusable.
//
// Invariant: in LegacyOpen and LegacyDraining, with a legacy Live wallet and
// the drain unusable (contained, disabled, no eligible FROST target, or a
// custody budget below the main), the production client
// makes the legal legacy commitment to the Live target and completes the
// base movement: one P2WPKH output to it, mined by Bitcoin Core and accepted
// by the upgraded Bridge. It never commits FROST targets the Bridge would
// refuse. (In FrostOnly it never falls back: TestDrainProducer_NoRouteRecovery.)
// When the drain is admissible, the host must drain instead, in every mode
// (D16; TestDrainProducer_Drains).
//
// Spec: KC-SF-V3 line 448 ("ECDSA-to-ECDSA for pending legacy commitments";
// legacy routes stay legal before the cutover). Review 5655962724 (K09-31
// criterion: "in modes 0 and 1, with no eligible targets or under containment,
// the producer makes a legacy commitment. In mode 2 it never falls back").
//
// A main below dust is not a fallback state: base's WalletProposalValidator
// refuses every legacy movement below the dust threshold (TestDrainProducer_
// BelowDust covers that state).
//
// This passes on the integration branch, whose producer is legacy-only: it is
// a guard for the drain producer of #34. Its removal control is
// test/remediation-b2/controls/N-naive-drain.fix.patch.

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

var drainFallbackRoutes = []drainRoute{routeContained, routeDrainOff, routeZeroEligible, routeBudgetPartial}

// TestDrainProducer_LegacyFallbackBeforeCutover: see the file comment.
// Refs tuxxy/keep-core#47.
func TestDrainProducer_LegacyFallbackBeforeCutover(t *testing.T) {
	for _, mode := range []uint8{modeLegacyOpen, modeLegacyDraining} {
		for _, route := range drainFallbackRoutes {
			setup := drainSetup{mode: mode, live: true, route: route, encoding: revertCode3}
			t.Run(modeName(mode)+"-"+route.String(), func(t *testing.T) {
				drainFallbackRound(t, setup)
			})
		}
	}
}

func drainFallbackRound(t *testing.T, setup drainSetup) {
	dw := newDrainWorld(t, setup)
	proposal, err := dw.window("moving-funds proposal")
	commitments := dw.commitments()
	for _, list := range commitments {
		for _, target := range list {
			if dw.isFrostLabel(target) {
				t.Fatalf("legacy fallback (%s): the host committed FROST targets %s while the drain was unusable",
					setup, dw.labels(list))
			}
		}
	}
	if len(commitments) != 1 || len(commitments[0]) != 1 || commitments[0][0] != dw.target.pkh {
		t.Fatalf("legacy fallback (%s): before the cutover, with the drain unusable, the host made no legacy commitment "+
			"to the Live target %x (commitments %d, proposal %v, error %v)",
			setup, dw.target.pkh, len(commitments), proposal != nil, err)
	}
	if proposal == nil {
		t.Fatalf("legacy fallback (%s): the legacy commitment exists but the host made no proposal: %v", setup, err)
	}

	stop := dw.mineInBackground()
	operator := chain.Address(dw.operator.Hex())
	sign := tbtc.LegacySignBatchFn(func(_ context.Context, messages []*big.Int, _ uint64) ([]*tecdsa.Signature, error) {
		return legacySignatures(dw.source.key, messages)
	})
	start, err := dw.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	executeErr := legacyBounded(t, "legacy fallback execution", func() error {
		return tbtc.ExecuteMovingFundsActionForTest(dw.client, dw.btc, &dw.source.key.PublicKey,
			[]chain.Address{operator}, sign, proposal, start, dw.waitForBlock)
	})
	stop()
	if executeErr != nil {
		t.Fatalf("legacy fallback (%s): the legacy moving-funds action failed: %v", setup, executeErr)
	}
	broadcasts := dw.btc.Broadcasts()
	if len(broadcasts) != 1 {
		t.Fatalf("legacy fallback (%s): %d transactions broadcast, want 1", setup, len(broadcasts))
	}
	tx := broadcasts[0]
	script, err := bitcoin.PayToWitnessPublicKeyHash(dw.target.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Outputs) != 1 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, script) {
		t.Errorf("legacy fallback (%s): outputs %v, want one P2WPKH output to %x", setup, legacyOutputs(tx), dw.target.pkh)
	}
	if block := dw.btc.mine(t); !legacyBlockHas(block, tx.Hash()) {
		t.Fatalf("legacy fallback (%s): Bitcoin Core did not mine the movement", setup)
	}
	if err := dw.prove(tx); err != nil {
		t.Fatalf("legacy fallback (%s): the upgraded Bridge refuses the proof of the legacy movement: %v", setup, err)
	}
}
