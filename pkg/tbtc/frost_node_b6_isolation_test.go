package tbtc_test

// Remediation batch 6 acceptance tests: per-wallet and per-epoch errors are
// isolated in the base node with FROST configured. Refs tuxxy/keep-core#38
// (K09-21), #39 (K09-22). World: frost_node_b6_world_test.go.
//
// "No second distinct signature for the interrupted plan" (K09-21 criterion)
// is read as: no second transaction with a different txid spends the plan's
// input. A FROST re-signing of the same unsigned transaction differs only in
// its witness (same txid) and is not a conflicting spend.

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// b6CrashPoint is where TestFrostNodeIsolation_CrashPoints kills a node.
type b6CrashPoint int

const (
	// b6Candidate: the registry shows the epoch's DKG result submitted, the
	// node's record still a candidate.
	b6Candidate b6CrashPoint = iota
	// b6Certified: the registry shows the wallet ReadyUnfunded; the node may
	// not have saved it locally yet.
	b6Certified
	// b6Signed: a node broadcast the incoming sweep; no proof yet.
	b6Signed
)

func (p b6CrashPoint) String() string {
	return [...]string{"candidate", "certified", "signed-before-proof"}[p]
}

// TestFrostNodeIsolation_CrashPoints (#38, K09-21 criterion): a node killed
// (SIGKILL) at a submitted DKG result, at certification, or between signing
// and proof restarts and stays up; a fresh-epoch DKG completes; and an
// incoming sweep settles (for another wallet, or for the interrupted plan)
// with no second distinct transaction spending the interrupted plan's input.
func TestFrostNodeIsolation_CrashPoints(t *testing.T) {
	t.Parallel()
	const victim = 2
	for _, point := range []b6CrashPoint{b6Candidate, b6Certified, b6Signed} {
		point := point
		t.Run(point.String(), func(t *testing.T) {
			t.Parallel()
			w := newFrostNodeWorld(t)
			w.startNodes()
			epoch := w.requestWallet([]int{1, 2, 3})
			var first b6Wallet
			var drain *bitcoin.Transaction
			switch point {
			case b6Candidate:
				deadline := time.Now().Add(b6Ready)
				for {
					at, err := w.registry.SubmittedAt(&bind.CallOpts{})
					if err == nil && at != 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("#26 %s precondition: no DKG result for epoch %d was submitted within %v: no node ran the FROST DKG duty", t.Name(), epoch, b6Ready)
					}
					time.Sleep(10 * time.Millisecond)
				}
				w.kill(victim)
				w.startNode(victim)
				var ok bool
				if first, ok = w.readyWallet(epoch, b6Ready); !ok {
					t.Fatalf("#38 %s: epoch %d's wallet did not become ready after the restart of node %d (the other seats reach the readiness quorum)", point, epoch, victim)
				}
			case b6Certified:
				var ok bool
				if first, ok = w.readyWallet(epoch, b6Ready); !ok {
					t.Fatalf("#26 %s precondition: epoch %d's FROST wallet did not reach ReadyUnfunded within %v: no node ran the FROST DKG and readiness duty", t.Name(), epoch, b6Ready)
				}
				w.kill(victim)
				w.startNode(victim)
			case b6Signed:
				var ok bool
				if first, ok = w.readyWallet(epoch, b6Ready); !ok {
					t.Fatalf("#26 %s precondition: epoch %d's FROST wallet did not reach ReadyUnfunded within %v: no node ran the FROST DKG and readiness duty", t.Name(), epoch, b6Ready)
				}
				drain = w.drainInto(first)
				outpoint := bitcoin.TransactionOutpoint{TransactionHash: drain.Hash(), OutputIndex: 0}
				window := w.nextSweepWindow()
				deadline := time.Now().Add(b6Sweep)
				for len(w.spenders(outpoint)) == 0 {
					if time.Now().After(deadline) {
						t.Fatalf("#26 %s precondition: no node broadcast the incoming sweep in window %d: the duty is not dispatched", t.Name(), window)
					}
					time.Sleep(10 * time.Millisecond)
				}
				w.kill(victim)
				w.startNode(victim)
				// A full sweep window passes before the proof: a restarted
				// node must not sign a different plan for the same input.
				next := w.nextSweepWindow()
				if !w.waitBlock(next+tbtc.B6CoordinationActivePhaseBlocks+40, b6Sweep) {
					t.Fatal("setup: the chain did not advance")
				}
				if spenders := w.spenders(outpoint); len(spenders) != 1 {
					t.Fatalf("#38 %s: %d distinct transactions spend the interrupted plan's input %s: %v", point, len(spenders), drain.Hash().Hex(bitcoin.ReversedByteOrder), spenders)
				}
			}

			// A fresh-epoch DKG completes with the restarted node up.
			second := w.requestWallet([]int{1, 2, 3})
			fresh, ok := w.readyWallet(second, b6Ready)
			if !ok {
				t.Fatalf("#38 %s: after node %d restarted, the fresh epoch %d's DKG did not complete within %v (exited: %v)%s", point, victim, second, b6Ready, w.exitedNodes(), w.debugLogs())
			}
			if exited := w.exitedNodes(); len(exited) > 0 {
				t.Fatalf("#38 %s: a node exited after the restart: %v", point, exited)
			}

			// An incoming sweep settles: for the fresh wallet, or for the
			// interrupted plan.
			target := fresh
			if point == b6Signed {
				target = first
			} else {
				drain = w.drainInto(fresh)
			}
			window := w.nextSweepWindow()
			if _, ok := w.settleIncoming(target, drain, b6Sweep); !ok {
				t.Fatalf("#38 %s: the incoming sweep did not settle in window %d after node %d restarted (exited: %v)%s", point, window, victim, w.exitedNodes(), w.debugLogs())
			}
			outpoint := bitcoin.TransactionOutpoint{TransactionHash: drain.Hash(), OutputIndex: 0}
			if spenders := w.spenders(outpoint); len(spenders) != 1 {
				t.Fatalf("#38 %s: %d distinct transactions spend the incoming output: %v", point, len(spenders), spenders)
			}
			if exited := w.exitedNodes(); len(exited) > 0 {
				t.Fatalf("#38 %s: a node exited: %v", point, exited)
			}
		})
	}
}

