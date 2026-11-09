package dkg_test

// Acceptance tests for tuxxy/keep-core#139 after D31 and D32 (tux,
// 2026-11-09). Refs tuxxy/keep-core#139
//
// D31 (b): Run's success path treats an unclassified observer error as
// Restore does: the saved ReadyUnfunded record stays (unquarantined, not
// Closed), Run returns the error, and the node restores the wallet at its
// next startup.
//
// D32 (a): a readiness certificate submission refused because the executor
// had already cancelled itself (the certificate never left the host) is
// unclassified. The saved record stays, on Restore and on Run. A submission
// that did leave the host and whose outcome is still unresolved after the
// bounded reconciliation still quarantines.
//
// So:
//   - Restore, RegisteredPendingReady, at before-certificate-submit (the cell
//     restore_unclassified_test.go leaves out): an unclassified observer
//     error leaves the saved wallet as it was.
//   - Run, after its ReadyUnfunded save (after-wallet-ready-save) and at
//     before-certificate-submit: an unclassified observer error leaves the
//     saved wallet unquarantined and not Closed, and Run returns an error
//     that is not a classified outcome.
//   - Negative side, Run: a classified quarantine (approval replaced) read at
//     any ready-tail boundary is recorded; a classified expiry (wallet
//     closed) read before the ReadyUnfunded save is recorded as Closed.
//   - Negative side, both modes: a certificate that left the host (the
//     transactor accepted it with a live context) but whose receipt never
//     shows and whose wallet stays pending still quarantines.
// Run's other pre-success boundaries are not constrained here.
//
// Real code: dkg.Executor.Run (through WaitCandidateAcceptance, the shared
// ready tail and its finishers) and Executor.Restore, their monitor and
// receive loop, and the real encrypted journal (pkg/frost/store).
//
// FIXTURES (labelled, test-only), built on the batch 10 world:
//   - b12rChain: the registry. Main-path reads (any call under Run or
//     Restore) are answered from the current view and refused on a cancelled
//     context, as a real RPC read is. Background reads (the monitor) are not
//     held; once the case arms a fault, the next one returns it. Submit
//     approves the submitted result at once (block 10, head 20, so finality
//     holds). Ready certifies as the batch 10 adapter does, or, armed
//     "unresolved", takes the certificate with a live context and reports a
//     receipt that never shows, leaving the wallet pending.
//   - b12rBus: the other operator (seats 3 and 4) answers the host's result
//     attestation with its two signed result votes and the host's readiness
//     attestation with its signed seat-3 readiness vote; the receive loop can
//     fail when armed.
//   - b12rEngine: the DKG engine. It claims the attempt in the journal, waits
//     for the host's candidate acceptance and stores the completed key, as
//     the guarded engine does; the reload is the batch 10 reload-only engine.
//   - The RPC errors are b12Fault (restore_unclassified_test.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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

const b12rInvariant = "A-4/D31/D32 an unclassified error leaves the saved wallet (tuxxy/keep-core#139)"

// TestRestoredHostKeepsWalletOnCancelledCertificate (D32 (a), Restore): the
// before-certificate-submit cell of the restore matrix.
func TestRestoredHostKeepsWalletOnCancelledCertificate(t *testing.T) {
	for _, source := range b12Sources {
		source := source
		t.Run(fmt.Sprintf("RegisteredPendingReady/before-certificate-submit/%s", source), func(t *testing.T) {
			t.Parallel()
			r := b12rExec(t, "Restore", "RegisteredPendingReady", "before-certificate-submit", b12rArm{source: source})
			got, stored := r.outcome, r.stored
			if stored.Quarantined || stored.State == "Closed" {
				t.Errorf("%s: saved wallet changed after an unclassified %s at before-certificate-submit (restored from RegisteredPendingReady; the certificate never left the host): durable state=%s quarantined=%t; Restore returned err=%v (want the saved wallet as it was)",
					b12rInvariant, source, stored.State, stored.Quarantined, got.err)
			}
			if stored.State != "RegisteredPendingReady" && stored.State != "ReadyUnfunded" {
				t.Errorf("%s: saved wallet state %s after an unclassified %s at before-certificate-submit", b12rInvariant, stored.State, source)
			}
			if !b12SameIdentity(stored, r.saved) {
				t.Errorf("%s: saved wallet identity or approval fields changed by an unclassified %s at before-certificate-submit", b12rInvariant, source)
			}
			if errors.Is(got.err, dkg.ErrQuarantined) || errors.Is(got.err, dkg.ErrExpired) || got.record.Quarantined || got.record.State == "Closed" {
				t.Errorf("%s: Restore reported a classified outcome for an unclassified %s at before-certificate-submit: err=%v, record state=%s quarantined=%t",
					b12rInvariant, source, got.err, got.record.State, got.record.Quarantined)
			}
			r.reportHarness(t)
		})
	}
}

