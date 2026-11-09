package tbtc_test

// Remediation batch 6 world: base node processes with FROST configured.
// Refs tuxxy/keep-core#26, #38, #39 (DECISIONS.md "D5 revised", D2, D19).
//
// Chain: the merged legacy world (frozen, batch 1) on the upgraded Bridge
// (LEGACY_UPGRADED_DIR), plus the real FROST wallet registry stack exactly as
// test/frost/deploy.cjs deploys it (sortition pool, DKG validator, admission,
// test beacon and stake), bound to the Bridge with configureFrostRegistry.
// FROST wallets come only from DKG run by the nodes; there is no FROST
// registry fixture here.
//
// Nodes: one OS process per operator, the test binary re-executed as
// TestB6NodeProcess. It builds what cmd/start.go builds before tbtc.Initialize
// and calls frostNodeStart (frost_node_b6_seam_test.go). It holds no duty
// logic, so a kill -9 is a real crash and a restart is a new process on the
// same storage.
//
// FIXTURES (labelled where they are built):
//   - the legacy registry fixture behind the Bridge (batch 1, frozen);
//   - b6StakingFixture: answers the ECDSA sortition-pool startup reads the
//     legacy registry fixture does not serve (the T staking stack is not under
//     test, K09-HARNESS-PLAN-v2 section 1);
//   - b6DiskFence: the FROST store's external fence on a local file;
//   - the legacy source wallet's test key, which signs the drain transaction
//     that gives a FROST wallet an incoming output (as batch 3);
//   - b6Recorder and b6BitcoinRecorder: observe what a node sends to the
//     network and to Bitcoin; they never change traffic;
//   - b6LiveBitcoin (frost_node_b6_bitcoin_test.go): a node's Bitcoin chain
//     over Core, live, with UnspentByScript as the Electrum connection has;
//   - b6Scheduler: base's scheduler with ECDSA pre-parameter generation held
//     off (legacy ECDSA DKG is not under test);
//   - the node processes also report their latest watched block and their
//     peer count, which the window driver and node start wait on.
// Time: blocks come every 20 ms, and every 50 ms from a window on, so base's
// 80-block active phase lasts 4 s (16 minutes in production). Worlds are
// copied from cached stages (frost_node_b6_snapshot_test.go).
// The test itself acts only as depositor, keeper, SPV maintainer and
// governance, through public calls with its own keys.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	commonethereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/generator"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/storage"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// b6Operators is the number of FROST operators; operator i (1-based) has the
// scalar i as its key, as deploy.cjs and setupWith derive them.
const b6Operators = 3

// b6KeyPassword is legacyKeyFile's keystore password.
const b6KeyPassword = "legacy-unchanged"

// b6ArtifactRoot is keep-core's compiled solidity/ecdsa build (the FROST
// registry stack), as test/frost/deploy.cjs reads it.
func b6ArtifactRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "solidity", "ecdsa", "build")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("setup: keep-core solidity/ecdsa/build is required (the FROST registry stack): %v", err)
	}
	return root
}

type b6Artifact struct {
	ABI            json.RawMessage `json:"abi"`
	Bytecode       string          `json:"bytecode"`
	LinkReferences map[string]map[string][]struct {
		Start  int `json:"start"`
		Length int `json:"length"`
	} `json:"linkReferences"`
}

// b6Deploy deploys a keep-core artifact, linking its libraries as deploy.cjs does.
func (w *frostNodeWorld) b6Deploy(source, name string, args ...interface{}) (common.Address, abi.ABI) {
	t := w.t
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.artifacts, source, name+".json"))
	if err != nil {
		t.Fatalf("setup: artifact %s: %v", name, err)
	}
	var a b6Artifact
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	code := strings.TrimPrefix(a.Bytecode, "0x")
	for file, names := range a.LinkReferences {
		for lib, positions := range names {
			key := file + ":" + lib
			address, ok := w.libraries[key]
			if !ok {
				address, _ = w.b6Deploy(file, lib)
				w.libraries[key] = address
			}
			for _, p := range positions {
				code = code[:p.Start*2] + strings.ToLower(strings.TrimPrefix(address.Hex(), "0x")) + code[(p.Start+p.Length)*2:]
			}
		}
	}
	parsed, err := abi.JSON(bytes.NewReader(a.ABI))
	if err != nil {
		t.Fatal(err)
	}
	return w.deploy(name, parsed, common.FromHex(code), args...), parsed
}

// frostNodeWorld is the legacy world plus the real FROST registry and the
// operators' node processes.
type frostNodeWorld struct {
	*legacyWorld
	artifacts string
	libraries map[string]common.Address
	dir       string

	registryAddress common.Address
	poolAddress     common.Address
	beaconAddress   common.Address
	registry        *binding.FrostWalletRegistry
	// stageWallet and stageEpoch are a ready stage's FROST wallet.
	stageWallet *b6Wallet
	stageEpoch  uint64
	pool        *bind.BoundContract
	beacon      *bind.BoundContract
	frost       *bind.BoundContract
	keys        []*ecdsa.PrivateKey
	keyFiles    []string
	peers       []string
	ports       []int

	nodesMu sync.Mutex
	nodes   map[int]*b6Node
	// stopMining stops the background miner.
	stopMining func()
	began      time.Time
	// lastStart is when the last node start completed.
	lastStart time.Time

	// extraContracts are contract addresses the nodes get beside the legacy
	// client configuration (K-09: the EcdsaDkgValidator FIXTURE the legacy
	// tECDSA group's parameters are read from). Nil adds none.
	extraContracts map[string]string
}

// phase logs the time since the world was built began (budget tracing).
func (w *frostNodeWorld) phase(name string) {
	w.t.Logf("b6 phase %-28s %6.2f s", name, time.Since(w.began).Seconds())
}

// newFrostNodeWorld builds the world. No FROST wallet exists yet and no node
// runs; requestWallet asks for one, startNodes starts the nodes.
func newFrostNodeWorld(t *testing.T) *frostNodeWorld {
	t.Helper()
	return loadFrostNodeWorld(t, b6StageWorld)
}

// b6Environment is the legacy environment with the batch-6 requirements and
// Anvil keeping its states in memory.
func b6Environment(t *testing.T) legacyEnvironment {
	t.Helper()
	env := legacyEnv(t)
	if os.Getenv("SNOWFALL_WORKER") == "" || os.Getenv("SNOWFALL_WORKER_SHA256") == "" {
		t.Skip("batch 6: run through test/remediation-b6/run.sh (the pinned real worker: SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256)")
	}
	env.anvil = b6Anvil(t, env.anvil)
	return env
}

// buildFrostNodeWorld deploys the world from scratch (the snapshot builder's
// first stage; see frost_node_b6_snapshot_test.go).
func buildFrostNodeWorld(t *testing.T) *frostNodeWorld {
	t.Helper()
	env := b6Environment(t)
	dir, commit := upgradedBuild(t)
	w := newFrostNodeWorldShell(t)
	w.legacyWorld = newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), w.setUpFrostRegistry)
	w.finish()
	return w
}

