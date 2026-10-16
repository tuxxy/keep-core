package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

func planFixture(t *testing.T) (Proposal, *btcec.PrivateKey, *bitcoin.Transaction) {
	t.Helper()
	secret := make([]byte, 32)
	secret[31] = 17
	private, pub := btcec.PrivKeyFromBytes(secret)
	var q [32]byte
	copy(q[:], schnorr.SerializePubKey(pub))
	script, err := bitcoin.PayToTaproot(q)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address{1}, Epoch: 17, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{1}, OutputKey: q}
	funding := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{19}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100000, PublicKeyScript: script}}}
	point := bitcoin.TransactionOutpoint{TransactionHash: funding.Hash()}
	recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{13})
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: 10000, PublicKeyScript: recipient}, {Value: 89000, PublicKeyScript: script}}}
	return Proposal{Wallet: descriptor, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Purpose: Redemption, Generation: 1, StateVersion: 1, Requests: []Request{{ID: [32]byte{21}, Value: 10000, Script: recipient}}, ConflictInput: point, Fees: FeePolicy{Minimum: 100, Maximum: 2000, MaximumSatPerVByte: 20}, Transaction: tx, Prevouts: []bitcoin.PreviousOutput{{Outpoint: point, Value: 100000, PublicKeyScript: script}}}, private, funding
}
func TestPlanRequiresExactPurpose(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Proposal)
	}{
		{"unknown purpose", func(p *Proposal) { p.Purpose = "heartbeat" }},
		{"no generation", func(p *Proposal) { p.Generation = 0 }},
		{"no version", func(p *Proposal) { p.StateVersion = 0 }},
		{"no network", func(p *Proposal) { p.BitcoinGenesis = [32]byte{} }},
		{"unknown scheme", func(p *Proposal) { p.Wallet.Scheme = 1 }},
		{"recipient bytes", func(p *Proposal) {
			p.Requests[0].Script = append([]byte(nil), p.Requests[0].Script...)
			p.Requests[0].Script[2] ^= 1
		}},
		{"recipient value", func(p *Proposal) { p.Requests[0].Value++ }},
		{"zero net", func(p *Proposal) { p.Requests[0].Value = 0 }},
		{"unknown request", func(p *Proposal) { p.Requests[0].ID = [32]byte{} }},
		{"P2TR recipient", func(p *Proposal) { p.Requests[0].Script = p.Prevouts[0].PublicKeyScript }},
		{"ECDSA change", func(p *Proposal) { p.Transaction.Outputs[1].PublicKeyScript = p.Requests[0].Script }},
		{"absent conflict input", func(p *Proposal) { p.ConflictInput.OutputIndex++ }},
		{"fee too low", func(p *Proposal) { p.Fees.Minimum = 1001 }},
		{"fee too high", func(p *Proposal) { p.Fees.Maximum = 999 }},
		{"fee rate", func(p *Proposal) { p.Fees.MaximumSatPerVByte = 1 }},
		{"extra output", func(p *Proposal) { p.Transaction.Outputs = append(p.Transaction.Outputs, p.Transaction.Outputs[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := planFixture(t)
			tc.change(&p)
			if _, err := NewPlan(p); err == nil {
				t.Fatal("unauthorized plan accepted")
			}
		})
	}
	t.Run("intent copies every field", func(t *testing.T) {
		p, _, _ := planFixture(t)
		plan, err := NewPlan(p)
		if err != nil {
			t.Fatal(err)
		}
		old := plan.IntentBytes()
		p.Requests[0].Script[2] ^= 1
		p.Transaction.Outputs[0].Value++
		p.Wallet.ChainId.SetInt64(9)
		if !bytes.Equal(old, plan.IntentBytes()) {
			t.Fatal("intent aliases proposal")
		}
	})
}
func TestSignatureDoneContext(t *testing.T) {
	p, private, _ := planFixture(t)
	plan, err := NewPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := DoneContext{Version: 1, Scheme: bitcoin.SignatureSchnorr, WalletID: plan.WalletID(), DescriptorHash: plan.DescriptorHash(), BitcoinGenesis: plan.Genesis(), Plan: plan.Commitment(), Reservation: [32]byte{2}, Attempt: [32]byte{3}, Message: plan.Snapshot().Digests()[0], Purpose: Redemption, StartBlock: 15}
	signature, err := schnorr.Sign(private, ctx.Message[:])
	if err != nil {
		t.Fatal(err)
	}
	done := Done{Context: ctx}
	copy(done.Signature[:], signature.Serialize())
	if _, err = done.Verify(ctx, p.Wallet.OutputKey); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*DoneContext)
	}{
		{"version", func(c *DoneContext) { c.Version++ }}, {"scheme", func(c *DoneContext) { c.Scheme = bitcoin.SignatureECDSA }},
		{"wallet", func(c *DoneContext) { c.WalletID[0] ^= 1 }}, {"descriptor", func(c *DoneContext) { c.DescriptorHash[0] ^= 1 }},
		{"network", func(c *DoneContext) { c.BitcoinGenesis[0] ^= 1 }}, {"plan", func(c *DoneContext) { c.Plan[0] ^= 1 }},
		{"reservation", func(c *DoneContext) { c.Reservation[0] ^= 1 }}, {"attempt", func(c *DoneContext) { c.Attempt[0] ^= 1 }},
		{"purpose", func(c *DoneContext) { c.Purpose = "cancellation" }}, {"input", func(c *DoneContext) { c.Input++ }},
		{"start", func(c *DoneContext) { c.StartBlock++ }}, {"digest", func(c *DoneContext) { c.Message[0] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := done
			tc.change(&bad.Context)
			if _, err := bad.Verify(ctx, p.Wallet.OutputKey); err == nil {
				t.Fatal("valid signature accepted in another context")
			}
		})
	}
	raw, _ := done.Marshal()
	var decoded Done
	if err = decoded.Unmarshal(raw); err != nil {
		t.Fatal(err)
	}
	if decoded != done {
		t.Fatal("done round trip changed")
	}
	if decoded.Unmarshal(append(raw, []byte("{}")...)) == nil {
		t.Fatal("trailing done accepted")
	}
}

