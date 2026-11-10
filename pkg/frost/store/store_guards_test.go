package store

// Mutation batch M2 (MUTATION-REPORT-2026-11-10, pkg/frost/store): one case
// per clause of the store's validation guards, and both sides of each bound.
// These guard code that is already correct; each case was shown to fail at its
// named assertion under the listed mutant (kill table in the PR body).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

// faultFence is a TEST fixture: the disk test authority with one injected
// fault. It stands in for an unavailable or failing fencing service.
type faultFence struct {
	diskFence
	acquireErr, headErr, closeErr error
}
type faultLease struct {
	FenceLease
	headErr, closeErr error
}

func (f faultFence) Acquire(ctx context.Context, id [32]byte) (FenceLease, error) {
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	l, e := f.diskFence.Acquire(ctx, id)
	if e != nil {
		return nil, e
	}
	return &faultLease{l, f.headErr, f.closeErr}, nil
}
func (l *faultLease) Head(ctx context.Context) (Point, error) {
	if l.headErr != nil {
		return Point{}, l.headErr
	}
	return l.FenceLease.Head(ctx)
}
func (l *faultLease) Close() error {
	e := l.FenceLease.Close()
	if l.closeErr != nil {
		return l.closeErr
	}
	return e
}

// tryOpen opens c and converts a panic into an error so that a guard that
// lets an invalid configuration through fails at the caller's assertion.
func tryOpen(t *testing.T, ctx context.Context, c Config) (s *Store, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, nil
			t.Fatalf("Open panicked instead of refusing: %v", r)
		}
	}()
	s, err = Open(ctx, c)
	if s != nil {
		t.Cleanup(func() { s.Close() })
	}
	return s, err
}

// Invariant: Open refuses a configuration that lacks any one of a context, a
// fencing authority, an absolute keystore path, a storage ID or a key. Each
// case leaves every other field valid. Rows store.go:95 (4 LO), :96.
func TestOpenRefusesEachMissingConfigField(t *testing.T) {
	cases := map[string]func(*Config, *context.Context){
		"nil-context":       func(_ *Config, ctx *context.Context) { *ctx = nil },
		"nil-fence":         func(c *Config, _ *context.Context) { c.Fence = nil },
		"relative-keystore": func(c *Config, _ *context.Context) { c.Keystore = "relative-keystore" },
		"zero-storage-id":   func(c *Config, _ *context.Context) { c.StorageID = [32]byte{} },
		"zero-key":          func(c *Config, _ *context.Context) { c.Key = [32]byte{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			// A relative keystore resolves against the working directory: keep
			// it inside this test's own temporary directory.
			t.Chdir(t.TempDir())
			c := testConfig(t)
			ctx := context.Background()
			change(&c, &ctx)
			s, err := tryOpen(t, ctx, c)
			if s != nil || err == nil {
				t.Fatalf("Open accepted a configuration with %s", name)
			}
		})
	}
	t.Run("complete", func(t *testing.T) {
		if _, err := tryOpen(t, context.Background(), testConfig(t)); err != nil {
			t.Fatalf("positive control: a complete configuration was refused: %v", err)
		}
	})
}

