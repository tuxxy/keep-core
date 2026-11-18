package store

// Chaosnet M4 finding (2026-11-18, FINDING-M4-DKG-QUARANTINE.md): a FROST DKG
// aborted while its claim was mid-commit quarantined node-02's store for good.
// The DKG executor cancels the claim's context; the store passes that context
// to fence.Advance after the journal is installed (store.go, "before-fence");
// the developer fence returned ctx.Err() without installing; the store treats
// every Advance error as possibly committed and quarantines.
//
// Decision B (tux, 2026-11-18): the developer fence must not let the caller's
// context decide an Advance. Once called, its compare-and-install on the local
// file runs to completion, as K-09's test fence (b6DiskLease.Advance in
// pkg/tbtc/frost_node_b6_world_test.go) already does. The store does not
// change. Everything else about the fence stays as frozen in chaosnet M2
// (config/frost_developer_fence_test.go): flock exclusivity, two slots,
// storage-ID binding, compare-and-install, missing file refused, closed lease
// refused.
//
// Choices pinned here (test writer, 2026-11-18):
//   - Head does not consult the context either. The store, which is not to
//     change, poisons itself on any Head error (Store.check), and a
//     cancellation can land between check's own ctx.Err() and its Head call.
//     Head commits nothing and reads a local file in microseconds, so honouring
//     cancellation there buys nothing and keeps the same permanent-quarantine
//     trap open. K-09's test fence also ignores the context in Head.
//   - Acquire keeps refusing an already-cancelled context up front. It runs
//     only in Open, which returns its error as is (no quarantine, nothing
//     persisted), and refusing before the flock means a cancelled Open neither
//     binds a provisioned (empty) fence file nor holds its lock.
//
// Every fence file below is a TEST file in t.TempDir(); the fence itself is the
// real developer fence (NewDeveloperFence). The store is the real store, with
// its existing test-only boundary hook to place the cancellation.

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// storeStepDeadline is the deadline-mode context's lifetime in the store
// case: the claim must reach its commit step before it expires, so it is far
// above the few milliseconds that takes, and the case then waits out the rest
// (the cases run in parallel).
const storeStepDeadline = 300 * time.Millisecond

// cancelModes are the two ways a caller's context ends.
var cancelModes = []string{"cancel", "deadline"}

// endingContext returns a context and a function that ends it in the given
// mode and returns only once it has ended (bounded: a deadline context expires
// after d, and end waits at most 5 s). A real deadline cannot be started
// later, so a caller that must reach a step before it ends passes a d that is
// a wide margin over that step (storeStepDeadline).
func endingContext(t *testing.T, mode string, d time.Duration) (context.Context, func()) {
	t.Helper()
	switch mode {
	case "cancel":
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		return ctx, cancel
	case "deadline":
		ctx, cancel := context.WithTimeout(context.Background(), d)
		t.Cleanup(cancel)
		return ctx, func() {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Error("test deadline context did not expire")
			}
		}
	}
	t.Fatalf("unknown mode %q", mode)
	return nil, nil
}

// endedContext returns a context that has already ended in the given mode.
func endedContext(t *testing.T, mode string) context.Context {
	t.Helper()
	ctx, end := endingContext(t, mode, time.Millisecond)
	end()
	if ctx.Err() == nil {
		t.Fatal("test context did not end")
	}
	return ctx
}

// provisionFence creates an empty TEST fence file, as the lab provisions it.
func provisionFence(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fence")
	must(t, os.WriteFile(path, nil, 0600))
	return path
}

func developerFenceAt(t *testing.T, path string) Fence {
	t.Helper()
	f, err := NewDeveloperFence(path)
	must(t, err)
	return f
}

// devStoreConfig is a store configuration backed by the real developer fence
// on a provisioned TEST fence file.
func devStoreConfig(t *testing.T) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	fencePath := provisionFence(t, dir)
	c := configAt(filepath.Join(dir, "keystore"), filepath.Join(dir, "unused"))
	c.Fence = developerFenceAt(t, fencePath)
	return c, fencePath
}

func fencePoint(sequence uint64, label string) Point {
	return Point{Sequence: sequence, Digest: sha256.Sum256([]byte(label))}
}

