// Package redemption is the K-05 host for FROST redemptions. It connects the
// K-04 exact-authorization executor to the C-04 Bridge reservation, timeout,
// cancellation and settlement API. See README.md.
package redemption

import (
	"context"
	"errors"
)

// RequestState mirrors C-04 FrostRedemptionLedger.RequestState.
type RequestState uint8

const (
	RequestUnknown RequestState = iota
	RequestRequested
	RequestReserved
	RequestTimeoutPending
	RequestPaid
	RequestRefunded
	RequestRefundable
)

// IntentState mirrors C-04 FrostRedemptionLedger.IntentState.
type IntentState uint8

const (
	IntentUnknown IntentState = iota
	IntentReserved
	IntentPaid
	IntentCancelled
	IntentTimeoutPending
	IntentQuarantined
	IntentConflictResolved
)

// Unresolved reports whether payment or cancellation proofs may still settle.
func (s IntentState) Unresolved() bool {
	return s == IntentReserved || s == IntentTimeoutPending || s == IntentQuarantined
}

// ExecutorState is the state string the K-04 executor compares.
func (s IntentState) ExecutorState() string {
	switch s {
	case IntentReserved:
		return "Reserved"
	case IntentPaid:
		return "Paid"
	case IntentCancelled:
		return "Cancelled"
	case IntentTimeoutPending:
		return "TimeoutPending"
	case IntentQuarantined:
		return "Quarantined"
	case IntentConflictResolved:
		return "ConflictResolved"
	}
	return "Unknown"
}

// Head is one canonical Ethereum block.
type Head struct {
	Number uint64
	Hash   [32]byte
}

// Configuration is the C-04 redemption configuration.
type Configuration struct {
	BitcoinGenesis           [32]byte
	FinalityBlocks           uint64
	MaximumCertificateBlocks uint64
	Enabled                  bool
	Watchtower               [20]byte
}

// Request is a funded C-04 redemption request with its captured terms.
type Request struct {
	ID, WalletID, DescriptorHash, ReservationID [32]byte
	Redeemer                                    [20]byte
	Gross, TreasuryFee, MaximumBitcoinFee       uint64
	Deadline, FinalityBlocks, TimeoutBlock      uint64
	State                                       RequestState
	Script                                      []byte
	NotBefore                                   uint64
}

// Certificate is the C-04 reservation certificate tuple (v2).
type Certificate struct {
	WalletID, DescriptorHash                   [32]byte
	Generation, StateVersion                   uint64
	IntentCommitment, RequestIDsHash           [32]byte
	ConflictTxHash                             [32]byte
	ConflictOutputIndex                        uint32
	MinimumFee, MaximumFee, MaximumSatPerVByte uint64
	ValidUntilBlock                            uint64
}

// UTXO is a C-04 recognized custody input in raw (internal) hash order.
type UTXO struct {
	TransactionHash [32]byte
	OutputIndex     uint32
	Value           uint64
}

// Intent is the admitted reservation record.
type Intent struct {
	ID                        [32]byte
	Certificate               Certificate
	AdmittedBlock             uint64
	Snapshot, TransactionHash [32]byte
	State                     IntentState
	RequestIDs                [][32]byte
	Payouts                   []uint64
	Inputs                    []UTXO
	Seats                     []uint16
	ReservedNet               uint64
}

// Custody is the per-wallet C-04 custody record.
type Custody struct {
	Generation, StateVersion, Value uint64
	MainOutpoint, ActiveReservation [32]byte
	CancellationExposure            uint64
}

// CustodyOutput is one recognized custody output keyed by outpoint key.
type CustodyOutput struct {
	WalletID [32]byte
	Value    uint64
	Spent    bool
}

// Alternative is one admitted cancellation alternative.
type Alternative struct {
	TransactionHash, Snapshot [32]byte
	Fee                       uint64
	Sponsor                   [20]byte
	AdmittedBlock             uint64
	ValidUntilBlock           uint64
	Seats                     []uint16
	BudgetSettled             bool
}

