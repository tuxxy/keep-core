package tbtc_test

// K-09 lifecycle demonstration (KC-SF-V3 §6 K-09, SC-01..SC-10): the FROST
// lifecycle driven through real node processes on the merged integration
// branch, one stage at a time. World and snapshots: k09_world_test.go.
//
// Main chain (each stage starts from its predecessor's snapshot and leaves
// its own):
//
//	01-legacy -> 02-upgrade -> 03-dkg -> 04-drain -> 05-redemption -> 06-restart
//
// Branch stages start from a main-chain snapshot and leave none:
//
//	04-drain   -> 07-reservation-timeout
//	06-restart -> 08-deposit-refund, 09-movement, 10-inactivity, 11-cutover-closure,
//	              12-no-inactivity
//
// Every stage asserts its outcome from chain state (Bridge, registry) and
// Bitcoin Core, never from node logs. A stage that fails because production
// behaviour is missing or wrong is a finding (K09-RUN.md); its assertion says
// "FINDING". Run through test/k09/run.sh, which records every stage.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// k09Stage is one lifecycle stage.
type k09Stage struct {
	name string
	// after is the snapshot the stage starts from ("" builds the world).
	after string
	// save: the stage leaves a snapshot for its successors.
	save bool
	run  func(t *testing.T, w *k09World)
}

var k09Stages = []k09Stage{
	{"01-legacy", "", true, k09Legacy},
	{"02-upgrade", "01-legacy", true, k09Upgrade},
	{"03-dkg", "02-upgrade", true, k09DKG},
	{"04-drain", "03-dkg", true, k09Drain},
	{"05-redemption", "04-drain", true, k09Redemption},
	{"06-restart", "05-redemption", true, k09Restart},
	{"07-reservation-timeout", "04-drain", false, k09ReservationTimeout},
	{"08-deposit-refund", "06-restart", false, k09DepositRefund},
	{"09-movement", "06-restart", false, k09Movement},
	{"10-inactivity", "06-restart", false, k09Inactivity},
	{"11-cutover-closure", "06-restart", false, k09CutoverClosure},
	{"12-no-inactivity", "06-restart", false, k09NoInactivity},
}

// TestK09Lifecycle runs the stages in order. Each subtest's elapsed time
// includes loading its predecessor's snapshot and saving its own; the stage
// body alone is logged as "K09TIME stage=... body=...".
func TestK09Lifecycle(t *testing.T) {
	if frostNodeStart == nil {
		t.Fatal("K-09: frostNodeStart is unbound: no base node initialisation with FROST configured")
	}
	if os.Getenv("K09_RUN_ID") == "" {
		// This process's own snapshots go with it.
		t.Cleanup(func() {
			matches, _ := filepath.Glob(filepath.Join(b6CacheRoot(t), k09StageDir("*")))
			for _, m := range matches {
				_ = os.RemoveAll(m)
			}
		})
	}
	for _, stage := range k09Stages {
		stage := stage
		t.Run(stage.name, func(t *testing.T) {
			began := time.Now()
			var w *k09World
			if stage.after == "" {
				w = k09BuildLegacy(t)
			} else {
				w = k09Load(t, stage.after)
			}
			loaded := time.Now()
			t.Logf("K09TIME stage=%s load=%.2f", stage.name, loaded.Sub(began).Seconds())
			stage.run(t, w)
			t.Logf("K09TIME stage=%s body=%.2f", stage.name, time.Since(loaded).Seconds())
			if stage.save {
				w.save(stage.name, 2*time.Second)
			}
		})
	}
}

// --- 01: the legacy ECDSA world on the 40a11d1d Bridge (SC-01) ---------------

