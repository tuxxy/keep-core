package integration

// Restart and crash witnesses for the seat readiness and key reload remediation
// (batch 4). Refs tuxxy/keep-core#46, tuxxy/keep-core#52.
//
// One operator's node runs as a separate host process: this test binary
// re-executed with FROST_B4_HOST set. It opens the real encrypted store on
// disk, runs the real guarded engine with the pinned worker, and joins the
// other two in-process nodes over libp2p and the real local registry. The
// kill is a SIGKILL of that host process, raised from the executor's own
// verification boundary callback (dkg.Executor.Boundary) or, before the key
// exists, right after the durable RegisteredPendingReady save returns. The
// restart is a new process on the same keystore and fence that starts the
// node, as base keep-core restores signers at startup and does not rerun the
// DKG duty (frostNodeStartup, startup_test.go). The parent then reopens that
// store itself and reads public state only: wallet metadata, DKG status and
// whether a key loads. No key, share, nonce or worker record is decoded or
// printed.
//
// FIXTURE: diskFence is a test fence on a local file, as in the store tests.
// It is not a deployment fence service.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
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
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// preKeyKill names the kill point between the RegisteredPendingReady save and
// SaveKey: right after the acceptance callback's durable save returns, before
// the callback hands its receipt to the worker, so the worker cannot have
// returned KeyReady. Every other kill point is an executor boundary.
const preKeyKill = "after-pending-ready-save"

// killAfterPendingReady is the killed host's DKG journal: the real store,
// which kills the process once a RegisteredPendingReady save is durable. It
// observes the real write and changes no state.
type killAfterPendingReady struct {
	dkg.Journal
	kill func()
}

func (j killAfterPendingReady) SaveWallet(ctx context.Context, r dkg.WalletRecord) error {
	if err := j.Journal.SaveWallet(ctx, r); err != nil || r.State != "RegisteredPendingReady" {
		return err
	}
	if _, err := j.Journal.LoadKey(ctx); !errors.Is(err, store.ErrMissing) {
		fmt.Fprintf(os.Stderr, "B4_HOST_PRECONDITION:key present at the pending-ready save: %v\n", err)
		os.Exit(3)
	}
	j.kill()
	return nil
}

// FIXTURE: an independent fence on a local file, shared by every process of
// one operator. Flock excludes concurrent holders; a killed holder's lock is
// released by the kernel. Copied from the store package's test fence.
type diskFence struct{ path string }
type diskLease struct {
	path string
	lock *os.File
}

func (f diskFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	lock, err := os.OpenFile(f.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, store.ErrBusy
	}
	return &diskLease{f.path, lock}, nil
}
func (l *diskLease) Head(context.Context) (store.Point, error) {
	var p store.Point
	b, err := os.ReadFile(l.path)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}
func (l *diskLease) Advance(ctx context.Context, before, after store.Point) error {
	p, err := l.Head(ctx)
	if err != nil {
		return err
	}
	if p != before || after.Sequence != before.Sequence+1 {
		return store.ErrQuarantined
	}
	b, _ := json.Marshal(after)
	tmp := l.path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = out.Write(b); err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}
func (l *diskLease) Close() error { return l.lock.Close() }

// hostSpec is everything a separate host process needs; all of it is public
// or a test path. The operator's ECDSA key is derived from its index exactly
// as setupWith derives it.
type hostSpec struct {
	Phase      string
	URL        string
	Deployment deployment
	Operator   int
	Keystore   string
	Fence      string
	Worker     string
	WorkerHash string
	// WorkerRuns is the restart launcher's record of worker runs (one
	// "start" and one "exit <code>" line each); SignWorker is the plain
	// worker copy the recovered host signs with afterwards.
	WorkerRuns     string
	SignWorker     string
	SignWorkerHash string
	// Sign: after startup, sign the witness digest with the certified seats.
	Sign      bool
	Bootstrap []string
	KillAt    string
	Result    string
	TimeoutMS int64
}

