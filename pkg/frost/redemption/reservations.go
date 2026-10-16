package redemption

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// ErrInconsistentRead reports that the canonical head changed during one read.
var ErrInconsistentRead = errors.New("FROST redemption chain head changed during read")

// ChainReservations implements signing.Reservations and signing.Cancellations
// from the authoritative C-04 record. Every view is read at one block and the
// block is checked to still be canonical afterwards; the admission block hash
// comes from the admission log and is compared with the canonical header.
type ChainReservations struct {
	Chain Reader
}

var _ signing.Reservations = ChainReservations{}
var _ signing.Cancellations = ChainReservations{}

func (r ChainReservations) snapshot(ctx context.Context, read func(head Head) error) (Head, error) {
	head, err := r.Chain.Head(ctx)
	if err != nil {
		return Head{}, err
	}
	if err = read(head); err != nil {
		return Head{}, err
	}
	again, err := r.Chain.HeaderHash(ctx, head.Number)
	if err != nil {
		return Head{}, err
	}
	if again != head.Hash {
		return Head{}, ErrInconsistentRead
	}
	return head, nil
}

func (r ChainReservations) ReadReservation(ctx context.Context, id [32]byte) (signing.ReservationView, error) {
	var intent Intent
	var admission Admission
	var canonical [32]byte
	head, err := r.snapshot(ctx, func(head Head) error {
		var err error
		if intent, err = r.Chain.Intent(ctx, head.Number, id); err != nil {
			return err
		}
		if admission, err = r.Chain.Admission(ctx, id); err != nil {
			return err
		}
		if admission.Block != intent.AdmittedBlock || admission.Block > head.Number || admission.TransactionHash != intent.TransactionHash {
			return fmt.Errorf("admission log differs from reservation record")
		}
		canonical, err = r.Chain.HeaderHash(ctx, admission.Block)
		return err
	})
	if err != nil {
		return signing.ReservationView{}, err
	}
	k := intent.Certificate
	return signing.ReservationView{ID: id, Commitment: k.IntentCommitment, BlockHash: admission.BlockHash, CanonicalBlockHash: canonical, HeadHash: head.Hash, State: intent.State.ExecutorState(), Block: admission.Block, Head: head.Number, Generation: k.Generation, StateVersion: k.StateVersion}, nil
}

func (r ChainReservations) ReadCancellation(ctx context.Context, id, hash [32]byte) (signing.CancellationView, error) {
	var alternative *Alternative
	var admission Admission
	var canonical [32]byte
	_, err := r.snapshot(ctx, func(head Head) error {
		alternatives, err := r.Chain.Alternatives(ctx, head.Number, id)
		if err != nil {
			return err
		}
		for i := range alternatives {
			if alternatives[i].TransactionHash == hash {
				alternative = &alternatives[i]
			}
		}
		if alternative == nil {
			return ErrNotFound
		}
		if admission, err = r.Chain.CancellationAdmission(ctx, id, hash); err != nil {
			return err
		}
		if admission.Block != alternative.AdmittedBlock || admission.Block > head.Number {
			return fmt.Errorf("cancellation log differs from alternative record")
		}
		canonical, err = r.Chain.HeaderHash(ctx, admission.Block)
		return err
	})
	if err != nil {
		return signing.CancellationView{}, err
	}
	return signing.CancellationView{Reservation: id, TransactionHash: hash, Snapshot: alternative.Snapshot, Fee: alternative.Fee, Block: admission.Block, BlockHash: admission.BlockHash, CanonicalBlockHash: canonical}, nil
}
