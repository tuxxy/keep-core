package movement

// Remediation batch 3: the world for the incoming-sweep coordination tests
// (incoming_b3_test.go). No build tag (tuxxy/keep-core#64).
//
// Real: the pinned snowfall worker, the guarded engine, a real DKG of one
// FROST wallet, the encrypted store (per operator, reopened on restart),
// libp2p on loopback with the authenticated FROST transport, the K-06 main
// lock, the K-05 ledger, and the K-07 Planner, View, Authority and Mover.
//
// FIXTURE (labelled): the C-06 chain (chainFixture), the Bitcoin view and the
// miner plus SPV maintainer (btcFixture, world.mine), and the wallet record
// written after the DKG (no registry approval ran).
//
// ADAPTER (labelled): recipeHub, the in-memory recipe channel between
// operators. It authenticates the sending seat and lets a test crash an
// operator the moment it publishes.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// Test values (D-09 OPEN; D2 topology): five seats, threshold three, three
// operators with uneven seat counts. No operator reaches the threshold alone.
var b3Topology = [][]uint16{{1, 2}, {3, 4}, {5}}

const (
	b3Threshold = 3
	b3Finality  = 2
	b3Round     = 4
	// b3StepBound bounds one operator's step in one tick.
	b3StepBound = time.Second
	// b3Ticks bounds how many rounds a case may take to settle.
	b3Ticks = 6
)

// ---------------------------------------------------------------- chain

// chainFixture is a FIXTURE C-06/C-09 chain: one target wallet, its custody
// and incoming records, at a head the test moves between ticks. Reads the
// fixture does not model reach the embedded nil Reader and panic, naming the
// method.
type chainFixture struct {
	Reader
	mu       sync.RWMutex
	head     uint64
	wallet   Wallet
	custody  redemption.Custody
	outputs  map[[32]byte]redemption.CustodyOutput
	incoming map[[32]byte]Incoming
	logs     []IncomingLog
	params   Parameters
	proven   map[[32]byte][32]byte
}

func (c *chainFixture) ChainID() uint64  { return 1 }
func (c *chainFixture) Bridge() [20]byte { return [20]byte{0xb3} }
func (c *chainFixture) hash(n uint64) [32]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("b3-block-%d", n)))
}
func (c *chainFixture) Head(context.Context) (redemption.Head, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return redemption.Head{Number: c.head, Hash: c.hash(c.head)}, nil
}
func (c *chainFixture) HeaderHash(_ context.Context, n uint64) ([32]byte, error) {
	return c.hash(n), nil
}
func (c *chainFixture) Wallet(_ context.Context, _ uint64, l [20]byte) (Wallet, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if l != Label(c.wallet.ID) {
		return Wallet{}, ErrNotFound
	}
	return c.wallet, nil
}
func (c *chainFixture) Custody(_ context.Context, _ uint64, w [32]byte) (redemption.Custody, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if w != c.wallet.ID {
		return redemption.Custody{}, nil
	}
	return c.custody, nil
}
func (c *chainFixture) CustodyOutput(_ context.Context, _ uint64, k [32]byte) (redemption.CustodyOutput, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.outputs[k], nil
}
func (c *chainFixture) MovementParameters(context.Context, uint64) (Parameters, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.params, nil
}
func (c *chainFixture) Lifecycle(context.Context, uint64, [32]byte) (Lifecycle, error) {
	return Lifecycle{}, nil
}
func (c *chainFixture) Counters(context.Context, uint64, [32]byte) (Counters, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var pending uint64
	for _, in := range c.incoming {
		if in.State == IncomingPending || in.State == IncomingEscalated {
			pending++
		}
	}
	return Counters{PendingIncoming: pending}, nil
}
func (c *chainFixture) IncomingLogs(_ context.Context, _ [32]byte, from, to uint64) ([]IncomingLog, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []IncomingLog
	for _, l := range c.logs {
		if l.Block >= from && l.Block <= to {
			out = append(out, l)
		}
	}
	return out, nil
}
func (c *chainFixture) Incoming(_ context.Context, _ uint64, k [32]byte) (Incoming, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incoming[k], nil
}
func (c *chainFixture) ProvenTransaction(_ context.Context, _ uint64, h [32]byte) ([32]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proven[h], nil
}

