package transport

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type crashJournal struct {
	frost.RecoveryJournal
	stage string
}

func (j *crashJournal) Put(ctx context.Context, w frost.Write) (frost.PutResult, error) {
	result, e := j.RecoveryJournal.Put(ctx, w)
	if e != nil {
		return result, e
	}
	if (j.stage == "ready-lock" && w.Slot != nil && w.Slot.Kind == "ready-attestation") || (j.stage == "completion" && w.Kind == "completion-record") {
		return 0, errors.New("injected crash after fsync")
	}
	return result, nil
}
func (j *crashJournal) SaveRecovery(ctx context.Context, h frost.RecoveryHandle) error {
	if j.stage == "candidate" {
		return errors.New("injected crash before handle")
	}
	return j.RecoveryJournal.SaveRecovery(ctx, h)
}
func (j *crashJournal) SaveKey(ctx context.Context, k frost.KeyReady) error {
	if j.stage == "install" {
		return errors.New("injected crash before wallet install")
	}
	return j.RecoveryJournal.SaveKey(ctx, k)
}
func TestRealWorkerHostRecovery(t *testing.T) {
	worker := realWorkerConfig(t)
	for _, stage := range []string{"candidate", "ready-lock", "completion", "install"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			f := newFixture(t, [][]uint16{{1, 2}, {3}}, 256)
			for _, tr := range f.nodes {
				tr.Close()
			}
			configs := make([]store.Config, 2)
			roots := make([]*store.Store, 2)
			journals := make([]*crashJournal, 2)
			engines := make([]*snowfallengine.Engine, 2)
			transports := make([]*Transport, 2)
			open := func(i int, crash string) {
				var e error
				roots[i], e = store.Open(ctx, configs[i])
				if e != nil {
					t.Fatal(e)
				}
				scoped, e := roots[i].Scope(f.configs[i].Domain)
				if e != nil {
					t.Fatal(e)
				}
				journals[i] = &crashJournal{scoped, crash}
				engines[i], e = snowfallengine.NewGuarded(worker, journals[i])
				if e != nil {
					t.Fatal(e)
				}
			}
			connect := func(recovery bool) {
				hub := &testHub{names: map[string]bool{}}
				for i := range transports {
					c := f.configs[i]
					c.RecoveryJournal = journals[i]
					c.RecoveryOnly = recovery
					c.Network = testProvider{hub: hub, key: c.LocalPublicKey}
					var e error
					transports[i], e = New(ctx, c)
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			for i := range roots {
				configs[i] = store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{byte(i + 1)}, Key: [32]byte{42}, Fence: &memoryAuthority{}}
				open(i, stage)
			}
			connect(false)
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i := range engines {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, errs[i] = engines[i].DKG(ctx, frost.DKGRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: f.configs[i].LocalSeats, Attempt: f.configs[i].Attempt}, frost.Providers{Transport: transports[i], Acceptance: testFinality{}})
				}(i)
			}
			wg.Wait()
			for i, e := range errs {
				if e == nil {
					t.Fatal("crash not reached")
				}
				transports[i].Close()
				if _, e = journals[i].LoadKey(ctx); !errors.Is(e, store.ErrMissing) {
					t.Fatal("wallet installed across crash")
				}
				if e = roots[i].Close(); e != nil {
					t.Fatal(e)
				}
				open(i, "")
			}
			connect(true)
			keys := make([]frost.KeyReady, 2)
			for i := range engines {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					keys[i], errs[i] = engines[i].RecoverPending(ctx, frost.Providers{Transport: transports[i], Acceptance: testFinality{}})
				}(i)
			}
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatalf("recovery node %d: %v", i, e)
				}
				transports[i].Close()
				saved, e := journals[i].LoadKey(ctx)
				if e != nil || !reflect.DeepEqual(saved, keys[i]) {
					t.Fatal("wallet install mismatch")
				}
				if e = roots[i].Close(); e != nil {
					t.Fatal(e)
				}
				open(i, "")
			}
			// A second fresh host can reconstruct and recover solely from the journal.
			connect(true)
			for i := range engines {
				again, e := engines[i].RecoverPending(ctx, frost.Providers{Transport: transports[i], Acceptance: testFinality{}})
				if e != nil || !reflect.DeepEqual(again, keys[i]) {
					t.Fatalf("recovery not idempotent: %v", e)
				}
				transports[i].Close()
			}
			// Fresh attempt, new network and workers; normal engine verifies BIP340 too.
			for i := range f.configs {
				a, e := f.configs[i].Domain.NewAttempt("sign", [32]byte{9}, 101)
				if e != nil {
					t.Fatal(e)
				}
				f.configs[i].Attempt = a
				f.configs[i].ID = snowfallengine.AttemptID(a)
			}
			connect(false)
			message := sha256.Sum256([]byte("SF-02 recovered wallet"))
			signatures := make([][64]byte, 2)
			for i := range engines {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					signatures[i], errs[i] = engines[i].Sign(ctx, frost.SigningRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: f.configs[i].LocalSeats, Selected: []uint16{1, 2, 3}, Attempt: f.configs[i].Attempt, Key: keys[i], Message: message}, frost.Providers{Transport: transports[i], Acceptance: testFinality{}})
				}(i)
			}
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatal(e)
				}
				transports[i].Close()
				if e = roots[i].Close(); e != nil {
					t.Fatal(e)
				}
			}
			if signatures[0] != signatures[1] {
				t.Fatal("signatures differ")
			}
			requireNoWorkers(t, worker.Path)
			t.Logf("stage=%s key=%x message=%x signature=%x", stage, keys[0].Candidate.OutputKey, message, signatures[0])
		})
	}
}