// k09Legacy: the pre-FROST Bridge (D1) holds a legacy obligation: a
// MovingFunds source wallet with a proven main, which it owes to a target.
// The Bridge has no FROST surface.
func k09Legacy(t *testing.T, w *k09World) {
	source, err := w.legacy.Wallets(&bind.CallOpts{}, w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if source.State != 2 || source.MainUtxoHash == ([32]byte{}) {
		t.Fatalf("K-09 01-legacy: the legacy source is in state %d with main hash %x, want MovingFunds with a main", source.State, source.MainUtxoHash)
	}
	w.bind()
	if _, err := w.bridge.FrostCreationMode(&bind.CallOpts{}); err == nil {
		t.Fatal("K-09 01-legacy: the 40a11d1d Bridge answered frostCreationMode: it is not the pre-FROST Bridge")
	}
	w.facts.SourceState = w.legacyState()
}

// --- 02: upgrade in place (SC-01, D1, D4(c)) ---------------------------------

// k09Upgrade: governance upgrades the same proxy to the integration Bridge and
// configures the FROST extension (5 seats, threshold 3). The legacy wallets
// and the depositor's balance are unchanged; creation stays LegacyOpen.
func k09Upgrade(t *testing.T, w *k09World) {
	before := w.legacyState()
	if before != w.facts.SourceState {
		t.Fatalf("setup: the 01-legacy snapshot does not hold its recorded legacy state:\n%s\n%s", before, w.facts.SourceState)
	}
	old := w.implementation()
	upgraded := w.upgradeBridge()
	if got := w.implementation(); got != upgraded || got == old {
		t.Fatalf("K-09 02-upgrade: the proxy's implementation is %s after the upgrade (old %s, new %s)", got, old, upgraded)
	}
	w.setUpFrost()
	if after := w.legacyState(); after != before {
		t.Fatalf("K-09 02-upgrade (SC-01): the upgrade changed the legacy state:\nbefore %s\nafter  %s", before, after)
	}
	if mode := w.creationMode(); mode != 0 {
		t.Fatalf("K-09 02-upgrade: creation mode %d after the upgrade, want LegacyOpen", mode)
	}
	registry, err := w.registry.Epoch(&bind.CallOpts{})
	if err != nil || registry != 0 {
		t.Fatalf("K-09 02-upgrade: the FROST registry is not bound fresh (epoch %d, %v)", registry, err)
	}
}

// --- 03: FROST DKG and readiness by the nodes, multi-seat (SC-02, SC-03) -----

// k09DKG: governance requests a FROST wallet; the nodes alone take it to
// ReadyUnfunded. Operators 1 and 2 hold two seats each, operator 3 one; no
// operator holds the threshold.
func k09DKG(t *testing.T, w *k09World) {
	w.startNodes()
	epoch := w.requestWallet(k09Pattern)
	wallet, ok := w.readyWallet(epoch, k09DKGBound)
	if !ok {
		t.Fatalf("K-09 03-dkg: epoch %d's 5-seat FROST wallet did not reach ReadyUnfunded within %v (exited: %v)%s", epoch, k09DKGBound, w.exitedNodes(), w.debugLogs())
	}
	seats := map[common.Address]int{}
	for _, operator := range wallet.operators {
		seats[operator]++
	}
	for i, want := range map[int]int{1: 2, 2: 2, 3: 1} {
		address := crypto.PubkeyToAddress(w.keys[i-1].PublicKey)
		if seats[address] != want {
			t.Fatalf("K-09 03-dkg: operator %d holds %d of the wallet's seats %v, want %d", i, seats[address], wallet.operators, want)
		}
	}
	for operator, n := range seats {
		if n >= k09Threshold {
			t.Fatalf("K-09 03-dkg: operator %s holds %d seats, at least the threshold %d", operator, n, k09Threshold)
		}
	}
	if after := w.legacyState(); after != w.facts.SourceState {
		t.Fatalf("K-09 03-dkg (SC-01): the legacy state changed during the FROST DKG:\n%s\n%s", w.facts.SourceState, after)
	}
	w.wallet, w.stageEpoch, w.facts.Epoch = wallet, epoch, epoch
}

// --- 04: ECDSA-to-FROST drain and incoming sweep (SC-01, SC-06) -------------

// k09Drain: the legacy source owes its main; it commits the drain to the
// node-made FROST wallet and moves it (FIXTURE: the source's test key signs;
// legacy tECDSA signers are not in the nodes), the SPV maintainer proves it,
// and the source closes. The nodes then sweep the pending incoming output in
// a base coordination window, and the maintainer proves the sweep: the
// sweep's output is the wallet's main.
func k09Drain(t *testing.T, w *k09World) {
	window := w.chooseWindow(4*tbtc.B6CoordinationFrequencyBlocks, nil)
	drain := w.drainInto(w.wallet)
	source, err := w.legacy.Wallets(&bind.CallOpts{}, w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if source.State != 3 {
		t.Fatalf("K-09 04-drain: after the proven drain the legacy source is in state %d, want Closing", source.State)
	}
	w.startNodes()
	w.enterWindow(window)
	sweep, ok := w.settleIncoming(w.wallet, drain, k09Action)
	if !ok {
		t.Fatalf("K-09 04-drain: no node swept the FROST wallet's pending incoming output in coordination window %d (exited: %v)%s", window, w.exitedNodes(), w.debugLogs())
	}
	tx, err := w.nodeTransaction(sweep)
	if err != nil {
		t.Fatal(err)
	}
	w.assertMain("04-drain", w.wallet, tx, 0)
	if spenders := w.spenders(bitcoin.TransactionOutpoint{TransactionHash: drain.Hash()}); len(spenders) != 1 {
		t.Fatalf("K-09 04-drain: %d distinct node transactions spend the incoming output: %v", len(spenders), spenders)
	}
}

// --- 05: redemption (SC-05, SC-06) --------------------------------------------

// k09Redemption: the redeemer requests a FROST redemption; in a base window
// the nodes reserve it on the Bridge and pay it; the maintainer proves the
// payment. Core holds the payment to the redeemer's script; the Bridge's main
// is the payment's change.
func k09Redemption(t *testing.T, w *k09World) {
	main := k09ParseOutpoint(t, w.facts.MainA)
	redeemer, err := bitcoin.PayToWitnessPublicKeyHash([20]byte{0x09, 0x05})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := w.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	gross := 2 * parameters.RedemptionDustThreshold
	w.startNodes()
	request := w.requestRedemption(w.wallet, redeemer, gross)
	w.nextWindow()
	reserved, ok := w.reservationAdmitted(w.wallet, request, k09Action)
	if !ok {
		t.Fatalf("K-09 05-redemption: the nodes did not reserve the pending FROST redemption within a coordination window (exited: %v)%s", w.exitedNodes(), w.debugLogs())
	}
	payment := w.awaitSpenders(main, k09Action)
	if len(payment) != 1 {
		t.Fatalf("K-09 05-redemption: %d distinct node transactions spend the reserved main %s: %v", len(payment), w.facts.MainA, payment)
	}
	tx, err := w.nodeTransaction(payment[0])
	if err != nil {
		t.Fatalf("K-09 05-redemption: Core does not hold the nodes' payment %s: %v", payment[0], err)
	}
	w.mineCore(tx)
	w.send("redemption proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostRedemptionProof", reserved, legacyTxInfo(tx), w.btc.proof(t, tx.Hash()))
	})
	record, err := w.bridge.FrostRedemptionRequest(&bind.CallOpts{}, request)
	if err != nil {
		t.Fatal(err)
	}
	if record.ReservationId != reserved {
		t.Fatalf("K-09 05-redemption: request %x names reservation %x, want %x", request, record.ReservationId, reserved)
	}
	paid, change := -1, -1
	walletScript, err := bitcoin.PayToTaproot(w.wallet.q)
	if err != nil {
		t.Fatal(err)
	}
	for i, output := range tx.Outputs {
		switch {
		case bytes.Equal(output.PublicKeyScript, redeemer):
			paid = i
		case bytes.Equal(output.PublicKeyScript, walletScript):
			change = i
		}
	}
	if paid < 0 || uint64(tx.Outputs[paid].Value) < gross-record.TreasuryFee-record.MaximumBitcoinFee {
		t.Fatalf("K-09 05-redemption: the confirmed payment %s does not pay the redeemer at least %d sat", payment[0], gross-record.TreasuryFee-record.MaximumBitcoinFee)
	}
	if change < 0 {
		t.Fatalf("K-09 05-redemption: the payment %s has no change output to the wallet", payment[0])
	}
	w.assertMain("05-redemption", w.wallet, tx, uint32(change))
	if w.custody(w.wallet).ActiveReservation != ([32]byte{}) {
		t.Fatalf("K-09 05-redemption: the wallet still holds an active reservation after the proven payment")
	}
	// Still one spender at the stage's end (review N2).
	if spenders := w.spenders(main); len(spenders) != 1 {
		t.Fatalf("K-09 05-redemption: %d distinct node transactions spend the reserved main by the stage's end: %v", len(spenders), spenders)
	}
}

