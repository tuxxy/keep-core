package tbtc

// Acceptance tests for tuxxy/keep-core#151 (REVIEW-FIX-B12 N2, P3), e2e
// part: one real SNOWFALL worker process. Refs tuxxy/keep-core#151
//
// Invariant (see frost_node_b17b_claim_test.go): after a successful DKG the
// FROST epoch claim is never released. A release would make the next block
// read the stored key and start a second restore (key reload in a fresh
// worker) against the journal of a live wallet (REVIEW-FIX-B12 section 3).
// And after the journal claim, an error never reruns the DKG, here with the
// completed key already stored.
//
// Real code: frostNode.joinDKGIfEligible and executeDKG, ExecuteFrostDKG ->
// dkg.Executor.Run end to end (prepare, candidate acceptance, finality
// re-read, key reload, readiness collection and certificate, postcondition,
// ReadyUnfunded save), the real guarded engine with the pinned real worker
// (SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256) holding both seats, the real
// attempt transport, attestation bus and encrypted store, and addWallet.
//
// FIXTURES (labelled; frost_node_b17b_claim_test.go): b17bRegistry models
// the registry (Submit approves at once, Ready certifies), the provider
// delivers nothing (the operator holds every seat, so no remote message is
// needed), the host chain gives the operator key, the fence is in memory.
// The DKG success itself is real: the worker runs it and Run reaches
// ReadyUnfunded against the fixture registry.

import (
	"context"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// TestFrostNodeB17bSuccessfulDKGKeepsEpochClaim (e2e): after executeDKG
// succeeds, later blocks start no restore and no executor for the epoch.
func TestFrostNodeB17bSuccessfulDKGKeepsEpochClaim(t *testing.T) {
	t.Parallel()
	w := newB17bRealWorkerWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.fn.joinDKGIfEligible(ctx, b17bEpoch)
	if !b17bEventually(8*time.Second, w.firstAttemptReturned) {
		t.Fatalf("harness: the DKG never returned (%s)", w.counts())
	}
	w.requireSuccess(ctx, t)

	w.blocksFor(ctx, 1600*time.Millisecond)
	if n := w.registry.restoreStarts(); n != 0 {
		t.Errorf("%s: after a successful DKG, later blocks started %d restores of epoch %d's live wallet, want 0: the epoch claim was released after success (%s)",
			b17bInvariant, n, b17bEpoch, w.counts())
	}
	if n := w.registry.executorStarts(); n != 1 {
		t.Errorf("%s: after a successful DKG, the epoch's DKG executor started %d times, want exactly 1 (%s)",
			b17bInvariant, n, w.counts())
	}
}

// TestFrostNodeB17bKeyStoredErrorNeverRerunsDKG (e2e): Run fails after the
// worker stored the completed key (the readiness digest read fails). Whether
// or not the claim is released, later blocks never start another executor.
func TestFrostNodeB17bKeyStoredErrorNeverRerunsDKG(t *testing.T) {
	t.Parallel()
	w := newB17bRealWorkerWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.registry.failReadinessDigest(1)
	w.fn.joinDKGIfEligible(ctx, b17bEpoch)
	if !b17bEventually(8*time.Second, w.firstAttemptReturned) {
		t.Fatalf("harness: the DKG never returned (%s)", w.counts())
	}
	if !w.registry.readinessFaultHit() {
		t.Fatalf("harness: the readiness digest fault was never hit (%s)", w.counts())
	}
	journal, err := w.fn.scope(b17bEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := journal.DKGStatus(ctx); err != nil || status.State != frost.DKGKeyStored {
		t.Fatalf("harness: the completed key is not stored (state %v, %v)", status.State, err)
	}
	if w.walletCount() != 0 {
		t.Fatalf("harness: the failed DKG added a wallet")
	}

	w.blocksFor(ctx, 1600*time.Millisecond)
	if n := w.registry.executorStarts(); n != 1 {
		t.Errorf("%s: after Run failed with the key stored, the epoch's DKG executor started %d times, want exactly 1 (%s)",
			b17bInvariant, n, w.counts())
	}
}

// newB17bRealWorkerWorld is the both-seats world with the pinned real worker.
func newB17bRealWorkerWorld(t *testing.T) *b17bWorld {
	t.Helper()
	path, pin := os.Getenv("SNOWFALL_WORKER"), os.Getenv("SNOWFALL_WORKER_SHA256")
	if path == "" || pin == "" {
		t.Skip("batch 17b e2e: run through test/remediation-b17b/run.sh (the pinned real worker: SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256)")
	}
	raw, err := hex.DecodeString(pin)
	if err != nil || len(raw) != 32 {
		t.Fatalf("harness: SNOWFALL_WORKER_SHA256 is not a 32-byte hex digest")
	}
	var digest [32]byte
	copy(digest[:], raw)
	w := newB17bWorld(t, b17bBothSeatsWorld)
	w.fn.config.Worker = snowfallengine.WorkerConfig{Path: path, SHA256: digest}
	return w
}

// requireSuccess checks that executeDKG succeeded: the saved wallet is
// ReadyUnfunded and unquarantined, the registry certified it, and addWallet
// coordinated it.
func (w *b17bWorld) requireSuccess(ctx context.Context, t *testing.T) {
	t.Helper()
	journal, err := w.fn.scope(b17bEpoch)
	if err != nil {
		t.Fatal(err)
	}
	record, err := journal.LoadWallet(ctx)
	if err != nil || record.State != "ReadyUnfunded" || record.Quarantined {
		t.Fatalf("harness: the DKG did not succeed: saved wallet state %q quarantined %v (%v) (%s)", record.State, record.Quarantined, err, w.counts())
	}
	view, _ := w.registry.View(ctx, b17bEpoch)
	if view.Approved.State != 2 {
		t.Fatalf("harness: the registry did not certify the wallet (state %d)", view.Approved.State)
	}
	w.fn.mutex.Lock()
	_, added := w.fn.wallets[record.ID]
	w.fn.mutex.Unlock()
	if !added {
		t.Fatalf("harness: executeDKG returned without adding the ready wallet (%s)", w.counts())
	}
}

func (w *b17bWorld) walletCount() int {
	w.fn.mutex.Lock()
	defer w.fn.mutex.Unlock()
	return len(w.fn.wallets)
}
