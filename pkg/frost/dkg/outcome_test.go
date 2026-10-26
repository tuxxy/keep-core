package dkg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
)

type outcomeChain struct {
	Chain
	read func(context.Context) (View, error)
}

func (c outcomeChain) View(ctx context.Context, _ uint64) (View, error) { return c.read(ctx) }

func TestReconcileWalletOutcome(t *testing.T) {
	base := View{ApprovedID: [32]byte{1}, ApprovalHash: common.Hash{2}, Approved: Wallet{State: 1, ApprovalBlock: 10, Deadline: 20, ResultHash: [32]byte{3}, DescriptorHash: [32]byte{4}}}
	for _, tc := range []struct {
		name    string
		state   uint8
		changed bool
		readErr error
		want    error
		calls   int
	}{
		{"closed after pending reads", 3, false, nil, ErrExpired, 3},
		{"readiness won", 2, false, nil, nil, 3},
		{"different approval", 3, true, nil, ErrQuarantined, 1},
		{"unknown state", 0, false, nil, ErrQuarantined, 1},
		{"persistent pending", 1, false, nil, ErrQuarantined, 5},
		{"reorg", 1, false, ErrQuarantined, ErrQuarantined, 1},
		{"unavailable", 1, false, errors.New("RPC unavailable"), ErrQuarantined, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			e := Executor{selection: Selection{Epoch: 77}, Chain: outcomeChain{read: func(ctx context.Context) (View, error) {
				if ctx.Err() != nil {
					t.Fatal("classification inherited internal cancellation")
				}
				calls++
				v := base
				if tc.changed {
					v.ApprovalHash[0] ^= 1
				}
				if tc.calls != 3 || calls >= 3 {
					v.Approved.State = tc.state
				}
				return v, tc.readErr
			}}}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := e.reconcileWallet(ctx, base, errors.New("transaction lost race"))
			if !errors.Is(err, tc.want) || calls != tc.calls {
				t.Fatalf("outcome=%v calls=%d", err, calls)
			}
		})
	}
}

type outcomeJournal struct {
	Journal
	saved WalletRecord
	key   *frost.KeyReady
}

func (j *outcomeJournal) LoadKey(context.Context) (frost.KeyReady, error) {
	if j.key == nil {
		return frost.KeyReady{}, errors.New("no durable key")
	}
	return *j.key, nil
}

func (j *outcomeJournal) SaveWallet(_ context.Context, w WalletRecord) error { j.saved = w; return nil }

func TestFinishFailureDrainsAndPrefersClassification(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		initial, errorFromMonitor error
		want                      error
		closed                    bool
	}{
		{"canceled after ready", context.Canceled, ErrExpired, ErrExpired, true},
		{"losing expiry revert", errors.New("FROST expiry denied"), ErrExpired, ErrExpired, true},
		{"reorg beats expiry", ErrExpired, ErrQuarantined, ErrQuarantined, false},
		{"expiry cannot clear reorg", ErrQuarantined, ErrExpired, ErrQuarantined, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := &outcomeJournal{}
			ctx, cancel := context.WithCancel(context.Background())
			e := Executor{Journal: j, abort: cancel, failure: make(chan error, 2), record: WalletRecord{ID: [32]byte{1}, ApprovalBlock: 10, State: "RegisteredPendingReady"}}
			e.background.Add(1)
			go func() {
				defer e.background.Done()
				<-ctx.Done()
				e.failure <- context.Canceled
				e.failure <- fmt.Errorf("monitor: %w", tc.errorFromMonitor)
			}()
			_, err := e.finishFailure(ctx, tc.initial)
			if !errors.Is(err, tc.want) || len(e.failure) != 0 {
				t.Fatalf("cause not retained/drained: %v", err)
			}
			if (j.saved.State == "Closed") != tc.closed || j.saved.Quarantined == tc.closed {
				t.Fatalf("wrong durable outcome: %+v", j.saved)
			}
		})
	}
}

func TestCollectRejectsWrongOperatorSignature(t *testing.T) {
	owner, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	digest := [32]byte{3}
	valid, err := Sign(owner, digest)
	if err != nil {
		t.Fatal(err)
	}
	forged, err := Sign(other, digest)
	if err != nil {
		t.Fatal(err)
	}
	e := Executor{selection: Selection{Epoch: 77, Members: []uint32{1}, Operators: []common.Address{crypto.PubkeyToAddress(owner.PublicKey)}}, incoming: make(chan Attestation, 2), failure: make(chan error, 1), PollInterval: time.Second}
	e.incoming <- Attestation{Stage: "result", Epoch: 77, Seat: 1, Digest: digest, Signature: forged}
	e.incoming <- Attestation{Stage: "result", Epoch: 77, Seat: 1, Digest: digest, Signature: valid}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	votes, err := e.collect(ctx, "result", 1, nil, func(a Attestation) bool { return a.Digest == digest })
	if err != nil || len(votes) != 1 || !bytes.Equal(votes[0].Signature, valid) {
		t.Fatalf("forged attestation counted: %+v %v", votes, err)
	}
}

