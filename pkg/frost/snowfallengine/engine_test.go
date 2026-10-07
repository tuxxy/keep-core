package snowfallengine_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// These providers are test-only. Memory is not durable storage. Local queues
// are not production authentication. A test receipt is not chain finality.
type testStore struct {
	mu      sync.Mutex
	records map[[32]byte][]byte
	slots   map[string][32]byte
	reads   int
}

func newStore() *testStore {
	return &testStore{records: map[[32]byte][]byte{}, slots: map[string][32]byte{}}
}
func (s *testStore) Put(_ context.Context, w frost.Write) (frost.PutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.Kind == "lock" {
		if w.Slot == nil {
			return 0, errors.New("missing checked slot")
		}
		key := fmt.Sprintf("%d/%x/%s", w.Seat, w.Slot.Attempt, w.Slot.Kind)
		if id, ok := s.slots[key]; ok && id != w.ID {
			return frost.Conflict, nil
		}
		s.slots[key] = w.ID
	} else if w.Slot != nil {
		return 0, errors.New("record has lock slot")
	}
	if previous, ok := s.records[w.ID]; ok {
		if bytes.Equal(previous, w.Payload) {
			return frost.Identical, nil
		}
		return frost.Conflict, nil
	}
	s.records[w.ID] = slices.Clone(w.Payload)
	return frost.Durable, nil
}
func (s *testStore) Read(_ context.Context, id [32]byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	b, ok := s.records[id]
	if !ok {
		return nil, errors.New("missing test record")
	}
	return slices.Clone(b), nil
}

type testNetwork struct {
	mu    sync.Mutex
	nodes [][]uint16
	inbox []chan frost.Incoming
	seen  []map[[32]byte]bool
}

func newNetwork(nodes [][]uint16) *testNetwork {
	n := &testNetwork{nodes: nodes}
	for range nodes {
		n.inbox = append(n.inbox, make(chan frost.Incoming, 512))
		n.seen = append(n.seen, map[[32]byte]bool{})
	}
	return n
}

type testEndpoint struct {
	n    *testNetwork
	node int
}

