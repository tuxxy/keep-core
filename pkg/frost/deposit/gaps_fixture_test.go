package deposit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// Fixtures for the M5 mutation-gap tests. Everything in this file is a test
// fixture: an in-memory reader, journal or Bitcoin view that wraps the fakes
// of fake_test.go and fails or answers on request. The code under test
// (View, Planner, Authority, MainLock, Guard, ResolutionAttester, Sweeper,
// Refund, HeaderClock) is the real code.

// errInjected is the failure every fixture returns when asked to fail: a
// provider or storage outage, not a refusal.
var errInjected = errors.New("injected provider failure (fixture)")

// faultChain is the fake C-05/C-04 reader with injected read failures. A
// method named in fail returns errInjected. Reveals are returned in block,
// then key order, so loops over them are deterministic.
type faultChain struct {
	*fakeChain
	mu   sync.Mutex
	fail map[string]bool
	// hashFail fails HeaderHash for the blocks it selects.
	hashFail func(n uint64) bool
	// hashAt, when set, answers HeaderHash instead of the fake.
	hashAt func(n uint64) [32]byte
	// sweptFrom makes a record show SweptAt only at blocks at or above the
	// given block (the fake ignores the block number otherwise).
	sweptFrom map[[32]byte]uint64
	// reservedErrAt fails Reserved at exactly this block when nonzero.
	reservedErrAt uint64
	seats         []uint16
}

func newFaultChain(c *fakeChain) *faultChain {
	return &faultChain{fakeChain: c, fail: map[string]bool{}, seats: []uint16{1, 2, 3}}
}

func (c *faultChain) failing(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fail[name]
}

func (c *faultChain) set(name string, on bool) {
	c.mu.Lock()
	c.fail[name] = on
	c.mu.Unlock()
}

func (c *faultChain) Head(ctx context.Context) (redemption.Head, error) {
	if c.failing("Head") {
		return redemption.Head{}, errInjected
	}
	h, err := c.fakeChain.Head(ctx)
	if err == nil && c.hashAt != nil {
		h.Hash = c.hashAt(h.Number)
	}
	return h, err
}

func (c *faultChain) HeaderHash(ctx context.Context, n uint64) ([32]byte, error) {
	if c.failing("HeaderHash") || (c.hashFail != nil && c.hashFail(n)) {
		return [32]byte{}, errInjected
	}
	if c.hashAt != nil {
		return c.hashAt(n), nil
	}
	return c.fakeChain.HeaderHash(ctx, n)
}

func (c *faultChain) Timestamp(ctx context.Context, n uint64) (uint64, error) {
	if c.failing("Timestamp") {
		return 0, errInjected
	}
	return c.fakeChain.Timestamp(ctx, n)
}

func (c *faultChain) Configuration(ctx context.Context, at uint64) (redemption.Configuration, error) {
	if c.failing("Configuration") {
		return redemption.Configuration{}, errInjected
	}
	return c.fakeChain.Configuration(ctx, at)
}

func (c *faultChain) Custody(ctx context.Context, at uint64, wallet [32]byte) (redemption.Custody, error) {
	if c.failing("Custody") {
		return redemption.Custody{}, errInjected
	}
	return c.fakeChain.Custody(ctx, at, wallet)
}

func (c *faultChain) CustodyOutput(ctx context.Context, at uint64, key [32]byte) (redemption.CustodyOutput, error) {
	if c.failing("CustodyOutput") {
		return redemption.CustodyOutput{}, errInjected
	}
	return c.fakeChain.CustodyOutput(ctx, at, key)
}

func (c *faultChain) Intent(ctx context.Context, at uint64, id [32]byte) (redemption.Intent, error) {
	if c.failing("Intent") {
		return redemption.Intent{}, errInjected
	}
	return c.fakeChain.Intent(ctx, at, id)
}