// hostResult is public: the returned wallet record's lifecycle fields and the
// error text. It never carries key material.
type hostResult struct {
	Phase string
	// Missing: no FROST node startup entry point is bound.
	Missing     bool
	State       string
	Quarantined bool
	Error       string
	Deadline    bool
	ElapsedMS   int64
	// Selected and Signature are the public witness signature, if any.
	Selected  []uint16
	Signature string
	SignError string
}

func operatorKey(index int) (*operator.PrivateKey, error) {
	secret := make([]byte, 32)
	secret[31] = byte(index)
	key, err := crypto.ToECDSA(secret)
	if err != nil {
		return nil, err
	}
	return &operator.PrivateKey{PublicKey: operator.PublicKey{Curve: operator.Secp256k1, X: key.X, Y: key.Y}, D: key.D}, nil
}

func restartStoreConfig(spec hostSpec) store.Config {
	return store.Config{Keystore: spec.Keystore, StorageID: [32]byte{byte(spec.Operator)}, Key: [32]byte{17}, Fence: diskFence{spec.Fence}}
}

func restartDomain(spec hostSpec, chainID [32]byte) frost.Domain {
	return frost.Domain{Network: "K03/local-anvil", Chain: chainID, Registry: [20]byte(spec.Deployment.Registry), Epoch: spec.Deployment.Epoch}
}

