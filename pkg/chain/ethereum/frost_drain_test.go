package ethereum

import (
	"bytes"
	"context"
	eth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/internal/testutils"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	tbtccontract "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/contract"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	commonEthereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
)

func TestLegacyBridgeDoesNotProbeUnconfiguredFrostExtension(t *testing.T) {
	config := commonEthereum.Config{ContractAddresses: map[string]string{}}
	bridge := common.HexToAddress("0x1234")
	caller, address, err := configuredFrostBridge(config, bridge, nil)
	if err != nil || caller != nil || address != (common.Address{}) {
		t.Fatal("unconfigured extension was not legacy-only", err)
	}
	chain := &TbtcChain{}
	if policy, err := chain.FrostDrainPolicy(); err != nil || policy.CreationMode != 0 || policy.Enabled {
		t.Fatal("legacy chain attempted a FROST policy RPC", err)
	}
	if _, err := chain.MovingFundsNow(); err != nil {
		t.Fatal("legacy clock read required RPC", err)
	}
	config.SetContractAddress(FrostBridgeContractName, bridge.Hex())
	caller, address, err = configuredFrostBridge(config, bridge, nil)
	if err != nil || caller == nil || address != bridge {
		t.Fatal("explicit same-proxy extension was not configured", err)
	}
	if _, _, err := configuredFrostBridge(config, common.HexToAddress("0x5678"), nil); err == nil {
		t.Fatal("foreign FROST proxy accepted")
	}
}

// Only the external RPC boundary is controlled. Use the production TbtcChain,
// generated legacy binding and resolver; any FROST selector is a test failure.
type legacyRoutingBackend struct {
	bind.ContractBackend
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
	chain := &TbtcChain{bridge: bridge} // default configuration: no alias
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
