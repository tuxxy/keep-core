package tbtc

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// TestFrostHeartbeatSeatsRotate (review B2, batch 7): with a readiness
// certificate larger than the threshold (production ReadySeats), the
// heartbeat signers are a rotated threshold of whole operators, so with one
// certified operator absent some window's heartbeat is signed by present
// operators only and ends a failure streak. Signing with every certified
// seat would leave every such heartbeat unsigned.
func TestFrostHeartbeatSeatsRotate(t *testing.T) {
	a, b, c := common.Address{1}, common.Address{2}, common.Address{3}
	descriptor := dkg.Descriptor{
		Members:   []uint32{1, 2, 3, 4, 5},
		Operators: []common.Address{a, a, b, b, c},
		Threshold: 3,
		OutputKey: [32]byte{7},
	}
	certified := []uint16{1, 2, 3, 4, 5} // ReadySeats 5 > threshold 3
	withoutC := false
	for window := uint64(1); window <= 6; window++ {
		seats := frostHeartbeatSeats(descriptor, certified, window*coordinationFrequencyBlocks)
		if len(seats) < int(descriptor.Threshold) {
			t.Fatalf("window %d: heartbeat seats %v below the threshold", window, seats)
		}
		hasC := false
		for _, seat := range seats {
			hasC = hasC || seat == 5
		}
		withoutC = withoutC || !hasC
		if again := frostHeartbeatSeats(descriptor, certified, window*coordinationFrequencyBlocks); fmt.Sprint(again) != fmt.Sprint(seats) {
			t.Fatalf("window %d: heartbeat seats are not deterministic", window)
		}
	}
	if !withoutC {
		t.Fatal("B2: with operator c (seat 5) absent, no window's heartbeat can be signed by the present operators alone")
	}
	// A threshold-sized certificate still signs with all of it.
	if seats := frostHeartbeatSeats(descriptor, []uint16{1, 2, 5}, coordinationFrequencyBlocks); fmt.Sprint(seats) != "[1 2 5]" {
		t.Fatalf("threshold-sized certificate: heartbeat seats %v, want [1 2 5]", seats)
	}
}
