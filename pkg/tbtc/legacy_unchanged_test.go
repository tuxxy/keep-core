package tbtc_test

// Legacy-unchanged suite (remediation batch 1, shared).
//
// Invariant: with FROST not configured, a client against the pre-FROST Bridge
// built from tbtc-v2 main 40a11d1d behaves as at base. Startup succeeds, the
// moving-funds proposal and its validation succeed, and the wallet signs and
// broadcasts the base moving-funds transaction (one P2WPKH output per legacy
// target). The client calls no contract function the pre-FROST system lacks,
// measures the legacy safety margin on its own clock, and recovers from a
// transient endpoint error. All of this holds under both empty-revert
// encodings: code 3 with data "0x" (Anvil) and -32000 without data (geth).
//
// Spec: KC-SF-V3 line 362, SC-01; owner decision D4(c); SHARED-RULES rule 4.
// Refs tuxxy/keep-core#73 (K-07: FrostTargetOutputKey always calls
// WalletScheme), tuxxy/tbtc-v2#28 and tuxxy/tbtc-v2#31 (baseline built from git,
// no /private/tmp defaults).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

var legacyEncodings = []legacyRevertEncoding{revertCode3, revertGeth}

// legacyTransientAttempts bounds the retries in the transient cases: every
// distinct contract read fails once, so each attempt gets at least one read
// further than the previous one.
const legacyTransientAttempts = 40

// TestLegacyUnchanged_EmptyRevertEncodings checks the precondition that makes
// the two encodings distinct cases: through the recorder, a call to a
// function the pre-FROST Bridge lacks reverts without data, encoded as each
// mode claims. It fails if the ADAPTER does not do what the other tests rely
// on.
func TestLegacyUnchanged_EmptyRevertEncodings(t *testing.T) {
	w := newLegacyWorld(t)
	unknown := crypto.Keccak256([]byte("walletScheme(bytes20)"))[:4]
	for _, encoding := range legacyEncodings {
		t.Run(encoding.String(), func(t *testing.T) {
			recorder := newLegacyRecorder(t, w.anvilURL, encoding)
			client, err := rpc.Dial(recorder.URL())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			var result string
			callErr := client.Call(&result, "eth_call", map[string]string{
				"to":   w.bridge.Hex(),
				"data": "0x" + common.Bytes2Hex(append(unknown, make([]byte, 32)...)),
			}, "latest")
			code, data, hasData := rpcErrorParts(callErr)
			switch encoding {
			case revertCode3:
				if code != 3 || !hasData || data != "0x" {
					t.Fatalf("empty revert encoding: got code %d data %q (present %v), want 3 \"0x\"", code, data, hasData)
				}
			case revertGeth:
				if code != -32000 || hasData {
					t.Fatalf("empty revert encoding: got code %d data %q (present %v), want -32000 without data", code, data, hasData)
				}
			}
		})
	}
}

// TestLegacyUnchanged_Startup: the production Connect path with a default,
// FROST-unconfigured configuration starts against the pre-FROST Bridge,
// calls nothing outside the baseline surface, and transient endpoint errors
// (each distinct read failing once) do not prevent a later attempt (K09-53).
func TestLegacyUnchanged_Startup(t *testing.T) {
	w := newLegacyWorld(t)
	for _, encoding := range legacyEncodings {
		t.Run(encoding.String(), func(t *testing.T) {
			recorder := newLegacyRecorder(t, w.anvilURL, encoding)
			if _, err := w.connect(recorder); err != nil {
				t.Fatalf("legacy startup: %v", err)
			}
			w.assertBaselineSurface(t, "startup", recorder.Calls())
		})
		t.Run(encoding.String()+"+transient-then-success", func(t *testing.T) {
			recorder := newLegacyRecorder(t, w.anvilURL, encoding)
			recorder.FailEachReadOnce()
			var err error
			for attempt := 1; attempt <= legacyTransientAttempts; attempt++ {
				if _, err = w.connect(recorder); err == nil {
					break
				}
			}
			if err != nil {
				t.Fatalf("legacy startup after transient errors: %v", err)
			}
			w.assertBaselineSurface(t, "startup", recorder.Calls())
		})
	}
}

// TestLegacyUnchanged_MovingFunds drives one legacy moving-funds round end to
// end through production code: the coordinator's proposal task (it submits
// the commitment itself, waits for it and builds the proposal), then the
// wallet's moving-funds action (validation, commitment confirmation,
// assembly, signing, broadcast). Each case is a fresh chain.
func TestLegacyUnchanged_MovingFunds(t *testing.T) {
	for _, encoding := range legacyEncodings {
		for _, transient := range []bool{false, true} {
			name := encoding.String()
			if transient {
				name += "+transient-then-success"
			}
			t.Run(name, func(t *testing.T) {
				legacyMovingFundsRound(t, encoding, transient)
			})
		}
	}
}

