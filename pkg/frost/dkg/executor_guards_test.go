package dkg

// Mutation batch M10-dkg: the executor's entry guards, its approval checks
// and its registry-driving loop (executor.go).
//
// Real code: Executor.Run/Restore entry guards, prepare, inspectSubmission,
// WaitCandidateAcceptance, checkedApproval, reconcileWallet, collect and
// finishFailure, called on an Executor built in the test (the seams
// outcome_test.go and v03_replay_test.go already use).
//
// FIXTURES (labelled, test-only):
//   - gChain: a scripted registry. Its view function answers View reads;
//     every other call is counted, and Submit/Challenge/Approve/Expire can be
//     made to fail or to change the next view;
//   - gEngine, gJournal, gBus: counting stand-ins with no worker, store or
//     network;
//   - the parameters, selection and candidate of types_guards_test.go.
// Every wait is bounded by a context of at most two seconds.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
)

var (
	errFixtureStop   = errors.New("fixture: stop here")
	errFixtureRead   = errors.New("fixture: view read failed")
	errFixtureSubmit = errors.New("fixture: submission reverted")
	errFixtureChall  = errors.New("fixture: challenge reverted")
	errFixtureRetain = errors.New("fixture: wallet retain failed")
)

// gChain is a scripted registry (FIXTURE).
type gChain struct {
	mu                                            sync.Mutex
	params                                        Parameters
	paramsErr                                     error
	selection                                     Selection
	view                                          func(c *gChain, call int) (View, error)
	calls                                         []string
	viewCalls                                     int
	submitErr, challengeErr, approveErr           error
	submits, challenges, approves, expires, xdkgs int
	approved, challenged                          bool
	digest                                        [32]byte
}

func (c *gChain) record(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, name)
}
func (c *gChain) callCount() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.calls) }
func (c *gChain) Parameters(context.Context) (Parameters, error) {
	c.record("Parameters")
	return c.params, c.paramsErr
}
func (c *gChain) Selection(context.Context, uint64) (Selection, error) {
	c.record("Selection")
	return c.selection, nil
}
func (c *gChain) View(context.Context, uint64) (View, error) {
	c.record("View")
	c.mu.Lock()
	call := c.viewCalls
	c.viewCalls++
	c.mu.Unlock()
	return c.view(c, call)
}
func (c *gChain) ResultDigest(context.Context, uint64, Result) ([32]byte, error) {
	c.record("ResultDigest")
	return c.digest, nil
}
func (c *gChain) ReadinessDigest(context.Context, Wallet, uint16, [32]byte) ([32]byte, error) {
	c.record("ReadinessDigest")
	return c.digest, nil
}
func (c *gChain) Validate(context.Context, uint64, Result) (bool, error) {
	c.record("Validate")
	return false, nil
}
func (c *gChain) Submit(context.Context, Result) error {
	c.record("Submit")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.submits++
	return c.submitErr
}
func (c *gChain) Challenge(context.Context) error {
	c.record("Challenge")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.challenges++
	if c.challengeErr == nil {
		c.challenged = true
	}
	return c.challengeErr
}
func (c *gChain) Approve(context.Context) error {
	c.record("Approve")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.approves++
	c.approved = c.approveErr == nil
	return c.approveErr
}
func (c *gChain) Ready(context.Context, [32]byte, []uint16, [][32]byte, []byte) error {
	c.record("Ready")
	return errFixtureStop
}
func (c *gChain) Expire(context.Context, [32]byte) error {
	c.record("Expire")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expires++
	return nil
}
func (c *gChain) ExpireDKG(context.Context) error {
	c.record("ExpireDKG")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.xdkgs++
	return nil
}
func (c *gChain) counts() (submits, challenges, approves, expires, xdkgs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.submits, c.challenges, c.approves, c.expires, c.xdkgs
}

// gEngine counts worker calls and runs none (FIXTURE).
type gEngine struct {
	mu          sync.Mutex
	dkg, reload int
}

func (e *gEngine) DKG(context.Context, frost.DKGRequest, frost.Providers) (frost.KeyReady, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dkg++
	return frost.KeyReady{}, errFixtureStop
}
func (e *gEngine) Sign(context.Context, frost.SigningRequest, frost.Providers) ([64]byte, error) {
	return [64]byte{}, errFixtureStop
}
func (e *gEngine) ReloadKey(context.Context, frost.SigningRequest, frost.Providers) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reload++
	return errFixtureStop
}
func (e *gEngine) calls() (int, int) { e.mu.Lock(); defer e.mu.Unlock(); return e.dkg, e.reload }

