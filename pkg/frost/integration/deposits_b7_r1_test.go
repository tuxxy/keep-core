package integration

// Batch-7 regression test for review R1 (REVIEW-FIX-B7.md), from the
// reviewer's probe: a revealed deposit whose funding transaction never
// reaches Bitcoin must not stop the wallet's other deposits from being
// swept. C-05 reveal takes the funding bytes without an SPV proof, so anyone
// can make such a reveal for the price of gas.

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

func TestUnbroadcastRevealDoesNotBlockSweeps(t *testing.T) {
	w := newK06WorldWith(t, k06Options{mode: "k05", beforeDKG: certifyAllSeats})
	ctx := w.f.ctx
	for _, s := range w.sweepers {
		s.Planner.Clock = w.clock
	}
	locktime := w.locktime(k06RefundDelay)
	a := w.deposit(1_000_000, locktime, [20]byte{})

	// Attacker: a funding transaction built and signed, never broadcast.
	terms := w.terms(1_000_000, locktime, [20]byte{})
	c, err := deposit.Build(terms, w.wallet.OutputKey)
	if err != nil {
		t.Fatal(err)
	}
	address, err := btcutil.NewAddressTaproot(c.OutputKey[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := w.core.call(ctx, "createrawtransaction", []any{[]any{}, map[string]any{address.EncodeAddress(): json.Number("0.01000000")}}, &raw); err != nil {
		t.Fatal(err)
	}
	var funded struct{ Hex string }
	if err := w.core.call(ctx, "fundrawtransaction", []any{raw}, &funded); err != nil {
		t.Fatal(err)
	}
	var signed struct{ Hex string }
	if err := w.core.call(ctx, "signrawtransactionwithwallet", []any{funded.Hex}, &signed); err != nil {
		t.Fatal(err)
	}
	bytesTx, _ := hex.DecodeString(signed.Hex)
	tx := new(bitcoin.Transaction)
	if err := tx.Deserialize(bytesTx); err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, o := range tx.Outputs {
		if string(o.PublicKeyScript) == string(c.Script()) {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("setup: no deposit output")
	}
	info := spv.TransactionInfo(tx)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.RevealFrostDeposit(o, abiTerms(terms), frostbridgeabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, uint32(index))
	})

	r := w.round(a.readyBlock + k06Finality)
	for i, s := range w.sweepers {
		plan, err := s.Plan(ctx, w.fromBlock, r)
		if err != nil {
			t.Fatalf("RV-B7-R1: operator %d cannot plan the sweep of honest deposit A after an attacker revealed an unbroadcast funding (%v): %v", i, errors.Is(err, deposit.ErrWalletNotSynced), err)
		}
		if len(plan.SweepDeposits()) != 1 || plan.SweepDeposits()[0].Key != a.key {
			t.Fatal(fmt.Sprintf("operator %d planned %d deposits", i, len(plan.SweepDeposits())))
		}
	}
}