type journalStub struct {
	dkg.Journal
	domain       frost.Domain
	walletRecord dkg.WalletRecord
	key          frost.KeyReady
	records      map[[32]byte][]byte
	hook         func()
}

func (j *journalStub) Domain() frost.Domain { return j.domain }
func (j *journalStub) LoadWallet(context.Context) (dkg.WalletRecord, error) {
	return j.walletRecord, nil
}
func (j *journalStub) LoadKey(context.Context) (frost.KeyReady, error) { return j.key, nil }
func (j *journalStub) DKGStatus(context.Context) (frost.DKGStatus, error) {
	return frost.DKGStatus{State: frost.DKGKeyStored, LocalSeats: []uint16{1}}, nil
}
func (j *journalStub) RetainSigningRecord(_ context.Context, id [32]byte, p []byte) error {
	if old, ok := j.records[id]; ok && !bytes.Equal(old, p) {
		return errors.New("conflict")
	}
	j.records[id] = append([]byte(nil), p...)
	return nil
}
func (j *journalStub) ReadSigningRecord(_ context.Context, id [32]byte) ([]byte, error) {
	p, ok := j.records[id]
	if !ok {
		return nil, store.ErrMissing
	}
	return append([]byte(nil), p...), nil
}

type reservationStub struct {
	view  ReservationView
	calls int
	hook  func(int)
}

func (r *reservationStub) ReadReservation(context.Context, [32]byte) (ReservationView, error) {
	r.calls++
	if r.hook != nil {
		r.hook(r.calls)
	}
	return r.view, nil
}

type bitcoinStub struct {
	raw     []byte
	genesis [32]byte
	spent   bool
	sent    []byte
}

func (b *bitcoinStub) Genesis(context.Context) ([32]byte, error)                 { return b.genesis, nil }
func (b *bitcoinStub) Transaction(context.Context, bitcoin.Hash) ([]byte, error) { return b.raw, nil }
func (b *bitcoinStub) Confirmations(context.Context, bitcoin.Hash) (uint, error) { return 6, nil }
func (b *bitcoinStub) Unspent(context.Context, bitcoin.TransactionOutpoint) (bool, error) {
	return !b.spent, nil
}
func (b *bitcoinStub) Broadcast(_ context.Context, raw []byte) error {
	b.sent = append([]byte(nil), raw...)
	return nil
}

