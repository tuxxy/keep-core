// Package enforcement is the K-08 host side of FROST fraud, heartbeat and
// inactivity enforcement: it defends this host's released signatures, notifies
// expired challenges it cannot defend, runs heartbeats and claims inactivity
// from authenticated presence only. See verification/BOUNDARIES.md (V-09).
package enforcement

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// C-08 authorization kinds.
const (
	AuthProven   uint8 = 1
	AuthIntent   uint8 = 2
	AuthMovement uint8 = 3
	AuthSelf     uint8 = 4
)

// Challenge mirrors FrostFraudState.Challenge.
type Challenge struct {
	WalletID, Digest [32]byte
	Challenger       [20]byte
	ReportedAt       uint64
	Resolved         bool
}

// ChallengeLog is one FrostFraudChallengeSubmitted event.
type ChallengeLog struct {
	movement.Log
	WalletID, Digest [32]byte
}

// Defense is the C-08 transaction defense.
type Defense struct {
	WalletID      [32]byte
	Transaction   *bitcoin.Transaction
	Prevouts      []bitcoin.PreviousOutput
	Input         uint32
	LeafHash      [32]byte
	Authorization uint8
	Ref           [32]byte
}

// Claim is a C-09 inactivity claim.
type Claim struct {
	WalletID, DescriptorHash [32]byte
	Nonce                    uint64
	Inactive                 []uint16
	HeartbeatFailed          bool
}

// Chain is the C-08 and C-09 enforcement seam.
type Chain interface {
	ChainID() uint64
	Bridge() [20]byte
	Head(ctx context.Context) (redemption.Head, error)
	HeaderHash(ctx context.Context, number uint64) ([32]byte, error)
	Timestamp(ctx context.Context, number uint64) (uint64, error)
	ChallengeLogs(ctx context.Context, wallet [32]byte, from, to uint64) ([]ChallengeLog, error)
	Challenge(ctx context.Context, wallet, digest [32]byte) (Challenge, error)
	FraudDefeatTimeout(ctx context.Context) (uint64, error)
	ProvenTransaction(ctx context.Context, at uint64, txHash [32]byte) ([32]byte, error)
	DefeatChallenge(ctx context.Context, d Defense) error
	DefeatChallengeWithHeartbeat(ctx context.Context, wallet [32]byte, nonce uint64) error
	NotifyChallengeTimeout(ctx context.Context, wallet, digest [32]byte) error
	InactivityNonce(ctx context.Context, wallet [32]byte) (uint64, error)
	NotifyInactivity(ctx context.Context, c Claim, seats []uint16, signatures []byte) error
}

var inactivityDomain = crypto.Keccak256Hash([]byte("tbtc-v2/frost-inactivity/v1"))

// ClaimDigest is C-09's frostInactivityClaimDigest:
// keccak256(abi.encode(domain, chainid, bridge, walletId, descriptorHash,
// nonce, inactiveSeats, heartbeatFailed)).
func ClaimDigest(chainID uint64, bridge [20]byte, c Claim) ([32]byte, error) {
	types := []string{"bytes32", "uint256", "address", "bytes32", "bytes32", "uint64", "uint16[]", "bool"}
	var args abi.Arguments
	for _, name := range types {
		t, err := abi.NewType(name, "", nil)
		if err != nil {
			return [32]byte{}, err
		}
		args = append(args, abi.Argument{Type: t})
	}
	if len(c.Inactive) == 0 {
		return [32]byte{}, errors.New("empty inactivity claim")
	}
	packed, err := args.Pack([32]byte(inactivityDomain), new(big.Int).SetUint64(chainID), common.Address(bridge), c.WalletID, c.DescriptorHash, c.Nonce, append([]uint16(nil), c.Inactive...), c.HeartbeatFailed)
	if err != nil {
		return [32]byte{}, err
	}
	return crypto.Keccak256Hash(packed), nil
}
