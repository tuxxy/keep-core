package tbtcpg_test

import (
	"context"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Single-main rule for the FROST deposit-sweep leader (D13 refinement, tux
// 2026-10-30): while a FROST wallet's main is reserved for a redemption (C-04
// activeReservation != 0, in any unresolved intent state), the window's
// leader proposes no ordinary deposit sweep; the deposits wait until the
// reservation resolves, as an ECDSA wallet's deposits wait for its main. With
// no reservation the same deposit is proposed, with or without a main.
// Invariant: the honest K-06 host never plans the deposit-only side path
// beside a reserved main. Supports KC-VER P-K06-ONE-MAIN-PURPOSE and
// P-K06-D13-NO-SIDE-SWEEP (KC-VER rev 02 draft). Refs tuxxy/tbtc-v2#24
//
// Unit test. The FROST Bridge reader, the host chain and the Bitcoin chain
// are FIXTURES (in-memory fakes); DepositSweepTask.Run, its FROST branch and
// the real K-06 deposit planner are the code under test.

// d13FrostBridge is a FIXTURE FROST Bridge reader holding one wallet's
// custody, one revealed, reserved deposit and the reservation's intent.
// Methods outside the deposit-sweep proposal path are not implemented (the
// embedded nil interface panics if one is called).
type d13FrostBridge struct {
	tbtc.FrostBridgeChain

	head    uint64
	now     uint64
	custody redemption.Custody
	intents map[[32]byte]redemption.Intent
	record  deposit.Record
	reveal  deposit.Reveal
}

func (b *d13FrostBridge) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: b.head}, nil
}

func (b *d13FrostBridge) Timestamp(context.Context, uint64) (uint64, error) {
	return b.now, nil
}

func (b *d13FrostBridge) Custody(_ context.Context, _ uint64, wallet [32]byte) (redemption.Custody, error) {
	if wallet != b.record.Terms.WalletID {
		return redemption.Custody{}, nil
	}
	return b.custody, nil
}

func (b *d13FrostBridge) Intent(_ context.Context, _ uint64, reservation [32]byte) (redemption.Intent, error) {
	return b.intents[reservation], nil
}

func (b *d13FrostBridge) Reveals(_ context.Context, wallet [32]byte, from, to uint64) ([]deposit.Reveal, error) {
	if wallet != b.reveal.WalletID || b.reveal.Block < from || b.reveal.Block > to {
		return nil, nil
	}
	return []deposit.Reveal{b.reveal}, nil
}

func (b *d13FrostBridge) Deposit(_ context.Context, _ uint64, outpoint [32]byte) (deposit.Record, error) {
	if outpoint != b.reveal.Outpoint {
		return deposit.Record{}, deposit.ErrNotFound
	}
	return b.record, nil
}

func (b *d13FrostBridge) Reveal(_ context.Context, outpoint [32]byte) (deposit.Reveal, error) {
	if outpoint != b.reveal.Outpoint {
		return deposit.Reveal{}, deposit.ErrNotFound
	}
	return b.reveal, nil
}

func (b *d13FrostBridge) Reserved(_ context.Context, _ uint64, outpoint [32]byte) (bool, error) {
	return outpoint == b.reveal.Outpoint, nil
}

// d13HostChain is a FIXTURE host chain: the tbtcpg LocalChain plus the FROST
// target resolver and Bridge binding the FROST proposal branch needs.
type d13HostChain struct {
	*tbtcpg.LocalChain
	label     [20]byte
	outputKey [32]byte
	bridge    *d13FrostBridge
}

func (c *d13HostChain) GetDepositSweepMaxSize() (uint16, error) { return 5, nil }

func (c *d13HostChain) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	return c.outputKey, label == c.label, nil
}

func (c *d13HostChain) FrostBridge(context.Context) (tbtc.FrostBridgeChain, error) {
	return c.bridge, nil
}

// d13BitcoinChain is a FIXTURE Bitcoin chain: the tbtcpg LocalBitcoinChain
// plus a script query reporting the deposit's funding output unspent.
type d13BitcoinChain struct {
	*tbtcpg.LocalBitcoinChain
	funding bitcoin.PreviousOutput
}

func (c *d13BitcoinChain) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	if string(script) != string(c.funding.PublicKeyScript) {
		return nil, nil
	}
	return []bitcoin.PreviousOutput{c.funding}, nil
}