// requireInstalledHead checks that the store's fence head is at sequence and
// names an installed journal whose bytes hash to the head's digest, and that
// the store's own view agrees. The journal is hashed, never decoded.
func requireInstalledHead(t *testing.T, s *Store, sequence uint64, what string) {
	t.Helper()
	head, err := s.fence.Head(context.Background())
	if err != nil {
		t.Fatalf("%s: the fence head cannot be read: %v", what, err)
	}
	if head.Sequence != sequence {
		t.Fatalf("%s: the fence head is at sequence %d, want %d (the fence did not advance to the installed journal)", what, head.Sequence, sequence)
	}
	raw, err := os.ReadFile(s.filename(head))
	if err != nil {
		t.Fatalf("%s: the fence head names no installed journal: %v", what, err)
	}
	if sha256.Sum256(raw) != head.Digest {
		t.Fatalf("%s: the fence head's digest does not match its journal", what)
	}
	if s.point != head {
		t.Fatalf("%s: the store's point is not the fence head", what)
	}
}

// TestDeveloperFenceStoreSurvivesClaimCancelledInCommit: through the public
// store API with the real developer fence, a claim whose context ends while
// the claim is committing (at the store's "before-fence" step, where node-02's
// abort landed, and at the earlier commit steps, which reach the fence with the
// same ended context) leaves the store usable: the claim succeeds or fails
// without quarantine, the fence is at the installed journal, the next claim on
// the same handle works, and a reopen is not quarantined and still holds the
// cancelled claim.
func TestDeveloperFenceStoreSurvivesClaimCancelledInCommit(t *testing.T) {
	steps := []string{"before-write", "before-install", "after-directory-sync", "before-fence"}
	for _, mode := range cancelModes {
		for _, step := range steps {
			mode, step := mode, step
			t.Run(mode+"/"+step, func(t *testing.T) {
				t.Parallel()
				c, _ := devStoreConfig(t)
				s, err := Open(context.Background(), c)
				must(t, err)
				v, err := s.Scope(testDomain())
				must(t, err)
				ctx, end := endingContext(t, mode, storeStepDeadline)
				var ended atomic.Bool
				s.boundary = func(name string) error {
					if name == step {
						end()
						ended.Store(true)
					}
					return nil
				}
				release, err := v.Claim(ctx, [32]byte{7}, "dkg", []byte("intent"))
				s.boundary = nil
				if !ended.Load() {
					t.Fatalf("the claim never reached the %s step", step)
				}
				if errors.Is(err, ErrQuarantined) {
					t.Fatalf("DF-CANCEL-1: a claim whose context ended at %s quarantined the store: %v", step, err)
				}
				if release != nil {
					release()
				}
				requireInstalledHead(t, s, 1, "DF-CANCEL-1")

				next, err := v.Claim(context.Background(), [32]byte{8}, "sign", []byte("intent"))
				if err != nil {
					t.Fatalf("DF-CANCEL-1: the next claim on the same handle failed: %v", err)
				}
				next()
				requireInstalledHead(t, s, 2, "DF-CANCEL-1 next claim")
				if _, err = v.Claim(context.Background(), [32]byte{7}, "sign", []byte("intent")); !errors.Is(err, ErrClaimed) {
					t.Fatalf("DF-CANCEL-1: the cancelled claim is not durable on the same handle: %v", err)
				}
				must(t, s.Close())

				s, err = Open(context.Background(), c)
				if err != nil {
					t.Fatalf("DF-CANCEL-1: reopening after the cancelled claim failed: %v", err)
				}
				defer s.Close()
				requireInstalledHead(t, s, 2, "DF-CANCEL-1 reopen")
				v, err = s.Scope(testDomain())
				must(t, err)
				if _, err = v.Claim(context.Background(), [32]byte{7}, "sign", []byte("intent")); !errors.Is(err, ErrClaimed) {
					t.Fatalf("DF-CANCEL-1: the reopened store lost the cancelled claim: %v", err)
				}
				if _, err = v.Claim(context.Background(), [32]byte{9}, "dkg", []byte("intent")); !errors.Is(err, ErrDKGPending) {
					t.Fatalf("DF-CANCEL-1: the reopened store lost the cancelled DKG claim: %v", err)
				}
			})
		}
	}
}

