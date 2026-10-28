package ethereum

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// FrostChain is a separate, explicitly constructed local chain view. Creating
// the legacy TbtcChain does not create it or enable FROST dispatch.
type FrostChain struct {
	client                *ethclient.Client
	registry              *binding.FrostWalletRegistry
	validator             *binding.FrostDkgValidator
	address, bridge, pool common.Address
	key                   *ecdsa.PrivateKey
	chainID               *big.Int
	txMu                  sync.Mutex
}

func NewFrostChain(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, registry, bridge common.Address) (*FrostChain, error) {
	if client == nil || key == nil || registry == (common.Address{}) || bridge == (common.Address{}) {
		return nil, errors.New("invalid FROST chain configuration")
	}
	r, err := binding.NewFrostWalletRegistry(registry, client)
	if err != nil {
		return nil, err
	}
	opts := &bind.CallOpts{Context: ctx}
	owner, err := r.WalletOwner(opts)
	if err != nil {
		return nil, err
	}
	if owner != bridge {
		return nil, errors.New("FROST registry Bridge mismatch")
	}
	bridgeABI, _ := abi.JSON(strings.NewReader(`[{"type":"function","name":"frostRegistry","inputs":[],"outputs":[{"type":"address"}],"stateMutability":"view"}]`))
	var result []interface{}
	if err = bind.NewBoundContract(bridge, bridgeABI, client, client, client).Call(opts, &result, "frostRegistry"); err != nil {
		return nil, err
	}
	if len(result) != 1 || result[0].(common.Address) != registry {
		return nil, errors.New("Bridge FROST registry mismatch")
	}
	address, err := r.Validator(opts)
	if err != nil {
		return nil, err
	}
	v, err := binding.NewFrostDkgValidator(address, client)
	if err != nil {
		return nil, err
	}
	pool, err := r.SortitionPool(opts)
	if err != nil {
		return nil, err
	}
	chain, err := client.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	return &FrostChain{client: client, registry: r, validator: v, address: registry, bridge: bridge, pool: pool, key: key, chainID: chain}, nil
}
func (c *FrostChain) Parameters(ctx context.Context) (dkg.Parameters, error) {
	p := dkg.Parameters{ChainID: new(big.Int).Set(c.chainID), Registry: c.address, Pool: c.pool, Bridge: c.bridge}
	o := &bind.CallOpts{Context: ctx}
	var err error
	if p.GroupSize, err = c.validator.GroupSize(o); err != nil {
		return p, err
	}
	if p.Threshold, err = c.validator.Threshold(o); err != nil {
		return p, err
	}
	if p.ReadySeats, err = c.validator.ReadySeats(o); err != nil {
		return p, err
	}
	if p.FalseReadySeats, err = c.validator.FalseReadySeats(o); err != nil {
		return p, err
	}
	if p.UnavailableSeats, err = c.validator.UnavailableSeats(o); err != nil {
		return p, err
	}
	if p.Profile, err = c.validator.PROFILE(o); err != nil {
		return p, err
	}
	p.ChallengeBlocks, err = c.registry.ChallengePeriod(o)
	return p, err
}
func (c *FrostChain) Selection(ctx context.Context, epoch uint64) (dkg.Selection, error) {
	m, o, err := c.registry.Selection(&bind.CallOpts{Context: ctx}, epoch)
	return dkg.Selection{Epoch: epoch, Members: m, Operators: o}, err
}

// A provider can return a newer snapshot while the requested head advances.
// Never accept that mixed view. Retry only this read mismatch, within a bound;
// an observed reorg or any other failure still goes directly to the caller.
var errFrostSnapshotBlock = errors.New("FROST RPC snapshot block changed")

func readStableFrostView(ctx context.Context, read func() (dkg.View, error)) (dkg.View, error) {
	var view dkg.View
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if ctx.Err() != nil {
			return view, ctx.Err()
		}
		view, err = read()
		if !errors.Is(err, errFrostSnapshotBlock) {
			return view, err
		}
	}
	return view, fmt.Errorf("%w: no coherent RPC snapshot after five reads: %v", dkg.ErrQuarantined, err)
}

func (c *FrostChain) View(ctx context.Context, epoch uint64) (dkg.View, error) {
	return readStableFrostView(ctx, func() (dkg.View, error) { return c.readFrostView(ctx, epoch) })
}

