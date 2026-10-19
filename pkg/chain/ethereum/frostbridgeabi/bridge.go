// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package frostbridgeabi

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// BitcoinTxInfo is an auto generated low-level Go binding around an user-defined struct.
type BitcoinTxInfo struct {
	Version      [4]byte
	InputVector  []byte
	OutputVector []byte
	Locktime     [4]byte
}

// BitcoinTxProof is an auto generated low-level Go binding around an user-defined struct.
type BitcoinTxProof struct {
	MerkleProof      []byte
	TxIndexInBlock   *big.Int
	BitcoinHeaders   []byte
	CoinbasePreimage [32]byte
	CoinbaseProof    []byte
}

// BitcoinTxUTXO is an auto generated low-level Go binding around an user-defined struct.
type BitcoinTxUTXO struct {
	TxHash        [32]byte
	TxOutputIndex uint32
	TxOutputValue uint64
}

// FrostCancellationCertificateAuthorization is an auto generated low-level Go binding around an user-defined struct.
type FrostCancellationCertificateAuthorization struct {
	ReservationId   [32]byte
	Snapshot        [32]byte
	TransactionHash [32]byte
	Fee             uint64
	ValidUntilBlock uint64
}

// FrostCancellationHistoryRecord is an auto generated low-level Go binding around an user-defined struct.
type FrostCancellationHistoryRecord struct {
	TransactionHash [32]byte
	Snapshot        [32]byte
	Fee             uint64
	Sponsor         common.Address
	AdmittedBlock   uint64
	ValidUntilBlock uint64
	Seats           []uint16
	Signatures      []byte
	BudgetSettled   bool
}

// FrostCustodyOutput is an auto generated low-level Go binding around an user-defined struct.
type FrostCustodyOutput struct {
	WalletId [32]byte
	Value    uint64
	Spent    bool
}

// FrostCustodyWallet is an auto generated low-level Go binding around an user-defined struct.
type FrostCustodyWallet struct {
	Generation           uint64
	StateVersion         uint64
	Value                uint64
	MainOutpoint         [32]byte
	ActiveReservation    [32]byte
	CancellationExposure uint64
}

// FrostDepositAdmissionConstruction is an auto generated low-level Go binding around an user-defined struct.
type FrostDepositAdmissionConstruction struct {
	Output        FrostDepositAdmissionOutput
	Receipt       [32]byte
	WalletLeaf    []byte
	RefundLeaf    []byte
	WalletControl []byte
	RefundControl []byte
}

// FrostDepositAdmissionOutput is an auto generated low-level Go binding around an user-defined struct.
type FrostDepositAdmissionOutput struct {
	Key    [32]byte
	Root   [32]byte
	Parity uint8
}

// FrostDepositProfileTerms is an auto generated low-level Go binding around an user-defined struct.
type FrostDepositProfileTerms struct {
	ChainId        *big.Int
	Bridge         common.Address
	BitcoinGenesis [32]byte
	FullWalletId   [32]byte
	DescriptorHash [32]byte
	Depositor      common.Address
	Vault          common.Address
	Blinding       [8]byte
	Amount         uint64
	RefundKey      [32]byte
	RefundLocktime uint32
	ExtraData      [32]byte
}

// FrostDepositSweepResolution is an auto generated low-level Go binding around an user-defined struct.
type FrostDepositSweepResolution struct {
	Sweep         FrostDepositSweepSweep
	Authorization FrostSweepResolutionCertificateAuthorization
	Seats         []uint16
	Signatures    []byte
}

// FrostDepositSweepSweep is an auto generated low-level Go binding around an user-defined struct.
type FrostDepositSweepSweep struct {
	WalletId    [32]byte
	Transaction BitcoinTxInfo
	Proof       BitcoinTxProof
	IncludeMain bool
	Vault       common.Address
}

// FrostFraudDefense is an auto generated low-level Go binding around an user-defined struct.
type FrostFraudDefense struct {
	WalletId         [32]byte
	Transaction      BitcoinTxInfo
	Prevouts         []FrostFraudPrevout
	InputIndex       uint32
	LeafHash         [32]byte
	Authorization    uint8
	AuthorizationRef [32]byte
}

// FrostFraudPrevout is an auto generated low-level Go binding around an user-defined struct.
type FrostFraudPrevout struct {
	Value  uint64
	Script []byte
}

// FrostFraudStateChallenge is an auto generated low-level Go binding around an user-defined struct.
type FrostFraudStateChallenge struct {
	WalletId      [32]byte
	Digest        [32]byte
	Challenger    common.Address
	ReportedAt    uint64
	Resolved      bool
	DepositAmount *big.Int
}

// FrostMovementStateCommitment is an auto generated low-level Go binding around an user-defined struct.
type FrostMovementStateCommitment struct {
	MainOutpoint     [32]byte
	CommittedAt      uint64
	ProvenAt         uint64
	Escalated        bool
	Retired          bool
	MaxFee           uint64
	Targets          [][20]byte
	DescriptorHashes [][32]byte
}

// FrostMovementStateIncoming is an auto generated low-level Go binding around an user-defined struct.
type FrostMovementStateIncoming struct {
	Target      [32]byte
	Source      [32]byte
	Value       uint64
	CreatedAt   uint64
	State       uint8
	SweepFeeCap uint64
}

// FrostMovementStateLifecycle is an auto generated low-level Go binding around an user-defined struct.
type FrostMovementStateLifecycle struct {
	Period                 uint32
	MovingFundsRequestedAt uint64
	ClosingStartedAt       uint64
	Dust                   bool
	ReadyAt                uint64
	TerminatedAt           uint64
	InactivityNonce        uint64
}

// FrostRedemptionLedgerIntent is an auto generated low-level Go binding around an user-defined struct.
type FrostRedemptionLedgerIntent struct {
	Certificate                      FrostReservationCertificateReservation
	AdmittedBlock                    uint64
	Snapshot                         [32]byte
	TransactionHash                  [32]byte
	State                            uint8
	RequestIds                       [][32]byte
	Payouts                          []uint64
	Inputs                           []BitcoinTxUTXO
	Seats                            []uint16
	Signatures                       []byte
	CancellationHash                 [32]byte
	CancellationSnapshot             [32]byte
	CancellationFee                  uint64
	CancellationSponsor              common.Address
	CancellationAdmittedBlock        uint64
	CancellationSeats                []uint16
	CancellationSignatures           []byte
	ReservedNet                      uint64
	CancellationValidUntilBlock      uint64
	SweepResolutionHash              [32]byte
	SweepResolutionBlock             uint64
	SweepResolutionValidUntilBlock   uint64
	SweepResolutionSeats             []uint16
	SweepResolutionSignaturesHash    [32]byte
	SweepResolutionAuthorizationHash [32]byte
}

// FrostRedemptionLedgerRequest is an auto generated low-level Go binding around an user-defined struct.
type FrostRedemptionLedgerRequest struct {
	WalletId          [32]byte
	DescriptorHash    [32]byte
	ReservationId     [32]byte
	Redeemer          common.Address
	Gross             uint64
	TreasuryFee       uint64
	MaximumBitcoinFee uint64
	Deadline          uint64
	FinalityBlocks    uint64
	TimeoutBlock      uint64
	State             uint8
	Script            []byte
}

// FrostRedemptionStateConfiguration is an auto generated low-level Go binding around an user-defined struct.
type FrostRedemptionStateConfiguration struct {
	BitcoinGenesis           [32]byte
	FinalityBlocks           uint64
	MaximumCertificateBlocks uint64
	Enabled                  bool
	Watchtower               common.Address
	LegacyTransitionEvidence [32]byte
}

// FrostRedemptionStateQuarantineRecord is an auto generated low-level Go binding around an user-defined struct.
type FrostRedemptionStateQuarantineRecord struct {
	ReservationId        [32]byte
	ReasonHash           [32]byte
	EvidenceHash         [32]byte
	Authority            common.Address
	RecordedBlock        uint64
	PreviousStateVersion uint64
	StateVersion         uint64
}

// FrostRedemptionStateRequestPolicy is an auto generated low-level Go binding around an user-defined struct.
type FrostRedemptionStateRequestPolicy struct {
	Watchtower common.Address
	PolicyId   [32]byte
	NotBefore  uint64
}

// FrostRedemptionStateReservationProposal is an auto generated low-level Go binding around an user-defined struct.
type FrostRedemptionStateReservationProposal struct {
	Certificate FrostReservationCertificateReservation
	Transaction BitcoinTxInfo
	Inputs      []BitcoinTxUTXO
	Requests    []FrostSigningIntentRequest
	Seats       []uint16
	Signatures  []byte
}

// FrostReservationCertificateReservation is an auto generated low-level Go binding around an user-defined struct.
type FrostReservationCertificateReservation struct {
	WalletId            [32]byte
	DescriptorHash      [32]byte
	Generation          uint64
	StateVersion        uint64
	IntentCommitment    [32]byte
	RequestIdsHash      [32]byte
	ConflictTxHash      [32]byte
	ConflictOutputIndex uint32
	MinimumFee          uint64
	MaximumFee          uint64
	MaximumSatPerVByte  uint64
	ValidUntilBlock     uint64
}

// FrostSigningIntentRequest is an auto generated low-level Go binding around an user-defined struct.
type FrostSigningIntentRequest struct {
	Id     [32]byte
	Value  uint64
	Script []byte
}

// FrostSweepResolutionCertificateAuthorization is an auto generated low-level Go binding around an user-defined struct.
type FrostSweepResolutionCertificateAuthorization struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Vault           common.Address
	ValidUntilBlock uint64
}

// FrostBridgeMetaData contains all meta data concerning the FrostBridge contract.
var FrostBridgeMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"FrostDrainEnabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes20\",\"name\":\"source\",\"type\":\"bytes20\"}],\"name\":\"FrostDrainReleased\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"}],\"name\":\"FrostFraudChallengeDefeatTimedOut\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"}],\"name\":\"FrostFraudChallengeDefeated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"FrostFraudChallengeSubmitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"addressesEnabled\",\"type\":\"bool\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"revealEnabled\",\"type\":\"bool\"}],\"name\":\"FrostFundingGatesChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"verifier\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"runtimeHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"qualificationRecord\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"bitcoinGenesis\",\"type\":\"bytes32\"}],\"name\":\"FrostFundingInstalled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"}],\"name\":\"FrostIncomingEscalated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"target\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"source\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"outputIndex\",\"type\":\"uint32\"}],\"name\":\"FrostIncomingRecorded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"FrostMovedFundsSwept\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"FrostMovingFundsBelowDust\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"period\",\"type\":\"uint32\"}],\"name\":\"FrostMovingFundsCommitmentRetired\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"period\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"mainOutpoint\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes20[]\",\"name\":\"targets\",\"type\":\"bytes20[]\"},{\"indexed\":false,\"internalType\":\"bytes32[]\",\"name\":\"descriptorHashes\",\"type\":\"bytes32[]\"}],\"name\":\"FrostMovingFundsCommitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"FrostMovingFundsCompleted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"FrostMovingFundsEscalated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"FrostMovingFundsTimeoutReset\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint16[]\",\"name\":\"inactiveSeats\",\"type\":\"uint16[]\"},{\"indexed\":false,\"internalType\":\"address[]\",\"name\":\"inactiveOperators\",\"type\":\"address[]\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"heartbeatFailed\",\"type\":\"bool\"}],\"name\":\"FrostOperatorInactivityNotified\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"reason\",\"type\":\"bytes32\"}],\"name\":\"FrostSanctionNotified\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletClosed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletClosing\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"reason\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletMovingFunds\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletReopened\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"descriptor\",\"type\":\"bytes\"},{\"internalType\":\"uint64\",\"name\":\"deadline\",\"type\":\"uint64\"}],\"name\":\"__snowfallWalletCreated\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"}],\"name\":\"__snowfallWalletExpired\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"generation\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"capabilityVersion\",\"type\":\"uint32\"}],\"name\":\"__snowfallWalletReady\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"snapshot\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"fee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"validUntilBlock\",\"type\":\"uint64\"}],\"internalType\":\"structFrostCancellationCertificate.Authorization\",\"name\":\"authorization\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"}],\"name\":\"authorizeFrostCancellation\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"claimFrostCancellationBudget\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"verifier\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"runtimeHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"qualificationRecord\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"bitcoinGenesis\",\"type\":\"bytes32\"}],\"name\":\"configureFrostFunding\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"genesis\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"finalityBlocks\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumCertificateBlocks\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"watchtower\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"legacyTransitionEvidence\",\"type\":\"bytes32\"}],\"name\":\"configureFrostRedemptions\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"}],\"name\":\"configureFrostRegistry\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"},{\"internalType\":\"bytes\",\"name\":\"script\",\"type\":\"bytes\"}],\"internalType\":\"structFrostFraud.Prevout[]\",\"name\":\"prevouts\",\"type\":\"tuple[]\"},{\"internalType\":\"uint32\",\"name\":\"inputIndex\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"leafHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"authorization\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"authorizationRef\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostFraud.Defense\",\"name\":\"defense\",\"type\":\"tuple\"}],\"name\":\"defeatFrostFraudChallenge\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"}],\"name\":\"defeatFrostFraudChallengeWithHeartbeat\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"finalizeFrostWalletClosing\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"frostCancellationAlternative\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"snapshot\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"fee\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"sponsor\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"admittedBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"validUntilBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"bool\",\"name\":\"budgetSettled\",\"type\":\"bool\"}],\"internalType\":\"structFrostCancellationHistory.Record\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"}],\"name\":\"frostCancellationCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"frostCancellationHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"}],\"name\":\"frostCancellationWinner\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostClosureObligations\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"open\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"}],\"name\":\"frostCustodyOutput\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"spent\",\"type\":\"bool\"}],\"internalType\":\"structFrostCustody.Output\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"}],\"name\":\"frostDeposit\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"bridge\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"bitcoinGenesis\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"fullWalletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"},{\"internalType\":\"bytes8\",\"name\":\"blinding\",\"type\":\"bytes8\"},{\"internalType\":\"uint64\",\"name\":\"amount\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"refundKey\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"refundLocktime\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"extraData\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDepositProfile.Terms\",\"name\":\"terms\",\"type\":\"tuple\"}],\"name\":\"frostDepositConstruction\",\"outputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"key\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"parity\",\"type\":\"uint8\"}],\"internalType\":\"structFrostDepositAdmission.Output\",\"name\":\"output\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"receipt\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"walletLeaf\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"refundLeaf\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"walletControl\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"refundControl\",\"type\":\"bytes\"}],\"internalType\":\"structFrostDepositAdmission.Construction\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"bridge\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"bitcoinGenesis\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"fullWalletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"},{\"internalType\":\"bytes8\",\"name\":\"blinding\",\"type\":\"bytes8\"},{\"internalType\":\"uint64\",\"name\":\"amount\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"refundKey\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"refundLocktime\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"extraData\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDepositProfile.Terms\",\"name\":\"terms\",\"type\":\"tuple\"}],\"name\":\"frostDepositOutput\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"key\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"parity\",\"type\":\"uint8\"}],\"internalType\":\"structFrostDepositAdmission.Output\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"}],\"name\":\"frostDepositRefundConstruction\",\"outputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"key\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"parity\",\"type\":\"uint8\"}],\"internalType\":\"structFrostDepositAdmission.Output\",\"name\":\"output\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"receipt\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"walletLeaf\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"refundLeaf\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"walletControl\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"refundControl\",\"type\":\"bytes\"}],\"internalType\":\"structFrostDepositAdmission.Construction\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes20\",\"name\":\"source\",\"type\":\"bytes20\"}],\"name\":\"frostDrainTargets\",\"outputs\":[{\"internalType\":\"bytes20[]\",\"name\":\"\",\"type\":\"bytes20[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"}],\"name\":\"frostFraudChallenge\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"challenger\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"reportedAt\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"resolved\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"depositAmount\",\"type\":\"uint256\"}],\"internalType\":\"structFrostFraudState.Challenge\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"}],\"name\":\"frostHeartbeatDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"},{\"internalType\":\"uint16[]\",\"name\":\"inactiveSeats\",\"type\":\"uint16[]\"},{\"internalType\":\"bool\",\"name\":\"heartbeatFailed\",\"type\":\"bool\"}],\"name\":\"frostInactivityClaimDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostInactivityNonce\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"}],\"name\":\"frostIncoming\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"target\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"source\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"createdAt\",\"type\":\"uint64\"},{\"internalType\":\"enumFrostMovementState.IncomingState\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"uint64\",\"name\":\"sweepFeeCap\",\"type\":\"uint64\"}],\"internalType\":\"structFrostMovementState.Incoming\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostMovementCounters\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"pendingIncoming\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"inboundCommitments\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"eligibleTargets\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"drainEnabled\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"period\",\"type\":\"uint32\"}],\"name\":\"frostMovingFundsCommitment\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"mainOutpoint\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"committedAt\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"provenAt\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"escalated\",\"type\":\"bool\"},{\"internalType\":\"bool\",\"name\":\"retired\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"maxFee\",\"type\":\"uint64\"},{\"internalType\":\"bytes20[]\",\"name\":\"targets\",\"type\":\"bytes20[]\"},{\"internalType\":\"bytes32[]\",\"name\":\"descriptorHashes\",\"type\":\"bytes32[]\"}],\"internalType\":\"structFrostMovementState.Commitment\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostOpenChallenges\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"frostProvenTransaction\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"frostRedemptionConfiguration\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"bitcoinGenesis\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"finalityBlocks\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumCertificateBlocks\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"},{\"internalType\":\"address\",\"name\":\"watchtower\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"legacyTransitionEvidence\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostRedemptionState.Configuration\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostRedemptionEscrow\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"gross\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"netLiability\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"}],\"name\":\"frostRedemptionPolicy\",\"outputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"watchtower\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"policyId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"notBefore\",\"type\":\"uint64\"}],\"internalType\":\"structFrostRedemptionState.RequestPolicy\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"}],\"name\":\"frostRedemptionRequest\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"redeemer\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"gross\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"treasuryFee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumBitcoinFee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"deadline\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"finalityBlocks\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"timeoutBlock\",\"type\":\"uint64\"},{\"internalType\":\"enumFrostRedemptionLedger.State\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"script\",\"type\":\"bytes\"}],\"internalType\":\"structFrostRedemptionLedger.Request\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"frostRegistry\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"}],\"name\":\"frostReservation\",\"outputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"generation\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"intentCommitment\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"requestIdsHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"conflictTxHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"conflictOutputIndex\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"minimumFee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumFee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumSatPerVByte\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"validUntilBlock\",\"type\":\"uint64\"}],\"internalType\":\"structFrostReservationCertificate.Reservation\",\"name\":\"certificate\",\"type\":\"tuple\"},{\"internalType\":\"uint64\",\"name\":\"admittedBlock\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"snapshot\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"internalType\":\"enumFrostRedemptionLedger.IntentState\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"bytes32[]\",\"name\":\"requestIds\",\"type\":\"bytes32[]\"},{\"internalType\":\"uint64[]\",\"name\":\"payouts\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"txHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"txOutputIndex\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"txOutputValue\",\"type\":\"uint64\"}],\"internalType\":\"structBitcoinTx.UTXO[]\",\"name\":\"inputs\",\"type\":\"tuple[]\"},{\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"cancellationHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"cancellationSnapshot\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"cancellationFee\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"cancellationSponsor\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"cancellationAdmittedBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint16[]\",\"name\":\"cancellationSeats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"cancellationSignatures\",\"type\":\"bytes\"},{\"internalType\":\"uint64\",\"name\":\"reservedNet\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"cancellationValidUntilBlock\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"sweepResolutionHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"sweepResolutionBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"sweepResolutionValidUntilBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint16[]\",\"name\":\"sweepResolutionSeats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes32\",\"name\":\"sweepResolutionSignaturesHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"sweepResolutionAuthorizationHash\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostRedemptionLedger.Intent\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostWalletCustody\",\"outputs\":[{\"components\":[{\"internalType\":\"uint64\",\"name\":\"generation\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"mainOutpoint\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"activeReservation\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"cancellationExposure\",\"type\":\"uint64\"}],\"internalType\":\"structFrostCustody.Wallet\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"frostWalletLifecycle\",\"outputs\":[{\"components\":[{\"internalType\":\"uint32\",\"name\":\"period\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"movingFundsRequestedAt\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"closingStartedAt\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"dust\",\"type\":\"bool\"},{\"internalType\":\"uint64\",\"name\":\"readyAt\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"terminatedAt\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"inactivityNonce\",\"type\":\"uint64\"}],\"internalType\":\"structFrostMovementState.Lifecycle\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"}],\"name\":\"frostWalletQuarantine\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"evidenceHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"authority\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"recordedBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"previousStateVersion\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"}],\"internalType\":\"structFrostRedemptionState.QuarantineRecord\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes20\",\"name\":\"label\",\"type\":\"bytes20\"}],\"name\":\"isFrostFundingTarget\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostFraudChallengeDefeatTimeout\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostMovedFundsSweepTimeout\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostMovingFundsBelowDust\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostMovingFundsTimeout\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"},{\"internalType\":\"uint16[]\",\"name\":\"inactiveSeats\",\"type\":\"uint16[]\"},{\"internalType\":\"bool\",\"name\":\"heartbeatFailed\",\"type\":\"bool\"},{\"internalType\":\"uint16[]\",\"name\":\"signingSeats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"}],\"name\":\"notifyFrostOperatorInactivity\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostRedemptionTimeout\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostWalletCloseable\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"}],\"name\":\"notifyFrostWalletRedemptionTimeout\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"expectedStateVersion\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"evidenceHash\",\"type\":\"bytes32\"}],\"name\":\"quarantineFrostWallet\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"}],\"name\":\"refundResolvedFrostRedemption\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"}],\"name\":\"refundUnreservedFrostRedemption\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes20\",\"name\":\"source\",\"type\":\"bytes20\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"txHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"txOutputIndex\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"txOutputValue\",\"type\":\"uint64\"}],\"internalType\":\"structBitcoinTx.UTXO\",\"name\":\"main\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"}],\"name\":\"releaseFrostDrainOnConflict\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"script\",\"type\":\"bytes\"},{\"internalType\":\"uint64\",\"name\":\"gross\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"redeemer\",\"type\":\"address\"}],\"name\":\"requestFrostRedemption\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"requestNewFrostWallet\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"generation\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"intentCommitment\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"requestIdsHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"conflictTxHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"conflictOutputIndex\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"minimumFee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumFee\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"maximumSatPerVByte\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"validUntilBlock\",\"type\":\"uint64\"}],\"internalType\":\"structFrostReservationCertificate.Reservation\",\"name\":\"certificate\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"txHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"txOutputIndex\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"txOutputValue\",\"type\":\"uint64\"}],\"internalType\":\"structBitcoinTx.UTXO[]\",\"name\":\"inputs\",\"type\":\"tuple[]\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"},{\"internalType\":\"bytes\",\"name\":\"script\",\"type\":\"bytes\"}],\"internalType\":\"structFrostSigningIntent.Request[]\",\"name\":\"requests\",\"type\":\"tuple[]\"},{\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"}],\"internalType\":\"structFrostRedemptionState.ReservationProposal\",\"name\":\"proposal\",\"type\":\"tuple\"}],\"name\":\"reserveFrostRedemption\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"resetFrostMovingFundsTimeout\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"output\",\"type\":\"bytes\"}],\"name\":\"resolveFrostOutputScript\",\"outputs\":[{\"internalType\":\"bytes20\",\"name\":\"\",\"type\":\"bytes20\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"}],\"name\":\"resolveWallet\",\"outputs\":[{\"internalType\":\"bytes20\",\"name\":\"\",\"type\":\"bytes20\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"q\",\"type\":\"bytes32\"}],\"name\":\"resolveWalletOutput\",\"outputs\":[{\"internalType\":\"bytes20\",\"name\":\"\",\"type\":\"bytes20\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"bridge\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"bitcoinGenesis\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"fullWalletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"},{\"internalType\":\"bytes8\",\"name\":\"blinding\",\"type\":\"bytes8\"},{\"internalType\":\"uint64\",\"name\":\"amount\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"refundKey\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"refundLocktime\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"extraData\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDepositProfile.Terms\",\"name\":\"terms\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"internalType\":\"uint32\",\"name\":\"outputIndex\",\"type\":\"uint32\"}],\"name\":\"revealFrostDeposit\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"setFrostDrainEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"addressesEnabled\",\"type\":\"bool\"},{\"internalType\":\"bool\",\"name\":\"revealEnabled\",\"type\":\"bool\"}],\"name\":\"setFrostFundingGates\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"setFrostRedemptionsEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"setFrostRequestsEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"}],\"name\":\"submitFrostCancellationProof\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"},{\"internalType\":\"bool\",\"name\":\"includeMain\",\"type\":\"bool\"},{\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"}],\"internalType\":\"structFrostDepositSweep.Sweep\",\"name\":\"request\",\"type\":\"tuple\"}],\"name\":\"submitFrostDepositSweepProof\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"digest\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"submitFrostFraudChallenge\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"}],\"name\":\"submitFrostMovedFundsSweepProof\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"uint16\",\"name\":\"seat\",\"type\":\"uint16\"},{\"internalType\":\"bytes20[]\",\"name\":\"targets\",\"type\":\"bytes20[]\"}],\"name\":\"submitFrostMovingFundsCommitment\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"}],\"name\":\"submitFrostMovingFundsProof\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"}],\"name\":\"submitFrostRedemptionProof\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes4\",\"name\":\"version\",\"type\":\"bytes4\"},{\"internalType\":\"bytes\",\"name\":\"inputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"outputVector\",\"type\":\"bytes\"},{\"internalType\":\"bytes4\",\"name\":\"locktime\",\"type\":\"bytes4\"}],\"internalType\":\"structBitcoinTx.Info\",\"name\":\"transaction\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes\",\"name\":\"merkleProof\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"txIndexInBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"bitcoinHeaders\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"coinbasePreimage\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"coinbaseProof\",\"type\":\"bytes\"}],\"internalType\":\"structBitcoinTx.Proof\",\"name\":\"proof\",\"type\":\"tuple\"},{\"internalType\":\"bool\",\"name\":\"includeMain\",\"type\":\"bool\"},{\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"}],\"internalType\":\"structFrostDepositSweep.Sweep\",\"name\":\"sweep\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"validUntilBlock\",\"type\":\"uint64\"}],\"internalType\":\"structFrostSweepResolutionCertificate.Authorization\",\"name\":\"authorization\",\"type\":\"tuple\"},{\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"}],\"internalType\":\"structFrostDepositSweep.Resolution\",\"name\":\"resolution\",\"type\":\"tuple\"}],\"name\":\"submitFrostSweepResolutionProof\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes20\",\"name\":\"label\",\"type\":\"bytes20\"}],\"name\":\"walletDescriptor\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes20\",\"name\":\"label\",\"type\":\"bytes20\"}],\"name\":\"walletScheme\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"fee\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"sponsor\",\"type\":\"address\"}],\"name\":\"FrostCancellationAuthorized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"sponsor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"fee\",\"type\":\"uint64\"}],\"name\":\"FrostCancellationBudgetReturned\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"FrostCancellationConfirmed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"outpoint\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"receipt\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"amount\",\"type\":\"uint64\"}],\"name\":\"FrostDepositRevealed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"outputValue\",\"type\":\"uint64\"}],\"name\":\"FrostDepositsSwept\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"FrostRedemptionPaid\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"redeemer\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"gross\",\"type\":\"uint64\"}],\"name\":\"FrostRedemptionRefundAvailable\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"redeemer\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"gross\",\"type\":\"uint64\"}],\"name\":\"FrostRedemptionRefunded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"redeemer\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"gross\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"treasuryFee\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"maximumBitcoinFee\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"deadline\",\"type\":\"uint64\"}],\"name\":\"FrostRedemptionRequested\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"requestId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"timeoutBlock\",\"type\":\"uint64\"}],\"name\":\"FrostRedemptionTimeoutPending\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"genesis\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"finalityBlocks\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"maximumCertificateBlocks\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"watchtower\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"legacyTransitionEvidence\",\"type\":\"bytes32\"}],\"name\":\"FrostRedemptionsConfigured\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"FrostRedemptionsEnabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"}],\"name\":\"FrostRegistryConfigured\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"FrostRequestsEnabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"intentCommitment\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"generation\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"}],\"name\":\"FrostReservationAdmitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"}],\"name\":\"FrostReservationResolvedBySweep\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"transactionHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"vault\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"validUntilBlock\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"}],\"name\":\"FrostSweepResolutionApproved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletExpired\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reservationId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"stateVersion\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"evidenceHash\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletQuarantined\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"}],\"name\":\"FrostWalletReady\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes20\",\"name\":\"label\",\"type\":\"bytes20\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"deadline\",\"type\":\"uint64\"}],\"name\":\"FrostWalletRegistered\",\"type\":\"event\"}]",
}

// FrostBridgeABI is the input ABI used to generate the binding from.
// Deprecated: Use FrostBridgeMetaData.ABI instead.
var FrostBridgeABI = FrostBridgeMetaData.ABI

// FrostBridge is an auto generated Go binding around an Ethereum contract.
type FrostBridge struct {
	FrostBridgeCaller     // Read-only binding to the contract
	FrostBridgeTransactor // Write-only binding to the contract
	FrostBridgeFilterer   // Log filterer for contract events
}

