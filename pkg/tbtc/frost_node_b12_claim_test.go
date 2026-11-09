package tbtc

// Acceptance tests for tuxxy/keep-core#140 (FSM analysis C-2, P3).
// Refs tuxxy/keep-core#140
//
// Invariant: the DKG loop claims an epoch before it joins the epoch's DKG.
// A setup error before the DKG executor starts (the parameters read, the
// worker start, the attempt transport, the attestation bus, or the store
// scope in joinDKGIfEligible) must release that claim, so a later block of
// the DKG loop tries the epoch again, as base keep-core's expiring
// deduplication (pkg/tbtc/deduplicator.go, cache.TimeCache) lets a stuck DKG
// start heal. A DKG or restore that started is never run twice for one epoch:
// while it runs, and after its executor started and failed, later blocks
// leave the epoch alone.
//
// Real code: frostNode.joinDKGIfEligible, restoreEpoch, restoreStatus and
// executeDKG, the real snowfallengine.NewGuarded, transport.New,
// dkg.NewNetworkBus and dkg.Executor.Run (through ExecuteFrostDKG), and the
// real encrypted store (pkg/frost/store).
//
// FIXTURES (labelled, test-only):
//   - b12Registry: the FROST registry adapter. The node's own selection reads
//     are answered at once. A chain read made under a protocol deadline
//     (Run and Restore require one) is the executor's: its first selection
//     read is the witness that the executor started, and the fixture stops
//     (or holds) the executor there, before any worker or journal write.
//   - b12Provider/b12Channel: the libp2p provider and broadcast channels; a
//     channel open fails once when the case arms it.
//   - b12HostChain/b12Signing: the host chain's operator address derivation
//     (go-ethereum addresses), which the membership validator uses.
//   - advFence (batch 7): the in-memory store fence.
//   - The worker config names a worker path and pin that are never executed:
//     every case stops the executor before Engine.DKG.
// The setup faults are transient (once) except the worker start, whose
// config is fixed for the process; there a later block must still try the
// epoch again (its parameters read is the witness).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/net"
)

const b12ClaimInvariant = "C-2 a setup error releases the FROST epoch claim; a started DKG never runs twice (tuxxy/keep-core#140)"

const b12Epoch = 100

type b12Setup string

const (
	b12SetupParameters b12Setup = "parameters-read"
	b12SetupWorker     b12Setup = "worker-start"
	b12SetupTransport  b12Setup = "transport-open"
	b12SetupBus        b12Setup = "bus-open"
	b12SetupScope      b12Setup = "store-scope"
)

// TestFrostNodeB12SetupErrorReleasesEpochClaim: after a setup error before
// the executor starts, a later block of the DKG loop joins the epoch again,
// and (once the transient error is gone) its executor starts exactly once.
func TestFrostNodeB12SetupErrorReleasesEpochClaim(t *testing.T) {
	for _, tc := range []struct {
		setup   b12Setup
		startup bool // the first attempt comes from the startup restore (restoreEpoch, AwaitingResult)
	}{
		{setup: b12SetupParameters},
		{setup: b12SetupWorker},
		{setup: b12SetupTransport},
		{setup: b12SetupBus},
		{setup: b12SetupScope},
		{setup: b12SetupParameters, startup: true},
	} {
		tc := tc
		name := string(tc.setup)
		if tc.startup {
			name = "startup-restore/" + name
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newB12World(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			switch tc.setup {
			case b12SetupParameters:
				w.registry.failParameters(1)
			case b12SetupWorker:
				// FIXTURE: a worker config the guarded engine refuses (no
				// absolute path), fixed for the process.
				w.fn.config.Worker = snowfallengine.WorkerConfig{Path: "b12-worker-not-absolute", SHA256: [32]byte{1}}
			case b12SetupTransport:
				w.provider.failOpen("frost-v1-", 1)
			case b12SetupBus:
				w.provider.failOpen("frost-chain-v1-", 1)
			case b12SetupScope:
				// FIXTURE: the store refuses the scope (invalid domain) on
				// the first block only.
				w.fn.network = ""
			}

			// The first attempt.
			if tc.startup {
				w.fn.restoreEpoch(ctx, b12Epoch)
			} else {
				w.fn.joinDKGIfEligible(ctx, b12Epoch)
			}
			if tc.setup == b12SetupScope {
				w.fn.network = b12Network
			}
			if !b12Eventually(time.Second, func() bool { return w.faulted(tc.setup) }) {
				t.Fatalf("harness: the %s fault was never hit (%s)", tc.setup, w.counts())
			}

			// Later blocks of the DKG loop.
			retried := w.blocksUntil(ctx, 1500*time.Millisecond, func() bool { return w.retried(tc.setup) })
			if !retried {
				t.Errorf("%s: after a %s error before the executor started, no later block joined epoch %d again: the epoch stayed claimed (%s)",
					b12ClaimInvariant, tc.setup, b12Epoch, w.counts())
				return
			}
			if tc.setup == b12SetupWorker {
				return // the worker config never becomes valid; the retry is the property
			}
			w.blocksFor(ctx, 300*time.Millisecond)
			if n := w.registry.executorStarts(); n != 1 {
				t.Errorf("%s: after a retried %s error, the epoch's DKG executor started %d times, want exactly 1 (%s)",
					b12ClaimInvariant, tc.setup, n, w.counts())
			}
		})
	}
}

