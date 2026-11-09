package dkg_test

// Acceptance tests for tuxxy/keep-core#139 (FSM analysis A-4, P2).
// Refs tuxxy/keep-core#139
//
// Invariant (the Executor.Restore doc comment): "A failed reload or an
// unclassified error is returned without changing the saved wallet, so a
// later startup can restore it; an expiry or quarantine is recorded as Run
// records it." finishRestore applies that rule; the shared success tail
// (ready -> finishSuccess) must apply it too. So, for a host restored from its
// saved RegisteredPendingReady or ReadyUnfunded record:
//   - an unclassified error raised by its background observers (the approval
//     monitor's registry read failing with an RPC error, or the attestation
//     bus failing) never quarantines or closes the saved wallet, whichever
//     restore boundary it arrives at, including after the readiness
//     postcondition and after the ReadyUnfunded save;
//   - a classified quarantine (approval replaced) or expiry (wallet closed)
//     read by the same monitor at the same boundaries is still recorded.
//
// The before-certificate-submit cell (D32) and Run's success rule (D31) are
// tested in run_unclassified_test.go.
//
// A restore that goes on after an unclassified error must still watch the
// approval: TestRestoredHostStillWatchesAfterUnclassifiedError (review B1).
//
// "Without changing the saved wallet" is checked on the durable record: it
// stays unquarantined, never Closed, with every identity and approval field
// as saved. Its state is the saved one or ReadyUnfunded: the main path may
// legitimately have saved ReadyUnfunded before the error arrived (the
// after-wallet-ready-save boundary), and that save is not the error's doing.
// Whether Restore returns the unclassified error or completes is not
// asserted: the doc comment says it is returned, but a monitor that retries a
// transient read before classifying is a correct design too, and the
// acceptance criterion is about the saved wallet. Restore must not report a
// classified outcome it did not observe.
//
// Real code: dkg.Executor.Restore, its monitor, receive loop and finishers,
// and the real encrypted journal (pkg/frost/store), written as a Run host
// leaves it and reopened as a fresh process would (the batch 10 world,
// restore_removal_test.go, reused unchanged).
//
// FIXTURES (labelled, test-only): the batch 10 controlled chain adapter,
// wrapped here so that the monitor's held registry read returns an injected
// error or a classified view; the batch 10 bus, wrapped so that its receive
// loop can fail; the batch 10 reload-only engine and in-memory fence. The
// injected RPC errors model go-ethereum's JSON-RPC errors: a transport
// failure, and the two empty-revert encodings (code 3 with data "0x", and
// code -32000 with no data).
//
// Scheduling: as in batch 10, the monitor's first registry read is held until
// the case acts at its boundary. The case then injects its error (or view),
// releases the read, waits until the fault was delivered and the restore's
// context was cancelled by the observer that published it (bounded), and lets
// the main path continue.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

const b12Invariant = "A-4 restore keeps the saved wallet on an unclassified error (tuxxy/keep-core#139)"

type b12Source string

const (
	// The monitor's registry read fails at the transport (node unreachable).
	b12MonitorTransport b12Source = "monitor-read-transport-error"
	// The monitor's registry read reverts empty, code 3 / data "0x".
	b12MonitorRevert3 b12Source = "monitor-read-empty-revert-code3"
	// The monitor's registry read reverts empty, code -32000 / no data.
	b12MonitorRevert32000 b12Source = "monitor-read-empty-revert-32000"
	// The attestation bus's receive loop fails (channel closed).
	b12BusReceive b12Source = "bus-receive-error"
)

var b12Sources = []b12Source{b12MonitorTransport, b12MonitorRevert3, b12MonitorRevert32000, b12BusReceive}

type b12Classified string

const (
	// The monitor reads the same wallet approved in another block.
	b12Quarantine b12Classified = "classified-quarantine"
	// The monitor reads the saved approval with the wallet closed.
	b12Expiry b12Classified = "classified-expiry"
)

// FIXTURE: a go-ethereum style JSON-RPC error (rpc.Error and rpc.DataError).
type b12RPCError struct {
	message string
	code    int
	data    interface{}
}