// FrostBridgeCaller is an auto generated read-only Go binding around an Ethereum contract.
type FrostBridgeCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostBridgeTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FrostBridgeTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostBridgeFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FrostBridgeFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostBridgeSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FrostBridgeSession struct {
	Contract     *FrostBridge      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// FrostBridgeCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FrostBridgeCallerSession struct {
	Contract *FrostBridgeCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// FrostBridgeTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FrostBridgeTransactorSession struct {
	Contract     *FrostBridgeTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// FrostBridgeRaw is an auto generated low-level Go binding around an Ethereum contract.
type FrostBridgeRaw struct {
	Contract *FrostBridge // Generic contract binding to access the raw methods on
}

// FrostBridgeCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FrostBridgeCallerRaw struct {
	Contract *FrostBridgeCaller // Generic read-only contract binding to access the raw methods on
}

// FrostBridgeTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FrostBridgeTransactorRaw struct {
	Contract *FrostBridgeTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFrostBridge creates a new instance of FrostBridge, bound to a specific deployed contract.
func NewFrostBridge(address common.Address, backend bind.ContractBackend) (*FrostBridge, error) {
	contract, err := bindFrostBridge(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FrostBridge{FrostBridgeCaller: FrostBridgeCaller{contract: contract}, FrostBridgeTransactor: FrostBridgeTransactor{contract: contract}, FrostBridgeFilterer: FrostBridgeFilterer{contract: contract}}, nil
}

// NewFrostBridgeCaller creates a new read-only instance of FrostBridge, bound to a specific deployed contract.
func NewFrostBridgeCaller(address common.Address, caller bind.ContractCaller) (*FrostBridgeCaller, error) {
	contract, err := bindFrostBridge(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeCaller{contract: contract}, nil
}

// NewFrostBridgeTransactor creates a new write-only instance of FrostBridge, bound to a specific deployed contract.
func NewFrostBridgeTransactor(address common.Address, transactor bind.ContractTransactor) (*FrostBridgeTransactor, error) {
	contract, err := bindFrostBridge(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeTransactor{contract: contract}, nil
}

// NewFrostBridgeFilterer creates a new log filterer instance of FrostBridge, bound to a specific deployed contract.
func NewFrostBridgeFilterer(address common.Address, filterer bind.ContractFilterer) (*FrostBridgeFilterer, error) {
	contract, err := bindFrostBridge(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFilterer{contract: contract}, nil
}

// bindFrostBridge binds a generic wrapper to an already deployed contract.
func bindFrostBridge(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FrostBridgeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FrostBridge *FrostBridgeRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FrostBridge.Contract.FrostBridgeCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FrostBridge *FrostBridgeRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostBridge.Contract.FrostBridgeTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FrostBridge *FrostBridgeRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FrostBridge.Contract.FrostBridgeTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FrostBridge *FrostBridgeCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FrostBridge.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FrostBridge *FrostBridgeTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostBridge.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FrostBridge *FrostBridgeTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FrostBridge.Contract.contract.Transact(opts, method, params...)
}

// FrostCancellationAlternative is a free data retrieval call binding the contract method 0xd8d8ee6a.
//
// Solidity: function frostCancellationAlternative(bytes32 reservationId, bytes32 transactionHash) view returns((bytes32,bytes32,uint64,address,uint64,uint64,uint16[],bytes,bool))
func (_FrostBridge *FrostBridgeCaller) FrostCancellationAlternative(opts *bind.CallOpts, reservationId [32]byte, transactionHash [32]byte) (FrostCancellationHistoryRecord, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostCancellationAlternative", reservationId, transactionHash)

	if err != nil {
		return *new(FrostCancellationHistoryRecord), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostCancellationHistoryRecord)).(*FrostCancellationHistoryRecord)

	return out0, err

}

// FrostCancellationAlternative is a free data retrieval call binding the contract method 0xd8d8ee6a.
//
// Solidity: function frostCancellationAlternative(bytes32 reservationId, bytes32 transactionHash) view returns((bytes32,bytes32,uint64,address,uint64,uint64,uint16[],bytes,bool))
func (_FrostBridge *FrostBridgeSession) FrostCancellationAlternative(reservationId [32]byte, transactionHash [32]byte) (FrostCancellationHistoryRecord, error) {
	return _FrostBridge.Contract.FrostCancellationAlternative(&_FrostBridge.CallOpts, reservationId, transactionHash)
}

// FrostCancellationAlternative is a free data retrieval call binding the contract method 0xd8d8ee6a.
//
// Solidity: function frostCancellationAlternative(bytes32 reservationId, bytes32 transactionHash) view returns((bytes32,bytes32,uint64,address,uint64,uint64,uint16[],bytes,bool))
func (_FrostBridge *FrostBridgeCallerSession) FrostCancellationAlternative(reservationId [32]byte, transactionHash [32]byte) (FrostCancellationHistoryRecord, error) {
	return _FrostBridge.Contract.FrostCancellationAlternative(&_FrostBridge.CallOpts, reservationId, transactionHash)
}

// FrostCancellationCount is a free data retrieval call binding the contract method 0x97528013.
//
// Solidity: function frostCancellationCount(bytes32 reservationId) view returns(uint256)
func (_FrostBridge *FrostBridgeCaller) FrostCancellationCount(opts *bind.CallOpts, reservationId [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostCancellationCount", reservationId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FrostCancellationCount is a free data retrieval call binding the contract method 0x97528013.
//
// Solidity: function frostCancellationCount(bytes32 reservationId) view returns(uint256)
func (_FrostBridge *FrostBridgeSession) FrostCancellationCount(reservationId [32]byte) (*big.Int, error) {
	return _FrostBridge.Contract.FrostCancellationCount(&_FrostBridge.CallOpts, reservationId)
}

// FrostCancellationCount is a free data retrieval call binding the contract method 0x97528013.
//
// Solidity: function frostCancellationCount(bytes32 reservationId) view returns(uint256)
func (_FrostBridge *FrostBridgeCallerSession) FrostCancellationCount(reservationId [32]byte) (*big.Int, error) {
	return _FrostBridge.Contract.FrostCancellationCount(&_FrostBridge.CallOpts, reservationId)
}

// FrostCancellationHash is a free data retrieval call binding the contract method 0x625511b4.
//
// Solidity: function frostCancellationHash(bytes32 reservationId, uint256 index) view returns(bytes32)
func (_FrostBridge *FrostBridgeCaller) FrostCancellationHash(opts *bind.CallOpts, reservationId [32]byte, index *big.Int) ([32]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostCancellationHash", reservationId, index)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FrostCancellationHash is a free data retrieval call binding the contract method 0x625511b4.
//
// Solidity: function frostCancellationHash(bytes32 reservationId, uint256 index) view returns(bytes32)
func (_FrostBridge *FrostBridgeSession) FrostCancellationHash(reservationId [32]byte, index *big.Int) ([32]byte, error) {
	return _FrostBridge.Contract.FrostCancellationHash(&_FrostBridge.CallOpts, reservationId, index)
}

// FrostCancellationHash is a free data retrieval call binding the contract method 0x625511b4.
//
// Solidity: function frostCancellationHash(bytes32 reservationId, uint256 index) view returns(bytes32)
func (_FrostBridge *FrostBridgeCallerSession) FrostCancellationHash(reservationId [32]byte, index *big.Int) ([32]byte, error) {
	return _FrostBridge.Contract.FrostCancellationHash(&_FrostBridge.CallOpts, reservationId, index)
}

// FrostCancellationWinner is a free data retrieval call binding the contract method 0x2ce3e29d.
//
// Solidity: function frostCancellationWinner(bytes32 reservationId) view returns(bytes32)
func (_FrostBridge *FrostBridgeCaller) FrostCancellationWinner(opts *bind.CallOpts, reservationId [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostCancellationWinner", reservationId)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FrostCancellationWinner is a free data retrieval call binding the contract method 0x2ce3e29d.
//
// Solidity: function frostCancellationWinner(bytes32 reservationId) view returns(bytes32)
func (_FrostBridge *FrostBridgeSession) FrostCancellationWinner(reservationId [32]byte) ([32]byte, error) {
	return _FrostBridge.Contract.FrostCancellationWinner(&_FrostBridge.CallOpts, reservationId)
}

// FrostCancellationWinner is a free data retrieval call binding the contract method 0x2ce3e29d.
//
// Solidity: function frostCancellationWinner(bytes32 reservationId) view returns(bytes32)
func (_FrostBridge *FrostBridgeCallerSession) FrostCancellationWinner(reservationId [32]byte) ([32]byte, error) {
	return _FrostBridge.Contract.FrostCancellationWinner(&_FrostBridge.CallOpts, reservationId)
}

// FrostClosureObligations is a free data retrieval call binding the contract method 0x1456217e.
//
// Solidity: function frostClosureObligations(bytes32 walletId) view returns(uint256 open)
func (_FrostBridge *FrostBridgeCaller) FrostClosureObligations(opts *bind.CallOpts, walletId [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostClosureObligations", walletId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FrostClosureObligations is a free data retrieval call binding the contract method 0x1456217e.
//
// Solidity: function frostClosureObligations(bytes32 walletId) view returns(uint256 open)
func (_FrostBridge *FrostBridgeSession) FrostClosureObligations(walletId [32]byte) (*big.Int, error) {
	return _FrostBridge.Contract.FrostClosureObligations(&_FrostBridge.CallOpts, walletId)
}

// FrostClosureObligations is a free data retrieval call binding the contract method 0x1456217e.
//
// Solidity: function frostClosureObligations(bytes32 walletId) view returns(uint256 open)
func (_FrostBridge *FrostBridgeCallerSession) FrostClosureObligations(walletId [32]byte) (*big.Int, error) {
	return _FrostBridge.Contract.FrostClosureObligations(&_FrostBridge.CallOpts, walletId)
}

// FrostCustodyOutput is a free data retrieval call binding the contract method 0x451153d1.
//
// Solidity: function frostCustodyOutput(bytes32 outpoint) view returns((bytes32,uint64,bool))
func (_FrostBridge *FrostBridgeCaller) FrostCustodyOutput(opts *bind.CallOpts, outpoint [32]byte) (FrostCustodyOutput, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostCustodyOutput", outpoint)

	if err != nil {
		return *new(FrostCustodyOutput), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostCustodyOutput)).(*FrostCustodyOutput)

	return out0, err

}

// FrostCustodyOutput is a free data retrieval call binding the contract method 0x451153d1.
//
// Solidity: function frostCustodyOutput(bytes32 outpoint) view returns((bytes32,uint64,bool))
func (_FrostBridge *FrostBridgeSession) FrostCustodyOutput(outpoint [32]byte) (FrostCustodyOutput, error) {
	return _FrostBridge.Contract.FrostCustodyOutput(&_FrostBridge.CallOpts, outpoint)
}

// FrostCustodyOutput is a free data retrieval call binding the contract method 0x451153d1.
//
// Solidity: function frostCustodyOutput(bytes32 outpoint) view returns((bytes32,uint64,bool))
func (_FrostBridge *FrostBridgeCallerSession) FrostCustodyOutput(outpoint [32]byte) (FrostCustodyOutput, error) {
	return _FrostBridge.Contract.FrostCustodyOutput(&_FrostBridge.CallOpts, outpoint)
}

// FrostDeposit is a free data retrieval call binding the contract method 0x677bca9a.
//
// Solidity: function frostDeposit(bytes32 outpoint) view returns(bytes)
func (_FrostBridge *FrostBridgeCaller) FrostDeposit(opts *bind.CallOpts, outpoint [32]byte) ([]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostDeposit", outpoint)

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// FrostDeposit is a free data retrieval call binding the contract method 0x677bca9a.
//
// Solidity: function frostDeposit(bytes32 outpoint) view returns(bytes)
func (_FrostBridge *FrostBridgeSession) FrostDeposit(outpoint [32]byte) ([]byte, error) {
	return _FrostBridge.Contract.FrostDeposit(&_FrostBridge.CallOpts, outpoint)
}

// FrostDeposit is a free data retrieval call binding the contract method 0x677bca9a.
//
// Solidity: function frostDeposit(bytes32 outpoint) view returns(bytes)
func (_FrostBridge *FrostBridgeCallerSession) FrostDeposit(outpoint [32]byte) ([]byte, error) {
	return _FrostBridge.Contract.FrostDeposit(&_FrostBridge.CallOpts, outpoint)
}

// FrostDepositConstruction is a free data retrieval call binding the contract method 0x197f0d54.
//
// Solidity: function frostDepositConstruction((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms) view returns(((bytes32,bytes32,uint8),bytes32,bytes,bytes,bytes,bytes))
func (_FrostBridge *FrostBridgeCaller) FrostDepositConstruction(opts *bind.CallOpts, terms FrostDepositProfileTerms) (FrostDepositAdmissionConstruction, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostDepositConstruction", terms)

	if err != nil {
		return *new(FrostDepositAdmissionConstruction), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostDepositAdmissionConstruction)).(*FrostDepositAdmissionConstruction)

	return out0, err

}

// FrostDepositConstruction is a free data retrieval call binding the contract method 0x197f0d54.
//
// Solidity: function frostDepositConstruction((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms) view returns(((bytes32,bytes32,uint8),bytes32,bytes,bytes,bytes,bytes))
func (_FrostBridge *FrostBridgeSession) FrostDepositConstruction(terms FrostDepositProfileTerms) (FrostDepositAdmissionConstruction, error) {
	return _FrostBridge.Contract.FrostDepositConstruction(&_FrostBridge.CallOpts, terms)
}

// FrostDepositConstruction is a free data retrieval call binding the contract method 0x197f0d54.
//
// Solidity: function frostDepositConstruction((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms) view returns(((bytes32,bytes32,uint8),bytes32,bytes,bytes,bytes,bytes))
func (_FrostBridge *FrostBridgeCallerSession) FrostDepositConstruction(terms FrostDepositProfileTerms) (FrostDepositAdmissionConstruction, error) {
	return _FrostBridge.Contract.FrostDepositConstruction(&_FrostBridge.CallOpts, terms)
}

// FrostDepositOutput is a free data retrieval call binding the contract method 0xe321f38b.
//
// Solidity: function frostDepositOutput((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms) view returns((bytes32,bytes32,uint8))
func (_FrostBridge *FrostBridgeCaller) FrostDepositOutput(opts *bind.CallOpts, terms FrostDepositProfileTerms) (FrostDepositAdmissionOutput, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostDepositOutput", terms)

	if err != nil {
		return *new(FrostDepositAdmissionOutput), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostDepositAdmissionOutput)).(*FrostDepositAdmissionOutput)

	return out0, err

}

// FrostDepositOutput is a free data retrieval call binding the contract method 0xe321f38b.
//
// Solidity: function frostDepositOutput((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms) view returns((bytes32,bytes32,uint8))
func (_FrostBridge *FrostBridgeSession) FrostDepositOutput(terms FrostDepositProfileTerms) (FrostDepositAdmissionOutput, error) {
	return _FrostBridge.Contract.FrostDepositOutput(&_FrostBridge.CallOpts, terms)
}

// FrostDepositOutput is a free data retrieval call binding the contract method 0xe321f38b.
//
// Solidity: function frostDepositOutput((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms) view returns((bytes32,bytes32,uint8))
func (_FrostBridge *FrostBridgeCallerSession) FrostDepositOutput(terms FrostDepositProfileTerms) (FrostDepositAdmissionOutput, error) {
	return _FrostBridge.Contract.FrostDepositOutput(&_FrostBridge.CallOpts, terms)
}

// FrostDepositRefundConstruction is a free data retrieval call binding the contract method 0x44014caf.
//
// Solidity: function frostDepositRefundConstruction(bytes32 outpoint) view returns(((bytes32,bytes32,uint8),bytes32,bytes,bytes,bytes,bytes))
func (_FrostBridge *FrostBridgeCaller) FrostDepositRefundConstruction(opts *bind.CallOpts, outpoint [32]byte) (FrostDepositAdmissionConstruction, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostDepositRefundConstruction", outpoint)

	if err != nil {
		return *new(FrostDepositAdmissionConstruction), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostDepositAdmissionConstruction)).(*FrostDepositAdmissionConstruction)

	return out0, err

}

// FrostDepositRefundConstruction is a free data retrieval call binding the contract method 0x44014caf.
//
// Solidity: function frostDepositRefundConstruction(bytes32 outpoint) view returns(((bytes32,bytes32,uint8),bytes32,bytes,bytes,bytes,bytes))
func (_FrostBridge *FrostBridgeSession) FrostDepositRefundConstruction(outpoint [32]byte) (FrostDepositAdmissionConstruction, error) {
	return _FrostBridge.Contract.FrostDepositRefundConstruction(&_FrostBridge.CallOpts, outpoint)
}

// FrostDepositRefundConstruction is a free data retrieval call binding the contract method 0x44014caf.
//
// Solidity: function frostDepositRefundConstruction(bytes32 outpoint) view returns(((bytes32,bytes32,uint8),bytes32,bytes,bytes,bytes,bytes))
func (_FrostBridge *FrostBridgeCallerSession) FrostDepositRefundConstruction(outpoint [32]byte) (FrostDepositAdmissionConstruction, error) {
	return _FrostBridge.Contract.FrostDepositRefundConstruction(&_FrostBridge.CallOpts, outpoint)
}

// FrostDrainTargets is a free data retrieval call binding the contract method 0x37ab3fd8.
//
// Solidity: function frostDrainTargets(bytes20 source) view returns(bytes20[])
func (_FrostBridge *FrostBridgeCaller) FrostDrainTargets(opts *bind.CallOpts, source [20]byte) ([][20]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostDrainTargets", source)

	if err != nil {
		return *new([][20]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([][20]byte)).(*[][20]byte)

	return out0, err

}

// FrostDrainTargets is a free data retrieval call binding the contract method 0x37ab3fd8.
//
// Solidity: function frostDrainTargets(bytes20 source) view returns(bytes20[])
func (_FrostBridge *FrostBridgeSession) FrostDrainTargets(source [20]byte) ([][20]byte, error) {
	return _FrostBridge.Contract.FrostDrainTargets(&_FrostBridge.CallOpts, source)
}

// FrostDrainTargets is a free data retrieval call binding the contract method 0x37ab3fd8.
//
// Solidity: function frostDrainTargets(bytes20 source) view returns(bytes20[])
func (_FrostBridge *FrostBridgeCallerSession) FrostDrainTargets(source [20]byte) ([][20]byte, error) {
	return _FrostBridge.Contract.FrostDrainTargets(&_FrostBridge.CallOpts, source)
}

// FrostFraudChallenge is a free data retrieval call binding the contract method 0x6d3af04b.
//
// Solidity: function frostFraudChallenge(bytes32 walletId, bytes32 digest) view returns((bytes32,bytes32,address,uint64,bool,uint256))
func (_FrostBridge *FrostBridgeCaller) FrostFraudChallenge(opts *bind.CallOpts, walletId [32]byte, digest [32]byte) (FrostFraudStateChallenge, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostFraudChallenge", walletId, digest)

	if err != nil {
		return *new(FrostFraudStateChallenge), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostFraudStateChallenge)).(*FrostFraudStateChallenge)

	return out0, err

}

// FrostFraudChallenge is a free data retrieval call binding the contract method 0x6d3af04b.
//
// Solidity: function frostFraudChallenge(bytes32 walletId, bytes32 digest) view returns((bytes32,bytes32,address,uint64,bool,uint256))
func (_FrostBridge *FrostBridgeSession) FrostFraudChallenge(walletId [32]byte, digest [32]byte) (FrostFraudStateChallenge, error) {
	return _FrostBridge.Contract.FrostFraudChallenge(&_FrostBridge.CallOpts, walletId, digest)
}

// FrostFraudChallenge is a free data retrieval call binding the contract method 0x6d3af04b.
//
// Solidity: function frostFraudChallenge(bytes32 walletId, bytes32 digest) view returns((bytes32,bytes32,address,uint64,bool,uint256))
func (_FrostBridge *FrostBridgeCallerSession) FrostFraudChallenge(walletId [32]byte, digest [32]byte) (FrostFraudStateChallenge, error) {
	return _FrostBridge.Contract.FrostFraudChallenge(&_FrostBridge.CallOpts, walletId, digest)
}

// FrostHeartbeatDigest is a free data retrieval call binding the contract method 0xa33cf463.
//
// Solidity: function frostHeartbeatDigest(bytes32 walletId, uint64 nonce) view returns(bytes32)
func (_FrostBridge *FrostBridgeCaller) FrostHeartbeatDigest(opts *bind.CallOpts, walletId [32]byte, nonce uint64) ([32]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostHeartbeatDigest", walletId, nonce)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FrostHeartbeatDigest is a free data retrieval call binding the contract method 0xa33cf463.
//
// Solidity: function frostHeartbeatDigest(bytes32 walletId, uint64 nonce) view returns(bytes32)
func (_FrostBridge *FrostBridgeSession) FrostHeartbeatDigest(walletId [32]byte, nonce uint64) ([32]byte, error) {
	return _FrostBridge.Contract.FrostHeartbeatDigest(&_FrostBridge.CallOpts, walletId, nonce)
}

// FrostHeartbeatDigest is a free data retrieval call binding the contract method 0xa33cf463.
//
// Solidity: function frostHeartbeatDigest(bytes32 walletId, uint64 nonce) view returns(bytes32)
func (_FrostBridge *FrostBridgeCallerSession) FrostHeartbeatDigest(walletId [32]byte, nonce uint64) ([32]byte, error) {
	return _FrostBridge.Contract.FrostHeartbeatDigest(&_FrostBridge.CallOpts, walletId, nonce)
}

// FrostInactivityClaimDigest is a free data retrieval call binding the contract method 0x75be8bf5.
//
// Solidity: function frostInactivityClaimDigest(bytes32 walletId, uint64 nonce, uint16[] inactiveSeats, bool heartbeatFailed) view returns(bytes32)
func (_FrostBridge *FrostBridgeCaller) FrostInactivityClaimDigest(opts *bind.CallOpts, walletId [32]byte, nonce uint64, inactiveSeats []uint16, heartbeatFailed bool) ([32]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostInactivityClaimDigest", walletId, nonce, inactiveSeats, heartbeatFailed)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FrostInactivityClaimDigest is a free data retrieval call binding the contract method 0x75be8bf5.
//
// Solidity: function frostInactivityClaimDigest(bytes32 walletId, uint64 nonce, uint16[] inactiveSeats, bool heartbeatFailed) view returns(bytes32)
func (_FrostBridge *FrostBridgeSession) FrostInactivityClaimDigest(walletId [32]byte, nonce uint64, inactiveSeats []uint16, heartbeatFailed bool) ([32]byte, error) {
	return _FrostBridge.Contract.FrostInactivityClaimDigest(&_FrostBridge.CallOpts, walletId, nonce, inactiveSeats, heartbeatFailed)
}

// FrostInactivityClaimDigest is a free data retrieval call binding the contract method 0x75be8bf5.
//
// Solidity: function frostInactivityClaimDigest(bytes32 walletId, uint64 nonce, uint16[] inactiveSeats, bool heartbeatFailed) view returns(bytes32)
func (_FrostBridge *FrostBridgeCallerSession) FrostInactivityClaimDigest(walletId [32]byte, nonce uint64, inactiveSeats []uint16, heartbeatFailed bool) ([32]byte, error) {
	return _FrostBridge.Contract.FrostInactivityClaimDigest(&_FrostBridge.CallOpts, walletId, nonce, inactiveSeats, heartbeatFailed)
}

// FrostInactivityNonce is a free data retrieval call binding the contract method 0x768a6e09.
//
// Solidity: function frostInactivityNonce(bytes32 walletId) view returns(uint64)
func (_FrostBridge *FrostBridgeCaller) FrostInactivityNonce(opts *bind.CallOpts, walletId [32]byte) (uint64, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostInactivityNonce", walletId)

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// FrostInactivityNonce is a free data retrieval call binding the contract method 0x768a6e09.
//
// Solidity: function frostInactivityNonce(bytes32 walletId) view returns(uint64)
func (_FrostBridge *FrostBridgeSession) FrostInactivityNonce(walletId [32]byte) (uint64, error) {
	return _FrostBridge.Contract.FrostInactivityNonce(&_FrostBridge.CallOpts, walletId)
}

// FrostInactivityNonce is a free data retrieval call binding the contract method 0x768a6e09.
//
// Solidity: function frostInactivityNonce(bytes32 walletId) view returns(uint64)
func (_FrostBridge *FrostBridgeCallerSession) FrostInactivityNonce(walletId [32]byte) (uint64, error) {
	return _FrostBridge.Contract.FrostInactivityNonce(&_FrostBridge.CallOpts, walletId)
}

// FrostIncoming is a free data retrieval call binding the contract method 0x782751aa.
//
// Solidity: function frostIncoming(bytes32 outpoint) view returns((bytes32,bytes32,uint64,uint64,uint8,uint64))
func (_FrostBridge *FrostBridgeCaller) FrostIncoming(opts *bind.CallOpts, outpoint [32]byte) (FrostMovementStateIncoming, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostIncoming", outpoint)

	if err != nil {
		return *new(FrostMovementStateIncoming), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostMovementStateIncoming)).(*FrostMovementStateIncoming)

	return out0, err

}

// FrostIncoming is a free data retrieval call binding the contract method 0x782751aa.
//
// Solidity: function frostIncoming(bytes32 outpoint) view returns((bytes32,bytes32,uint64,uint64,uint8,uint64))
func (_FrostBridge *FrostBridgeSession) FrostIncoming(outpoint [32]byte) (FrostMovementStateIncoming, error) {
	return _FrostBridge.Contract.FrostIncoming(&_FrostBridge.CallOpts, outpoint)
}

// FrostIncoming is a free data retrieval call binding the contract method 0x782751aa.
//
// Solidity: function frostIncoming(bytes32 outpoint) view returns((bytes32,bytes32,uint64,uint64,uint8,uint64))
func (_FrostBridge *FrostBridgeCallerSession) FrostIncoming(outpoint [32]byte) (FrostMovementStateIncoming, error) {
	return _FrostBridge.Contract.FrostIncoming(&_FrostBridge.CallOpts, outpoint)
}

// FrostMovementCounters is a free data retrieval call binding the contract method 0x094646e3.
//
// Solidity: function frostMovementCounters(bytes32 walletId) view returns(uint64 pendingIncoming, uint64 inboundCommitments, uint64 eligibleTargets, bool drainEnabled)
func (_FrostBridge *FrostBridgeCaller) FrostMovementCounters(opts *bind.CallOpts, walletId [32]byte) (struct {
	PendingIncoming    uint64
	InboundCommitments uint64
	EligibleTargets    uint64
	DrainEnabled       bool
}, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostMovementCounters", walletId)

	outstruct := new(struct {
		PendingIncoming    uint64
		InboundCommitments uint64
		EligibleTargets    uint64
		DrainEnabled       bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.PendingIncoming = *abi.ConvertType(out[0], new(uint64)).(*uint64)
	outstruct.InboundCommitments = *abi.ConvertType(out[1], new(uint64)).(*uint64)
	outstruct.EligibleTargets = *abi.ConvertType(out[2], new(uint64)).(*uint64)
	outstruct.DrainEnabled = *abi.ConvertType(out[3], new(bool)).(*bool)

	return *outstruct, err

}

// FrostMovementCounters is a free data retrieval call binding the contract method 0x094646e3.
//
// Solidity: function frostMovementCounters(bytes32 walletId) view returns(uint64 pendingIncoming, uint64 inboundCommitments, uint64 eligibleTargets, bool drainEnabled)
func (_FrostBridge *FrostBridgeSession) FrostMovementCounters(walletId [32]byte) (struct {
	PendingIncoming    uint64
	InboundCommitments uint64
	EligibleTargets    uint64
	DrainEnabled       bool
}, error) {
	return _FrostBridge.Contract.FrostMovementCounters(&_FrostBridge.CallOpts, walletId)
}

// FrostMovementCounters is a free data retrieval call binding the contract method 0x094646e3.
//
// Solidity: function frostMovementCounters(bytes32 walletId) view returns(uint64 pendingIncoming, uint64 inboundCommitments, uint64 eligibleTargets, bool drainEnabled)
func (_FrostBridge *FrostBridgeCallerSession) FrostMovementCounters(walletId [32]byte) (struct {
	PendingIncoming    uint64
	InboundCommitments uint64
	EligibleTargets    uint64
	DrainEnabled       bool
}, error) {
	return _FrostBridge.Contract.FrostMovementCounters(&_FrostBridge.CallOpts, walletId)
}

// FrostMovingFundsCommitment is a free data retrieval call binding the contract method 0xeb5fdfb1.
//
// Solidity: function frostMovingFundsCommitment(bytes32 walletId, uint32 period) view returns((bytes32,uint64,uint64,bool,bool,uint64,bytes20[],bytes32[]))
func (_FrostBridge *FrostBridgeCaller) FrostMovingFundsCommitment(opts *bind.CallOpts, walletId [32]byte, period uint32) (FrostMovementStateCommitment, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostMovingFundsCommitment", walletId, period)

	if err != nil {
		return *new(FrostMovementStateCommitment), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostMovementStateCommitment)).(*FrostMovementStateCommitment)

	return out0, err

}

// FrostMovingFundsCommitment is a free data retrieval call binding the contract method 0xeb5fdfb1.
//
// Solidity: function frostMovingFundsCommitment(bytes32 walletId, uint32 period) view returns((bytes32,uint64,uint64,bool,bool,uint64,bytes20[],bytes32[]))
func (_FrostBridge *FrostBridgeSession) FrostMovingFundsCommitment(walletId [32]byte, period uint32) (FrostMovementStateCommitment, error) {
	return _FrostBridge.Contract.FrostMovingFundsCommitment(&_FrostBridge.CallOpts, walletId, period)
}

// FrostMovingFundsCommitment is a free data retrieval call binding the contract method 0xeb5fdfb1.
//
// Solidity: function frostMovingFundsCommitment(bytes32 walletId, uint32 period) view returns((bytes32,uint64,uint64,bool,bool,uint64,bytes20[],bytes32[]))
func (_FrostBridge *FrostBridgeCallerSession) FrostMovingFundsCommitment(walletId [32]byte, period uint32) (FrostMovementStateCommitment, error) {
	return _FrostBridge.Contract.FrostMovingFundsCommitment(&_FrostBridge.CallOpts, walletId, period)
}

// FrostOpenChallenges is a free data retrieval call binding the contract method 0x81149a35.
//
// Solidity: function frostOpenChallenges(bytes32 walletId) view returns(uint64)
func (_FrostBridge *FrostBridgeCaller) FrostOpenChallenges(opts *bind.CallOpts, walletId [32]byte) (uint64, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostOpenChallenges", walletId)

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// FrostOpenChallenges is a free data retrieval call binding the contract method 0x81149a35.
//
// Solidity: function frostOpenChallenges(bytes32 walletId) view returns(uint64)
func (_FrostBridge *FrostBridgeSession) FrostOpenChallenges(walletId [32]byte) (uint64, error) {
	return _FrostBridge.Contract.FrostOpenChallenges(&_FrostBridge.CallOpts, walletId)
}

// FrostOpenChallenges is a free data retrieval call binding the contract method 0x81149a35.
//
// Solidity: function frostOpenChallenges(bytes32 walletId) view returns(uint64)
func (_FrostBridge *FrostBridgeCallerSession) FrostOpenChallenges(walletId [32]byte) (uint64, error) {
	return _FrostBridge.Contract.FrostOpenChallenges(&_FrostBridge.CallOpts, walletId)
}

// FrostProvenTransaction is a free data retrieval call binding the contract method 0xc4ace6e5.
//
// Solidity: function frostProvenTransaction(bytes32 transactionHash) view returns(bytes32 walletId)
func (_FrostBridge *FrostBridgeCaller) FrostProvenTransaction(opts *bind.CallOpts, transactionHash [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostProvenTransaction", transactionHash)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FrostProvenTransaction is a free data retrieval call binding the contract method 0xc4ace6e5.
//
// Solidity: function frostProvenTransaction(bytes32 transactionHash) view returns(bytes32 walletId)
func (_FrostBridge *FrostBridgeSession) FrostProvenTransaction(transactionHash [32]byte) ([32]byte, error) {
	return _FrostBridge.Contract.FrostProvenTransaction(&_FrostBridge.CallOpts, transactionHash)
}

// FrostProvenTransaction is a free data retrieval call binding the contract method 0xc4ace6e5.
//
// Solidity: function frostProvenTransaction(bytes32 transactionHash) view returns(bytes32 walletId)
func (_FrostBridge *FrostBridgeCallerSession) FrostProvenTransaction(transactionHash [32]byte) ([32]byte, error) {
	return _FrostBridge.Contract.FrostProvenTransaction(&_FrostBridge.CallOpts, transactionHash)
}

// FrostRedemptionConfiguration is a free data retrieval call binding the contract method 0x1afcf2e3.
//
// Solidity: function frostRedemptionConfiguration() view returns((bytes32,uint64,uint64,bool,address,bytes32))
func (_FrostBridge *FrostBridgeCaller) FrostRedemptionConfiguration(opts *bind.CallOpts) (FrostRedemptionStateConfiguration, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostRedemptionConfiguration")

	if err != nil {
		return *new(FrostRedemptionStateConfiguration), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostRedemptionStateConfiguration)).(*FrostRedemptionStateConfiguration)

	return out0, err

}

// FrostRedemptionConfiguration is a free data retrieval call binding the contract method 0x1afcf2e3.
//
// Solidity: function frostRedemptionConfiguration() view returns((bytes32,uint64,uint64,bool,address,bytes32))
func (_FrostBridge *FrostBridgeSession) FrostRedemptionConfiguration() (FrostRedemptionStateConfiguration, error) {
	return _FrostBridge.Contract.FrostRedemptionConfiguration(&_FrostBridge.CallOpts)
}

// FrostRedemptionConfiguration is a free data retrieval call binding the contract method 0x1afcf2e3.
//
// Solidity: function frostRedemptionConfiguration() view returns((bytes32,uint64,uint64,bool,address,bytes32))
func (_FrostBridge *FrostBridgeCallerSession) FrostRedemptionConfiguration() (FrostRedemptionStateConfiguration, error) {
	return _FrostBridge.Contract.FrostRedemptionConfiguration(&_FrostBridge.CallOpts)
}

// FrostRedemptionEscrow is a free data retrieval call binding the contract method 0x9ddcea5a.
//
// Solidity: function frostRedemptionEscrow(bytes32 walletId) view returns(uint64 gross, uint64 netLiability)
func (_FrostBridge *FrostBridgeCaller) FrostRedemptionEscrow(opts *bind.CallOpts, walletId [32]byte) (struct {
	Gross        uint64
	NetLiability uint64
}, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostRedemptionEscrow", walletId)

	outstruct := new(struct {
		Gross        uint64
		NetLiability uint64
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Gross = *abi.ConvertType(out[0], new(uint64)).(*uint64)
	outstruct.NetLiability = *abi.ConvertType(out[1], new(uint64)).(*uint64)

	return *outstruct, err

}

// FrostRedemptionEscrow is a free data retrieval call binding the contract method 0x9ddcea5a.
//
// Solidity: function frostRedemptionEscrow(bytes32 walletId) view returns(uint64 gross, uint64 netLiability)
func (_FrostBridge *FrostBridgeSession) FrostRedemptionEscrow(walletId [32]byte) (struct {
	Gross        uint64
	NetLiability uint64
}, error) {
	return _FrostBridge.Contract.FrostRedemptionEscrow(&_FrostBridge.CallOpts, walletId)
}

// FrostRedemptionEscrow is a free data retrieval call binding the contract method 0x9ddcea5a.
//
// Solidity: function frostRedemptionEscrow(bytes32 walletId) view returns(uint64 gross, uint64 netLiability)
func (_FrostBridge *FrostBridgeCallerSession) FrostRedemptionEscrow(walletId [32]byte) (struct {
	Gross        uint64
	NetLiability uint64
}, error) {
	return _FrostBridge.Contract.FrostRedemptionEscrow(&_FrostBridge.CallOpts, walletId)
}

// FrostRedemptionPolicy is a free data retrieval call binding the contract method 0x92f24f58.
//
// Solidity: function frostRedemptionPolicy(bytes32 requestId) view returns((address,bytes32,uint64))
func (_FrostBridge *FrostBridgeCaller) FrostRedemptionPolicy(opts *bind.CallOpts, requestId [32]byte) (FrostRedemptionStateRequestPolicy, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostRedemptionPolicy", requestId)

	if err != nil {
		return *new(FrostRedemptionStateRequestPolicy), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostRedemptionStateRequestPolicy)).(*FrostRedemptionStateRequestPolicy)

	return out0, err

}

// FrostRedemptionPolicy is a free data retrieval call binding the contract method 0x92f24f58.
//
// Solidity: function frostRedemptionPolicy(bytes32 requestId) view returns((address,bytes32,uint64))
func (_FrostBridge *FrostBridgeSession) FrostRedemptionPolicy(requestId [32]byte) (FrostRedemptionStateRequestPolicy, error) {
	return _FrostBridge.Contract.FrostRedemptionPolicy(&_FrostBridge.CallOpts, requestId)
}

// FrostRedemptionPolicy is a free data retrieval call binding the contract method 0x92f24f58.
//
// Solidity: function frostRedemptionPolicy(bytes32 requestId) view returns((address,bytes32,uint64))
func (_FrostBridge *FrostBridgeCallerSession) FrostRedemptionPolicy(requestId [32]byte) (FrostRedemptionStateRequestPolicy, error) {
	return _FrostBridge.Contract.FrostRedemptionPolicy(&_FrostBridge.CallOpts, requestId)
}

// FrostRedemptionRequest is a free data retrieval call binding the contract method 0x8a12fd29.
//
// Solidity: function frostRedemptionRequest(bytes32 requestId) view returns((bytes32,bytes32,bytes32,address,uint64,uint64,uint64,uint64,uint64,uint64,uint8,bytes))
func (_FrostBridge *FrostBridgeCaller) FrostRedemptionRequest(opts *bind.CallOpts, requestId [32]byte) (FrostRedemptionLedgerRequest, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostRedemptionRequest", requestId)

	if err != nil {
		return *new(FrostRedemptionLedgerRequest), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostRedemptionLedgerRequest)).(*FrostRedemptionLedgerRequest)

	return out0, err

}

// FrostRedemptionRequest is a free data retrieval call binding the contract method 0x8a12fd29.
//
// Solidity: function frostRedemptionRequest(bytes32 requestId) view returns((bytes32,bytes32,bytes32,address,uint64,uint64,uint64,uint64,uint64,uint64,uint8,bytes))
func (_FrostBridge *FrostBridgeSession) FrostRedemptionRequest(requestId [32]byte) (FrostRedemptionLedgerRequest, error) {
	return _FrostBridge.Contract.FrostRedemptionRequest(&_FrostBridge.CallOpts, requestId)
}

// FrostRedemptionRequest is a free data retrieval call binding the contract method 0x8a12fd29.
//
// Solidity: function frostRedemptionRequest(bytes32 requestId) view returns((bytes32,bytes32,bytes32,address,uint64,uint64,uint64,uint64,uint64,uint64,uint8,bytes))
func (_FrostBridge *FrostBridgeCallerSession) FrostRedemptionRequest(requestId [32]byte) (FrostRedemptionLedgerRequest, error) {
	return _FrostBridge.Contract.FrostRedemptionRequest(&_FrostBridge.CallOpts, requestId)
}

// FrostRegistry is a free data retrieval call binding the contract method 0xddba6dc4.
//
// Solidity: function frostRegistry() view returns(address)
func (_FrostBridge *FrostBridgeCaller) FrostRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// FrostRegistry is a free data retrieval call binding the contract method 0xddba6dc4.
//
// Solidity: function frostRegistry() view returns(address)
func (_FrostBridge *FrostBridgeSession) FrostRegistry() (common.Address, error) {
	return _FrostBridge.Contract.FrostRegistry(&_FrostBridge.CallOpts)
}

// FrostRegistry is a free data retrieval call binding the contract method 0xddba6dc4.
//
// Solidity: function frostRegistry() view returns(address)
func (_FrostBridge *FrostBridgeCallerSession) FrostRegistry() (common.Address, error) {
	return _FrostBridge.Contract.FrostRegistry(&_FrostBridge.CallOpts)
}

// FrostReservation is a free data retrieval call binding the contract method 0xd55db175.
//
// Solidity: function frostReservation(bytes32 reservationId) view returns(((bytes32,bytes32,uint64,uint64,bytes32,bytes32,bytes32,uint32,uint64,uint64,uint64,uint64),uint64,bytes32,bytes32,uint8,bytes32[],uint64[],(bytes32,uint32,uint64)[],uint16[],bytes,bytes32,bytes32,uint64,address,uint64,uint16[],bytes,uint64,uint64,bytes32,uint64,uint64,uint16[],bytes32,bytes32))
func (_FrostBridge *FrostBridgeCaller) FrostReservation(opts *bind.CallOpts, reservationId [32]byte) (FrostRedemptionLedgerIntent, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostReservation", reservationId)

	if err != nil {
		return *new(FrostRedemptionLedgerIntent), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostRedemptionLedgerIntent)).(*FrostRedemptionLedgerIntent)

	return out0, err

}

// FrostReservation is a free data retrieval call binding the contract method 0xd55db175.
//
// Solidity: function frostReservation(bytes32 reservationId) view returns(((bytes32,bytes32,uint64,uint64,bytes32,bytes32,bytes32,uint32,uint64,uint64,uint64,uint64),uint64,bytes32,bytes32,uint8,bytes32[],uint64[],(bytes32,uint32,uint64)[],uint16[],bytes,bytes32,bytes32,uint64,address,uint64,uint16[],bytes,uint64,uint64,bytes32,uint64,uint64,uint16[],bytes32,bytes32))
func (_FrostBridge *FrostBridgeSession) FrostReservation(reservationId [32]byte) (FrostRedemptionLedgerIntent, error) {
	return _FrostBridge.Contract.FrostReservation(&_FrostBridge.CallOpts, reservationId)
}

// FrostReservation is a free data retrieval call binding the contract method 0xd55db175.
//
// Solidity: function frostReservation(bytes32 reservationId) view returns(((bytes32,bytes32,uint64,uint64,bytes32,bytes32,bytes32,uint32,uint64,uint64,uint64,uint64),uint64,bytes32,bytes32,uint8,bytes32[],uint64[],(bytes32,uint32,uint64)[],uint16[],bytes,bytes32,bytes32,uint64,address,uint64,uint16[],bytes,uint64,uint64,bytes32,uint64,uint64,uint16[],bytes32,bytes32))
func (_FrostBridge *FrostBridgeCallerSession) FrostReservation(reservationId [32]byte) (FrostRedemptionLedgerIntent, error) {
	return _FrostBridge.Contract.FrostReservation(&_FrostBridge.CallOpts, reservationId)
}

// FrostWalletCustody is a free data retrieval call binding the contract method 0xc607d582.
//
// Solidity: function frostWalletCustody(bytes32 walletId) view returns((uint64,uint64,uint64,bytes32,bytes32,uint64))
func (_FrostBridge *FrostBridgeCaller) FrostWalletCustody(opts *bind.CallOpts, walletId [32]byte) (FrostCustodyWallet, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostWalletCustody", walletId)

	if err != nil {
		return *new(FrostCustodyWallet), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostCustodyWallet)).(*FrostCustodyWallet)

	return out0, err

}

// FrostWalletCustody is a free data retrieval call binding the contract method 0xc607d582.
//
// Solidity: function frostWalletCustody(bytes32 walletId) view returns((uint64,uint64,uint64,bytes32,bytes32,uint64))
func (_FrostBridge *FrostBridgeSession) FrostWalletCustody(walletId [32]byte) (FrostCustodyWallet, error) {
	return _FrostBridge.Contract.FrostWalletCustody(&_FrostBridge.CallOpts, walletId)
}

// FrostWalletCustody is a free data retrieval call binding the contract method 0xc607d582.
//
// Solidity: function frostWalletCustody(bytes32 walletId) view returns((uint64,uint64,uint64,bytes32,bytes32,uint64))
func (_FrostBridge *FrostBridgeCallerSession) FrostWalletCustody(walletId [32]byte) (FrostCustodyWallet, error) {
	return _FrostBridge.Contract.FrostWalletCustody(&_FrostBridge.CallOpts, walletId)
}

// FrostWalletLifecycle is a free data retrieval call binding the contract method 0x310cd29a.
//
// Solidity: function frostWalletLifecycle(bytes32 walletId) view returns((uint32,uint64,uint64,bool,uint64,uint64,uint64))
func (_FrostBridge *FrostBridgeCaller) FrostWalletLifecycle(opts *bind.CallOpts, walletId [32]byte) (FrostMovementStateLifecycle, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostWalletLifecycle", walletId)

	if err != nil {
		return *new(FrostMovementStateLifecycle), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostMovementStateLifecycle)).(*FrostMovementStateLifecycle)

	return out0, err

}

// FrostWalletLifecycle is a free data retrieval call binding the contract method 0x310cd29a.
//
// Solidity: function frostWalletLifecycle(bytes32 walletId) view returns((uint32,uint64,uint64,bool,uint64,uint64,uint64))
func (_FrostBridge *FrostBridgeSession) FrostWalletLifecycle(walletId [32]byte) (FrostMovementStateLifecycle, error) {
	return _FrostBridge.Contract.FrostWalletLifecycle(&_FrostBridge.CallOpts, walletId)
}

// FrostWalletLifecycle is a free data retrieval call binding the contract method 0x310cd29a.
//
// Solidity: function frostWalletLifecycle(bytes32 walletId) view returns((uint32,uint64,uint64,bool,uint64,uint64,uint64))
func (_FrostBridge *FrostBridgeCallerSession) FrostWalletLifecycle(walletId [32]byte) (FrostMovementStateLifecycle, error) {
	return _FrostBridge.Contract.FrostWalletLifecycle(&_FrostBridge.CallOpts, walletId)
}

// FrostWalletQuarantine is a free data retrieval call binding the contract method 0x808c2a5d.
//
// Solidity: function frostWalletQuarantine(bytes32 walletId, uint64 stateVersion) view returns((bytes32,bytes32,bytes32,address,uint64,uint64,uint64))
func (_FrostBridge *FrostBridgeCaller) FrostWalletQuarantine(opts *bind.CallOpts, walletId [32]byte, stateVersion uint64) (FrostRedemptionStateQuarantineRecord, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "frostWalletQuarantine", walletId, stateVersion)

	if err != nil {
		return *new(FrostRedemptionStateQuarantineRecord), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostRedemptionStateQuarantineRecord)).(*FrostRedemptionStateQuarantineRecord)

	return out0, err

}

// FrostWalletQuarantine is a free data retrieval call binding the contract method 0x808c2a5d.
//
// Solidity: function frostWalletQuarantine(bytes32 walletId, uint64 stateVersion) view returns((bytes32,bytes32,bytes32,address,uint64,uint64,uint64))
func (_FrostBridge *FrostBridgeSession) FrostWalletQuarantine(walletId [32]byte, stateVersion uint64) (FrostRedemptionStateQuarantineRecord, error) {
	return _FrostBridge.Contract.FrostWalletQuarantine(&_FrostBridge.CallOpts, walletId, stateVersion)
}

// FrostWalletQuarantine is a free data retrieval call binding the contract method 0x808c2a5d.
//
// Solidity: function frostWalletQuarantine(bytes32 walletId, uint64 stateVersion) view returns((bytes32,bytes32,bytes32,address,uint64,uint64,uint64))
func (_FrostBridge *FrostBridgeCallerSession) FrostWalletQuarantine(walletId [32]byte, stateVersion uint64) (FrostRedemptionStateQuarantineRecord, error) {
	return _FrostBridge.Contract.FrostWalletQuarantine(&_FrostBridge.CallOpts, walletId, stateVersion)
}

// IsFrostFundingTarget is a free data retrieval call binding the contract method 0x1abb212a.
//
// Solidity: function isFrostFundingTarget(bytes20 label) view returns(bool)
func (_FrostBridge *FrostBridgeCaller) IsFrostFundingTarget(opts *bind.CallOpts, label [20]byte) (bool, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "isFrostFundingTarget", label)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsFrostFundingTarget is a free data retrieval call binding the contract method 0x1abb212a.
//
// Solidity: function isFrostFundingTarget(bytes20 label) view returns(bool)
func (_FrostBridge *FrostBridgeSession) IsFrostFundingTarget(label [20]byte) (bool, error) {
	return _FrostBridge.Contract.IsFrostFundingTarget(&_FrostBridge.CallOpts, label)
}

// IsFrostFundingTarget is a free data retrieval call binding the contract method 0x1abb212a.
//
// Solidity: function isFrostFundingTarget(bytes20 label) view returns(bool)
func (_FrostBridge *FrostBridgeCallerSession) IsFrostFundingTarget(label [20]byte) (bool, error) {
	return _FrostBridge.Contract.IsFrostFundingTarget(&_FrostBridge.CallOpts, label)
}

// ResolveFrostOutputScript is a free data retrieval call binding the contract method 0x919a2727.
//
// Solidity: function resolveFrostOutputScript(bytes output) view returns(bytes20)
func (_FrostBridge *FrostBridgeCaller) ResolveFrostOutputScript(opts *bind.CallOpts, output []byte) ([20]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "resolveFrostOutputScript", output)

	if err != nil {
		return *new([20]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([20]byte)).(*[20]byte)

	return out0, err

}

// ResolveFrostOutputScript is a free data retrieval call binding the contract method 0x919a2727.
//
// Solidity: function resolveFrostOutputScript(bytes output) view returns(bytes20)
func (_FrostBridge *FrostBridgeSession) ResolveFrostOutputScript(output []byte) ([20]byte, error) {
	return _FrostBridge.Contract.ResolveFrostOutputScript(&_FrostBridge.CallOpts, output)
}

// ResolveFrostOutputScript is a free data retrieval call binding the contract method 0x919a2727.
//
// Solidity: function resolveFrostOutputScript(bytes output) view returns(bytes20)
func (_FrostBridge *FrostBridgeCallerSession) ResolveFrostOutputScript(output []byte) ([20]byte, error) {
	return _FrostBridge.Contract.ResolveFrostOutputScript(&_FrostBridge.CallOpts, output)
}

// ResolveWallet is a free data retrieval call binding the contract method 0xd6dfbd4c.
//
// Solidity: function resolveWallet(address registry, bytes32 id) view returns(bytes20)
func (_FrostBridge *FrostBridgeCaller) ResolveWallet(opts *bind.CallOpts, registry common.Address, id [32]byte) ([20]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "resolveWallet", registry, id)

	if err != nil {
		return *new([20]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([20]byte)).(*[20]byte)

	return out0, err

}

// ResolveWallet is a free data retrieval call binding the contract method 0xd6dfbd4c.
//
// Solidity: function resolveWallet(address registry, bytes32 id) view returns(bytes20)
func (_FrostBridge *FrostBridgeSession) ResolveWallet(registry common.Address, id [32]byte) ([20]byte, error) {
	return _FrostBridge.Contract.ResolveWallet(&_FrostBridge.CallOpts, registry, id)
}

// ResolveWallet is a free data retrieval call binding the contract method 0xd6dfbd4c.
//
// Solidity: function resolveWallet(address registry, bytes32 id) view returns(bytes20)
func (_FrostBridge *FrostBridgeCallerSession) ResolveWallet(registry common.Address, id [32]byte) ([20]byte, error) {
	return _FrostBridge.Contract.ResolveWallet(&_FrostBridge.CallOpts, registry, id)
}

// ResolveWalletOutput is a free data retrieval call binding the contract method 0x30a77d9b.
//
// Solidity: function resolveWalletOutput(bytes32 q) view returns(bytes20)
func (_FrostBridge *FrostBridgeCaller) ResolveWalletOutput(opts *bind.CallOpts, q [32]byte) ([20]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "resolveWalletOutput", q)

	if err != nil {
		return *new([20]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([20]byte)).(*[20]byte)

	return out0, err

}

// ResolveWalletOutput is a free data retrieval call binding the contract method 0x30a77d9b.
//
// Solidity: function resolveWalletOutput(bytes32 q) view returns(bytes20)
func (_FrostBridge *FrostBridgeSession) ResolveWalletOutput(q [32]byte) ([20]byte, error) {
	return _FrostBridge.Contract.ResolveWalletOutput(&_FrostBridge.CallOpts, q)
}

// ResolveWalletOutput is a free data retrieval call binding the contract method 0x30a77d9b.
//
// Solidity: function resolveWalletOutput(bytes32 q) view returns(bytes20)
func (_FrostBridge *FrostBridgeCallerSession) ResolveWalletOutput(q [32]byte) ([20]byte, error) {
	return _FrostBridge.Contract.ResolveWalletOutput(&_FrostBridge.CallOpts, q)
}

// WalletDescriptor is a free data retrieval call binding the contract method 0x5326d002.
//
// Solidity: function walletDescriptor(bytes20 label) view returns(bytes)
func (_FrostBridge *FrostBridgeCaller) WalletDescriptor(opts *bind.CallOpts, label [20]byte) ([]byte, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "walletDescriptor", label)

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// WalletDescriptor is a free data retrieval call binding the contract method 0x5326d002.
//
// Solidity: function walletDescriptor(bytes20 label) view returns(bytes)
func (_FrostBridge *FrostBridgeSession) WalletDescriptor(label [20]byte) ([]byte, error) {
	return _FrostBridge.Contract.WalletDescriptor(&_FrostBridge.CallOpts, label)
}

// WalletDescriptor is a free data retrieval call binding the contract method 0x5326d002.
//
// Solidity: function walletDescriptor(bytes20 label) view returns(bytes)
func (_FrostBridge *FrostBridgeCallerSession) WalletDescriptor(label [20]byte) ([]byte, error) {
	return _FrostBridge.Contract.WalletDescriptor(&_FrostBridge.CallOpts, label)
}

// WalletScheme is a free data retrieval call binding the contract method 0xb9d91db7.
//
// Solidity: function walletScheme(bytes20 label) view returns(uint8)
func (_FrostBridge *FrostBridgeCaller) WalletScheme(opts *bind.CallOpts, label [20]byte) (uint8, error) {
	var out []interface{}
	err := _FrostBridge.contract.Call(opts, &out, "walletScheme", label)

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// WalletScheme is a free data retrieval call binding the contract method 0xb9d91db7.
//
// Solidity: function walletScheme(bytes20 label) view returns(uint8)
func (_FrostBridge *FrostBridgeSession) WalletScheme(label [20]byte) (uint8, error) {
	return _FrostBridge.Contract.WalletScheme(&_FrostBridge.CallOpts, label)
}

// WalletScheme is a free data retrieval call binding the contract method 0xb9d91db7.
//
// Solidity: function walletScheme(bytes20 label) view returns(uint8)
func (_FrostBridge *FrostBridgeCallerSession) WalletScheme(label [20]byte) (uint8, error) {
	return _FrostBridge.Contract.WalletScheme(&_FrostBridge.CallOpts, label)
}

// SnowfallWalletCreated is a paid mutator transaction binding the contract method 0x53a8a48d.
//
// Solidity: function __snowfallWalletCreated(bytes descriptor, uint64 deadline) returns()
func (_FrostBridge *FrostBridgeTransactor) SnowfallWalletCreated(opts *bind.TransactOpts, descriptor []byte, deadline uint64) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "__snowfallWalletCreated", descriptor, deadline)
}

// SnowfallWalletCreated is a paid mutator transaction binding the contract method 0x53a8a48d.
//
// Solidity: function __snowfallWalletCreated(bytes descriptor, uint64 deadline) returns()
func (_FrostBridge *FrostBridgeSession) SnowfallWalletCreated(descriptor []byte, deadline uint64) (*types.Transaction, error) {
	return _FrostBridge.Contract.SnowfallWalletCreated(&_FrostBridge.TransactOpts, descriptor, deadline)
}

// SnowfallWalletCreated is a paid mutator transaction binding the contract method 0x53a8a48d.
//
// Solidity: function __snowfallWalletCreated(bytes descriptor, uint64 deadline) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SnowfallWalletCreated(descriptor []byte, deadline uint64) (*types.Transaction, error) {
	return _FrostBridge.Contract.SnowfallWalletCreated(&_FrostBridge.TransactOpts, descriptor, deadline)
}

// SnowfallWalletExpired is a paid mutator transaction binding the contract method 0x6d8767ec.
//
// Solidity: function __snowfallWalletExpired(bytes32 id) returns()
func (_FrostBridge *FrostBridgeTransactor) SnowfallWalletExpired(opts *bind.TransactOpts, id [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "__snowfallWalletExpired", id)
}

// SnowfallWalletExpired is a paid mutator transaction binding the contract method 0x6d8767ec.
//
// Solidity: function __snowfallWalletExpired(bytes32 id) returns()
func (_FrostBridge *FrostBridgeSession) SnowfallWalletExpired(id [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.SnowfallWalletExpired(&_FrostBridge.TransactOpts, id)
}

// SnowfallWalletExpired is a paid mutator transaction binding the contract method 0x6d8767ec.
//
// Solidity: function __snowfallWalletExpired(bytes32 id) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SnowfallWalletExpired(id [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.SnowfallWalletExpired(&_FrostBridge.TransactOpts, id)
}

// SnowfallWalletReady is a paid mutator transaction binding the contract method 0xe0ca5b86.
//
// Solidity: function __snowfallWalletReady(bytes32 id, bytes32 descriptorHash, uint32 generation, uint32 capabilityVersion) returns()
func (_FrostBridge *FrostBridgeTransactor) SnowfallWalletReady(opts *bind.TransactOpts, id [32]byte, descriptorHash [32]byte, generation uint32, capabilityVersion uint32) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "__snowfallWalletReady", id, descriptorHash, generation, capabilityVersion)
}

// SnowfallWalletReady is a paid mutator transaction binding the contract method 0xe0ca5b86.
//
// Solidity: function __snowfallWalletReady(bytes32 id, bytes32 descriptorHash, uint32 generation, uint32 capabilityVersion) returns()
func (_FrostBridge *FrostBridgeSession) SnowfallWalletReady(id [32]byte, descriptorHash [32]byte, generation uint32, capabilityVersion uint32) (*types.Transaction, error) {
	return _FrostBridge.Contract.SnowfallWalletReady(&_FrostBridge.TransactOpts, id, descriptorHash, generation, capabilityVersion)
}

// SnowfallWalletReady is a paid mutator transaction binding the contract method 0xe0ca5b86.
//
// Solidity: function __snowfallWalletReady(bytes32 id, bytes32 descriptorHash, uint32 generation, uint32 capabilityVersion) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SnowfallWalletReady(id [32]byte, descriptorHash [32]byte, generation uint32, capabilityVersion uint32) (*types.Transaction, error) {
	return _FrostBridge.Contract.SnowfallWalletReady(&_FrostBridge.TransactOpts, id, descriptorHash, generation, capabilityVersion)
}

// AuthorizeFrostCancellation is a paid mutator transaction binding the contract method 0xf70bba80.
//
// Solidity: function authorizeFrostCancellation((bytes32,bytes32,bytes32,uint64,uint64) authorization, (bytes4,bytes,bytes,bytes4) transaction, uint16[] seats, bytes signatures) returns()
func (_FrostBridge *FrostBridgeTransactor) AuthorizeFrostCancellation(opts *bind.TransactOpts, authorization FrostCancellationCertificateAuthorization, transaction BitcoinTxInfo, seats []uint16, signatures []byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "authorizeFrostCancellation", authorization, transaction, seats, signatures)
}

// AuthorizeFrostCancellation is a paid mutator transaction binding the contract method 0xf70bba80.
//
// Solidity: function authorizeFrostCancellation((bytes32,bytes32,bytes32,uint64,uint64) authorization, (bytes4,bytes,bytes,bytes4) transaction, uint16[] seats, bytes signatures) returns()
func (_FrostBridge *FrostBridgeSession) AuthorizeFrostCancellation(authorization FrostCancellationCertificateAuthorization, transaction BitcoinTxInfo, seats []uint16, signatures []byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.AuthorizeFrostCancellation(&_FrostBridge.TransactOpts, authorization, transaction, seats, signatures)
}

// AuthorizeFrostCancellation is a paid mutator transaction binding the contract method 0xf70bba80.
//
// Solidity: function authorizeFrostCancellation((bytes32,bytes32,bytes32,uint64,uint64) authorization, (bytes4,bytes,bytes,bytes4) transaction, uint16[] seats, bytes signatures) returns()
func (_FrostBridge *FrostBridgeTransactorSession) AuthorizeFrostCancellation(authorization FrostCancellationCertificateAuthorization, transaction BitcoinTxInfo, seats []uint16, signatures []byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.AuthorizeFrostCancellation(&_FrostBridge.TransactOpts, authorization, transaction, seats, signatures)
}

// ClaimFrostCancellationBudget is a paid mutator transaction binding the contract method 0xf0e6e3fb.
//
// Solidity: function claimFrostCancellationBudget(bytes32 reservationId, bytes32 transactionHash) returns()
func (_FrostBridge *FrostBridgeTransactor) ClaimFrostCancellationBudget(opts *bind.TransactOpts, reservationId [32]byte, transactionHash [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "claimFrostCancellationBudget", reservationId, transactionHash)
}

// ClaimFrostCancellationBudget is a paid mutator transaction binding the contract method 0xf0e6e3fb.
//
// Solidity: function claimFrostCancellationBudget(bytes32 reservationId, bytes32 transactionHash) returns()
func (_FrostBridge *FrostBridgeSession) ClaimFrostCancellationBudget(reservationId [32]byte, transactionHash [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ClaimFrostCancellationBudget(&_FrostBridge.TransactOpts, reservationId, transactionHash)
}

// ClaimFrostCancellationBudget is a paid mutator transaction binding the contract method 0xf0e6e3fb.
//
// Solidity: function claimFrostCancellationBudget(bytes32 reservationId, bytes32 transactionHash) returns()
func (_FrostBridge *FrostBridgeTransactorSession) ClaimFrostCancellationBudget(reservationId [32]byte, transactionHash [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ClaimFrostCancellationBudget(&_FrostBridge.TransactOpts, reservationId, transactionHash)
}

// ConfigureFrostFunding is a paid mutator transaction binding the contract method 0xca8fdb25.
//
// Solidity: function configureFrostFunding(address verifier, bytes32 runtimeHash, bytes32 qualificationRecord, bytes32 bitcoinGenesis) returns()
func (_FrostBridge *FrostBridgeTransactor) ConfigureFrostFunding(opts *bind.TransactOpts, verifier common.Address, runtimeHash [32]byte, qualificationRecord [32]byte, bitcoinGenesis [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "configureFrostFunding", verifier, runtimeHash, qualificationRecord, bitcoinGenesis)
}

// ConfigureFrostFunding is a paid mutator transaction binding the contract method 0xca8fdb25.
//
// Solidity: function configureFrostFunding(address verifier, bytes32 runtimeHash, bytes32 qualificationRecord, bytes32 bitcoinGenesis) returns()
func (_FrostBridge *FrostBridgeSession) ConfigureFrostFunding(verifier common.Address, runtimeHash [32]byte, qualificationRecord [32]byte, bitcoinGenesis [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ConfigureFrostFunding(&_FrostBridge.TransactOpts, verifier, runtimeHash, qualificationRecord, bitcoinGenesis)
}

// ConfigureFrostFunding is a paid mutator transaction binding the contract method 0xca8fdb25.
//
// Solidity: function configureFrostFunding(address verifier, bytes32 runtimeHash, bytes32 qualificationRecord, bytes32 bitcoinGenesis) returns()
func (_FrostBridge *FrostBridgeTransactorSession) ConfigureFrostFunding(verifier common.Address, runtimeHash [32]byte, qualificationRecord [32]byte, bitcoinGenesis [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ConfigureFrostFunding(&_FrostBridge.TransactOpts, verifier, runtimeHash, qualificationRecord, bitcoinGenesis)
}

// ConfigureFrostRedemptions is a paid mutator transaction binding the contract method 0x1e067cc4.
//
// Solidity: function configureFrostRedemptions(bytes32 genesis, uint64 finalityBlocks, uint64 maximumCertificateBlocks, address watchtower, bytes32 legacyTransitionEvidence) returns()
func (_FrostBridge *FrostBridgeTransactor) ConfigureFrostRedemptions(opts *bind.TransactOpts, genesis [32]byte, finalityBlocks uint64, maximumCertificateBlocks uint64, watchtower common.Address, legacyTransitionEvidence [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "configureFrostRedemptions", genesis, finalityBlocks, maximumCertificateBlocks, watchtower, legacyTransitionEvidence)
}

// ConfigureFrostRedemptions is a paid mutator transaction binding the contract method 0x1e067cc4.
//
// Solidity: function configureFrostRedemptions(bytes32 genesis, uint64 finalityBlocks, uint64 maximumCertificateBlocks, address watchtower, bytes32 legacyTransitionEvidence) returns()
func (_FrostBridge *FrostBridgeSession) ConfigureFrostRedemptions(genesis [32]byte, finalityBlocks uint64, maximumCertificateBlocks uint64, watchtower common.Address, legacyTransitionEvidence [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ConfigureFrostRedemptions(&_FrostBridge.TransactOpts, genesis, finalityBlocks, maximumCertificateBlocks, watchtower, legacyTransitionEvidence)
}

// ConfigureFrostRedemptions is a paid mutator transaction binding the contract method 0x1e067cc4.
//
// Solidity: function configureFrostRedemptions(bytes32 genesis, uint64 finalityBlocks, uint64 maximumCertificateBlocks, address watchtower, bytes32 legacyTransitionEvidence) returns()
func (_FrostBridge *FrostBridgeTransactorSession) ConfigureFrostRedemptions(genesis [32]byte, finalityBlocks uint64, maximumCertificateBlocks uint64, watchtower common.Address, legacyTransitionEvidence [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ConfigureFrostRedemptions(&_FrostBridge.TransactOpts, genesis, finalityBlocks, maximumCertificateBlocks, watchtower, legacyTransitionEvidence)
}

// ConfigureFrostRegistry is a paid mutator transaction binding the contract method 0xc016646c.
//
// Solidity: function configureFrostRegistry(address registry) returns()
func (_FrostBridge *FrostBridgeTransactor) ConfigureFrostRegistry(opts *bind.TransactOpts, registry common.Address) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "configureFrostRegistry", registry)
}

// ConfigureFrostRegistry is a paid mutator transaction binding the contract method 0xc016646c.
//
// Solidity: function configureFrostRegistry(address registry) returns()
func (_FrostBridge *FrostBridgeSession) ConfigureFrostRegistry(registry common.Address) (*types.Transaction, error) {
	return _FrostBridge.Contract.ConfigureFrostRegistry(&_FrostBridge.TransactOpts, registry)
}

// ConfigureFrostRegistry is a paid mutator transaction binding the contract method 0xc016646c.
//
// Solidity: function configureFrostRegistry(address registry) returns()
func (_FrostBridge *FrostBridgeTransactorSession) ConfigureFrostRegistry(registry common.Address) (*types.Transaction, error) {
	return _FrostBridge.Contract.ConfigureFrostRegistry(&_FrostBridge.TransactOpts, registry)
}

// DefeatFrostFraudChallenge is a paid mutator transaction binding the contract method 0x875bd42e.
//
// Solidity: function defeatFrostFraudChallenge((bytes32,(bytes4,bytes,bytes,bytes4),(uint64,bytes)[],uint32,bytes32,uint8,bytes32) defense) returns()
func (_FrostBridge *FrostBridgeTransactor) DefeatFrostFraudChallenge(opts *bind.TransactOpts, defense FrostFraudDefense) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "defeatFrostFraudChallenge", defense)
}

// DefeatFrostFraudChallenge is a paid mutator transaction binding the contract method 0x875bd42e.
//
// Solidity: function defeatFrostFraudChallenge((bytes32,(bytes4,bytes,bytes,bytes4),(uint64,bytes)[],uint32,bytes32,uint8,bytes32) defense) returns()
func (_FrostBridge *FrostBridgeSession) DefeatFrostFraudChallenge(defense FrostFraudDefense) (*types.Transaction, error) {
	return _FrostBridge.Contract.DefeatFrostFraudChallenge(&_FrostBridge.TransactOpts, defense)
}

// DefeatFrostFraudChallenge is a paid mutator transaction binding the contract method 0x875bd42e.
//
// Solidity: function defeatFrostFraudChallenge((bytes32,(bytes4,bytes,bytes,bytes4),(uint64,bytes)[],uint32,bytes32,uint8,bytes32) defense) returns()
func (_FrostBridge *FrostBridgeTransactorSession) DefeatFrostFraudChallenge(defense FrostFraudDefense) (*types.Transaction, error) {
	return _FrostBridge.Contract.DefeatFrostFraudChallenge(&_FrostBridge.TransactOpts, defense)
}

// DefeatFrostFraudChallengeWithHeartbeat is a paid mutator transaction binding the contract method 0x437e458f.
//
// Solidity: function defeatFrostFraudChallengeWithHeartbeat(bytes32 walletId, uint64 nonce) returns()
func (_FrostBridge *FrostBridgeTransactor) DefeatFrostFraudChallengeWithHeartbeat(opts *bind.TransactOpts, walletId [32]byte, nonce uint64) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "defeatFrostFraudChallengeWithHeartbeat", walletId, nonce)
}

// DefeatFrostFraudChallengeWithHeartbeat is a paid mutator transaction binding the contract method 0x437e458f.
//
// Solidity: function defeatFrostFraudChallengeWithHeartbeat(bytes32 walletId, uint64 nonce) returns()
func (_FrostBridge *FrostBridgeSession) DefeatFrostFraudChallengeWithHeartbeat(walletId [32]byte, nonce uint64) (*types.Transaction, error) {
	return _FrostBridge.Contract.DefeatFrostFraudChallengeWithHeartbeat(&_FrostBridge.TransactOpts, walletId, nonce)
}

// DefeatFrostFraudChallengeWithHeartbeat is a paid mutator transaction binding the contract method 0x437e458f.
//
// Solidity: function defeatFrostFraudChallengeWithHeartbeat(bytes32 walletId, uint64 nonce) returns()
func (_FrostBridge *FrostBridgeTransactorSession) DefeatFrostFraudChallengeWithHeartbeat(walletId [32]byte, nonce uint64) (*types.Transaction, error) {
	return _FrostBridge.Contract.DefeatFrostFraudChallengeWithHeartbeat(&_FrostBridge.TransactOpts, walletId, nonce)
}

// FinalizeFrostWalletClosing is a paid mutator transaction binding the contract method 0x3272ba64.
//
// Solidity: function finalizeFrostWalletClosing(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactor) FinalizeFrostWalletClosing(opts *bind.TransactOpts, walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "finalizeFrostWalletClosing", walletId)
}