func TestReadinessReplayValidatesOrigin(t *testing.T) {
	for _, which := range []string{"valid", "operator", "sender", "attempt", "kind", "domain"} {
		t.Run(which, func(t *testing.T) {
			f := newFixture(t, [][]uint16{{1, 2}, {3}}, 256)
			for _, tr := range f.nodes {
				tr.Close()
			}
			c := f.configs[0]
			root, e := store.Open(f.ctx, store.Config{Keystore: filepath.Join(t.TempDir(), "keys"), StorageID: [32]byte{1}, Key: [32]byte{2}, Fence: &memoryAuthority{}})
			if e != nil {
				t.Fatal(e)
			}
			defer root.Close()
			j, e := root.Scope(c.Domain)
			if e != nil {
				t.Fatal(e)
			}
			saved := frost.ReadinessEvidence{Domain: c.Domain, Attempt: c.ID, Sender: 3, OperatorKey: f.configs[1].LocalPublicKey, Message: testEnvelope("ready-attestation", c.ID, 3, 0, []byte("statement"))}
			switch which {
			case "operator":
				saved.OperatorKey = c.LocalPublicKey
			case "sender":
				saved.Sender = 2
			case "attempt":
				saved.Message = testEnvelope("ready-attestation", [32]byte{9}, 3, 0, []byte("statement"))
			case "kind":
				saved.Message = testEnvelope("result-vote", c.ID, 3, 0, []byte("statement"))
			case "domain":
				saved.Domain.Registry[0]++
			}
			e = j.SaveReadiness(f.ctx, saved)
			if which == "domain" {
				if e == nil {
					t.Fatal("foreign-domain evidence accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			c.RecoveryJournal = j
			c.RecoveryOnly = true
			c.Network = testProvider{hub: &testHub{names: map[string]bool{}}, key: c.LocalPublicKey}
			tr, e := New(f.ctx, c)
			if which != "valid" {
				if e == nil {
					tr.Close()
					t.Fatal("invalid saved attribution replayed")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer tr.Close()
			got, e := tr.Receive(f.ctx)
			if e != nil || got.AuthenticatedSender != 3 || !reflect.DeepEqual(got.Message, saved.Message) {
				t.Fatal("original authenticated evidence not replayed")
			}
		})
	}
}

func TestGuardedStartupRejectsOldCapabilityBeforeClaim(t *testing.T) {
	ctx := context.Background()
	root, e := store.Open(ctx, store.Config{Keystore: filepath.Join(t.TempDir(), "keys"), StorageID: [32]byte{1}, Key: [32]byte{2}, Fence: &memoryAuthority{}})
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	domain := frost.Domain{Network: "startup-test", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 77}
	j, e := root.Scope(domain)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("/bin/cat")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = snowfallengine.NewGuarded(snowfallengine.WorkerConfig{Path: "/bin/cat", SHA256: sha256.Sum256(raw)}, j); e == nil {
		t.Fatal("guarded startup accepted worker without recovery")
	}
	if _, e = j.RecoveryIntent(ctx); !errors.Is(e, store.ErrMissing) {
		t.Fatal("startup probe changed the claim journal")
	}
}
