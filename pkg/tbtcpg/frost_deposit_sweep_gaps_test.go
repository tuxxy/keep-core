package tbtcpg_test

import (
	"bytes"
	"errors"
	"sort"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Mutation batch M8 (DepositSweepTask, FROST branch). Fixtures are in
// frost_gaps_fixture_test.go.
//
// Invariant (KC-SF-V3 K-06; runFrost doc; base FindDepositsToSweep and
// ProposeDepositsSweep): the leader proposes the deposits the K-06 planner
// would sweep that are old enough by the wall clock and whose funding has
// the required confirmations, at most the Bridge's deposit sweep max size
// of them (0 meaning no cap, as base), with a fee within every proposed
// deposit's cap (D19). With nothing to propose, a reserved main or a wallet
// not synced between chains it proposes nothing and reports no result
// (ok false), and a failed read or a wallet it cannot treat as FROST is an
// error, never a silent "no result".

type sweepGapsWorld struct {
	f    *gFaults
	host *gHost
	btc  *gBitcoin
	keys [][32]byte // in the planner's (sorted) order
}

// newSweepGapsWorld: three proposable deposits of the host's FROST wallet.
func newSweepGapsWorld(t *testing.T, maxFees ...uint64) *sweepGapsWorld {
	t.Helper()
	if len(maxFees) == 0 {
		maxFees = []uint64{50_000, 50_000, 50_000}
	}
	w := &sweepGapsWorld{f: &gFaults{}}
	w.host = newGHost(w.f, newGBridge(w.f))
	w.btc = newGBitcoin(w.f)
	for i, fee := range maxFees {
		w.keys = append(w.keys, gDeposit(w.host, w.btc, byte(i+1), fee))
	}
	sort.Slice(w.keys, func(i, j int) bool { return bytes.Compare(w.keys[i][:], w.keys[j][:]) < 0 })
	return w
}

func (w *sweepGapsWorld) run() (*tbtc.DepositSweepProposal, bool, error) {
	return w.runOn(w.host, w.btc)
}

func (w *sweepGapsWorld) runOn(host tbtcpg.Chain, btc bitcoin.Chain) (*tbtc.DepositSweepProposal, bool, error) {
	proposal, ok, err := recoverRun(tbtcpg.NewDepositSweepTask(host, btc), &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.host.label,
		FrostWallet:         true,
	})
	if proposal == nil {
		return nil, ok, err
	}
	sweep, isSweep := proposal.(*tbtc.DepositSweepProposal)
	if !isSweep {
		return nil, ok, errors.New("fixture: not a deposit sweep proposal")
	}
	return sweep, ok, err
}

func (w *sweepGapsWorld) record(key [32]byte) deposit.Record { return w.host.bridge.deposits[key] }

func (w *sweepGapsWorld) setRecord(key [32]byte, r deposit.Record) { w.host.bridge.deposits[key] = r }

// spend removes a deposit's funding output from Bitcoin's unspent set.
func (w *sweepGapsWorld) spend(key [32]byte) {
	var kept []bitcoin.PreviousOutput
	for _, u := range w.btc.unspent {
		if deposit.OutpointKey(u.Outpoint) != key {
			kept = append(kept, u)
		}
	}
	w.btc.unspent = kept
}

func TestFrostDepositSweepProposalSelection(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *sweepGapsWorld)
		// want lists the indices (into keys) proposed; nil: no proposal.
		want []int
		// count, if set, only fixes how many are proposed (a subset).
		count int
	}{
		{"every eligible deposit, max size 0 meaning no cap", func(w *sweepGapsWorld) { w.host.depositSweepMaxSize = 0 }, []int{0, 1, 2}, 0},
		{"max size above the deposits", func(w *sweepGapsWorld) { w.host.depositSweepMaxSize = 5 }, []int{0, 1, 2}, 0},
		{"max size caps the proposal", func(w *sweepGapsWorld) { w.host.depositSweepMaxSize = 2 }, nil, 2},
		{"max size of one", func(w *sweepGapsWorld) { w.host.depositSweepMaxSize = 1 }, nil, 1},
		{"a young deposit waits, the others do not", func(w *sweepGapsWorld) {
			r := w.record(w.keys[0])
			r.RevealedAt = w.host.bridge.now
			w.setRecord(w.keys[0], r)
		}, []int{1, 2}, 0},
		{"deposits younger than the minimum age wait", func(w *sweepGapsWorld) { w.host.minAge = 3 * 3600 }, nil, 0},
		{"an unconfirmed funding waits, the others do not", func(w *sweepGapsWorld) {
			w.btc.SetTransactionConfirmations(bitcoin.Hash(w.record(w.keys[0]).FundingTransactionHash), tbtc.DepositSweepRequiredFundingTxConfirmations-1)
		}, []int{1, 2}, 0},
		{"deposits far beyond the look-back start are still found", func(w *sweepGapsWorld) {
			b := w.host.bridge
			b.head = tbtcpg.DepositSweepLookBackBlocks + 300_000
			for i := range b.reveals {
				b.reveals[i].Block = b.head - 100
			}
		}, []int{0, 1, 2}, 0},
		{"a reserved main makes deposits wait", func(w *sweepGapsWorld) {
			w.host.bridge.custody = redemption.Custody{MainOutpoint: [32]byte{0x3a}, ActiveReservation: [32]byte{0x99}}
		}, nil, 0},
		{"a reserved deposit spent before its locktime (an unproven sweep) stops every sweep", func(w *sweepGapsWorld) {
			w.spend(w.keys[0])
		}, nil, 0},
		{"a deposit whose funding outpoint names no output stops every sweep", func(w *sweepGapsWorld) {
			r := w.record(w.keys[0])
			r.FundingOutputIndex = 1
			w.setRecord(w.keys[0], r)
		}, nil, 0},
		{"no deposits", func(w *sweepGapsWorld) {
			w.host.bridge.reveals = nil
		}, nil, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newSweepGapsWorld(t)
			test.setup(w)
			proposal, ok, err := w.run()
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if test.want == nil && test.count == 0 {
				if ok || proposal != nil {
					t.Fatalf("%s: proposed (ok %v, proposal %v), expected no result", test.name, ok, proposal != nil)
				}
				return
			}
			if !ok || proposal == nil {
				t.Fatalf("%s: no proposal (ok %v), expected one", test.name, ok)
			}
			got := proposalKeys(proposal)
			if len(proposal.DepositsKeys) != len(got) || len(proposal.DepositsRevealBlocks) != len(got) {
				t.Fatalf("%s: proposal has %d keys (%d distinct) and %d reveal blocks", test.name, len(proposal.DepositsKeys), len(got), len(proposal.DepositsRevealBlocks))
			}
			if test.count != 0 {
				if len(got) != test.count {
					t.Fatalf("%s: proposed %d deposits, expected %d", test.name, len(got), test.count)
				}
				for k := range got {
					if _, known := w.host.bridge.deposits[k]; !known {
						t.Fatalf("%s: proposed an unknown deposit %x", test.name, k)
					}
				}
				return
			}
			if len(got) != len(test.want) {
				t.Fatalf("%s: proposed %d deposits, expected %d", test.name, len(got), len(test.want))
			}
			for _, i := range test.want {
				if !got[w.keys[i]] {
					t.Fatalf("%s: deposit %d was not proposed", test.name, i)
				}
			}
		})
	}
}

