package tbtc_test

// Adversarial review, area dispatch (2026-11-04): rule 5 / D14 on the legacy
// moved-funds-sweep proposal task.
//
// Invariant: with FROST not configured, against the pre-FROST Bridge
// (40a11d1d), the client calls no contract function the pre-FROST system
// lacks, in every legacy duty. Base keep-core holds a wallet's signer from
// key generation on (dkgExecutor.registerSigner runs before the DKG result is
// published), so the coordination layer runs the legacy proposal tasks for a
// wallet the Bridge does not know yet (during the challenge period) or never
// will (a challenged result). For such a wallet MovedFundsSweepTask.Run must
// fail as base fails (GetWallet: no wallet) without probing the Bridge for a
// FROST wallet.

import (
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

func TestAdvLegacy_MovedFundsSweepUnregisteredWallet(t *testing.T) {
	w := newLegacyWorld(t)
	for _, encoding := range legacyEncodings {
		t.Run(encoding.String(), func(t *testing.T) {
			recorder := newLegacyRecorder(t, w.anvilURL, encoding)
			tbtcChain, err := w.connect(recorder)
			if err != nil {
				t.Fatalf("legacy startup: %v", err)
			}

			// A wallet whose key the node holds but the Bridge has not
			// registered (fixture: any key hash the Bridge never saw).
			unregistered := newLegacyWallet(t, 0x77)
			operator := chain.Address(w.operator.Hex())
			task := tbtcpg.NewMovedFundsSweepTask(tbtcChain, w.btc)
			_, ok, err := task.Run(&tbtc.CoordinationProposalRequest{
				WalletPublicKeyHash: unregistered.pkh,
				WalletOperators:     []chain.Address{operator},
				ExecutingOperator:   operator,
				ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovedFundsSweep},
			})
			if ok {
				t.Fatalf("moved funds sweep: proposal for a wallet the Bridge does not know")
			}
			if err == nil || !strings.Contains(err.Error(), "no wallet for public key hash") {
				t.Errorf("moved funds sweep: error %v, want base's GetWallet error", err)
			}

			w.assertBaselineSurface(t, "moved funds sweep of an unregistered wallet", recorder.Calls())
		})
	}
}
