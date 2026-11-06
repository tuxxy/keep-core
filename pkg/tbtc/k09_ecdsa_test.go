package tbtc_test

// K-09 legacy ECDSA side (remediation batch 9, tuxxy/keep-core#40): the
// legacy source wallets are tECDSA groups whose signers sit in the base
// nodes, so the ECDSA-to-FROST drain is committed and signed by the nodes
// through base's MovingFunds path (tbtcpg.MovingFundsTask, the batch-2 drain
// producer, movingFundsAction), not by the harness.
//
// FIXTURES (each labelled where it is made):
//   - the groups' key shares are base's tecdsatest 3-of-5 shares (group
//     tweak 0 for the first source, 1 for the second; see
//     k09_ecdsa_export_test.go), seeded into the nodes' base wallet storage;
//     seats {1,1,2,2,3} as the FROST wallet's, so no operator holds the
//     signing threshold alone;
//   - the harness signs only setup transactions with a group (funding a
//     source's main through a legacy deposit sweep), in its own process;
//   - the legacy registry fixture (test/legacy/fixtures) admits one wallet
//     member, bound to operator 1; drain windows are chosen whose base leader
//     (getLeader, by unique operator, D2) for the source is operator 1;
//   - EcdsaDkgValidator: a code-only contract answering base's group
//     parameter reads (groupSize 5, activeThreshold 4, groupThreshold 3), the
//     tecdsatest group's shape. Base reads it as it reads the real validator.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

// k09OperatorAddress is operator i's address (scalar i, as setUpFrost).
func k09OperatorAddress(i int) common.Address {
	key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(int64(i)).Bytes(), 32))
	if err != nil {
		panic(err)
	}
	return crypto.PubkeyToAddress(key.PublicKey)
}

// k09LegacySeats are the legacy groups' seat operators, seat i+1 at index i:
// the K-09 pattern {1,1,2,2,3}.
func k09LegacySeats() []chain.Address {
	seats := make([]chain.Address, len(k09Pattern))
	for i, operator := range k09Pattern {
		seats[i] = chain.Address(k09OperatorAddress(operator).Hex())
	}
	return seats
}

// k09LegacyMember is the one wallet member the legacy registry fixture
// admits (isWalletMember): operator 1.
var k09LegacyMember = k09OperatorAddress(1)

// k09LegacySource is legacy group d's wallet: its public key (no private
// key exists in the harness), registry ID and Bitcoin key hash.
func k09LegacySource(t *testing.T, d int64) legacyWallet {
	t.Helper()
	public, err := tbtc.K09LegacyGroupPublicKey(d)
	if err != nil {
		t.Fatal(err)
	}
	key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: tecdsa.Curve, X: public.X, Y: public.Y}}
	return legacyWallet{
		key: key,
		id:  crypto.Keccak256Hash([]byte("K-09 legacy group"), big.NewInt(d).Bytes()),
		pkh: bitcoin.PublicKeyHash(&key.PublicKey),
	}
}

// k09ValidatorAddress is where the EcdsaDkgValidator FIXTURE's code lives.
var k09ValidatorAddress = common.HexToAddress("0x00000000000000000000000000000000000ec5a9")

// k09ValidatorCode is the FIXTURE's runtime code: groupSize() 5,
// activeThreshold() 4, groupThreshold() 3; anything else reverts.
func k09ValidatorCode() []byte {
	answers := []struct {
		signature string
		value     byte
	}{
		{"groupSize()", tbtc.K09LegacyGroupSize},
		{"activeThreshold()", tbtc.K09LegacyGroupQuorum},
		{"groupThreshold()", tbtc.K09LegacyHonestThreshold},
	}
	// PUSH1 0 CALLDATALOAD PUSH1 0xe0 SHR
	code := []byte{0x60, 0x00, 0x35, 0x60, 0xe0, 0x1c}
	dispatch := 10 * len(answers)
	body := len(code) + dispatch + 4
	for i, a := range answers {
		selector := crypto.Keccak256([]byte(a.signature))[:4]
		// DUP1 PUSH4 selector EQ PUSH1 target JUMPI
		code = append(code, 0x80, 0x63)
		code = append(code, selector...)
		code = append(code, 0x14, 0x60, byte(body+11*i), 0x57)
	}
	// PUSH1 0 DUP1 REVERT
	code = append(code, 0x60, 0x00, 0x80, 0xfd)
	for _, a := range answers {
		// JUMPDEST PUSH1 value PUSH1 0 MSTORE PUSH1 32 PUSH1 0 RETURN
		code = append(code, 0x5b, 0x60, a.value, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3)
	}
	return code
}

