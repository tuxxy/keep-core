package dkg_test

// Acceptance tests for tuxxy/keep-core#128 (F-V03-R1, P2).
// Refs tuxxy/keep-core#128
//
// Invariant: P-K03-INCONSISTENCY-QUARANTINE (KC-VER V-03), restore path. "A
// challenge, a different approved hash, a removed approval or an inconsistent
// finality re-read quarantines the candidate; the host never publishes,
// attests or funds from a quarantined candidate." KC-SF-V3 4.3: "A challenge,
// different approved hash, removed approval, or inconsistent re-read must
// quarantine the candidate." A saved RegisteredPendingReady or ReadyUnfunded
// wallet record exists only after the host observed and finality-checked the
// approval, so a host restored from it (Executor.Restore, node startup) knows
// that approval durably. When the restored host's approval monitor reads that
// approval removed or replaced, the host must quarantine the record and return
// ErrQuarantined, exactly as a Run host does (trace
// B-READY-RETURN-MONITOR-QUARANTINE). It must never return ReadyUnfunded, and
// it must send no readiness attestation and submit no readiness certificate
// after that read.
//
// Matrix: saved state {RegisteredPendingReady, ReadyUnfunded} x every restore
// boundary the state passes x change {removed back to the challenge period,
// removed back to awaiting a result, the same wallet re-approved in another
// block (different approval hash), another wallet approved}. Negative side:
// the same runs with the approval unchanged must reach ReadyUnfunded.
//
// Real code: dkg.Executor.Restore and its monitor, and the real encrypted
// journal (pkg/frost/store) reopened as a fresh process would.
//
// FIXTURES (labelled, test-only): the controlled chain adapter on the K-03
// chain seam (dkg.Chain, KC-VER VD-06), the authenticated bus, the reload-only
// engine, the in-memory store fence, and the remote seat's readiness vote.
// The controlled chain holds every background registry read (any View call
// whose stack does not include Executor.Restore, so the monitor's reads
// whatever its private name) until the case changes the approval at its
// boundary. This is an outstanding RPC, not a stale answer (as the V-03
// replay harness models a monitor read): nothing in Restore orders the main
// path after the monitor's first read, so a removal between the restore-time
// reads and the monitor's first read is a reachable interleaving. Every read
// made synchronously under Restore is answered at once.
//
// Fallback (review B1, 2026-11-06): an implementation may make Restore's
// main path wait for the monitor's first read. If a background read is held,
// the boundary is not yet reached, and the main path has made no chain or bus
// call for b10Grace, the harness applies the change there instead
// ("before-first-main-read"), releases the read, and records the fallback.
// A change case may then end with ErrQuarantined before its boundary; the
// same assertions 1-4 decide it. Unchanged cases are released without a
// change and stay strict.
//
// A call made with a cancelled context is refused by the fixtures and not
// counted as an attestation or certificate: a real transactor or libp2p
// publish fails on a cancelled context, and a Run host stops the same way.

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

const b10Invariant = "P-K03-INCONSISTENCY-QUARANTINE (restore path, tuxxy/keep-core#128)"

type b10Change string

const (
	// The approval is gone and the registry is back in the challenge period
	// of the same (valid, local) submitted result.
	b10RemovedToChallenge b10Change = "removed-to-challenge"
	// The approval is gone and the registry awaits a result again.
	b10RemovedToAwaiting b10Change = "removed-to-awaiting-result"
	// The same wallet is approved again in another block: different
	// approval hash and block, same descriptor and result.
	b10Reapproved b10Change = "replaced-reapproved-other-block"
	// Another wallet (another descriptor and Q) is approved.
	b10OtherWallet b10Change = "replaced-other-wallet"
	// Negative case: the approval stays as saved.
	b10Unchanged b10Change = "unchanged"
)

var b10Changes = []b10Change{b10RemovedToChallenge, b10RemovedToAwaiting, b10Reapproved, b10OtherWallet}