func legacyMovingFundsRound(t *testing.T, encoding legacyRevertEncoding, transient bool) {
	w := newLegacyWorld(t)
	recorder := newLegacyRecorder(t, w.anvilURL, encoding)

	// Precondition for the clock half: the chain clock is far behind the
	// wall clock, so the 24-hour safety margin is over on the wall clock
	// only.
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := w.legacy.Wallets(nil, w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if header.Time-uint64(wallet.MovingFundsRequestedAt) > 3600 {
		t.Fatalf("setup: chain clock advanced %ds past MovingFunds", header.Time-uint64(wallet.MovingFundsRequestedAt))
	}

	tbtcChain, err := w.connect(recorder)
	if err != nil {
		t.Fatalf("legacy startup: %v", err)
	}
	stop := w.mineInBackground()
	defer stop()

	operator := chain.Address(w.operator.Hex())
	request := &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.source.pkh,
		WalletOperators:     []chain.Address{operator},
		ExecutingOperator:   operator,
		ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
	}
	task := tbtcpg.NewMovingFundsTask(tbtcChain, w.btc)

	attempts := 1
	if transient {
		attempts = legacyTransientAttempts
		recorder.FailEachReadOnce()
	}

	var proposal *tbtc.MovingFundsProposal
	var proposeErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		var result tbtc.CoordinationProposal
		var ok bool
		err := legacyBounded(t, "moving-funds proposal", func() error {
			var err error
			result, ok, err = task.Run(request)
			return err
		})
		proposeErr = err
		if err == nil && !ok {
			proposeErr = fmt.Errorf("the task made no proposal (the wallet was judged not ready to move funds)")
		}
		if err == nil && ok {
			proposal = result.(*tbtc.MovingFundsProposal)
			break
		}
	}
	if proposeErr != nil {
		t.Fatalf("legacy moving-funds proposal: %v", proposeErr)
	}
	if len(proposal.TargetWallets) != 1 || proposal.TargetWallets[0] != w.target.pkh {
		t.Fatalf("legacy moving-funds proposal: targets %x, want [%x]", proposal.TargetWallets, w.target.pkh)
	}

	sign := tbtc.LegacySignBatchFn(func(_ context.Context, messages []*big.Int, _ uint64) ([]*tecdsa.Signature, error) {
		return legacySignatures(w.source.key, messages)
	})
	var executeErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		start, err := w.admin.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		executeErr = legacyBounded(t, "moving-funds execution", func() error {
			return tbtc.ExecuteMovingFundsActionForTest(
				tbtcChain, w.btc, &w.source.key.PublicKey,
				[]chain.Address{operator}, sign, proposal, start, w.waitForBlock,
			)
		})
		if executeErr == nil {
			break
		}
	}
	if executeErr != nil {
		t.Errorf("legacy moving-funds execution: %v", executeErr)
	}

	w.assertBaselineSurface(t, "moving funds", recorder.Calls())
	if encoding == revertGeth && recorder.Rewritten() > 0 {
		t.Logf("geth encoding: %d empty reverts were served as -32000 without data", recorder.Rewritten())
	}
	if executeErr != nil {
		return
	}

	// The base transaction: the main UTXO to one P2WPKH output per target.
	broadcasts := w.btc.Broadcasts()
	if len(broadcasts) != 1 {
		t.Fatalf("legacy moving-funds broadcast: %d transactions, want 1", len(broadcasts))
	}
	tx := broadcasts[0]
	if len(tx.Inputs) != 1 || *tx.Inputs[0].Outpoint != *w.main.Outpoint {
		t.Errorf("legacy moving-funds transaction: inputs do not spend exactly the main UTXO")
	}
	script, err := bitcoin.PayToWitnessPublicKeyHash(w.target.pkh)
	if err != nil {
		t.Fatal(err)
	}
	want := w.main.Value - proposal.MovingFundsTxFee.Int64()
	if len(tx.Outputs) != 1 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, script) || tx.Outputs[0].Value != want {
		t.Errorf("legacy moving-funds transaction: outputs %v, want one P2WPKH output of %d to %x",
			legacyOutputs(tx), want, w.target.pkh)
	}
}

func legacyOutputs(tx *bitcoin.Transaction) []string {
	outputs := make([]string, len(tx.Outputs))
	for i, output := range tx.Outputs {
		outputs[i] = fmt.Sprintf("%d:%x", output.Value, output.PublicKeyScript)
	}
	return outputs
}

// legacyBounded runs one client step with a hard bound, so a hang fails the
// test at a named point instead of at the binary timeout.
func legacyBounded(t *testing.T, what string, fn func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- fn() }()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Minute):
		t.Fatalf("%s did not finish within 3 minutes", what)
		return nil
	}
}

// rpcErrorParts splits a JSON-RPC error into its code and data.
func rpcErrorParts(err error) (int, string, bool) {
	var rpcError rpc.Error
	if !errors.As(err, &rpcError) {
		return 0, "", false
	}
	var dataError rpc.DataError
	if !errors.As(err, &dataError) || dataError.ErrorData() == nil {
		return rpcError.ErrorCode(), "", false
	}
	return rpcError.ErrorCode(), fmt.Sprint(dataError.ErrorData()), true
}
