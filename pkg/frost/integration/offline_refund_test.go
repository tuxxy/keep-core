package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// TestFrostLifecycleRefundWithSignersOffline makes operator signing resources
// unavailable after public reveal. Only the depositor tool, historical chain
// construction and Core remain available; no host planner or signer is called.
func TestFrostLifecycleRefundWithSignersOffline(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	locktime := w.locktime(2 * k06RefundMargin)
	d := w.deposit(1_000_000, locktime, [20]byte{})
	before := w.balance(w.adminAddr)
	for i, node := range w.f.nodes {
		for _, peer := range node.network.ConnectionManager().ConnectedPeers() {
			node.network.ConnectionManager().DisconnectPeer(peer)
		}
		if err := w.f.roots[i].Close(); err != nil {
			t.Fatal("stop operator store:", err)
		}
		if err := os.Remove(w.f.workers[i]); err != nil {
			t.Fatal("remove operator worker executable:", err)
		}
		if _, err := w.journals[i].LoadKey(ctx); err == nil {
			t.Fatal("offline operator key remains loadable")
		}
		if _, err := os.Stat(w.f.workers[i]); !os.IsNotExist(err) {
			t.Fatal("offline operator executable remains available", err)
		}
	}
	// Construct the refund AFTER operator resources are unavailable, from the
	// depositor key and the public historical refund construction alone.
	tx := w.refund(d)
	if err := w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err == nil || !strings.Contains(err.Error(), "non-final") {
		t.Fatal("refund before locktime:", err)
	}
	w.moveBitcoinTime(uint64(locktime) + 1)
	if err := w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err != nil {
		t.Fatal("refund with signers offline:", err)
	}
	w.coreMine(1)
	if unspent, err := w.core.Unspent(ctx, d.funding); err != nil || unspent {
		t.Fatal("refund did not consume the revealed deposit", err)
	}
	if w.launchTotal() != 0 || w.balance(w.adminAddr) != before {
		t.Fatal("offline refund invoked a worker or created Bank credit")
	}
	record, err := w.chain.Deposit(ctx, w.head(), d.key)
	if err != nil || record.SweptAt != 0 {
		t.Fatal("offline refund was marked as a sweep", err)
	}
	t.Log("refund constructed and mined with closed operator stores and removed workers")
}