// Restore-path boundaries each saved state passes (RESTORE-SCOPE-2026-11-06
// section 2). A ReadyUnfunded restore reads the wallet certified, so it skips
// collection and the certificate.
var b10Boundaries = map[string][]string{
	"RegisteredPendingReady": {"after-key-reload", "before-readiness-collect", "before-certificate-submit", "after-certificate-submit", "after-readiness-postcondition", "after-wallet-ready-save"},
	"ReadyUnfunded":          {"after-key-reload", "before-readiness-collect", "after-readiness-postcondition", "after-wallet-ready-save"},
}

func TestRestoredHostQuarantinesOnApprovalChange(t *testing.T) {
	for _, saved := range []string{"RegisteredPendingReady", "ReadyUnfunded"} {
		for _, boundary := range b10Boundaries[saved] {
			for _, change := range b10Changes {
				saved, boundary, change := saved, boundary, change
				t.Run(fmt.Sprintf("%s/%s/%s", saved, boundary, change), func(t *testing.T) {
					t.Parallel()
					r := b10Restore(t, saved, boundary, change)
					got, stored, w, at := r.outcome, r.stored, r.world, r.at
					if !errors.Is(got.err, dkg.ErrQuarantined) {
						t.Errorf("%s: restored host from saved %s did not quarantine after its monitor read the approval %s at %s: Restore returned err=%v, record state=%s quarantined=%t (want ErrQuarantined)",
							b10Invariant, saved, change, at, got.err, got.record.State, got.record.Quarantined)
					}
					if !stored.Quarantined {
						t.Errorf("%s: saved wallet not quarantined after the approval %s at %s: durable state=%s quarantined=%t",
							b10Invariant, change, at, stored.State, stored.Quarantined)
					}
					read, attestations, certificates := w.chain.after()
					if len(attestations) != 0 {
						t.Errorf("%s: restored host sent %d readiness attestation(s) after reading the approval %s at %s (read #%d, sends %v)",
							b10Invariant, len(attestations), change, at, read, attestations)
					}
					if len(certificates) != 0 {
						t.Errorf("%s: restored host submitted %d readiness certificate(s) after reading the approval %s at %s (read #%d, submissions %v)",
							b10Invariant, len(certificates), change, at, read, certificates)
					}
					r.reportHarness(t)
				})
			}
		}
	}
}

// TestRestoredHostWithUnchangedApprovalBecomesReady is the negative side of
// #128: with the same held monitor read released at the same boundaries but
// the approval unchanged, the restored host must not quarantine. It reaches
// ReadyUnfunded, and a pending wallet is certified exactly once. A fix that
// quarantines every restored wallet, or that seeds the wrong approval, fails
// here.
func TestRestoredHostWithUnchangedApprovalBecomesReady(t *testing.T) {
	for _, saved := range []string{"RegisteredPendingReady", "ReadyUnfunded"} {
		for _, boundary := range b10Boundaries[saved] {
			saved, boundary := saved, boundary
			t.Run(fmt.Sprintf("%s/%s", saved, boundary), func(t *testing.T) {
				t.Parallel()
				r := b10Restore(t, saved, boundary, b10Unchanged)
				got, stored, w := r.outcome, r.stored, r.world
				if got.err != nil || got.record.State != "ReadyUnfunded" || got.record.Quarantined {
					t.Errorf("%s: restored host from saved %s with an unchanged approval must reach ReadyUnfunded (monitor released at %s): Restore returned err=%v, record state=%s quarantined=%t",
						b10Invariant, saved, boundary, got.err, got.record.State, got.record.Quarantined)
				}
				if stored.State != "ReadyUnfunded" || stored.Quarantined {
					t.Errorf("%s: saved wallet after restoring %s with an unchanged approval: durable state=%s quarantined=%t, want ReadyUnfunded unquarantined",
						b10Invariant, saved, stored.State, stored.Quarantined)
				}
				want := 1
				if saved == "ReadyUnfunded" {
					want = 0
				}
				if n := w.chain.submissionCount(); n != want {
					t.Errorf("%s: restored host from saved %s submitted %d readiness certificate(s), want %d", b10Invariant, saved, n, want)
				}
				r.reportHarness(t)
			})
		}
	}
}

