package tbtc_test

// K-09 lifecycle demonstration: world and stage snapshots.
// Spec: KC-SF-V3 §6 K-09, SC-01..SC-10; plan K09-HARNESS-PLAN-v2.
//
// The world is batch 6's (frost_node_b6_world_test.go): base node processes
// started through the same node initialisation cmd/start.go calls
// (tbtc.Initialize, FROST configured through the bound seam), the real FROST
// registry stack, Bitcoin Core regtest and Anvil. Two differences:
//   - the Bridge starts as the pre-FROST 40a11d1d build (D1) and is upgraded
//     in place through its ProxyAdmin, as drain_upgrade_test.go does;
//   - the FROST group is 5 seats with threshold 3, held by weighted operators
//     {1,1,2,2,3}: no operator reaches the threshold alone (DECISIONS.md D2
//     topology, tux 2026-10-28).
//
// Stages: each stage starts from its predecessor's snapshot and, on the main
// chain, leaves a snapshot for its successor. A snapshot is an Anvil state
// dump, a stopped copy of Core's datadir, the operators' node storage (copied,
// never read) and the public facts; it lives in batch 6's stage cache
// (b6CacheRoot, keyed by the test binary and the pins) under "k09-<stage>".
// The joined run is the chain of stages built in one test run, in order.
//
// Actors (D17): the harness is depositor, redeemer, SPV maintainer, keeper
// and governance, each through public calls with the admin key, never with an
// operator's key. FIXTURES are those batch 6 labels, plus the legacy source's
// test key that signs the ECDSA-to-FROST drain transaction (legacy tECDSA
// signers are not seeded into the nodes; see K09-RUN.md "not covered").

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// k09Pattern is the K-09 seat pattern: operators 1 and 2 hold two seats each,
// operator 3 one; threshold 3 (k09Threshold).
var k09Pattern = []int{1, 1, 2, 2, 3}

const (
	k09Seats     = 5
	k09Threshold = 3
	// k09DKGBound bounds a node-run 5-seat FROST DKG to ReadyUnfunded (it only
	// sets how long a failing stage waits).
	k09DKGBound = 30 * time.Second
	// k09Action bounds a node action after its window (active phase plus
	// signing), as b6Sweep does.
	k09Action = 40 * time.Second
)

// k09Facts are the K-09 facts a stage hands to its successors, beside batch
// 6's b6Facts (which carries wallet A, the first node-made FROST wallet).
type k09Facts struct {
	// SourceState is the legacy source wallet as stage 01 recorded it.
	SourceState string
	// MainA is wallet A's main outpoint after the last node action, as
	// "txid:index", and its value.
	MainA      string
	MainAValue int64
	// Epochs requested so far.
	Epoch uint64
}

// k09World is the batch-6 world with the K-09 facts.
type k09World struct {
	*frostNodeWorld
	facts  k09Facts
	bridge *frostbridgeabi.FrostBridge
	wallet b6Wallet
}

// k09StageDir is the cache name of a K-09 stage in this run. K09_RUN_ID
// (test/k09/run.sh sets a fresh one) keeps a run from starting a stage on a
// snapshot an earlier run left: a stage whose predecessor failed in this run
// is blocked, never fed an older snapshot.
func k09StageDir(stage string) string {
	run := os.Getenv("K09_RUN_ID")
	if run == "" {
		// A plain go test runs every stage in this one process.
		run = fmt.Sprintf("local-%d", os.Getpid())
	}
	return "k09-" + run + "-" + stage
}

// k09Load returns a fresh copy of the stage's snapshot. A missing snapshot
// means the stage failed or did not run earlier in this run: the caller's
// stage is then blocked, and fails saying so.
func k09Load(t *testing.T, stage string) *k09World {
	t.Helper()
	env := b6Environment(t)
	root := b6CacheRoot(t)
	dir := filepath.Join(root, k09StageDir(stage))
	raw, err := os.ReadFile(filepath.Join(dir, "k09.json"))
	if err != nil {
		t.Fatalf("K-09 blocked: predecessor stage %s left no snapshot in this run (it failed or did not run)", stage)
	}
	w := &k09World{frostNodeWorld: k09LoadStage(t, env, root, k09StageDir(stage))}
	if err := json.Unmarshal(raw, &w.facts); err != nil {
		t.Fatal(err)
	}
	w.bind()
	if w.stageWallet != nil {
		w.wallet = *w.stageWallet
	}
	return w
}

func (w *k09World) bind() {
	var err error
	if w.bridge, err = frostbridgeabi.NewFrostBridge(w.legacyWorld.bridge, w.admin); err != nil {
		w.t.Fatal(err)
	}
}

