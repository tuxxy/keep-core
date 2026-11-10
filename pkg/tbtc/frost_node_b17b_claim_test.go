package tbtc

// Acceptance tests for tuxxy/keep-core#151 (REVIEW-FIX-B12 N1, N2, P3), unit
// part (no process, no chain). Refs tuxxy/keep-core#151
//
// Invariant: the FROST epoch claim is released on every executeDKG error
// that happens before the engine claims the epoch's journal, including errors
// inside dkg.Executor.Run before that claim, so a later block of the DKG loop
// joins the epoch again. After the journal claim, an error never reruns the
// DKG (DKGStatus routes the epoch to restore). After a successful DKG the
// claim is never released (frost_node_b17b_success_test.go, e2e).
//
// Source: the B12-B3 line in DECISIONS.md ("an epoch claim may be released
// on any executeDKG error before the engine claims the journal; after the
// journal claim the DKG never runs twice"), issue #151's acceptance, and
// REVIEW-FIX-B12 section 3 (the in-Run failures before the journal claim:
// prepare's Parameters and Selection reads and its selection check, the
// engine's NewClient, validSubset, the journal claim).
//
// Cases here, all inside Run and before the journal claim:
//   - prepare-parameters: Run's own parameters read fails once (transient);
//   - prepare-selection: Run's own selection read fails once (transient);
//   - prepare-selection-unknown: Run's selection read once answers an empty
//     selection (a lagging RPC node), which prepare refuses;
//   - engine-client: snowfall.NewClient refuses the worker configuration (a
//     malformed worker environment entry, fixed for the process);
//   - startup-restore/prepare-selection: as prepare-selection, with the first
//     attempt from the startup restore (restoreEpoch, AwaitingResult).
// The negative case: the engine claims the journal and then fails (no worker
// binary); later blocks never start another executor for the epoch.
//
// Not covered here (see TESTS-BATCH17B.md): validSubset (unreachable once
// prepare accepted the selection) and the journal claim itself failing (every
// store-side Claim failure either poisons the store, after which DKGStatus no
// longer returns ErrMissing, or follows a cancelled context that
// Engine.client refuses first).
//
// Real code: frostNode.joinDKGIfEligible, restoreEpoch, executeDKG, the real
// ExecuteFrostDKG -> dkg.Executor.Run (prepare, monitor, receive loop), the
// real snowfallengine.NewGuarded and Engine.DKG (snowfall.NewClient, the
// journal claim), transport.New, dkg.NewNetworkBus and the real encrypted
// store (pkg/frost/store).
//
// FIXTURES (labelled, test-only; built from batch 12's claim fixture,
// frost_node_b12_claim_test.go, under new names):
//   - b17bRegistry: the FROST registry adapter. A parameters read made under
//     dkg.Executor.Run (by call stack) is an executor start. The case can fail
//     Run's parameters or selection read once, answer an empty selection
//     once, or hold a retried executor at its parameters read. For the e2e
//     success world it also models result submission, approval and the
//     readiness certificate (see its methods).
//   - b17bProvider/b17bChannel: the libp2p provider and broadcast channels;
//     nothing is delivered.
//   - b17bHostChain/b17bSigning: the host chain's operator key and address
//     derivation (go-ethereum addresses).
//   - b17bFence: the in-memory store fence (a copy of batch 7's advFence).
//   - The unit worlds name a worker path that does not exist: no worker
//     process ever runs.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"runtime"
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
	"github.com/keep-network/keep-core/pkg/operator"
)

const b17bInvariant = "#151 an executeDKG error before the journal claim releases the FROST epoch claim; success never does; a journal-claimed DKG never reruns (tuxxy/keep-core#151)"

const b17bEpoch = 100

type b17bFault string

const (
	b17bPrepareParameters       b17bFault = "prepare-parameters"
	b17bPrepareSelection        b17bFault = "prepare-selection"
	b17bPrepareSelectionUnknown b17bFault = "prepare-selection-unknown"
	b17bEngineClient            b17bFault = "engine-client"
)

