package tbtc_test

// Remediation batch 6 acceptance tests: FROST duties dispatched by the base
// node. Refs tuxxy/keep-core#26, #38, #39; DECISIONS.md "D5 revised", D2, D19.
// World and observation: frost_node_b6_world_test.go. Entry: the seam in
// frost_node_b6_seam_test.go. No build tag.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// b6Ready bounds a node-run FROST DKG to ReadyUnfunded. Alone it takes about
// 3-4 s; the bound is generous because the machine is shared (it only sets
// how long a failing case waits).
const b6Ready = 20 * time.Second

// TestFrostNodeDispatch_DKG (#26): with FROST configured, the nodes alone take
// a requested FROST wallet to ReadyUnfunded; the test calls no DKG function.
// Multi-seat operators (1,1,2) run every seat they hold.
func TestFrostNodeDispatch_DKG(t *testing.T) {
	t.Parallel()
	for name, pattern := range map[string][]int{"1,2,3": {1, 2, 3}, "1,1,2": {1, 1, 2}} {
		pattern := pattern
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newFrostNodeWorld(t)
			w.startNodes()
			epoch := w.requestWallet(pattern)
			if _, ok := w.readyWallet(epoch, b6Ready); !ok {
				t.Fatalf("#26 DKG: epoch %d's FROST wallet did not reach ReadyUnfunded within %v: no node ran the FROST DKG and readiness duty (exited: %v)%s", epoch, b6Ready, w.exitedNodes(), w.debugLogs())
			}
		})
	}
}

// b6Sweep bounds the nodes' incoming sweep after the window's coordination
// block: the 80-block active phase at the miner's 20 ms (1.6 s), plus one
// FROST signing; generous for the same reason as b6Ready.
const b6Sweep = 15 * time.Second

// loadB6Ready loads a copy of the stage in which the nodes made one FROST
// wallet with the given seat pattern (frost_node_b6_snapshot_test.go), without
// starting the nodes, so a case can position the chain first. The stage's
// build is the #26 DKG duty; a failure there is reported as such.
func loadB6Ready(t *testing.T, pattern []int) (*frostNodeWorld, b6Wallet) {
	t.Helper()
	if frostNodeStart == nil {
		t.Fatalf("#26 %s: frostNodeStart is unbound: no base node initialisation with FROST configured", t.Name())
	}
	w := loadFrostNodeWorld(t, b6StageReady(pattern))
	return w, *w.stageWallet
}

// newB6ReadyWorld is loadB6Ready with the nodes started, each on its restored
// store.
func newB6ReadyWorld(t *testing.T, pattern []int) (*frostNodeWorld, b6Wallet) {
	t.Helper()
	w, wallet := loadB6Ready(t, pattern)
	w.startNodes()
	return w, wallet
}

// operatorIndex maps an operator address to its 1-based index.
func (w *frostNodeWorld) operatorIndex(address common.Address) int {
	for i, key := range w.keys {
		if crypto.PubkeyToAddress(key.PublicKey) == address {
			return i + 1
		}
	}
	return 0
}

// coordination returns, per window, the operators that sent a base
// coordination message for the wallet, and the actions they proposed.
func (w *frostNodeWorld) coordination(wallet b6Wallet) (map[uint64]map[int]bool, map[uint64]map[string]bool, map[uint64]time.Time) {
	senders := map[uint64]map[int]bool{}
	actions := map[uint64]map[string]bool{}
	first := map[uint64]time.Time{}
	label := hex.EncodeToString(wallet.label[:])
	for _, e := range w.events() {
		if e.Kind != "net" || e.Window == 0 || e.Wallet != label {
			continue
		}
		if senders[e.Window] == nil {
			senders[e.Window], actions[e.Window] = map[int]bool{}, map[string]bool{}
			first[e.Window] = e.At
		}
		senders[e.Window][e.Operator] = true
		actions[e.Window][e.Action] = true
		if e.At.Before(first[e.Window]) {
			first[e.Window] = e.At
		}
	}
	return senders, actions, first
}

// nextSweepWindow mines to the next coordination window whose checklist has
// the moved-funds sweep: before DepositSweepEveryWindowActivationBlock, base
// checks it every fourth window (index % 4 == 0).
func (w *frostNodeWorld) nextSweepWindow() uint64 {
	w.t.Helper()
	return w.jumpToWindow(4 * tbtc.B6CoordinationFrequencyBlocks)
}

// assertLeaders checks D2 in every window the nodes coordinated the wallet
// in: only base's leader (unique operator, seeded by the wallet's 20-byte
// Bridge key and the safe block hash) sent the wallet's coordination message.
// It returns the distinct leaders seen.
func (w *frostNodeWorld) assertLeaders(t *testing.T, wallet b6Wallet, senders map[uint64]map[int]bool) map[int]bool {
	t.Helper()
	leaders := map[int]bool{}
	for window, set := range senders {
		expected := w.operatorIndex(w.expectedLeader(wallet, window))
		var got []int
		for operator := range set {
			got = append(got, operator)
		}
		if len(got) != 1 || got[0] != expected {
			t.Fatalf("D2 window %d: the FROST wallet's coordination message was sent by operator(s) %v; base's leader by unique operator is operator %d", window, got, expected)
		}
		leaders[expected] = true
	}
	return leaders
}

