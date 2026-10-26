package ethereum

import (
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
	if _, frost, err := chain.FrostTargetOutputKey([20]byte{1}); err != nil || frost {
		t.Fatal("legacy chain attempted a FROST target RPC", err)
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
