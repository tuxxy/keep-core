package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	frostnode "github.com/keep-network/keep-core/pkg/frost/node"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
)

// Independent nodes poll with wall-clock backoff while the test mines much
// faster than Ethereum. Leave a common round longer than that polling skew;
// component harnesses retain their separate ten-block schedule. Both are OPEN
// local timing inputs, not production availability qualification.
const k09NodeRoundBlocks = 100

type remoteNode struct {
	done        chan struct{}
	command     *exec.Cmd
	endpoint    string
	config      frostnode.Config
	log         *os.File
	revokeFence func()
}

func (n *remoteNode) status(ctx context.Context) (frostnode.Status, error) {
	var status frostnode.Status
	request, err := http.NewRequestWithContext(ctx, "GET", n.endpoint+"/status", nil)
	if err != nil {
		return status, err
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return status, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return status, fmt.Errorf("node status %d", response.StatusCode)
	}
	err = json.NewDecoder(response.Body).Decode(&status)
	return status, err
}
func localPort(f *fixture) string {
	f.t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}
func localNodeFence(f *fixture, id [32]byte) (string, func()) {
	var mu sync.Mutex
	var lease store.FenceLease
	authority := &authority{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var err error
		switch r.URL.Path {
		case "/acquire":
			var got [32]byte
			err = json.NewDecoder(r.Body).Decode(&got)
			if err == nil && got != id {
				err = store.ErrBusy
			}
			if err == nil && lease != nil {
				err = store.ErrBusy
			}
			if err == nil {
				lease, err = authority.Acquire(r.Context(), got)
			}
		case "/head":
			if lease == nil {
				err = store.ErrBusy
			} else {
				var point store.Point
				point, err = lease.Head(r.Context())
				if err == nil {
					_ = json.NewEncoder(w).Encode(point)
					return
				}
			}
		case "/advance":
			var change k09Advance
			if lease == nil {
				err = store.ErrBusy
			} else if err = json.NewDecoder(r.Body).Decode(&change); err == nil {
				err = lease.Advance(r.Context(), change.Before, change.After)
			}
		case "/close":
			if lease != nil {
				err = lease.Close()
				lease = nil
			}
		default:
			err = store.ErrBusy
		}
		if err != nil {
			http.Error(w, "local authority refused", 409)
		}
	}))
	f.t.Cleanup(server.Close)
	return server.URL, func() {
		mu.Lock()
		defer mu.Unlock()
		if lease != nil {
			if err := lease.Close(); err != nil {
				f.t.Fatal(err)
			}
			lease = nil
		}
	}
}
func startRemoteNodes(f *fixture, url, root string) {
	f.t.Helper()
	if f.core == nil {
		f.t.Fatal("remote node setup requires the public Core precursor")
	}
	directory := f.t.TempDir()
	binary := filepath.Join(directory, "keep-core")
	build := exec.CommandContext(f.ctx, "go", "build", "-p", "2", "-o", binary, ".")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		f.t.Fatalf("node executable build: %v\n%s", err, out)
	}
	worker := os.Getenv("SNOWFALL_WORKER")
	pin, err := hex.DecodeString(os.Getenv("SNOWFALL_WORKER_SHA256"))
	if err != nil || len(pin) != 32 {
		f.t.Fatal("worker pin required")
	}
	var workerHash [32]byte
	copy(workerHash[:], pin)
	diagnostics := f.t.TempDir()
	if retained := os.Getenv("FROST_K09_NODE_LOG_ROOT"); retained != "" {
		if !filepath.IsAbs(retained) {
			f.t.Fatal("absolute retained node-log root required")
		}
		if err = os.MkdirAll(retained, 0700); err != nil {
			f.t.Fatal(err)
		}
		info, statErr := os.Stat(retained)
		if statErr != nil || info.Mode().Perm()&0077 != 0 {
			f.t.Fatal("private node-log root permissions required")
		}
		diagnostics, err = os.MkdirTemp(retained, "nodes-")
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var bootstrap []string
	for operatorIndex := 1; operatorIndex <= 2; operatorIndex++ {
		dir := filepath.Join(directory, fmt.Sprint(operatorIndex))
		if err = os.Mkdir(dir, 0700); err != nil {
			f.t.Fatal(err)
		}
		// Public local fixture identities; store encryption material is independently
		// provisioned and never printed or transferred in status/worker RPCs.
		private := make([]byte, 32)
		private[31] = byte(operatorIndex)
		keyFile := filepath.Join(dir, "operator.hex")
		if err = os.WriteFile(keyFile, []byte(hex.EncodeToString(private)), 0600); err != nil {
			f.t.Fatal(err)
		}
		material := make([]byte, 32)
		if _, err = rand.Read(material); err != nil {
			f.t.Fatal(err)
		}
		storeFile := filepath.Join(dir, "store.key")
		if err = os.WriteFile(storeFile, material, 0600); err != nil {
			f.t.Fatal(err)
		}
		id := [32]byte{byte(operatorIndex)}
		fenceURL, revoke := localNodeFence(f, id)
		config := frostnode.Config{LocalRegtest: true, EthereumURL: url, BitcoinURL: f.core.url, CookieFile: f.core.cookiePath, Registry: f.deployment.Registry, Bridge: f.deployment.Bridge, OperatorKeyFile: keyFile, StoreKeyFile: storeFile, Keystore: filepath.Join(dir, "keys"), FenceURL: fenceURL, StorageID: id, Worker: snowfallengine.WorkerConfig{Path: worker, SHA256: workerHash}, Peers: bootstrap, FinalityBlocks: k06Finality, RoundBlocks: k09NodeRoundBlocks, FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate, ProtocolSeconds: 120}
		// Peers retain this advertised loopback endpoint across process death.
		// Port zero would silently give a restarted bootstrap node a new address.
		_, networkPort, err := net.SplitHostPort(localPort(f))
		if err != nil {
			f.t.Fatal(err)
		}
		config.NetworkPort, err = strconv.Atoi(networkPort)
		if err != nil {
			f.t.Fatal(err)
		}
		if point := os.Getenv("FROST_K09_PAUSE_BOUNDARY"); point != "" && operatorIndex == 2 {
			config.VerificationPause = &frostnode.VerificationPause{Epoch: f.deployment.Epoch, Boundary: point, Marker: filepath.Join(dir, "public-boundary.txt")}
		}
		raw, err := json.Marshal(config)
		if err != nil {
			f.t.Fatal(err)
		}
		cfgFile := filepath.Join(dir, "config.json")
		if err = os.WriteFile(cfgFile, raw, 0600); err != nil {
			f.t.Fatal(err)
		}
		address := localPort(f)
		command := exec.CommandContext(f.ctx, binary, "frost-local-node", cfgFile, "--listen", address)
		log, err := os.OpenFile(filepath.Join(diagnostics, fmt.Sprintf("operator-%d.log", operatorIndex)), os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			f.t.Fatal(err)
		}
		command.Stdout = log
		command.Stderr = log
		if err = command.Start(); err != nil {
			f.t.Fatal(err)
		}
		node := &remoteNode{done: make(chan struct{}), command: command, endpoint: "http://" + address, config: config, log: log, revokeFence: revoke}
		go func() { _ = node.command.Wait(); close(node.done) }()
		f.remotes = append(f.remotes, node)
		f.t.Cleanup(func() {
			_ = node.command.Process.Signal(os.Interrupt)
			done := node.done
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = node.command.Process.Kill()
				<-done
			}
			_ = node.log.Close()
		})
		ready := false
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			state, err := node.status(f.ctx)
			if err == nil && len(state.Addresses) > 0 {
				bootstrap = []string{state.Addresses[0]}
				ready = true
				break
			}
		}
		if !ready {
			f.t.Fatalf("node %d did not expose public status; private diagnostics: %s", operatorIndex, log.Name())
		}
	}
	if len(f.nodes) != 0 || len(f.journals) != 0 || f.remotes[0].command.Process.Pid == f.remotes[1].command.Process.Pid {
		f.t.Fatal("nodes must own distinct processes/stores outside the parent")
	}
}
func (f *fixture) remoteRecords() ([]dkg.WalletRecord, []error) {
	records := make([]dkg.WalletRecord, len(f.remotes))
	failures := make([]error, len(records))
	for deadline := time.Now().Add(150 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		ready := true
		for i, node := range f.remotes {
			select {
			case <-node.done:
				for j := range failures {
					failures[j] = fmt.Errorf("node exited; private diagnostics: %s", node.log.Name())
				}
				return records, failures
			default:
			}
			state, err := node.status(f.ctx)
			if err != nil {
				ready = false
				continue
			}
			found := false
			for _, record := range state.Wallets {
				if record.Descriptor.Epoch == f.deployment.Epoch && record.State == "ReadyUnfunded" {
					records[i] = record
					found = true
					break
				}
			}
			if !found {
				ready = false
			}
		}
		if ready {
			for _, record := range records {
				if record.ID != records[0].ID {
					f.t.Fatal("independent node public wallet identities differ")
				}
			}
			return records, failures
		}
		if f.ctx.Err() != nil {
			break
		}
	}
	for i, node := range f.remotes {
		failures[i] = fmt.Errorf("separate node did not finish epoch; private diagnostics: %s", node.log.Name())
	}
	return records, failures
}