// ---------------------------------------------------------------- bitcoin

// btcFixture is a FIXTURE Bitcoin view, mempool and miner. It refuses a
// broadcast that spends an output a mined or pooled transaction already
// spends, as Bitcoin Core does without RBF.
type btcFixture struct {
	mu        sync.Mutex
	txs       map[bitcoin.Hash][]byte
	confirmed map[bitcoin.Hash]bool
	spent     map[bitcoin.TransactionOutpoint]bitcoin.Hash
	pool      []bitcoin.Hash
	// broadcasts is every distinct transaction offered, refused or not.
	broadcasts map[bitcoin.Hash]*bitcoin.Transaction
}

func newBTC() *btcFixture {
	return &btcFixture{txs: map[bitcoin.Hash][]byte{}, confirmed: map[bitcoin.Hash]bool{}, spent: map[bitcoin.TransactionOutpoint]bitcoin.Hash{}, broadcasts: map[bitcoin.Hash]*bitcoin.Transaction{}}
}

func (b *btcFixture) Genesis(context.Context) ([32]byte, error) {
	return [32]byte(*chaincfg.RegressionNetParams.GenesisHash), nil
}
func (b *btcFixture) Transaction(_ context.Context, h bitcoin.Hash) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.txs[h]
	if !ok {
		return nil, errors.New("FIXTURE: unknown transaction")
	}
	return append([]byte(nil), raw...), nil
}
func (b *btcFixture) Confirmations(_ context.Context, h bitcoin.Hash) (uint, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.confirmed[h] {
		return 6, nil
	}
	return 0, nil
}
func (b *btcFixture) Unspent(_ context.Context, o bitcoin.TransactionOutpoint) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, spent := b.spent[o]
	return !spent && b.confirmed[o.TransactionHash], nil
}
func (b *btcFixture) Broadcast(_ context.Context, raw []byte) error {
	tx := new(bitcoin.Transaction)
	if err := tx.Deserialize(raw); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	h := tx.Hash()
	b.broadcasts[h] = tx
	if _, ok := b.txs[h]; ok {
		return nil
	}
	for _, in := range tx.Inputs {
		if by, ok := b.spent[*in.Outpoint]; ok && by != h {
			return errors.New("FIXTURE: txn-mempool-conflict")
		}
	}
	for _, in := range tx.Inputs {
		b.spent[*in.Outpoint] = h
	}
	b.txs[h] = append([]byte(nil), raw...)
	b.pool = append(b.pool, h)
	return nil
}

// fund adds a confirmed transaction paying value to script and returns its
// first output.
func (b *btcFixture) fund(tag byte, value int64, script bitcoin.Script) bitcoin.PreviousOutput {
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{tag, 0xb3}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: value, PublicKeyScript: script}}}
	b.mu.Lock()
	defer b.mu.Unlock()
	h := tx.Hash()
	b.txs[h] = tx.Serialize(bitcoin.Standard)
	b.confirmed[h] = true
	return bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: h}, Value: value, PublicKeyScript: script}
}

// scriptUnspent lists confirmed unspent outputs paying a script.
type scriptUnspent struct{ b *btcFixture }