// Invariant: Open refuses a keystore whose namespace or process lock is not a
// private directory or regular 0600 file, or whose parent cannot be synced.
// Rows store.go:100, :103, :106, :114, :120, :124, :204, :214, :217, :218.
func TestOpenRefusesUnsafeKeystoreLayout(t *testing.T) {
	cases := map[string]func(t *testing.T, c Config){
		"keystore-mode-0755": func(t *testing.T, c Config) { must(t, os.Mkdir(c.Keystore, 0755)); must(t, os.Chmod(c.Keystore, 0755)) },
		"parent-not-readable": func(t *testing.T, c Config) {
			parent := filepath.Dir(c.Keystore)
			must(t, os.Mkdir(c.Keystore, 0700))
			must(t, os.Chmod(parent, 0300))
			t.Cleanup(func() { os.Chmod(parent, 0700) })
		},
		"namespace-mode-0755": func(t *testing.T, c Config) {
			must(t, os.Mkdir(c.Keystore, 0700))
			root := filepath.Join(c.Keystore, "snowfall")
			must(t, os.Mkdir(root, 0755))
			must(t, os.Chmod(root, 0755))
		},
		"lock-is-directory": func(t *testing.T, c Config) {
			root := mkRoot(t, c)
			must(t, os.Mkdir(filepath.Join(root, "owner.lock"), 0700))
		},
		"lock-is-symlink": func(t *testing.T, c Config) {
			root := mkRoot(t, c)
			target := filepath.Join(filepath.Dir(c.Keystore), "lock-target")
			must(t, os.WriteFile(target, nil, 0600))
			must(t, os.Symlink(target, filepath.Join(root, "owner.lock")))
		},
		"lock-not-writable": func(t *testing.T, c Config) {
			root := mkRoot(t, c)
			must(t, os.WriteFile(filepath.Join(root, "owner.lock"), nil, 0400))
			must(t, os.Chmod(filepath.Join(root, "owner.lock"), 0400))
		},
		"lock-mode-0640": func(t *testing.T, c Config) {
			root := mkRoot(t, c)
			must(t, os.WriteFile(filepath.Join(root, "owner.lock"), nil, 0640))
			must(t, os.Chmod(filepath.Join(root, "owner.lock"), 0640))
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			c := testConfig(t)
			prepare(t, c)
			s, err := tryOpen(t, context.Background(), c)
			if s != nil || err == nil {
				t.Fatalf("Open accepted an unsafe keystore layout (%s)", name)
			}
		})
	}
	t.Run("private-layout", func(t *testing.T) {
		c := testConfig(t)
		root := mkRoot(t, c)
		must(t, os.WriteFile(filepath.Join(root, "owner.lock"), nil, 0600))
		if _, err := tryOpen(t, context.Background(), c); err != nil {
			t.Fatalf("positive control: a private pre-existing layout was refused: %v", err)
		}
	})
}

func mkRoot(t *testing.T, c Config) string {
	t.Helper()
	root := filepath.Join(c.Keystore, "snowfall")
	must(t, os.MkdirAll(root, 0700))
	must(t, os.Chmod(c.Keystore, 0700))
	must(t, os.Chmod(root, 0700))
	return root
}

// Invariant: a second owner of the same keystore is refused with ErrBusy
// while the first is open, and admitted after it closes. Row store.go:128.
func TestOpenSecondOwnerIsBusy(t *testing.T) {
	c := testConfig(t)
	s, _ := openTest(t, c)
	second, err := tryOpen(t, context.Background(), c)
	if second != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("a second owner opened a held keystore: %v", err)
	}
	must(t, s.Close())
	again, err := tryOpen(t, context.Background(), c)
	if err != nil || again == nil {
		t.Fatalf("positive control: keystore stayed busy after Close: %v", err)
	}
}

// Invariant: Open fails closed when the fencing authority cannot be acquired,
// cannot report its head, or reports a zero sequence with a non-zero digest.
// The last two are ErrQuarantined. Rows store.go:137, :160, :164.
func TestOpenFailsClosedOnFenceFaults(t *testing.T) {
	injected := errors.New("injected fence fault")
	t.Run("acquire", func(t *testing.T) {
		c := testConfig(t)
		c.Fence = faultFence{diskFence: c.Fence.(diskFence), acquireErr: injected}
		s, err := tryOpen(t, context.Background(), c)
		if s != nil || !errors.Is(err, injected) {
			t.Fatalf("Open proceeded without a fence lease: %v", err)
		}
	})
	t.Run("head", func(t *testing.T) {
		c := testConfig(t)
		c.Fence = faultFence{diskFence: c.Fence.(diskFence), headErr: injected}
		s, err := tryOpen(t, context.Background(), c)
		if s != nil || !errors.Is(err, ErrQuarantined) {
			t.Fatalf("Open proceeded without a fence head: %v", err)
		}
	})
	t.Run("zero-sequence-with-digest", func(t *testing.T) {
		c := testConfig(t)
		b, _ := json.Marshal(Point{Digest: [32]byte{1}})
		must(t, os.WriteFile(c.Fence.(diskFence).path, b, 0600))
		s, err := tryOpen(t, context.Background(), c)
		if s != nil || !errors.Is(err, ErrQuarantined) {
			t.Fatalf("Open accepted an inconsistent zero head: %v", err)
		}
	})
}