func TestFrostNodeProcessesDKGAndDispatch(t *testing.T) {
	w := runPublicLegacyFlow(t, true)
	if len(w.f.nodes) != 0 || len(w.f.journals) != 0 || len(w.f.remotes) != 2 {
		t.Fatal("parent must not own node components or stores")
	}
	if w.a.id == w.b.id {
		t.Fatal("separate processes did not create distinct wallets")
	}
	checkUnadmittedPeer(w.f)
	restartRemoteNode(w.f, 1)
	t.Log("K09-NODES: two OS processes dispatched two DKG epochs and certified readiness through the actual eligibility firewall")
}

// The parent observes/mines public chains only. Planning, signing, broadcast and
// proof submission are discovered and dispatched inside the two executables.
func waitRemoteIncoming(w *k07World, target *walletHost, inputValue uint64) uint64 {
	w.t.Helper()
	ctx := w.f.ctx
	var candidate *bitcoin.Transaction
	script, err := bitcoin.PayToTaproot(target.desc.OutputKey)
	if err != nil {
		w.t.Fatal(err)
	}
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		// The fixture stopped interval mining after DKG. Canonical round
		// anchors must still advance while autonomous duties poll the chain.
		w.mine(1)
		if w.observeIncoming != nil {
			w.observeIncoming()
		}
		for _, node := range w.f.remotes {
			select {
			case <-node.done:
				w.t.Fatalf("node exited during automatic incoming sweep; private diagnostics: %s", node.log.Name())
			default:
			}
		}
		var mempool []string
		if err = w.core.call(ctx, "getrawmempool", nil, &mempool); err != nil {
			w.t.Fatal(err)
		}
		for _, id := range mempool {
			hash, e := bitcoin.NewHashFromString(id, bitcoin.ReversedByteOrder)
			if e != nil {
				w.t.Fatal(e)
			}
			tx := w.coreTx(hash)
			if len(tx.Outputs) == 1 && bytes.Equal(tx.Outputs[0].PublicKeyScript, script) {
				candidate = tx
			}
		}
		if candidate != nil && len(mempool) > 0 {
			w.coreMine(1)
		}
		custody := w.custody(target.id)
		if custody.Value > 0 {
			if candidate == nil || len(candidate.Inputs) != 1 || uint64(candidate.Outputs[0].Value) != custody.Value || custody.Value >= inputValue {
				w.t.Fatal("autonomous node sweep lost the actual incoming/custody transaction link")
			}
			fee := inputValue - custody.Value
			if fee > 2000 {
				w.t.Fatal("node sweep exceeded configured fee cap")
			}
			w.mine(int(k06Finality))
			for done := time.Now().Add(10 * time.Second); time.Now().Before(done); time.Sleep(100 * time.Millisecond) {
				settled := true
				for _, node := range w.f.remotes {
					status, err := node.status(ctx)
					if err != nil || status.State != "settled" {
						settled = false
					}
				}
				if settled {
					w.t.Log("K09-NODES:runtime-incoming-settled")
					return fee
				}
			}
			w.t.Fatal("node durable outcomes did not reconcile after final proof")
		}
	}
	for _, node := range w.f.remotes {
		status, err := node.status(ctx)
		if err == nil {
			w.t.Logf("incoming wait status: state=%s retries=%d stages=%v", status.State, status.Retries, status.FailureStages)
		}
	}
	w.t.Fatal("automatic incoming sweep did not finish within its bound")
	return 0
}

