package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// memoryAuthority is TEST ONLY. It outlives reopened stores inside this test,
// but cannot survive host death and is not a deployable fencing provider.
type memoryAuthority struct {
	mu     sync.Mutex
	point  store.Point
	leased bool
}
type memoryLease struct{ a *memoryAuthority }

func (a *memoryAuthority) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.leased {
		return nil, store.ErrBusy
	}
	a.leased = true
	return memoryLease{a}, nil
}
func (l memoryLease) Head(context.Context) (store.Point, error) {
	l.a.mu.Lock()
	defer l.a.mu.Unlock()
	return l.a.point, nil
}
func (l memoryLease) Advance(_ context.Context, before, after store.Point) error {
	l.a.mu.Lock()
	defer l.a.mu.Unlock()
	if l.a.point != before {
		return store.ErrQuarantined
	}
	l.a.point = after
	return nil
}
func (l memoryLease) Close() error {
	l.a.mu.Lock()
	defer l.a.mu.Unlock()
	l.a.leased = false
	return nil
}

type notifyTransport struct {
	*Transport
	sent chan struct{}
	once sync.Once
}

func (n *notifyTransport) Broadcast(ctx context.Context, seat uint16, b []byte) error {
	e := n.Transport.Broadcast(ctx, seat, b)
	n.once.Do(func() { close(n.sent) })
	return e
}

type testFinality struct{}

func (testFinality) WaitCandidateAcceptance(_ context.Context, c frost.Candidate) (frost.Receipt, error) {
	return frost.Receipt{Epoch: c.Epoch, Descriptor: c.Descriptor}, nil
}

type countedJournal struct {
	frost.RecoveryJournal
	reads atomic.Int32
}

func (j *countedJournal) Read(ctx context.Context, id [32]byte) ([]byte, error) {
	j.reads.Add(1)
	return j.RecoveryJournal.Read(ctx, id)
}

type countedProvider struct {
	net.Provider
	mu       sync.Mutex
	channels map[string]net.BroadcastChannel
}

func (p *countedProvider) BroadcastChannelFor(name string) (net.BroadcastChannel, error) {
	ch, e := p.Provider.BroadcastChannelFor(name)
	if e != nil {
		return nil, e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if old, ok := p.channels[name]; ok && old != ch {
		return nil, errors.New("libp2p did not reuse channel")
	}
	p.channels[name] = ch
	return ch, nil
}

func realWorkerConfig(t *testing.T) snowfallengine.WorkerConfig {
	t.Helper()
	path, hash := os.Getenv("SNOWFALL_WORKER"), os.Getenv("SNOWFALL_WORKER_SHA256")
	if testing.Short() || (path == "" && hash == "") {
		t.Skip("explicit real-worker opt-in required; this is not K-02 acceptance")
	}
	raw, e := hex.DecodeString(hash)
	if e != nil || len(raw) != 32 || !filepath.IsAbs(path) {
		t.Fatal("invalid explicit worker pin")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	actual := sha256.Sum256(b)
	if !reflect.DeepEqual(actual[:], raw) {
		t.Fatal("worker hash mismatch")
	}
	return snowfallengine.WorkerConfig{Path: path, SHA256: actual}
}
func requireNoWorkers(t *testing.T, path string) {
	t.Helper()
	out, e := exec.Command("/bin/ps", "-axo", "command=").Output()
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) > 0 && parts[0] == path {
			t.Fatal("worker survived operation")
		}
	}
}

