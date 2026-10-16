package signing

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Controlled authority; real encrypted store and commit protocol, not a claim mock.
type reviewFence struct{ point store.Point }

func (f *reviewFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) { return f, nil }
func (f *reviewFence) Head(context.Context) (store.Point, error)                   { return f.point, nil }
func (f *reviewFence) Advance(_ context.Context, b, a store.Point) error {
	if b != f.point {
		return store.ErrQuarantined
	}
	f.point = a
	return nil
}
func (f *reviewFence) Close() error { return nil }

type reviewJournal struct {
	*journalStub
	durable *store.Scoped
}

func (j *reviewJournal) RetainSigningRecord(c context.Context, id [32]byte, p []byte) error {
	return j.durable.RetainSigningRecord(c, id, p)
}
func (j *reviewJournal) ReadSigningRecord(c context.Context, id [32]byte) ([]byte, error) {
	return j.durable.ReadSigningRecord(c, id)
}

// Engine seam replaces only crypto/transport execution. It makes actual terminal
// claims, calls the actual executor guard, and returns valid BIP340 signatures.
type reviewEngine struct {
	frost.Engine
	journal *reviewJournal
	private *btcec.PrivateKey
	signed  func()
	calls   int
}

func (e *reviewEngine) Sign(c context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	raw, _ := json.Marshal(r)
	release, err := e.journal.durable.Claim(c, snowfallengine.AttemptID(r.Attempt), "sign", raw)
	if err != nil {
		return [64]byte{}, err
	}
	defer release()
	if err = p.SigningAuthorization.BeforeSigning(c, r); err != nil {
		return [64]byte{}, err
	}
	sig, err := schnorr.Sign(e.private, r.Message[:])
	if err != nil {
		return [64]byte{}, err
	}
	var out [64]byte
	copy(out[:], sig.Serialize())
	e.calls++
	if e.signed != nil {
		e.signed()
	}
	return out, nil
}
func reviewFixture(t *testing.T) (*Executor, *reviewEngine, *store.Store, store.Config, *reviewJournal) {
	t.Helper()
	e, _, _, old := executorFixture(t)
	c := store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{1}, Key: [32]byte{2}, Fence: &reviewFence{}}
	s, err := store.Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := s.Scope(e.config.Journal.Domain())
	if err != nil {
		t.Fatal(err)
	}
	j := &reviewJournal{journalStub: e.config.Journal.(*journalStub), durable: scoped}
	e.config.Journal = j
	eng := &reviewEngine{journal: j, private: old.private}
	e.engine = eng
	t.Cleanup(func() { s.Close() })
	return e, eng, s, c, j
}
func doneID(e *Executor) [32]byte {
	a, _ := e.attempt(0)
	id := snowfallengine.AttemptID(a)
	return sha256.Sum256(append([]byte("keep-core/frost/signing-done/v1/"), id[:]...))
}