// TestFrostNodeB17bPreJournalRunErrorReleasesEpochClaim (unit): an error
// inside Run before the journal claim releases the epoch claim, so a later
// block starts the epoch's executor again, exactly once while it runs.
func TestFrostNodeB17bPreJournalRunErrorReleasesEpochClaim(t *testing.T) {
	for _, tc := range []struct {
		fault   b17bFault
		startup bool
	}{
		{fault: b17bPrepareParameters},
		{fault: b17bPrepareSelection},
		{fault: b17bPrepareSelectionUnknown},
		{fault: b17bEngineClient},
		{fault: b17bPrepareSelection, startup: true},
	} {
		tc := tc
		name := string(tc.fault)
		if tc.startup {
			name = "startup-restore/" + name
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newB17bWorld(t, b17bUnitWorld)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			switch tc.fault {
			case b17bPrepareParameters:
				w.registry.failRunParameters(1)
			case b17bPrepareSelection:
				w.registry.failRunSelection(1)
			case b17bPrepareSelectionUnknown:
				w.registry.unknownRunSelection(1)
			case b17bEngineClient:
				// FIXTURE: a worker environment entry snowfall.NewClient
				// refuses; NewGuarded accepts it. Fixed for the process.
				w.fn.config.Worker.Env = []string{"=b17b-FIXTURE-malformed"}
			}

			// The first attempt.
			if tc.startup {
				w.fn.restoreEpoch(ctx, b17bEpoch)
			} else {
				w.fn.joinDKGIfEligible(ctx, b17bEpoch)
			}
			if !b17bEventually(3*time.Second, w.firstAttemptReturned) {
				t.Fatalf("harness: the first executeDKG never returned (%s)", w.counts())
			}
			if n := w.registry.executorStarts(); n != 1 {
				t.Fatalf("harness: the first attempt started %d executors before any later block, want 1: the %s fault is not inside Run, or executeDKG reran itself (%s)", n, tc.fault, w.counts())
			}
			if !w.registry.faultHit(tc.fault) {
				t.Fatalf("harness: the %s fault was never hit (%s)", tc.fault, w.counts())
			}
			if w.journalClaimed(ctx) {
				t.Fatalf("harness: the journal is claimed after a %s error: not a pre-journal error (%s)", tc.fault, w.counts())
			}

			// Later blocks of the DKG loop. A retried executor is held
			// (running) at its parameters read, so the case is judged on
			// the release alone.
			w.registry.holdExecutor()
			retried := w.blocksUntil(ctx, 1500*time.Millisecond, func() bool { return w.registry.executorStarts() >= 2 })
			if !retried {
				t.Errorf("%s: after the %s error inside Run before the journal claim, no later block started epoch %d's DKG executor again: the epoch stayed claimed (%s)",
					b17bInvariant, tc.fault, b17bEpoch, w.counts())
				return
			}
			w.blocksFor(ctx, 300*time.Millisecond)
			if n := w.registry.executorStarts(); n != 2 {
				t.Errorf("%s: after the retried %s error, the epoch's DKG executor started %d times, want 2 (the failed one and one running retry) (%s)",
					b17bInvariant, tc.fault, n, w.counts())
			}
		})
	}
}

// TestFrostNodeB17bJournalClaimedErrorNeverRerunsDKG (unit) is the negative
// side: the engine claims the journal and then fails (the worker binary is
// absent). Whether or not the claim is released, later blocks never start
// another executor for the epoch. A fix that releases and reruns the DKG
// fails here.
func TestFrostNodeB17bJournalClaimedErrorNeverRerunsDKG(t *testing.T) {
	t.Parallel()
	w := newB17bWorld(t, b17bUnitWorld)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.fn.joinDKGIfEligible(ctx, b17bEpoch)
	if !b17bEventually(3*time.Second, func() bool { return w.journalClaimed(ctx) }) {
		t.Fatalf("harness: the engine never claimed the journal (%s)", w.counts())
	}
	if !b17bEventually(3*time.Second, w.firstAttemptReturned) {
		t.Fatalf("harness: the failed executor never returned (%s)", w.counts())
	}
	w.registry.holdExecutor()
	w.blocksFor(ctx, 1600*time.Millisecond)
	if n := w.registry.executorStarts(); n != 1 {
		t.Errorf("%s: after the engine claimed the journal and failed, the epoch's DKG executor started %d times, want exactly 1 (%s)",
			b17bInvariant, n, w.counts())
	}
}

