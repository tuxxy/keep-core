package redemption

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// fakeChain is a controlled C-04 view for unit tests only. It is not C-04
// evidence; the integration tests use the deployed Bridge.
type fakeChain struct {
	mu           sync.Mutex
	head         uint64
	fork         byte
	flipAfter    int // change the fork after this many HeaderHash calls (0: never)
	headerCalls  int
	time         uint64
	config       Configuration
	maxTotalFee  uint64
	requests     map[[32]byte]Request
	intents      map[[32]byte]Intent
	custody      map[[32]byte]Custody
	outputs      map[[32]byte]CustodyOutput
	alternatives map[[32]byte][]Alternative
	admissions   map[[32]byte]Admission
	cancellation map[[64]byte]Admission
	events       []RequestEvent
}

func newFakeChain() *fakeChain {
	return &fakeChain{head: 100, time: 1_000_000, config: Configuration{BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FinalityBlocks: 2, MaximumCertificateBlocks: 50, Enabled: true}, maxTotalFee: 5000, requests: map[[32]byte]Request{}, intents: map[[32]byte]Intent{}, custody: map[[32]byte]Custody{}, outputs: map[[32]byte]CustodyOutput{}, alternatives: map[[32]byte][]Alternative{}, admissions: map[[32]byte]Admission{}, cancellation: map[[64]byte]Admission{}}
}