// TestFrostDepositSweepFeeWithinEveryCap: the proposed fee never exceeds the
// proposed deposits' count times the smallest of their caps (base
// estimateDepositsSweepFee with perDepositMaxFee the minimum, D19). At 10
// sat/vB the buffered estimate is above three times the smallest cap here,
// so the fee is clamped to it.
func TestFrostDepositSweepFeeWithinEveryCap(t *testing.T) {
	for _, order := range []string{"smallest cap first", "smallest cap last"} {
		t.Run(order, func(t *testing.T) {
			w := newSweepGapsWorld(t)
			low := w.keys[0]
			if order == "smallest cap last" {
				low = w.keys[2]
			}
			r := w.record(low)
			r.MaximumSweepFee = 1_700
			w.setRecord(low, r)
			for blocks := uint32(1); blocks <= 32; blocks++ {
				w.btc.SetEstimateSatPerVByteFee(blocks, 10)
			}
			proposal, ok, err := w.run()
			if err != nil || !ok || proposal == nil {
				t.Fatalf("no proposal (ok %v): %v", ok, err)
			}
			n := int64(len(proposal.DepositsKeys))
			if fee := proposal.SweepTxFee.Int64(); fee <= 0 || fee > n*1_700 {
				t.Fatalf("fee %d for %d deposits, expected positive and at most %d (the smallest cap per deposit)", fee, n, n*1_700)
			}
		})
	}
}

// TestFrostDepositSweepRefusesNonFrost: a host or Bitcoin chain without the
// FROST seams, or a wallet the host does not resolve as FROST, is an error.
func TestFrostDepositSweepRefusesNonFrost(t *testing.T) {
	w := newSweepGapsWorld(t)
	for name, run := range map[string]func() (*tbtc.DepositSweepProposal, bool, error){
		"host without FROST seams": func() (*tbtc.DepositSweepProposal, bool, error) {
			return w.runOn(gPlainHost{LocalChain: tbtcpg.NewLocalChain()}, w.btc)
		},
		"Bitcoin without a script query": func() (*tbtc.DepositSweepProposal, bool, error) {
			return w.runOn(w.host, w.btc.LocalBitcoinChain)
		},
		"wallet not FROST": func() (*tbtc.DepositSweepProposal, bool, error) {
			w.host.frost = false
			defer func() { w.host.frost = true }()
			return w.run()
		},
	} {
		t.Run(name, func(t *testing.T) {
			proposal, ok, err := run()
			if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
				t.Fatalf("%s: error %v, ok %v, proposal %v; expected an error and no proposal", name, err, ok, proposal != nil)
			}
		})
	}
}

// TestFrostDepositSweepReadFaults: every failed read is an error and no
// proposal, except a failed funding-confirmations read, which (as base) only
// leaves that deposit out.
func TestFrostDepositSweepReadFaults(t *testing.T) {
	for n := 1; n < 500; n++ {
		w := newSweepGapsWorld(t)
		w.f.failAt = n
		proposal, ok, err := w.run()
		if !w.f.fired {
			if n == 1 {
				t.Fatal("fixture: no read was made")
			}
			return
		}
		if w.f.firedOp == "confirmations" {
			if err != nil || !ok || proposal == nil || len(proposal.DepositsKeys) != 2 {
				t.Fatalf("read %d (confirmations) failed: ok %v, error %v; expected the other two deposits proposed", n, ok, err)
			}
			continue
		}
		if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
			t.Fatalf("read %d (%s) failed: error %v, ok %v, proposal %v; expected an error and no proposal", n, w.f.firedOp, err, ok, proposal != nil)
		}
	}
	t.Fatal("fixture: reads never ended")
}
