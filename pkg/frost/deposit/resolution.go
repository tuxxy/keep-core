package deposit

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

var resolutionDomain = crypto.Keccak256Hash([]byte("tbtc-v2/frost-reservation-sweep-resolution/v1"))

// ResolutionDigest is the C-04 FrostSweepResolutionCertificate digest:
// keccak256(abi.encode(DOMAIN, chainid, bridge, authorization)).
func ResolutionDigest(chainID uint64, bridge [20]byte, a ResolutionAuthorization) [32]byte {
	b := append([]byte(nil), resolutionDomain[:]...)
	b = append(b, word(chainID)...)
	b = append(b, address32(bridge)...)
	b = append(b, a.ReservationID[:]...)
	b = append(b, a.TransactionHash[:]...)
	b = append(b, address32(a.Vault)...)
	b = append(b, common32(new(big.Int).SetUint64(a.ValidUntilBlock))...)
	return crypto.Keccak256Hash(b)
}

func common32(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}

// ResolutionAttester is one operator's honest path for the C-04 sweep-
// resolution certificate (VD-07 reading 3). Signing the certificate is a host
// act: the main lock moves from redemption(r) to resolution(r, tx) durably
// first, so the host never issues a payment or cancellation grant for r after
// it may have approved a sweep of the same main.
type ResolutionAttester struct {
	Chain Reader
	Lock  *MainLock
	// Authority validates the complete sweep (deposits, margin, template,
	// main rule) before the lock moves; a stale plan is refused.
	Authority *Authority
	Key       *ecdsa.PrivateKey
	Seats     []uint16
	// Reload checks the completed key in a fresh worker before attesting.
	Reload func(context.Context) error
	// Directives are governance instructions for Quarantined reservations.
	// A resolution sweep is a cancellation-class act: a contained
	// reservation needs a cancel directive (spec issue #3).
	Directives redemption.Directives
	boundary   func(string) error
}

func (a *ResolutionAttester) step(name string) error {
	if a.boundary != nil {
		return a.boundary(name)
	}
	return nil
}

// Attest signs the resolution certificate for a resolution sweep plan. The
// reservation must be unresolved and active for this wallet, with the plan's
// main as its reserved conflict input.
func (a *ResolutionAttester) Attest(ctx context.Context, plan *signing.Plan, validUntil uint64) (map[uint16][]byte, ResolutionAuthorization, error) {
	var none ResolutionAuthorization
	reservation := plan.SweepResolution()
	main := plan.SweepMain()
	if !plan.IsSweep() || reservation == ([32]byte{}) || main == nil {
		return nil, none, errors.New("not a resolution sweep")
	}
	head, err := a.Chain.Head(ctx)
	if err != nil {
		return nil, none, err
	}
	cfg, err := a.Chain.Configuration(ctx, head.Number)
	if err != nil {
		return nil, none, err
	}
	if validUntil < head.Number || validUntil > head.Number+cfg.MaximumCertificateBlocks {
		return nil, none, errors.New("certificate deadline outside the admissible window")
	}
	intent, err := a.Chain.Intent(ctx, head.Number, reservation)
	if err != nil {
		return nil, none, err
	}
	custody, err := a.Chain.Custody(ctx, head.Number, plan.WalletID())
	if err != nil {
		return nil, none, err
	}
	k := intent.Certificate
	conflict := OutpointKey(main.Outpoint)
	if !intent.State.Unresolved() || k.WalletID != plan.WalletID() || k.DescriptorHash != plan.DescriptorHash() || custody.ActiveReservation != reservation || custody.MainOutpoint != conflict || k.ConflictTxHash != [32]byte(main.Outpoint.TransactionHash) || k.ConflictOutputIndex != main.Outpoint.OutputIndex {
		return nil, none, errors.New("reservation is not an unresolved active reservation of this main")
	}
	if intent.State == redemption.IntentQuarantined {
		if err = a.directed(ctx, reservation); err != nil {
			return nil, none, err
		}
	}
	if a.Reload != nil {
		if err = a.Reload(ctx); err != nil {
			return nil, none, fmt.Errorf("completed key reload: %w", err)
		}
	}
	if a.Authority == nil {
		return nil, none, errors.New("resolution attester needs the sweep authority")
	}
	authority := *a.Authority
	if authority.Directives == nil {
		authority.Directives = a.Directives
	}
	if _, err = authority.AuthorizeSweep(ctx, plan); err != nil {
		return nil, none, fmt.Errorf("resolution sweep refused: %w", err)
	}
	auth := ResolutionAuthorization{ReservationID: reservation, TransactionHash: plan.BitcoinTransactionHash(), Vault: plan.SweepVault(), ValidUntilBlock: validUntil}
	if err = a.step("before-resolution-attest"); err != nil {
		return nil, none, err
	}
	if err = a.Lock.Resolve(ctx, plan.WalletID(), main.Outpoint, reservation, auth.TransactionHash); err != nil {
		return nil, none, err
	}
	if err = a.step("after-resolution-attest"); err != nil {
		return nil, none, err
	}
	digest := ResolutionDigest(a.Chain.ChainID(), a.Chain.Bridge(), auth)
	out := map[uint16][]byte{}
	for _, seat := range a.Seats {
		if out[seat], err = redemption.SignDigest(a.Key, digest); err != nil {
			return nil, none, err
		}
	}
	return out, auth, nil
}

// directed requires a governance cancel directive for a contained reservation.
func (a *ResolutionAttester) directed(ctx context.Context, reservation [32]byte) error {
	if a.Directives == nil {
		return redemption.ErrContained
	}
	d, ok, err := a.Directives.Directive(ctx, reservation)
	if err != nil {
		return err
	}
	if !ok || d.Reservation != reservation || d.Class == "" || d.Action != redemption.DirectiveCancel {
		return redemption.ErrContained
	}
	return nil
}
