// Package movement drives K-07 FROST fund movement and wallet closure on the
// host: C-06 commitments, movement and incoming sweep signatures, their proofs,
// and the C-09 closure check. It enables nothing in production; like K-04 to
// K-06 it runs only with an explicit LocalRegtest executor. The predicates and
// hooks are fixed in verification/BOUNDARIES.md (V-08).
package movement

import (
	"context"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// ErrNotFound reports an absent wallet, commitment or incoming record.
var ErrNotFound = errors.New("FROST movement record not found")

// State mirrors FrostWallets.State.
type State uint8

const (
	StateUnknown State = iota
	StateCandidate
	StateRegisteredPendingReady
	StateReadyUnfunded
	StateLive
	StateMovingFunds
	StateClosing
	StateClosed
	StateTerminated
)

// Wallet is the C-01 FROST wallet record of one label.
type Wallet struct {
	Descriptor        dkg.Descriptor
	ID                [32]byte
	DescriptorHash    [32]byte
	State             State
	Quarantined       bool
	CapabilityVersion uint32
}

// Label is the 20-byte C-01 label of a wallet ID.
func Label(id [32]byte) (l [20]byte) {
	copy(l[:], id[:20])
	return l
}

// Eligible is the C-06 isMovementTarget rule.
func (w Wallet) Eligible() bool {
	return (w.State == StateReadyUnfunded || w.State == StateLive) && !w.Quarantined && w.CapabilityVersion > 0
}

// Commitment mirrors FrostMovementState.Commitment.
type Commitment struct {
	MainOutpoint          [32]byte
	CommittedAt, ProvenAt uint64
	Escalated             bool
	// MaxFee is movingFundsTxMaxTotalFee when the commitment was recorded;
	// the movement proof and its fraud defense check this cap, never the
	// current parameter.
	MaxFee           uint64
	Targets          [][20]byte
	DescriptorHashes [][32]byte
}

// Lifecycle mirrors FrostMovementState.Lifecycle.
type Lifecycle struct {
	Period                                   uint32
	MovingFundsRequestedAt, ClosingStartedAt uint64
	Dust                                     bool
	ReadyAt, TerminatedAt                    uint64
}

// IncomingState mirrors FrostMovementState.IncomingState.
type IncomingState uint8

const (
	IncomingUnknown IncomingState = iota
	IncomingPending
	IncomingEscalated
	IncomingSwept
)

// Incoming mirrors FrostMovementState.Incoming.
type Incoming struct {
	Target, Source   [32]byte
	Value, CreatedAt uint64
	State            IncomingState
	// SweepFeeCap is movedFundsSweepTxMaxTotalFee when the output was
	// recorded; the C-08 self-transfer defense checks this cap (K-08).
	SweepFeeCap uint64
}

// Counters are the C-06 per-wallet counters and the global ones.
type Counters struct {
	PendingIncoming, InboundCommitments, EligibleTargets uint64
	DrainEnabled                                         bool
}

// Parameters are the legacy moving funds parameters C-06 shares.
type Parameters struct {
	MovingFundsTxMaxTotalFee, MovingFundsDustThreshold uint64
	MovedFundsSweepTxMaxTotalFee, WalletMaxBtcTransfer uint64
}

// Log is one event with its block, so a caller can check it is canonical.
type Log struct {
	Block     uint64
	BlockHash [32]byte
}

// CommitmentLog is one FrostMovingFundsCommitted event.
type CommitmentLog struct {
	Log
	Wallet [32]byte
	Period uint32
}

// IncomingLog is one FrostIncomingRecorded event. TransactionHash is the raw
// (internal-order) hash of the movement transaction.
type IncomingLog struct {
	Log
	Outpoint, Target, Source [32]byte
	Value                    uint64
	TransactionHash          [32]byte
	OutputIndex              uint32
}

// Closure obligation bits, as frostClosureObligations returns them.
const (
	OpenCustody uint64 = 1 << iota
	OpenPendingIncoming
	OpenCommittedIncoming
	OpenUnprovenCommitment
	// Bit 16 is unused: revealed deposits no longer block closure; a late
	// sweep of a real deposit reopens a Closing or Closed wallet instead.
	_
	OpenEscrow
	OpenReservation
	OpenChallenge
)

// Reader is the read half of the C-06/C-09 seam. Every At method reads at one
// block number. It extends the C-05 reader for deposits, custody and heads.
type Reader interface {
	deposit.Reader
	Wallet(ctx context.Context, at uint64, label [20]byte) (Wallet, error)
	ReadyWallets(ctx context.Context, from, to uint64) ([][32]byte, error)
	Commitment(ctx context.Context, at uint64, wallet [32]byte, period uint32) (Commitment, error)
	CommitmentLog(ctx context.Context, wallet [32]byte, period uint32) (CommitmentLog, error)
	Lifecycle(ctx context.Context, at uint64, wallet [32]byte) (Lifecycle, error)
	Incoming(ctx context.Context, at uint64, outpoint [32]byte) (Incoming, error)
	IncomingLogs(ctx context.Context, target [32]byte, from, to uint64) ([]IncomingLog, error)
	Counters(ctx context.Context, at uint64, wallet [32]byte) (Counters, error)
	Obligations(ctx context.Context, at uint64, wallet [32]byte) (uint64, error)
	MovementParameters(ctx context.Context, at uint64) (Parameters, error)
	ProvenTransaction(ctx context.Context, at uint64, hash [32]byte) ([32]byte, error)
}

// Proof is a Bitcoin transaction with its SPV proof.
type Proof struct {
	Transaction redemption.BitcoinInfo
	Proof       redemption.BitcoinProof
}

// Writer is the write half. Calls return after the transaction is mined; a
// revert is an error. SubmitCommitment is sent by the caller's own operator
// account; the proofs by an SPV maintainer.
type Writer interface {
	SubmitCommitment(ctx context.Context, wallet [32]byte, seat uint16, targets [][20]byte) error
	SubmitMovementProof(ctx context.Context, wallet [32]byte, proof Proof) error
	SubmitIncomingSweepProof(ctx context.Context, wallet [32]byte, proof Proof) error
	NotifyCloseable(ctx context.Context, wallet [32]byte) error
	FinalizeClosing(ctx context.Context, wallet [32]byte) error
	NotifyBelowDust(ctx context.Context, wallet [32]byte) error
	ResetMovingFundsTimeout(ctx context.Context, wallet [32]byte) error
}

// Chain is the complete seam.
type Chain interface {
	Reader
	Writer
}
