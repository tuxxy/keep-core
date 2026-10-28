package ethereum

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
)

// depositWords is abi.encode(FrostFundingState.Deposit): the twelve Terms
// words and eleven record words, all static.
const depositWords = 23

// Deposit reads the C-05 deposit record of one outpoint key at block at.
func (c *FrostRedemptionChain) Deposit(ctx context.Context, at uint64, key [32]byte) (deposit.Record, error) {
	raw, err := c.bridge.FrostDeposit(c.at(ctx, at), key)
	if err != nil {
		return deposit.Record{}, err
	}
	if len(raw) != depositWords*32 {
		return deposit.Record{}, fmt.Errorf("deposit record length %d", len(raw))
	}
	w := func(i int) []byte { return raw[i*32 : (i+1)*32] }
	b32 := func(i int) (out [32]byte) { copy(out[:], w(i)); return out }
	addr := func(i int) (out [20]byte) { copy(out[:], w(i)[12:]); return out }
	u64 := func(i int) (uint64, error) {
		v := new(big.Int).SetBytes(w(i))
		if !v.IsUint64() {
			return 0, fmt.Errorf("deposit word %d width", i)
		}
		return v.Uint64(), nil
	}
	var r deposit.Record
	r.Terms.ChainID = new(big.Int).SetBytes(w(0))
	r.Terms.Bridge = addr(1)
	r.Terms.BitcoinGenesis = b32(2)
	r.Terms.WalletID = b32(3)
	r.Terms.DescriptorHash = b32(4)
	r.Terms.Depositor = addr(5)
	r.Terms.Vault = addr(6)
	copy(r.Terms.Blinding[:], w(7)[:8])
	if r.Terms.Amount, err = u64(8); err != nil {
		return deposit.Record{}, err
	}
	r.Terms.RefundKey = b32(9)
	r.Terms.RefundLocktime = binary.BigEndian.Uint32(w(10)[28:])
	r.Terms.ExtraData = b32(11)
	r.Receipt = b32(12)
	r.FundingTransactionHash = b32(13)
	r.FundingOutputIndex = binary.BigEndian.Uint32(w(14)[28:])
	r.DepositOutputKey = b32(15)
	r.WalletKey = b32(16)
	r.MerkleRoot = b32(17)
	r.OutputParity = w(18)[31]
	for i, dst := range []*uint64{&r.TreasuryFee, &r.MaximumSweepFee, &r.RevealedAt, &r.SweptAt} {
		if *dst, err = u64(19 + i); err != nil {
			return deposit.Record{}, err
		}
	}
	if r.RevealedAt == 0 {
		return deposit.Record{}, deposit.ErrNotFound
	}
	return r, nil
}

func reveal(e *frostbridgeabi.FrostBridgeFrostDepositRevealed) deposit.Reveal {
	return deposit.Reveal{Outpoint: e.Outpoint, WalletID: e.WalletId, Block: e.Raw.BlockNumber, BlockHash: e.Raw.BlockHash}
}

// Reveals lists the reveal logs of a wallet in [from, to].
func (c *FrostRedemptionChain) Reveals(ctx context.Context, wallet [32]byte, from, to uint64) ([]deposit.Reveal, error) {
	it, err := c.bridge.FilterFrostDepositRevealed(c.filter(ctx, from, to), nil, [][32]byte{wallet}, nil)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []deposit.Reveal
	for it.Next() {
		if !it.Event.Raw.Removed {
			out = append(out, reveal(it.Event))
		}
	}
	return out, it.Error()
}

// Reveal returns the single reveal log of one deposit. The caller compares its
// block hash with the canonical header.
func (c *FrostRedemptionChain) Reveal(ctx context.Context, key [32]byte) (deposit.Reveal, error) {
	it, err := c.bridge.FilterFrostDepositRevealed(&bind.FilterOpts{Context: ctx}, [][32]byte{key}, nil, nil)
	if err != nil {
		return deposit.Reveal{}, err
	}
	defer it.Close()
	var found []deposit.Reveal
	for it.Next() {
		if !it.Event.Raw.Removed {
			found = append(found, reveal(it.Event))
		}
	}
	if err = it.Error(); err != nil {
		return deposit.Reveal{}, err
	}
	if len(found) != 1 {
		return deposit.Reveal{}, fmt.Errorf("%w: %d reveal logs", deposit.ErrNotFound, len(found))
	}
	return found[0], nil
}

func sweepRequest(p deposit.SweepProof) frostbridgeabi.FrostDepositSweepSweep {
	return frostbridgeabi.FrostDepositSweepSweep{WalletId: p.WalletID, Transaction: info(p.Transaction), Proof: proof(p.Proof), IncludeMain: p.IncludeMain, Vault: common.Address(p.Vault)}
}

// SubmitSweepProof proves an ordinary C-05 sweep (SPV maintainer only).
func (c *FrostRedemptionChain) SubmitSweepProof(ctx context.Context, p deposit.SweepProof) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostDepositSweepProof(o, sweepRequest(p))
	})
}

// SubmitSweepResolutionProof proves a sweep through the C-04 resolution route.
func (c *FrostRedemptionChain) SubmitSweepResolutionProof(ctx context.Context, p deposit.SweepProof, a deposit.ResolutionAuthorization, seats []uint16, signatures []byte) error {
	resolution := frostbridgeabi.FrostDepositSweepResolution{Sweep: sweepRequest(p), Authorization: frostbridgeabi.FrostSweepResolutionCertificateAuthorization{ReservationId: a.ReservationID, TransactionHash: a.TransactionHash, Vault: common.Address(a.Vault), ValidUntilBlock: a.ValidUntilBlock}, Seats: seats, Signatures: signatures}
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostSweepResolutionProof(o, resolution)
	})
}

var _ deposit.Chain = (*FrostRedemptionChain)(nil)
