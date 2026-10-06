package store

import (
	"context"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost"
	"reflect"
	"testing"
)

func TestRecoveryOwnershipAndJournalReopen(t *testing.T) {
	ctx := context.Background()
	c := testConfig(t)
	s, e := Open(ctx, c)
	must(t, e)
	v, e := s.Scope(testDomain())
	must(t, e)
	attempt := [32]byte{8}
	intent := []byte("original public request")
	release, e := v.Claim(ctx, attempt, "dkg", intent)
	must(t, e)
	if _, e = v.AcquireRecovery(ctx, attempt, intent); !errors.Is(e, ErrBusy) {
		t.Fatal("live DKG owner was not excluded")
	}
	release()
	if _, e = v.AcquireRecovery(ctx, attempt, []byte("other")); e == nil {
		t.Fatal("different intent accepted")
	}
	release, e = v.AcquireRecovery(ctx, attempt, intent)
	must(t, e)
	if _, e = v.AcquireRecovery(ctx, attempt, intent); !errors.Is(e, ErrBusy) {
		t.Fatal("duplicate recovery accepted")
	}
	release()
	candidate := frost.Write{Kind: "candidate-record", Seat: 1, ID: [32]byte{9}, Payload: []byte("opaque stored candidate")}
	h := frost.RecoveryHandle{Version: 1, Attempt: attempt, Candidate: frost.Candidate{Epoch: v.Domain().Epoch, Profile: frost.ApprovedProfile}, Records: []frost.CandidateRecordRef{{Seat: 1, ID: candidate.ID}}}
	if e = v.SaveRecovery(ctx, h); e == nil {
		t.Fatal("undurable candidate accepted")
	}
	_, e = v.Put(ctx, candidate)
	must(t, e)
	refs, e := v.PendingCandidates(ctx)
	must(t, e)
	if !reflect.DeepEqual(refs, h.Records) {
		t.Fatal("candidate discovery changed IDs")
	}
	must(t, v.SaveRecovery(ctx, h))
	must(t, v.SaveRecovery(ctx, h))
	changed := h
	changed.Candidate.Descriptor[0]++
	if e = v.SaveRecovery(ctx, changed); e == nil {
		t.Fatal("handle replacement accepted")
	}
	evidence := frost.ReadinessEvidence{Domain: v.Domain(), Attempt: attempt, Sender: 1, OperatorKey: []byte("authenticated operator"), Message: []byte("original envelope")}
	must(t, v.SaveReadiness(ctx, evidence))
	must(t, v.SaveReadiness(ctx, evidence))
	changedEvidence := evidence
	changedEvidence.Message = []byte("different")
	if e = v.SaveReadiness(ctx, changedEvidence); e == nil {
		t.Fatal("readiness conflict accepted")
	}
	must(t, s.Close())
	s, e = Open(ctx, c)
	must(t, e)
	defer s.Close()
	v, e = s.Scope(testDomain())
	must(t, e)
	got, e := v.LoadRecovery(ctx)
	must(t, e)
	if !reflect.DeepEqual(got, h) {
		t.Fatal("recovery handle lost")
	}
	original, e := v.RecoveryIntent(ctx)
	must(t, e)
	if string(original) != string(intent) {
		t.Fatal("original request lost")
	}
	saved, e := v.ReadReadiness(ctx, attempt)
	must(t, e)
	if !reflect.DeepEqual(saved, []frost.ReadinessEvidence{evidence}) {
		t.Fatal("readiness attribution lost")
	}
	release, e = v.AcquireRecovery(ctx, attempt, original)
	must(t, e)
	release()
	other := testDomain()
	other.Registry[0]++
	foreign, e := s.Scope(other)
	must(t, e)
	if _, e = foreign.LoadRecovery(ctx); !errors.Is(e, ErrMissing) {
		t.Fatal("foreign domain read recovery")
	}
}