// d13World builds a FROST wallet with one revealed deposit that the leader
// proposes when the main is not reserved: final, reserved under C-10,
// unswept, old enough, confirmed and far from its refund locktime.
func d13World(t *testing.T) (*tbtcpg.DepositSweepTask, *d13HostChain, [20]byte, deposit.Reveal) {
	t.Helper()
	outputKey := [32]byte{0x51, 0x02}
	walletID := dkg.WalletID(outputKey)
	label := [20]byte{0x24}

	fundingScript := bitcoin.Script{0x51, 0x20, 0xaa}
	fundingTx := &bitcoin.Transaction{
		Version: 2,
		Outputs: []*bitcoin.TransactionOutput{{Value: 100_000, PublicKeyScript: fundingScript}},
	}
	fundingHash := fundingTx.Hash()
	outpoint := bitcoin.TransactionOutpoint{TransactionHash: fundingHash, OutputIndex: 0}
	key := deposit.OutpointKey(outpoint)

	now := uint64(time.Now().Unix())
	bridge := &d13FrostBridge{
		head:    1_000,
		now:     now,
		intents: map[[32]byte]redemption.Intent{},
		record: deposit.Record{
			Terms: deposit.Terms{
				WalletID:       walletID,
				Amount:         100_000,
				RefundLocktime: uint32(now + 30*24*3600),
			},
			FundingTransactionHash: [32]byte(fundingHash),
			FundingOutputIndex:     0,
			WalletKey:              outputKey,
			MaximumSweepFee:        50_000,
			RevealedAt:             now - 2*3600,
		},
		reveal: deposit.Reveal{Outpoint: key, WalletID: walletID, Block: 900},
	}

	host := &d13HostChain{
		LocalChain: tbtcpg.NewLocalChain(),
		label:      label,
		outputKey:  outputKey,
		bridge:     bridge,
	}
	host.SetDepositMinAge(60)

	btc := &d13BitcoinChain{
		LocalBitcoinChain: tbtcpg.NewLocalBitcoinChain(),
		funding: bitcoin.PreviousOutput{
			Outpoint:        outpoint,
			Value:           100_000,
			PublicKeyScript: fundingScript,
		},
	}
	btc.SetTransaction(fundingHash, fundingTx)
	btc.SetTransactionConfirmations(fundingHash, tbtc.DepositSweepRequiredFundingTxConfirmations)
	for blocks := uint32(1); blocks <= 32; blocks++ {
		btc.SetEstimateSatPerVByteFee(blocks, 2)
	}

	return tbtcpg.NewDepositSweepTask(host, btc), host, label, bridge.reveal
}

func d13Propose(t *testing.T, task *tbtcpg.DepositSweepTask, label [20]byte) (*tbtc.DepositSweepProposal, error) {
	t.Helper()
	proposal, ok, err := task.Run(&tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: label,
		FrostWallet:         true,
	})
	if !ok || proposal == nil {
		return nil, err
	}
	sweep, isSweep := proposal.(*tbtc.DepositSweepProposal)
	if !isSweep {
		t.Fatalf("fixture: the FROST deposit-sweep task returned a %T", proposal)
	}
	return sweep, err
}

// TestFrostDepositSweepLeaderWaitsWhileMainReserved: the leader proposes the
// deposit when the wallet has no main or an unreserved main (positive
// controls: the deposit is proposable), and proposes nothing while the main
// is reserved, in every unresolved intent state. A refusal may or may not be
// reported as an error; what matters is that no proposal is made.
// Refs tuxxy/tbtc-v2#24
func TestFrostDepositSweepLeaderWaitsWhileMainReserved(t *testing.T) {
	mainKey := [32]byte{0x3a}
	reservation := [32]byte{0x99}

	t.Run("no main, no reservation: proposed", func(t *testing.T) {
		task, host, label, reveal := d13World(t)
		host.bridge.custody = redemption.Custody{}
		proposal, err := d13Propose(t, task, label)
		if err != nil || proposal == nil || len(proposal.DepositsKeys) != 1 ||
			deposit.OutpointKey(bitcoin.TransactionOutpoint{
				TransactionHash: proposal.DepositsKeys[0].FundingTxHash,
				OutputIndex:     proposal.DepositsKeys[0].FundingOutputIndex,
			}) != reveal.Outpoint {
			t.Fatalf("D13 leader: the first sweep of a wallet with no main was not proposed: proposal %v, err %v", proposal != nil, err)
		}
	})

	t.Run("unreserved main: proposed", func(t *testing.T) {
		task, host, label, _ := d13World(t)
		host.bridge.custody = redemption.Custody{Generation: 1, StateVersion: 1, Value: 1_000_000, MainOutpoint: mainKey}
		proposal, err := d13Propose(t, task, label)
		if err != nil || proposal == nil || len(proposal.DepositsKeys) != 1 {
			t.Fatalf("D13 leader: a sweep beside an unreserved main was not proposed: proposal %v, err %v", proposal != nil, err)
		}
	})

	for _, s := range []struct {
		name  string
		state redemption.IntentState
	}{
		{"Reserved", redemption.IntentReserved},
		{"TimeoutPending", redemption.IntentTimeoutPending},
		{"Quarantined", redemption.IntentQuarantined},
	} {
		t.Run("reserved main ("+s.name+"): deposits wait", func(t *testing.T) {
			task, host, label, _ := d13World(t)
			host.bridge.custody = redemption.Custody{Generation: 1, StateVersion: 2, Value: 1_000_000, MainOutpoint: mainKey, ActiveReservation: reservation}
			host.bridge.intents[reservation] = redemption.Intent{ID: reservation, State: s.state}
			proposal, err := d13Propose(t, task, label)
			if proposal != nil {
				t.Fatalf("D13 leader: an ordinary deposit sweep was proposed while the main is reserved (%s): %d deposits, err %v", s.name, len(proposal.DepositsKeys), err)
			}
		})
	}
}
