package ethereum

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// CurrentEpoch is the FROST registry's latest DKG epoch.
func (c *FrostChain) CurrentEpoch(ctx context.Context) (uint64, error) {
	return c.registry.Epoch(&bind.CallOpts{Context: ctx})
}

// ApprovedWallet is the wallet the FROST registry approved for the epoch,
// zero if none.
func (c *FrostChain) ApprovedWallet(ctx context.Context, epoch uint64) ([32]byte, error) {
	return c.registry.ApprovedWallet(&bind.CallOpts{Context: ctx}, epoch)
}

// Wallet is the FROST registry's record of the wallet.
func (c *FrostChain) Wallet(ctx context.Context, id [32]byte) (dkg.Wallet, error) {
	return c.registry.Wallet(&bind.CallOpts{Context: ctx}, id)
}

// ApprovedEpochs lists the epochs whose DKG result the FROST registry
// approved, from its ResultApproved logs, oldest first.
func (c *FrostChain) ApprovedEpochs(ctx context.Context) ([]uint64, error) {
	iterator, err := c.registry.FilterResultApproved(&bind.FilterOpts{Context: ctx}, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	var epochs []uint64
	for iterator.Next() {
		if !iterator.Event.Raw.Removed {
			epochs = append(epochs, iterator.Event.Epoch)
		}
	}
	return epochs, iterator.Error()
}

var _ tbtc.FrostRegistryChain = (*FrostChain)(nil)

// ConnectFrost implements tbtc.FrostChainConnector: it binds the FROST
// wallet registry and the FROST extension of the configured Bridge with the
// operator's key, for a node with FROST configured. A node without FROST
// never calls it.
func (tc *TbtcChain) ConnectFrost(
	ctx context.Context,
	registry [20]byte,
) (tbtc.FrostRegistryChain, tbtc.FrostBridgeChain, error) {
	frostChain, err := NewFrostChain(ctx, tc.ethClient, tc.key.PrivateKey, registry, tc.bridgeAddress)
	if err != nil {
		return nil, nil, err
	}
	bridge, err := tc.frostRedemptionChain(ctx)
	if err != nil {
		return nil, nil, err
	}
	return frostChain, bridge, nil
}

var _ tbtc.FrostChainConnector = (*TbtcChain)(nil)

// FrostBridge implements tbtc.FrostBridgeConnector: the FROST extension of
// the Bridge, bound on first use by a FROST wallet's proposal task.
func (tc *TbtcChain) FrostBridge(ctx context.Context) (tbtc.FrostBridgeChain, error) {
	return tc.frostRedemptionChain(ctx)
}

var _ tbtc.FrostBridgeConnector = (*TbtcChain)(nil)

// frostRedemptionChain binds the FROST extension of the Bridge once.
func (tc *TbtcChain) frostRedemptionChain(ctx context.Context) (*FrostRedemptionChain, error) {
	tc.frostRedemptionMutex.Lock()
	defer tc.frostRedemptionMutex.Unlock()

	if tc.frostRedemption == nil {
		bridge, err := NewFrostRedemptionChain(ctx, tc.ethClient, tc.key.PrivateKey, tc.bridgeAddress)
		if err != nil {
			return nil, err
		}
		tc.frostRedemption = bridge
	}
	return tc.frostRedemption, nil
}

// PendingFrostRedemptions implements tbtc.FrostRedemptionRequestChain: the
// output scripts of the FROST wallet's requested and unreserved redemptions
// requested since startBlock, oldest first, at most limit.
func (tc *TbtcChain) PendingFrostRedemptions(
	outputKey [32]byte,
	startBlock uint64,
	limit int,
) ([]bitcoin.Script, error) {
	if limit <= 0 {
		return nil, errors.New("invalid FROST redemption limit")
	}
	ctx := context.Background()
	bridge, err := tc.frostRedemptionChain(ctx)
	if err != nil {
		return nil, err
	}
	head, err := tc.blockCounter.CurrentBlock()
	if err != nil {
		return nil, err
	}
	wallet := dkg.WalletID(outputKey)
	events, err := bridge.RequestEvents(ctx, wallet, startBlock, head)
	if err != nil {
		return nil, err
	}

	var scripts []bitcoin.Script
	seen := make(map[[32]byte]bool)
	for _, event := range events {
		if seen[event.RequestID] || len(scripts) == limit {
			continue
		}
		seen[event.RequestID] = true
		request, err := bridge.Request(ctx, head, event.RequestID)
		if err != nil {
			return nil, err
		}
			if request.State != redemption.RequestRequested ||
			request.ReservationID != ([32]byte{}) ||
			request.WalletID != wallet {
			continue
		}
		scripts = append(scripts, bitcoin.Script(request.Script))
	}
	return scripts, nil
}

// ReservedFrostRedemptions implements tbtc.FrostRedemptionRequestChain: the
// output scripts of the requests of the FROST wallet's admitted, unresolved
// reservation, in its order, and whether the wallet holds one.
func (tc *TbtcChain) ReservedFrostRedemptions(
	outputKey [32]byte,
) ([]bitcoin.Script, bool, error) {
	ctx := context.Background()
	bridge, err := tc.frostRedemptionChain(ctx)
	if err != nil {
		return nil, false, err
	}
	head, err := tc.blockCounter.CurrentBlock()
	if err != nil {
		return nil, false, err
	}
	custody, err := bridge.Custody(ctx, head, dkg.WalletID(outputKey))
	if err != nil {
		return nil, false, err
	}
	if custody.ActiveReservation == ([32]byte{}) {
		return nil, false, nil
	}
	intent, err := bridge.Intent(ctx, head, custody.ActiveReservation)
	if err != nil {
		return nil, false, err
	}
	if !intent.State.Unresolved() {
		return nil, false, nil
	}

	scripts := make([]bitcoin.Script, len(intent.RequestIDs))
	for i, id := range intent.RequestIDs {
		request, err := bridge.Request(ctx, head, id)
		if err != nil {
			return nil, false, err
		}
		scripts[i] = bitcoin.Script(request.Script)
	}
	return scripts, true, nil
}

var _ tbtc.FrostRedemptionRequestChain = (*TbtcChain)(nil)