// Invariant: the storage identity file is bound before any data: an identity
// that is not a regular private file, or a missing identity beside existing
// journals, quarantines Open. Rows store.go:716, :719, :720.
func TestOpenIdentityBinding(t *testing.T) {
	t.Run("identity-is-directory", func(t *testing.T) {
		c := testConfig(t)
		root := mkRoot(t, c)
		must(t, os.Mkdir(filepath.Join(root, "identity"), 0700))
		s, err := tryOpen(t, context.Background(), c)
		if s != nil || !errors.Is(err, ErrQuarantined) {
			t.Fatalf("Open rebound a keystore whose identity is unreadable: %v", err)
		}
	})
	t.Run("identity-removed-beside-journals", func(t *testing.T) {
		c := testConfig(t)
		s, v := openTest(t, c)
		_, err := v.Put(context.Background(), writeTest())
		must(t, err)
		must(t, s.Close())
		// The fence head is intact; only the identity file is gone.
		must(t, os.Remove(filepath.Join(c.Keystore, "snowfall", "identity")))
		opened, err := tryOpen(t, context.Background(), c)
		if opened != nil || !errors.Is(err, ErrQuarantined) {
			t.Fatalf("Open recreated the identity of a keystore with journals: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(c.Keystore, "snowfall", "identity")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("quarantined Open wrote a new identity")
		}
	})
}

// installHead writes plain, sealed for sequence seq under this store's key,
// as the journal named by the test authority's head. TEST FIXTURE: it builds
// authenticated state that a correct store never writes, to test that Open
// checks the decrypted snapshot. s is closed; the payloads are test data.
func installHead(t *testing.T, c Config, s *Store, seq uint64, plain []byte) Point {
	t.Helper()
	raw, err := s.seal(plain, s.header(seq))
	must(t, err)
	p := Point{Sequence: seq, Digest: sha256.Sum256(raw)}
	must(t, os.WriteFile(s.filename(p), raw, 0600))
	must(t, s.Close())
	b, _ := json.Marshal(p)
	must(t, os.WriteFile(c.Fence.(diskFence).path, b, 0600))
	return p
}
func validSnapshot(c Config, seq uint64) snapshot {
	return snapshot{Version: 1, StorageID: c.StorageID, Sequence: seq, Records: map[string]record{}, Locks: map[string]string{}, Claims: map[string][]byte{}, DKGs: map[string]string{}, Keys: map[string][]byte{}}
}

// Invariant: an authenticated snapshot is accepted only if it decodes exactly
// (no unknown fields, no trailing data) and is consistent with the fence and
// the storage identity: version 1, this storage ID, the fenced sequence and
// every map present. Each case breaks exactly one of these.
// Rows store.go:191, :193 (7 LO), :194, :258, :262.
func TestOpenChecksDecryptedSnapshot(t *testing.T) {
	cases := map[string]func(c Config, seq uint64) []byte{
		"not-json":      func(Config, uint64) []byte { return []byte("not a snapshot") },
		"unknown-field": func(Config, uint64) []byte { return []byte(`{"Version":1,"Unexpected":true}`) },
		"trailing-data": func(c Config, seq uint64) []byte {
			b, _ := json.Marshal(validSnapshot(c, seq))
			return append(b, []byte(" {}")...)
		},
		"version":        func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.Version = 2; return marshal(x) },
		"storage-id":     func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.StorageID[0] ^= 1; return marshal(x) },
		"sequence":       func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.Sequence = seq + 1; return marshal(x) },
		"records-absent": func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.Records = nil; return marshal(x) },
		"locks-absent":   func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.Locks = nil; return marshal(x) },
		"claims-absent":  func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.Claims = nil; return marshal(x) },
		"dkgs-absent":    func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.DKGs = nil; return marshal(x) },
		"keys-absent":    func(c Config, seq uint64) []byte { x := validSnapshot(c, seq); x.Keys = nil; return marshal(x) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			c := testConfig(t)
			s, err := Open(context.Background(), c)
			must(t, err)
			installHead(t, c, s, 1, build(c, 1))
			opened, err := tryOpen(t, context.Background(), c)
			if opened != nil || !errors.Is(err, ErrQuarantined) {
				t.Fatalf("Open accepted an inconsistent authenticated snapshot (%s): %v", name, err)
			}
		})
	}
	t.Run("consistent", func(t *testing.T) {
		c := testConfig(t)
		s, err := Open(context.Background(), c)
		must(t, err)
		installHead(t, c, s, 1, marshal(validSnapshot(c, 1)))
		if _, err := tryOpen(t, context.Background(), c); err != nil {
			t.Fatalf("positive control: a consistent installed snapshot was refused: %v", err)
		}
	})
}

func marshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Invariant: the journal sequence never wraps. At the last sequence a commit
// is refused before anything is written, and the fenced head is unchanged.
// The head at the maximum sequence is an installed fixture. Row store.go:351.
func TestJournalSequenceNeverWraps(t *testing.T) {
	c := testConfig(t)
	s, err := Open(context.Background(), c)
	must(t, err)
	head := installHead(t, c, s, ^uint64(0), marshal(validSnapshot(c, ^uint64(0))))
	s, v := openTest(t, c)
	before, err := os.ReadDir(s.root)
	must(t, err)
	if release, err := v.Claim(context.Background(), [32]byte{61}, "sign", []byte("intent")); err == nil {
		if release != nil {
			release()
		}
		t.Fatal("a commit after the last journal sequence was accepted")
	}
	after, err := os.ReadDir(s.root)
	must(t, err)
	if len(after) != len(before) {
		t.Fatal("a refused commit at the last sequence wrote a journal file")
	}
	got, err := s.fence.Head(context.Background())
	must(t, err)
	if got != head {
		t.Fatal("a refused commit at the last sequence moved the fence")
	}
}

// Invariant: a sealed snapshot of exactly the journal capacity commits and
// reopens; one byte more is refused before anything is written, without
// quarantining the store. Rows store.go:359 (CB and 2 AR), :360; it also
// reads a snapshot file of exactly maxSnapshot bytes back through Open.
func TestJournalCapacityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra int
	}{{"at-capacity", 0}, {"one-byte-over", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig(t)
			s, v := openTest(t, c)
			next := clone(s.state)
			next.Sequence = s.point.Sequence + 1
			next.Locks["capacity-probe"] = ""
			base := len(marshal(next))
			limit := maxSnapshot - s.aead.Overhead() - s.aead.NonceSize()
			next.Locks["capacity-probe"] = strings.Repeat("a", limit-base+tc.extra)
			if n := len(marshal(next)); n != limit+tc.extra {
				t.Fatalf("fixture size %d, want %d", n, limit+tc.extra)
			}
			err := s.commit(context.Background(), next)
			if tc.extra == 0 {
				if err != nil {
					t.Fatalf("a snapshot of exactly the journal capacity was refused: %v", err)
				}
				must(t, s.Close())
				reopened, err := tryOpen(t, context.Background(), c)
				if err != nil || reopened == nil {
					t.Fatalf("a full-capacity snapshot did not reopen: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a snapshot over the journal capacity was committed")
			}
			if s.point.Sequence != 0 || s.poisoned {
				t.Fatal("a refused oversize commit moved the head or quarantined the store")
			}
			if _, err := v.Put(context.Background(), writeTest()); err != nil {
				t.Fatalf("the store was unusable after a refused oversize commit: %v", err)
			}
		})
	}
}

// Invariant: readPrivate returns a regular file of up to limit bytes in full
// and refuses a larger file or anything that is not a regular file.
// Rows store.go:228, :238, :242, :243.
func TestReadPrivateBounds(t *testing.T) {
	dir := t.TempDir()
	const limit = 64
	write := func(name string, n int) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, bytes.Repeat([]byte{7}, n), 0600))
		return p
	}
	exact := write("exact", limit)
	got, err := readPrivate(exact, limit)
	if err != nil || len(got) != limit {
		t.Fatalf("a file of exactly the limit was not read in full: %d bytes, %v", len(got), err)
	}
	if _, err := readPrivate(write("over", limit+1), limit); err == nil {
		t.Fatal("a file over the limit was read")
	}
	link := filepath.Join(dir, "link")
	must(t, os.Symlink(exact, link))
	if raw, err := readPrivate(link, limit); err == nil {
		t.Fatalf("a symlink was read as a storage file (%d bytes)", len(raw))
	}
	sub := filepath.Join(dir, "sub")
	must(t, os.Mkdir(sub, 0700))
	if _, err := readPrivate(sub, limit); err == nil {
		t.Fatal("a directory was read as a storage file")
	}
}

