package redemption

// Shared fixtures for the mutation-gap tests (batch M4, tuxxy/keep-core
// mutation wave 2). Everything here is a test fixture: a controlled C-04
// reader, fault-injecting journals and a no-worker executor configuration.
// None of it is C-04 or worker evidence; the integration suites use the
// deployed Bridge and the pinned worker.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// errInjected is the fault a fixture injects; tests check it is surfaced.
var errInjected = errors.New("fixture: injected fault")

// errStop is returned by a crash-boundary hook to stop a flow at one step.
var errStop = errors.New("fixture: stopped at boundary")

// faultChain is a controlled C-04 reader (fixture) over fakeChain. Each field
// injects one read failure, or answers a read as of a block (custodyAt,
// requestAt) where the unit fake ignores the block number.
type faultChain struct {
	*fakeChain
	headErr, configErr, maxFeeErr, timestampErr  error
	requestErr, intentErr, custodyErr, outputErr error
	alternativesErr, eventsErr, certifiedErr     error
	headerErrAt                                  map[uint64]bool
	certified                                    []uint16
	custodyAt                                    func(at uint64, wallet [32]byte) (Custody, bool)
	requestAt                                    func(at uint64, r Request) Request
}

func (f *faultChain) Head(ctx context.Context) (Head, error) {
	if f.headErr != nil {
		return Head{}, f.headErr
	}
	return f.fakeChain.Head(ctx)
}
func (f *faultChain) HeaderHash(ctx context.Context, n uint64) ([32]byte, error) {
	if f.headerErrAt[n] {
		return [32]byte{}, errInjected
	}
	return f.fakeChain.HeaderHash(ctx, n)
}
func (f *faultChain) Timestamp(ctx context.Context, n uint64) (uint64, error) {
	if f.timestampErr != nil {
		return 0, f.timestampErr
	}
	return f.fakeChain.Timestamp(ctx, n)
}
func (f *faultChain) Configuration(ctx context.Context, at uint64) (Configuration, error) {
	if f.configErr != nil {
		return Configuration{}, f.configErr
	}
	return f.fakeChain.Configuration(ctx, at)
}
func (f *faultChain) MaximumTotalFee(ctx context.Context, at uint64) (uint64, error) {
	if f.maxFeeErr != nil {
		return 0, f.maxFeeErr
	}
	return f.fakeChain.MaximumTotalFee(ctx, at)
}
func (f *faultChain) Request(ctx context.Context, at uint64, id [32]byte) (Request, error) {
	if f.requestErr != nil {
		return Request{}, f.requestErr
	}
	r, err := f.fakeChain.Request(ctx, at, id)
	if err == nil && f.requestAt != nil {
		r = f.requestAt(at, r)
	}
	return r, err
}
func (f *faultChain) Intent(ctx context.Context, at uint64, id [32]byte) (Intent, error) {
	if f.intentErr != nil {
		return Intent{}, f.intentErr
	}
	return f.fakeChain.Intent(ctx, at, id)
}
func (f *faultChain) Custody(ctx context.Context, at uint64, w [32]byte) (Custody, error) {
	if f.custodyErr != nil {
		return Custody{}, f.custodyErr
	}
	if f.custodyAt != nil {
		if c, ok := f.custodyAt(at, w); ok {
			return c, nil
		}
	}
	return f.fakeChain.Custody(ctx, at, w)
}
func (f *faultChain) CustodyOutput(ctx context.Context, at uint64, k [32]byte) (CustodyOutput, error) {
	if f.outputErr != nil {
		return CustodyOutput{}, f.outputErr
	}
	return f.fakeChain.CustodyOutput(ctx, at, k)
}
func (f *faultChain) Alternatives(ctx context.Context, at uint64, id [32]byte) ([]Alternative, error) {
	if f.alternativesErr != nil {
		return nil, f.alternativesErr
	}
	return f.fakeChain.Alternatives(ctx, at, id)
}
func (f *faultChain) RequestEvents(ctx context.Context, w [32]byte, from, to uint64) ([]RequestEvent, error) {
	if f.eventsErr != nil {
		return nil, f.eventsErr
	}
	return f.fakeChain.RequestEvents(ctx, w, from, to)
}
func (f *faultChain) CertifiedSeats(ctx context.Context, w [32]byte) ([]uint16, error) {
	if f.certifiedErr != nil {
		return nil, f.certifiedErr
	}
	if f.certified != nil {
		return append([]uint16(nil), f.certified...), nil
	}
	return f.fakeChain.CertifiedSeats(ctx, w)
}

