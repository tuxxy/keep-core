package deposit

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// fakeChain is an in-memory C-05/C-04 reader. Methods the deposit package
// does not use are left to the embedded nil interface.
type fakeChain struct {
	redemption.Reader
	mu       sync.Mutex
	head     uint64
	hashes   map[uint64][32]byte
	times    map[uint64]uint64
	records  map[[32]byte]Record
	reveals  map[[32]byte]Reveal
	custody  redemption.Custody
	outputs  map[[32]byte]redemption.CustodyOutput
	intents  map[[32]byte]redemption.Intent
	maxCerts uint64
	// revealErr, when set, fails reveal-log reads (a provider outage).
	revealErr error
	// unreserved lists deposits without a C-10 reservation, and reservedAt
	// the first block holding one; every other deposit is reserved throughout.
	unreserved map[[32]byte]bool
	reservedAt map[[32]byte]uint64
}

func newFakeChain() *fakeChain {
	c := &fakeChain{head: 100, hashes: map[uint64][32]byte{}, times: map[uint64]uint64{}, records: map[[32]byte]Record{}, reveals: map[[32]byte]Reveal{}, outputs: map[[32]byte]redemption.CustodyOutput{}, intents: map[[32]byte]redemption.Intent{}, maxCerts: 1000}
	return c
}

func (c *fakeChain) hash(n uint64) [32]byte {
	if h, ok := c.hashes[n]; ok {
		return h
	}
	var h [32]byte
	h[0], h[31] = 0xb1, byte(n)
	h[30] = byte(n >> 8)
	return h
}
func (c *fakeChain) ChainID() uint64  { return 31337 }
func (c *fakeChain) Bridge() [20]byte { return [20]byte{0xb2} }
func (c *fakeChain) Head(context.Context) (redemption.Head, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return redemption.Head{Number: c.head, Hash: c.hash(c.head)}, nil
}
func (c *fakeChain) HeaderHash(_ context.Context, n uint64) ([32]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hash(n), nil
}
func (c *fakeChain) Timestamp(_ context.Context, n uint64) (uint64, error) {
	if t, ok := c.times[n]; ok {
		return t, nil
	}
	return 1_800_000_000 + n*12, nil
}
func (c *fakeChain) Configuration(context.Context, uint64) (redemption.Configuration, error) {
	return redemption.Configuration{FinalityBlocks: 2, MaximumCertificateBlocks: c.maxCerts, Enabled: true}, nil
}
func (c *fakeChain) Custody(context.Context, uint64, [32]byte) (redemption.Custody, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.custody, nil
}
func (c *fakeChain) CustodyOutput(_ context.Context, _ uint64, key [32]byte) (redemption.CustodyOutput, error) {
	return c.outputs[key], nil
}
func (c *fakeChain) Intent(_ context.Context, _ uint64, id [32]byte) (redemption.Intent, error) {
	i, ok := c.intents[id]
	if !ok {
		return redemption.Intent{}, redemption.ErrNotFound
	}
	return i, nil
}
func (c *fakeChain) Deposit(_ context.Context, _ uint64, key [32]byte) (Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.records[key]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}
func (c *fakeChain) Reveals(_ context.Context, wallet [32]byte, from, to uint64) ([]Reveal, error) {
	var out []Reveal
	for _, r := range c.reveals {
		if r.WalletID == wallet && r.Block >= from && r.Block <= to {
			out = append(out, r)
		}
	}
	return out, nil
}
func (c *fakeChain) Reserved(_ context.Context, at uint64, key [32]byte) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.unreserved[key] && at >= c.reservedAt[key], nil
}

func (c *fakeChain) Reveal(_ context.Context, key [32]byte) (Reveal, error) {
	if c.revealErr != nil {
		return Reveal{}, c.revealErr
	}
	r, ok := c.reveals[key]
	if !ok {
		return Reveal{}, ErrNotFound
	}
	return r, nil
}

type fixedClock struct {
	mu  sync.Mutex
	mtp uint64
}

func (f *fixedClock) MedianTimePast(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mtp, nil
}
func (f *fixedClock) set(v uint64) { f.mu.Lock(); f.mtp = v; f.mu.Unlock() }

// memoryJournal is a compare-and-insert record store like store.Scoped.
type memoryJournal struct {
	mu      sync.Mutex
	records map[[32]byte][]byte
	onRead  func(id [32]byte)
}