type b10Outcome struct {
	status frost.DKGStatus
	record dkg.WalletRecord
	err    error
}

// b10Grace is how long the main path may stay silent, with a background read
// held and the boundary not reached, before the harness falls back to
// applying the change there (review B1).
const b10Grace = time.Second

type b10Result struct {
	outcome b10Outcome
	stored  dkg.WalletRecord
	world   *b10World
	at      string   // where the change was applied
	harness []string // harness problems, reported after the property checks
}

// reportHarness reports harness preconditions after the property assertions
// (review N2), so a wrong implementation fails at a named assertion first.
func (r b10Result) reportHarness(t *testing.T) {
	t.Helper()
	for _, problem := range r.harness {
		t.Errorf("harness: %s", problem)
	}
}

// b10Restore runs the real Executor.Restore from a saved state. At boundary
// (or at the fallback point, see the file comment) it applies change on the
// controlled chain, releases the held background reads and waits until the
// monitor has classified the new view, then lets Restore continue. It returns
// Restore's outcome, the durable wallet afterwards and any harness problems.
func b10Restore(t *testing.T, saved, boundary string, change b10Change) b10Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	w := newB10World(t)
	journal := w.persist(ctx, t, saved)

	var (
		problemsMu sync.Mutex
		problems   []string
		reached    atomic.Bool
		fellBack   atomic.Bool
		applyOnce  sync.Once
	)
	problem := func(format string, args ...any) {
		problemsMu.Lock()
		defer problemsMu.Unlock()
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	apply := func() { applyOnce.Do(func() { w.chain.apply(change) }) }
	engine := &b10Engine{journal: journal}
	e := &dkg.Executor{
		Chain:        w.chain,
		Engine:       engine,
		Journal:      journal,
		Bus:          w.bus,
		Signer:       b10Signer(0),
		PollInterval: 2 * time.Millisecond,
	}
	e.Boundary = func(name string) error {
		switch name {
		case "after-monitor-observation-snapshot":
			return nil
		case "after-monitor-inspect":
			if w.chain.monitorReadsAfterRelease() > 0 {
				w.chain.signalClassified()
			}
			return nil
		}
		w.chain.touch()
		if name != boundary || !reached.CompareAndSwap(false, true) {
			return nil
		}
		// Change the approval (once; the fallback may already have), then let
		// the held read complete and wait until the monitor has classified it.
		apply()
		select {
		case <-w.chain.classified:
		case <-time.After(3 * time.Second):
			problem("the restored monitor did not classify the %s view within 3 s", change)
		}
		return nil
	}

	done := make(chan b10Outcome, 1)
	go func() {
		status, record, err := e.Restore(ctx)
		done <- b10Outcome{status, record, err}
	}()
	var got b10Outcome
	watchdog := time.NewTicker(20 * time.Millisecond)
	defer watchdog.Stop()
	deadline := time.After(9 * time.Second)
wait:
	for {
		select {
		case got = <-done:
			break wait
		case <-watchdog.C:
			if !reached.Load() && !fellBack.Load() && w.chain.heldNow() > 0 && w.chain.silentFor() >= b10Grace {
				fellBack.Store(true)
				apply()
			}
		case <-deadline:
			t.Fatalf("harness: Restore did not return within 9 s (boundary %s reached=%t fallback=%t)", boundary, reached.Load(), fellBack.Load())
		}
	}

	at := boundary
	if fellBack.Load() {
		at = "before-first-main-read (fallback: the main path waited on a held background read)"
	}
	stored, err := journal.LoadWallet(context.Background())
	if err != nil {
		t.Fatalf("harness: reading the saved wallet: %v", err)
	}
	// Harness preconditions: not the property under test. The caller reports
	// them after its assertions.
	if got.status.State != frost.DKGKeyStored {
		problem("restore status %s, want %s", got.status.State, frost.DKGKeyStored)
	}
	if !reached.Load() && (!fellBack.Load() || change == b10Unchanged) {
		problem("Restore returned before boundary %s (fallback=%t, err=%v)", boundary, fellBack.Load(), got.err)
	}
	if w.chain.monitorReadsAfterRelease() == 0 {
		problem("no background registry read saw the %s view", change)
	}
	if reached.Load() && engine.reloads.Load() != 1 {
		problem("%d key reloads, want 1", engine.reloads.Load())
	}
	problemsMu.Lock()
	defer problemsMu.Unlock()
	return b10Result{outcome: got, stored: stored, world: w, at: at, harness: append([]string(nil), problems...)}
}

