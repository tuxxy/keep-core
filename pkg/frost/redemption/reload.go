package redemption

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// FreshWorkerReload returns an Attester.Reload that revalidates the durably
// installed completed key in a fresh worker immediately before each
// attestation (C-04 honest-host rule). Each reload uses a new random attempt
// with an irrevocable claim and never advances a signing protocol.
func FreshWorkerReload(worker snowfallengine.WorkerConfig, journal signing.Journal, head func(context.Context) (uint64, error)) func(context.Context) error {
	return func(ctx context.Context) error {
		engine, err := snowfallengine.NewGuarded(worker, journal)
		if err != nil {
			return err
		}
		key, err := journal.LoadKey(ctx)
		if err != nil {
			return err
		}
		status, err := journal.DKGStatus(ctx)
		if err != nil {
			return err
		}
		block, err := head(ctx)
		if err != nil {
			return err
		}
		if block == 0 {
			block = 1
		}
		var session [32]byte
		if _, err = rand.Read(session[:]); err != nil {
			return err
		}
		attempt, err := journal.Domain().NewAttempt("sign", session, block)
		if err != nil {
			return err
		}
		c := key.Candidate
		request := frost.SigningRequest{Group: frost.Group{Roster: append([]uint16(nil), c.Roster...), Threshold: c.Threshold, Quorum: uint16(len(c.Roster)), Epoch: c.Epoch}, LocalSeats: append([]uint16(nil), status.LocalSeats...), Selected: append([]uint16(nil), c.Roster...), Attempt: attempt, Key: key}
		return engine.ReloadKey(ctx, request, frost.Providers{Transport: reloadOnly{attempt}, Store: journal, Acceptance: reloadOnly{attempt}})
	}
}

type reloadOnly struct{ attempt frost.Attempt }

func (t reloadOnly) Binding() (frost.Attempt, [32]byte) {
	return t.attempt, snowfallengine.AttemptID(t.attempt)
}
func (reloadOnly) Broadcast(context.Context, uint16, []byte) error {
	return errors.New("reload attempted broadcast")
}
func (reloadOnly) SendPrivate(context.Context, uint16, uint16, []byte) error {
	return errors.New("reload attempted private message")
}
func (reloadOnly) Receive(context.Context) (frost.Incoming, error) {
	return frost.Incoming{}, errors.New("reload attempted receive")
}
func (reloadOnly) WaitCandidateAcceptance(context.Context, frost.Candidate) (frost.Receipt, error) {
	return frost.Receipt{}, errors.New("reload cannot admit a DKG candidate")
}
