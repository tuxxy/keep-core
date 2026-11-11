package redemption

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tecdsa/retry"
)

// TestAgreeRefusesEachMissingInput (A-K04-AGREED-EXECUTOR-CONFIG): no agreed
// configuration, and so no signing session, exists unless the finality
// depth, the round length, the admission block and its hash and at least one
// seat are all set. Each missing input is refused on its own, by Agree and by
// AgreeRound, without a crash.
func TestAgreeRefusesEachMissingInput(t *testing.T) {
	defer guard(t)
	s := Schedule{FinalityBlocks: 2, RoundBlocks: 10}
	adm := Admission{ReservationID: [32]byte{1}, TransactionHash: [32]byte{2}, Block: 50, BlockHash: [32]byte{3}}
	seats := []uint16{1, 3}
	for _, tc := range []struct {
		name  string
		s     Schedule
		adm   Admission
		seats []uint16
	}{
		{"zero finality", Schedule{RoundBlocks: 10}, adm, seats},
		{"zero round length", Schedule{FinalityBlocks: 2}, adm, seats},
		{"zero admission block", s, Admission{ReservationID: adm.ReservationID, TransactionHash: adm.TransactionHash, BlockHash: adm.BlockHash}, seats},
		{"zero admission hash", s, Admission{ReservationID: adm.ReservationID, TransactionHash: adm.TransactionHash, Block: adm.Block}, seats},
		{"no seats", s, adm, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			noPanic(t, "Agree", func() {
				if a, err := Agree(tc.s, 1, [20]byte{4}, signing.Redemption, adm.ReservationID, tc.adm, tc.seats, 75); err == nil {
					t.Fatalf("Agree accepted incomplete inputs: %+v", a)
				}
			})
			noPanic(t, "AgreeRound", func() {
				if a, err := AgreeRound(tc.s, 1, [20]byte{4}, signing.Redemption, adm.ReservationID, tc.adm, tc.seats, 2); err == nil {
					t.Fatalf("AgreeRound accepted incomplete inputs: %+v", a)
				}
			})
		})
	}
	t.Run("positive control", func(t *testing.T) {
		defer guard(t)
		if _, err := Agree(s, 1, [20]byte{4}, signing.Redemption, adm.ReservationID, adm, seats, 75); err != nil {
			t.Fatal(err)
		}
		if _, err := AgreeRound(s, 1, [20]byte{4}, signing.Redemption, adm.ReservationID, adm, seats, 2); err != nil {
			t.Fatal(err)
		}
	})
}

// TestSelectSeatsEligibility (ADV-D2, KC-SF-V3 4.2): only a certified seat
// that holds a member and has an operator is ever selected, an ineligible
// seat anywhere in the descriptor does not stop the scan, and a certificate
// whose eligible seats exactly reach the threshold still selects signers.
func TestSelectSeatsEligibility(t *testing.T) {
	defer guard(t)
	a, b, c := common.Address{1}, common.Address{2}, common.Address{3}
	t.Run("empty member and missing operator never selected", func(t *testing.T) {
		defer guard(t)
		// Seat 2 holds no member; seat 4 has no operator entry.
		d := dkg.Descriptor{Members: []uint32{1, 0, 3, 4}, Operators: []common.Address{a, b, c}, Threshold: 2, OutputKey: [32]byte{9}}
		for round := uint64(0); round < 6; round++ {
			var seats []uint16
			noPanic(t, "SelectSeats", func() { seats = SelectSeats(d, []uint16{1, 2, 3, 4}, round) })
			if fmt.Sprint(seats) != "[1 3]" {
				t.Fatalf("round %d: eligible seats exactly at the threshold gave %v, want [1 3]", round, seats)
			}
		}
	})
	t.Run("uncertified first seat does not end the scan", func(t *testing.T) {
		defer guard(t)
		d := dkg.Descriptor{Members: []uint32{1, 2, 3}, Operators: []common.Address{a, b, c}, Threshold: 2, OutputKey: [32]byte{9}}
		if seats := SelectSeats(d, []uint16{2, 3}, 0); fmt.Sprint(seats) != "[2 3]" {
			t.Fatalf("certified seats 2 and 3 gave %v, want [2 3]", seats)
		}
	})
}

