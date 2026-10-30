package spv

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/movement"
)

// FrostMovementMaintainer proves K-07 movements and incoming sweeps
// (P-K07-INCOMING-ONCE). It is idempotent: a transaction a FROST proof already
// recorded is not resubmitted, and a submission whose reply is lost is settled
// by rereading the recorded proof, never by submitting another transaction.
type FrostMovementMaintainer struct {
	Chain         movement.Chain
	Bitcoin       bitcoin.Chain
	Confirmations uint
	boundary      func(string) error
}

func (m FrostMovementMaintainer) step(name string) error {
	if m.boundary != nil {
		return m.boundary(name)
	}
	return nil
}

// ErrMovementNotConfirmed reports a transaction below the proof depth.
var ErrMovementNotConfirmed = errors.New("FROST movement transaction is not confirmed")

func (m FrostMovementMaintainer) proven(ctx context.Context, wallet, hash [32]byte) (bool, error) {
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return false, err
	}
	recorded, err := m.Chain.ProvenTransaction(ctx, head.Number, hash)
	if err != nil {
		return false, err
	}
	if recorded != ([32]byte{}) && recorded != wallet {
		return false, fmt.Errorf("transaction %x is recorded for another wallet", hash)
	}
	return recorded == wallet, nil
}

func (m FrostMovementMaintainer) prove(ctx context.Context, wallet, hash [32]byte, kind string, submit func(movement.Proof) error, before, submitted, seen string) (FrostAction, error) {
	action := FrostAction{Kind: kind, Transaction: hash}
	if m.Confirmations == 0 {
		return action, errors.New("confirmations must be positive")
	}
	depth, ok := frostProofDepth(m.Chain, m.Bitcoin, bitcoin.Hash(hash), m.Confirmations)
	if !ok {
		return action, ErrMovementNotConfirmed
	}
	done, err := m.proven(ctx, wallet, hash)
	if err != nil {
		return action, err
	}
	if done {
		action.Kind = "already-proven"
		return action, nil
	}
	tx, proof, err := bitcoin.AssembleSpvProof(bitcoin.Hash(hash), depth, m.Bitcoin)
	if err != nil {
		return action, err
	}
	if tx.Hash() != bitcoin.Hash(hash) {
		return action, errors.New("assembled transaction hash mismatch")
	}
	if err = m.step(before); err != nil {
		return action, err
	}
	err = submit(movement.Proof{Transaction: TransactionInfo(tx), Proof: spvProof(proof)})
	if err == nil {
		err = m.step(submitted)
	}
	if err != nil {
		if again, readErr := m.proven(ctx, wallet, hash); readErr != nil || !again {
			return action, fmt.Errorf("%s: %w", kind, err)
		}
	}
	return action, m.step(seen)
}

// ProveMovement proves a confirmed FROST movement of wallet.
func (m FrostMovementMaintainer) ProveMovement(ctx context.Context, wallet, hash [32]byte) (FrostAction, error) {
	return m.prove(ctx, wallet, hash, "movement-proof", func(p movement.Proof) error {
		return m.Chain.SubmitMovementProof(ctx, wallet, p)
	}, "before-movement-proof", "movement-proof-submitted", "incoming-recorded")
}

// ProveIncomingSweep proves a confirmed incoming sweep of target.
func (m FrostMovementMaintainer) ProveIncomingSweep(ctx context.Context, target, hash [32]byte) (FrostAction, error) {
	return m.prove(ctx, target, hash, "incoming-sweep-proof", func(p movement.Proof) error {
		return m.Chain.SubmitIncomingSweepProof(ctx, target, p)
	}, "before-sweep-proof", "sweep-proof-submitted", "credit-seen")
}