func (e testEndpoint) send(ctx context.Context, sender, recipient uint16, b []byte) error {
	e.n.mu.Lock()
	defer e.n.mu.Unlock()
	if !slices.Contains(e.n.nodes[e.node], sender) {
		return errors.New("unowned test seat")
	}
	for node, seats := range e.n.nodes {
		deliver := false
		for _, seat := range seats {
			if recipient == 0 && seat != sender || recipient == seat {
				deliver = true
			}
		}
		if !deliver {
			continue
		}
		id := sha256.Sum256(append([]byte(fmt.Sprintf("%d/%d/", sender, recipient)), b...))
		if e.n.seen[node][id] {
			continue
		}
		select {
		case e.n.inbox[node] <- frost.Incoming{AuthenticatedSender: sender, Message: slices.Clone(b)}:
			e.n.seen[node][id] = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (e testEndpoint) Broadcast(c context.Context, s uint16, b []byte) error {
	return e.send(c, s, 0, b)
}
func (e testEndpoint) SendPrivate(c context.Context, s, r uint16, b []byte) error {
	return e.send(c, s, r, b)
}
func (e testEndpoint) Receive(c context.Context) (frost.Incoming, error) {
	select {
	case m := <-e.n.inbox[e.node]:
		return m, nil
	case <-c.Done():
		return frost.Incoming{}, c.Err()
	}
}

type testAcceptance struct {
	mu      sync.Mutex
	seen    []frost.Candidate
	entered chan struct{}
	gate    <-chan struct{}
	mode    string
}

func (a *testAcceptance) WaitCandidateAcceptance(ctx context.Context, c frost.Candidate) (frost.Receipt, error) {
	a.mu.Lock()
	a.seen = append(a.seen, c)
	if a.entered != nil {
		close(a.entered)
	}
	a.mu.Unlock()
	if a.gate != nil {
		select {
		case <-a.gate:
		case <-ctx.Done():
			return frost.Receipt{}, ctx.Err()
		}
	}
	receipt := frost.Receipt{Epoch: c.Epoch, Descriptor: c.Descriptor}
	switch a.mode {
	case "wrong-epoch":
		receipt.Epoch++
	case "wrong-descriptor":
		receipt.Descriptor[0] ^= 1
	case "reject":
		return frost.Receipt{}, errors.New("test acceptance refused")
	}
	return receipt, nil
}
func (a *testAcceptance) snapshot() []frost.Candidate {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.seen)
}

func pinnedWorker(t *testing.T) snowfallengine.WorkerConfig {
	t.Helper()
	path := os.Getenv("SNOWFALL_WORKER")
	pinText := os.Getenv("SNOWFALL_WORKER_SHA256")
	if path == "" && pinText == "" {
		t.Skip("real worker opt-in requires SNOWFALL_WORKER and SNOWFALL_WORKER_SHA256")
	}
	if testing.Short() {
		t.Skip("real-worker integration is excluded by -short")
	}
	pin, err := hex.DecodeString(pinText)
	if err != nil || len(pin) != 32 {
		t.Fatal("explicit worker SHA-256 is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("configured worker is missing")
	}
	actual := sha256.Sum256(raw)
	if !bytes.Equal(pin, actual[:]) {
		t.Fatal("configured worker does not match the requested pin")
	}
	// Give this run a unique executable path, so process checks cannot mistake
	// another concurrent integration run for this test's worker.
	copyPath := filepath.Join(t.TempDir(), "snowfall-pinned")
	if err := os.WriteFile(copyPath, raw, 0700); err != nil {
		t.Fatal(err)
	}
	return snowfallengine.WorkerConfig{Path: copyPath, SHA256: actual, CommandTimeoutMs: 3000, ShutdownTimeoutMs: 1000}
}
func newEngine(t *testing.T, cfg snowfallengine.WorkerConfig) *snowfallengine.Engine {
	t.Helper()
	e, err := snowfallengine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func request(seats []uint16) frost.DKGRequest {
	return frost.DKGRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: seats, Attempt: frost.Attempt{Channel: []byte("K01/test-only/dkg"), Session: []byte("local-2-of-3"), StartBlock: 100}}
}

func processes(t *testing.T, path string) []string {
	t.Helper()
	raw, err := exec.Command("/bin/ps", "-axo", "pid=,command=").Output()
	if err != nil {
		t.Fatal("cannot inspect worker process exit:", err)
	}
	var found []string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] == path {
			found = append(found, fields[0])
		}
	}
	return found
}

func runReal(t *testing.T, nodes [][]uint16) {
	cfg := pinnedWorker(t)
	e := newEngine(t, cfg)
	net := newNetwork(nodes)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	stores := make([]*testStore, len(nodes))
	acceptors := make([]*testAcceptance, len(nodes))
	providers := make([]frost.Providers, len(nodes))
	keys := make([]frost.KeyReady, len(nodes))
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, seats := range nodes {
		stores[i] = newStore()
		acceptors[i] = &testAcceptance{entered: make(chan struct{}), gate: gate}
		providers[i] = frost.Providers{Store: stores[i], Transport: testEndpoint{net, i}, Acceptance: acceptors[i]}
		wg.Add(1)
		go func(i int, seats []uint16) {
			defer wg.Done()
			keys[i], errs[i] = e.DKG(ctx, request(seats), providers[i])
		}(i, seats)
	}
	for _, a := range acceptors {
		select {
		case <-a.entered:
		case <-ctx.Done():
			release()
			wg.Wait()
			t.Fatal("real DKG did not reach candidate acceptance")
		}
	}
	if pids := processes(t, cfg.Path); len(pids) != len(nodes) {
		release()
		wg.Wait()
		t.Fatalf("want one worker per node; got %d for %d nodes", len(pids), len(nodes))
	}
	release()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
		seen := acceptors[i].snapshot()
		if len(seen) != 1 || seen[0].OutputKey != keys[i].Candidate.OutputKey || seen[0].Descriptor != keys[i].Candidate.Descriptor || seen[0].Profile != frost.ApprovedProfile {
			t.Fatal("candidate callback mismatch")
		}
		if len(keys[i].LocalReferences) != len(nodes[i]) {
			t.Fatal("lost local seat")
		}
		if keys[i].Candidate.OutputKey != keys[0].Candidate.OutputKey || keys[i].Candidate.Descriptor != keys[0].Candidate.Descriptor {
			t.Fatal("nodes disagree")
		}
	}
	if len(processes(t, cfg.Path)) != 0 {
		t.Fatal("DKG worker still running after return")
	}
	// A separate attempt and new worker loads the same opaque records.
	net = newNetwork(nodes)
	selected := []uint16{1, 3}
	if len(nodes[0]) > 1 {
		selected = []uint16{1, 2, 3}
	}
	firstSigner := -1
	message := sha256.Sum256([]byte("K-01 exact 32-byte local signing message"))
	signatures := make([][64]byte, len(nodes))
	for i, seats := range nodes {
		active := false
		for _, seat := range seats {
			if slices.Contains(selected, seat) {
				active = true
			}
		}
		if !active {
			continue
		}
		if firstSigner < 0 {
			firstSigner = i
		}
		providers[i].Transport = testEndpoint{net, i}
		wg.Add(1)
		go func(i int, seats []uint16) {
			defer wg.Done()
			r := request(seats)
			signatures[i], errs[i] = e.Sign(ctx, frost.SigningRequest{Group: r.Group, LocalSeats: seats, Selected: selected, Key: keys[i], Attempt: frost.Attempt{Channel: []byte("K01/test-only/sign"), Session: []byte("fresh-signing-attempt"), StartBlock: 101}, Message: message}, providers[i])
		}(i, seats)
	}
	wg.Wait()
	observedReads := 0
	for i, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
		expectedReads := 0
		for _, seat := range nodes[i] {
			if slices.Contains(selected, seat) {
				expectedReads += 2
			}
		}
		if stores[i].reads != expectedReads {
			t.Fatal("incorrect local key reload count")
		}
		observedReads += stores[i].reads
		if expectedReads == 0 {
			continue
		}
		if signatures[i] != signatures[firstSigner] {
			t.Fatal("signature disagreement")
		}

	}
	publicKey, err := schnorr.ParsePubKey(keys[0].Candidate.OutputKey[:])
	if err != nil {
		t.Fatal(err)
	}
	signature, err := schnorr.ParseSignature(signatures[firstSigner][:])
	if err != nil || !signature.Verify(message[:], publicKey) {
		t.Fatal("independent BIP340 verification failed")
	}
	changed := message
	changed[0] ^= 1
	if signature.Verify(changed[:], publicKey) {
		t.Fatal("signature verifies for wrong message")
	}
	if len(processes(t, cfg.Path)) != 0 {
		t.Fatal("signing worker still running after return")
	}
	t.Logf("TEST ONLY: nodes=%v selected=%v threshold=2 epoch=77 candidate_callbacks=%d key_loads=%d clean_exit=true", nodes, selected, len(nodes), observedReads)
	t.Logf("PUBLIC WITNESS: worker_sha256=%x descriptor=%x output_key=%x message=%x signature=%x", cfg.SHA256, keys[0].Candidate.Descriptor, keys[0].Candidate.OutputKey, message, signatures[firstSigner])
}
func TestRealWorkerDKGAndSign(t *testing.T)         { runReal(t, [][]uint16{{1}, {2}, {3}}) }
func TestRealWorkerMultipleLocalSeats(t *testing.T) { runReal(t, [][]uint16{{1, 2}, {3}}) }