type engineStub struct {
	frost.Engine
	executor *Executor
	private  *btcec.PrivateKey
	calls    int
	before   func()
}

func (engine *engineStub) Sign(ctx context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	engine.calls++
	if engine.before != nil {
		engine.before()
	}
	if _, ok := engine.executor.config.Journal.(*journalStub).records[engine.executor.config.Plan.hash]; !ok {
		return [64]byte{}, errors.New("intent not durable before sign")
	}
	if err := p.SigningAuthorization.BeforeSigning(ctx, r); err != nil {
		return [64]byte{}, err
	}
	s, err := schnorr.Sign(engine.private, r.Message[:])
	var raw [64]byte
	if err == nil {
		copy(raw[:], s.Serialize())
	}
	return raw, err
}

type transportStub struct{ frost.Transport }

func executorFixture(t *testing.T) (*Executor, *reservationStub, *bitcoinStub, *engineStub) {
	t.Helper()
	proposal, private, funding := planFixture(t)
	plan, err := NewPlan(proposal)
	if err != nil {
		t.Fatal(err)
	}
	domain := frost.Domain{Network: "local", Chain: [32]byte{31: 1}, Registry: [20]byte(proposal.Wallet.Registry), Epoch: proposal.Wallet.Epoch}
	key := frost.KeyReady{Candidate: frost.Candidate{Epoch: proposal.Wallet.Epoch, Descriptor: proposal.Wallet.SnowfallDescriptor, OutputKey: proposal.Wallet.OutputKey, Threshold: 2, Roster: []uint16{1, 2, 3}, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{make([]byte, 32)}}
	j := &journalStub{domain: domain, key: key, walletRecord: dkg.WalletRecord{Descriptor: proposal.Wallet, ID: plan.WalletID(), DescriptorHash: plan.DescriptorHash(), State: "ReadyUnfunded"}, records: map[[32]byte][]byte{}}
	reservation := &reservationStub{view: ReservationView{ID: [32]byte{8}, Commitment: plan.Commitment(), BlockHash: [32]byte{1}, CanonicalBlockHash: [32]byte{1}, HeadHash: [32]byte{2}, Block: 10, Head: 12, Generation: 1, StateVersion: 1, State: "Reserved"}}
	btc := &bitcoinStub{raw: funding.Serialize(), genesis: proposal.BitcoinGenesis}
	executor, err := NewLocalExecutor(Config{Plan: plan, Reservation: reservation.view.ID, Session: [32]byte{9}, StartBlock: 13, FinalityBlocks: 2, MinimumConfirmations: 1, Journal: j, Reservations: reservation, Bitcoin: btc, Worker: snowfallengine.WorkerConfig{Path: "/unused-test-worker", SHA256: [32]byte{1}}, Selected: []uint16{1, 2}, Transport: func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
		return transportStub{}, func() {}, nil
	}, LocalRegtest: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := &engineStub{executor: executor, private: private}
	executor.engine = engine
	return executor, reservation, btc, engine
}
func TestAuthorizationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Executor, *reservationStub, *bitcoinStub)
	}{
		{"missing", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.ID = [32]byte{} }},
		{"not final", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.Head-- }},
		{"reorg", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.CanonicalBlockHash[0] ^= 1 }},
		{"foreign plan", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.Commitment[0] ^= 1 }},
		{"generation", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.Generation++ }},
		{"state version", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.StateVersion++ }},
		{"timeout", func(_ *Executor, r *reservationStub, _ *bitcoinStub) { r.view.State = "TimeoutPending" }},
		{"wrong network", func(_ *Executor, _ *reservationStub, b *bitcoinStub) { b.genesis[0] ^= 1 }},
		{"spent", func(_ *Executor, _ *reservationStub, b *bitcoinStub) { b.spent = true }},
		{"false prevout", func(_ *Executor, _ *reservationStub, b *bitcoinStub) { b.raw[len(b.raw)-1] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, r, b, engine := executorFixture(t)
			tc.change(e, r, b)
			if _, err := e.Run(context.Background()); !errors.Is(err, ErrUnauthorized) {
				t.Fatal("invalid state accepted", err)
			}
			if engine.calls != 0 {
				t.Fatal("worker reached before authorization")
			}
		})
	}
	t.Run("last check after claim", func(t *testing.T) {
		e, r, _, engine := executorFixture(t)
		engine.before = func() { r.view.State = "TimeoutPending" }
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
	})
	t.Run("receipt changes", func(t *testing.T) {
		e, r, _, _ := executorFixture(t)
		r.hook = func(n int) {
			if n == 2 {
				r.view.BlockHash[0]++
				r.view.CanonicalBlockHash = r.view.BlockHash
			}
		}
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
	})
	t.Run("late signature retained", func(t *testing.T) {
		e, r, _, _ := executorFixture(t)
		e.boundary = func(name string) error {
			if name == "after-signature" {
				r.view.State = "TimeoutPending"
			}
			return nil
		}
		if _, err := e.Run(context.Background()); !errors.Is(err, ErrLateSignature) {
			t.Fatal(err)
		}
		attempt, _ := e.attempt(0)
		id := snowfallengine.AttemptID(attempt)
		id = sha256.Sum256(append([]byte("keep-core/frost/signing-done/v1/"), id[:]...))
		if _, err := e.config.Journal.ReadSigningRecord(context.Background(), id); err != nil {
			t.Fatal("lost escaped signature record")
		}
	})
	t.Run("success recovery mutation and terminal executor", func(t *testing.T) {
		e, r, b, engine := executorFixture(t)
		result, err := e.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := e.LoadResult(context.Background())
		if err != nil || !bytes.Equal(loaded.Transaction.Serialize(), result.Transaction.Serialize()) {
			t.Fatal("result recovery failed", err)
		}
		if _, err = e.Run(context.Background()); err == nil || engine.calls != 1 {
			t.Fatal("executor reused")
		}
		result.Transaction.Outputs[0].Value++
		if e.Broadcast(context.Background(), result) == nil || len(b.sent) != 0 {
			t.Fatal("mutated transaction broadcast")
		}
		result.Transaction.Outputs[0].Value--
		r.hook = func(int) { result.Transaction.Outputs[0].Value++ }
		if err = e.Broadcast(context.Background(), result); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b.sent, loaded.Transaction.Serialize()) {
			t.Fatal("provider callback changed broadcast bytes")
		}
	})
}

