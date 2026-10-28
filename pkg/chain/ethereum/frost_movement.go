package ethereum

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// walletArguments decodes walletDescriptor(label): abi.encode of the
// FrostWallets.Wallet struct, whose descriptor is the registry tuple.
func walletArguments() (abi.Arguments, error) {
	a, err := binding.FrostWalletRegistryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	descriptor := *a.Methods["wallet"].Outputs[0].Type.TupleElems[0]
	var components []abi.ArgumentMarshaling
	for i, name := range descriptor.TupleRawNames {
		components = append(components, marshaling(name, descriptor.TupleElems[i]))
	}
	wallet, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "descriptor", Type: "tuple", Components: components},
		{Name: "id", Type: "bytes32"},
		{Name: "descriptorHash", Type: "bytes32"},
		{Name: "deadline", Type: "uint64"},
		{Name: "state", Type: "uint8"},
		{Name: "quarantined", Type: "bool"},
		{Name: "generation", Type: "uint32"},
		{Name: "capabilityVersion", Type: "uint32"},
	})
	if err != nil {
		return nil, err
	}
	return abi.Arguments{{Type: wallet}}, nil
}

func marshaling(name string, t *abi.Type) abi.ArgumentMarshaling {
	return abi.ArgumentMarshaling{Name: name, Type: t.String()}
}

type walletABI struct {
	Descriptor        dkg.Descriptor
	Id                [32]byte
	DescriptorHash    [32]byte
	Deadline          uint64
	State             uint8
	Quarantined       bool
	Generation        uint32
	CapabilityVersion uint32
}

func decodeWallet(raw []byte) (*walletABI, error) {
	args, err := walletArguments()
	if err != nil {
		return nil, err
	}
	values, err := args.Unpack(raw)
	if err != nil {
		return nil, fmt.Errorf("wallet record: %w", err)
	}
	if len(values) != 1 {
		return nil, errors.New("wallet record shape")
	}
	w, ok := abi.ConvertType(values[0], new(walletABI)).(*walletABI)
	if !ok {
		return nil, errors.New("wallet record shape")
	}
	return w, nil
}

// Wallet reads the C-01 FROST wallet record of label at block at.
func (c *FrostRedemptionChain) Wallet(ctx context.Context, at uint64, label [20]byte) (movement.Wallet, error) {
	raw, err := c.bridge.WalletDescriptor(c.at(ctx, at), label)
	if err != nil {
		return movement.Wallet{}, err
	}
	w, err := decodeWallet(raw)
	if err != nil {
		return movement.Wallet{}, err
	}
	if w.Id == ([32]byte{}) {
		return movement.Wallet{}, movement.ErrNotFound
	}
	return movement.Wallet{Descriptor: w.Descriptor, ID: w.Id, DescriptorHash: w.DescriptorHash, State: movement.State(w.State), Quarantined: w.Quarantined, CapabilityVersion: w.CapabilityVersion}, nil
}

// ReadyWallets lists the IDs of FrostWalletReady logs in [from, to].
func (c *FrostRedemptionChain) ReadyWallets(ctx context.Context, from, to uint64) ([][32]byte, error) {
	it, err := c.bridge.FilterFrostWalletReady(c.filter(ctx, from, to), nil)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out [][32]byte
	for it.Next() {
		if !it.Event.Raw.Removed {
			out = append(out, it.Event.WalletId)
		}
	}
	return out, it.Error()
}

func (c *FrostRedemptionChain) Commitment(ctx context.Context, at uint64, wallet [32]byte, period uint32) (movement.Commitment, error) {
	r, err := c.bridge.FrostMovingFundsCommitment(c.at(ctx, at), wallet, period)
	if err != nil {
		return movement.Commitment{}, err
	}
	out := movement.Commitment{MainOutpoint: r.MainOutpoint, CommittedAt: r.CommittedAt, ProvenAt: r.ProvenAt, Escalated: r.Escalated, MaxFee: r.MaxFee, DescriptorHashes: r.DescriptorHashes}
	out.Targets = append(out.Targets, r.Targets...)
	return out, nil
}

// CommitmentLog returns the single FrostMovingFundsCommitted log of a period.
func (c *FrostRedemptionChain) CommitmentLog(ctx context.Context, wallet [32]byte, period uint32) (movement.CommitmentLog, error) {
	it, err := c.bridge.FilterFrostMovingFundsCommitted(&bind.FilterOpts{Context: ctx}, [][32]byte{wallet})
	if err != nil {
		return movement.CommitmentLog{}, err
	}
	defer it.Close()
	var found []movement.CommitmentLog
	for it.Next() {
		if !it.Event.Raw.Removed && it.Event.Period == period {
			found = append(found, movement.CommitmentLog{Log: movement.Log{Block: it.Event.Raw.BlockNumber, BlockHash: it.Event.Raw.BlockHash}, Wallet: it.Event.WalletId, Period: it.Event.Period})
		}
	}
	if err = it.Error(); err != nil {
		return movement.CommitmentLog{}, err
	}
	if len(found) != 1 {
		return movement.CommitmentLog{}, fmt.Errorf("%w: %d commitment logs", movement.ErrNotFound, len(found))
	}
	return found[0], nil
}

func (c *FrostRedemptionChain) Lifecycle(ctx context.Context, at uint64, wallet [32]byte) (movement.Lifecycle, error) {
	r, err := c.bridge.FrostWalletLifecycle(c.at(ctx, at), wallet)
	if err != nil {
		return movement.Lifecycle{}, err
	}
	return movement.Lifecycle{Period: r.Period, MovingFundsRequestedAt: r.MovingFundsRequestedAt, ClosingStartedAt: r.ClosingStartedAt, Dust: r.Dust, ReadyAt: r.ReadyAt, TerminatedAt: r.TerminatedAt}, nil
}