// newFrostNodeWorldShell is a world without a chain yet.
func newFrostNodeWorldShell(t *testing.T) *frostNodeWorld {
	dir := t.TempDir()
	// With B6_DEBUG_DIR set, the nodes' directories (logs included) are kept.
	if keep := os.Getenv("B6_DEBUG_DIR"); keep != "" {
		var err error
		if dir, err = os.MkdirTemp(keep, "case-"); err != nil {
			t.Fatal(err)
		}
	}
	return &frostNodeWorld{artifacts: b6ArtifactRoot(t), libraries: map[string]common.Address{}, dir: dir, nodes: map[int]*b6Node{}, began: time.Now()}
}

// finish gives the operators their keys, ports and peers, and starts the
// background miner.
func (w *frostNodeWorld) finish() {
	t := w.t
	t.Helper()
	for i := 1; i <= b6Operators; i++ {
		key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(int64(i)).Bytes(), 32))
		if err != nil {
			t.Fatal(err)
		}
		w.keys = append(w.keys, key)
		w.keyFiles = append(w.keyFiles, b6KeyFile(t, key))
		w.ports = append(w.ports, legacyFreePort(t))
	}
	for i := range w.keys {
		// Every node bootstraps from every other one, on loopback.
		w.peers = append(w.peers, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/ipfs/%s", w.ports[i], b6PeerID(t, w.keys[i])))
	}
	w.stopMining = w.mineInBackground()
	// jumpToWindow replaces stopMining; stop whichever miner runs last.
	t.Cleanup(func() { w.stopMining() })
	t.Cleanup(w.killAll)
	w.phase("world")
}

// setUpFrostRegistry runs before any legacy wallet exists: the FROST
// extension as setUpFrostDrain configures it (C-10 test ceilings, D-10 values
// OPEN), but with the real FROST wallet registry stack of deploy.cjs.
func (w *frostNodeWorld) setUpFrostRegistry(lw *legacyWorld) {
	w.legacyWorld = lw
	t := lw.t
	t.Helper()
	admin := crypto.PubkeyToAddress(lw.adminKey.PublicKey)
	w.frost = bind.NewBoundContract(lw.bridge, lw.frostABI(), lw.admin, lw.admin, lw.admin)
	call := func(what string, contract *bind.BoundContract, method string, args ...interface{}) {
		lw.send(what, func(o *bind.TransactOpts) (*types.Transaction, error) {
			return contract.Transact(o, method, args...)
		})
	}
	bound := func(address common.Address, parsed abi.ABI) *bind.BoundContract {
		return bind.NewBoundContract(address, parsed, lw.admin, lw.admin, lw.admin)
	}

	tokenAddress, _ := w.b6Deploy("contracts/test/FrostTestDependencies.sol", "FrostTestToken")
	stakeAddress, stakeABI := w.b6Deploy("contracts/test/FrostTestDependencies.sol", "FrostTestStakeSource")
	beaconAddress, beaconABI := w.b6Deploy("contracts/test/FrostTestDependencies.sol", "FrostTestBeacon")
	poolAddress, poolABI := w.b6Deploy("@keep-network/sortition-pools/contracts/SortitionPool.sol", "SortitionPool", tokenAddress, big.NewInt(1))
	w.pool = bound(poolAddress, poolABI)
	w.beacon = bound(beaconAddress, beaconABI)
	w.poolAddress, w.beaconAddress = poolAddress, beaconAddress
	call("pool chaosnet off", w.pool, "deactivateChaosnet")
	admissionAddress, admissionABI := w.b6Deploy("contracts/frost/FrostAdmission.sol", "FrostAdmission", stakeAddress)
	validatorAddress, _ := w.b6Deploy("contracts/frost/FrostDkgValidator.sol", "FrostDkgValidator",
		uint16(3), uint16(2), uint16(2), uint16(0), uint16(0))
	w.registryAddress, _ = w.b6Deploy("contracts/frost/FrostWalletRegistry.sol", "FrostWalletRegistry",
		poolAddress, validatorAddress, admissionAddress, beaconAddress, lw.bridge,
		[4]uint64{10000, 1000, 10, 1000}, uint16(3))
	var err error
	w.registry, err = binding.NewFrostWalletRegistry(w.registryAddress, lw.admin)
	if err != nil {
		t.Fatal(err)
	}
	call("pool owner", w.pool, "transferOwnership", w.registryAddress)
	call("FROST registry", w.frost, "configureFrostRegistry", w.registryAddress)

	// The FROST funding and drain configuration of setUpFrostDrain.
	libraries := map[string]common.Address{}
	verifier, _ := lw.deployBaseline(libraries, "contracts/frost/FrostVerifier.sol", "FrostVerifier")
	code, err := lw.admin.CodeAt(context.Background(), verifier, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The funding network is Core regtest's genesis, as deploy.cjs sets it;
	// FROST redemptions require the same network.
	genesis := chaincfg.RegressionNetParams.GenesisHash
	call("FROST funding", w.frost, "configureFrostFunding", verifier, crypto.Keccak256Hash(code),
		crypto.Keccak256Hash([]byte("UNQUALIFIED batch-6 local test")), [32]byte(*genesis))
	exposureAddress, exposureABI := lw.deployBaseline(libraries, "contracts/frost/FrostExposureController.sol", "FrostExposureController", lw.bridge, admin)
	exposure := bound(exposureAddress, exposureABI)
	call("C-10 budget", exposure, "openBudgetPeriod", ^uint64(0), ^uint64(0),
		crypto.Keccak256Hash([]byte("C-10 test budget; D-10 OPEN")))
	call("C-10 controller", w.frost, "installFrostExposureController", exposureAddress)
	call("FROST gates", w.frost, "setFrostFundingGates", true, true)
	call("FROST drain enabled", w.frost, "setFrostDrainEnabled", true)

	// The operators, as deploy.cjs admits them.
	stake := bound(stakeAddress, stakeABI)
	admission := bound(admissionAddress, admissionABI)
	registry := bound(w.registryAddress, b6ABI(t, binding.FrostWalletRegistryMetaData))
	for i := 1; i <= b6Operators; i++ {
		key, _ := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(int64(i)).Bytes(), 32))
		address := crypto.PubkeyToAddress(key.PublicKey)
		if err := lw.admin.Client().Call(nil, "anvil_setBalance", address, "0x3635c9adc5dea00000"); err != nil {
			t.Fatal(err)
		}
		call("stake", stake, "set", address, big.NewInt(100))
		call("admission", admission, "setAllowed", address, true)
		call("refresh operator", registry, "refreshOperator", address)
	}
	// FROST redemptions under the Bridge's own redemption parameters, with
	// deploy.cjs's k05 local values for finality (2 blocks) and certificates.
	call("FROST redemptions", w.frost, "configureFrostRedemptions", [32]byte(*genesis), uint64(2), uint64(1000), common.Address{},
		crypto.Keccak256Hash([]byte("UNQUALIFIED batch-6 local D-08 reference")))
	call("FROST redemptions enabled", w.frost, "setFrostRedemptionsEnabled", true)
	call("registry requests", registry, "setRequestsEnabled", true)
	call("Bridge FROST requests", w.frost, "setFrostRequestsEnabled", true)
}

