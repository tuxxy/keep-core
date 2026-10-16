package deposit

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

type directives map[[32]byte]redemption.Directive

func (d directives) Directive(_ context.Context, r [32]byte) (redemption.Directive, bool, error) {
	out, ok := d[r]
	return out, ok, nil
}

// A contained reservation gets no resolution attestation without a
// governance cancel directive, and the main lock does not move.
func TestResolutionNeedsDirectiveWhenContained(t *testing.T) {
	w := newWorld(t)
	main := w.fund(1_000_000)
	r := [32]byte{0x99}
	w.chain.custody.ActiveReservation = r
	key := w.deposit(1, 100_000, 90, [20]byte{})
	plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
	if err != nil {
		t.Fatal(err)
	}
	descriptorHash := plan.DescriptorHash()
	w.chain.intents[r] = redemption.Intent{ID: r, State: redemption.IntentQuarantined, Certificate: redemption.Certificate{WalletID: w.id, DescriptorHash: descriptorHash, ConflictTxHash: [32]byte(main.Outpoint.TransactionHash), ConflictOutputIndex: main.Outpoint.OutputIndex}}
	if err = w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 200); err != nil {
		t.Fatal(err)
	}
	a := &ResolutionAttester{Chain: w.chain, Lock: w.lock, Authority: &Authority{View: w.view}, Key: testECDSA(t), Seats: []uint16{1}}
	if _, _, err = a.Attest(ctx, plan, 150); !errors.Is(err, redemption.ErrContained) {
		t.Fatal("resolution attested for a contained reservation:", err)
	}
	a.Directives = directives{r: {Reservation: r, Class: "signer-integrity", Action: redemption.DirectiveRebroadcast}}
	if _, _, err = a.Attest(ctx, plan, 150); !errors.Is(err, redemption.ErrContained) {
		t.Fatal("a rebroadcast directive allowed a resolution:", err)
	}
	if current, _ := w.lock.Current(ctx, w.id, OutpointKey(main.Outpoint)); current.Purpose != Redemption {
		t.Fatal("refused attestation moved the lock")
	}
	a.Directives = directives{r: {Reservation: r, Class: "signer-integrity", Action: redemption.DirectiveCancel}}
	if _, _, err = a.Attest(ctx, plan, 150); err != nil {
		t.Fatal("cancel directive refused:", err)
	}
	if current, _ := w.lock.Current(ctx, w.id, OutpointKey(main.Outpoint)); current.Purpose != Resolution {
		t.Fatal("directed resolution did not move the lock")
	}
}

