package cmd

// Chaosnet M2 acceptance (tux, 2026-11-13): keep-core starts as a FROST node
// from TOML on the --developer network only. Items 5 (storage ID) and 7
// (wiring) of the M2 brief.
//
// Seam: tbtc.Initialize, the node initialisation cmd/start.go calls with
// clientConfig.Tbtc and clientConfig.Ethereum.Network, and start itself up to
// its Ethereum connection. No chain runs: the node stops at the FROST store's
// fence, which the tests observe.
//
// FIXTURES (labelled where defined): stubChain, stubFrostRegistry,
// stubFrostBridge and stubBitcoin serve only the reads tbtc.Initialize makes
// before it opens the FROST store; recordingFence records the storage ID the
// store acquires and refuses, so the node stops there. The worker file is a
// placeholder and the store key a test value. Nothing here is a real key,
// share, nonce or worker record, and none is printed.

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	commonethereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/keep-network/keep-core/config"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/generator"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/storage"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

const m2Registry = "0x5f3b1c2d4e6a7980a1b2c3d4e5f60718293a4b5c"

// m2StoreKey is a TEST store key.
var m2StoreKey = [32]byte{
	0xa7, 0x3c, 0xe1, 0x5b, 0x92, 0x4f, 0xd8, 0x16, 0x6e, 0xb3, 0x29, 0xf4, 0x87, 0x1a, 0xc5, 0x58,
	0x3d, 0x9e, 0x61, 0xab, 0x14, 0xf7, 0x82, 0x4c, 0xd9, 0x35, 0x6a, 0xbe, 0x23, 0x97, 0xe8, 0x71,
}