// TestRestartHostProcess is the external operator's host process. It is inert
// unless FROST_B4_HOST names a spec file.
func TestRestartHostProcess(t *testing.T) {
	path := os.Getenv("FROST_B4_HOST")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var spec hostSpec
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	kill := func(point string) {
		fmt.Fprintf(os.Stderr, "B4_HOST_KILL:%s\n", point)
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {} // the signal ends the process; nothing after it may run
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(spec.TimeoutMS)*time.Millisecond)
	defer cancel()
	rpcClient, err := rpc.DialContext(ctx, spec.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	client := ethclient.NewClient(rpcClient)
	id, err := client.ChainID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var chainID [32]byte
	id.FillBytes(chainID[:])
	domain := restartDomain(spec, chainID)
	registry, err := binding.NewFrostWalletRegistry(spec.Deployment.Registry, client)
	if err != nil {
		t.Fatal(err)
	}
	selected, operators, err := registry.Selection(&bind.CallOpts{Context: ctx}, spec.Deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	var roster []uint16
	for i := range selected {
		roster = append(roster, uint16(i+1))
	}
	private, err := operatorKey(spec.Operator)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.ToECDSA(private.D.FillBytes(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	var locals []uint16
	for i, a := range operators {
		if a == address {
			locals = append(locals, uint16(i+1))
		}
	}
	signing := local_v1.NewSigner(private)
	addresses := make([]chain.Address, len(operators))
	for i, idx := range spec.Deployment.Pattern {
		member, err := operatorKey(idx)
		if err != nil {
			t.Fatal(err)
		}
		if addresses[i], err = signing.PublicKeyToAddress(&member.PublicKey); err != nil {
			t.Fatal(err)
		}
	}
	membership := group.NewMembershipValidator(&testutils.MockLogger{}, addresses, signing)
	network, err := libp2p.Connect(ctx, libp2p.Config{Peers: spec.Bootstrap}, private, firewall.Disabled, retransmission.NewTimeTicker(ctx, 100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Open(ctx, restartStoreConfig(spec))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	journal, err := root.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	var hash [32]byte
	if _, err = hex.Decode(hash[:], []byte(spec.WorkerHash)); err != nil {
		t.Fatal(err)
	}
	if spec.Phase == "restart" {
		// The restart starts the node on the reopened store; it does not
		// rerun the DKG duty.
		result := hostResult{Phase: spec.Phase, Missing: frostNodeStartup == nil}
		if !result.Missing {
			chainView, err := eth.NewFrostChain(ctx, client, key, spec.Deployment.Registry, spec.Deployment.Bridge)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(os.Stderr, "B4_HOST_START:%s\n", spec.Phase)
			started := time.Now()
			runErr := frostNodeStartup(ctx, startupHost{Store: root, Journal: journal, Domain: domain, Worker: snowfallengine.WorkerConfig{Path: spec.Worker, SHA256: hash}, Chain: chainView, Signer: key, Operator: private, Network: network, Membership: membership, Roster: roster, LocalSeats: locals})
			result.ElapsedMS = time.Since(started).Milliseconds()
			if runErr != nil {
				result.Error = runErr.Error()
				result.Deadline = errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() != nil
			}
			if runErr == nil && spec.Sign {
				// The recovered host signs with the readiness-certified seats.
				var signHash [32]byte
				if _, err = hex.Decode(signHash[:], []byte(spec.SignWorkerHash)); err != nil {
					t.Fatal(err)
				}
				signature, selected, signErr := hostWitness(ctx, registry, journal, snowfallengine.WorkerConfig{Path: spec.SignWorker, SHA256: signHash}, private, network, membership, roster)
				result.Selected = selected
				if signErr != nil {
					result.SignError = signErr.Error()
				} else {
					result.Signature = hex.EncodeToString(signature[:])
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "B4_HOST_START:%s\n", spec.Phase)
		}
		writeHostResult(t, spec, result)
		return
	}
	engine, err := snowfallengine.NewGuarded(snowfallengine.WorkerConfig{Path: spec.Worker, SHA256: hash}, journal)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := domain.NewAttempt("dkg", [32]byte{23}, spec.Deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := transport.New(ctx, transport.Config{Domain: domain, Attempt: attempt, ID: snowfallengine.AttemptID(attempt), LocalSeats: locals, Roster: roster, LocalPublicKey: operator.MarshalUncompressed(&private.PublicKey), Membership: membership, Network: network})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	bus, err := dkg.NewNetworkBus(ctx, network, membership, domain.ID(), spec.Deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	chainView, err := eth.NewFrostChain(ctx, client, key, spec.Deployment.Registry, spec.Deployment.Bridge)
	if err != nil {
		t.Fatal(err)
	}
	var dkgJournal dkg.Journal = journal
	if spec.KillAt == preKeyKill {
		dkgJournal = killAfterPendingReady{Journal: journal, kill: func() { kill(preKeyKill) }}
	}
	executor := &dkg.Executor{Chain: chainView, Engine: engine, Journal: dkgJournal, Bus: bus, Signer: key, Request: frost.DKGRequest{Group: frost.Group{Roster: roster, Threshold: 2, Quorum: 3, Epoch: domain.Epoch}, LocalSeats: locals, Participants: roster, Attempt: attempt}, Providers: frost.Providers{Transport: tr, Store: journal}, FinalityBlocks: 2, PollInterval: 50 * time.Millisecond}
	if spec.KillAt != "" && spec.KillAt != preKeyKill {
		executor.Boundary = func(name string) error {
			if name == spec.KillAt {
				kill(name)
			}
			return nil
		}
	}
	fmt.Fprintf(os.Stderr, "B4_HOST_START:%s\n", spec.Phase)
	started := time.Now()
	// The first run is the node's DKG duty, killed at spec.KillAt.
	record, runErr := tbtc.ExecuteFrostDKG(ctx, executor)
	result := hostResult{Phase: spec.Phase, State: record.State, Quarantined: record.Quarantined, ElapsedMS: time.Since(started).Milliseconds()}
	if runErr != nil {
		result.Error = runErr.Error()
		result.Deadline = errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() != nil
	}
	writeHostResult(t, spec, result)
}

func writeHostResult(t *testing.T, spec hostSpec, result hostResult) {
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(spec.Result, out, 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "B4_HOST_DONE:%s\n", spec.Phase)
}

// externalHost is one launch of the external operator's host process.
type externalHost struct {
	cmd     *exec.Cmd
	spec    hostSpec
	worker  string
	started chan struct{}
	signing chan struct{}
	killed  chan string
	exited  chan error
	mu      sync.Mutex
	tail    []string
	logPath string
}

func (h *externalHost) lines() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.tail, "\n") + " (host log " + h.logPath + ")"
}

// launchHost starts one host process. Each launch gets its own worker copy, so
// a worker orphaned by the kill can be found and stopped by its path.
// hostLaunch selects the restart's worker behaviour and witness signature.
type hostLaunch struct {
	// reloadFails: every worker run of the restarted host fails at once.
	reloadFails bool
	sign        bool
}

func (f *fixture) launchHost(dir string, index int, phase, killAt string, timeout time.Duration, opts ...hostLaunch) *externalHost {
	f.t.Helper()
	raw, err := os.ReadFile(os.Getenv("SNOWFALL_WORKER"))
	if err != nil {
		f.t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	worker := filepath.Join(dir, "snowfall-"+phase)
	if err = os.WriteFile(worker, raw, 0700); err != nil {
		f.t.Fatal(err)
	}
	spec := hostSpec{Phase: phase, URL: f.url, Deployment: f.deployment, Operator: index, Keystore: filepath.Join(dir, "keys"), Fence: filepath.Join(dir, "external-fence"), Worker: worker, WorkerHash: hex.EncodeToString(digest[:]), Bootstrap: f.bootstrap, KillAt: killAt, Result: filepath.Join(dir, "result-"+phase+".json"), TimeoutMS: timeout.Milliseconds()}
	if phase == "restart" {
		// The restarted host's worker runs through a launcher that records
		// each run and its exit code, so a fresh-process key reload during
		// startup is observable from outside. Signing afterwards uses the
		// plain copy, so its runs are not counted.
		var option hostLaunch
		if len(opts) > 0 {
			option = opts[0]
		}
		spec.SignWorker, spec.SignWorkerHash, spec.Sign = worker, spec.WorkerHash, option.sign
		spec.WorkerRuns = filepath.Join(dir, "worker-runs-"+phase)
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
		body := quote(worker) + " \"$@\"\nrc=$?\n"
		if option.reloadFails {
			body = "rc=1\n"
		}
		script := []byte("#!/bin/sh\nprintf 'start\\n' >> " + quote(spec.WorkerRuns) + " || exit 125\n" + body + "printf 'exit %d\\n' \"$rc\" >> " + quote(spec.WorkerRuns) + "\nexit \"$rc\"\n")
		launcher := filepath.Join(dir, "worker-launcher-"+phase)
		if err = os.WriteFile(launcher, script, 0700); err != nil {
			f.t.Fatal(err)
		}
		launcherHash := sha256.Sum256(script)
		spec.Worker, spec.WorkerHash = launcher, hex.EncodeToString(launcherHash[:])
	}
	if _, err = os.Stat(spec.Fence); errors.Is(err, os.ErrNotExist) {
		b, _ := json.Marshal(store.Point{})
		if err = os.WriteFile(spec.Fence, b, 0600); err != nil {
			f.t.Fatal(err)
		}
	}
	specPath := filepath.Join(dir, "spec-"+phase+".json")
	b, err := json.Marshal(spec)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = os.WriteFile(specPath, b, 0600); err != nil {
		f.t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestRestartHostProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "FROST_B4_HOST="+specPath)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		f.t.Fatal(err)
	}
	// Host test output (never key material) is kept for harness diagnosis.
	logFile, err := os.Create(filepath.Join(dir, "host-"+phase+".log"))
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = logFile.Close() })
	cmd.Stdout = logFile
	h := &externalHost{cmd: cmd, spec: spec, worker: worker, started: make(chan struct{}), signing: make(chan struct{}), killed: make(chan string, 1), exited: make(chan error, 1), logPath: logFile.Name()}
	if err = cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
		stopWorkerCopy(worker)
	})
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		var once sync.Once
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(logFile, line)
			if strings.HasPrefix(line, "B4_") {
				h.mu.Lock()
				h.tail = append(h.tail, line)
				h.mu.Unlock()
			}
			switch {
			case strings.HasPrefix(line, "B4_HOST_START:"):
				once.Do(func() { close(h.started) })
			case strings.HasPrefix(line, "B4_HOST_SIGNING:"):
				close(h.signing)
			case strings.HasPrefix(line, "B4_HOST_KILL:"):
				h.killed <- strings.TrimPrefix(line, "B4_HOST_KILL:")
			}
		}
		h.exited <- cmd.Wait()
	}()
	return h
}

// stopWorkerCopy stops a worker left running by a killed host. Only the
// test's own copy at this path is matched.
func stopWorkerCopy(path string) {
	out, err := exec.Command("/bin/ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] == path {
			if pid, err := strconv.Atoi(fields[0]); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
}

// awaitKill requires that the host process died by SIGKILL at the named point.
// Anything else is a harness failure, not a result.
func (f *fixture) awaitKill(h *externalHost, point string) {
	f.t.Helper()
	var err error
	select {
	case err = <-h.exited:
	case <-f.ctx.Done():
		f.t.Fatalf("harness: host process was not killed at %s before the test deadline; log: %s", point, h.lines())
	}
	var got string
	select {
	case got = <-h.killed:
	default:
	}
	status, ok := h.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if err == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL || got != point {
		f.t.Fatalf("harness: host process did not die by SIGKILL at %s (exit %v, marker %q); log: %s", point, err, got, h.lines())
	}
	stopWorkerCopy(h.worker)
}

// awaitResult waits for a restarted host process to exit normally and returns
// what its DKG duty returned.
func (f *fixture) awaitResult(h *externalHost) hostResult {
	f.t.Helper()
	select {
	case err := <-h.exited:
		if err != nil {
			f.t.Fatalf("harness: restarted host process failed (%v); log: %s", err, h.lines())
		}
	case <-f.ctx.Done():
		f.t.Fatalf("harness: restarted host process did not exit before the test deadline; log: %s", h.lines())
	}
	stopWorkerCopy(h.worker)
	raw, err := os.ReadFile(h.spec.Result)
	if err != nil {
		f.t.Fatal(err)
	}
	var r hostResult
	if err = json.Unmarshal(raw, &r); err != nil {
		f.t.Fatal(err)
	}
	return r
}

// openExternal reopens the external operator's real store in this process,
// after its host process has exited.
func (f *fixture) openExternal(h *externalHost) *store.Scoped {
	f.t.Helper()
	id, err := f.client.ChainID(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	var chainID [32]byte
	id.FillBytes(chainID[:])
	root, err := store.Open(f.ctx, restartStoreConfig(h.spec))
	if err != nil {
		f.t.Fatal("harness: reopen external store", err)
	}
	f.t.Cleanup(func() { _ = root.Close() })
	journal, err := root.Scope(restartDomain(h.spec, chainID))
	if err != nil {
		f.t.Fatal(err)
	}
	return journal
}

func (f *fixture) approvedWallet() ([32]byte, dkg.Wallet) {
	f.t.Helper()
	id, err := f.registry.ApprovedWallet(&bind.CallOpts{Context: f.ctx}, f.deployment.Epoch)
	if err != nil || id == ([32]byte{}) {
		f.t.Fatal("harness: no approved wallet", err)
	}
	w, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return id, w
}

type restartCase struct {
	name string
	// pattern is the selection; operator is the external, killed operator.
	pattern  string
	operator int
	seats    []uint16
	killAt   string
	// gated holds the in-process operators' certificate until the external
	// operator has restarted, so its restart sees PendingReady (state 1).
	gated bool
	// lost: the kill precedes the durable KeyReady, so the seats are lost.
	lost bool
	// reloadFails: every worker run of the restarted host fails, so no
	// completed-key reload can succeed and the restored seats must not
	// attest readiness.
	reloadFails bool
}

// witness: the restored seats are needed for certification, so the recovered
// host must show a fresh-worker reload and sign with the certified seats.
func (tc restartCase) witness() bool { return !tc.survivorsCertify() && !tc.lost && !tc.reloadFails }

// survivorsCertify: the in-process operators hold the two ready seats the
// deployed policy needs without the killed operator.
func (tc restartCase) survivorsCertify() bool { return len(tc.seats) == 1 }

// Invariant (KC-SF-V3 section 4.3 and C-03; criteria of K09-30 round 5 and
// K09-37; base keep-core restores signers at node startup): a seat is lost
// only if its host died before the durable KeyReady.
//   - Killed at or after the durable KeyReady, whether the wallet is certified
//     before or after the restart, the restarted node's startup restores every
//     local seat: its store holds the certified wallet as ReadyUnfunded with
//     the key, and no seat is reported lost. When the killed operator holds
//     several seats, every one is restored, and the wallet's certification
//     itself needs the restored seats. Then (KC-SF-V3 4.2, C-03; Astra
//     review B1) the startup must reload the completed key in a fresh worker
//     run before those seats attest: a successful run is observed through
//     the restarted host's worker launcher, and if every worker run fails
//     the seats never attest and the wallet stays uncertified. After startup
//     the recovered host must sign a witness digest with the registry's
//     certified seat set, which includes a restored seat, and the signature
//     must verify under the wallet key. (#46)
//   - Killed between the RegisteredPendingReady save and SaveKey, the
//     restarted node's startup reports every local seat lost, before its
//     deadline, and never saves the wallet ReadyUnfunded. (#52; also the
//     negative side of #46)
//
// Pattern 1,2,3 kills operator 3 (seat 3 alone; operators 1 and 2 certify
// without it). Pattern 1,3,3 kills operator 3 holding seats 2 and 3 (the
// harness's 3-seat group cannot also keep every operator below the
// threshold, D2).
func TestSeatRestartAfterKill(t *testing.T) {
	single := restartCase{pattern: "1,2,3", operator: 3, seats: []uint16{3}}
	multi := restartCase{pattern: "1,3,3", operator: 3, seats: []uint16{2, 3}}
	with := func(base restartCase, name, killAt string, gated, lost bool) restartCase {
		base.name, base.killAt, base.gated, base.lost = name, killAt, gated, lost
		return base
	}
	cases := []restartCase{
		with(single, "pre-key", preKeyKill, false, true),
		with(single, "after-key-ready", "after-key-ready", false, false),
		with(single, "after-key-reload", "after-key-reload", false, false),
		with(single, "before-certificate-submit", "before-certificate-submit", false, false),
		with(single, "after-readiness-postcondition", "after-readiness-postcondition", false, false),
		with(single, "after-key-reload/restart-before-certification", "after-key-reload", true, false),
		with(multi, "multi-seat/pre-key", preKeyKill, false, true),
		with(multi, "multi-seat/after-key-ready", "after-key-ready", false, false),
	}
	failing := with(multi, "multi-seat/after-key-ready/reload-fails", "after-key-ready", false, false)
	failing.reloadFails = true
	cases = append(cases, failing)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			seatRestart(t, tc)
		})
	}
}