// ---------------------------------------------------------------------------
// World: one operator (seats 1 and 2 of a 4-seat, threshold-3 group; the other
// operator holds seats 3 and 4), restored from its own journal.

type b10World struct {
	root   string
	fence  *b10Fence
	domain frost.Domain
	params dkg.Parameters
	sel    dkg.Selection
	desc   dkg.Descriptor
	record dkg.WalletRecord
	key    frost.KeyReady
	chain  *b10Chain
	bus    *b10Bus
}

func b10Signer(n int) *ecdsa.PrivateKey {
	raw := make([]byte, 32)
	raw[31] = byte(n + 1)
	key, err := crypto.ToECDSA(raw)
	if err != nil {
		panic(err)
	}
	return key
}

func b10Candidate(epoch uint64, outputSeed int, descriptor byte) frost.Candidate {
	var output [32]byte
	b10Signer(outputSeed).PublicKey.X.FillBytes(output[:])
	return frost.Candidate{Epoch: epoch, Descriptor: [32]byte{descriptor, 3}, OutputKey: output, Roster: []uint16{1, 2, 3, 4}, Threshold: 3, Profile: frost.ApprovedProfile}
}

func newB10World(t *testing.T) *b10World {
	t.Helper()
	w := &b10World{root: t.TempDir(), fence: &b10Fence{}}
	w.domain = frost.Domain{Network: "B10/restore-removal FIXTURE", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 1}
	w.params = dkg.Parameters{ChainID: new(big.Int).Lsh(big.NewInt(1), 248), Registry: common.Address{2}, Pool: common.Address{3}, Bridge: common.Address{4}, GroupSize: 4, Threshold: 3, ReadySeats: 3, Profile: 1, ChallengeBlocks: 1}
	a, b := crypto.PubkeyToAddress(b10Signer(0).PublicKey), crypto.PubkeyToAddress(b10Signer(1).PublicKey)
	w.sel = dkg.Selection{Epoch: 1, Members: []uint32{1, 1, 2, 2}, Operators: []common.Address{a, a, b, b}}
	var err error
	if w.desc, err = dkg.NewDescriptor(w.params, w.sel, b10Candidate(1, 5, 1)); err != nil {
		t.Fatal(err)
	}
	approved := b10Approval(t, w.desc, 10, common.Hash{0xa1}, 1)
	w.record = dkg.WalletRecord{Descriptor: w.desc, DescriptorHash: approved.Approved.DescriptorHash, ID: approved.ApprovedID, ResultHash: approved.Approved.ResultHash, ApprovalBlock: 10, ApprovalHash: approved.ApprovalHash, Deadline: approved.Approved.Deadline}
	w.key = frost.KeyReady{Candidate: b10Candidate(1, 5, 1), LocalReferences: []frost.KeyReference{{1, 1, 42}, {1, 2, 42}}}

	other, err := dkg.NewDescriptor(w.params, w.sel, b10Candidate(1, 6, 9))
	if err != nil {
		t.Fatal(err)
	}
	result := dkg.NewResult(w.desc)
	resultHash, err := dkg.ResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	w.chain = &b10Chain{
		params: w.params, sel: w.sel,
		changes: map[b10Change]dkg.View{
			b10RemovedToChallenge: {Head: 20, Epoch: 1, State: 3, ResultDeadline: 100, SubmittedAt: 9, Submitted: &result, SubmittedHash: resultHash},
			b10RemovedToAwaiting:  {Head: 20, Epoch: 1, State: 2, ResultDeadline: 100},
			b10Reapproved:         b10Approval(t, w.desc, 12, common.Hash{0xa2}, 1),
			b10OtherWallet:        b10Approval(t, other, 12, common.Hash{0xa3}, 1),
		},
		release:    make(chan struct{}),
		classified: make(chan struct{}),
		lastMain:   time.Now(),
	}
	w.bus = &b10Bus{chain: w.chain, incoming: make(chan dkg.Attestation, 4)}
	return w
}

