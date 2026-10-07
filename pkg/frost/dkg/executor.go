package dkg

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// Executor is one operator's original selected seats in one DKG epoch. Run
// owns the worker for the full protocol deadline, including public acceptance.
// It never treats Submitted or the registry's Idle state as completion.
type Executor struct {
	approvalSeen         bool
	observedApprovalID   [32]byte
	observedApprovalHash [32]byte
	callbackErr          error
	callbackMu           sync.Mutex
	abort                context.CancelFunc
	Chain                Chain
	Engine               Engine
	Journal              Journal
	Bus                  Bus
	Signer               *ecdsa.PrivateKey
	Request              frost.DKGRequest
	Providers            frost.Providers
	FinalityBlocks       uint64
	PollInterval         time.Duration
	mu                   sync.Mutex
	background           sync.WaitGroup
	descriptor           *Descriptor
	params               Parameters
	selection            Selection
	incoming             chan Attestation
	failure              chan error
	pending              []Attestation
	record               WalletRecord
}

func (e *Executor) Run(ctx context.Context) (WalletRecord, error) {
	if ctx == nil || e.Chain == nil || e.Engine == nil || e.Journal == nil || e.Bus == nil || e.Signer == nil || e.FinalityBlocks == 0 {
		return WalletRecord{}, errors.New("incomplete FROST executor")
	}
	if _, ok := ctx.Deadline(); !ok {
		return WalletRecord{}, errors.New("FROST executor needs a protocol deadline")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.abort = cancel
	var err error
	e.params, err = e.Chain.Parameters(ctx)
	if err != nil {
		return WalletRecord{}, err
	}
	if err = e.params.Validate(); err != nil {
		return WalletRecord{}, err
	}
	e.selection, err = e.Chain.Selection(ctx, e.Request.Group.Epoch)
	if err != nil {
		return WalletRecord{}, err
	}
	if err = e.selection.Validate(e.params); err != nil {
		return WalletRecord{}, err
	}
	domain := e.Journal.Domain()
	var chain [32]byte
	e.params.ChainID.FillBytes(chain[:])
	if domain.Chain != chain || domain.Registry != [20]byte(e.params.Registry) || domain.Epoch != e.selection.Epoch || !reflect.DeepEqual(e.Request.Group.Roster, e.selection.Roster()) || e.Request.Group.Threshold != e.params.Threshold || e.Request.Group.Quorum != e.params.GroupSize || !reflect.DeepEqual(e.Request.Participants, e.selection.Roster()) {
		return WalletRecord{}, errors.New("executor does not match chain selection")
	}
	address := crypto.PubkeyToAddress(e.Signer.PublicKey)
	var locals []uint16
	for i, a := range e.selection.Operators {
		if a == address {
			locals = append(locals, uint16(i+1))
		}
	}
	if len(locals) == 0 || !reflect.DeepEqual(locals, e.Request.LocalSeats) {
		return WalletRecord{}, errors.New("executor must own every selected local seat")
	}
	if e.PollInterval <= 0 {
		e.PollInterval = 250 * time.Millisecond
	}
	e.incoming = make(chan Attestation, 400)
	e.failure = make(chan error, 2)
	e.background.Add(2)
	go func() { defer e.background.Done(); e.receive(ctx) }()
	go func() { defer e.background.Done(); e.monitor(ctx) }()
	defer func() { cancel(); e.background.Wait() }()
	e.Providers.Acceptance = e
	workerContext, stopWorker := context.WithCancel(ctx)
	key, err := e.Engine.DKG(workerContext, e.Request, e.Providers)
	stopWorker()
	if err != nil {
		return e.finishFailure(ctx, err)
	}
	// KeyReady is durable at this point; re-read approval before allowing reload.
	view, err := e.checkedApproval(ctx)
	if err != nil {
		return e.finishFailure(ctx, err)
	}
	var session [32]byte
	if _, err = rand.Read(session[:]); err != nil {
		return e.finishFailure(ctx, err)
	}
	attempt, err := domain.NewAttempt("sign", session, view.Head)
	if err != nil {
		return e.finishFailure(ctx, err)
	}
	reload := frost.SigningRequest{Group: e.Request.Group, LocalSeats: locals, Selected: e.selection.Roster(), Attempt: attempt, Key: key}
	providers := e.Providers
	providers.Transport = reloadOnlyTransport{attempt}
	providers.Store = e.Journal
	if err = e.Engine.ReloadKey(ctx, reload, providers); err != nil {
		return e.finishFailure(ctx, err)
	}
	local := make([]Attestation, len(locals))
	for i, seat := range locals {
		reference := crypto.Keccak256Hash(key.LocalReferences[i])
		digest, x := e.Chain.ReadinessDigest(ctx, view.Approved, seat, reference)
		if x != nil {
			return e.finishFailure(ctx, x)
		}
		signature, x := Sign(e.Signer, digest)
		if x != nil {
			return e.finishFailure(ctx, x)
		}
		local[i] = Attestation{Stage: "ready", Epoch: e.selection.Epoch, Seat: seat, Digest: digest, Reference: reference, Signature: signature}
	}
	votes, err := e.collect(ctx, "ready", int(e.params.ReadySeats), local, func(a Attestation) bool {
		if a.Reference == ([32]byte{}) {
			return false
		}
		digest, x := e.Chain.ReadinessDigest(ctx, view.Approved, a.Seat, a.Reference)
		return x == nil && a.Digest == digest
	})
	if err != nil {
		return e.finishFailure(ctx, err)
	}
	var seats []uint16
	var references [][32]byte
	var signatures []byte
	for _, a := range votes {
		seats = append(seats, a.Seat)
		references = append(references, a.Reference)
		signatures = append(signatures, a.Signature...)
	}
	if err = e.Chain.Ready(ctx, e.record.ID, seats, references, signatures); err != nil {
		if _, x := e.reconcileWallet(ctx, view, err); x != nil {
			return e.finishFailure(ctx, x)
		}
	}
	// Verify the public postcondition; never infer it from transaction submission.
	view, err = e.checkedApproval(ctx)
	if err != nil {
		return e.finishFailure(ctx, err)
	}
	if view.Approved.State != 2 {
		return e.finishFailure(ctx, errors.New("readiness transaction did not certify wallet"))
	}
	e.record.State = "ReadyUnfunded"
	if err = e.Journal.SaveWallet(ctx, e.record); err != nil {
		return e.finishFailure(ctx, err)
	}
	return e.record, nil
}
func (e *Executor) receive(ctx context.Context) {
	for {
		a, err := e.Bus.Receive(ctx)
		if err != nil {
			select {
			case e.failure <- err:
				e.abort()
			default:
			}
			return
		}
		select {
		case e.incoming <- a:
		case <-ctx.Done():
			return
		}
	}
}
func (e *Executor) monitor(ctx context.Context) {
	ticker := time.NewTicker(e.PollInterval)
	defer ticker.Stop()
	for {
		if err := e.inspectSubmission(ctx); err != nil {
			select {
			case e.failure <- err:
				e.abort()
			default:
			}
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
func (e *Executor) inspectSubmission(ctx context.Context) error {
	view, err := e.Chain.View(ctx, e.selection.Epoch)
	if err != nil {
		return err
	}
	if view.ApprovedID == ([32]byte{}) && !e.approvalSeen {
		if view.Epoch != e.selection.Epoch {
			return ErrQuarantined
		}
		if view.State == 0 {
			return fmt.Errorf("%w: idle without approval at block %d, epoch %d", ErrExpired, view.Head, view.Epoch)
		}
		if (view.State == 2 || view.State == 3) && view.Head >= view.ResultDeadline {
			if err := e.Chain.ExpireDKG(ctx); err != nil {
				next, readErr := e.Chain.View(ctx, e.selection.Epoch)
				if readErr != nil || next.State != 0 {
					return err
				}
			}
			return fmt.Errorf("%w: result deadline %d at block %d", ErrExpired, view.ResultDeadline, view.Head)
		}
	}
	e.mu.Lock()
	if e.approvalSeen && (view.ApprovedID != e.observedApprovalID || [32]byte(view.ApprovalHash) != e.observedApprovalHash) {
		e.mu.Unlock()
		return fmt.Errorf("%w: observed approval was removed or replaced", ErrQuarantined)
	}
	if view.ApprovedID != ([32]byte{}) {
		e.approvalSeen = true
		e.observedApprovalID = view.ApprovedID
		e.observedApprovalHash = view.ApprovalHash
		if e.descriptor != nil && !reflect.DeepEqual(view.Approved.Descriptor, *e.descriptor) {
			e.mu.Unlock()
			return fmt.Errorf("%w: approved descriptor differs from candidate", ErrQuarantined)
		}
	}
	e.mu.Unlock()
	if view.ApprovedID != ([32]byte{}) {
		if view.Approved.State == 3 {
			return fmt.Errorf("%w: public wallet closed at block %d", ErrExpired, view.Head)
		}
		if view.Approved.State == 1 && view.Head >= view.Approved.Deadline {
			cause := e.Chain.Expire(ctx, view.ApprovedID)
			view, err = e.reconcileWallet(ctx, view, cause)
			if err != nil {
				return err
			}
			// A competing readiness transaction may have won before the deadline.
			// Only the observed terminal state, not a losing transaction, decides.
		}
	}
	if view.Submitted == nil || view.Epoch != e.selection.Epoch {
		return nil
	}
	valid, err := e.Chain.Validate(ctx, e.selection.Epoch, *view.Submitted)
	if err != nil {
		return err
	}
	if !valid {
		if err = e.Chain.Challenge(ctx); err != nil {
			next, x := e.Chain.View(ctx, e.selection.Epoch)
			if x != nil || next.SubmittedHash == view.SubmittedHash {
				return err
			}
		}
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.descriptor != nil && !bytes.Equal(NewResult(*e.descriptor).GroupPubKey, view.Submitted.GroupPubKey) {
		return fmt.Errorf("%w: foreign valid candidate", ErrQuarantined)
	}
	return nil
}
func (e *Executor) WaitCandidateAcceptance(ctx context.Context, c frost.Candidate) (receipt frost.Receipt, returnedErr error) {
	e.callbackMu.Lock()
	defer e.callbackMu.Unlock()
	defer func() { e.callbackErr = returnedErr }()
	if ctx.Err() != nil {
		return frost.Receipt{}, ctx.Err()
	}
	d, err := NewDescriptor(e.params, e.selection, c)
	if err != nil {
		return frost.Receipt{}, err
	}
	hash, err := DescriptorHash(d)
	if err != nil {
		return frost.Receipt{}, err
	}
	e.mu.Lock()
	e.descriptor = &d
	e.mu.Unlock()
	e.record = WalletRecord{Descriptor: d, DescriptorHash: hash, ID: WalletID(d.OutputKey), State: "Candidate"}
	if err = e.Journal.SaveWallet(ctx, e.record); err != nil {
		return frost.Receipt{}, err
	}
	result := NewResult(d)
	digest, err := e.Chain.ResultDigest(ctx, d.Epoch, result)
	if err != nil {
		return frost.Receipt{}, err
	}
	signature, err := Sign(e.Signer, digest)
	if err != nil {
		return frost.Receipt{}, err
	}
	var local []Attestation
	for _, seat := range e.Request.LocalSeats {
		local = append(local, Attestation{Stage: "result", Epoch: d.Epoch, Seat: seat, Digest: digest, Signature: signature})
	}
	votes, err := e.collect(ctx, "result", int(e.params.GroupSize), local, func(a Attestation) bool { return a.Digest == digest && a.Reference == ([32]byte{}) })
	if err != nil {
		return frost.Receipt{}, err
	}
	for _, a := range votes {
		result.Signatures = append(result.Signatures, a.Signature...)
	}
	want, err := ResultHash(result)
	if err != nil {
		return frost.Receipt{}, err
	}
	e.record.ResultHash = want
	ticker := time.NewTicker(e.PollInterval)
	defer ticker.Stop()
	var observedApproval [32]byte
	for {
		view, x := e.Chain.View(ctx, d.Epoch)
		if x != nil {
			return frost.Receipt{}, x
		}
		if view.ApprovedID != ([32]byte{}) {
			if view.ApprovedID != e.record.ID || view.Approved.ResultHash != want || view.Approved.DescriptorHash != hash || !reflect.DeepEqual(view.Approved.Descriptor, d) {
				return frost.Receipt{}, fmt.Errorf("%w: acceptance identity or canonical block", ErrQuarantined)
			}
			if observedApproval == ([32]byte{}) {
				observedApproval = view.ApprovalHash
			} else if observedApproval != [32]byte(view.ApprovalHash) {
				return frost.Receipt{}, fmt.Errorf("%w: acceptance identity or canonical block", ErrQuarantined)
			}
			if view.Head >= view.Approved.ApprovalBlock && view.Head-view.Approved.ApprovalBlock >= e.FinalityBlocks {
				e.record.ApprovalBlock = view.Approved.ApprovalBlock
				e.record.ApprovalHash = view.ApprovalHash
				e.record.Deadline = view.Approved.Deadline
				e.record.State = "RegisteredPendingReady"
				if _, x = e.checkedApproval(ctx); x != nil {
					return frost.Receipt{}, x
				}
				if x = e.Journal.SaveWallet(ctx, e.record); x != nil {
					return frost.Receipt{}, x
				}
				return frost.Receipt{Epoch: d.Epoch, Descriptor: d.SnowfallDescriptor}, nil
			}
		} else if observedApproval != ([32]byte{}) {
			return frost.Receipt{}, fmt.Errorf("%w: acceptance identity or canonical block", ErrQuarantined)
		} else if view.Epoch != d.Epoch {
			return frost.Receipt{}, fmt.Errorf("%w: acceptance identity or canonical block", ErrQuarantined)
		} else if view.State == 2 && e.selection.Operators[0] == crypto.PubkeyToAddress(e.Signer.PublicKey) {
			if x = e.Chain.Submit(ctx, result); x != nil {
				next, y := e.Chain.View(ctx, d.Epoch)
				if y != nil || next.State == 2 {
					return frost.Receipt{}, x
				}
			}
		} else if view.State == 3 && view.SubmittedHash == want && view.Head >= view.SubmittedAt+e.params.ChallengeBlocks {
			if x = e.Chain.Approve(ctx); x != nil {
				next, y := e.Chain.View(ctx, d.Epoch)
				if y != nil || next.ApprovedID == ([32]byte{}) {
					return frost.Receipt{}, x
				}
			}
		}
		select {
		case err = <-e.failure:
			return frost.Receipt{}, err
		case <-ctx.Done():
			return frost.Receipt{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
func (e *Executor) checkedApproval(ctx context.Context) (View, error) {
	v, err := e.Chain.View(ctx, e.selection.Epoch)
	if err != nil {
		return v, err
	}
	if v.ApprovedID != e.record.ID || v.Approved.ResultHash != e.record.ResultHash || v.Approved.DescriptorHash != e.record.DescriptorHash || v.ApprovalHash != e.record.ApprovalHash || v.Approved.ApprovalBlock != e.record.ApprovalBlock || !reflect.DeepEqual(v.Approved.Descriptor, e.record.Descriptor) {
		return v, fmt.Errorf("%w: saved approval mismatch id=%t result=%t descriptor=%t block=%t fields=%t", ErrQuarantined, v.ApprovedID == e.record.ID, v.Approved.ResultHash == e.record.ResultHash, v.Approved.DescriptorHash == e.record.DescriptorHash, v.ApprovalHash == e.record.ApprovalHash, reflect.DeepEqual(v.Approved.Descriptor, e.record.Descriptor))
	}
	if v.Approved.State == 3 {
		return v, ErrExpired
	}
	if v.Approved.State == 1 && v.Head >= v.Approved.Deadline {
		cause := e.Chain.Expire(ctx, e.record.ID)
		return e.reconcileWallet(ctx, v, cause)
	}
	return v, nil
}

// reconcileWallet classifies a competing readiness/expiry transaction by its
// public postcondition. A losing send can revert before the winner's block is
// visible. Read at most five times and for at most one second; do not resubmit.
// Internal cancellation cannot erase the outcome, but these reads cannot extend
// the worker protocol, resume an attempt, or accept a changed approval.
func (e *Executor) reconcileWallet(ctx context.Context, expected View, cause error) (View, error) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	var view View
	var readErr error
	for attempt := 0; attempt < 5; attempt++ {
		view, readErr = e.Chain.View(readCtx, e.selection.Epoch)
		if readErr == nil {
			if view.ApprovedID != expected.ApprovedID || view.ApprovalHash != expected.ApprovalHash || view.Approved.ResultHash != expected.Approved.ResultHash || view.Approved.DescriptorHash != expected.Approved.DescriptorHash || view.Approved.ApprovalBlock != expected.Approved.ApprovalBlock || view.Approved.Deadline != expected.Approved.Deadline || !reflect.DeepEqual(view.Approved.Descriptor, expected.Approved.Descriptor) {
				return view, fmt.Errorf("%w: approval changed during wallet reconciliation", ErrQuarantined)
			}
			switch view.Approved.State {
			case 3:
				return view, ErrExpired
			case 2:
				return view, nil
			case 1:
				// The competing transaction may not be visible yet.
			default:
				return view, fmt.Errorf("%w: unexpected wallet state %d", ErrQuarantined, view.Approved.State)
			}
		} else if errors.Is(readErr, ErrQuarantined) {
			return view, readErr
		}
		if attempt < 4 {
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-readCtx.Done():
				timer.Stop()
				return view, fmt.Errorf("%w: wallet reconciliation deadline: %v", ErrQuarantined, readCtx.Err())
			case <-timer.C:
			}
		}
	}
	return view, fmt.Errorf("%w: wallet outcome unresolved after transaction: %v; last read: %v", ErrQuarantined, cause, readErr)
}

func (e *Executor) collect(ctx context.Context, stage string, count int, local []Attestation, valid func(Attestation) bool) ([]Attestation, error) {
	votes := map[uint16]Attestation{}
	add := func(a Attestation) {
		if a.Stage == stage && a.Epoch == e.selection.Epoch && a.Seat > 0 && int(a.Seat) <= len(e.selection.Operators) && valid(a) && SignedBy(a.Signature, a.Digest, e.selection.Operators[a.Seat-1]) {
			votes[a.Seat] = a
		}
	}
	for _, a := range local {
		add(a)
		if err := e.Bus.Send(ctx, a); err != nil {
			return nil, err
		}
	}
	pending := e.pending
	e.pending = nil
	for _, a := range pending {
		add(a)
	}
	ticker := time.NewTicker(e.PollInterval)
	defer ticker.Stop()
	for len(votes) < count {
		select {
		case a := <-e.incoming:
			if stage == "result" && a.Stage == "ready" && a.Epoch == e.selection.Epoch && a.Seat > 0 && int(a.Seat) <= len(e.selection.Operators) && SignedBy(a.Signature, a.Digest, e.selection.Operators[a.Seat-1]) {
				exists := false
				for _, old := range e.pending {
					if old.Seat == a.Seat {
						exists = true
						break
					}
				}
				if !exists {
					e.pending = append(e.pending, a)
				}
			} else {
				add(a)
			}
		case err := <-e.failure:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if stage == "ready" {
				if _, err := e.checkedApproval(ctx); err != nil {
					return nil, err
				}
			}
		}
	}
	var ordered []Attestation
	for _, seat := range e.selection.Roster() {
		if a, ok := votes[seat]; ok {
			ordered = append(ordered, a)
		}
	}
	return ordered, nil
}
func (e *Executor) finishFailure(ctx context.Context, err error) (WalletRecord, error) {
	// Join both publishers before draining. Cancellation often reaches Run
	// before the monitor has delivered the classified cause that triggered it.
	if e.abort != nil {
		e.abort()
	}
	e.background.Wait()
	e.callbackMu.Lock()
	err = preferFailure(err, e.callbackErr)
	e.callbackMu.Unlock()
	for draining := true; draining; {
		select {
		case failure := <-e.failure:
			err = preferFailure(err, failure)
		default:
			draining = false
		}
	}
	if e.record.ID != ([32]byte{}) {
		e.record.Quarantined = !errors.Is(err, ErrExpired)
		if errors.Is(err, ErrExpired) && e.record.ApprovalBlock != 0 {
			e.record.State = "Closed"
		}
		if x := e.Journal.SaveWallet(context.WithoutCancel(ctx), e.record); x != nil {
			return e.record, fmt.Errorf("%w; retain failure: %v", err, x)
		}
	}
	return e.record, err
}

// A proven approval conflict remains quarantined even if another observer saw
// expiry. Otherwise, a classified terminal outcome outranks transport/worker
// errors, which in turn outrank the cancellation they caused.
func preferFailure(current, candidate error) error {
	rank := func(err error) int {
		switch {
		case err == nil:
			return -1
		case errors.Is(err, ErrQuarantined):
			return 3
		case errors.Is(err, ErrExpired):
			return 2
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return 0
		default:
			return 1
		}
	}
	if rank(candidate) > rank(current) {
		return candidate
	}
	return current
}

// Reload has no networking. Any unexpected worker I/O fails closed.
type reloadOnlyTransport struct{ attempt frost.Attempt }

func (t reloadOnlyTransport) Binding() (frost.Attempt, [32]byte) {
	return t.attempt, snowfallengine.AttemptID(t.attempt)
}
func (reloadOnlyTransport) Broadcast(context.Context, uint16, []byte) error {
	return errors.New("reload attempted broadcast")
}
func (reloadOnlyTransport) SendPrivate(context.Context, uint16, uint16, []byte) error {
	return errors.New("reload attempted private message")
}
func (reloadOnlyTransport) Receive(ctx context.Context) (frost.Incoming, error) {
	return frost.Incoming{}, errors.New("reload attempted receive")
}