func TestReservationReceiptSurvivesExecutorRecovery(t *testing.T) {
	e, r, b, _ := executorFixture(t)
	ctx := context.Background()
	result, err := e.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewLocalExecutor(e.config)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := fresh.LoadResult(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.Broadcast(ctx, recovered); err != nil {
		t.Fatal("same durable anchor rejected", err)
	}
	b.sent = nil
	for _, change := range []struct {
		name string
		f    func()
	}{
		{"new block", func() { r.view.Block++; r.view.Head++ }},
		{"new block hash", func() { r.view.BlockHash[0]++; r.view.CanonicalBlockHash = r.view.BlockHash }},
	} {
		t.Run(change.name, func(t *testing.T) {
			old := r.view
			defer func() { r.view = old }()
			change.f()
			f, err := NewLocalExecutor(e.config)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Broadcast(ctx, result); !errors.Is(err, ErrUnauthorized) || len(b.sent) != 0 {
				t.Fatal("recovered executor accepted re-anchored reservation", err)
			}
		})
	}
	bad := *result
	bad.Receipt.Hash[0]++
	if err = fresh.Broadcast(ctx, &bad); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("changed result receipt accepted", err)
	}
	journal := e.config.Journal.(*journalStub)
	delete(journal.records, e.receiptID())
	fresh, err = NewLocalExecutor(e.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.LoadResult(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("missing anchor accepted on recovery", err)
	}
	if err = fresh.Broadcast(ctx, result); !errors.Is(err, ErrUnauthorized) || len(b.sent) != 0 {
		t.Fatal("missing anchor recreated during broadcast", err)
	}
}