// ---------------------------------------------------------------------------
// World: one operator. The unit world holds seat 1 of a 2-seat epoch (the
// other seat is another operator); the e2e success world holds both seats.

const b17bNetwork = "B17b/claim FIXTURE"

type b17bShape int

const (
	b17bUnitWorld b17bShape = iota
	b17bBothSeatsWorld
)

type b17bWorld struct {
	fn       *frostNode
	registry *b17bRegistry
	provider *b17bProvider
}

func newB17bWorld(t *testing.T, shape b17bShape) *b17bWorld {
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
		StorageID: [32]byte{0xb1, 0x7b},
		Key:       [32]byte{0xb1, 0x7b, 2},
		Fence:     &b17bFence{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	self := crypto.PubkeyToAddress(signer.PublicKey)
	selection := dkg.Selection{Members: []uint32{1, 2}, Operators: []common.Address{self, crypto.PubkeyToAddress(other.PublicKey)}}
	if shape == b17bBothSeatsWorld {
		selection = dkg.Selection{Members: []uint32{1, 1}, Operators: []common.Address{self, self}}
	}
	registry := &b17bRegistry{
		selection: selection,
		params:    dkg.Parameters{ChainID: big.NewInt(1), Registry: common.Address{9}, Pool: common.Address{3}, Bridge: common.Address{4}, Profile: 1, GroupSize: 2, Threshold: 2, ReadySeats: 2, ChallengeBlocks: 1},
		release:   make(chan struct{}),
	}
	registry.view = dkg.View{Epoch: b17bEpoch, State: frostDkgAwaitingResult, Head: 1, ResultDeadline: 1 << 40}
	provider := &b17bProvider{}
	fn := &frostNode{
		node: &node{chain: b17bHostChain{signer: &signer.PublicKey}, netProvider: provider},
		config: FrostConfig{
			Registry: [20]byte{9},
			// FIXTURE: never executed in the unit world (the e2e world sets
			// the pinned real worker).
			Worker: snowfallengine.WorkerConfig{Path: "/nonexistent/b17b-worker-never-run", SHA256: [32]byte{0xb1, 0x7b}},
		},
		registry: registry,
		store:    root,
		signer:   signer,
		self:     self,
		network:  b17bNetwork,
		chainID:  [32]byte{31: 1},
		scopes:   map[uint64]*store.Scoped{},
		epochs:   map[uint64]bool{},
		wallets:  map[[32]byte]*frostWallet{},

		restoreFailures: map[uint64]int{},
	}
	t.Cleanup(func() { close(registry.release) })
	return &b17bWorld{fn: fn, registry: registry, provider: provider}
}

// firstAttemptReturned reports that an executeDKG returned: its DKG context
// (and with it the attestation bus's receive context) ended.
func (w *b17bWorld) firstAttemptReturned() bool {
	return w.provider.closed("frost-chain-v1-") >= 1
}

func (w *b17bWorld) blocksUntil(ctx context.Context, budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		w.fn.joinDKGIfEligible(ctx, b17bEpoch)
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func (w *b17bWorld) blocksFor(ctx context.Context, d time.Duration) {
	w.blocksUntil(ctx, d, func() bool { return false })
}

// journalClaimed reports that the epoch's journal holds a DKG claim, so
// DKGStatus no longer returns ErrMissing.
func (w *b17bWorld) journalClaimed(ctx context.Context) bool {
	journal, err := w.fn.scope(b17bEpoch)
	if err != nil {
		return false
	}
	_, err = journal.DKGStatus(ctx)
	return !errors.Is(err, store.ErrMissing)
}

func (w *b17bWorld) counts() string {
	return fmt.Sprintf("node selection reads %d, executor starts %d, run selection reads %d, restore starts %d, executeDKG returns %d",
		w.registry.nodeSelectionReads(), w.registry.executorStarts(), w.registry.runSelectionReads(), w.registry.restoreStarts(), w.provider.closed("frost-chain-v1-"))
}

func b17bEventually(budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// b17bUnderRun reports a call made under dkg.Executor.Run.
func b17bUnderRun() bool {
	pc := make([]uintptr, 64)
	frames := runtime.CallersFrames(pc[:runtime.Callers(2, pc)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, "/pkg/frost/dkg.(*Executor).Run") {
			return true
		}
		if !more {
			return false
		}
	}
}

// ---------------------------------------------------------------------------
// FIXTURE: FROST registry adapter. Only the reads and writes of the join,
// startup, executor and restore paths are implemented; anything else panics
// (nil embedded interface).

type b17bRegistry struct {
	FrostRegistryChain
	selection dkg.Selection
	params    dkg.Parameters
	release   chan struct{}

	mu               sync.Mutex
	view             dkg.View
	runParamFails    int
	runSelFails      int
	runSelUnknown    int
	readinessFails   int
	hits             map[b17bFault]bool
	readinessHit     bool
	runStarts        int
	nodeSelections   int
	runSelections    int
	restores         int
	hold             bool
	certificatesSent int
}

func (r *b17bRegistry) failRunParameters(n int)   { r.mu.Lock(); r.runParamFails = n; r.mu.Unlock() }
func (r *b17bRegistry) failRunSelection(n int)    { r.mu.Lock(); r.runSelFails = n; r.mu.Unlock() }
func (r *b17bRegistry) unknownRunSelection(n int) { r.mu.Lock(); r.runSelUnknown = n; r.mu.Unlock() }
func (r *b17bRegistry) failReadinessDigest(n int) { r.mu.Lock(); r.readinessFails = n; r.mu.Unlock() }
func (r *b17bRegistry) holdExecutor()             { r.mu.Lock(); r.hold = true; r.mu.Unlock() }

func (r *b17bRegistry) hit(f b17bFault) {
	if r.hits == nil {
		r.hits = map[b17bFault]bool{}
	}
	r.hits[f] = true
}

// faultHit reports that the armed fault was hit. The engine-client fault is
// not a registry read: it is hit once the executor passed prepare (its
// selection read answered) and returned without a journal claim.
func (r *b17bRegistry) faultHit(f b17bFault) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f == b17bEngineClient {
		return r.runSelections >= 1
	}
	return r.hits[f]
}

func (r *b17bRegistry) executorStarts() int { r.mu.Lock(); defer r.mu.Unlock(); return r.runStarts }
func (r *b17bRegistry) restoreStarts() int  { r.mu.Lock(); defer r.mu.Unlock(); return r.restores }
func (r *b17bRegistry) runSelectionReads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runSelections
}
func (r *b17bRegistry) nodeSelectionReads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nodeSelections
}
func (r *b17bRegistry) readinessFaultHit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readinessHit
}

