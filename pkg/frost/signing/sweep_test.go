package signing

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/txscript"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// depositOutput commits a Q wallet leaf (tagged by receipt byte r) beside a
// refund-like sibling under an unrelated internal key.
func depositOutput(t *testing.T, q [32]byte, r byte) (bitcoin.Script, *bitcoin.TapscriptSpend) {
	t.Helper()
	leaf := append([]byte{txscript.OP_DATA_32}, bytes.Repeat([]byte{r}, 32)...)
	leaf = append(append(leaf, txscript.OP_DROP, txscript.OP_DATA_32), q[:]...)
	leaf = append(leaf, txscript.OP_CHECKSIG)
	sibling := append([]byte{txscript.OP_DATA_32}, bytes.Repeat([]byte{r + 1}, 32)...)
	sibling = append(sibling, txscript.OP_DROP, txscript.OP_TRUE)
	tree := txscript.AssembleTaprootScriptTree(txscript.NewBaseTapLeaf(leaf), txscript.NewBaseTapLeaf(sibling))
	internalSecret := make([]byte, 32)
	internalSecret[31] = 99
	_, internal := btcec.PrivKeyFromBytes(internalSecret)
	root := tree.RootNode.TapHash()
	output := txscript.ComputeTaprootOutputKey(internal, root[:])
	proof := tree.LeafMerkleProofs[0]
	if !bytes.Equal(proof.TapLeaf.Script, leaf) {
		proof = tree.LeafMerkleProofs[1]
	}
	block := proof.ToControlBlock(internal)
	control, err := block.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	script, err := txscript.PayToTaprootScript(output)
	if err != nil {
		t.Fatal(err)
	}
	return script, &bitcoin.TapscriptSpend{Leaf: leaf, Control: control}
}

// sweepFixture is a main input and two deposits from one funding transaction.
func sweepFixture(t *testing.T) (SweepProposal, *btcec.PrivateKey, *bitcoin.Transaction) {
	t.Helper()
	p, private, _ := planFixture(t)
	q := p.Wallet.OutputKey
	main, _ := bitcoin.PayToTaproot(q)
	a, spendA := depositOutput(t, q, 0x31)
	b, spendB := depositOutput(t, q, 0x41)
	funding := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{19}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100_000, PublicKeyScript: main}, {Value: 50_000, PublicKeyScript: a}, {Value: 60_000, PublicKeyScript: b}}}
	hash := funding.Hash()
	prev := func(i uint32) bitcoin.PreviousOutput {
		return bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: i}, Value: funding.Outputs[i].Value, PublicKeyScript: funding.Outputs[i].PublicKeyScript}
	}
	mainPrev := prev(0)
	deposits := []SweepDeposit{{Prevout: prev(1), Spend: spendA, TreasuryFee: 500, MaximumSweepFee: 2_000, RefundLocktime: 1_900_000_000}, {Prevout: prev(2), Spend: spendB, TreasuryFee: 600, MaximumSweepFee: 2_000, RefundLocktime: 1_900_000_100}}
	for i := range deposits {
		deposits[i].Key = depositKey(deposits[i].Prevout.Outpoint)
	}
	tx := &bitcoin.Transaction{Version: 2}
	for _, o := range []bitcoin.PreviousOutput{mainPrev, deposits[0].Prevout, deposits[1].Prevout} {
		o := o.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
	}
	tx.Outputs = []*bitcoin.TransactionOutput{{Value: 210_000 - 1_001, PublicKeyScript: main}}
	return SweepProposal{Wallet: p.Wallet, BitcoinGenesis: p.BitcoinGenesis, Generation: 0, StateVersion: 1, Vault: [20]byte{7}, Main: &mainPrev, Deposits: deposits, MaximumSatPerVByte: 10, Transaction: tx}, private, funding
}

func TestSweepPlanFreezesC05Accounting(t *testing.T) {
	p, _, _ := sweepFixture(t)
	plan, err := NewSweepPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsSweep() || plan.Purpose() != Sweep || plan.SweepMain() == nil || len(plan.SweepDeposits()) != 2 || plan.SweepVault() != p.Vault || plan.ConflictInput() != p.Main.Outpoint {
		t.Fatal("sweep accessors")
	}
	// 1,001 sat over two deposits: 500 to the first, 501 to the last (C-05).
	if s := sweepShares(1_001, 2); s[0] != 500 || s[1] != 501 {
		t.Fatal("C-05 fee split", s)
	}
	digests := plan.Snapshot().Digests()
	key, _ := bitcoin.TaprootSignatureHash(p.Transaction, plan.Snapshot().PreviousOutputs(), 0)
	leaf, _ := bitcoin.TapscriptSignatureHash(p.Transaction, plan.Snapshot().PreviousOutputs(), 1, p.Deposits[0].Spend.Leaf)
	if digests[0] != key || digests[1] != leaf {
		t.Fatal("main must be key-path and deposits BIP342")
	}
	// Every accounting input is committed.
	other := p
	other.Deposits = append([]SweepDeposit(nil), p.Deposits...)
	other.Deposits[0].MaximumSweepFee = 1_999
	changed, err := NewSweepPlan(other)
	if err != nil || changed.Commitment() == plan.Commitment() {
		t.Fatal("captured cap must be committed", err)
	}
	other = p
	other.Vault = [20]byte{8}
	if changed, err = NewSweepPlan(other); err != nil || changed.Commitment() == plan.Commitment() {
		t.Fatal("vault must be committed", err)
	}
	deposit := p
	deposit.Main = nil
	deposit.Transaction = &bitcoin.Transaction{Version: 2, Inputs: p.Transaction.Inputs[1:], Outputs: []*bitcoin.TransactionOutput{{Value: 110_000 - 1_000, PublicKeyScript: p.Transaction.Outputs[0].PublicKeyScript}}}
	if only, err := NewSweepPlan(deposit); err != nil || only.SweepMain() != nil || only.ConflictInput() != p.Deposits[0].Prevout.Outpoint {
		t.Fatal("deposit-only sweep", err)
	}
}

