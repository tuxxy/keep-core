package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
)

// diskFence is a TEST authority on a separate path outside the copied keystore.
// It is not a production service or a defense against whole-host rollback.
type diskFence struct{ path string }
type diskLease struct {
	path string
	lock *os.File
}

func (f diskFence) Acquire(_ context.Context, _ [32]byte) (FenceLease, error) {
	lock, e := os.OpenFile(f.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = lockFile(lock); e != nil {
		lock.Close()
		return nil, ErrBusy
	}
	return &diskLease{f.path, lock}, nil
}
func (f *diskLease) Head(context.Context) (Point, error) {
	var p Point
	b, e := os.ReadFile(f.path)
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(b, &p)
	return p, e
}
func (f *diskLease) Advance(ctx context.Context, before, after Point) error {
	p, e := f.Head(ctx)
	if e != nil {
		return e
	}
	if p != before || after.Sequence != before.Sequence+1 {
		return ErrQuarantined
	}
	b, _ := json.Marshal(after)
	tmp := f.path + ".tmp"
	out, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = out.Write(b); e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(tmp, f.path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(f.path))
}
func (f *diskLease) Close() error { return f.lock.Close() }
func testDomain() frost.Domain {
	return frost.Domain{Network: "local-test", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 11}
}
func configAt(root, authority string) Config {
	return Config{Keystore: root, StorageID: [32]byte{3}, Key: [32]byte{4}, Fence: diskFence{authority}}
}
func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "external-fence")
	b, _ := json.Marshal(Point{})
	must(t, os.WriteFile(p, b, 0600))
	return configAt(filepath.Join(dir, "keystore"), p)
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func openTest(t *testing.T, c Config) (*Store, *Scoped) {
	t.Helper()
	s, e := Open(context.Background(), c)
	must(t, e)
	v, e := s.Scope(testDomain())
	must(t, e)
	t.Cleanup(func() { must(t, s.Close()) })
	return s, v
}
func writeTest() frost.Write {
	return frost.Write{Kind: "candidate-record", Seat: 1, ID: [32]byte{8}, Payload: []byte("opaque test secret; never log payloads")}
}