// Invariant: privateDir accepts only a directory with mode exactly 0700; a
// regular file with mode 0700 is refused. Row store.go:207 (and :208).
func TestPrivateDirRefusesFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	must(t, os.WriteFile(file, nil, 0700))
	must(t, os.Chmod(file, 0700))
	if err := privateDir(file); err == nil {
		t.Fatal("a regular file was accepted as a private directory")
	}
	open := filepath.Join(dir, "open")
	must(t, os.Mkdir(open, 0755))
	must(t, os.Chmod(open, 0755))
	if err := privateDir(open); err == nil {
		t.Fatal("a directory with mode 0755 was accepted")
	}
	if err := privateDir(filepath.Join(dir, "fresh")); err != nil {
		t.Fatalf("positive control: a new private directory was refused: %v", err)
	}
}

// Invariant: a store that is closed, or quarantined by I/O uncertainty, gives
// out no new scope. Rows store.go:459, :460.
func TestScopeRefusedAfterCloseOrQuarantine(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		s, err := Open(context.Background(), testConfig(t))
		must(t, err)
		must(t, s.Close())
		if _, err := s.Scope(testDomain()); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("a closed store gave out a scope: %v", err)
		}
	})
	t.Run("quarantined", func(t *testing.T) {
		s, v := openTest(t, testConfig(t))
		poison(t, s, v)
		if _, err := s.Scope(testDomain()); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("a quarantined store gave out a scope: %v", err)
		}
	})
}

// poison quarantines s through a real injected write failure.
func poison(t *testing.T, s *Store, v *Scoped) {
	t.Helper()
	s.boundary = func(string) error { return errors.New("injected storage uncertainty") }
	if _, err := v.Put(context.Background(), writeTest()); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("fixture: injected failure did not quarantine: %v", err)
	}
	s.boundary = nil
	if !s.poisoned {
		t.Fatal("fixture: store not quarantined")
	}
}

// Invariant: Close refuses while a claim is active, and reports a failure to
// release the fencing lease. Rows store.go:471, :477.
func TestCloseReportsBusyAndLeaseFailure(t *testing.T) {
	t.Run("active-claim", func(t *testing.T) {
		s, err := Open(context.Background(), testConfig(t))
		must(t, err)
		v, err := s.Scope(testDomain())
		must(t, err)
		release, err := v.Claim(context.Background(), [32]byte{62}, "sign", []byte("intent"))
		must(t, err)
		if err := s.Close(); !errors.Is(err, ErrBusy) {
			t.Fatalf("Close succeeded with an active claim: %v", err)
		}
		if s.closed {
			t.Fatal("a refused Close closed the store")
		}
		release()
		must(t, s.Close())
	})
	t.Run("lease-close-failure", func(t *testing.T) {
		injected := errors.New("injected lease close failure")
		c := testConfig(t)
		c.Fence = faultFence{diskFence: c.Fence.(diskFence), closeErr: injected}
		s, err := Open(context.Background(), c)
		must(t, err)
		if err := s.Close(); !errors.Is(err, injected) {
			t.Fatalf("Close hid a fence lease close failure: %v", err)
		}
	})
}

// Invariant: every store operation refuses a nil context instead of running,
// whatever its arguments. Each operation gets valid arguments on a fresh
// store, so only the context check can refuse it. Row store.go:305.
func TestNilContextRefusedByEveryOperation(t *testing.T) {
	var nilCtx context.Context
	ops := map[string]func(*Scoped) error{
		"Put":  func(v *Scoped) error { _, e := v.Put(nilCtx, writeTest()); return e },
		"Read": func(v *Scoped) error { _, e := v.Read(nilCtx, writeTest().ID); return e },
		"Claim": func(v *Scoped) error {
			release, e := v.Claim(nilCtx, [32]byte{67}, "sign", []byte("intent"))
			if release != nil {
				release()
			}
			return e
		},
		"LoadKey":             func(v *Scoped) error { _, e := v.LoadKey(nilCtx); return e },
		"SaveWallet":          func(v *Scoped) error { return v.SaveWallet(nilCtx, walletFixture(t)) },
		"LoadWallet":          func(v *Scoped) error { _, e := v.LoadWallet(nilCtx); return e },
		"RetainSigningRecord": func(v *Scoped) error { return v.RetainSigningRecord(nilCtx, [32]byte{95}, []byte("intent")) },
		"ReadSigningRecord":   func(v *Scoped) error { _, e := v.ReadSigningRecord(nilCtx, [32]byte{95}); return e },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s did not refuse a nil context (panicked: %v)", name, r)
					}
				}()
				err = op(v)
			}()
			if err == nil || errors.Is(err, ErrMissing) {
				t.Fatalf("%s did not refuse a nil context: %v", name, err)
			}
		})
	}
}