func (f *fakeChain) hash(n uint64) [32]byte {
	b := binary.BigEndian.AppendUint64([]byte{f.fork}, n)
	return sha256.Sum256(b)
}
func (f *fakeChain) ChainID() uint64  { return 31337 }
func (f *fakeChain) Bridge() [20]byte { return [20]byte{0xb1} }
func (f *fakeChain) Head(context.Context) (Head, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Head{Number: f.head, Hash: f.hash(f.head)}, nil
}
func (f *fakeChain) HeaderHash(_ context.Context, n uint64) ([32]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headerCalls++
	if f.flipAfter > 0 && f.headerCalls >= f.flipAfter {
		f.fork++
		f.flipAfter = 0
	}
	if n > f.head {
		return [32]byte{}, errors.New("future block")
	}
	return f.hash(n), nil
}
func (f *fakeChain) Timestamp(context.Context, uint64) (uint64, error) { return f.time, nil }
func (f *fakeChain) Configuration(context.Context, uint64) (Configuration, error) {
	return f.config, nil
}
func (f *fakeChain) MaximumTotalFee(context.Context, uint64) (uint64, error) {
	return f.maxTotalFee, nil
}
func (f *fakeChain) Request(_ context.Context, _ uint64, id [32]byte) (Request, error) {
	r, ok := f.requests[id]
	if !ok {
		return r, ErrNotFound
	}
	r.Script = append([]byte(nil), r.Script...)
	return r, nil
}
func (f *fakeChain) Intent(_ context.Context, _ uint64, id [32]byte) (Intent, error) {
	i, ok := f.intents[id]
	if !ok {
		return i, ErrNotFound
	}
	return i, nil
}
func (f *fakeChain) Custody(_ context.Context, _ uint64, w [32]byte) (Custody, error) {
	return f.custody[w], nil
}
func (f *fakeChain) CustodyOutput(_ context.Context, _ uint64, k [32]byte) (CustodyOutput, error) {
	return f.outputs[k], nil
}
func (f *fakeChain) Alternatives(_ context.Context, _ uint64, id [32]byte) ([]Alternative, error) {
	return f.alternatives[id], nil
}
func (f *fakeChain) RequestEvents(_ context.Context, w [32]byte, from, to uint64) ([]RequestEvent, error) {
	var out []RequestEvent
	for _, e := range f.events {
		if e.WalletID == w && e.Block >= from && e.Block <= to {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f *fakeChain) Admissions(_ context.Context, w [32]byte, from, to uint64) ([]Admission, error) {
	var out []Admission
	for id, a := range f.admissions {
		if f.intents[id].Certificate.WalletID == w && a.Block >= from && a.Block <= to {
			out = append(out, a)
		}
	}
	return out, nil
}
// The redemption host never sizes proofs; the producers in pkg/maintainer/spv
// read these from the real binding.
func (f *fakeChain) TxProofDifficultyFactor() (*big.Int, error) {
	return nil, errors.New("fake chain has no proof difficulty")
}

func (f *fakeChain) GetCurrentAndPrevEpochDifficulty() (*big.Int, *big.Int, error) {
	return nil, nil, errors.New("fake chain has no proof difficulty")
}

func (f *fakeChain) Admission(_ context.Context, id [32]byte) (Admission, error) {
	a, ok := f.admissions[id]
	if !ok {
		return a, ErrNotFound
	}
	return a, nil
}
func (f *fakeChain) CancellationAdmission(_ context.Context, id, h [32]byte) (Admission, error) {
	var k [64]byte
	copy(k[:32], id[:])
	copy(k[32:], h[:])
	a, ok := f.cancellation[k]
	if !ok {
		return a, ErrNotFound
	}
	return a, nil
}

// memJournal is a compare-and-insert record store with the same identical /
// conflict semantics as store.Scoped.RetainSigningRecord.
type memJournal struct {
	mu      sync.Mutex
	records map[[32]byte][]byte
}

func newMemJournal() *memJournal { return &memJournal{records: map[[32]byte][]byte{}} }
func (m *memJournal) RetainSigningRecord(_ context.Context, id [32]byte, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.records[id]; ok {
		if bytes.Equal(old, raw) {
			return nil
		}
		return errors.New("conflicting signing application record")
	}
	m.records[id] = append([]byte(nil), raw...)
	return nil
}
func (m *memJournal) ReadSigningRecord(_ context.Context, id [32]byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.records[id]
	if !ok {
		return nil, store.ErrMissing
	}
	return append([]byte(nil), raw...), nil
}

type fakeUnspent []bitcoin.PreviousOutput

func (u fakeUnspent) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	var out []bitcoin.PreviousOutput
	for _, p := range u {
		if bytes.Equal(p.PublicKeyScript, script) {
			out = append(out, p)
		}
	}
	return out, nil
}

type world struct {
	chain   *fakeChain
	planner Planner
	wallet  dkg.Descriptor
	main    bitcoin.PreviousOutput
	ids     [][32]byte
}

// newWorld funds one wallet with a 1,000,000 sat main output and two funded
// P2WPKH requests of 100,000 sat gross with a 500 sat treasury fee each.
func newWorld(t *testing.T) *world {
	t.Helper()
	secret := make([]byte, 32)
	secret[31] = 17
	_, pub := btcec.PrivKeyFromBytes(secret)
	var q [32]byte
	copy(q[:], schnorr.SerializePubKey(pub))
	wallet := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(31337), Registry: common.Address{1}, Epoch: 17, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{1}, OutputKey: q}
	script, _ := bitcoin.PayToTaproot(q)
	main := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x33}, OutputIndex: 1}, Value: 1_000_000, PublicKeyScript: script}
	c := newFakeChain()
	walletID := dkg.WalletID(q)
	descriptorHash, err := dkg.DescriptorHash(wallet)
	if err != nil {
		t.Fatal(err)
	}
	c.custody[walletID] = Custody{Generation: 0, StateVersion: 1, Value: 1_000_000, MainOutpoint: OutpointKey(main.Outpoint)}
	c.outputs[OutpointKey(main.Outpoint)] = CustodyOutput{WalletID: walletID, Value: 1_000_000}
	w := &world{chain: c, wallet: wallet, main: main}
	for i := 1; i <= 2; i++ {
		recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{byte(i)})
		id := [32]byte{0xa0, byte(i)}
		c.requests[id] = Request{ID: id, WalletID: walletID, DescriptorHash: descriptorHash, Gross: 100_000, TreasuryFee: 500, MaximumBitcoinFee: 2000, Deadline: c.time + 3600, FinalityBlocks: 2, State: RequestRequested, Script: recipient}
		c.events = append(c.events, RequestEvent{RequestID: id, WalletID: walletID, Block: 90 + uint64(i)})
		w.ids = append(w.ids, id)
	}
	w.planner = Planner{Chain: c, Bitcoin: fakeUnspent{main}, Wallet: wallet, Genesis: c.config.BitcoinGenesis, Fees: FeePolicy{FeeRate: 2, MaximumSatPerVByte: 10, Minimum: 100}, DeadlineMargin: 600}
	return w
}
