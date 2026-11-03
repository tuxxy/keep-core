package integration

// Test setup (no assertions): the default K-05/K-06/K-07 worlds certify every
// seat, as base keep-core's normal path puts every member in the final
// signing group. Only the #86 tests (seat_eligibility_test.go) keep a seat
// outside the readiness certificate on purpose. Refs tuxxy/keep-core#86.

import (
	"context"
	"fmt"
	"slices"
)

// certifyAllSeats holds every executor's readiness certificate, merges their
// per-seat attestations into one certificate over every seat, and submits it
// through operator 1's real registry adapter before any executor submits its
// own. The executors then observe the certified wallet. A failure is reported
// on the test; the DKG then fails the world's own checks.
func certifyAllSeats(f *fixture) {
	chain := f.executors[0].Chain
	pending, release := f.gateReadiness()
	go func() {
		defer close(release)
		type vote struct {
			reference [32]byte
			signature []byte
		}
		votes := map[uint16]vote{}
		var id [32]byte
		for range f.executors {
			select {
			case c := <-pending:
				id = c.id
				for i, seat := range c.seats {
					votes[seat] = vote{c.references[i], c.signatures[i*65 : (i+1)*65]}
				}
			case <-f.ctx.Done():
				f.t.Error("harness: readiness certificates not collected:", f.ctx.Err())
				return
			}
		}
		var seats []uint16
		var references [][32]byte
		var signatures []byte
		for seat := uint16(1); int(seat) <= len(f.deployment.Pattern); seat++ {
			v, ok := votes[seat]
			if !ok {
				f.t.Errorf("harness: no readiness attestation for seat %d", seat)
				return
			}
			seats = append(seats, seat)
			references = append(references, v.reference)
			signatures = append(signatures, v.signature...)
		}
		if err := chain.Ready(context.WithoutCancel(f.ctx), id, seats, references, signatures); err != nil {
			f.t.Error("harness: certificate over every seat:", fmt.Errorf("%v: %w", slices.Clone(seats), err))
		}
	}()
}
