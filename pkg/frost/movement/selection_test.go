package movement

// Signer selection per round (tuxxy/keep-core#66; review REVIEW-FIX-B3 B1).
// Invariant: whenever the live operators' seats reach the threshold, some
// round selects live operators only, so signing completes; with one operator
// absent, that round comes within one rotation cycle. Unit level on the
// mover's selection rule; no chain or worker.

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// selectionDescriptor gives operator i seatsPer[i] consecutive seats. Operator
// addresses ascend with i, so operator indexes are the address order.
func selectionDescriptor(seatsPer []int, threshold uint16, wallet byte) dkg.Descriptor {
	d := dkg.Descriptor{Threshold: threshold, OutputKey: [32]byte{0x66, wallet}}
	for op, count := range seatsPer {
		for j := 0; j < count; j++ {
			d.Members = append(d.Members, uint32(op+1))
			d.Operators = append(d.Operators, common.BigToAddress(big.NewInt(int64(op+1))))
		}
	}
	return d
}

// liveOnly reports whether round's selection avoids every down operator, and
// checks the selection is whole operators reaching the threshold.
func liveOnly(t *testing.T, d dkg.Descriptor, round uint64, down map[int]bool) bool {
	t.Helper()
	seats := selected(d, round)
	if len(seats) < int(d.Threshold) {
		t.Fatalf("round %d selects %d seats, below the threshold %d", round, len(seats), d.Threshold)
	}
	chosen := map[common.Address]int{}
	ok := true
	for _, seat := range seats {
		chosen[d.Operators[seat-1]]++
		if down[int(d.Members[seat-1])-1] {
			ok = false
		}
	}
	for i, operator := range d.Operators {
		if n, in := chosen[operator]; in {
			total := 0
			for _, other := range d.Operators {
				if other == operator {
					total++
				}
			}
			if n != total {
				t.Fatalf("round %d splits the seats of operator %d", round, d.Members[i])
			}
		}
	}
	return ok
}

func firstLiveOnly(t *testing.T, d dkg.Descriptor, from, rounds uint64, down map[int]bool) (uint64, bool) {
	t.Helper()
	for r := from; r < from+rounds; r++ {
		if liveOnly(t, d, r, down) {
			return r, true
		}
	}
	return 0, false
}

// TestSelectionReachesLiveSetWithNonAdjacentOperatorsDown (B1): two operators
// that are not neighbours in address order are down. A fixed rotation over the
// address order never selects only live operators; a round must.
func TestSelectionReachesLiveSetWithNonAdjacentOperatorsDown(t *testing.T) {
	for wallet := byte(0); wallet < 8; wallet++ {
		d := selectionDescriptor([]int{1, 1, 1, 1, 1}, 3, wallet)
		if _, ok := firstLiveOnly(t, d, 40, 100, map[int]bool{0: true, 2: true}); !ok {
			t.Fatalf("wallet %d: with operators 0 and 2 down (3 live seats, threshold 3) no round in 100 selects live operators only", wallet)
		}
	}
}

// TestSelectionReachesLiveSetAtProductionShape (B1 sanity): 100 seats, 20
// operators with 5 each, threshold 51. For every pair of operators down (90
// live seats), some round selects live operators only.
func TestSelectionReachesLiveSetAtProductionShape(t *testing.T) {
	seatsPer := make([]int, 20)
	for i := range seatsPer {
		seatsPer[i] = 5
	}
	d := selectionDescriptor(seatsPer, 51, 1)
	stalled := 0
	for a := 0; a < 20; a++ {
		for b := a + 1; b < 20; b++ {
			if _, ok := firstLiveOnly(t, d, 1000, 400, map[int]bool{a: true, b: true}); !ok {
				stalled++
			}
		}
	}
	if stalled > 0 {
		t.Fatalf("%d of 190 two-operator-down sets get no live-only selection in 400 rounds", stalled)
	}
}

// TestSelectionSingleOperatorDownWithinOneCycle: with one operator down and
// the rest reaching the threshold, a live-only round comes within any
// 2n-1 consecutive rounds (one whole rotation cycle), so the bound does not
// depend on the shuffle.
func TestSelectionSingleOperatorDownWithinOneCycle(t *testing.T) {
	for _, shape := range []struct {
		seatsPer  []int
		threshold uint16
	}{{[]int{2, 2, 1}, 3}, {[]int{1, 1, 1}, 2}, {[]int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5}, 26}} {
		n := uint64(len(shape.seatsPer))
		for wallet := byte(0); wallet < 4; wallet++ {
			d := selectionDescriptor(shape.seatsPer, shape.threshold, wallet)
			for down := range shape.seatsPer {
				for from := uint64(0); from < 3*n; from++ {
					if _, ok := firstLiveOnly(t, d, from, 2*n-1, map[int]bool{down: true}); !ok {
						t.Fatalf("shape %v wallet %d: operator %d down, no live-only round in rounds [%d, %d)", shape.seatsPer, wallet, down, from, from+2*n-1)
					}
				}
			}
		}
	}
}