// gJournal is a public-state journal with no store behind it (FIXTURE).
type gJournal struct {
	Journal
	mu      sync.Mutex
	domain  frost.Domain
	status  frost.DKGStatus
	wallet  WalletRecord
	saved   []WalletRecord
	saveErr error
}

func (j *gJournal) Domain() frost.Domain { return j.domain }
func (j *gJournal) DKGStatus(context.Context) (frost.DKGStatus, error) {
	return j.status, nil
}
func (j *gJournal) LoadWallet(context.Context) (WalletRecord, error) { return j.wallet, nil }
func (j *gJournal) LoadKey(context.Context) (frost.KeyReady, error) {
	return frost.KeyReady{LocalReferences: []frost.KeyReference{[]byte("fixture reference")}}, nil
}
func (j *gJournal) SaveWallet(_ context.Context, w WalletRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.saved = append(j.saved, w)
	return j.saveErr
}
func (j *gJournal) savedRecords() []WalletRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]WalletRecord(nil), j.saved...)
}

// gBus sends nowhere and receives nothing (FIXTURE).
type gBus struct{}

func (gBus) Send(context.Context, Attestation) error { return nil }
func (gBus) Receive(ctx context.Context) (Attestation, error) {
	<-ctx.Done()
	return Attestation{}, ctx.Err()
}

// gWorld is one operator holding every seat of the fixture selection.
type gWorld struct {
	params    Parameters
	selection Selection
	signer    *ecdsa.PrivateKey
	chain     *gChain
	engine    *gEngine
	journal   *gJournal
}

func newGWorld(t *testing.T) *gWorld {
	t.Helper()
	signer := fixtureKey(t)
	p := fixtureParams()
	s := fixtureSelection(p, crypto.PubkeyToAddress(signer.PublicKey))
	var chainID [32]byte
	p.ChainID.FillBytes(chainID[:])
	w := &gWorld{params: p, selection: s, signer: signer, engine: &gEngine{},
		chain:   &gChain{params: p, selection: s, digest: [32]byte{0xD9}},
		journal: &gJournal{domain: frost.Domain{Network: "m10-fixture", Chain: chainID, Registry: [20]byte(p.Registry), Epoch: s.Epoch}}}
	w.chain.view = func(*gChain, int) (View, error) { return View{}, errFixtureRead }
	return w
}

// executor returns a complete executor whose request matches the selection.
func (w *gWorld) executor() *Executor {
	roster := w.selection.Roster()
	return &Executor{Chain: w.chain, Engine: w.engine, Journal: w.journal, Bus: gBus{}, Signer: w.signer, FinalityBlocks: 2, PollInterval: 5 * time.Millisecond,
		Request:   frost.DKGRequest{Group: frost.Group{Roster: roster, Threshold: w.params.Threshold, Quorum: w.params.GroupSize, Epoch: w.selection.Epoch}, LocalSeats: roster, Participants: roster},
		params:    w.params,
		selection: w.selection,
		incoming:  make(chan Attestation, 400),
		failure:   make(chan error, 2),
	}
}

func bounded(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func runSafely(ctx context.Context, e *Executor) (err error, panicked any) {
	defer func() { panicked = recover() }()
	_, err = e.Run(ctx)
	return err, nil
}

// Invariant: Run refuses an incomplete executor (no context, chain, engine,
// journal, bus, signer or finality depth), each missing on its own, before
// any chain read and without panicking. Rows executor.go:58 (4).
func TestRunRefusesIncompleteExecutorBeforeAnyRead(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Executor, *context.Context){
		"nil-context":   func(_ *Executor, ctx *context.Context) { *ctx = nil },
		"nil-chain":     func(e *Executor, _ *context.Context) { e.Chain = nil },
		"nil-engine":    func(e *Executor, _ *context.Context) { e.Engine = nil },
		"nil-journal":   func(e *Executor, _ *context.Context) { e.Journal = nil },
		"nil-bus":       func(e *Executor, _ *context.Context) { e.Bus = nil },
		"nil-signer":    func(e *Executor, _ *context.Context) { e.Signer = nil },
		"zero-finality": func(e *Executor, _ *context.Context) { e.FinalityBlocks = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			w := newGWorld(t)
			w.chain.paramsErr = errFixtureStop
			e := w.executor()
			ctx := bounded(t, 2*time.Second)
			change(e, &ctx)
			err, p := runSafely(ctx, e)
			if p != nil {
				t.Fatalf("Run panicked instead of refusing an executor with %s: %v", name, p)
			}
			if err == nil || w.chain.callCount() != 0 {
				t.Fatalf("Run with %s read the chain before refusing: err=%v calls=%v", name, err, w.chain.calls)
			}
		})
	}
}

