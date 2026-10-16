package snowfallengine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

type authorizationJournal struct {
	frost.Journal
	domain   frost.Domain
	key      frost.KeyReady
	claimed  bool
	released bool
	intent   frost.SigningRequest
}

func (j *authorizationJournal) Domain() frost.Domain                            { return j.domain }
func (j *authorizationJournal) LoadKey(context.Context) (frost.KeyReady, error) { return j.key, nil }
func (j *authorizationJournal) Claim(_ context.Context, _ [32]byte, purpose string, raw []byte) (func(), error) {
	if j.claimed {
		return nil, errors.New("terminal claim")
	}
	if purpose != "sign" {
		return nil, errors.New("wrong purpose")
	}
	if err := json.Unmarshal(raw, &j.intent); err != nil {
		return nil, err
	}
	j.claimed = true
	return func() { j.released = true }, nil
}

type authorizationTransport struct {
	frost.Transport
	a frost.Attempt
}

func (t authorizationTransport) Binding() (frost.Attempt, [32]byte) { return t.a, AttemptID(t.a) }

type authorizationCheck func(context.Context, frost.SigningRequest) error

func (f authorizationCheck) BeforeSigning(c context.Context, r frost.SigningRequest) error {
	return f(c, r)
}
func TestSigningAuthorizationAfterDurableClaim(t *testing.T) {
	domain := frost.Domain{Network: "authorization-test", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 77}
	a, err := domain.NewAttempt("sign", [32]byte{3}, 10)
	if err != nil {
		t.Fatal(err)
	}
	key := frost.KeyReady{Candidate: frost.Candidate{Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 77, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{make([]byte, 220)}}
	journal := &authorizationJournal{domain: domain, key: key}
	engine, err := NewGuarded(WorkerConfig{Path: "/missing/worker-must-not-start", SHA256: [32]byte{1}}, journal)
	if err != nil {
		t.Fatal(err)
	}
	request := frost.SigningRequest{Group: frost.Group{Roster: []uint16{1, 2, 3}, Threshold: 2, Quorum: 3, Epoch: 77}, LocalSeats: []uint16{1}, Selected: []uint16{1, 2}, Attempt: a, Key: key, Message: [32]byte{8}, Authorization: []byte("exact intent")}
	for _, tc := range []struct {
		name     string
		grant    []byte
		callback bool
	}{
		{"empty grant and no callback", nil, false}, {"empty grant with callback", nil, true}, {"grant without callback", []byte("exact intent"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := &authorizationJournal{domain: domain, key: key}
			e, err := NewGuarded(WorkerConfig{Path: "/missing/worker-must-not-start", SHA256: [32]byte{1}}, j)
			if err != nil {
				t.Fatal(err)
			}
			invalid := request
			invalid.Authorization = tc.grant
			called := false
			p := frost.Providers{Transport: authorizationTransport{a: a}, Acceptance: &profileObserver{}}
			if tc.callback {
				p.SigningAuthorization = authorizationCheck(func(context.Context, frost.SigningRequest) error { called = true; return nil })
			}
			if _, err = e.Sign(context.Background(), invalid, p); err == nil || j.claimed || called {
				t.Fatal("missing authorization reached claim/callback", err)
			}
		})
	}
	p := frost.Providers{Transport: authorizationTransport{a: a}, Acceptance: &profileObserver{}}
	if _, err = engine.Sign(context.Background(), request, p); err == nil || journal.claimed {
		t.Fatal("missing guard reached claim or worker", err)
	}
	sentinel := errors.New("reservation changed")
	calls := 0
	p.SigningAuthorization = authorizationCheck(func(_ context.Context, r frost.SigningRequest) error {
		calls++
		if !journal.claimed || journal.released || !reflect.DeepEqual(r, request) || !reflect.DeepEqual(journal.intent, request) {
			t.Fatal("callback did not follow exact durable claim")
		}
		return sentinel
	})
	if _, err = engine.Sign(context.Background(), request, p); !errors.Is(err, sentinel) || calls != 1 || !journal.released {
		t.Fatal("authorization failed to stop worker", err, calls)
	}
	if _, err = engine.Sign(context.Background(), request, p); err == nil || calls != 1 {
		t.Fatal("refused attempt was reused", err, calls)
	}
}
