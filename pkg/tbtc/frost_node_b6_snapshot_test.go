package tbtc_test

// Batch-6 world snapshots (speed only; no assertion depends on them).
// Refs tuxxy/keep-core#26.
//
// Building the world costs about 4 s, and a node-run FROST DKG another 3-8 s,
// for every case. As legacyCoreTemplate does for Core, each stage is built
// once and copied per case:
//   - "world": the legacy world on the upgraded Bridge with the real FROST
//     registry stack, before any node runs;
//   - "ready-<pattern>": the world after the nodes, started through the seam,
//     made one FROST wallet with that seat pattern (#26 DKG). The node
//     processes are then killed (SIGKILL, after 3 s of quiet), so every case
//     starting from it starts its nodes on restored stores: restore at startup
//     is exercised by every such case.
//
// A stage is an Anvil state dump (anvil_dumpState; blocks, hashes and logs
// are kept), a stopped Core datadir, the operators' node storage (copied,
// never read) and the public world facts. The cache lives under
// os.UserCacheDir()/keep-core-b6-tests, keyed by the test binary's SHA-256 and
// the pinned worker and upgraded build, so any change to the code under test
// or to these tests builds new stages. Stages older than 6 hours are rebuilt
// (the chain's clock must not jump far on load). A stage whose build failed
// at a named assertion fails every later case of the same run with it at
// once.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
)

const b6StageWorld = "world"

// b6StageReady is the stage with one node-made FROST wallet of the pattern.
func b6StageReady(pattern []int) string {
	var parts []string
	for _, p := range pattern {
		parts = append(parts, fmt.Sprint(p))
	}
	return "ready-" + strings.Join(parts, "-")
}

// b6Facts are the public facts a stage needs to rebuild the world's handles.
type b6Facts struct {
	Bridge, Bank, ReimbursementPool, MaintainerProxy, Validator, Registry, Relay common.Address
	FrostRegistry, Pool, Beacon                                                  common.Address
	MainHash                                                                     string
	MainValue                                                                    int64
	CoreMiner                                                                    string
	// CoreStage is the stage whose Core datadir this stage uses.
	CoreStage string
	// Ready stages only.
	Epoch           uint64
	WalletID        common.Hash
	WalletQ         common.Hash
	WalletOperators []common.Address
}

var (
	b6KeyOnce sync.Once
	b6Key     string
	// b6Failed holds this run's stage builds that failed at a named
	// assertion, so later cases fail with it at once; the next run retries.
	b6FailedMu sync.Mutex
	b6Failed   = map[string]string{}
)

// b6CacheRoot is this binary's stage directory.
func b6CacheRoot(t *testing.T) string {
	t.Helper()
	b6KeyOnce.Do(func() {
		raw, err := os.ReadFile(os.Args[0])
		if err != nil {
			return
		}
		digest := sha256.New()
		digest.Write(raw)
		digest.Write([]byte(os.Getenv("SNOWFALL_WORKER_SHA256")))
		digest.Write([]byte(os.Getenv("LEGACY_UPGRADED_COMMIT")))
		digest.Write([]byte(os.Getenv("LEGACY_BITCOIND_SHA256")))
		// Control switches of scratch bindings change what a stage holds.
		var switches []string
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "B6CTL_") || strings.HasPrefix(kv, "RV_B6_") {
				switches = append(switches, kv)
			}
		}
		sort.Strings(switches)
		digest.Write([]byte(strings.Join(switches, "\n")))
		b6Key = hex.EncodeToString(digest.Sum(nil))[:16]
	})
	if b6Key == "" {
		t.Fatal("setup: cannot hash the test binary")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(cache, "keep-core-b6-tests")
	root := filepath.Join(parent, b6Key)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	// Other binaries' stages older than 6 hours go.
	if entries, err := os.ReadDir(parent); err == nil {
		for _, e := range entries {
			info, err := e.Info()
			if err == nil && e.Name() != b6Key && time.Since(info.ModTime()) > 6*time.Hour {
				_ = os.RemoveAll(filepath.Join(parent, e.Name()))
			}
		}
	}
	return root
}

// loadFrostNodeWorld returns a fresh copy of the stage, building it first if
// this binary has none.
func loadFrostNodeWorld(t *testing.T, stage string) *frostNodeWorld {
	t.Helper()
	env := b6Environment(t)
	root := b6CacheRoot(t)
	dir := filepath.Join(root, stage)
	lock, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	func() {
		defer lock.Close()
		b6FailedMu.Lock()
		failure, failed := b6Failed[dir]
		b6FailedMu.Unlock()
		if failed {
			t.Fatalf("%s (stage %s, built by an earlier case of this run)", failure, stage)
		}
		if info, err := os.Stat(filepath.Join(dir, "facts.json")); err == nil && time.Since(info.ModTime()) < 6*time.Hour {
			return
		}
		_ = os.RemoveAll(dir)
		b6BuildStage(t, env, root, stage)
	}()
	return b6LoadStage(t, env, root, stage)
}