// Invariant: Run stops when the chain's parameters cannot be read or
// checked; no worker is started. Row executor.go:69.
func TestRunStopsWhenPrepareFails(t *testing.T) {
	t.Parallel()
	w := newGWorld(t)
	w.chain.paramsErr = errFixtureStop
	e := w.executor()
	err, p := runSafely(bounded(t, 2*time.Second), e)
	if p != nil || !errors.Is(err, errFixtureStop) {
		t.Fatalf("Run continued after the chain policy read failed: err=%v panic=%v", err, p)
	}
	if dkg, _ := w.engine.calls(); dkg != 0 {
		t.Fatalf("Run started the worker %d time(s) without a checked policy", dkg)
	}
}

// Invariant: prepare accepts a request only if the chain policy is valid and
// the journal domain, roster, threshold, quorum, participants and local
// seats all match the chain's selection; each case breaks one of these on
// its own. Rows executor.go:111, :123 (3), :134.
func TestPrepareRefusesEachMismatch(t *testing.T) {
	t.Parallel()
	stranger := fixtureKey(t)
	cases := map[string]func(*gWorld, *Executor){
		"policy-invalid":  func(w *gWorld, _ *Executor) { w.chain.params.ChallengeBlocks = 0 },
		"selection-short": func(w *gWorld, _ *Executor) { w.chain.selection.Members = w.chain.selection.Members[:4] },
		"domain-chain":    func(w *gWorld, _ *Executor) { w.journal.domain.Chain[0] ^= 1 },
		"domain-registry": func(w *gWorld, _ *Executor) { w.journal.domain.Registry[0] ^= 1 },
		"domain-epoch":    func(w *gWorld, _ *Executor) { w.journal.domain.Epoch++ },
		"request-roster":  func(_ *gWorld, e *Executor) { e.Request.Group.Roster = []uint16{1, 2, 3, 4, 6} },
		"request-threshold": func(_ *gWorld, e *Executor) {
			e.Request.Group.Threshold++
		},
		"request-quorum":       func(_ *gWorld, e *Executor) { e.Request.Group.Quorum-- },
		"request-participants": func(_ *gWorld, e *Executor) { e.Request.Participants = []uint16{1, 2, 3, 4} },
		"local-seats-subset":   func(_ *gWorld, e *Executor) { e.Request.LocalSeats = []uint16{1, 2} },
		"signer-not-selected":  func(_ *gWorld, e *Executor) { e.Signer = stranger },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			w := newGWorld(t)
			e := w.executor()
			change(w, e)
			if locals, err := e.prepare(context.Background()); err == nil {
				t.Fatalf("prepare accepted a request with %s (locals %v)", name, locals)
			}
		})
	}
	t.Run("matching", func(t *testing.T) {
		w := newGWorld(t)
		e := w.executor()
		locals, err := e.prepare(context.Background())
		if err != nil || !reflect.DeepEqual(locals, w.selection.Roster()) {
			t.Fatalf("prepare refused a matching request: %v %v", locals, err)
		}
	})
}

// Invariant: Restore never reloads a key for seats this operator does not
// hold in the chain's selection: it returns an error with the saved wallet
// unchanged. Row executor.go:199.
func TestRestoreRefusesSeatsItDoesNotHold(t *testing.T) {
	t.Parallel()
	w := newGWorld(t)
	// Seat 1 belongs to another operator; the saved status claims it.
	w.chain.selection.Members[0] = 99
	w.chain.selection.Operators[0] = common.Address{0xBE}
	w.journal.status = frost.DKGStatus{State: frost.DKGKeyStored, LocalSeats: []uint16{1, 2, 3, 4, 5}}
	saved := WalletRecord{ID: [32]byte{1}, State: "RegisteredPendingReady"}
	w.journal.wallet = saved
	e := &Executor{Chain: w.chain, Engine: w.engine, Journal: w.journal, Bus: gBus{}, Signer: w.signer, PollInterval: 5 * time.Millisecond}
	_, record, err := e.Restore(bounded(t, 2*time.Second))
	if err == nil {
		t.Fatal("Restore accepted seats this operator does not hold")
	}
	if _, reload := w.engine.calls(); reload != 0 || len(w.journal.savedRecords()) != 0 || !reflect.DeepEqual(record, saved) {
		t.Fatalf("Restore acted on unowned seats: reloads=%d saves=%d record=%+v", reload, len(w.journal.savedRecords()), record)
	}
}