func (c *faultChain) Deposit(ctx context.Context, at uint64, key [32]byte) (Record, error) {
	if c.failing("Deposit") {
		return Record{}, errInjected
	}
	r, err := c.fakeChain.Deposit(ctx, at, key)
	if from, ok := c.sweptFrom[key]; ok && err == nil {
		r.SweptAt = 0
		if at >= from {
			r.SweptAt = from
		}
	}
	return r, err
}

func (c *faultChain) Reveals(ctx context.Context, wallet [32]byte, from, to uint64) ([]Reveal, error) {
	if c.failing("Reveals") {
		return nil, errInjected
	}
	out, err := c.fakeChain.Reveals(ctx, wallet, from, to)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Block != out[j].Block {
			return out[i].Block < out[j].Block
		}
		return string(out[i].Outpoint[:]) < string(out[j].Outpoint[:])
	})
	return out, err
}

func (c *faultChain) Reveal(ctx context.Context, key [32]byte) (Reveal, error) {
	if c.failing("Reveal") {
		return Reveal{}, errInjected
	}
	return c.fakeChain.Reveal(ctx, key)
}

func (c *faultChain) Reserved(ctx context.Context, at uint64, key [32]byte) (bool, error) {
	if c.failing("Reserved") || (c.reservedErrAt != 0 && at == c.reservedErrAt) {
		return false, errInjected
	}
	return c.fakeChain.Reserved(ctx, at, key)
}

func (c *faultChain) CertifiedSeats(context.Context, [32]byte) ([]uint16, error) {
	if c.failing("CertifiedSeats") {
		return nil, errInjected
	}
	return append([]uint16(nil), c.seats...), nil
}

// faultUnspent is the fake Bitcoin view with injected failures.
type faultUnspent struct {
	*scriptUnspent
	failUnspent, failByScript bool
}

func (s *faultUnspent) Unspent(ctx context.Context, o bitcoin.TransactionOutpoint) (bool, error) {
	if s.failUnspent {
		return false, errInjected
	}
	return s.scriptUnspent.Unspent(ctx, o)
}

func (s *faultUnspent) UnspentByScript(ctx context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	if s.failByScript {
		return nil, errInjected
	}
	return s.scriptUnspent.UnspentByScript(ctx, script)
}

// faultJournal is the compare-and-insert journal with injected failures.
// failReadsFrom > 0 fails every read from that read (1-based) on; failReadAt
// > 0 fails that one read only; failWrites fails every write.
type faultJournal struct {
	*memoryJournal
	mu            sync.Mutex
	reads         int
	failReadsFrom int
	failReadAt    int
	failWrites    bool
}

func newFaultJournal() *faultJournal { return &faultJournal{memoryJournal: newJournal()} }

func (j *faultJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	j.mu.Lock()
	j.reads++
	fail := (j.failReadsFrom > 0 && j.reads >= j.failReadsFrom) || (j.failReadAt > 0 && j.reads == j.failReadAt)
	j.mu.Unlock()
	if fail {
		return nil, errInjected
	}
	return j.memoryJournal.ReadSigningRecord(ctx, id)
}

func (j *faultJournal) RetainSigningRecord(ctx context.Context, id [32]byte, raw []byte) error {
	j.mu.Lock()
	fail := j.failWrites
	j.mu.Unlock()
	if fail {
		return errInjected
	}
	return j.memoryJournal.RetainSigningRecord(ctx, id, raw)
}

// failOnlyRead makes the n-th read from now fail, and no other.
func (j *faultJournal) failOnlyRead(n int) {
	j.mu.Lock()
	j.failReadAt = j.reads + n
	j.mu.Unlock()
}

// failReadsAfter makes every read after the next n reads fail.
func (j *faultJournal) failReadsAfter(n int) {
	j.mu.Lock()
	j.failReadsFrom = j.reads + n + 1
	j.mu.Unlock()
}

// errNoWallet is the fixture host journal's LoadWallet failure: the executor
// stops at its first durable read, before any grant or worker.
var errNoWallet = errors.New("fixture host journal holds no wallet record")

