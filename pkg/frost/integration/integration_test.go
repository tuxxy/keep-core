package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdnet "net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/chain"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	keepnet "github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// Test-only independent authority. It survives store reopen inside the test,
// but is not a deployment fence and is not evidence of host-loss durability.
type authority struct {
	mu     sync.Mutex
	point  store.Point
	leased bool
}
type lease struct{ a *authority }

func (a *authority) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.leased {
		return nil, store.ErrBusy
	}
	a.leased = true
	return lease{a}, nil
}
func (l lease) Head(context.Context) (store.Point, error) {
	l.a.mu.Lock()
	defer l.a.mu.Unlock()
	return l.a.point, nil
}
func (l lease) Advance(_ context.Context, before, after store.Point) error {
	l.a.mu.Lock()
	defer l.a.mu.Unlock()
	if l.a.point != before {
		return store.ErrQuarantined
	}
	l.a.point = after
	return nil
}
func (l lease) Close() error { l.a.mu.Lock(); defer l.a.mu.Unlock(); l.a.leased = false; return nil }

type deployment struct {
	Registry, Bridge, Pool, Validator, Beacon common.Address
	Epoch                                     uint64
	Seed                                      int
	Pattern                                   []int
	Operators                                 []common.Address
}
type nodeConfig struct {
	root       *store.Store
	network    keepnet.Provider
	membership *group.MembershipValidator
	private    *operator.PrivateKey
	key        *ecdsa.PrivateKey
	worker     snowfallengine.WorkerConfig
}
type fixture struct {
	chainMu    sync.RWMutex
	nodes      []nodeConfig
	t          *testing.T
	ctx        context.Context
	cancel     context.CancelFunc
	rpc        *rpc.Client
	client     *ethclient.Client
	deployment deployment
	registry   *binding.FrostWalletRegistry
	executors  []*dkg.Executor
	journals   []*store.Scoped
	roots      []*store.Store
	workers    []string
	stopMining context.CancelFunc
}

