package tbtcpg_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Mutation batch M8 (RedemptionTask, FROST branch, and
// ProposeFrostRedemption). Fixtures are in frost_gaps_fixture_test.go.
//
// Invariant (KC-SF-V3 K-05; runFrost and findPendingFrostRedemptions docs;
// base FindPendingRedemptions): a wallet holding an unresolved reservation
// proposes that reservation's requests again; otherwise the leader proposes
// the requests the members' own planner selects at the current block, in
// request order, at most the redemption max size of them (0 meaning the
// K-05 cap, as the planner), with a fee within the Bridge's total fee cap.
// With no request it reports no result (ok false); a failed read or a wallet
// the host does not resolve as FROST is an error.

type redemptionGapsWorld struct {
	f    *gFaults
	host *gHost
	btc  *gBitcoin
	// scripts of the requests in request order.
	scripts []bitcoin.Script
}

// gDescriptor is a FIXTURE descriptor of the host's wallet.
func gDescriptor(outputKey [32]byte) dkg.Descriptor {
	return dkg.Descriptor{Scheme: 1, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address{0x01}, Members: []uint32{1, 2, 3}, Operators: []common.Address{{0x0a}, {0x0b}, {0x0c}}, Threshold: 2, OutputKey: outputKey}
}

// newRedemptionGapsWorld: three redeemable requests of the host's FROST
// wallet, requested at blocks 900..902.
func newRedemptionGapsWorld(t *testing.T, requests int) *redemptionGapsWorld {
	t.Helper()
	w := &redemptionGapsWorld{f: &gFaults{}}
	w.host = newGHost(w.f, newGBridge(w.f))
	w.host.redemptionMaxSize = 5
	w.btc = newGBitcoin(w.f)
	descriptor := gDescriptor(w.host.outputKey)
	descriptorHash, err := dkg.DescriptorHash(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	b := w.host.bridge
	b.wallets[movement.Label(w.host.walletID())] = movement.Wallet{Descriptor: descriptor, ID: w.host.walletID(), DescriptorHash: descriptorHash}
	for i := 0; i < requests; i++ {
		id := [32]byte{0x0e, byte(i + 1)}
		script := bitcoin.Script(append([]byte{0x00, 0x14}, bytesOf(20, byte(0x40+i))...))
		w.scripts = append(w.scripts, script)
		b.events = append(b.events, redemption.RequestEvent{RequestID: id, WalletID: w.host.walletID(), Block: w.host.block - 100 + uint64(i)})
		b.requests[id] = redemption.Request{
			ID: id, WalletID: w.host.walletID(), DescriptorHash: descriptorHash, State: redemption.RequestRequested,
			Deadline: b.now + 7*24*3600, Script: script, Gross: 1_000_000,
		}
	}
	return w
}

func bytesOf(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// shiftBlocks moves the host's current block and every request event by d.
func (w *redemptionGapsWorld) shiftBlocks(d uint64) {
	w.host.block += d
	for i := range w.host.bridge.events {
		w.host.bridge.events[i].Block += d
	}
}

func (w *redemptionGapsWorld) runOn(host tbtcpg.Chain) (*tbtc.RedemptionProposal, bool, error) {
	proposal, ok, err := recoverRun(tbtcpg.NewRedemptionTask(host, w.btc), &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.host.label,
		FrostWallet:         true,
	})
	if proposal == nil {
		return nil, ok, err
	}
	r, isRedemption := proposal.(*tbtc.RedemptionProposal)
	if !isRedemption {
		return nil, ok, errors.New("fixture: not a redemption proposal")
	}
	return r, ok, err
}

func sameScripts(a, b []bitcoin.Script) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if string(a[i]) != string(b[i]) {
			return false
		}
	}
	return true
}

func TestFrostRedemptionProposal(t *testing.T) {
	reserved := []bitcoin.Script{bitcoin.Script(append([]byte{0x00, 0x14}, bytesOf(20, 0x77)...))}
	tests := []struct {
		name    string
		setup   func(w *redemptionGapsWorld)
		want    func(w *redemptionGapsWorld) []bitcoin.Script // nil: no result
		zeroFee bool
	}{
		{"pending requests in request order", func(w *redemptionGapsWorld) {}, func(w *redemptionGapsWorld) []bitcoin.Script { return w.scripts }, false},
		{"max size caps the requests", func(w *redemptionGapsWorld) { w.host.redemptionMaxSize = 2 }, func(w *redemptionGapsWorld) []bitcoin.Script { return w.scripts[:2] }, false},
		{"max size 0 means the K-05 cap", func(w *redemptionGapsWorld) { w.host.redemptionMaxSize = 0 }, func(w *redemptionGapsWorld) []bitcoin.Script { return w.scripts }, false},
		{"requests far beyond the look-back start are still found", func(w *redemptionGapsWorld) {
			w.shiftBlocks(tbtcpg.DepositSweepLookBackBlocks + 300_000)
		}, func(w *redemptionGapsWorld) []bitcoin.Script { return w.scripts }, false},
		{"an unresolved reservation is proposed again", func(w *redemptionGapsWorld) {
			w.host.reservedScripts, w.host.reservedHeld = reserved, true
		}, func(w *redemptionGapsWorld) []bitcoin.Script { return reserved }, true},
		{"no pending requests", func(w *redemptionGapsWorld) {
			w.host.bridge.events = nil
		}, nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newRedemptionGapsWorld(t, 3)
			test.setup(w)
			proposal, ok, err := w.runOn(w.host)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if test.want == nil {
				if ok || proposal != nil {
					t.Fatalf("%s: proposed (ok %v), expected no result", test.name, ok)
				}
				return
			}
			if !ok || proposal == nil {
				t.Fatalf("%s: no proposal (ok %v), expected one", test.name, ok)
			}
			if want := test.want(w); !sameScripts(proposal.RedeemersOutputScripts, want) {
				t.Fatalf("%s: proposed %d scripts %x, expected %d %x", test.name, len(proposal.RedeemersOutputScripts), proposal.RedeemersOutputScripts, len(want), want)
			}
			if test.zeroFee != (proposal.RedemptionTxFee.Sign() == 0) || proposal.RedemptionTxFee.Sign() < 0 {
				t.Fatalf("%s: fee %v, expected zero %v", test.name, proposal.RedemptionTxFee, test.zeroFee)
			}
		})
	}
}

