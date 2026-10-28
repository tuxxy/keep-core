package tbtc_test

// Remediation batch 1b, tuxxy/keep-core#73 fix review W2.
//
// Invariant (D4(c), rule 4 as D14 reads it provisionally): with FROST off,
// legacy moving funds against the pre-FROST Bridge completes with the base
// transaction and calls only base functions, whatever state the committed
// target wallet reached after the commitment. A legacy target is a wallet
// the Bridge registered, Live or not; it is never asked a FROST-only
// function.
//
// After the client's commitment (targets are Live then), the target moves
// through public 40a11d1d calls only:
//   MovingFunds  a deposit swept to it, then a heartbeat failure;
//   Closing      a heartbeat failure with no main UTXO;
//   Closed       Closing, then notifyWalletClosingPeriodElapsed;
//   Terminated   a fraud challenge it does not defeat in time.
// Governance shortens the closing period and the fraud defeat timeout to one
// second (test values); nothing else changes. One encoding (code 3, "0x"):
// the empty-revert cases are covered by legacy_unchanged_test.go, and a
// FROST-only selector is caught by the surface oracle under either.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

// Wallets.WalletState in 40a11d1d.
const (
	legacyStateMovingFunds uint8 = 2
	legacyStateClosing     uint8 = 3
	legacyStateClosed      uint8 = 4
	legacyStateTerminated  uint8 = 5
)

var legacyNonLiveTargets = []struct {
	name  string
	state uint8
	reach func(*legacyWorld)
}{
	{"MovingFunds", legacyStateMovingFunds, func(w *legacyWorld) {
		w.fundTarget()
		w.heartbeatFailed(w.target)
	}},
	{"Closing", legacyStateClosing, func(w *legacyWorld) {
		w.heartbeatFailed(w.target)
	}},
	{"Closed", legacyStateClosed, func(w *legacyWorld) {
		w.heartbeatFailed(w.target)
		parameters, err := w.legacy.WalletParameters(&bind.CallOpts{})
		if err != nil {
			w.t.Fatal(err)
		}
		w.send("closing period", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.UpdateWalletParameters(o, parameters.WalletCreationPeriod,
				parameters.WalletCreationMinBtcBalance, parameters.WalletCreationMaxBtcBalance,
				parameters.WalletClosureMinBtcBalance, parameters.WalletMaxAge,
				parameters.WalletMaxBtcTransfer, 1)
		})
		w.elapse(2)
		w.send("closing period elapsed", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.NotifyWalletClosingPeriodElapsed(o, w.target.pkh)
		})
	}},
	{"Terminated", legacyStateTerminated, func(w *legacyWorld) {
		w.defeatedFraudChallenge(w.target)
	}},
}

// TestLegacyUnchanged_NonLiveTargets: see the file comment.
// Refs tuxxy/keep-core#73.
func TestLegacyUnchanged_NonLiveTargets(t *testing.T) {
	for _, target := range legacyNonLiveTargets {
		target := target
		t.Run(target.name, func(t *testing.T) {
			legacyNonLiveTargetRound(t, target.state, target.reach)
		})
	}
}

