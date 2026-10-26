package tbtcpg_test

import (
	"errors"
	"testing"

	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

type drainChain struct {
	*tbtcpg.LocalChain
	policy tbtc.FrostDrainPolicy
	err    error
}

func (c *drainChain) FrostDrainPolicy() (tbtc.FrostDrainPolicy, error) { return c.policy, c.err }

func TestFrostDrainTargetsWithoutLegacyLiveWallets(t *testing.T) {
	for _, mode := range []uint8{0, 1, 2} {
		c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, EligibleCount: 2, Targets: [][20]byte{{3}, {2}, {3}}}}
		c.SetWalletParameters(0, 0, 0, 0, 0, 100, 0)
		task := tbtcpg.NewMovingFundsTask(c, nil)
		targets, existing, err := task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 250, 0)
		if err != nil || existing || len(targets) != 2 || targets[0] != [20]byte{2} || targets[1] != [20]byte{3} {
			t.Fatalf("control FrostDrainTargets assertion: mode=%d targets=%v existing=%v err=%v", mode, targets, existing, err)
		}
		c.policy.EligibleCount = 3
		if _, _, err = task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 250, 0); !errors.Is(err, tbtcpg.ErrNotEnoughTargetWallets) {
			t.Fatal("incomplete eligible-target snapshot accepted", err)
		}
	}
}

func TestFrostOnlyDoesNotFallBackToLegacyTargets(t *testing.T) {
	c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: 2}}
	c.SetWalletParameters(0, 0, 0, 0, 0, 100, 0)
	task := tbtcpg.NewMovingFundsTask(c, nil)
	if _, _, err := task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 50, 10); err == nil {
		t.Fatal("disabled FROST drain fell back to legacy targets")
	}
	c.policy.Enabled = true
	if _, _, err := task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 50, 10); !errors.Is(err, tbtcpg.ErrMovingFundsRouteUnavailable) {
		t.Fatal("empty FROST target set fell back to legacy targets", err)
	}
	c.err = errors.New("controlled policy RPC failure")
	if _, _, err := task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 50, 10); !errors.Is(err, c.err) {
		t.Fatal("unknown policy fell back to legacy targets", err)
	}
}

func TestPreCutoverDrainFallback(t *testing.T) {
	for _, mode := range []uint8{0, 1, 2} {
		for _, contained := range []bool{false, true} {
			c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, Contained: contained}}
			if contained {
				c.policy.EligibleCount = 1
				c.policy.Targets = [][20]byte{{8}}
			}
			c.SetWalletParameters(0, 0, 0, 0, 0, 100, 0)
			label := [20]byte{4}
			c.SetWallet(label, &tbtc.WalletChainData{State: tbtc.StateLive})
			c.AddPastNewWalletRegisteredEvent(nil, &tbtc.NewWalletRegisteredEvent{WalletPublicKeyHash: label})
			task := tbtcpg.NewMovingFundsTask(c, nil)
			targets, _, err := task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 50, 1)
			if mode == 2 {
				if !errors.Is(err, tbtcpg.ErrMovingFundsRouteUnavailable) {
					t.Fatal("FrostOnly fell back", err)
				}
			} else if err != nil || len(targets) != 1 || targets[0] != label {
				t.Fatal("legal legacy fallback unavailable", mode, contained, err)
			}
		}
	}
}

type noRouteChain struct{ *tbtcpg.LocalChain }

func (*noRouteChain) GetWalletParameters() (tbtc.WalletParameters, error) {
	return tbtc.WalletParameters{}, errors.New("unexpected transfer-policy RPC")
}
func TestLegacyNoTargetDoesNotReadTransferPolicy(t *testing.T) {
	chain := &noRouteChain{tbtcpg.NewLocalChain()}
	task := tbtcpg.NewMovingFundsTask(chain, nil)
	if _, _, err := task.FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 100, 0); !errors.Is(err, tbtcpg.ErrMovingFundsRouteUnavailable) {
		t.Fatal("legacy no-target path acquired an extra RPC", err)
	}
}