func (e *b12RPCError) Error() string          { return e.message }
func (e *b12RPCError) ErrorCode() int         { return e.code }
func (e *b12RPCError) ErrorData() interface{} { return e.data }

func b12Fault(source b12Source) error {
	switch source {
	case b12MonitorTransport:
		return errors.New(`FIXTURE RPC: Post "http://127.0.0.1:8545": dial tcp 127.0.0.1:8545: connect: connection refused`)
	case b12MonitorRevert3:
		return &b12RPCError{message: "execution reverted", code: 3, data: "0x"}
	case b12MonitorRevert32000:
		return &b12RPCError{message: "execution reverted", code: -32000}
	case b12BusReceive:
		return errors.New("FIXTURE bus: attestation channel closed")
	}
	panic("unknown source " + string(source))
}

// TestRestoredHostKeepsWalletOnUnclassifiedError: an unclassified observer
// error at any restore boundary leaves the saved wallet as it was.
func TestRestoredHostKeepsWalletOnUnclassifiedError(t *testing.T) {
	for _, saved := range []string{"RegisteredPendingReady", "ReadyUnfunded"} {
		for _, boundary := range b10Boundaries[saved] {
			if boundary == "before-certificate-submit" {
				// Covered by TestRestoredHostKeepsWalletOnCancelledCertificate
				// in run_unclassified_test.go (D32 (a): a certificate refused
				// on the executor's own cancellation is unclassified), with
				// the sent-but-unresolved negative beside it.
				continue
			}
			for _, source := range b12Sources {
				saved, boundary, source := saved, boundary, source
				t.Run(fmt.Sprintf("%s/%s/%s", saved, boundary, source), func(t *testing.T) {
					t.Parallel()
					r := b12Restore(t, saved, boundary, source, "")
					got, stored, want := r.outcome, r.stored, r.saved
					if stored.Quarantined {
						t.Errorf("%s: saved wallet quarantined after an unclassified %s at %s (restored from %s): durable state=%s quarantined=true; Restore returned err=%v (want the saved wallet as it was)",
							b12Invariant, source, boundary, saved, stored.State, got.err)
					}
					if stored.State != saved && stored.State != "ReadyUnfunded" {
						t.Errorf("%s: saved wallet state changed by an unclassified %s at %s: durable state=%s, want %s (or ReadyUnfunded if the main path saved it before the error)",
							b12Invariant, source, boundary, stored.State, saved)
					}
					if !b12SameIdentity(stored, want) {
						t.Errorf("%s: saved wallet identity or approval fields changed by an unclassified %s at %s", b12Invariant, source, boundary)
					}
					if errors.Is(got.err, dkg.ErrQuarantined) || errors.Is(got.err, dkg.ErrExpired) || got.record.Quarantined || got.record.State == "Closed" {
						t.Errorf("%s: Restore reported a classified outcome for an unclassified %s at %s: err=%v, record state=%s quarantined=%t",
							b12Invariant, source, boundary, got.err, got.record.State, got.record.Quarantined)
					}
					if got.err == nil && got.record.State != "ReadyUnfunded" {
						t.Errorf("%s: Restore succeeded after an unclassified %s at %s with record state %s, want ReadyUnfunded",
							b12Invariant, source, boundary, got.record.State)
					}
					r.reportHarness(t)
				})
			}
		}
	}
}

