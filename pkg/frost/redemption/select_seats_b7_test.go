package redemption

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// TestSelectSeatsRotatesOverCertifiedSeats (ADV-D2): payment signers rotate
// over whole operators of the readiness-certified seats, so every set of
// online operators whose certified seats reach the threshold signs in some
// round of every cycle; a seat outside the certificate is never selected.
func TestSelectSeatsRotatesOverCertifiedSeats(t *testing.T) {
	a, b, c := common.Address{1}, common.Address{2}, common.Address{3}
	d := dkg.Descriptor{Members: []uint32{1, 2, 3, 4}, Operators: []common.Address{a, b, c, c}, Threshold: 2, OutputKey: [32]byte{9}}
	certified := []uint16{1, 2, 3}
	seen := map[string]bool{}
	for round := uint64(0); round < 6; round++ {
		seats := SelectSeats(d, certified, round)
		if len(seats) < int(d.Threshold) {
			t.Fatalf("round %d: %v below the threshold", round, seats)
		}
		for _, s := range seats {
			if s == 4 {
				t.Fatalf("round %d: uncertified seat 4 selected", round)
			}
		}
		if again := SelectSeats(d, certified, round); fmt.Sprint(again) != fmt.Sprint(seats) {
			t.Fatalf("round %d: selection is not deterministic", round)
		}
		seen[fmt.Sprint(seats)] = true
	}
	// Operators b and c online (seats 2 and 3) must sign in some round.
	if !seen["[2 3]"] {
		t.Fatalf("operators b and c never selected together: %v", seen)
	}
	if SelectSeats(d, []uint16{1}, 0) != nil {
		t.Fatal("a certificate below the threshold selected signers")
	}
}

// TestRebuildAcceptsOnlyTheAdmittedPlan (ADV-D2): a member with no retained
// plan rebuilds the admitted one from chain state; it is accepted only if
// its commitment is the admitted intent commitment.
func TestRebuildAcceptsOnlyTheAdmittedPlan(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
	if err != nil {
		t.Fatal(err)
	}
	intent := Intent{AdmittedBlock: w.chain.head + 1, RequestIDs: w.ids, Certificate: Certificate{IntentCommitment: plan.Commitment()}}
	for _, r := range plan.Requests() {
		intent.Payouts = append(intent.Payouts, uint64(r.Value))
	}
	// The rebuilding member sizes fees differently: the fee comes from the
	// admitted payouts, not from its own rate.
	rebuilder := w.planner
	rebuilder.Fees.FeeRate = 1
	rebuilt, err := rebuilder.Rebuild(ctx, intent)
	if err != nil {
		t.Fatal("the admitted plan was not rebuilt:", err)
	}
	if rebuilt.Commitment() != plan.Commitment() {
		t.Fatal("the rebuilt plan differs from the admitted one")
	}

	// Chain state that implies another transaction is refused.
	other := intent
	other.Payouts = append([]uint64(nil), intent.Payouts...)
	other.Payouts[0]--
	if _, err := rebuilder.Rebuild(ctx, other); err == nil {
		t.Fatal("a plan with other payouts than the admitted ones was accepted")
	}
	wrong := intent
	wrong.Certificate.IntentCommitment[0] ^= 1
	if _, err := rebuilder.Rebuild(ctx, wrong); !errors.Is(err, ErrRebuildMismatch) {
		t.Fatal("a plan whose commitment is not the admitted one was accepted:", err)
	}
}
