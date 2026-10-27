package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost/movement"

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
		var retirement incomingRetirement
		main := plan.MovementMain()
		retired := json.Unmarshal(completion, &retirement) == nil && retirement.Commitment == commitment && retirement.BlockHash != ([32]byte{}) && main != nil && retirement.SpentMain == movement.OutpointKey(main.Outpoint) && retirement.Finalized > 0
		if !bytes.Equal(completion, commitment[:]) && !bytes.Equal(completion, append([]byte("abandoned/"), commitment[:]...)) && !retired {
			return errors.New("incoming completion does not match retained intent")
		}
		if bytes.Equal(completion, commitment[:]) {
			transaction := plan.BitcoinTransactionHash()
			w.lastIncoming.Store(&transaction)
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
	transaction := w.plan.BitcoinTransactionHash()
	w.lastIncoming.Store(&transaction)
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
	if _, err := w.journal.ReadSigningRecord(ctx, advertisedIncomingID(w, w.plan)); err == nil {
		return false, nil
	} else if !errors.Is(err, store.ErrMissing) {
		return false, err
	}
	// Journals predating publication markers used an unconditional seat-1 pin.
	// Preserve it when enabling cohorts; absence of a new marker is not evidence.
	if len(w.request.LocalSeats) != 0 && w.request.LocalSeats[0] == 1 {
		return false, nil
	}
	return w.mover.CanAbandon(ctx, w.plan)
}

// incomingRetirement is a public, durable chain-evidence tombstone. It never
// deletes signing claims, grants, results or purpose-lock history.
type incomingRetirement struct {
	Commitment, SpentMain, BlockHash [32]byte
	Finalized                        uint64
}

// retireSpentIncoming handles a finalized incompatible spend of the OLD main.
// A fee/cap change, reservation or an authorization error is not this evidence.
// In particular a formerly mainless plan cannot use this retirement path.
func (n *Node) retireSpentIncoming(ctx context.Context, w *wallet) (bool, error) {
	main := w.plan.MovementMain()
	if main == nil {
		return false, nil
	}
	head, err := w.mover.Chain.Head(ctx)
	if err != nil {
		return false, err
	}
	if head.Number < n.cfg.FinalityBlocks {
		return false, nil
	}
	at := head.Number - n.cfg.FinalityBlocks
	hash, err := w.mover.Chain.HeaderHash(ctx, at)
	if err != nil {
		return false, err
	}
	key := movement.OutpointKey(main.Outpoint)
	out, err := w.mover.Chain.CustodyOutput(ctx, at, key)
	if err != nil {
		return false, err
	}
	if !out.Spent || out.WalletID != w.record.ID || out.Value != uint64(main.Value) {
		return false, nil
	}
	custody, err := w.mover.Chain.Custody(ctx, at, w.record.ID)
	if err != nil {
		return false, err
	}
	if custody.MainOutpoint == key {
		return false, nil
	}
	// Settlement is reconciled separately. At least one still-pending input
	// distinguishes this from the old sweep's own successful proof.
	pending := false
	for _, in := range w.plan.IncomingOutputs() {
		record, err := w.mover.Chain.Incoming(ctx, at, movement.OutpointKey(in.Outpoint))
		if err != nil {
			return false, err
		}
		if record.Target != w.record.ID {
			return false, nil
		}
		if record.State == movement.IncomingPending || record.State == movement.IncomingEscalated {
			pending = true
		}
	}
	if !pending {
		return false, nil
	}
	confirmed, ok := w.mover.Base.Bitcoin.(signing.ConfirmedSpends)
	if !ok {
		return false, errors.New("retirement needs confirmed Bitcoin spends")
	}
	unspent, err := confirmed.ConfirmedUnspent(ctx, main.Outpoint)
	if err != nil || unspent {
		return false, err
	}
	again, err := w.mover.Chain.HeaderHash(ctx, at)
	if err != nil {
		return false, err
	}
	if again != hash {
		return false, errors.New("retirement block reorganized")
	}
	if err = w.mover.Lock.Close(ctx, w.mover.Chain, at, w.record.ID, main.Outpoint, "incoming recipe input spent at finalized head"); err != nil {
		return false, err
	}
	evidence := incomingRetirement{Commitment: w.plan.Commitment(), SpentMain: key, BlockHash: hash, Finalized: at}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return false, err
	}
	if err = w.journal.RetainSigningRecord(ctx, incomingRecord(w.journal.Domain().ID(), w.incomingSlot, true), raw); err != nil {
		return false, err
	}
	w.incomingSlot++
	w.plan, w.signed = nil, false
	return true, nil
}

func advertisedIncomingID(w *wallet, plan *signing.Plan) [32]byte {
	domain, commitment := w.journal.Domain().ID(), plan.Commitment()
	return sha256.Sum256(append(append([]byte("keep-core/frost/incoming-advertised/v1/"), domain[:]...), commitment[:]...))
}
