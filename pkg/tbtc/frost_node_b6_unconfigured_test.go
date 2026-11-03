package tbtc_test

// Remediation batch 6, shared rule 5: with FROST not configured, the node
// initialisation that now carries FROST dispatch behaves as base against the
// pre-FROST Bridge (40a11d1d, D1): it starts (no new startup failure) and,
// through startup and two coordination windows, calls only functions of the
// base contract ABIs (no new RPC calls, D14 "base calls only"). Refs
// tuxxy/keep-core#26 (KC-SF-V3 line 362, SC-01, D4(c)).
//
// World: the frozen legacy world and its JSON-RPC recorder (batch 1). The
// node is built as cmd/start.go builds it and started in process through
// frostNodeStart with no FROST settings (zero FrostRegistry, no worker, no
// FROST store): that is "FROST unconfigured". Anvil keeps its states in
// memory (b6Anvil, speed only). FIXTURE: b6StakingFixture for
// the ECDSA sortition-pool startup reads the legacy registry fixture does not
// serve (base's own MonitorPool fails on them, with or without FROST).

import (
	"context"
	"testing"
	"time"

	commonethereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/generator"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/storage"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

func TestFrostUnconfigured_NodeUnchanged(t *testing.T) {
	t.Parallel()
	if frostNodeStart == nil {
		t.Fatalf("#26 %s: frostNodeStart is unbound: no base node initialisation with FROST configured", t.Name())
	}
	env := legacyEnv(t)
	env.anvil = b6Anvil(t, env.anvil)
	w := newLegacyWorldOn(t, env, openLegacyBaseline(t, env), nil)
	stop := w.mineInBackground()
	t.Cleanup(stop)
	for _, encoding := range legacyEncodings {
		encoding := encoding
		t.Run(encoding.String(), func(t *testing.T) {
			recorder := newLegacyRecorder(t, w.anvilURL, encoding)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			config := w.clientConfig(recorder.URL())
			_, tbtcChain, blockCounter, _, operatorKey, err := chainethereum.Connect(ctx, config)
			if err != nil {
				t.Fatalf("setup: connect: %v", err)
			}
			provider, err := libp2p.Connect(ctx, libp2p.Config{Port: legacyFreePort(t)}, operatorKey,
				firewall.AnyApplicationPolicy([]firewall.Application{tbtcChain}, firewall.EmptyAllowList()),
				retransmission.NewTicker(blockCounter.WatchBlocks(ctx)))
			if err != nil {
				t.Fatalf("setup: network: %v", err)
			}
			disk, err := storage.Initialize(storage.Config{Dir: t.TempDir()}, b6KeyPassword)
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
			host := frostNodeHost{
				Chain: &b6StakingFixture{TbtcChain: tbtcChain}, BitcoinChain: w.btc, Network: provider,
				KeyStore: keyStore, Work: work, Scheduler: generator.StartScheduler(),
				Proposals:       tbtcpg.NewProposalGenerator(tbtcChain, w.btc),
				Config:          tbtc.Config{PreParamsPoolSize: 0, PreParamsGenerationTimeout: time.Minute, PreParamsGenerationDelay: time.Hour, PreParamsGenerationConcurrency: 1, KeyGenerationConcurrency: 1},
				EthereumNetwork: commonethereum.Developer, EthereumConfig: config,
			}
			if err := frostNodeStart(ctx, host); err != nil {
				t.Fatalf("rule 5: with FROST unconfigured, the node initialisation failed against the pre-FROST Bridge: %v", err)
			}
			// Two coordination windows (the second a multiple of four, whose
			// checklist has every base action).
			head, err := w.admin.BlockNumber(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, window := range []uint64{(head/900 + 1) * 900, (head/3600 + 1) * 3600} {
				current, _ := w.admin.BlockNumber(context.Background())
				if window <= current+2 {
					continue
				}
				if err := w.admin.Client().Call(nil, "anvil_mine", window-current-2); err != nil {
					t.Fatal(err)
				}
				bctx, bcancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := w.waitForBlock(bctx, window+tbtc.B6CoordinationActivePhaseBlocks+5)
				bcancel()
				if err != nil {
					t.Fatal(err)
				}
			}
			w.assertBaselineSurface(t, "rule 5: FROST unconfigured, node initialisation and two coordination windows", recorder.Calls())
		})
	}
}
