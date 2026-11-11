package movement

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Fixtures for the M6 mutation-gap tests (mutation batch M6-movement).
// Everything in this file is a test fixture: an in-memory C-06/C-09 reader
// that wraps the fake of movement_test.go and fails or answers on request,
// in-memory journals with injected failures, and a fixture signing host whose
// worker never starts. The code under test (View, Planner, Commitments,
// Authority, Closure, Mover) is the real code.

// errInjected is the failure every fixture returns when asked to fail: a
// provider or storage outage, not a refusal.
var errInjected = errors.New("injected provider failure (fixture)")

// faultChain is the fake chain with injected failures and block-aware
// answers. failWhen(method, at) decides whether a read fails; at is the block
// the method reads at (HeaderHash: the block number; reads without a block:
// 0). Wallet reads also ask for "Wallet/<label hex>". It records the block of every Obligations read.
type faultChain struct {
	*fake
	mu       sync.Mutex
	failWhen func(method string, at uint64) bool
	// headHash, when set, is the hash Head reports, so HeaderHash(head)
	// differs from it (a reorg between the two reads).
	headHash *[32]byte
	// proven maps a Bitcoin transaction hash to the wallet that proved it and
	// the block of the proof; ProvenTransaction answers only at or after it.
	proven map[[32]byte]provenAt
	// obligationsAt records the block of every Obligations read.
	obligationsAt []uint64
	// submitErr fails SubmitCommitment; submitRecords decides whether the
	// commitment was recorded anyway (a lost reply) and with which targets.
	submitErr     error
	submitRecords func(w [32]byte, targets [][20]byte) [][20]byte
	// finalizeErr fails FinalizeClosing; finalizeApplies closes the wallet
	// anyway (a lost reply).
	finalizeErr     error
	finalizeApplies bool
	finalizes       atomic.Int32
	// obligationReads counts Obligations calls.
	obligationReads atomic.Int32
}

type provenAt struct {
	wallet [32]byte
	block  uint64
}

func newFaultChain(f *fake) *faultChain {
	return &faultChain{fake: f, proven: map[[32]byte]provenAt{}}
}

func (c *faultChain) failing(method string, at uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failWhen != nil && c.failWhen(method, at)
}

// failOn makes every call of the named methods fail.
func (c *faultChain) failOn(methods ...string) {
	set := map[string]bool{}
	for _, m := range methods {
		set[m] = true
	}
	c.mu.Lock()
	c.failWhen = func(method string, _ uint64) bool { return set[method] }
	c.mu.Unlock()
}

func (c *faultChain) ChainID() uint64  { return 1 }
func (c *faultChain) Bridge() [20]byte { return [20]byte{0x06} }