// k09ValidatorContracts are the node contract addresses K-09 adds.
func k09ValidatorContracts() map[string]string {
	// Keys as commonethereum.Config.SetContractAddress stores them.
	return map[string]string{strings.ToLower(chainethereum.EcdsaDkgValidatorContractName): k09ValidatorAddress.Hex()}
}

// installValidator puts the FIXTURE's code on chain and checks it answers.
func k09InstallValidator(t *testing.T, lw *legacyWorld) {
	t.Helper()
	if err := lw.admin.Client().Call(nil, "anvil_setCode", k09ValidatorAddress, fmt.Sprintf("0x%x", k09ValidatorCode())); err != nil {
		t.Fatalf("setup: EcdsaDkgValidator fixture: %v", err)
	}
	for signature, want := range map[string]int64{"groupSize()": 5, "activeThreshold()": 4, "groupThreshold()": 3} {
		var out string
		call := map[string]string{"to": k09ValidatorAddress.Hex(), "data": fmt.Sprintf("0x%x", crypto.Keccak256([]byte(signature))[:4])}
		if err := lw.admin.Client().Call(&out, "eth_call", call, "latest"); err != nil {
			t.Fatalf("setup: EcdsaDkgValidator fixture %s: %v", signature, err)
		}
		if got := new(big.Int).SetBytes(common.FromHex(out)); got.Int64() != want {
			t.Fatalf("setup: EcdsaDkgValidator fixture %s = %v, want %d", signature, got, want)
		}
	}
}

// k09NewLegacyWorld is newLegacyWorldOn (legacy_world_test.go) with the K-09
// legacy source: the same 40a11d1d contracts and fixtures, the same Live
// target (test key 12), but the MovingFunds source is legacy group 0, its
// main funded by a deposit sweep the group signs (setup), and the registry
// fixture's member is operator 1.
func k09NewLegacyWorld(t *testing.T, env legacyEnvironment, build *legacyBaseline) *legacyWorld {
	t.Helper()
	w := &legacyWorld{t: t, env: env, baseline: build}
	w.anvilURL = startLegacyAnvil(t, env)
	var err error
	if w.admin, err = ethclient.Dial(w.anvilURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.admin.Close)
	if w.chainID, err = w.admin.ChainID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.adminKey, err = crypto.HexToECDSA(legacyAdminKey); err != nil {
		t.Fatal(err)
	}
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	w.operator = crypto.PubkeyToAddress(operatorKey.PublicKey)
	w.keyFile = legacyKeyFile(t, operatorKey)
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)

	registryABI, registryCode := w.baseline.fixture("LegacyWalletRegistryFixture")
	relayABI, relayCode := w.baseline.fixture("LegacyRegtestRelayFixture")
	w.registryABI, w.relayABI = registryABI, relayABI
	w.registry = w.deploy("registry fixture", registryABI, registryCode)
	w.relay = w.deploy("relay fixture", relayABI, relayCode)
	w.fixture = bind.NewBoundContract(w.registry, registryABI, w.admin, w.admin, w.admin)

	libraries := map[string]common.Address{}
	var bankABI abi.ABI
	w.bank, bankABI = w.deployBaseline(libraries, "contracts/bank/Bank.sol", "Bank")
	implementation, bridgeABI := w.deployBaseline(libraries, "contracts/bridge/Bridge.sol", "Bridge")
	w.bridgeABI = bridgeABI
	proxyAdmin, _ := w.deployBaseline(libraries, "@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol", "ProxyAdmin")
	reimbursementPool, reimbursementABI := w.deployBaseline(libraries,
		"@keep-network/random-beacon/contracts/ReimbursementPool.sol", "ReimbursementPool",
		big.NewInt(0), big.NewInt(0))
	initializer, err := bridgeABI.Pack("initialize", w.bank, w.relay, admin, w.registry, reimbursementPool, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	w.bridge, _ = w.deployBaseline(libraries,
		"@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol", "TransparentUpgradeableProxy",
		implementation, proxyAdmin, initializer)
	w.validator, w.validatorABI = w.deployBaseline(libraries, "contracts/bridge/WalletProposalValidator.sol", "WalletProposalValidator", w.bridge)
	w.reimbursementPool = reimbursementPool
	w.maintainerProxy, _ = w.deployBaseline(libraries, "contracts/maintainer/MaintainerProxy.sol", "MaintainerProxy", w.bridge, reimbursementPool)

	pool := bind.NewBoundContract(reimbursementPool, reimbursementABI, w.admin, w.admin, w.admin)
	w.send("reimbursement authorization", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return pool.Transact(o, "authorize", w.bridge)
	})
	bank := bind.NewBoundContract(w.bank, bankABI, w.admin, w.admin, w.admin)
	w.send("bank bridge", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return bank.Transact(o, "updateBridge", w.bridge)
	})
	// FIXTURE: the registry fixture's one member is operator 1.
	w.send("fixture bind", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "bind", w.bridge, k09LegacyMember)
	})
	if w.legacy, err = tbtcabi.NewBridge(w.bridge, w.admin); err != nil {
		t.Fatal(err)
	}
	w.send("spv maintainer", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SetSpvMaintainerStatus(o, admin, true)
	})
	parameters, err := w.legacy.WalletParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("wallet parameters", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateWalletParameters(o, 0, 0,
			parameters.WalletCreationMaxBtcBalance, parameters.WalletClosureMinBtcBalance,
			parameters.WalletMaxAge, parameters.WalletMaxBtcTransfer, parameters.WalletClosingPeriod)
	})
	for _, address := range []common.Address{w.operator, k09LegacyMember} {
		if err := w.admin.Client().Call(nil, "anvil_setBalance", address, "0x3635c9adc5dea00000"); err != nil {
			t.Fatal(err)
		}
	}
	k09InstallValidator(t, w)

	w.btc = newLegacyRegtest(t, env)
	w.source = k09LegacySource(t, 0)
	w.target = newLegacyWallet(t, 12)
	w.registerWallet(w.source)
	w.registerWallet(w.target)
	w.main = k09FundLegacy(w, w.source, 0, [8]byte{0x1e, 0x9a})
	k09MovingFunds(w, w.source)
	return w
}

