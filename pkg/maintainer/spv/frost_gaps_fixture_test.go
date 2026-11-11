package spv

// Everything in this file is a test FIXTURE for the FROST maintainers
// (FrostMovementMaintainer, FrostSweepMaintainer, FrostRedemptionMaintainer):
// an in-memory FROST Bridge and an in-memory Bitcoin chain that serves SPV
// proofs. Neither validates anything: the code under test is the maintainers'
// own logic between the reads they make and the proofs they submit.
//
// Both fakes share one read-fault injector: with failAt = n, the n-th read
// (counted across the Bridge and Bitcoin) fails. Tests loop n = 1, 2, ...
// until a run no longer reaches the fault, so they do not depend on the order
// or number of reads the maintainers make.

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// errFixtureRead is the injected read fault.
var errFixtureRead = errors.New("fixture: injected read fault")

// faults counts reads and fails the failAt-th one (0: never). firedOp names
// the read that failed.
type faults struct {
	reads   int
	failAt  int
	fired   bool
	firedOp string
}

func (f *faults) read(op ...string) error {
	f.reads++
	if f.failAt != 0 && f.reads == f.failAt {
		f.fired = true
		if len(op) > 0 {
			f.firedOp = op[0]
		}
		return errFixtureRead
	}
	return nil
}

// gapsBridge is the union of the C-04/C-05/C-06 seams the maintainers take.
type gapsBridge interface {
	redemption.Chain
	movement.Chain
	deposit.Chain
}

// submission is one write the maintainer sent.
type submission struct {
	kind        string
	reservation [32]byte
	request     [32]byte
	transaction [32]byte
	sweep       deposit.SweepProof
}

// fakeBridge is a FIXTURE FROST Bridge. Methods the maintainers do not call
// are not implemented (the embedded nil interface panics if one is called).
type fakeBridge struct {
	gapsBridge
	f *faults

	head uint64
	now  uint64
	// factor is the Bridge proof difficulty factor; current and previous are
	// the relay epoch difficulties.
	factor            int64
	current, previous int64
	// factorErr fails every factor read (a Bridge that cannot be read).
	factorErr error

	// C-06: the wallet a transaction's proof recorded it for.
	proven map[[32]byte][32]byte
	// C-05 / C-04 custody.
	custody  redemption.Custody
	outputs  map[[32]byte]redemption.CustodyOutput
	deposits map[[32]byte]deposit.Record
	reserved map[[32]byte]bool
	// C-04 redemption.
	admissions   []redemption.Admission
	intents      map[[32]byte]redemption.Intent
	alternatives map[[32]byte][]redemption.Alternative
	events       []redemption.RequestEvent
	requests     map[[32]byte]redemption.Request

	// submit, if set, decides the outcome of each write (nil: accepted). It
	// may change the fake's state, as a mined write would.
	submit func(s submission) error
	sent   []submission
}

func newFakeBridge(f *faults) *fakeBridge {
	return &fakeBridge{
		f: f, head: 100, factor: 2, current: 32, previous: 32,
		proven:       map[[32]byte][32]byte{},
		outputs:      map[[32]byte]redemption.CustodyOutput{},
		deposits:     map[[32]byte]deposit.Record{},
		reserved:     map[[32]byte]bool{},
		intents:      map[[32]byte]redemption.Intent{},
		alternatives: map[[32]byte][]redemption.Alternative{},
		requests:     map[[32]byte]redemption.Request{},
	}
}

func (b *fakeBridge) write(s submission) error {
	b.sent = append(b.sent, s)
	if b.submit == nil {
		return nil
	}
	return b.submit(s)
}

func (b *fakeBridge) count(kind string) int {
	n := 0
	for _, s := range b.sent {
		if s.kind == kind {
			n++
		}
	}
	return n
}

func (b *fakeBridge) TxProofDifficultyFactor() (*big.Int, error) {
	if err := b.f.read(); err != nil {
		return nil, err
	}
	if b.factorErr != nil {
		return nil, b.factorErr
	}
	return big.NewInt(b.factor), nil
}

func (b *fakeBridge) GetCurrentAndPrevEpochDifficulty() (*big.Int, *big.Int, error) {
	if err := b.f.read(); err != nil {
		return nil, nil, err
	}
	return big.NewInt(b.current), big.NewInt(b.previous), nil
}

func (b *fakeBridge) Head(context.Context) (redemption.Head, error) {
	if err := b.f.read(); err != nil {
		return redemption.Head{}, err
	}
	return redemption.Head{Number: b.head}, nil
}

func (b *fakeBridge) Timestamp(context.Context, uint64) (uint64, error) {
	if err := b.f.read(); err != nil {
		return 0, err
	}
	return b.now, nil
}

func (b *fakeBridge) ProvenTransaction(_ context.Context, _ uint64, hash [32]byte) ([32]byte, error) {
	if err := b.f.read(); err != nil {
		return [32]byte{}, err
	}
	return b.proven[hash], nil
}