// Invariant: before approval, the monitor classifies the epoch: Idle is an
// expiry; AwaitingResult or Challenge at or past the result deadline is
// expired once (ExpireDKG) and reported as ErrExpired; earlier, or while
// still awaiting the seed, nothing is sent. Rows executor.go:471, :473, :480.
func TestMonitorExpiresDKGAtResultDeadline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		state   uint8
		head    uint64
		expired bool
	}{
		{"idle", 0, 10, true},
		{"seed-past-deadline", 1, 40, false},
		{"result-before", 2, 29, false},
		{"result-at", 2, 30, true},
		{"result-after", 2, 31, true},
		{"challenge-before", 3, 29, false},
		{"challenge-at", 3, 30, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &gChain{view: func(*gChain, int) (View, error) {
				return View{Epoch: 77, State: tc.state, Head: tc.head, ResultDeadline: 30}, nil
			}}
			e := Executor{Chain: c, selection: Selection{Epoch: 77}}
			err := e.inspectSubmission(context.Background())
			_, _, _, _, xdkgs := c.counts()
			if tc.expired != errors.Is(err, ErrExpired) {
				t.Fatalf("state %d head %d: got %v, want expired=%t", tc.state, tc.head, err, tc.expired)
			}
			if !tc.expired && err != nil {
				t.Fatalf("state %d head %d: unexpected %v", tc.state, tc.head, err)
			}
			if want := map[bool]int{true: 1, false: 0}[tc.expired && tc.state != 0]; xdkgs != want {
				t.Fatalf("state %d head %d: ExpireDKG sent %d time(s), want %d", tc.state, tc.head, xdkgs, want)
			}
		})
	}
}

// Invariant: an approval whose descriptor differs from this host's candidate
// is quarantined by the monitor. Row executor.go:497.
func TestMonitorQuarantinesApprovalOfAnotherDescriptor(t *testing.T) {
	t.Parallel()
	d := fixtureDescriptor(t)
	for _, tc := range []struct {
		name  string
		other bool
		want  error
	}{{"same", false, nil}, {"other", true, ErrQuarantined}} {
		t.Run(tc.name, func(t *testing.T) {
			approved := d
			if tc.other {
				approved.SnowfallDescriptor = [32]byte{0xD2}
			}
			c := &gChain{view: func(*gChain, int) (View, error) {
				return View{Epoch: 77, Head: 5, ApprovedID: [32]byte{1}, ApprovalHash: common.Hash{2}, Approved: Wallet{Descriptor: approved, State: 1, Deadline: 100}}, nil
			}}
			own := d
			e := Executor{Chain: c, selection: Selection{Epoch: 77}, descriptor: &own}
			if err := e.inspectSubmission(context.Background()); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("approval of %s descriptor: got %v want %v", tc.name, err, tc.want)
			}
		})
	}
}

// Invariant: an invalid submission is challenged. A refused challenge is
// reported unless a re-read shows the submission already gone (another
// observer challenged it). Rows executor.go:525, :526.
func TestMonitorChallengesInvalidSubmission(t *testing.T) {
	t.Parallel()
	submitted := View{Epoch: 77, State: 3, Head: 5, ResultDeadline: 100, SubmittedHash: [32]byte{0x51}, Submitted: &Result{}}
	for _, tc := range []struct {
		name      string
		refused   bool
		next      func() (View, error)
		wantError bool
	}{
		{"accepted", false, nil, false},
		{"refused-already-gone", true, func() (View, error) { return View{Epoch: 77, State: 2, ResultDeadline: 100}, nil }, false},
		{"refused-still-submitted", true, func() (View, error) { return submitted, nil }, true},
		{"refused-reread-failed", true, func() (View, error) { return View{}, errFixtureRead }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &gChain{view: func(_ *gChain, call int) (View, error) {
				if call == 0 {
					return submitted, nil
				}
				return tc.next()
			}}
			if tc.refused {
				c.challengeErr = errFixtureChall
			}
			e := Executor{Chain: c, selection: Selection{Epoch: 77}}
			err := e.inspectSubmission(context.Background())
			if _, challenges, _, _, _ := c.counts(); challenges != 1 {
				t.Fatalf("invalid submission challenged %d time(s)", challenges)
			}
			if tc.wantError != errors.Is(err, errFixtureChall) || (!tc.wantError && err != nil) {
				t.Fatalf("challenge %s: got %v, want refusal reported=%t", tc.name, err, tc.wantError)
			}
		})
	}
}

