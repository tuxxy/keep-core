package spv_test

// Refs tuxxy/keep-core#28 (K09-8; owner decision D18, fast tier).
//
// Invariant (KC-SF-V3 K-05 "Run discovery and public maintainer paths"; the
// legacy maintainer's getProofInfo; tbtc-v2 BitcoinTx.evaluateProofDifficulty):
// a FROST proof producer derives its proof depth from the Bridge's
// txProofDifficultyFactor and the relay's epoch difficulties, exactly as the
// Bridge evaluates it. Given enough confirmations it submits a proof of the
// minimal depth the Bridge accepts (d*); with fewer than d* it submits
// nothing. An operator-configured count (Confirmations: 1 here) is at most a
// floor, never the depth.
//
// The oracle is the real tbtc-v2 proof validation, not this test: d* is the
// smallest header count the deployed validation accepts, and d* - 1 must be
// refused at "Insufficient accumulated difficulty in header chain" (shorter
// proofs that end before the first non-DIFF1 header are refused at "Not at
// current or previous difficulty").
//
// Real:
//   - Bitcoin testnet4 headers, transactions and difficulty, recorded from a
//     public explorer (testdata/testnet4-spv-depth.json, a labelled FIXTURE
//     of real data). Between real-difficulty blocks testnet4 has BIP94
//     minimum-difficulty (DIFF1) blocks, so d* is neither 1 nor the factor.
//   - The tbtc-v2 Bridge (implementation, ProxyAdmin, proxy) from the
//     integration build made by test/legacy/build-baseline.sh, deployed on
//     Anvil twice: with txProofDifficultyFactor 6 (the production factor)
//     and with factor 2 (a test value). The same recorded headers give a
//     different d* at each factor, so a producer that hard-codes the
//     production factor instead of reading the Bridge fails at factor 2.
//   - BitcoinTx.validateProof, compiled from the same build into
//     SpvDepthFixture (FIXTURE wrapper: no Bridge entrypoint validates a proof
//     alone), configured from the deployed Bridge's own factor and relay.
//   - The FROST producers: FrostMovementMaintainer (movement and incoming
//     sweep), FrostSweepMaintainer and FrostRedemptionMaintainer, with the
//     real ethereum.FrostRedemptionChain binding to the deployed Bridge.
// FIXTURE / ADAPTER (labelled):
//   - SystemTestRelay, with both epoch difficulties set from the recorded
//     epoch-start headers (157248 current, 155232 previous).
//   - recordedBitcoin: a bitcoin.Chain over the recorded data, with a tip
//     height that sets how many confirmations exist.
//   - depthRecorder: embeds the real binding (every read the producer makes,
//     including any it adds, reaches the deployed Bridge) but captures the
//     proof submissions and stands in for the wallet state the transaction
//     needs (its inputs are unswept deposits; one admitted reservation).
//
// The producers are constructed only from their existing fields (Chain,
// Bitcoin, Confirmations), so a fix must derive the depth from what they
// already hold: the Bridge binding and the Bitcoin chain.
//
// Runs through test/spvdepth/run.sh, which fails on any skip.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

// depthFactors are the Bridge factors each producer runs against: the
// production factor and a test value.
var depthFactors = []int64{6, 2}

const (
	// Anvil's first default account; local chain only.
	depthAdminKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
)

// --- FIXTURE: recorded testnet4 data ----------------------------------------

type depthFixture struct {
	Label       string `json:"label"`
	Source      string `json:"source"`
	EpochStarts struct {
		Current  int `json:"current"`
		Previous int `json:"previous"`
	} `json:"epochStarts"`
	FirstHeight int `json:"firstHeight"`
	LastHeight  int `json:"lastHeight"`
	Headers     map[string]struct {
		Hash   string `json:"hash"`
		Header string `json:"header"`
	} `json:"headers"`
	Blocks map[string]struct {
		Txids    []string `json:"txids"`
		Coinbase string   `json:"coinbase"`
		Txid     string   `json:"txid"`
		Tx       string   `json:"tx"`
	} `json:"blocks"`
}

