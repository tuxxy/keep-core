package tbtcpg_test

import (
	"bytes"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Mutation batch M8 (MovingFundsTask, FROST branch). Fixtures are in
// frost_gaps_fixture_test.go.
//
// Invariant (KC-SF-V3 K-07, C-06; runFrost doc; base Run and
// ValidateMovingFundsSafetyMargin): only a MovingFunds or Terminated source
// with no recorded dust, no proven movement, past the safety margin (24 h,
// 14 days for a pending movement target, at most half the moving funds
// timeout), with no reservation, pending FROST redemption, pending incoming
// output or unswept deposit within the look-back, and a positive main, is
// moved. Uncommitted, the leader commits once, with its own descriptor seat,
// the targets the K-07 planner chooses; committed, the proposal names the
// recorded targets. Otherwise it reports no result (ok false). With too few
// targets it returns ErrNotEnoughTargetWallets (D6: the next window
// retries); any failed read or a wallet that is not FROST is an error, never
// reported as "not enough targets" and never a crash.

type movingGapsWorld struct {
	f       *gFaults
	host    *gHost
	btc     *gBitcoin
	request *tbtc.CoordinationProposalRequest
	// targets are the ready target labels in ascending order.
	targets [][20]byte
	main    [32]byte
}

func newMovingGapsWorld(t *testing.T) *movingGapsWorld {
	t.Helper()
	w := &movingGapsWorld{f: &gFaults{}, main: [32]byte{0x3a}}
	w.host = newGHost(w.f, newGBridge(w.f))
	w.btc = newGBitcoin(w.f)
	b, wallet := w.host.bridge, w.host.walletID()
	b.wallets[movement.Label(wallet)] = movement.Wallet{ID: wallet, State: movement.StateMovingFunds, CapabilityVersion: 1}
	b.lifecycle = movement.Lifecycle{Period: 1, MovingFundsRequestedAt: uint64(time.Now().Add(-48 * time.Hour).Unix())}
	b.custody = redemption.Custody{MainOutpoint: w.main, Value: 1_000_000}
	b.outputs[w.main] = redemption.CustodyOutput{WalletID: wallet, Value: 1_000_000}
	b.params = movement.Parameters{WalletMaxBtcTransfer: 600_000, MovingFundsTxMaxTotalFee: 100_000}
	b.counters = movement.Counters{EligibleTargets: 5}
	for i := byte(1); i <= 3; i++ {
		id := [32]byte{0x90 - i*0x10, i}
		b.ready = append(b.ready, id)
		b.wallets[movement.Label(id)] = movement.Wallet{ID: id, State: movement.StateLive, CapabilityVersion: 1}
		w.targets = append(w.targets, movement.Label(id))
	}
	sort.Slice(w.targets, func(i, j int) bool { return bytes.Compare(w.targets[i][:], w.targets[j][:]) < 0 })
	w.request = &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.host.label,
		FrostWallet:         true,
		WalletOperators:     []chain.Address{"0xop0", "0xop1", "0xop2"},
		ExecutingOperator:   "0xop1",
	}
	return w
}

// commit records a commitment of the current period naming the highest
// target (not the planner's choice) with a cap of maxFee.
func (w *movingGapsWorld) commit(maxFee uint64) [][20]byte {
	b := w.host.bridge
	targets := [][20]byte{w.targets[2]}
	b.commitment = movement.Commitment{MainOutpoint: w.main, CommittedAt: b.head - 10, MaxFee: maxFee, Targets: targets}
	b.logBlock = b.head - 10
	return targets
}

func (w *movingGapsWorld) runOn(host tbtcpg.Chain, btc bitcoin.Chain) (*tbtc.MovingFundsProposal, bool, error) {
	proposal, ok, err := recoverRun(tbtcpg.NewMovingFundsTask(host, btc), w.request)
	if proposal == nil {
		return nil, ok, err
	}
	p, isMoving := proposal.(*tbtc.MovingFundsProposal)
	if !isMoving {
		return nil, ok, errors.New("fixture: not a moving funds proposal")
	}
	return p, ok, err
}

func (w *movingGapsWorld) run() (*tbtc.MovingFundsProposal, bool, error) {
	return w.runOn(w.host, w.btc)
}