func legacyNonLiveTargetRound(t *testing.T, state uint8, reach func(*legacyWorld)) {
	w := newLegacyWorld(t)
	recorder := newLegacyRecorder(t, w.anvilURL, revertCode3)
	tbtcChain, err := w.connect(recorder)
	if err != nil {
		t.Fatalf("legacy startup: %v", err)
	}
	stop := w.mineInBackground()
	defer stop()

	operator := chain.Address(w.operator.Hex())
	var proposal *tbtc.MovingFundsProposal
	err = legacyBounded(t, "moving-funds proposal", func() error {
		result, ok, err := tbtcpg.NewMovingFundsTask(tbtcChain, w.btc).Run(&tbtc.CoordinationProposalRequest{
			WalletPublicKeyHash: w.source.pkh,
			WalletOperators:     []chain.Address{operator},
			ExecutingOperator:   operator,
			ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
		})
		if err == nil && ok {
			proposal = result.(*tbtc.MovingFundsProposal)
		}
		return err
	})
	if err != nil || proposal == nil {
		t.Fatalf("legacy moving-funds proposal: %v (proposal %v)", err, proposal != nil)
	}
	if len(proposal.TargetWallets) != 1 || proposal.TargetWallets[0] != w.target.pkh {
		t.Fatalf("legacy moving-funds proposal: targets %x, want [%x]", proposal.TargetWallets, w.target.pkh)
	}

	// The committed target leaves Live through public calls.
	reach(w)
	target, err := w.legacy.Wallets(&bind.CallOpts{}, w.target.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != state {
		t.Fatalf("setup: target wallet state %d, want %d", target.State, state)
	}

	start, err := w.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sign := tbtc.LegacySignBatchFn(func(_ context.Context, messages []*big.Int, _ uint64) ([]*tecdsa.Signature, error) {
		return legacySignatures(w.source.key, messages)
	})
	executeErr := legacyBounded(t, "moving-funds execution", func() error {
		return tbtc.ExecuteMovingFundsActionForTest(tbtcChain, w.btc, &w.source.key.PublicKey,
			[]chain.Address{operator}, sign, proposal, start, w.waitForBlock)
	})
	if executeErr != nil {
		t.Errorf("legacy moving-funds execution to a target in state %d: %v", state, executeErr)
	}
	w.assertBaselineSurface(t, "moving funds to a non-Live target", recorder.Calls())
	if rejected := w.btc.Rejected(); len(rejected) > 0 {
		t.Errorf("legacy moving-funds transaction: Bitcoin Core refused a broadcast: %v", rejected)
	}
	if executeErr != nil {
		return
	}

	// The base transaction: the main UTXO to one P2WPKH output to the target.
	broadcasts := w.btc.Broadcasts()
	if len(broadcasts) != 1 {
		t.Fatalf("legacy moving-funds broadcast: %d transactions, want 1", len(broadcasts))
	}
	tx := broadcasts[0]
	script, err := bitcoin.PayToWitnessPublicKeyHash(w.target.pkh)
	if err != nil {
		t.Fatal(err)
	}
	want := w.main.Value - proposal.MovingFundsTxFee.Int64()
	if len(tx.Inputs) != 1 || *tx.Inputs[0].Outpoint != *w.main.Outpoint ||
		len(tx.Outputs) != 1 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, script) || tx.Outputs[0].Value != want {
		t.Errorf("legacy moving-funds transaction: outputs %v, want the main UTXO to one P2WPKH output of %d to %x",
			legacyOutputs(tx), want, w.target.pkh)
	}
	block := w.btc.mine(t)
	if !legacyBlockHas(block, tx.Hash()) {
		t.Errorf("legacy moving-funds transaction: Bitcoin Core regtest did not mine %s",
			tx.Hash().Hex(bitcoin.ReversedByteOrder))
	}
	// The pre-FROST Bridge's own verdict.
	stop()
	if _, err := w.legacy.SubmitMovingFundsProof(w.opts(), legacyTxInfo(tx), w.btc.proof(t, tx.Hash()),
		legacyUTXO(w.main), w.source.pkh); err != nil {
		t.Errorf("legacy moving-funds transaction: the pre-FROST Bridge refuses its proof: %v", err)
	}
}

func (w *legacyWorld) heartbeatFailed(wallet legacyWallet) {
	w.t.Helper()
	w.send("heartbeat failure", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "heartbeatFailed", wallet.id,
			common.BytesToHash(wallet.key.PublicKey.X.FillBytes(make([]byte, 32))),
			common.BytesToHash(wallet.key.PublicKey.Y.FillBytes(make([]byte, 32))))
	})
}

// elapse advances the chain clock by seconds and mines a block.
func (w *legacyWorld) elapse(seconds int64) {
	w.t.Helper()
	if err := w.admin.Client().Call(nil, "evm_increaseTime", seconds); err != nil {
		w.t.Fatal(err)
	}
	if err := w.admin.Client().Call(nil, "evm_mine"); err != nil {
		w.t.Fatal(err)
	}
}