// --- 06: restart and restore at startup, mid-lifecycle (SC-08) ---------------

// k09Restart: a second legacy source drains into the wallet; the nodes start,
// and every node process is killed (SIGKILL) right after startup, before any
// action (the crash matrix during actions is batch 4's), and restarted on its
// storage.
// The restarted nodes sweep the new incoming output with the restored keys
// (Core accepts only the wallet's signature) and no new DKG runs.
func k09Restart(t *testing.T, w *k09World) {
	epoch, err := w.registry.Epoch(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	began, err := w.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	source, main := w.secondSource()
	window := w.chooseWindow(4*tbtc.B6CoordinationFrequencyBlocks, nil)
	drain := w.drainFrom(source, main, w.wallet)
	w.startNodes()
	w.killAll()
	w.startNodes()
	w.enterWindow(window)
	sweep, ok := w.settleIncoming(w.wallet, drain, k09Action)
	if !ok {
		t.Fatalf("K-09 06-restart: after every node was killed and restarted, the wallet's incoming output was not swept in window %d: the FROST signers were not restored at startup (exited: %v)%s", window, w.exitedNodes(), w.debugLogs())
	}
	tx, err := w.nodeTransaction(sweep)
	if err != nil {
		t.Fatal(err)
	}
	w.assertMain("06-restart", w.wallet, tx, 0)
	if after, err := w.registry.Epoch(&bind.CallOpts{}); err != nil || after != epoch {
		t.Fatalf("K-09 06-restart: the registry epoch went from %d to %d (%v): a DKG ran again", epoch, after, err)
	}
	// No node reran a DKG after the restart (review N1): no DKG result was
	// submitted during the stage.
	it, err := w.registry.FilterResultSubmitted(&bind.FilterOpts{Start: began}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	if it.Next() {
		t.Fatalf("K-09 06-restart (SC-08): a DKG result for epoch %d was submitted after the restart: the nodes reran the DKG instead of restoring their keys", it.Event.Epoch)
	}
}

// --- 07: reservation timeout (SC-05, SC-06) -----------------------------------

// k09ReservationTimeout: the nodes reserve a redemption, then every node goes
// down before paying. Past the request's deadline a keeper notifies the
// timeout: the Bridge sanctions the wallet and moves its funds (KC-SF-V3
// C-09: a timed-out redemption request moves its wallet's funds). The
// timeout is the request's deadline (C-04), not a block count.
func k09ReservationTimeout(t *testing.T, w *k09World) {
	main := k09ParseOutpoint(t, w.facts.MainA)
	redeemer, err := bitcoin.PayToWitnessPublicKeyHash([20]byte{0x09, 0x07})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := w.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.startNodes()
	request := w.requestRedemption(w.wallet, redeemer, 2*parameters.RedemptionDustThreshold)
	w.nextWindow()
	if _, ok := w.reservationAdmitted(w.wallet, request, k09Action); !ok {
		t.Fatalf("K-09 07-reservation-timeout: the nodes did not reserve the redemption (exited: %v)%s", w.exitedNodes(), w.debugLogs())
	}
	w.stopMining()
	w.killAll()
	if spenders := w.spenders(main); len(spenders) != 0 {
		t.Fatalf("setup: the payment %v was broadcast before the nodes went down", spenders)
	}
	record, err := w.bridge.FrostRedemptionRequest(&bind.CallOpts{}, request)
	if err != nil {
		t.Fatal(err)
	}
	// Past the request's deadline a keeper notifies the timeout (the request
	// becomes TimeoutPending at this block), then the wallet's sanction.
	head, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.Deadline > head.Time {
		w.increaseTime(record.Deadline - head.Time + 1)
	}
	w.send("keeper: redemption timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.NotifyFrostRedemptionTimeout(o, request)
	})
	if record, err = w.bridge.FrostRedemptionRequest(&bind.CallOpts{}, request); err != nil || record.TimeoutBlock == 0 {
		t.Fatalf("K-09 07-reservation-timeout: the notified timeout recorded no timeout block (%v)", err)
	}
	w.send("keeper: wallet redemption timeout", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.NotifyFrostWalletRedemptionTimeout(o, request)
	})
	if n := w.walletEvents(w.wallet, k09MovingFundsEvent); n == 0 {
		t.Fatalf("K-09 07-reservation-timeout: the timed-out redemption did not move the wallet's funds (no FrostWalletMovingFunds)")
	}
	if w.lifecycle(w.wallet).MovingFundsRequestedAt == 0 {
		t.Fatalf("K-09 07-reservation-timeout: the wallet's lifecycle records no moving-funds request")
	}
}