func sameLabels(a, b [][20]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFrostMovingFundsCommitsAndProposes: the two positive paths.
func TestFrostMovingFundsCommitsAndProposes(t *testing.T) {
	t.Run("uncommitted: commits the planner's targets with the executing seat", func(t *testing.T) {
		w := newMovingGapsWorld(t)
		proposal, ok, err := w.run()
		if err != nil || !ok || proposal == nil {
			t.Fatalf("no proposal (ok %v): %v", ok, err)
		}
		want := w.targets[:2]
		commits := w.host.bridge.commits
		if len(commits) != 1 || commits[0].wallet != w.host.walletID() || commits[0].seat != 1 || !sameLabels(commits[0].targets, want) {
			t.Fatalf("commitments %+v, expected one for the wallet with seat 1 and targets %x", commits, want)
		}
		if !sameLabels(proposal.TargetWallets, want) {
			t.Fatalf("proposal names %x, expected the committed %x", proposal.TargetWallets, want)
		}
		if fee := proposal.MovingFundsTxFee.Int64(); fee <= 0 || uint64(fee) > w.host.bridge.params.MovingFundsTxMaxTotalFee {
			t.Fatalf("fee %d, expected positive and within the cap %d", fee, w.host.bridge.params.MovingFundsTxMaxTotalFee)
		}
	})
	t.Run("committed: proposes the recorded targets without committing", func(t *testing.T) {
		w := newMovingGapsWorld(t)
		want := w.commit(50_000)
		proposal, ok, err := w.run()
		if err != nil || !ok || proposal == nil {
			t.Fatalf("no proposal (ok %v): %v", ok, err)
		}
		if len(w.host.bridge.commits) != 0 {
			t.Fatalf("committed %d times over a recorded commitment", len(w.host.bridge.commits))
		}
		if !sameLabels(proposal.TargetWallets, want) {
			t.Fatalf("proposal names %x, expected the recorded %x", proposal.TargetWallets, want)
		}
		if fee := proposal.MovingFundsTxFee.Int64(); fee <= 0 || fee > 50_000 {
			t.Fatalf("fee %d, expected positive and within the committed cap 50000", fee)
		}
	})
	t.Run("a Terminated source is moved", func(t *testing.T) {
		w := newMovingGapsWorld(t)
		b := w.host.bridge
		wl := b.wallets[movement.Label(w.host.walletID())]
		wl.State = movement.StateTerminated
		b.wallets[movement.Label(w.host.walletID())] = wl
		if proposal, ok, err := w.run(); err != nil || !ok || proposal == nil {
			t.Fatalf("no proposal for a Terminated source (ok %v): %v", ok, err)
		}
	})
	t.Run("deposits that are missing, swept or spent do not hold the source", func(t *testing.T) {
		w := newMovingGapsWorld(t)
		w.depositStates(false)
		if proposal, ok, err := w.run(); err != nil || !ok || proposal == nil {
			t.Fatalf("no proposal (ok %v): %v", ok, err)
		}
	})
	t.Run("past a safety margin capped by half the timeout", func(t *testing.T) {
		w := newMovingGapsWorld(t)
		w.host.bridge.counters.InboundCommitments = 1
		w.host.mfParams.Timeout = 2 * 24 * 3600
		if proposal, ok, err := w.run(); err != nil || !ok || proposal == nil {
			t.Fatalf("no proposal (ok %v): %v", ok, err)
		}
	})
}

// depositStates reveals, in this order: a deposit with no C-05 record, a
// swept one (its funding spent), an unswept one whose funding is spent, and,
// if unspent, an unswept one whose funding is unspent.
func (w *movingGapsWorld) depositStates(unspent bool) {
	b := w.host.bridge
	b.reveals = append(b.reveals, deposit.Reveal{Outpoint: [32]byte{0xd0}, WalletID: w.host.walletID(), Block: b.head - 100})
	swept := gDeposit(w.host, w.btc, 0xd1, 50_000)
	r := b.deposits[swept]
	r.SweptAt = 5
	b.deposits[swept] = r
	spent := gDeposit(w.host, w.btc, 0xd2, 50_000)
	keep := w.btc.unspent[:0]
	for _, u := range w.btc.unspent {
		if k := deposit.OutpointKey(u.Outpoint); k != swept && k != spent {
			keep = append(keep, u)
		}
	}
	w.btc.unspent = keep
	if unspent {
		gDeposit(w.host, w.btc, 0xd3, 50_000)
	}
}

// TestFrostMovingFundsWaits: each condition alone stops the movement with no
// result and no commitment.
func TestFrostMovingFundsWaits(t *testing.T) {
	ago := func(d time.Duration) uint64 { return uint64(time.Now().Add(-d).Unix()) }
	setState := func(w *movingGapsWorld, s movement.State) {
		b := w.host.bridge
		wl := b.wallets[movement.Label(w.host.walletID())]
		wl.State = s
		b.wallets[movement.Label(w.host.walletID())] = wl
	}
	far := tbtcpg.DepositSweepLookBackBlocks + 300_000
	tests := map[string]func(w *movingGapsWorld){
		"a Live source":                   func(w *movingGapsWorld) { setState(w, movement.StateLive) },
		"a Closing source":                func(w *movingGapsWorld) { setState(w, movement.StateClosing) },
		"funds recorded as dust":          func(w *movingGapsWorld) { w.host.bridge.lifecycle.Dust = true },
		"a proven movement":               func(w *movingGapsWorld) { w.commit(50_000); w.host.bridge.commitment.ProvenAt = 7 },
		"within the 24 hour margin":       func(w *movingGapsWorld) { w.host.bridge.lifecycle.MovingFundsRequestedAt = ago(time.Hour) },
		"a pending target within 14 days": func(w *movingGapsWorld) { w.host.bridge.counters.InboundCommitments = 1 },
		"within half a short timeout": func(w *movingGapsWorld) {
			w.host.bridge.counters.InboundCommitments = 1
			w.host.mfParams.Timeout = 2 * 24 * 3600
			w.host.bridge.lifecycle.MovingFundsRequestedAt = ago(12 * time.Hour)
		},
		"an active reservation": func(w *movingGapsWorld) { w.host.bridge.custody.ActiveReservation = [32]byte{0x99} },
		"a pending FROST redemption": func(w *movingGapsWorld) {
			w.host.pendingScripts, w.host.pendingBlocks = []bitcoin.Script{{0x00}}, []uint64{w.host.bridge.head - 100}
		},
		"a pending FROST redemption far beyond the look-back start": func(w *movingGapsWorld) {
			w.host.bridge.head = far
			w.host.bridge.logBlock = far - 10
			w.host.pendingScripts, w.host.pendingBlocks = []bitcoin.Script{{0x00}}, []uint64{far - 100}
		},
		"a pending incoming output": func(w *movingGapsWorld) { w.host.bridge.counters.PendingIncoming = 1 },
		"an unswept deposit":        func(w *movingGapsWorld) { w.depositStates(true) },
		"an unswept deposit far beyond the look-back start": func(w *movingGapsWorld) {
			w.host.bridge.head = far
			gDeposit(w.host, w.btc, 0xd4, 50_000)
		},
		"no main value": func(w *movingGapsWorld) {
			w.host.bridge.custody = redemption.Custody{}
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			w := newMovingGapsWorld(t)
			setup(w)
			proposal, ok, err := w.run()
			if err != nil || ok || proposal != nil {
				t.Fatalf("%s: error %v, ok %v, proposal %v; expected no result", name, err, ok, proposal != nil)
			}
			if len(w.host.bridge.commits) != 0 {
				t.Fatalf("%s: committed %d times", name, len(w.host.bridge.commits))
			}
		})
	}
}