func TestDurableCompareInsert(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	w := writeTest()
	r, e := v.Put(ctx, w)
	must(t, e)
	if r != frost.Durable {
		t.Fatal("missing durable result")
	}
	var steps []string
	s.boundary = func(p string) error { steps = append(steps, p); return nil }
	r, e = v.Put(ctx, w)
	must(t, e)
	if r != frost.Identical || strings.Join(steps, ",") != "before-identical-sync,after-identical-sync" {
		t.Fatal("identical result was not synced")
	}
	s.boundary = nil
	different := w
	different.Payload = []byte("different")
	r, e = v.Put(ctx, different)
	must(t, e)
	if r != frost.Conflict {
		t.Fatal("overwrote immutable data")
	}
	w.Kind = "lock"
	w.ID = [32]byte{9}
	w.Slot = &frost.LockSlot{Attempt: [32]byte{7}, Kind: "round-one"}
	r, e = v.Put(ctx, w)
	must(t, e)
	if r != frost.Durable {
		t.Fatal("lock not durable")
	}
	different = w
	different.ID = [32]byte{10}
	r, e = v.Put(ctx, different)
	must(t, e)
	if r != frost.Conflict {
		t.Fatal("slot was indexed by digest rather than obligation")
	}
	lock, e := v.Read(ctx, w.ID)
	must(t, e)
	if !bytes.Equal(lock, w.Payload) {
		t.Fatal("wrong lock")
	}
	raw, e := os.ReadFile(s.filename(s.point))
	must(t, e)
	if bytes.Contains(raw, w.Payload) {
		t.Fatal("plaintext reached disk")
	}
	st, e := os.Stat(s.filename(s.point))
	must(t, e)
	if st.Mode().Perm() != 0600 {
		t.Fatal("wrong file mode")
	}
	must(t, s.Close())
	_, v = openTest(t, c)
	got, e := v.Read(ctx, writeTest().ID)
	must(t, e)
	if !bytes.Equal(got, writeTest().Payload) {
		t.Fatal("reopen lost data")
	}
}
func TestRecordContextAndCiphertext(t *testing.T) {
	t.Run("swap", func(t *testing.T) {
		s, v := openTest(t, testConfig(t))
		a := writeTest()
		b := a
		b.ID = [32]byte{12}
		b.Seat = 2
		_, e := v.Put(context.Background(), a)
		must(t, e)
		_, e = v.Put(context.Background(), b)
		must(t, e)
		ar, br := s.state.Records[v.recordKey(a.ID)], s.state.Records[v.recordKey(b.ID)]
		ar.Ciphertext = br.Ciphertext
		s.state.Records[v.recordKey(a.ID)] = ar
		if _, e = v.Read(context.Background(), a.ID); !errors.Is(e, ErrQuarantined) {
			t.Fatal("swapped record was accepted")
		}
	})
	t.Run("domain", func(t *testing.T) {
		s, v := openTest(t, testConfig(t))
		_, e := v.Put(context.Background(), writeTest())
		must(t, e)
		d := testDomain()
		d.Registry[0]++
		other, e := s.Scope(d)
		must(t, e)
		if _, e = other.Read(context.Background(), writeTest().ID); !errors.Is(e, ErrMissing) {
			t.Fatal("cross-registry record read")
		}
	})
	t.Run("tamper", func(t *testing.T) {
		c := testConfig(t)
		s, v := openTest(t, c)
		_, e := v.Put(context.Background(), writeTest())
		must(t, e)
		name := s.filename(s.point)
		must(t, s.Close())
		raw, e := os.ReadFile(name)
		must(t, e)
		raw[len(raw)-1] ^= 1
		must(t, os.WriteFile(name, raw, 0600))
		if _, e = Open(context.Background(), c); !errors.Is(e, ErrQuarantined) {
			t.Fatal("tampered snapshot accepted")
		}
	})
	t.Run("wrong-key", func(t *testing.T) {
		c := testConfig(t)
		s, v := openTest(t, c)
		_, e := v.Put(context.Background(), writeTest())
		must(t, e)
		must(t, s.Close())
		c.Key[0]++
		if _, e = Open(context.Background(), c); !errors.Is(e, ErrQuarantined) {
			t.Fatal("wrong key accepted")
		}
	})
}
func TestExclusiveAttemptOwner(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	if _, e := Open(ctx, c); !errors.Is(e, ErrBusy) {
		t.Fatal("second local owner accepted")
	}
	cmd := helper(t, c, "open", "")
	if e := cmd.Run(); exitCode(e) != 23 {
		t.Fatalf("second process was not excluded: %v", e)
	}
	release, e := v.Claim(ctx, [32]byte{1}, "sign", []byte("exact signed intent"))
	must(t, e)
	if e = s.Close(); !errors.Is(e, ErrBusy) {
		t.Fatal("closed lease while worker could run")
	}
	if _, e = v.Claim(ctx, [32]byte{1}, "sign", []byte("same")); !errors.Is(e, ErrClaimed) {
		t.Fatal("duplicate claim")
	}
	release()
	release()
	must(t, s.Close())
	s, v = openTest(t, c)
	if _, e = v.Claim(ctx, [32]byte{1}, "sign", []byte("different")); !errors.Is(e, ErrClaimed) {
		t.Fatal("claimed attempt resumed after restart")
	}
	release, e = v.Claim(ctx, [32]byte{2}, "sign", []byte("fresh agreed attempt"))
	must(t, e)
	release()
	// A clone has a different local lock, but the same external authority rejects it.
	cloneRoot := filepath.Join(filepath.Dir(c.Keystore), "clone")
	must(t, os.Mkdir(cloneRoot, 0700))
	cc := c
	cc.Keystore = cloneRoot
	if _, e = Open(ctx, cc); !errors.Is(e, ErrBusy) {
		t.Fatal("cloned owner accepted")
	}
}
func TestSnapshotRestoreQuarantine(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	_, e := v.Put(ctx, writeTest())
	must(t, e)
	release, e := v.Claim(ctx, [32]byte{22}, "sign", []byte("intent after snapshot"))
	must(t, e)
	release()
	latest := s.filename(s.point)
	must(t, s.Close())
	must(t, os.Remove(latest)) // restore the pre-claim keystore while the authority survives
	if _, e = Open(ctx, c); !errors.Is(e, ErrQuarantined) {
		t.Fatal("stale restore resumed")
	}
}