// TestRestoredHostRecordsClassifiedOutcome is the negative side: the same
// held monitor read, released at the same boundaries with a classified view,
// is still recorded. A quarantine (the approval replaced) quarantines the
// saved wallet and Restore returns ErrQuarantined, at every boundary of both
// saved states. An expiry (the pending wallet closed) closes it and Restore
// returns ErrExpired, at the boundaries before the ReadyUnfunded save (the
// store refuses ReadyUnfunded -> Closed; that rule is not #139's). A fix
// that never quarantines or closes on restore, or that drops every cause
// after the restore's success tail, fails here.
func TestRestoredHostRecordsClassifiedOutcome(t *testing.T) {
	expiryBoundaries := map[string]bool{"after-key-reload": true, "before-readiness-collect": true, "before-certificate-submit": true, "after-certificate-submit": true}
	for _, saved := range []string{"RegisteredPendingReady", "ReadyUnfunded"} {
		for _, boundary := range b10Boundaries[saved] {
			for _, classified := range []b12Classified{b12Quarantine, b12Expiry} {
				if classified == b12Expiry && (saved != "RegisteredPendingReady" || !expiryBoundaries[boundary]) {
					continue
				}
				saved, boundary, classified := saved, boundary, classified
				t.Run(fmt.Sprintf("%s/%s/%s", saved, boundary, classified), func(t *testing.T) {
					t.Parallel()
					r := b12Restore(t, saved, boundary, "", classified)
					got, stored := r.outcome, r.stored
					switch classified {
					case b12Quarantine:
						if !errors.Is(got.err, dkg.ErrQuarantined) {
							t.Errorf("%s (negative side): restored host from %s did not report the quarantine its monitor read at %s: err=%v, record state=%s quarantined=%t (want ErrQuarantined)",
								b12Invariant, saved, boundary, got.err, got.record.State, got.record.Quarantined)
						}
						if !stored.Quarantined {
							t.Errorf("%s (negative side): classified quarantine read at %s not recorded: durable state=%s quarantined=false",
								b12Invariant, boundary, stored.State)
						}
					case b12Expiry:
						if !errors.Is(got.err, dkg.ErrExpired) {
							t.Errorf("%s (negative side): restored host from %s did not report the expiry its monitor read at %s: err=%v, record state=%s (want ErrExpired)",
								b12Invariant, saved, boundary, got.err, got.record.State)
						}
						if stored.State != "Closed" || stored.Quarantined {
							t.Errorf("%s (negative side): classified expiry read at %s not recorded: durable state=%s quarantined=%t, want Closed unquarantined",
								b12Invariant, boundary, stored.State, stored.Quarantined)
						}
					}
					r.reportHarness(t)
				})
			}
		}
	}
}

func b12SameIdentity(a, b dkg.WalletRecord) bool {
	a.State, b.State = "", ""
	a.Quarantined, b.Quarantined = false, false
	return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b)
}

type b12Result struct {
	outcome b10Outcome
	stored  dkg.WalletRecord
	saved   dkg.WalletRecord // the durable record before Restore
	harness []string
}

func (r b12Result) reportHarness(t *testing.T) {
	t.Helper()
	for _, problem := range r.harness {
		t.Errorf("harness: %s", problem)
	}
}

