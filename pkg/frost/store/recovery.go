package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost"
	"sort"
	"sync"
)

var _ frost.RecoveryJournal = (*Scoped)(nil)

func (v *Scoped) AcquireRecovery(ctx context.Context, id [32]byte, intent []byte) (func(), error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	key := hex.EncodeToString(id[:])
	if s.state.DKGs[v.prefix()] != key {
		return nil, ErrMissing
	}
	if s.activeAttempts[key] {
		return nil, ErrBusy
	}
	header := append([]byte("keep-core/snowfall/claim/v1/"+key), v.domain.Bytes()...)
	raw, e := s.open(s.state.Claims[key], header)
	if e != nil {
		return nil, s.fail(e)
	}
	defer clear(raw)
	if !bytes.Equal(raw, intent) {
		return nil, errors.New("recovery intent differs from original claim")
	}
	if e = s.syncCurrent(); e != nil {
		return nil, e
	}
	s.active++
	s.activeAttempts[key] = true
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); s.active--; delete(s.activeAttempts, key); s.mu.Unlock() }) }, nil
}

func (v *Scoped) SaveRecovery(ctx context.Context, h frost.RecoveryHandle) error {
	if h.Version != 1 || h.Attempt == ([32]byte{}) || h.Candidate.Epoch != v.domain.Epoch || h.Candidate.Profile != frost.ApprovedProfile || len(h.Records) == 0 || len(h.Records) > 100 {
		return errors.New("invalid recovery identity")
	}
	raw, e := json.Marshal(h)
	if e != nil {
		return e
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = s.check(ctx); e != nil {
		return e
	}
	if s.state.DKGs[v.prefix()] != hex.EncodeToString(h.Attempt[:]) {
		return errors.New("recovery is not the claimed DKG")
	}
	var last uint16
	for _, ref := range h.Records {
		rec, ok := s.state.Records[v.recordKey(ref.ID)]
		if ref.Seat <= last || !ok || rec.Kind != "candidate-record" || rec.Seat != ref.Seat || rec.Domain != v.domain {
			return errors.New("recovery references are not durable local candidates")
		}
		last = ref.Seat
	}
	key := v.prefix()
	header := []byte("keep-core/snowfall/recovery/v1/" + key)
	if old, ok := s.state.Recoveries[key]; ok {
		plain, e := s.open(old, header)
		if e != nil {
			return s.fail(e)
		}
		if !bytes.Equal(raw, plain) {
			return errors.New("recovery identity conflict")
		}
		return s.syncCurrent()
	}
	sealed, e := s.seal(raw, header)
	if e != nil {
		return s.fail(e)
	}
	next := clone(s.state)
	next.Recoveries[key] = sealed
	return s.commit(ctx, next)
}
func (v *Scoped) LoadRecovery(ctx context.Context) (frost.RecoveryHandle, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return frost.RecoveryHandle{}, e
	}
	raw, ok := s.state.Recoveries[v.prefix()]
	if !ok {
		return frost.RecoveryHandle{}, ErrMissing
	}
	plain, e := s.open(raw, []byte("keep-core/snowfall/recovery/v1/"+v.prefix()))
	if e != nil {
		return frost.RecoveryHandle{}, s.fail(e)
	}
	var h frost.RecoveryHandle
	e = decode(plain, &h)
	if e != nil {
		return h, s.fail(e)
	}
	return h, nil
}

// PendingCandidates closes the crash window after the last candidate fsync and
// before SaveRecovery. The client validates all opaque records before rebuilding
// the handle; this journal never parses secret-bearing candidate bytes.
func (v *Scoped) PendingCandidates(ctx context.Context) ([]frost.CandidateRecordRef, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	var out []frost.CandidateRecordRef
	for _, r := range s.state.Records {
		if r.Domain == v.domain && r.Kind == "candidate-record" {
			out = append(out, frost.CandidateRecordRef{Seat: r.Seat, ID: r.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seat < out[j].Seat })
	return out, nil
}
func (v *Scoped) SaveReadiness(ctx context.Context, e frost.ReadinessEvidence) error {
	if e.Domain != v.domain || e.Attempt == ([32]byte{}) || e.Sender == 0 || e.Sender > 100 || len(e.OperatorKey) == 0 || len(e.OperatorKey) > 256 || len(e.Message) == 0 || len(e.Message) > 8448 {
		return errors.New("invalid readiness evidence")
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.check(ctx); err != nil {
		return err
	}
	key := v.prefix() + "/" + hex.EncodeToString(e.Attempt[:]) + "/" + hex.EncodeToString([]byte{byte(e.Sender >> 8), byte(e.Sender)})
	header := []byte("keep-core/snowfall/readiness/v1/" + key)
	if old, ok := s.state.Readiness[key]; ok {
		plain, err := s.open(old, header)
		if err != nil {
			return s.fail(err)
		}
		if !bytes.Equal(plain, raw) {
			return errors.New("authenticated readiness conflict")
		}
		return s.syncCurrent()
	}
	count := 0
	for k := range s.state.Readiness {
		if len(k) >= len(v.prefix()) && k[:len(v.prefix())] == v.prefix() {
			count++
		}
	}
	if count >= 100 {
		return errors.New("readiness capacity exceeded")
	}
	sealed, err := s.seal(raw, header)
	if err != nil {
		return s.fail(err)
	}
	next := clone(s.state)
	next.Readiness[key] = sealed
	return s.commit(ctx, next)
}
func (v *Scoped) ReadReadiness(ctx context.Context, attempt [32]byte) ([]frost.ReadinessEvidence, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	prefix := v.prefix() + "/" + hex.EncodeToString(attempt[:]) + "/"
	var out []frost.ReadinessEvidence
	for k, raw := range s.state.Readiness {
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		plain, e := s.open(raw, []byte("keep-core/snowfall/readiness/v1/"+k))
		if e != nil {
			return nil, s.fail(e)
		}
		var evidence frost.ReadinessEvidence
		if e = decode(plain, &evidence); e != nil {
			return nil, s.fail(e)
		}
		if evidence.Domain != v.domain || evidence.Attempt != attempt {
			return nil, s.fail(ErrQuarantined)
		}
		out = append(out, evidence)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sender < out[j].Sender })
	return out, nil
}

// RecoveryIntent is the original encrypted DKG request, scoped to this domain.
func (v *Scoped) RecoveryIntent(ctx context.Context) ([]byte, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	id, ok := s.state.DKGs[v.prefix()]
	if !ok {
		return nil, ErrMissing
	}
	header := append([]byte("keep-core/snowfall/claim/v1/"+id), v.domain.Bytes()...)
	raw, e := s.open(s.state.Claims[id], header)
	if e != nil {
		return nil, s.fail(e)
	}
	return raw, nil
}