func (b *fakeBridge) Custody(context.Context, uint64, [32]byte) (redemption.Custody, error) {
	if err := b.f.read(); err != nil {
		return redemption.Custody{}, err
	}
	return b.custody, nil
}

func (b *fakeBridge) CustodyOutput(_ context.Context, _ uint64, key [32]byte) (redemption.CustodyOutput, error) {
	if err := b.f.read(); err != nil {
		return redemption.CustodyOutput{}, err
	}
	out, ok := b.outputs[key]
	if !ok {
		return redemption.CustodyOutput{}, fmt.Errorf("fixture: no custody output %x", key)
	}
	return out, nil
}

func (b *fakeBridge) Deposit(_ context.Context, _ uint64, key [32]byte) (deposit.Record, error) {
	if err := b.f.read(); err != nil {
		return deposit.Record{}, err
	}
	r, ok := b.deposits[key]
	if !ok {
		return deposit.Record{}, deposit.ErrNotFound
	}
	return r, nil
}

func (b *fakeBridge) Reserved(_ context.Context, _ uint64, key [32]byte) (bool, error) {
	if err := b.f.read(); err != nil {
		return false, err
	}
	return b.reserved[key], nil
}

func (b *fakeBridge) Admissions(context.Context, [32]byte, uint64, uint64) ([]redemption.Admission, error) {
	if err := b.f.read(); err != nil {
		return nil, err
	}
	return b.admissions, nil
}

func (b *fakeBridge) Intent(_ context.Context, _ uint64, id [32]byte) (redemption.Intent, error) {
	if err := b.f.read(); err != nil {
		return redemption.Intent{}, err
	}
	return b.intents[id], nil
}

func (b *fakeBridge) Alternatives(_ context.Context, _ uint64, id [32]byte) ([]redemption.Alternative, error) {
	if err := b.f.read(); err != nil {
		return nil, err
	}
	return b.alternatives[id], nil
}

func (b *fakeBridge) RequestEvents(context.Context, [32]byte, uint64, uint64) ([]redemption.RequestEvent, error) {
	if err := b.f.read(); err != nil {
		return nil, err
	}
	return b.events, nil
}

func (b *fakeBridge) Request(_ context.Context, _ uint64, id [32]byte) (redemption.Request, error) {
	if err := b.f.read(); err != nil {
		return redemption.Request{}, err
	}
	return b.requests[id], nil
}

func (b *fakeBridge) SubmitMovementProof(_ context.Context, wallet [32]byte, p movement.Proof) error {
	return b.write(submission{kind: "movement", transaction: txHashOf(p.Transaction)})
}

func (b *fakeBridge) SubmitIncomingSweepProof(_ context.Context, wallet [32]byte, p movement.Proof) error {
	return b.write(submission{kind: "incoming-sweep", transaction: txHashOf(p.Transaction)})
}

func (b *fakeBridge) SubmitSweepProof(_ context.Context, p deposit.SweepProof) error {
	return b.write(submission{kind: "sweep", transaction: txHashOf(p.Transaction), sweep: p})
}

func (b *fakeBridge) SubmitSweepResolutionProof(_ context.Context, p deposit.SweepProof, a deposit.ResolutionAuthorization, _ []uint16, _ []byte) error {
	return b.write(submission{kind: "sweep-resolution", reservation: a.ReservationID, transaction: txHashOf(p.Transaction), sweep: p})
}

func (b *fakeBridge) ReserveDeposit(_ context.Context, key [32]byte, funding redemption.BitcoinInfo, _ redemption.BitcoinProof) error {
	return b.write(submission{kind: "deposit-reservation", reservation: key, transaction: txHashOf(funding)})
}

func (b *fakeBridge) SubmitPaymentProof(_ context.Context, reservation [32]byte, tx redemption.BitcoinInfo, _ redemption.BitcoinProof) error {
	return b.write(submission{kind: "payment", reservation: reservation, transaction: txHashOf(tx)})
}

func (b *fakeBridge) SubmitCancellationProof(_ context.Context, reservation [32]byte, tx redemption.BitcoinInfo, _ redemption.BitcoinProof) error {
	return b.write(submission{kind: "cancellation", reservation: reservation, transaction: txHashOf(tx)})
}

func (b *fakeBridge) NotifyTimeout(_ context.Context, request [32]byte) error {
	return b.write(submission{kind: "timeout", request: request})
}

func (b *fakeBridge) RefundUnreserved(_ context.Context, request [32]byte) error {
	return b.write(submission{kind: "unreserved-refund", request: request})
}

func (b *fakeBridge) RefundResolved(_ context.Context, request [32]byte) error {
	return b.write(submission{kind: "resolved-refund", request: request})
}