func loadDepthFixture(t *testing.T) *depthFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "testnet4-spv-depth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f depthFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Label, "FIXTURE") {
		t.Fatal("testnet4 data must be labelled as a fixture")
	}
	// Every header hashes to its recorded hash and links to its parent.
	for h := f.FirstHeight; h <= f.LastHeight; h++ {
		header := f.header(t, h)
		if f.hashOf(header) != f.Headers[strconv.Itoa(h)].Hash {
			t.Fatalf("fixture header %d does not hash to its recorded hash", h)
		}
		if h > f.FirstHeight && hex.EncodeToString(reverse(header[4:36])) != f.Headers[strconv.Itoa(h-1)].Hash {
			t.Fatalf("fixture header %d does not link to %d", h, h-1)
		}
	}
	return &f
}

func (f *depthFixture) header(t *testing.T, height int) []byte {
	t.Helper()
	entry, ok := f.Headers[strconv.Itoa(height)]
	if !ok {
		t.Fatalf("no recorded header at %d", height)
	}
	raw, err := hex.DecodeString(entry.Header)
	if err != nil || len(raw) != 80 {
		t.Fatalf("recorded header %d: %v", height, err)
	}
	return raw
}

func (f *depthFixture) hashOf(header []byte) string {
	return hex.EncodeToString(reverse(doubleSHA(header)))
}

func doubleSHA(b []byte) []byte {
	first := sha256.Sum256(b)
	second := sha256.Sum256(first[:])
	return second[:]
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}

// --- ADAPTER: recordedBitcoin -----------------------------------------------

// recordedBitcoin serves the recorded testnet4 data as a bitcoin.Chain whose
// latest block is tip. Only the reads proof assembly uses are served.
type recordedBitcoin struct {
	t   *testing.T
	f   *depthFixture
	tip int
	txs map[bitcoin.Hash]*bitcoin.Transaction
	at  map[bitcoin.Hash]int
}

func newRecordedBitcoin(t *testing.T, f *depthFixture, tip int) *recordedBitcoin {
	r := &recordedBitcoin{t: t, f: f, tip: tip, txs: map[bitcoin.Hash]*bitcoin.Transaction{}, at: map[bitcoin.Hash]int{}}
	for key, block := range f.Blocks {
		height, _ := strconv.Atoi(key)
		for _, rawHex := range []string{block.Coinbase, block.Tx} {
			raw, err := hex.DecodeString(rawHex)
			if err != nil {
				t.Fatal(err)
			}
			tx := new(bitcoin.Transaction)
			if err := tx.Deserialize(raw); err != nil {
				t.Fatal(err)
			}
			r.txs[tx.Hash()] = tx
			r.at[tx.Hash()] = height
		}
		if hash, _ := bitcoin.NewHashFromString(block.Txid, bitcoin.ReversedByteOrder); r.txs[hash] == nil {
			t.Fatalf("recorded transaction %s does not hash to its txid", block.Txid)
		}
	}
	return r
}

func (r *recordedBitcoin) GetTransaction(hash bitcoin.Hash) (*bitcoin.Transaction, error) {
	if tx, ok := r.txs[hash]; ok {
		return tx, nil
	}
	return nil, fmt.Errorf("recorded chain: no transaction %s", hash.Hex(bitcoin.ReversedByteOrder))
}

func (r *recordedBitcoin) GetTransactionConfirmations(_ context.Context, hash bitcoin.Hash) (uint, error) {
	height, ok := r.at[hash]
	if !ok || height > r.tip {
		return 0, fmt.Errorf("recorded chain: transaction %s unconfirmed", hash.Hex(bitcoin.ReversedByteOrder))
	}
	return uint(r.tip - height + 1), nil
}

func (r *recordedBitcoin) BroadcastTransaction(*bitcoin.Transaction) error {
	return errors.New("recorded chain: read only")
}

func (r *recordedBitcoin) GetLatestBlockHeight() (uint, error) { return uint(r.tip), nil }

