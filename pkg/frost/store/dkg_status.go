package store

import (
	"context"
	"encoding/hex"
	"slices"

	"github.com/keep-network/keep-core/pkg/frost"
)

// DKGStatus reconstructs a terminal lost-seat marker from the immutable claim.
// Reopening never restores live ownership. A missing or uncertain claim is an
// error, not evidence that this operator has no lost seats. The caller supplies
// the original deployment/epoch domain; this is not a wallet discovery API.
func (v *Scoped) DKGStatus(ctx context.Context) (frost.DKGStatus, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return frost.DKGStatus{}, err
	}
	request, attempt, err := v.dkgRequest()
	if err != nil {
		return frost.DKGStatus{}, err
	}
	status := frost.DKGStatus{Domain: v.domain, Attempt: attempt,
		LocalSeats: slices.Clone(request.LocalSeats)}
	if _, ok := s.state.Keys[v.prefix()]; ok {
		status.State = frost.DKGKeyStored
	} else if s.liveDKGs[v.prefix()] == hex.EncodeToString(attempt[:]) {
		status.State = frost.DKGInProgress
	} else {
		status.State = frost.DKGSeatsLost
		status.LostSeats = slices.Clone(status.LocalSeats)
	}
	return status, nil
}

// Caller holds the store mutex. This reads only the host's public request,
// never an opaque candidate or completion record.
func (v *Scoped) dkgRequest() (frost.DKGRequest, [32]byte, error) {
	s := v.store
	var request frost.DKGRequest
	var attempt [32]byte
	key, ok := s.state.DKGs[v.prefix()]
	if !ok {
		return request, attempt, ErrMissing
	}
	id, err := hex.DecodeString(key)
	if err != nil || len(id) != len(attempt) {
		return request, attempt, s.fail(ErrQuarantined)
	}
	copy(attempt[:], id)
	raw, ok := s.state.Claims[key]
	if !ok || attempt == ([32]byte{}) {
		return request, attempt, s.fail(ErrQuarantined)
	}
	header := append([]byte("keep-core/snowfall/claim/v1/"+key), v.domain.Bytes()...)
	plain, err := s.open(raw, header)
	if err != nil {
		return request, attempt, s.fail(err)
	}
	defer clear(plain)
	if err = decode(plain, &request); err != nil || !validDKGRequest(v.domain, request) {
		return frost.DKGRequest{}, attempt, s.fail(ErrQuarantined)
	}
	return request, attempt, nil
}

func validDKGRequest(domain frost.Domain, r frost.DKGRequest) bool {
	n := len(r.Group.Roster)
	if n == 0 || n > 100 || !validSeats(r.Group.Roster) ||
		r.Group.Epoch != domain.Epoch || domain.ValidateAttempt(r.Attempt, "dkg") != nil ||
		int(r.Group.Threshold) <= n/2 || int(r.Group.Threshold) > n ||
		(r.Group.Quorum != 0 && int(r.Group.Quorum) != n) ||
		(len(r.Participants) != 0 && !slices.Equal(r.Participants, r.Group.Roster)) ||
		!validSeats(r.LocalSeats) {
		return false
	}
	for _, seat := range r.LocalSeats {
		if !slices.Contains(r.Group.Roster, seat) {
			return false
		}
	}
	return true
}

func validSeats(seats []uint16) bool {
	if len(seats) == 0 || len(seats) > 100 {
		return false
	}
	var previous uint16
	for _, seat := range seats {
		if seat <= previous || seat > 100 {
			return false
		}
		previous = seat
	}
	return true
}

func keyMatchesRequest(k frost.KeyReady, r frost.DKGRequest) bool {
	if k.Candidate.Epoch != r.Group.Epoch || k.Candidate.Threshold != r.Group.Threshold ||
		!slices.Equal(k.Candidate.Roster, r.Group.Roster) ||
		len(k.LocalReferences) != len(r.LocalSeats) {
		return false
	}
	for _, ref := range k.LocalReferences {
		if len(ref) == 0 {
			return false
		}
	}
	return true
}
