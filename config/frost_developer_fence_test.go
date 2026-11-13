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
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"

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

// Invariant (in-process exclusion; the lease is non-expiring until Close):
// while a lease is held, a second Acquire for the same storage ID in the same
// process is refused, through the same fence and through a freshly loaded
// configuration on the same files. After Close it succeeds. Exclusion within
// one process is a deliberate extra requirement beyond the Fence contract
// (which speaks of other processes), consistent with store.Open's flock on
// its owner lock. Other processes are covered by
// TestDeveloperFenceCrossProcess.
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

// Invariant (decision F6, coordinator 2026-11-13): the disk fence binds the
// storage ID. A fence file already used (advanced) under one storage ID is
// refused to Acquire under another; the owning ID still acquires it, at the
// head it installed.
func TestDeveloperFenceBoundToStorageID(t *testing.T) {
	d := newDevFrost(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fence := loadFence(t, d)
	lease := acquire(t, fence)
	p1 := point(1, "one")
	if err := lease.Advance(ctx, store.Point{}, p1); err != nil {
		t.Fatalf("M2-6: Advance failed: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("M2-6: Close failed: %v", err)
	}
	other := [32]byte{0x0b, 0x1d}
	for name, f := range map[string]store.Fence{"same fence": fence, "reloaded configuration": loadFence(t, d)} {
		if l, err := f.Acquire(ctx, other); err == nil {
			l.Close()
			t.Fatalf("M2-6: a fence file used by one storage ID was acquired under another (%s)", name)
		}
	}
	owner := acquire(t, loadFence(t, d))
	defer owner.Close()
	if got := head(t, owner); got != p1 {
		t.Fatalf("M2-6: the owning storage ID's head is %+v after a refused foreign Acquire, want %+v", got, p1)
	}
}

const (
	fenceHolderEnv   = "M2_FENCE_HOLDER"
	fenceHolderReady = "M2-FENCE-HOLDER-READY"
)

// fenceHolderPoint is the point the holder process installs.
var fenceHolderPoint = point(1, "holder")

// Invariant (review F2; Fence contract: Acquire excludes every other
// process, Advance durably installs): while another process holds the
// developer disk fence, this process's Acquire on the same fence file is
// refused; after that process is killed (SIGKILL, no Close), this process
// acquires the fence and its head is the point the killed holder installed.
// The holder is this test binary re-run as TestDeveloperFenceHolder.
func TestDeveloperFenceCrossProcess(t *testing.T) {
	d := newDevFrost(t)
	own := loadFence(t, d)
	path := filepath.Join(d.dir, "holder.toml")
	if err := os.WriteFile(path, []byte(d.toml(true, nil)), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDeveloperFenceHolder$", "-test.count=1")
	cmd.Env = append(os.Environ(), fenceHolderEnv+"="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			if scanner.Text() == fenceHolderReady {
				close(ready)
				break
			}
		}
		_, _ = io.Copy(io.Discard, out)
	}()
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })

	select {
	case <-ready:
	case <-exited:
		t.Fatalf("M2-6: the holder process exited before holding the fence: %s", tail(stderr.String()))
	case <-time.After(8 * time.Second):
		t.Fatal("M2-6: the holder process did not hold the fence within 8 s")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if lease, err := own.Acquire(ctx, devStorageID); err == nil {
		lease.Close()
		t.Fatal("M2-6: Acquire succeeded while another process holds the fence")
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: the killed holder process did not exit within 5 s")
	}
	lease := acquire(t, own)
	defer lease.Close()
	if got := head(t, lease); got != fenceHolderPoint {
		t.Fatalf("M2-6: head after the holder was killed is %+v, want its installed %+v", got, fenceHolderPoint)
	}
}

func tail(s string) string {
	if len(s) > 400 {
		return "..." + s[len(s)-400:]
	}
	return s
}

// TestDeveloperFenceHolder is the subprocess entry point of
// TestDeveloperFenceCrossProcess (test/suites/e2e.txt helper): it loads the
// developer configuration named by M2_FENCE_HOLDER, acquires the fence,
// advances it to fenceHolderPoint, reports ready and holds the lease without
// closing it until killed (bounded at 20 s).
func TestDeveloperFenceHolder(t *testing.T) {
	path := os.Getenv(fenceHolderEnv)
	if path == "" {
		t.Skip("subprocess helper of TestDeveloperFenceCrossProcess")
	}
	viper.Reset()
	c := &Config{}
	if err := c.ReadConfig(path, networkFlags(t, "--developer"), StartCmdCategories...); err != nil {
		t.Fatalf("holder: configuration refused: %v", err)
	}
	if c.Tbtc.Frost.Store.Fence == nil {
		t.Fatal("holder: no fence configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lease, err := c.Tbtc.Frost.Store.Fence.Acquire(ctx, devStorageID)
	if err != nil {
		t.Fatalf("holder: Acquire failed: %v", err)
	}
	if err := lease.Advance(ctx, store.Point{}, fenceHolderPoint); err != nil {
		t.Fatalf("holder: Advance failed: %v", err)
	}
	fmt.Println(fenceHolderReady)
	time.Sleep(20 * time.Second)
	os.Exit(3) // never Close: the lease ends only with the process
}
