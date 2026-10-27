package ethereum

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	commonEthereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	chainutil "github.com/keep-network/keep-common/pkg/chain/ethereum/ethutil"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

func configuredFrostBridge(config commonEthereum.Config, bridge common.Address, backend bind.ContractCaller) (*frostbridgeabi.FrostBridgeCaller, common.Address, error) {
	address, err := config.ContractAddress(FrostBridgeContractName)
	if errors.Is(err, commonEthereum.ErrAddressNotConfigured) {
		return nil, common.Address{}, nil
	}
	if err != nil {
		return nil, common.Address{}, err
	}
	if address != bridge || address == (common.Address{}) {
		return nil, common.Address{}, fmt.Errorf("FROST extension must use the configured Bridge proxy")
	}
	caller, err := frostbridgeabi.NewFrostBridgeCaller(address, backend)
	return caller, address, err
}

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
	legacy, err := legacyabi.NewBridgeCaller(address, backend)
	if err != nil {
		return out, err
	}
	parameters, err := legacy.MovingFundsParameters(opts)
	if err != nil {
		return out, err
	}
	out.DustThreshold = parameters.MovingFundsDustThreshold
	if out.Enabled {
		controller, err := bridge.FrostExposureController(opts)
		if err != nil {
			return out, err
		}
		if controller == (common.Address{}) {
			return out, errors.New("FROST exposure controller missing")
		}
		// The controller is immutable and read at the same block as policy.
		method, _ := abi.JSON(strings.NewReader(`[{"name":"contained","type":"function","inputs":[],"outputs":[{"type":"bool"}]},{"name":"remainingCustody","type":"function","inputs":[],"outputs":[{"type":"uint64"}]}]`))
		var result []interface{}
		bound := bind.NewBoundContract(controller, method, backend, nil, nil)
		if err := bound.Call(opts, &result, "contained"); err != nil {
			return out, err
		}
		out.Contained = *abi.ConvertType(result[0], new(bool)).(*bool)
		result = nil
		if err := bound.Call(opts, &result, "remainingCustody"); err != nil {
			return out, err
		}
		out.RemainingCustody = *abi.ConvertType(result[0], new(uint64)).(*uint64)

	}
	if !out.Enabled || out.Contained {
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
	if tc.frostBridge == nil {
		return tbtc.FrostDrainPolicy{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return ReadFrostDrainPolicy(ctx, tc.client, tc.frostBridgeAddress)
}

var _ tbtc.FrostDrainResolver = (*TbtcChain)(nil)

func (tc *TbtcChain) MovingFundsNow() (time.Time, error) {
	if tc.frostBridge == nil {
		return time.Now(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	header, err := tc.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(header.Time), 0), nil
}

// ResetLegacyMovingFundsTimeout submits the actual recovery transition. The
// additive extension selector verifies the current main, including partial
// budget insufficiency; pre-extension hosts keep the original Bridge selector.
func (tc *TbtcChain) ResetLegacyMovingFundsTimeout(source [20]byte, main *bitcoin.UnspentTransactionOutput) error {
	if tc.frostBridge == nil {
		_, err := tc.bridge.ResetMovingFundsTimeout(source)
		return err
	}
	if main == nil || main.Outpoint == nil || main.Value <= 0 {
		return errors.New("legacy recovery needs current main UTXO")
	}
	// Same submission/nonce/mining pattern as the generated legacy wrappers.
	// No receipt wait runs while holding the shared transaction mutex.
	tc.transactionMutex.Lock()
	defer tc.transactionMutex.Unlock()
	opts, err := bind.NewKeyedTransactorWithChainID(tc.key.PrivateKey, tc.chainID)
	if err != nil {
		return err
	}
	nonce, err := tc.nonceManager.CurrentNonce()
	if err != nil {
		return err
	}
	opts.Nonce = new(big.Int).SetUint64(nonce)
	contract, err := frostbridgeabi.NewFrostBridgeTransactor(tc.frostBridgeAddress, tc.client)
	if err != nil {
		return err
	}
	metadata, err := frostbridgeabi.FrostBridgeMetaData.GetAbi()
	if err != nil {
		return err
	}
	resolver := chainutil.NewErrorResolver(tc.client, metadata, &tc.frostBridgeAddress)
	utxo := frostbridgeabi.BitcoinTxUTXO{TxHash: [32]byte(main.Outpoint.TransactionHash), TxOutputIndex: main.Outpoint.OutputIndex, TxOutputValue: uint64(main.Value)}
	submit := func(options *bind.TransactOpts) (*types.Transaction, error) {
		tx, err := contract.ResetLegacyMovingFundsTimeout(options, source, utxo)
		if err != nil {
			return nil, resolver.ResolveError(err, opts.From, nil, "resetLegacyMovingFundsTimeout", source, utxo)
		}
		return tx, nil
	}
	tx, err := submit(opts)
	if err != nil {
		return err
	}
	tc.nonceManager.IncrementNonce()
	go tc.miningWaiter.ForceMining(tx, opts, submit)
	return nil
}