// Parameters: a read under dkg.Executor.Run is the executor's first chain
// read (prepare): it counts as an executor start. It fails when armed, and
// it holds a retried executor (until the test ends) once the case holds them.
func (r *b17bRegistry) Parameters(ctx context.Context) (dkg.Parameters, error) {
	if !b17bUnderRun() {
		return r.params, nil
	}
	r.mu.Lock()
	r.runStarts++
	if r.runParamFails > 0 {
		r.runParamFails--
		r.hit(b17bPrepareParameters)
		r.mu.Unlock()
		return dkg.Parameters{}, errors.New("FIXTURE RPC: parameters read failed (transient)")
	}
	hold := r.hold
	r.mu.Unlock()
	if hold {
		select {
		case <-r.release:
		case <-ctx.Done():
		}
		return dkg.Parameters{}, errors.New("FIXTURE registry: held executor released")
	}
	return r.params, nil
}

func (r *b17bRegistry) Selection(_ context.Context, epoch uint64) (dkg.Selection, error) {
	s := r.selection
	s.Epoch = epoch
	r.mu.Lock()
	defer r.mu.Unlock()
	if !b17bUnderRun() {
		r.nodeSelections++
		return s, nil
	}
	r.runSelections++
	if r.runSelFails > 0 {
		r.runSelFails--
		r.hit(b17bPrepareSelection)
		return dkg.Selection{}, errors.New("FIXTURE RPC: selection read failed (transient)")
	}
	if r.runSelUnknown > 0 {
		r.runSelUnknown--
		r.hit(b17bPrepareSelectionUnknown)
		return dkg.Selection{Epoch: epoch}, nil // FIXTURE: a lagging node that has not seen the seed yet
	}
	return s, nil
}