// tryPut converts a panic in Put into an error for the caller's assertion.
func tryPut(t *testing.T, v *Scoped, w frost.Write) (r frost.PutResult, err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Put panicked on an invalid write instead of refusing it: %v", p)
		}
	}()
	return v.Put(context.Background(), w)
}

// Invariant (validWrite): a record needs a seat, an ID and a payload of 1 to
// maxRecord bytes; a lock needs a slot with an attempt and a kind of 1 to 64
// bytes; a record kind other than a lock is a candidate or completion record
// with no slot. Each refused case breaks one condition; the accepted cases
// sit on each bound. Rows store.go:494 (3 LO, CB), :495, :498 (3 LO, 2 CB),
// :500, :504.
func TestPutValidatesEachWriteField(t *testing.T) {
	lock := func(kind string) frost.Write {
		return frost.Write{Kind: "lock", Seat: 1, ID: [32]byte{70}, Payload: []byte("lock"), Slot: &frost.LockSlot{Attempt: [32]byte{71}, Kind: kind}}
	}
	refused := map[string]frost.Write{}
	add := func(name string, change func(*frost.Write)) {
		w := writeTest()
		change(&w)
		refused[name] = w
	}
	add("seat-zero", func(w *frost.Write) { w.Seat = 0 })
	add("id-zero", func(w *frost.Write) { w.ID = [32]byte{} })
	add("payload-empty", func(w *frost.Write) { w.Payload = nil })
	add("payload-over-max", func(w *frost.Write) { w.Payload = make([]byte, maxRecord+1) })
	add("record-with-slot", func(w *frost.Write) { w.Slot = &frost.LockSlot{Attempt: [32]byte{71}, Kind: "round-one"} })
	add("unknown-kind", func(w *frost.Write) { w.Kind = "other-record" })
	noSlot := lock("round-one")
	noSlot.Slot = nil
	refused["lock-without-slot"] = noSlot
	zeroAttempt := lock("round-one")
	zeroAttempt.Slot.Attempt = [32]byte{}
	refused["lock-zero-attempt"] = zeroAttempt
	refused["lock-empty-kind"] = lock("")
	refused["lock-kind-65"] = lock(strings.Repeat("k", 65))
	for name, w := range refused {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			if r, err := tryPut(t, v, w); err == nil || r != 0 {
				t.Fatalf("Put accepted an invalid write (%s): result %v", name, r)
			}
			if _, err := v.Read(context.Background(), w.ID); !errors.Is(err, ErrMissing) {
				t.Fatalf("a refused write (%s) reached the store: %v", name, err)
			}
		})
	}
	accepted := map[string]frost.Write{
		"payload-at-max":    func() frost.Write { w := writeTest(); w.Payload = make([]byte, maxRecord); w.Payload[0] = 1; return w }(),
		"completion-record": func() frost.Write { w := writeTest(); w.Kind = "completion-record"; return w }(),
		"lock-kind-1":       lock("k"),
		"lock-kind-64":      lock(strings.Repeat("k", 64)),
	}
	for name, w := range accepted {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			if r, err := tryPut(t, v, w); err != nil || r != frost.Durable {
				t.Fatalf("Put refused a valid write on a bound (%s): %v %v", name, r, err)
			}
		})
	}
}

