package enforcement

// FIXTURES (labelled): fault-injecting wrappers around the package's existing
// fakeChain and memRecords, used by the mutation batch M3 tests
// (MUTATION-REPORT-2026-11-10, keep-core pkg/frost/enforcement). They add only
// failures and alternative chain views; every success still comes from the
// unmodified fakes.

import (
	"context"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

var errInjected = errors.New("injected fault")

// faultChain is a fakeChain whose reads and writes can fail on demand.
type faultChain struct {
	*fakeChain
	headErr, logsErr, challengeErr, headerErr     error
	timeoutErr, timestampErr, provenErr, nonceErr error
	// notifyTimeoutErr fails NotifyChallengeTimeout on a digest without
	// resolving it.
	notifyTimeoutErr map[[32]byte]error
	// notifyInactivityErr fails NotifyInactivity without advancing the nonce.
	notifyInactivityErr error
	// view, when set, replaces the challenge view returned for a digest.
	view func(digest [32]byte, c Challenge) Challenge
}

func (f *faultChain) Head(ctx context.Context) (redemption.Head, error) {
	if f.headErr != nil {
		return redemption.Head{}, f.headErr
	}
	return f.fakeChain.Head(ctx)
}

func (f *faultChain) HeaderHash(ctx context.Context, n uint64) ([32]byte, error) {
	if f.headerErr != nil {
		return [32]byte{}, f.headerErr
	}
	return f.fakeChain.HeaderHash(ctx, n)
}

func (f *faultChain) Timestamp(ctx context.Context, n uint64) (uint64, error) {
	if f.timestampErr != nil {
		return 0, f.timestampErr
	}
	return f.fakeChain.Timestamp(ctx, n)
}

func (f *faultChain) ChallengeLogs(ctx context.Context, w [32]byte, from, to uint64) ([]ChallengeLog, error) {
	if f.logsErr != nil {
		return nil, f.logsErr
	}
	return f.fakeChain.ChallengeLogs(ctx, w, from, to)
}

func (f *faultChain) Challenge(ctx context.Context, w, d [32]byte) (Challenge, error) {
	if f.challengeErr != nil {
		return Challenge{}, f.challengeErr
	}
	c, err := f.fakeChain.Challenge(ctx, w, d)
	if f.view != nil {
		c = f.view(d, c)
	}
	return c, err
}

func (f *faultChain) FraudDefeatTimeout(ctx context.Context) (uint64, error) {
	if f.timeoutErr != nil {
		return 0, f.timeoutErr
	}
	return f.fakeChain.FraudDefeatTimeout(ctx)
}

func (f *faultChain) ProvenTransaction(ctx context.Context, at uint64, h [32]byte) ([32]byte, error) {
	if f.provenErr != nil {
		return [32]byte{}, f.provenErr
	}
	return f.fakeChain.ProvenTransaction(ctx, at, h)
}

func (f *faultChain) NotifyChallengeTimeout(ctx context.Context, w, d [32]byte) error {
	if err := f.notifyTimeoutErr[d]; err != nil {
		f.timeouts = append(f.timeouts, d)
		return err
	}
	return f.fakeChain.NotifyChallengeTimeout(ctx, w, d)
}

func (f *faultChain) InactivityNonce(ctx context.Context, w [32]byte) (uint64, error) {
	if f.nonceErr != nil {
		return 0, f.nonceErr
	}
	return f.fakeChain.InactivityNonce(ctx, w)
}

func (f *faultChain) NotifyInactivity(ctx context.Context, c Claim, seats []uint16, sigs []byte) error {
	if f.notifyInactivityErr != nil {
		f.claims = append(f.claims, c)
		return f.notifyInactivityErr
	}
	return f.fakeChain.NotifyInactivity(ctx, c, seats, sigs)
}

// faultRecords is a memRecords whose reads or retains of chosen IDs fail.
type faultRecords struct {
	*memRecords
	readErr, retainErr map[[32]byte]error
}

func newFaultRecords(r *memRecords) *faultRecords {
	return &faultRecords{memRecords: r, readErr: map[[32]byte]error{}, retainErr: map[[32]byte]error{}}
}

func (r *faultRecords) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	if err := r.readErr[id]; err != nil {
		return nil, err
	}
	return r.memRecords.ReadSigningRecord(ctx, id)
}

func (r *faultRecords) RetainSigningRecord(ctx context.Context, id [32]byte, p []byte) error {
	if err := r.retainErr[id]; err != nil {
		return err
	}
	return r.memRecords.RetainSigningRecord(ctx, id, p)
}

// faultJournal adapts faultRecords to signing.Journal for the runner's base
// configuration; only the record methods are used.
type faultJournal struct {
	signing.Journal
	r *faultRecords
}

func (j faultJournal) RetainSigningRecord(ctx context.Context, id [32]byte, p []byte) error {
	return j.r.RetainSigningRecord(ctx, id, p)
}
func (j faultJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	return j.r.ReadSigningRecord(ctx, id)
}

// failAt returns a boundary hook that fails only at the named step.
func failAt(step string) func(string) error {
	return func(name string) error {
		if name == step {
			return errInjected
		}
		return nil
	}
}
