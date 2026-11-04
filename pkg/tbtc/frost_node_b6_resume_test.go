package tbtc_test

// Fix review B2 (tuxxy/keep-core#26, #39): an admitted FROST reservation is
// resumed, not dropped. Not a frozen batch-6 file; it uses the batch-6 world.

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// TestFrostNodeResume_ReservationPaidAfterRestart (review B2; as base: after a
// restart the node takes up its wallet's unfinished redemption from chain
// state): every node process is killed right after the Bridge admitted a
// reservation and before any payment, and restarted on its storage. The
// nodes then pay that same reservation, and its reserved main is spent by
// exactly one transaction, also a window after the proof.
func TestFrostNodeResume_ReservationPaidAfterRestart(t *testing.T) {
	t.Parallel()
	w, a := loadB6Ready(t, []int{1, 2, 3})
	window := w.chooseWindow(4*tbtc.B6CoordinationFrequencyBlocks, nil)
	drain := w.drainInto(a)
	w.startNodes()
	w.enterWindow(window)
	sweep, ok := w.settleIncoming(a, drain, b6Sweep)
	if !ok {
		t.Fatalf("setup: no node swept wallet A's incoming output in window %d", window)
	}
	hash, err := bitcoin.NewHashFromString(sweep, bitcoin.ReversedByteOrder)
	if err != nil {
		t.Fatal(err)
	}
	main := bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: 0}

	redeemer, err := bitcoin.PayToWitnessPublicKeyHash([20]byte{0xb2})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := w.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	request := w.requestRedemption(a, redeemer, 2*parameters.RedemptionDustThreshold)
	w.nextWindow()
	reserved, ok := w.reservationAdmitted(a, request, b6Sweep)
	if !ok {
		t.Fatalf("setup: the nodes did not reserve the FROST redemption (exited: %v)", w.exitedNodes())
	}
	// Every node goes down before the payment's first signing round ends.
	w.stopMining()
	w.killAll()
	if spenders := w.spenders(main); len(spenders) != 0 {
		t.Fatalf("setup: the payment %v was broadcast before the nodes were killed", spenders)
	}

	w.startNodes()
	window = w.nextWindow()
	payment := w.awaitSpenders(main, b6Sweep)
	if len(payment) == 0 {
		t.Fatalf("B2 resume: after every node restarted, no node paid the admitted reservation %x on wallet A by window %d (exited: %v)%s", reserved, window, w.exitedNodes(), w.debugLogs())
	}
	if len(payment) != 1 {
		t.Fatalf("B2 resume: %d distinct transactions spend wallet A's reserved main: %v", len(payment), payment)
	}
	tx, err := w.coreTransaction(payment[0])
	if err != nil {
		t.Fatalf("B2 resume: Core does not hold the nodes' payment %s: %v", payment[0], err)
	}
	w.mineCore(tx)
	w.send("redemption proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostRedemptionProof", reserved, legacyTxInfo(tx), w.btc.proof(t, tx.Hash()))
	})

	// A window after the proof, the reservation is still paid once.
	next := w.nextWindow()
	if !w.waitBlock(next+tbtc.B6CoordinationActivePhaseBlocks+40, b6Sweep) {
		t.Fatal("setup: the chain did not advance")
	}
	if spenders := w.spenders(main); len(spenders) != 1 {
		t.Fatalf("B2 resume: %d distinct transactions spend wallet A's reserved main: %v", len(spenders), spenders)
	}
	if exited := w.exitedNodes(); len(exited) > 0 {
		t.Fatalf("B2 resume: a node exited: %v", exited)
	}
}

// awaitSpenders waits, within bound, until a node broadcast a spend of the
// outpoint, and returns the distinct spenders seen.
func (w *frostNodeWorld) awaitSpenders(outpoint bitcoin.TransactionOutpoint, bound time.Duration) []string {
	deadline := time.Now().Add(bound)
	for {
		if spenders := w.spenders(outpoint); len(spenders) > 0 || time.Now().After(deadline) {
			return spenders
		}
		time.Sleep(50 * time.Millisecond)
	}
}