// b10Approval is the registry view after the approval of d at block, before
// any certificate (wallet state 1, RegisteredPendingReady).
func b10Approval(t *testing.T, d dkg.Descriptor, block uint64, hash common.Hash, state uint8) dkg.View {
	t.Helper()
	dh, err := dkg.DescriptorHash(d)
	if err != nil {
		t.Fatal(err)
	}
	rh, err := dkg.ResultHash(dkg.NewResult(d))
	if err != nil {
		t.Fatal(err)
	}
	return dkg.View{Head: 20, Epoch: d.Epoch, State: 0, ResultDeadline: 100, ApprovedID: dkg.WalletID(d.OutputKey), ApprovalHash: hash,
		Approved: dkg.Wallet{Descriptor: d, ResultHash: rh, DescriptorHash: dh, ApprovalBlock: block, Deadline: block + 990, State: state}}
}

func (w *b10World) config() store.Config {
	return store.Config{Keystore: filepath.Join(w.root, "keystore"), StorageID: [32]byte{0xb1}, Key: [32]byte{0xb1, 4}, Fence: w.fence}
}

// persist writes what a Run host leaves durable at the saved state (claim,
// wallet record, KeyReady, and for ReadyUnfunded the ready save), closes that
// "process", and reopens the store as a fresh process would. It also sets the
// chain to the matching public state and queues the remote seat's vote.
func (w *b10World) persist(ctx context.Context, t *testing.T, saved string) *store.Scoped {
	t.Helper()
	first, err := store.Open(ctx, w.config())
	if err != nil {
		t.Fatal(err)
	}
	j, err := first.Scope(w.domain)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := w.domain.NewAttempt("dkg", [32]byte{0xb1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := frost.DKGRequest{Group: frost.Group{Roster: w.sel.Roster(), Threshold: 3, Quorum: 4, Epoch: 1}, LocalSeats: []uint16{1, 2}, Participants: w.sel.Roster(), Attempt: attempt}
	intent, _ := json.Marshal(request)
	release, err := j.Claim(ctx, snowfallengine.AttemptID(attempt), "dkg", intent)
	if err != nil {
		t.Fatal(err)
	}
	record := w.record
	record.State = "RegisteredPendingReady"
	if err = j.SaveWallet(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err = j.SaveKey(ctx, w.key); err != nil {
		t.Fatal(err)
	}
	walletState := uint8(1)
	if saved == "ReadyUnfunded" {
		record.State = "ReadyUnfunded"
		if err = j.SaveWallet(ctx, record); err != nil {
			t.Fatal(err)
		}
		walletState = 2
	}
	release()
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(ctx, w.config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	journal, err := reopened.Scope(w.domain)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := journal.LoadWallet(ctx); err != nil || got.State != saved || got.Quarantined {
		t.Fatalf("harness: reopened wallet %s/%t (%v), want %s", got.State, got.Quarantined, err, saved)
	}

	w.chain.view = b10Approval(t, w.desc, 10, common.Hash{0xa1}, walletState)
	// FIXTURE: the other operator's readiness vote for seat 3, signed with
	// its operator key over the registry digest of the approved wallet.
	reference := crypto.Keccak256Hash([]byte{1, 3, 42})
	digest, _ := w.chain.ReadinessDigest(ctx, w.chain.view.Approved, 3, reference)
	signature, err := dkg.Sign(b10Signer(1), digest)
	if err != nil {
		t.Fatal(err)
	}
	w.bus.incoming <- dkg.Attestation{Stage: "ready", Epoch: 1, Seat: 3, Digest: digest, Reference: reference, Signature: signature}
	return journal
}

// ---------------------------------------------------------------------------
// FIXTURE: controlled chain adapter on the K-03 chain seam.

type b10Chain struct {
	params  dkg.Parameters
	sel     dkg.Selection
	changes map[b10Change]dkg.View

	mu           sync.Mutex
	view         dkg.View
	changed      bool
	released     bool
	release      chan struct{}
	seq          int64
	changeRead   int64 // sequence of the first read returning the changed view
	monitorAfter int   // background reads after the held reads were released
	held         int   // background reads currently held
	lastMain     time.Time
	classified   chan struct{}
	classifyOnce sync.Once
	sends        []int64
	submissions  []int64
}

func (c *b10Chain) apply(change b10Change) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if change != b10Unchanged {
		view, ok := c.changes[change]
		if !ok {
			panic("unknown change " + string(change))
		}
		c.view = view
		c.changed = true
	}
	c.released = true
	close(c.release)
}

// touch records main-path activity (any call made under Restore).
func (c *b10Chain) touch() {
	c.mu.Lock()
	c.lastMain = time.Now()
	c.mu.Unlock()
}

func (c *b10Chain) silentFor() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.lastMain)
}

func (c *b10Chain) heldNow() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held
}