func TestReturnedSignatureSurvivesCancellation(t *testing.T) {
	for _, cancelAt := range []string{"engine-return", "transport-cleanup"} {
		t.Run(cancelAt, func(t *testing.T) {
			e, eng, _, _, j := reviewFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelAt == "engine-return" {
				eng.signed = cancel
			}
			e.config.Transport = func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
				return transportStub{}, func() { e.config.Reservations.(*reservationStub).view.State = "TimeoutPending"; cancel() }, nil
			}
			result, err := e.Run(ctx)
			if !errors.Is(err, ErrLateSignature) || result != nil || eng.calls != 1 {
				t.Fatalf("unexpected result=%v err=%v signed=%d", result, err, eng.calls)
			}
			raw, err := j.ReadSigningRecord(context.Background(), doneID(e))
			if err != nil {
				t.Fatal("returned signature was lost", err)
			}
			var done Done
			if err = done.Unmarshal(raw); err != nil {
				t.Fatal(err)
			}
			attempt, _ := e.attempt(0)
			if _, err = done.Verify(e.doneContext(0, attempt), e.Plan().Snapshot().OutputKey()); err != nil {
				t.Fatal(err)
			}
			if _, err = e.LoadResult(context.Background()); !errors.Is(err, ErrLateSignature) {
				t.Fatal("late completion escaped quarantine", err)
			}
		})
	}
}
func TestRecoverResultAfterDurableDone(t *testing.T) {
	e, eng, s, c, j := reviewFixture(t)
	e.boundary = func(name string) error {
		if name == "after-signature" {
			return errors.New("simulated crash boundary")
		}
		return nil
	}
	if _, err := e.Run(context.Background()); err == nil {
		t.Fatal("boundary ignored")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	scoped, err := reopened.Scope(j.Domain())
	if err != nil {
		t.Fatal(err)
	}
	j.durable = scoped
	fresh, err := NewLocalExecutor(e.config)
	if err != nil {
		t.Fatal(err)
	}
	fresh.engine = eng
	result, err := fresh.LoadResult(context.Background())
	if err != nil {
		t.Fatal("complete durable signatures could not recover", err)
	}
	if err = fresh.ValidateResult(result); err != nil {
		t.Fatal(err)
	}
	if err = fresh.Broadcast(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.Run(context.Background()); !errors.Is(err, store.ErrClaimed) {
		t.Fatal("original claim ceased to be terminal", err)
	}
	if eng.calls != 1 {
		t.Fatal("recovery generated another signature")
	}
	retained, err := fresh.LoadResult(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if retained.Transaction.Hash() != result.Transaction.Hash() {
		t.Fatal("recovered bytes changed")
	}
}

func TestMultiInputCompletionRecovery(t *testing.T) {
	for _, completed := range []int{1, 2} {
		t.Run(fmt.Sprintf("completed-%d", completed), func(t *testing.T) {
			e, engine, _, _, journal := reviewFixture(t)
			proposal, _, funding := planFixture(t)
			secondOutput := *funding.Outputs[0]
			funding.Outputs = append(funding.Outputs, &secondOutput)
			first := bitcoin.TransactionOutpoint{TransactionHash: funding.Hash()}
			second := bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: 1}
			proposal.Transaction.Inputs[0].Outpoint = &first
			proposal.Transaction.Inputs = append(proposal.Transaction.Inputs, &bitcoin.TransactionInput{Outpoint: &second, Sequence: proposal.Transaction.Inputs[0].Sequence})
			proposal.Transaction.Outputs[1].Value += secondOutput.Value
			proposal.Prevouts[0].Outpoint = first
			prevout := proposal.Prevouts[0]
			prevout.Outpoint = second
			proposal.Prevouts = append(proposal.Prevouts, prevout)
			proposal.ConflictInput = first
			plan, err := NewPlan(proposal)
			if err != nil {
				t.Fatal(err)
			}
			e.config.Plan = plan
			e.config.Reservations.(*reservationStub).view.Commitment = plan.Commitment()
			e.config.Bitcoin.(*bitcoinStub).raw = funding.Serialize()
			e.boundary = func(name string) error {
				if name == "after-signature" && engine.calls == completed {
					return errors.New("crash after completion")
				}
				return nil
			}
			if _, err := e.Run(context.Background()); err == nil {
				t.Fatal("crash boundary ignored")
			}
			fresh, err := NewLocalExecutor(e.config)
			if err != nil {
				t.Fatal(err)
			}
			fresh.engine = engine
			result, err := fresh.LoadResult(context.Background())
			if completed == 1 {
				if !errors.Is(err, store.ErrMissing) {
					t.Fatal("partial result was accepted", err)
				}
				if _, err := journal.ReadSigningRecord(context.Background(), fresh.resultID()); !errors.Is(err, store.ErrMissing) {
					t.Fatal("partial aggregate was persisted", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Done) != 2 {
					t.Fatal("not all inputs recovered")
				}
				if err := fresh.Broadcast(context.Background(), result); err != nil {
					t.Fatal(err)
				}
			}
			if engine.calls != completed {
				t.Fatal("recovery granted another signature")
			}
		})
	}
}