// TestFrostMovingFundsRefusals: refusals are errors with no proposal; too
// few targets is ErrNotEnoughTargetWallets.
func TestFrostMovingFundsRefusals(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain)
		notEnough   bool
		wantCommits int
	}{
		{"host without FROST seams", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) {
			return gPlainHost{LocalChain: tbtcpg.NewLocalChain()}, w.btc
		}, false, 0},
		{"wallet not FROST", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) { w.host.frost = false; return w.host, w.btc }, false, 0},
		{"Bitcoin without a script query", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) { return w.host, w.btc.LocalBitcoinChain }, false, 0},
		{"no eligible target", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) {
			w.host.bridge.counters.EligibleTargets = 0
			return w.host, w.btc
		}, true, 0},
		{"fewer ready targets than counted", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) {
			w.host.bridge.ready = w.host.bridge.ready[:0]
			return w.host, w.btc
		}, true, 0},
		{"executing operator not a wallet operator", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) {
			w.request.ExecutingOperator = "0xother"
			return w.host, w.btc
		}, false, 0},
		{"a refused commitment", func(w *movingGapsWorld) (tbtcpg.Chain, bitcoin.Chain) {
			w.host.bridge.commitErr = errors.New("fixture: refused")
			return w.host, w.btc
		}, false, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newMovingGapsWorld(t)
			host, btc := test.setup(w)
			proposal, ok, err := w.runOn(host, btc)
			if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
				t.Fatalf("%s: error %v, ok %v, proposal %v; expected a refusal and no proposal", test.name, err, ok, proposal != nil)
			}
			if errors.Is(err, tbtcpg.ErrNotEnoughTargetWallets) != test.notEnough {
				t.Fatalf("%s: error %v; not-enough-targets %v, expected %v", test.name, err, !test.notEnough, test.notEnough)
			}
			if len(w.host.bridge.commits) != test.wantCommits {
				t.Fatalf("%s: %d commitments, expected %d", test.name, len(w.host.bridge.commits), test.wantCommits)
			}
		})
	}
}

// TestFrostMovingFundsReadFaults: every failed read is an error, no proposal
// and never "not enough targets", except the moving funds parameters, whose
// failure (as base's safety margin check) only means no result.
func TestFrostMovingFundsReadFaults(t *testing.T) {
	for _, committed := range []bool{false, true} {
		for n := 1; n < 500; n++ {
			w := newMovingGapsWorld(t)
			w.depositStates(false)
			if committed {
				w.commit(50_000)
			}
			w.f.failAt = n
			proposal, ok, err := w.run()
			if !w.f.fired {
				if n == 1 {
					t.Fatal("fixture: no read was made")
				}
				break
			}
			if w.f.firedOp == "moving-funds-parameters" {
				if ok || proposal != nil || errors.Is(err, errGapsPanic) {
					t.Fatalf("read %d (%s) failed: ok %v, proposal %v, error %v; expected no proposal", n, w.f.firedOp, ok, proposal != nil, err)
				}
				continue
			}
			if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
				t.Fatalf("committed %v, read %d (%s) failed: error %v, ok %v, proposal %v; expected an error and no proposal", committed, n, w.f.firedOp, err, ok, proposal != nil)
			}
			if errors.Is(err, tbtcpg.ErrNotEnoughTargetWallets) {
				t.Fatalf("committed %v, read %d (%s) failed and was reported as not enough targets: %v", committed, n, w.f.firedOp, err)
			}
		}
	}
}