// Admission is the log that admitted a reservation or cancellation.
type Admission struct {
	ReservationID   [32]byte
	TransactionHash [32]byte
	Block           uint64
	BlockHash       [32]byte
}

// RequestEvent is a FrostRedemptionRequested log.
type RequestEvent struct {
	RequestID, WalletID [32]byte
	Block               uint64
}

// CancellationAuthorization is the C-04 cancellation certificate tuple.
type CancellationAuthorization struct {
	ReservationID, Snapshot, TransactionHash [32]byte
	Fee, ValidUntilBlock                     uint64
}

// BitcoinInfo and BitcoinProof are the BitcoinTx structs.
type BitcoinInfo struct {
	Version      [4]byte
	InputVector  []byte
	OutputVector []byte
	Locktime     [4]byte
}
type BitcoinProof struct {
	MerkleProof      []byte
	TxIndexInBlock   uint64
	BitcoinHeaders   []byte
	CoinbasePreimage [32]byte
	CoinbaseProof    []byte
}

// ReservationProposal is the reserveFrostRedemption calldata.
type ReservationProposal struct {
	Certificate Certificate
	Transaction BitcoinInfo
	Inputs      []UTXO
	Requests    []ProposalRequest
	Seats       []uint16
	Signatures  []byte
}
type ProposalRequest struct {
	ID     [32]byte
	Value  uint64
	Script []byte
}

// ErrNotFound reports an absent chain record or log.
var ErrNotFound = errors.New("FROST redemption record not found")

// Reader is the read half of the C-04 seam. Every At method reads the state at
// one block number so a caller can build an internally consistent snapshot
// and then confirm the block is still canonical.
type Reader interface {
	ChainID() uint64
	Bridge() [20]byte
	Head(ctx context.Context) (Head, error)
	HeaderHash(ctx context.Context, number uint64) ([32]byte, error)
	Timestamp(ctx context.Context, number uint64) (uint64, error)
	Configuration(ctx context.Context, at uint64) (Configuration, error)
	MaximumTotalFee(ctx context.Context, at uint64) (uint64, error)
	Request(ctx context.Context, at uint64, id [32]byte) (Request, error)
	Intent(ctx context.Context, at uint64, reservation [32]byte) (Intent, error)
	Custody(ctx context.Context, at uint64, wallet [32]byte) (Custody, error)
	CustodyOutput(ctx context.Context, at uint64, outpoint [32]byte) (CustodyOutput, error)
	Alternatives(ctx context.Context, at uint64, reservation [32]byte) ([]Alternative, error)
	RequestEvents(ctx context.Context, wallet [32]byte, from, to uint64) ([]RequestEvent, error)
	Admissions(ctx context.Context, wallet [32]byte, from, to uint64) ([]Admission, error)
	Admission(ctx context.Context, reservation [32]byte) (Admission, error)
	CancellationAdmission(ctx context.Context, reservation, transactionHash [32]byte) (Admission, error)
}

// Writer is the permissionless write half. Every call returns after the
// transaction is mined; a revert is an error.
type Writer interface {
	Reserve(ctx context.Context, proposal ReservationProposal) error
	AuthorizeCancellation(ctx context.Context, authorization CancellationAuthorization, transaction BitcoinInfo, seats []uint16, signatures []byte) error
	SubmitPaymentProof(ctx context.Context, reservation [32]byte, transaction BitcoinInfo, proof BitcoinProof) error
	SubmitCancellationProof(ctx context.Context, reservation [32]byte, transaction BitcoinInfo, proof BitcoinProof) error
	NotifyTimeout(ctx context.Context, request [32]byte) error
	RefundUnreserved(ctx context.Context, request [32]byte) error
	RefundResolved(ctx context.Context, request [32]byte) error
	ClaimCancellationBudget(ctx context.Context, reservation, transactionHash [32]byte) error
}

// Chain is the complete C-04 seam.
type Chain interface {
	Reader
	Writer
}
