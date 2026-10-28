package tbtc_test

// Remediation batch 2: the world for the ECDSA-to-FROST drain producer tests
// (tuxxy/keep-core#34 and its constraint issues).
//
// It extends the frozen batch-1 legacy world (legacy_world_test.go, never
// edited here). The Bridge is the real tbtc-v2 integration build compiled from
// git at LEGACY_UPGRADED_COMMIT, deployed behind a real transparent proxy; the
// legacy source reaches MovingFunds through public calls (revealed deposit,
// sweep proven by SPV from Bitcoin Core regtest blocks, heartbeat failure).
// FROST targets are created through FrostRegistryFixture (tbtc-v2 test
// contract: registry callbacks only, no DKG). The C-10 controller is the real
// FrostExposureController, with test ceilings (D-10 values OPEN).
//
// The honest host is the production client: chainethereum.Connect with the
// default, FROST-unconfigured configuration (production cannot configure FROST:
// config.Frost.ValidateNode refuses it, and D5 keeps the FROST runtime
// test-only), driven through tbtcpg.MovingFundsTask.Run, the only production
// entry that sees a MovingFunds wallet each coordination window.

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Creation modes of the upgraded Bridge (frostCreationMode).
const (
	modeLegacyOpen     uint8 = 0
	modeLegacyDraining uint8 = 1
	modeFrostOnly      uint8 = 2
)

// drainRoute is the state of the FROST drain route when the host runs.
type drainRoute int

const (
	// routeOpen: drain enabled, eligible FROST targets, budget for the whole
	// main, not contained, main above dust. A drain commitment is accepted.
	routeOpen drainRoute = iota
	// routeContained: C-10 containment ("FROST exposure contained").
	routeContained
	// routeDrainOff: setFrostDrainEnabled(false) ("FROST drain disabled").
	routeDrainOff
	// routeZeroEligible: no FROST wallet is a movement target.
	routeZeroEligible
	// routeBudgetExhausted: custody ceiling 0 ("FROST custody budget").
	routeBudgetExhausted
	// routeBudgetPartial: 0 < remaining custody < main ("FROST custody
	// budget"), the K09-39 state.
	routeBudgetPartial
	// routeBelowDust: main below movingFundsDustThreshold ("FROST drain below
	// dust").
	routeBelowDust
)

func (r drainRoute) String() string {
	return [...]string{"open", "contained", "drain-off", "zero-eligible",
		"budget-exhausted", "budget-partial", "below-dust"}[r]
}

func modeName(mode uint8) string {
	return [...]string{"LegacyOpen", "LegacyDraining", "FrostOnly"}[mode]
}

// Moving-funds timing (governance test values): short enough that the chain
// clock stays far behind the wall clock (legacyChainLag) for the whole test,
// so the base wall-clock safety margin never interferes.
const (
	drainResetDelay = uint32(3600)
	drainTimeout    = uint32(7200)
)

// drainMain is the source's main UTXO value (fundSource).
const drainMain = uint64(legacyDepositValue - legacySweepFee)

type drainSetup struct {
	mode     uint8
	live     bool // a legacy Live wallet remains (w.target)
	route    drainRoute
	targets  int // eligible FROST targets for an open route (default 1)
	encoding legacyRevertEncoding
	// maxTransfer, if set, is walletMaxBtcTransfer (C-06 target count).
	maxTransfer uint64
	// faults puts the drainFaults ADAPTER between the recorder and Anvil.
	faults bool
}

func (s drainSetup) String() string {
	live := "live0"
	if s.live {
		live = "live1"
	}
	return fmt.Sprintf("%s/%s/%s", modeName(s.mode), live, s.route)
}

type drainWorld struct {
	*legacyWorld
	setup drainSetup

	frost       *bind.BoundContract
	exposure    *bind.BoundContract
	frostReg    *bind.BoundContract
	frostRegAt  common.Address
	targets     []*frostTarget
	nextKey     int64
	recorder    *legacyRecorder
	faults      *drainFaults
	client      *chainethereum.TbtcChain
}