// View is the registry's DKG and approved-wallet state: AwaitingResult far
// from its deadline until a result is submitted.
func (r *b17bRegistry) View(_ context.Context, epoch uint64) (dkg.View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.view
	v.Epoch = epoch
	return v, nil
}

func (r *b17bRegistry) ResultDigest(_ context.Context, _ uint64, result dkg.Result) ([32]byte, error) {
	return dkg.ResultHash(result)
}

func (r *b17bRegistry) Validate(context.Context, uint64, dkg.Result) (bool, error) {
	return true, nil
}

// Submit approves the submitted result at once (FIXTURE): approval at block
// 10, head 20 (past the node's finality depth), wallet RegisteredPendingReady
// with a far deadline. The descriptor is rebuilt from the result's public key
// payload (profile, output key, SNOWFALL descriptor), as the contract stores it.
func (r *b17bRegistry) Submit(_ context.Context, result dkg.Result) error {
	payload := result.GroupPubKey
	if len(payload) != 65 {
		return errors.New("FIXTURE registry: malformed result payload")
	}
	var output, descriptor [32]byte
	copy(output[:], payload[1:33])
	copy(descriptor[:], payload[33:65])
	selection := r.selection
	selection.Epoch = b17bEpoch
	d, err := dkg.NewDescriptor(r.params, selection, frost.Candidate{Epoch: b17bEpoch, Descriptor: descriptor, OutputKey: output, Roster: selection.Roster(), Threshold: r.params.Threshold, Profile: frost.ApprovedProfile})
	if err != nil {
		return err
	}
	dh, err := dkg.DescriptorHash(d)
	if err != nil {
		return err
	}
	rh, err := dkg.ResultHash(result)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.view = dkg.View{Head: 20, Epoch: b17bEpoch, State: 0, ResultDeadline: 1 << 40, ApprovedID: dkg.WalletID(d.OutputKey), ApprovalHash: common.Hash{0xb1, 0x7b},
		Approved: dkg.Wallet{Descriptor: d, ResultHash: rh, DescriptorHash: dh, ApprovalBlock: 10, Deadline: 1 << 40, State: 1}}
	return nil
}

// ReadinessDigest is a fixed public hash of the wallet, seat and reference
// (FIXTURE). Armed, the next read fails.
func (r *b17bRegistry) ReadinessDigest(_ context.Context, w dkg.Wallet, seat uint16, reference [32]byte) ([32]byte, error) {
	r.mu.Lock()
	if r.readinessFails > 0 {
		r.readinessFails--
		r.readinessHit = true
		r.mu.Unlock()
		return [32]byte{}, errors.New("FIXTURE RPC: readiness digest read failed (transient)")
	}
	r.mu.Unlock()
	return crypto.Keccak256Hash([]byte("B17b FIXTURE readiness"), w.DescriptorHash[:], []byte{byte(seat >> 8), byte(seat)}, reference[:]), nil
}

// Ready certifies the approved wallet (FIXTURE): wallet state 2.
func (r *b17bRegistry) Ready(_ context.Context, id [32]byte, _ []uint16, _ [][32]byte, _ []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != r.view.ApprovedID {
		return errors.New("FIXTURE registry: certificate for an unknown wallet")
	}
	r.certificatesSent++
	r.view.Approved.State = 2
	return nil
}

func (r *b17bRegistry) Challenge(context.Context) error {
	return errors.New("FIXTURE registry: unexpected challenge")
}
func (r *b17bRegistry) Approve(context.Context) error {
	return errors.New("FIXTURE registry: unexpected approve")
}
func (r *b17bRegistry) Expire(context.Context, [32]byte) error {
	return errors.New("FIXTURE registry: unexpected expire")
}
func (r *b17bRegistry) ExpireDKG(context.Context) error {
	return errors.New("FIXTURE registry: unexpected DKG expiry")
}