// k09FundLegacy reveals a legacy deposit to group d's wallet and proves its
// sweep into the wallet's main; the group signs the sweep in the harness's
// process (setup FIXTURE, as fundSource signs with a test key). It returns
// the main.
func k09FundLegacy(w *legacyWorld, wallet legacyWallet, d int64, blinding [8]byte) *bitcoin.UnspentTransactionOutput {
	t := w.t
	t.Helper()
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	deposit := &tbtc.Deposit{
		Depositor:           chain.Address(admin.Hex()),
		BlindingFactor:      blinding,
		WalletPublicKeyHash: wallet.pkh,
		RefundPublicKeyHash: [20]byte{9},
	}
	binary.LittleEndian.PutUint32(deposit.RefundLocktime[:], uint32(header.Time+30*24*3600))
	script, err := deposit.Script()
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
	w.send("reveal legacy group deposit", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.RevealDeposit(o, legacyTxInfo(funding), tbtcabi.DepositDepositRevealInfo{
			FundingOutputIndex: uint32(index), BlindingFactor: deposit.BlindingFactor, WalletPubKeyHash: deposit.WalletPublicKeyHash,
			RefundPubKeyHash: deposit.RefundPublicKeyHash, RefundLocktime: deposit.RefundLocktime,
		})
	})
	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddScriptHashInput(&bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: uint32(index)},
		Value:    legacyDepositValue,
	}, script); err != nil {
		t.Fatal(err)
	}
	walletScript, err := bitcoin.PayToWitnessPublicKeyHash(wallet.pkh)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: legacyDepositValue - legacySweepFee, PublicKeyScript: walletScript})
	hashes, err := builder.ComputeSignatureHashes()
	if err != nil {
		t.Fatal(err)
	}
	signatures := tbtc.K09SignWithLegacyGroup(t, d, hashes)
	containers := make([]*bitcoin.SignatureContainer, len(signatures))
	for i, s := range signatures {
		containers[i] = &bitcoin.SignatureContainer{R: s.R, S: s.S, PublicKey: &wallet.key.PublicKey}
	}
	sweep, err := builder.AddSignatures(containers)
	if err != nil {
		t.Fatal(err)
	}
	w.btc.mine(t, sweep)
	w.send("legacy group sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitDepositSweepProof(o, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()), tbtcabi.BitcoinTxUTXO{}, common.Address{})
	})
	return &bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: sweep.Hash(), OutputIndex: 0},
		Value:    legacyDepositValue - legacySweepFee,
	}
}