func b6ABI(t *testing.T, meta *bind.MetaData) abi.ABI {
	t.Helper()
	parsed, err := meta.GetAbi()
	if err != nil {
		t.Fatal(err)
	}
	return *parsed
}

func b6KeyFile(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	return legacyKeyFile(t, key)
}

// b6PeerID is the libp2p peer ID of an operator key.
func b6PeerID(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	public, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(crypto.CompressPubkey(&key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// b6Leader is D2 as base getLeader states it: unique operators, sorted
// ascending, shuffled by a math/rand source seeded with the first 8 bytes of
// sha256(wallet key || safe block hash); the first one leads.
func b6Leader(wallet [20]byte, safeBlockHash [32]byte, operators []chain.Address) chain.Address {
	seed := sha256.Sum256(append(append([]byte{}, wallet[:]...), safeBlockHash[:]...))
	set := map[chain.Address]bool{}
	var unique []chain.Address
	for _, o := range operators {
		if !set[o] {
			set[o] = true
			unique = append(unique, o)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	rng := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(seed[:8]))))
	rng.Shuffle(len(unique), func(i, j int) { unique[i], unique[j] = unique[j], unique[i] })
	return unique[0]
}

// b6Anvil wraps Anvil to keep the last 20,000 states in memory. By default
// Anvil writes older states to disk, which made each 900-block jump to a
// coordination window cost 2-8 s and grow with the chain (measured
// 2026-11-03); in memory a jump costs about 30 ms. Speed only: every state a
// node reads (at most one window back) stays available.
func b6Anvil(t *testing.T, anvil string) string {
	t.Helper()
	wrapper := filepath.Join(t.TempDir(), "anvil")
	script := fmt.Sprintf("#!/bin/sh\nexec %q \"$@\" --prune-history 20000\n", anvil)
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

// requestWallet asks the Bridge for a new FROST wallet as governance does and
// fulfils the beacon with a seed that selects the given seat pattern (each
// entry an operator index, as deploy.cjs). It returns the new epoch.
func (w *frostNodeWorld) requestWallet(pattern []int) uint64 {
	t := w.t
	t.Helper()
	w.send("request FROST wallet", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frost.Transact(o, "requestNewFrostWallet")
	})
	for seed := int64(1); seed < 10000; seed++ {
		var out []interface{}
		if err := w.pool.Call(&bind.CallOpts{}, &out, "selectGroup", big.NewInt(int64(len(pattern))), common.BigToHash(big.NewInt(seed))); err != nil {
			t.Fatalf("setup: selectGroup: %v", err)
		}
		members := out[0].([]uint32)
		match := len(members) == len(pattern)
		for i := range members {
			match = match && int(members[i]) == pattern[i]
		}
		if !match {
			continue
		}
		w.send("beacon fulfil", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.beacon.Transact(o, "fulfill", big.NewInt(seed))
		})
		epoch, err := w.registry.Epoch(&bind.CallOpts{})
		if err != nil {
			t.Fatal(err)
		}
		return epoch
	}
	t.Fatalf("setup: no selection seed for pattern %v", pattern)
	return 0
}

// b6Wallet is a FROST wallet as the chain shows it.
type b6Wallet struct {
	id    [32]byte
	label [20]byte
	q     [32]byte
	// operators are the wallet's seat holders in seat order.
	operators []common.Address
}

// readyWallet waits until the registry shows epoch's wallet ReadyUnfunded
// (state 2), within bound. It returns false if it did not get there.
func (w *frostNodeWorld) readyWallet(epoch uint64, bound time.Duration) (b6Wallet, bool) {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		id, err := w.registry.ApprovedWallet(&bind.CallOpts{}, epoch)
		if err == nil && id != ([32]byte{}) {
			wallet, err := w.registry.Wallet(&bind.CallOpts{}, id)
			if err == nil && wallet.State == 2 {
				w.phase("wallet ready")
				result := b6Wallet{id: id, q: wallet.Descriptor.OutputKey, operators: wallet.Descriptor.Operators}
				copy(result.label[:], id[:20])
				return result, true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return b6Wallet{}, false
}

// --- Node processes ---------------------------------------------------------

// b6NodeConfig is everything a node process needs; all of it is public or a
// test path.
type b6NodeConfig struct {
	Operator      int
	AnvilURL      string
	KeyFile       string
	Contracts     map[string]string
	FrostRegistry string
	StorageDir    string
	FrostKeystore string
	FrostFence    string
	Port          int
	Peers         []string
	AllowKeys     []string
	Worker        string
	WorkerSHA256  string
	CoreURL       string
	CoreUser      string
	CorePass      string
	CoreMiner     string
	Record        string
	Status        string
	Height        string
	PeersFile     string
}

type b6Node struct {
	config  b6NodeConfig
	cmd     *exec.Cmd
	exited  chan struct{}
	exitErr error
	log     *bytes.Buffer
}

// nodeConfig is operator i's (1-based) node configuration. The paths depend
// only on i, so a restarted node reuses its storage.
func (w *frostNodeWorld) nodeConfig(i int) b6NodeConfig {
	worker, pin := os.Getenv("SNOWFALL_WORKER"), os.Getenv("SNOWFALL_WORKER_SHA256")
	root := filepath.Join(w.dir, fmt.Sprintf("operator-%d", i))
	contracts := map[string]string{}
	config := w.clientConfig("")
	for name, address := range config.ContractAddresses {
		contracts[name] = address
	}
	for name, address := range w.extraContracts {
		contracts[name] = address
	}
	var allow []string
	for _, key := range w.keys {
		allow = append(allow, hex.EncodeToString(crypto.FromECDSAPub(&key.PublicKey)))
	}
	// A node dials the running nodes: on a full start, in index order, each
	// dials only those before it, so no two nodes dial each other at once
	// (libp2p's TLS refuses such simultaneous opens and the retry is slow).
	var peers []string
	w.nodesMu.Lock()
	for j, peer := range w.peers {
		if _, running := w.nodes[j+1]; running && j != i-1 {
			peers = append(peers, peer)
		}
	}
	w.nodesMu.Unlock()
	return b6NodeConfig{
		Operator: i, AnvilURL: "ws" + strings.TrimPrefix(w.anvilURL, "http"), KeyFile: w.keyFiles[i-1],
		Contracts: contracts, FrostRegistry: w.registryAddress.Hex(),
		StorageDir: filepath.Join(root, "storage"), FrostKeystore: filepath.Join(root, "frost-keys"),
		FrostFence: filepath.Join(root, "frost-fence"), Port: w.ports[i-1], Peers: peers, AllowKeys: allow,
		Worker: worker, WorkerSHA256: pin,
		CoreURL: w.btc.core.url, CoreUser: w.btc.core.user, CorePass: w.btc.core.pass, CoreMiner: w.btc.miner,
		Record: filepath.Join(root, "record.jsonl"), Status: filepath.Join(root, "status"), Height: filepath.Join(root, "height"), PeersFile: filepath.Join(root, "peers"),
	}
}

// startNode starts operator i's node process and waits until its node
// initialisation returned. The seam must be bound (see the seam file).
func (w *frostNodeWorld) startNode(i int) {
	w.t.Helper()
	if failure := w.launchNode(i); failure != "" {
		w.t.Fatal(failure)
	}
	w.connected()
}

// connected waits (at most 5 s) until every running node is connected to
// every other running node, as operators online before a duty are, and
// notes the time: enterWindow gives the nodes 1 s after it.
func (w *frostNodeWorld) connected() {
	began := time.Now()
	for time.Since(began) < 5*time.Second && !w.allConnected() {
		time.Sleep(10 * time.Millisecond)
	}
	w.lastStart = time.Now()
	w.phase("nodes connected")
}

func (w *frostNodeWorld) allConnected() bool {
	w.nodesMu.Lock()
	defer w.nodesMu.Unlock()
	for _, node := range w.nodes {
		raw, err := os.ReadFile(node.config.PeersFile)
		if err != nil {
			return false
		}
		var peers int
		if _, err := fmt.Sscan(string(raw), &peers); err != nil || peers < len(w.nodes)-1 {
			return false
		}
	}
	return true
}

// launchNode is startNode, returning a failure message instead of failing,
// so several nodes can start concurrently.
func (w *frostNodeWorld) launchNode(i int) string {
	config := w.nodeConfig(i)
	for _, dir := range []string{config.StorageDir, config.FrostKeystore} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err.Error()
		}
	}
	if _, err := os.Stat(config.FrostFence); os.IsNotExist(err) {
		b, _ := json.Marshal(store.Point{})
		if err := os.WriteFile(config.FrostFence, b, 0o600); err != nil {
			return err.Error()
		}
	}
	_ = os.Remove(config.Status)
	_ = os.Remove(config.Height)
	_ = os.Remove(config.PeersFile)
	raw, _ := json.Marshal(config)
	configPath := filepath.Join(filepath.Dir(config.Status), "config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		return err.Error()
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestB6NodeProcess$", "-test.timeout=0")
	// One Go core per node process (shared rule: at most 8 Go cores).
	cmd.Env = append(os.Environ(), "B6_NODE_CONFIG="+configPath, "GOMAXPROCS=1")
	if os.Getenv("B6_DEBUG_DIR") != "" {
		cmd.Env = append(cmd.Env, "GOLOG_LOG_LEVEL=info", "IPFS_LOGGING=info")
	}
	node := &b6Node{config: config, cmd: cmd, exited: make(chan struct{}), log: &bytes.Buffer{}}
	// The node's output stays in the case's temporary directory (debugging).
	logFile, err := os.OpenFile(filepath.Join(filepath.Dir(config.Status), "node.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err.Error()
	}
	cmd.Stdout = io.MultiWriter(&b6SyncWriter{w: node.log}, logFile)
	cmd.Stderr = cmd.Stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err.Error()
	}
	go func() { node.exitErr = cmd.Wait(); logFile.Close(); close(node.exited) }()
	w.nodesMu.Lock()
	w.nodes[i] = node
	w.nodesMu.Unlock()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := os.ReadFile(config.Status); err == nil {
			if string(status) == "started" {
				return ""
			}
			return fmt.Sprintf("#26 node %d: the node initialisation with FROST configured failed: %s", i, status)
		}
		select {
		case <-node.exited:
			return fmt.Sprintf("#26 node %d: the node process exited during startup (%v): %s", i, node.exitErr, b6Tail(node.log))
		case <-time.After(20 * time.Millisecond):
		}
	}
	return fmt.Sprintf("#26 node %d: the node initialisation did not return within 20 s", i)
}

// startNodes starts every operator's node.
func (w *frostNodeWorld) startNodes() {
	w.t.Helper()
	if frostNodeStart == nil {
		w.t.Fatalf("#26 %s: frostNodeStart is unbound: no base node initialisation with FROST configured", w.t.Name())
	}
	for i := 1; i <= b6Operators; i++ {
		if failure := w.launchNode(i); failure != "" {
			w.t.Fatal(failure)
		}
	}
	w.phase("nodes started")
	w.connected()
}

// kill crashes operator i's node with SIGKILL and waits for its exit.
func (w *frostNodeWorld) kill(i int) {
	w.nodesMu.Lock()
	node := w.nodes[i]
	delete(w.nodes, i)
	w.nodesMu.Unlock()
	if node == nil {
		return
	}
	_ = syscall.Kill(-node.cmd.Process.Pid, syscall.SIGKILL)
	<-node.exited
}

func (w *frostNodeWorld) killAll() {
	for i := 1; i <= b6Operators; i++ {
		w.kill(i)
	}
}

// alive reports which started nodes have exited, with their output tails.
func (w *frostNodeWorld) exitedNodes() []string {
	w.nodesMu.Lock()
	defer w.nodesMu.Unlock()
	var exited []string
	for i, node := range w.nodes {
		select {
		case <-node.exited:
			exited = append(exited, fmt.Sprintf("node %d (%v): %s", i, node.exitErr, b6Tail(node.log)))
		default:
		}
	}
	return exited
}

// debugLogs returns the running nodes' output tails when B6_DEBUG is set.
func (w *frostNodeWorld) debugLogs() string {
	if os.Getenv("B6_DEBUG") == "" {
		return ""
	}
	w.nodesMu.Lock()
	defer w.nodesMu.Unlock()
	var out strings.Builder
	fmt.Fprintf(&out, "\nnode logs: %s/operator-*/node.log", w.dir)
	for i, node := range w.nodes {
		log := node.log.String()
		if len(log) > 4000 {
			log = log[len(log)-4000:]
		}
		fmt.Fprintf(&out, "\n--- node %d ---\n%s", i, log)
	}
	return out.String()
}

type b6SyncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *b6SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func b6Tail(b *bytes.Buffer) string {
	s := b.String()
	if len(s) > 600 {
		s = s[len(s)-600:]
	}
	return s
}

// --- Observation ------------------------------------------------------------

// b6Event is one line of a node's record: a message the node sent to the
// network, or a transaction it sent to Bitcoin.
type b6Event struct {
	Operator int
	Kind     string // "net" or "btc"
	Type     string `json:",omitempty"`
	// Coordination message fields (base coordination messages only).
	Window uint64 `json:",omitempty"`
	Wallet string `json:",omitempty"`
	Action string `json:",omitempty"`
	// Bitcoin broadcast fields.
	TxHash string   `json:",omitempty"`
	Inputs []string `json:",omitempty"`
	At     time.Time
}

// events reads every node's record, including those of killed processes.
func (w *frostNodeWorld) events() []b6Event {
	var all []b6Event
	for i := 1; i <= b6Operators; i++ {
		file, err := os.Open(filepath.Join(w.dir, fmt.Sprintf("operator-%d", i), "record.jsonl"))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			var e b6Event
			if json.Unmarshal(scanner.Bytes(), &e) == nil {
				all = append(all, e)
			}
		}
		file.Close()
	}
	return all
}

