package tbtc_test

// Tests of the D14 oracle itself (legacySurface), against the pinned
// 40a11d1d production ABIs. Refs tuxxy/keep-core#73 (review R1 on #95).

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestLegacyUnchanged_SurfaceOracle(t *testing.T) {
	env := legacyEnv(t)
	baseline := openLegacyBaseline(t, env)
	addresses := legacyAddresses{
		bridge:            common.HexToAddress("0x1001"),
		validator:         common.HexToAddress("0x1002"),
		bank:              common.HexToAddress("0x1003"),
		reimbursementPool: common.HexToAddress("0x1004"),
		relay:             common.HexToAddress("0x1005"),
		maintainerProxy:   common.HexToAddress("0x1006"),
		registry:          common.HexToAddress("0x1007"),
	}
	surface := newLegacySurface(baseline, addresses)
	selector := func(signature string) string {
		return hex.EncodeToString(crypto.Keccak256([]byte(signature))[:4])
	}
	registryFixtureABI, _ := baseline.fixture("LegacyWalletRegistryFixture")

	cases := []struct {
		name    string
		call    legacyRPCCall
		allowed bool
	}{
		{"registry fixture-only getter bridge()",
			legacyRPCCall{Method: "eth_call", To: addresses.registry,
				Selector: hex.EncodeToString(registryFixtureABI.Methods["bridge"].ID)}, false},
		{"registry fixture-only getter member()",
			legacyRPCCall{Method: "eth_call", To: addresses.registry,
				Selector: hex.EncodeToString(registryFixtureABI.Methods["member"].ID)}, false},
		{"extra base read Bank.balanceOf at the Bank",
			legacyRPCCall{Method: "eth_call", To: addresses.bank, Selector: selector("balanceOf(address)")}, true},
		{"Bank.balanceOf at the Bridge (right function, wrong address)",
			legacyRPCCall{Method: "eth_call", To: addresses.bridge, Selector: selector("balanceOf(address)")}, false},
		{"FROST walletScheme at the pre-FROST Bridge",
			legacyRPCCall{Method: "eth_call", To: addresses.bridge, Selector: selector("walletScheme(bytes20)")}, false},
		{"base Bridge wallets(bytes20)",
			legacyRPCCall{Method: "eth_call", To: addresses.bridge, Selector: selector("wallets(bytes20)")}, true},
		{"WalletRegistry role: isWalletMember",
			legacyRPCCall{Method: "eth_call", To: addresses.registry,
				Selector: selector("isWalletMember(bytes32,uint32[],address,uint256)")}, true},
		{"SortitionPool role: getOperatorID",
			legacyRPCCall{Method: "eth_call", To: addresses.registry, Selector: selector("getOperatorID(address)")}, true},
		{"RandomBeacon role: sortitionPool",
			legacyRPCCall{Method: "eth_call", To: addresses.registry, Selector: selector("sortitionPool()")}, true},
		{"call to an address that plays no role",
			legacyRPCCall{Method: "eth_call", To: common.HexToAddress("0x2001"), Selector: selector("wallets(bytes20)")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			violations := surface.violations([]legacyRPCCall{c.call})
			if c.allowed && len(violations) > 0 {
				t.Errorf("surface oracle: rejected a production base call: %v", violations)
			}
			if !c.allowed && len(violations) == 0 {
				t.Errorf("surface oracle: accepted a call outside the 40a11d1d production surface")
			}
		})
	}
}
