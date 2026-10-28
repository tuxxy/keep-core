package spv

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// FrostResolution is a collected C-04 sweep-resolution certificate.
type FrostResolution struct {
	Authorization deposit.ResolutionAuthorization
	Seats         []uint16
	Signatures    []byte
}

// FrostSweepMaintainer proves K-06 deposit sweeps (P-K06-CREDIT-ONCE). It is
// idempotent: a sweep whose deposits are all already credited is not
// resubmitted, and a submission whose reply is lost is settled by rereading
// the C-05 records, never by submitting a different sweep.
type FrostSweepMaintainer struct {
	Chain         deposit.Chain
	Bitcoin       bitcoin.Chain
	Confirmations uint
	// Funding, Clock and RefundMargin gate ReserveDeposit with the planner's
	// eligibility: a spent or late deposit is never swept, and its
	// reservation could never be released.
	Funding      deposit.Funding
	Clock        deposit.Clock
	RefundMargin uint64
	boundary     func(string) error
	// assemble defaults to bitcoin.AssembleSpvProof.
	assemble func(bitcoin.Hash, uint, bitcoin.Chain) (*bitcoin.Transaction, *bitcoin.SpvProof, error)
}

func (m FrostSweepMaintainer) step(name string) error {
	if m.boundary != nil {
		return m.boundary(name)
	}
	return nil
}

// ErrSweepNotConfirmed reports a sweep below the required confirmations.
var ErrSweepNotConfirmed = errors.New("FROST sweep is not confirmed")

// credited reports whether every deposit input is swept at the head. inputs
// that are not C-05 deposits must be the wallet's custody main.
func (m FrostSweepMaintainer) credited(ctx context.Context, wallet [32]byte, tx *bitcoin.Transaction) (bool, bool, error) {
	head, err := m.Chain.Head(ctx)
	if err != nil {
		return false, false, err
	}
	custody, err := m.Chain.Custody(ctx, head.Number, wallet)
	if err != nil {
		return false, false, err
	}
	swept, includeMain := true, false
	for _, input := range tx.Inputs {
		key := deposit.OutpointKey(*input.Outpoint)
		r, err := m.Chain.Deposit(ctx, head.Number, key)
		if errors.Is(err, deposit.ErrNotFound) {
			if key != custody.MainOutpoint {
				out, outErr := m.Chain.CustodyOutput(ctx, head.Number, key)
				if outErr != nil || out.WalletID != wallet {
					return false, false, fmt.Errorf("sweep input %x is neither a deposit nor custody", key)
				}
			}
			includeMain = true
			continue
		}
		if err != nil {
			return false, false, err
		}
		swept = swept && r.SweptAt != 0
	}
	return swept, includeMain, nil
}

// ProveSweep submits the inclusion proof of a confirmed sweep through the
// ordinary route, or through the C-04 resolution route when resolution is
// set. It returns "already-credited" without submitting when every deposit is
// already swept, and "credited" when the submission (or an earlier one whose
// reply was lost) credited it.
func (m FrostSweepMaintainer) ProveSweep(ctx context.Context, wallet, hash [32]byte, vault [20]byte, resolution *FrostResolution) (FrostAction, error) {
	action := FrostAction{Kind: "sweep-proof", Transaction: hash}
	if resolution != nil {
		action.Kind, action.Reservation = "sweep-resolution-proof", resolution.Authorization.ReservationID
	}
	if m.Confirmations == 0 {
		return action, errors.New("confirmations must be positive")
	}
	n, err := m.Bitcoin.GetTransactionConfirmations(ctx, bitcoin.Hash(hash))
	if err != nil || n < m.Confirmations {
		return action, ErrSweepNotConfirmed
	}
	tx, proof, err := bitcoin.AssembleSpvProof(bitcoin.Hash(hash), m.Confirmations, m.Bitcoin)
	if err != nil {
		return action, err
	}
	if tx.Hash() != bitcoin.Hash(hash) {
		return action, errors.New("assembled transaction hash mismatch")
	}
	swept, includeMain, err := m.credited(ctx, wallet, tx)
	if err != nil {
		return action, err
	}
	if swept {
		action.Kind = "already-credited"
		return action, nil
	}
	request := deposit.SweepProof{WalletID: wallet, Transaction: TransactionInfo(tx), Proof: spvProof(proof), IncludeMain: includeMain, Vault: vault}
	if err = m.step("before-sweep-proof"); err != nil {
		return action, err
	}
	if resolution != nil {
		err = m.Chain.SubmitSweepResolutionProof(ctx, request, resolution.Authorization, resolution.Seats, resolution.Signatures)
	} else {
		err = m.Chain.SubmitSweepProof(ctx, request)
	}
	if err == nil {
		err = m.step("sweep-proof-submitted")
	}
	if err != nil {
		// The reply may be lost after the proof was mined: reread before
		// reporting failure, and never resubmit on a guess.
		if again, _, readErr := m.credited(ctx, wallet, tx); readErr != nil || !again {
			return action, fmt.Errorf("sweep proof: %w", err)
		}
	}
	if err = m.step("credit-seen"); err != nil {
		return action, err
	}
	return action, nil
}