// TestFrostNodeB12StartedDKGNeverRunsTwice is the negative side: once the
// executor started (or a restore started), later blocks never start another
// for the epoch, whether it is still running or failed. A fix that releases
// the claim on every executeDKG error, or never holds it, fails here.
func TestFrostNodeB12StartedDKGNeverRunsTwice(t *testing.T) {
	for _, mode := range []string{"executor-failed", "executor-running", "restore-running"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			w := newB12World(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			switch mode {
			case "executor-running":
				w.registry.holdExecutor()
			case "restore-running":
				w.persistKey(ctx, t)
				w.registry.holdRestore()
			}
			w.fn.joinDKGIfEligible(ctx, b12Epoch)
			started := func() bool {
				if mode == "restore-running" {
					return w.registry.restoreStarts() > 0
				}
				return w.registry.executorStarts() > 0
			}
			if !b12Eventually(time.Second, started) {
				t.Fatalf("harness: the %s never started (%s)", strings.SplitN(mode, "-", 2)[0], w.counts())
			}
			if mode == "executor-failed" {
				// Let the failed executor's goroutine return first.
				if !b12Eventually(time.Second, w.registry.executorReturned) {
					t.Fatalf("harness: the failed executor never returned (%s)", w.counts())
				}
			}
			w.blocksFor(ctx, 400*time.Millisecond)

			if n := w.registry.executorStarts(); mode != "restore-running" && n != 1 {
				t.Errorf("%s: the epoch's DKG executor started %d times (%s), want exactly 1 (%s)",
					b12ClaimInvariant, n, mode, w.counts())
			}
			if mode == "restore-running" {
				if n := w.registry.restoreStarts(); n != 1 {
					t.Errorf("%s: the epoch's restore started %d times while one was running, want exactly 1 (%s)",
						b12ClaimInvariant, n, w.counts())
				}
				if n := w.registry.executorStarts(); n != 0 {
					t.Errorf("%s: a DKG executor started %d times for an epoch with a stored key, want 0 (%s)",
						b12ClaimInvariant, n, w.counts())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// World: one operator holding seat 1 of a 2-seat epoch.

const b12Network = "B12/claim FIXTURE"

type b12World struct {
	fn       *frostNode
	registry *b12Registry
	provider *b12Provider
}

func newB12World(t *testing.T) *b12World {
	t.Helper()
	signer, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Open(context.Background(), store.Config{
		Keystore:  filepath.Join(t.TempDir(), "ks"),
		StorageID: [32]byte{0xb1, 2},
		Key:       [32]byte{0xb1, 2, 2},
		Fence:     &advFence{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	self := crypto.PubkeyToAddress(signer.PublicKey)
	registry := &b12Registry{
		selection: dkg.Selection{Members: []uint32{1, 2}, Operators: []common.Address{self, crypto.PubkeyToAddress(other.PublicKey)}},
		params:    dkg.Parameters{ChainID: big.NewInt(1), Registry: common.Address{9}, Pool: common.Address{3}, Bridge: common.Address{4}, Profile: 1, GroupSize: 2, Threshold: 2, ReadySeats: 2, ChallengeBlocks: 1},
		release:   make(chan struct{}),
	}
	provider := &b12Provider{fail: map[string]int{}}
	fn := &frostNode{
		node: &node{chain: b12HostChain{}, netProvider: provider},
		config: FrostConfig{
			Registry: [20]byte{9},
			// FIXTURE: never executed; every case stops the executor before
			// Engine.DKG.
			Worker: snowfallengine.WorkerConfig{Path: "/nonexistent/b12-worker-never-run", SHA256: [32]byte{0xb1, 2}},
		},
		registry: registry,
		store:    root,
		signer:   signer,
		self:     self,
		network:  b12Network,
		chainID:  [32]byte{31: 1},
		scopes:   map[uint64]*store.Scoped{},
		epochs:   map[uint64]bool{},
		wallets:  map[[32]byte]*frostWallet{},

		restoreFailures: map[uint64]int{},
	}
	t.Cleanup(func() { close(registry.release) })
	return &b12World{fn: fn, registry: registry, provider: provider}
}

// persistKey writes a completed key for the epoch, as Run leaves it (claim,
// SaveKey, release), so the next block restores instead of joining.
func (w *b12World) persistKey(ctx context.Context, t *testing.T) {
	t.Helper()
	journal, err := w.fn.scope(b12Epoch)
	if err != nil {
		t.Fatal(err)
	}
	domain := journal.Domain()
	attempt, err := domain.NewAttempt("dkg", domain.ID(), b12Epoch)
	if err != nil {
		t.Fatal(err)
	}
	request := frost.DKGRequest{Group: frost.Group{Roster: []uint16{1, 2}, Threshold: 2, Quorum: 2, Epoch: b12Epoch}, LocalSeats: []uint16{1}, Participants: []uint16{1, 2}, Attempt: attempt}
	intent, _ := json.Marshal(request)
	release, err := journal.Claim(ctx, snowfallengine.AttemptID(attempt), "dkg", intent)
	if err != nil {
		t.Fatal(err)
	}
	key := frost.KeyReady{Candidate: frost.Candidate{Epoch: b12Epoch, Roster: []uint16{1, 2}, Threshold: 2, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{{1}}}
	if err = journal.SaveKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	release()
	if status, err := journal.DKGStatus(ctx); err != nil || status.State != frost.DKGKeyStored {
		t.Fatalf("harness: key not stored: %v %v", status.State, err)
	}
}

// faulted reports that the armed setup fault was hit.
func (w *b12World) faulted(setup b12Setup) bool {
	switch setup {
	case b12SetupParameters, b12SetupWorker:
		return w.registry.parameterReads() >= 1
	case b12SetupTransport:
		return w.provider.opens("frost-v1-") >= 1
	case b12SetupBus:
		return w.provider.opens("frost-chain-v1-") >= 1
	case b12SetupScope:
		return w.registry.nodeSelectionReads() >= 1
	}
	return false
}

// retried reports that a later block joined the epoch again: for a transient
// fault, the executor started; for the fixed worker config, the setup read
// the parameters again.
func (w *b12World) retried(setup b12Setup) bool {
	if setup == b12SetupWorker {
		return w.registry.parameterReads() >= 2
	}
	return w.registry.executorStarts() >= 1
}

// blocksUntil runs DKG-loop blocks for the epoch every 10 ms until cond holds
// or the budget is spent.
func (w *b12World) blocksUntil(ctx context.Context, budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		w.fn.joinDKGIfEligible(ctx, b12Epoch)
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func (w *b12World) blocksFor(ctx context.Context, d time.Duration) {
	w.blocksUntil(ctx, d, func() bool { return false })
}

func (w *b12World) counts() string {
	return fmt.Sprintf("node selection reads %d, parameter reads %d, transport opens %d, bus opens %d, executor starts %d, restore starts %d",
		w.registry.nodeSelectionReads(), w.registry.parameterReads(), w.provider.opens("frost-v1-"), w.provider.opens("frost-chain-v1-"),
		w.registry.executorStarts(), w.registry.restoreStarts())
}

func b12Eventually(budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// ---------------------------------------------------------------------------
// FIXTURE: FROST registry adapter. Only the reads of the join, startup and
// restore paths are implemented; anything else panics (nil embedded
// interface).

type b12Registry struct {
	FrostRegistryChain
	selection dkg.Selection
	params    dkg.Parameters
	release   chan struct{}

	mu             sync.Mutex
	parameterFails int
	parameters     int
	nodeSelections int
	executors      int
	executorDone   int
	restores       int
	holdExec       bool
	holdRest       bool
}

func (r *b12Registry) failParameters(n int) { r.mu.Lock(); r.parameterFails = n; r.mu.Unlock() }
func (r *b12Registry) holdExecutor()        { r.mu.Lock(); r.holdExec = true; r.mu.Unlock() }
func (r *b12Registry) holdRestore()         { r.mu.Lock(); r.holdRest = true; r.mu.Unlock() }

func (r *b12Registry) parameterReads() int { r.mu.Lock(); defer r.mu.Unlock(); return r.parameters }
func (r *b12Registry) nodeSelectionReads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nodeSelections
}
func (r *b12Registry) executorStarts() int { r.mu.Lock(); defer r.mu.Unlock(); return r.executors }
func (r *b12Registry) restoreStarts() int  { r.mu.Lock(); defer r.mu.Unlock(); return r.restores }
func (r *b12Registry) executorReturned() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.executorDone > 0
}

func (r *b12Registry) Parameters(context.Context) (dkg.Parameters, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parameters++
	if r.parameterFails > 0 {
		r.parameterFails--
		return dkg.Parameters{}, errors.New("FIXTURE RPC: parameters read failed (transient)")
	}
	return r.params, nil
}

// Selection answers the node's reads at once. A read under a protocol
// deadline is the executor's first selection read (Run and Restore refuse a
// context without one): it is counted as an executor start and the executor
// is stopped there with an error, or held until the test ends.
func (r *b12Registry) Selection(ctx context.Context, epoch uint64) (dkg.Selection, error) {
	s := r.selection
	s.Epoch = epoch
	r.mu.Lock()
	if _, executor := ctx.Deadline(); !executor {
		r.nodeSelections++
		r.mu.Unlock()
		return s, nil
	}
	r.executors++
	hold := r.holdExec
	r.mu.Unlock()
	if hold {
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	}
	r.mu.Lock()
	r.executorDone++
	r.mu.Unlock()
	return dkg.Selection{}, errors.New("FIXTURE registry: executor stopped at its first selection read")
}

// View is the startup path's DKG state read: the epoch awaits a result.
func (r *b12Registry) View(_ context.Context, epoch uint64) (dkg.View, error) {
	return dkg.View{Epoch: epoch, State: frostDkgAwaitingResult}, nil
}

// ApprovedWallet is the restore path's first registry read: a restore
// start. Held until the test ends when the case holds restores.
func (r *b12Registry) ApprovedWallet(ctx context.Context, _ uint64) ([32]byte, error) {
	r.mu.Lock()
	r.restores++
	hold := r.holdRest
	r.mu.Unlock()
	if hold {
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	}
	return [32]byte{}, errors.New("FIXTURE registry: restore stopped at its approved-wallet read")
}

// ---------------------------------------------------------------------------
// FIXTURE: libp2p provider and broadcast channels. An armed channel name
// prefix fails to open the given number of times.

type b12Provider struct {
	net.Provider
	mu     sync.Mutex
	fail   map[string]int
	opened map[string]int
}

func (p *b12Provider) failOpen(prefix string, n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail[prefix] = n
}

func (p *b12Provider) opens(prefix string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opened[prefix]
}

func b12Prefix(name string) string {
	for _, prefix := range []string{"frost-chain-v1-", "frost-v1-"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return name
}

func (p *b12Provider) BroadcastChannelFor(name string) (net.BroadcastChannel, error) {
	prefix := b12Prefix(name)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.opened == nil {
		p.opened = map[string]int{}
	}
	p.opened[prefix]++
	if p.fail[prefix] > 0 {
		p.fail[prefix]--
		return nil, fmt.Errorf("FIXTURE libp2p: cannot join topic %s (transient)", prefix)
	}
	return &b12Channel{name: name}, nil
}

type b12Channel struct {
	net.BroadcastChannel
	name string
}

func (c *b12Channel) Name() string { return c.name }
func (c *b12Channel) Send(context.Context, net.TaggedMarshaler, ...net.RetransmissionStrategy) error {
	return nil
}
func (c *b12Channel) Recv(context.Context, func(net.Message))     {}
func (c *b12Channel) SetUnmarshaler(func() net.TaggedUnmarshaler) {}
func (c *b12Channel) SetFilter(net.BroadcastChannelFilter) error  { return nil }

// ---------------------------------------------------------------------------
// FIXTURE: the host chain's signing, for the membership validator only.

type b12HostChain struct{ Chain }

func (b12HostChain) Signing() chain.Signing { return b12Signing{} }

type b12Signing struct{ chain.Signing }

func (b12Signing) PublicKeyBytesToAddress(publicKey []byte) chain.Address {
	key, err := crypto.UnmarshalPubkey(publicKey)
	if err != nil {
		return ""
	}
	return chain.Address(crypto.PubkeyToAddress(*key).Hex())
}