// TestRunHostKeepsWalletOnUnclassifiedError (D31 (b) and D32 (a), Run).
func TestRunHostKeepsWalletOnUnclassifiedError(t *testing.T) {
	for _, boundary := range []string{"before-certificate-submit", "after-wallet-ready-save"} {
		for _, source := range b12Sources {
			boundary, source := boundary, source
			t.Run(fmt.Sprintf("%s/%s", boundary, source), func(t *testing.T) {
				t.Parallel()
				r := b12rExec(t, "Run", "", boundary, b12rArm{source: source})
				got, stored := r.outcome, r.stored
				want := "ReadyUnfunded"
				if boundary == "before-certificate-submit" {
					want = "RegisteredPendingReady"
				}
				if stored.Quarantined || stored.State == "Closed" {
					t.Errorf("%s: Run host's saved wallet changed after an unclassified %s at %s: durable state=%s quarantined=%t; Run returned err=%v (want %s unquarantined)",
						b12rInvariant, source, boundary, stored.State, stored.Quarantined, got.err, want)
				}
				if stored.State != want && stored.State != "ReadyUnfunded" {
					t.Errorf("%s: Run host's saved wallet state %s after an unclassified %s at %s, want %s", b12rInvariant, stored.State, source, boundary, want)
				}
				if got.err == nil {
					t.Errorf("%s: Run returned success after an unclassified %s at %s (D31: Run returns the error)", b12rInvariant, source, boundary)
				}
				if errors.Is(got.err, dkg.ErrQuarantined) || errors.Is(got.err, dkg.ErrExpired) || got.record.Quarantined || got.record.State == "Closed" {
					t.Errorf("%s: Run reported a classified outcome for an unclassified %s at %s: err=%v, record state=%s quarantined=%t",
						b12rInvariant, source, boundary, got.err, got.record.State, got.record.Quarantined)
				}
				r.reportHarness(t)
			})
		}
	}
}

// TestRunHostRecordsClassifiedOutcome is the Run negative side: a classified
// quarantine read by the monitor at any ready-tail boundary is recorded, and
// a classified expiry read before the ReadyUnfunded save is recorded as
// Closed (the store refuses ReadyUnfunded -> Closed).
func TestRunHostRecordsClassifiedOutcome(t *testing.T) {
	boundaries := []string{"after-key-reload", "before-readiness-collect", "before-certificate-submit", "after-certificate-submit", "after-readiness-postcondition", "after-wallet-ready-save"}
	expiry := map[string]bool{"after-key-reload": true, "before-readiness-collect": true, "before-certificate-submit": true, "after-certificate-submit": true}
	for _, boundary := range boundaries {
		for _, classified := range []b12Classified{b12Quarantine, b12Expiry} {
			if classified == b12Expiry && !expiry[boundary] {
				continue
			}
			boundary, classified := boundary, classified
			t.Run(fmt.Sprintf("%s/%s", boundary, classified), func(t *testing.T) {
				t.Parallel()
				r := b12rExec(t, "Run", "", boundary, b12rArm{classified: classified})
				got, stored := r.outcome, r.stored
				switch classified {
				case b12Quarantine:
					if !errors.Is(got.err, dkg.ErrQuarantined) || !stored.Quarantined {
						t.Errorf("%s (negative side): Run host did not record the quarantine its monitor read at %s: err=%v, durable state=%s quarantined=%t (want ErrQuarantined, quarantined)",
							b12rInvariant, boundary, got.err, stored.State, stored.Quarantined)
					}
				case b12Expiry:
					if !errors.Is(got.err, dkg.ErrExpired) || stored.State != "Closed" || stored.Quarantined {
						t.Errorf("%s (negative side): Run host did not record the expiry its monitor read at %s: err=%v, durable state=%s quarantined=%t (want ErrExpired, Closed unquarantined)",
							b12rInvariant, boundary, got.err, stored.State, stored.Quarantined)
					}
				}
				r.reportHarness(t)
			})
		}
	}
}