// TestFrostNodeIsolation_FailedDKGDoesNotBlockSweep (#38, K09-21 criterion):
// while the latest epoch's DKG cannot complete (operator 3's node is down for
// good), the earlier wallet's pending incoming sweep settles with the two
// remaining seats (threshold 2), and the remaining nodes stay up.
func TestFrostNodeIsolation_FailedDKGDoesNotBlockSweep(t *testing.T) {
	t.Parallel()
	w, wallet := newB6ReadyWorld(t, []int{1, 2, 3})
	drain := w.drainInto(wallet)
	w.kill(3)
	epoch := w.requestWallet([]int{1, 2, 3})
	window := w.nextSweepWindow()
	if _, ok := w.settleIncoming(wallet, drain, b6Sweep); !ok {
		t.Fatalf("#38 failed DKG: with epoch %d's DKG unable to complete (operator 3 down), the earlier wallet's pending incoming sweep did not settle in window %d (exited: %v)%s", epoch, window, w.exitedNodes(), w.debugLogs())
	}
	if exited := w.exitedNodes(); len(exited) > 0 {
		t.Fatalf("#38 failed DKG: a node exited: %v", exited)
	}
}

// TestFrostNodeIsolation_ReservationOnOtherWallet (#39, K09-22 criterion):
// with a real node set running, a redemption on wallet A is reserved, paid
// and proven; while A's reservation is active, the nodes stay up and sweep an
// incoming output on wallet B. The redemption itself is node work too
// (#26): the test only requests it and, as SPV maintainer, proves it.
func TestFrostNodeIsolation_ReservationOnOtherWallet(t *testing.T) {
	t.Parallel()
	w := newFrostNodeWorld(t)
	sourceB, mainB := w.secondSource()
	w.startNodes()
	epochA := w.requestWallet([]int{1, 2, 3})
	a, ok := w.readyWallet(epochA, b6Ready)
	if !ok {
		t.Fatalf("#26 %s precondition: wallet A (epoch %d) did not reach ReadyUnfunded within %v: no node ran the FROST DKG and readiness duty", t.Name(), epochA, b6Ready)
	}
	// A gets a main through its own incoming sweep.
	drainA := w.drainInto(a)
	window := w.nextSweepWindow()
	sweepA, ok := w.settleIncoming(a, drainA, b6Sweep)
	if !ok {
		t.Fatalf("#26 %s precondition: no node swept wallet A's incoming output in window %d: the duty is not dispatched", t.Name(), window)
	}
	epochB := w.requestWallet([]int{1, 2, 3})
	b, ok := w.readyWallet(epochB, b6Ready)
	if !ok {
		t.Fatalf("#26 %s precondition: wallet B (epoch %d) did not reach ReadyUnfunded: no node ran the FROST DKG duty", t.Name(), epochB)
	}

	// The redeemer requests a redemption from A; the nodes reserve it.
	redeemer, err := bitcoin.PayToWitnessPublicKeyHash([20]byte{0xb6, 0x39})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := w.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	reservation := w.requestRedemption(a, redeemer, 2*parameters.RedemptionDustThreshold)
	reserved, ok := w.reservationAdmitted(a, reservation, b6Sweep)
	if !ok {
		// Redemptions are checked in every window.
		w.nextWindow()
		if reserved, ok = w.reservationAdmitted(a, reservation, b6Sweep); !ok {
			t.Fatalf("#26 redemption: the nodes did not reserve the pending FROST redemption on wallet A within a coordination window (exited: %v)%s", w.exitedNodes(), w.debugLogs())
		}
	}

	// While A's reservation is active (its proof is held back), B's incoming
	// output is swept and every node stays up.
	drainB := w.drainFrom(sourceB, mainB, b)
	window = w.nextSweepWindow()
	if _, ok := w.settleIncoming(b, drainB, b6Sweep); !ok {
		t.Fatalf("#39: with an active redemption reservation on wallet A, no node swept wallet B's incoming output in window %d (exited: %v)%s", window, w.exitedNodes(), w.debugLogs())
	}
	if exited := w.exitedNodes(); len(exited) > 0 {
		t.Fatalf("#39: a node exited with a redemption reservation active on wallet A: %v", exited)
	}

	// A's redemption is paid by the nodes and proven by the maintainer.
	hash, err := bitcoin.NewHashFromString(sweepA, bitcoin.ReversedByteOrder)
	if err != nil {
		t.Fatal(err)
	}
	mainA := bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: 0}
	deadline := time.Now().Add(b6Sweep)
	var payment []string
	for payment = w.spenders(mainA); len(payment) == 0; payment = w.spenders(mainA) {
		if time.Now().After(deadline) {
			t.Fatalf("#26 redemption: the nodes did not pay reservation %x on wallet A (exited: %v)", reserved, w.exitedNodes())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(payment) != 1 {
		t.Fatalf("#39: %d distinct transactions spend wallet A's reserved main: %v", len(payment), payment)
	}
	paymentHash, err := bitcoin.NewHashFromString(payment[0], bitcoin.ReversedByteOrder)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.btc.GetTransaction(paymentHash)
	if err != nil {
		t.Fatalf("#26 redemption: Core does not hold the nodes' payment %s: %v", payment[0], err)
	}
	w.btc.mine(t, tx)
	w.send("redemption proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "submitFrostRedemptionProof", reserved, legacyTxInfo(tx), w.btc.proof(t, tx.Hash()))
	})
	if exited := w.exitedNodes(); len(exited) > 0 {
		t.Fatalf("#39: a node exited: %v", exited)
	}
}