var crashBoundaries = []string{"before-claim", "before-write", "before-file-sync", "after-file-sync", "before-install", "after-install", "before-directory-sync", "after-directory-sync", "before-fence", "after-fence", "before-acknowledgement", "after-claim", "after-return"}

func committedAt(p string) bool {
	return p == "after-fence" || p == "before-acknowledgement" || p == "after-claim" || p == "after-return"
}
func TestCrashEveryStoreBoundary(t *testing.T) {
	for _, point := range crashBoundaries {
		t.Run(point, func(t *testing.T) {
			c := testConfig(t)
			cmd := helper(t, c, "crash", point)
			output, e := cmd.CombinedOutput()
			if exitCode(e) != 73 {
				t.Fatalf("crash injection did not run: %v %s", e, output)
			}
			switch point {
			case "after-install", "before-directory-sync", "after-directory-sync", "before-fence":
				// An installed journal with a zero fence is ambiguous. It could
				// also be a reset authority, so the store must fail closed.
				if opened, err := Open(context.Background(), c); opened != nil {
					must(t, opened.Close())
					t.Fatal("unfenced journal reopened as empty state")
				} else if !errors.Is(err, ErrQuarantined) {
					t.Fatalf("unfenced journal did not quarantine: %v", err)
				}
				return
			}
			_, v := openTest(t, c)
			release, e := v.Claim(context.Background(), [32]byte{31}, "sign", []byte("same original intent"))
			if committedAt(point) {
				if !errors.Is(e, ErrClaimed) {
					t.Fatal("committed claim was recycled")
				}
			} else {
				must(t, e)
				release()
			}
			release, e = v.Claim(context.Background(), [32]byte{32}, "sign", []byte("fresh agreed intent"))
			must(t, e)
			release()
		})
	}
}
func TestIOUncertaintyQuarantines(t *testing.T) {
	for _, point := range crashBoundaries[:len(crashBoundaries)-1] {
		t.Run(point, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			s.boundary = func(p string) error {
				if p == point {
					return errors.New("injected I/O uncertainty")
				}
				return nil
			}
			if _, e := v.Claim(context.Background(), [32]byte{31}, "sign", []byte("intent")); !errors.Is(e, ErrQuarantined) {
				t.Fatal("uncertain I/O acknowledged")
			}
			if _, e := v.Claim(context.Background(), [32]byte{32}, "sign", []byte("fresh")); !errors.Is(e, ErrQuarantined) {
				t.Fatal("uncertain owner continued")
			}
		})
	}
}
func helper(t *testing.T, c Config, action, point string) *exec.Cmd {
	t.Helper()
	exe, e := os.Executable()
	must(t, e)
	cmd := exec.Command(exe, "-test.run=^TestStoreProcessHelper$")
	cmd.Env = append(os.Environ(), "FROST_STORE_HELPER="+action, "FROST_STORE_ROOT="+c.Keystore, "FROST_STORE_FENCE="+c.Fence.(diskFence).path, "FROST_STORE_BOUNDARY="+point)
	return cmd
}
func exitCode(e error) int {
	var x *exec.ExitError
	if errors.As(e, &x) {
		return x.ExitCode()
	}
	if e == nil {
		return 0
	}
	return -1
}
func TestStoreProcessHelper(t *testing.T) {
	action := os.Getenv("FROST_STORE_HELPER")
	if action == "" {
		return
	}
	c := configAt(os.Getenv("FROST_STORE_ROOT"), os.Getenv("FROST_STORE_FENCE"))
	s, e := Open(context.Background(), c)
	if errors.Is(e, ErrBusy) {
		os.Exit(23)
	}
	must(t, e)
	s.boundary = func(p string) error {
		if p == os.Getenv("FROST_STORE_BOUNDARY") {
			os.Exit(73)
		}
		return nil
	}
	v, e := s.Scope(testDomain())
	must(t, e)
	release, e := v.Claim(context.Background(), [32]byte{31}, "sign", []byte("same original intent"))
	must(t, e)
	release()
	if action == "crash" {
		os.Exit(73)
	}
	must(t, s.Close())
}
func TestConcurrentCompareInsert(t *testing.T) {
	_, v := openTest(t, testConfig(t))
	var wg sync.WaitGroup
	results := make(chan frost.PutResult, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := v.Put(context.Background(), writeTest())
			if e != nil {
				t.Error(e)
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	durable, identical := 0, 0
	for r := range results {
		if r == frost.Durable {
			durable++
		}
		if r == frost.Identical {
			identical++
		}
	}
	if durable != 1 || identical != 7 {
		t.Fatal("compare-insert was not atomic")
	}
}
func TestAcceptedKeyJournal(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	request, k := dkgFixture(t, v.Domain())
	release := claimDKG(t, v, [32]byte{31}, request)
	must(t, v.SaveKey(ctx, k))
	must(t, v.SaveKey(ctx, k))
	release()
	must(t, s.Close())
	_, v = openTest(t, c)
	got, e := v.LoadKey(ctx)
	must(t, e)
	if !bytes.Equal(got.LocalReferences[0], k.LocalReferences[0]) {
		t.Fatal("accepted key reference lost")
	}
	k.Candidate.Descriptor[0]++
	if e = v.SaveKey(ctx, k); e == nil {
		t.Fatal("replaced accepted key")
	}
}
func TestCancelledStoreDoesNotClaim(t *testing.T) {
	_, v := openTest(t, testConfig(t))
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if _, e := v.Claim(ctx, [32]byte{1}, "sign", []byte("intent")); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("cancel ignored")
	}
}

func TestPendingDKGCannotBeReplaced(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	release, e := v.Claim(ctx, [32]byte{1}, "dkg", []byte("first DKG"))
	must(t, e)
	if _, e = v.Claim(ctx, [32]byte{2}, "dkg", []byte("competing DKG")); !errors.Is(e, ErrDKGPending) {
		t.Fatal("competing DKG admitted")
	}
	release()
	must(t, s.Close())
	_, v = openTest(t, c)
	if _, e = v.Claim(ctx, [32]byte{3}, "dkg", []byte("replacement after restart")); !errors.Is(e, ErrDKGPending) {
		t.Fatal("pending DKG replaced after restart")
	}
}
func TestIdenticalWriteChecksDurableBytes(t *testing.T) {
	s, v := openTest(t, testConfig(t))
	ctx := context.Background()
	w := writeTest()
	_, e := v.Put(ctx, w)
	must(t, e)
	must(t, os.WriteFile(s.filename(s.point), []byte("corrupted after Open"), 0600))
	if _, e = v.Put(ctx, w); !errors.Is(e, ErrQuarantined) {
		t.Fatal("identical write acknowledged lost data")
	}
}

func TestStorageIdentityCannotResetClaims(t *testing.T) {
	t.Run("empty store identity", func(t *testing.T) {
		c := testConfig(t)
		s, _ := openTest(t, c)
		must(t, s.Close())
		// No claim or record exists, so the zero-head journal-presence guard
		// cannot substitute for the immutable storage-root identity check.
		c.StorageID[0]++
		opened, err := Open(context.Background(), c)
		if opened != nil {
			must(t, opened.Close())
		}
		if opened != nil || !errors.Is(err, ErrQuarantined) {
			t.Fatal("empty storage identity was rebound")
		}
	})
	c := testConfig(t)
	s, v := openTest(t, c)
	release, e := v.Claim(context.Background(), [32]byte{1}, "sign", []byte("intent"))
	must(t, e)
	release()
	must(t, s.Close())
	c.StorageID[0]++
	fresh := filepath.Join(filepath.Dir(c.Keystore), "replacement-authority")
	initial, _ := json.Marshal(Point{})
	must(t, os.WriteFile(fresh, initial, 0600))
	c.Fence = diskFence{fresh}
	if _, e = Open(context.Background(), c); !errors.Is(e, ErrQuarantined) {
		t.Fatal("new storage identity reopened old keystore")
	}
}

func TestDurabilityOperationErrorsPreventAcknowledgement(t *testing.T) {
	for _, operation := range []string{"file-sync", "install", "directory-sync", "identical-file-sync", "identical-directory-sync"} {
		t.Run(operation, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			ctx := context.Background()
			w := writeTest()
			if strings.HasPrefix(operation, "identical-") {
				_, e := v.Put(ctx, w)
				must(t, e)
			}
			calls := 0
			fail := func() error { calls++; return errors.New("injected filesystem operation failure") }
			switch operation {
			case "file-sync", "identical-file-sync":
				s.syncFile = func(*os.File) error { return fail() }
			case "install":
				s.install = func(string, string) error { return fail() }
			case "directory-sync", "identical-directory-sync":
				s.syncDirectory = func(string) error { return fail() }
			}
			result, e := v.Put(ctx, w)
			if !errors.Is(e, ErrQuarantined) || result != 0 || calls != 1 {
				t.Fatal("failed durability operation was skipped or acknowledged")
			}
		})
	}
}
func TestLiveFenceChangeQuarantines(t *testing.T) {
	c := testConfig(t)
	_, v := openTest(t, c)
	ctx := context.Background()
	_, e := v.Put(ctx, writeTest())
	must(t, e)
	b, _ := json.Marshal(Point{})
	must(t, os.WriteFile(c.Fence.(diskFence).path, b, 0600))
	if _, e = v.Read(ctx, writeTest().ID); !errors.Is(e, ErrQuarantined) {
		t.Fatal("changed external fence was ignored")
	}
}