// signalClassified marks that the monitor has classified a released read: it
// fired after-monitor-inspect, came back for a second read, or its context
// was cancelled (it quarantined and aborted Restore).
func (c *b10Chain) signalClassified() { c.classifyOnce.Do(func() { close(c.classified) }) }

func (c *b10Chain) monitorReadsAfterRelease() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.monitorAfter
}

func (c *b10Chain) submissionCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.submissions)
}

// after returns the first changed read and the attestations and certificate
// submissions made with a live context after it.
func (c *b10Chain) after() (int64, []int64, []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sends, submissions []int64
	if c.changeRead == 0 {
		return 0, nil, nil
	}
	for _, s := range c.sends {
		if s > c.changeRead {
			sends = append(sends, s)
		}
	}
	for _, s := range c.submissions {
		if s > c.changeRead {
			submissions = append(submissions, s)
		}
	}
	return c.changeRead, sends, submissions
}

// inBackground reports a registry read made outside Restore's own call stack
// (the monitor goroutine, whatever its private name). Executor.Restore is the
// exported entry point under test.
func inBackground() bool {
	pc := make([]uintptr, 64)
	frames := runtime.CallersFrames(pc[:runtime.Callers(2, pc)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, "/pkg/frost/dkg.(*Executor).Restore") {
			return false
		}
		if !more {
			return true
		}
	}
}

func (c *b10Chain) Parameters(context.Context) (dkg.Parameters, error) {
	c.touch()
	return c.params, nil
}
func (c *b10Chain) Selection(_ context.Context, epoch uint64) (dkg.Selection, error) {
	if epoch != c.sel.Epoch {
		return dkg.Selection{}, errors.New("FIXTURE registry: unknown epoch")
	}
	return c.sel, nil
}