// k09MovingFunds moves a legacy wallet to MovingFunds through the registry
// fixture's heartbeat-failure callback, as newLegacyWorldOn does.
func k09MovingFunds(w *legacyWorld, wallet legacyWallet) {
	t := w.t
	t.Helper()
	w.send("heartbeat failure", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "heartbeatFailed", wallet.id,
			common.BytesToHash(wallet.key.PublicKey.X.FillBytes(make([]byte, 32))),
			common.BytesToHash(wallet.key.PublicKey.Y.FillBytes(make([]byte, 32))))
	})
	state, err := w.legacy.Wallets(&bind.CallOpts{}, wallet.pkh)
	if err != nil || state.State != 2 {
		t.Fatalf("setup: legacy wallet %x not in MovingFunds: %v %+v", wallet.pkh, err, state)
	}
}

// k09SecondLegacySource is stage 06's second legacy source: legacy group 1,
// registered, funded and moved to MovingFunds as the first.
func (w *k09World) secondLegacySource() (legacyWallet, *bitcoin.UnspentTransactionOutput) {
	w.t.Helper()
	source := k09LegacySource(w.t, 1)
	w.registerWallet(source)
	main := k09FundLegacy(w.legacyWorld, source, 1, [8]byte{0x2e, 0x9b})
	k09MovingFunds(w.legacyWorld, source)
	return source, main
}

// seedLegacySigners writes legacy group d's signers into every operator's
// base wallet storage (FIXTURE: base's DKG writes them after key
// generation); each node then restores them at startup (loadSigners).
func (w *k09World) seedLegacySigners(d int64) {
	t := w.t
	t.Helper()
	seats := k09LegacySeats()
	total := 0
	for i := 1; i <= b6Operators; i++ {
		dir := filepath.Join(w.dir, fmt.Sprintf("operator-%d", i), "storage")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		n, err := tbtc.K09SeedLegacySigners(dir, b6KeyPassword, d, seats, chain.Address(k09OperatorAddress(i).Hex()))
		if err != nil {
			t.Fatalf("setup: seeding legacy group %d into operator %d: %v", d, i, err)
		}
		total += n
	}
	if total != len(seats) {
		t.Fatalf("setup: seeded %d legacy signers for group %d, want %d", total, d, len(seats))
	}
	w.phase(fmt.Sprintf("legacy group %d signers seeded", d))
}

// legacyLeader accepts a coordination window whose base leader for the
// legacy wallet (D2: by unique operator, from the window's seed block) is
// the registry fixture's member.
func (w *k09World) legacyLeader(wallet legacyWallet) func(window uint64) bool {
	return func(window uint64) bool {
		header, err := w.admin.HeaderByNumber(context.Background(), new(big.Int).SetUint64(window-tbtc.B6CoordinationSafeBlockShift))
		if err != nil {
			w.t.Fatal(err)
		}
		return b6Leader(wallet.pkh, header.Hash(), k09LegacySeats()) == chain.Address(k09LegacyMember.Hex())
	}
}

// k09ECDSABlockInterval is the block interval after the active phase of a
// window in which the legacy group signs. A base tECDSA signing attempt has
// 30 blocks (signing_loop.go; 6 minutes in production); with 400 ms blocks
// an attempt lasts 12 s for three tss-lib signers on one-core nodes.
const k09ECDSABlockInterval = 400 * time.Millisecond

// k09DrainBound bounds the nodes' drain after its window: commitment, its
// 32-block confirmation, signing and broadcast.
const k09DrainBound = 120 * time.Second

// enterECDSASigning is enterSigning with k09ECDSABlockInterval.
func (w *k09World) enterECDSASigning(window uint64) {
	t := w.t
	t.Helper()
	w.enterWindow(window)
	if !w.waitBlock(window+tbtc.B6CoordinationActivePhaseBlocks, 10*time.Second) {
		t.Fatal("setup: the chain did not advance")
	}
	w.stopMining()
	w.stopMining = w.mineEvery(k09ECDSABlockInterval)
}

