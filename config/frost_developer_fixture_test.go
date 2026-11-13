package config

// Chaosnet M2 acceptance (tux, 2026-11-13): keep-core starts as a FROST node
// from TOML on the --developer network only. Shared fixture of the config
// tests in frost_developer_test.go and frost_developer_fence_test.go.
//
// FIXTURE: the worker file is a placeholder (test bytes, never run); its
// pinned SHA-256 is the digest of those bytes. The store key is a test value
// rendered into a key file as a lab would render it per node. Nothing here is
// a real key, share, nonce or worker record.
//
// Settings pinned by these tests (names are case-insensitive, as every viper
// key):
//
//	[developer]
//	FrostWalletRegistryAddress = "0x..."  # the FROST registry, as stock
//	                                      # developer contract addresses
//	[tbtc.frost]
//	WorkerPath   = "/abs/worker"          # snowfallengine.WorkerConfig.Path
//	WorkerSHA256 = "<64 hex digits>"      # snowfallengine.WorkerConfig.SHA256
//	Keystore     = "/abs/dir"             # store.Config.Keystore
//	KeyFile      = "/abs/key"             # store.Config.Key, read from the file
//	FenceFile    = "/abs/fence"           # store.Config.Fence, a disk fence
//
// Key file encoding: exactly 64 hexadecimal digits (no 0x prefix),
// optionally followed by one "\n". A provisioned fence file exists; an empty
// one is the zero head (a newly provisioned store).

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	golog "github.com/ipfs/go-log/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// devStoreKey is the TEST store key. Its bytes are distinctive (no zero, no
// repeated runs), so a leak in any common rendering is detectable.
var devStoreKey = [32]byte{
	0xa7, 0x3c, 0xe1, 0x5b, 0x92, 0x4f, 0xd8, 0x16,
	0x6e, 0xb3, 0x29, 0xf4, 0x87, 0x1a, 0xc5, 0x58,
	0x3d, 0x9e, 0x61, 0xab, 0x14, 0xf7, 0x82, 0x4c,
	0xd9, 0x35, 0x6a, 0xbe, 0x23, 0x97, 0xe8, 0x71,
}

const devRegistry = "0x5f3b1c2d4e6a7980a1b2c3d4e5f60718293a4b5c"

// devFrost is one node's rendered FROST settings, all absolute, under a
// fresh test directory.
type devFrost struct {
	dir      string
	worker   string
	workerSH [32]byte
	keystore string
	keyFile  string
	fence    string
	storage  string
}