func testECDSA(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Review finding: a resolution plan whose deposit is no longer sweepable is
// refused before the lock moves, and a resolution can be replaced by an
// alternative of the same reservation after its deposit lapses.
func TestResolutionValidatesAndCanBeReplaced(t *testing.T) {
	w := newWorld(t)
	main := w.fund(1_000_000)
	r := [32]byte{0x99}
	w.chain.custody.ActiveReservation = r
	mtp := w.clock.mtp
	w.locktime = uint32(mtp + 10_000)
	stale := w.deposit(1, 100_000, 90, [20]byte{})
	w.locktime = uint32(mtp + 20_000)
	lapsing := w.deposit(2, 100_000, 90, [20]byte{})
	w.locktime = 0
	fresh := w.deposit(3, 100_000, 90, [20]byte{})
	build := func(key [32]byte) *signing.Plan {
		p, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first, second, third := build(stale), build(lapsing), build(fresh)
	w.chain.intents[r] = redemption.Intent{ID: r, State: redemption.IntentReserved, Certificate: redemption.Certificate{WalletID: w.id, DescriptorHash: first.DescriptorHash(), ConflictTxHash: [32]byte(main.Outpoint.TransactionHash), ConflictOutputIndex: main.Outpoint.OutputIndex}}
	if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, r, 200); err != nil {
		t.Fatal(err)
	}
	a := &ResolutionAttester{Chain: w.chain, Lock: w.lock, Authority: &Authority{View: w.view}, Key: testECDSA(t), Seats: []uint16{1}}
	w.clock.set(mtp + 10_000 - 3_600) // the first deposit reaches its margin
	if _, _, err := a.Attest(ctx, first, 150); !errors.Is(err, ErrRefundMargin) {
		t.Fatal("stale resolution attested:", err)
	}
	if current, _ := w.lock.Current(ctx, w.id, OutpointKey(main.Outpoint)); current.Purpose != Redemption {
		t.Fatal("a refused resolution moved the lock")
	}
	if _, _, err := a.Attest(ctx, second, 150); err != nil {
		t.Fatal(err)
	}
	w.clock.set(mtp + 20_000 - 3_600) // the resolved deposit lapses
	if _, _, err := a.Attest(ctx, third, 150); err != nil {
		t.Fatal("replacement resolution refused:", err)
	}
	guard := Guard{Lock: w.lock}
	for _, p := range []*signing.Plan{second, third} {
		if err := guard.Permit(ctx, p, SweepID(p)); err != nil {
			t.Fatal("resolution alternative not recorded:", err)
		}
	}
	if err := guard.Permit(ctx, first, SweepID(first)); err == nil {
		t.Fatal("a refused resolution is permitted")
	}
}

// Review finding: a provider error while authorizing is not a refusal; the
// sweeper reports it and keeps the plan.
func TestSweeperKeepsPlanOnReadError(t *testing.T) {
	w := newWorld(t)
	key := w.deposit(1, 100_000, 90, [20]byte{})
	plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Sweeper{Chain: w.chain, Planner: w.planner, Authority: &Authority{View: w.view}, Lock: w.lock, Ledger: redemption.Ledger{Journal: newJournal()}, Schedule: redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}}
	w.chain.revealErr = errors.New("rpc unavailable")
	if status, err := s.Resolve(ctx, plan); err == nil || status == StatusAbandoned {
		t.Fatal("a read error abandoned the plan:", status, err)
	}
	w.chain.revealErr = nil
	record := w.chain.records[key]
	record.SweptAt = 0
	record.Terms.RefundLocktime = uint32(w.clock.mtp + 10)
	w.chain.records[key] = record
	if status, err := s.Resolve(ctx, plan); err != nil || status != StatusAbandoned {
		t.Fatal("a terminal refusal must abandon:", status, err)
	}
}

// Review finding: a resolution attested while Reserved cannot be signed or
// broadcast after the wallet is contained unless a cancel directive exists;
// the authority rereads the reservation at every authorization.
func TestResolutionSweepNeedsDirectiveAfterContainment(t *testing.T) {
	w := newWorld(t)
	main := w.fund(1_000_000)
	r := [32]byte{0x99}
	w.chain.custody.ActiveReservation = r
	key := w.deposit(1, 100_000, 90, [20]byte{})
	plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
	if err != nil {
		t.Fatal(err)
	}
	w.chain.intents[r] = redemption.Intent{ID: r, State: redemption.IntentReserved, Certificate: redemption.Certificate{WalletID: w.id, DescriptorHash: plan.DescriptorHash(), ConflictTxHash: [32]byte(main.Outpoint.TransactionHash), ConflictOutputIndex: main.Outpoint.OutputIndex}}
	authority := &Authority{View: w.view}
	if _, err = authority.AuthorizeSweep(ctx, plan); err != nil {
		t.Fatal("resolution sweep of a reserved reservation:", err)
	}
	intent := w.chain.intents[r]
	intent.State = redemption.IntentQuarantined
	w.chain.intents[r] = intent
	if _, err = authority.AuthorizeSweep(ctx, plan); !errors.Is(err, redemption.ErrContained) {
		t.Fatal("contained resolution sweep authorized without a directive:", err)
	}
	authority.Directives = directives{r: {Reservation: r, Class: "issuance-fault", Action: redemption.DirectiveRebroadcast}}
	if _, err = authority.AuthorizeSweep(ctx, plan); !errors.Is(err, redemption.ErrContained) {
		t.Fatal("a rebroadcast directive authorized a resolution sweep:", err)
	}
	authority.Directives = directives{r: {Reservation: r, Class: "issuance-fault", Action: redemption.DirectiveCancel}}
	if _, err = authority.AuthorizeSweep(ctx, plan); err != nil {
		t.Fatal("cancel directive refused:", err)
	}
	intent.State = redemption.IntentPaid
	w.chain.intents[r] = intent
	if _, err = authority.AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
		t.Fatal("resolution sweep of a resolved reservation:", err)
	}
}
