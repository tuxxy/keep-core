package spv

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// FrostRedemptionMaintainer is the permissionless K-05 maintainer for one
// FROST wallet. It discovers admitted reservations and funded requests from
// Bridge logs, proves payment or cancellation inclusion, notifies timeouts,
// collects refunds and returns unused cancellation budgets. It identifies
// transactions by their admitted raw hash, never by HASH160 of a key.
type FrostRedemptionMaintainer struct {
	Chain         redemption.Chain
	Bitcoin       bitcoin.Chain
	Confirmations uint
	ProofDepth    FrostProofDepth
	Wallet        [32]byte
	// FromBlock bounds log scans (the wallet's funding block or later).
	FromBlock uint64
	assemble  func(bitcoin.Hash, uint, bitcoin.Chain) (*bitcoin.Transaction, *bitcoin.SpvProof, error)
}

// FrostAction records one submitted maintainer transaction.
type FrostAction struct {
	Kind        string
	Reservation [32]byte
	Request     [32]byte
	Transaction [32]byte
}

// TransactionInfo splits a transaction into the C-04 BitcoinTx.Info fields.
func TransactionInfo(tx *bitcoin.Transaction) redemption.BitcoinInfo {
	return redemption.BitcoinInfo{Version: tx.SerializeVersion(), InputVector: tx.SerializeInputs(), OutputVector: tx.SerializeOutputs(), Locktime: tx.SerializeLocktime()}
}

func (m FrostRedemptionMaintainer) proof(hash [32]byte) (redemption.BitcoinInfo, redemption.BitcoinProof, error) {
	depth, err := frostProofDepth(context.Background(), bitcoin.Hash(hash), m.Confirmations, m.ProofDepth)
	if err != nil {
		return redemption.BitcoinInfo{}, redemption.BitcoinProof{}, err
	}
	assemble := m.assemble
	if assemble == nil {
		assemble = bitcoin.AssembleSpvProof
	}
	tx, proof, err := assemble(bitcoin.Hash(hash), depth, m.Bitcoin)
	if err != nil {
		return redemption.BitcoinInfo{}, redemption.BitcoinProof{}, err
	}
	if tx.Hash() != bitcoin.Hash(hash) {
		return redemption.BitcoinInfo{}, redemption.BitcoinProof{}, errors.New("assembled transaction hash mismatch")
	}
	return TransactionInfo(tx), redemption.BitcoinProof{MerkleProof: proof.MerkleProof, TxIndexInBlock: uint64(proof.TxIndexInBlock), BitcoinHeaders: proof.BitcoinHeaders, CoinbasePreimage: proof.CoinbasePreimage, CoinbaseProof: proof.CoinbaseProof}, nil
}

func (m FrostRedemptionMaintainer) confirmed(hash [32]byte) bool {
	depth, err := frostProofDepth(context.Background(), bitcoin.Hash(hash), m.Confirmations, m.ProofDepth)
	if err != nil {
		return false
	}
	n, err := m.Bitcoin.GetTransactionConfirmations(context.Background(), bitcoin.Hash(hash))
	return err == nil && n >= depth
}

// ProveSettlements submits one inclusion proof for every unresolved
// reservation whose payment or admitted cancellation alternative has the
// required confirmations. Payment is tried first; a conflicting confirmed
// cancellation excludes it on Bitcoin, so at most one can be confirmed.
func (m FrostRedemptionMaintainer) ProveSettlements(ctx context.Context) ([]FrostAction, error) {
	if m.Confirmations == 0 {
		return nil, errors.New("confirmations must be positive")
	}
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	admissions, err := m.Chain.Admissions(ctx, m.Wallet, m.FromBlock, head.Number)
	if err != nil {
		return nil, err
	}
	var actions []FrostAction
	for _, a := range admissions {
		intent, err := m.Chain.Intent(ctx, head.Number, a.ReservationID)
		if err != nil {
			return actions, err
		}
		if !intent.State.Unresolved() {
			continue
		}
		if m.confirmed(intent.TransactionHash) {
			info, proof, err := m.proof(intent.TransactionHash)
			if err != nil {
				return actions, err
			}
			if err = m.Chain.SubmitPaymentProof(ctx, a.ReservationID, info, proof); err != nil {
				return actions, fmt.Errorf("payment proof: %w", err)
			}
			actions = append(actions, FrostAction{Kind: "payment-proof", Reservation: a.ReservationID, Transaction: intent.TransactionHash})
			continue
		}
		alternatives, err := m.Chain.Alternatives(ctx, head.Number, a.ReservationID)
		if err != nil {
			return actions, err
		}
		for _, alt := range alternatives {
			if !m.confirmed(alt.TransactionHash) {
				continue
			}
			info, proof, err := m.proof(alt.TransactionHash)
			if err != nil {
				return actions, err
			}
			if err = m.Chain.SubmitCancellationProof(ctx, a.ReservationID, info, proof); err != nil {
				return actions, fmt.Errorf("cancellation proof: %w", err)
			}
			actions = append(actions, FrostAction{Kind: "cancellation-proof", Reservation: a.ReservationID, Transaction: alt.TransactionHash})
			break
		}
	}
	return actions, nil
}

