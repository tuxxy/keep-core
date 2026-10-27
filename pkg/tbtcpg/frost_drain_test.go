package tbtcpg_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"testing"
	"time"

	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

type drainChain struct {
	*tbtcpg.LocalChain
	policy tbtc.FrostDrainPolicy
	live   uint32
	err    error
}

func (c *drainChain) GetLiveWalletsCount() (uint32, error) { return c.live, nil }

func (c *drainChain) FrostDrainPolicy() (tbtc.FrostDrainPolicy, error) { return c.policy, c.err }

func TestFrostDrainTargetsWithoutLegacyLiveWallets(t *testing.T) {
	for _, mode := range []uint8{0, 1, 2} {
		c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, RemainingCustody: 1000000, EligibleCount: 2, Targets: [][20]byte{{3}, {2}, {3}}}}
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
			c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, RemainingCustody: 1000000, Contained: contained}}
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

func TestBudgetExhaustionFallsBackBeforeCutover(t *testing.T) {
	for _, mode := range []uint8{0, 1, 2} {
		for _, remaining := range []uint64{0, 49} {
			c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, EligibleCount: 1, Targets: [][20]byte{{8}}, RemainingCustody: remaining}}
			c.SetWalletParameters(0, 0, 0, 0, 0, 100, 0)
			label := [20]byte{4}
			c.SetWallet(label, &tbtc.WalletChainData{State: tbtc.StateLive})
			c.AddPastNewWalletRegisteredEvent(nil, &tbtc.NewWalletRegisteredEvent{WalletPublicKeyHash: label})
			targets, _, err := tbtcpg.NewMovingFundsTask(c, nil).FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 50, 1)
			if mode == 2 {
				if !errors.Is(err, tbtcpg.ErrMovingFundsRouteUnavailable) {
					t.Fatal("control DrainBudget assertion: FrostOnly proposed an inadmissible drain", err)
				}
			} else if err != nil || len(targets) != 1 || targets[0] != label {
				t.Fatal("control DrainBudget assertion: legal legacy fallback lost", err, targets)
			}
		}
	}
}

type resetProducer struct {
	*drainChain
	now    time.Time
	resets int
}

func (c *resetProducer) ComputeMainUtxoHash(u *bitcoin.UnspentTransactionOutput) [32]byte {
	raw := append([]byte(nil), u.Outpoint.TransactionHash[:]...)
	raw = binary.BigEndian.AppendUint32(raw, u.Outpoint.OutputIndex)
	raw = binary.BigEndian.AppendUint64(raw, uint64(u.Value))
	return crypto.Keccak256Hash(raw)
}
func (c *resetProducer) MovingFundsNow() (time.Time, error) { return c.now, nil }
func (c *resetProducer) ResetLegacyMovingFundsTimeout(source [20]byte, main *bitcoin.UnspentTransactionOutput) error {
	c.resets++
	wallet, err := c.GetWallet(source)
	if err != nil {
		return err
	}
	wallet.MovingFundsRequestedAt = c.now
	c.SetWallet(source, wallet)
	return nil
}

type resetBitcoin struct {
	*tbtcpg.LocalBitcoinChain
	hash bitcoin.Hash
}

func (c resetBitcoin) GetTxHashesForPublicKeyHash([20]byte) ([]bitcoin.Hash, error) {
	return []bitcoin.Hash{c.hash}, nil
}
func TestHostSubmitsDueLegacyReset(t *testing.T) {
	source := [20]byte{9}
	script, err := bitcoin.PayToWitnessPublicKeyHash(source)
	if err != nil {
		t.Fatal(err)
	}
	funding := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{1}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100000, PublicKeyScript: script}}}
	hash := funding.Hash()
	main := &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: hash}, Value: 100000}
	c := &resetProducer{drainChain: &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: 2, Enabled: true, Contained: true, EligibleCount: 1, RemainingCustody: 1000000}}, now: time.Unix(2000000, 0)}
	wallet := &tbtc.WalletChainData{State: tbtc.StateMovingFunds, MovingFundsRequestedAt: c.now, MainUtxoHash: c.ComputeMainUtxoHash(main)}
	c.SetWallet(source, wallet)
	c.SetMovingFundsParameters(1000, 0, 10, 100, nil, 0, 0, 1000, 100, nil, 0)
	btc := resetBitcoin{LocalBitcoinChain: tbtcpg.NewLocalBitcoinChain(), hash: hash}
	btc.SetTransaction(hash, funding)
	task := tbtcpg.NewMovingFundsTask(c, btc)
	c.now = c.now.Add(10 * time.Second)
	if reset, err := task.ResetMovingFundsTimeoutIfDue(source, wallet); err != nil || reset {
		t.Fatal("early reset sent", err)
	}
	for i := 1; i <= 3; i++ {
		c.now = c.now.Add(99 * time.Second)
		if proposal, valid, err := task.Run(&tbtc.CoordinationProposalRequest{WalletPublicKeyHash: source}); err != nil || valid || proposal != nil || c.resets != i {
			t.Fatal("control HostTimeoutReset assertion: due host reset absent", err, c.resets)
		}
	}
	c.policy.Contained = false
	c.policy.Targets = [][20]byte{{8}}
	c.now = c.now.Add(99 * time.Second)
	if reset, err := task.ResetMovingFundsTimeoutIfDue(source, wallet); err != nil || reset {
		t.Fatal("reset with open route", err)
	}
}

