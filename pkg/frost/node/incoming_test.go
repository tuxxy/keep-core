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
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
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
func incomingRecipe(t *testing.T, fee int64) (dkg.WalletRecord, *signing.Plan) {
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
	plan, err := signing.NewIncomingSweepPlan(signing.IncomingSweepProposal{Wallet: desc, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Incoming: []bitcoin.PreviousOutput{input}, MaximumFee: 1000, MaximumSatPerVByte: 50, Transaction: tx})
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
