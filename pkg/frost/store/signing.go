package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Signing application records use a separate claim-ID domain. Reusing the
// fenced immutable claim path preserves its durability and terminal history.
// These records contain public intents/results, never decoded worker records.
func signingRecordID(id [32]byte) [32]byte {
	return sha256.Sum256(append([]byte("keep-core/frost/signing-record/v1/"), id[:]...))
}
func (v *Scoped) RetainSigningRecord(ctx context.Context, id [32]byte, payload []byte) error {
	if id == ([32]byte{}) || len(payload) == 0 || len(payload) > maxRecord {
		return errors.New("invalid signing application record")
	}
	claim := signingRecordID(id)
	release, err := v.Claim(ctx, claim, "sign", payload)
	if err == nil {
		release()
		return nil
	}
	if !errors.Is(err, ErrClaimed) {
		return err
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.check(ctx); err != nil {
		return err
	}
	raw, err := v.readSigningRecord(claim)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, payload) {
		return errors.New("conflicting signing application record")
	}
	return s.syncCurrent()
}
func (v *Scoped) readSigningRecord(claim [32]byte) ([]byte, error) {
	key := hex.EncodeToString(claim[:])
	raw, ok := v.store.state.Claims[key]
	if !ok {
		return nil, ErrMissing
	}
	header := append([]byte("keep-core/snowfall/claim/v1/"+key), v.domain.Bytes()...)
	plain, err := v.store.open(raw, header)
	if err != nil {
		return nil, v.store.fail(err)
	}
	return plain, nil
}
func (v *Scoped) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	return v.readSigningRecord(signingRecordID(id))
}