func (c *faultChain) Head(ctx context.Context) (redemption.Head, error) {
	if c.failing("Head", 0) {
		return redemption.Head{}, errInjected
	}
	h, err := c.fake.Head(ctx)
	if c.headHash != nil {
		h.Hash = *c.headHash
	}
	return h, err
}
func (c *faultChain) HeaderHash(ctx context.Context, n uint64) ([32]byte, error) {
	if c.failing("HeaderHash", n) {
		return [32]byte{}, errInjected
	}
	return c.fake.HeaderHash(ctx, n)
}
func (c *faultChain) Wallet(ctx context.Context, at uint64, l [20]byte) (Wallet, error) {
	if c.failing("Wallet", at) || c.failing(fmt.Sprintf("Wallet/%x", l), at) {
		return Wallet{}, errInjected
	}
	return c.fake.Wallet(ctx, at, l)
}
func (c *faultChain) ReadyWallets(ctx context.Context, from, to uint64) ([][32]byte, error) {
	if c.failing("ReadyWallets", to) {
		return nil, errInjected
	}
	return c.fake.ReadyWallets(ctx, from, to)
}
func (c *faultChain) Lifecycle(ctx context.Context, at uint64, w [32]byte) (Lifecycle, error) {
	if c.failing("Lifecycle", at) {
		return Lifecycle{}, errInjected
	}
	return c.fake.Lifecycle(ctx, at, w)
}
func (c *faultChain) Commitment(ctx context.Context, at uint64, w [32]byte, p uint32) (Commitment, error) {
	if c.failing("Commitment", at) {
		return Commitment{}, errInjected
	}
	return c.fake.Commitment(ctx, at, w, p)
}
func (c *faultChain) CommitmentLog(ctx context.Context, w [32]byte, p uint32) (CommitmentLog, error) {
	if c.failing("CommitmentLog", 0) {
		return CommitmentLog{}, errInjected
	}
	return c.fake.CommitmentLog(ctx, w, p)
}
func (c *faultChain) Custody(ctx context.Context, at uint64, w [32]byte) (redemption.Custody, error) {
	if c.failing("Custody", at) {
		return redemption.Custody{}, errInjected
	}
	return c.fake.Custody(ctx, at, w)
}
func (c *faultChain) CustodyOutput(ctx context.Context, at uint64, k [32]byte) (redemption.CustodyOutput, error) {
	if c.failing("CustodyOutput", at) {
		return redemption.CustodyOutput{}, errInjected
	}
	return c.fake.CustodyOutput(ctx, at, k)
}
func (c *faultChain) MovementParameters(ctx context.Context, at uint64) (Parameters, error) {
	if c.failing("MovementParameters", at) {
		return Parameters{}, errInjected
	}
	return c.fake.MovementParameters(ctx, at)
}
func (c *faultChain) Counters(ctx context.Context, at uint64, w [32]byte) (Counters, error) {
	if c.failing("Counters", at) {
		return Counters{}, errInjected
	}
	return c.fake.Counters(ctx, at, w)
}
func (c *faultChain) IncomingLogs(ctx context.Context, w [32]byte, from, to uint64) ([]IncomingLog, error) {
	if c.failing("IncomingLogs", to) {
		return nil, errInjected
	}
	return c.fake.IncomingLogs(ctx, w, from, to)
}
func (c *faultChain) Incoming(ctx context.Context, at uint64, k [32]byte) (Incoming, error) {
	if c.failing("Incoming", at) {
		return Incoming{}, errInjected
	}
	return c.fake.Incoming(ctx, at, k)
}
func (c *faultChain) Obligations(ctx context.Context, at uint64, w [32]byte) (uint64, error) {
	c.mu.Lock()
	c.obligationsAt = append(c.obligationsAt, at)
	c.mu.Unlock()
	defer c.obligationReads.Add(1)
	if c.failing("Obligations", at) {
		return 0, errInjected
	}
	return c.fake.Obligations(ctx, at, w)
}
func (c *faultChain) ProvenTransaction(_ context.Context, at uint64, h [32]byte) ([32]byte, error) {
	if c.failing("ProvenTransaction", at) {
		return [32]byte{}, errInjected
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.proven[h]; ok && at >= p.block {
		return p.wallet, nil
	}
	return [32]byte{}, nil
}
func (c *faultChain) SubmitCommitment(ctx context.Context, w [32]byte, seat uint16, targets [][20]byte) error {
	if c.submitErr != nil {
		if c.submitRecords != nil {
			if recorded := c.submitRecords(w, targets); recorded != nil {
				_ = c.fake.SubmitCommitment(ctx, w, seat, recorded)
			}
		}
		return c.submitErr
	}
	return c.fake.SubmitCommitment(ctx, w, seat, targets)
}
func (c *faultChain) FinalizeClosing(ctx context.Context, w [32]byte) error {
	c.finalizes.Add(1)
	if c.finalizeErr != nil {
		if c.finalizeApplies {
			_ = c.fake.FinalizeClosing(ctx, w)
		}
		return c.finalizeErr
	}
	return c.fake.FinalizeClosing(ctx, w)
}

// faultJournal is an in-memory compare-and-insert record journal with
// injected failures: failReadAt fails that one read (1-based, counted from
// the last arm call) and no other; failWrites fails every write. fired
// reports whether the armed read was reached.
type faultJournal struct {
	mu         sync.Mutex
	records    map[[32]byte][]byte
	reads      int
	failReadAt int
	fired      bool
	failReads  bool
	failWrites bool
	writes     int
}

func newFaultJournal() *faultJournal { return &faultJournal{records: map[[32]byte][]byte{}} }

func (j *faultJournal) ReadSigningRecord(_ context.Context, id [32]byte) ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.reads++
	if j.failReads || (j.failReadAt > 0 && j.reads == j.failReadAt) {
		j.fired = true
		return nil, errInjected
	}
	raw, ok := j.records[id]
	if !ok {
		return nil, store.ErrMissing
	}
	return append([]byte(nil), raw...), nil
}