// Invariant: a claim needs purpose dkg or sign, a non-zero ID and an intent of
// 1 to maxRecord bytes. Rows store.go:577 (3 LO, CB), :578.
func TestClaimValidatesEachField(t *testing.T) {
	refused := map[string]struct {
		id      [32]byte
		purpose string
		intent  []byte
	}{
		"purpose":         {[32]byte{63}, "other", []byte("intent")},
		"id-zero":         {[32]byte{}, "sign", []byte("intent")},
		"intent-empty":    {[32]byte{63}, "sign", nil},
		"intent-over-max": {[32]byte{63}, "sign", make([]byte, maxRecord+1)},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			release, err := v.Claim(context.Background(), tc.id, tc.purpose, tc.intent)
			if err == nil {
				if release != nil {
					release()
				}
				t.Fatalf("Claim accepted an invalid attempt (%s)", name)
			}
			if s.point.Sequence != 0 {
				t.Fatalf("a refused claim (%s) was committed", name)
			}
		})
	}
	t.Run("intent-at-max", func(t *testing.T) {
		_, v := openTest(t, testConfig(t))
		release, err := v.Claim(context.Background(), [32]byte{63}, "sign", make([]byte, maxRecord))
		if err != nil {
			t.Fatalf("Claim refused an intent of exactly maxRecord bytes: %v", err)
		}
		release()
	})
}

// commitFixture commits next directly. TEST FIXTURE: used only to install
// authenticated records that a correct store never writes.
func commitFixture(t *testing.T, s *Store, next snapshot) {
	t.Helper()
	must(t, s.commit(context.Background(), next))
}

// Invariant: Read returns a record only when its stored domain and ID are the
// ones it is filed under, and only when its ciphertext authenticates. Each
// fixture record is sealed under its own header, so only these checks can
// refuse it. Rows store.go:566 (LO), :280.
func TestReadRefusesMisfiledOrTruncatedRecord(t *testing.T) {
	id := [32]byte{64}
	other := testDomain()
	other.Epoch++
	cases := map[string]func(*Scoped) record{
		"foreign-domain": func(v *Scoped) record { return record{Domain: other, Kind: "candidate-record", Seat: 1, ID: id} },
		"foreign-id": func(v *Scoped) record {
			return record{Domain: v.domain, Kind: "candidate-record", Seat: 1, ID: [32]byte{65}}
		},
		"truncated": func(v *Scoped) record { return record{Domain: v.domain, Kind: "candidate-record", Seat: 1, ID: id} },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			r := build(v)
			var err error
			r.Ciphertext, err = s.seal([]byte("test payload"), recordHeader(r))
			must(t, err)
			if name == "truncated" {
				r.Ciphertext = r.Ciphertext[:3]
			}
			next := clone(s.state)
			next.Records[v.recordKey(id)] = r
			commitFixture(t, s, next)
			got, err := v.Read(context.Background(), id)
			if !errors.Is(err, ErrQuarantined) || got != nil {
				t.Fatalf("Read returned a %s record: %v", name, err)
			}
		})
	}
}

// Invariant: a completed key alone reserves the domain against a new DKG,
// even without a DKG claim beside it. The key is an opaque fixture.
// Row store.go:596.
func TestDKGClaimRefusedByInstalledKey(t *testing.T) {
	s, v := openTest(t, testConfig(t))
	next := clone(s.state)
	next.Keys[v.prefix()] = []byte("opaque fixture, never decoded")
	commitFixture(t, s, next)
	release, err := v.Claim(context.Background(), [32]byte{66}, "dkg", []byte("intent"))
	if !errors.Is(err, ErrDKGPending) {
		if release != nil {
			release()
		}
		t.Fatalf("a DKG was claimed over an installed key: %v", err)
	}
}

// Invariant: LoadKey reports ErrMissing when no key exists, and quarantines a
// stored key that authenticates but does not decode. The undecodable key is
// a fixture of test bytes. Rows store.go:690, :697.
func TestLoadKeyMissingAndUndecodable(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, v := openTest(t, testConfig(t))
		if _, err := v.LoadKey(context.Background()); !errors.Is(err, ErrMissing) {
			t.Fatalf("LoadKey invented a key: %v", err)
		}
	})
	t.Run("undecodable", func(t *testing.T) {
		s, v := openTest(t, testConfig(t))
		raw, err := s.seal([]byte("not a key record"), []byte("keep-core/snowfall/key/v1/"+v.prefix()))
		must(t, err)
		next := clone(s.state)
		next.Keys[v.prefix()] = raw
		commitFixture(t, s, next)
		if _, err := v.LoadKey(context.Background()); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("LoadKey returned an undecodable key: %v", err)
		}
		if !s.poisoned {
			t.Fatal("an undecodable key did not quarantine the store")
		}
	})
}