// TestUnresolvedSentCertificateStillQuarantines is the D32 negative side: a
// certificate that left the host (accepted by the transactor with a live
// context) whose outcome stays unresolved after reconciliation (the wallet
// still pending) quarantines, on Run and on Restore. A fix that treats every
// unresolved submission as unclassified fails here.
func TestUnresolvedSentCertificateStillQuarantines(t *testing.T) {
	for _, mode := range []string{"Run", "Restore"} {
		mode := mode
		t.Run(mode+"/before-certificate-submit/sent-unresolved", func(t *testing.T) {
			t.Parallel()
			saved := ""
			if mode == "Restore" {
				saved = "RegisteredPendingReady"
			}
			r := b12rExec(t, mode, saved, "before-certificate-submit", b12rArm{unresolved: true})
			got, stored := r.outcome, r.stored
			if !errors.Is(got.err, dkg.ErrQuarantined) || !stored.Quarantined {
				t.Errorf("%s (negative side): %s host's certificate left the host and stayed unresolved, but the wallet was not quarantined: err=%v, durable state=%s quarantined=%t",
					b12rInvariant, mode, got.err, stored.State, stored.Quarantined)
			}
			if n := r.chain.sentCertificates(); n != 1 {
				t.Errorf("harness: %d certificates left the host, want 1", n)
			}
			r.reportHarness(t)
		})
	}
}

// ---------------------------------------------------------------------------

type b12rArm struct {
	source     b12Source
	classified b12Classified
	unresolved bool
}

type b12rResult struct {
	b12Result
	chain *b12rChain
}

// b12rExec runs the real Executor.Run (fresh store) or Executor.Restore (from
// the saved state) and arms the case at boundary. For a fault or a
// classified view it waits (bounded) until the fault or view reached an
// observer and the executor's context was cancelled, then lets the main path
// go on.
func b12rExec(t *testing.T, mode, saved, boundary string, arm b12rArm) b12rResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	w := newB10World(t)
	var journal *store.Scoped
	var before dkg.WalletRecord
	attempt, err := w.domain.NewAttempt("dkg", [32]byte{0xb1, 0x2f}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := frost.DKGRequest{Group: frost.Group{Roster: w.sel.Roster(), Threshold: 3, Quorum: 4, Epoch: 1}, LocalSeats: []uint16{1, 2}, Participants: w.sel.Roster(), Attempt: attempt}
	if mode == "Restore" {
		journal = w.persist(ctx, t, saved)
		if before, err = journal.LoadWallet(ctx); err != nil {
			t.Fatalf("harness: reading the saved wallet: %v", err)
		}
	} else {
		root, err := store.Open(ctx, store.Config{Keystore: filepath.Join(w.root, "keystore"), StorageID: [32]byte{0xb1, 0x2f}, Key: [32]byte{0xb1, 0x2f, 4}, Fence: w.fence})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = root.Close() })
		if journal, err = root.Scope(w.domain); err != nil {
			t.Fatal(err)
		}
		w.chain.mu.Lock()
		w.chain.view = dkg.View{Head: 5, Epoch: 1, State: 2, ResultDeadline: 100}
		w.chain.mu.Unlock()
	}
	chain := &b12rChain{b10Chain: w.chain, desc: w.desc, delivered: make(chan struct{})}
	bus := &b12rBus{b10Bus: w.bus, chain: chain, run: mode == "Run", fault: make(chan error, 1)}
	engine := &b12rEngine{b10Engine: &b10Engine{journal: journal}, key: w.key}

	var (
		problemsMu sync.Mutex
		problems   []string
		reached    atomic.Bool
	)
	problem := func(format string, args ...any) {
		problemsMu.Lock()
		defer problemsMu.Unlock()
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	e := &dkg.Executor{
		Chain:          chain,
		Engine:         engine,
		Journal:        journal,
		Bus:            bus,
		Signer:         b10Signer(0),
		FinalityBlocks: 2,
		PollInterval:   2 * time.Millisecond,
	}
	if mode == "Run" {
		e.Request = request
	}
	e.Boundary = func(name string) error {
		if name != boundary || !reached.CompareAndSwap(false, true) {
			return nil
		}
		switch {
		case arm.unresolved:
			chain.armUnresolved()
			return nil
		case arm.source == b12BusReceive:
			bus.fault <- b12Fault(arm.source)
		case arm.source != "":
			chain.armFault(b12Fault(arm.source))
		case arm.classified != "":
			chain.armView(arm.classified)
		}
		select {
		case <-chain.delivered:
		case <-time.After(3 * time.Second):
			problem("the armed %s%s was not delivered within 3 s", arm.source, arm.classified)
			return nil
		}
		select {
		case <-chain.cancelled():
		case <-time.After(500 * time.Millisecond):
		}
		return nil
	}

	done := make(chan b10Outcome, 1)
	go func() {
		if mode == "Run" {
			record, err := e.Run(ctx)
			done <- b10Outcome{record: record, err: err}
			return
		}
		status, record, err := e.Restore(ctx)
		done <- b10Outcome{status, record, err}
	}()
	var got b10Outcome
	select {
	case got = <-done:
	case <-time.After(9 * time.Second):
		t.Fatalf("harness: %s did not return within 9 s (boundary %s reached=%t)", mode, boundary, reached.Load())
	}
	stored, err := journal.LoadWallet(context.Background())
	if err != nil {
		t.Fatalf("harness: reading the saved wallet after %s: %v", mode, err)
	}
	if !reached.Load() {
		problem("%s returned before boundary %s (err=%v)", mode, boundary, got.err)
	}
	if engine.reloads.Load() != 1 {
		problem("%d key reloads, want 1", engine.reloads.Load())
	}
	problemsMu.Lock()
	defer problemsMu.Unlock()
	return b12rResult{b12Result: b12Result{outcome: got, stored: stored, saved: before, harness: append([]string(nil), problems...)}, chain: chain}
}