func checkUnadmittedPeer(f *fixture) {
	f.t.Helper()
	first := f.remotes[0]
	before, err := first.status(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	raw, err := json.Marshal(before.Addresses)
	if err != nil {
		f.t.Fatal(err)
	}
	file := filepath.Join(f.t.TempDir(), "public-bootstrap.json")
	if err = os.WriteFile(file, raw, 0600); err != nil {
		f.t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	command := exec.CommandContext(f.ctx, executable, "-test.run=^TestK09UnadmittedPeerHelper$")
	command.Env = append(os.Environ(), "FROST_K09_PEER_PROBE="+file)
	output, err := command.CombinedOutput()
	if bytes.Contains(output, []byte("control NodeFirewall assertion")) {
		f.t.Fatal("control NodeFirewall assertion: unadmitted authenticated connection accepted")
	}
	if err != nil || !bytes.Contains(output, []byte("K09-PEER-REJECTED")) {
		f.t.Fatal("unadmitted peer probe did not finish at its public connection assertion")
	}
	after, err := first.status(f.ctx)
	if err != nil || after.RejectedPeers <= before.RejectedPeers {
		f.t.Fatal("control NodeFirewall assertion: actual node did not reject unadmitted authenticated identity", err)
	}
	f.t.Log("K09-NODES:firewall-rejected-unadmitted-peer")
}

// Explicit attacker adapter: its own firewall is disabled so that rejection
// must come from the actual node's production eligibility policy.
func TestK09UnadmittedPeerHelper(t *testing.T) {
	file := os.Getenv("FROST_K09_PEER_PROBE")
	if file == "" {
		return
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var peers []string
	if err = json.Unmarshal(raw, &peers); err != nil {
		t.Fatal(err)
	}
	scalar := make([]byte, 32)
	scalar[31] = 4
	key, err := crypto.ToECDSA(scalar)
	if err != nil {
		t.Fatal(err)
	}
	private := &operator.PrivateKey{PublicKey: operator.PublicKey{Curve: operator.Secp256k1, X: key.X, Y: key.Y}, D: key.D}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	network, err := libp2p.Connect(ctx, libp2p.Config{LoopbackOnly: true, Peers: peers}, private, firewall.Disabled, retransmission.NewTimeTicker(ctx, 100*time.Millisecond))
	if err != nil {
		if !strings.Contains(err.Error(), "bootstrap failed") {
			t.Fatal("peer probe harness failed")
		}
		fmt.Println("K09-PEER-REJECTED")
		return
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	<-timer.C
	if len(network.ConnectionManager().ConnectedPeers()) != 0 {
		t.Fatal("control NodeFirewall assertion: unadmitted peer connected to the real node")
	}
	fmt.Println("K09-PEER-REJECTED")
}

func restartRemoteNode(f *fixture, index int, killed ...bool) {
	f.t.Helper()
	node := f.remotes[index]
	before, err := node.status(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	if len(killed) > 0 && killed[0] {
		err = node.command.Process.Kill()
	} else {
		err = node.command.Process.Signal(os.Interrupt)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	select {
	case <-node.done:
	case <-time.After(10 * time.Second):
		f.t.Fatal("node did not close its store on cancellation")
	}
	// The test authority revokes ownership only after confirmed process death.
	// Its monotone fence point is retained across both SIGINT and SIGKILL.
	node.revokeFence()
	oldPID := node.command.Process.Pid
	oldArgs := node.command.Args
	node.command = exec.CommandContext(f.ctx, oldArgs[0], oldArgs[1:]...)
	node.command.Stdout = node.log
	node.command.Stderr = node.log
	node.done = make(chan struct{})
	if err = node.command.Start(); err != nil {
		f.t.Fatal(err)
	}
	go func() { _ = node.command.Wait(); close(node.done) }()
	if oldPID == node.command.Process.Pid {
		f.t.Fatal("restart did not create a new process")
	}
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		select {
		case <-node.done:
			f.t.Fatalf("restart failed; private diagnostics: %s", node.log.Name())
		default:
		}
		current, err := node.status(f.ctx)
		if err != nil || len(current.Wallets) != len(before.Wallets) {
			continue
		}
		retained := true
		for _, old := range before.Wallets {
			found := false
			for _, fresh := range current.Wallets {
				if fresh.ID == old.ID && fresh.DescriptorHash == old.DescriptorHash {
					found = true
				}
			}
			if !found {
				retained = false
			}
		}
		if retained {
			f.t.Log("K09-NODES:restart-reloaded-both-wallets")
			return
		}
	}
	last, _ := node.status(f.ctx)
	f.t.Fatalf("restarted node did not reload earlier wallets: ready=%d lost=%v stages=%v", len(last.Wallets), last.LostEpochs, last.FailureStages)
}