func TestNoRouteHostRecoveryAllModes(t *testing.T) {
	for _, mode := range []uint8{0, 1, 2} {
		for _, blocked := range []string{"partial-budget", "budget", "contained", "disabled", "empty", "dust"} {
			for _, live := range []uint32{0, 1} {
				t.Run(fmt.Sprintf("mode%d/%s/live%d", mode, blocked, live), func(t *testing.T) {
					source := [20]byte{9}
					script, _ := bitcoin.PayToWitnessPublicKeyHash(source)
					funding := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{1}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100000, PublicKeyScript: script}}}
					hash := funding.Hash()
					main := &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: hash}, Value: 100000}
					policy := tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, RemainingCustody: 1000000, DustThreshold: 1000, EligibleCount: 1, Targets: [][20]byte{{8}}}
					switch blocked {
					case "partial-budget":
						policy.RemainingCustody = 99999
					case "budget":
						policy.RemainingCustody = 0
					case "contained":
						policy.Contained = true
					case "disabled":
						policy.Enabled = false
					case "empty":
						policy.EligibleCount = 0
						policy.Targets = nil
					case "dust":
						policy.DustThreshold = 100001
					}
					c := &resetProducer{drainChain: &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: policy, live: live}, now: time.Unix(2000000, 0)}
					wallet := &tbtc.WalletChainData{State: tbtc.StateMovingFunds, MovingFundsRequestedAt: c.now, MainUtxoHash: c.ComputeMainUtxoHash(main)}
					c.SetWallet(source, wallet)
					c.SetMovingFundsParameters(1000, 0, 10, 100, nil, 0, 0, 1000, 100, nil, 0)
					btc := resetBitcoin{LocalBitcoinChain: tbtcpg.NewLocalBitcoinChain(), hash: hash}
					btc.SetTransaction(hash, funding)
					task := tbtcpg.NewMovingFundsTask(c, btc)
					for i := 0; i < 3; i++ {
						c.now = c.now.Add(99 * time.Second)
						reset, err := task.ResetMovingFundsTimeoutIfDue(source, wallet)
						want := mode == 2 || live == 0
						if err != nil || reset != want || c.resets != func() int {
							if want {
								return i + 1
							}
							return 0
						}() {
							t.Fatal("control NoRouteHost assertion: reset eligibility mismatch", reset, err, c.resets)
						}
					}
					if mode == 2 || live == 0 {
						if _, _, err := task.FindTargetWallets(&testutils.MockLogger{}, source, [32]byte{}, 100000, live); !errors.Is(err, tbtcpg.ErrMovingFundsRouteUnavailable) {
							t.Fatal("control DrainDust assertion: inadmissible FROST drain proposed", err)
						}
					}
					c.policy.Enabled = true
					c.policy.Contained = false
					c.policy.EligibleCount = 1
					c.policy.Targets = [][20]byte{{8}}
					c.policy.RemainingCustody = 1000000
					c.policy.DustThreshold = 1000
					c.now = c.now.Add(99 * time.Second)
					if reset, err := task.ResetMovingFundsTimeoutIfDue(source, wallet); err != nil || reset {
						t.Fatal("reopened route still reset", err)
					}
				})
			}
		}
	}
}
func TestIncompleteFrostIndexKeepsLegalLegacyFallback(t *testing.T) {
	for _, mode := range []uint8{0, 1, 2} {
		c := &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{CreationMode: mode, Enabled: true, RemainingCustody: 1000000, EligibleCount: 2, Targets: [][20]byte{{8}}}}
		c.SetWalletParameters(0, 0, 0, 0, 0, 100, 0)
		label := [20]byte{4}
		c.SetWallet(label, &tbtc.WalletChainData{State: tbtc.StateLive})
		c.AddPastNewWalletRegisteredEvent(nil, &tbtc.NewWalletRegisteredEvent{WalletPublicKeyHash: label})
		targets, _, err := tbtcpg.NewMovingFundsTask(c, nil).FindTargetWallets(&testutils.MockLogger{}, [20]byte{9}, [32]byte{}, 150, 1)
		if mode == 2 {
			if !errors.Is(err, tbtcpg.ErrNotEnoughTargetWallets) {
				t.Fatal("FrostOnly used incomplete targets", err)
			}
		} else if err != nil || len(targets) != 1 || targets[0] != label {
			t.Fatal("legal legacy fallback lost with partial index", targets, err)
		}
	}
}

func TestPreExtensionHostKeepsOriginalResetDispatch(t *testing.T) {
	c := &resetProducer{drainChain: &drainChain{LocalChain: tbtcpg.NewLocalChain(), policy: tbtc.FrostDrainPolicy{LegacyOnly: true}}}
	task := tbtcpg.NewMovingFundsTask(c, nil)
	if reset, err := task.ResetMovingFundsTimeoutIfDue([20]byte{1}, &tbtc.WalletChainData{State: tbtc.StateMovingFunds}); err != nil || reset || c.resets != 0 {
		t.Fatal("pre-extension host acquired native FROST reset dispatch", reset, err)
	}
}
