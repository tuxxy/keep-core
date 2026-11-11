package tbtcpg_test

import (
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Mutation batch M8 (MovedFundsSweepTask, FROST branch). Fixtures are in
// frost_gaps_fixture_test.go.
//
// Invariant (KC-SF-V3 K-07; runFrost doc; base Run): a FROST wallet unknown
// to the legacy registry proposes the sweep of the first pending incoming
// output the Bridge recorded within the base look-back period, with a fee
// the on-chain validation accepts. With no pending output it reports no
// result (ok false). A wallet that is neither legacy nor FROST, or a failed
// read, is an error; it is refused, never a crash.

type movedGapsWorld struct {
	f       *gFaults
	host    *gHost
	btc     *gBitcoin
	outputs []bitcoin.TransactionOutpoint
}

// newMovedGapsWorld: three recorded incoming outputs; the first is no longer
// pending, the second and third are.
func newMovedGapsWorld(t *testing.T) *movedGapsWorld {
	t.Helper()
	w := &movedGapsWorld{f: &gFaults{}}
	w.host = newGHost(w.f, newGBridge(w.f))
	w.btc = newGBitcoin(w.f)
	for i := 0; i < 3; i++ {
		o := bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x1c, byte(i)}, OutputIndex: uint32(i)}
		w.outputs = append(w.outputs, o)
		w.host.incoming = append(w.host.incoming, o)
		w.host.incomingBlock[o] = w.host.block - 100 + uint64(i)
		w.host.incomingPending[o] = i > 0
	}
	w.host.validated = &tbtc.MovedFundsSweepProposal{MovingFundsTxHash: w.outputs[1].TransactionHash, MovingFundsTxOutputIndex: w.outputs[1].OutputIndex}
	return w
}

func (w *movedGapsWorld) runOn(host tbtcpg.Chain) (*tbtc.MovedFundsSweepProposal, bool, error) {
	proposal, ok, err := recoverRun(tbtcpg.NewMovedFundsSweepTask(host, w.btc), &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.host.label,
		FrostWallet:         true,
	})
	if proposal == nil {
		return nil, ok, err
	}
	p, isSweep := proposal.(*tbtc.MovedFundsSweepProposal)
	if !isSweep {
		return nil, ok, errors.New("fixture: not a moved funds sweep proposal")
	}
	return p, ok, err
}

func TestFrostMovedFundsSweepProposal(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *movedGapsWorld)
		want  int // index into outputs; -1: no result
	}{
		{"the first pending incoming output", func(w *movedGapsWorld) {}, 1},
		{"with a main", func(w *movedGapsWorld) { w.host.hasMain = true }, 1},
		{"outputs far beyond the look-back start are still found", func(w *movedGapsWorld) {
			w.host.block += tbtcpg.MovedFundsSweepLookBackBlocks + 300_000
			for o := range w.host.incomingBlock {
				w.host.incomingBlock[o] += tbtcpg.MovedFundsSweepLookBackBlocks + 300_000
			}
		}, 1},
		{"no pending incoming output", func(w *movedGapsWorld) {
			for o := range w.host.incomingPending {
				w.host.incomingPending[o] = false
			}
		}, -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newMovedGapsWorld(t)
			test.setup(w)
			proposal, ok, err := w.runOn(w.host)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if test.want < 0 {
				if ok || proposal != nil {
					t.Fatalf("%s: proposed (ok %v), expected no result", test.name, ok)
				}
				return
			}
			if !ok || proposal == nil {
				t.Fatalf("%s: no proposal (ok %v), expected one", test.name, ok)
			}
			if o := w.outputs[test.want]; proposal.MovingFundsTxHash != o.TransactionHash || proposal.MovingFundsTxOutputIndex != o.OutputIndex || proposal.SweepTxFee.Sign() <= 0 {
				t.Fatalf("%s: proposed %x:%d fee %v, expected %x:%d with a positive fee", test.name, proposal.MovingFundsTxHash, proposal.MovingFundsTxOutputIndex, proposal.SweepTxFee, o.TransactionHash, o.OutputIndex)
			}
		})
	}
}

// TestFrostMovedFundsSweepRefusesUnknownWallet: a wallet the legacy registry
// does not know and the host does not resolve as FROST, or a host without
// the FROST incoming seam, is refused with an error, not a crash.
func TestFrostMovedFundsSweepRefusesUnknownWallet(t *testing.T) {
	for name, host := range map[string]func(w *movedGapsWorld) tbtcpg.Chain{
		"wallet not FROST":                 func(w *movedGapsWorld) tbtcpg.Chain { w.host.frost = false; return w.host },
		"host without the FROST incomings": func(w *movedGapsWorld) tbtcpg.Chain { return gPlainHost{LocalChain: tbtcpg.NewLocalChain()} },
	} {
		t.Run(name, func(t *testing.T) {
			w := newMovedGapsWorld(t)
			proposal, ok, err := w.runOn(host(w))
			if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
				t.Fatalf("%s: error %v, ok %v, proposal %v; expected a refusal and no proposal", name, err, ok, proposal != nil)
			}
		})
	}
}

// TestFrostMovedFundsSweepReadFaults: every failed read is an error and no
// proposal.
func TestFrostMovedFundsSweepReadFaults(t *testing.T) {
	for n := 1; n < 200; n++ {
		w := newMovedGapsWorld(t)
		w.f.failAt = n
		proposal, ok, err := w.runOn(w.host)
		if !w.f.fired {
			if n == 1 {
				t.Fatal("fixture: no read was made")
			}
			return
		}
		if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
			t.Fatalf("read %d (%s) failed: error %v, ok %v, proposal %v; expected an error and no proposal", n, w.f.firedOp, err, ok, proposal != nil)
		}
	}
	t.Fatal("fixture: reads never ended")
}