// b6BuildStage builds the stage into a temporary directory and renames it.
func b6BuildStage(t *testing.T, env legacyEnvironment, root, stage string) {
	t.Helper()
	building, err := os.MkdirTemp(root, stage+".building-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(building)
	var w *frostNodeWorld
	var facts b6Facts
	if stage == b6StageWorld {
		w = buildFrostNodeWorld(t)
		facts = w.facts()
		facts.CoreStage = b6StageWorld
		w.stopMining()
		// Core first: its datadir is part of the stage.
		w.copyStoppedCore(filepath.Join(building, "core"))
	} else {
		w = loadFrostNodeWorld(t, b6StageWorld)
		var pattern []int
		for _, p := range strings.Split(strings.TrimPrefix(stage, "ready-"), "-") {
			var n int
			fmt.Sscan(p, &n)
			pattern = append(pattern, n)
		}
		if frostNodeStart == nil {
			t.Fatalf("#26 %s: frostNodeStart is unbound: no base node initialisation with FROST configured", t.Name())
		}
		w.startNodes()
		epoch := w.requestWallet(pattern)
		wallet, ok := w.readyWallet(epoch, b6Ready)
		if !ok {
			failure := fmt.Sprintf("#26 %s precondition: epoch %d's FROST wallet did not reach ReadyUnfunded within %v: no node ran the FROST DKG and readiness duty (exited: %v)", t.Name(), epoch, b6Ready, w.exitedNodes())
			b6FailedMu.Lock()
			b6Failed[filepath.Join(root, stage)] = failure
			b6FailedMu.Unlock()
			t.Fatal(failure + w.debugLogs())
		}
		// A quiet point, not a crash test (the certified crash point of #38
		// is one): the nodes get 3 s after certification to finish their
		// local writes before they are killed.
		time.Sleep(3 * time.Second)
		w.killAll()
		w.stopMining()
		facts = w.facts()
		facts.CoreStage = b6StageWorld
		facts.Epoch, facts.WalletID, facts.WalletQ, facts.WalletOperators = epoch, wallet.id, wallet.q, wallet.operators
		for i := 1; i <= b6Operators; i++ {
			from := filepath.Join(w.dir, fmt.Sprintf("operator-%d", i))
			to := filepath.Join(building, fmt.Sprintf("operator-%d", i))
			for _, part := range []string{"storage", "frost-keys"} {
				if err := os.MkdirAll(filepath.Join(to, part), 0o700); err != nil {
					t.Fatal(err)
				}
				legacyCopyDir(t, filepath.Join(from, part), filepath.Join(to, part))
			}
			raw, err := os.ReadFile(filepath.Join(from, "frost-fence"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(to, "frost-fence"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var state string
	if err := w.admin.Client().Call(&state, "anvil_dumpState"); err != nil {
		t.Fatalf("setup: anvil_dumpState: %v", err)
	}
	if err := os.WriteFile(filepath.Join(building, "anvil.state"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(facts)
	if err := os.WriteFile(filepath.Join(building, "facts.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(building, filepath.Join(root, stage)); err != nil {
		t.Fatal(err)
	}
	w.phase("stage " + stage + " built")
}

// facts records the world's public facts.
func (w *frostNodeWorld) facts() b6Facts {
	f := b6Facts{
		Bridge: w.bridge, Bank: w.bank, ReimbursementPool: w.reimbursementPool, MaintainerProxy: w.maintainerProxy,
		Validator: w.validator, Registry: w.legacyWorld.registry, Relay: w.relay,
		FrostRegistry: w.registryAddress, Pool: w.poolAddress, Beacon: w.beaconAddress,
		MainHash: w.main.Outpoint.TransactionHash.Hex(bitcoin.ReversedByteOrder), MainValue: w.main.Value,
		CoreMiner: w.btc.miner,
	}
	return f
}

// copyStoppedCore stops the world's Core and copies its datadir.
func (w *frostNodeWorld) copyStoppedCore(to string) {
	t := w.t
	t.Helper()
	var info struct {
		LogPath string `json:"logpath"`
	}
	if err := w.btc.core.call("getrpcinfo", nil, &info); err != nil || info.LogPath == "" {
		t.Fatalf("setup: Core datadir: %v", err)
	}
	// logpath is <datadir>/regtest/debug.log.
	datadir := filepath.Dir(filepath.Dir(info.LogPath))
	_ = w.btc.core.call("stop", nil, nil)
	deadline := time.Now().Add(15 * time.Second)
	for w.btc.core.call("getblockcount", nil, nil) == nil {
		if time.Now().After(deadline) {
			t.Fatal("setup: Core did not stop")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The RPC server stops before the process; wait for its lock to go.
	lock := filepath.Join(datadir, "regtest", ".lock")
	for deadline = time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		f, err := os.OpenFile(lock, os.O_RDWR, 0)
		if err != nil {
			break
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			break
		}
		f.Close()
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyCopyDir(t, datadir, to)
}

// b6LoadStage builds a world on a copy of the stage.
func b6LoadStage(t *testing.T, env legacyEnvironment, root, stage string) *frostNodeWorld {
	t.Helper()
	dir := filepath.Join(root, stage)
	var facts b6Facts
	raw, err := os.ReadFile(filepath.Join(dir, "facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	upgradedDir, commit := upgradedBuild(t)
	w := newFrostNodeWorldShell(t)
	lw := &legacyWorld{t: t, env: env, baseline: openTbtcBuild(t, env, upgradedDir, commit)}
	w.legacyWorld = lw
	lw.anvilURL = startLegacyAnvil(t, env)
	if lw.admin, err = ethclient.Dial(lw.anvilURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lw.admin.Close)
	state, err := os.ReadFile(filepath.Join(dir, "anvil.state"))
	if err != nil {
		t.Fatal(err)
	}
	var loaded bool
	if err := lw.admin.Client().Call(&loaded, "anvil_loadState", string(state)); err != nil || !loaded {
		t.Fatalf("setup: anvil_loadState: %v", err)
	}
	if lw.chainID, err = lw.admin.ChainID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lw.adminKey, err = crypto.HexToECDSA(legacyAdminKey); err != nil {
		t.Fatal(err)
	}
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	lw.operator = crypto.PubkeyToAddress(operatorKey.PublicKey)
	lw.keyFile = legacyKeyFile(t, operatorKey)
	parse := func(source, name string) abi.ABI {
		parsed, err := abi.JSON(bytes.NewReader(lw.baseline.artifact(source, name).ABI))
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	lw.bridgeABI = parse("contracts/bridge/Bridge.sol", "Bridge")
	lw.validatorABI = parse("contracts/bridge/WalletProposalValidator.sol", "WalletProposalValidator")
	lw.registryABI, _ = lw.baseline.fixture("LegacyWalletRegistryFixture")
	lw.relayABI, _ = lw.baseline.fixture("LegacyRegtestRelayFixture")
	lw.bridge, lw.bank, lw.reimbursementPool, lw.maintainerProxy = facts.Bridge, facts.Bank, facts.ReimbursementPool, facts.MaintainerProxy
	lw.validator, lw.registry, lw.relay = facts.Validator, facts.Registry, facts.Relay
	if lw.legacy, err = tbtcabi.NewBridge(lw.bridge, lw.admin); err != nil {
		t.Fatal(err)
	}
	lw.fixture = bind.NewBoundContract(lw.registry, lw.registryABI, lw.admin, lw.admin, lw.admin)
	lw.source, lw.target = newLegacyWallet(t, 11), newLegacyWallet(t, 12)
	mainHash, err := bitcoin.NewHashFromString(facts.MainHash, bitcoin.ReversedByteOrder)
	if err != nil {
		t.Fatal(err)
	}
	lw.main = &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: mainHash, OutputIndex: 0}, Value: facts.MainValue}

	// Core on a copy of the stage's datadir.
	datadir := t.TempDir()
	legacyCopyDir(t, filepath.Join(root, facts.CoreStage, "core"), datadir)
	core, _ := startLegacyCore(t, env, datadir)
	lw.btc = &legacyRegtest{t: t, core: core, miner: facts.CoreMiner}
	if err := core.call("loadwallet", []interface{}{"legacy-unchanged"}, nil); err != nil {
		t.Fatal(err)
	}
	lw.btc.mutex.Lock()
	lw.btc.syncLocked(t)
	lw.btc.mutex.Unlock()

	// The FROST side.
	w.registryAddress, w.poolAddress, w.beaconAddress = facts.FrostRegistry, facts.Pool, facts.Beacon
	if w.registry, err = binding.NewFrostWalletRegistry(w.registryAddress, lw.admin); err != nil {
		t.Fatal(err)
	}
	w.pool = bind.NewBoundContract(w.poolAddress, w.b6ArtifactABI("@keep-network/sortition-pools/contracts/SortitionPool.sol", "SortitionPool"), lw.admin, lw.admin, lw.admin)
	w.beacon = bind.NewBoundContract(w.beaconAddress, w.b6ArtifactABI("contracts/test/FrostTestDependencies.sol", "FrostTestBeacon"), lw.admin, lw.admin, lw.admin)
	w.frost = bind.NewBoundContract(lw.bridge, lw.frostABI(), lw.admin, lw.admin, lw.admin)
	if facts.WalletID != (common.Hash{}) {
		w.stageWallet = &b6Wallet{id: facts.WalletID, q: facts.WalletQ, operators: facts.WalletOperators}
		copy(w.stageWallet.label[:], facts.WalletID[:20])
		w.stageEpoch = facts.Epoch
		for i := 1; i <= b6Operators; i++ {
			from := filepath.Join(dir, fmt.Sprintf("operator-%d", i))
			to := filepath.Join(w.dir, fmt.Sprintf("operator-%d", i))
			if err := os.MkdirAll(to, 0o700); err != nil {
				t.Fatal(err)
			}
			legacyCopyDir(t, from, to)
		}
	}
	w.finish()
	w.phase("stage " + stage + " loaded")
	return w
}

// b6ArtifactABI is a keep-core artifact's ABI.
func (w *frostNodeWorld) b6ArtifactABI(source, name string) abi.ABI {
	t := w.t
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.artifacts, source, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var a b6Artifact
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(bytes.NewReader(a.ABI))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