// newDrainWorld builds the upgraded system, brings the legacy source into
// MovingFunds publicly, then puts the drain route and the creation mode into
// the requested state, and connects the production client.
func newDrainWorld(t *testing.T, setup drainSetup) *drainWorld {
	t.Helper()
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	dw := &drainWorld{setup: setup, nextKey: 1000}
	w := newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), dw.configureFrost)
	dw.legacyWorld = w
	dw.shortenTimeouts()

	if !setup.live {
		// Closing: a heartbeat failure of a wallet with no main UTXO.
		w.heartbeatFailed(w.target)
	}
	if setup.maxTransfer != 0 {
		parameters, err := w.legacy.WalletParameters(&bind.CallOpts{})
		if err != nil {
			t.Fatal(err)
		}
		w.send("wallet max BTC transfer", func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.UpdateWalletParameters(o, 0, 0,
				parameters.WalletCreationMaxBtcBalance, parameters.WalletClosureMinBtcBalance,
				parameters.WalletMaxAge, setup.maxTransfer, parameters.WalletClosingPeriod)
		})
	}
	if setup.route != routeZeroEligible {
		n := setup.targets
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			dw.createFrostTarget()
		}
	}
	if setup.route != routeDrainOff {
		dw.call("FROST drain enabled", dw.frost, "setFrostDrainEnabled", true)
	}
	dw.setMode(setup.mode)
	switch setup.route {
	case routeContained:
		dw.call("C-10 containment", dw.exposure, "contain", crypto.Keccak256Hash([]byte("batch-2 test containment")))
	case routeBelowDust:
		dw.raiseDust(2 * drainMain)
	}

	// Preconditions the cases rely on.
	source, err := w.legacy.Wallets(&bind.CallOpts{}, w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if source.State != 2 || source.MovingFundsTargetWalletsCommitmentHash != [32]byte{} {
		t.Fatalf("setup: source state %d with commitment %x, want MovingFunds and none", source.State, source.MovingFundsTargetWalletsCommitmentHash)
	}
	live, err := w.legacy.LiveWalletsCount(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[bool]uint32{true: 1, false: 0}[setup.live]; live != want {
		t.Fatalf("setup: liveWalletsCount %d, want %d", live, want)
	}
	if got := dw.creationMode(); got != setup.mode {
		t.Fatalf("setup: creation mode %d, want %d", got, setup.mode)
	}

	upstream := w.anvilURL
	if setup.faults {
		dw.faults = newDrainFaults(t, w.anvilURL)
		upstream = dw.faults.URL()
	}
	dw.recorder = newLegacyRecorder(t, upstream, setup.encoding)
	dw.client, err = w.connect(dw.recorder)
	if err != nil {
		t.Fatalf("startup against the upgraded Bridge (%s): %v", setup.encoding, err)
	}
	return dw
}

func (dw *drainWorld) call(what string, contract *bind.BoundContract, method string, args ...interface{}) {
	dw.t.Helper()
	dw.send(what, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return contract.Transact(o, method, args...)
	})
}