// --- 08: deposit and sweep, independent refund (SC-04, SC-05) ---------------

// k09DepositRefund: a depositor builds a FROST deposit to the wallet (the Go
// template, checked against C-05's), pays it from Core and reveals it; the
// SPV maintainer reserves it (C-10). Two hours of chain time later (base's
// deposit age), in the next deposit-sweep window the nodes must sweep it with
// the wallet's main, and the maintainer proves the sweep: the Bridge's main
// is the sweep output and the depositor is credited once
// (assertDepositSweep). Then, with no operator involved, the depositor
// refunds an unswept deposit after its locktime from public C-05 data and the
// refund key; Core accepts it.
func k09DepositRefund(t *testing.T, w *k09World) {
	stage := "08-deposit-refund"
	params, err := w.legacy.DepositParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	revealAhead := uint64(params.DepositRevealAheadPeriod)
	refundKey, _ := btcec.PrivKeyFromBytes(common.LeftPadBytes([]byte{0x09, 0x08}, 32))
	registered, err := w.registry.Wallet(&bind.CallOpts{}, w.wallet.id)
	if err != nil {
		t.Fatal(err)
	}
	newDeposit := func(blinding byte, amount uint64) (deposit.Terms, deposit.Construction, bitcoin.TransactionOutpoint) {
		terms := deposit.Terms{
			ChainID: w.chainID, Bridge: w.legacyWorld.bridge, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash),
			WalletID: w.wallet.id, DescriptorHash: registered.DescriptorHash, Depositor: crypto.PubkeyToAddress(w.adminKey.PublicKey),
			Blinding: [8]byte{0x09, blinding}, Amount: amount, RefundLocktime: uint32(header.Time + revealAhead + 3600),
		}
		copy(terms.RefundKey[:], schnorr.SerializePubKey(refundKey.PubKey()))
		c, err := deposit.Build(terms, w.wallet.q)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := w.bridge.FrostDepositConstruction(&bind.CallOpts{}, k09Terms(terms))
		if err != nil || contract.Output.Key != c.OutputKey {
			t.Fatalf("K-09 %s: the Go deposit template differs from C-05's (%v)", stage, err)
		}
		address, err := btcutil.NewAddressTaproot(c.OutputKey[:], &chaincfg.RegressionNetParams)
		if err != nil {
			t.Fatal(err)
		}
		funding := w.btc.pay(t, address.EncodeAddress(), int64(amount))
		index := -1
		for i, output := range funding.Outputs {
			if bytes.Equal(output.PublicKeyScript, c.Script()) && output.Value == int64(amount) {
				index = i
			}
		}
		if index < 0 {
			t.Fatal("setup: Core did not create the deposit output")
		}
		// Base sweeps a deposit only with 6 funding confirmations
		// (DepositSweepRequiredFundingTxConfirmations).
		w.coreBlocks(6)
		w.send("depositor: reveal", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.bridge.RevealFrostDeposit(o, k09Terms(terms), k09TxInfo(funding), uint32(index))
		})
		outpoint := bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: uint32(index)}
		w.send("maintainer: reserve deposit", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.bridge.ReserveFrostDeposit(o, deposit.OutpointKey(outpoint), k09TxInfo(funding), k09Proof(w.btc.proof(t, funding.Hash())))
		})
		return terms, c, outpoint
	}
	amount := uint64(params.DepositDustThreshold) * 4

	// Sweep: the nodes' duty.
	window := w.chooseWindow(4*tbtc.B6CoordinationFrequencyBlocks, nil)
	terms, construction, funding := newDeposit(1, amount)
	before := w.custody(w.wallet)
	oldMain := k09ParseOutpoint(t, w.facts.MainA)
	depositor := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	balance := w.bankBalance(depositor)
	// Base sweeps a deposit only once it is DEPOSIT_MIN_AGE (2 hours) old
	// in chain time (WalletProposalValidator) and by the wall clock
	// (tbtcpg); the chain clock lags the wall clock (legacyChainLag), so
	// only chain time moves here (review B2).
	w.increaseTime(2*3600 + 1)
	w.startNodes()
	w.enterWindow(window)
	sweeps := w.awaitSpenders(funding, k09Action)
	sweepFailure := ""
	if len(sweeps) == 0 {
		sweepFailure = fmt.Sprintf("FINDING K-09 %s (SC-04): the nodes did not sweep the revealed, reserved FROST deposit %s in deposit-sweep window %d: no node spent the funding output (Core holds it unspent: %v)", stage, k09Outpoint(funding), window, w.coreUnspent(funding))
	} else {
		w.assertDepositSweep(stage, sweeps, funding, oldMain, before, balance, amount, uint64(params.DepositTreasuryFeeDivisor))
	}
	// Independent refund: the depositor alone, after the locktime. If the
	// nodes swept the first deposit, the refund is of a second one.
	if len(sweeps) > 0 {
		w.killAll()
		terms, construction, funding = newDeposit(2, amount)
	}
	contract, err := w.bridge.FrostDepositRefundConstruction(&bind.CallOpts{}, deposit.OutpointKey(funding))
	if err != nil {
		t.Fatal(err)
	}
	historical := deposit.Construction{OutputKey: contract.Output.Key, Root: contract.Output.Root, Parity: contract.Output.Parity, Receipt: contract.Receipt,
		WalletLeaf: contract.WalletLeaf, RefundLeaf: contract.RefundLeaf, WalletControl: contract.WalletControl, RefundControl: contract.RefundControl}
	if historical.OutputKey != construction.OutputKey {
		t.Fatalf("K-09 %s: C-05's refund construction differs from the deposit's", stage)
	}
	destination, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{0x09, 0x08})
	refund, err := deposit.Refund(terms, historical, funding, destination, 1_000, refundKey)
	if err != nil {
		t.Fatal(err)
	}
	// Bitcoin's median-time-past passes the locktime.
	w.coreAfter(int64(terms.RefundLocktime) + 7200)
	w.btc.mine(t, refund)
	if confirmations, err := w.coreConfirmations(refund.Hash().Hex(bitcoin.ReversedByteOrder)); err != nil || confirmations == 0 {
		t.Fatalf("K-09 %s (SC-05): Core did not confirm the depositor's refund %s: %v", stage, refund.Hash().Hex(bitcoin.ReversedByteOrder), err)
	}
	if sweepFailure != "" {
		t.Fatal(sweepFailure)
	}
}