func (j *faultJournal) RetainSigningRecord(_ context.Context, id [32]byte, raw []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failWrites {
		return errInjected
	}
	if existing, ok := j.records[id]; ok {
		if string(existing) != string(raw) {
			return errors.New("record conflict (fixture)")
		}
		return nil
	}
	j.records[id] = append([]byte(nil), raw...)
	j.writes++
	return nil
}

// armRead makes the n-th read from now fail, and no other.
func (j *faultJournal) armRead(n int) {
	j.mu.Lock()
	j.reads, j.failReadAt, j.fired = 0, n, false
	j.mu.Unlock()
}

func (j *faultJournal) wasFired() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.fired
}

// errNoWallet is the fixture host journal's LoadWallet failure: an executor
// Run stops at its first durable read, before any grant or worker.
var errNoWallet = errors.New("fixture host journal holds no wallet record")

// hostJournal is a fixture signing.Journal: a valid domain, a DKG status
// naming the local seats, signing records in memory, and no wallet record,
// so every executor Run fails at its first wallet read. loads counts those
// reads: one per executor Run. Methods the mover does not reach are left to
// the embedded nil interface.
type hostJournal struct {
	signing.Journal
	*faultJournal
	mu         sync.Mutex
	seats      []uint16
	failStatus bool
	loads      int
}

func newHostJournal(seats ...uint16) *hostJournal {
	return &hostJournal{faultJournal: newFaultJournal(), seats: seats}
}

func (j *hostJournal) Domain() frost.Domain {
	return frost.Domain{Network: "regtest-fixture", Chain: [32]byte{1}, Registry: [20]byte{1}, Epoch: 17}
}

func (j *hostJournal) DKGStatus(context.Context) (frost.DKGStatus, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failStatus {
		return frost.DKGStatus{}, errInjected
	}
	return frost.DKGStatus{Domain: j.Domain(), State: frost.DKGKeyStored, LocalSeats: append([]uint16(nil), j.seats...)}, nil
}

func (j *hostJournal) LoadWallet(context.Context) (dkg.WalletRecord, error) {
	j.mu.Lock()
	j.loads++
	j.mu.Unlock()
	return dkg.WalletRecord{}, errNoWallet
}

func (j *hostJournal) runs() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.loads
}

func (j *hostJournal) RetainSigningRecord(ctx context.Context, id [32]byte, raw []byte) error {
	return j.faultJournal.RetainSigningRecord(ctx, id, raw)
}

func (j *hostJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	return j.faultJournal.ReadSigningRecord(ctx, id)
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

// snowfallWorkerFixture names a worker binary that does not exist. The
// executor accepts it at construction and never runs it here.
func snowfallWorkerFixture() snowfallengine.WorkerConfig {
	return snowfallengine.WorkerConfig{Path: "/nonexistent/snowfall-worker-fixture", SHA256: [32]byte{1}}
}

// errPanic marks a panic that recovered turned into an error.
var errPanic = errors.New("panic (recovered by the fixture)")

// recovered runs f and turns a panic into an error, so a crash becomes a
// named assertion failure in the caller instead of aborting the binary.
func recovered(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errPanic, r)
		}
	}()
	return f()
}

// gapWorld is newWorld over a fault chain, with every view and planner
// reading through it.
type gapWorld struct {
	*world
	fc *faultChain
}