// Invariant: a monitor read that overlaps the other observer's approval
// publication and differs from it in any field is re-read once before it is
// classified; a difference that persists is quarantined. Row executor.go:453.
func TestMonitorReReadsOverlappingSnapshotThatDiffersInHash(t *testing.T) {
	t.Parallel()
	approved := View{Epoch: 77, Head: 10, ApprovedID: [32]byte{1}, ApprovalHash: common.Hash{2}, Approved: Wallet{State: 1, ApprovalBlock: 10, Deadline: 20}}
	stale := approved
	stale.ApprovalHash = common.Hash{3}
	for _, tc := range []struct {
		name    string
		persist bool
		want    error
	}{{"stale-then-current", false, nil}, {"stale-persists", true, ErrQuarantined}} {
		t.Run(tc.name, func(t *testing.T) {
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			e := Executor{selection: Selection{Epoch: 77}}
			e.Chain = &gChain{view: func(_ *gChain, call int) (View, error) {
				if call == 0 {
					close(started)
					<-release
					return stale, nil
				}
				if tc.persist {
					return stale, nil
				}
				return approved, nil
			}}
			go func() { done <- e.inspectSubmission(context.Background()) }()
			<-started
			e.mu.Lock()
			e.approvalGeneration++
			e.approvalSeen = true
			e.observedApprovalID = approved.ApprovedID
			e.observedApprovalHash = approved.ApprovalHash
			e.mu.Unlock()
			close(release)
			select {
			case err := <-done:
				if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
					t.Fatalf("overlapping stale snapshot (%s): got %v want %v", tc.name, err, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("inspectSubmission did not return within 2s")
			}
		})
	}
}

// acceptance is a WaitCandidateAcceptance fixture: the descriptor the host
// derives, its hash, ID and the result hash the host must see approved.
type acceptance struct {
	w    *gWorld
	e    *Executor
	c    frost.Candidate
	d    Descriptor
	hash [32]byte
	id   [32]byte
	want [32]byte
}

func newAcceptance(t *testing.T) *acceptance {
	t.Helper()
	w := newGWorld(t)
	a := &acceptance{w: w, e: w.executor(), c: fixtureCandidate(t, w.params, w.selection)}
	var err error
	if a.d, err = NewDescriptor(w.params, w.selection, a.c); err != nil {
		t.Fatal(err)
	}
	if a.hash, err = DescriptorHash(a.d); err != nil {
		t.Fatal(err)
	}
	a.id = WalletID(a.d.OutputKey)
	// The host signs the fixture digest once per local seat, in roster order.
	result := NewResult(a.d)
	signature, err := Sign(w.signer, w.chain.digest)
	if err != nil {
		t.Fatal(err)
	}
	for range a.d.Members {
		result.Signatures = append(result.Signatures, signature...)
	}
	if a.want, err = ResultHash(result); err != nil {
		t.Fatal(err)
	}
	return a
}

// approval is the public registry view of this host's approved wallet.
func (a *acceptance) approval(head uint64) View {
	return View{Epoch: a.d.Epoch, Head: head, ApprovedID: a.id, ApprovalHash: common.Hash{0xAB}, Approved: Wallet{Descriptor: a.d, ResultHash: a.want, DescriptorHash: a.hash, ApprovalBlock: 100, Deadline: 1000, State: 1}}
}

func (a *acceptance) wait(t *testing.T, d time.Duration) (frost.Receipt, error) {
	t.Helper()
	return a.e.WaitCandidateAcceptance(bounded(t, d), a.c)
}

// Invariant: the candidate's approval is accepted once it is exactly
// FinalityBlocks deep, and not before; acceptance saves the wallet
// RegisteredPendingReady. Row executor.go:622.
func TestAcceptanceAtFinalityDepth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		head   uint64
		accept bool
	}{{"at-depth", 102, true}, {"one-short", 101, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAcceptance(t)
			a.w.chain.view = func(*gChain, int) (View, error) { return a.approval(tc.head), nil }
			receipt, err := a.wait(t, 400*time.Millisecond)
			saved := a.w.journal.savedRecords()
			registered := len(saved) > 0 && saved[len(saved)-1].State == "RegisteredPendingReady"
			if tc.accept && (err != nil || receipt.Descriptor != a.d.SnowfallDescriptor || !registered) {
				t.Fatalf("approval at finality depth not accepted: err=%v receipt=%+v registered=%t", err, receipt, registered)
			}
			if !tc.accept && (err == nil || registered) {
				t.Fatalf("approval accepted before finality depth: err=%v registered=%t", err, registered)
			}
		})
	}
}

// Invariant: an approval is this host's only if its wallet ID, result hash,
// descriptor hash and descriptor all match; any one differing is
// quarantined as soon as it is seen, before it is final. Row
// executor.go:597.
func TestAcceptanceQuarantinesEachForeignApprovalField(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*View){
		"id":              func(v *View) { v.ApprovedID[0] ^= 1 },
		"result-hash":     func(v *View) { v.Approved.ResultHash[0] ^= 1 },
		"descriptor-hash": func(v *View) { v.Approved.DescriptorHash[0] ^= 1 },
		"descriptor":      func(v *View) { v.Approved.Descriptor.Threshold = 4 },
	} {
		t.Run(name, func(t *testing.T) {
			a := newAcceptance(t)
			a.w.chain.view = func(*gChain, int) (View, error) { v := a.approval(100); change(&v); return v, nil }
			if _, err := a.wait(t, time.Second); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("approval with a foreign %s not quarantined: %v", name, err)
			}
		})
	}
}

