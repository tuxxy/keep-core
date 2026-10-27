package ethereum

import (
	"bytes"
	"context"
	"errors"
	eth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	chainutil "github.com/keep-network/keep-common/pkg/chain/ethereum/ethutil"
	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	tbtccontract "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/contract"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	commonEthereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
)

type capabilityRevert struct{ data interface{} }

func (e capabilityRevert) Error() string          { return "execution reverted" }
func (e capabilityRevert) ErrorCode() int         { return 3 }
func (e capabilityRevert) ErrorData() interface{} { return e.data }

type capabilityBackend struct {
	chainutil.EthereumClient
	mode    *uint8
	failure error
	code    []byte
}

func (b *capabilityBackend) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return b.code, nil
}
func (b *capabilityBackend) CallContract(_ context.Context, msg eth.CallMsg, _ *big.Int) ([]byte, error) {
	a, _ := frostbridgeabi.FrostBridgeMetaData.GetAbi()
	if bytes.Equal(msg.Data, a.Methods["frostCreationMode"].ID) {
		if b.failure != nil {
			return nil, b.failure
		}
		if b.mode == nil {
			return nil, capabilityRevert{"0x"}
		}
		return a.Methods["frostCreationMode"].Outputs.Pack(*b.mode)
	}
	legacy, _ := tbtcabi.BridgeMetaData.GetAbi()
	if bytes.Equal(msg.Data, legacy.Methods["liveWalletsCount"].ID) {
		return legacy.Methods["liveWalletsCount"].Outputs.Pack(uint32(0))
	}
	return nil, errors.New("unexpected capability call")
}
func TestBridgeCapabilityIndependentOfAlias(t *testing.T) {
	bridge := common.Address{1}
	config := commonEthereum.Config{ContractAddresses: map[string]string{}}
	backend := &capabilityBackend{code: []byte{1}}
	caller, address, err := configuredFrostBridge(config, bridge, backend)
	if err != nil || caller != nil || address != bridge {
		t.Fatal("old Bridge did not retain legacy dispatch", err)
	}
	for _, mode := range []uint8{0, 1, 2} {
		backend.mode = &mode
		if _, _, err = configuredFrostBridge(config, bridge, backend); err == nil || !strings.Contains(err.Error(), "configure FrostBridge") {
			t.Fatal("control BridgeCapability assertion: upgraded default-config host started", mode, err)
		}
	}
	backend.mode = nil
	for _, failure := range []error{errors.New("RPC unavailable"), capabilityRevert{"0x1234"}, errors.New("execution reverted")} {
		backend.failure = failure
		if _, _, err = configuredFrostBridge(config, bridge, backend); err == nil {
			t.Fatal("unknown failure became LegacyOpen")
		}
	}
	backend.failure = nil
	config.SetContractAddress(FrostBridgeContractName, bridge.Hex())
	if caller, _, err = configuredFrostBridge(config, bridge, backend); err != nil || caller == nil {
		t.Fatal(err)
	}
	if _, _, err = configuredFrostBridge(config, common.Address{2}, backend); err == nil {
		t.Fatal("foreign proxy accepted")
	}
}

// Only the external RPC boundary is controlled. Use the production TbtcChain,
// generated legacy binding and resolver; any FROST selector is a test failure.
type legacyRoutingBackend struct {
	chainutil.EthereumClient
	t     *testing.T
	known [20]byte
	reads int
}

func (b *legacyRoutingBackend) CallContract(_ context.Context, msg eth.CallMsg, _ *big.Int) ([]byte, error) {
	a, err := tbtcabi.BridgeMetaData.GetAbi()
	if err != nil {
		b.t.Fatal(err)
	}
	method := a.Methods["wallets"]
	if len(msg.Data) == 4 {
		return (&capabilityBackend{code: []byte{1}}).CallContract(context.Background(), msg, nil)
	}
	if len(msg.Data) != 36 || !bytes.Equal(msg.Data[:4], method.ID) {
		b.t.Fatal("non-legacy routing RPC")
	}
	b.reads++
	var label [20]byte
	copy(label[:], msg.Data[4:24])
	wallet := tbtcabi.WalletsWallet{}
	if label == b.known {
		wallet.CreatedAt, wallet.State = 1, 1
	}
	return method.Outputs.Pack(wallet)
}
func (b *legacyRoutingBackend) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return []byte{1}, nil
}
func TestUnconfiguredHostRefusesUnknownCommittedTarget(t *testing.T) {
	backend := &legacyRoutingBackend{t: t, known: [20]byte{1}}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := tbtccontract.NewBridge(common.Address{1}, big.NewInt(1), &keystore.Key{PrivateKey: key}, backend, nil, nil, nil, &sync.Mutex{})
	if err != nil {
		t.Fatal(err)
	}
	chain := &TbtcChain{bridge: bridge, baseChain: &baseChain{client: backend}, frostBridgeAddress: common.Address{1}} // default configuration: no alias
	if keys, err := tbtc.FrostTargetKeys(chain, [][20]byte{backend.known}); err != nil || keys != nil {
		t.Fatal("known legacy target refused", err)
	}
	unknown := [20]byte{2} // e.g. a publicly committed FROST label
	if _, err := tbtc.FrostTargetKeys(chain, [][20]byte{unknown}); err == nil {
		t.Fatal("control UnconfiguredTarget assertion: unknown label became P2WPKH")
	}
	// Caller-specified fees must not bypass the production resolver either.
	task := tbtcpg.NewMovingFundsTask(chain, nil)
	if _, err := task.ProposeMovingFunds(&testutils.MockLogger{}, [20]byte{3}, nil, [][20]byte{unknown}, 500); err == nil {
		t.Fatal("control UnconfiguredProposal assertion: unknown target proposed")
	}
	if backend.reads != 3 {
		t.Fatal("unexpected routing reads", backend.reads)
	}
}
