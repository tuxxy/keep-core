package ethereum

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// ReadFrostDrainPolicy reads a single block's routing policy and eligible
// immutable descriptors. It is shared by the live legacy chain adapter and
// the local integration adapter; neither supplies a preselected target list.
func ReadFrostDrainPolicy(ctx context.Context, backend bind.ContractBackend, address common.Address) (tbtc.FrostDrainPolicy, error) {
	var out tbtc.FrostDrainPolicy
	header, err := backend.HeaderByNumber(ctx, nil)
	if err != nil {
		return out, err
	}
	bridge, err := frostbridgeabi.NewFrostBridge(address, backend)
	if err != nil {
		return out, err
	}
	opts := &bind.CallOpts{Context: ctx, BlockNumber: header.Number}
	if out.CreationMode, err = bridge.FrostCreationMode(opts); err != nil {
		return out, err
	}
	counters, err := bridge.FrostMovementCounters(opts, [32]byte{})
	if err != nil {
		return out, err
	}
	out.Enabled, out.EligibleCount = counters.DrainEnabled, counters.EligibleTargets
	if !out.Enabled {
		return out, nil
	}
	end := header.Number.Uint64()
	it, err := bridge.FilterFrostWalletReady(&bind.FilterOpts{Context: ctx, Start: 0, End: &end}, nil)
	if err != nil {
		return out, err
	}
	defer it.Close()
	seen := make(map[[32]byte]bool)
	for it.Next() {
		id := it.Event.WalletId
		if it.Event.Raw.Removed || seen[id] {
			continue
		}
		seen[id] = true
		label := movement.Label(id)
		raw, err := bridge.WalletDescriptor(opts, label)
		if err != nil {
			return out, err
		}
		w, err := decodeWallet(raw)
		if err != nil {
			return out, err
		}
		wallet := movement.Wallet{ID: w.Id, State: movement.State(w.State), Quarantined: w.Quarantined, CapabilityVersion: w.CapabilityVersion}
		if w.Id == id && wallet.Eligible() {
			out.Targets = append(out.Targets, label)
		}
	}
	return out, it.Error()
}

func (tc *TbtcChain) FrostDrainPolicy() (tbtc.FrostDrainPolicy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return ReadFrostDrainPolicy(ctx, tc.client, tc.frostBridgeAddress)
}

var _ tbtc.FrostDrainResolver = (*TbtcChain)(nil)

func (tc *TbtcChain) MovingFundsNow() (time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	header, err := tc.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(header.Time), 0), nil
}