func (c *b10Chain) View(ctx context.Context, _ uint64) (dkg.View, error) {
	background := inBackground()
	if !background {
		c.touch()
	} else {
		c.mu.Lock()
		release := c.release
		c.held++
		c.mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		c.mu.Lock()
		c.held--
		c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return dkg.View{}, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.view
	if background && c.released {
		c.monitorAfter++
		if c.monitorAfter > 1 {
			go c.signalClassified()
		}
		go func() {
			<-ctx.Done()
			c.signalClassified()
		}()
	}
	if c.changed {
		c.seq++
		if c.changeRead == 0 {
			c.changeRead = c.seq
		}
	}
	return v, nil
}

func (c *b10Chain) ReadinessDigest(_ context.Context, w dkg.Wallet, seat uint16, reference [32]byte) ([32]byte, error) {
	var s [2]byte
	binary.BigEndian.PutUint16(s[:], seat)
	return crypto.Keccak256Hash([]byte("B10 FIXTURE readiness"), w.DescriptorHash[:], s[:], reference[:]), nil
}

func (c *b10Chain) Validate(context.Context, uint64, dkg.Result) (bool, error) {
	return true, nil // FIXTURE: the submitted result is the host's own valid result.
}

func (c *b10Chain) Ready(ctx context.Context, id [32]byte, seats []uint16, _ [][32]byte, _ []byte) error {
	c.touch()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.submissions = append(c.submissions, c.seq)
	if c.view.ApprovedID != id || c.view.Approved.State != 1 || len(seats) < int(c.params.ReadySeats) {
		return errors.New("FIXTURE registry: readiness certificate reverted")
	}
	c.view.Approved.State = 2
	return nil
}

func (c *b10Chain) Expire(context.Context, [32]byte) error {
	return errors.New("FIXTURE registry: not expirable before the deadline")
}
func (c *b10Chain) ExpireDKG(context.Context) error {
	return errors.New("FIXTURE registry: DKG not timed out")
}
func (c *b10Chain) ResultDigest(context.Context, uint64, dkg.Result) ([32]byte, error) {
	return [32]byte{}, errors.New("FIXTURE registry: no result publication on restore")
}
func (c *b10Chain) Submit(context.Context, dkg.Result) error {
	return errors.New("FIXTURE registry: no result publication on restore")
}
func (c *b10Chain) Challenge(context.Context) error {
	return errors.New("FIXTURE registry: no challenge expected")
}
func (c *b10Chain) Approve(context.Context) error {
	return errors.New("FIXTURE registry: no approval on restore")
}

// ---------------------------------------------------------------------------
// FIXTURE: authenticated bus. Records the host's own readiness attestations.

type b10Bus struct {
	chain    *b10Chain
	incoming chan dkg.Attestation
}

func (b *b10Bus) Send(ctx context.Context, a dkg.Attestation) error {
	b.chain.touch()
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Stage == "ready" {
		b.chain.mu.Lock()
		b.chain.seq++
		b.chain.sends = append(b.chain.sends, b.chain.seq)
		b.chain.mu.Unlock()
	}
	return nil
}

func (b *b10Bus) Receive(ctx context.Context) (dkg.Attestation, error) {
	select {
	case a := <-b.incoming:
		return a, nil
	case <-ctx.Done():
		return dkg.Attestation{}, ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// FIXTURE: reload-only engine. Restore never runs a DKG or signs; the reload
// checks the request carries the durable KeyReady (the real fresh-worker
// reload is covered by the K-03 real-contract suite).

type b10Engine struct {
	journal *store.Scoped
	reloads atomic.Int32
}

func (e *b10Engine) DKG(context.Context, frost.DKGRequest, frost.Providers) (frost.KeyReady, error) {
	return frost.KeyReady{}, errors.New("FIXTURE engine: Restore must not rerun the DKG")
}
func (e *b10Engine) Sign(context.Context, frost.SigningRequest, frost.Providers) ([64]byte, error) {
	return [64]byte{}, errors.New("FIXTURE engine: no signing on restore")
}
func (e *b10Engine) ReloadKey(ctx context.Context, r frost.SigningRequest, _ frost.Providers) error {
	k, err := e.journal.LoadKey(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(k, r.Key) || len(k.LocalReferences) != len(r.LocalSeats) {
		return errors.New("FIXTURE engine: completed key reload mismatch")
	}
	e.reloads.Add(1)
	return nil
}

// ---------------------------------------------------------------------------
// FIXTURE: in-memory store fence, independent of the keystore directory.

type b10Fence struct {
	mu     sync.Mutex
	point  store.Point
	leased bool
}
type b10Lease struct{ f *b10Fence }

func (f *b10Fence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leased {
		return nil, store.ErrBusy
	}
	f.leased = true
	return b10Lease{f}, nil
}
func (l b10Lease) Head(context.Context) (store.Point, error) {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	return l.f.point, nil
}
func (l b10Lease) Advance(_ context.Context, before, after store.Point) error {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	if l.f.point != before {
		return store.ErrQuarantined
	}
	l.f.point = after
	return nil
}
func (l b10Lease) Close() error {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	l.f.leased = false
	return nil
}
