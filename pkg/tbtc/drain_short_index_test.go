package tbtc_test

// D6 (tentative: wait when the target index is short), in every mode after
// D16; review R6 on tuxxy/keep-core#98. Refs tuxxy/keep-core#34.
//
// Invariant: when the host's view of the FROST targets is readable but
// short (fewer eligible targets visible than the drain needs), the host waits:
// it makes no legacy commitment and no undersized drain attempt, although a
// legacy Live target is available. Once its view is complete, it commits the
// drain to every required target, ascending.
//
// The short view is a lagging log index (drainFaults.SetLogHorizon): the
// endpoint answers eth_getLogs successfully but omits the logs of blocks
// above a height, while every call sees the head. Two FROST targets are
// eligible on chain and the main needs two (walletMaxBtcTransfer below the
// main); the second target became ready above the horizon. A host that
// builds its index from logs sees one target; a host that does not sees both
// and must simply drain to both (the log reports how many logs were
// withheld).
//
// The first frost-read-faults cases (drain_producer_test.go) cover the other
// half of D6: an index that cannot be read at all.

import (
	"bytes"
	"context"
	"sort"
	"testing"
)

// TestDrainProducer_ShortIndex: see the file comment.
func TestDrainProducer_ShortIndex(t *testing.T) {
	for _, mode := range []uint8{modeFrostOnly, modeLegacyOpen, modeLegacyDraining} {
		setup := drainSetup{mode: mode, live: true, route: routeOpen, encoding: revertCode3,
			targets: 1, maxTransfer: drainMain/2 + 1, faults: true}
		t.Run(modeName(mode)+"-live1", func(t *testing.T) {
			drainShortIndexRound(t, setup)
		})
	}
}

func drainShortIndexRound(t *testing.T, setup drainSetup) {
	dw := newDrainWorld(t, setup)
	horizon, err := dw.admin.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	late := dw.createFrostTarget()
	dw.faults.SetLogHorizon(horizon)

	for window := 1; window <= 2; window++ {
		_, err := dw.window("moving-funds window with a short index")
		if legacy := dw.committedLegacy(); legacy != nil {
			t.Fatalf("short index (%s): window %d: with its target index short, the host committed legacy targets %s",
				setup, window, dw.labels(legacy))
		}
		if n := dw.commitmentAttempts(); n > 0 {
			t.Fatalf("short index (%s): window %d: with its target index short, the host attempted %d moving-funds "+
				"commitments (an undersized drain or a legacy commitment; window error: %v)", setup, window, n, err)
		}
	}
	t.Logf("short index (%s): the lagging log index withheld %d logs", setup, dw.faults.Hidden())

	// The index catches up: the host drains to both targets.
	dw.faults.SetLogHorizon(0)
	_, err = dw.window("moving-funds window with the index complete")
	commitments := dw.commitments()
	if len(commitments) != 1 {
		t.Fatalf("short index (%s): after the index completed, the host made no drain commitment (commitments %d, window error %v)",
			setup, len(commitments), err)
	}
	committed := commitments[0]
	want := [][20]byte{dw.targets[0].label, late.label}
	sort.Slice(want, func(i, j int) bool { return bytes.Compare(want[i][:], want[j][:]) < 0 })
	if len(committed) != 2 || committed[0] != want[0] || committed[1] != want[1] {
		t.Fatalf("short index (%s): after the index completed, the host committed %s, want both eligible targets %s",
			setup, dw.labels(committed), dw.labels(want))
	}
}
