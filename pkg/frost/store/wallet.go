package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// SaveWallet installs the immutable application identity and its chain receipt
// in the same fenced journal as the key. Lifecycle updates cannot replace either.
func (v *Scoped) SaveWallet(ctx context.Context, w dkg.WalletRecord) error {
	if err := dkg.ValidateDescriptor(w.Descriptor); err != nil {
		return err
	}
	hash, err := dkg.DescriptorHash(w.Descriptor)
	if err != nil || hash != w.DescriptorHash || dkg.WalletID(w.Descriptor.OutputKey) != w.ID || w.Descriptor.Epoch != v.domain.Epoch || [20]byte(w.Descriptor.Registry) != v.domain.Registry || w.Descriptor.ChainId == nil {
		return errors.New("invalid wallet record")
	}
	var chain [32]byte
	if w.Descriptor.ChainId.Sign() <= 0 || w.Descriptor.ChainId.BitLen() > 256 {
		return errors.New("invalid wallet chain")
	}
	w.Descriptor.ChainId.FillBytes(chain[:])
	if chain != v.domain.Chain {
		return errors.New("wallet chain mismatch")
	}
	ranks := map[string]int{"Candidate": 1, "RegisteredPendingReady": 2, "ReadyUnfunded": 3, "Closed": 4}
	if ranks[w.State] == 0 {
		return errors.New("invalid wallet state")
	}
	if w.State != "Candidate" && (w.ApprovalBlock == 0 || w.ApprovalHash == ([32]byte{}) || w.Deadline <= w.ApprovalBlock || w.ResultHash == ([32]byte{})) {
		return errors.New("missing wallet approval")
	}
	plain, err := json.Marshal(w)
	if err != nil {
		return err
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.check(ctx); err != nil {
		return err
	}
	key := v.prefix()
	if w.State == "ReadyUnfunded" {
		raw, ok := s.state.Keys[key]
		if !ok {
			return errors.New("ready wallet has no durable key")
		}
		plain, err := s.open(raw, []byte("keep-core/snowfall/key/v1/"+key))
		if err != nil {
			return s.fail(err)
		}
		var key frost.KeyReady
		if err = decode(plain, &key); err != nil {
			return s.fail(err)
		}
		if key.Candidate.Descriptor != w.Descriptor.SnowfallDescriptor || key.Candidate.OutputKey != w.Descriptor.OutputKey || key.Candidate.Epoch != w.Descriptor.Epoch {
			return errors.New("ready wallet key mismatch")
		}
	}
	header := []byte("keep-core/snowfall/wallet/v1/" + key)
	if raw, ok := s.state.Wallets[key]; ok {
		previous, err := s.open(raw, header)
		if err != nil {
			return s.fail(err)
		}
		if bytes.Equal(previous, plain) {
			return s.syncCurrent()
		}
		var old dkg.WalletRecord
		if err = decode(previous, &old); err != nil {
			return s.fail(err)
		}
		if !reflect.DeepEqual(old.Descriptor, w.Descriptor) || old.ID != w.ID || old.DescriptorHash != w.DescriptorHash || ranks[w.State] < ranks[old.State] || (old.Quarantined && !w.Quarantined) || (old.State == "Closed" && w.State != "Closed") || (old.State == "ReadyUnfunded" && w.State == "Closed") || (old.ApprovalBlock != 0 && (old.ApprovalBlock != w.ApprovalBlock || old.ApprovalHash != w.ApprovalHash || old.Deadline != w.Deadline || old.ResultHash != w.ResultHash)) {
			return errors.New("wallet identity or lifecycle conflict")
		}
	}
	raw, err := s.seal(plain, header)
	if err != nil {
		return s.fail(err)
	}
	next := clone(s.state)
	if next.Wallets == nil {
		next.Wallets = map[string][]byte{}
	}
	next.Wallets[key] = raw
	return s.commit(ctx, next)
}
func (v *Scoped) LoadWallet(ctx context.Context) (dkg.WalletRecord, error) {
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return dkg.WalletRecord{}, err
	}
	raw, ok := s.state.Wallets[v.prefix()]
	if !ok {
		return dkg.WalletRecord{}, ErrMissing
	}
	plain, err := s.open(raw, []byte("keep-core/snowfall/wallet/v1/"+v.prefix()))
	if err != nil {
		return dkg.WalletRecord{}, s.fail(err)
	}
	var w dkg.WalletRecord
	if err = decode(plain, &w); err != nil {
		return w, s.fail(err)
	}
	hash, hashErr := dkg.DescriptorHash(w.Descriptor)
	var chain [32]byte
	if dkg.ValidateDescriptor(w.Descriptor) != nil || hashErr != nil || hash != w.DescriptorHash || dkg.WalletID(w.Descriptor.OutputKey) != w.ID || w.Descriptor.Epoch != v.domain.Epoch || [20]byte(w.Descriptor.Registry) != v.domain.Registry {
		return w, s.fail(errors.New("invalid stored wallet descriptor"))
	}
	w.Descriptor.ChainId.FillBytes(chain[:])
	if chain != v.domain.Chain {
		return w, s.fail(errors.New("stored wallet chain mismatch"))
	}
	return w, nil
}
