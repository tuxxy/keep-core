package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// This loopback authority stays in the parent while a separate host process
// owns the real store. It is an explicit local fence adapter, not production
// fence durability or power-loss qualification. Only public fence points cross
// HTTP; keys, worker records and store encryption material never enter IPC.
type k09Fence struct{ URL string }
type k09Advance struct{ Before, After store.Point }

func (f k09Fence) call(ctx context.Context, method string, value, out any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", f.URL+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("test fence refused")
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}
func (f k09Fence) Acquire(ctx context.Context, id [32]byte) (store.FenceLease, error) {
	return f, f.call(ctx, "acquire", id, nil)
}
func (f k09Fence) Head(ctx context.Context) (store.Point, error) {
	var p store.Point
	err := f.call(ctx, "head", nil, &p)
	return p, err
}
func (f k09Fence) Advance(ctx context.Context, before, after store.Point) error {
	return f.call(ctx, "advance", k09Advance{before, after}, nil)
}
func (f k09Fence) Close() error { return f.call(context.Background(), "close", nil, nil) }

type k09CrashConfig struct {
	Keystore  string
	StorageID [32]byte
	Domain    frost.Domain
	FenceURL  string
}

var k09CrashClaim = [32]byte{0x09, 0x23}

// k09CrashReopen kills a separate host while an acknowledged signing claim is
// active (before any Done/release), then resumes the SAME lifecycle journal.
// Existing worker key/defense records are only loaded through the opaque API.
func k09CrashReopen(t *testing.T, f *fixture, operator int, domain frost.Domain) *store.Scoped {
	t.Helper()
	config := f.rootConfigs[operator]
	if config.Key != ([32]byte{17}) {
		t.Fatal("crash helper requires the public fixture store key")
	}
	if err := f.roots[operator].Close(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var owner store.FenceLease
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var err error
		switch r.URL.Path {
		case "/acquire":
			if owner != nil {
				err = store.ErrBusy
				break
			}
			owner, err = config.Fence.Acquire(r.Context(), config.StorageID)
		case "/head":
			if owner == nil {
				err = store.ErrBusy
				break
			}
			var point store.Point
			point, err = owner.Head(r.Context())
			if err == nil {
				_ = json.NewEncoder(w).Encode(point)
				return
			}
		case "/advance":
			var change k09Advance
			if owner == nil {
				err = store.ErrBusy
				break
			}
			if err = json.NewDecoder(r.Body).Decode(&change); err == nil {
				err = owner.Advance(r.Context(), change.Before, change.After)
			}
		case "/close":
			if owner != nil {
				err = owner.Close()
				owner = nil
			}
		default:
			err = errors.New("unknown fence method")
		}
		if err != nil {
			http.Error(w, "test fence refused", http.StatusConflict)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "public-crash-config.json")
	raw, err := json.Marshal(k09CrashConfig{config.Keystore, config.StorageID, domain, server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(f.ctx, executable, "-test.run=^TestK09CrashProcessHelper$", "-test.v")
	command.Env = append(os.Environ(), "FROST_K09_CRASH_HELPER="+path)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if scanner.Text() == "K09-ACTIVE-CLAIM" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("crash helper failed before the active-claim boundary")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("crash helper did not reach the active-claim boundary")
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var killed *exec.ExitError
	if err = command.Wait(); !errors.As(err, &killed) {
		t.Fatal("host did not die by signal", err)
	}
	status := command.ProcessState.Sys().(syscall.WaitStatus)
	if !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("host cleanup ran instead of SIGKILL")
	}
	// The test-only authority revokes the old lease ONLY after confirmed death.
	// Its point survives outside the killed host and is never reset or inferred
	// from the store. This is not a production lease-recovery implementation.
	mu.Lock()
	if owner != nil {
		err = owner.Close()
		owner = nil
	}
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(f.ctx, config)
	if err != nil {
		t.Fatal("open after killed host:", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	records, err := reopened.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = records.Claim(f.ctx, k09CrashClaim, "sign", []byte("K-09 interrupted host claim")); !errors.Is(err, store.ErrClaimed) {
		t.Fatal("killed host's committed claim was recycled", err)
	}
	release, err := records.Claim(f.ctx, [32]byte{0x09, 0x24}, "sign", []byte("K-09 fresh agreed claim"))
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err = records.LoadKey(f.ctx); err != nil {
		t.Fatal("key lost after process death:", err)
	}
	f.roots[operator] = reopened
	f.nodes[operator].root = reopened
	t.Log("K09-CRASH: SIGKILL while claim active; old claim terminal; fresh claim and key reload succeed")
	return records
}

func TestK09CrashProcessHelper(t *testing.T) {
	path := os.Getenv("FROST_K09_CRASH_HELPER")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config k09CrashConfig
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	root, err := store.Open(context.Background(), store.Config{Keystore: config.Keystore, StorageID: config.StorageID, Key: [32]byte{17}, Fence: k09Fence{config.FenceURL}})
	if err != nil {
		t.Fatal(err)
	}
	records, err := root.Scope(config.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = records.LoadKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = records.Claim(context.Background(), k09CrashClaim, "sign", []byte("K-09 interrupted host claim")); err != nil {
		t.Fatal(err)
	}
	_, _ = os.Stdout.WriteString("K09-ACTIVE-CLAIM\n")
	for {
		time.Sleep(time.Hour)
	} // Deliberately never release/Close or run teardown.
}