func (s scriptUnspent) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	var out []bitcoin.PreviousOutput
	for h := range s.b.confirmed {
		tx := new(bitcoin.Transaction)
		if tx.Deserialize(s.b.txs[h]) != nil {
			continue
		}
		for i, o := range tx.Outputs {
			p := bitcoin.TransactionOutpoint{TransactionHash: h, OutputIndex: uint32(i)}
			if _, spent := s.b.spent[p]; !spent && bytes.Equal(o.PublicKeyScript, script) {
				out = append(out, bitcoin.PreviousOutput{Outpoint: p, Value: o.Value, PublicKeyScript: o.PublicKeyScript})
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- recipes

// recipeHub is the ADAPTER recipe channel. Each operator's endpoint
// implements frost.Transport: Broadcast and SendPrivate check that the
// sending seat is one of the operator's own seats, and Receive yields
// messages with the authenticated sending seat. An offline operator receives
// nothing; what it missed is lost.
type recipeHub struct {
	mu      sync.Mutex
	inboxes map[int]chan frost.Incoming
	// onSend, when set for an operator, runs right after its message is
	// delivered (used to crash it at publish).
	onSend map[int]func()
	sent   map[int]int
}

type recipeEndpoint struct {
	hub      *recipeHub
	operator int
}

func newHub() *recipeHub {
	return &recipeHub{inboxes: map[int]chan frost.Incoming{}, onSend: map[int]func(){}, sent: map[int]int{}}
}

func (h *recipeHub) endpoint(op int) *recipeEndpoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inboxes[op] = make(chan frost.Incoming, 1024)
	return &recipeEndpoint{hub: h, operator: op}
}

func (h *recipeHub) offline(op int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.inboxes, op)
}

func operatorOf(seat uint16) int {
	for op, seats := range b3Topology {
		for _, s := range seats {
			if s == seat {
				return op
			}
		}
	}
	return -1
}

func (e *recipeEndpoint) deliver(from uint16, to int, b []byte) error {
	if operatorOf(from) != e.operator {
		return fmt.Errorf("ADAPTER: operator %d cannot send as seat %d", e.operator, from)
	}
	h := e.hub
	h.mu.Lock()
	var hook func()
	for op, inbox := range h.inboxes {
		if op == e.operator || (to >= 0 && op != to) {
			continue
		}
		select {
		case inbox <- frost.Incoming{AuthenticatedSender: from, Message: append([]byte(nil), b...)}:
		default:
		}
	}
	h.sent[e.operator]++
	hook = h.onSend[e.operator]
	h.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (e *recipeEndpoint) Broadcast(_ context.Context, seat uint16, b []byte) error {
	return e.deliver(seat, -1, b)
}
func (e *recipeEndpoint) SendPrivate(_ context.Context, from, to uint16, b []byte) error {
	return e.deliver(from, operatorOf(to), b)
}
func (e *recipeEndpoint) Receive(ctx context.Context) (frost.Incoming, error) {
	e.hub.mu.Lock()
	inbox := e.hub.inboxes[e.operator]
	e.hub.mu.Unlock()
	if inbox == nil {
		<-ctx.Done()
		return frost.Incoming{}, ctx.Err()
	}
	select {
	case m := <-inbox:
		return m, nil
	case <-ctx.Done():
		return frost.Incoming{}, ctx.Err()
	}
}

// ---------------------------------------------------------------- world

// fenceFixture is a TEST ONLY fence that outlives reopened stores.
type fenceFixture struct {
	mu    sync.Mutex
	point store.Point
}
type fenceLease struct{ f *fenceFixture }

func (f *fenceFixture) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	return fenceLease{f}, nil
}
func (l fenceLease) Head(context.Context) (store.Point, error) {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	return l.f.point, nil
}
func (l fenceLease) Advance(_ context.Context, before, after store.Point) error {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	if before != l.f.point {
		return store.ErrQuarantined
	}
	l.f.point = after
	return nil
}
func (l fenceLease) Close() error { return nil }

type dkgAcceptance struct{}

func (dkgAcceptance) WaitCandidateAcceptance(_ context.Context, c frost.Candidate) (frost.Receipt, error) {
	return frost.Receipt{Epoch: c.Epoch, Descriptor: c.Descriptor}, nil
}
func (dkgAcceptance) BeforeSigning(context.Context, frost.SigningRequest) error { return nil }

// signingTap passes the real signing transport through and observes the
// worker's protocol messages. The engine sends them only after its durable
// claim and the executor's grant, so the first one marks an operator whose
// share may be on the wire. A case may crash the operator right there.
type signingTap struct {
	*transport.Transport
	op *b3Operator
}

func (s *signingTap) sent() {
	s.op.mu.Lock()
	s.op.signingSends++
	hook := s.op.onSigningSend
	s.op.mu.Unlock()
	if hook != nil {
		hook()
	}
}
func (s *signingTap) Broadcast(ctx context.Context, seat uint16, b []byte) error {
	err := s.Transport.Broadcast(ctx, seat, b)
	s.sent()
	return err
}
func (s *signingTap) SendPrivate(ctx context.Context, from, to uint16, b []byte) error {
	err := s.Transport.SendPrivate(ctx, from, to, b)
	s.sent()
	return err
}

type b3Operator struct {
	mu            sync.Mutex
	signingSends  int
	onSigningSend func()
	seats         []uint16
	config        store.Config
	root          *store.Store
	journal       *store.Scoped
	private       *operator.PrivateKey
	public        *operator.PublicKey
	network       net.Provider
	mover         *Mover
	planner       Planner
	recipes       *recipeEndpoint
	online        bool
	restarts      int
}

type b3World struct {
	t        *testing.T
	ctx      context.Context
	worker   snowfallengine.WorkerConfig
	domain   frost.Domain
	desc     dkg.Descriptor
	id       [32]byte
	script   bitcoin.Script
	chain    *chainFixture
	btc      *btcFixture
	hub      *recipeHub
	ops      []*b3Operator
	member   *group.MembershipValidator
	sequence byte
}

func b3WorkerConfig(t *testing.T) snowfallengine.WorkerConfig {
	t.Helper()
	path, pin := os.Getenv("SNOWFALL_WORKER"), os.Getenv("SNOWFALL_WORKER_SHA256")
	if path == "" || pin == "" {
		t.Fatal("batch 3 needs the pinned worker: set SNOWFALL_WORKER and SNOWFALL_WORKER_SHA256 (see TESTS-BATCH3.md)")
	}
	raw, err := hex.DecodeString(pin)
	if err != nil || len(raw) != 32 || !filepath.IsAbs(path) {
		t.Fatal("invalid worker pin")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(b); !bytes.Equal(sum[:], raw) {
		t.Fatal("worker hash mismatch")
	}
	copyPath := filepath.Join(t.TempDir(), "snowfall-pinned")
	if err = os.WriteFile(copyPath, b, 0700); err != nil {
		t.Fatal(err)
	}
	return snowfallengine.WorkerConfig{Path: copyPath, SHA256: [32]byte(raw)}
}

// newB3World runs a real DKG for the five-seat wallet, writes the wallet
// record (FIXTURE), and builds each operator's K-07 host. The target is Live
// with no main until a case adds one.
func newB3World(t *testing.T) *b3World {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	w := &b3World{t: t, ctx: ctx, worker: b3WorkerConfig(t), btc: newBTC(), hub: newHub()}
	w.domain = frost.Domain{Network: "b3/" + t.Name(), Chain: [32]byte{31: 1}, Registry: [20]byte{0xb3}, Epoch: 31}
	signer := local_v1.Connect(5, 3).Signing()
	addresses := make([]chain.Address, 5)
	var bootstrap []string
	for _, seats := range b3Topology {
		op := &b3Operator{seats: seats, online: true}
		var err error
		if op.private, op.public, err = operator.GenerateKeyPair(local_v1.DefaultCurve); err != nil {
			t.Fatal(err)
		}
		addr, err := signer.PublicKeyToAddress(op.public)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range seats {
			addresses[s-1] = addr
		}
		if op.network, err = libp2p.Connect(ctx, libp2p.Config{Peers: bootstrap}, op.private, firewall.Disabled, retransmission.NewTimeTicker(ctx, 50*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if bootstrap == nil {
			for _, a := range op.network.ConnectionManager().AddrStrings() {
				if strings.Contains(a, "/ip4/127.0.0.1/") {
					bootstrap = []string{a}
					break
				}
			}
		}
		op.config = store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{byte(len(w.ops) + 1)}, Key: [32]byte{42}, Fence: &fenceFixture{}}
		w.ops = append(w.ops, op)
	}
	w.member = group.NewMembershipValidator(&testutils.MockLogger{}, addresses, signer)
	for _, op := range w.ops {
		w.open(op)
	}
	keys := w.dkg()
	key := keys[0].Candidate
	members := make([]uint32, 5)
	operators := make([]common.Address, 5)
	for i := range members {
		members[i] = uint32(operatorOf(uint16(i+1)) + 1)
		operators[i] = common.HexToAddress(addresses[i].String())
	}
	w.desc = dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address(w.domain.Registry), Epoch: w.domain.Epoch, Members: members, Operators: operators, Threshold: b3Threshold, SnowfallDescriptor: key.Descriptor, OutputKey: key.OutputKey}
	if err := dkg.ValidateDescriptor(w.desc); err != nil {
		t.Fatal(err)
	}
	hash, err := dkg.DescriptorHash(w.desc)
	if err != nil {
		t.Fatal(err)
	}
	w.id = dkg.WalletID(w.desc.OutputKey)
	record := dkg.WalletRecord{Descriptor: w.desc, DescriptorHash: hash, ID: w.id, ResultHash: [32]byte{1}, ApprovalBlock: 1, ApprovalHash: common.Hash{2}, Deadline: 1 << 40, State: "ReadyUnfunded"}
	for _, op := range w.ops {
		if err = op.journal.SaveWallet(ctx, record); err != nil {
			t.Fatal("FIXTURE wallet record:", err)
		}
	}
	w.script, _ = bitcoin.PayToTaproot(w.desc.OutputKey)
	w.chain = &chainFixture{head: 40, wallet: Wallet{Descriptor: w.desc, ID: w.id, DescriptorHash: hash, State: StateLive, CapabilityVersion: 1}, outputs: map[[32]byte]redemption.CustodyOutput{}, incoming: map[[32]byte]Incoming{}, params: Parameters{MovingFundsTxMaxTotalFee: 5_000, MovingFundsDustThreshold: 20_000, MovedFundsSweepTxMaxTotalFee: 5_000, WalletMaxBtcTransfer: 1_000_000}, proven: map[[32]byte][32]byte{}}
	for _, op := range w.ops {
		w.host(op)
	}
	return w
}

func (w *b3World) open(op *b3Operator) {
	root, err := store.Open(w.ctx, op.config)
	if err != nil {
		w.t.Fatal(err)
	}
	op.root = root
	w.t.Cleanup(func() { root.Close() })
	if op.journal, err = root.Scope(w.domain); err != nil {
		w.t.Fatal(err)
	}
}

func (w *b3World) transport(op *b3Operator, a frost.Attempt, roster []uint16) (*transport.Transport, error) {
	return transport.New(w.ctx, transport.Config{Domain: w.domain, Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: op.seats, Roster: roster, LocalPublicKey: operator.MarshalUncompressed(op.public), Membership: w.member, Network: op.network})
}

func (w *b3World) dkg() []frost.KeyReady {
	t := w.t
	roster := []uint16{1, 2, 3, 4, 5}
	a, err := w.domain.NewAttempt("dkg", [32]byte{1}, 10)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]frost.KeyReady, len(w.ops))
	errs := make([]error, len(w.ops))
	var wg sync.WaitGroup
	for i, op := range w.ops {
		tr, err := w.transport(op, a, roster)
		if err != nil {
			t.Fatal(err)
		}
		engine, err := snowfallengine.NewGuarded(w.worker, op.journal)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(i int, op *b3Operator, tr *transport.Transport) {
			defer wg.Done()
			defer tr.Close()
			keys[i], errs[i] = engine.DKG(w.ctx, frost.DKGRequest{Group: frost.Group{Roster: roster, Threshold: b3Threshold, Epoch: w.domain.Epoch}, LocalSeats: op.seats, Attempt: a}, frost.Providers{Transport: tr, Acceptance: dkgAcceptance{}, SigningAuthorization: dkgAcceptance{}})
		}(i, op, tr)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("DKG operator %d: %v", i, err)
		}
		if keys[i].Candidate.OutputKey != keys[0].Candidate.OutputKey {
			t.Fatal("DKG candidate mismatch")
		}
	}
	return keys
}

