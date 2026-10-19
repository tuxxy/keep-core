package ethereum

import (
	"context"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost/enforcement"
	"github.com/keep-network/keep-core/pkg/frost/movement"
)

var _ enforcement.Chain = (*FrostRedemptionChain)(nil)

// ChallengeLogs lists the FrostFraudChallengeSubmitted logs of wallet in
// [from, to].
func (c *FrostRedemptionChain) ChallengeLogs(ctx context.Context, wallet [32]byte, from, to uint64) ([]enforcement.ChallengeLog, error) {
	it, err := c.bridge.FilterFrostFraudChallengeSubmitted(c.filter(ctx, from, to), [][32]byte{wallet}, nil)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []enforcement.ChallengeLog
	for it.Next() {
		if !it.Event.Raw.Removed {
			out = append(out, enforcement.ChallengeLog{Log: movement.Log{Block: it.Event.Raw.BlockNumber, BlockHash: it.Event.Raw.BlockHash}, WalletID: it.Event.WalletId, Digest: it.Event.Digest})
		}
	}
	return out, it.Error()
}

// Challenge reads a C-08 challenge at the current head.
func (c *FrostRedemptionChain) Challenge(ctx context.Context, wallet, digest [32]byte) (enforcement.Challenge, error) {
	r, err := c.bridge.FrostFraudChallenge(&bind.CallOpts{Context: ctx}, wallet, digest)
	if err != nil {
		return enforcement.Challenge{}, err
	}
	return enforcement.Challenge{WalletID: r.WalletId, Digest: r.Digest, Challenger: r.Challenger, ReportedAt: r.ReportedAt, Resolved: r.Resolved}, nil
}

// FraudDefeatTimeout is the shared legacy fraudChallengeDefeatTimeout, in seconds.
func (c *FrostRedemptionChain) FraudDefeatTimeout(ctx context.Context) (uint64, error) {
	p, err := c.legacy.FraudParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		return 0, err
	}
	return uint64(p.FraudChallengeDefeatTimeout), nil
}

func (c *FrostRedemptionChain) DefeatChallenge(ctx context.Context, d enforcement.Defense) error {
	tx := d.Transaction
	defense := frostbridgeabi.FrostFraudDefense{WalletId: d.WalletID, Transaction: frostbridgeabi.BitcoinTxInfo{Version: [4]byte(tx.SerializeVersion()), InputVector: tx.SerializeInputs(), OutputVector: tx.SerializeOutputs(), Locktime: [4]byte(tx.SerializeLocktime())}, InputIndex: d.Input, LeafHash: d.LeafHash, Authorization: d.Authorization, AuthorizationRef: d.Ref}
	for _, p := range d.Prevouts {
		defense.Prevouts = append(defense.Prevouts, frostbridgeabi.FrostFraudPrevout{Value: uint64(p.Value), Script: append([]byte(nil), p.PublicKeyScript...)})
	}
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.DefeatFrostFraudChallenge(o, defense)
	})
}

func (c *FrostRedemptionChain) DefeatChallengeWithHeartbeat(ctx context.Context, wallet [32]byte, nonce uint64) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.DefeatFrostFraudChallengeWithHeartbeat(o, wallet, nonce)
	})
}

func (c *FrostRedemptionChain) NotifyChallengeTimeout(ctx context.Context, wallet, digest [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.NotifyFrostFraudChallengeDefeatTimeout(o, wallet, digest)
	})
}

func (c *FrostRedemptionChain) InactivityNonce(ctx context.Context, wallet [32]byte) (uint64, error) {
	return c.bridge.FrostInactivityNonce(&bind.CallOpts{Context: ctx}, wallet)
}

// InactivityClaimDigest reads C-09's claim digest; tests compare it with
// enforcement.ClaimDigest.
func (c *FrostRedemptionChain) InactivityClaimDigest(ctx context.Context, claim enforcement.Claim) ([32]byte, error) {
	return c.bridge.FrostInactivityClaimDigest(&bind.CallOpts{Context: ctx}, claim.WalletID, claim.Nonce, claim.Inactive, claim.HeartbeatFailed)
}

func (c *FrostRedemptionChain) NotifyInactivity(ctx context.Context, claim enforcement.Claim, seats []uint16, signatures []byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.NotifyFrostOperatorInactivity(o, claim.WalletID, claim.Nonce, claim.Inactive, claim.HeartbeatFailed, seats, signatures)
	})
}
