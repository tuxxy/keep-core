package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// The append-only cursor retains public transaction recipes, never opaque
// worker material. Completion is a separate durable record so a crash cannot
// replace a signed plan with a new plan built from changed chain state.
const incomingCapacity = 1 << 16

func incomingRecord(domain [32]byte, slot uint64, completed bool) [32]byte {
	raw := append([]byte("keep-core/frost/local-node/incoming/v1/"), domain[:]...)
	raw = binary.BigEndian.AppendUint64(raw, slot)
	if completed {
		raw = append(raw, 1)
	} else {
		raw = append(raw, 0)
	}
	return sha256.Sum256(raw)
}

func (w *wallet) restoreIncoming(ctx context.Context) error {
	for w.incomingSlot < incomingCapacity {
		raw, err := w.journal.ReadSigningRecord(ctx, incomingRecord(w.journal.Domain().ID(), w.incomingSlot, false))
		if errors.Is(err, store.ErrMissing) {
			return nil
		}
		if err != nil {
			return err
		}
		plan, err := signing.RestoreIncomingSweepPlan(w.record.Descriptor, raw)
		if err != nil {
			return err
		}
		completion, err := w.journal.ReadSigningRecord(ctx, incomingRecord(w.journal.Domain().ID(), w.incomingSlot, true))
		if errors.Is(err, store.ErrMissing) {
			w.plan = plan
			return nil
		}
		if err != nil {
			return err
		}
		commitment := plan.Commitment()
		if !bytes.Equal(completion, commitment[:]) && !bytes.Equal(completion, append([]byte("abandoned/"), commitment[:]...)) {
			return errors.New("incoming completion does not match retained intent")
		}
		w.incomingSlot++
	}
	return errors.New("local incoming journal capacity reached")
}

func (w *wallet) retainIncoming(ctx context.Context) error {
	if w.incomingSlot >= incomingCapacity {
		return errors.New("local incoming journal capacity reached")
	}
	return w.journal.RetainSigningRecord(ctx, incomingRecord(w.journal.Domain().ID(), w.incomingSlot, false), w.plan.IntentBytes())
}

func (w *wallet) finishIncoming(ctx context.Context) error {
	commitment := w.plan.Commitment()
	if err := w.journal.RetainSigningRecord(ctx, incomingRecord(w.journal.Domain().ID(), w.incomingSlot, true), commitment[:]); err != nil {
		return err
	}
	w.incomingSlot++
	w.plan, w.signed = nil, false
	return nil
}

// Abandonment is distinct from settlement and cannot erase a possibly signed
// intent. The caller has serialized this wallet's duty; no signing job races it.
func (w *wallet) abandonIncoming(ctx context.Context) (bool, error) {
	safe, err := w.canAbandonIncoming(ctx)
	if err != nil || !safe {
		return false, err
	}
	commitment := w.plan.Commitment()
	if err = w.journal.RetainSigningRecord(ctx, incomingRecord(w.journal.Domain().ID(), w.incomingSlot, true), append([]byte("abandoned/"), commitment[:]...)); err != nil {
		return false, err
	}
	w.incomingSlot++
	w.plan, w.signed = nil, false
	return true, nil
}

// A leader's durable recipe may already have produced a remote grant, even if
// no local worker started. Until canonical settlement, only followers can use
// their own absent-release evidence to replace a recipe with the leader's.
func (w *wallet) canAbandonIncoming(ctx context.Context) (bool, error) {
	if len(w.request.LocalSeats) != 0 && w.request.LocalSeats[0] == 1 {
		return false, nil
	}
	return w.mover.CanAbandon(ctx, w.plan)
}