// host builds the operator's K-07 host on its current store.
func (w *b3World) host(op *b3Operator) {
	view := &View{Chain: w.chain, Policy: Policy{FinalityBlocks: b3Finality}}
	journal := op.journal
	base := signing.Config{Journal: journal, Bitcoin: w.btc, Worker: w.worker, MinimumConfirmations: 1, LocalRegtest: true, Transport: func(ctx context.Context, a frost.Attempt) (frost.Transport, func(), error) {
		tr, err := w.transport(op, a, []uint16{1, 2, 3, 4, 5})
		if err != nil {
			return nil, nil, err
		}
		return &signingTap{Transport: tr, op: op}, tr.Close, nil
	}}
	op.mover = &Mover{Chain: w.chain, Authority: &Authority{View: view}, Lock: &deposit.MainLock{Journal: journal}, Ledger: redemption.Ledger{Journal: journal}, Schedule: redemption.Schedule{FinalityBlocks: b3Finality, RoundBlocks: b3Round}, Wallet: w.desc, Base: base}
	op.planner = Planner{View: view, Bitcoin: scriptUnspent{w.btc}, Wallet: w.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: 2, MaximumSatPerVByte: 20}
	op.recipes = w.hub.endpoint(w.index(op))
}

func (w *b3World) index(op *b3Operator) int {
	for i, o := range w.ops {
		if o == op {
			return i
		}
	}
	return -1
}