func seatRestart(t *testing.T, tc restartCase) {
	f := setupWith(t, tc.pattern, setupOptions{timeout: 120 * time.Second, external: map[int]bool{tc.operator: true}})
	if len(f.external) != 1 || len(f.executors) != 3-len(tc.seats) {
		t.Fatalf("harness: external operator layout %v with %d in-process executors", f.external, len(f.executors))
	}
	dir := t.TempDir()
	if keep := os.Getenv("FROST_B4_HOST_LOGS"); keep != "" {
		// Optional: retain host logs and results for diagnosis.
		var err error
		if dir, err = os.MkdirTemp(keep, "restart-"); err != nil {
			t.Fatal(err)
		}
	}
	var pending chan certificate
	var release chan struct{}
	if tc.gated {
		pending, release = f.gateReadiness()
	}
	first := f.launchHost(dir, tc.operator, "first", tc.killAt, 60*time.Second)
	done := f.start()
	f.awaitKill(first, tc.killAt)

	switch {
	case tc.gated:
		// Every in-process operator holds a certificate; nothing is submitted.
		for range f.executors {
			f.certificate(pending)
		}
		if _, w := f.approvedWallet(); w.State != 1 {
			t.Fatalf("harness: wallet state %d before the restart, want PendingReady", w.State)
		}
	case tc.survivorsCertify():
		survivors := f.await(done)
		for i, err := range survivors.errs {
			if err != nil || survivors.records[i].State != "ReadyUnfunded" {
				t.Fatalf("harness: in-process operator %d did not certify without the killed seat: state=%q error=%v", i, survivors.records[i].State, err)
			}
		}
	}

	timeout := 20 * time.Second
	if tc.reloadFails {
		// A startup whose reload fails reports or gives up within this
		// bound; the wallet must still be uncertified afterwards.
		timeout = 4 * time.Second
	}
	restart := f.launchHost(dir, tc.operator, "restart", "", timeout, hostLaunch{reloadFails: tc.reloadFails, sign: tc.witness()})
	var survivorWitness chan witnessResult
	if tc.witness() {
		survivorWitness = make(chan witnessResult, 1)
		go func() {
			// Operator 1 joins the witness signature once the recovered host
			// announces it, if its seat is certified.
			select {
			case <-restart.signing:
			case <-f.ctx.Done():
				survivorWitness <- witnessResult{err: f.ctx.Err()}
				return
			}
			id, _ := f.approvedWallet()
			selected, err := acceptedSeats(f.ctx, f.registry, id)
			if err != nil || !slices.Contains(selected, 1) {
				survivorWitness <- witnessResult{selected: selected, err: err}
				return
			}
			node := f.nodes[0]
			sig, err := signWitness(f.ctx, node.worker, f.journals[0], node.private, node.network, node.membership, f.executors[0].Request.Group.Roster, selected)
			survivorWitness <- witnessResult{selected: selected, signed: true, signature: sig, err: err}
		}()
	}
	if tc.gated {
		select {
		case <-restart.started:
		case err := <-restart.exited:
			restart.exited <- err
		case <-f.ctx.Done():
			t.Fatal("harness: restarted host process did not start")
		}
		close(release)
		survivors := f.await(done)
		for i, err := range survivors.errs {
			if err != nil || survivors.records[i].State != "ReadyUnfunded" {
				t.Fatalf("harness: in-process operator %d did not certify after release: state=%q error=%v", i, survivors.records[i].State, err)
			}
		}
	}
	got := f.awaitResult(restart)
	t.Logf("restarted node startup: missing=%t elapsed=%dms error=%q", got.Missing, got.ElapsedMS, got.Error)
	if got.Missing {
		t.Fatalf("#46/#52 %s: no FROST node startup restores or reports the seats after a restart (the counterpart of base newWalletRegistry/loadSigners; frostNodeStartup is unbound)", tc.name)
	}

	id, certified := f.approvedWallet()
	journal := f.openExternal(restart)
	status, statusErr := journal.DKGStatus(f.ctx)
	saved, walletErr := journal.LoadWallet(f.ctx)
	key, keyErr := journal.LoadKey(f.ctx)

	if tc.lost {
		if got.Deadline {
			t.Errorf("#52 %s: the restarted node's startup ran to its deadline instead of reporting the loss: %s", tc.name, got.Error)
		}
		if statusErr != nil || status.State != frost.DKGSeatsLost || !slices.Equal(status.LostSeats, tc.seats) || !slices.Equal(status.LocalSeats, tc.seats) {
			t.Errorf("#52 %s: seats %v killed between the RegisteredPendingReady save and SaveKey are not all reported lost after restart: status=%+v error=%v", tc.name, tc.seats, status, statusErr)
		}
		if !errors.Is(keyErr, store.ErrMissing) {
			t.Errorf("#52 %s: a seat killed before SaveKey has a durable key after restart: %v", tc.name, keyErr)
		}
		if walletErr == nil && saved.State == "ReadyUnfunded" {
			t.Errorf("#52 %s: a lost seat's wallet record became ReadyUnfunded", tc.name)
		}
		return
	}
	if tc.reloadFails {
		if certified.State == 2 || (walletErr == nil && saved.State == "ReadyUnfunded") {
			t.Errorf("#46 %s: the restored seats %v attested readiness although every completed-key reload in a fresh worker failed (KC-SF-V3 4.2, C-03): wallet state %d, stored %q, worker runs %q", tc.name, tc.seats, certified.State, saved.State, workerRuns(restart))
		}
		return
	}
	if tc.witness() {
		runs := workerRuns(restart)
		if !strings.Contains(runs, "exit 0\n") {
			t.Errorf("#46 %s: no successful completed-key reload in a fresh worker process was observed during the restarted node's startup (KC-SF-V3 4.2, C-03): worker runs %q", tc.name, runs)
		}
		checkWitness(t, tc, got, survivorWitness, certified.Descriptor.OutputKey)
	}
	if !tc.survivorsCertify() && certified.State != 2 {
		t.Errorf("#46 %s: the restarted operator's seats %v were not restored into readiness, so the wallet was never certified: state %d", tc.name, tc.seats, certified.State)
	} else if certified.State != 2 {
		t.Fatalf("harness: wallet not certified by the other operators: state %d", certified.State)
	}
	if got.Error != "" {
		t.Errorf("#46 %s: the restarted node's startup failed for seats with a durable KeyReady: %s", tc.name, got.Error)
	}
	if statusErr != nil || status.State != frost.DKGKeyStored || len(status.LostSeats) != 0 || !slices.Equal(status.LocalSeats, tc.seats) {
		t.Errorf("#46 %s: seats %v with a durable KeyReady are not all restored after restart: status=%+v error=%v", tc.name, tc.seats, status, statusErr)
	}
	if walletErr != nil || saved.State != "ReadyUnfunded" || saved.Quarantined || saved.ID != id || saved.DescriptorHash != certified.DescriptorHash || saved.Descriptor.OutputKey != certified.Descriptor.OutputKey {
		t.Errorf("#46 %s: after restart the stored wallet is not the certified wallet as ReadyUnfunded: state=%q quarantined=%t identity=%t error=%v", tc.name, saved.State, saved.Quarantined, walletErr == nil && saved.ID == id && saved.DescriptorHash == certified.DescriptorHash, walletErr)
	}
	if keyErr != nil || key.Candidate.OutputKey != certified.Descriptor.OutputKey || key.Candidate.Descriptor != certified.Descriptor.SnowfallDescriptor {
		t.Errorf("#46 %s: after restart the durable key is missing or not the certified wallet's key: %v", tc.name, keyErr)
	}
}