// assertDepositSweep is the deposit sweep oracle (review B1): exactly one
// node transaction spends the deposit, together with the wallet's main
// (D13: custody stays in the main); the maintainer proves it on the Bridge;
// the Bridge's main is the sweep's wallet output; the depositor is credited
// exactly the deposit minus the treasury fee and the sweep fee; and the
// deposit's C-10 reservation is released.
func (w *k09World) assertDepositSweep(stage string, sweeps []string, funding, oldMain bitcoin.TransactionOutpoint, before frostbridgeabi.FrostCustodyWallet, balance *big.Int, amount, divisor uint64) {
	t := w.t
	t.Helper()
	if len(sweeps) != 1 {
		t.Fatalf("K-09 %s: %d distinct node transactions spend the deposit %s: %v", stage, len(sweeps), k09Outpoint(funding), sweeps)
	}
	tx, err := w.nodeTransaction(sweeps[0])
	if err != nil {
		t.Fatalf("K-09 %s: Core does not hold the nodes' sweep %s: %v", stage, sweeps[0], err)
	}
	spendsMain := false
	for _, input := range tx.Inputs {
		spendsMain = spendsMain || *input.Outpoint == oldMain
	}
	if !spendsMain {
		t.Fatalf("K-09 %s (D13): the deposit sweep %s does not spend the wallet's main %s", stage, sweeps[0], k09Outpoint(oldMain))
	}
	w.mineCore(tx)
	w.send("maintainer: deposit sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.SubmitFrostDepositSweepProof(o, frostbridgeabi.FrostDepositSweepSweep{
			WalletId: w.wallet.id, Transaction: k09TxInfo(tx), Proof: k09Proof(w.btc.proof(t, tx.Hash())), IncludeMain: true,
		})
	})
	walletScript, err := bitcoin.PayToTaproot(w.wallet.q)
	if err != nil {
		t.Fatal(err)
	}
	index, outputs := -1, int64(0)
	for i, output := range tx.Outputs {
		outputs += output.Value
		if bytes.Equal(output.PublicKeyScript, walletScript) {
			index = i
		}
	}
	if index < 0 || len(tx.Outputs) != 1 {
		t.Fatalf("K-09 %s: the deposit sweep %s has %d outputs and pays the wallet at index %d; want one output to the wallet", stage, sweeps[0], len(tx.Outputs), index)
	}
	w.assertMain(stage, w.wallet, tx, uint32(index))
	fee := int64(before.Value) + int64(amount) - outputs
	treasury := uint64(0)
	if divisor != 0 {
		treasury = amount / divisor
	}
	credited := new(big.Int).Sub(w.bankBalance(crypto.PubkeyToAddress(w.adminKey.PublicKey)), balance)
	if want := int64(amount) - int64(treasury) - fee; fee < 0 || credited.Cmp(big.NewInt(want)) != 0 {
		t.Fatalf("K-09 %s (SC-04): the depositor was credited %s, want deposit %d - treasury fee %d - sweep fee %d = %d", stage, credited, amount, treasury, fee, want)
	}
	if reserved, err := w.bridge.FrostDepositReserved(&bind.CallOpts{}, deposit.OutpointKey(funding)); err != nil || reserved {
		t.Fatalf("K-09 %s (C-10): the swept deposit is still reserved (%v)", stage, err)
	}
}

func k09Terms(t deposit.Terms) frostbridgeabi.FrostDepositProfileTerms {
	return frostbridgeabi.FrostDepositProfileTerms{ChainId: t.ChainID, Bridge: common.Address(t.Bridge), BitcoinGenesis: t.BitcoinGenesis, FullWalletId: t.WalletID,
		DescriptorHash: t.DescriptorHash, Depositor: common.Address(t.Depositor), Vault: common.Address(t.Vault), Blinding: t.Blinding, Amount: t.Amount,
		RefundKey: t.RefundKey, RefundLocktime: t.RefundLocktime, ExtraData: t.ExtraData}
}