func newGapWorld(t *testing.T) *gapWorld {
	t.Helper()
	w := newWorld(t)
	fc := newFaultChain(w.f)
	w.view = &View{Chain: fc, Policy: Policy{FinalityBlocks: 2}}
	return &gapWorld{world: w, fc: fc}
}

// commitments is the C-06 commitment submitter over the fault chain.
func (g *gapWorld) commitments() Commitments {
	return Commitments{Chain: g.fc, Planner: g.planner()}
}

// committedPlan records the source's commitment with its descriptor hashes,
// lets it finalize, and returns the movement plan.
func (g *gapWorld) committedPlan(t *testing.T) *signing.Plan {
	t.Helper()
	ctx := context.Background()
	targets, err := g.commitments().Commit(ctx)
	if err != nil {
		t.Fatal("fixture commitment:", err)
	}
	c := g.f.commitments[g.source.ID]
	c.DescriptorHashes = nil
	for _, label := range targets {
		c.DescriptorHashes = append(c.DescriptorHashes, g.f.wallets[label].DescriptorHash)
	}
	g.f.commitments[g.source.ID] = c
	g.f.head += 2
	plan, err := g.planner().Movement(ctx)
	if err != nil {
		t.Fatal("fixture movement plan:", err)
	}
	return plan
}

// incomingKey is the outpoint key of the fixture movement output tag.
func incomingOutpoint(tag byte, index uint32) bitcoin.TransactionOutpoint {
	return bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{tag, 0x16}, OutputIndex: index}
}

// addIncoming records a pending incoming output to target at block, with
// value and the live sweep cap, and returns its key.
func (g *gapWorld) addIncoming(target Wallet, tag byte, value uint64, block uint64) [32]byte {
	o := incomingOutpoint(tag, 0)
	key := OutpointKey(o)
	g.f.incoming[key] = Incoming{Target: target.ID, Source: g.source.ID, Value: value, State: IncomingPending, SweepFeeCap: g.f.params.MovedFundsSweepTxMaxTotalFee}
	g.f.incomingLog = append(g.f.incomingLog, IncomingLog{Log: Log{Block: block, BlockHash: g.f.hash(block)}, Outpoint: key, Target: target.ID, Source: g.source.ID, Value: value, TransactionHash: [32]byte(o.TransactionHash), OutputIndex: o.OutputIndex})
	return key
}

// targetPlanner plans for target; with a main when main is not nil.
func (g *gapWorld) targetPlanner(target Wallet, main *bitcoin.PreviousOutput) Planner {
	p := g.planner()
	p.Wallet = target.Descriptor
	p.Bitcoin = unspent{}
	if main != nil {
		p.Bitcoin = unspent{[]bitcoin.PreviousOutput{*main}}
	}
	return p
}

// setTargetMain gives target a recorded custody main of value.
func (g *gapWorld) setTargetMain(target Wallet, value int64) bitcoin.PreviousOutput {
	script, _ := bitcoin.PayToTaproot(target.Descriptor.OutputKey)
	main := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x4d, 0x16}, OutputIndex: 3}, Value: value, PublicKeyScript: script}
	key := OutpointKey(main.Outpoint)
	g.f.custody[target.ID] = redemption.Custody{StateVersion: 1, Value: uint64(value), MainOutpoint: key}
	g.f.outputs[key] = redemption.CustodyOutput{WalletID: target.ID, Value: uint64(value)}
	return main
}

// incomingPlan is target's sweep of one pending incoming output at block 90,
// with a main when withMain.
func (g *gapWorld) incomingPlan(t *testing.T, withMain bool) (*signing.Plan, Wallet, [32]byte) {
	t.Helper()
	target := g.targets[0]
	var main *bitcoin.PreviousOutput
	if withMain {
		m := g.setTargetMain(target, 50_000)
		main = &m
	}
	key := g.addIncoming(target, 0x55, 99_000, 90)
	plan, err := g.targetPlanner(target, main).IncomingSweep(context.Background())
	if err != nil {
		t.Fatal("fixture incoming sweep plan:", err)
	}
	return plan, target, key
}

