package node

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
	keepnet "github.com/keep-network/keep-core/pkg/net"
)

// A controlled independent fence; only public recipe records enter this test.
type incomingFence struct{ point store.Point }

func (f *incomingFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) { return f, nil }
func (f *incomingFence) Head(context.Context) (store.Point, error)                   { return f.point, nil }
func (f *incomingFence) Advance(_ context.Context, before, after store.Point) error {
	if f.point != before {
		return errors.New("fence mismatch")
	}
	f.point = after
	return nil
}
func (*incomingFence) Close() error { return nil }
func incomingRecipe(t *testing.T, fee int64, withMain ...bool) (dkg.WalletRecord, *signing.Plan) {
	t.Helper()
	key, err := crypto.ToECDSA(common.LeftPadBytes([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var q [32]byte
	key.X.FillBytes(q[:])
	desc := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(100123), Registry: common.Address{2}, Epoch: 9, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{4}, OutputKey: q}
	script, err := bitcoin.PayToTaproot(q)
	if err != nil {
		t.Fatal(err)
	}
	input := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{7}}, Value: 99000, PublicKeyScript: script}
	outpoint := input.Outpoint
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &outpoint, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 99000 - fee, PublicKeyScript: script}}}
	var main *bitcoin.PreviousOutput
	if len(withMain) > 0 && withMain[0] {
		m := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{8}}, Value: 50000, PublicKeyScript: script}
		main = &m
		tx.Inputs = append([]*bitcoin.TransactionInput{{Outpoint: &m.Outpoint, Sequence: 0xffffffff}}, tx.Inputs...)
		tx.Outputs[0].Value += m.Value
	}
	plan, err := signing.NewIncomingSweepPlan(signing.IncomingSweepProposal{Wallet: desc, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Main: main, Incoming: []bitcoin.PreviousOutput{input}, MaximumFee: 1000, MaximumSatPerVByte: 50, Transaction: tx})
	if err != nil {
		t.Fatal(err)
	}
	return dkg.WalletRecord{Descriptor: desc, ID: dkg.WalletID(q)}, plan
}
func TestIncomingAbandonmentSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	record, plan := incomingRecipe(t, 300)
	cfg := store.Config{Keystore: filepath.Join(t.TempDir(), "store"), StorageID: [32]byte{1}, Key: [32]byte{2}, Fence: &incomingFence{}}
	var chain [32]byte
	record.Descriptor.ChainId.FillBytes(chain[:])
	domain := frost.Domain{Network: "local-test", Chain: chain, Registry: [20]byte(record.Descriptor.Registry), Epoch: record.Descriptor.Epoch}
	open := func() (*store.Store, *wallet) {
		root, err := store.Open(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		journal, err := root.Scope(domain)
		if err != nil {
			t.Fatal(err)
		}
		return root, &wallet{record: record, journal: journal, mover: &movement.Mover{Ledger: redemption.Ledger{Journal: journal}}}
	}
	root, w := open()
	w.plan = plan
	if err := w.retainIncoming(ctx); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	root, w = open()
	if err := w.restoreIncoming(ctx); err != nil || w.plan == nil || !bytes.Equal(w.plan.IntentBytes(), plan.IntentBytes()) {
		t.Fatal("retained recipe changed", err)
	}
	w.request.LocalSeats = []uint16{1}
	if safe, err := w.canAbandonIncoming(ctx); err != nil || safe {
		t.Fatal("control LeaderRecipe assertion: advertised leader recipe became replaceable", err)
	}
	if abandoned, err := w.abandonIncoming(ctx); err != nil || abandoned {
		t.Fatal("control LeaderRecipe assertion: advertised leader recipe abandoned", err)
	}
	w.request.LocalSeats = []uint16{2}
	if abandoned, err := w.abandonIncoming(ctx); err != nil || !abandoned {
		t.Fatal("unsigned stale recipe not abandoned", err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	root, w = open()
	defer root.Close()
	if err := w.restoreIncoming(ctx); err != nil || w.plan != nil || w.incomingSlot != 1 {
		t.Fatal("control IncomingAbandon assertion: stale recipe restored after durable abandonment", err)
	}
	_, fresh := incomingRecipe(t, 400)
	w.plan = fresh
	if err := w.retainIncoming(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.mover.Ledger.Append(ctx, redemption.Entry{Reservation: movement.ID(fresh), Kind: redemption.KindPossibleRelease}); err != nil {
		t.Fatal(err)
	}
	if abandoned, err := w.abandonIncoming(ctx); err != nil || abandoned {
		t.Fatal("control IncomingRelease assertion: possibly released recipe was abandoned", err)
	}
	if w.plan != fresh || w.incomingSlot != 1 {
		t.Fatal("refused abandonment changed recipe")
	}
}

func TestIncomingRecipeEnforcesLocalCeiling(t *testing.T) {
	_, plan := incomingRecipe(t, 300)
	if err := validateIncomingCeiling(plan, 50); err != nil {
		t.Fatal("valid shared recipe refused", err)
	}
	if err := validateIncomingCeiling(plan, 1); err == nil {
		t.Fatal("control IncomingCeiling assertion: leader overrode local ceiling")
	}
	for _, raw := range [][]byte{[]byte("not-json"), make([]byte, (1<<20)+1)} {
		var proposal incomingProposal
		if proposal.Unmarshal(raw) == nil {
			t.Fatal("invalid public recipe accepted")
		}
	}
}

func TestFollowerRecipeCeilingCallSite(t *testing.T) {
	record, plan := incomingRecipe(t, 300)
	n := &Node{cfg: Config{MaximumSatPerVByte: 1}}
	authorized := false
	authorize := func(*signing.Plan) error { authorized = true; return nil }
	if accepted, err := n.acceptIncomingRecipe(record.Descriptor, plan.IntentBytes(), authorize); err == nil || !strings.Contains(err.Error(), "local fee ceiling") || accepted != nil || authorized {
		t.Fatal("control FollowerCeiling assertion: over-ceiling recipe reached authorization", err)
	}
	n.cfg.MaximumSatPerVByte = 50
	if accepted, err := n.acceptIncomingRecipe(record.Descriptor, plan.IntentBytes(), authorize); err != nil || accepted == nil || !authorized {
		t.Fatal("valid follower recipe refused", err)
	}
}

// Observe the journal at the actual publication boundary, then interrupt Send.
// Returning a Send error prevents the later duty-level retain from masking a
// deleted persist-before-send branch.
type recipeNetwork struct {
	keepnet.Provider
	channel keepnet.BroadcastChannel
}

func (n recipeNetwork) BroadcastChannelFor(string) (keepnet.BroadcastChannel, error) {
	return n.channel, nil
}

type recipeChannel struct {
	keepnet.BroadcastChannel
	send func(keepnet.TaggedMarshaler) error
}

func (c recipeChannel) SetFilter(keepnet.BroadcastChannelFilter) error  { return nil }
func (c recipeChannel) SetUnmarshaler(func() keepnet.TaggedUnmarshaler) {}
func (c recipeChannel) Recv(context.Context, func(keepnet.Message))     {}
func (c recipeChannel) Send(_ context.Context, m keepnet.TaggedMarshaler, _ ...keepnet.RetransmissionStrategy) error {
	return c.send(m)
}
func TestIncomingRecipePersistedBeforeSend(t *testing.T) {
	ctx := context.Background()
	record, plan := incomingRecipe(t, 300)
	root, err := store.Open(ctx, store.Config{Keystore: filepath.Join(t.TempDir(), "store"), StorageID: [32]byte{3}, Key: [32]byte{4}, Fence: &incomingFence{}})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var chain [32]byte
	record.Descriptor.ChainId.FillBytes(chain[:])
	journal, err := root.Scope(frost.Domain{Network: "local-test", Chain: chain, Registry: [20]byte(record.Descriptor.Registry), Epoch: record.Descriptor.Epoch})
	if err != nil {
		t.Fatal(err)
	}
	w := &wallet{record: record, journal: journal}
	w.request.LocalSeats = []uint16{1}
	interrupted := errors.New("interrupted at publication")
	sent := false
	ch := recipeChannel{send: func(message keepnet.TaggedMarshaler) error {
		sent = true
		raw, err := journal.ReadSigningRecord(ctx, incomingRecord(journal.Domain().ID(), 0, false))
		if err != nil || !bytes.Equal(raw, plan.IntentBytes()) {
			t.Fatal("control PersistBeforeSend assertion: recipe was published before its journal record", err)
		}
		recipe, ok := message.(*incomingProposal)
		if !ok || !bytes.Equal(recipe.Intent, raw) {
			t.Fatal("publication differs from durable recipe")
		}
		return interrupted
	}}
	n := &Node{network: recipeNetwork{channel: ch}}
	if _, err = n.agreedIncoming(ctx, w, movement.Planner{}, 10, plan); !errors.Is(err, interrupted) || !sent {
		t.Fatal("publication boundary not reached", err)
	}
}

type retirementChain struct {
	movement.Reader
	wallet       [32]byte
	spent, reorg bool
	hashes       int
}

func (c *retirementChain) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 100, Hash: [32]byte{1}}, nil
}
func (c *retirementChain) HeaderHash(context.Context, uint64) ([32]byte, error) {
	c.hashes++
	if c.reorg && c.hashes > 1 {
		return [32]byte{2}, nil
	}
	return [32]byte{1}, nil
}
func (c *retirementChain) Custody(context.Context, uint64, [32]byte) (redemption.Custody, error) {
	return redemption.Custody{MainOutpoint: [32]byte{99}}, nil
}
func (c *retirementChain) CustodyOutput(context.Context, uint64, [32]byte) (redemption.CustodyOutput, error) {
	return redemption.CustodyOutput{WalletID: c.wallet, Value: 50000, Spent: c.spent}, nil
}
func (c *retirementChain) Incoming(context.Context, uint64, [32]byte) (movement.Incoming, error) {
	return movement.Incoming{Target: c.wallet, State: movement.IncomingPending}, nil
}

type retirementBitcoin struct {
	signing.Bitcoin
	unspent bool
	err     error
}

func (b retirementBitcoin) ConfirmedUnspent(context.Context, bitcoin.TransactionOutpoint) (bool, error) {
	return b.unspent, b.err
}
func TestIncomingRetirementRequiresFinalizedSpentInput(t *testing.T) {
	ctx := context.Background()
	record, plan := incomingRecipe(t, 300, true)
	cfg := store.Config{Keystore: filepath.Join(t.TempDir(), "store"), StorageID: [32]byte{11}, Key: [32]byte{12}, Fence: &incomingFence{}}
	var chain [32]byte
	record.Descriptor.ChainId.FillBytes(chain[:])
	domain := frost.Domain{Network: "local-test", Chain: chain, Registry: [20]byte(record.Descriptor.Registry), Epoch: record.Descriptor.Epoch}
	root, err := store.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := root.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	view := &retirementChain{wallet: record.ID}
	w := &wallet{record: record, journal: journal, plan: plan}
	w.request.LocalSeats = []uint16{1}
	w.mover = &movement.Mover{Chain: view, Lock: &deposit.MainLock{Journal: journal}, Ledger: redemption.Ledger{Journal: journal}, Base: signing.Config{Bitcoin: retirementBitcoin{}}}
	if err = w.retainIncoming(ctx); err != nil {
		t.Fatal(err)
	}
	// Even a complete signature remains pinned until an incompatible spend is final.
	if err = w.mover.Ledger.Append(ctx, redemption.Entry{Reservation: movement.ID(plan), Kind: redemption.KindPossibleRelease}); err != nil {
		t.Fatal(err)
	}
	n := &Node{cfg: Config{FinalityBlocks: 2}}
	for _, test := range []struct {
		name                  string
		spent, unspent, reorg bool
		err                   error
	}{
		{name: "policy-only"}, {name: "Bitcoin-still-unspent", spent: true, unspent: true}, {name: "RPC-failure", spent: true, err: errors.New("RPC unavailable")}, {name: "reorg", spent: true, reorg: true},
	} {
		if !t.Run(test.name, func(t *testing.T) {
			view.spent, view.reorg, view.hashes = test.spent, test.reorg, 0
			w.mover.Base.Bitcoin = retirementBitcoin{unspent: test.unspent, err: test.err}
			if retired, _ := n.retireSpentIncoming(ctx, w); retired || w.plan != plan {
				t.Fatal("control SpentRecipe assertion: recipe retired without finalized incompatible input")
			}
		}) {
			return
		}
	}
	view.spent, view.reorg, view.hashes = true, false, 0
	w.mover.Base.Bitcoin = retirementBitcoin{}
	if retired, err := n.retireSpentIncoming(ctx, w); err != nil || !retired || w.plan != nil || w.incomingSlot != 1 {
		t.Fatal("spent recipe not retired", err)
	}
	if lock, err := w.mover.Lock.Current(ctx, record.ID, movement.OutpointKey(plan.MovementMain().Outpoint)); err != nil || lock.Purpose != deposit.Closed {
		t.Fatal("old main lock not closed", err)
	}
	if entries, err := w.mover.Ledger.Entries(ctx, movement.ID(plan)); err != nil || len(entries) != 1 || entries[0].Kind != redemption.KindPossibleRelease {
		t.Fatal("retirement erased release evidence", err)
	}
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	root, err = store.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	journal, err = root.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	reopened := &wallet{record: record, journal: journal}
	if err = reopened.restoreIncoming(ctx); err != nil || reopened.plan != nil || reopened.incomingSlot != 1 {
		t.Fatal("retired plan resurrected", err)
	}
}

type pinReader struct {
	movement.Reader
	record dkg.WalletRecord
	plan   *signing.Plan
}

func (c pinReader) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 100, Hash: [32]byte{1}}, nil
}
func (c pinReader) HeaderHash(context.Context, uint64) ([32]byte, error) { return [32]byte{1}, nil }
func (c pinReader) Wallet(context.Context, uint64, [20]byte) (movement.Wallet, error) {
	return movement.Wallet{ID: c.record.ID, State: movement.StateLive}, nil
}
func (c pinReader) Custody(context.Context, uint64, [32]byte) (redemption.Custody, error) {
	return redemption.Custody{}, nil
}
func (c pinReader) MovementParameters(context.Context, uint64) (movement.Parameters, error) {
	return movement.Parameters{MovedFundsSweepTxMaxTotalFee: 1000}, nil
}
func (c pinReader) IncomingLogs(context.Context, [32]byte, uint64, uint64) ([]movement.IncomingLog, error) {
	in := c.plan.IncomingOutputs()[0]
	return []movement.IncomingLog{{Log: movement.Log{Block: 10, BlockHash: [32]byte{1}}, Outpoint: movement.OutpointKey(in.Outpoint), Target: c.record.ID, Value: uint64(in.Value)}}, nil
}
func (c pinReader) Incoming(context.Context, uint64, [32]byte) (movement.Incoming, error) {
	return movement.Incoming{Target: c.record.ID, State: movement.IncomingPending, Value: uint64(c.plan.IncomingOutputs()[0].Value), SweepFeeCap: 1000}, nil
}
func TestCohortRecoversFollowerRecipeBeforeLeaderChoice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	record, pinned := incomingRecipe(t, 300)
	_, fresh := incomingRecipe(t, 400)
	root, err := store.Open(ctx, store.Config{Keystore: filepath.Join(t.TempDir(), "store"), StorageID: [32]byte{21}, Key: [32]byte{22}, Fence: &incomingFence{}})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var chain [32]byte
	record.Descriptor.ChainId.FillBytes(chain[:])
	journal, err := root.Scope(frost.Domain{Network: "local-test", Chain: chain, Registry: [20]byte(record.Descriptor.Registry), Epoch: record.Descriptor.Epoch})
	if err != nil {
		t.Fatal(err)
	}
	w := &wallet{record: record, journal: journal}
	w.request.LocalSeats = []uint16{2}
	w.mover = &movement.Mover{Authority: &movement.Authority{View: &movement.View{Chain: pinReader{record: record, plan: pinned}, Policy: movement.Policy{FinalityBlocks: 2}}}}
	n := &Node{cfg: Config{MaximumSatPerVByte: 50}}
	channel := recipeChannel{send: func(message keepnet.TaggedMarshaler) error {
		p := message.(*incomingProposal)
		if !p.Announcement || p.Seat != 2 || len(p.Intent) != 0 {
			t.Fatal("leader falsely announced a pin")
		}
		return nil
	}}
	received := make(chan *incomingProposal, 1)
	received <- &incomingProposal{Seat: 3, Intent: pinned.IntentBytes()}
	plan, mustMatch, err := n.cohortIncoming(ctx, w, []uint16{2, 3}, 100, fresh, channel, received)
	if err != nil || !mustMatch || plan == nil || plan.Commitment() != pinned.Commitment() {
		t.Fatal("control FollowerPin assertion: leader discarded follower's pinned recipe", err)
	}
	if w.plan != nil {
		t.Fatal("choice bypassed persist-before-send")
	}
}