// --- Block and window driving -----------------------------------------------

// nextWindow jumps to the next coordination window (after at least one
// active phase) and returns its coordination block; see jumpToWindow.
func (w *frostNodeWorld) nextWindow() uint64 {
	w.t.Helper()
	return w.jumpToWindow(tbtc.B6CoordinationFrequencyBlocks)
}

// jumpToWindow jumps to the next coordination block that is a multiple of
// every (a multiple of the window frequency) and at least 40 blocks ahead.
func (w *frostNodeWorld) jumpToWindow(every uint64) uint64 {
	w.t.Helper()
	window := w.chooseWindow(every, nil)
	w.enterWindow(window)
	return window
}

// chooseWindow stops the miner and mines, one candidate at a time, to 32
// blocks before the next coordination block that is a multiple of every,
// until accept (nil accepts any) takes the candidate given its seed block
// hash. The chain is left at window-32 with the miner stopped; enterWindow
// continues. A rejected candidate is skipped in the next jump, so callers
// choose before the state a skipped window could act on exists, and, where
// the case allows, before the nodes start (a jump is then free).
func (w *frostNodeWorld) chooseWindow(every uint64, accept func(window uint64) bool) uint64 {
	t := w.t
	t.Helper()
	w.stopMining()
	for candidates := 0; candidates < 32; candidates++ {
		head, err := w.admin.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		window := (head/every + 1) * every
		if window-head < 40 {
			window += every
		}
		w.mineTo(window - tbtc.B6CoordinationSafeBlockShift)
		if accept == nil || accept(window) {
			return window
		}
	}
	t.Fatalf("setup: no acceptable coordination window in 32 candidates")
	return 0
}