// Invariant: if the monitor already observed a different approval, the
// acceptance path quarantines instead of accepting. Row executor.go:607.
func TestAcceptanceQuarantinesWhenObserversDisagree(t *testing.T) {
	t.Parallel()
	a := newAcceptance(t)
	a.w.chain.view = func(*gChain, int) (View, error) { return a.approval(102), nil }
	a.e.approvalSeen = true
	a.e.observedApprovalID = a.id
	a.e.observedApprovalHash = [32]byte{0xCD}
	receipt, err := a.wait(t, time.Second)
	if !errors.Is(err, ErrQuarantined) {
		t.Fatalf("acceptance ignored a disagreeing observer: receipt=%+v err=%v", receipt, err)
	}
}

// Invariant: the leader's refused submission is reported while the registry
// still awaits a result (no silent retry loop). Row executor.go:651.
func TestAcceptanceReportsRefusedSubmission(t *testing.T) {
	t.Parallel()
	a := newAcceptance(t)
	a.w.chain.submitErr = errFixtureSubmit
	a.w.chain.view = func(*gChain, int) (View, error) {
		return View{Epoch: a.d.Epoch, State: 2, Head: 10, ResultDeadline: 100}, nil
	}
	if _, err := a.wait(t, time.Second); !errors.Is(err, errFixtureSubmit) {
		t.Fatalf("refused submission not reported: %v", err)
	}
	if submits, _, _, _, _ := a.w.chain.counts(); submits != 1 {
		t.Fatalf("refused submission sent %d time(s), want 1", submits)
	}
}

// Invariant: while another result is in its challenge period, the leader
// neither resubmits nor approves it. Rows executor.go:645, :658 (LO).
func TestAcceptanceLeavesForeignSubmissionAlone(t *testing.T) {
	t.Parallel()
	a := newAcceptance(t)
	a.w.chain.view = func(*gChain, int) (View, error) {
		return View{Epoch: a.d.Epoch, State: 3, Head: 90, SubmittedAt: 10, ResultDeadline: 100, SubmittedHash: [32]byte{0xF0}}, nil
	}
	_, err := a.wait(t, 150*time.Millisecond)
	if submits, _, approves, _, _ := a.w.chain.counts(); submits != 0 || approves != 0 {
		t.Fatalf("leader acted on a foreign submission: submits=%d approves=%d err=%v", submits, approves, err)
	}
}

// Invariant: the host approves its own submission at exactly
// SubmittedAt+ChallengeBlocks, and not before. Row executor.go:658 (CB).
func TestAcceptanceApprovesAtChallengeEnd(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		head    uint64
		approve bool
	}{{"at-end", 52, true}, {"one-early", 51, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAcceptance(t)
			a.w.chain.view = func(c *gChain, _ int) (View, error) {
				c.mu.Lock()
				approved := c.approved
				c.mu.Unlock()
				if approved {
					return a.approval(102), nil
				}
				return View{Epoch: a.d.Epoch, State: 3, Head: tc.head, SubmittedAt: 50, ResultDeadline: 100, SubmittedHash: a.want}, nil
			}
			_, err := a.wait(t, 400*time.Millisecond)
			_, _, approves, _, _ := a.w.chain.counts()
			if tc.approve && (approves == 0 || err != nil) {
				t.Fatalf("own submission not approved at challenge end: approves=%d err=%v", approves, err)
			}
			if !tc.approve && approves != 0 {
				t.Fatalf("own submission approved during its challenge period: approves=%d", approves)
			}
		})
	}
}

// savedApproval is a record and the matching registry view (FIXTURE).
func savedApproval() (WalletRecord, View) {
	d := Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1337), Epoch: 77, Threshold: 3}
	record := WalletRecord{Descriptor: d, ID: [32]byte{1}, ResultHash: [32]byte{3}, DescriptorHash: [32]byte{4}, ApprovalBlock: 10, ApprovalHash: common.Hash{2}, Deadline: 20, State: "RegisteredPendingReady"}
	view := View{Epoch: 77, Head: 12, ApprovedID: record.ID, ApprovalHash: record.ApprovalHash, Approved: Wallet{Descriptor: d, ResultHash: record.ResultHash, DescriptorHash: record.DescriptorHash, ApprovalBlock: 10, Deadline: 20, State: 1}}
	return record, view
}