func TestFinishSuccessRetainsClassifiedObserverFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		late error
		want error
	}{
		{"monitor quarantine", ErrQuarantined, ErrQuarantined},
		{"quarantine joined with cancellation", errors.Join(ErrQuarantined, context.Canceled), ErrQuarantined},
		{"transport uncertainty", errors.New("closed transport"), errors.New("closed transport")},
		{"self cancellation", context.Canceled, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			j := &outcomeJournal{}
			e := Executor{Journal: j, abort: cancel, failure: make(chan error, 2), record: WalletRecord{ID: [32]byte{1}, ApprovalBlock: 10, State: "ReadyUnfunded"}}
			e.background.Add(1)
			go func() { defer e.background.Done(); <-ctx.Done(); e.failure <- context.Canceled; e.failure <- tc.late }()
			record, err := e.finishSuccess(ctx)
			if tc.want == nil {
				if err != nil || record.Quarantined || j.saved.ID != ([32]byte{}) {
					t.Fatalf("normal completion became quarantine: %v %+v", err, record)
				}
			} else {
				if err == nil || !record.Quarantined || !j.saved.Quarantined || j.saved.State != "ReadyUnfunded" {
					t.Fatalf("late classification lost: %v %+v %+v", err, record, j.saved)
				}
				if errors.Is(tc.late, ErrQuarantined) && !errors.Is(err, ErrQuarantined) {
					t.Fatal("classification changed", err)
				}
			}
			if len(e.failure) != 0 {
				t.Fatal("observer failures not drained")
			}
		})
	}
}

func TestMonitorReconcilesOverlappingApprovalObservation(t *testing.T) {
	approved := View{Epoch: 77, Head: 10, ApprovedID: [32]byte{1}, ApprovalHash: common.Hash{2}, Approved: Wallet{State: 1, ApprovalBlock: 10, Deadline: 20}}
	for _, tc := range []struct {
		name    string
		removed bool
		want    error
	}{
		{"delayed canonical preapproval", false, nil}, {"removal still detected after overlap", true, ErrQuarantined},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			calls := 0
			e := Executor{selection: Selection{Epoch: 77}}
			e.Chain = outcomeChain{read: func(context.Context) (View, error) {
				calls++
				if calls == 1 {
					close(started)
					<-release
					return View{Epoch: 77, Head: 9, State: 2, ResultDeadline: 30}, nil
				}
				v := approved
				if tc.removed {
					v.ApprovedID = [32]byte{}
				}
				return v, nil
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
			err := <-done
			if !errors.Is(err, tc.want) || calls != 2 {
				t.Fatalf("overlapping view: calls=%d got=%v want=%v", calls, err, tc.want)
			}
		})
	}
}

func TestMonitorPublicationAfterObservationSnapshot(t *testing.T) {
	e := Executor{selection: Selection{Epoch: 77}}
	e.Chain = outcomeChain{read: func(context.Context) (View, error) {
		return View{Epoch: 77, Head: 9, State: 2, ResultDeadline: 30}, nil
	}}
	published := false
	e.Boundary = func(name string) error {
		if name == "after-monitor-observation-snapshot" {
			e.mu.Lock()
			e.approvalSeen = true
			e.approvalGeneration++
			e.observedApprovalID = [32]byte{1}
			e.observedApprovalHash = [32]byte{2}
			e.mu.Unlock()
			published = true
		}
		return nil
	}
	if err := e.inspectSubmission(context.Background()); err != nil || !published {
		t.Fatalf("canonical older snapshot falsely classified: published=%t error=%v", published, err)
	}
}

func TestDurableKeyCancellationDoesNotQuarantine(t *testing.T) {
	for _, ready := range []bool{false, true} {
		j := &outcomeJournal{}
		e := Executor{Journal: j, keyReady: ready, record: WalletRecord{ID: [32]byte{1}, State: "RegisteredPendingReady"}}
		_, err := e.finishFailure(context.Background(), context.Canceled)
		if !errors.Is(err, context.Canceled) || j.saved.Quarantined == ready {
			t.Fatal("control DurableKeyInterruption assertion: wrong cancellation classification", ready, err)
		}
	}
	// A classified approval conflict still dominates a completed key.
	j := &outcomeJournal{}
	e := Executor{Journal: j, keyReady: true, callbackErr: ErrQuarantined, record: WalletRecord{ID: [32]byte{1}, State: "RegisteredPendingReady"}}
	_, err := e.finishFailure(context.Background(), context.Canceled)
	if !errors.Is(err, ErrQuarantined) || !j.saved.Quarantined {
		t.Fatal("durable key erased an approval conflict")
	}
}

func TestDurableKeySurvivesLostReplyAndRPCFailure(t *testing.T) {
	for _, lostReply := range []bool{false, true} {
		descriptor := Descriptor{Epoch: 9, SnowfallDescriptor: [32]byte{1}, OutputKey: [32]byte{2}}
		key := &frost.KeyReady{Candidate: frost.Candidate{Epoch: 9, Descriptor: descriptor.SnowfallDescriptor, OutputKey: descriptor.OutputKey}}
		j := &outcomeJournal{key: key}
		e := Executor{Journal: j, keyReady: !lostReply, record: WalletRecord{ID: [32]byte{3}, State: "RegisteredPendingReady", Descriptor: descriptor}}
		_, err := e.finishFailure(context.Background(), errors.New("RPC unavailable after certification"))
		if err == nil || j.saved.Quarantined {
			t.Fatal("completed key quarantined by an unclassified transient failure")
		}
	}
}