// mineTo mines to height. Base's block counter takes Anvil's new heads one
// header at a time and hands a watcher a block only if the watcher is parked
// at that instant, so in a burst a watcher gets the burst's first block
// alone. With nodes running, a jump therefore goes in bursts of at most 300
// blocks, each followed by single blocks until every running node delivered
// one to a watcher: its backlog is then cleared, and its subscription never
// overflows.
func (w *frostNodeWorld) mineTo(height uint64) {
	t := w.t
	t.Helper()
	for {
		head, err := w.admin.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if head >= height {
			return
		}
		step := height - head
		if !w.running() {
			if err := w.admin.Client().Call(nil, "anvil_mine", fmt.Sprintf("0x%x", step)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if step > 300 {
			step = 300
		}
		if step > 1 {
			if err := w.admin.Client().Call(nil, "anvil_mine", fmt.Sprintf("0x%x", step-1)); err != nil {
				t.Fatal(err)
			}
		}
		w.settleNodes(head + step - 1)
	}
}

// settleNodes mines single blocks, 100 ms apart, until every running node
// delivered one of them to a watcher (at most 5 s).
func (w *frostNodeWorld) settleNodes(head uint64) {
	t := w.t
	t.Helper()
	began := time.Now()
	for time.Since(began) < 5*time.Second {
		if err := w.admin.Client().Call(nil, "evm_mine"); err != nil {
			t.Fatal(err)
		}
		head++
		for wait := time.Now(); time.Since(wait) < 100*time.Millisecond; time.Sleep(5 * time.Millisecond) {
			if w.nodesSaw(head) {
				return
			}
		}
	}
	w.phase(fmt.Sprintf("nodes did not catch up to block %d within 5 s", head))
}

// running reports whether any node process runs.
func (w *frostNodeWorld) running() bool {
	w.nodesMu.Lock()
	defer w.nodesMu.Unlock()
	return len(w.nodes) > 0
}

// enterWindow brings the chain to the window, no sooner than 1 s after the
// last node start. Base's block counter drops
// blocks its watchers are not parked for, so a window block inside a jump's
// burst is lost (review B4): the jump stops 32 blocks short with the nodes
// caught up (mineTo), and the background miner produces the rest one block
// at a time, as a real chain does.
func (w *frostNodeWorld) enterWindow(window uint64) {
	t := w.t
	t.Helper()
	began := time.Now()
	w.mineTo(window - tbtc.B6CoordinationSafeBlockShift)
	// The nodes get 1 s after their start before a window comes.
	if wait := time.Until(w.lastStart.Add(time.Second)); wait > 0 {
		time.Sleep(wait)
	}
	// From here a block every 50 ms: base's 80-block active phase then lasts
	// 4 s, enough for a leader's proposal generation on a one-core node
	// (about 1.8 s measured; 16 minutes in production).
	w.stopMining = w.mineEvery(50 * time.Millisecond)
	if !w.waitBlock(window, 10*time.Second) {
		t.Fatal("setup: the chain did not advance")
	}
	w.phase(fmt.Sprintf("window %d (after %.2f s)", window, time.Since(began).Seconds()))
}

// mineEvery mines a block every interval until the returned function is
// called (the frozen mineInBackground mines every 20 ms).
func (w *frostNodeWorld) mineEvery(interval time.Duration) func() {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = w.admin.Client().Call(nil, "evm_mine")
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); <-stopped }) }
}

// nodesSaw reports whether every running node's block counter reported
// height (each node writes its latest watched block to its "height" file).
func (w *frostNodeWorld) nodesSaw(height uint64) bool {
	w.nodesMu.Lock()
	defer w.nodesMu.Unlock()
	for _, node := range w.nodes {
		raw, err := os.ReadFile(node.config.Height)
		if err != nil {
			return false
		}
		var seen uint64
		if _, err := fmt.Sscan(string(raw), &seen); err != nil || seen < height {
			return false
		}
	}
	return true
}

// waitBlock waits until the chain reaches height, bounded.
func (w *frostNodeWorld) waitBlock(height uint64, bound time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	_ = w.waitForBlock(ctx, height)
	head, err := w.admin.BlockNumber(context.Background())
	return err == nil && head >= height
}

// expectedLeader is D2's leader for a FROST wallet in a coordination window,
// derived from the rule, not from the node: the wallet's unique operators,
// sorted ascending, shuffled with the first 8 bytes of
// sha256(wallet 20-byte Bridge key || hash of block window-32); the first one
// leads.
func (w *frostNodeWorld) expectedLeader(wallet b6Wallet, window uint64) common.Address {
	t := w.t
	t.Helper()
	header, err := w.admin.HeaderByNumber(context.Background(), new(big.Int).SetUint64(window-tbtc.B6CoordinationSafeBlockShift))
	if err != nil {
		t.Fatal(err)
	}
	var operators []chain.Address
	for _, o := range wallet.operators {
		operators = append(operators, chain.Address(o.Hex()))
	}
	return common.HexToAddress(string(b6Leader(wallet.label, header.Hash(), operators)))
}

// --- Bitcoin -----------------------------------------------------------------