func newJournal() *memoryJournal { return &memoryJournal{records: map[[32]byte][]byte{}} }
func (j *memoryJournal) RetainSigningRecord(_ context.Context, id [32]byte, raw []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if old, ok := j.records[id]; ok {
		if !bytes.Equal(old, raw) {
			return errors.New("conflicting signing application record")
		}
		return nil
	}
	j.records[id] = append([]byte(nil), raw...)
	return nil
}
func (j *memoryJournal) ReadSigningRecord(_ context.Context, id [32]byte) ([]byte, error) {
	j.mu.Lock()
	hook := j.onRead
	raw, ok := j.records[id]
	j.mu.Unlock()
	if hook != nil {
		hook(id)
	}
	if !ok {
		return nil, store.ErrMissing
	}
	return append([]byte(nil), raw...), nil
}

type scriptUnspent struct {
	outputs []bitcoin.PreviousOutput
	spent   map[bitcoin.TransactionOutpoint]bool
}

func (s *scriptUnspent) Unspent(_ context.Context, o bitcoin.TransactionOutpoint) (bool, error) {
	return !s.spent[o], nil
}

func (s *scriptUnspent) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	var out []bitcoin.PreviousOutput
	for _, o := range s.outputs {
		if bytes.Equal(o.PublicKeyScript, script) {
			out = append(out, o)
		}
	}
	return out, nil
}

// world is one wallet with revealed deposits on a fake chain.
type world struct {
	t       *testing.T
	chain   *fakeChain
	clock   *fixedClock
	wallet  dkg.Descriptor
	id      [32]byte
	view    *View
	lock    *MainLock
	journal *memoryJournal
	btc     *scriptUnspent
	planner Planner
	// locktime, when set, is the refund locktime of the next deposit.
	locktime uint32
}

func newWorld(t *testing.T) *world {
	t.Helper()
	q := xonly(testKey(17))
	wallet := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address{1}, Epoch: 17, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{1}, OutputKey: q}
	w := &world{t: t, chain: newFakeChain(), clock: &fixedClock{mtp: 1_800_000_000}, wallet: wallet, id: dkg.WalletID(q), journal: newJournal(), btc: &scriptUnspent{spent: map[bitcoin.TransactionOutpoint]bool{}}}
	w.view = &View{Chain: w.chain, Clock: w.clock, Policy: Policy{FinalityBlocks: 2, RefundMargin: 3600}}
	w.lock = &MainLock{Journal: w.journal}
	w.planner = Planner{Chain: w.chain, Bitcoin: w.btc, Funding: w.btc, Lock: w.lock, Wallet: wallet, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Policy: w.view.Policy, FeeRate: 2, MaximumSatPerVByte: 10}
	return w
}

// deposit reveals one funded deposit at block and returns its key.
func (w *world) deposit(tag byte, amount uint64, block uint64, vault [20]byte) [32]byte {
	w.t.Helper()
	terms := sampleTerms(testKey(9), w.wallet.OutputKey)
	terms.WalletID = w.id
	descriptorHash, err := dkg.DescriptorHash(w.wallet)
	if err != nil {
		w.t.Fatal(err)
	}
	terms.DescriptorHash = descriptorHash
	terms.Amount, terms.Vault = amount, vault
	terms.Blinding[0] = tag
	if w.locktime != 0 {
		terms.RefundLocktime = w.locktime
	}
	c, err := Build(terms, w.wallet.OutputKey)
	if err != nil {
		w.t.Fatal(err)
	}
	funding := bitcoin.TransactionOutpoint{OutputIndex: uint32(tag)}
	funding.TransactionHash[0], funding.TransactionHash[1] = 0xf0, tag
	key := OutpointKey(funding)
	w.chain.records[key] = Record{Terms: terms, Receipt: c.Receipt, FundingTransactionHash: [32]byte(funding.TransactionHash), FundingOutputIndex: funding.OutputIndex, DepositOutputKey: c.OutputKey, WalletKey: w.wallet.OutputKey, MerkleRoot: c.Root, OutputParity: c.Parity, TreasuryFee: amount / 100, MaximumSweepFee: 2_000, RevealedAt: 1}
	w.chain.reveals[key] = Reveal{Outpoint: key, WalletID: w.id, Block: block, BlockHash: w.chain.hash(block)}
	return key
}

// fund gives the wallet a recognized, unreserved main output.
func (w *world) fund(value int64) bitcoin.PreviousOutput {
	script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	main := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xaa}}, Value: value, PublicKeyScript: script}
	key := OutpointKey(main.Outpoint)
	w.chain.custody = redemption.Custody{MainOutpoint: key, Value: uint64(value), StateVersion: 1}
	w.chain.outputs[key] = redemption.CustodyOutput{WalletID: w.id, Value: uint64(value)}
	w.btc.outputs = append(w.btc.outputs, main)
	return main
}