// Invariant: a re-read approval must match every saved field (ID, result
// hash, descriptor hash, approval hash and block, descriptor), each checked
// on its own; a closed wallet is expired; a pending wallet at or past its
// deadline is expired; a failed read is reported. Rows executor.go:678,
// :680 (2), :686.
func TestCheckedApprovalMatchesEverySavedField(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		change  func(*View)
		readErr error
		want    error
		expires int
	}{
		{"matching", func(*View) {}, nil, nil, 0},
		{"read-failed", func(*View) {}, errFixtureRead, errFixtureRead, 0},
		{"id", func(v *View) { v.ApprovedID[0] ^= 1 }, nil, ErrQuarantined, 0},
		{"result-hash", func(v *View) { v.Approved.ResultHash[0] ^= 1 }, nil, ErrQuarantined, 0},
		{"descriptor-hash", func(v *View) { v.Approved.DescriptorHash[0] ^= 1 }, nil, ErrQuarantined, 0},
		{"approval-hash", func(v *View) { v.ApprovalHash[0] ^= 1 }, nil, ErrQuarantined, 0},
		{"approval-block", func(v *View) { v.Approved.ApprovalBlock++ }, nil, ErrQuarantined, 0},
		{"descriptor", func(v *View) { v.Approved.Descriptor.Epoch++ }, nil, ErrQuarantined, 0},
		{"closed", func(v *View) { v.Approved.State = 3 }, nil, ErrExpired, 0},
		{"before-deadline", func(v *View) { v.Head = 19 }, nil, nil, 0},
		{"at-deadline", func(v *View) { v.Head = 20 }, nil, ErrExpired, 1},
		{"ready-past-deadline", func(v *View) { v.Head, v.Approved.State = 25, 2 }, nil, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, view := savedApproval()
			tc.change(&view)
			c := &gChain{view: func(c *gChain, _ int) (View, error) {
				v := view
				c.mu.Lock()
				if c.expires > 0 {
					v.Approved.State = 3
				}
				c.mu.Unlock()
				return v, tc.readErr
			}}
			e := Executor{Chain: c, selection: Selection{Epoch: 77}, record: record}
			_, err := e.checkedApproval(context.Background())
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("checked approval (%s): got %v want %v", tc.name, err, tc.want)
			}
			if _, _, _, expires, _ := c.counts(); expires != tc.expires {
				t.Fatalf("checked approval (%s): Expire sent %d time(s), want %d", tc.name, expires, tc.expires)
			}
		})
	}
}