// TestFrostRedemptionRefusesNonFrost: a host without the FROST request seam
// or Bridge binding, or a wallet the host does not resolve as FROST (also
// when the registry answer changes between the two reads), is an error.
func TestFrostRedemptionRefusesNonFrost(t *testing.T) {
	cases := map[string]func(w *redemptionGapsWorld) tbtcpg.Chain{
		"host without FROST seams": func(w *redemptionGapsWorld) tbtcpg.Chain { return gPlainHost{LocalChain: tbtcpg.NewLocalChain()} },
		"host without the Bridge": func(w *redemptionGapsWorld) tbtcpg.Chain {
			return gHostNoBridge{LocalChain: tbtcpg.NewLocalChain(), h: w.host}
		},
		"wallet not FROST":              func(w *redemptionGapsWorld) tbtcpg.Chain { w.host.frost = false; return w.host },
		"wallet not FROST at selection": func(w *redemptionGapsWorld) tbtcpg.Chain { w.host.frostAnswers = []bool{true, false}; return w.host },
		// A host that binds the Bridge but cannot read reservations could
		// not tell a held reservation from none.
		"host without the reservation read": func(w *redemptionGapsWorld) tbtcpg.Chain {
			return gHostNoRequests{LocalChain: tbtcpg.NewLocalChain(), h: w.host}
		},
		"wallet not FROST at the reservation read": func(w *redemptionGapsWorld) tbtcpg.Chain { w.host.frostAnswers = []bool{false, true}; return w.host },
	}
	for name, host := range cases {
		t.Run(name, func(t *testing.T) {
			w := newRedemptionGapsWorld(t, 3)
			proposal, ok, err := w.runOn(host(w))
			if err == nil || errors.Is(err, errGapsPanic) || proposal != nil {
				t.Fatalf("%s: error %v, ok %v, proposal %v; expected an error and no proposal", name, err, ok, proposal != nil)
			}
		})
	}
}

// TestFrostRedemptionReadFaults: every failed read is an error and no
// proposal.
func TestFrostRedemptionReadFaults(t *testing.T) {
	for n := 1; n < 500; n++ {
		w := newRedemptionGapsWorld(t, 3)
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

// Invariant (ProposeFrostRedemption doc): a plan is built only from at least
// one pending request, and then it is the planner's Build of exactly those
// requests; a failed selection is returned, never built on. The planner here
// has no fee domain, so Build refuses with redemption.ErrFeeDomain: seeing
// that sentinel shows the requests reached Build. This relies on Build's
// check order: it refuses 0 or more than MaxRedemptionRequests ids
// (ErrNotRedeemable) before it checks the fee domain, so ErrFeeDomain proves
// that a non-empty, in-range list reached it. A reorder of those checks would
// fail this test, never pass it falsely. ProposeFrostRedemption has no
// production caller today (only the integration test calls it).
func TestProposeFrostRedemptionNeedsPendingRequests(t *testing.T) {
	for name, test := range map[string]struct {
		setup      func(w *redemptionGapsWorld)
		reachBuild bool
	}{
		"pending requests are built": {func(w *redemptionGapsWorld) {}, true},
		"no pending requests":        {func(w *redemptionGapsWorld) { w.host.bridge.events = nil }, false},
		"a failed selection":         {func(w *redemptionGapsWorld) { w.f.failAt = 1 }, false},
	} {
		t.Run(name, func(t *testing.T) {
			w := newRedemptionGapsWorld(t, 2)
			planner := redemption.Planner{Chain: w.host.bridge, Wallet: gDescriptor(w.host.outputKey)}
			test.setup(w)
			plan, requests, err := tbtcpg.ProposeFrostRedemption(context.Background(), planner, 0, w.host.block, 0)
			if err == nil || plan != nil || requests != nil {
				t.Fatalf("%s: error %v, plan %v, %d requests; expected an error and no plan", name, err, plan != nil, len(requests))
			}
			if reached := errors.Is(err, redemption.ErrFeeDomain); reached != test.reachBuild {
				t.Fatalf("%s: error %v; reached Build %v, expected %v", name, err, reached, test.reachBuild)
			}
		})
	}
}