func (b *fakeBridge) ClaimCancellationBudget(_ context.Context, reservation, hash [32]byte) error {
	return b.write(submission{kind: "budget-claim", reservation: reservation, transaction: hash})
}

// txHashOf is the raw hash of the transaction a BitcoinInfo carries.
func txHashOf(info redemption.BitcoinInfo) [32]byte {
	raw := append(append(append(append([]byte{}, info.Version[:]...), info.InputVector...), info.OutputVector...), info.Locktime[:]...)
	tx := new(bitcoin.Transaction)
	if err := tx.Deserialize(raw); err != nil {
		return [32]byte{}
	}
	return [32]byte(tx.Hash())
}

// proofBitcoin is a FIXTURE Bitcoin chain: the package's localBitcoinChain
// (transactions, confirmations, headers of relay difficulty 32 at heights
// 101..106) plus the Merkle and coinbase reads proof assembly makes. A
// substitute transaction is served under another transaction's hash, to
// model a Bitcoin backend that returns the wrong transaction.
type proofBitcoin struct {
	*localBitcoinChain
	f          *faults
	coinbase   *bitcoin.Transaction
	substitute map[bitcoin.Hash]*bitcoin.Transaction
}

func newProofBitcoin(t *testing.T, f *faults) *proofBitcoin {
	t.Helper()
	btc := &proofBitcoin{
		localBitcoinChain: newLocalBitcoinChain(),
		f:                 f,
		coinbase:          gapsTx(0xcb),
		substitute:        map[bitcoin.Hash]*bitcoin.Transaction{},
	}
	if err := populateBlockHeaders(btc.localBitcoinChain, 101, 106, func(uint) *big.Int { return big.NewInt(32) }); err != nil {
		t.Fatal(err)
	}
	if err := btc.localBitcoinChain.BroadcastTransaction(btc.coinbase); err != nil {
		t.Fatal(err)
	}
	return btc
}

// confirm adds tx with the given number of confirmations.
func (c *proofBitcoin) confirm(t *testing.T, tx *bitcoin.Transaction, confirmations uint) [32]byte {
	t.Helper()
	if err := c.localBitcoinChain.BroadcastTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if err := c.addTransactionConfirmations(tx.Hash(), confirmations); err != nil {
		t.Fatal(err)
	}
	return [32]byte(tx.Hash())
}

func (c *proofBitcoin) GetTransaction(hash bitcoin.Hash) (*bitcoin.Transaction, error) {
	if err := c.f.read(); err != nil {
		return nil, err
	}
	if tx, ok := c.substitute[hash]; ok {
		return tx, nil
	}
	return c.localBitcoinChain.GetTransaction(hash)
}

func (c *proofBitcoin) GetTransactionConfirmations(ctx context.Context, hash bitcoin.Hash) (uint, error) {
	if err := c.f.read("confirmations"); err != nil {
		return 0, err
	}
	return c.localBitcoinChain.GetTransactionConfirmations(ctx, hash)
}

func (c *proofBitcoin) GetLatestBlockHeight() (uint, error) {
	if err := c.f.read(); err != nil {
		return 0, err
	}
	return c.localBitcoinChain.GetLatestBlockHeight()
}

func (c *proofBitcoin) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	if err := c.f.read(); err != nil {
		return nil, err
	}
	return c.localBitcoinChain.GetBlockHeader(height)
}

func (c *proofBitcoin) GetTransactionMerkleProof(_ bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
	if err := c.f.read(); err != nil {
		return nil, err
	}
	return &bitcoin.TransactionMerkleProof{BlockHeight: height}, nil
}

func (c *proofBitcoin) GetCoinbaseTxHash(uint) (bitcoin.Hash, error) {
	if err := c.f.read(); err != nil {
		return bitcoin.Hash{}, err
	}
	return c.coinbase.Hash(), nil
}

// gapsTx is a distinct FIXTURE transaction (no witness) for each tag. With
// no inputs given it spends a placeholder outpoint, so that it serializes
// unambiguously.
func gapsTx(tag byte, inputs ...bitcoin.TransactionOutpoint) *bitcoin.Transaction {
	if len(inputs) == 0 {
		inputs = []bitcoin.TransactionOutpoint{{TransactionHash: bitcoin.Hash{0xee, tag}}}
	}
	tx := &bitcoin.Transaction{Version: 2, Outputs: []*bitcoin.TransactionOutput{{Value: 1000 + int64(tag), PublicKeyScript: []byte{0x51}}}}
	for i := range inputs {
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &inputs[i], Sequence: 0xffffffff})
	}
	return tx
}

// errFixturePanic marks a recovered panic: a maintainer must refuse, never
// crash.
var errFixturePanic = errors.New("maintainer panicked")

// recovered runs fn and turns a panic into an errFixturePanic error, so that
// a mutant that crashes instead of refusing fails a named assertion.
func recovered(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errFixturePanic, r)
		}
	}()
	return fn()
}