// TestDeveloperFenceStoreSurvivesCancelBeforeHead: a claim whose context ends
// after the store's own cancellation check and before the store reads the
// developer fence's head does not quarantine the store. The store poisons
// itself on any Head error, so the developer fence's Head must not fail just
// because the caller's context ended.
func TestDeveloperFenceStoreSurvivesCancelBeforeHead(t *testing.T) {
	c, _ := devStoreConfig(t)
	s, err := Open(context.Background(), c)
	must(t, err)
	defer s.Close()
	v, err := s.Scope(testDomain())
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spy := &headSpy{FenceLease: s.fence, target: ctx, cancel: cancel}
	s.fence = spy
	release, err := v.Claim(ctx, [32]byte{7}, "dkg", []byte("intent"))
	if !spy.fired.Load() {
		t.Fatal("the store never read the fence head with the claim's context")
	}
	if errors.Is(err, ErrQuarantined) {
		t.Fatalf("DF-CANCEL-3: a cancellation between the store's check and its fence read quarantined the store: %v", err)
	}
	if release != nil {
		release()
	}
	next, err := v.Claim(context.Background(), [32]byte{8}, "sign", []byte("intent"))
	if err != nil {
		t.Fatalf("DF-CANCEL-3: the next claim on the same handle failed: %v", err)
	}
	next()
	requireInstalledHead(t, s, 2, "DF-CANCEL-3")
}

// headSpy is a TEST wrapper around the real developer lease. The first time
// the store reads the head with the target context (in Store.check, after the
// store's own ctx.Err() check passed), it cancels that real context, then
// delegates. This places the cancellation between the store's check and its
// head read without depending on how often the store consults the context.
// Every other call is delegated unchanged.
type headSpy struct {
	FenceLease
	target context.Context
	cancel context.CancelFunc
	fired  atomic.Bool
}

func (h *headSpy) Head(ctx context.Context) (Point, error) {
	if ctx == h.target && h.fired.CompareAndSwap(false, true) {
		h.cancel()
	}
	return h.FenceLease.Head(ctx)
}

// TestDeveloperFenceAdvanceIgnoresCancellation: on the developer fence itself,
// Advance with an already-ended context installs a valid successor; Head (with
// a live or an ended context) then returns it, and it survives a new lease on
// the same file.
func TestDeveloperFenceAdvanceIgnoresCancellation(t *testing.T) {
	for _, mode := range cancelModes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			path := provisionFence(t, t.TempDir())
			id := [32]byte{0x4d, 0x34}
			lease, err := developerFenceAt(t, path).Acquire(context.Background(), id)
			must(t, err)
			ended := endedContext(t, mode)

			one := fencePoint(1, "one")
			if err = lease.Advance(ended, Point{}, one); err != nil {
				t.Fatalf("DF-CANCEL-2: Advance with an ended context did not install a valid successor: %v", err)
			}
			head, err := lease.Head(context.Background())
			if err != nil || head != one {
				t.Fatalf("DF-CANCEL-2: after Advance with an ended context the head is %+v (err %v), want the successor", head, err)
			}
			two := fencePoint(2, "two")
			if err = lease.Advance(ended, one, two); err != nil {
				t.Fatalf("DF-CANCEL-2: a second Advance with an ended context failed: %v", err)
			}
			head, err = lease.Head(ended)
			if err != nil || head != two {
				t.Fatalf("DF-CANCEL-3: Head with an ended context returned %+v (err %v), want the installed head", head, err)
			}
			must(t, lease.Close())

			again, err := developerFenceAt(t, path).Acquire(context.Background(), id)
			must(t, err)
			defer again.Close()
			head, err = again.Head(context.Background())
			if err != nil || head != two {
				t.Fatalf("DF-CANCEL-2: the head installed under an ended context is not durable: %+v (err %v)", head, err)
			}
		})
	}
}

// TestDeveloperFenceCancellationKeepsCompareAndInstall: an ended context does
// not weaken the developer fence. A stale before, or an after that is not the
// successor, is still refused as stale and leaves the head unchanged, and a
// closed lease still refuses.
func TestDeveloperFenceCancellationKeepsCompareAndInstall(t *testing.T) {
	for _, mode := range cancelModes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			path := provisionFence(t, t.TempDir())
			lease, err := developerFenceAt(t, path).Acquire(context.Background(), [32]byte{0x4d, 0x35})
			must(t, err)
			one := fencePoint(1, "one")
			must(t, lease.Advance(context.Background(), Point{}, one))
			ended := endedContext(t, mode)

			refused := []struct {
				name          string
				before, after Point
			}{
				{"stale before", Point{}, fencePoint(1, "other")},
				{"before with wrong hash", fencePoint(1, "forged"), fencePoint(2, "two")},
				{"sequence skip", one, fencePoint(3, "three")},
				{"same sequence", one, fencePoint(1, "again")},
			}
			for _, r := range refused {
				name := r.name
				err = lease.Advance(ended, r.before, r.after)
				if err == nil {
					t.Fatalf("DF-CANCEL-2: %s was accepted under an ended context", name)
				}
				head, herr := lease.Head(context.Background())
				if herr != nil || head != one {
					t.Fatalf("DF-CANCEL-2: %s under an ended context changed the head to %+v (err %v)", name, head, herr)
				}
				if !errors.Is(err, ErrFenceStale) {
					t.Fatalf("DF-CANCEL-2: %s under an ended context was refused for the context, not by compare-and-install: %v", name, err)
				}
			}

			must(t, lease.Close())
			if err = lease.Advance(ended, one, fencePoint(2, "two")); !errors.Is(err, ErrFenceClosed) {
				t.Fatalf("DF-CANCEL-2: a closed lease did not refuse Advance under an ended context: %v", err)
			}
			if _, err = lease.Head(ended); !errors.Is(err, ErrFenceClosed) {
				t.Fatalf("DF-CANCEL-3: a closed lease did not refuse Head under an ended context: %v", err)
			}
		})
	}
}

