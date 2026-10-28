package tbtc_test

// tuxxy/keep-core#69 (K09-56): "add a test that upgrades the Bridge after
// construction". Also tuxxy/keep-core#62 (K09-47) and tuxxy/keep-core#34.
//
// Invariant: a client that started against the pre-FROST Bridge keeps
// following the Bridge it talks to. After governance upgrades the same proxy
// to the FROST Bridge, configures the extension, enables the drain and
// completes the cutover, the same running client (not restarted) never
// commits a legacy target for its MovingFunds source, and commits the drain
// to the eligible FROST target instead. Whatever the client concluded about
// the Bridge at startup does not outlive the upgrade.
//
// The upgrade is real: the integration Bridge implementation compiled from git
// at LEGACY_UPGRADED_COMMIT replaces the 40a11d1d implementation behind the
// same TransparentUpgradeableProxy, through its ProxyAdmin (D1: the upgrade
// starts from 40a11d1d; K09-54).
//
// Spec: KC-SF-V3 line 448, line 729 (SC-01), D4(c). Review 5681389770
// (K09-56).

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// EIP-1967 admin slot of the transparent proxy.
var drainProxyAdminSlot = common.HexToHash("0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103")

// TestDrainProducer_BridgeUpgradedAfterStartup: see the file comment. Refs
// tuxxy/keep-core#69, tuxxy/keep-core#62, tuxxy/keep-core#34.
func TestDrainProducer_BridgeUpgradedAfterStartup(t *testing.T) {
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	w := newLegacyWorld(t)
	setup := drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertCode3}
	dw := &drainWorld{legacyWorld: w, setup: setup, nextKey: 1000}
	dw.shortenTimeouts()

	// The client starts against the pre-FROST Bridge.
	dw.recorder = newLegacyRecorder(t, w.anvilURL, setup.encoding)
	var err error
	dw.client, err = w.connect(dw.recorder)
	if err != nil {
		t.Fatalf("pre-FROST startup: %v", err)
	}

	// Governance upgrades the proxy in place.
	upgraded := openTbtcBuild(t, env, dir, commit)
	w.baseline = upgraded
	implementation, _ := w.deployBaseline(map[string]common.Address{}, "contracts/bridge/Bridge.sol", "Bridge")
	slot, err := w.admin.StorageAt(context.Background(), w.bridge, drainProxyAdminSlot, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyAdmin := bind.NewBoundContract(common.BytesToAddress(slot),
		upgraded.buildABI("@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol", "ProxyAdmin"),
		w.admin, w.admin, w.admin)
	w.send("Bridge upgrade", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return proxyAdmin.Transact(o, "upgrade", w.bridge, implementation)
	})
	dw.configureFrost(w)
	dw.createFrostTarget()
	dw.call("FROST drain enabled", dw.frost, "setFrostDrainEnabled", true)
	dw.setMode(modeFrostOnly)
	if got := dw.creationMode(); got != modeFrostOnly {
		t.Fatalf("setup: creation mode %d after the upgrade, want FrostOnly", got)
	}

	// The same client, one coordination window later.
	proposal, err := dw.window("moving-funds window after the upgrade")
	if legacy := dw.committedLegacy(); legacy != nil {
		t.Fatalf("Bridge upgraded after startup: the running client committed legacy targets %s after the cutover", dw.labels(legacy))
	}
	commitments := dw.commitments()
	if len(commitments) != 1 || proposal == nil {
		t.Fatalf("Bridge upgraded after startup: the running client made no drain commitment (commitments %d, proposal %v, error %v)",
			len(commitments), proposal != nil, err)
	}
	t.Logf("Bridge upgraded after startup: drain committed to %s", dw.labels(commitments[0]))
}