// coreUnspent reports whether Core holds the outpoint unspent, mempool
// included.
func (w *k09World) coreUnspent(o bitcoin.TransactionOutpoint) bool {
	var out map[string]interface{}
	if err := w.btc.core.call("gettxout", []interface{}{o.TransactionHash.Hex(bitcoin.ReversedByteOrder), o.OutputIndex, true}, &out); err != nil {
		w.t.Fatal(err)
	}
	return out != nil
}

// coreBlocks mines n empty blocks on Core.
func (w *k09World) coreBlocks(n int) {
	var hashes []string
	if err := w.btc.core.call("generatetoaddress", []interface{}{n, w.btc.miner}, &hashes); err != nil {
		w.t.Fatal(err)
	}
	w.syncParent()
}

// coreAfter moves Core's clock to unix and mines 11 blocks, so median-time-
// past passes it.
func (w *k09World) coreAfter(unix int64) {
	t := w.t
	t.Helper()
	if err := w.btc.core.call("setmocktime", []interface{}{unix}, nil); err != nil {
		t.Fatal(err)
	}
	w.coreBlocks(11)
}

// --- 09: FROST-to-FROST movement (SC-06) -------------------------------------

// k09Movement: wallet A ages out; a keeper notifies it closeable and the
// Bridge moves its funds (MovingFunds). The nodes make wallet B. In the next
// moving-funds windows the nodes must commit A's targets
// (submitFrostMovingFundsCommitment: "any seat" of A), as base's
// MovingFundsTask commits a legacy wallet's, and sign and broadcast the
// movement; the maintainer proves it, A's custody moves out and B holds the
// pending incoming output (review B3).
func k09Movement(t *testing.T, w *k09World) {
	parameters, err := w.legacy.WalletParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.increaseTime(uint64(parameters.WalletMaxAge) + 1)
	w.send("keeper: A closeable", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.NotifyFrostWalletCloseable(o, w.wallet.id)
	})
	if w.walletEvents(w.wallet, k09MovingFundsEvent) == 0 {
		t.Fatal("K-09 09-movement: the aged, funded wallet A did not enter MovingFunds")
	}
	w.startNodes()
	epoch := w.requestWallet(k09Pattern)
	b, ok := w.readyWallet(epoch, k09DKGBound)
	if !ok {
		t.Fatalf("K-09 09-movement: the target wallet B (epoch %d) did not reach ReadyUnfunded (exited: %v)", epoch, w.exitedNodes())
	}
	committed := uint64(0)
	for i := 0; i < 2 && committed == 0; i++ {
		window := w.nextSweepWindow()
		if !w.waitBlock(window+tbtc.B6CoordinationActivePhaseBlocks+40, k09Action) {
			t.Fatal("setup: the chain did not advance")
		}
		time.Sleep(2 * time.Second)
		if w.movementCommitments(w.wallet) > 0 {
			committed = window
		}
	}
	if committed == 0 {
		t.Fatalf("FINDING K-09 09-movement (SC-06): wallet A has been in MovingFunds for two moving-funds coordination windows with the eligible FROST target B (%x) ready, and no node committed its targets: the Bridge emitted no FrostMovingFundsCommitted for A. The node dispatches no FROST-to-FROST movement (exited: %v)", b.label, w.exitedNodes())
	}
	// The movement itself (review B3): the nodes sign and broadcast A's main
	// to B; the maintainer proves it; B holds the pending incoming output.
	main := k09ParseOutpoint(t, w.facts.MainA)
	movements := w.awaitSpenders(main, k09Action)
	if len(movements) == 0 {
		t.Fatalf("FINDING K-09 09-movement (SC-06): A's targets were committed by window %d, but no node broadcast the movement of A's main %s (exited: %v)", committed, w.facts.MainA, w.exitedNodes())
	}
	if len(movements) != 1 {
		t.Fatalf("K-09 09-movement: %d distinct node transactions spend A's main: %v", len(movements), movements)
	}
	tx, err := w.nodeTransaction(movements[0])
	if err != nil {
		t.Fatalf("K-09 09-movement: Core does not hold the nodes' movement %s: %v", movements[0], err)
	}
	w.mineCore(tx)
	w.send("maintainer: movement proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.SubmitFrostMovingFundsProof(o, w.wallet.id, k09TxInfo(tx), k09Proof(w.btc.proof(t, tx.Hash())))
	})
	if n := w.movementsCompleted(w.wallet); n != 1 {
		t.Fatalf("K-09 09-movement: the Bridge recorded %d completed movements of A after the proof, want 1", n)
	}
	target, err := bitcoin.PayToTaproot(b.q)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Outputs) != 1 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, target) || tx.Outputs[0].Value >= w.facts.MainAValue {
		t.Fatalf("K-09 09-movement: the movement %s does not move A's main (%d sat) to B in one output less its fee", movements[0], w.facts.MainAValue)
	}
	incoming, err := w.bridge.FrostIncoming(&bind.CallOpts{}, deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: tx.Hash()}))
	if err != nil {
		t.Fatal(err)
	}
	if incoming.State != 1 || incoming.Target != b.id || incoming.Value != uint64(tx.Outputs[0].Value) {
		t.Fatalf("K-09 09-movement: B holds no pending incoming output for the proven movement (state %d, target %x, value %d)", incoming.State, incoming.Target, incoming.Value)
	}
	if c := w.custody(w.wallet); c.Value != 0 {
		t.Fatalf("K-09 09-movement: A still holds custody %d after the proven movement", c.Value)
	}
}