// ---------------------------------------------------------------------------
// FIXTURE: registry adapter for Run and Restore (see the file comment).

type b12rChain struct {
	*b10Chain
	desc      dkg.Descriptor
	delivered chan struct{}

	mu         sync.Mutex
	fault      error
	faultArmed bool
	viewArmed  bool
	unresolved bool
	sent       int
	ctx        context.Context
	deliver    sync.Once
}

// onMainPath reports a call made under Run's or Restore's own call stack.
func onMainPath() bool {
	pc := make([]uintptr, 64)
	frames := runtime.CallersFrames(pc[:runtime.Callers(2, pc)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, "/pkg/frost/dkg.(*Executor).Run") || strings.HasSuffix(f.Function, "/pkg/frost/dkg.(*Executor).Restore") {
			return true
		}
		if !more {
			return false
		}
	}
}

func (c *b12rChain) current() dkg.View {
	c.b10Chain.mu.Lock()
	defer c.b10Chain.mu.Unlock()
	return c.b10Chain.view
}

func (c *b12rChain) armFault(fault error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fault, c.faultArmed = fault, true
}

// armView replaces the approved view with a classified one: the same wallet
// approved in another block (quarantine) or the wallet closed (expiry).
func (c *b12rChain) armView(classified b12Classified) {
	c.b10Chain.mu.Lock()
	v := c.b10Chain.view
	switch classified {
	case b12Quarantine:
		v.ApprovalHash = common.Hash{0xa2}
		v.Approved.ApprovalBlock = 12
	case b12Expiry:
		v.Approved.State = 3
	}
	c.b10Chain.view = v
	c.b10Chain.mu.Unlock()
	c.mu.Lock()
	c.viewArmed = true
	c.mu.Unlock()
}

func (c *b12rChain) armUnresolved() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unresolved = true
}

func (c *b12rChain) sentCertificates() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent
}

func (c *b12rChain) markDelivered(ctx context.Context) {
	c.deliver.Do(func() {
		c.mu.Lock()
		c.ctx = ctx
		c.mu.Unlock()
		close(c.delivered)
	})
}

func (c *b12rChain) cancelled() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil {
		return make(chan struct{})
	}
	return c.ctx.Done()
}

func (c *b12rChain) View(ctx context.Context, _ uint64) (dkg.View, error) {
	if err := ctx.Err(); err != nil {
		return dkg.View{}, err
	}
	if !onMainPath() {
		c.mu.Lock()
		fault, armed := c.fault, c.faultArmed
		c.faultArmed = false
		viewArmed := c.viewArmed
		c.mu.Unlock()
		if armed {
			c.markDelivered(ctx)
			return dkg.View{}, fault
		}
		if viewArmed {
			c.markDelivered(ctx)
		}
	}
	return c.current(), nil
}

func (c *b12rChain) ReadinessDigest(ctx context.Context, w dkg.Wallet, seat uint16, reference [32]byte) ([32]byte, error) {
	if err := ctx.Err(); err != nil {
		return [32]byte{}, err
	}
	return c.b10Chain.ReadinessDigest(ctx, w, seat, reference)
}