// ApprovedWallet is the restore path's first registry read (restoreStatus):
// a restore start. The restore is stopped there.
func (r *b17bRegistry) ApprovedWallet(context.Context, uint64) ([32]byte, error) {
	r.mu.Lock()
	r.restores++
	r.mu.Unlock()
	return [32]byte{}, errors.New("FIXTURE registry: restore stopped at its approved-wallet read")
}

// ---------------------------------------------------------------------------
// FIXTURE: libp2p provider and broadcast channels. Nothing is delivered.

type b17bProvider struct {
	net.Provider
	mu    sync.Mutex
	ended map[string]int
}

// closed counts the channels of the prefix whose receive context ended: for
// the attestation bus, that executeDKG returned.
func (p *b17bProvider) closed(prefix string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended[prefix]
}

func b17bPrefix(name string) string {
	for _, prefix := range []string{"frost-chain-v1-", "frost-v1-"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return name
}

func (p *b17bProvider) BroadcastChannelFor(name string) (net.BroadcastChannel, error) {
	return &b17bChannel{name: name, prefix: b17bPrefix(name), provider: p}, nil
}

type b17bChannel struct {
	net.BroadcastChannel
	name     string
	prefix   string
	provider *b17bProvider
}

func (c *b17bChannel) Name() string { return c.name }
func (c *b17bChannel) Send(context.Context, net.TaggedMarshaler, ...net.RetransmissionStrategy) error {
	return nil
}
func (c *b17bChannel) Recv(ctx context.Context, _ func(net.Message)) {
	go func() {
		<-ctx.Done()
		c.provider.mu.Lock()
		defer c.provider.mu.Unlock()
		if c.provider.ended == nil {
			c.provider.ended = map[string]int{}
		}
		c.provider.ended[c.prefix]++
	}()
}
func (c *b17bChannel) SetUnmarshaler(func() net.TaggedUnmarshaler) {}
func (c *b17bChannel) SetFilter(net.BroadcastChannelFilter) error  { return nil }

// ---------------------------------------------------------------------------
// FIXTURE: the host chain's operator key and signing, for the membership
// validator and the ready wallet's coordination setup only.

type b17bHostChain struct {
	Chain
	signer *ecdsa.PublicKey
}

func (b17bHostChain) Signing() chain.Signing { return b17bSigning{} }

func (c b17bHostChain) OperatorKeyPair() (*operator.PrivateKey, *operator.PublicKey, error) {
	return nil, &operator.PublicKey{Curve: operator.Secp256k1, X: c.signer.X, Y: c.signer.Y}, nil
}

type b17bSigning struct{ chain.Signing }

func (b17bSigning) PublicKeyBytesToAddress(publicKey []byte) chain.Address {
	key, err := crypto.UnmarshalPubkey(publicKey)
	if err != nil {
		return ""
	}
	return chain.Address(crypto.PubkeyToAddress(*key).Hex())
}

func (b17bSigning) PublicKeyToAddress(publicKey *operator.PublicKey) (chain.Address, error) {
	key := ecdsa.PublicKey{Curve: crypto.S256(), X: publicKey.X, Y: publicKey.Y}
	return chain.Address(crypto.PubkeyToAddress(key).Hex()), nil
}

// ---------------------------------------------------------------------------
// FIXTURE: in-memory store fence (a copy of batch 7's advFence).

type b17bFence struct {
	mu   sync.Mutex
	head store.Point
}
type b17bLease struct{ f *b17bFence }

func (f *b17bFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	return b17bLease{f}, nil
}
func (l b17bLease) Head(context.Context) (store.Point, error) {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	return l.f.head, nil
}
func (l b17bLease) Advance(_ context.Context, before, after store.Point) error {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	if l.f.head != before {
		return store.ErrQuarantined
	}
	l.f.head = after
	return nil
}
func (l b17bLease) Close() error { return nil }