// configureFrost runs before any legacy wallet exists: the FROST registry,
// funding, the C-10 controller with the case's custody ceiling, and the gates.
// The drain is enabled later, as the case requires.
func (dw *drainWorld) configureFrost(w *legacyWorld) {
	t := w.t
	t.Helper()
	dw.legacyWorld = w
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	dw.frost = bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
	libraries := map[string]common.Address{}

	var registryABI abi.ABI
	dw.frostRegAt, registryABI = w.deployBaseline(libraries, "contracts/test/FrostRegistryFixture.sol", "FrostRegistryFixture", w.bridge)
	dw.frostReg = bind.NewBoundContract(dw.frostRegAt, registryABI, w.admin, w.admin, w.admin)
	dw.call("FROST registry", dw.frost, "configureFrostRegistry", dw.frostRegAt)

	verifier, _ := w.deployBaseline(libraries, "contracts/frost/FrostVerifier.sol", "FrostVerifier")
	code, err := w.admin.CodeAt(context.Background(), verifier, nil)
	if err != nil {
		t.Fatal(err)
	}
	dw.call("FROST funding", dw.frost, "configureFrostFunding", verifier, crypto.Keccak256Hash(code),
		crypto.Keccak256Hash([]byte("UNQUALIFIED batch-2 local test")),
		crypto.Keccak256Hash([]byte("batch-2 synthetic regtest domain")))

	exposureAddress, exposureABI := w.deployBaseline(libraries, "contracts/frost/FrostExposureController.sol", "FrostExposureController", w.bridge, admin)
	dw.exposure = bind.NewBoundContract(exposureAddress, exposureABI, w.admin, w.admin, w.admin)
	ceiling := ^uint64(0)
	switch dw.setup.route {
	case routeBudgetExhausted:
		ceiling = 0
	case routeBudgetPartial:
		ceiling = drainMain / 2
	}
	dw.call("C-10 budget", dw.exposure, "openBudgetPeriod", ceiling, ^uint64(0),
		crypto.Keccak256Hash([]byte("C-10 test budget; D-10 OPEN")))
	dw.call("C-10 controller", dw.frost, "installFrostExposureController", exposureAddress)
	dw.call("FROST gates", dw.frost, "setFrostFundingGates", true, true)
}

// shortenTimeouts sets the moving-funds reset delay and timeout (governance
// test values), keeping every other parameter.
func (dw *drainWorld) shortenTimeouts() {
	p, err := dw.legacy.MovingFundsParameters(&bind.CallOpts{})
	if err != nil {
		dw.t.Fatal(err)
	}
	dw.send("moving funds timeouts", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return dw.legacy.UpdateMovingFundsParameters(o, p.MovingFundsTxMaxTotalFee, p.MovingFundsDustThreshold,
			drainResetDelay, drainTimeout, p.MovingFundsTimeoutSlashingAmount,
			p.MovingFundsTimeoutNotifierRewardMultiplier, p.MovingFundsCommitmentGasOffset,
			p.MovedFundsSweepTxMaxTotalFee, p.MovedFundsSweepTimeout,
			p.MovedFundsSweepTimeoutSlashingAmount, p.MovedFundsSweepTimeoutNotifierRewardMultiplier)
	})
}

// raiseDust puts the moving-funds dust threshold above the main (the
// redemption dust threshold must stay above it).
func (dw *drainWorld) raiseDust(dust uint64) {
	r, err := dw.legacy.RedemptionParameters(&bind.CallOpts{})
	if err != nil {
		dw.t.Fatal(err)
	}
	dw.send("redemption dust", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return dw.legacy.UpdateRedemptionParameters(o, 2*dust, r.RedemptionTreasuryFeeDivisor,
			r.RedemptionTxMaxFee, r.RedemptionTxMaxTotalFee, r.RedemptionTimeout,
			r.RedemptionTimeoutSlashingAmount, r.RedemptionTimeoutNotifierRewardMultiplier)
	})
	p, err := dw.legacy.MovingFundsParameters(&bind.CallOpts{})
	if err != nil {
		dw.t.Fatal(err)
	}
	dw.send("moving funds dust", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return dw.legacy.UpdateMovingFundsParameters(o, p.MovingFundsTxMaxTotalFee, dust,
			p.MovingFundsTimeoutResetDelay, p.MovingFundsTimeout, p.MovingFundsTimeoutSlashingAmount,
			p.MovingFundsTimeoutNotifierRewardMultiplier, p.MovingFundsCommitmentGasOffset,
			p.MovedFundsSweepTxMaxTotalFee, p.MovedFundsSweepTimeout,
			p.MovedFundsSweepTimeoutSlashingAmount, p.MovedFundsSweepTimeoutNotifierRewardMultiplier)
	})
}

func (dw *drainWorld) setMode(mode uint8) {
	if mode >= modeLegacyDraining && dw.creationMode() == modeLegacyOpen {
		dw.call("legacy DKG drain", dw.frost, "beginLegacyDkgDrain")
	}
	if mode == modeFrostOnly && dw.creationMode() == modeLegacyDraining {
		dw.call("FROST cutover", dw.frost, "completeFrostCutover", crypto.Keccak256Hash([]byte("batch-2 test cutover manifest")))
	}
}