// moverHost is one operator's mover over the fault chain, with a complete
// local executor configuration whose worker never starts (the fixture host
// journal holds no wallet): every Sign that reaches the executor records its
// attempt and then fails at the wallet read.
type moverHost struct {
	*gapWorld
	host    *hostJournal
	ledgerJ *faultJournal
	lockJ   *faultJournal
	lock    *deposit.MainLock
	mover   *Mover
}

func newMoverHost(t *testing.T, g *gapWorld, wallet dkg.Descriptor) *moverHost {
	t.Helper()
	h := &moverHost{gapWorld: g, host: newHostJournal(1, 2, 3), ledgerJ: newFaultJournal(), lockJ: newFaultJournal()}
	h.lock = &deposit.MainLock{Journal: h.lockJ}
	h.mover = &Mover{
		Chain:     g.fc,
		Authority: &Authority{View: g.view},
		Lock:      h.lock,
		Ledger:    redemption.Ledger{Journal: h.ledgerJ},
		Schedule:  redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 4},
		Wallet:    wallet,
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

// entries returns the ledger records of plan (reads bypass faults).
func (h *moverHost) entries(t *testing.T, plan *signing.Plan) []redemption.Entry {
	t.Helper()
	clean := newFaultJournal()
	h.ledgerJ.mu.Lock()
	for id, raw := range h.ledgerJ.records {
		clean.records[id] = raw
	}
	h.ledgerJ.mu.Unlock()
	out, err := redemption.Ledger{Journal: clean}.Entries(context.Background(), ID(plan))
	if err != nil {
		t.Fatal("fixture ledger read:", err)
	}
	return out
}

// attempts returns the rounds of plan's attempt records, in ledger order.
func (h *moverHost) attempts(t *testing.T, plan *signing.Plan) []string {
	t.Helper()
	var out []string
	for _, e := range h.entries(t, plan) {
		if e.Kind == redemption.KindMovementAttempt {
			out = append(out, e.Detail)
		}
	}
	return out
}

func (h *moverHost) has(t *testing.T, plan *signing.Plan, kind redemption.Kind) bool {
	t.Helper()
	for _, e := range h.entries(t, plan) {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// plant appends a ledger record for plan as the mover writes it (through the
// real Ledger), standing in for an earlier step of this host.
func (h *moverHost) plant(t *testing.T, plan *signing.Plan, kind redemption.Kind, detail string) {
	t.Helper()
	if err := (redemption.Ledger{Journal: h.ledgerJ}).Append(context.Background(), redemption.Entry{Reservation: ID(plan), Kind: kind, Block: h.f.head, Transaction: plan.BitcoinTransactionHash(), Detail: detail}); err != nil {
		t.Fatal("fixture ledger append:", err)
	}
}

// locked reports the main lock purpose of wallet's main key; Free when the
// lock holds no record.
func (h *moverHost) locked(t *testing.T, wallet, key [32]byte) deposit.Purpose {
	t.Helper()
	e, err := h.lock.Current(context.Background(), wallet, key)
	if err != nil {
		t.Fatal("fixture lock read:", err)
	}
	return e.Purpose
}

// resolve, sign and broadcast call the Mover and turn a panic into an error
// wrapping errPanic, so a crash fails the caller's named assertion.
func (h *moverHost) resolve(ctx context.Context, plan *signing.Plan) (status Status, err error) {
	err = recovered(func() error { var err error; status, err = h.mover.Resolve(ctx, plan); return err })
	return status, err
}

func (h *moverHost) sign(ctx context.Context, plan *signing.Plan, round uint64) (result *signing.Result, err error) {
	err = recovered(func() error { var err error; result, err = h.mover.Sign(ctx, plan, round); return err })
	return result, err
}

func (h *moverHost) broadcast(ctx context.Context, plan *signing.Plan) error {
	return recovered(func() error { return h.mover.Broadcast(ctx, plan) })
}
