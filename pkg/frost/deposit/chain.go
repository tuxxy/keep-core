package deposit

import (
	"context"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// ErrNotFound reports an absent deposit record or reveal log.
var ErrNotFound = errors.New("FROST deposit record not found")

// Record mirrors C-05 FrostFundingState.Deposit. FundingTransactionHash is the
// raw (internal-order) Bitcoin hash.
type Record struct {
	Terms                                   Terms
	Receipt, FundingTransactionHash         [32]byte
	FundingOutputIndex                      uint32
	DepositOutputKey, WalletKey, MerkleRoot [32]byte
	OutputParity                            uint8
	TreasuryFee, MaximumSweepFee            uint64
	RevealedAt, SweptAt                     uint64
}

// Reveal is one FrostDepositRevealed log with its block.
type Reveal struct {
	Outpoint, WalletID [32]byte
	Block              uint64
	BlockHash          [32]byte
}

// Reader is the read half of the C-05 seam. Every At method reads at one block
// number. It extends the C-04 reader for custody, reservations and heads.
type Reader interface {
	redemption.Reader
	Deposit(ctx context.Context, at uint64, outpoint [32]byte) (Record, error)
	Reveals(ctx context.Context, wallet [32]byte, from, to uint64) ([]Reveal, error)
	Reveal(ctx context.Context, outpoint [32]byte) (Reveal, error)
}

// SweepProof is the submitFrostDepositSweepProof calldata.
type SweepProof struct {
	WalletID    [32]byte
	Transaction redemption.BitcoinInfo
	Proof       redemption.BitcoinProof
	IncludeMain bool
	Vault       [20]byte
}

// ResolutionAuthorization is the C-04 sweep-resolution certificate tuple.
// TransactionHash is the raw (internal-order) Bitcoin hash of the sweep.
type ResolutionAuthorization struct {
	ReservationID, TransactionHash [32]byte
	Vault                          [20]byte
	ValidUntilBlock                uint64
}

// Writer is the SPV-maintainer write half. Calls return after the transaction
// is mined; a revert is an error.
type Writer interface {
	SubmitSweepProof(ctx context.Context, proof SweepProof) error
	SubmitSweepResolutionProof(ctx context.Context, proof SweepProof, authorization ResolutionAuthorization, seats []uint16, signatures []byte) error
}

// Chain is the complete C-05 seam.
type Chain interface {
	Reader
	Writer
}