// ProcessRequests notifies timeouts at or after the deadline, refunds
// unreserved timed-out requests after their captured finality boundary, and
// collects refunds made available by proof-backed conflict resolution. A
// reserved timed-out request is never refunded here: it waits for a payment
// or cancellation proof.
func (m FrostRedemptionMaintainer) ProcessRequests(ctx context.Context) ([]FrostAction, error) {
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	now, err := m.Chain.Timestamp(ctx, head.Number)
	if err != nil {
		return nil, err
	}
	events, err := m.Chain.RequestEvents(ctx, m.Wallet, m.FromBlock, head.Number)
	if err != nil {
		return nil, err
	}
	var actions []FrostAction
	for _, e := range events {
		r, err := m.Chain.Request(ctx, head.Number, e.RequestID)
		if err != nil {
			return actions, err
		}
		switch {
		case (r.State == redemption.RequestRequested || r.State == redemption.RequestReserved) && now >= r.Deadline:
			if err = m.Chain.NotifyTimeout(ctx, r.ID); err != nil {
				return actions, fmt.Errorf("timeout notice: %w", err)
			}
			actions = append(actions, FrostAction{Kind: "timeout", Request: r.ID, Reservation: r.ReservationID})
		case r.State == redemption.RequestTimeoutPending && r.ReservationID == ([32]byte{}) && head.Number >= r.TimeoutBlock+r.FinalityBlocks:
			if err = m.Chain.RefundUnreserved(ctx, r.ID); err != nil {
				return actions, fmt.Errorf("unreserved refund: %w", err)
			}
			actions = append(actions, FrostAction{Kind: "unreserved-refund", Request: r.ID})
		case r.State == redemption.RequestRefundable:
			if err = m.Chain.RefundResolved(ctx, r.ID); err != nil {
				return actions, fmt.Errorf("resolved refund: %w", err)
			}
			actions = append(actions, FrostAction{Kind: "resolved-refund", Request: r.ID, Reservation: r.ReservationID})
		}
	}
	return actions, nil
}

// ClaimBudgets returns every unused cancellation budget of a terminal
// reservation to its recorded sponsor. The contract refuses the winner.
func (m FrostRedemptionMaintainer) ClaimBudgets(ctx context.Context) ([]FrostAction, error) {
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	admissions, err := m.Chain.Admissions(ctx, m.Wallet, m.FromBlock, head.Number)
	if err != nil {
		return nil, err
	}
	var actions []FrostAction
	for _, a := range admissions {
		intent, err := m.Chain.Intent(ctx, head.Number, a.ReservationID)
		if err != nil {
			return actions, err
		}
		if intent.State.Unresolved() {
			continue
		}
		alternatives, err := m.Chain.Alternatives(ctx, head.Number, a.ReservationID)
		if err != nil {
			return actions, err
		}
		for _, alt := range alternatives {
			if alt.BudgetSettled {
				continue
			}
			if err = m.Chain.ClaimCancellationBudget(ctx, a.ReservationID, alt.TransactionHash); err != nil {
				continue // The winning budget is burned, not claimable.
			}
			actions = append(actions, FrostAction{Kind: "budget-claim", Reservation: a.ReservationID, Transaction: alt.TransactionHash})
		}
	}
	return actions, nil
}