// restart is a process restart of operator i: the store is closed and
// reopened, and the host is rebuilt from it alone.
func (w *b3World) restart(i int) {
	op := w.ops[i]
	if err := op.root.Close(); err != nil {
		w.t.Fatal(err)
	}
	w.open(op)
	w.host(op)
	op.restarts++
}

// setOnline takes operator i down (it receives nothing and runs no step) or
// brings it back with a restart.
func (w *b3World) setOnline(i int, online bool) {
	op := w.ops[i]
	if op.online == online {
		return
	}
	op.online = online
	if !online {
		w.hub.offline(i)
		return
	}
	w.restart(i)
}

// ---------------------------------------------------------------- chain events

// addIncoming records a FROST incoming output of value to the target, from a
// confirmed Bitcoin transaction, with its log at block.
func (w *b3World) addIncoming(value int64, block uint64) [32]byte {
	w.sequence++
	prev := w.btc.fund(w.sequence, value, w.script)
	key := OutpointKey(prev.Outpoint)
	w.chain.mu.Lock()
	defer w.chain.mu.Unlock()
	w.chain.incoming[key] = Incoming{Target: w.id, Source: [32]byte{0x50, w.sequence}, Value: uint64(value), CreatedAt: block, State: IncomingPending, SweepFeeCap: 5_000}
	w.chain.logs = append(w.chain.logs, IncomingLog{Log: Log{Block: block, BlockHash: w.chain.hash(block)}, Outpoint: key, Target: w.id, Source: [32]byte{0x50, w.sequence}, Value: uint64(value), TransactionHash: [32]byte(prev.Outpoint.TransactionHash), OutputIndex: 0})
	return key
}