// seatLeader is the wrong rule D2 excludes: base getLeader without the
// operator deduplication, so an operator with more seats leads more often
// (review B5). It is computed independently, as b6Leader is.
func (w *frostNodeWorld) seatLeader(wallet b6Wallet, window uint64) int {
	t := w.t
	t.Helper()
	header, err := w.admin.HeaderByNumber(context.Background(), new(big.Int).SetUint64(window-tbtc.B6CoordinationSafeBlockShift))
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256(append(append([]byte{}, wallet.label[:]...), header.Hash().Bytes()...))
	var seats []chain.Address
	for _, o := range wallet.operators {
		seats = append(seats, chain.Address(o.Hex()))
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i] < seats[j] })
	rng := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(seed[:8]))))
	rng.Shuffle(len(seats), func(i, j int) { seats[i], seats[j] = seats[j], seats[i] })
	return w.operatorIndex(common.HexToAddress(string(seats[0])))
}

// TestFrostNodeDispatch_IncomingSweepByLeader (#26, leader rule D2): a pending
// incoming output of a node-made FROST wallet (seats 1,1,2: operator 1 holds
// two seats) is swept by the nodes in a base coordination window whose
// checklist has the moved-funds sweep, never before that window's
// coordination message; in every window the nodes coordinated, the wallet's
// coordination message comes only from base's leader by unique operator. The
// sweep window is one in which a seat-weighted leader would differ from the
// D2 leader (review B5).
func TestFrostNodeDispatch_IncomingSweepByLeader(t *testing.T) {
	t.Parallel()
	w, wallet := loadB6Ready(t, []int{1, 1, 2})
	// The sweep window is one in which a seat-weighted leader would differ
	// from D2's (review B5). It is chosen from its seed block before the
	// drain exists and before the nodes start, so a skipped window has
	// nothing to sweep.
	window := w.chooseWindow(4*tbtc.B6CoordinationFrequencyBlocks, func(window uint64) bool {
		return w.seatLeader(wallet, window) != w.operatorIndex(w.expectedLeader(wallet, window))
	})
	drain := w.drainInto(wallet)
	outpoint := bitcoin.TransactionOutpoint{TransactionHash: drain.Hash(), OutputIndex: 0}
	w.startNodes()
	w.enterWindow(window)
	if spenders := w.spenders(outpoint); len(spenders) > 0 {
		t.Fatalf("#26 incoming sweep: a node broadcast sweep %v before coordination window %d: the duty is not dispatched from base coordination windows", spenders, window)
	}
	sweep, ok := w.settleIncoming(wallet, drain, b6Sweep)
	if !ok {
		t.Fatalf("#26 incoming sweep: no node swept the FROST wallet's pending incoming output in coordination window %d (exited: %v)%s", window, w.exitedNodes(), w.debugLogs())
	}
	senders, actions, first := w.coordination(wallet)
	if len(senders[window]) == 0 {
		t.Fatalf("#26 incoming sweep: sweep %s was broadcast, but no node sent a base coordination message for the FROST wallet in window %d: the sweep was not coordinated on base windows", sweep, window)
	}
	if !actions[window]["MovedFundsSweep"] {
		t.Fatalf("#26 incoming sweep: window %d's coordination message proposed %v, not the moved-funds sweep that was broadcast", window, actions[window])
	}
	var broadcastAt time.Time
	for _, e := range w.events() {
		if e.Kind == "btc" && e.TxHash == sweep && (broadcastAt.IsZero() || e.At.Before(broadcastAt)) {
			broadcastAt = e.At
		}
	}
	if broadcastAt.Before(first[window]) {
		t.Fatalf("#26 incoming sweep: sweep %s was broadcast at %v, before window %d's coordination message at %v", sweep, broadcastAt, window, first[window])
	}
	w.assertLeaders(t, wallet, senders)
	if w.seatLeader(wallet, window) == w.operatorIndex(w.expectedLeader(wallet, window)) {
		t.Fatalf("setup: window %d does not tell a seat-weighted leader from D2's", window)
	}
}

// TestFrostNodeDispatch_RestoreAtStartup (#26, DECISIONS.md "Key restore after
// a restart": at node startup, as in base): after every node process is
// killed and restarted on its storage, the wallet signs again with no new
// DKG. The oracle is Bitcoin's: Core accepts a sweep only if it is signed by
// the wallet's output key.
func TestFrostNodeDispatch_RestoreAtStartup(t *testing.T) {
	t.Parallel()
	w, wallet := newB6ReadyWorld(t, []int{1, 1, 2})
	w.killAll()
	window := w.chooseWindow(4*tbtc.B6CoordinationFrequencyBlocks, nil)
	drain := w.drainInto(wallet)
	w.startNodes()
	w.enterWindow(window)
	if _, ok := w.settleIncoming(wallet, drain, b6Sweep); !ok {
		t.Fatalf("#26 restore: after every node restarted, the wallet's pending incoming output was not swept in window %d: the FROST signers were not restored at node startup (or the sweep is not dispatched at all: see IncomingSweepByLeader) (exited: %v)%s", window, w.exitedNodes(), w.debugLogs())
	}
}