// FinalizeFrostWalletClosing is a paid mutator transaction binding the contract method 0x3272ba64.
//
// Solidity: function finalizeFrostWalletClosing(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeSession) FinalizeFrostWalletClosing(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.FinalizeFrostWalletClosing(&_FrostBridge.TransactOpts, walletId)
}

// FinalizeFrostWalletClosing is a paid mutator transaction binding the contract method 0x3272ba64.
//
// Solidity: function finalizeFrostWalletClosing(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) FinalizeFrostWalletClosing(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.FinalizeFrostWalletClosing(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostFraudChallengeDefeatTimeout is a paid mutator transaction binding the contract method 0x662b8319.
//
// Solidity: function notifyFrostFraudChallengeDefeatTimeout(bytes32 walletId, bytes32 digest) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostFraudChallengeDefeatTimeout(opts *bind.TransactOpts, walletId [32]byte, digest [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostFraudChallengeDefeatTimeout", walletId, digest)
}

// NotifyFrostFraudChallengeDefeatTimeout is a paid mutator transaction binding the contract method 0x662b8319.
//
// Solidity: function notifyFrostFraudChallengeDefeatTimeout(bytes32 walletId, bytes32 digest) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostFraudChallengeDefeatTimeout(walletId [32]byte, digest [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostFraudChallengeDefeatTimeout(&_FrostBridge.TransactOpts, walletId, digest)
}