// hostJournal is a fixture signing.Journal for the sweeper: a valid domain, a
// DKG status naming local seats, signing records in memory, and no wallet
// record, so every executor Run fails at its first wallet read. Methods the
// sweeper does not reach are left to the embedded nil interface.
type hostJournal struct {
	signing.Journal
	*memoryJournal
	seats      []uint16
	failStatus bool
}

func newHostJournal() *hostJournal {
	return &hostJournal{memoryJournal: newJournal(), seats: []uint16{1}}
}

func (j *hostJournal) Domain() frost.Domain {
	return frost.Domain{Network: "regtest-fixture", Chain: [32]byte{1}, Registry: [20]byte{1}, Epoch: 17}
}

func (j *hostJournal) DKGStatus(context.Context) (frost.DKGStatus, error) {
	if j.failStatus {
		return frost.DKGStatus{}, errInjected
	}
	return frost.DKGStatus{Domain: j.Domain(), State: frost.DKGKeyStored, LocalSeats: append([]uint16(nil), j.seats...)}, nil
}

func (j *hostJournal) LoadWallet(context.Context) (dkg.WalletRecord, error) {
	return dkg.WalletRecord{}, errNoWallet
}

func (j *hostJournal) RetainSigningRecord(ctx context.Context, id [32]byte, raw []byte) error {
	return j.memoryJournal.RetainSigningRecord(ctx, id, raw)
}

func (j *hostJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	return j.memoryJournal.ReadSigningRecord(ctx, id)
}

// noBitcoin is a fixture signing.Bitcoin that is never consulted: the
// executor stops at the wallet read first.
type noBitcoin struct{}

func (noBitcoin) Genesis(context.Context) ([32]byte, error) { return [32]byte{}, errInjected }
func (noBitcoin) Transaction(context.Context, bitcoin.Hash) ([]byte, error) {
	return nil, errInjected
}
func (noBitcoin) Confirmations(context.Context, bitcoin.Hash) (uint, error) { return 0, errInjected }
func (noBitcoin) Unspent(context.Context, bitcoin.TransactionOutpoint) (bool, error) {
	return false, errInjected
}
func (noBitcoin) Broadcast(context.Context, []byte) error { return errInjected }

// sweepHost is one operator's sweeper over a fault chain, with a complete
// local executor configuration whose worker never starts (the fixture
// journal holds no wallet). The worker path is never executed.
type sweepHost struct {
	*world
	fc      *faultChain
	host    *hostJournal
	ledgerJ *faultJournal
	sweeper *Sweeper
}

func newSweepHost(t *testing.T) *sweepHost {
	t.Helper()
	w := newWorld(t)
	fc := newFaultChain(w.chain)
	w.view.Chain = fc
	w.planner.Chain = fc
	h := &sweepHost{world: w, fc: fc, host: newHostJournal(), ledgerJ: newFaultJournal()}
	h.sweeper = &Sweeper{
		Chain:     fc,
		Planner:   w.planner,
		Authority: &Authority{View: w.view},
		Lock:      w.lock,
		Ledger:    redemption.Ledger{Journal: h.ledgerJ},
		Schedule:  redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10},
		Base: signing.Config{
			MinimumConfirmations: 1,
			Journal:              h.host,
			Bitcoin:              noBitcoin{},
			Worker:               snowfallWorkerFixture(),
			Transport: func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
				return nil, nil, errInjected
			},
			LocalRegtest: true,
		},
	}
	return h
}

// entries returns the ledger records of plan's sweep (reads bypass faults).
func (h *sweepHost) entries(t *testing.T, plan *signing.Plan) []redemption.Entry {
	t.Helper()
	out, err := redemption.Ledger{Journal: h.ledgerJ.memoryJournal}.Entries(ctx, SweepID(plan))
	if err != nil {
		t.Fatal("fixture ledger read:", err)
	}
	return out
}

func kinds(entries []redemption.Entry) map[redemption.Kind]int {
	out := map[redemption.Kind]int{}
	for _, e := range entries {
		out[e.Kind]++
	}
	return out
}