type witnessResult struct {
	selected  []uint16
	signed    bool
	signature [64]byte
	err       error
}

// workerRuns is the restarted host's launcher record ("start" and "exit
// <code>" lines), empty if no worker ran.
func workerRuns(h *externalHost) string {
	raw, err := os.ReadFile(h.spec.WorkerRuns)
	if err != nil {
		return ""
	}
	return string(raw)
}

// checkWitness requires the recovered host to have signed the witness digest
// with the registry's certified seat set, which includes at least one
// restored seat, and the signature to verify under the wallet's key.
func checkWitness(t *testing.T, tc restartCase, got hostResult, survivor chan witnessResult, outputKey [32]byte) {
	t.Helper()
	if got.Signature == "" {
		t.Errorf("#46 %s: the recovered host did not take part in a FROST signature with the readiness-certified seats %v: %s", tc.name, got.Selected, got.SignError)
		return
	}
	var other witnessResult
	select {
	case other = <-survivor:
	case <-time.After(5 * time.Second):
		other.err = errors.New("operator 1 witness did not finish")
	}
	restored := slices.ContainsFunc(got.Selected, func(s uint16) bool { return slices.Contains(tc.seats, s) })
	raw, err := hex.DecodeString(got.Signature)
	if err != nil || len(raw) != 64 || !restored {
		t.Fatalf("harness: witness signature %d bytes over seats %v: %v", len(raw), got.Selected, err)
	}
	if other.signed && (other.err != nil || [64]byte(raw) != other.signature) {
		t.Errorf("#46 %s: operator 1's witness signature differs from the recovered host's: %v", tc.name, other.err)
	}
	q, err := schnorr.ParsePubKey(outputKey[:])
	if err != nil {
		t.Fatal(err)
	}
	signature, err := schnorr.ParseSignature(raw)
	if err != nil || !signature.Verify(witnessDigest[:], q) {
		t.Errorf("#46 %s: the recovered host's signature with seats %v does not verify under the wallet key: %v", tc.name, got.Selected, err)
	}
}
