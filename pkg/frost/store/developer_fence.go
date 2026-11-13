package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// The developer fence is the exclusive-host, no-snapshot operational
// assumption that KC-SF-V3 K-02 "Safe state/recovery" allows when no
// independent fencing authority exists. It is for the developer network only
// (the configuration builds it only there); it is not the production fencing
// authority, which is a later decision.
//
// Its state is one lab-provisioned file, kept outside the keystore. The lab
// creates the file empty (a newly provisioned store, the zero head); the fence
// never creates it, so a lost file is refused instead of silently becoming a
// new identity. The holder of a lease keeps an exclusive flock on the file
// until Close or process death, which excludes every other lease, in this
// process and in others. The first Acquire binds the file to its storage ID.
// Every installed head is written in place and synchronized before Advance
// returns, so it survives the holder's death.
//
// File format: an empty file, or exactly developerFenceSize bytes: the magic,
// the bound storage ID, the head's sequence (big-endian) and digest, and the
// SHA-256 of all of these. Anything else is refused, never reset.

var (
	// ErrFenceBound means the fence file belongs to another storage ID.
	ErrFenceBound = errors.New("FROST developer fence is bound to another storage ID")
	// ErrFenceStale means Advance's before is not the fence's head.
	ErrFenceStale = errors.New("FROST developer fence head is not the expected point")
	// ErrFenceClosed means the lease was closed.
	ErrFenceClosed = errors.New("FROST developer fence lease is closed")
	errFenceFile   = errors.New("invalid FROST developer fence file")
)

var developerFenceMagic = [8]byte{'K', 'C', 'F', 'E', 'N', 'C', 'E', '1'}

const developerFenceSize = 8 + 32 + 8 + 32 + sha256.Size

type developerFence struct{ path string }

// NewDeveloperFence returns the developer disk fence backed by the
// provisioned file at path, which must be absolute and an existing regular
// file. It never creates the file.
func NewDeveloperFence(path string) (Fence, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("the fence file path must be absolute")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errFenceFile
	}
	return developerFence{path: path}, nil
}

func (f developerFence) Acquire(ctx context.Context, id [32]byte) (FenceLease, error) {
	if ctx == nil || id == ([32]byte{}) {
		return nil, errors.New("invalid FROST developer fence request")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// O_RDWR without O_CREATE: a missing file is refused, never created.
	file, err := os.OpenFile(f.path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	lease := &developerLease{file: file, id: id}
	if err = acquireDeveloperLease(file, lease, id); err != nil {
		file.Close()
		return nil, err
	}
	return lease, nil
}

func acquireDeveloperLease(file *os.File, lease *developerLease, id [32]byte) error {
	st, err := file.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errFenceFile
	}
	// flock excludes every other open file description, including another
	// lease of this process.
	if err = lockFile(file); err != nil {
		return ErrBusy
	}
	bound, head, empty, err := readDeveloperFence(file)
	if err != nil {
		return err
	}
	if empty {
		// First use: bind the provisioned file to this storage ID at the
		// zero head.
		return lease.install(Point{})
	}
	if bound != id {
		return ErrFenceBound
	}
	lease.head = head
	return nil
}

func readDeveloperFence(file *os.File) (id [32]byte, head Point, empty bool, err error) {
	b, err := io.ReadAll(io.NewSectionReader(file, 0, developerFenceSize+1))
	if err != nil {
		return id, head, false, err
	}
	if len(b) == 0 {
		return id, head, true, nil
	}
	if len(b) != developerFenceSize || !bytes.Equal(b[:8], developerFenceMagic[:]) {
		return id, head, false, errFenceFile
	}
	sum := sha256.Sum256(b[:developerFenceSize-sha256.Size])
	if !bytes.Equal(sum[:], b[developerFenceSize-sha256.Size:]) {
		return id, head, false, errFenceFile
	}
	copy(id[:], b[8:40])
	head.Sequence = binary.BigEndian.Uint64(b[40:48])
	copy(head.Digest[:], b[48:80])
	return id, head, false, nil
}

type developerLease struct {
	mu     sync.Mutex
	file   *os.File
	id     [32]byte
	head   Point
	closed bool
}

// install writes p as the head bound to the lease's storage ID, in place
// (the flock is on this inode, so the file is never replaced), and
// synchronizes it before returning.
func (l *developerLease) install(p Point) error {
	b := make([]byte, 0, developerFenceSize)
	b = append(b, developerFenceMagic[:]...)
	b = append(b, l.id[:]...)
	b = binary.BigEndian.AppendUint64(b, p.Sequence)
	b = append(b, p.Digest[:]...)
	sum := sha256.Sum256(b)
	b = append(b, sum[:]...)
	if _, err := l.file.WriteAt(b, 0); err != nil {
		return err
	}
	if err := l.file.Sync(); err != nil {
		return err
	}
	l.head = p
	return nil
}

// Head rereads the file, so a change made outside the lease is seen.
func (l *developerLease) Head(ctx context.Context) (Point, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current(ctx)
}

func (l *developerLease) current(ctx context.Context) (Point, error) {
	if l.closed {
		return Point{}, ErrFenceClosed
	}
	if ctx == nil {
		return Point{}, errors.New("missing context")
	}
	if err := ctx.Err(); err != nil {
		return Point{}, err
	}
	id, head, empty, err := readDeveloperFence(l.file)
	if err != nil {
		return Point{}, err
	}
	if empty || id != l.id {
		return Point{}, ErrFenceBound
	}
	if head != l.head {
		return Point{}, fmt.Errorf("%w: changed outside the lease", errFenceFile)
	}
	return head, nil
}

// Advance installs after only if before is the current head and after is
// its successor.
func (l *developerLease) Advance(ctx context.Context, before, after Point) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	head, err := l.current(ctx)
	if err != nil {
		return err
	}
	if head != before || after.Sequence != before.Sequence+1 {
		return ErrFenceStale
	}
	return l.install(after)
}

// Close releases the lease; a closed lease cannot read or advance the fence.
func (l *developerLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.file.Close()
}
