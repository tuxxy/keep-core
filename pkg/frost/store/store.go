package store

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/keep-network/keep-core/pkg/frost"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrQuarantined = errors.New("FROST store quarantined: reconcile with fencing authority")
	ErrDKGPending  = errors.New("this domain already has a DKG claim or a completed key; a crashed or abandoned DKG needs a fresh epoch")
	ErrClaimed     = errors.New("FROST attempt already claimed")
	ErrBusy        = errors.New("FROST store has an active owner or operation")
	ErrMissing     = errors.New("FROST record missing")
	ErrDKGLost     = errors.New("FROST DKG seats lost: use a fresh epoch")
)

const maxSnapshot = 32 << 20
const maxRecord = 1 << 20

// Config.Key is high-entropy key material from the host secret manager, not a
// password. The caller owns provisioning and recovery; it must never use a key
// from a worker environment. This implementation performs no password KDF.
type Config struct {
	Keystore  string
	StorageID [32]byte
	Key       [32]byte
	Fence     Fence
}

type record struct {
	Domain     frost.Domain
	Kind       string
	Seat       uint16
	ID         [32]byte
	Slot       *frost.LockSlot
	Ciphertext []byte
}
type snapshot struct {
	Version   uint16
	StorageID [32]byte
	Sequence  uint64
	Records   map[string]record
	Locks     map[string]string
	Claims    map[string][]byte
	DKGs      map[string]string
	Keys      map[string][]byte
	Wallets   map[string][]byte `json:",omitempty"`
}

type Store struct {
	mu               sync.Mutex
	root             string
	id               [32]byte
	aead             cipher.AEAD
	lock             *os.File
	fence            FenceLease
	point            Point
	state            snapshot
	closed, poisoned bool
	active           int
	// A live DKG exists only in this process, while its claim lease is held.
	liveDKGs map[string]string
	// Tests interrupt real I/O at named boundaries. Production leaves nil.
	boundary      func(string) error
	syncFile      func(*os.File) error
	syncDirectory func(string) error
	install       func(string, string) error
}

type Scoped struct {
	store  *Store
	domain frost.Domain
}

var _ frost.Journal = (*Scoped)(nil)

