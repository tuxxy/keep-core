package signing

// Fixture adapted from Popper's independent 2026-10-13 adversarial probe.
// Assertions below are V-04 verification-agent regressions and self-review.
// Real application records and claims; wallet/key and crypto execution are
// controlled. This is not worker IPC, process-kill or storage-class evidence.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/frost"

	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Controlled authority; real encrypted store and commit protocol, not a claim mock.
type v04RecoveryFence struct{ point store.Point }

func (f *v04RecoveryFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	return f, nil
}
func (f *v04RecoveryFence) Head(context.Context) (store.Point, error) { return f.point, nil }
func (f *v04RecoveryFence) Advance(_ context.Context, b, a store.Point) error {
	if b != f.point {
		return store.ErrQuarantined
	}
	f.point = a
	return nil
}
func (f *v04RecoveryFence) Close() error { return nil }

type v04RecoveryJournal struct {
	*journalStub
	durable *store.Scoped
}

func (j *v04RecoveryJournal) RetainSigningRecord(c context.Context, id [32]byte, p []byte) error {
	return j.durable.RetainSigningRecord(c, id, p)
}
func (j *v04RecoveryJournal) ReadSigningRecord(c context.Context, id [32]byte) ([]byte, error) {
	return j.durable.ReadSigningRecord(c, id)
}

// Engine seam replaces only crypto/transport execution. It makes actual terminal
// claims, calls the actual executor guard, and returns valid BIP340 signatures.
type v04RecoveryEngine struct {
	frost.Engine
	journal    *v04RecoveryJournal
	private    *btcec.PrivateKey
	signed     func()
	afterClaim func()
	calls      int
}

func (e *v04RecoveryEngine) Sign(c context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	raw, _ := json.Marshal(r)
	release, err := e.journal.durable.Claim(c, snowfallengine.AttemptID(r.Attempt), "sign", raw)
	if err != nil {
		return [64]byte{}, err
	}
	defer release()
	if e.afterClaim != nil {
		e.afterClaim()
	}
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
func v04RecoveryFixture(t *testing.T) (*Executor, *v04RecoveryEngine, *store.Store, store.Config, *v04RecoveryJournal) {
	t.Helper()
	e, _, _, old := executorFixture(t)
	c := store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{1}, Key: [32]byte{2}, Fence: &v04RecoveryFence{}}
	s, err := store.Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := s.Scope(e.config.Journal.Domain())
	if err != nil {
		t.Fatal(err)
	}
	j := &v04RecoveryJournal{journalStub: e.config.Journal.(*journalStub), durable: scoped}
	e.config.Journal = j
	eng := &v04RecoveryEngine{journal: j, private: old.private}
	e.engine = eng
	t.Cleanup(func() { s.Close() })
	return e, eng, s, c, j
}
func v04DoneID(e *Executor) [32]byte {
	a, _ := e.attempt(0)
	id := snowfallengine.AttemptID(a)
	return sha256.Sum256(append([]byte("keep-core/frost/signing-done/v1/"), id[:]...))
}

func TestV04CancelledReturnedSignatureRetained(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "cancel_and_timeout"}[late], func(t *testing.T) {
			e, eng, _, _, j := v04RecoveryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e.config.Transport = func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
				return transportStub{}, func() {
					if late {
						e.config.Reservations.(*reservationStub).view.State = "TimeoutPending"
					}
					cancel()
				}, nil
			}
			result, err := e.Run(ctx)
			if err == nil || result != nil || eng.calls != 1 {
				t.Fatalf("expected classified cancellation after one valid return, result=%v err=%v calls=%d", result, err, eng.calls)
			}
			raw, err := j.ReadSigningRecord(context.Background(), v04DoneID(e))
			if err != nil {
				t.Fatal("returned valid signature lost at caller cancellation", err)
			}
			var done Done
			if err = done.Unmarshal(raw); err != nil {
				t.Fatal(err)
			}
			attempt, _ := e.attempt(0)
			if _, err = done.Verify(e.doneContext(0, attempt), e.Plan().Snapshot().OutputKey()); err != nil {
				t.Fatal(err)
			}
			if _, err = j.ReadSigningRecord(context.Background(), e.Plan().Commitment()); err != nil {
				t.Fatal("lost intent", err)
			}
			if _, err = j.durable.Claim(context.Background(), snowfallengine.AttemptID(attempt), "sign", []byte("retry")); !errors.Is(err, store.ErrClaimed) {
				t.Fatal("lost terminal claim", err)
			}
		})
	}
}
func TestV04DoneOnlyRecovery(t *testing.T) {
	e, eng, s, c, j := v04RecoveryFixture(t)
	e.boundary = func(name string) error {
		if name == "after-signature" {
			return errors.New("post-durable completion interruption")
		}
		return nil
	}
	if _, err := e.Run(context.Background()); err == nil {
		t.Fatal("boundary ignored")
	}
	if _, err := j.ReadSigningRecord(context.Background(), v04DoneID(e)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ReadSigningRecord(context.Background(), e.resultID()); !errors.Is(err, store.ErrMissing) {
		t.Fatal("aggregate should be absent", err)
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
		t.Fatal("durable per-input completion did not reconstruct", err)
	}
	if err = fresh.ValidateResult(result); err != nil {
		t.Fatal(err)
	}
	if err = fresh.Broadcast(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if eng.calls != 1 {
		t.Fatal("recovery generated another signature")
	}
	fresh.config.Reservations.(*reservationStub).view.State = "TimeoutPending"
	if err = fresh.Broadcast(context.Background(), result); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("recovered late signature broadcast", err)
	}
}