// nodesDrain waits for the ECDSA-to-FROST drain of the legacy source's main
// that the nodes make in the current window: a node operator commits the
// source's targets on the Bridge (base MovingFundsTask) and the nodes'
// ECDSA signers sign and broadcast the movement (movingFundsAction). The
// harness, as SPV maintainer, then mines and proves it. It checks on chain
// that the commitment names the FROST wallet as the one target and was sent
// by a node operator, that the drain spends the source's main and pays the
// wallet's key, and that the Bridge accepted the proof (a pending FROST
// incoming output). It then logs the K09DRAIN record
// (test/k09/evidence/README.md) and returns the drain.
func (w *k09World) nodesDrain(stage string, source legacyWallet, main *bitcoin.UnspentTransactionOutput, wallet b6Wallet, window, began uint64) *bitcoin.Transaction {
	t := w.t
	t.Helper()
	deadline := time.Now().Add(k09DrainBound)
	var spenders []string
	for time.Now().Before(deadline) {
		if spenders = w.spenders(*main.Outpoint); len(spenders) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	commitments := w.legacyCommitments(source, began)
	if len(spenders) == 0 {
		t.Fatalf("FINDING K-09 %s (SC-01, #40): within %s of window %d the nodes did not sign and broadcast the ECDSA-to-FROST drain of legacy source %x (commitments on the Bridge: %d; exited: %v)%s",
			stage, k09DrainBound, window, source.pkh, len(commitments), w.exitedNodes(), w.debugLogs())
	}
	if len(spenders) != 1 {
		t.Fatalf("K-09 %s (#40): %d distinct node transactions spend the legacy source's main: %v", stage, len(spenders), spenders)
	}
	if len(commitments) != 1 {
		t.Fatalf("K-09 %s (#40): the Bridge holds %d moving-funds commitments for the legacy source, want 1", stage, len(commitments))
	}
	commitment := commitments[0]
	if len(commitment.TargetWallets) != 1 || commitment.TargetWallets[0] != wallet.label {
		t.Fatalf("K-09 %s (#40, D16): the nodes committed the legacy source to %x, want the FROST wallet %x alone", stage, commitment.TargetWallets, wallet.label)
	}
	committer := commitment.Submitter
	if sender := w.sender(commitment.Raw.TxHash); sender != committer {
		t.Fatalf("K-09 %s (#40): the commitment's submitter %s is not its transaction's sender %s", stage, committer.Hex(), sender.Hex())
	}
	operator := false
	for i := 1; i <= b6Operators; i++ {
		operator = operator || k09OperatorAddress(i) == committer
	}
	if !operator {
		t.Fatalf("K-09 %s (#40): the drain commitment was sent by %s, not by a node operator", stage, committer.Hex())
	}
	drain, err := w.nodeTransaction(spenders[0])
	if err != nil {
		t.Fatalf("K-09 %s (#40): Core does not hold the nodes' drain %s: %v", stage, spenders[0], err)
	}
	script, err := bitcoin.PayToTaproot(wallet.q)
	if err != nil {
		t.Fatal(err)
	}
	if len(drain.Inputs) != 1 || len(drain.Outputs) != 1 || !bytes.Equal(drain.Outputs[0].PublicKeyScript, script) {
		t.Fatalf("K-09 %s (#40): the nodes' drain %s is not one input to one output paying the FROST wallet's key", stage, spenders[0])
	}
	w.mineCore(drain)
	w.send("drain proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsProof(o, legacyTxInfo(drain), w.btc.proof(t, drain.Hash()), legacyUTXO(main), source.pkh)
	})
	if state := w.incomingState(drain); state != 1 {
		t.Fatalf("K-09 %s (#40): after the proven drain %s the Bridge holds no pending FROST incoming output (state %d)", stage, spenders[0], state)
	}
	w.phase("node-signed drain mined and its proof accepted")
	t.Logf("K09DRAIN source=0x%x committer=%s drain=%s", source.pkh, committer.Hex(), drain.Hash().Hex(bitcoin.ReversedByteOrder))
	return drain
}

// legacyCommitments are the Bridge's moving-funds commitments for the legacy
// wallet since block began.
func (w *k09World) legacyCommitments(wallet legacyWallet, began uint64) []*tbtcabi.BridgeMovingFundsCommitmentSubmitted {
	t := w.t
	t.Helper()
	it, err := w.legacy.FilterMovingFundsCommitmentSubmitted(&bind.FilterOpts{Start: began}, [][20]byte{wallet.pkh})
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	var all []*tbtcabi.BridgeMovingFundsCommitmentSubmitted
	for it.Next() {
		all = append(all, it.Event)
	}
	return all
}

// sender is an Ethereum transaction's sender.
func (w *k09World) sender(hash common.Hash) common.Address {
	t := w.t
	t.Helper()
	tx, _, err := w.admin.TransactionByHash(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	from, err := types.Sender(types.LatestSignerForChainID(w.chainID), tx)
	if err != nil {
		t.Fatal(err)
	}
	return from
}