// b12Restore runs the real Executor.Restore from a saved state. At boundary it
// arms either an unclassified fault (source) or a classified view
// (classified), releases the monitor's held read and waits (bounded) until
// the fault was delivered and the restore's context cancelled, then lets
// Restore continue. It returns Restore's outcome and the durable wallet before
// and after.
func b12Restore(t *testing.T, saved, boundary string, source b12Source, classified b12Classified) b12Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	w := newB10World(t)
	journal := w.persist(ctx, t, saved)
	before, err := journal.LoadWallet(ctx)
	if err != nil {
		t.Fatalf("harness: reading the saved wallet: %v", err)
	}
	chain := &b12Chain{b10Chain: w.chain, release: make(chan struct{}), delivered: make(chan struct{})}
	bus := &b12Bus{b10Bus: w.bus, fault: make(chan error, 1), chain: chain}

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
	engine := &b10Engine{journal: journal}
	e := &dkg.Executor{
		Chain:        chain,
		Engine:       engine,
		Journal:      journal,
		Bus:          bus,
		Signer:       b10Signer(0),
		PollInterval: 2 * time.Millisecond,
	}
	e.Boundary = func(name string) error {
		if name != boundary || !reached.CompareAndSwap(false, true) {
			return nil
		}
		switch {
		case source == b12BusReceive:
			bus.fault <- b12Fault(source)
			chain.arm(nil, nil)
		case source != "":
			chain.arm(b12Fault(source), nil)
		case classified == b12Quarantine:
			v := b10Approval(t, w.desc, 12, common.Hash{0xa2}, 1)
			chain.arm(nil, &v)
		case classified == b12Expiry:
			v := b10Approval(t, w.desc, 10, common.Hash{0xa1}, 3)
			chain.arm(nil, &v)
		}
		// Wait until the armed fault or view reached its observer and that
		// observer cancelled the restore (it published the cause). A design
		// that retries a transient read instead never cancels; that wait then
		// ends after 500 ms and the case goes on.
		select {
		case <-chain.delivered:
		case <-time.After(3 * time.Second):
			problem("the armed %s%s was not delivered within 3 s", source, classified)
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
		status, record, err := e.Restore(ctx)
		done <- b10Outcome{status, record, err}
	}()
	var got b10Outcome
	select {
	case got = <-done:
	case <-time.After(9 * time.Second):
		t.Fatalf("harness: Restore did not return within 9 s (boundary %s reached=%t)", boundary, reached.Load())
	}
	stored, err := journal.LoadWallet(context.Background())
	if err != nil {
		t.Fatalf("harness: reading the saved wallet after Restore: %v", err)
	}
	if !reached.Load() {
		problem("Restore returned before boundary %s (err=%v)", boundary, got.err)
	}
	if engine.reloads.Load() != 1 {
		problem("%d key reloads, want 1", engine.reloads.Load())
	}
	problemsMu.Lock()
	defer problemsMu.Unlock()
	return b12Result{outcome: got, stored: stored, saved: before, harness: append([]string(nil), problems...)}
}

// ---------------------------------------------------------------------------
// FIXTURE: the batch 10 chain adapter with an armable monitor read. Reads made
// under Restore's own call stack go to the batch 10 adapter (answered at
// once, and refused on a cancelled context as a real RPC read is). Background
// reads (the monitor) are held until the case arms the
// adapter at its boundary; the first released read then returns the armed
// error, or the armed view (which also becomes the chain's view, so later
// main-path reads see it too).

type b12Chain struct {
	*b10Chain
	release   chan struct{}
	delivered chan struct{}

	mu       sync.Mutex
	fault    error
	armed    bool
	consumed bool
	ctx      context.Context
	deliver  sync.Once
}

func (c *b12Chain) arm(fault error, view *dkg.View) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fault = fault
	if view != nil {
		c.b10Chain.mu.Lock()
		c.b10Chain.view = *view
		c.b10Chain.mu.Unlock()
	}
	c.armed = true
	close(c.release)
}

// markDelivered records that the armed fault or view reached an observer
// running under ctx (the restore's internal context).
func (c *b12Chain) markDelivered(ctx context.Context) {
	c.deliver.Do(func() {
		c.mu.Lock()
		c.ctx = ctx
		c.mu.Unlock()
		close(c.delivered)
	})
}

// cancelled is closed once the restore's internal context, seen by the
// observer that received the armed fault or view, is cancelled.
func (c *b12Chain) cancelled() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil {
		ch := make(chan struct{})
		return ch
	}
	return c.ctx.Done()
}

func (c *b12Chain) View(ctx context.Context, epoch uint64) (dkg.View, error) {
	if !inBackground() {
		// A real RPC read fails on a cancelled context.
		if err := ctx.Err(); err != nil {
			return dkg.View{}, err
		}
		return c.b10Chain.View(ctx, epoch)
	}
	select {
	case <-c.release:
	case <-ctx.Done():
		return dkg.View{}, ctx.Err()
	}
	c.mu.Lock()
	first := c.armed && !c.consumed
	c.consumed = true
	fault := c.fault
	c.mu.Unlock()
	if first {
		c.markDelivered(ctx)
		if fault != nil {
			return dkg.View{}, fault
		}
	}
	c.b10Chain.mu.Lock()
	defer c.b10Chain.mu.Unlock()
	return c.b10Chain.view, nil
}

