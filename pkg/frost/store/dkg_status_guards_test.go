package store

// Mutation batch M10-dkg: DKG status reconstruction guards (dkg_status.go).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

// Invariant: a claimed DKG of the largest supported group (100 seats) is
// reported like any other; the 100-seat bound is inclusive. Row
// dkg_status.go:72.
func TestDKGStatusAcceptsFullWidthRoster(t *testing.T) {
	_, v := openTest(t, testConfig(t))
	request, _ := dkgFixture(t, v.Domain())
	request.Group.Roster = nil
	for seat := uint16(1); seat <= 100; seat++ {
		request.Group.Roster = append(request.Group.Roster, seat)
	}
	request.Group.Threshold = 51
	id := [32]byte{0x64}
	claimDKG(t, v, id, request)
	status, err := v.DKGStatus(context.Background())
	if err != nil || status.State != frost.DKGInProgress || status.Attempt != id {
		t.Fatalf("a claimed 100-seat DKG was not reported: %+v %v", status, err)
	}
}

// Invariant: the DKG pointer of a domain must name exactly one 32-byte
// attempt ID in hex; any other pointer quarantines the store, even when an
// authenticated claim is filed under it. The pointer and claim are a TEST
// FIXTURE committed directly (a correct store never writes them). Row
// dkg_status.go:50.
func TestDKGStatusRefusesMalformedAttemptPointer(t *testing.T) {
	for name, key := range map[string]string{
		"short":        strings.Repeat("ab", 31),
		"long":         strings.Repeat("ab", 33),
		"invalid-tail": strings.Repeat("ab", 32) + "zz",
	} {
		t.Run(name, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			request, _ := dkgFixture(t, v.Domain())
			intent, err := json.Marshal(request)
			must(t, err)
			header := append([]byte("keep-core/snowfall/claim/v1/"+key), v.domain.Bytes()...)
			raw, err := s.seal(intent, header)
			must(t, err)
			next := clone(s.state)
			next.Claims[key] = raw
			next.DKGs[v.prefix()] = key
			commitFixture(t, s, next)
			if status, err := v.DKGStatus(context.Background()); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("DKG status read through a %s attempt pointer: %+v %v", name, status, err)
			}
		})
	}
}