// Invariant: wallet reconciliation accepts an outcome only for the same
// approval: any one changed field (ID, approval hash, result hash,
// descriptor hash, approval block, deadline, descriptor) is quarantined.
// Rows executor.go:706 (3).
func TestReconcileWalletRefusesEachChangedApprovalField(t *testing.T) {
	t.Parallel()
	_, expected := savedApproval()
	for name, change := range map[string]func(*View){
		"unchanged":       func(*View) {},
		"id":              func(v *View) { v.ApprovedID[0] ^= 1 },
		"approval-hash":   func(v *View) { v.ApprovalHash[0] ^= 1 },
		"result-hash":     func(v *View) { v.Approved.ResultHash[0] ^= 1 },
		"descriptor-hash": func(v *View) { v.Approved.DescriptorHash[0] ^= 1 },
		"approval-block":  func(v *View) { v.Approved.ApprovalBlock++ },
		"deadline":        func(v *View) { v.Approved.Deadline++ },
		"descriptor":      func(v *View) { v.Approved.Descriptor.Threshold++ },
	} {
		t.Run(name, func(t *testing.T) {
			read := expected
			read.Approved.State = 2
			change(&read)
			e := Executor{selection: Selection{Epoch: 77}, Chain: &gChain{view: func(*gChain, int) (View, error) { return read, nil }}}
			_, err := e.reconcileWallet(context.Background(), expected, errFixtureStop)
			if name == "unchanged" {
				if err != nil {
					t.Fatalf("reconciliation refused the same approval, now ready: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrQuarantined) {
				t.Fatalf("reconciliation accepted a changed %s: %v", name, err)
			}
		})
	}
}

// collectSafely runs collect and turns a panic into a reported value.
func collectSafely(ctx context.Context, e *Executor, stage string, count int, local []Attestation, valid func(Attestation) bool) (votes []Attestation, err error, panicked any) {
	defer func() { panicked = recover() }()
	votes, err = e.collect(ctx, stage, count, local, valid)
	return votes, err, nil
}

// Invariant: collect counts only seats 1..n of the selection; an attestation
// for seat 0 or a seat past the roster is ignored, never indexed. Row
// executor.go:738.
func TestCollectIgnoresSeatsOutsideRoster(t *testing.T) {
	t.Parallel()
	key := fixtureKey(t)
	digest := [32]byte{3}
	signature, err := Sign(key, digest)
	if err != nil {
		t.Fatal(err)
	}
	e := Executor{selection: Selection{Epoch: 77, Members: []uint32{1}, Operators: []common.Address{crypto.PubkeyToAddress(key.PublicKey)}}, incoming: make(chan Attestation, 3), failure: make(chan error, 1), PollInterval: time.Second}
	for _, seat := range []uint16{0, 2, 1} {
		e.incoming <- Attestation{Stage: "result", Epoch: 77, Seat: seat, Digest: digest, Signature: signature}
	}
	votes, err, p := collectSafely(bounded(t, time.Second), &e, "result", 1, nil, func(Attestation) bool { return true })
	if p != nil {
		t.Fatalf("collect panicked on an attestation outside the roster: %v", p)
	}
	if err != nil || len(votes) != 1 || votes[0].Seat != 1 {
		t.Fatalf("collect counted a seat outside the roster: %+v %v", votes, err)
	}
}

// Invariant: readiness attestations that arrive while results are still
// collected are kept, one per seat, for every seat, and counted by the next
// readiness collection. Row executor.go:761.
func TestCollectBuffersEveryEarlyReadinessSeat(t *testing.T) {
	t.Parallel()
	keys := []*ecdsa.PrivateKey{fixtureKey(t), fixtureKey(t), fixtureKey(t)}
	var operators []common.Address
	for _, k := range keys {
		operators = append(operators, crypto.PubkeyToAddress(k.PublicKey))
	}
	resultDigest, readyDigest := [32]byte{0x0E}, [32]byte{0x0F}
	sign := func(i int, d [32]byte) []byte {
		s, err := Sign(keys[i], d)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	e := Executor{selection: Selection{Epoch: 77, Members: []uint32{11, 12, 13}, Operators: operators}, Bus: gBus{}, incoming: make(chan Attestation, 8), failure: make(chan error, 1), PollInterval: 10 * time.Second}
	ready := func(seat uint16) Attestation {
		return Attestation{Stage: "ready", Epoch: 77, Seat: seat, Digest: readyDigest, Reference: [32]byte{byte(seat)}, Signature: sign(int(seat-1), readyDigest)}
	}
	e.incoming <- ready(2)
	e.incoming <- ready(3)
	e.incoming <- ready(2) // a duplicate seat is kept once
	for _, seat := range []uint16{2, 3} {
		e.incoming <- Attestation{Stage: "result", Epoch: 77, Seat: seat, Digest: resultDigest, Signature: sign(int(seat-1), resultDigest)}
	}
	local := []Attestation{{Stage: "result", Epoch: 77, Seat: 1, Digest: resultDigest, Signature: sign(0, resultDigest)}}
	if _, err := e.collect(bounded(t, time.Second), "result", 3, local, func(a Attestation) bool { return a.Digest == resultDigest }); err != nil {
		t.Fatalf("result collection failed: %v", err)
	}
	votes, err := e.collect(bounded(t, 500*time.Millisecond), "ready", 2, nil, func(a Attestation) bool { return a.Digest == readyDigest })
	var seats []int
	for _, v := range votes {
		seats = append(seats, int(v.Seat))
	}
	sort.Ints(seats)
	if err != nil || !reflect.DeepEqual(seats, []int{2, 3}) {
		t.Fatalf("early readiness lost: seats=%v err=%v", seats, err)
	}
}

// Invariant: when recording a classified failure cannot be retained, the
// caller is told: the classification is returned and the retain failure is
// reported with it. The retain error is formatted with %v, so its text is
// the only channel. Row executor.go:819.
func TestFinishFailureReportsRetainFailure(t *testing.T) {
	t.Parallel()
	j := &gJournal{saveErr: errFixtureRetain}
	e := Executor{Journal: j, failure: make(chan error, 2), record: WalletRecord{ID: [32]byte{1}, ApprovalBlock: 10, State: "RegisteredPendingReady"}}
	_, err := e.finishFailure(context.Background(), ErrExpired)
	if !errors.Is(err, ErrExpired) || !strings.Contains(err.Error(), errFixtureRetain.Error()) {
		t.Fatalf("retain failure not reported with the classification: %v", err)
	}
	if saved := j.savedRecords(); len(saved) != 1 || saved[0].State != "Closed" {
		t.Fatalf("expiry not offered to the journal as Closed: %+v", saved)
	}
}