// faultJournal is a record journal (fixture) whose reads or writes fail.
type faultJournal struct {
	RecordJournal
	readErr, writeErr error
}

func (j faultJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	if j.readErr != nil {
		return nil, j.readErr
	}
	return j.RecordJournal.ReadSigningRecord(ctx, id)
}
func (j faultJournal) RetainSigningRecord(ctx context.Context, id [32]byte, raw []byte) error {
	if j.writeErr != nil {
		return j.writeErr
	}
	return j.RecordJournal.RetainSigningRecord(ctx, id, raw)
}

// stopAt is a crash-boundary hook that stops the flow at one named step.
func stopAt(name string) func(string) error {
	return func(step string) error {
		if step == name {
			return errStop
		}
		return nil
	}
}

// noPanic runs f and turns a crash into a named test failure.
func noPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if v := recover(); v != nil {
			t.Fatalf("%s crashed instead of refusing: %v", what, v)
		}
	}()
	f()
}

// gapFence is a single-writer fence for the unit store (fixture).
type gapFence struct{ point store.Point }

func (f *gapFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) { return f, nil }
func (f *gapFence) Head(context.Context) (store.Point, error)                   { return f.point, nil }
func (f *gapFence) Advance(_ context.Context, b, a store.Point) error {
	if b != f.point {
		return store.ErrQuarantined
	}
	f.point = a
	return nil
}
func (f *gapFence) Close() error { return nil }

