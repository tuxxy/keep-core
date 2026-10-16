package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

type v04Transport struct {
	frost.Transport
	attempt frost.Attempt
}

func (v v04Transport) Binding() (frost.Attempt, [32]byte) {
	return v.attempt, snowfallengine.AttemptID(v.attempt)
}

type v04Acceptance struct{ frost.Acceptance }

func (v04Acceptance) WaitCandidateAcceptance(context.Context, frost.Candidate) (frost.Receipt, error) {
	return frost.Receipt{}, errors.New("no DKG in signing replay")
}

type v04Authorization func(context.Context, frost.SigningRequest) error

func (f v04Authorization) BeforeSigning(c context.Context, r frost.SigningRequest) error {
	return f(c, r)
}

// This is a concrete real-store/real-adapter seam check, with a deliberately
// non-cryptographic launcher. It does not replace the K-04 real-worker fixture.
func TestV04RealStoreWorkerGuard(t *testing.T) {
	ctx := context.Background()
	c := testConfig(t)
	s, v := openTest(t, c)
	request, key := dkgFixture(t, v.Domain())
	// Public synthetic references only. No opaque secret record is decoded.
	key.LocalReferences = nil
	for _, seat := range request.LocalSeats {
		point := append([]byte{2}, key.Candidate.OutputKey[:]...)
		seatBytes := binary.BigEndian.AppendUint16(nil, seat)
		epochBytes := binary.BigEndian.AppendUint64(nil, request.Group.Epoch)
		var ref []byte
		for _, field := range [][]byte{[]byte("snowfall/v1/key-reference"), key.Candidate.Descriptor[:], v04BytesID(seat), v04BytesID(seat + 128), point, seatBytes, epochBytes} {
			ref = binary.BigEndian.AppendUint64(ref, uint64(len(field)))
			ref = append(ref, field...)
		}
		key.LocalReferences = append(key.LocalReferences, ref)
		for _, kind := range []struct {
			name string
			id   [32]byte
		}{{"candidate-record", arrayID(seat)}, {"completion-record", arrayID(seat + 128)}} {
			outcome, err := v.Put(ctx, frost.Write{Kind: kind.name, Seat: seat, ID: kind.id, Payload: []byte("synthetic non-secret launch-control record")})
			must(t, err)
			if outcome != frost.Durable {
				t.Fatal("launch-control record not durable")
			}
		}
	}
	release := claimDKG(t, v, [32]byte{101}, request)
	must(t, v.SaveKey(ctx, key))
	release()
	marker := filepath.Join(t.TempDir(), "started")
	worker := filepath.Join(t.TempDir(), "launch-observer")
	raw := []byte("#!/bin/sh\nprintf started > '" + marker + "'\nexit 1\n")
	must(t, os.WriteFile(worker, raw, 0700))
	engine, err := snowfallengine.NewGuarded(snowfallengine.WorkerConfig{Path: worker, SHA256: sha256.Sum256(raw), CommandTimeoutMs: 1000, ShutdownTimeoutMs: 1000}, v)
	must(t, err)
	attempt, err := v.Domain().NewAttempt("sign", [32]byte{102}, 20)
	must(t, err)
	grant := frost.SigningRequest{Group: request.Group, LocalSeats: request.LocalSeats, Selected: []uint16{4, 7, 12}, Attempt: attempt, Key: key, Message: [32]byte{103}, Authorization: []byte("public fixed intent")}
	p := frost.Providers{Transport: v04Transport{attempt: attempt}, Store: v, Acceptance: v04Acceptance{}}
	for _, tc := range []struct {
		name     string
		empty    bool
		callback bool
	}{{"no callback", false, false}, {"empty intent", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			g := grant
			if tc.empty {
				g.Authorization = nil
			}
			providers := p
			if tc.callback {
				providers.SigningAuthorization = v04Authorization(func(context.Context, frost.SigningRequest) error {
					t.Fatal("missing grant reached callback")
					return nil
				})
			}
			before := s.point
			if _, err := engine.Sign(ctx, g, providers); err == nil || s.point != before {
				t.Fatal("missing authority reached durable claim", err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing authority launched worker", err)
			}
		})
	}
	refused := errors.New("post-claim reservation timed out")
	called := false
	p.SigningAuthorization = v04Authorization(func(_ context.Context, g frost.SigningRequest) error {
		called = true
		if s.point.Sequence < 2 || g.Message != grant.Message || string(g.Authorization) != string(grant.Authorization) {
			t.Fatal("guard before exact durable claim")
		}
		return refused
	})
	if _, err = engine.Sign(ctx, grant, p); !errors.Is(err, refused) || !called {
		t.Fatal("post-claim guard failed", err)
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused authorization launched worker", err)
	}
	must(t, s.Close())
	s, v = openTest(t, c)
	if _, err = v.Claim(ctx, snowfallengine.AttemptID(attempt), "sign", []byte("retry")); !errors.Is(err, ErrClaimed) {
		t.Fatal("claim not terminal after reopen", err)
	}
	// A fresh agreed attempt can reach the same launch detector. This establishes
	// the refusal oracle's sensitivity, not a successful cryptographic signature.
	fresh, err := v.Domain().NewAttempt("sign", [32]byte{104}, 21)
	must(t, err)
	grant.Attempt = fresh
	engine, err = snowfallengine.NewGuarded(snowfallengine.WorkerConfig{Path: worker, SHA256: sha256.Sum256(raw), CommandTimeoutMs: 1000, ShutdownTimeoutMs: 1000}, v)
	must(t, err)
	p.Transport = v04Transport{attempt: fresh}
	p.Store = v
	p.SigningAuthorization = v04Authorization(func(context.Context, frost.SigningRequest) error { return nil })
	if _, err = engine.Sign(ctx, grant, p); err == nil {
		t.Fatal("non-cryptographic launch observer returned signature")
	}
	t.Logf("launch observer completion: %v", err)
	if _, err = os.Stat(marker); err != nil {
		t.Fatal("positive launch detector did not run", err)
	}
}

func arrayID(seat uint16) (id [32]byte) { binary.BigEndian.PutUint16(id[:2], seat); return }
func v04BytesID(seat uint16) []byte     { id := arrayID(seat); return id[:] }