// NotifyFrostFraudChallengeDefeatTimeout is a paid mutator transaction binding the contract method 0x662b8319.
//
// Solidity: function notifyFrostFraudChallengeDefeatTimeout(bytes32 walletId, bytes32 digest) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostFraudChallengeDefeatTimeout(walletId [32]byte, digest [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostFraudChallengeDefeatTimeout(&_FrostBridge.TransactOpts, walletId, digest)
}

// NotifyFrostMovedFundsSweepTimeout is a paid mutator transaction binding the contract method 0x22f7b61f.
//
// Solidity: function notifyFrostMovedFundsSweepTimeout(bytes32 outpoint) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostMovedFundsSweepTimeout(opts *bind.TransactOpts, outpoint [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostMovedFundsSweepTimeout", outpoint)
}

// NotifyFrostMovedFundsSweepTimeout is a paid mutator transaction binding the contract method 0x22f7b61f.
//
// Solidity: function notifyFrostMovedFundsSweepTimeout(bytes32 outpoint) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostMovedFundsSweepTimeout(outpoint [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostMovedFundsSweepTimeout(&_FrostBridge.TransactOpts, outpoint)
}

// NotifyFrostMovedFundsSweepTimeout is a paid mutator transaction binding the contract method 0x22f7b61f.
//
// Solidity: function notifyFrostMovedFundsSweepTimeout(bytes32 outpoint) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostMovedFundsSweepTimeout(outpoint [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostMovedFundsSweepTimeout(&_FrostBridge.TransactOpts, outpoint)
}

// NotifyFrostMovingFundsBelowDust is a paid mutator transaction binding the contract method 0xad8fc553.
//
// Solidity: function notifyFrostMovingFundsBelowDust(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostMovingFundsBelowDust(opts *bind.TransactOpts, walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostMovingFundsBelowDust", walletId)
}

// NotifyFrostMovingFundsBelowDust is a paid mutator transaction binding the contract method 0xad8fc553.
//
// Solidity: function notifyFrostMovingFundsBelowDust(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostMovingFundsBelowDust(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostMovingFundsBelowDust(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostMovingFundsBelowDust is a paid mutator transaction binding the contract method 0xad8fc553.
//
// Solidity: function notifyFrostMovingFundsBelowDust(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostMovingFundsBelowDust(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostMovingFundsBelowDust(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostMovingFundsTimeout is a paid mutator transaction binding the contract method 0x0ed8d3d2.
//
// Solidity: function notifyFrostMovingFundsTimeout(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostMovingFundsTimeout(opts *bind.TransactOpts, walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostMovingFundsTimeout", walletId)
}

// NotifyFrostMovingFundsTimeout is a paid mutator transaction binding the contract method 0x0ed8d3d2.
//
// Solidity: function notifyFrostMovingFundsTimeout(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostMovingFundsTimeout(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostMovingFundsTimeout(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostMovingFundsTimeout is a paid mutator transaction binding the contract method 0x0ed8d3d2.
//
// Solidity: function notifyFrostMovingFundsTimeout(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostMovingFundsTimeout(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostMovingFundsTimeout(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostOperatorInactivity is a paid mutator transaction binding the contract method 0x89e579bc.
//
// Solidity: function notifyFrostOperatorInactivity(bytes32 walletId, uint64 nonce, uint16[] inactiveSeats, bool heartbeatFailed, uint16[] signingSeats, bytes signatures) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostOperatorInactivity(opts *bind.TransactOpts, walletId [32]byte, nonce uint64, inactiveSeats []uint16, heartbeatFailed bool, signingSeats []uint16, signatures []byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostOperatorInactivity", walletId, nonce, inactiveSeats, heartbeatFailed, signingSeats, signatures)
}

// NotifyFrostOperatorInactivity is a paid mutator transaction binding the contract method 0x89e579bc.
//
// Solidity: function notifyFrostOperatorInactivity(bytes32 walletId, uint64 nonce, uint16[] inactiveSeats, bool heartbeatFailed, uint16[] signingSeats, bytes signatures) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostOperatorInactivity(walletId [32]byte, nonce uint64, inactiveSeats []uint16, heartbeatFailed bool, signingSeats []uint16, signatures []byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostOperatorInactivity(&_FrostBridge.TransactOpts, walletId, nonce, inactiveSeats, heartbeatFailed, signingSeats, signatures)
}

// NotifyFrostOperatorInactivity is a paid mutator transaction binding the contract method 0x89e579bc.
//
// Solidity: function notifyFrostOperatorInactivity(bytes32 walletId, uint64 nonce, uint16[] inactiveSeats, bool heartbeatFailed, uint16[] signingSeats, bytes signatures) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostOperatorInactivity(walletId [32]byte, nonce uint64, inactiveSeats []uint16, heartbeatFailed bool, signingSeats []uint16, signatures []byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostOperatorInactivity(&_FrostBridge.TransactOpts, walletId, nonce, inactiveSeats, heartbeatFailed, signingSeats, signatures)
}

// NotifyFrostRedemptionTimeout is a paid mutator transaction binding the contract method 0xdf222e56.
//
// Solidity: function notifyFrostRedemptionTimeout(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostRedemptionTimeout(opts *bind.TransactOpts, requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostRedemptionTimeout", requestId)
}

// NotifyFrostRedemptionTimeout is a paid mutator transaction binding the contract method 0xdf222e56.
//
// Solidity: function notifyFrostRedemptionTimeout(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostRedemptionTimeout(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostRedemptionTimeout(&_FrostBridge.TransactOpts, requestId)
}

// NotifyFrostRedemptionTimeout is a paid mutator transaction binding the contract method 0xdf222e56.
//
// Solidity: function notifyFrostRedemptionTimeout(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostRedemptionTimeout(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostRedemptionTimeout(&_FrostBridge.TransactOpts, requestId)
}

// NotifyFrostWalletCloseable is a paid mutator transaction binding the contract method 0x828ae5b4.
//
// Solidity: function notifyFrostWalletCloseable(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostWalletCloseable(opts *bind.TransactOpts, walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostWalletCloseable", walletId)
}

// NotifyFrostWalletCloseable is a paid mutator transaction binding the contract method 0x828ae5b4.
//
// Solidity: function notifyFrostWalletCloseable(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostWalletCloseable(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostWalletCloseable(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostWalletCloseable is a paid mutator transaction binding the contract method 0x828ae5b4.
//
// Solidity: function notifyFrostWalletCloseable(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostWalletCloseable(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostWalletCloseable(&_FrostBridge.TransactOpts, walletId)
}

// NotifyFrostWalletRedemptionTimeout is a paid mutator transaction binding the contract method 0x5a880a1e.
//
// Solidity: function notifyFrostWalletRedemptionTimeout(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactor) NotifyFrostWalletRedemptionTimeout(opts *bind.TransactOpts, requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "notifyFrostWalletRedemptionTimeout", requestId)
}

// NotifyFrostWalletRedemptionTimeout is a paid mutator transaction binding the contract method 0x5a880a1e.
//
// Solidity: function notifyFrostWalletRedemptionTimeout(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeSession) NotifyFrostWalletRedemptionTimeout(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostWalletRedemptionTimeout(&_FrostBridge.TransactOpts, requestId)
}

// NotifyFrostWalletRedemptionTimeout is a paid mutator transaction binding the contract method 0x5a880a1e.
//
// Solidity: function notifyFrostWalletRedemptionTimeout(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) NotifyFrostWalletRedemptionTimeout(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.NotifyFrostWalletRedemptionTimeout(&_FrostBridge.TransactOpts, requestId)
}

// QuarantineFrostWallet is a paid mutator transaction binding the contract method 0x170e1d83.
//
// Solidity: function quarantineFrostWallet(bytes32 walletId, uint64 expectedStateVersion, bytes32 reasonHash, bytes32 evidenceHash) returns()
func (_FrostBridge *FrostBridgeTransactor) QuarantineFrostWallet(opts *bind.TransactOpts, walletId [32]byte, expectedStateVersion uint64, reasonHash [32]byte, evidenceHash [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "quarantineFrostWallet", walletId, expectedStateVersion, reasonHash, evidenceHash)
}

// QuarantineFrostWallet is a paid mutator transaction binding the contract method 0x170e1d83.
//
// Solidity: function quarantineFrostWallet(bytes32 walletId, uint64 expectedStateVersion, bytes32 reasonHash, bytes32 evidenceHash) returns()
func (_FrostBridge *FrostBridgeSession) QuarantineFrostWallet(walletId [32]byte, expectedStateVersion uint64, reasonHash [32]byte, evidenceHash [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.QuarantineFrostWallet(&_FrostBridge.TransactOpts, walletId, expectedStateVersion, reasonHash, evidenceHash)
}

// QuarantineFrostWallet is a paid mutator transaction binding the contract method 0x170e1d83.
//
// Solidity: function quarantineFrostWallet(bytes32 walletId, uint64 expectedStateVersion, bytes32 reasonHash, bytes32 evidenceHash) returns()
func (_FrostBridge *FrostBridgeTransactorSession) QuarantineFrostWallet(walletId [32]byte, expectedStateVersion uint64, reasonHash [32]byte, evidenceHash [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.QuarantineFrostWallet(&_FrostBridge.TransactOpts, walletId, expectedStateVersion, reasonHash, evidenceHash)
}

// RefundResolvedFrostRedemption is a paid mutator transaction binding the contract method 0xa6501248.
//
// Solidity: function refundResolvedFrostRedemption(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactor) RefundResolvedFrostRedemption(opts *bind.TransactOpts, requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "refundResolvedFrostRedemption", requestId)
}

// RefundResolvedFrostRedemption is a paid mutator transaction binding the contract method 0xa6501248.
//
// Solidity: function refundResolvedFrostRedemption(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeSession) RefundResolvedFrostRedemption(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.RefundResolvedFrostRedemption(&_FrostBridge.TransactOpts, requestId)
}

// RefundResolvedFrostRedemption is a paid mutator transaction binding the contract method 0xa6501248.
//
// Solidity: function refundResolvedFrostRedemption(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) RefundResolvedFrostRedemption(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.RefundResolvedFrostRedemption(&_FrostBridge.TransactOpts, requestId)
}

// RefundUnreservedFrostRedemption is a paid mutator transaction binding the contract method 0x00a4cec4.
//
// Solidity: function refundUnreservedFrostRedemption(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactor) RefundUnreservedFrostRedemption(opts *bind.TransactOpts, requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "refundUnreservedFrostRedemption", requestId)
}

// RefundUnreservedFrostRedemption is a paid mutator transaction binding the contract method 0x00a4cec4.
//
// Solidity: function refundUnreservedFrostRedemption(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeSession) RefundUnreservedFrostRedemption(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.RefundUnreservedFrostRedemption(&_FrostBridge.TransactOpts, requestId)
}

// RefundUnreservedFrostRedemption is a paid mutator transaction binding the contract method 0x00a4cec4.
//
// Solidity: function refundUnreservedFrostRedemption(bytes32 requestId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) RefundUnreservedFrostRedemption(requestId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.RefundUnreservedFrostRedemption(&_FrostBridge.TransactOpts, requestId)
}

// ReleaseFrostDrainOnConflict is a paid mutator transaction binding the contract method 0xbe2b8293.
//
// Solidity: function releaseFrostDrainOnConflict(bytes20 source, (bytes32,uint32,uint64) main, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactor) ReleaseFrostDrainOnConflict(opts *bind.TransactOpts, source [20]byte, main BitcoinTxUTXO, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "releaseFrostDrainOnConflict", source, main, transaction, proof)
}

// ReleaseFrostDrainOnConflict is a paid mutator transaction binding the contract method 0xbe2b8293.
//
// Solidity: function releaseFrostDrainOnConflict(bytes20 source, (bytes32,uint32,uint64) main, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeSession) ReleaseFrostDrainOnConflict(source [20]byte, main BitcoinTxUTXO, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.ReleaseFrostDrainOnConflict(&_FrostBridge.TransactOpts, source, main, transaction, proof)
}

// ReleaseFrostDrainOnConflict is a paid mutator transaction binding the contract method 0xbe2b8293.
//
// Solidity: function releaseFrostDrainOnConflict(bytes20 source, (bytes32,uint32,uint64) main, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactorSession) ReleaseFrostDrainOnConflict(source [20]byte, main BitcoinTxUTXO, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.ReleaseFrostDrainOnConflict(&_FrostBridge.TransactOpts, source, main, transaction, proof)
}

// RequestFrostRedemption is a paid mutator transaction binding the contract method 0x96d908c1.
//
// Solidity: function requestFrostRedemption(bytes32 walletId, bytes script, uint64 gross, address redeemer) returns(bytes32)
func (_FrostBridge *FrostBridgeTransactor) RequestFrostRedemption(opts *bind.TransactOpts, walletId [32]byte, script []byte, gross uint64, redeemer common.Address) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "requestFrostRedemption", walletId, script, gross, redeemer)
}

// RequestFrostRedemption is a paid mutator transaction binding the contract method 0x96d908c1.
//
// Solidity: function requestFrostRedemption(bytes32 walletId, bytes script, uint64 gross, address redeemer) returns(bytes32)
func (_FrostBridge *FrostBridgeSession) RequestFrostRedemption(walletId [32]byte, script []byte, gross uint64, redeemer common.Address) (*types.Transaction, error) {
	return _FrostBridge.Contract.RequestFrostRedemption(&_FrostBridge.TransactOpts, walletId, script, gross, redeemer)
}

// RequestFrostRedemption is a paid mutator transaction binding the contract method 0x96d908c1.
//
// Solidity: function requestFrostRedemption(bytes32 walletId, bytes script, uint64 gross, address redeemer) returns(bytes32)
func (_FrostBridge *FrostBridgeTransactorSession) RequestFrostRedemption(walletId [32]byte, script []byte, gross uint64, redeemer common.Address) (*types.Transaction, error) {
	return _FrostBridge.Contract.RequestFrostRedemption(&_FrostBridge.TransactOpts, walletId, script, gross, redeemer)
}

// RequestNewFrostWallet is a paid mutator transaction binding the contract method 0x7d0383b7.
//
// Solidity: function requestNewFrostWallet() returns()
func (_FrostBridge *FrostBridgeTransactor) RequestNewFrostWallet(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "requestNewFrostWallet")
}

// RequestNewFrostWallet is a paid mutator transaction binding the contract method 0x7d0383b7.
//
// Solidity: function requestNewFrostWallet() returns()
func (_FrostBridge *FrostBridgeSession) RequestNewFrostWallet() (*types.Transaction, error) {
	return _FrostBridge.Contract.RequestNewFrostWallet(&_FrostBridge.TransactOpts)
}

// RequestNewFrostWallet is a paid mutator transaction binding the contract method 0x7d0383b7.
//
// Solidity: function requestNewFrostWallet() returns()
func (_FrostBridge *FrostBridgeTransactorSession) RequestNewFrostWallet() (*types.Transaction, error) {
	return _FrostBridge.Contract.RequestNewFrostWallet(&_FrostBridge.TransactOpts)
}

