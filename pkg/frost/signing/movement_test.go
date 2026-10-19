package signing

import (
	"context"
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

func movementTargets(t *testing.T, n int) []MovementTarget {
	t.Helper()
	var out []MovementTarget
	for i := 0; i < n; i++ {
		secret := make([]byte, 32)
		secret[31] = byte(50 + i)
		_, public := btcec.PrivKeyFromBytes(secret)
		var q [32]byte
		copy(q[:], schnorr.SerializePubKey(public))
		id := dkg.WalletID(q)
		var label [20]byte
		copy(label[:], id[:20])
		out = append(out, MovementTarget{Label: label, DescriptorHash: [32]byte{byte(i + 1)}, OutputKey: q})
	}
	return out
}

// movementFixture is a 300,000 sat main moved to three targets with a 3,000
// sat fee: 99,000 each.
func movementFixture(t *testing.T) MovementProposal {
	p, _, _ := movementFixtureWithFunding(t)
	return p
}

func movementFixtureWithFunding(t *testing.T) (MovementProposal, *btcec.PrivateKey, *bitcoin.Transaction) {
	t.Helper()
	p, private, _ := planFixture(t)
	script, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	funding := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{23}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 1_000, PublicKeyScript: script}, {Value: 300_000, PublicKeyScript: script}}}
	main := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: 1}, Value: 300_000, PublicKeyScript: script}
	targets := movementTargets(t, 3)
	o := main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &o, Sequence: 0xffffffff}}}
	for i, v := range MovementSplit(300_000-3_000, 3) {
		s, _ := bitcoin.PayToTaproot(targets[i].OutputKey)
		tx.Outputs = append(tx.Outputs, &bitcoin.TransactionOutput{Value: v, PublicKeyScript: s})
	}
	return MovementProposal{Wallet: p.Wallet, BitcoinGenesis: p.BitcoinGenesis, Generation: 0, StateVersion: 1, Period: 0, Main: main, Targets: targets, MaximumFee: 5_000, MaximumSatPerVByte: 50, Transaction: tx}, private, funding
}

func TestMovementPlanFreezesC06Rules(t *testing.T) {
	p := movementFixture(t)
	plan, err := NewMovementPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsMovement() || plan.IsIncomingSweep() || plan.IsSweep() || plan.Purpose() != Movement || !plan.hostAuthorized() {
		t.Fatal("movement plan kind")
	}
	if plan.MovementMain().Outpoint != p.Main.Outpoint || len(plan.MovementTargets()) != 3 || plan.ConflictInput() != p.Main.Outpoint {
		t.Fatal("movement plan accessors")
	}
	again, err := NewMovementPlan(movementFixture(t))
	if err != nil || again.Commitment() != plan.Commitment() {
		t.Fatal("movement commitment is not deterministic")
	}
	other := movementFixture(t)
	other.Period = 1
	if plan2, _ := NewMovementPlan(other); plan2.Commitment() == plan.Commitment() {
		t.Fatal("the period must bind the plan")
	}
}