func (dw *drainWorld) creationMode() uint8 {
	var out []interface{}
	if err := dw.frost.Call(&bind.CallOpts{}, &out, "frostCreationMode"); err != nil {
		dw.t.Fatal(err)
	}
	return out[0].(uint8)
}

// createFrostTarget creates one Live FROST wallet through the registry
// FIXTURE's callbacks, as movementWorld builds them.
func (dw *drainWorld) createFrostTarget() *frostTarget {
	t := dw.t
	t.Helper()
	dw.nextKey++
	target := &frostTarget{}
	key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(dw.nextKey).Bytes(), 32))
	if err != nil {
		t.Fatal(err)
	}
	copy(target.q[:], crypto.CompressPubkey(&key.PublicKey)[1:])
	head, err := dw.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	descriptorType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "scheme", Type: "uint8"}, {Name: "profile", Type: "uint8"},
		{Name: "chainId", Type: "uint256"}, {Name: "registry", Type: "address"},
		{Name: "epoch", Type: "uint64"}, {Name: "members", Type: "uint32[]"},
		{Name: "operators", Type: "address[]"}, {Name: "threshold", Type: "uint16"},
		{Name: "snowfallDescriptor", Type: "bytes32"}, {Name: "outputKey", Type: "bytes32"},
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := crypto.PubkeyToAddress(dw.adminKey.PublicKey)
	descriptor := struct {
		Scheme             uint8
		Profile            uint8
		ChainId            *big.Int
		Registry           common.Address
		Epoch              uint64
		Members            []uint32
		Operators          []common.Address
		Threshold          uint16
		SnowfallDescriptor [32]byte
		OutputKey          [32]byte
	}{2, 1, dw.chainID, dw.frostRegAt, head, []uint32{1, 2, 3},
		[]common.Address{dw.operator, admin, common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")},
		2, crypto.Keccak256Hash([]byte(fmt.Sprintf("batch-2 FROST target %d", dw.nextKey))), target.q}
	encoded, err := abi.Arguments{{Type: descriptorType}}.Pack(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	tagged, err := abi.Arguments{{Type: bytes32Type}, {Type: descriptorType}}.Pack(
		crypto.Keccak256Hash([]byte("tbtc-v2/frost-descriptor/v1")), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	target.id = crypto.Keccak256Hash(crypto.Keccak256([]byte("tbtc-v2/frost-wallet-id/v1")), target.q[:])
	copy(target.label[:], target.id[:20])
	dw.call("FROST wallet created", dw.frostReg, "created", encoded, head+100000)
	dw.call("FROST wallet ready", dw.frostReg, "ready", target.id, crypto.Keccak256Hash(tagged))
	dw.targets = append(dw.targets, target)
	return target
}

// openRoute removes the case's blocker, through the same governance and
// registry calls that would do it on a live system.
func (dw *drainWorld) openRoute() {
	switch dw.setup.route {
	case routeContained:
		dw.call("C-10 lift", dw.exposure, "lift", crypto.Keccak256Hash([]byte("batch-2 test lift record")))
	case routeDrainOff:
		dw.call("FROST drain enabled", dw.frost, "setFrostDrainEnabled", true)
	case routeZeroEligible:
		dw.createFrostTarget()
	case routeBudgetExhausted, routeBudgetPartial:
		dw.call("C-10 top-up", dw.exposure, "openBudgetPeriod", ^uint64(0), ^uint64(0),
			crypto.Keccak256Hash([]byte("C-10 test top-up; D-10 OPEN")))
	}
}

func (dw *drainWorld) isFrostLabel(label [20]byte) bool {
	for _, target := range dw.targets {
		if target.label == label {
			return true
		}
	}
	return false
}

// request is the coordination request a coordinator makes for the source.
func (dw *drainWorld) request() *tbtc.CoordinationProposalRequest {
	operator := chain.Address(dw.operator.Hex())
	return &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: dw.source.pkh,
		WalletOperators:     []chain.Address{operator},
		ExecutingOperator:   operator,
		ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
	}
}

// window runs the honest host's moving-funds task once, as one coordination
// window does, with background mining so its transactions confirm.
func (dw *drainWorld) window(what string) (*tbtc.MovingFundsProposal, error) {
	dw.t.Helper()
	stop := dw.mineInBackground()
	defer stop()
	var proposal *tbtc.MovingFundsProposal
	err := legacyBounded(dw.t, what, func() error {
		result, ok, err := tbtcpg.NewMovingFundsTask(dw.client, dw.btc).Run(dw.request())
		if err == nil && ok {
			proposal = result.(*tbtc.MovingFundsProposal)
		}
		return err
	})
	return proposal, err
}

// sourceWallet reads the source's legacy record.
func (dw *drainWorld) sourceWallet() (state uint8, requestedAt uint32, commitment [32]byte) {
	wallet, err := dw.legacy.Wallets(&bind.CallOpts{}, dw.source.pkh)
	if err != nil {
		dw.t.Fatal(err)
	}
	return wallet.State, wallet.MovingFundsRequestedAt, wallet.MovingFundsTargetWalletsCommitmentHash
}

// commitments lists every MovingFundsCommitmentSubmitted target list of the
// source, oldest first.
func (dw *drainWorld) commitments() [][][20]byte {
	iterator, err := dw.legacy.FilterMovingFundsCommitmentSubmitted(&bind.FilterOpts{Start: 0}, [][20]byte{dw.source.pkh})
	if err != nil {
		dw.t.Fatal(err)
	}
	defer iterator.Close()
	var lists [][][20]byte
	for iterator.Next() {
		lists = append(lists, iterator.Event.TargetWallets)
	}
	return lists
}

// commitmentAttempts counts the client's eth_estimateGas and
// eth_sendRawTransaction requests carrying submitMovingFundsCommitment: each
// is an attempt to create an obligation (a dry run through eth_call is not).
func (dw *drainWorld) commitmentAttempts() int {
	selector := fmt.Sprintf("%x", dw.bridgeABI.Methods["submitMovingFundsCommitment"].ID)
	n := 0
	for _, call := range dw.recorder.Calls() {
		if (call.Method == "eth_estimateGas" || call.Method == "eth_sendRawTransaction") &&
			call.To == dw.bridge && call.Selector == selector {
			n++
		}
	}
	return n
}

// chainNow is the latest block's timestamp.
func (dw *drainWorld) chainNow() uint64 {
	header, err := dw.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		dw.t.Fatal(err)
	}
	return header.Time
}