// drainInto gives the FROST wallet a pending incoming output: the legacy
// MovingFunds source commits its main to the wallet (fixture member), the
// FIXTURE source key signs the drain, Core mines it, and the test, as SPV
// maintainer, proves it. Returns the drain transaction.
func (w *frostNodeWorld) drainInto(wallet b6Wallet) *bitcoin.Transaction {
	w.t.Helper()
	return w.drainFrom(w.source, w.main, wallet)
}

// drainFrom drains the given legacy MovingFunds source's main into the FROST
// wallet; see drainInto.
func (w *frostNodeWorld) drainFrom(source legacyWallet, main *bitcoin.UnspentTransactionOutput, wallet b6Wallet) *bitcoin.Transaction {
	t := w.t
	t.Helper()
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(operatorKey, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.legacy.SubmitMovingFundsCommitment(opts, source.pkh, legacyUTXO(main), []uint32{1}, big.NewInt(1), [][20]byte{wallet.label})
	if err != nil {
		t.Fatalf("setup: drain commitment: %v", err)
	}
	if receipt, err := legacyWaitMined(context.Background(), w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: drain commitment not mined: %v", err)
	}
	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddPublicKeyHashInput(main); err != nil {
		t.Fatal(err)
	}
	script, err := bitcoin.PayToTaproot(wallet.q)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: main.Value - frostB6DrainFee, PublicKeyScript: script})
	drain := legacySign(t, builder, source.key)
	w.btc.mine(t, drain)
	w.send("drain proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsProof(o, legacyTxInfo(drain), w.btc.proof(t, drain.Hash()), legacyUTXO(main), source.pkh)
	})
	w.phase("drain proven")
	if state := w.incomingState(drain); state != 1 {
		t.Fatalf("setup: the drain did not record a pending FROST incoming output (state %d)", state)
	}
	return drain
}

const frostB6DrainFee = int64(2_000)

// incomingState is the Bridge's record of the drain's FROST incoming output
// (1 = Pending).
func (w *frostNodeWorld) incomingState(drain *bitcoin.Transaction) uint8 {
	t := w.t
	t.Helper()
	key := deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: drain.Hash(), OutputIndex: 0})
	var out []interface{}
	if err := w.frost.Call(&bind.CallOpts{}, &out, "frostIncoming", key); err != nil || len(out) != 1 {
		t.Fatalf("frostIncoming: %v", err)
	}
	var record struct {
		Target, Source   [32]byte
		Value, CreatedAt uint64
		State            uint8
		SweepFeeCap      uint64
	}
	record = *abi.ConvertType(out[0], &record).(*struct {
		Target, Source   [32]byte
		Value, CreatedAt uint64
		State            uint8
		SweepFeeCap      uint64
	})
	return record.State
}

// spenders returns the distinct transactions any node broadcast that spend
// the outpoint, from every node record (killed processes included).
func (w *frostNodeWorld) spenders(outpoint bitcoin.TransactionOutpoint) []string {
	want := fmt.Sprintf("%s:%d", outpoint.TransactionHash.Hex(bitcoin.ReversedByteOrder), outpoint.OutputIndex)
	seen := map[string]bool{}
	var hashes []string
	for _, e := range w.events() {
		if e.Kind != "btc" {
			continue
		}
		for _, input := range e.Inputs {
			if input == want && !seen[e.TxHash] {
				seen[e.TxHash] = true
				hashes = append(hashes, e.TxHash)
			}
		}
	}
	return hashes
}

// settleIncoming waits until a node broadcast a transaction spending the
// drain output, mines it as Core would, proves it as SPV maintainer, and
// checks the Bridge no longer holds the output Pending. It returns the sweep
// hash and false when no sweep came within bound.
func (w *frostNodeWorld) settleIncoming(wallet b6Wallet, drain *bitcoin.Transaction, bound time.Duration) (string, bool) {
	t := w.t
	t.Helper()
	outpoint := bitcoin.TransactionOutpoint{TransactionHash: drain.Hash(), OutputIndex: 0}
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if spenders := w.spenders(outpoint); len(spenders) > 0 {
			// The node broadcast through its own adapter, in its own
			// process: read the sweep from Core (review B1).
			sweep, err := w.coreTransaction(spenders[0])
			if err != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			w.mineCore(sweep)
			w.send("incoming sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
				return w.frost.Transact(o, "submitFrostMovedFundsSweepProof", wallet.id, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()))
			})
			if state := w.incomingState(drain); state == 1 {
				t.Fatalf("the Bridge still holds the incoming output Pending after the proven sweep %s", spenders[0])
			}
			return spenders[0], true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", false
}

// --- The node process -------------------------------------------------------

