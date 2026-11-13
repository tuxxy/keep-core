package config

// Chaosnet M2 acceptance item 6 (tux, 2026-11-13): the developer disk fence
// configured by [tbtc.frost] FenceFile. KC-SF-V3 K-02 "Safe state/recovery"
// allows an exclusive-host/no-snapshot operational assumption when no
// independent fencing authority exists; this fence is that assumption on
// the developer network only, not a production fence. The tests reach the
// fence only through the loaded configuration (c.Tbtc.Frost.Store.Fence) and
// the store.Fence contract (pkg/frost/store/fence.go).
//
// Fixture and pinned settings: frost_developer_fixture_test.go.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// devStorageID is a TEST storage ID (the node derives its own, item 5).
var devStorageID = [32]byte{0x51, 0x0e, 0x1d}

func point(sequence uint64, label string) store.Point {
	return store.Point{Sequence: sequence, Digest: sha256.Sum256([]byte(label))}
}

// loadFence loads d's developer configuration and returns its fence.
func loadFence(t *testing.T, d *devFrost) store.Fence {
	t.Helper()
	c, err := readDev(t, d, d.toml(true, nil), "--developer")
	if err != nil {
		t.Fatalf("M2-6: the developer FROST configuration with a fence file was refused: %v", err)
	}
	if c.Tbtc.Frost.Store.Fence == nil {
		t.Fatal("M2-6: the developer FROST configuration has no fence")
	}
	return c.Tbtc.Frost.Store.Fence
}

func acquire(t *testing.T, f store.Fence) store.FenceLease {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lease, err := f.Acquire(ctx, devStorageID)
	if err != nil {
		t.Fatalf("M2-6: Acquire on a provisioned, free fence failed: %v", err)
	}
	return lease
}

func head(t *testing.T, l store.FenceLease) store.Point {
	t.Helper()
	p, err := l.Head(context.Background())
	if err != nil {
		t.Fatalf("M2-6: Head failed: %v", err)
	}
	return p
}

// Invariant: a provisioned fence file (rendered empty) is the zero head, a
// newly provisioned store.
func TestDeveloperFenceProvisionedHeadIsZero(t *testing.T) {
	lease := acquire(t, loadFence(t, newDevFrost(t)))
	defer lease.Close()
	if got := head(t, lease); got != (store.Point{}) {
		t.Fatalf("M2-6: a provisioned fence's head is %+v, want the zero point", got)
	}
}

// Invariant (Fence contract: Advance atomically compares before and durably
// installs after): Advance installs only from the current head; a stale or
// forged before (older point, same sequence with another digest, a future
// point) is refused and leaves the head unchanged.
func TestDeveloperFenceCompareAndInstall(t *testing.T) {
	lease := acquire(t, loadFence(t, newDevFrost(t)))
	defer lease.Close()
	ctx := context.Background()
	p1, p2 := point(1, "one"), point(2, "two")
	if err := lease.Advance(ctx, store.Point{}, p1); err != nil {
		t.Fatalf("M2-6: Advance from the current (zero) head failed: %v", err)
	}
	if got := head(t, lease); got != p1 {
		t.Fatalf("M2-6: head after Advance is %+v, want %+v", got, p1)
	}
	for name, stale := range map[string]store.Point{
		"older point":           {},
		"same sequence, forged": point(1, "forged"),
		"future point":          p2,
		"digest only, zero seq": {Digest: p1.Digest},
	} {
		if err := lease.Advance(ctx, stale, point(stale.Sequence+1, "stale-"+name)); err == nil {
			t.Fatalf("M2-6: Advance from a stale before (%s) was accepted", name)
		}
		if got := head(t, lease); got != p1 {
			t.Fatalf("M2-6: a refused Advance (%s) changed the head to %+v", name, got)
		}
	}
	if err := lease.Advance(ctx, p1, p2); err != nil {
		t.Fatalf("M2-6: Advance from the current head failed after refusals: %v", err)
	}
	if got := head(t, lease); got != p2 {
		t.Fatalf("M2-6: head is %+v, want %+v", got, p2)
	}
}