func expectCategory(t *testing.T, err error, want frost.Category) {
	t.Helper()
	var got *frost.Error
	if !errors.As(err, &got) || got.Category != want {
		t.Fatalf("want %s, got %v", want, err)
	}
	if len(got.Detail) > 1024 {
		t.Fatal("unbounded error")
	}
}

// The test executable can act as a broken worker or an environment probe.
// These modes exercise the adapter's public operation path, not cryptography.
func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if strings.HasPrefix(name, "frost-env-") {
		want := []string{}
		if name == "frost-env-exact" {
			want = []string{"ALLOWED=only"}
		}
		env := os.Environ()
		slices.Sort(env)
		if !slices.Equal(env, want) {
			os.Exit(8)
		}
		if err := os.WriteFile(os.Args[0]+".ok", []byte("environment matched"), 0600); err != nil {
			os.Exit(9)
		}
		_, _ = os.Stdout.Write([]byte{0, 0, 0, 1, 255})
		os.Exit(0)
	}
	if name == "frost-malformed" {
		_, _ = os.Stdout.Write([]byte{0, 0, 0, 9, 255})
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func helperConfig(t *testing.T, name string) snowfallengine.WorkerConfig {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(self, path); err != nil {
		t.Fatal(err)
	}
	return snowfallengine.WorkerConfig{Path: path, SHA256: sha256.Sum256(raw), CommandTimeoutMs: 1000, ShutdownTimeoutMs: 3000}
}
func simpleProviders() frost.Providers {
	return frost.Providers{Store: newStore(), Transport: testEndpoint{newNetwork([][]uint16{{1, 2, 3}}), 0}, Acceptance: &testAcceptance{}}
}
func TestWorkerPinAndEnvironment(t *testing.T) {
	t.Setenv("KEEP_ETHEREUM_PASSWORD", "sentinel-not-for-worker")
	for _, mode := range []string{"empty", "exact"} {
		t.Run(mode, func(t *testing.T) {
			cfg := helperConfig(t, "frost-env-"+mode)
			if mode == "exact" {
				cfg.Env = []string{"ALLOWED=only"}
			}
			e := newEngine(t, cfg)
			if len(cfg.Env) > 0 {
				cfg.Env[0] = "CHANGED=bad"
			}
			_, err := e.DKG(context.Background(), request([]uint16{1, 2, 3}), simpleProviders())
			expectCategory(t, err, frost.ProtocolFailure)
			if _, err := os.Stat(cfg.Path + ".ok"); err != nil {
				t.Fatal("environment probe did not match explicit configuration")
			}
			if len(processes(t, cfg.Path)) != 0 {
				t.Fatal("probe still running")
			}
		})
	}
	t.Run("wrong hash", func(t *testing.T) {
		cfg := helperConfig(t, "frost-env-empty")
		cfg.SHA256[0] ^= 1
		_, err := newEngine(t, cfg).DKG(context.Background(), request([]uint16{1, 2, 3}), simpleProviders())
		expectCategory(t, err, frost.DependencyFailure)
		if _, err := os.Stat(cfg.Path + ".ok"); !os.IsNotExist(err) {
			t.Fatal("wrong-pin worker started")
		}
	})
	t.Run("missing worker", func(t *testing.T) {
		cfg := snowfallengine.WorkerConfig{Path: filepath.Join(t.TempDir(), "missing"), SHA256: [32]byte{1}}
		_, err := newEngine(t, cfg).DKG(context.Background(), request([]uint16{1}), simpleProviders())
		expectCategory(t, err, frost.DependencyFailure)
	})
}
func TestMalformedInput(t *testing.T) {
	cfg := helperConfig(t, "frost-malformed")
	e := newEngine(t, cfg)
	r := request([]uint16{1})
	r.Group.Roster = []uint16{2, 1, 3}
	_, err := e.DKG(context.Background(), r, simpleProviders())
	expectCategory(t, err, frost.InvalidRequest)
	_, err = e.DKG(context.Background(), request([]uint16{1}), simpleProviders())
	expectCategory(t, err, frost.ProtocolFailure)
	if len(processes(t, cfg.Path)) != 0 {
		t.Fatal("malformed worker still running")
	}
}
func TestRealWorkerFalseAcceptance(t *testing.T) {
	cfg := pinnedWorker(t)
	e := newEngine(t, cfg)
	for _, mode := range []string{"wrong-epoch", "wrong-descriptor", "reject"} {
		t.Run(mode, func(t *testing.T) {
			p := simpleProviders()
			p.Acceptance = &testAcceptance{mode: mode}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := e.DKG(ctx, request([]uint16{1, 2, 3}), p)
			expectCategory(t, err, frost.DependencyFailure)
			if len(processes(t, cfg.Path)) != 0 {
				t.Fatal("worker survived false acceptance")
			}
		})
	}
}
func TestRealWorkerCancellation(t *testing.T) {
	cfg := pinnedWorker(t)
	e := newEngine(t, cfg)
	p := simpleProviders()
	a := &testAcceptance{entered: make(chan struct{}), gate: make(chan struct{})}
	p.Acceptance = a
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.DKG(ctx, request([]uint16{1, 2, 3}), p); done <- err }()
	select {
	case <-a.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("acceptance wait not reached")
	}
	if len(processes(t, cfg.Path)) != 1 {
		t.Fatal("expected one multi-seat worker")
	}
	cancel()
	select {
	case err := <-done:
		expectCategory(t, err, frost.Cancelled)
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	if len(processes(t, cfg.Path)) != 0 {
		t.Fatal("cancelled worker still running")
	}
}