// k09Save snapshots the world as the stage's successors start from it. The
// nodes are killed first (after quiet seconds, so they finish local writes:
// a quiet point, not a crash test), then the miner, then Core.
func (w *k09World) save(stage string, quiet time.Duration) {
	t := w.t
	t.Helper()
	began := time.Now()
	if w.running() {
		time.Sleep(quiet)
	}
	w.killAll()
	w.stopMining()
	root := b6CacheRoot(t)
	building, err := os.MkdirTemp(root, k09StageDir(stage)+".building-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(building)
	var state string
	if err := w.admin.Client().Call(&state, "anvil_dumpState"); err != nil {
		t.Fatalf("setup: anvil_dumpState: %v", err)
	}
	if err := os.WriteFile(filepath.Join(building, "anvil.state"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	w.copyStoppedCore(filepath.Join(building, "core"))
	facts := w.frostNodeWorld.facts()
	facts.CoreStage = k09StageDir(stage)
	if w.wallet.id != ([32]byte{}) {
		facts.Epoch, facts.WalletID, facts.WalletQ, facts.WalletOperators = w.stageEpoch, w.wallet.id, w.wallet.q, w.wallet.operators
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
	for name, v := range map[string]interface{}{"facts.json": facts, "k09.json": w.facts} {
		raw, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(building, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	final := filepath.Join(root, k09StageDir(stage))
	_ = os.RemoveAll(final)
	if err := os.Rename(building, final); err != nil {
		t.Fatal(err)
	}
	t.Logf("K09TIME stage=%s save=%.2f", stage, time.Since(began).Seconds())
}

// k09BuildLegacy is stage 01's world: the pre-FROST 40a11d1d Bridge (D1)
// with the legacy wallets of the legacy-unchanged suite (a MovingFunds source
// with a proven main, a Live target), and the operators' keys and ports.
func k09BuildLegacy(t *testing.T) *k09World {
	t.Helper()
	env := b6Environment(t)
	w := &k09World{frostNodeWorld: newFrostNodeWorldShell(t)}
	w.legacyWorld = newLegacyWorldOn(t, env, openLegacyBaseline(t, env), nil)
	w.finish()
	return w
}

// k09LegacyState is the legacy part of the Bridge a K-09 stage must leave
// unchanged unless it acts on it: both legacy wallets and the depositor's
// Bank balance.
func (w *k09World) legacyState() string {
	t := w.t
	t.Helper()
	source, err := w.legacy.Wallets(&bind.CallOpts{}, w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	target, err := w.legacy.Wallets(&bind.CallOpts{}, w.target.pkh)
	if err != nil {
		t.Fatal(err)
	}
	balance := w.bankBalance(crypto.PubkeyToAddress(w.adminKey.PublicKey))
	return fmt.Sprintf("source %+v; target %+v; depositor bank balance %s", source, target, balance)
}

func (w *k09World) bankBalance(owner common.Address) *big.Int {
	t := w.t
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"balanceOf","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"}]`))
	if err != nil {
		t.Fatal(err)
	}
	var out []interface{}
	if err := bind.NewBoundContract(w.bank, parsed, w.admin, w.admin, w.admin).Call(&bind.CallOpts{}, &out, "balanceOf", owner); err != nil {
		t.Fatal(err)
	}
	return out[0].(*big.Int)
}

// upgradeBridge replaces the 40a11d1d implementation behind the Bridge's
// proxy with the integration build, through the proxy's own ProxyAdmin (as
// drain_upgrade_test.go does), and returns the new implementation.
func (w *k09World) upgradeBridge() common.Address {
	t := w.t
	t.Helper()
	implementation, _ := w.deployBaseline(map[string]common.Address{}, "contracts/bridge/Bridge.sol", "Bridge")
	slot, err := w.admin.StorageAt(context.Background(), w.legacyWorld.bridge, drainProxyAdminSlot, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyAdmin := bind.NewBoundContract(common.BytesToAddress(slot),
		w.baseline.buildABI("@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol", "ProxyAdmin"),
		w.admin, w.admin, w.admin)
	w.send("Bridge upgrade", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return proxyAdmin.Transact(o, "upgrade", w.legacyWorld.bridge, implementation)
	})
	return implementation
}

// k09ImplementationSlot is the EIP-1967 implementation slot.
var k09ImplementationSlot = common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

func (w *k09World) implementation() common.Address {
	raw, err := w.admin.StorageAt(context.Background(), w.legacyWorld.bridge, k09ImplementationSlot, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return common.BytesToAddress(raw)
}

// setUpFrost is batch 6's setUpFrostRegistry (frozen, 3 seats) with the K-09
// group policy: 5 seats, threshold 3, readiness 3. Everything else is the
// same governance configuration.
func (w *k09World) setUpFrost() {
	lw := w.legacyWorld
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
	w.pool, w.beacon = bound(poolAddress, poolABI), bound(beaconAddress, beaconABI)
	w.poolAddress, w.beaconAddress = poolAddress, beaconAddress
	call("pool chaosnet off", w.pool, "deactivateChaosnet")
	admissionAddress, admissionABI := w.b6Deploy("contracts/frost/FrostAdmission.sol", "FrostAdmission", stakeAddress)
	validatorAddress, _ := w.b6Deploy("contracts/frost/FrostDkgValidator.sol", "FrostDkgValidator",
		uint16(k09Seats), uint16(k09Threshold), uint16(k09Threshold), uint16(0), uint16(0))
	w.registryAddress, _ = w.b6Deploy("contracts/frost/FrostWalletRegistry.sol", "FrostWalletRegistry",
		poolAddress, validatorAddress, admissionAddress, beaconAddress, lw.bridge,
		[4]uint64{10000, 1000, 10, 1000}, uint16(3))
	var err error
	if w.registry, err = binding.NewFrostWalletRegistry(w.registryAddress, lw.admin); err != nil {
		t.Fatal(err)
	}
	call("pool owner", w.pool, "transferOwnership", w.registryAddress)
	call("FROST registry", w.frost, "configureFrostRegistry", w.registryAddress)
	libraries := map[string]common.Address{}
	verifier, _ := lw.deployBaseline(libraries, "contracts/frost/FrostVerifier.sol", "FrostVerifier")
	code, err := lw.admin.CodeAt(context.Background(), verifier, nil)
	if err != nil {
		t.Fatal(err)
	}
	genesis := chaincfg.RegressionNetParams.GenesisHash
	call("FROST funding", w.frost, "configureFrostFunding", verifier, crypto.Keccak256Hash(code),
		crypto.Keccak256Hash([]byte("UNQUALIFIED K-09 local test")), [32]byte(*genesis))
	exposureAddress, exposureABI := lw.deployBaseline(libraries, "contracts/frost/FrostExposureController.sol", "FrostExposureController", lw.bridge, admin)
	call("C-10 budget", bound(exposureAddress, exposureABI), "openBudgetPeriod", ^uint64(0), ^uint64(0),
		crypto.Keccak256Hash([]byte("C-10 test budget; D-10 OPEN")))
	call("C-10 controller", w.frost, "installFrostExposureController", exposureAddress)
	call("FROST gates", w.frost, "setFrostFundingGates", true, true)
	call("FROST drain enabled", w.frost, "setFrostDrainEnabled", true)
	stake, admission := bound(stakeAddress, stakeABI), bound(admissionAddress, admissionABI)
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
	call("FROST redemptions", w.frost, "configureFrostRedemptions", [32]byte(*genesis), uint64(2), uint64(1000), common.Address{},
		crypto.Keccak256Hash([]byte("UNQUALIFIED K-09 local D-08 reference")))
	call("FROST redemptions enabled", w.frost, "setFrostRedemptionsEnabled", true)
	call("registry requests", registry, "setRequestsEnabled", true)
	call("Bridge FROST requests", w.frost, "setFrostRequestsEnabled", true)
}

// --- Chain-state reads (the stages' oracles) --------------------------------

// custody is the Bridge's custody record of a FROST wallet.
func (w *k09World) custody(wallet b6Wallet) frostbridgeabi.FrostCustodyWallet {
	c, err := w.bridge.FrostWalletCustody(&bind.CallOpts{}, wallet.id)
	if err != nil {
		w.t.Fatal(err)
	}
	return c
}

func (w *k09World) lifecycle(wallet b6Wallet) frostbridgeabi.FrostMovementStateLifecycle {
	l, err := w.bridge.FrostWalletLifecycle(&bind.CallOpts{}, wallet.id)
	if err != nil {
		w.t.Fatal(err)
	}
	return l
}

func (w *k09World) obligations(wallet b6Wallet) uint64 {
	o, err := w.bridge.FrostClosureObligations(&bind.CallOpts{}, wallet.id)
	if err != nil {
		w.t.Fatal(err)
	}
	return o.Uint64()
}

func (w *k09World) creationMode() uint8 {
	m, err := w.bridge.FrostCreationMode(&bind.CallOpts{})
	if err != nil {
		w.t.Fatal(err)
	}
	return m
}

// walletEvents counts the Bridge's FROST lifecycle events for the wallet
// (indexed by wallet ID), by event signature.
func (w *k09World) walletEvents(wallet b6Wallet, signature string) int {
	logs, err := w.admin.FilterLogs(context.Background(), ethereum.FilterQuery{
		Addresses: []common.Address{w.legacyWorld.bridge},
		Topics:    [][]common.Hash{{crypto.Keccak256Hash([]byte(signature))}, {common.Hash(wallet.id)}},
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return len(logs)
}

const (
	k09MovingFundsEvent = "FrostWalletMovingFunds(bytes32,bytes32)"
	k09ClosingEvent     = "FrostWalletClosing(bytes32)"
	k09ClosedEvent      = "FrostWalletClosed(bytes32)"
)

// chooseWindowWithin is batch 6's chooseWindow (frozen, 32 candidates) with
// a larger candidate limit, for rules that accept about one window in 16
// (base's heartbeat probability).
func (w *k09World) chooseWindowWithin(every uint64, accept func(window uint64) bool, limit int) uint64 {
	t := w.t
	t.Helper()
	w.stopMining()
	for candidates := 0; candidates < limit; candidates++ {
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
	t.Fatalf("setup: no acceptable coordination window in %d candidates", limit)
	return 0
}

// outpoint formats an outpoint as "txid:index".
func k09Outpoint(o bitcoin.TransactionOutpoint) string {
	return fmt.Sprintf("%s:%d", o.TransactionHash.Hex(bitcoin.ReversedByteOrder), o.OutputIndex)
}

func k09ParseOutpoint(t *testing.T, s string) bitcoin.TransactionOutpoint {
	t.Helper()
	var txid string
	var index uint32
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		t.Fatalf("setup: outpoint %q", s)
	}
	txid = parts[0]
	fmt.Sscan(parts[1], &index)
	hash, err := bitcoin.NewHashFromString(txid, bitcoin.ReversedByteOrder)
	if err != nil {
		t.Fatal(err)
	}
	return bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: index}
}

// assertMain checks that the Bridge's custody main of the wallet is the given
// output of a transaction Core confirmed, with that output's value.
func (w *k09World) assertMain(stage string, wallet b6Wallet, tx *bitcoin.Transaction, index uint32) {
	t := w.t
	t.Helper()
	outpoint := bitcoin.TransactionOutpoint{TransactionHash: tx.Hash(), OutputIndex: index}
	c := w.custody(wallet)
	if c.MainOutpoint != deposit.OutpointKey(outpoint) || c.Value != uint64(tx.Outputs[index].Value) {
		t.Fatalf("K-09 %s: the Bridge's custody main of wallet %x is %x (value %d), not %s (value %d)", stage, wallet.label, c.MainOutpoint, c.Value, k09Outpoint(outpoint), tx.Outputs[index].Value)
	}
	confirmations, err := w.coreConfirmations(tx.Hash().Hex(bitcoin.ReversedByteOrder))
	if err != nil || confirmations == 0 {
		t.Fatalf("K-09 %s: Core has not confirmed %s (%d confirmations, %v)", stage, tx.Hash().Hex(bitcoin.ReversedByteOrder), confirmations, err)
	}
	w.facts.MainA, w.facts.MainAValue = k09Outpoint(outpoint), tx.Outputs[index].Value
}

// nodeTransaction reads a node's transaction from Core. The node's recorder
// logs a broadcast just before Core receives it, so the read is retried for
// up to 5 s; a transaction Core never accepted fails it.
func (w *k09World) nodeTransaction(txid string) (*bitcoin.Transaction, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		tx, err := w.coreTransaction(txid)
		if err == nil || time.Now().After(deadline) {
			return tx, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// increaseTime moves Ethereum time forward and mines a block.
func (w *k09World) increaseTime(seconds uint64) {
	t := w.t
	t.Helper()
	if err := w.admin.Client().Call(nil, "evm_increaseTime", seconds); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.Client().Call(nil, "evm_mine"); err != nil {
		t.Fatal(err)
	}
}

// reverts reports whether an admin call of the method reverts (estimated, not
// sent).
func (w *k09World) reverts(contract common.Address, parsed abi.ABI, method string, args ...interface{}) (bool, string) {
	data, err := parsed.Pack(method, args...)
	if err != nil {
		w.t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	_, err = w.admin.EstimateGas(context.Background(), ethereum.CallMsg{From: from, To: &contract, Data: data})
	if err != nil {
		return true, err.Error()
	}
	return false, ""
}

func k09TxInfo(tx *bitcoin.Transaction) frostbridgeabi.BitcoinTxInfo {
	return frostbridgeabi.BitcoinTxInfo(legacyTxInfo(tx))
}

func k09Proof(p tbtcabi.BitcoinTxProof) frostbridgeabi.BitcoinTxProof {
	return frostbridgeabi.BitcoinTxProof(p)
}

// k09LoadStage is batch 6's b6LoadStage (frozen) with one change: the state
// dump is loaded over Anvil's WebSocket. Anvil's HTTP endpoint refuses
// request bodies over 2 MiB, and the lifecycle's later dumps exceed it.
func k09LoadStage(t *testing.T, env legacyEnvironment, root, stage string) *frostNodeWorld {
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
	ws, err := rpc.Dial("ws" + strings.TrimPrefix(lw.anvilURL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	var loaded bool
	err = ws.Call(&loaded, "anvil_loadState", string(state))
	ws.Close()
	if err != nil || !loaded {
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