// Invariant: installed heads are durable across close and a fresh load of
// the configuration (a restarted process): Head after reopen equals the
// last installed point, and the old point is stale there.
func TestDeveloperFenceDurableAcrossReopen(t *testing.T) {
	d := newDevFrost(t)
	ctx := context.Background()
	p1, p2 := point(1, "one"), point(2, "two")
	lease := acquire(t, loadFence(t, d))
	if err := lease.Advance(ctx, store.Point{}, p1); err != nil {
		t.Fatalf("M2-6: Advance failed: %v", err)
	}
	if err := lease.Advance(ctx, p1, p2); err != nil {
		t.Fatalf("M2-6: Advance failed: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("M2-6: Close failed: %v", err)
	}
	reopened := acquire(t, loadFence(t, d))
	defer reopened.Close()
	if got := head(t, reopened); got != p2 {
		t.Fatalf("M2-6: head after reopen is %+v, want the last installed %+v", got, p2)
	}
	if err := reopened.Advance(ctx, p1, point(2, "replayed")); err == nil {
		t.Fatal("M2-6: after reopen, Advance from an older head was accepted")
	}
	if got := head(t, reopened); got != p2 {
		t.Fatalf("M2-6: a refused Advance after reopen changed the head to %+v", got)
	}
}

// Invariant (Fence contract: Acquire excludes every other process; the lease
// is non-expiring until Close): while a lease is held, a second Acquire for
// the same storage ID is refused, through the same fence and through a
// freshly loaded configuration (another process on the same files). After
// Close it succeeds.
func TestDeveloperFenceExclusiveWhileHeld(t *testing.T) {
	d := newDevFrost(t)
	first := loadFence(t, d)
	lease := acquire(t, first)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if second, err := first.Acquire(ctx, devStorageID); err == nil {
		second.Close()
		lease.Close()
		t.Fatal("M2-6: a second Acquire through the same fence succeeded while the lease is held")
	}
	other := loadFence(t, d)
	if second, err := other.Acquire(ctx, devStorageID); err == nil {
		second.Close()
		lease.Close()
		t.Fatal("M2-6: a second Acquire from another loaded configuration succeeded while the lease is held")
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("M2-6: Close failed: %v", err)
	}
	again := acquire(t, other)
	again.Close()
}

// Invariant (Fence contract: an old owner must not continue after Close): a
// closed lease cannot advance the fence, and the head stays as it was.
func TestDeveloperFenceClosedLeaseCannotAdvance(t *testing.T) {
	d := newDevFrost(t)
	ctx := context.Background()
	lease := acquire(t, loadFence(t, d))
	p1 := point(1, "one")
	if err := lease.Advance(ctx, store.Point{}, p1); err != nil {
		t.Fatalf("M2-6: Advance failed: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("M2-6: Close failed: %v", err)
	}
	if err := lease.Advance(ctx, p1, point(2, "after close")); err == nil {
		t.Fatal("M2-6: a closed lease advanced the fence")
	}
	reopened := acquire(t, loadFence(t, d))
	defer reopened.Close()
	if got := head(t, reopened); got != p1 {
		t.Fatalf("M2-6: head after a closed lease's Advance is %+v, want %+v", got, p1)
	}
}

// Invariant (Fence contract: never silently create a new identity after data
// loss): a missing fence file is refused, by the configuration or by
// Acquire/Head, and is not created as a fresh zero head. The setup checks
// that the same configuration with the file present loads and acquires, so
// the refusal is the missing file's.
func TestDeveloperFenceMissingFileRefused(t *testing.T) {
	present := newDevFrost(t)
	acquire(t, loadFence(t, present)).Close()

	d := newDevFrost(t)
	if err := os.Remove(d.fence); err != nil {
		t.Fatal(err)
	}
	c, err := readDev(t, d, d.toml(true, nil), "--developer")
	if err == nil {
		if c.Tbtc.Frost.Store.Fence == nil {
			t.Fatal("M2-6: a configuration with a missing fence file loaded with no fence")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lease, aerr := c.Tbtc.Frost.Store.Fence.Acquire(ctx, devStorageID)
		if aerr == nil {
			p, herr := lease.Head(ctx)
			lease.Close()
			if herr == nil {
				t.Fatalf("M2-6: a missing fence file was taken as head %+v", p)
			}
		}
	}
	if _, serr := os.Stat(d.fence); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("M2-6: a missing fence file was created (stat: %v)", serr)
	}
}

// Invariant: the FROST store opened with the developer configuration (its
// keystore, key and disk fence) keeps a committed record across close and a
// freshly loaded configuration, and its commit advanced the disk fence.
func TestDeveloperStoreSurvivesReopen(t *testing.T) {
	d := newDevFrost(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	domain := frost.Domain{Network: "developer", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 7}
	// FIXTURE: an opaque test record, not a key, share, nonce or worker record.
	write := frost.Write{Kind: "candidate-record", Seat: 1, ID: [32]byte{8}, Payload: []byte("M2 test record: opaque fixture bytes")}
	open := func() *store.Store {
		t.Helper()
		c, err := readDev(t, d, d.toml(true, nil), "--developer")
		if err != nil {
			t.Fatalf("M2-6: the developer FROST configuration was refused: %v", err)
		}
		sc := c.Tbtc.Frost.Store
		sc.StorageID = devStorageID
		s, err := store.Open(ctx, sc)
		if err != nil {
			t.Fatalf("M2-6: store.Open with the developer configuration failed: %v", err)
		}
		return s
	}
	s := open()
	scoped, err := s.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := scoped.Put(ctx, write); err != nil || r != frost.Durable {
		t.Fatalf("M2-6: Put on the developer store: result %v, error %v", r, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("M2-6: store Close failed: %v", err)
	}

	lease := acquire(t, loadFence(t, d))
	advanced := head(t, lease)
	lease.Close()
	if advanced.Sequence == 0 {
		t.Fatal("M2-6: the store's commit did not advance the disk fence")
	}

	s = open()
	defer s.Close()
	scoped, err = s.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scoped.Read(ctx, write.ID)
	if err != nil {
		t.Fatalf("M2-6: Read after reopen failed: %v", err)
	}
	if !bytes.Equal(got, write.Payload) {
		t.Fatal("M2-6: the record read after reopen differs from the one committed")
	}
}