func (c *b12rChain) ResultDigest(ctx context.Context, _ uint64, r dkg.Result) ([32]byte, error) {
	if err := ctx.Err(); err != nil {
		return [32]byte{}, err
	}
	return dkg.ResultHash(r)
}

// Submit approves the submitted result at once: block 10, head 20.
func (c *b12rChain) Submit(ctx context.Context, r dkg.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rh, err := dkg.ResultHash(r)
	if err != nil {
		return err
	}
	dh, err := dkg.DescriptorHash(c.desc)
	if err != nil {
		return err
	}
	c.b10Chain.mu.Lock()
	defer c.b10Chain.mu.Unlock()
	c.b10Chain.view = dkg.View{Head: 20, Epoch: 1, State: 0, ResultDeadline: 100, ApprovedID: dkg.WalletID(c.desc.OutputKey), ApprovalHash: common.Hash{0xa1},
		Approved: dkg.Wallet{Descriptor: c.desc, ResultHash: rh, DescriptorHash: dh, ApprovalBlock: 10, Deadline: 1000, State: 1}}
	return nil
}

// Ready certifies as the batch 10 adapter does. Armed unresolved, it takes
// the certificate (live context: it left the host) and reports a receipt
// that never shows; the wallet stays pending.
func (c *b12rChain) Ready(ctx context.Context, id [32]byte, seats []uint16, refs [][32]byte, sigs []byte) error {
	c.mu.Lock()
	unresolved := c.unresolved
	c.mu.Unlock()
	if !unresolved {
		return c.b10Chain.Ready(ctx, id, seats, refs, sigs)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.sent++
	c.mu.Unlock()
	return errors.New("FIXTURE transactor: readiness transaction sent; receipt not seen before timeout")
}

// ---------------------------------------------------------------------------
// FIXTURE: bus with the other operator's votes (seats 3 and 4, key 1).

type b12rBus struct {
	*b10Bus
	chain *b12rChain
	run   bool
	fault chan error

	mu                sync.Mutex
	answeredResult    bool
	answeredReadiness bool
}

func (b *b12rBus) Send(ctx context.Context, a dkg.Attestation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.run || a.Seat != 1 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case a.Stage == "result" && !b.answeredResult:
		b.answeredResult = true
		for _, seat := range []uint16{3, 4} {
			signature, err := dkg.Sign(b10Signer(1), a.Digest)
			if err != nil {
				return err
			}
			b.incoming <- dkg.Attestation{Stage: "result", Epoch: 1, Seat: seat, Digest: a.Digest, Signature: signature}
		}
	case a.Stage == "ready" && !b.answeredReadiness:
		b.answeredReadiness = true
		reference := crypto.Keccak256Hash([]byte{1, 3, 42})
		digest, err := b.chain.b10Chain.ReadinessDigest(ctx, b.chain.current().Approved, 3, reference)
		if err != nil {
			return err
		}
		signature, err := dkg.Sign(b10Signer(1), digest)
		if err != nil {
			return err
		}
		b.incoming <- dkg.Attestation{Stage: "ready", Epoch: 1, Seat: 3, Digest: digest, Reference: reference, Signature: signature}
	}
	return nil
}

func (b *b12rBus) Receive(ctx context.Context) (dkg.Attestation, error) {
	select {
	case a := <-b.incoming:
		return a, nil
	case err := <-b.fault:
		b.chain.markDelivered(ctx)
		return dkg.Attestation{}, err
	case <-ctx.Done():
		return dkg.Attestation{}, ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// FIXTURE: DKG engine. Claims the attempt, waits for the host's acceptance of
// the candidate, stores the completed key (as the guarded engine does). The
// reload is the batch 10 reload-only engine.

type b12rEngine struct {
	*b10Engine
	key frost.KeyReady
}

func (e *b12rEngine) DKG(ctx context.Context, r frost.DKGRequest, p frost.Providers) (frost.KeyReady, error) {
	intent, err := json.Marshal(r)
	if err != nil {
		return frost.KeyReady{}, err
	}
	release, err := e.journal.Claim(ctx, snowfallengine.AttemptID(r.Attempt), "dkg", intent)
	if err != nil {
		return frost.KeyReady{}, err
	}
	defer release()
	if _, err = p.Acceptance.WaitCandidateAcceptance(ctx, e.key.Candidate); err != nil {
		return frost.KeyReady{}, err
	}
	if err = e.journal.SaveKey(ctx, e.key); err != nil {
		return frost.KeyReady{}, err
	}
	return e.key, nil
}