// m2Node renders one developer node's configuration file, as in the config
// package's M2 fixture: [developer] FrostWalletRegistryAddress and
// [tbtc.frost] WorkerPath, WorkerSHA256, Keystore, KeyFile, FenceFile.
func m2Node(t *testing.T) (path, keystore string) {
	t.Helper()
	dir := t.TempDir()
	worker := filepath.Join(dir, "snowfall-worker")
	placeholder := []byte("FIXTURE: placeholder worker bytes, never executed\n")
	if err := os.WriteFile(worker, placeholder, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(placeholder)
	keyFile := filepath.Join(dir, "frost-store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(m2StoreKey[:])+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fence := filepath.Join(dir, "frost.fence")
	if err := os.WriteFile(fence, nil, 0600); err != nil {
		t.Fatal(err)
	}
	keystore = filepath.Join(dir, "frost-keystore")
	content := fmt.Sprintf(`[ethereum]
URL = "ws://127.0.0.1:1"
KeyFile = %q
KeyFilePassword = "test-only password"

[bitcoin.electrum]
URL = "tcp://127.0.0.1:1"

[network]
Port = 3919

[storage]
Dir = %q

[tbtc]
PreParamsPoolSize = 0
PreParamsGenerationTimeout = "1m"
PreParamsGenerationDelay = "1h"
PreParamsGenerationConcurrency = 1
KeyGenerationConcurrency = 1

[developer]
WalletRegistryAddress = "0x143ba24e66fce8bca22f7d739f9a932c519b1c76"
FrostWalletRegistryAddress = %q

[tbtc.frost]
WorkerPath = %q
WorkerSHA256 = %q
Keystore = %q
KeyFile = %q
FenceFile = %q
`, filepath.Join(dir, "absent-operator-keyfile"), filepath.Join(dir, "storage"), m2Registry,
		worker, hex.EncodeToString(digest[:]), keystore, keyFile, fence)
	path = filepath.Join(dir, "node.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path, keystore
}

func m2Flags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("start", pflag.ContinueOnError)
	fs.Bool("mainnet", false, "")
	fs.Bool("testnet", false, "")
	fs.Bool("developer", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

// FIXTURE: stubChain is the node's chain handle up to the FROST store: the
// operator key, its signer and the FROST registry binding, which it records.
// Every other tbtc.Chain method is absent (a call panics).
type stubChain struct {
	tbtc.Chain
	private *operator.PrivateKey
	public  *operator.PublicKey

	mu         sync.Mutex
	registries [][20]byte
}

func (c *stubChain) OperatorKeyPair() (*operator.PrivateKey, *operator.PublicKey, error) {
	return c.private, c.public, nil
}
func (c *stubChain) Signing() chain.Signing { return local_v1.NewSigner(c.private) }
func (c *stubChain) CalculateWalletID(*ecdsa.PublicKey) ([32]byte, error) {
	return [32]byte{}, errors.New("FIXTURE: no wallets")
}
func (c *stubChain) ConnectFrost(_ context.Context, registry [20]byte) (tbtc.FrostRegistryChain, tbtc.FrostBridgeChain, error) {
	c.mu.Lock()
	c.registries = append(c.registries, registry)
	c.mu.Unlock()
	return stubFrostRegistry{}, stubFrostBridge{}, nil
}

// FIXTURE: the FROST registry's parameters read before the store opens.
type stubFrostRegistry struct{ tbtc.FrostRegistryChain }

func (stubFrostRegistry) Parameters(context.Context) (dkg.Parameters, error) {
	return dkg.Parameters{
		ChainID: big.NewInt(1337), Registry: common.HexToAddress(m2Registry),
		Pool: common.Address{1}, Bridge: common.Address{2}, GroupSize: 3, Threshold: 2,
		ReadySeats: 2, Profile: 1, ChallengeBlocks: 10,
	}, nil
}

// FIXTURE: the FROST Bridge reads made before the store opens.
type stubFrostBridge struct{ tbtc.FrostBridgeChain }

func (stubFrostBridge) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 1}, nil
}
func (stubFrostBridge) Configuration(context.Context, uint64) (redemption.Configuration, error) {
	return redemption.Configuration{}, nil
}

// FIXTURE: a Bitcoin chain with the script query FROST requires; no method
// is called before the store opens.
type stubBitcoin struct {
	bitcoin.Chain
	redemption.ScriptUnspent
}

var errFenceReached = errors.New("FIXTURE: recording fence refuses after recording")

// FIXTURE: recordingFence records the storage ID the FROST store acquires
// and refuses it, so tbtc.Initialize stops at the store.
type recordingFence struct {
	mu  sync.Mutex
	ids [][32]byte
}

func (f *recordingFence) Acquire(_ context.Context, id [32]byte) (store.FenceLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return nil, errFenceReached
}

func newOperator(t *testing.T) (*operator.PrivateKey, *operator.PublicKey, common.Address) {
	t.Helper()
	private, public, err := operator.GenerateKeyPair(local_v1.DefaultCurve)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.ToECDSA(private.D.FillBytes(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return private, public, crypto.PubkeyToAddress(key.PublicKey)
}

// initializeNode runs tbtc.Initialize as cmd/start.go does, on the fixture
// chain of the given operator, and returns its error and the chain.
func initializeNode(t *testing.T, cfg tbtc.Config, network commonethereum.Network, private *operator.PrivateKey, public *operator.PublicKey) (*stubChain, error) {
	t.Helper()
	disk, err := storage.Initialize(storage.Config{Dir: t.TempDir()}, "test-only password")
	if err != nil {
		t.Fatal(err)
	}
	keyStore, err := disk.InitializeKeyStorePersistence("tbtc")
	if err != nil {
		t.Fatal(err)
	}
	work, err := disk.InitializeWorkPersistence("tbtc")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := &stubChain{private: private, public: public}
	err = tbtc.Initialize(ctx, c, stubBitcoin{}, nil, keyStore, work, generator.StartScheduler(), nil, cfg, nil, nil, network)
	return c, err
}

// frostConfigAsLoaded is a node FROST configuration as the developer
// configuration file yields it: no storage ID (it is not configured).
func frostConfigAsLoaded(t *testing.T, registry [20]byte, fence store.Fence) tbtc.Config {
	t.Helper()
	dir := t.TempDir()
	return tbtc.Config{
		PreParamsPoolSize: 0, PreParamsGenerationTimeout: time.Minute, PreParamsGenerationDelay: time.Hour,
		PreParamsGenerationConcurrency: 1, KeyGenerationConcurrency: 1,
		Frost: tbtc.FrostConfig{
			Registry: registry,
			Worker:   snowfallengine.WorkerConfig{Path: filepath.Join(dir, "snowfall-worker"), SHA256: sha256.Sum256([]byte("FIXTURE worker"))},
			Store:    store.Config{Keystore: filepath.Join(dir, "frost-keystore"), Key: m2StoreKey, Fence: fence},
		},
	}
}

// Acceptance 5. Invariant: a node whose FROST configuration has no storage
// ID (as the developer configuration file yields it) opens its FROST store
// under a storage ID derived from the operator address alone: the same for
// the same operator (also under another registry), different for another
// operator, never zero, and domain-separated (not the address itself, nor
// its plain Keccak-256 or SHA-256).
func TestFrostStorageIDDerivedFromOperator(t *testing.T) {
	privateA, publicA, addressA := newOperator(t)
	privateB, publicB, addressB := newOperator(t)
	registry := common.HexToAddress(m2Registry)
	derive := func(name string, private *operator.PrivateKey, public *operator.PublicKey, registry [20]byte) [32]byte {
		t.Helper()
		fence := &recordingFence{}
		_, err := initializeNode(t, frostConfigAsLoaded(t, registry, fence), commonethereum.Developer, private, public)
		if !errors.Is(err, errFenceReached) {
			t.Fatalf("M2-5: %s: the FROST store was not opened under a derived storage ID: %v", name, err)
		}
		if len(fence.ids) != 1 {
			t.Fatalf("M2-5: %s: the store acquired its fence %d times, want 1", name, len(fence.ids))
		}
		return fence.ids[0]
	}
	a1 := derive("operator A", privateA, publicA, registry)
	a2 := derive("operator A again", privateA, publicA, registry)
	aOther := derive("operator A, another registry", privateA, publicA, [20]byte{0xee})
	b := derive("operator B", privateB, publicB, registry)

	if a1 == ([32]byte{}) || b == ([32]byte{}) {
		t.Fatal("M2-5: a derived storage ID is zero")
	}
	if a1 != a2 {
		t.Fatal("M2-5: the storage ID is not deterministic for the same operator")
	}
	if a1 != aOther {
		t.Fatal("M2-5: the storage ID depends on more than the operator address (registry)")
	}
	if a1 == b {
		t.Fatal("M2-5: two operators derived the same storage ID")
	}
	for operatorName, pair := range map[string]struct {
		id      [32]byte
		address common.Address
	}{"A": {a1, addressA}, "B": {b, addressB}} {
		var left, right [32]byte
		copy(left[12:], pair.address[:])
		copy(right[:], pair.address[:])
		for name, plain := range map[string][32]byte{
			"left-padded address": left, "right-padded address": right,
			"Keccak-256 of the address": crypto.Keccak256Hash(pair.address[:]),
			"SHA-256 of the address":    sha256.Sum256(pair.address[:]),
		} {
			if pair.id == plain {
				t.Fatalf("M2-5: operator %s's storage ID is the %s, not a domain-separated hash", operatorName, name)
			}
		}
	}
}

// Known-answer vector (decision F5, coordinator 2026-11-13): the storage ID
// is Keccak-256("keep-core/frost/storage-id/v1" || operator address, 20
// bytes). TEST operator key: bytes 0x01..0x20 (never a real key).
const (
	m2VectorAddress   = "0x6370eF2f4Db3611D657b90667De398a2Cc2a370C"
	m2VectorStorageID = "665b9786f3b1527eb9472e35f485064126d44a88ed606b6e6f32727ae8ddd42e"
)

// Acceptance 5 (decision F5). Invariant: the derived storage ID is exactly
// Keccak-256("keep-core/frost/storage-id/v1" || operator address): the fixed
// vector, and the formula for a random operator.
func TestFrostStorageIDKnownAnswer(t *testing.T) {
	fixed := make([]byte, 32)
	for i := range fixed {
		fixed[i] = byte(i + 1)
	}
	key, err := crypto.ToECDSA(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if got := crypto.PubkeyToAddress(key.PublicKey); got != common.HexToAddress(m2VectorAddress) {
		t.Fatalf("setup: the vector operator's address is %s, want %s", got.Hex(), m2VectorAddress)
	}
	vectorPrivate, vectorPublic, err := chainethereum.ChainPrivateKeyToOperatorKeyPair(key)
	if err != nil {
		t.Fatal(err)
	}
	randomPrivate, randomPublic, randomAddress := newOperator(t)
	for _, operatorCase := range []struct {
		name    string
		private *operator.PrivateKey
		public  *operator.PublicKey
		want    string
	}{
		{"fixed vector", vectorPrivate, vectorPublic, m2VectorStorageID},
		{"random operator", randomPrivate, randomPublic, hex.EncodeToString(crypto.Keccak256([]byte("keep-core/frost/storage-id/v1"), randomAddress[:]))},
	} {
		fence := &recordingFence{}
		_, err := initializeNode(t, frostConfigAsLoaded(t, common.HexToAddress(m2Registry), fence), commonethereum.Developer, operatorCase.private, operatorCase.public)
		if !errors.Is(err, errFenceReached) {
			t.Fatalf("M2-5: %s: the FROST store was not opened under a derived storage ID: %v", operatorCase.name, err)
		}
		if len(fence.ids) != 1 {
			t.Fatalf("M2-5: %s: the store acquired its fence %d times, want 1", operatorCase.name, len(fence.ids))
		}
		if got := hex.EncodeToString(fence.ids[0][:]); got != operatorCase.want {
			t.Fatalf("M2-5: %s: storage ID %s, want Keccak-256(\"keep-core/frost/storage-id/v1\" || address) = %s", operatorCase.name, got, operatorCase.want)
		}
	}
}

// Acceptance 7. Invariant: a node configured from a developer TOML file
// reaches tbtc.Initialize with FROST configured: the FROST registry it binds
// is [developer] FrostWalletRegistryAddress, and it opens its FROST store
// in the configured keystore under a nonzero derived storage ID. The fence
// alone is replaced by the recording fixture so the node stops there.
func TestDeveloperFrostConfigReachesInitialize(t *testing.T) {
	path, keystore := m2Node(t)
	viper.Reset()
	defer viper.Reset()
	var c config.Config
	if err := c.ReadConfig(path, m2Flags(t, "--developer"), config.StartCmdCategories...); err != nil {
		t.Fatalf("M2-7: the developer FROST node configuration was refused: %v", err)
	}
	if c.Ethereum.Network != commonethereum.Developer {
		t.Fatalf("M2-7: the configuration's network is %v, want developer", c.Ethereum.Network)
	}
	if !c.Tbtc.Frost.Configured() {
		t.Fatal("M2-7: the developer configuration did not configure FROST")
	}
	if c.Tbtc.Frost.Store.Key != m2StoreKey {
		t.Fatal("M2-7: the store key is not the key file's (values withheld)")
	}
	fence := &recordingFence{}
	c.Tbtc.Frost.Store.Fence = fence
	private, public, _ := newOperator(t)
	host, err := initializeNode(t, c.Tbtc, c.Ethereum.Network, private, public)
	if !errors.Is(err, errFenceReached) {
		t.Fatalf("M2-7: tbtc.Initialize did not open the configured FROST store: %v", err)
	}
	if len(host.registries) != 1 || host.registries[0] != common.HexToAddress(m2Registry) {
		t.Fatalf("M2-7: tbtc.Initialize bound FROST registries %x, want [%s]", host.registries, m2Registry)
	}
	if len(fence.ids) != 1 || fence.ids[0] == ([32]byte{}) {
		t.Fatal("M2-7: the FROST store did not acquire its fence under a nonzero storage ID")
	}
	if st, serr := os.Stat(keystore); serr != nil || !st.IsDir() {
		t.Fatalf("M2-7: the FROST store was not opened in the configured keystore %s: %v", keystore, serr)
	}
}

// Acceptance 7 (start). Invariant: start on the developer network with that
// configuration passes the FROST node gate and goes on to the Ethereum
// connection (unreachable here), where a node without FROST would stop too;
// the frozen mainnet start tests keep the gate refusal.
func TestStartPassesFrostGateOnDeveloper(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	path, _ := m2Node(t)
	saved := *clientConfig
	defer func() { *clientConfig = saved }()
	viper.Reset()
	defer viper.Reset()
	*clientConfig = config.Config{}
	if err := clientConfig.ReadConfig(path, m2Flags(t, "--developer"), config.StartCmdCategories...); err != nil {
		t.Fatalf("M2-7: the developer FROST node configuration was refused: %v", err)
	}
	if !clientConfig.Tbtc.Frost.Configured() {
		t.Fatal("M2-7: the developer configuration did not configure FROST")
	}
	err := start(StartCommand)
	if err == nil {
		t.Fatal("M2-7: start returned without error against an unreachable Ethereum URL")
	}
	if gate != nil && err.Error() == gate.Error() {
		t.Fatalf("M2-7: start refused a developer FROST node at the D5 gate: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "error connecting to Ethereum node") {
		t.Fatalf("M2-7: start stopped before the Ethereum connection: %v", err)
	}
}
