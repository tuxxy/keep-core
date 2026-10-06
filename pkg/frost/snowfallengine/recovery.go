package snowfallengine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost"
	storepkg "github.com/keep-network/keep-core/pkg/frost/store"
	snowfall "github.com/threshold-network/snowfall/clients/go"
)

type recoveryBridge struct {
	storeBridge
	journal frost.RecoveryJournal
}

func toRecovery(h frost.RecoveryHandle) snowfall.RecoveryHandle {
	c := h.Candidate
	out := snowfall.RecoveryHandle{Version: h.Version, Attempt: h.Attempt, Candidate: snowfall.Candidate{Epoch: c.Epoch, Descriptor: c.Descriptor, OutputKey: c.OutputKey, Roster: append([]uint16(nil), c.Roster...), Threshold: c.Threshold, Profile: c.Profile}}
	for _, r := range h.Records {
		out.Records = append(out.Records, snowfall.CandidateRecordRef{Seat: r.Seat, ID: r.ID})
	}
	return out
}
func fromRecovery(h snowfall.RecoveryHandle) frost.RecoveryHandle {
	c := h.Candidate
	out := frost.RecoveryHandle{Version: h.Version, Attempt: h.Attempt, Candidate: frost.Candidate{Epoch: c.Epoch, Descriptor: c.Descriptor, OutputKey: c.OutputKey, Roster: append([]uint16(nil), c.Roster...), Threshold: c.Threshold, Profile: c.Profile}}
	for _, r := range h.Records {
		out.Records = append(out.Records, frost.CandidateRecordRef{Seat: r.Seat, ID: r.ID})
	}
	return out
}
func (b recoveryBridge) SaveRecovery(ctx context.Context, h snowfall.RecoveryHandle) error {
	return b.journal.SaveRecovery(ctx, fromRecovery(h))
}
func (b recoveryBridge) ReadLock(ctx context.Context, seat uint16, attempt [32]byte, kind string) (snowfall.Write, error) {
	w, e := b.journal.ReadLock(ctx, seat, attempt, kind)
	if errors.Is(e, storepkg.ErrMissing) {
		e = snowfall.ErrLockMissing
	}
	return snowfall.Write{Kind: w.Kind, Seat: w.Seat, ID: w.ID, Payload: w.Payload}, e
}

// Recover resumes the original claimed DKG request. It uses the same finalized
// acceptance provider, replays only authenticated readiness, and atomically
// installs the resulting wallet metadata and references in the journal.
func (e *Engine) Recover(ctx context.Context, r frost.DKGRequest, p frost.Providers) (frost.KeyReady, error) {
	if e.journal == nil {
		return frost.KeyReady{}, invalid("recovery requires a guarded engine")
	}
	r.Group.Roster = append([]uint16(nil), r.Group.Roster...)
	r.LocalSeats = append([]uint16(nil), r.LocalSeats...)
	r.Participants = append([]uint16(nil), r.Participants...)
	r.Attempt = copyAttempt(r.Attempt)
	p.Store = e.journal
	c, chain, err := e.client(ctx, r.Group, r.LocalSeats, r.Attempt, p)
	if err != nil {
		return frost.KeyReady{}, err
	}
	// Validate the same domain and transport checks as a fresh claim, but acquire
	// the already-existing DKG ownership instead of creating a new attempt.
	release, err := e.claim(ctx, "recover-dkg", r.Attempt, r.Group.Epoch, r, &p)
	if err != nil {
		return frost.KeyReady{}, err
	}
	defer release()
	h, err := e.journal.LoadRecovery(ctx)
	if errors.Is(err, storepkg.ErrMissing) {
		refs, loadErr := e.journal.PendingCandidates(ctx)
		if loadErr != nil {
			return frost.KeyReady{}, classified(loadErr)
		}
		sr := make([]snowfall.CandidateRecordRef, len(refs))
		for i, r := range refs {
			sr[i] = snowfall.CandidateRecordRef{Seat: r.Seat, ID: r.ID}
		}
		recovered, inspectErr := c.InspectRecovery(ctx, AttemptID(r.Attempt), sr)
		if inspectErr != nil {
			return frost.KeyReady{}, classified(inspectErr)
		}
		h = fromRecovery(recovered)
		err = e.journal.SaveRecovery(ctx, h)
	}
	if err != nil {
		return frost.KeyReady{}, classified(err)
	}
	if h.Attempt != AttemptID(r.Attempt) {
		return frost.KeyReady{}, invalid("recovery handle attempt mismatch")
	}
	op, err := c.BeginRecovery(toRecovery(h))
	if err != nil {
		return frost.KeyReady{}, classified(err)
	}
	defer op.Cancel()
	key, err := op.Wait(ctx)
	if err != nil {
		return frost.KeyReady{}, classified(err)
	}
	candidate, ok := chain.result()
	if !ok || candidate.Descriptor != key.Descriptor || candidate.OutputKey != key.OutputKey {
		return frost.KeyReady{}, invalid("recovered key differs from accepted candidate")
	}
	result := frost.KeyReady{Candidate: candidate}
	for _, ref := range key.LocalReferences {
		result.LocalReferences = append(result.LocalReferences, append(frost.KeyReference(nil), ref[:]...))
	}
	if err = e.journal.SaveKey(ctx, result); err != nil {
		return frost.KeyReady{}, classified(err)
	}
	return result, nil
}

// RecoverPending reads the original request from the protected queue. A fresh
// host needs only its journal configuration and the bound transport/provider.
func (e *Engine) RecoverPending(ctx context.Context, p frost.Providers) (frost.KeyReady, error) {
	if e.journal == nil {
		return frost.KeyReady{}, invalid("recovery requires a guarded engine")
	}
	raw, err := e.journal.RecoveryIntent(ctx)
	if err != nil {
		return frost.KeyReady{}, classified(err)
	}
	var r frost.DKGRequest
	if err = json.Unmarshal(raw, &r); err != nil {
		return frost.KeyReady{}, invalid("invalid persisted recovery intent")
	}
	return e.Recover(ctx, r, p)
}