func TestFenceResetQuarantinesExistingJournal(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	signID, dkgID := [32]byte{41}, [32]byte{42}
	for _, claim := range []struct {
		id      [32]byte
		purpose string
	}{{signID, "sign"}, {dkgID, "dkg"}} {
		release, err := v.Claim(ctx, claim.id, claim.purpose, []byte("durable test intent"))
		must(t, err)
		release()
	}
	_, err := v.Put(ctx, writeTest())
	must(t, err)
	must(t, s.Close())

	// Keep the storage identity and every journal intact. Only the independent
	// test authority is reset, as after accidental service reprovisioning.
	root := filepath.Join(c.Keystore, "snowfall")
	files, err := os.ReadDir(root)
	must(t, err)
	before := make(map[string][32]byte)
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(root, file.Name()))
		must(t, err)
		before[file.Name()] = sha256.Sum256(raw)
	}
	if _, ok := before["identity"]; !ok || len(before) < 3 {
		t.Fatal("test requires an existing identity and journals")
	}
	authority := c.Fence.(diskFence).path
	trustedHead, err := os.ReadFile(authority)
	must(t, err)
	zero, err := json.Marshal(Point{})
	must(t, err)
	must(t, os.WriteFile(authority, zero, 0600))
	for i := 0; i < 2; i++ {
		opened, err := Open(ctx, c)
		if opened != nil {
			must(t, opened.Close())
			t.Fatal("reset fence opened existing journal as empty state")
		}
		if !errors.Is(err, ErrQuarantined) {
			t.Fatalf("reset fence must quarantine and release both leases: %v", err)
		}
	}
	files, err = os.ReadDir(root)
	must(t, err)
	if len(files) != len(before) {
		t.Fatal("quarantine changed stored files")
	}
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(root, file.Name()))
		must(t, err)
		if digest, ok := before[file.Name()]; !ok || digest != sha256.Sum256(raw) {
			t.Fatal("quarantine changed a stored file")
		}
	}
	gotHead, err := os.ReadFile(authority)
	must(t, err)
	if !bytes.Equal(gotHead, zero) {
		t.Fatal("quarantine advanced the reset fence")
	}

	// Restore only the known test-authority head. The store must still enforce
	// both tombstones and the DKG reservation, and retain the opaque record.
	must(t, os.WriteFile(authority, trustedHead, 0600))
	_, v = openTest(t, c)
	for _, id := range [][32]byte{signID, dkgID} {
		if _, err := v.Claim(ctx, id, "sign", []byte("reuse")); !errors.Is(err, ErrClaimed) {
			t.Fatal("reset lost a retained attempt tombstone")
		}
	}
	if _, err := v.Claim(ctx, [32]byte{43}, "dkg", []byte("replacement")); !errors.Is(err, ErrDKGPending) {
		t.Fatal("reset lost the DKG reservation")
	}
	raw, err := v.Read(ctx, writeTest().ID)
	must(t, err)
	if !bytes.Equal(raw, writeTest().Payload) {
		t.Fatal("reset lost the opaque record")
	}
}