// setMain gives the target a confirmed custody main of value.
func (w *b3World) setMain(value int64) bitcoin.PreviousOutput {
	w.sequence++
	prev := w.btc.fund(w.sequence, value, w.script)
	w.chain.mu.Lock()
	defer w.chain.mu.Unlock()
	key := OutpointKey(prev.Outpoint)
	w.chain.custody = redemption.Custody{Generation: w.chain.custody.Generation + 1, StateVersion: w.chain.custody.StateVersion + 1, Value: uint64(value), MainOutpoint: key}
	w.chain.outputs[key] = redemption.CustodyOutput{WalletID: w.id, Value: uint64(value)}
	return prev
}

// spendMain is a FIXTURE redemption: a mined, proven transaction spends the
// target's main and leaves a new main of value. Any recipe that spent the
// old main can never confirm.
func (w *b3World) spendMain(value int64) {
	w.chain.mu.RLock()
	old := w.chain.custody.MainOutpoint
	w.chain.mu.RUnlock()
	var oldOutpoint *bitcoin.TransactionOutpoint
	w.btc.mu.Lock()
	for h := range w.btc.confirmed {
		tx := new(bitcoin.Transaction)
		if tx.Deserialize(w.btc.txs[h]) != nil {
			continue
		}
		for i := range tx.Outputs {
			o := bitcoin.TransactionOutpoint{TransactionHash: h, OutputIndex: uint32(i)}
			if OutpointKey(o) == old {
				oldOutpoint = &o
			}
		}
	}
	w.btc.mu.Unlock()
	if oldOutpoint == nil {
		w.t.Fatal("FIXTURE: no main to spend")
	}
	recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{0x77})
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: oldOutpoint, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: value, PublicKeyScript: w.script}, {Value: 1_000, PublicKeyScript: recipient}}}
	h := tx.Hash()
	w.btc.mu.Lock()
	if by, ok := w.btc.spent[*oldOutpoint]; ok {
		w.btc.mu.Unlock()
		w.t.Fatalf("FIXTURE: main already spent by %x", by)
	}
	w.btc.txs[h] = tx.Serialize(bitcoin.Standard)
	w.btc.confirmed[h] = true
	w.btc.spent[*oldOutpoint] = h
	w.btc.mu.Unlock()
	w.chain.mu.Lock()
	defer w.chain.mu.Unlock()
	o := w.chain.outputs[old]
	o.Spent = true
	w.chain.outputs[old] = o
	key := OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: h})
	w.chain.custody = redemption.Custody{Generation: w.chain.custody.Generation + 1, StateVersion: w.chain.custody.StateVersion + 1, Value: uint64(value), MainOutpoint: key}
	w.chain.outputs[key] = redemption.CustodyOutput{WalletID: w.id, Value: uint64(value)}
	w.chain.proven[[32]byte(h)] = w.id
}