// TestDeveloperFenceCancellationKeepsOutsideChangeCheck: an ended context
// does not switch off the developer fence's reread of its file. When the file
// was rolled back outside the lease (a TEST rollback: an older valid fence
// file written back over the current one), Head and Advance under an ended
// context still refuse it as changed outside the lease (errFenceFile), and
// Advance installs nothing.
func TestDeveloperFenceCancellationKeepsOutsideChangeCheck(t *testing.T) {
	for _, mode := range cancelModes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			path := provisionFence(t, t.TempDir())
			lease, err := developerFenceAt(t, path).Acquire(context.Background(), [32]byte{0x4d, 0x37})
			must(t, err)
			defer lease.Close()
			one, two := fencePoint(1, "one"), fencePoint(2, "two")
			must(t, lease.Advance(context.Background(), Point{}, one))
			older, err := os.ReadFile(path)
			must(t, err)
			must(t, lease.Advance(context.Background(), one, two))
			must(t, os.WriteFile(path, older, 0600))
			ended := endedContext(t, mode)

			if head, err := lease.Head(ended); !errors.Is(err, errFenceFile) {
				t.Fatalf("DF-CANCEL-2: Head under an ended context did not refuse a fence file rolled back outside the lease: %+v, %v", head, err)
			}
			if err = lease.Advance(ended, two, fencePoint(3, "three")); !errors.Is(err, errFenceFile) {
				t.Fatalf("DF-CANCEL-2: Advance under an ended context did not refuse a fence file rolled back outside the lease: %v", err)
			}
			after, err := os.ReadFile(path)
			must(t, err)
			if string(after) != string(older) {
				t.Fatal("DF-CANCEL-2: Advance under an ended context wrote over a fence file rolled back outside the lease")
			}
		})
	}
}

// TestDeveloperFenceAcquireRefusesEndedContext pins the Acquire choice:
// Acquire refuses an already-ended context up front, with the context's error,
// before it binds or locks anything. The provisioned file stays empty and
// unlocked, and the store's Open returns the context error, not quarantine, so
// a later Open works.
func TestDeveloperFenceAcquireRefusesEndedContext(t *testing.T) {
	for _, mode := range cancelModes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			want := context.Canceled
			if mode == "deadline" {
				want = context.DeadlineExceeded
			}
			path := provisionFence(t, t.TempDir())
			f := developerFenceAt(t, path)
			ended := endedContext(t, mode)
			if lease, err := f.Acquire(ended, [32]byte{0x4d, 0x36}); !errors.Is(err, want) {
				if lease != nil {
					lease.Close()
				}
				t.Fatalf("DF-CANCEL-4: Acquire with an ended context returned %v, want the context's error", err)
			}
			st, err := os.Stat(path)
			must(t, err)
			if st.Size() != 0 {
				t.Fatalf("DF-CANCEL-4: a refused Acquire bound the provisioned fence file (%d bytes)", st.Size())
			}
			lease, err := f.Acquire(context.Background(), [32]byte{0x4d, 0x36})
			if err != nil {
				t.Fatalf("DF-CANCEL-4: a refused Acquire left the fence unusable: %v", err)
			}
			must(t, lease.Close())

			c, _ := devStoreConfig(t)
			if s, err := Open(endedContext(t, mode), c); !errors.Is(err, want) || errors.Is(err, ErrQuarantined) {
				if s != nil {
					s.Close()
				}
				t.Fatalf("DF-CANCEL-4: Open with an ended context returned %v, want the context's error", err)
			}
			s, err := Open(context.Background(), c)
			if err != nil {
				t.Fatalf("DF-CANCEL-4: Open after a cancelled Open failed: %v", err)
			}
			must(t, s.Close())
		})
	}
}