// movementsCompleted counts the Bridge's FrostMovingFundsCompleted events
// for the wallet.
func (w *k09World) movementsCompleted(wallet b6Wallet) int {
	it, err := w.bridge.FilterFrostMovingFundsCompleted(&bind.FilterOpts{}, [][32]byte{wallet.id}, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	defer it.Close()
	n := 0
	for it.Next() {
		n++
	}
	return n
}

// movementCommitments counts the Bridge's FrostMovingFundsCommitted events
// for the wallet.
func (w *k09World) movementCommitments(wallet b6Wallet) int {
	it, err := w.bridge.FilterFrostMovingFundsCommitted(&bind.FilterOpts{}, [][32]byte{wallet.id})
	if err != nil {
		w.t.Fatal(err)
	}
	defer it.Close()
	n := 0
	for it.Next() {
		n++
	}
	return n
}

// --- 10 and 12: heartbeat and inactivity (SC-07) ----------------------------

// k09HeartbeatWindows is how many heartbeat windows the inactivity stages
// enter: base claims after K09HeartbeatFailureThreshold consecutive failed
// heartbeats, and a heartbeat whose signing is cancelled by its deadline is
// not counted, so a correct node may need more than three windows (review
// F3 risk). Both stages enter the same number.
const k09HeartbeatWindows = 6

// k09Heartbeats starts the nodes with operator down's node killed for good
// and enters up to k09HeartbeatWindows windows whose seeded checklist (base's
// rule, derived here independently) holds a heartbeat and whose leader is
// live, stopping at the first inactivity claim for the wallet. It returns the
// claims seen and the windows entered.
func (w *k09World) heartbeats(down int) ([]*frostbridgeabi.FrostBridgeFrostOperatorInactivityNotified, int) {
	t := w.t
	t.Helper()
	heartbeat := func(window uint64) bool {
		if w.operatorIndex(w.expectedLeader(w.wallet, window)) == down {
			return false
		}
		header, err := w.admin.HeaderByNumber(context.Background(), new(big.Int).SetUint64(window-tbtc.B6CoordinationSafeBlockShift))
		if err != nil {
			t.Fatal(err)
		}
		seed := sha256.Sum256(append(append([]byte{}, w.wallet.label[:]...), header.Hash().Bytes()...))
		rng := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(seed[:8]))))
		return rng.Float64() < tbtc.K09HeartbeatProbability
	}
	for i := 0; i < k09HeartbeatWindows; i++ {
		window := w.chooseWindowWithin(tbtc.B6CoordinationFrequencyBlocks, heartbeat, 400)
		if i == 0 {
			w.startNodes()
			w.kill(down)
		}
		w.enterWindow(window)
		if !w.waitBlock(window+tbtc.B6CoordinationActivePhaseBlocks+40, k09Action) {
			t.Fatal("setup: the chain did not advance")
		}
		time.Sleep(3 * time.Second)
		if claims := w.inactivityClaims(); len(claims) > 0 {
			return claims, i + 1
		}
	}
	time.Sleep(5 * time.Second)
	return w.inactivityClaims(), k09HeartbeatWindows
}

// inactivityClaims returns the Bridge's FrostOperatorInactivityNotified
// events for the wallet.
func (w *k09World) inactivityClaims() []*frostbridgeabi.FrostBridgeFrostOperatorInactivityNotified {
	it, err := w.bridge.FilterFrostOperatorInactivityNotified(&bind.FilterOpts{}, [][32]byte{w.wallet.id})
	if err != nil {
		w.t.Fatal(err)
	}
	defer it.Close()
	var claims []*frostbridgeabi.FrostBridgeFrostOperatorInactivityNotified
	for it.Next() {
		claims = append(claims, it.Event)
	}
	return claims
}

// seatsOf returns the wallet's seats (1-based, ascending) held by operator i.
func (w *k09World) seatsOf(i int) []uint16 {
	address := crypto.PubkeyToAddress(w.keys[i-1].PublicKey)
	var seats []uint16
	for seat, operator := range w.wallet.operators {
		if operator == address {
			seats = append(seats, uint16(seat+1))
		}
	}
	return seats
}

// k09Inactivity: operator 2's node (two of the five seats) is down for good.
// In base, every window whose seeded checklist holds a heartbeat runs one; a
// heartbeat with fewer than 70% of the wallet's seats active counts as a
// failure (DECISIONS.md, 2026-11-04: base's 70 of 100), and after
// K09HeartbeatFailureThreshold consecutive failures the active members claim
// inactivity on chain with heartbeatFailed set. With 3 of 5 seats active
// (60%) the heartbeat still signs (threshold 3) but fails the activity rule.
// The claim must name exactly operator 2's seats and operator, with
// heartbeatFailed, and move the Live wallet's funds (review B4).
func k09Inactivity(t *testing.T, w *k09World) {
	nonce, err := w.bridge.FrostInactivityNonce(&bind.CallOpts{}, w.wallet.id)
	if err != nil {
		t.Fatal(err)
	}
	claims, windows := w.heartbeats(2)
	if len(claims) == 0 {
		after, _ := w.bridge.FrostInactivityNonce(&bind.CallOpts{}, w.wallet.id)
		t.Fatalf("FINDING K-09 10-inactivity (SC-07): after %d heartbeat windows with operator 2's node (two seats) down, no inactivity claim reached the Bridge (nonce %d, was %d): the node dispatches no FROST heartbeat or inactivity claim (exited: %v)", windows, after, nonce, w.exitedNodes())
	}
	claim := claims[0]
	operator := crypto.PubkeyToAddress(w.keys[1].PublicKey)
	sameOperator := len(claim.InactiveOperators) > 0
	for _, o := range claim.InactiveOperators {
		sameOperator = sameOperator && o == operator
	}
	if fmt.Sprint(claim.InactiveSeats) != fmt.Sprint(w.seatsOf(2)) || !sameOperator || !claim.HeartbeatFailed {
		t.Fatalf("K-09 10-inactivity (SC-07): the inactivity claim names seats %v and operators %v with heartbeatFailed %v; want operator 2's seats %v, operator %s, heartbeatFailed true", claim.InactiveSeats, claim.InactiveOperators, claim.HeartbeatFailed, w.seatsOf(2), operator)
	}
	if len(claims) != 1 {
		t.Fatalf("K-09 10-inactivity: %d inactivity claims for one outage, want 1", len(claims))
	}
	if w.walletEvents(w.wallet, k09MovingFundsEvent) == 0 {
		t.Fatal("K-09 10-inactivity: the heartbeat-failure claim did not move the Live wallet's funds")
	}
}

