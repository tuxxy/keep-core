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
	go e.receive(ctx)
	go e.monitor(ctx)
	e.Providers.Acceptance = e
	workerContext, stopWorker := context.WithCancel(ctx)
	key, err := e.Engine.DKG(workerContext, e.Request, e.Providers)
	stopWorker()
	e.callbackMu.Lock()
	if err != nil && e.callbackErr != nil && !errors.Is(e.callbackErr, context.Canceled) {
		err = e.callbackErr
	}
	e.callbackMu.Unlock()
	if err != nil {
		select {
		case failure := <-e.failure:
			if !errors.Is(failure, context.Canceled) && !errors.Is(failure, context.DeadlineExceeded) {
				err = failure
			}
		default:
		}
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
		v, x := e.checkedApproval(ctx)
		if x != nil {
			return e.finishFailure(ctx, x)
		}
		if v.Approved.State != 2 {
			return e.finishFailure(ctx, err)
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
		return WalletRecord{}, err
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
			if err := e.Chain.Expire(ctx, view.ApprovedID); err != nil {
				next, readErr := e.Chain.View(ctx, e.selection.Epoch)
				if readErr != nil || next.Approved.State != 3 {
					return err
				}
			}
			return fmt.Errorf("%w: readiness deadline %d at block %d", ErrExpired, view.Approved.Deadline, view.Head)
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
		if err = e.Chain.Expire(ctx, e.record.ID); err != nil {
			next, readErr := e.Chain.View(ctx, e.selection.Epoch)
			if readErr != nil || next.Approved.State != 3 {
				return v, err
			}
		}
		return v, ErrExpired
	}
	return v, nil
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
