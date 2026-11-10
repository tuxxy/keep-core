//go:build unix

package store

import (
	"path/filepath"
	"syscall"
	"testing"
)

// Invariant: privateDir checks the mode of a directory it has just created,
// not only of one that already existed. Under a umask that strips owner bits
// the new directory is not 0700 and must be refused. The umask is process
// wide, so this test must not run in parallel. Row store.go:200.
func TestPrivateDirChecksNewDirectory(t *testing.T) {
	dir := t.TempDir()
	old := syscall.Umask(0o277)
	defer syscall.Umask(old)
	if err := privateDir(filepath.Join(dir, "created-0500")); err == nil {
		t.Fatal("a newly created directory without owner write access was accepted")
	}
}
