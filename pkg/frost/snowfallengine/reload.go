package snowfallengine

import (
	"context"
	"reflect"

	"github.com/keep-network/keep-core/pkg/frost"
	snowfall "github.com/threshold-network/snowfall/clients/go"
)

// ReloadKey revalidates the durably installed key in a fresh worker. It uses
// an irrevocable sign-purpose claim but never advances the signing protocol.
// It does not install a key or recover an unfinished DKG.
func (e *Engine) ReloadKey(ctx context.Context, r frost.SigningRequest, p frost.Providers) error {
	if e.journal == nil {
		return invalid("completed-key reload requires a durable journal")
	}
	saved, err := e.journal.LoadKey(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(saved, r.Key) || saved.Candidate.Profile != frost.ApprovedProfile {
		return invalid("reload key is not the installed approved key")
	}
	status, err := e.journal.DKGStatus(ctx)
	if err != nil {
		return err
	}
	if status.State != frost.DKGKeyStored || !reflect.DeepEqual(status.LocalSeats, r.LocalSeats) {
		return invalid("reload must include all durably completed local seats")
	}
	p.Store = e.journal
	c, _, err := e.client(ctx, r.Group, r.LocalSeats, r.Attempt, p)
	if err != nil {
		return err
	}
	candidate := saved.Candidate
	key := snowfall.KeyReady{Roster: candidate.Roster, Threshold: candidate.Threshold, Epoch: candidate.Epoch, Descriptor: candidate.Descriptor, OutputKey: candidate.OutputKey}
	for _, raw := range saved.LocalReferences {
		var ref snowfall.KeyReference
		if len(raw) != len(ref) {
			return invalid("malformed completed-key reference")
		}
		copy(ref[:], raw)
		key.LocalReferences = append(key.LocalReferences, ref)
	}
	release, err := e.claim(ctx, "sign", r.Attempt, r.Group.Epoch, r, &p)
	if err != nil {
		return err
	}
	defer release()
	return classified(c.ReloadKey(ctx, key, AttemptID(r.Attempt)))
}