// defeatedFraudChallenge terminates the wallet publicly: a fraud challenge
// over a digest it signed, not defeated within the (shortened) timeout.
func (w *legacyWorld) defeatedFraudChallenge(wallet legacyWallet) {
	t := w.t
	t.Helper()
	parameters, err := w.legacy.FraudParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("fraud defeat timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateFraudParameters(o, parameters.FraudChallengeDepositAmount, 1,
			parameters.FraudSlashingAmount, parameters.FraudNotifierRewardMultiplier)
	})
	preimage := sha256.Sum256([]byte("keep-core#73 non-live target: undefended digest"))
	sighash := sha256.Sum256(preimage[:])
	key, err := crypto.ToECDSA(wallet.key.D.FillBytes(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(sighash[:], key)
	if err != nil {
		t.Fatal(err)
	}
	var rsv tbtcabi.BitcoinTxRSVSignature
	copy(rsv.R[:], signature[:32])
	copy(rsv.S[:], signature[32:64])
	rsv.V = signature[64] + 27
	publicKey := append(wallet.key.PublicKey.X.FillBytes(make([]byte, 32)), wallet.key.PublicKey.Y.FillBytes(make([]byte, 32))...)
	w.send("fraud challenge", func(o *bind.TransactOpts) (*types.Transaction, error) {
		o.Value = parameters.FraudChallengeDepositAmount
		return w.legacy.SubmitFraudChallenge(o, publicKey, preimage[:], rsv)
	})
	w.elapse(2)
	w.send("fraud challenge defeat timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.NotifyFraudChallengeDefeatTimeout(o, publicKey, []uint32{1}, preimage[:])
	})
}

// fundTarget reveals a deposit to the target wallet and proves its sweep,
// signed with the target's key, so the Bridge records a main UTXO for it
// (fundSource, for the target).
func (w *legacyWorld) fundTarget() {
	t := w.t
	t.Helper()
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	deposit := &tbtc.Deposit{
		Depositor:           chain.Address(admin.Hex()),
		BlindingFactor:      [8]byte{0x7a, 0x12},
		WalletPublicKeyHash: w.target.pkh,
		RefundPublicKeyHash: [20]byte{9},
	}
	binary.LittleEndian.PutUint32(deposit.RefundLocktime[:], uint32(header.Time+30*24*3600))
	script, err := deposit.Script()
	if err != nil {
		t.Fatal(err)
	}
	locking, err := bitcoin.PayToWitnessScriptHash(bitcoin.WitnessScriptHash(script))
	if err != nil {
		t.Fatal(err)
	}
	scriptHash := sha256.Sum256(script)
	address, err := btcutil.NewAddressWitnessScriptHash(scriptHash[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	funding := w.btc.pay(t, address.EncodeAddress(), legacyDepositValue)
	fundingIndex := -1
	for i, output := range funding.Outputs {
		if bytes.Equal(output.PublicKeyScript, locking) && output.Value == legacyDepositValue {
			fundingIndex = i
		}
	}
	if fundingIndex < 0 {
		t.Fatal("Core did not create the target's deposit output")
	}
	w.send("reveal target deposit", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.RevealDeposit(o, legacyTxInfo(funding), tbtcabi.DepositDepositRevealInfo{
			FundingOutputIndex: uint32(fundingIndex),
			BlindingFactor:     deposit.BlindingFactor,
			WalletPubKeyHash:   deposit.WalletPublicKeyHash,
			RefundPubKeyHash:   deposit.RefundPublicKeyHash,
			RefundLocktime:     deposit.RefundLocktime,
		})
	})
	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddScriptHashInput(&bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: uint32(fundingIndex)},
		Value:    legacyDepositValue,
	}, script); err != nil {
		t.Fatal(err)
	}
	walletScript, err := bitcoin.PayToWitnessPublicKeyHash(w.target.pkh)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: legacyDepositValue - legacySweepFee, PublicKeyScript: walletScript})
	sweep := legacySign(t, builder, w.target.key)
	w.btc.mine(t, sweep)
	w.send("target sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitDepositSweepProof(o, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()), tbtcabi.BitcoinTxUTXO{}, common.Address{})
	})
}