func TestMovementPlanRefusals(t *testing.T) {
	for name, mutate := range map[string]func(*MovementProposal){
		"swapped targets": func(p *MovementProposal) { p.Targets[0], p.Targets[1] = p.Targets[1], p.Targets[0] },
		"uneven split": func(p *MovementProposal) {
			p.Transaction.Outputs[0].Value += 2
			p.Transaction.Outputs[1].Value -= 2
		},
		"fee above cap":   func(p *MovementProposal) { p.MaximumFee = 2_999 },
		"missing target":  func(p *MovementProposal) { p.Transaction.Outputs = p.Transaction.Outputs[:2] },
		"foreign main":    func(p *MovementProposal) { p.Main.Outpoint.OutputIndex = 7 },
		"rate above cap":  func(p *MovementProposal) { p.MaximumSatPerVByte = 1 },
		"no targets":      func(p *MovementProposal) { p.Targets = nil },
		"locktime future": func(p *MovementProposal) { p.Transaction.Locktime = 1 << 30; p.Transaction.Inputs[0].Sequence = 0 },
	} {
		p := movementFixture(t)
		mutate(&p)
		if _, err := NewMovementPlan(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func incomingFixture(t *testing.T, withMain bool) IncomingSweepProposal {
	t.Helper()
	p, _, _ := planFixture(t)
	script, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	prev := func(h byte, v int64) bitcoin.PreviousOutput {
		return bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{h}}, Value: v, PublicKeyScript: script}
	}
	proposal := IncomingSweepProposal{Wallet: p.Wallet, BitcoinGenesis: p.BitcoinGenesis, Generation: 0, StateVersion: 1, Incoming: []bitcoin.PreviousOutput{prev(31, 99_000), prev(32, 99_000)}, MaximumFee: 1_000, MaximumSatPerVByte: 50}
	total := int64(198_000)
	tx := &bitcoin.Transaction{Version: 2}
	all := proposal.Incoming
	if withMain {
		main := prev(30, 50_000)
		proposal.Main = &main
		all = append([]bitcoin.PreviousOutput{main}, all...)
		total += 50_000
	}
	for _, in := range all {
		o := in.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
	}
	tx.Outputs = []*bitcoin.TransactionOutput{{Value: total - 500, PublicKeyScript: script}}
	proposal.Transaction = tx
	return proposal
}

func TestIncomingSweepPlan(t *testing.T) {
	for _, withMain := range []bool{false, true} {
		plan, err := NewIncomingSweepPlan(incomingFixture(t, withMain))
		if err != nil {
			t.Fatal(err)
		}
		if !plan.IsIncomingSweep() || plan.IsMovement() || len(plan.IncomingOutputs()) != 2 || (plan.MovementMain() != nil) != withMain {
			t.Fatal("incoming sweep accessors")
		}
	}
	for name, mutate := range map[string]func(*IncomingSweepProposal){
		"pays elsewhere": func(p *IncomingSweepProposal) {
			p.Transaction.Outputs[0].PublicKeyScript, _ = bitcoin.PayToTaproot(movementTargets(t, 1)[0].OutputKey)
		},
		"fee above cap":   func(p *IncomingSweepProposal) { p.Transaction.Outputs[0].Value -= 1_000 },
		"no incoming":     func(p *IncomingSweepProposal) { p.Incoming = nil },
		"duplicate":       func(p *IncomingSweepProposal) { p.Incoming[1] = p.Incoming[0] },
		"unplanned input": func(p *IncomingSweepProposal) { p.Transaction.Inputs[0].Outpoint.OutputIndex = 9 },
	} {
		p := incomingFixture(t, true)
		mutate(&p)
		if _, err := NewIncomingSweepPlan(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMovementExecutorAuthorization(t *testing.T) {
	run := func(t *testing.T, refuse error) (*Result, *engineStub, error) {
		proposal, private, funding := movementFixtureWithFunding(t)
		plan, err := NewMovementPlan(proposal)
		if err != nil {
			t.Fatal(err)
		}
		domain := frost.Domain{Network: "local", Chain: [32]byte{31: 1}, Registry: [20]byte(proposal.Wallet.Registry), Epoch: proposal.Wallet.Epoch}
		key := frost.KeyReady{Candidate: frost.Candidate{Epoch: proposal.Wallet.Epoch, Descriptor: proposal.Wallet.SnowfallDescriptor, OutputKey: proposal.Wallet.OutputKey, Threshold: 2, Roster: []uint16{1, 2, 3}, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{make([]byte, 32)}}
		j := &journalStub{domain: domain, key: key, walletRecord: dkg.WalletRecord{Descriptor: proposal.Wallet, ID: plan.WalletID(), DescriptorHash: plan.DescriptorHash(), State: "ReadyUnfunded"}, records: map[[32]byte][]byte{}}
		authority := &sweepAuthorityStub{receipt: ReservationReceipt{Block: 20, Hash: [32]byte{3}}, err: refuse}
		btc := &bitcoinStub{raw: funding.Serialize(), genesis: proposal.BitcoinGenesis}
		executor, err := NewLocalExecutor(Config{Plan: plan, Reservation: [32]byte{0x6e}, Session: [32]byte{9}, StartBlock: 23, FinalityBlocks: 2, MinimumConfirmations: 1, Journal: j, Sweeps: authority, Bitcoin: btc, Worker: snowfallengine.WorkerConfig{Path: "/unused-test-worker", SHA256: [32]byte{1}}, Selected: []uint16{1, 2}, Transport: func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
			return transportStub{}, func() {}, nil
		}, LocalRegtest: true})
		if err != nil {
			t.Fatal(err)
		}
		engine := &engineStub{executor: executor, private: private}
		executor.engine = engine
		result, err := executor.Run(context.Background())
		return result, engine, err
	}
	t.Run("signs the key-path main under the movement purpose", func(t *testing.T) {
		result, engine, err := run(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		if engine.calls != 1 || len(result.Transaction.Inputs[0].Witness) != 1 || len(result.Transaction.Outputs) != 3 {
			t.Fatal("movement witness")
		}
	})
	t.Run("refused before the worker", func(t *testing.T) {
		_, engine, err := run(t, errors.New("target no longer committed"))
		if !errors.Is(err, ErrUnauthorized) || engine.calls != 0 {
			t.Fatal("unauthorized movement reached the worker", err)
		}
	})
}