func (r *recordedBitcoin) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	if int(height) > r.tip {
		return nil, fmt.Errorf("recorded chain: height %d above tip %d", height, r.tip)
	}
	var raw [bitcoin.BlockHeaderByteLength]byte
	copy(raw[:], r.f.header(r.t, int(height)))
	header := new(bitcoin.BlockHeader)
	header.Deserialize(raw)
	return header, nil
}

func (r *recordedBitcoin) GetTransactionMerkleProof(hash bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
	block, ok := r.f.Blocks[strconv.Itoa(int(height))]
	if !ok {
		return nil, fmt.Errorf("recorded chain: no transactions recorded at %d", height)
	}
	var level [][]byte
	position := -1
	for i, txid := range block.Txids {
		raw, _ := hex.DecodeString(txid)
		leaf := reverse(raw)
		if bytes.Equal(leaf, hash[:]) {
			position = i
		}
		level = append(level, leaf)
	}
	if position < 0 {
		return nil, fmt.Errorf("recorded chain: %s not in block %d", hash.Hex(bitcoin.ReversedByteOrder), height)
	}
	proof := &bitcoin.TransactionMerkleProof{BlockHeight: height, Position: uint(position)}
	index := position
	for len(level) > 1 {
		if len(level)%2 == 1 {
			level = append(level, level[len(level)-1])
		}
		proof.MerkleNodes = append(proof.MerkleNodes, hex.EncodeToString(reverse(level[index^1])))
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			next = append(next, doubleSHA(append(append([]byte{}, level[i]...), level[i+1]...)))
		}
		level, index = next, index/2
	}
	header := r.f.header(r.t, int(height))
	if !bytes.Equal(level[0], header[36:68]) {
		return nil, fmt.Errorf("recorded chain: block %d txids do not match its merkle root", height)
	}
	return proof, nil
}

func (r *recordedBitcoin) GetTransactionsForPublicKeyHash([20]byte, int) ([]*bitcoin.Transaction, error) {
	return nil, errors.New("recorded chain: no address index")
}

func (r *recordedBitcoin) GetTxHashesForPublicKeyHash([20]byte) ([]bitcoin.Hash, error) {
	return nil, errors.New("recorded chain: no address index")
}

func (r *recordedBitcoin) GetMempoolForPublicKeyHash([20]byte) ([]*bitcoin.Transaction, error) {
	return nil, errors.New("recorded chain: no mempool")
}