func TestRealWorkerDurableAuthenticatedReload(t *testing.T) {
	cfg := realWorkerConfig(t)
	for name, nodes := range map[string][][]uint16{"three-operators": {{1}, {2}, {3}}, "multiple-local-seats": {{1, 2}, {3}}} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			signing := local_v1.Connect(3, 3).Signing()
			privs := make([]*operator.PrivateKey, len(nodes))
			pubs := make([]*operator.PublicKey, len(nodes))
			addresses := make([]chain.Address, 3)
			for i, seats := range nodes {
				var e error
				privs[i], pubs[i], e = operator.GenerateKeyPair(local_v1.DefaultCurve)
				if e != nil {
					t.Fatal(e)
				}
				addr, e := signing.PublicKeyToAddress(pubs[i])
				if e != nil {
					t.Fatal(e)
				}
				for _, seat := range seats {
					addresses[seat-1] = addr
				}
			}
			membership := group.NewMembershipValidator(&testutils.MockLogger{}, addresses, signing)
			providers := make([]*countedProvider, len(nodes))
			var bootstrap []string
			for i := range nodes {
				p, e := libp2p.Connect(ctx, libp2p.Config{Peers: bootstrap}, privs[i], firewall.Disabled, retransmission.NewTimeTicker(ctx, 100*time.Millisecond))
				if e != nil {
					t.Fatal(e)
				}
				providers[i] = &countedProvider{Provider: p, channels: map[string]net.BroadcastChannel{}}
				if i == 0 {
					for _, addr := range p.ConnectionManager().AddrStrings() {
						if strings.Contains(addr, "/ip4/127.0.0.1/") {
							bootstrap = []string{addr}
							break
						}
					}
					if len(bootstrap) == 0 {
						t.Fatal("no loopback bootstrap endpoint")
					}
				}
			}
			domain := frost.Domain{Network: "K02/" + name, Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 77}
			configs := make([]store.Config, len(nodes))
			roots := make([]*store.Store, len(nodes))
			journals := make([]*countedJournal, len(nodes))
			engines := make([]*snowfallengine.Engine, len(nodes))
			openStore := func(i int) {
				root, e := store.Open(ctx, configs[i])
				if e != nil {
					t.Fatal(e)
				}
				roots[i] = root
				t.Cleanup(func() {
					if e := root.Close(); e != nil {
						t.Error(e)
					}
				})
				scoped, e := root.Scope(domain)
				if e != nil {
					t.Fatal(e)
				}
				journals[i] = &countedJournal{RecoveryJournal: scoped}
				engines[i], e = snowfallengine.NewGuarded(cfg, journals[i])
				if e != nil {
					t.Fatal(e)
				}
			}
			for i := range nodes {
				configs[i] = store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{byte(i + 1)}, Key: [32]byte{42}, Fence: &memoryAuthority{}}
				openStore(i)
			}
			makeTransports := func(purpose string, session byte, start uint64) []*Transport {
				a, e := domain.NewAttempt(purpose, [32]byte{session}, start)
				if e != nil {
					t.Fatal(e)
				}
				ts := make([]*Transport, len(nodes))
				for i, seats := range nodes {
					ts[i], e = New(ctx, Config{RecoveryJournal: journals[i], Domain: domain, Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: seats, Roster: []uint16{1, 2, 3}, LocalPublicKey: operator.MarshalUncompressed(pubs[i]), Membership: membership, Network: providers[i]})
					if e != nil {
						t.Fatal(e)
					}
					t.Cleanup(ts[i].Close)
				}
				return ts
			}
			ts := makeTransports("dkg", 1, 100)
			keys := make([]frost.KeyReady, len(nodes))
			errs := make([]error, len(nodes))
			var wg sync.WaitGroup
			// Invalid calls must not burn the domain before the corrected DKG starts.
			for i, seats := range nodes {
				a, _ := ts[i].Binding()
				r := frost.DKGRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: seats, Attempt: a}
				if _, e := engines[i].DKG(ctx, r, frost.Providers{Transport: ts[i]}); e == nil {
					t.Fatal("missing acceptance accepted")
				}
				r.Participants = []uint16{1, 1, 3}
				if _, e := engines[i].DKG(ctx, r, frost.Providers{Transport: ts[i], Acceptance: testFinality{}}); e == nil {
					t.Fatal("malformed participants accepted")
				}
			}
			requireNoWorkers(t, cfg.Path)
			for i, seats := range nodes {
				wg.Add(1)
				go func(i int, seats []uint16) {
					defer wg.Done()
					a, _ := ts[i].Binding()
					keys[i], errs[i] = engines[i].DKG(ctx, frost.DKGRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: seats, Attempt: a}, frost.Providers{Transport: ts[i], Acceptance: testFinality{}})
				}(i, seats)
			}
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatalf("DKG node %d: %v", i, e)
				}
				ts[i].Close()
				if keys[i].Candidate.Descriptor != keys[0].Candidate.Descriptor || keys[i].Candidate.OutputKey != keys[0].Candidate.OutputKey {
					t.Fatal("candidate mismatch")
				}
				if e = roots[i].Close(); e != nil {
					t.Fatal(e)
				}
				openStore(i)
				loaded, e := journals[i].LoadKey(ctx)
				if e != nil || !reflect.DeepEqual(loaded, keys[i]) {
					t.Fatal("accepted key did not survive reopen")
				}
			}
			// All peers restart, then recover from retained authenticated evidence.
			for i := range nodes {
				a, _ := domain.NewAttempt("dkg", [32]byte{1}, 100)
				tr, e := New(ctx, Config{RecoveryJournal: journals[i], RecoveryOnly: true, Domain: domain, Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: nodes[i], Roster: []uint16{1, 2, 3}, LocalPublicKey: operator.MarshalUncompressed(pubs[i]), Membership: membership, Network: providers[i]})
				if e != nil {
					t.Fatal(e)
				}
				recovered, e := engines[i].RecoverPending(ctx, frost.Providers{Transport: tr, Acceptance: testFinality{}})
				tr.Close()
				journals[i].reads.Store(0)
				if e != nil || !reflect.DeepEqual(recovered, keys[i]) {
					t.Fatalf("libp2p recovery node %d: %v", i, e)
				}
			}
			requireNoWorkers(t, cfg.Path)
			ts = makeTransports("sign", 2, 101)
			// Kill the first signing operation after its worker releases a commitment.
			// No peer runs a worker for this attempt, so it cannot complete normally.
			a, _ := ts[0].Binding()
			aborted := frost.SigningRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: nodes[0], Selected: []uint16{1, 2, 3}, Attempt: a, Key: keys[0], Message: sha256.Sum256([]byte("aborted intent"))}
			notified := &notifyTransport{Transport: ts[0], sent: make(chan struct{})}
			abortCtx, abort := context.WithCancel(ctx)
			abortResult := make(chan error, 1)
			go func() {
				_, e := engines[0].Sign(abortCtx, aborted, frost.Providers{Transport: notified, Acceptance: testFinality{}})
				abortResult <- e
			}()
			select {
			case <-notified.sent:
			case e := <-abortResult:
				t.Fatalf("worker ended before commitment: %v", e)
			case <-ctx.Done():
				t.Fatal("no signing commitment")
			}
			killOwnedWorker(t, cfg.Path)
			select {
			case e := <-abortResult:
				if e == nil {
					t.Fatal("killed worker succeeded")
				}
			case <-ctx.Done():
				t.Fatal("killed worker did not return")
			}
			abort()
			requireNoWorkers(t, cfg.Path)
			if e := roots[0].Close(); e != nil {
				t.Fatal(e)
			}
			openStore(0)
			if _, e := engines[0].Sign(ctx, aborted, frost.Providers{Transport: ts[0], Acceptance: testFinality{}}); e == nil {
				t.Fatal("aborted signing attempt resumed after reopen")
			}
			for _, tr := range ts {
				tr.Close()
			}
			ts = makeTransports("sign", 3, 102)
			message := sha256.Sum256([]byte("K02 exact message after durable reopen"))
			sigs := make([][64]byte, len(nodes))
			requests := make([]frost.SigningRequest, len(nodes))
			for i, seats := range nodes {
				a, _ := ts[i].Binding()
				requests[i] = frost.SigningRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77}, LocalSeats: seats, Selected: []uint16{1, 2, 3}, Attempt: a, Key: keys[i], Message: message}
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					sigs[i], errs[i] = engines[i].Sign(ctx, requests[i], frost.Providers{Transport: ts[i], Acceptance: testFinality{}})
				}(i)
			}
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatalf("sign node %d: %v", i, e)
				}
				if sigs[i] != sigs[0] {
					t.Fatal("signature mismatch")
				}
				if journals[i].reads.Load() != int32(2*len(nodes[i])) {
					t.Fatal("worker did not reload every local key record")
				}
			}
			pub, e := schnorr.ParsePubKey(keys[0].Candidate.OutputKey[:])
			if e != nil {
				t.Fatal(e)
			}
			sig, e := schnorr.ParseSignature(sigs[0][:])
			if e != nil || !sig.Verify(message[:], pub) {
				t.Fatal("independent BIP340 verification failed")
			}
			wrong := message
			wrong[0] ^= 1
			if sig.Verify(wrong[:], pub) {
				t.Fatal("signature accepted changed message")
			}
			if _, e = engines[0].Sign(ctx, requests[0], frost.Providers{Transport: ts[0], Acceptance: testFinality{}}); e == nil {
				t.Fatal("finished signing claim reused")
			}
			for _, tr := range ts {
				tr.Close()
			}
			// Repeated fresh attempts must reuse libp2p's provider-owned sign channel.
			for retry := byte(4); retry < 9; retry++ {
				retryTs := makeTransports("sign", retry, uint64(100+retry))
				for _, tr := range retryTs {
					tr.Close()
				}
			}
			for _, p := range providers {
				if len(p.channels) != 2 {
					t.Fatal("retry leaked a permanent pubsub topic")
				}
			}
			requireNoWorkers(t, cfg.Path)
			t.Logf("PUBLIC WITNESS: nodes=%v worker_sha256=%x descriptor=%x output_key=%x message=%x signature=%x key_reload=true claims_permanent=true", nodes, cfg.SHA256, keys[0].Candidate.Descriptor, keys[0].Candidate.OutputKey, message, sigs[0])
		})
	}
}

// Kill only a direct worker child of this test process, never an unrelated node.
func killOwnedWorker(t *testing.T, path string) {
	t.Helper()
	out, e := exec.Command("/bin/ps", "-axo", "pid=,ppid=,command=").Output()
	if e != nil {
		t.Fatal(e)
	}
	var ids []int
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[2] != path {
			continue
		}
		parent, _ := strconv.Atoi(f[1])
		if parent != os.Getpid() {
			continue
		}
		id, e := strconv.Atoi(f[0])
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		t.Fatalf("expected exactly one owned worker, got %d", len(ids))
	}
	p, e := os.FindProcess(ids[0])
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Kill(); e != nil {
		t.Fatal(e)
	}
}