func (c *FrostChain) readFrostView(ctx context.Context, epoch uint64) (dkg.View, error) {
	var v dkg.View
	head, err := c.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return v, err
	}
	v.Head = head.Number.Uint64()
	v.HeadHash = head.Hash()
	o := &bind.CallOpts{Context: ctx, BlockHash: head.Hash()}
	// The state machine and its payload must come from one EVM read. In
	// particular, approval deletes Submitted while installing Approved.
	snapshot, err := c.registry.EpochView(o, epoch)
	if err != nil {
		return v, err
	}
	if snapshot.BlockNumber != v.Head {
		return v, fmt.Errorf("%w: returned %d, requested %d", errFrostSnapshotBlock, snapshot.BlockNumber, v.Head)
	}
	v.State, v.Epoch = snapshot.State, snapshot.Epoch
	v.SubmittedAt, v.ResultDeadline = snapshot.SubmittedAt, snapshot.ResultDeadline
	v.SubmittedHash = snapshot.SubmittedHash
	v.ApprovedID, v.Approved = snapshot.ApprovedId, snapshot.Approved
	if v.State == 3 {
		a, _ := binding.FrostWalletRegistryMetaData.GetAbi()
		values, x := a.Methods["submitDkgResult"].Inputs.Unpack(snapshot.Submitted)
		if x != nil || len(values) != 1 {
			return v, fmt.Errorf("%w: malformed submitted result", dkg.ErrQuarantined)
		}
		result := *abi.ConvertType(values[0], new(dkg.Result)).(*dkg.Result)
		hash, x := dkg.ResultHash(result)
		if x != nil || hash != v.SubmittedHash {
			return v, fmt.Errorf("%w: submitted result hash mismatch", dkg.ErrQuarantined)
		}
		v.Submitted = &result
	}
	if v.ApprovedID != ([32]byte{}) {
		h, x := c.client.HeaderByNumber(ctx, new(big.Int).SetUint64(v.Approved.ApprovalBlock))
		if x != nil {
			return v, x
		}
		v.ApprovalHash = h.Hash()
		// Dedicated address/epoch/hash/identity filter. A storage read alone does
		// not establish correspondence with the canonical approval event.
		a, _ := binding.FrostWalletRegistryMetaData.GetAbi()
		block := new(big.Int).SetUint64(v.Approved.ApprovalBlock)
		logs, x := c.client.FilterLogs(ctx, geth.FilterQuery{FromBlock: block, ToBlock: block, Addresses: []common.Address{c.address}, Topics: [][]common.Hash{{a.Events["ResultApproved"].ID}, {common.BigToHash(new(big.Int).SetUint64(epoch))}, {v.Approved.ResultHash}, {v.ApprovedID}}})
		if x != nil {
			return v, x
		}
		if len(logs) != 1 || logs[0].Removed || logs[0].BlockHash != v.ApprovalHash {
			return v, fmt.Errorf("%w: approval log count=%d or block hash mismatch", dkg.ErrQuarantined, len(logs))
		}
		event, x := c.registry.ParseResultApproved(logs[0])
		if x != nil || event.DescriptorHash != v.Approved.DescriptorHash {
			return v, fmt.Errorf("%w: approval event descriptor mismatch", dkg.ErrQuarantined)
		}
	}
	again, err := c.client.HeaderByNumber(ctx, head.Number)
	if err != nil {
		return v, err
	}
	if again.Hash() != v.HeadHash {
		return v, fmt.Errorf("%w: chain head changed during read", dkg.ErrQuarantined)
	}
	return v, nil
}
func (c *FrostChain) ResultDigest(ctx context.Context, epoch uint64, r dkg.Result) ([32]byte, error) {
	return c.validator.ResultDigest(&bind.CallOpts{Context: ctx}, c.address, c.pool, epoch, r)
}
func (c *FrostChain) ReadinessDigest(ctx context.Context, w dkg.Wallet, seat uint16, reference [32]byte) ([32]byte, error) {
	return c.validator.ReadinessDigest(&bind.CallOpts{Context: ctx}, dkg.WalletID(w.Descriptor.OutputKey), w.DescriptorHash, c.address, w.Descriptor.Epoch, 1, 1, seat, reference)
}
func (c *FrostChain) Validate(ctx context.Context, epoch uint64, r dkg.Result) (bool, error) {
	return c.registry.IsResultValid(&bind.CallOpts{Context: ctx}, r, epoch)
}
func (c *FrostChain) transact(ctx context.Context, send func(*bind.TransactOpts) (*types.Transaction, error)) error {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	opts, err := bind.NewKeyedTransactorWithChainID(c.key, c.chainID)
	if err != nil {
		return err
	}
	opts.Context = ctx
	tx, err := send(opts)
	if err != nil {
		return err
	}
	receipt, err := bind.WaitMined(ctx, c.client, tx)
	if err != nil {
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return errors.New("FROST transaction reverted")
	}
	return nil
}
func (c *FrostChain) Submit(ctx context.Context, r dkg.Result) error {
	return c.transact(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.registry.SubmitDkgResult(o, r) })
}
func (c *FrostChain) Challenge(ctx context.Context) error {
	return c.transact(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.registry.ChallengeDkgResult(o) })
}
func (c *FrostChain) Approve(ctx context.Context) error {
	return c.transact(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.registry.ApproveDkgResult(o) })
}
func (c *FrostChain) Ready(ctx context.Context, id [32]byte, seats []uint16, refs [][32]byte, sigs []byte) error {
	return c.transact(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.registry.SubmitReadinessV1(o, id, seats, refs, sigs)
	})
}
func (c *FrostChain) Expire(ctx context.Context, id [32]byte) error {
	return c.transact(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.registry.ExpireWallet(o, id) })
}

func (c *FrostChain) ExpireDKG(ctx context.Context) error {
	return c.transact(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.registry.ExpireDkg(o) })
}