// TestB6NodeProcess is not a test: it is a node process started by
// frostNodeWorld.startNode (B6_NODE_CONFIG set), and is skipped otherwise.
func TestB6NodeProcess(t *testing.T) {
	path := os.Getenv("B6_NODE_CONFIG")
	if path == "" {
		t.Skip("node process only")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config b6NodeConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	status := func(s string) {
		// Atomic, so the parent never reads a partial status.
		_ = os.WriteFile(config.Status+".tmp", []byte(s), 0o600)
		_ = os.Rename(config.Status+".tmp", config.Status)
	}
	if err := runB6Node(t, config); err != nil {
		status(err.Error())
		os.Exit(1)
	}
	status("started")
	select {}
}

// runB6Node builds what cmd/start.go builds before tbtc.Initialize and hands
// it to the seam.
func runB6Node(t *testing.T, config b6NodeConfig) error {
	ctx := context.Background()
	began := time.Now()
	trace := func(what string) {
		if os.Getenv("B6_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "b6 node %d %s %.2f s\n", config.Operator, what, time.Since(began).Seconds())
		}
	}
	defer trace("initialised")
	ethereumConfig := commonethereum.Config{
		Network: commonethereum.Developer, URL: config.AnvilURL,
		Account:             commonethereum.Account{KeyFile: config.KeyFile, KeyFilePassword: b6KeyPassword},
		ContractAddresses:   config.Contracts,
		MiningCheckInterval: time.Second,
	}
	_, tbtcChain, blockCounter, _, operatorKey, err := chainethereum.Connect(ctx, ethereumConfig)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	trace("connected")
	// Observation for the window driver: the latest block base's block
	// counter delivered to a watcher (review B4). It adds a watcher; it does
	// not change the node's.
	var latest atomic.Uint64
	go func() {
		// Parked again at once, so this watcher drops no block of its own.
		for height := range blockCounter.WatchBlocks(ctx) {
			latest.Store(height)
		}
	}()
	go func() {
		written := uint64(0)
		for range time.Tick(10 * time.Millisecond) {
			if height := latest.Load(); height != written {
				written = height
				_ = os.WriteFile(config.Height+".tmp", []byte(fmt.Sprint(height)), 0o600)
				_ = os.Rename(config.Height+".tmp", config.Height)
			}
		}
	}()
	record, err := os.OpenFile(config.Record, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	recorder := &b6Recorder{operator: config.Operator, file: record}

	var allowed []*operator.PublicKey
	for _, k := range config.AllowKeys {
		raw, err := hex.DecodeString(k)
		if err != nil {
			return err
		}
		key, err := crypto.UnmarshalPubkey(raw)
		if err != nil {
			return err
		}
		allowed = append(allowed, &operator.PublicKey{Curve: operator.Secp256k1, X: key.X, Y: key.Y})
	}
	// The base firewall: applications first, then base's own allow list
	// (as cmd/start_test.go builds it); the allow list holds the operators.
	policy := firewall.AnyApplicationPolicy([]firewall.Application{tbtcChain}, firewall.NewAllowList(allowed))
	provider, err := libp2p.Connect(ctx, libp2p.Config{Port: config.Port, Peers: config.Peers}, operatorKey, policy,
		retransmission.NewTicker(blockCounter.WatchBlocks(ctx)))
	if err != nil {
		return fmt.Errorf("network: %w", err)
	}
	trace("network")
	// Observation for the harness: how many peers the node is connected to.
	go func() {
		for range time.Tick(20 * time.Millisecond) {
			count := len(provider.ConnectionManager().ConnectedPeers())
			_ = os.WriteFile(config.PeersFile+".tmp", []byte(fmt.Sprint(count)), 0o600)
			_ = os.Rename(config.PeersFile+".tmp", config.PeersFile)
		}
	}()

	disk, err := storage.Initialize(storage.Config{Dir: config.StorageDir}, b6KeyPassword)
	if err != nil {
		return err
	}
	keyStore, err := disk.InitializeKeyStorePersistence("tbtc")
	if err != nil {
		return err
	}
	work, err := disk.InitializeWorkPersistence("tbtc")
	if err != nil {
		return err
	}

	core := &legacyCore{url: config.CoreURL, user: config.CoreUser, pass: config.CorePass}
	live := &b6LiveBitcoin{inner: &legacyRegtest{core: core, miner: config.CoreMiner}}
	if err := live.refresh(); err != nil {
		return fmt.Errorf("bitcoin: %w", err)
	}
	trace("bitcoin synced")
	btc := &b6BitcoinRecorder{Chain: live, recorder: recorder}

	pin, err := hex.DecodeString(config.WorkerSHA256)
	if err != nil || len(pin) != 32 {
		return fmt.Errorf("worker pin: %v", err)
	}
	host := frostNodeHost{
		Chain:           &b6StakingFixture{TbtcChain: tbtcChain},
		BitcoinChain:    btc,
		Network:         &b6RecordingProvider{Provider: provider, recorder: recorder},
		KeyStore:        keyStore,
		Work:            work,
		Scheduler:       b6Scheduler(),
		Proposals:       tbtcpg.NewProposalGenerator(tbtcChain, btc),
		Config:          tbtc.Config{PreParamsPoolSize: 0, PreParamsGenerationTimeout: time.Minute, PreParamsGenerationDelay: time.Hour, PreParamsGenerationConcurrency: 1, KeyGenerationConcurrency: 1},
		EthereumConfig:  ethereumConfig,
		EthereumNetwork: commonethereum.Developer,
		FrostRegistry:   common.HexToAddress(config.FrostRegistry),
		Worker:          snowfallengine.WorkerConfig{Path: config.Worker, SHA256: [32]byte(pin)},
		Store:           store.Config{Keystore: config.FrostKeystore, StorageID: [32]byte{byte(config.Operator)}, Key: [32]byte{17}, Fence: b6DiskFence{config.FrostFence}},
	}
	return frostNodeStart(ctx, host)
}

// FIXTURE: b6Scheduler is base's scheduler with ECDSA pre-parameter
// generation held off: a registered protocol that always executes stops its
// computations at its first check (within 1 s), as a running DKG does. Base
// generates TSS pre-parameters even for a pool of size 0, which took 5-6 s of
// a node's one core; legacy ECDSA DKG is not under test here.
func b6Scheduler() *generator.Scheduler {
	scheduler := generator.StartScheduler()
	scheduler.RegisterProtocol(b6Busy{})
	return scheduler
}

type b6Busy struct{}

func (b6Busy) IsExecuting() bool { return true }

// FIXTURE: b6StakingFixture answers the ECDSA sortition-pool reads node
// startup makes (sortition.MonitorPool), which the legacy registry fixture
// does not serve: the operator is registered, in the pool and up to date.
// Everything else is the real TbtcChain.
type b6StakingFixture struct{ *chainethereum.TbtcChain }

func (f *b6StakingFixture) OperatorToStakingProvider() (chain.Address, bool, error) {
	return f.Signing().Address(), true, nil
}
func (f *b6StakingFixture) IsOperatorInPool() (bool, error)   { return true, nil }
func (f *b6StakingFixture) IsOperatorUpToDate() (bool, error) { return true, nil }

// IsWalletRegistered answers base's startup check of the ECDSA wallets a
// node holds signers for (archiveClosedWallets), which the legacy registry
// fixture does not serve: the fixture never closes a wallet, so every
// published wallet stays registered (K-09 seeds legacy signers, #40).
func (f *b6StakingFixture) IsWalletRegistered([32]byte) (bool, error) { return true, nil }

// b6Recorder appends events to the node's record file.
type b6Recorder struct {
	operator int
	mu       sync.Mutex
	file     *os.File
}

func (r *b6Recorder) add(e b6Event) {
	e.Operator, e.At = r.operator, time.Now()
	line, _ := json.Marshal(e)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.file.Write(append(line, '\n'))
	_ = r.file.Sync()
}

// FIXTURE (observation only): b6RecordingProvider records each message the
// node sends on a broadcast channel; base coordination messages are decoded
// into window, wallet and action.
type b6RecordingProvider struct {
	net.Provider
	recorder *b6Recorder
}

func (p *b6RecordingProvider) BroadcastChannelFor(name string) (net.BroadcastChannel, error) {
	channel, err := p.Provider.BroadcastChannelFor(name)
	if err != nil {
		return nil, err
	}
	return &b6RecordingChannel{BroadcastChannel: channel, recorder: p.recorder}, nil
}

type b6RecordingChannel struct {
	net.BroadcastChannel
	recorder *b6Recorder
}

func (c *b6RecordingChannel) Send(ctx context.Context, m net.TaggedMarshaler, strategy ...net.RetransmissionStrategy) error {
	e := b6Event{Kind: "net", Type: m.Type()}
	if window, wallet, action, ok := tbtc.B6CoordinationMessage(m); ok {
		e.Window, e.Wallet, e.Action = window, hex.EncodeToString(wallet[:]), action
	}
	c.recorder.add(e)
	return c.BroadcastChannel.Send(ctx, m, strategy...)
}

// FIXTURE (observation only): b6BitcoinRecorder records each transaction the
// node broadcasts, with its inputs.
type b6BitcoinRecorder struct {
	bitcoin.Chain
	recorder *b6Recorder
}

// UnspentByScript forwards the FROST script query (review B3).
func (b *b6BitcoinRecorder) UnspentByScript(ctx context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	return b.Chain.(*b6LiveBitcoin).UnspentByScript(ctx, script)
}

func (b *b6BitcoinRecorder) BroadcastTransaction(tx *bitcoin.Transaction) error {
	var inputs []string
	for _, input := range tx.Inputs {
		inputs = append(inputs, fmt.Sprintf("%s:%d", input.Outpoint.TransactionHash.Hex(bitcoin.ReversedByteOrder), input.Outpoint.OutputIndex))
	}
	b.recorder.add(b6Event{Kind: "btc", TxHash: tx.Hash().Hex(bitcoin.ReversedByteOrder), Inputs: inputs})
	return b.Chain.BroadcastTransaction(tx)
}

// FIXTURE: b6DiskFence is the FROST store's external fence on a local file,
// shared by every process of one operator (flock; a killed holder's lock is
// released by the kernel). Copied from batch 4's diskFence.
type b6DiskFence struct{ path string }
type b6DiskLease struct {
	path string
	lock *os.File
}

func (f b6DiskFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	lock, err := os.OpenFile(f.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, store.ErrBusy
	}
	return &b6DiskLease{f.path, lock}, nil
}
func (l *b6DiskLease) Head(context.Context) (store.Point, error) {
	var p store.Point
	b, err := os.ReadFile(l.path)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}
func (l *b6DiskLease) Advance(ctx context.Context, before, after store.Point) error {
	p, err := l.Head(ctx)
	if err != nil {
		return err
	}
	if p != before || after.Sequence != before.Sequence+1 {
		return store.ErrQuarantined
	}
	b, _ := json.Marshal(after)
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}
func (l *b6DiskLease) Close() error { return l.lock.Close() }

// secondSource registers a second legacy wallet, funds its main through a
// revealed deposit swept with its own test key (as fundSource does for the
// first), and moves it to MovingFunds through the registry fixture's
// heartbeat failure, as newLegacyWorldOn does for the first source.
func (w *frostNodeWorld) secondSource() (legacyWallet, *bitcoin.UnspentTransactionOutput) {
	t := w.t
	t.Helper()
	source := newLegacyWallet(t, 13)
	w.registerWallet(source)
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &tbtc.Deposit{
		Depositor:           chain.Address(admin.Hex()),
		BlindingFactor:      [8]byte{0x2e, 0x9b},
		WalletPublicKeyHash: source.pkh,
		RefundPublicKeyHash: [20]byte{9},
	}
	binary.LittleEndian.PutUint32(d.RefundLocktime[:], uint32(header.Time+30*24*3600))
	script, err := d.Script()
	if err != nil {
		t.Fatal(err)
	}
	locking, err := bitcoin.PayToWitnessScriptHash(bitcoin.WitnessScriptHash(script))
	if err != nil {
		t.Fatal(err)
	}
	scriptHash := sha256.Sum256(script)
	address, err := btcutil.NewAddressWitnessScriptHash(scriptHash[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	funding := w.btc.pay(t, address.EncodeAddress(), legacyDepositValue)
	index := -1
	for i, output := range funding.Outputs {
		if bytes.Equal(output.PublicKeyScript, locking) && output.Value == legacyDepositValue {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("setup: Core did not create the deposit output")
	}
	w.send("reveal second deposit", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.RevealDeposit(o, legacyTxInfo(funding), tbtcabi.DepositDepositRevealInfo{
			FundingOutputIndex: uint32(index), BlindingFactor: d.BlindingFactor, WalletPubKeyHash: d.WalletPublicKeyHash,
			RefundPubKeyHash: d.RefundPublicKeyHash, RefundLocktime: d.RefundLocktime,
		})
	})
	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddScriptHashInput(&bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: uint32(index)},
		Value:    legacyDepositValue,
	}, script); err != nil {
		t.Fatal(err)
	}
	walletScript, err := bitcoin.PayToWitnessPublicKeyHash(source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: legacyDepositValue - legacySweepFee, PublicKeyScript: walletScript})
	sweep := legacySign(t, builder, source.key)
	w.btc.mine(t, sweep)
	w.send("second sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitDepositSweepProof(o, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()), tbtcabi.BitcoinTxUTXO{}, common.Address{})
	})
	w.send("second heartbeat failure", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "heartbeatFailed", source.id,
			common.BytesToHash(source.key.PublicKey.X.FillBytes(make([]byte, 32))),
			common.BytesToHash(source.key.PublicKey.Y.FillBytes(make([]byte, 32))))
	})
	state, err := w.legacy.Wallets(&bind.CallOpts{}, source.pkh)
	if err != nil || state.State != 2 {
		t.Fatalf("setup: second source not in MovingFunds: %v %+v", err, state)
	}
	return source, &bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: sweep.Hash(), OutputIndex: 0},
		Value:    legacyDepositValue - legacySweepFee,
	}
}