func Open(ctx context.Context, c Config) (out *Store, err error) {
	if ctx == nil || c.Fence == nil || !filepath.IsAbs(c.Keystore) || c.StorageID == ([32]byte{}) || c.Key == ([32]byte{}) {
		return nil, errors.New("invalid FROST store configuration")
	}
	root := filepath.Join(c.Keystore, "snowfall")
	if err = privateDir(c.Keystore); err != nil {
		return nil, err
	}
	if err = syncDir(filepath.Dir(c.Keystore)); err != nil {
		return nil, err
	}
	if err = privateDir(root); err != nil {
		return nil, err
	}
	// Persist creation of the dedicated namespace before any data can be acked.
	if err = syncDir(c.Keystore); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(root, "owner.lock")
	if st, e := os.Lstat(lockPath); e == nil && !st.Mode().IsRegular() {
		return nil, errors.New("invalid process lock file")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = checkFile(lock, 0600); err != nil {
		lock.Close()
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return nil, ErrBusy
	}
	defer func() {
		if out == nil {
			lock.Close()
		}
	}()
	lease, err := c.Fence.Acquire(ctx, c.StorageID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if out == nil {
			lease.Close()
		}
	}()
	var key [32]byte
	if _, err = io.ReadFull(hkdf.New(sha256.New, c.Key[:], c.StorageID[:], []byte("keep-core/snowfall/storage/v1")), key[:]); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key[:])
	clear(key[:])
	clear(c.Key[:])
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, id: c.StorageID, aead: aead, lock: lock, fence: lease, liveDKGs: make(map[string]string), syncFile: func(f *os.File) error { return f.Sync() }, syncDirectory: syncDir, install: os.Link}
	if err = bindIdentity(root, c.StorageID); err != nil {
		return nil, err
	}
	s.point, err = lease.Head(ctx)
	if err != nil {
		return nil, ErrQuarantined
	}
	if s.point.Sequence == 0 {
		if s.point.Digest != ([32]byte{}) {
			return nil, ErrQuarantined
		}
		// A reset fence must not hide existing journals, even when identity
		// still matches. Only an empty store can use a provisioned zero head.
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, ErrQuarantined
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".journal") {
				return nil, ErrQuarantined
			}
		}
		s.state = snapshot{Version: 1, StorageID: c.StorageID, Records: map[string]record{}, Locks: map[string]string{}, Claims: map[string][]byte{}, DKGs: map[string]string{}, Keys: map[string][]byte{}}
	} else {
		raw, e := readPrivate(s.filename(s.point), maxSnapshot)
		if e != nil {
			return nil, ErrQuarantined
		}
		if sha256.Sum256(raw) != s.point.Digest {
			return nil, ErrQuarantined
		}
		plain, e := s.open(raw, s.header(s.point.Sequence))
		if e != nil {
			return nil, ErrQuarantined
		}
		if e = decode(plain, &s.state); e != nil {
			return nil, ErrQuarantined
		}
		if s.state.Version != 1 || s.state.StorageID != s.id || s.state.Sequence != s.point.Sequence || s.state.Records == nil || s.state.Locks == nil || s.state.Claims == nil || s.state.DKGs == nil || s.state.Keys == nil {
			return nil, ErrQuarantined
		}
	}
	return s, nil
}
func privateDir(path string) error {
	if e := os.Mkdir(path, 0700); e != nil && !errors.Is(e, os.ErrExist) {
		return e
	}
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode().Perm() != 0700 {
		return errors.New("FROST storage directory must have mode 0700 and cannot be a symlink")
	}
	return nil
}
func checkFile(f *os.File, mode os.FileMode) error {
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != mode {
		return errors.New("invalid FROST storage file mode")
	}
	return nil
}
func readPrivate(path string, limit int64) ([]byte, error) {
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("invalid storage file")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if e = checkFile(f, 0600); e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, errors.New("storage capacity exceeded")
	}
	return b, nil
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
func (s *Store) header(seq uint64) []byte {
	b := append([]byte("keep-core/snowfall/journal/v1"), s.id[:]...)
	return binary.BigEndian.AppendUint64(b, seq)
}
func (s *Store) seal(plain, header []byte) ([]byte, error) {
	n := make([]byte, s.aead.NonceSize())
	if _, e := io.ReadFull(rand.Reader, n); e != nil {
		return nil, e
	}
	return s.aead.Seal(n, n, plain, header), nil
}
func (s *Store) open(raw, header []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(raw) < n {
		return nil, ErrQuarantined
	}
	return s.aead.Open(nil, raw[:n], raw[n:], header)
}
func (s *Store) filename(p Point) string {
	return filepath.Join(s.root, fmt.Sprintf("%020d-%x.journal", p.Sequence, p.Digest))
}
func (s *Store) step(name string) error {
	if s.boundary != nil {
		return s.boundary(name)
	}
	return nil
}
func (s *Store) fail(err error) error {
	if err != nil {
		s.poisoned = true
		return ErrQuarantined
	}
	return nil
}
func (s *Store) check(ctx context.Context) error {
	if s.closed || s.poisoned {
		return ErrQuarantined
	}
	if ctx == nil {
		return errors.New("missing context")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	p, e := s.fence.Head(ctx)
	if e != nil || p != s.point {
		return s.fail(ErrQuarantined)
	}
	return nil
}
func clone(s snapshot) snapshot {
	c := s
	c.Records = make(map[string]record, len(s.Records))
	for k, v := range s.Records {
		c.Records[k] = v
	}
	c.Locks = make(map[string]string, len(s.Locks))
	for k, v := range s.Locks {
		c.Locks[k] = v
	}
	c.Claims = make(map[string][]byte, len(s.Claims))
	for k, v := range s.Claims {
		c.Claims[k] = v
	}
	c.DKGs = make(map[string]string, len(s.DKGs))
	for k, v := range s.DKGs {
		c.DKGs[k] = v
	}
	c.Keys = make(map[string][]byte, len(s.Keys))
	for k, v := range s.Keys {
		c.Keys[k] = v
	}
	c.Wallets = make(map[string][]byte, len(s.Wallets))
	for k, v := range s.Wallets {
		c.Wallets[k] = v
	}

	return c
}

// commit never overwrites an installed snapshot. Only the independently fenced
// digest identifies committed data. An interrupted unfenced install is an orphan
// and cannot authorize a worker; an acknowledged head can never be rolled back.
func (s *Store) commit(ctx context.Context, next snapshot) error {
	if s.point.Sequence == ^uint64(0) {
		return errors.New("journal sequence exhausted")
	}
	next.Sequence = s.point.Sequence + 1
	plain, e := json.Marshal(next)
	if e != nil {
		return e
	}
	defer clear(plain)
	if len(plain) > maxSnapshot-s.aead.Overhead()-s.aead.NonceSize() {
		return errors.New("FROST journal capacity exceeded")
	}
	raw, e := s.seal(plain, s.header(next.Sequence))
	if e != nil {
		return s.fail(e)
	}
	point := Point{Sequence: next.Sequence, Digest: sha256.Sum256(raw)}
	f, e := os.CreateTemp(s.root, ".pending-")
	if e != nil {
		return s.fail(e)
	}
	path := f.Name()
	defer os.Remove(path)
	defer f.Close()
	if e = s.step("before-write"); e != nil {
		return s.fail(e)
	}
	if _, e = f.Write(raw); e != nil {
		return s.fail(e)
	}
	if e = s.step("before-file-sync"); e != nil {
		return s.fail(e)
	}
	if e = s.syncFile(f); e != nil {
		return s.fail(e)
	}
	if e = s.step("after-file-sync"); e != nil {
		return s.fail(e)
	}
	if e = f.Close(); e != nil {
		return s.fail(e)
	}
	if e = s.step("before-install"); e != nil {
		return s.fail(e)
	}
	if e = s.install(path, s.filename(point)); e != nil {
		return s.fail(e)
	}
	if e = s.step("after-install"); e != nil {
		return s.fail(e)
	}
	if e = s.step("before-directory-sync"); e != nil {
		return s.fail(e)
	}
	if e = s.syncDirectory(s.root); e != nil {
		return s.fail(e)
	}
	if e = s.step("after-directory-sync"); e != nil {
		return s.fail(e)
	}
	if e = s.step("before-fence"); e != nil {
		return s.fail(e)
	}
	if e = s.fence.Advance(ctx, s.point, point); e != nil {
		return s.fail(e)
	}
	if e = s.step("after-fence"); e != nil {
		return s.fail(e)
	}
	s.point = point
	s.state = next
	if e = s.step("before-acknowledgement"); e != nil {
		return s.fail(e)
	}
	return nil
}
func (s *Store) syncCurrent() error {
	if s.point.Sequence == 0 {
		return nil
	}
	raw, e := readPrivate(s.filename(s.point), maxSnapshot)
	if e != nil {
		return s.fail(e)
	}
	if sha256.Sum256(raw) != s.point.Digest {
		return s.fail(ErrQuarantined)
	}
	f, e := os.OpenFile(s.filename(s.point), os.O_RDWR, 0600)
	if e != nil {
		return s.fail(e)
	}
	defer f.Close()
	if e = s.step("before-identical-sync"); e != nil {
		return s.fail(e)
	}
	if e = s.syncFile(f); e != nil {
		return s.fail(e)
	}
	if e = s.syncDirectory(s.root); e != nil {
		return s.fail(e)
	}
	return s.fail(s.step("after-identical-sync"))
}
func (s *Store) Scope(d frost.Domain) (*Scoped, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.poisoned {
		return nil, ErrQuarantined
	}
	return &Scoped{s, d}, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.active != 0 {
		return ErrBusy
	}
	s.closed = true
	e := s.fence.Close()
	le := s.lock.Close()
	s.aead = nil
	if e != nil {
		return e
	}
	return le
}
func (v *Scoped) Domain() frost.Domain         { return v.domain }
func (v *Scoped) prefix() string               { id := v.domain.ID(); return hex.EncodeToString(id[:]) }
func (v *Scoped) recordKey(id [32]byte) string { return v.prefix() + "/" + hex.EncodeToString(id[:]) }
func recordHeader(r record) []byte {
	r.Ciphertext = nil
	b, _ := json.Marshal(r)
	return append([]byte("keep-core/snowfall/record/v1/"), b...)
}
func (v *Scoped) slot(w frost.Write) string {
	return fmt.Sprintf("%s/%d/%x/%s", v.prefix(), w.Seat, w.Slot.Attempt, w.Slot.Kind)
}
func validWrite(w frost.Write) bool {
	if w.Seat == 0 || w.ID == ([32]byte{}) || len(w.Payload) == 0 || len(w.Payload) > maxRecord {
		return false
	}
	if w.Kind == "lock" {
		return w.Slot != nil && w.Slot.Attempt != ([32]byte{}) && len(w.Slot.Kind) > 0 && len(w.Slot.Kind) <= 64
	}
	return w.Slot == nil && (w.Kind == "candidate-record" || w.Kind == "completion-record")
}
func (v *Scoped) Put(ctx context.Context, w frost.Write) (frost.PutResult, error) {
	if !validWrite(w) {
		return 0, errors.New("invalid opaque FROST record")
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return 0, e
	}
	key := v.recordKey(w.ID)
	if w.Slot != nil {
		if id, ok := s.state.Locks[v.slot(w)]; ok && id != key {
			return frost.Conflict, nil
		}
	}
	r := record{Domain: v.domain, Kind: w.Kind, Seat: w.Seat, ID: w.ID, Slot: w.Slot}
	if old, ok := s.state.Records[key]; ok {
		if !bytes.Equal(recordHeader(old), recordHeader(r)) {
			return frost.Conflict, nil
		}
		plain, e := s.open(old.Ciphertext, recordHeader(old))
		if e != nil {
			return 0, s.fail(e)
		}
		defer clear(plain)
		if !bytes.Equal(plain, w.Payload) {
			return frost.Conflict, nil
		}
		if e = s.syncCurrent(); e != nil {
			return 0, e
		}
		return frost.Identical, nil
	}
	var e error
	r.Ciphertext, e = s.seal(w.Payload, recordHeader(r))
	if e != nil {
		return 0, s.fail(e)
	}
	if w.Slot != nil {
		slot := *w.Slot
		r.Slot = &slot
	}
	next := clone(s.state)
	next.Records[key] = r
	if w.Slot != nil {
		next.Locks[v.slot(w)] = key
	}
	if e = s.commit(ctx, next); e != nil {
		return 0, e
	}
	return frost.Durable, nil
}
func (v *Scoped) Read(ctx context.Context, id [32]byte) ([]byte, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	r, ok := s.state.Records[v.recordKey(id)]
	if !ok {
		return nil, ErrMissing
	}
	if r.Domain != v.domain || r.ID != id {
		return nil, s.fail(ErrQuarantined)
	}
	out, e := s.open(r.Ciphertext, recordHeader(r))
	if e != nil {
		return nil, s.fail(e)
	}
	return out, nil
}

func (v *Scoped) Claim(ctx context.Context, id [32]byte, purpose string, intent []byte) (func(), error) {
	if (purpose != "dkg" && purpose != "sign") || id == ([32]byte{}) || len(intent) == 0 || len(intent) > maxRecord {
		return nil, errors.New("invalid attempt intent")
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	// Claim IDs are global to this storage owner, not removable by changing scope.
	key := hex.EncodeToString(id[:])
	if _, ok := s.state.Claims[key]; ok {
		return nil, ErrClaimed
	}
	if purpose == "dkg" {
		if _, ok := s.state.DKGs[v.prefix()]; ok {
			return nil, ErrDKGPending
		}
		if _, ok := s.state.Keys[v.prefix()]; ok {
			return nil, ErrDKGPending
		}
	}
	if e := s.step("before-claim"); e != nil {
		return nil, s.fail(e)
	}
	header := append([]byte("keep-core/snowfall/claim/v1/"+key), v.domain.Bytes()...)
	raw, e := s.seal(intent, header)
	if e != nil {
		return nil, s.fail(e)
	}
	next := clone(s.state)
	next.Claims[key] = raw
	if purpose == "dkg" {
		next.DKGs[v.prefix()] = key
	}
	if e = s.commit(ctx, next); e != nil {
		return nil, e
	}
	if e = s.step("after-claim"); e != nil {
		return nil, s.fail(e)
	}
	s.active++
	if purpose == "dkg" {
		s.liveDKGs[v.prefix()] = key
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.active--
			if purpose == "dkg" {
				delete(s.liveDKGs, v.prefix())
			}
		})
	}, nil
}
func (v *Scoped) SaveKey(ctx context.Context, k frost.KeyReady) error {
	if k.Candidate.Epoch != v.domain.Epoch || k.Candidate.Profile != frost.ApprovedProfile || len(k.LocalReferences) == 0 {
		return errors.New("invalid installed key")
	}
	plain, e := json.Marshal(k)
	if e != nil {
		return e
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = s.check(ctx); e != nil {
		return e
	}
	key := v.prefix()
	header := []byte("keep-core/snowfall/key/v1/" + key)
	if raw, ok := s.state.Keys[key]; ok {
		old, e := s.open(raw, header)
		if e != nil {
			return s.fail(e)
		}
		if !bytes.Equal(old, plain) {
			return errors.New("installed key conflict")
		}
		return s.syncCurrent()
	}
	// A released or restarted DKG cannot install a late key and undo the
	// terminal loss report. Identical saved keys above remain idempotent.
	attempt, ok := s.state.DKGs[key]
	if !ok || s.liveDKGs[key] != attempt {
		return ErrDKGLost
	}
	request, _, e := v.dkgRequest()
	if e != nil {
		return e
	}
	if !keyMatchesRequest(k, request) {
		return errors.New("installed key does not match the claimed DKG seats and group")
	}
	raw, e := s.seal(plain, header)
	if e != nil {
		return s.fail(e)
	}
	next := clone(s.state)
	next.Keys[key] = raw
	return s.commit(ctx, next)
}
func (v *Scoped) LoadKey(ctx context.Context) (frost.KeyReady, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return frost.KeyReady{}, e
	}
	raw, ok := s.state.Keys[v.prefix()]
	if !ok {
		return frost.KeyReady{}, ErrMissing
	}
	plain, e := s.open(raw, []byte("keep-core/snowfall/key/v1/"+v.prefix()))
	if e != nil {
		return frost.KeyReady{}, s.fail(e)
	}
	var k frost.KeyReady
	if e = decode(plain, &k); e != nil {
		return k, s.fail(e)
	}
	return k, nil
}

// bindIdentity prevents an accidental fresh fence namespace from resetting a
// pre-existing keystore. The authority still supplies the anti-rollback fact.
func bindIdentity(root string, id [32]byte) error {
	path := filepath.Join(root, "identity")
	want := append([]byte("keep-core/snowfall/storage-id/v1/"), id[:]...)
	raw, err := readPrivate(path, 256)
	if err == nil {
		if !bytes.Equal(raw, want) {
			return ErrQuarantined
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ErrQuarantined
	}
	existing, err := filepath.Glob(filepath.Join(root, "*.journal"))
	if err != nil || len(existing) > 0 {
		return ErrQuarantined
	}
	f, err := os.CreateTemp(root, ".identity-")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if _, err = f.Write(want); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	return syncDir(root)
}