// elapseTo advances the chain clock to at least timestamp.
func (dw *drainWorld) elapseTo(timestamp uint64) {
	if now := dw.chainNow(); timestamp > now {
		dw.elapse(int64(timestamp - now))
	}
}

// notifyTimeoutRefusal dry-runs the permissionless notifyMovingFundsTimeout as
// a third party would, and returns its revert reason ("" if it would succeed).
func (dw *drainWorld) notifyTimeoutRefusal() string {
	_, err := dw.legacy.NotifyMovingFundsTimeout(&bind.TransactOpts{
		From:   crypto.PubkeyToAddress(dw.adminKey.PublicKey),
		Signer: func(_ common.Address, tx *types.Transaction) (*types.Transaction, error) { return tx, nil },
		NoSend: true, Context: context.Background(),
	}, dw.source.pkh, []uint32{1})
	if err == nil {
		return ""
	}
	return err.Error()
}

// labels formats target lists, marking FROST and legacy targets.
func (dw *drainWorld) labels(targets [][20]byte) string {
	var parts []string
	for _, target := range targets {
		kind := "legacy"
		if dw.isFrostLabel(target) {
			kind = "FROST"
		}
		parts = append(parts, fmt.Sprintf("%s:%x", kind, target))
	}
	return "[" + strings.Join(parts, " ") + "]"
}