func (r *recordedBitcoin) GetUtxosForPublicKeyHash([20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	return nil, errors.New("recorded chain: no address index")
}

func (r *recordedBitcoin) GetMempoolUtxosForPublicKeyHash([20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	return nil, errors.New("recorded chain: no mempool")
}

func (r *recordedBitcoin) EstimateSatPerVByteFee(uint32) (int64, error) {
	return 0, errors.New("recorded chain: no fee estimate")
}

func (r *recordedBitcoin) GetCoinbaseTxHash(height uint) (bitcoin.Hash, error) {
	block, ok := r.f.Blocks[strconv.Itoa(int(height))]
	if !ok {
		return bitcoin.Hash{}, fmt.Errorf("recorded chain: no coinbase recorded at %d", height)
	}
	return bitcoin.NewHashFromString(block.Txids[0], bitcoin.ReversedByteOrder)
}

// --- ADAPTER: depthRecorder -------------------------------------------------

type capturedProof struct {
	kind  string
	info  redemption.BitcoinInfo
	proof redemption.BitcoinProof
}

// depthRecorder embeds the real Bridge binding. It captures submissions and
// stands in for the wallet state of the recorded transaction.
type depthRecorder struct {
	*eth.FrostRedemptionChain
	tx       [32]byte
	captured []capturedProof
}

func (r *depthRecorder) capture(kind string, info redemption.BitcoinInfo, proof redemption.BitcoinProof) error {
	r.captured = append(r.captured, capturedProof{kind, info, proof})
	return nil
}

func (r *depthRecorder) SubmitMovementProof(_ context.Context, _ [32]byte, p movement.Proof) error {
	return r.capture("movement", p.Transaction, p.Proof)
}

func (r *depthRecorder) SubmitIncomingSweepProof(_ context.Context, _ [32]byte, p movement.Proof) error {
	return r.capture("incoming-sweep", p.Transaction, p.Proof)
}

func (r *depthRecorder) SubmitSweepProof(_ context.Context, p deposit.SweepProof) error {
	return r.capture("deposit-sweep", p.Transaction, p.Proof)
}

func (r *depthRecorder) SubmitPaymentProof(_ context.Context, _ [32]byte, tx redemption.BitcoinInfo, p redemption.BitcoinProof) error {
	return r.capture("redemption-payment", tx, p)
}

func (r *depthRecorder) SubmitCancellationProof(_ context.Context, _ [32]byte, tx redemption.BitcoinInfo, p redemption.BitcoinProof) error {
	return r.capture("redemption-cancellation", tx, p)
}

// Deposit: every input of the recorded sweep is a revealed, unswept deposit.
func (r *depthRecorder) Deposit(context.Context, uint64, [32]byte) (deposit.Record, error) {
	return deposit.Record{RevealedAt: 1}, nil
}

// Admissions and Intent: one admitted reservation whose payment is the
// recorded transaction.
func (r *depthRecorder) Admissions(context.Context, [32]byte, uint64, uint64) ([]redemption.Admission, error) {
	return []redemption.Admission{{ReservationID: depthReservation, TransactionHash: r.tx}}, nil
}

func (r *depthRecorder) Intent(context.Context, uint64, [32]byte) (redemption.Intent, error) {
	return redemption.Intent{ID: depthReservation, TransactionHash: r.tx, State: redemption.IntentReserved}, nil
}

func (r *depthRecorder) Alternatives(context.Context, uint64, [32]byte) ([]redemption.Alternative, error) {
	return nil, nil
}

var (
	depthWallet      = [32]byte{0x28, 0x08}
	depthReservation = [32]byte{0x28, 0x09}
)

// --- Real contracts on Anvil --------------------------------------------------

type depthWorld struct {
	t         *testing.T
	factor    int64
	client    *ethclient.Client
	url       string
	key       []byte
	bridge    common.Address
	validator common.Address
	validABI  abi.ABI
}

type depthArtifact struct {
	ABI            json.RawMessage `json:"abi"`
	Bytecode       string          `json:"bytecode"`
	LinkReferences map[string]map[string][]struct {
		Start  int `json:"start"`
		Length int `json:"length"`
	} `json:"linkReferences"`
}

func depthEnv(t *testing.T) (build, commit, anvil, solc string) {
	t.Helper()
	build, commit = os.Getenv("FROST_DEPTH_TBTC_BUILD"), os.Getenv("FROST_DEPTH_TBTC_COMMIT")
	anvil, solc = os.Getenv("FROST_DEPTH_ANVIL"), os.Getenv("FROST_DEPTH_SOLC")
	if build == "" || commit == "" || anvil == "" || solc == "" {
		t.Skip("FROST proof depth: run through test/spvdepth/run.sh " +
			"(FROST_DEPTH_TBTC_BUILD, FROST_DEPTH_TBTC_COMMIT, FROST_DEPTH_ANVIL, FROST_DEPTH_SOLC)")
	}
	return build, commit, anvil, solc
}

// depthDeployer deploys a contract from the tbtc-v2 build, linking libraries.
type depthDeployer func(source, name string, args ...interface{}) (common.Address, abi.ABI)

// newDepthWorld deploys the Bridge with the given factor and the relay that
// relay deploys and configures.
func newDepthWorld(t *testing.T, factor int64, relay func(*depthWorld, depthDeployer) common.Address) *depthWorld {
	t.Helper()
	build, commit, anvil, solc := depthEnv(t)
	raw, err := os.ReadFile(filepath.Join(build, "manifest.json"))
	if err != nil {
		t.Fatalf("tbtc-v2 build manifest: %v", err)
	}
	var manifest struct {
		TbtcCommit string `json:"tbtcCommit"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.TbtcCommit != commit {
		t.Fatalf("tbtc-v2 build %s is not built from %s", build, commit)
	}

	w := &depthWorld{t: t, factor: factor, url: startDepthAnvil(t, anvil)}
	if w.client, err = ethclient.Dial(w.url); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.client.Close)
	key, _ := crypto.HexToECDSA(depthAdminKey)
	admin := crypto.PubkeyToAddress(key.PublicKey)

	art := func(source, name string) *depthArtifact {
		raw, err := os.ReadFile(filepath.Join(build, "solidity", "build", source, name+".json"))
		if err != nil {
			t.Fatalf("artifact %s:%s: %v", source, name, err)
		}
		var a depthArtifact
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		return &a
	}
	libraries := map[string]common.Address{}
	var deploy func(source, name string, args ...interface{}) (common.Address, abi.ABI)
	deploy = func(source, name string, args ...interface{}) (common.Address, abi.ABI) {
		a := art(source, name)
		code := strings.TrimPrefix(a.Bytecode, "0x")
		for file, names := range a.LinkReferences {
			for library, positions := range names {
				id := file + ":" + library
				address, ok := libraries[id]
				if !ok {
					address, _ = deploy(file, library)
					libraries[id] = address
				}
				for _, p := range positions {
					code = code[:2*p.Start] + hex.EncodeToString(address[:]) + code[2*(p.Start+p.Length):]
				}
			}
		}
		parsed, err := abi.JSON(bytes.NewReader(a.ABI))
		if err != nil {
			t.Fatal(err)
		}
		return w.deploy(source+":"+name, key, parsed, common.FromHex(code), args...), parsed
	}

	relayAddress := relay(w, deploy)
	implementation, bridgeABI := deploy("contracts/bridge/Bridge.sol", "Bridge")
	proxyAdmin, _ := deploy("@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol", "ProxyAdmin")
	initializer, err := bridgeABI.Pack("initialize", admin, relayAddress, admin, admin, admin, big.NewInt(factor))
	if err != nil {
		t.Fatal(err)
	}
	w.bridge, _ = deploy("@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol",
		"TransparentUpgradeableProxy", implementation, proxyAdmin, initializer)

	// FIXTURE wrapper around the real BitcoinTx.validateProof, compiled from
	// the same build's sources with the build's own solc version.
	fixture := filepath.Join("..", "..", "..", "test", "spvdepth", "fixtures", "SpvDepthFixture.sol")
	fixture, _ = filepath.Abs(fixture)
	cmd := exec.Command(solc, "--optimize", "--optimize-runs", "1000",
		"--base-path", ".", "--include-path", "node_modules",
		"--allow-paths", filepath.Dir(fixture), "--combined-json", "abi,bin", fixture)
	cmd.Dir = filepath.Join(build, "solidity")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("compile SpvDepthFixture: %v", err)
	}
	var combined struct {
		Contracts map[string]struct {
			ABI json.RawMessage `json:"abi"`
			Bin string          `json:"bin"`
		} `json:"contracts"`
	}
	if err := json.Unmarshal(out, &combined); err != nil {
		t.Fatal(err)
	}
	c, ok := combined.Contracts[fixture+":SpvDepthFixture"]
	if !ok {
		t.Fatal("SpvDepthFixture not in solc output")
	}
	if w.validABI, err = abi.JSON(bytes.NewReader(c.ABI)); err != nil {
		t.Fatal(err)
	}
	w.validator = w.deploy("SpvDepthFixture", key, w.validABI, common.FromHex(c.Bin), w.bridge)
	w.key = crypto.FromECDSA(key)
	return w
}

// systemTestRelay is the FIXTURE relay with both epoch difficulties set from
// the recorded epoch-start headers.
func systemTestRelay(f *depthFixture) func(*depthWorld, depthDeployer) common.Address {
	return func(w *depthWorld, deploy depthDeployer) common.Address {
		relay, relayABI := deploy("contracts/test/SystemTestRelay.sol", "SystemTestRelay")
		w.send(nil, relay, relayABI, "setCurrentEpochDifficultyFromHeaders", f.header(w.t, f.EpochStarts.Current))
		w.send(nil, relay, relayABI, "setPrevEpochDifficultyFromHeaders", f.header(w.t, f.EpochStarts.Previous))
		return relay
	}
}

func startDepthAnvil(t *testing.T, anvil string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, anvil, "--port", fmt.Sprint(port), "--hardfork", "cancun", "--silent")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("anvil: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		client, err := ethclient.Dial(url)
		if err == nil {
			_, err = client.ChainID(context.Background())
			client.Close()
			if err == nil {
				return url
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("anvil did not start: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (w *depthWorld) opts(key interface{}) *bind.TransactOpts {
	k, _ := crypto.HexToECDSA(depthAdminKey)
	id, err := w.client.ChainID(context.Background())
	if err != nil {
		w.t.Fatal(err)
	}
	o, err := bind.NewKeyedTransactorWithChainID(k, id)
	if err != nil {
		w.t.Fatal(err)
	}
	return o
}

func (w *depthWorld) deploy(what string, key interface{}, parsed abi.ABI, code []byte, args ...interface{}) common.Address {
	w.t.Helper()
	address, tx, _, err := bind.DeployContract(w.opts(key), parsed, code, w.client, args...)
	if err != nil {
		w.t.Fatalf("deploy %s: %v", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if receipt, err := depthWaitMined(ctx, w.client, tx); err != nil || receipt.Status != 1 {
		w.t.Fatalf("deploy %s: not mined successfully: %v", what, err)
	}
	return address
}

func (w *depthWorld) send(key interface{}, to common.Address, parsed abi.ABI, method string, args ...interface{}) {
	w.t.Helper()
	tx, err := bind.NewBoundContract(to, parsed, w.client, w.client, w.client).Transact(w.opts(key), method, args...)
	if err != nil {
		w.t.Fatalf("%s: %v", method, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if receipt, err := depthWaitMined(ctx, w.client, tx); err != nil || receipt.Status != 1 {
		w.t.Fatalf("%s: not mined successfully: %v", method, err)
	}
}

// depthWaitMined polls for the receipt every 10 ms; Anvil mines on submission,
// and bind.WaitMined polls only once a second.
func depthWaitMined(ctx context.Context, b bind.DeployBackend, tx *types.Transaction) (*types.Receipt, error) {
	for {
		receipt, err := b.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type depthInfo struct {
	Version      [4]byte
	InputVector  []byte
	OutputVector []byte
	Locktime     [4]byte
}

type depthProof struct {
	MerkleProof      []byte
	TxIndexInBlock   *big.Int
	BitcoinHeaders   []byte
	CoinbasePreimage [32]byte
	CoinbaseProof    []byte
}

// validate runs the real tbtc-v2 proof validation on a proof. It returns ""
// on acceptance, or the revert reason.
func (w *depthWorld) validate(info redemption.BitcoinInfo, proof redemption.BitcoinProof) string {
	w.t.Helper()
	data, err := w.validABI.Pack("validateProof",
		depthInfo{info.Version, info.InputVector, info.OutputVector, info.Locktime},
		depthProof{proof.MerkleProof, new(big.Int).SetUint64(proof.TxIndexInBlock), proof.BitcoinHeaders, proof.CoinbasePreimage, proof.CoinbaseProof})
	if err != nil {
		w.t.Fatal(err)
	}
	_, err = w.client.CallContract(context.Background(), ethereum.CallMsg{To: &w.validator, Data: data}, nil)
	if err == nil {
		return ""
	}
	return err.Error()
}

// --- Helpers ---------------------------------------------------------------------

func assembled(t *testing.T, btc bitcoin.Chain, hash bitcoin.Hash, depth uint) (redemption.BitcoinInfo, redemption.BitcoinProof) {
	t.Helper()
	tx, proof, err := bitcoin.AssembleSpvProof(hash, depth, btc)
	if err != nil {
		t.Fatalf("assemble a %d-header proof: %v", depth, err)
	}
	return spv.TransactionInfo(tx), redemption.BitcoinProof{MerkleProof: proof.MerkleProof, TxIndexInBlock: uint64(proof.TxIndexInBlock), BitcoinHeaders: proof.BitcoinHeaders, CoinbasePreimage: proof.CoinbasePreimage, CoinbaseProof: proof.CoinbaseProof}
}

// minimalDepth is the oracle: the smallest header count the deployed
// validation accepts for the transaction, with d* - 1 refused for
// insufficient accumulated difficulty.
func (w *depthWorld) minimalDepth(t *testing.T, f *depthFixture, height int, hash bitcoin.Hash) uint {
	t.Helper()
	full := newRecordedBitcoin(t, f, f.LastHeight)
	for d := uint(1); d <= uint(f.LastHeight-height+1); d++ {
		info, proof := assembled(t, full, hash, d)
		reason := w.validate(info, proof)
		if reason == "" {
			if d == 1 {
				t.Fatalf("oracle: a one-header proof is accepted at factor %d; the fixture shows nothing", w.factor)
			}
			info, proof = assembled(t, full, hash, d-1)
			if r := w.validate(info, proof); !strings.Contains(r, "Insufficient accumulated difficulty in header chain") {
				t.Fatalf("oracle: a %d-header proof is not refused for insufficient difficulty: %q", d-1, r)
			}
			return d
		}
		// Short of d*, a proof either ends before its decisive (non-DIFF1)
		// header or has too little accumulated difficulty.
		if !strings.Contains(reason, "Insufficient accumulated difficulty in header chain") &&
			!strings.Contains(reason, "Not at current or previous difficulty") {
			t.Fatalf("oracle: a %d-header proof is refused for another reason: %q", d, reason)
		}
	}
	t.Fatalf("oracle: no recorded depth is accepted for the transaction in block %d", height)
	return 0
}

type depthProducer struct {
	name string
	run  func(ctx context.Context, chain *depthRecorder, btc bitcoin.Chain, hash [32]byte) error
}

var depthProducers = []depthProducer{
	{"movement", func(ctx context.Context, c *depthRecorder, btc bitcoin.Chain, hash [32]byte) error {
		_, err := spv.FrostMovementMaintainer{Chain: c, Bitcoin: btc, Confirmations: 1}.ProveMovement(ctx, depthWallet, hash)
		return err
	}},
	{"incoming-sweep", func(ctx context.Context, c *depthRecorder, btc bitcoin.Chain, hash [32]byte) error {
		_, err := spv.FrostMovementMaintainer{Chain: c, Bitcoin: btc, Confirmations: 1}.ProveIncomingSweep(ctx, depthWallet, hash)
		return err
	}},
	{"deposit-sweep", func(ctx context.Context, c *depthRecorder, btc bitcoin.Chain, hash [32]byte) error {
		_, err := spv.FrostSweepMaintainer{Chain: c, Bitcoin: btc, Confirmations: 1}.ProveSweep(ctx, depthWallet, hash, [20]byte{}, nil)
		return err
	}},
	{"redemption-payment", func(ctx context.Context, c *depthRecorder, btc bitcoin.Chain, hash [32]byte) error {
		_, err := spv.FrostRedemptionMaintainer{Chain: c, Bitcoin: btc, Confirmations: 1, Wallet: depthWallet}.ProveSettlements(ctx)
		return err
	}},
}

// TestFrostProofDepthFollowsBridgeFactor runs each FROST producer on two
// recorded testnet4 transactions, one in a real-difficulty block (157954) and
// one in a DIFF1 block (157937) whose decisive header comes later, against a
// Bridge at the production factor 6 and at the test factor 2.
func TestFrostProofDepthFollowsBridgeFactor(t *testing.T) {
	f := loadDepthFixture(t)
	heights := []int{157954, 157937}
	worlds := make([]*depthWorld, len(depthFactors))
	dStars := make([][]uint, len(depthFactors))
	for i, factor := range depthFactors {
		worlds[i] = newDepthWorld(t, factor, systemTestRelay(f))
		for _, height := range heights {
			dStars[i] = append(dStars[i], worlds[i].minimalDepth(t, f, height, depthTxHash(t, f, height)))
		}
	}
	// The second factor discriminates only if the Bridge's own answer moves.
	for h, height := range heights {
		if dStars[0][h] == dStars[1][h] {
			t.Fatalf("oracle: the Bridge accepts block %d from %d headers at both factor %d and factor %d",
				height, dStars[0][h], depthFactors[0], depthFactors[1])
		}
	}
	for i, w := range worlds {
		w, dStar := w, dStars[i]
		t.Run(fmt.Sprintf("factor-%d", w.factor), func(t *testing.T) {
			checkFrostProofDepth(t, w, f, heights, dStar)
		})
	}
}

func depthTxHash(t *testing.T, f *depthFixture, height int) bitcoin.Hash {
	t.Helper()
	hash, err := bitcoin.NewHashFromString(f.Blocks[strconv.Itoa(height)].Txid, bitcoin.ReversedByteOrder)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// checkFrostProofDepth runs every producer, for the recorded transaction in
// each block, with d* - 1 confirmations and with all recorded confirmations;
// dStars holds the Bridge's minimal accepted depth for each block.
func checkFrostProofDepth(t *testing.T, w *depthWorld, f *depthFixture, heights []int, dStars []uint) {
	t.Helper()
	for h, height := range heights {
		height, dStar := height, dStars[h]
		hash := depthTxHash(t, f, height)
		t.Logf("transaction %s in block %d: the Bridge at factor %d accepts from %d headers", f.Blocks[strconv.Itoa(height)].Txid, height, w.factor, dStar)
		for _, p := range depthProducers {
			p := p
			t.Run(fmt.Sprintf("%s-block-%d", p.name, height), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				key, _ := crypto.ToECDSA(w.key)

				// d* - 1 confirmations: nothing the Bridge would accept exists.
				short := &depthRecorder{tx: hash}
				var err error
				if short.FrostRedemptionChain, err = eth.NewFrostRedemptionChain(ctx, w.client, key, w.bridge); err != nil {
					t.Fatal(err)
				}
				_ = p.run(ctx, short, newRecordedBitcoin(t, f, height+int(dStar)-2), hash)
				for _, c := range short.captured {
					t.Errorf("FROST %s producer: with %d confirmations it submitted a %d-header proof; the Bridge at factor %d needs %d (%s)",
						p.name, dStar-1, len(c.proof.BitcoinHeaders)/80, w.factor, dStar, w.validate(c.info, c.proof))
				}

				// Every recorded confirmation: exactly one proof, of depth d*.
				full := &depthRecorder{tx: hash}
				if full.FrostRedemptionChain, err = eth.NewFrostRedemptionChain(ctx, w.client, key, w.bridge); err != nil {
					t.Fatal(err)
				}
				if err := p.run(ctx, full, newRecordedBitcoin(t, f, f.LastHeight), hash); err != nil {
					t.Fatalf("FROST %s producer: no proof with %d confirmations available: %v", p.name, f.LastHeight-height+1, err)
				}
				if len(full.captured) != 1 {
					t.Fatalf("FROST %s producer: %d submissions, want 1", p.name, len(full.captured))
				}
				c := full.captured[0]
				if got := uint(len(c.proof.BitcoinHeaders) / 80); got != dStar {
					t.Errorf("FROST %s producer: derived a %d-header proof; the Bridge at factor %d needs exactly %d (minimal accepted depth)",
						p.name, got, w.factor, dStar)
				}
				if reason := w.validate(c.info, c.proof); reason != "" {
					t.Errorf("FROST %s producer: the Bridge's proof validation refuses its proof: %s", p.name, reason)
				}
			})
		}
	}
}