// ReserveFrostRedemption is a paid mutator transaction binding the contract method 0xb4c5bc13.
//
// Solidity: function reserveFrostRedemption(((bytes32,bytes32,uint64,uint64,bytes32,bytes32,bytes32,uint32,uint64,uint64,uint64,uint64),(bytes4,bytes,bytes,bytes4),(bytes32,uint32,uint64)[],(bytes32,uint64,bytes)[],uint16[],bytes) proposal) returns(bytes32)
func (_FrostBridge *FrostBridgeTransactor) ReserveFrostRedemption(opts *bind.TransactOpts, proposal FrostRedemptionStateReservationProposal) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "reserveFrostRedemption", proposal)
}

// ReserveFrostRedemption is a paid mutator transaction binding the contract method 0xb4c5bc13.
//
// Solidity: function reserveFrostRedemption(((bytes32,bytes32,uint64,uint64,bytes32,bytes32,bytes32,uint32,uint64,uint64,uint64,uint64),(bytes4,bytes,bytes,bytes4),(bytes32,uint32,uint64)[],(bytes32,uint64,bytes)[],uint16[],bytes) proposal) returns(bytes32)
func (_FrostBridge *FrostBridgeSession) ReserveFrostRedemption(proposal FrostRedemptionStateReservationProposal) (*types.Transaction, error) {
	return _FrostBridge.Contract.ReserveFrostRedemption(&_FrostBridge.TransactOpts, proposal)
}

// ReserveFrostRedemption is a paid mutator transaction binding the contract method 0xb4c5bc13.
//
// Solidity: function reserveFrostRedemption(((bytes32,bytes32,uint64,uint64,bytes32,bytes32,bytes32,uint32,uint64,uint64,uint64,uint64),(bytes4,bytes,bytes,bytes4),(bytes32,uint32,uint64)[],(bytes32,uint64,bytes)[],uint16[],bytes) proposal) returns(bytes32)
func (_FrostBridge *FrostBridgeTransactorSession) ReserveFrostRedemption(proposal FrostRedemptionStateReservationProposal) (*types.Transaction, error) {
	return _FrostBridge.Contract.ReserveFrostRedemption(&_FrostBridge.TransactOpts, proposal)
}

// ResetFrostMovingFundsTimeout is a paid mutator transaction binding the contract method 0x80d78829.
//
// Solidity: function resetFrostMovingFundsTimeout(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactor) ResetFrostMovingFundsTimeout(opts *bind.TransactOpts, walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "resetFrostMovingFundsTimeout", walletId)
}

// ResetFrostMovingFundsTimeout is a paid mutator transaction binding the contract method 0x80d78829.
//
// Solidity: function resetFrostMovingFundsTimeout(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeSession) ResetFrostMovingFundsTimeout(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ResetFrostMovingFundsTimeout(&_FrostBridge.TransactOpts, walletId)
}

// ResetFrostMovingFundsTimeout is a paid mutator transaction binding the contract method 0x80d78829.
//
// Solidity: function resetFrostMovingFundsTimeout(bytes32 walletId) returns()
func (_FrostBridge *FrostBridgeTransactorSession) ResetFrostMovingFundsTimeout(walletId [32]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.ResetFrostMovingFundsTimeout(&_FrostBridge.TransactOpts, walletId)
}

// RevealFrostDeposit is a paid mutator transaction binding the contract method 0xe55ecf19.
//
// Solidity: function revealFrostDeposit((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms, (bytes4,bytes,bytes,bytes4) transaction, uint32 outputIndex) returns(bytes32)
func (_FrostBridge *FrostBridgeTransactor) RevealFrostDeposit(opts *bind.TransactOpts, terms FrostDepositProfileTerms, transaction BitcoinTxInfo, outputIndex uint32) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "revealFrostDeposit", terms, transaction, outputIndex)
}

// RevealFrostDeposit is a paid mutator transaction binding the contract method 0xe55ecf19.
//
// Solidity: function revealFrostDeposit((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms, (bytes4,bytes,bytes,bytes4) transaction, uint32 outputIndex) returns(bytes32)
func (_FrostBridge *FrostBridgeSession) RevealFrostDeposit(terms FrostDepositProfileTerms, transaction BitcoinTxInfo, outputIndex uint32) (*types.Transaction, error) {
	return _FrostBridge.Contract.RevealFrostDeposit(&_FrostBridge.TransactOpts, terms, transaction, outputIndex)
}

// RevealFrostDeposit is a paid mutator transaction binding the contract method 0xe55ecf19.
//
// Solidity: function revealFrostDeposit((uint256,address,bytes32,bytes32,bytes32,address,address,bytes8,uint64,bytes32,uint32,bytes32) terms, (bytes4,bytes,bytes,bytes4) transaction, uint32 outputIndex) returns(bytes32)
func (_FrostBridge *FrostBridgeTransactorSession) RevealFrostDeposit(terms FrostDepositProfileTerms, transaction BitcoinTxInfo, outputIndex uint32) (*types.Transaction, error) {
	return _FrostBridge.Contract.RevealFrostDeposit(&_FrostBridge.TransactOpts, terms, transaction, outputIndex)
}

// SetFrostDrainEnabled is a paid mutator transaction binding the contract method 0x003d5513.
//
// Solidity: function setFrostDrainEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeTransactor) SetFrostDrainEnabled(opts *bind.TransactOpts, enabled bool) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "setFrostDrainEnabled", enabled)
}

// SetFrostDrainEnabled is a paid mutator transaction binding the contract method 0x003d5513.
//
// Solidity: function setFrostDrainEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeSession) SetFrostDrainEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostDrainEnabled(&_FrostBridge.TransactOpts, enabled)
}

// SetFrostDrainEnabled is a paid mutator transaction binding the contract method 0x003d5513.
//
// Solidity: function setFrostDrainEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SetFrostDrainEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostDrainEnabled(&_FrostBridge.TransactOpts, enabled)
}

// SetFrostFundingGates is a paid mutator transaction binding the contract method 0x88beb592.
//
// Solidity: function setFrostFundingGates(bool addressesEnabled, bool revealEnabled) returns()
func (_FrostBridge *FrostBridgeTransactor) SetFrostFundingGates(opts *bind.TransactOpts, addressesEnabled bool, revealEnabled bool) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "setFrostFundingGates", addressesEnabled, revealEnabled)
}

// SetFrostFundingGates is a paid mutator transaction binding the contract method 0x88beb592.
//
// Solidity: function setFrostFundingGates(bool addressesEnabled, bool revealEnabled) returns()
func (_FrostBridge *FrostBridgeSession) SetFrostFundingGates(addressesEnabled bool, revealEnabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostFundingGates(&_FrostBridge.TransactOpts, addressesEnabled, revealEnabled)
}

// SetFrostFundingGates is a paid mutator transaction binding the contract method 0x88beb592.
//
// Solidity: function setFrostFundingGates(bool addressesEnabled, bool revealEnabled) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SetFrostFundingGates(addressesEnabled bool, revealEnabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostFundingGates(&_FrostBridge.TransactOpts, addressesEnabled, revealEnabled)
}

// SetFrostRedemptionsEnabled is a paid mutator transaction binding the contract method 0x66da11f7.
//
// Solidity: function setFrostRedemptionsEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeTransactor) SetFrostRedemptionsEnabled(opts *bind.TransactOpts, enabled bool) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "setFrostRedemptionsEnabled", enabled)
}

// SetFrostRedemptionsEnabled is a paid mutator transaction binding the contract method 0x66da11f7.
//
// Solidity: function setFrostRedemptionsEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeSession) SetFrostRedemptionsEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostRedemptionsEnabled(&_FrostBridge.TransactOpts, enabled)
}

// SetFrostRedemptionsEnabled is a paid mutator transaction binding the contract method 0x66da11f7.
//
// Solidity: function setFrostRedemptionsEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SetFrostRedemptionsEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostRedemptionsEnabled(&_FrostBridge.TransactOpts, enabled)
}

// SetFrostRequestsEnabled is a paid mutator transaction binding the contract method 0xfe040aa1.
//
// Solidity: function setFrostRequestsEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeTransactor) SetFrostRequestsEnabled(opts *bind.TransactOpts, enabled bool) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "setFrostRequestsEnabled", enabled)
}

// SetFrostRequestsEnabled is a paid mutator transaction binding the contract method 0xfe040aa1.
//
// Solidity: function setFrostRequestsEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeSession) SetFrostRequestsEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostRequestsEnabled(&_FrostBridge.TransactOpts, enabled)
}

// SetFrostRequestsEnabled is a paid mutator transaction binding the contract method 0xfe040aa1.
//
// Solidity: function setFrostRequestsEnabled(bool enabled) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SetFrostRequestsEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostBridge.Contract.SetFrostRequestsEnabled(&_FrostBridge.TransactOpts, enabled)
}

// SubmitFrostCancellationProof is a paid mutator transaction binding the contract method 0x0c79aeb9.
//
// Solidity: function submitFrostCancellationProof(bytes32 reservationId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostCancellationProof(opts *bind.TransactOpts, reservationId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostCancellationProof", reservationId, transaction, proof)
}

// SubmitFrostCancellationProof is a paid mutator transaction binding the contract method 0x0c79aeb9.
//
// Solidity: function submitFrostCancellationProof(bytes32 reservationId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostCancellationProof(reservationId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostCancellationProof(&_FrostBridge.TransactOpts, reservationId, transaction, proof)
}

// SubmitFrostCancellationProof is a paid mutator transaction binding the contract method 0x0c79aeb9.
//
// Solidity: function submitFrostCancellationProof(bytes32 reservationId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostCancellationProof(reservationId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostCancellationProof(&_FrostBridge.TransactOpts, reservationId, transaction, proof)
}

// SubmitFrostDepositSweepProof is a paid mutator transaction binding the contract method 0x40d2d1ff.
//
// Solidity: function submitFrostDepositSweepProof((bytes32,(bytes4,bytes,bytes,bytes4),(bytes,uint256,bytes,bytes32,bytes),bool,address) request) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostDepositSweepProof(opts *bind.TransactOpts, request FrostDepositSweepSweep) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostDepositSweepProof", request)
}

// SubmitFrostDepositSweepProof is a paid mutator transaction binding the contract method 0x40d2d1ff.
//
// Solidity: function submitFrostDepositSweepProof((bytes32,(bytes4,bytes,bytes,bytes4),(bytes,uint256,bytes,bytes32,bytes),bool,address) request) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostDepositSweepProof(request FrostDepositSweepSweep) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostDepositSweepProof(&_FrostBridge.TransactOpts, request)
}

// SubmitFrostDepositSweepProof is a paid mutator transaction binding the contract method 0x40d2d1ff.
//
// Solidity: function submitFrostDepositSweepProof((bytes32,(bytes4,bytes,bytes,bytes4),(bytes,uint256,bytes,bytes32,bytes),bool,address) request) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostDepositSweepProof(request FrostDepositSweepSweep) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostDepositSweepProof(&_FrostBridge.TransactOpts, request)
}

// SubmitFrostFraudChallenge is a paid mutator transaction binding the contract method 0x4595a54b.
//
// Solidity: function submitFrostFraudChallenge(bytes32 walletId, bytes32 digest, bytes signature) payable returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostFraudChallenge(opts *bind.TransactOpts, walletId [32]byte, digest [32]byte, signature []byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostFraudChallenge", walletId, digest, signature)
}

// SubmitFrostFraudChallenge is a paid mutator transaction binding the contract method 0x4595a54b.
//
// Solidity: function submitFrostFraudChallenge(bytes32 walletId, bytes32 digest, bytes signature) payable returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostFraudChallenge(walletId [32]byte, digest [32]byte, signature []byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostFraudChallenge(&_FrostBridge.TransactOpts, walletId, digest, signature)
}

// SubmitFrostFraudChallenge is a paid mutator transaction binding the contract method 0x4595a54b.
//
// Solidity: function submitFrostFraudChallenge(bytes32 walletId, bytes32 digest, bytes signature) payable returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostFraudChallenge(walletId [32]byte, digest [32]byte, signature []byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostFraudChallenge(&_FrostBridge.TransactOpts, walletId, digest, signature)
}

// SubmitFrostMovedFundsSweepProof is a paid mutator transaction binding the contract method 0xa5b12d07.
//
// Solidity: function submitFrostMovedFundsSweepProof(bytes32 walletId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostMovedFundsSweepProof(opts *bind.TransactOpts, walletId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostMovedFundsSweepProof", walletId, transaction, proof)
}

// SubmitFrostMovedFundsSweepProof is a paid mutator transaction binding the contract method 0xa5b12d07.
//
// Solidity: function submitFrostMovedFundsSweepProof(bytes32 walletId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostMovedFundsSweepProof(walletId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostMovedFundsSweepProof(&_FrostBridge.TransactOpts, walletId, transaction, proof)
}

// SubmitFrostMovedFundsSweepProof is a paid mutator transaction binding the contract method 0xa5b12d07.
//
// Solidity: function submitFrostMovedFundsSweepProof(bytes32 walletId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostMovedFundsSweepProof(walletId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostMovedFundsSweepProof(&_FrostBridge.TransactOpts, walletId, transaction, proof)
}

// SubmitFrostMovingFundsCommitment is a paid mutator transaction binding the contract method 0x952d9182.
//
// Solidity: function submitFrostMovingFundsCommitment(bytes32 walletId, uint16 seat, bytes20[] targets) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostMovingFundsCommitment(opts *bind.TransactOpts, walletId [32]byte, seat uint16, targets [][20]byte) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostMovingFundsCommitment", walletId, seat, targets)
}

// SubmitFrostMovingFundsCommitment is a paid mutator transaction binding the contract method 0x952d9182.
//
// Solidity: function submitFrostMovingFundsCommitment(bytes32 walletId, uint16 seat, bytes20[] targets) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostMovingFundsCommitment(walletId [32]byte, seat uint16, targets [][20]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostMovingFundsCommitment(&_FrostBridge.TransactOpts, walletId, seat, targets)
}

// SubmitFrostMovingFundsCommitment is a paid mutator transaction binding the contract method 0x952d9182.
//
// Solidity: function submitFrostMovingFundsCommitment(bytes32 walletId, uint16 seat, bytes20[] targets) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostMovingFundsCommitment(walletId [32]byte, seat uint16, targets [][20]byte) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostMovingFundsCommitment(&_FrostBridge.TransactOpts, walletId, seat, targets)
}

// SubmitFrostMovingFundsProof is a paid mutator transaction binding the contract method 0x55d2024c.
//
// Solidity: function submitFrostMovingFundsProof(bytes32 walletId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostMovingFundsProof(opts *bind.TransactOpts, walletId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostMovingFundsProof", walletId, transaction, proof)
}

// SubmitFrostMovingFundsProof is a paid mutator transaction binding the contract method 0x55d2024c.
//
// Solidity: function submitFrostMovingFundsProof(bytes32 walletId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostMovingFundsProof(walletId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostMovingFundsProof(&_FrostBridge.TransactOpts, walletId, transaction, proof)
}

// SubmitFrostMovingFundsProof is a paid mutator transaction binding the contract method 0x55d2024c.
//
// Solidity: function submitFrostMovingFundsProof(bytes32 walletId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostMovingFundsProof(walletId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostMovingFundsProof(&_FrostBridge.TransactOpts, walletId, transaction, proof)
}

// SubmitFrostRedemptionProof is a paid mutator transaction binding the contract method 0x63625f73.
//
// Solidity: function submitFrostRedemptionProof(bytes32 reservationId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostRedemptionProof(opts *bind.TransactOpts, reservationId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostRedemptionProof", reservationId, transaction, proof)
}

// SubmitFrostRedemptionProof is a paid mutator transaction binding the contract method 0x63625f73.
//
// Solidity: function submitFrostRedemptionProof(bytes32 reservationId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostRedemptionProof(reservationId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostRedemptionProof(&_FrostBridge.TransactOpts, reservationId, transaction, proof)
}

// SubmitFrostRedemptionProof is a paid mutator transaction binding the contract method 0x63625f73.
//
// Solidity: function submitFrostRedemptionProof(bytes32 reservationId, (bytes4,bytes,bytes,bytes4) transaction, (bytes,uint256,bytes,bytes32,bytes) proof) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostRedemptionProof(reservationId [32]byte, transaction BitcoinTxInfo, proof BitcoinTxProof) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostRedemptionProof(&_FrostBridge.TransactOpts, reservationId, transaction, proof)
}

// SubmitFrostSweepResolutionProof is a paid mutator transaction binding the contract method 0xbfd154f8.
//
// Solidity: function submitFrostSweepResolutionProof(((bytes32,(bytes4,bytes,bytes,bytes4),(bytes,uint256,bytes,bytes32,bytes),bool,address),(bytes32,bytes32,address,uint64),uint16[],bytes) resolution) returns()
func (_FrostBridge *FrostBridgeTransactor) SubmitFrostSweepResolutionProof(opts *bind.TransactOpts, resolution FrostDepositSweepResolution) (*types.Transaction, error) {
	return _FrostBridge.contract.Transact(opts, "submitFrostSweepResolutionProof", resolution)
}

// SubmitFrostSweepResolutionProof is a paid mutator transaction binding the contract method 0xbfd154f8.
//
// Solidity: function submitFrostSweepResolutionProof(((bytes32,(bytes4,bytes,bytes,bytes4),(bytes,uint256,bytes,bytes32,bytes),bool,address),(bytes32,bytes32,address,uint64),uint16[],bytes) resolution) returns()
func (_FrostBridge *FrostBridgeSession) SubmitFrostSweepResolutionProof(resolution FrostDepositSweepResolution) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostSweepResolutionProof(&_FrostBridge.TransactOpts, resolution)
}

// SubmitFrostSweepResolutionProof is a paid mutator transaction binding the contract method 0xbfd154f8.
//
// Solidity: function submitFrostSweepResolutionProof(((bytes32,(bytes4,bytes,bytes,bytes4),(bytes,uint256,bytes,bytes32,bytes),bool,address),(bytes32,bytes32,address,uint64),uint16[],bytes) resolution) returns()
func (_FrostBridge *FrostBridgeTransactorSession) SubmitFrostSweepResolutionProof(resolution FrostDepositSweepResolution) (*types.Transaction, error) {
	return _FrostBridge.Contract.SubmitFrostSweepResolutionProof(&_FrostBridge.TransactOpts, resolution)
}

// FrostBridgeFrostCancellationAuthorizedIterator is returned from FilterFrostCancellationAuthorized and is used to iterate over the raw logs and unpacked data for FrostCancellationAuthorized events raised by the FrostBridge contract.
type FrostBridgeFrostCancellationAuthorizedIterator struct {
	Event *FrostBridgeFrostCancellationAuthorized // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostCancellationAuthorizedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostCancellationAuthorized)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostCancellationAuthorized)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostCancellationAuthorizedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostCancellationAuthorizedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostCancellationAuthorized represents a FrostCancellationAuthorized event raised by the FrostBridge contract.
type FrostBridgeFrostCancellationAuthorized struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Fee             uint64
	Sponsor         common.Address
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostCancellationAuthorized is a free log retrieval operation binding the contract event 0x17748976bed651bc2d640dec8f479c79c456b3814aeebcfd05e234291c85325b.
//
// Solidity: event FrostCancellationAuthorized(bytes32 indexed reservationId, bytes32 transactionHash, uint64 fee, address sponsor)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostCancellationAuthorized(opts *bind.FilterOpts, reservationId [][32]byte) (*FrostBridgeFrostCancellationAuthorizedIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostCancellationAuthorized", reservationIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostCancellationAuthorizedIterator{contract: _FrostBridge.contract, event: "FrostCancellationAuthorized", logs: logs, sub: sub}, nil
}

// WatchFrostCancellationAuthorized is a free log subscription operation binding the contract event 0x17748976bed651bc2d640dec8f479c79c456b3814aeebcfd05e234291c85325b.
//
// Solidity: event FrostCancellationAuthorized(bytes32 indexed reservationId, bytes32 transactionHash, uint64 fee, address sponsor)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostCancellationAuthorized(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostCancellationAuthorized, reservationId [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostCancellationAuthorized", reservationIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostCancellationAuthorized)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostCancellationAuthorized", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostCancellationAuthorized is a log parse operation binding the contract event 0x17748976bed651bc2d640dec8f479c79c456b3814aeebcfd05e234291c85325b.
//
// Solidity: event FrostCancellationAuthorized(bytes32 indexed reservationId, bytes32 transactionHash, uint64 fee, address sponsor)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostCancellationAuthorized(log types.Log) (*FrostBridgeFrostCancellationAuthorized, error) {
	event := new(FrostBridgeFrostCancellationAuthorized)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostCancellationAuthorized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostCancellationBudgetReturnedIterator is returned from FilterFrostCancellationBudgetReturned and is used to iterate over the raw logs and unpacked data for FrostCancellationBudgetReturned events raised by the FrostBridge contract.
type FrostBridgeFrostCancellationBudgetReturnedIterator struct {
	Event *FrostBridgeFrostCancellationBudgetReturned // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostCancellationBudgetReturnedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostCancellationBudgetReturned)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostCancellationBudgetReturned)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostCancellationBudgetReturnedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostCancellationBudgetReturnedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostCancellationBudgetReturned represents a FrostCancellationBudgetReturned event raised by the FrostBridge contract.
type FrostBridgeFrostCancellationBudgetReturned struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Sponsor         common.Address
	Fee             uint64
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostCancellationBudgetReturned is a free log retrieval operation binding the contract event 0x2c18f5ab7340ad183b6c6ea94570de661ffbb0e913865bb68ee53438dafbc993.
//
// Solidity: event FrostCancellationBudgetReturned(bytes32 indexed reservationId, bytes32 indexed transactionHash, address sponsor, uint64 fee)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostCancellationBudgetReturned(opts *bind.FilterOpts, reservationId [][32]byte, transactionHash [][32]byte) (*FrostBridgeFrostCancellationBudgetReturnedIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostCancellationBudgetReturned", reservationIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostCancellationBudgetReturnedIterator{contract: _FrostBridge.contract, event: "FrostCancellationBudgetReturned", logs: logs, sub: sub}, nil
}

// WatchFrostCancellationBudgetReturned is a free log subscription operation binding the contract event 0x2c18f5ab7340ad183b6c6ea94570de661ffbb0e913865bb68ee53438dafbc993.
//
// Solidity: event FrostCancellationBudgetReturned(bytes32 indexed reservationId, bytes32 indexed transactionHash, address sponsor, uint64 fee)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostCancellationBudgetReturned(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostCancellationBudgetReturned, reservationId [][32]byte, transactionHash [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostCancellationBudgetReturned", reservationIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostCancellationBudgetReturned)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostCancellationBudgetReturned", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostCancellationBudgetReturned is a log parse operation binding the contract event 0x2c18f5ab7340ad183b6c6ea94570de661ffbb0e913865bb68ee53438dafbc993.
//
// Solidity: event FrostCancellationBudgetReturned(bytes32 indexed reservationId, bytes32 indexed transactionHash, address sponsor, uint64 fee)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostCancellationBudgetReturned(log types.Log) (*FrostBridgeFrostCancellationBudgetReturned, error) {
	event := new(FrostBridgeFrostCancellationBudgetReturned)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostCancellationBudgetReturned", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostCancellationConfirmedIterator is returned from FilterFrostCancellationConfirmed and is used to iterate over the raw logs and unpacked data for FrostCancellationConfirmed events raised by the FrostBridge contract.
type FrostBridgeFrostCancellationConfirmedIterator struct {
	Event *FrostBridgeFrostCancellationConfirmed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostCancellationConfirmedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostCancellationConfirmed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostCancellationConfirmed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostCancellationConfirmedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostCancellationConfirmedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostCancellationConfirmed represents a FrostCancellationConfirmed event raised by the FrostBridge contract.
type FrostBridgeFrostCancellationConfirmed struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostCancellationConfirmed is a free log retrieval operation binding the contract event 0xae09b0295c230391dc63faad67abc2d470c31e8366c5d7df07b3452e45347157.
//
// Solidity: event FrostCancellationConfirmed(bytes32 indexed reservationId, bytes32 transactionHash)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostCancellationConfirmed(opts *bind.FilterOpts, reservationId [][32]byte) (*FrostBridgeFrostCancellationConfirmedIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostCancellationConfirmed", reservationIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostCancellationConfirmedIterator{contract: _FrostBridge.contract, event: "FrostCancellationConfirmed", logs: logs, sub: sub}, nil
}

// WatchFrostCancellationConfirmed is a free log subscription operation binding the contract event 0xae09b0295c230391dc63faad67abc2d470c31e8366c5d7df07b3452e45347157.
//
// Solidity: event FrostCancellationConfirmed(bytes32 indexed reservationId, bytes32 transactionHash)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostCancellationConfirmed(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostCancellationConfirmed, reservationId [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostCancellationConfirmed", reservationIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostCancellationConfirmed)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostCancellationConfirmed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostCancellationConfirmed is a log parse operation binding the contract event 0xae09b0295c230391dc63faad67abc2d470c31e8366c5d7df07b3452e45347157.
//
// Solidity: event FrostCancellationConfirmed(bytes32 indexed reservationId, bytes32 transactionHash)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostCancellationConfirmed(log types.Log) (*FrostBridgeFrostCancellationConfirmed, error) {
	event := new(FrostBridgeFrostCancellationConfirmed)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostCancellationConfirmed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostDepositRevealedIterator is returned from FilterFrostDepositRevealed and is used to iterate over the raw logs and unpacked data for FrostDepositRevealed events raised by the FrostBridge contract.