// requestRedemption requests a FROST redemption from the wallet as the
// redeemer does (the admin's Bank balance from the legacy deposits) and
// returns the request ID.
func (w *frostNodeWorld) requestRedemption(wallet b6Wallet, script []byte, gross uint64) [32]byte {
	t := w.t
	t.Helper()
	bankABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"approveBalance","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"}]`))
	if err != nil {
		t.Fatal(err)
	}
	bank := bind.NewBoundContract(w.bank, bankABI, w.admin, w.admin, w.admin)
	w.send("approve redemption balance", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return bank.Transact(o, "approveBalance", w.bridge, new(big.Int).SetUint64(gross))
	})
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	opts := w.opts()
	tx, err := w.frost.Transact(opts, "requestFrostRedemption", wallet.id, script, gross, admin)
	if err != nil {
		t.Fatalf("setup: FROST redemption request: %v", err)
	}
	receipt, err := legacyWaitMined(context.Background(), w.admin, tx)
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: FROST redemption request not mined: %v", err)
	}
	topic := crypto.Keccak256Hash([]byte("FrostRedemptionRequested(bytes32,bytes32,address,uint64,uint64,uint64,uint64)"))
	for _, l := range receipt.Logs {
		if len(l.Topics) == 3 && l.Topics[0] == topic && l.Topics[2] == common.Hash(wallet.id) {
			return l.Topics[1]
		}
	}
	t.Fatal("setup: no FrostRedemptionRequested log")
	return [32]byte{}
}

// reservationAdmitted waits for the Bridge's FrostReservationAdmitted log for
// the wallet and returns the reservation ID. request is kept for the failure
// message only: the log does not name requests.
func (w *frostNodeWorld) reservationAdmitted(wallet b6Wallet, request [32]byte, bound time.Duration) ([32]byte, bool) {
	topic := crypto.Keccak256Hash([]byte("FrostReservationAdmitted(bytes32,bytes32,bytes32,bytes32,uint64,uint64)"))
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		logs, err := w.admin.FilterLogs(context.Background(), ethereum.FilterQuery{
			Addresses: []common.Address{w.bridge},
			Topics:    [][]common.Hash{{topic}, nil, {common.Hash(wallet.id)}},
		})
		if err == nil && len(logs) > 0 {
			return logs[0].Topics[1], true
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = request
	return [32]byte{}, false
}