func TestPossibleReleaseIncludesGrantWithoutDone(t *testing.T) {
	e, _, _, _ := sweepExecutor(t)
	journal := e.config.Journal
	e.config.AfterGrant = func(context.Context) error { return errors.New("interrupted after durable grant") }
	if released, err := e.PossibleRelease(context.Background()); err != nil || released {
		t.Fatal("fresh plan already marked released", err)
	}
	if _, err := e.Run(context.Background()); err == nil {
		t.Fatal("grant interruption ignored")
	}
	attempt, err := e.attempt(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = journal.ReadSigningRecord(context.Background(), signingDoneID(snowfallengine.AttemptID(attempt))); err == nil {
		t.Fatal("test unexpectedly retained a done record")
	}
	if released, err := e.PossibleRelease(context.Background()); err != nil || !released {
		t.Fatal("control IncompleteGrant assertion: grant without done was considered safe", err)
	}
}

func TestSweepPlanRefusals(t *testing.T) {
	for name, mutate := range map[string]func(*SweepProposal){
		"share above cap": func(p *SweepProposal) { p.Deposits[1].MaximumSweepFee = 500 },
		"no credit left":  func(p *SweepProposal) { p.Deposits[0].TreasuryFee = 49_500 },
		"two outputs": func(p *SweepProposal) {
			p.Transaction.Outputs = append(p.Transaction.Outputs, &bitcoin.TransactionOutput{Value: 1_000, PublicKeyScript: p.Transaction.Outputs[0].PublicKeyScript})
			p.Transaction.Outputs[0].Value -= 1_000
		},
		"foreign output": func(p *SweepProposal) {
			p.Transaction.Outputs[0].PublicKeyScript = append(bitcoin.Script{0x51, 0x20}, bytes.Repeat([]byte{2}, 32)...)
		},
		"relative lock": func(p *SweepProposal) { p.Transaction.Inputs[1].Sequence = 10 },
		"absolute lock": func(p *SweepProposal) {
			p.Transaction.Locktime = 800_000
			p.Transaction.Inputs[0].Sequence = 0xfffffffd
		},
		"fee rate":          func(p *SweepProposal) { p.MaximumSatPerVByte = 1 },
		"duplicate deposit": func(p *SweepProposal) { p.Deposits[1] = p.Deposits[0] },
		"wrong deposit key": func(p *SweepProposal) { p.Deposits[0].Key[0] ^= 1 },
		"missing leaf":      func(p *SweepProposal) { p.Deposits[0].Spend = nil },
		"swapped leaves": func(p *SweepProposal) {
			p.Deposits[0].Spend, p.Deposits[1].Spend = p.Deposits[1].Spend, p.Deposits[0].Spend
		},
		"resolution without main": func(p *SweepProposal) {
			p.Main = nil
			p.Resolution = [32]byte{1}
			p.Transaction.Inputs = p.Transaction.Inputs[1:]
		},
		"main moved": func(p *SweepProposal) {
			p.Transaction.Inputs[0], p.Transaction.Inputs[1] = p.Transaction.Inputs[1], p.Transaction.Inputs[0]
		},
		"invalid wallet": func(p *SweepProposal) { p.Wallet = dkg.Descriptor{} },
	} {
		t.Run(name, func(t *testing.T) {
			p, _, _ := sweepFixture(t)
			mutate(&p)
			if _, err := NewSweepPlan(p); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	t.Run("input cap", func(t *testing.T) {
		p, _, _ := sweepFixture(t)
		for len(p.Deposits)+1 <= MaxSweepInputs {
			d := p.Deposits[0]
			d.Prevout.Outpoint.OutputIndex = uint32(100 + len(p.Deposits))
			d.Key = depositKey(d.Prevout.Outpoint)
			p.Deposits = append(p.Deposits, d)
			o := d.Prevout.Outpoint
			p.Transaction.Inputs = append(p.Transaction.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
		}
		if _, err := NewSweepPlan(p); err == nil || !strings.Contains(err.Error(), "input count") {
			t.Fatal("129 inputs accepted:", err)
		}
	})
}

type sweepAuthorityStub struct {
	receipt ReservationReceipt
	err     error
	calls   int
	hook    func(int)
}

func (s *sweepAuthorityStub) AuthorizeSweep(context.Context, *Plan) (ReservationReceipt, error) {
	s.calls++
	if s.hook != nil {
		s.hook(s.calls)
	}
	return s.receipt, s.err
}

func sweepExecutor(t *testing.T) (*Executor, *sweepAuthorityStub, *bitcoinStub, *engineStub) {
	t.Helper()
	proposal, private, funding := sweepFixture(t)
	plan, err := NewSweepPlan(proposal)
	if err != nil {
		t.Fatal(err)
	}
	domain := frost.Domain{Network: "local", Chain: [32]byte{31: 1}, Registry: [20]byte(proposal.Wallet.Registry), Epoch: proposal.Wallet.Epoch}
	key := frost.KeyReady{Candidate: frost.Candidate{Epoch: proposal.Wallet.Epoch, Descriptor: proposal.Wallet.SnowfallDescriptor, OutputKey: proposal.Wallet.OutputKey, Threshold: 2, Roster: []uint16{1, 2, 3}, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{make([]byte, 32)}}
	j := &journalStub{domain: domain, key: key, walletRecord: dkg.WalletRecord{Descriptor: proposal.Wallet, ID: plan.WalletID(), DescriptorHash: plan.DescriptorHash(), State: "ReadyUnfunded"}, records: map[[32]byte][]byte{}}
	authority := &sweepAuthorityStub{receipt: ReservationReceipt{Block: 20, Hash: [32]byte{3}}}
	btc := &bitcoinStub{raw: funding.Serialize(), genesis: proposal.BitcoinGenesis}
	executor, err := NewLocalExecutor(Config{Plan: plan, Reservation: [32]byte{0x5e}, Session: [32]byte{9}, StartBlock: 23, FinalityBlocks: 2, MinimumConfirmations: 1, Journal: j, Sweeps: authority, Bitcoin: btc, Worker: snowfallengine.WorkerConfig{Path: "/unused-test-worker", SHA256: [32]byte{1}}, Selected: []uint16{1, 2}, Transport: func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
		return transportStub{}, func() {}, nil
	}, LocalRegtest: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := &engineStub{executor: executor, private: private}
	executor.engine = engine
	return executor, authority, btc, engine
}

func TestSweepExecutorAuthorization(t *testing.T) {
	t.Run("signs key-path main and BIP342 deposits", func(t *testing.T) {
		e, _, b, engine := sweepExecutor(t)
		result, err := e.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if engine.calls != 3 || len(result.Transaction.Inputs[0].Witness) != 1 || len(result.Transaction.Inputs[1].Witness) != 3 {
			t.Fatal("sweep witnesses")
		}
		if err = e.Broadcast(context.Background(), result); err != nil || len(b.sent) == 0 {
			t.Fatal("authorized sweep not broadcast", err)
		}
		if err = e.Rebroadcast(context.Background()); err == nil {
			t.Fatal("rebroadcast is for payments only")
		}
	})
	t.Run("refused before the worker", func(t *testing.T) {
		e, a, _, engine := sweepExecutor(t)
		a.err = errors.New("deposit past refund margin")
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrUnauthorized) || engine.calls != 0 {
			t.Fatal("unauthorized sweep reached the worker", err)
		}
	})
	t.Run("last check after claim", func(t *testing.T) {
		e, a, _, _ := sweepExecutor(t)
		e.boundary = func(name string) error {
			if name == "before-worker-start" {
				a.err = errors.New("margin crossed")
			}
			return nil
		}
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
	})
	t.Run("anchor change", func(t *testing.T) {
		e, a, _, _ := sweepExecutor(t)
		a.hook = func(n int) {
			if n == 2 {
				a.receipt.Hash[0]++
			}
		}
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
	})
	t.Run("late signature quarantined and never broadcast", func(t *testing.T) {
		e, a, b, _ := sweepExecutor(t)
		e.boundary = func(name string) error {
			if name == "after-signature" {
				a.err = errors.New("margin crossed")
			}
			return nil
		}
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrLateSignature) {
			t.Fatal(err)
		}
		if released, err := e.PossibleRelease(context.Background()); err != nil || !released {
			t.Fatal("escaped signature not retained", err)
		}
		if _, err := e.LoadResult(context.Background()); err == nil || len(b.sent) != 0 {
			t.Fatal("late sweep recovered into a broadcastable result")
		}
	})
	t.Run("sweep attempts are separate from payments", func(t *testing.T) {
		e, _, _, _ := sweepExecutor(t)
		sweep, _ := e.attempt(0)
		e.config.Plan.sweep = nil
		payment, _ := e.attempt(0)
		if snowfallengine.AttemptID(sweep) == snowfallengine.AttemptID(payment) {
			t.Fatal("sweep attempt domain")
		}
	})
}