// ReadinessDigest is the registry's view call; like any RPC read it fails on a
// cancelled context.
func (c *b12Chain) ReadinessDigest(ctx context.Context, w dkg.Wallet, seat uint16, reference [32]byte) ([32]byte, error) {
	if err := ctx.Err(); err != nil {
		return [32]byte{}, err
	}
	return c.b10Chain.ReadinessDigest(ctx, w, seat, reference)
}

// FIXTURE: the batch 10 bus whose receive loop fails once the case arms it.
type b12Bus struct {
	*b10Bus
	fault chan error
	chain *b12Chain
}

func (b *b12Bus) Receive(ctx context.Context) (dkg.Attestation, error) {
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

// TestRestoredHostStillWatchesAfterUnclassifiedError (review B1): the
// tolerance above ("a design that retries a transient read") holds only if
// the restored host keeps watching. An unclassified monitor fault early in
// the restore (after-key-reload), then the approval replaced after the
// ReadyUnfunded save: a restore that stopped on the fault (error returned,
// wallet unchanged) passes; one that went on must report and record the
// quarantine (batch 10, P-K03-INCONSISTENCY-QUARANTINE). A monitor that
// silently gives up on the fault returns success with the replaced approval
// missed, and fails here.
func TestRestoredHostStillWatchesAfterUnclassifiedError(t *testing.T) {
	for _, saved := range []string{"RegisteredPendingReady", "ReadyUnfunded"} {
		for _, source := range []b12Source{b12MonitorTransport, b12MonitorRevert3, b12MonitorRevert32000} {
			t.Run(fmt.Sprintf("%s/%s", saved, source), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				w := newB10World(t)
				journal := w.persist(ctx, t, saved)
				chain := &b12Chain{b10Chain: w.chain, release: make(chan struct{}), delivered: make(chan struct{})}
				bus := &b12Bus{b10Bus: w.bus, fault: make(chan error, 1), chain: chain}
				var faulted, replaced atomic.Bool
				e := &dkg.Executor{Chain: chain, Engine: &b10Engine{journal: journal}, Journal: journal, Bus: bus, Signer: b10Signer(0), PollInterval: 2 * time.Millisecond}
				e.Boundary = func(name string) error {
					switch name {
					case "after-key-reload":
						chain.arm(b12Fault(source), nil)
						select {
						case <-chain.delivered:
							faulted.Store(true)
						case <-time.After(3 * time.Second):
							return nil
						}
						select {
						case <-chain.cancelled():
						case <-time.After(300 * time.Millisecond):
						}
					case "after-wallet-ready-save":
						v := b10Approval(t, w.desc, 12, common.Hash{0xa2}, 2)
						w.chain.mu.Lock()
						w.chain.view = v
						w.chain.mu.Unlock()
						replaced.Store(true)
						time.Sleep(200 * time.Millisecond) // about 100 monitor polls
					}
					return nil
				}
				done := make(chan b10Outcome, 1)
				go func() {
					status, record, err := e.Restore(ctx)
					done <- b10Outcome{status, record, err}
				}()
				var got b10Outcome
				select {
				case got = <-done:
				case <-time.After(9 * time.Second):
					t.Fatalf("harness: Restore did not return within 9 s")
				}
				stored, err := journal.LoadWallet(context.Background())
				if err != nil {
					t.Fatalf("harness: reading the saved wallet: %v", err)
				}
				if got.err == nil {
					t.Errorf("%s: restored host from %s went on after an unclassified %s and returned success with the approval replaced after its ReadyUnfunded save (replaced=%t): durable state=%s quarantined=%t (want the fault returned, or the quarantine recorded)",
						b12Invariant, saved, source, replaced.Load(), stored.State, stored.Quarantined)
				} else if errors.Is(got.err, dkg.ErrQuarantined) != stored.Quarantined {
					t.Errorf("%s: restored host from %s after an unclassified %s: Restore returned err=%v but durable quarantined=%t (a reported quarantine must be recorded, and an unreported one must not be)",
						b12Invariant, saved, source, got.err, stored.Quarantined)
				}
				if !faulted.Load() {
					t.Errorf("harness: the %s was never delivered to the monitor", source)
				}
			})
		}
	}
}