// baseSeats is base keep-core's qualified-operator step
// (tecdsa/retry.EvaluateRetryParticipantsForSigning) over the certified,
// operated seats, mapped back to seat numbers. SelectSeats documents that it
// chooses operators as base does: at the first round of each rotation cycle
// it starts from the first shuffled operator, so the two must agree there.
func baseSeats(t *testing.T, d dkg.Descriptor, certified []uint16, cycle uint64) []uint16 {
	t.Helper()
	var members []chain.Address
	var seats []uint16
	for i, m := range d.Members {
		seat := uint16(i + 1)
		eligible := false
		for _, s := range certified {
			eligible = eligible || s == seat
		}
		if m == 0 || i >= len(d.Operators) || !eligible {
			continue
		}
		// Lower-case fixed-width hex orders as the address bytes do.
		members = append(members, chain.Address(hex.EncodeToString(d.Operators[i][:])))
		seats = append(seats, seat)
	}
	id := dkg.WalletID(d.OutputKey)
	chosen, err := retry.EvaluateRetryParticipantsForSigning(members, int64(binary.BigEndian.Uint64(id[:8])), uint(cycle), uint(d.Threshold))
	if err != nil {
		t.Fatal(err)
	}
	var out []uint16
	for i, m := range members {
		for _, c := range chosen {
			if c == m {
				out = append(out, seats[i])
				break
			}
		}
	}
	return out
}

// TestSelectSeatsMatchesBaseRetrySelection (ADV-D2): at the first round of
// every rotation cycle the payment signers are the seats of the operators
// base keep-core qualifies for that retry count (operators sorted by
// address, shuffled with the wallet ID plus the cycle, whole operators taken
// until the threshold is reached); within a cycle every operator starts a
// round exactly once. Two documented differences from base are not tested
// here: base's signing loop then trims the qualified members to exactly
// HonestThreshold with a second shuffle (pkg/tbtc signing_loop
// excludedMembersIndexes), while SelectSeats keeps whole operators and may
// return more than the threshold (FROST signs with at least t); and base
// reshuffles on every retry, while SelectSeats rotates the start operator
// over one shuffle within a cycle. Base also seeds with the message digest,
// SelectSeats with the wallet ID (ADV-D2).
func TestSelectSeatsMatchesBaseRetrySelection(t *testing.T) {
	defer guard(t)
	ops := []common.Address{{0x51}, {0x0c}, {0xe7}, {0x33}, {0x9a}, {0x21}, {0xb4}}
	for _, tc := range []struct {
		name      string
		operators []int // descriptor seat -> index in ops
		threshold uint16
		certified []uint16
	}{
		{"one seat each, threshold 3", []int{0, 1, 2, 3, 4}, 3, []uint16{1, 2, 3, 4, 5}},
		{"multi-seat operators", []int{0, 1, 1, 2, 3, 3, 3, 4, 5}, 5, []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{"partial certificate", []int{6, 5, 4, 3, 2, 1, 0}, 4, []uint16{1, 2, 4, 5, 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			for key := byte(1); key <= 4; key++ {
				d := dkg.Descriptor{Threshold: tc.threshold, OutputKey: [32]byte{key, 0x77}}
				for i, o := range tc.operators {
					d.Members = append(d.Members, uint32(i+1))
					d.Operators = append(d.Operators, ops[o])
				}
				operators := map[common.Address]bool{}
				for _, s := range tc.certified {
					operators[d.Operators[s-1]] = true
				}
				n := uint64(len(operators))
				for cycle := uint64(0); cycle < 4; cycle++ {
					got := SelectSeats(d, tc.certified, cycle*n)
					if want := baseSeats(t, d, tc.certified, cycle); fmt.Sprint(got) != fmt.Sprint(want) {
						t.Fatalf("key %d cycle %d: selected %v, base retry selects %v", key, cycle, got, want)
					}
				}
			}
		})
	}
	t.Run("every operator starts once per cycle", func(t *testing.T) {
		defer guard(t)
		d := dkg.Descriptor{Threshold: 1, OutputKey: [32]byte{0x42}}
		var certified []uint16
		for i, o := range ops {
			d.Members = append(d.Members, uint32(i+1))
			d.Operators = append(d.Operators, o)
			certified = append(certified, uint16(i+1))
		}
		n := uint64(len(ops))
		for cycle := uint64(0); cycle < 3; cycle++ {
			starts := map[uint16]bool{}
			for j := uint64(0); j < n; j++ {
				seats := SelectSeats(d, certified, cycle*n+j)
				if len(seats) != 1 {
					t.Fatalf("threshold 1 selected %v", seats)
				}
				starts[seats[0]] = true
			}
			if len(starts) != int(n) {
				t.Fatalf("cycle %d: %d distinct start operators, want %d", cycle, len(starts), n)
			}
		}
	})
}