// recovered runs f and turns a panic into an error, so a crash becomes a
// named assertion failure in the caller instead of aborting the binary.
func recovered(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return f()
}

// snowfallWorkerFixture names a worker binary that does not exist. The
// executor accepts it at construction and never runs it here.
func snowfallWorkerFixture() snowfallengine.WorkerConfig {
	return snowfallengine.WorkerConfig{Path: "/nonexistent/snowfall-worker-fixture", SHA256: [32]byte{1}}
}

// Plans of other purposes over the world's wallet, built with the public
// signing constructors. They exist only to drive the deposit package's
// purpose dispatch; nothing signs them.

// movementPlanOn is a K-07 movement of main to one committed target.
func movementPlanOn(t *testing.T, w *world, main bitcoin.PreviousOutput) *signing.Plan {
	t.Helper()
	target := xonly(testKey(33))
	script, _ := bitcoin.PayToTaproot(target)
	o := main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &o, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: main.Value - 1_000, PublicKeyScript: script}}}
	plan, err := signing.NewMovementPlan(signing.MovementProposal{Wallet: w.wallet, BitcoinGenesis: w.planner.Genesis, Generation: 1, StateVersion: 1, Period: 1, Main: main, Targets: []signing.MovementTarget{{Label: [20]byte{1}, DescriptorHash: [32]byte{2}, OutputKey: target}}, MaximumFee: 10_000, MaximumSatPerVByte: 100, Transaction: tx})
	if err != nil {
		t.Fatal("fixture movement plan:", err)
	}
	return plan
}

// incomingSweepPlanOn is a K-07 incoming sweep of one moved output, with the
// wallet's main first when main is not nil.
func incomingSweepPlanOn(t *testing.T, w *world, main *bitcoin.PreviousOutput) *signing.Plan {
	t.Helper()
	script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	incoming := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xcc}, OutputIndex: 2}, Value: 50_000, PublicKeyScript: script}
	tx := &bitcoin.Transaction{Version: 2}
	total := incoming.Value
	if main != nil {
		o := main.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
		total += main.Value
	}
	in := incoming.Outpoint
	tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &in, Sequence: 0xffffffff})
	tx.Outputs = []*bitcoin.TransactionOutput{{Value: total - 1_000, PublicKeyScript: script}}
	plan, err := signing.NewIncomingSweepPlan(signing.IncomingSweepProposal{Wallet: w.wallet, BitcoinGenesis: w.planner.Genesis, Generation: 1, StateVersion: 1, Main: main, Incoming: []bitcoin.PreviousOutput{incoming}, MaximumFee: 10_000, MaximumSatPerVByte: 100, Transaction: tx})
	if err != nil {
		t.Fatal("fixture incoming sweep plan:", err)
	}
	return plan
}

// paymentPlanOn is a K-05 redemption payment spending main, its reserved
// conflict input.
func paymentPlanOn(t *testing.T, w *world, main bitcoin.PreviousOutput) *signing.Plan {
	t.Helper()
	change, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	recipient := append(bitcoin.Script{0x00, 0x14}, make([]byte, 20)...)
	o := main.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &o, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100_000, PublicKeyScript: recipient}, {Value: main.Value - 101_000, PublicKeyScript: change}}}
	plan, err := signing.NewPlan(signing.Proposal{Wallet: w.wallet, BitcoinGenesis: w.planner.Genesis, Purpose: signing.Redemption, Generation: 1, StateVersion: 1, Requests: []signing.Request{{ID: [32]byte{1}, Value: 100_000, Script: recipient}}, ConflictInput: main.Outpoint, Fees: signing.FeePolicy{Minimum: 1, Maximum: 10_000, MaximumSatPerVByte: 100}, Transaction: tx, Prevouts: []bitcoin.PreviousOutput{main}})
	if err != nil {
		t.Fatal("fixture payment plan:", err)
	}
	return plan
}