// mine is the FIXTURE miner and SPV maintainer: every pooled transaction is
// mined; one that sweeps pending incoming outputs of the target into its
// script is proven, its inputs become Swept and output 0 the new main.
func (w *b3World) mine() {
	w.btc.mu.Lock()
	pool := w.btc.pool
	w.btc.pool = nil
	var txs []*bitcoin.Transaction
	for _, h := range pool {
		w.btc.confirmed[h] = true
		tx := new(bitcoin.Transaction)
		if tx.Deserialize(w.btc.txs[h]) == nil {
			txs = append(txs, tx)
		}
	}
	w.btc.mu.Unlock()
	w.chain.mu.Lock()
	defer w.chain.mu.Unlock()
	for _, tx := range txs {
		h := tx.Hash()
		swept := false
		for _, in := range tx.Inputs {
			k := OutpointKey(*in.Outpoint)
			if r, ok := w.chain.incoming[k]; ok && r.Target == w.id && (r.State == IncomingPending || r.State == IncomingEscalated) {
				r.State = IncomingSwept
				w.chain.incoming[k] = r
				swept = true
			}
			if k == w.chain.custody.MainOutpoint {
				o := w.chain.outputs[k]
				o.Spent = true
				w.chain.outputs[k] = o
			}
		}
		if !swept || len(tx.Outputs) == 0 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, w.script) {
			continue
		}
		key := OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: h})
		w.chain.custody = redemption.Custody{Generation: w.chain.custody.Generation + 1, StateVersion: w.chain.custody.StateVersion + 1, Value: uint64(tx.Outputs[0].Value), MainOutpoint: key}
		w.chain.outputs[key] = redemption.CustodyOutput{WalletID: w.id, Value: uint64(tx.Outputs[0].Value)}
		w.chain.proven[[32]byte(h)] = w.id
	}
}

// advance moves the head by blocks.
func (w *b3World) advance(blocks uint64) {
	w.chain.mu.Lock()
	w.chain.head += blocks
	w.chain.mu.Unlock()
}

func (w *b3World) head() uint64 {
	w.chain.mu.RLock()
	defer w.chain.mu.RUnlock()
	return w.chain.head
}

// round is the round whose anchor is final at the current head.
func (w *b3World) round() uint64 { return (w.head() - b3Finality) / b3Round }

// pending lists the target's incoming outputs not yet Swept.
func (w *b3World) pending() [][32]byte {
	w.chain.mu.RLock()
	defer w.chain.mu.RUnlock()
	var out [][32]byte
	for k, r := range w.chain.incoming {
		if r.State != IncomingSwept {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}

func (w *b3World) isProven(h [32]byte) bool {
	w.chain.mu.RLock()
	defer w.chain.mu.RUnlock()
	return w.chain.proven[h] != [32]byte{}
}

// conflicts returns every pair of distinct offered transactions that spend a
// common incoming output of the target: two different signed sweeps of the
// same inputs.
func (w *b3World) conflicts() [][2]bitcoin.Hash {
	w.btc.mu.Lock()
	defer w.btc.mu.Unlock()
	w.chain.mu.RLock()
	defer w.chain.mu.RUnlock()
	by := map[[32]byte][]bitcoin.Hash{}
	var out [][2]bitcoin.Hash
	for h, tx := range w.btc.broadcasts {
		for _, in := range tx.Inputs {
			k := OutpointKey(*in.Outpoint)
			if _, ok := w.chain.incoming[k]; !ok {
				continue
			}
			for _, other := range by[k] {
				out = append(out, [2]bitcoin.Hash{other, h})
			}
			by[k] = append(by[k], h)
		}
	}
	return out
}