func (c *FrostRedemptionChain) Incoming(ctx context.Context, at uint64, outpoint [32]byte) (movement.Incoming, error) {
	r, err := c.bridge.FrostIncoming(c.at(ctx, at), outpoint)
	if err != nil {
		return movement.Incoming{}, err
	}
	if r.State == uint8(movement.IncomingUnknown) {
		return movement.Incoming{}, movement.ErrNotFound
	}
	return movement.Incoming{Target: r.Target, Source: r.Source, Value: r.Value, CreatedAt: r.CreatedAt, State: movement.IncomingState(r.State)}, nil
}

// IncomingLogs lists FrostIncomingRecorded logs of a target in [from, to].
func (c *FrostRedemptionChain) IncomingLogs(ctx context.Context, target [32]byte, from, to uint64) ([]movement.IncomingLog, error) {
	it, err := c.bridge.FilterFrostIncomingRecorded(c.filter(ctx, from, to), nil, [][32]byte{target})
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []movement.IncomingLog
	for it.Next() {
		e := it.Event
		if !e.Raw.Removed {
			out = append(out, movement.IncomingLog{Log: movement.Log{Block: e.Raw.BlockNumber, BlockHash: e.Raw.BlockHash}, Outpoint: e.Outpoint, Target: e.Target, Source: e.Source, Value: e.Value, TransactionHash: e.TransactionHash, OutputIndex: e.OutputIndex})
		}
	}
	return out, it.Error()
}

func (c *FrostRedemptionChain) Counters(ctx context.Context, at uint64, wallet [32]byte) (movement.Counters, error) {
	r, err := c.bridge.FrostMovementCounters(c.at(ctx, at), wallet)
	if err != nil {
		return movement.Counters{}, err
	}
	return movement.Counters{PendingIncoming: r.PendingIncoming, InboundCommitments: r.InboundCommitments, EligibleTargets: r.EligibleTargets, DrainEnabled: r.DrainEnabled}, nil
}

func (c *FrostRedemptionChain) Obligations(ctx context.Context, at uint64, wallet [32]byte) (uint64, error) {
	r, err := c.bridge.FrostClosureObligations(c.at(ctx, at), wallet)
	if err != nil {
		return 0, err
	}
	if !r.IsUint64() {
		return 0, errors.New("closure obligations width")
	}
	return r.Uint64(), nil
}

func (c *FrostRedemptionChain) MovementParameters(ctx context.Context, at uint64) (movement.Parameters, error) {
	m, err := c.legacy.MovingFundsParameters(c.at(ctx, at))
	if err != nil {
		return movement.Parameters{}, err
	}
	w, err := c.legacy.WalletParameters(c.at(ctx, at))
	if err != nil {
		return movement.Parameters{}, err
	}
	return movement.Parameters{MovingFundsTxMaxTotalFee: m.MovingFundsTxMaxTotalFee, MovingFundsDustThreshold: m.MovingFundsDustThreshold, MovedFundsSweepTxMaxTotalFee: m.MovedFundsSweepTxMaxTotalFee, WalletMaxBtcTransfer: w.WalletMaxBtcTransfer}, nil
}

func (c *FrostRedemptionChain) ProvenTransaction(ctx context.Context, at uint64, hash [32]byte) ([32]byte, error) {
	return c.bridge.FrostProvenTransaction(c.at(ctx, at), hash)
}

func (c *FrostRedemptionChain) SubmitCommitment(ctx context.Context, wallet [32]byte, seat uint16, targets [][20]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostMovingFundsCommitment(o, wallet, seat, targets)
	})
}

func (c *FrostRedemptionChain) SubmitMovementProof(ctx context.Context, wallet [32]byte, p movement.Proof) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostMovingFundsProof(o, wallet, info(p.Transaction), proof(p.Proof))
	})
}

func (c *FrostRedemptionChain) SubmitIncomingSweepProof(ctx context.Context, wallet [32]byte, p movement.Proof) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostMovedFundsSweepProof(o, wallet, info(p.Transaction), proof(p.Proof))
	})
}

func (c *FrostRedemptionChain) NotifyCloseable(ctx context.Context, wallet [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.NotifyFrostWalletCloseable(o, wallet)
	})
}

func (c *FrostRedemptionChain) FinalizeClosing(ctx context.Context, wallet [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.FinalizeFrostWalletClosing(o, wallet)
	})
}

func (c *FrostRedemptionChain) NotifyBelowDust(ctx context.Context, wallet [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.NotifyFrostMovingFundsBelowDust(o, wallet)
	})
}

func (c *FrostRedemptionChain) ResetMovingFundsTimeout(ctx context.Context, wallet [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.ResetFrostMovingFundsTimeout(o, wallet)
	})
}

var _ movement.Chain = (*FrostRedemptionChain)(nil)

// FrostTargetOutputKey implements tbtc.FrostTargetResolver: a label the
// Bridge reports as scheme 2 is a FROST wallet, and its output key comes from
// its immutable descriptor.
func (tc *TbtcChain) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	if tc.frostBridge == nil {
		return [32]byte{}, false, nil
	}
	scheme, err := tc.frostBridge.WalletScheme(nil, label)
	if err != nil {
		return [32]byte{}, false, err
	}
	if scheme != 2 {
		return [32]byte{}, false, nil
	}
	raw, err := tc.frostBridge.WalletDescriptor(nil, label)
	if err != nil {
		return [32]byte{}, false, err
	}
	w, err := decodeWallet(raw)
	if err != nil {
		return [32]byte{}, false, err
	}
	return w.Descriptor.OutputKey, true, nil
}

var _ tbtc.FrostTargetResolver = (*TbtcChain)(nil)