func setup(t *testing.T, pattern string) *fixture {
	t.Helper()
	tb := os.Getenv("FROST_K03_TBTC")
	worker := os.Getenv("SNOWFALL_WORKER")
	pin := os.Getenv("SNOWFALL_WORKER_SHA256")
	if testing.Short() || tb == "" || worker == "" {
		t.Skip("K-03 requires compiled TB/KC contracts, Anvil and the pinned real worker")
	}
	anvil := os.Getenv("FROST_K03_ANVIL")
	if anvil == "" {
		anvil = "anvil"
	}
	raw, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != pin {
		t.Fatal("worker pin mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	listener, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*stdnet.TCPAddr).Port
	listener.Close()
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	var logs bytes.Buffer
	// Distinct chain domains isolate concurrent test instances that reuse the
	// deterministic operator keys and contract deployment addresses.
	command := exec.CommandContext(ctx, anvil, "--silent", "--hardfork", "cancun", "--port", fmt.Sprint(port), "--chain-id", fmt.Sprint(100000+port))
	command.Stdout = &logs
	command.Stderr = &logs
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	var rpcClient *rpc.Client
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		rpcClient, err = rpc.DialContext(ctx, url)
		if err == nil {
			var version string
			err = rpcClient.CallContext(ctx, &version, "web3_clientVersion")
			if err == nil {
				break
			}
			rpcClient.Close()
		}
	}
	if err != nil {
		t.Fatalf("Anvil startup: %v", err)
	}
	t.Cleanup(rpcClient.Close)
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	output := filepath.Join(t.TempDir(), "deployment.json")
	deploy := exec.CommandContext(ctx, "node", filepath.Join(root, "test/frost/deploy.cjs"), url, tb, output, pattern)
	if out, err := deploy.CombinedOutput(); err != nil {
		t.Fatalf("actual contract deployment: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, ctx: ctx, cancel: cancel, rpc: rpcClient, client: ethclient.NewClient(rpcClient)}
	if err = json.Unmarshal(data, &f.deployment); err != nil {
		t.Fatal(err)
	}
	f.registry, err = binding.NewFrostWalletRegistry(f.deployment.Registry, f.client)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fresh contracts: registry=%s bridge=%s epoch=%d selection=%v seed=%d", f.deployment.Registry, f.deployment.Bridge, f.deployment.Epoch, f.deployment.Pattern, f.deployment.Seed)
	chainID, err := f.client.ChainID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var chainBytes [32]byte
	chainID.FillBytes(chainBytes[:])
	domain := frost.Domain{Network: "K03/local-anvil", Chain: chainBytes, Registry: [20]byte(f.deployment.Registry), Epoch: f.deployment.Epoch}
	selected, operators, err := f.registry.Selection(&bind.CallOpts{Context: ctx}, f.deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	var roster []uint16
	for i := range selected {
		roster = append(roster, uint16(i+1))
	}
	var bootstrap []string
	for operatorIndex := 1; operatorIndex <= 3; operatorIndex++ {
		secret := make([]byte, 32)
		secret[31] = byte(operatorIndex)
		key, _ := crypto.ToECDSA(secret)
		address := crypto.PubkeyToAddress(key.PublicKey)
		var locals []uint16
		for i, a := range operators {
			if a == address {
				locals = append(locals, uint16(i+1))
			}
		}
		if len(locals) == 0 {
			continue
		}
		private := &operator.PrivateKey{PublicKey: operator.PublicKey{Curve: operator.Secp256k1, X: key.X, Y: key.Y}, D: key.D}
		signing := local_v1.NewSigner(private)
		addresses := make([]chain.Address, len(operators))
		for i, idx := range f.deployment.Pattern {
			b := make([]byte, 32)
			b[31] = byte(idx)
			k, _ := crypto.ToECDSA(b)
			pub := &operator.PublicKey{Curve: operator.Secp256k1, X: k.X, Y: k.Y}
			addresses[i], err = signing.PublicKeyToAddress(pub)
			if err != nil {
				t.Fatal(err)
			}
		}
		membership := group.NewMembershipValidator(&testutils.MockLogger{}, addresses, signing)
		network, err := libp2p.Connect(ctx, libp2p.Config{Peers: bootstrap}, private, firewall.Disabled, retransmission.NewTimeTicker(ctx, 100*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if len(bootstrap) == 0 {
			for _, a := range network.ConnectionManager().AddrStrings() {
				if strings.Contains(a, "/ip4/127.0.0.1/") {
					bootstrap = []string{a}
					break
				}
			}
			if len(bootstrap) == 0 {
				t.Fatal("no loopback bootstrap")
			}
		}
		rootStore, err := store.Open(ctx, store.Config{Keystore: filepath.Join(t.TempDir(), "keys"), StorageID: [32]byte{byte(operatorIndex)}, Key: [32]byte{17}, Fence: &authority{}})
		if err != nil {
			t.Fatal(err)
		}
		f.roots = append(f.roots, rootStore)
		t.Cleanup(func() { _ = rootStore.Close() })
		journal, err := rootStore.Scope(domain)
		if err != nil {
			t.Fatal(err)
		}
		f.journals = append(f.journals, journal)
		workerCopy := filepath.Join(t.TempDir(), "snowfall")
		if err = os.WriteFile(workerCopy, raw, 0700); err != nil {
			t.Fatal(err)
		}
		f.workers = append(f.workers, workerCopy)
		engine, err := snowfallengine.NewGuarded(snowfallengine.WorkerConfig{Path: workerCopy, SHA256: hash}, journal)
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := domain.NewAttempt("dkg", [32]byte{23}, f.deployment.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		tr, err := transport.New(ctx, transport.Config{Domain: domain, Attempt: attempt, ID: snowfallengine.AttemptID(attempt), LocalSeats: locals, Roster: roster, LocalPublicKey: operator.MarshalUncompressed(&private.PublicKey), Membership: membership, Network: network})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tr.Close)
		bus, err := dkg.NewNetworkBus(ctx, network, membership, domain.ID(), domain.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		chainView, err := eth.NewFrostChain(ctx, f.client, key, f.deployment.Registry, f.deployment.Bridge)
		if err != nil {
			t.Fatal(err)
		}
		f.nodes = append(f.nodes, nodeConfig{rootStore, network, membership, private, key, snowfallengine.WorkerConfig{Path: workerCopy, SHA256: hash}})
		f.executors = append(f.executors, &dkg.Executor{Chain: lockedChain{chainView, &f.chainMu}, Engine: engine, Journal: journal, Bus: bus, Signer: key, Request: frost.DKGRequest{Group: frost.Group{Roster: roster, Threshold: 2, Quorum: 3, Epoch: domain.Epoch}, LocalSeats: locals, Participants: roster, Attempt: attempt}, Providers: frost.Providers{Transport: tr, Store: journal}, FinalityBlocks: 2, PollInterval: 50 * time.Millisecond})
	}
	miningCtx, stop := context.WithCancel(ctx)
	miningDone := make(chan struct{})
	f.stopMining = func() { stop(); <-miningDone }
	t.Cleanup(f.stopMining)
	go func() {
		defer close(miningDone)
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-miningCtx.Done():
				return
			case <-ticker.C:
				var out interface{}
				// Finish an in-flight mine before the deadline tests proceed.
				f.chainMu.Lock()
				_ = rpcClient.CallContext(ctx, &out, "evm_mine")
				f.chainMu.Unlock()
			}
		}
	}()
	return f
}
func (f *fixture) run() ([]dkg.WalletRecord, []error) {
	records := make([]dkg.WalletRecord, len(f.executors))
	errs := make([]error, len(records))
	var wg sync.WaitGroup
	for i, e := range f.executors {
		wg.Add(1)
		go func(i int, e *dkg.Executor) {
			defer wg.Done()
			records[i], errs[i] = tbtc.ExecuteFrostDKG(f.ctx, e)
			f.t.Logf("operator %d exited state=%s error=%v", i, records[i].State, errs[i])
			if errs[i] != nil && (strings.Contains(f.t.Name(), "Foreign") || strings.Contains(f.t.Name(), "PublicApproval")) {
				f.cancel()
			}
		}(i, e)
	}
	wg.Wait()
	return records, errs
}
func TestFrostDKGPublicApprovalAndReadiness(t *testing.T) {
	for _, pattern := range []string{"1,2,3", "1,1,2"} {
		t.Run(pattern, func(t *testing.T) {
			f := setup(t, pattern)
			records, errs := f.run()
			for i, err := range errs {
				if err != nil {
					t.Fatalf("operator %d: %v", i, err)
				}
			}
			for i, r := range records {
				if r.State != "ReadyUnfunded" || r.Quarantined || r.ID != records[0].ID {
					t.Fatalf("operator %d public postcondition: %+v", i, r)
				}
				saved, err := f.journals[i].LoadWallet(f.ctx)
				if err != nil || saved.ID != r.ID {
					t.Fatal("wallet record missing", err)
				}
				key, err := f.journals[i].LoadKey(f.ctx)
				if err != nil || key.Candidate.OutputKey != r.Descriptor.OutputKey {
					t.Fatal("exact Q mismatch", err)
				}
				status, err := f.journals[i].DKGStatus(f.ctx)
				if err != nil || status.State != frost.DKGKeyStored || len(status.LostSeats) != 0 {
					t.Fatal("completed seats not stored", err)
				}
			}
			w, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, records[0].ID)
			if err != nil || w.State != 2 || w.DescriptorHash != records[0].DescriptorHash {
				t.Fatal("public registry not ReadyUnfunded", err)
			}
			// This is a public signature check independent of the contract's ready count.
			if _, err := crypto.DecompressPubkey(append([]byte{2}, w.Descriptor.OutputKey[:]...)); err != nil {
				t.Fatal("invalid output point", err)
			}
			t.Logf("witness wallet=%x descriptor=%x approvalBlock=%d readySeats=%d", records[0].ID, records[0].DescriptorHash, w.ApprovalBlock, 2)
		})
	}
}