func newDevFrost(t *testing.T) *devFrost {
	t.Helper()
	dir := t.TempDir()
	d := &devFrost{
		dir:      dir,
		worker:   filepath.Join(dir, "snowfall-worker"),
		keystore: filepath.Join(dir, "frost-keystore"),
		keyFile:  filepath.Join(dir, "frost-store.key"),
		fence:    filepath.Join(dir, "frost.fence"),
		storage:  filepath.Join(dir, "storage"),
	}
	placeholder := []byte("FIXTURE: placeholder worker bytes, never executed\n")
	if err := os.WriteFile(d.worker, placeholder, 0700); err != nil {
		t.Fatal(err)
	}
	d.workerSH = sha256.Sum256(placeholder)
	if err := os.WriteFile(d.keyFile, []byte(hex.EncodeToString(devStoreKey[:])+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.fence, nil, 0600); err != nil {
		t.Fatal(err)
	}
	return d
}

// frostSettings are the [tbtc.frost] settings in file order.
func (d *devFrost) frostSettings() [][2]string {
	return [][2]string{
		{"WorkerPath", fmt.Sprintf("%q", d.worker)},
		{"WorkerSHA256", fmt.Sprintf("%q", hex.EncodeToString(d.workerSH[:]))},
		{"Keystore", fmt.Sprintf("%q", d.keystore)},
		{"KeyFile", fmt.Sprintf("%q", d.keyFile)},
		{"FenceFile", fmt.Sprintf("%q", d.fence)},
	}
}

// toml renders a complete start configuration. override replaces a
// [tbtc.frost] or registry value (raw TOML); omit drops a setting; a nil
// frost map leaves out the FROST settings (registry included) entirely.
func (d *devFrost) toml(withFrost bool, override map[string]string, omit ...string) string {
	skip := func(k string) bool {
		for _, o := range omit {
			if strings.EqualFold(o, k) {
				return true
			}
		}
		return false
	}
	value := func(k, v string) string {
		if o, ok := override[k]; ok {
			return o
		}
		return v
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[ethereum]\nURL = \"ws://127.0.0.1:1\"\nKeyFile = %q\nKeyFilePassword = \"test-only password\"\n\n", filepath.Join(d.dir, "absent-operator-keyfile"))
	b.WriteString("[bitcoin.electrum]\nURL = \"tcp://127.0.0.1:1\"\n\n")
	b.WriteString("[network]\nPort = 3919\n\n")
	fmt.Fprintf(&b, "[storage]\nDir = %q\n\n", d.storage)
	b.WriteString("[developer]\nWalletRegistryAddress = \"0x143ba24e66fce8bca22f7d739f9a932c519b1c76\"\n")
	if withFrost && !skip("FrostWalletRegistryAddress") {
		fmt.Fprintf(&b, "FrostWalletRegistryAddress = %s\n", value("FrostWalletRegistryAddress", fmt.Sprintf("%q", devRegistry)))
	}
	if withFrost {
		b.WriteString("\n[tbtc.frost]\n")
		for _, kv := range d.frostSettings() {
			if skip(kv[0]) {
				continue
			}
			fmt.Fprintf(&b, "%s = %s\n", kv[0], value(kv[0], kv[1]))
		}
	}
	return b.String()
}

// networkFlags is the client's network flag set (cmd initGlobalNetworkFlags)
// with the given arguments parsed, e.g. "--developer".
func networkFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("start", pflag.ContinueOnError)
	fs.Bool("mainnet", false, "")
	fs.Bool("testnet", false, "")
	fs.Bool("developer", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

// readDev reads content as the start command does (StartCmdCategories) with
// the given network flags. Viper is global: these tests do not run in
// parallel.
func readDev(t *testing.T, d *devFrost, content string, args ...string) (*Config, error) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := filepath.Join(d.dir, fmt.Sprintf("node-%d.toml", len(content)))
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Config{}
	err := c.ReadConfig(path, networkFlags(t, args...), StartCmdCategories...)
	return c, err
}

// leakForms are renderings of secret whose presence in any text means the
// secret leaked: 8-byte windows in lower/upper hex and raw bytes, and 4-byte
// windows as Go prints a byte array with %v and %#v.
func leakForms(secret []byte) []string {
	var forms []string
	for i := 0; i+8 <= len(secret); i += 4 {
		w := secret[i : i+8]
		forms = append(forms, hex.EncodeToString(w), strings.ToUpper(hex.EncodeToString(w)), string(w))
	}
	for i := 0; i+4 <= len(secret); i += 2 {
		w := secret[i : i+4]
		dec := make([]string, len(w))
		gox := make([]string, len(w))
		for j, b := range w {
			dec[j] = fmt.Sprint(b)
			gox[j] = fmt.Sprintf("0x%x", b)
		}
		forms = append(forms, strings.Join(dec, " "), strings.Join(gox, ", "))
	}
	return forms
}

// assertNoLeak fails, without printing the secret, when text contains any of
// its renderings.
func assertNoLeak(t *testing.T, label, text string, secret []byte) {
	t.Helper()
	for _, form := range leakForms(secret) {
		if form != "" && strings.Contains(text, form) {
			t.Errorf("%s contains the store key or key-file contents (rendering withheld)", label)
			return
		}
	}
}

// captureLogs records every log line (all subsystems, debug and up) and
// everything written to the process's stdout and stderr until the returned
// function is called; it returns the captured text.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	stdout, stderr := os.Stdout, os.Stderr
	var streams sync.WaitGroup
	var streamText [2]strings.Builder
	var writers [2]*os.File
	for i := range writers {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		writers[i] = w
		streams.Add(1)
		go func(i int, r *os.File) {
			defer streams.Done()
			b, _ := io.ReadAll(r)
			r.Close()
			streamText[i].Write(b)
		}(i, r)
	}
	os.Stdout, os.Stderr = writers[0], writers[1]
	previous := golog.GetConfig().Level
	golog.SetAllLoggers(golog.LevelDebug)
	reader := golog.NewPipeReader(golog.PipeLevel(golog.LevelDebug))
	var (
		mu   sync.Mutex
		text strings.Builder
		done = make(chan struct{})
	)
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			mu.Lock()
			text.WriteString(scanner.Text())
			text.WriteByte('\n')
			mu.Unlock()
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	var once sync.Once
	stop := func() string {
		once.Do(func() {
			os.Stdout, os.Stderr = stdout, stderr
			for _, w := range writers {
				_ = w.Close()
			}
			streams.Wait()
			_ = reader.Close()
			<-done
			golog.SetAllLoggers(previous)
			mu.Lock()
			text.WriteString(streamText[0].String())
			text.WriteString(streamText[1].String())
			mu.Unlock()
		})
		mu.Lock()
		defer mu.Unlock()
		return text.String()
	}
	t.Cleanup(func() { stop() })
	return stop
}