// ErrFundingNotConfirmed reports a deposit funding transaction below the
// required confirmations.
var ErrFundingNotConfirmed = errors.New("FROST deposit funding is not confirmed")

// ReserveDeposit reserves one revealed deposit against the C-10 exposure
// budget with an SPV proof of its funding transaction, before any sweep
// plans it. A deposit reservation is never released, so it reserves only a
// deposit the planner could still sweep: funding unspent and median-time-past
// plus the refund margin before the locktime ("funding-spent" and
// "refund-margin" refuse without submitting). It is idempotent: a deposit
// already reserved or swept is not resubmitted, and a submission whose reply
// is lost (or that lost a race to another operator or to a sweep) is settled
// by rereading the record and the reservation.
func (m FrostSweepMaintainer) ReserveDeposit(ctx context.Context, key [32]byte) (FrostAction, error) {
	action := FrostAction{Kind: "deposit-reservation", Reservation: key}
	if m.Confirmations == 0 {
		return action, errors.New("confirmations must be positive")
	}
	if m.Funding == nil || m.Clock == nil {
		return action, errors.New("deposit reservation needs funding and clock readers")
	}
	reserved := func() (bool, deposit.Record, error) {
		head, err := m.Chain.Head(ctx)
		if err != nil {
			return false, deposit.Record{}, err
		}
		record, err := m.Chain.Deposit(ctx, head.Number, key)
		if err != nil {
			return false, record, err
		}
		ok, err := m.Chain.Reserved(ctx, head.Number, key)
		return ok, record, err
	}
	settled := func(ok bool, record deposit.Record) bool {
		switch {
		case record.SweptAt != 0:
			action.Kind = "already-swept"
		case ok:
			action.Kind = "already-reserved"
		default:
			return false
		}
		return true
	}
	ok, record, err := reserved()
	if err != nil {
		return action, err
	}
	action.Transaction = record.FundingTransactionHash
	if settled(ok, record) {
		return action, nil
	}
	hash := bitcoin.Hash(record.FundingTransactionHash)
	n, err := m.Bitcoin.GetTransactionConfirmations(ctx, hash)
	if err != nil || n < m.Confirmations {
		return action, ErrFundingNotConfirmed
	}
	unspent, err := m.Funding.Unspent(ctx, bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: record.FundingOutputIndex})
	if err != nil {
		return action, err
	}
	if !unspent {
		action.Kind = "funding-spent"
		return action, deposit.ErrFundingSpent
	}
	mtp, err := m.Clock.MedianTimePast(ctx)
	if err != nil {
		return action, err
	}
	if err = (deposit.Policy{RefundMargin: m.RefundMargin}).BeforeMargin(mtp, record); err != nil {
		action.Kind = "refund-margin"
		return action, err
	}
	assemble := m.assemble
	if assemble == nil {
		assemble = bitcoin.AssembleSpvProof
	}
	tx, proof, err := assemble(hash, m.Confirmations, m.Bitcoin)
	if err != nil {
		return action, err
	}
	if tx.Hash() != hash {
		return action, errors.New("assembled funding hash mismatch")
	}
	if err = m.step("before-deposit-reservation"); err != nil {
		return action, err
	}
	err = m.Chain.ReserveDeposit(ctx, key, TransactionInfo(tx), spvProof(proof))
	if err == nil {
		err = m.step("deposit-reservation-submitted")
	}
	if err != nil {
		// The reply may be lost after the reservation was mined, or another
		// operator's reservation or a sweep may have won: reread before
		// reporting failure, and report what the record now shows.
		again, record, readErr := reserved()
		if readErr != nil || !settled(again, record) {
			return action, fmt.Errorf("deposit reservation: %w", err)
		}
	}
	return action, nil
}

func spvProof(p *bitcoin.SpvProof) redemption.BitcoinProof {
	return redemption.BitcoinProof{MerkleProof: p.MerkleProof, TxIndexInBlock: uint64(p.TxIndexInBlock), BitcoinHeaders: p.BitcoinHeaders, CoinbasePreimage: p.CoinbasePreimage, CoinbaseProof: p.CoinbaseProof}
}