// k09NoInactivity (review B5): operator 3's node (one seat) is down for
// good, so 4 of 5 seats (80%) are active: each heartbeat passes the 70%
// activity rule and no claim may follow in the same number of heartbeat
// windows. Base's absolute rule (70 active members) applied to a 5-seat
// wallet would fail every heartbeat and claim: this stage catches it.
func k09NoInactivity(t *testing.T, w *k09World) {
	nonce, err := w.bridge.FrostInactivityNonce(&bind.CallOpts{}, w.wallet.id)
	if err != nil {
		t.Fatal(err)
	}
	claims, windows := w.heartbeats(3)
	after, err := w.bridge.FrostInactivityNonce(&bind.CallOpts{}, w.wallet.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) > 0 || after != nonce {
		var seats []uint16
		if len(claims) > 0 {
			seats = claims[0].InactiveSeats
		}
		t.Fatalf("K-09 12-no-inactivity (SC-07): with 4 of 5 seats active (80%%, above the 70%% activity threshold), an inactivity claim reached the Bridge within %d heartbeat windows (claimed seats %v; nonce %d -> %d)", windows, seats, nonce, after)
	}
}

// --- 11: cutover and closure (SC-01, SC-08, SC-09) ---------------------------

// k09CutoverClosure: governance cuts creation over to FROST. Legacy wallet
// creation is then refused, while the nodes still make a FROST wallet (C).
// The drained legacy source closes after its closing period (keeper); the
// empty FROST wallet C ages out, closes and is finalised Closed with an
// empty closure ledger (keeper).
func k09CutoverClosure(t *testing.T, w *k09World) {
	if reverted, why := w.reverts(w.legacyWorld.bridge, w.bridgeABI, "requestNewWallet", tbtcabi.BitcoinTxUTXO{}); reverted {
		t.Fatalf("setup: legacy wallet creation already refused before the cutover: %s", why)
	}
	w.send("governance: legacy DKG drain", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "beginLegacyDkgDrain")
	})
	w.send("governance: cutover", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "completeFrostCutover", crypto.Keccak256Hash([]byte("K-09 local cutover manifest")))
	})
	if mode := w.creationMode(); mode != 2 {
		t.Fatalf("K-09 11-cutover-closure: creation mode %d after the cutover, want FrostOnly", mode)
	}
	if reverted, _ := w.reverts(w.legacyWorld.bridge, w.bridgeABI, "requestNewWallet", tbtcabi.BitcoinTxUTXO{}); !reverted {
		t.Fatal("K-09 11-cutover-closure (SC-09): the Bridge still accepts a legacy wallet request after the cutover")
	}
	w.startNodes()
	epoch := w.requestWallet(k09Pattern)
	c, ok := w.readyWallet(epoch, k09DKGBound)
	if !ok {
		t.Fatalf("K-09 11-cutover-closure: after the cutover the nodes did not take epoch %d's FROST wallet to ReadyUnfunded (exited: %v)", epoch, w.exitedNodes())
	}
	w.killAll()
	w.stopMining()

	parameters, err := w.legacy.WalletParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	// The drained legacy source (Closing since 04-drain) closes.
	w.increaseTime(uint64(parameters.WalletClosingPeriod) + 1)
	w.send("keeper: legacy closing period elapsed", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.NotifyWalletClosingPeriodElapsed(o, w.source.pkh)
	})
	if source, err := w.legacy.Wallets(&bind.CallOpts{}, w.source.pkh); err != nil || source.State != 4 {
		t.Fatalf("K-09 11-cutover-closure (SC-01): the drained legacy source is in state %d after its closing period, want Closed (%v)", source.State, err)
	}
	// The empty FROST wallet C ages out, closes and is finalised.
	w.increaseTime(uint64(parameters.WalletMaxAge) + 1)
	w.send("keeper: C closeable", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.NotifyFrostWalletCloseable(o, c.id)
	})
	if w.walletEvents(c, k09ClosingEvent) == 0 {
		t.Fatal("K-09 11-cutover-closure: the empty, aged FROST wallet C did not enter Closing")
	}
	w.increaseTime(uint64(parameters.WalletClosingPeriod) + 1)
	w.send("keeper: finalise C", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.FinalizeFrostWalletClosing(o, c.id)
	})
	if w.walletEvents(c, k09ClosedEvent) == 0 || w.obligations(c) != 0 {
		t.Fatalf("K-09 11-cutover-closure (SC-08): FROST wallet C is not Closed with an empty closure ledger (obligations %b)", w.obligations(c))
	}
	// Wallet A holds custody: it can close only after a movement (09-movement).
	t.Logf("K-09 11-cutover-closure: wallet A's closure obligations: %b (custody %d)", w.obligations(w.wallet), w.custody(w.wallet).Value)
}