// openJournal opens a real encrypted store scope holding no key: a host
// whose completed key is absent, so any signing attempt fails before a
// worker could start (fixture).
func openJournal(t *testing.T, wallet [32]byte) *store.Scoped {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{0x4d}, Key: [32]byte{0x34}, Fence: &gapFence{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	scoped, err := s.Scope(frost.Domain{Network: "M4/redemption-gaps", Chain: [32]byte{1}, Registry: [20]byte{2}, Wallet: wallet, Epoch: 17})
	if err != nil {
		t.Fatal(err)
	}
	return scoped
}

// noBitcoin is a Bitcoin view (fixture) that answers nothing.
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

func noTransport(context.Context, frost.Attempt) (frost.Transport, func(), error) {
	return nil, nil, errInjected
}

// fixtureWorker is a pinned worker configuration whose binary does not
// exist (fixture): it passes configuration checks but can never run.
var fixtureWorker = snowfallengine.WorkerConfig{Path: "/nonexistent/m4-fixture/snowfall-worker", SHA256: [32]byte{1}}

// resolverWorld is one admitted reservation of the two-request unit plan.
type resolverWorld struct {
	*world
	fc      *faultChain
	plan    *signing.Plan
	id      [32]byte
	journal *store.Scoped
	r       *Resolver
	t       *testing.T
}

// newResolverWorld admits the unit plan at block 90 (finality 2, rounds of
// 10 blocks, so head 100 is round 0) with the requests timing out at blocks
// 97 and 95. The executor configuration is complete, but the host holds no
// completed key and the worker path does not exist (fixture): every signing
// attempt fails, and no durable result exists.
func newResolverWorld(t *testing.T, state IntentState) *resolverWorld {
	t.Helper()
	ctx := context.Background()
	w := newWorld(t)
	fc := &faultChain{fakeChain: w.chain}
	w.planner.Chain = fc
	plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range w.ids {
		r := w.chain.requests[id]
		r.TimeoutBlock = []uint64{97, 95}[i]
		w.chain.requests[id] = r
	}
	id := [32]byte{0x5e}
	w.chain.intents[id] = Intent{ID: id, Certificate: Certificate{WalletID: plan.WalletID(), IntentCommitment: plan.Commitment(), Generation: 1, StateVersion: 1}, AdmittedBlock: 90, TransactionHash: [32]byte{0x7a}, State: state, RequestIDs: w.ids, Seats: []uint16{1, 2}}
	w.chain.admissions[id] = Admission{ReservationID: id, TransactionHash: [32]byte{0x7a}, Block: 90, BlockHash: w.chain.hash(90)}
	journal := openJournal(t, plan.WalletID())
	r := &Resolver{
		Chain:    fc,
		Ledger:   Ledger{Journal: journal},
		Schedule: Schedule{FinalityBlocks: 2, RoundBlocks: 10, CancelAfterBlocks: 5},
		Policy:   RebroadcastThenCancel,
		Base: signing.Config{
			MinimumConfirmations: 1,
			Journal:              journal,
			Bitcoin:              noBitcoin{},
			Worker:               fixtureWorker,
			Transport:            noTransport,
			LocalRegtest:         true,
		},
	}
	return &resolverWorld{world: w, fc: fc, plan: plan, id: id, journal: journal, r: r, t: t}
}

// entries returns the reservation's ledger records.
func (rw *resolverWorld) entries(t *testing.T) []Entry {
	t.Helper()
	e, err := rw.r.Ledger.Entries(context.Background(), rw.id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// count counts ledger records of a kind (and detail, when given) for this
// plan's transaction or for no transaction.
func (rw *resolverWorld) count(t *testing.T, kind Kind, detail string) int {
	t.Helper()
	n := 0
	for _, e := range rw.entries(t) {
		if e.Kind == kind && (detail == "" || e.Detail == detail) && (e.Transaction == ([32]byte{}) || e.Transaction == rw.plan.BitcoinTransactionHash()) {
			n++
		}
	}
	return n
}

// mark appends a ledger record for this plan's transaction (t nil: the
// world's own test). A KindSigned mark is a planted marker (fixture): the
// executor's durable result record is absent, as when it is unreadable or
// invalid (the marker is only a cache, resolver.go signedRound).
func (rw *resolverWorld) mark(t *testing.T, kind Kind, detail string) {
	if t == nil {
		t = rw.t
	}
	t.Helper()
	if err := rw.r.Ledger.Append(context.Background(), Entry{Reservation: rw.id, Kind: kind, Block: 100, Transaction: rw.plan.BitcoinTransactionHash(), Detail: detail}); err != nil {
		t.Fatal(err)
	}
}

func (rw *resolverWorld) setHead(n uint64) { rw.chain.head = n }

// fixedDirectives is a governance directive source (fixture) with one answer.
type fixedDirectives struct {
	d   Directive
	ok  bool
	err error
}

func (f fixedDirectives) Directive(context.Context, [32]byte) (Directive, bool, error) {
	return f.d, f.ok, f.err
}

// failingMainLock is a K-06 main purpose lock (fixture) that refuses.
type failingMainLock struct{}

func (failingMainLock) AcquireRedemption(context.Context, [32]byte, bitcoin.TransactionOutpoint, [32]byte, uint64) error {
	return errInjected
}

// guard, deferred at the top of a test, turns a crash anywhere in it into a
// named test failure.
func guard(t *testing.T) {
	if v := recover(); v != nil {
		t.Helper()
		t.Fatalf("crashed instead of refusing: %v", v)
	}
}

// invalidOperatorKey is an operator key that cannot sign (fixture): the zero
// scalar on secp256k1. Attester is a struct literal, so nothing validates
// its Key before signing.
func invalidOperatorKey() *ecdsa.PrivateKey {
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: crypto.S256(), X: new(big.Int), Y: new(big.Int)}, D: new(big.Int)}
}

// countingJournal is a ledger journal (fixture) that fails exactly its n-th
// read, and records whether that read came before the first ledger write.
type countingJournal struct {
	RecordJournal
	n, reads   int
	wrote, hit bool
}

func (j *countingJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	j.reads++
	if j.reads == j.n {
		j.hit = !j.wrote
		return nil, errInjected
	}
	return j.RecordJournal.ReadSigningRecord(ctx, id)
}

func (j *countingJournal) RetainSigningRecord(ctx context.Context, id [32]byte, raw []byte) error {
	j.wrote = true
	return j.RecordJournal.RetainSigningRecord(ctx, id, raw)
}