type FrostBridgeFrostDepositRevealedIterator struct {
	Event *FrostBridgeFrostDepositRevealed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostDepositRevealedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostDepositRevealed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostDepositRevealed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostDepositRevealedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostDepositRevealedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostDepositRevealed represents a FrostDepositRevealed event raised by the FrostBridge contract.
type FrostBridgeFrostDepositRevealed struct {
	Outpoint  [32]byte
	WalletId  [32]byte
	Depositor common.Address
	Receipt   [32]byte
	Amount    uint64
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterFrostDepositRevealed is a free log retrieval operation binding the contract event 0x6acb856088ae879d3db2ee2441dc71614680f1c1cc3441fd7cc08fca214ac146.
//
// Solidity: event FrostDepositRevealed(bytes32 indexed outpoint, bytes32 indexed walletId, address indexed depositor, bytes32 receipt, uint64 amount)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostDepositRevealed(opts *bind.FilterOpts, outpoint [][32]byte, walletId [][32]byte, depositor []common.Address) (*FrostBridgeFrostDepositRevealedIterator, error) {

	var outpointRule []interface{}
	for _, outpointItem := range outpoint {
		outpointRule = append(outpointRule, outpointItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostDepositRevealed", outpointRule, walletIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostDepositRevealedIterator{contract: _FrostBridge.contract, event: "FrostDepositRevealed", logs: logs, sub: sub}, nil
}

// WatchFrostDepositRevealed is a free log subscription operation binding the contract event 0x6acb856088ae879d3db2ee2441dc71614680f1c1cc3441fd7cc08fca214ac146.
//
// Solidity: event FrostDepositRevealed(bytes32 indexed outpoint, bytes32 indexed walletId, address indexed depositor, bytes32 receipt, uint64 amount)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostDepositRevealed(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostDepositRevealed, outpoint [][32]byte, walletId [][32]byte, depositor []common.Address) (event.Subscription, error) {

	var outpointRule []interface{}
	for _, outpointItem := range outpoint {
		outpointRule = append(outpointRule, outpointItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostDepositRevealed", outpointRule, walletIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostDepositRevealed)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostDepositRevealed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostDepositRevealed is a log parse operation binding the contract event 0x6acb856088ae879d3db2ee2441dc71614680f1c1cc3441fd7cc08fca214ac146.
//
// Solidity: event FrostDepositRevealed(bytes32 indexed outpoint, bytes32 indexed walletId, address indexed depositor, bytes32 receipt, uint64 amount)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostDepositRevealed(log types.Log) (*FrostBridgeFrostDepositRevealed, error) {
	event := new(FrostBridgeFrostDepositRevealed)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostDepositRevealed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostDepositsSweptIterator is returned from FilterFrostDepositsSwept and is used to iterate over the raw logs and unpacked data for FrostDepositsSwept events raised by the FrostBridge contract.
type FrostBridgeFrostDepositsSweptIterator struct {
	Event *FrostBridgeFrostDepositsSwept // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostDepositsSweptIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostDepositsSwept)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostDepositsSwept)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostDepositsSweptIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostDepositsSweptIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostDepositsSwept represents a FrostDepositsSwept event raised by the FrostBridge contract.
type FrostBridgeFrostDepositsSwept struct {
	WalletId        [32]byte
	TransactionHash [32]byte
	OutputValue     uint64
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostDepositsSwept is a free log retrieval operation binding the contract event 0x8339916f0a63a4b585c920de38fcee5c8d4d9913a597bc2dd02fef90bd5146ad.
//
// Solidity: event FrostDepositsSwept(bytes32 indexed walletId, bytes32 indexed transactionHash, uint64 outputValue)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostDepositsSwept(opts *bind.FilterOpts, walletId [][32]byte, transactionHash [][32]byte) (*FrostBridgeFrostDepositsSweptIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostDepositsSwept", walletIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostDepositsSweptIterator{contract: _FrostBridge.contract, event: "FrostDepositsSwept", logs: logs, sub: sub}, nil
}

// WatchFrostDepositsSwept is a free log subscription operation binding the contract event 0x8339916f0a63a4b585c920de38fcee5c8d4d9913a597bc2dd02fef90bd5146ad.
//
// Solidity: event FrostDepositsSwept(bytes32 indexed walletId, bytes32 indexed transactionHash, uint64 outputValue)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostDepositsSwept(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostDepositsSwept, walletId [][32]byte, transactionHash [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostDepositsSwept", walletIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostDepositsSwept)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostDepositsSwept", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostDepositsSwept is a log parse operation binding the contract event 0x8339916f0a63a4b585c920de38fcee5c8d4d9913a597bc2dd02fef90bd5146ad.
//
// Solidity: event FrostDepositsSwept(bytes32 indexed walletId, bytes32 indexed transactionHash, uint64 outputValue)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostDepositsSwept(log types.Log) (*FrostBridgeFrostDepositsSwept, error) {
	event := new(FrostBridgeFrostDepositsSwept)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostDepositsSwept", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostDrainEnabledIterator is returned from FilterFrostDrainEnabled and is used to iterate over the raw logs and unpacked data for FrostDrainEnabled events raised by the FrostBridge contract.
type FrostBridgeFrostDrainEnabledIterator struct {
	Event *FrostBridgeFrostDrainEnabled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostDrainEnabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostDrainEnabled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostDrainEnabled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostDrainEnabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostDrainEnabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostDrainEnabled represents a FrostDrainEnabled event raised by the FrostBridge contract.
type FrostBridgeFrostDrainEnabled struct {
	Enabled bool
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterFrostDrainEnabled is a free log retrieval operation binding the contract event 0xe4aeea147b68a682e69c19cf62c299c117d55c43de92b6bdb63772037e9d6292.
//
// Solidity: event FrostDrainEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostDrainEnabled(opts *bind.FilterOpts) (*FrostBridgeFrostDrainEnabledIterator, error) {

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostDrainEnabled")
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostDrainEnabledIterator{contract: _FrostBridge.contract, event: "FrostDrainEnabled", logs: logs, sub: sub}, nil
}

// WatchFrostDrainEnabled is a free log subscription operation binding the contract event 0xe4aeea147b68a682e69c19cf62c299c117d55c43de92b6bdb63772037e9d6292.
//
// Solidity: event FrostDrainEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostDrainEnabled(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostDrainEnabled) (event.Subscription, error) {

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostDrainEnabled")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostDrainEnabled)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostDrainEnabled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostDrainEnabled is a log parse operation binding the contract event 0xe4aeea147b68a682e69c19cf62c299c117d55c43de92b6bdb63772037e9d6292.
//
// Solidity: event FrostDrainEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostDrainEnabled(log types.Log) (*FrostBridgeFrostDrainEnabled, error) {
	event := new(FrostBridgeFrostDrainEnabled)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostDrainEnabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostDrainReleasedIterator is returned from FilterFrostDrainReleased and is used to iterate over the raw logs and unpacked data for FrostDrainReleased events raised by the FrostBridge contract.
type FrostBridgeFrostDrainReleasedIterator struct {
	Event *FrostBridgeFrostDrainReleased // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostDrainReleasedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostDrainReleased)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostDrainReleased)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostDrainReleasedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostDrainReleasedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostDrainReleased represents a FrostDrainReleased event raised by the FrostBridge contract.
type FrostBridgeFrostDrainReleased struct {
	Source [20]byte
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterFrostDrainReleased is a free log retrieval operation binding the contract event 0xbb67bfcf2932d80220d92874b4fda0ddc1c87449dc25af1a11642fc578f34a82.
//
// Solidity: event FrostDrainReleased(bytes20 indexed source)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostDrainReleased(opts *bind.FilterOpts, source [][20]byte) (*FrostBridgeFrostDrainReleasedIterator, error) {

	var sourceRule []interface{}
	for _, sourceItem := range source {
		sourceRule = append(sourceRule, sourceItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostDrainReleased", sourceRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostDrainReleasedIterator{contract: _FrostBridge.contract, event: "FrostDrainReleased", logs: logs, sub: sub}, nil
}

// WatchFrostDrainReleased is a free log subscription operation binding the contract event 0xbb67bfcf2932d80220d92874b4fda0ddc1c87449dc25af1a11642fc578f34a82.
//
// Solidity: event FrostDrainReleased(bytes20 indexed source)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostDrainReleased(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostDrainReleased, source [][20]byte) (event.Subscription, error) {

	var sourceRule []interface{}
	for _, sourceItem := range source {
		sourceRule = append(sourceRule, sourceItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostDrainReleased", sourceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostDrainReleased)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostDrainReleased", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostDrainReleased is a log parse operation binding the contract event 0xbb67bfcf2932d80220d92874b4fda0ddc1c87449dc25af1a11642fc578f34a82.
//
// Solidity: event FrostDrainReleased(bytes20 indexed source)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostDrainReleased(log types.Log) (*FrostBridgeFrostDrainReleased, error) {
	event := new(FrostBridgeFrostDrainReleased)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostDrainReleased", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostFraudChallengeDefeatTimedOutIterator is returned from FilterFrostFraudChallengeDefeatTimedOut and is used to iterate over the raw logs and unpacked data for FrostFraudChallengeDefeatTimedOut events raised by the FrostBridge contract.
type FrostBridgeFrostFraudChallengeDefeatTimedOutIterator struct {
	Event *FrostBridgeFrostFraudChallengeDefeatTimedOut // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostFraudChallengeDefeatTimedOutIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostFraudChallengeDefeatTimedOut)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostFraudChallengeDefeatTimedOut)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostFraudChallengeDefeatTimedOutIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostFraudChallengeDefeatTimedOutIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostFraudChallengeDefeatTimedOut represents a FrostFraudChallengeDefeatTimedOut event raised by the FrostBridge contract.
type FrostBridgeFrostFraudChallengeDefeatTimedOut struct {
	WalletId [32]byte
	Digest   [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostFraudChallengeDefeatTimedOut is a free log retrieval operation binding the contract event 0x96d0d97689e4aad63048204299fbd192ea9f290df0a6fd5bbaab3c57e18f4565.
//
// Solidity: event FrostFraudChallengeDefeatTimedOut(bytes32 indexed walletId, bytes32 indexed digest)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostFraudChallengeDefeatTimedOut(opts *bind.FilterOpts, walletId [][32]byte, digest [][32]byte) (*FrostBridgeFrostFraudChallengeDefeatTimedOutIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var digestRule []interface{}
	for _, digestItem := range digest {
		digestRule = append(digestRule, digestItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostFraudChallengeDefeatTimedOut", walletIdRule, digestRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostFraudChallengeDefeatTimedOutIterator{contract: _FrostBridge.contract, event: "FrostFraudChallengeDefeatTimedOut", logs: logs, sub: sub}, nil
}

// WatchFrostFraudChallengeDefeatTimedOut is a free log subscription operation binding the contract event 0x96d0d97689e4aad63048204299fbd192ea9f290df0a6fd5bbaab3c57e18f4565.
//
// Solidity: event FrostFraudChallengeDefeatTimedOut(bytes32 indexed walletId, bytes32 indexed digest)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostFraudChallengeDefeatTimedOut(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostFraudChallengeDefeatTimedOut, walletId [][32]byte, digest [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var digestRule []interface{}
	for _, digestItem := range digest {
		digestRule = append(digestRule, digestItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostFraudChallengeDefeatTimedOut", walletIdRule, digestRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostFraudChallengeDefeatTimedOut)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostFraudChallengeDefeatTimedOut", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostFraudChallengeDefeatTimedOut is a log parse operation binding the contract event 0x96d0d97689e4aad63048204299fbd192ea9f290df0a6fd5bbaab3c57e18f4565.
//
// Solidity: event FrostFraudChallengeDefeatTimedOut(bytes32 indexed walletId, bytes32 indexed digest)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostFraudChallengeDefeatTimedOut(log types.Log) (*FrostBridgeFrostFraudChallengeDefeatTimedOut, error) {
	event := new(FrostBridgeFrostFraudChallengeDefeatTimedOut)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostFraudChallengeDefeatTimedOut", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostFraudChallengeDefeatedIterator is returned from FilterFrostFraudChallengeDefeated and is used to iterate over the raw logs and unpacked data for FrostFraudChallengeDefeated events raised by the FrostBridge contract.
type FrostBridgeFrostFraudChallengeDefeatedIterator struct {
	Event *FrostBridgeFrostFraudChallengeDefeated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostFraudChallengeDefeatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostFraudChallengeDefeated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostFraudChallengeDefeated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostFraudChallengeDefeatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostFraudChallengeDefeatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostFraudChallengeDefeated represents a FrostFraudChallengeDefeated event raised by the FrostBridge contract.
type FrostBridgeFrostFraudChallengeDefeated struct {
	WalletId [32]byte
	Digest   [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostFraudChallengeDefeated is a free log retrieval operation binding the contract event 0x7bd8debc0d4b2afed9388ce3d1ca5d7b95640a0e3838ac0095e2db19eb8adfba.
//
// Solidity: event FrostFraudChallengeDefeated(bytes32 indexed walletId, bytes32 indexed digest)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostFraudChallengeDefeated(opts *bind.FilterOpts, walletId [][32]byte, digest [][32]byte) (*FrostBridgeFrostFraudChallengeDefeatedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var digestRule []interface{}
	for _, digestItem := range digest {
		digestRule = append(digestRule, digestItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostFraudChallengeDefeated", walletIdRule, digestRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostFraudChallengeDefeatedIterator{contract: _FrostBridge.contract, event: "FrostFraudChallengeDefeated", logs: logs, sub: sub}, nil
}

// WatchFrostFraudChallengeDefeated is a free log subscription operation binding the contract event 0x7bd8debc0d4b2afed9388ce3d1ca5d7b95640a0e3838ac0095e2db19eb8adfba.
//
// Solidity: event FrostFraudChallengeDefeated(bytes32 indexed walletId, bytes32 indexed digest)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostFraudChallengeDefeated(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostFraudChallengeDefeated, walletId [][32]byte, digest [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var digestRule []interface{}
	for _, digestItem := range digest {
		digestRule = append(digestRule, digestItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostFraudChallengeDefeated", walletIdRule, digestRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostFraudChallengeDefeated)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostFraudChallengeDefeated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostFraudChallengeDefeated is a log parse operation binding the contract event 0x7bd8debc0d4b2afed9388ce3d1ca5d7b95640a0e3838ac0095e2db19eb8adfba.
//
// Solidity: event FrostFraudChallengeDefeated(bytes32 indexed walletId, bytes32 indexed digest)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostFraudChallengeDefeated(log types.Log) (*FrostBridgeFrostFraudChallengeDefeated, error) {
	event := new(FrostBridgeFrostFraudChallengeDefeated)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostFraudChallengeDefeated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostFraudChallengeSubmittedIterator is returned from FilterFrostFraudChallengeSubmitted and is used to iterate over the raw logs and unpacked data for FrostFraudChallengeSubmitted events raised by the FrostBridge contract.
type FrostBridgeFrostFraudChallengeSubmittedIterator struct {
	Event *FrostBridgeFrostFraudChallengeSubmitted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostFraudChallengeSubmittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostFraudChallengeSubmitted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostFraudChallengeSubmitted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostFraudChallengeSubmittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostFraudChallengeSubmittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostFraudChallengeSubmitted represents a FrostFraudChallengeSubmitted event raised by the FrostBridge contract.
type FrostBridgeFrostFraudChallengeSubmitted struct {
	WalletId  [32]byte
	Digest    [32]byte
	Signature []byte
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterFrostFraudChallengeSubmitted is a free log retrieval operation binding the contract event 0x2b0cc0f09346f568c6806c0e2bb83da0f1f30cf50683fa9cda6df306ec9b2a6f.
//
// Solidity: event FrostFraudChallengeSubmitted(bytes32 indexed walletId, bytes32 indexed digest, bytes signature)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostFraudChallengeSubmitted(opts *bind.FilterOpts, walletId [][32]byte, digest [][32]byte) (*FrostBridgeFrostFraudChallengeSubmittedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var digestRule []interface{}
	for _, digestItem := range digest {
		digestRule = append(digestRule, digestItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostFraudChallengeSubmitted", walletIdRule, digestRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostFraudChallengeSubmittedIterator{contract: _FrostBridge.contract, event: "FrostFraudChallengeSubmitted", logs: logs, sub: sub}, nil
}

// WatchFrostFraudChallengeSubmitted is a free log subscription operation binding the contract event 0x2b0cc0f09346f568c6806c0e2bb83da0f1f30cf50683fa9cda6df306ec9b2a6f.
//
// Solidity: event FrostFraudChallengeSubmitted(bytes32 indexed walletId, bytes32 indexed digest, bytes signature)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostFraudChallengeSubmitted(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostFraudChallengeSubmitted, walletId [][32]byte, digest [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var digestRule []interface{}
	for _, digestItem := range digest {
		digestRule = append(digestRule, digestItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostFraudChallengeSubmitted", walletIdRule, digestRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostFraudChallengeSubmitted)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostFraudChallengeSubmitted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostFraudChallengeSubmitted is a log parse operation binding the contract event 0x2b0cc0f09346f568c6806c0e2bb83da0f1f30cf50683fa9cda6df306ec9b2a6f.
//
// Solidity: event FrostFraudChallengeSubmitted(bytes32 indexed walletId, bytes32 indexed digest, bytes signature)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostFraudChallengeSubmitted(log types.Log) (*FrostBridgeFrostFraudChallengeSubmitted, error) {
	event := new(FrostBridgeFrostFraudChallengeSubmitted)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostFraudChallengeSubmitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostFundingGatesChangedIterator is returned from FilterFrostFundingGatesChanged and is used to iterate over the raw logs and unpacked data for FrostFundingGatesChanged events raised by the FrostBridge contract.
type FrostBridgeFrostFundingGatesChangedIterator struct {
	Event *FrostBridgeFrostFundingGatesChanged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostFundingGatesChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostFundingGatesChanged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostFundingGatesChanged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostFundingGatesChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostFundingGatesChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostFundingGatesChanged represents a FrostFundingGatesChanged event raised by the FrostBridge contract.
type FrostBridgeFrostFundingGatesChanged struct {
	AddressesEnabled bool
	RevealEnabled    bool
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterFrostFundingGatesChanged is a free log retrieval operation binding the contract event 0x3551b3b85e57e2e59a4ce9242edc107591de17e24d71c4c5c5f976990b990029.
//
// Solidity: event FrostFundingGatesChanged(bool addressesEnabled, bool revealEnabled)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostFundingGatesChanged(opts *bind.FilterOpts) (*FrostBridgeFrostFundingGatesChangedIterator, error) {

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostFundingGatesChanged")
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostFundingGatesChangedIterator{contract: _FrostBridge.contract, event: "FrostFundingGatesChanged", logs: logs, sub: sub}, nil
}

// WatchFrostFundingGatesChanged is a free log subscription operation binding the contract event 0x3551b3b85e57e2e59a4ce9242edc107591de17e24d71c4c5c5f976990b990029.
//
// Solidity: event FrostFundingGatesChanged(bool addressesEnabled, bool revealEnabled)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostFundingGatesChanged(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostFundingGatesChanged) (event.Subscription, error) {

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostFundingGatesChanged")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostFundingGatesChanged)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostFundingGatesChanged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostFundingGatesChanged is a log parse operation binding the contract event 0x3551b3b85e57e2e59a4ce9242edc107591de17e24d71c4c5c5f976990b990029.
//
// Solidity: event FrostFundingGatesChanged(bool addressesEnabled, bool revealEnabled)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostFundingGatesChanged(log types.Log) (*FrostBridgeFrostFundingGatesChanged, error) {
	event := new(FrostBridgeFrostFundingGatesChanged)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostFundingGatesChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostFundingInstalledIterator is returned from FilterFrostFundingInstalled and is used to iterate over the raw logs and unpacked data for FrostFundingInstalled events raised by the FrostBridge contract.
type FrostBridgeFrostFundingInstalledIterator struct {
	Event *FrostBridgeFrostFundingInstalled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostFundingInstalledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostFundingInstalled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostFundingInstalled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostFundingInstalledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostFundingInstalledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostFundingInstalled represents a FrostFundingInstalled event raised by the FrostBridge contract.
type FrostBridgeFrostFundingInstalled struct {
	Verifier            common.Address
	RuntimeHash         [32]byte
	QualificationRecord [32]byte
	BitcoinGenesis      [32]byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterFrostFundingInstalled is a free log retrieval operation binding the contract event 0x86684d41266d26a3ad43ca0387abec1ce76821e9d1809619963612a62d060d2c.
//
// Solidity: event FrostFundingInstalled(address indexed verifier, bytes32 runtimeHash, bytes32 qualificationRecord, bytes32 bitcoinGenesis)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostFundingInstalled(opts *bind.FilterOpts, verifier []common.Address) (*FrostBridgeFrostFundingInstalledIterator, error) {

	var verifierRule []interface{}
	for _, verifierItem := range verifier {
		verifierRule = append(verifierRule, verifierItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostFundingInstalled", verifierRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostFundingInstalledIterator{contract: _FrostBridge.contract, event: "FrostFundingInstalled", logs: logs, sub: sub}, nil
}

// WatchFrostFundingInstalled is a free log subscription operation binding the contract event 0x86684d41266d26a3ad43ca0387abec1ce76821e9d1809619963612a62d060d2c.
//
// Solidity: event FrostFundingInstalled(address indexed verifier, bytes32 runtimeHash, bytes32 qualificationRecord, bytes32 bitcoinGenesis)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostFundingInstalled(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostFundingInstalled, verifier []common.Address) (event.Subscription, error) {

	var verifierRule []interface{}
	for _, verifierItem := range verifier {
		verifierRule = append(verifierRule, verifierItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostFundingInstalled", verifierRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostFundingInstalled)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostFundingInstalled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostFundingInstalled is a log parse operation binding the contract event 0x86684d41266d26a3ad43ca0387abec1ce76821e9d1809619963612a62d060d2c.
//
// Solidity: event FrostFundingInstalled(address indexed verifier, bytes32 runtimeHash, bytes32 qualificationRecord, bytes32 bitcoinGenesis)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostFundingInstalled(log types.Log) (*FrostBridgeFrostFundingInstalled, error) {
	event := new(FrostBridgeFrostFundingInstalled)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostFundingInstalled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostIncomingEscalatedIterator is returned from FilterFrostIncomingEscalated and is used to iterate over the raw logs and unpacked data for FrostIncomingEscalated events raised by the FrostBridge contract.
type FrostBridgeFrostIncomingEscalatedIterator struct {
	Event *FrostBridgeFrostIncomingEscalated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostIncomingEscalatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostIncomingEscalated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostIncomingEscalated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostIncomingEscalatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostIncomingEscalatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostIncomingEscalated represents a FrostIncomingEscalated event raised by the FrostBridge contract.
type FrostBridgeFrostIncomingEscalated struct {
	Outpoint [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostIncomingEscalated is a free log retrieval operation binding the contract event 0xcea3b7601007d92f42ae747009f3f30c1863d61c3db91680c4ecd29a065d21a0.
//
// Solidity: event FrostIncomingEscalated(bytes32 indexed outpoint)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostIncomingEscalated(opts *bind.FilterOpts, outpoint [][32]byte) (*FrostBridgeFrostIncomingEscalatedIterator, error) {

	var outpointRule []interface{}
	for _, outpointItem := range outpoint {
		outpointRule = append(outpointRule, outpointItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostIncomingEscalated", outpointRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostIncomingEscalatedIterator{contract: _FrostBridge.contract, event: "FrostIncomingEscalated", logs: logs, sub: sub}, nil
}

// WatchFrostIncomingEscalated is a free log subscription operation binding the contract event 0xcea3b7601007d92f42ae747009f3f30c1863d61c3db91680c4ecd29a065d21a0.
//
// Solidity: event FrostIncomingEscalated(bytes32 indexed outpoint)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostIncomingEscalated(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostIncomingEscalated, outpoint [][32]byte) (event.Subscription, error) {

	var outpointRule []interface{}
	for _, outpointItem := range outpoint {
		outpointRule = append(outpointRule, outpointItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostIncomingEscalated", outpointRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostIncomingEscalated)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostIncomingEscalated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostIncomingEscalated is a log parse operation binding the contract event 0xcea3b7601007d92f42ae747009f3f30c1863d61c3db91680c4ecd29a065d21a0.
//
// Solidity: event FrostIncomingEscalated(bytes32 indexed outpoint)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostIncomingEscalated(log types.Log) (*FrostBridgeFrostIncomingEscalated, error) {
	event := new(FrostBridgeFrostIncomingEscalated)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostIncomingEscalated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostIncomingRecordedIterator is returned from FilterFrostIncomingRecorded and is used to iterate over the raw logs and unpacked data for FrostIncomingRecorded events raised by the FrostBridge contract.
type FrostBridgeFrostIncomingRecordedIterator struct {
	Event *FrostBridgeFrostIncomingRecorded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostIncomingRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostIncomingRecorded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostIncomingRecorded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostIncomingRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostIncomingRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostIncomingRecorded represents a FrostIncomingRecorded event raised by the FrostBridge contract.
type FrostBridgeFrostIncomingRecorded struct {
	Outpoint        [32]byte
	Target          [32]byte
	Source          [32]byte
	Value           uint64
	TransactionHash [32]byte
	OutputIndex     uint32
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostIncomingRecorded is a free log retrieval operation binding the contract event 0x27dbe9d25b86175aafa916f88a2304f5f73c26efb90ecb93e6c5c48e790beae9.
//
// Solidity: event FrostIncomingRecorded(bytes32 indexed outpoint, bytes32 indexed target, bytes32 source, uint64 value, bytes32 transactionHash, uint32 outputIndex)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostIncomingRecorded(opts *bind.FilterOpts, outpoint [][32]byte, target [][32]byte) (*FrostBridgeFrostIncomingRecordedIterator, error) {

	var outpointRule []interface{}
	for _, outpointItem := range outpoint {
		outpointRule = append(outpointRule, outpointItem)
	}
	var targetRule []interface{}
	for _, targetItem := range target {
		targetRule = append(targetRule, targetItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostIncomingRecorded", outpointRule, targetRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostIncomingRecordedIterator{contract: _FrostBridge.contract, event: "FrostIncomingRecorded", logs: logs, sub: sub}, nil
}

// WatchFrostIncomingRecorded is a free log subscription operation binding the contract event 0x27dbe9d25b86175aafa916f88a2304f5f73c26efb90ecb93e6c5c48e790beae9.
//
// Solidity: event FrostIncomingRecorded(bytes32 indexed outpoint, bytes32 indexed target, bytes32 source, uint64 value, bytes32 transactionHash, uint32 outputIndex)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostIncomingRecorded(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostIncomingRecorded, outpoint [][32]byte, target [][32]byte) (event.Subscription, error) {

	var outpointRule []interface{}
	for _, outpointItem := range outpoint {
		outpointRule = append(outpointRule, outpointItem)
	}
	var targetRule []interface{}
	for _, targetItem := range target {
		targetRule = append(targetRule, targetItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostIncomingRecorded", outpointRule, targetRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostIncomingRecorded)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostIncomingRecorded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostIncomingRecorded is a log parse operation binding the contract event 0x27dbe9d25b86175aafa916f88a2304f5f73c26efb90ecb93e6c5c48e790beae9.
//
// Solidity: event FrostIncomingRecorded(bytes32 indexed outpoint, bytes32 indexed target, bytes32 source, uint64 value, bytes32 transactionHash, uint32 outputIndex)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostIncomingRecorded(log types.Log) (*FrostBridgeFrostIncomingRecorded, error) {
	event := new(FrostBridgeFrostIncomingRecorded)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostIncomingRecorded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovedFundsSweptIterator is returned from FilterFrostMovedFundsSwept and is used to iterate over the raw logs and unpacked data for FrostMovedFundsSwept events raised by the FrostBridge contract.
type FrostBridgeFrostMovedFundsSweptIterator struct {
	Event *FrostBridgeFrostMovedFundsSwept // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovedFundsSweptIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovedFundsSwept)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovedFundsSwept)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovedFundsSweptIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovedFundsSweptIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovedFundsSwept represents a FrostMovedFundsSwept event raised by the FrostBridge contract.
type FrostBridgeFrostMovedFundsSwept struct {
	WalletId        [32]byte
	TransactionHash [32]byte
	Value           uint64
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostMovedFundsSwept is a free log retrieval operation binding the contract event 0xeaa2d1db37fb489b747d0c2567694ff5d870156c2c178f39551f5339d19211c5.
//
// Solidity: event FrostMovedFundsSwept(bytes32 indexed walletId, bytes32 indexed transactionHash, uint64 value)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovedFundsSwept(opts *bind.FilterOpts, walletId [][32]byte, transactionHash [][32]byte) (*FrostBridgeFrostMovedFundsSweptIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovedFundsSwept", walletIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovedFundsSweptIterator{contract: _FrostBridge.contract, event: "FrostMovedFundsSwept", logs: logs, sub: sub}, nil
}

// WatchFrostMovedFundsSwept is a free log subscription operation binding the contract event 0xeaa2d1db37fb489b747d0c2567694ff5d870156c2c178f39551f5339d19211c5.
//
// Solidity: event FrostMovedFundsSwept(bytes32 indexed walletId, bytes32 indexed transactionHash, uint64 value)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovedFundsSwept(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovedFundsSwept, walletId [][32]byte, transactionHash [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovedFundsSwept", walletIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovedFundsSwept)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovedFundsSwept", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovedFundsSwept is a log parse operation binding the contract event 0xeaa2d1db37fb489b747d0c2567694ff5d870156c2c178f39551f5339d19211c5.
//
// Solidity: event FrostMovedFundsSwept(bytes32 indexed walletId, bytes32 indexed transactionHash, uint64 value)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovedFundsSwept(log types.Log) (*FrostBridgeFrostMovedFundsSwept, error) {
	event := new(FrostBridgeFrostMovedFundsSwept)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovedFundsSwept", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovingFundsBelowDustIterator is returned from FilterFrostMovingFundsBelowDust and is used to iterate over the raw logs and unpacked data for FrostMovingFundsBelowDust events raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsBelowDustIterator struct {
	Event *FrostBridgeFrostMovingFundsBelowDust // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovingFundsBelowDustIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovingFundsBelowDust)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovingFundsBelowDust)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovingFundsBelowDustIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovingFundsBelowDustIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovingFundsBelowDust represents a FrostMovingFundsBelowDust event raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsBelowDust struct {
	WalletId [32]byte
	Value    uint64
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostMovingFundsBelowDust is a free log retrieval operation binding the contract event 0x5a2e36acfc000c0a8a56db7a5549be66d7af14fbf7e09a259142e896f20af1f7.
//
// Solidity: event FrostMovingFundsBelowDust(bytes32 indexed walletId, uint64 value)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovingFundsBelowDust(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostMovingFundsBelowDustIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovingFundsBelowDust", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovingFundsBelowDustIterator{contract: _FrostBridge.contract, event: "FrostMovingFundsBelowDust", logs: logs, sub: sub}, nil
}

// WatchFrostMovingFundsBelowDust is a free log subscription operation binding the contract event 0x5a2e36acfc000c0a8a56db7a5549be66d7af14fbf7e09a259142e896f20af1f7.
//
// Solidity: event FrostMovingFundsBelowDust(bytes32 indexed walletId, uint64 value)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovingFundsBelowDust(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovingFundsBelowDust, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovingFundsBelowDust", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovingFundsBelowDust)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsBelowDust", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovingFundsBelowDust is a log parse operation binding the contract event 0x5a2e36acfc000c0a8a56db7a5549be66d7af14fbf7e09a259142e896f20af1f7.
//
// Solidity: event FrostMovingFundsBelowDust(bytes32 indexed walletId, uint64 value)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovingFundsBelowDust(log types.Log) (*FrostBridgeFrostMovingFundsBelowDust, error) {
	event := new(FrostBridgeFrostMovingFundsBelowDust)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsBelowDust", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovingFundsCommitmentRetiredIterator is returned from FilterFrostMovingFundsCommitmentRetired and is used to iterate over the raw logs and unpacked data for FrostMovingFundsCommitmentRetired events raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsCommitmentRetiredIterator struct {
	Event *FrostBridgeFrostMovingFundsCommitmentRetired // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovingFundsCommitmentRetiredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovingFundsCommitmentRetired)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovingFundsCommitmentRetired)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovingFundsCommitmentRetiredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovingFundsCommitmentRetiredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovingFundsCommitmentRetired represents a FrostMovingFundsCommitmentRetired event raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsCommitmentRetired struct {
	WalletId [32]byte
	Period   uint32
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostMovingFundsCommitmentRetired is a free log retrieval operation binding the contract event 0xd33796ef0b07d17b8e92603a76d4f25c9b3831fefc987e2449087094b4ea1160.
//
// Solidity: event FrostMovingFundsCommitmentRetired(bytes32 indexed walletId, uint32 period)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovingFundsCommitmentRetired(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostMovingFundsCommitmentRetiredIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovingFundsCommitmentRetired", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovingFundsCommitmentRetiredIterator{contract: _FrostBridge.contract, event: "FrostMovingFundsCommitmentRetired", logs: logs, sub: sub}, nil
}

// WatchFrostMovingFundsCommitmentRetired is a free log subscription operation binding the contract event 0xd33796ef0b07d17b8e92603a76d4f25c9b3831fefc987e2449087094b4ea1160.
//
// Solidity: event FrostMovingFundsCommitmentRetired(bytes32 indexed walletId, uint32 period)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovingFundsCommitmentRetired(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovingFundsCommitmentRetired, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovingFundsCommitmentRetired", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovingFundsCommitmentRetired)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsCommitmentRetired", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovingFundsCommitmentRetired is a log parse operation binding the contract event 0xd33796ef0b07d17b8e92603a76d4f25c9b3831fefc987e2449087094b4ea1160.
//
// Solidity: event FrostMovingFundsCommitmentRetired(bytes32 indexed walletId, uint32 period)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovingFundsCommitmentRetired(log types.Log) (*FrostBridgeFrostMovingFundsCommitmentRetired, error) {
	event := new(FrostBridgeFrostMovingFundsCommitmentRetired)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsCommitmentRetired", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovingFundsCommittedIterator is returned from FilterFrostMovingFundsCommitted and is used to iterate over the raw logs and unpacked data for FrostMovingFundsCommitted events raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsCommittedIterator struct {
	Event *FrostBridgeFrostMovingFundsCommitted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovingFundsCommittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovingFundsCommitted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovingFundsCommitted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovingFundsCommittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovingFundsCommittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovingFundsCommitted represents a FrostMovingFundsCommitted event raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsCommitted struct {
	WalletId         [32]byte
	Period           uint32
	MainOutpoint     [32]byte
	Targets          [][20]byte
	DescriptorHashes [][32]byte
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterFrostMovingFundsCommitted is a free log retrieval operation binding the contract event 0x5777612a2fee97c09b894303d0f922a6905cd36a3adaed1f4c49331128c3bf75.
//
// Solidity: event FrostMovingFundsCommitted(bytes32 indexed walletId, uint32 period, bytes32 mainOutpoint, bytes20[] targets, bytes32[] descriptorHashes)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovingFundsCommitted(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostMovingFundsCommittedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovingFundsCommitted", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovingFundsCommittedIterator{contract: _FrostBridge.contract, event: "FrostMovingFundsCommitted", logs: logs, sub: sub}, nil
}

// WatchFrostMovingFundsCommitted is a free log subscription operation binding the contract event 0x5777612a2fee97c09b894303d0f922a6905cd36a3adaed1f4c49331128c3bf75.
//
// Solidity: event FrostMovingFundsCommitted(bytes32 indexed walletId, uint32 period, bytes32 mainOutpoint, bytes20[] targets, bytes32[] descriptorHashes)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovingFundsCommitted(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovingFundsCommitted, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovingFundsCommitted", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovingFundsCommitted)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsCommitted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovingFundsCommitted is a log parse operation binding the contract event 0x5777612a2fee97c09b894303d0f922a6905cd36a3adaed1f4c49331128c3bf75.
//
// Solidity: event FrostMovingFundsCommitted(bytes32 indexed walletId, uint32 period, bytes32 mainOutpoint, bytes20[] targets, bytes32[] descriptorHashes)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovingFundsCommitted(log types.Log) (*FrostBridgeFrostMovingFundsCommitted, error) {
	event := new(FrostBridgeFrostMovingFundsCommitted)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsCommitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovingFundsCompletedIterator is returned from FilterFrostMovingFundsCompleted and is used to iterate over the raw logs and unpacked data for FrostMovingFundsCompleted events raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsCompletedIterator struct {
	Event *FrostBridgeFrostMovingFundsCompleted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovingFundsCompletedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovingFundsCompleted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovingFundsCompleted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovingFundsCompletedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovingFundsCompletedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovingFundsCompleted represents a FrostMovingFundsCompleted event raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsCompleted struct {
	WalletId        [32]byte
	TransactionHash [32]byte
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostMovingFundsCompleted is a free log retrieval operation binding the contract event 0x3e612c0e52fd7d83e55bfd9974dcd33719c280033dbf90496ce16b71ef0c81fe.
//
// Solidity: event FrostMovingFundsCompleted(bytes32 indexed walletId, bytes32 indexed transactionHash)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovingFundsCompleted(opts *bind.FilterOpts, walletId [][32]byte, transactionHash [][32]byte) (*FrostBridgeFrostMovingFundsCompletedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovingFundsCompleted", walletIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovingFundsCompletedIterator{contract: _FrostBridge.contract, event: "FrostMovingFundsCompleted", logs: logs, sub: sub}, nil
}

// WatchFrostMovingFundsCompleted is a free log subscription operation binding the contract event 0x3e612c0e52fd7d83e55bfd9974dcd33719c280033dbf90496ce16b71ef0c81fe.
//
// Solidity: event FrostMovingFundsCompleted(bytes32 indexed walletId, bytes32 indexed transactionHash)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovingFundsCompleted(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovingFundsCompleted, walletId [][32]byte, transactionHash [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovingFundsCompleted", walletIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovingFundsCompleted)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsCompleted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovingFundsCompleted is a log parse operation binding the contract event 0x3e612c0e52fd7d83e55bfd9974dcd33719c280033dbf90496ce16b71ef0c81fe.
//
// Solidity: event FrostMovingFundsCompleted(bytes32 indexed walletId, bytes32 indexed transactionHash)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovingFundsCompleted(log types.Log) (*FrostBridgeFrostMovingFundsCompleted, error) {
	event := new(FrostBridgeFrostMovingFundsCompleted)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsCompleted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovingFundsEscalatedIterator is returned from FilterFrostMovingFundsEscalated and is used to iterate over the raw logs and unpacked data for FrostMovingFundsEscalated events raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsEscalatedIterator struct {
	Event *FrostBridgeFrostMovingFundsEscalated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovingFundsEscalatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovingFundsEscalated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovingFundsEscalated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovingFundsEscalatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovingFundsEscalatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovingFundsEscalated represents a FrostMovingFundsEscalated event raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsEscalated struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostMovingFundsEscalated is a free log retrieval operation binding the contract event 0x43d8cabacb62305131df32e709f562d6f13ec8be1a6daab14af59dac6e77ca90.
//
// Solidity: event FrostMovingFundsEscalated(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovingFundsEscalated(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostMovingFundsEscalatedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovingFundsEscalated", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovingFundsEscalatedIterator{contract: _FrostBridge.contract, event: "FrostMovingFundsEscalated", logs: logs, sub: sub}, nil
}

// WatchFrostMovingFundsEscalated is a free log subscription operation binding the contract event 0x43d8cabacb62305131df32e709f562d6f13ec8be1a6daab14af59dac6e77ca90.
//
// Solidity: event FrostMovingFundsEscalated(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovingFundsEscalated(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovingFundsEscalated, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovingFundsEscalated", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovingFundsEscalated)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsEscalated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovingFundsEscalated is a log parse operation binding the contract event 0x43d8cabacb62305131df32e709f562d6f13ec8be1a6daab14af59dac6e77ca90.
//
// Solidity: event FrostMovingFundsEscalated(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovingFundsEscalated(log types.Log) (*FrostBridgeFrostMovingFundsEscalated, error) {
	event := new(FrostBridgeFrostMovingFundsEscalated)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsEscalated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostMovingFundsTimeoutResetIterator is returned from FilterFrostMovingFundsTimeoutReset and is used to iterate over the raw logs and unpacked data for FrostMovingFundsTimeoutReset events raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsTimeoutResetIterator struct {
	Event *FrostBridgeFrostMovingFundsTimeoutReset // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostMovingFundsTimeoutResetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostMovingFundsTimeoutReset)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostMovingFundsTimeoutReset)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostMovingFundsTimeoutResetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostMovingFundsTimeoutResetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostMovingFundsTimeoutReset represents a FrostMovingFundsTimeoutReset event raised by the FrostBridge contract.
type FrostBridgeFrostMovingFundsTimeoutReset struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostMovingFundsTimeoutReset is a free log retrieval operation binding the contract event 0xb9e1d426c3de944c34ee8b4278688fda9fb542ba4f81ba6d028b92188ee5880a.
//
// Solidity: event FrostMovingFundsTimeoutReset(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostMovingFundsTimeoutReset(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostMovingFundsTimeoutResetIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostMovingFundsTimeoutReset", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostMovingFundsTimeoutResetIterator{contract: _FrostBridge.contract, event: "FrostMovingFundsTimeoutReset", logs: logs, sub: sub}, nil
}

// WatchFrostMovingFundsTimeoutReset is a free log subscription operation binding the contract event 0xb9e1d426c3de944c34ee8b4278688fda9fb542ba4f81ba6d028b92188ee5880a.
//
// Solidity: event FrostMovingFundsTimeoutReset(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostMovingFundsTimeoutReset(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostMovingFundsTimeoutReset, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostMovingFundsTimeoutReset", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostMovingFundsTimeoutReset)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsTimeoutReset", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostMovingFundsTimeoutReset is a log parse operation binding the contract event 0xb9e1d426c3de944c34ee8b4278688fda9fb542ba4f81ba6d028b92188ee5880a.
//
// Solidity: event FrostMovingFundsTimeoutReset(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostMovingFundsTimeoutReset(log types.Log) (*FrostBridgeFrostMovingFundsTimeoutReset, error) {
	event := new(FrostBridgeFrostMovingFundsTimeoutReset)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostMovingFundsTimeoutReset", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostOperatorInactivityNotifiedIterator is returned from FilterFrostOperatorInactivityNotified and is used to iterate over the raw logs and unpacked data for FrostOperatorInactivityNotified events raised by the FrostBridge contract.
type FrostBridgeFrostOperatorInactivityNotifiedIterator struct {
	Event *FrostBridgeFrostOperatorInactivityNotified // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostOperatorInactivityNotifiedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostOperatorInactivityNotified)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostOperatorInactivityNotified)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostOperatorInactivityNotifiedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostOperatorInactivityNotifiedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostOperatorInactivityNotified represents a FrostOperatorInactivityNotified event raised by the FrostBridge contract.
type FrostBridgeFrostOperatorInactivityNotified struct {
	WalletId          [32]byte
	Nonce             uint64
	InactiveSeats     []uint16
	InactiveOperators []common.Address
	HeartbeatFailed   bool
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterFrostOperatorInactivityNotified is a free log retrieval operation binding the contract event 0xa7fbf4ddea49d28f1473ec78b702a67dfbfd3592fda98a0e01b0c6c28768810f.
//
// Solidity: event FrostOperatorInactivityNotified(bytes32 indexed walletId, uint64 nonce, uint16[] inactiveSeats, address[] inactiveOperators, bool heartbeatFailed)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostOperatorInactivityNotified(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostOperatorInactivityNotifiedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostOperatorInactivityNotified", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostOperatorInactivityNotifiedIterator{contract: _FrostBridge.contract, event: "FrostOperatorInactivityNotified", logs: logs, sub: sub}, nil
}

// WatchFrostOperatorInactivityNotified is a free log subscription operation binding the contract event 0xa7fbf4ddea49d28f1473ec78b702a67dfbfd3592fda98a0e01b0c6c28768810f.
//
// Solidity: event FrostOperatorInactivityNotified(bytes32 indexed walletId, uint64 nonce, uint16[] inactiveSeats, address[] inactiveOperators, bool heartbeatFailed)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostOperatorInactivityNotified(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostOperatorInactivityNotified, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostOperatorInactivityNotified", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostOperatorInactivityNotified)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostOperatorInactivityNotified", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostOperatorInactivityNotified is a log parse operation binding the contract event 0xa7fbf4ddea49d28f1473ec78b702a67dfbfd3592fda98a0e01b0c6c28768810f.
//
// Solidity: event FrostOperatorInactivityNotified(bytes32 indexed walletId, uint64 nonce, uint16[] inactiveSeats, address[] inactiveOperators, bool heartbeatFailed)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostOperatorInactivityNotified(log types.Log) (*FrostBridgeFrostOperatorInactivityNotified, error) {
	event := new(FrostBridgeFrostOperatorInactivityNotified)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostOperatorInactivityNotified", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionPaidIterator is returned from FilterFrostRedemptionPaid and is used to iterate over the raw logs and unpacked data for FrostRedemptionPaid events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionPaidIterator struct {
	Event *FrostBridgeFrostRedemptionPaid // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionPaidIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionPaid)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionPaid)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionPaidIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionPaidIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionPaid represents a FrostRedemptionPaid event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionPaid struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionPaid is a free log retrieval operation binding the contract event 0x30da16c8beac11bc28e89608a8a7f35a637475d7ca87c7a81e65096ac4ff073d.
//
// Solidity: event FrostRedemptionPaid(bytes32 indexed reservationId, bytes32 transactionHash)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionPaid(opts *bind.FilterOpts, reservationId [][32]byte) (*FrostBridgeFrostRedemptionPaidIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionPaid", reservationIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionPaidIterator{contract: _FrostBridge.contract, event: "FrostRedemptionPaid", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionPaid is a free log subscription operation binding the contract event 0x30da16c8beac11bc28e89608a8a7f35a637475d7ca87c7a81e65096ac4ff073d.
//
// Solidity: event FrostRedemptionPaid(bytes32 indexed reservationId, bytes32 transactionHash)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionPaid(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionPaid, reservationId [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionPaid", reservationIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionPaid)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionPaid", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionPaid is a log parse operation binding the contract event 0x30da16c8beac11bc28e89608a8a7f35a637475d7ca87c7a81e65096ac4ff073d.
//
// Solidity: event FrostRedemptionPaid(bytes32 indexed reservationId, bytes32 transactionHash)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionPaid(log types.Log) (*FrostBridgeFrostRedemptionPaid, error) {
	event := new(FrostBridgeFrostRedemptionPaid)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionPaid", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionRefundAvailableIterator is returned from FilterFrostRedemptionRefundAvailable and is used to iterate over the raw logs and unpacked data for FrostRedemptionRefundAvailable events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionRefundAvailableIterator struct {
	Event *FrostBridgeFrostRedemptionRefundAvailable // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionRefundAvailableIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionRefundAvailable)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionRefundAvailable)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionRefundAvailableIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionRefundAvailableIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionRefundAvailable represents a FrostRedemptionRefundAvailable event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionRefundAvailable struct {
	RequestId [32]byte
	Redeemer  common.Address
	Gross     uint64
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionRefundAvailable is a free log retrieval operation binding the contract event 0x63d288c09739f401752f5fd7550ff8144d1871b1b33c29cb2e882510cfa860fd.
//
// Solidity: event FrostRedemptionRefundAvailable(bytes32 indexed requestId, address redeemer, uint64 gross)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionRefundAvailable(opts *bind.FilterOpts, requestId [][32]byte) (*FrostBridgeFrostRedemptionRefundAvailableIterator, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionRefundAvailable", requestIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionRefundAvailableIterator{contract: _FrostBridge.contract, event: "FrostRedemptionRefundAvailable", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionRefundAvailable is a free log subscription operation binding the contract event 0x63d288c09739f401752f5fd7550ff8144d1871b1b33c29cb2e882510cfa860fd.
//
// Solidity: event FrostRedemptionRefundAvailable(bytes32 indexed requestId, address redeemer, uint64 gross)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionRefundAvailable(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionRefundAvailable, requestId [][32]byte) (event.Subscription, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionRefundAvailable", requestIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionRefundAvailable)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionRefundAvailable", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionRefundAvailable is a log parse operation binding the contract event 0x63d288c09739f401752f5fd7550ff8144d1871b1b33c29cb2e882510cfa860fd.
//
// Solidity: event FrostRedemptionRefundAvailable(bytes32 indexed requestId, address redeemer, uint64 gross)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionRefundAvailable(log types.Log) (*FrostBridgeFrostRedemptionRefundAvailable, error) {
	event := new(FrostBridgeFrostRedemptionRefundAvailable)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionRefundAvailable", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionRefundedIterator is returned from FilterFrostRedemptionRefunded and is used to iterate over the raw logs and unpacked data for FrostRedemptionRefunded events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionRefundedIterator struct {
	Event *FrostBridgeFrostRedemptionRefunded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionRefundedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionRefunded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionRefunded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionRefundedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionRefundedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionRefunded represents a FrostRedemptionRefunded event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionRefunded struct {
	RequestId [32]byte
	Redeemer  common.Address
	Gross     uint64
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionRefunded is a free log retrieval operation binding the contract event 0xc6860f127d6743659dbf3f99edf863402931d496a40a24cd4cb71b94bf6e3b48.
//
// Solidity: event FrostRedemptionRefunded(bytes32 indexed requestId, address redeemer, uint64 gross)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionRefunded(opts *bind.FilterOpts, requestId [][32]byte) (*FrostBridgeFrostRedemptionRefundedIterator, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionRefunded", requestIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionRefundedIterator{contract: _FrostBridge.contract, event: "FrostRedemptionRefunded", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionRefunded is a free log subscription operation binding the contract event 0xc6860f127d6743659dbf3f99edf863402931d496a40a24cd4cb71b94bf6e3b48.
//
// Solidity: event FrostRedemptionRefunded(bytes32 indexed requestId, address redeemer, uint64 gross)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionRefunded(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionRefunded, requestId [][32]byte) (event.Subscription, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionRefunded", requestIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionRefunded)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionRefunded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionRefunded is a log parse operation binding the contract event 0xc6860f127d6743659dbf3f99edf863402931d496a40a24cd4cb71b94bf6e3b48.
//
// Solidity: event FrostRedemptionRefunded(bytes32 indexed requestId, address redeemer, uint64 gross)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionRefunded(log types.Log) (*FrostBridgeFrostRedemptionRefunded, error) {
	event := new(FrostBridgeFrostRedemptionRefunded)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionRefunded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionRequestedIterator is returned from FilterFrostRedemptionRequested and is used to iterate over the raw logs and unpacked data for FrostRedemptionRequested events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionRequestedIterator struct {
	Event *FrostBridgeFrostRedemptionRequested // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionRequestedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionRequested)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionRequested)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionRequestedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionRequestedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionRequested represents a FrostRedemptionRequested event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionRequested struct {
	RequestId         [32]byte
	WalletId          [32]byte
	Redeemer          common.Address
	Gross             uint64
	TreasuryFee       uint64
	MaximumBitcoinFee uint64
	Deadline          uint64
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionRequested is a free log retrieval operation binding the contract event 0xc4a9a91e975a77ad2a02fd7badf19bee4370a0d9c875073b0ac1b28268c44203.
//
// Solidity: event FrostRedemptionRequested(bytes32 indexed requestId, bytes32 indexed walletId, address redeemer, uint64 gross, uint64 treasuryFee, uint64 maximumBitcoinFee, uint64 deadline)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionRequested(opts *bind.FilterOpts, requestId [][32]byte, walletId [][32]byte) (*FrostBridgeFrostRedemptionRequestedIterator, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionRequested", requestIdRule, walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionRequestedIterator{contract: _FrostBridge.contract, event: "FrostRedemptionRequested", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionRequested is a free log subscription operation binding the contract event 0xc4a9a91e975a77ad2a02fd7badf19bee4370a0d9c875073b0ac1b28268c44203.
//
// Solidity: event FrostRedemptionRequested(bytes32 indexed requestId, bytes32 indexed walletId, address redeemer, uint64 gross, uint64 treasuryFee, uint64 maximumBitcoinFee, uint64 deadline)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionRequested(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionRequested, requestId [][32]byte, walletId [][32]byte) (event.Subscription, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionRequested", requestIdRule, walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionRequested)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionRequested", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionRequested is a log parse operation binding the contract event 0xc4a9a91e975a77ad2a02fd7badf19bee4370a0d9c875073b0ac1b28268c44203.
//
// Solidity: event FrostRedemptionRequested(bytes32 indexed requestId, bytes32 indexed walletId, address redeemer, uint64 gross, uint64 treasuryFee, uint64 maximumBitcoinFee, uint64 deadline)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionRequested(log types.Log) (*FrostBridgeFrostRedemptionRequested, error) {
	event := new(FrostBridgeFrostRedemptionRequested)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionRequested", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionTimeoutPendingIterator is returned from FilterFrostRedemptionTimeoutPending and is used to iterate over the raw logs and unpacked data for FrostRedemptionTimeoutPending events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionTimeoutPendingIterator struct {
	Event *FrostBridgeFrostRedemptionTimeoutPending // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionTimeoutPendingIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionTimeoutPending)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionTimeoutPending)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionTimeoutPendingIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionTimeoutPendingIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionTimeoutPending represents a FrostRedemptionTimeoutPending event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionTimeoutPending struct {
	RequestId     [32]byte
	ReservationId [32]byte
	TimeoutBlock  uint64
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionTimeoutPending is a free log retrieval operation binding the contract event 0x0b64907beb1b2cc43b13a532e9beb5d43d8842f0e7b44db5bc22bde6d8a21216.
//
// Solidity: event FrostRedemptionTimeoutPending(bytes32 indexed requestId, bytes32 indexed reservationId, uint64 timeoutBlock)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionTimeoutPending(opts *bind.FilterOpts, requestId [][32]byte, reservationId [][32]byte) (*FrostBridgeFrostRedemptionTimeoutPendingIterator, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}
	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionTimeoutPending", requestIdRule, reservationIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionTimeoutPendingIterator{contract: _FrostBridge.contract, event: "FrostRedemptionTimeoutPending", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionTimeoutPending is a free log subscription operation binding the contract event 0x0b64907beb1b2cc43b13a532e9beb5d43d8842f0e7b44db5bc22bde6d8a21216.
//
// Solidity: event FrostRedemptionTimeoutPending(bytes32 indexed requestId, bytes32 indexed reservationId, uint64 timeoutBlock)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionTimeoutPending(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionTimeoutPending, requestId [][32]byte, reservationId [][32]byte) (event.Subscription, error) {

	var requestIdRule []interface{}
	for _, requestIdItem := range requestId {
		requestIdRule = append(requestIdRule, requestIdItem)
	}
	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionTimeoutPending", requestIdRule, reservationIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionTimeoutPending)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionTimeoutPending", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionTimeoutPending is a log parse operation binding the contract event 0x0b64907beb1b2cc43b13a532e9beb5d43d8842f0e7b44db5bc22bde6d8a21216.
//
// Solidity: event FrostRedemptionTimeoutPending(bytes32 indexed requestId, bytes32 indexed reservationId, uint64 timeoutBlock)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionTimeoutPending(log types.Log) (*FrostBridgeFrostRedemptionTimeoutPending, error) {
	event := new(FrostBridgeFrostRedemptionTimeoutPending)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionTimeoutPending", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionsConfiguredIterator is returned from FilterFrostRedemptionsConfigured and is used to iterate over the raw logs and unpacked data for FrostRedemptionsConfigured events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionsConfiguredIterator struct {
	Event *FrostBridgeFrostRedemptionsConfigured // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionsConfiguredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionsConfigured)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionsConfigured)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionsConfiguredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionsConfiguredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionsConfigured represents a FrostRedemptionsConfigured event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionsConfigured struct {
	Genesis                  [32]byte
	FinalityBlocks           uint64
	MaximumCertificateBlocks uint64
	Watchtower               common.Address
	LegacyTransitionEvidence [32]byte
	Raw                      types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionsConfigured is a free log retrieval operation binding the contract event 0xc29dc5d327856b8da883795af66660b82ec48152747b20baafff93973fc24cdf.
//
// Solidity: event FrostRedemptionsConfigured(bytes32 genesis, uint64 finalityBlocks, uint64 maximumCertificateBlocks, address watchtower, bytes32 legacyTransitionEvidence)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionsConfigured(opts *bind.FilterOpts) (*FrostBridgeFrostRedemptionsConfiguredIterator, error) {

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionsConfigured")
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionsConfiguredIterator{contract: _FrostBridge.contract, event: "FrostRedemptionsConfigured", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionsConfigured is a free log subscription operation binding the contract event 0xc29dc5d327856b8da883795af66660b82ec48152747b20baafff93973fc24cdf.
//
// Solidity: event FrostRedemptionsConfigured(bytes32 genesis, uint64 finalityBlocks, uint64 maximumCertificateBlocks, address watchtower, bytes32 legacyTransitionEvidence)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionsConfigured(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionsConfigured) (event.Subscription, error) {

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionsConfigured")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionsConfigured)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionsConfigured", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionsConfigured is a log parse operation binding the contract event 0xc29dc5d327856b8da883795af66660b82ec48152747b20baafff93973fc24cdf.
//
// Solidity: event FrostRedemptionsConfigured(bytes32 genesis, uint64 finalityBlocks, uint64 maximumCertificateBlocks, address watchtower, bytes32 legacyTransitionEvidence)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionsConfigured(log types.Log) (*FrostBridgeFrostRedemptionsConfigured, error) {
	event := new(FrostBridgeFrostRedemptionsConfigured)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionsConfigured", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRedemptionsEnabledIterator is returned from FilterFrostRedemptionsEnabled and is used to iterate over the raw logs and unpacked data for FrostRedemptionsEnabled events raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionsEnabledIterator struct {
	Event *FrostBridgeFrostRedemptionsEnabled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRedemptionsEnabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRedemptionsEnabled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRedemptionsEnabled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRedemptionsEnabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRedemptionsEnabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRedemptionsEnabled represents a FrostRedemptionsEnabled event raised by the FrostBridge contract.
type FrostBridgeFrostRedemptionsEnabled struct {
	Enabled bool
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterFrostRedemptionsEnabled is a free log retrieval operation binding the contract event 0x9d19e5cae59159a18456ae074727b8181e4d6c47c59ab62a6d5a6fb8054ef148.
//
// Solidity: event FrostRedemptionsEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRedemptionsEnabled(opts *bind.FilterOpts) (*FrostBridgeFrostRedemptionsEnabledIterator, error) {

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRedemptionsEnabled")
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRedemptionsEnabledIterator{contract: _FrostBridge.contract, event: "FrostRedemptionsEnabled", logs: logs, sub: sub}, nil
}

// WatchFrostRedemptionsEnabled is a free log subscription operation binding the contract event 0x9d19e5cae59159a18456ae074727b8181e4d6c47c59ab62a6d5a6fb8054ef148.
//
// Solidity: event FrostRedemptionsEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRedemptionsEnabled(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRedemptionsEnabled) (event.Subscription, error) {

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRedemptionsEnabled")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRedemptionsEnabled)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionsEnabled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRedemptionsEnabled is a log parse operation binding the contract event 0x9d19e5cae59159a18456ae074727b8181e4d6c47c59ab62a6d5a6fb8054ef148.
//
// Solidity: event FrostRedemptionsEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRedemptionsEnabled(log types.Log) (*FrostBridgeFrostRedemptionsEnabled, error) {
	event := new(FrostBridgeFrostRedemptionsEnabled)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRedemptionsEnabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRegistryConfiguredIterator is returned from FilterFrostRegistryConfigured and is used to iterate over the raw logs and unpacked data for FrostRegistryConfigured events raised by the FrostBridge contract.
type FrostBridgeFrostRegistryConfiguredIterator struct {
	Event *FrostBridgeFrostRegistryConfigured // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRegistryConfiguredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRegistryConfigured)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRegistryConfigured)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRegistryConfiguredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRegistryConfiguredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRegistryConfigured represents a FrostRegistryConfigured event raised by the FrostBridge contract.
type FrostBridgeFrostRegistryConfigured struct {
	Registry common.Address
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostRegistryConfigured is a free log retrieval operation binding the contract event 0x697b189fe80b7a12d44fd2e26a1d2e7bfe842d5be32ccb109ac55cb0b4dd76be.
//
// Solidity: event FrostRegistryConfigured(address indexed registry)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRegistryConfigured(opts *bind.FilterOpts, registry []common.Address) (*FrostBridgeFrostRegistryConfiguredIterator, error) {

	var registryRule []interface{}
	for _, registryItem := range registry {
		registryRule = append(registryRule, registryItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRegistryConfigured", registryRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRegistryConfiguredIterator{contract: _FrostBridge.contract, event: "FrostRegistryConfigured", logs: logs, sub: sub}, nil
}

// WatchFrostRegistryConfigured is a free log subscription operation binding the contract event 0x697b189fe80b7a12d44fd2e26a1d2e7bfe842d5be32ccb109ac55cb0b4dd76be.
//
// Solidity: event FrostRegistryConfigured(address indexed registry)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRegistryConfigured(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRegistryConfigured, registry []common.Address) (event.Subscription, error) {

	var registryRule []interface{}
	for _, registryItem := range registry {
		registryRule = append(registryRule, registryItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRegistryConfigured", registryRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRegistryConfigured)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRegistryConfigured", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRegistryConfigured is a log parse operation binding the contract event 0x697b189fe80b7a12d44fd2e26a1d2e7bfe842d5be32ccb109ac55cb0b4dd76be.
//
// Solidity: event FrostRegistryConfigured(address indexed registry)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRegistryConfigured(log types.Log) (*FrostBridgeFrostRegistryConfigured, error) {
	event := new(FrostBridgeFrostRegistryConfigured)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRegistryConfigured", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostRequestsEnabledIterator is returned from FilterFrostRequestsEnabled and is used to iterate over the raw logs and unpacked data for FrostRequestsEnabled events raised by the FrostBridge contract.
type FrostBridgeFrostRequestsEnabledIterator struct {
	Event *FrostBridgeFrostRequestsEnabled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostRequestsEnabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostRequestsEnabled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostRequestsEnabled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostRequestsEnabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostRequestsEnabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostRequestsEnabled represents a FrostRequestsEnabled event raised by the FrostBridge contract.
type FrostBridgeFrostRequestsEnabled struct {
	Enabled bool
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterFrostRequestsEnabled is a free log retrieval operation binding the contract event 0xbe452238987f226b3033c974ef976c9559d2c8085a1f2fda9c4ee964cc0fde93.
//
// Solidity: event FrostRequestsEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostRequestsEnabled(opts *bind.FilterOpts) (*FrostBridgeFrostRequestsEnabledIterator, error) {

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostRequestsEnabled")
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostRequestsEnabledIterator{contract: _FrostBridge.contract, event: "FrostRequestsEnabled", logs: logs, sub: sub}, nil
}

// WatchFrostRequestsEnabled is a free log subscription operation binding the contract event 0xbe452238987f226b3033c974ef976c9559d2c8085a1f2fda9c4ee964cc0fde93.
//
// Solidity: event FrostRequestsEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostRequestsEnabled(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostRequestsEnabled) (event.Subscription, error) {

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostRequestsEnabled")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostRequestsEnabled)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostRequestsEnabled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostRequestsEnabled is a log parse operation binding the contract event 0xbe452238987f226b3033c974ef976c9559d2c8085a1f2fda9c4ee964cc0fde93.
//
// Solidity: event FrostRequestsEnabled(bool enabled)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostRequestsEnabled(log types.Log) (*FrostBridgeFrostRequestsEnabled, error) {
	event := new(FrostBridgeFrostRequestsEnabled)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostRequestsEnabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostReservationAdmittedIterator is returned from FilterFrostReservationAdmitted and is used to iterate over the raw logs and unpacked data for FrostReservationAdmitted events raised by the FrostBridge contract.
type FrostBridgeFrostReservationAdmittedIterator struct {
	Event *FrostBridgeFrostReservationAdmitted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostReservationAdmittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostReservationAdmitted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostReservationAdmitted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostReservationAdmittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostReservationAdmittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostReservationAdmitted represents a FrostReservationAdmitted event raised by the FrostBridge contract.
type FrostBridgeFrostReservationAdmitted struct {
	ReservationId    [32]byte
	WalletId         [32]byte
	IntentCommitment [32]byte
	TransactionHash  [32]byte
	Generation       uint64
	StateVersion     uint64
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterFrostReservationAdmitted is a free log retrieval operation binding the contract event 0xf286dafd89cf8be3ca5a2f134e3ce536b687808b4398c38ce3dcf43455870b5c.
//
// Solidity: event FrostReservationAdmitted(bytes32 indexed reservationId, bytes32 indexed walletId, bytes32 intentCommitment, bytes32 transactionHash, uint64 generation, uint64 stateVersion)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostReservationAdmitted(opts *bind.FilterOpts, reservationId [][32]byte, walletId [][32]byte) (*FrostBridgeFrostReservationAdmittedIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostReservationAdmitted", reservationIdRule, walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostReservationAdmittedIterator{contract: _FrostBridge.contract, event: "FrostReservationAdmitted", logs: logs, sub: sub}, nil
}

// WatchFrostReservationAdmitted is a free log subscription operation binding the contract event 0xf286dafd89cf8be3ca5a2f134e3ce536b687808b4398c38ce3dcf43455870b5c.
//
// Solidity: event FrostReservationAdmitted(bytes32 indexed reservationId, bytes32 indexed walletId, bytes32 intentCommitment, bytes32 transactionHash, uint64 generation, uint64 stateVersion)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostReservationAdmitted(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostReservationAdmitted, reservationId [][32]byte, walletId [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostReservationAdmitted", reservationIdRule, walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostReservationAdmitted)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostReservationAdmitted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostReservationAdmitted is a log parse operation binding the contract event 0xf286dafd89cf8be3ca5a2f134e3ce536b687808b4398c38ce3dcf43455870b5c.
//
// Solidity: event FrostReservationAdmitted(bytes32 indexed reservationId, bytes32 indexed walletId, bytes32 intentCommitment, bytes32 transactionHash, uint64 generation, uint64 stateVersion)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostReservationAdmitted(log types.Log) (*FrostBridgeFrostReservationAdmitted, error) {
	event := new(FrostBridgeFrostReservationAdmitted)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostReservationAdmitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostReservationResolvedBySweepIterator is returned from FilterFrostReservationResolvedBySweep and is used to iterate over the raw logs and unpacked data for FrostReservationResolvedBySweep events raised by the FrostBridge contract.
type FrostBridgeFrostReservationResolvedBySweepIterator struct {
	Event *FrostBridgeFrostReservationResolvedBySweep // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostReservationResolvedBySweepIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostReservationResolvedBySweep)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostReservationResolvedBySweep)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostReservationResolvedBySweepIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostReservationResolvedBySweepIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostReservationResolvedBySweep represents a FrostReservationResolvedBySweep event raised by the FrostBridge contract.
type FrostBridgeFrostReservationResolvedBySweep struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostReservationResolvedBySweep is a free log retrieval operation binding the contract event 0x19f188e7e9c088afadab446f59fc710d37a48aba5b1735c493e4fe0acdf9fba8.
//
// Solidity: event FrostReservationResolvedBySweep(bytes32 indexed reservationId, bytes32 indexed transactionHash)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostReservationResolvedBySweep(opts *bind.FilterOpts, reservationId [][32]byte, transactionHash [][32]byte) (*FrostBridgeFrostReservationResolvedBySweepIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostReservationResolvedBySweep", reservationIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostReservationResolvedBySweepIterator{contract: _FrostBridge.contract, event: "FrostReservationResolvedBySweep", logs: logs, sub: sub}, nil
}

// WatchFrostReservationResolvedBySweep is a free log subscription operation binding the contract event 0x19f188e7e9c088afadab446f59fc710d37a48aba5b1735c493e4fe0acdf9fba8.
//
// Solidity: event FrostReservationResolvedBySweep(bytes32 indexed reservationId, bytes32 indexed transactionHash)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostReservationResolvedBySweep(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostReservationResolvedBySweep, reservationId [][32]byte, transactionHash [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostReservationResolvedBySweep", reservationIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostReservationResolvedBySweep)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostReservationResolvedBySweep", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostReservationResolvedBySweep is a log parse operation binding the contract event 0x19f188e7e9c088afadab446f59fc710d37a48aba5b1735c493e4fe0acdf9fba8.
//
// Solidity: event FrostReservationResolvedBySweep(bytes32 indexed reservationId, bytes32 indexed transactionHash)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostReservationResolvedBySweep(log types.Log) (*FrostBridgeFrostReservationResolvedBySweep, error) {
	event := new(FrostBridgeFrostReservationResolvedBySweep)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostReservationResolvedBySweep", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostSanctionNotifiedIterator is returned from FilterFrostSanctionNotified and is used to iterate over the raw logs and unpacked data for FrostSanctionNotified events raised by the FrostBridge contract.
type FrostBridgeFrostSanctionNotifiedIterator struct {
	Event *FrostBridgeFrostSanctionNotified // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostSanctionNotifiedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostSanctionNotified)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostSanctionNotified)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostSanctionNotifiedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostSanctionNotifiedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostSanctionNotified represents a FrostSanctionNotified event raised by the FrostBridge contract.
type FrostBridgeFrostSanctionNotified struct {
	WalletId       [32]byte
	DescriptorHash [32]byte
	Reason         [32]byte
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterFrostSanctionNotified is a free log retrieval operation binding the contract event 0x08873c7ff320b988a5b04599937e8a396dce669bfaefde230f4a73681abe9e0b.
//
// Solidity: event FrostSanctionNotified(bytes32 indexed walletId, bytes32 descriptorHash, bytes32 reason)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostSanctionNotified(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostSanctionNotifiedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostSanctionNotified", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostSanctionNotifiedIterator{contract: _FrostBridge.contract, event: "FrostSanctionNotified", logs: logs, sub: sub}, nil
}

// WatchFrostSanctionNotified is a free log subscription operation binding the contract event 0x08873c7ff320b988a5b04599937e8a396dce669bfaefde230f4a73681abe9e0b.
//
// Solidity: event FrostSanctionNotified(bytes32 indexed walletId, bytes32 descriptorHash, bytes32 reason)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostSanctionNotified(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostSanctionNotified, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostSanctionNotified", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostSanctionNotified)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostSanctionNotified", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostSanctionNotified is a log parse operation binding the contract event 0x08873c7ff320b988a5b04599937e8a396dce669bfaefde230f4a73681abe9e0b.
//
// Solidity: event FrostSanctionNotified(bytes32 indexed walletId, bytes32 descriptorHash, bytes32 reason)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostSanctionNotified(log types.Log) (*FrostBridgeFrostSanctionNotified, error) {
	event := new(FrostBridgeFrostSanctionNotified)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostSanctionNotified", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostSweepResolutionApprovedIterator is returned from FilterFrostSweepResolutionApproved and is used to iterate over the raw logs and unpacked data for FrostSweepResolutionApproved events raised by the FrostBridge contract.
type FrostBridgeFrostSweepResolutionApprovedIterator struct {
	Event *FrostBridgeFrostSweepResolutionApproved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostSweepResolutionApprovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostSweepResolutionApproved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostSweepResolutionApproved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostSweepResolutionApprovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostSweepResolutionApprovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostSweepResolutionApproved represents a FrostSweepResolutionApproved event raised by the FrostBridge contract.
type FrostBridgeFrostSweepResolutionApproved struct {
	ReservationId   [32]byte
	TransactionHash [32]byte
	Vault           common.Address
	ValidUntilBlock uint64
	Seats           []uint16
	Signatures      []byte
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterFrostSweepResolutionApproved is a free log retrieval operation binding the contract event 0xb6e4967e3a3ea91062c61d3913831cda6559eea6495326f30a1a642c2f8740ff.
//
// Solidity: event FrostSweepResolutionApproved(bytes32 indexed reservationId, bytes32 indexed transactionHash, address vault, uint64 validUntilBlock, uint16[] seats, bytes signatures)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostSweepResolutionApproved(opts *bind.FilterOpts, reservationId [][32]byte, transactionHash [][32]byte) (*FrostBridgeFrostSweepResolutionApprovedIterator, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostSweepResolutionApproved", reservationIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostSweepResolutionApprovedIterator{contract: _FrostBridge.contract, event: "FrostSweepResolutionApproved", logs: logs, sub: sub}, nil
}

// WatchFrostSweepResolutionApproved is a free log subscription operation binding the contract event 0xb6e4967e3a3ea91062c61d3913831cda6559eea6495326f30a1a642c2f8740ff.
//
// Solidity: event FrostSweepResolutionApproved(bytes32 indexed reservationId, bytes32 indexed transactionHash, address vault, uint64 validUntilBlock, uint16[] seats, bytes signatures)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostSweepResolutionApproved(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostSweepResolutionApproved, reservationId [][32]byte, transactionHash [][32]byte) (event.Subscription, error) {

	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}
	var transactionHashRule []interface{}
	for _, transactionHashItem := range transactionHash {
		transactionHashRule = append(transactionHashRule, transactionHashItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostSweepResolutionApproved", reservationIdRule, transactionHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostSweepResolutionApproved)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostSweepResolutionApproved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostSweepResolutionApproved is a log parse operation binding the contract event 0xb6e4967e3a3ea91062c61d3913831cda6559eea6495326f30a1a642c2f8740ff.
//
// Solidity: event FrostSweepResolutionApproved(bytes32 indexed reservationId, bytes32 indexed transactionHash, address vault, uint64 validUntilBlock, uint16[] seats, bytes signatures)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostSweepResolutionApproved(log types.Log) (*FrostBridgeFrostSweepResolutionApproved, error) {
	event := new(FrostBridgeFrostSweepResolutionApproved)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostSweepResolutionApproved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletClosedIterator is returned from FilterFrostWalletClosed and is used to iterate over the raw logs and unpacked data for FrostWalletClosed events raised by the FrostBridge contract.
type FrostBridgeFrostWalletClosedIterator struct {
	Event *FrostBridgeFrostWalletClosed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletClosedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletClosed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletClosed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletClosedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletClosedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletClosed represents a FrostWalletClosed event raised by the FrostBridge contract.
type FrostBridgeFrostWalletClosed struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletClosed is a free log retrieval operation binding the contract event 0xd0d1638ad0a658328baebc8f2a3f30e9ce81aa10e2772ccbf125d24ce9ce3683.
//
// Solidity: event FrostWalletClosed(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletClosed(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostWalletClosedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletClosed", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletClosedIterator{contract: _FrostBridge.contract, event: "FrostWalletClosed", logs: logs, sub: sub}, nil
}

// WatchFrostWalletClosed is a free log subscription operation binding the contract event 0xd0d1638ad0a658328baebc8f2a3f30e9ce81aa10e2772ccbf125d24ce9ce3683.
//
// Solidity: event FrostWalletClosed(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletClosed(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletClosed, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletClosed", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletClosed)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletClosed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletClosed is a log parse operation binding the contract event 0xd0d1638ad0a658328baebc8f2a3f30e9ce81aa10e2772ccbf125d24ce9ce3683.
//
// Solidity: event FrostWalletClosed(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletClosed(log types.Log) (*FrostBridgeFrostWalletClosed, error) {
	event := new(FrostBridgeFrostWalletClosed)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletClosed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletClosingIterator is returned from FilterFrostWalletClosing and is used to iterate over the raw logs and unpacked data for FrostWalletClosing events raised by the FrostBridge contract.
type FrostBridgeFrostWalletClosingIterator struct {
	Event *FrostBridgeFrostWalletClosing // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletClosingIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletClosing)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletClosing)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletClosingIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletClosingIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletClosing represents a FrostWalletClosing event raised by the FrostBridge contract.
type FrostBridgeFrostWalletClosing struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletClosing is a free log retrieval operation binding the contract event 0x6d1f85693f27b1b7641ac0daf8d1f21ac7364bb7fbff8174973dea175573af66.
//
// Solidity: event FrostWalletClosing(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletClosing(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostWalletClosingIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletClosing", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletClosingIterator{contract: _FrostBridge.contract, event: "FrostWalletClosing", logs: logs, sub: sub}, nil
}

// WatchFrostWalletClosing is a free log subscription operation binding the contract event 0x6d1f85693f27b1b7641ac0daf8d1f21ac7364bb7fbff8174973dea175573af66.
//
// Solidity: event FrostWalletClosing(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletClosing(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletClosing, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletClosing", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletClosing)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletClosing", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletClosing is a log parse operation binding the contract event 0x6d1f85693f27b1b7641ac0daf8d1f21ac7364bb7fbff8174973dea175573af66.
//
// Solidity: event FrostWalletClosing(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletClosing(log types.Log) (*FrostBridgeFrostWalletClosing, error) {
	event := new(FrostBridgeFrostWalletClosing)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletClosing", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletExpiredIterator is returned from FilterFrostWalletExpired and is used to iterate over the raw logs and unpacked data for FrostWalletExpired events raised by the FrostBridge contract.
type FrostBridgeFrostWalletExpiredIterator struct {
	Event *FrostBridgeFrostWalletExpired // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletExpiredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletExpired)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletExpired)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletExpiredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletExpiredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletExpired represents a FrostWalletExpired event raised by the FrostBridge contract.
type FrostBridgeFrostWalletExpired struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletExpired is a free log retrieval operation binding the contract event 0x7eedb8a73fa8c825dd6a5d313c6b817c88514ddbfd73bc11f0b509cfbc546c19.
//
// Solidity: event FrostWalletExpired(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletExpired(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostWalletExpiredIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletExpired", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletExpiredIterator{contract: _FrostBridge.contract, event: "FrostWalletExpired", logs: logs, sub: sub}, nil
}

// WatchFrostWalletExpired is a free log subscription operation binding the contract event 0x7eedb8a73fa8c825dd6a5d313c6b817c88514ddbfd73bc11f0b509cfbc546c19.
//
// Solidity: event FrostWalletExpired(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletExpired(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletExpired, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletExpired", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletExpired)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletExpired", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletExpired is a log parse operation binding the contract event 0x7eedb8a73fa8c825dd6a5d313c6b817c88514ddbfd73bc11f0b509cfbc546c19.
//
// Solidity: event FrostWalletExpired(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletExpired(log types.Log) (*FrostBridgeFrostWalletExpired, error) {
	event := new(FrostBridgeFrostWalletExpired)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletExpired", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletMovingFundsIterator is returned from FilterFrostWalletMovingFunds and is used to iterate over the raw logs and unpacked data for FrostWalletMovingFunds events raised by the FrostBridge contract.
type FrostBridgeFrostWalletMovingFundsIterator struct {
	Event *FrostBridgeFrostWalletMovingFunds // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletMovingFundsIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletMovingFunds)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletMovingFunds)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletMovingFundsIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletMovingFundsIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletMovingFunds represents a FrostWalletMovingFunds event raised by the FrostBridge contract.
type FrostBridgeFrostWalletMovingFunds struct {
	WalletId [32]byte
	Reason   [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletMovingFunds is a free log retrieval operation binding the contract event 0x780de56a8864bb5b0044a0cd441559589565b1e0c1f228e4122305d82f4be4b8.
//
// Solidity: event FrostWalletMovingFunds(bytes32 indexed walletId, bytes32 reason)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletMovingFunds(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostWalletMovingFundsIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletMovingFunds", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletMovingFundsIterator{contract: _FrostBridge.contract, event: "FrostWalletMovingFunds", logs: logs, sub: sub}, nil
}

// WatchFrostWalletMovingFunds is a free log subscription operation binding the contract event 0x780de56a8864bb5b0044a0cd441559589565b1e0c1f228e4122305d82f4be4b8.
//
// Solidity: event FrostWalletMovingFunds(bytes32 indexed walletId, bytes32 reason)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletMovingFunds(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletMovingFunds, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletMovingFunds", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletMovingFunds)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletMovingFunds", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletMovingFunds is a log parse operation binding the contract event 0x780de56a8864bb5b0044a0cd441559589565b1e0c1f228e4122305d82f4be4b8.
//
// Solidity: event FrostWalletMovingFunds(bytes32 indexed walletId, bytes32 reason)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletMovingFunds(log types.Log) (*FrostBridgeFrostWalletMovingFunds, error) {
	event := new(FrostBridgeFrostWalletMovingFunds)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletMovingFunds", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletQuarantinedIterator is returned from FilterFrostWalletQuarantined and is used to iterate over the raw logs and unpacked data for FrostWalletQuarantined events raised by the FrostBridge contract.
type FrostBridgeFrostWalletQuarantinedIterator struct {
	Event *FrostBridgeFrostWalletQuarantined // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletQuarantinedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletQuarantined)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletQuarantined)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletQuarantinedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletQuarantinedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletQuarantined represents a FrostWalletQuarantined event raised by the FrostBridge contract.
type FrostBridgeFrostWalletQuarantined struct {
	WalletId      [32]byte
	ReservationId [32]byte
	StateVersion  uint64
	ReasonHash    [32]byte
	EvidenceHash  [32]byte
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletQuarantined is a free log retrieval operation binding the contract event 0x287e5549a07aca4ce96ee860cacb0ed88868155c8d3a1bb033e527ea851a2d36.
//
// Solidity: event FrostWalletQuarantined(bytes32 indexed walletId, bytes32 indexed reservationId, uint64 stateVersion, bytes32 reasonHash, bytes32 evidenceHash)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletQuarantined(opts *bind.FilterOpts, walletId [][32]byte, reservationId [][32]byte) (*FrostBridgeFrostWalletQuarantinedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletQuarantined", walletIdRule, reservationIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletQuarantinedIterator{contract: _FrostBridge.contract, event: "FrostWalletQuarantined", logs: logs, sub: sub}, nil
}

// WatchFrostWalletQuarantined is a free log subscription operation binding the contract event 0x287e5549a07aca4ce96ee860cacb0ed88868155c8d3a1bb033e527ea851a2d36.
//
// Solidity: event FrostWalletQuarantined(bytes32 indexed walletId, bytes32 indexed reservationId, uint64 stateVersion, bytes32 reasonHash, bytes32 evidenceHash)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletQuarantined(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletQuarantined, walletId [][32]byte, reservationId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var reservationIdRule []interface{}
	for _, reservationIdItem := range reservationId {
		reservationIdRule = append(reservationIdRule, reservationIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletQuarantined", walletIdRule, reservationIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletQuarantined)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletQuarantined", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletQuarantined is a log parse operation binding the contract event 0x287e5549a07aca4ce96ee860cacb0ed88868155c8d3a1bb033e527ea851a2d36.
//
// Solidity: event FrostWalletQuarantined(bytes32 indexed walletId, bytes32 indexed reservationId, uint64 stateVersion, bytes32 reasonHash, bytes32 evidenceHash)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletQuarantined(log types.Log) (*FrostBridgeFrostWalletQuarantined, error) {
	event := new(FrostBridgeFrostWalletQuarantined)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletQuarantined", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletReadyIterator is returned from FilterFrostWalletReady and is used to iterate over the raw logs and unpacked data for FrostWalletReady events raised by the FrostBridge contract.
type FrostBridgeFrostWalletReadyIterator struct {
	Event *FrostBridgeFrostWalletReady // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletReadyIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletReady)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletReady)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletReadyIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletReadyIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletReady represents a FrostWalletReady event raised by the FrostBridge contract.
type FrostBridgeFrostWalletReady struct {
	WalletId       [32]byte
	DescriptorHash [32]byte
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletReady is a free log retrieval operation binding the contract event 0xe9d5769706430a8676db63f8a08b5f068028442ccd1092602673ab9af19eea32.
//
// Solidity: event FrostWalletReady(bytes32 indexed walletId, bytes32 descriptorHash)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletReady(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostWalletReadyIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletReady", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletReadyIterator{contract: _FrostBridge.contract, event: "FrostWalletReady", logs: logs, sub: sub}, nil
}

// WatchFrostWalletReady is a free log subscription operation binding the contract event 0xe9d5769706430a8676db63f8a08b5f068028442ccd1092602673ab9af19eea32.
//
// Solidity: event FrostWalletReady(bytes32 indexed walletId, bytes32 descriptorHash)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletReady(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletReady, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletReady", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletReady)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletReady", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletReady is a log parse operation binding the contract event 0xe9d5769706430a8676db63f8a08b5f068028442ccd1092602673ab9af19eea32.
//
// Solidity: event FrostWalletReady(bytes32 indexed walletId, bytes32 descriptorHash)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletReady(log types.Log) (*FrostBridgeFrostWalletReady, error) {
	event := new(FrostBridgeFrostWalletReady)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletReady", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletRegisteredIterator is returned from FilterFrostWalletRegistered and is used to iterate over the raw logs and unpacked data for FrostWalletRegistered events raised by the FrostBridge contract.
type FrostBridgeFrostWalletRegisteredIterator struct {
	Event *FrostBridgeFrostWalletRegistered // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletRegisteredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletRegistered)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletRegistered)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletRegisteredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletRegisteredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletRegistered represents a FrostWalletRegistered event raised by the FrostBridge contract.
type FrostBridgeFrostWalletRegistered struct {
	WalletId       [32]byte
	Label          [20]byte
	DescriptorHash [32]byte
	Deadline       uint64
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletRegistered is a free log retrieval operation binding the contract event 0x9e23d86fb1cd65554e54ddd6b6336981dcfc26dbea68ca5291ea6aa3e4f64e01.
//
// Solidity: event FrostWalletRegistered(bytes32 indexed walletId, bytes20 indexed label, bytes32 descriptorHash, uint64 deadline)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletRegistered(opts *bind.FilterOpts, walletId [][32]byte, label [][20]byte) (*FrostBridgeFrostWalletRegisteredIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var labelRule []interface{}
	for _, labelItem := range label {
		labelRule = append(labelRule, labelItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletRegistered", walletIdRule, labelRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletRegisteredIterator{contract: _FrostBridge.contract, event: "FrostWalletRegistered", logs: logs, sub: sub}, nil
}

// WatchFrostWalletRegistered is a free log subscription operation binding the contract event 0x9e23d86fb1cd65554e54ddd6b6336981dcfc26dbea68ca5291ea6aa3e4f64e01.
//
// Solidity: event FrostWalletRegistered(bytes32 indexed walletId, bytes20 indexed label, bytes32 descriptorHash, uint64 deadline)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletRegistered(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletRegistered, walletId [][32]byte, label [][20]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}
	var labelRule []interface{}
	for _, labelItem := range label {
		labelRule = append(labelRule, labelItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletRegistered", walletIdRule, labelRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletRegistered)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletRegistered", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletRegistered is a log parse operation binding the contract event 0x9e23d86fb1cd65554e54ddd6b6336981dcfc26dbea68ca5291ea6aa3e4f64e01.
//
// Solidity: event FrostWalletRegistered(bytes32 indexed walletId, bytes20 indexed label, bytes32 descriptorHash, uint64 deadline)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletRegistered(log types.Log) (*FrostBridgeFrostWalletRegistered, error) {
	event := new(FrostBridgeFrostWalletRegistered)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletRegistered", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostBridgeFrostWalletReopenedIterator is returned from FilterFrostWalletReopened and is used to iterate over the raw logs and unpacked data for FrostWalletReopened events raised by the FrostBridge contract.
type FrostBridgeFrostWalletReopenedIterator struct {
	Event *FrostBridgeFrostWalletReopened // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostBridgeFrostWalletReopenedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostBridgeFrostWalletReopened)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostBridgeFrostWalletReopened)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostBridgeFrostWalletReopenedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostBridgeFrostWalletReopenedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostBridgeFrostWalletReopened represents a FrostWalletReopened event raised by the FrostBridge contract.
type FrostBridgeFrostWalletReopened struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterFrostWalletReopened is a free log retrieval operation binding the contract event 0xf3de04da4b7828422db8b3d861f248a1760ee6bdc8f9db9c9144a301d8ed677b.
//
// Solidity: event FrostWalletReopened(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) FilterFrostWalletReopened(opts *bind.FilterOpts, walletId [][32]byte) (*FrostBridgeFrostWalletReopenedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.FilterLogs(opts, "FrostWalletReopened", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostBridgeFrostWalletReopenedIterator{contract: _FrostBridge.contract, event: "FrostWalletReopened", logs: logs, sub: sub}, nil
}

// WatchFrostWalletReopened is a free log subscription operation binding the contract event 0xf3de04da4b7828422db8b3d861f248a1760ee6bdc8f9db9c9144a301d8ed677b.
//
// Solidity: event FrostWalletReopened(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) WatchFrostWalletReopened(opts *bind.WatchOpts, sink chan<- *FrostBridgeFrostWalletReopened, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostBridge.contract.WatchLogs(opts, "FrostWalletReopened", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostBridgeFrostWalletReopened)
				if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletReopened", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFrostWalletReopened is a log parse operation binding the contract event 0xf3de04da4b7828422db8b3d861f248a1760ee6bdc8f9db9c9144a301d8ed677b.
//
// Solidity: event FrostWalletReopened(bytes32 indexed walletId)
func (_FrostBridge *FrostBridgeFilterer) ParseFrostWalletReopened(log types.Log) (*FrostBridgeFrostWalletReopened, error) {
	event := new(FrostBridgeFrostWalletReopened)
	if err := _FrostBridge.contract.UnpackLog(event, "FrostWalletReopened", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
