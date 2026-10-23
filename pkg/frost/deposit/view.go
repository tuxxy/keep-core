package deposit

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// Refusals of a deposit input. Each one stops a sweep before any signature.
var (
	ErrNotFinal      = errors.New("FROST deposit reveal is not finalized")
	ErrSwept         = errors.New("FROST deposit already swept")
	ErrRefundMargin  = errors.New("FROST deposit at or past its refund safety margin")
	ErrFundingSpent  = errors.New("FROST deposit funding output is spent")
	ErrForeignWallet = errors.New("FROST deposit belongs to another wallet")
	ErrUnreserved    = errors.New("FROST deposit has no exposure reservation")
)

// Clock reads Bitcoin's current median-time-past (BIP113): the clock a CLTV
// timestamp refund is checked against. It is an owned chain assumption
// (A-K06-MTP); the local fixture reads Bitcoin Core.
type Clock interface {
	MedianTimePast(ctx context.Context) (uint64, error)
}

// Policy holds the K-06 timing inputs. Both are D-09 decisions; the local
// fixture uses test values marked OPEN.
type Policy struct {
	// FinalityBlocks is the Ethereum depth at which a reveal is final.
	FinalityBlocks uint64
	// RefundMargin is how many seconds before its refund locktime a deposit
	// stops being swept: signing starts only while MTP + margin < locktime.
	RefundMargin uint64
}

// Deposit is one C-05 record with its reveal log, key and C-10 reservation.
type Deposit struct {
	Key      [32]byte
	Record   Record
	Reveal   Reveal
	Reserved bool
}

// Snapshot is one internally consistent read: every chain value at Head,
// checked canonical after the reads, and Bitcoin median-time-past.
type Snapshot struct {
	Head           redemption.Head
	Deposits       []Deposit
	Custody        redemption.Custody
	MedianTimePast uint64
}

// View reads deposits for planning and authorization.
type View struct {
	Chain    Reader
	Clock    Clock
	Policy   Policy
	boundary func(string) error
}

func (v *View) step(name string) error {
	if v.boundary != nil {
		return v.boundary(name)
	}
	return nil
}

// Read returns the named deposits and the wallet custody at the current head.
// A reveal log on a block that is no longer canonical is refused.
func (v *View) Read(ctx context.Context, wallet [32]byte, keys [][32]byte) (Snapshot, error) {
	if v.Chain == nil || v.Clock == nil || v.Policy.FinalityBlocks == 0 {
		return Snapshot{}, errors.New("incomplete deposit view")
	}
	if err := v.step("before-deposit-read"); err != nil {
		return Snapshot{}, err
	}
	head, err := v.Chain.Head(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Head: head}
	for _, key := range keys {
		record, err := v.Chain.Deposit(ctx, head.Number, key)
		if err != nil {
			return Snapshot{}, fmt.Errorf("deposit %x: %w", key, err)
		}
		reveal, err := v.Chain.Reveal(ctx, key)
		if err != nil {
			return Snapshot{}, fmt.Errorf("reveal %x: %w", key, err)
		}
		if reveal.Block > head.Number {
			return Snapshot{}, redemption.ErrInconsistentRead
		}
		canonical, err := v.Chain.HeaderHash(ctx, reveal.Block)
		if err != nil {
			return Snapshot{}, err
		}
		if canonical != reveal.BlockHash {
			return Snapshot{}, fmt.Errorf("%w: reveal block %d is not canonical", ErrNotFinal, reveal.Block)
		}
		reserved, err := v.finallyReserved(ctx, head.Number, key)
		if err != nil {
			return Snapshot{}, err
		}
		s.Deposits = append(s.Deposits, Deposit{Key: key, Record: record, Reveal: reveal, Reserved: reserved})
	}
	if s.Custody, err = v.Chain.Custody(ctx, head.Number, wallet); err != nil {
		return Snapshot{}, err
	}
	again, err := v.Chain.HeaderHash(ctx, head.Number)
	if err != nil {
		return Snapshot{}, err
	}
	if again != head.Hash {
		return Snapshot{}, redemption.ErrInconsistentRead
	}
	if err = v.step("deposits-seen"); err != nil {
		return Snapshot{}, err
	}
	if s.MedianTimePast, err = v.Clock.MedianTimePast(ctx); err != nil {
		return Snapshot{}, err
	}
	if err = v.step("refund-margin-seen"); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// finallyReserved reports a C-10 reservation that is open at block at and
// already existed FinalityBlocks earlier, so a shallow reorganization cannot
// remove it after the sweep is broadcast (review of #20). The caller checks
// that both reads lie on one canonical chain.
func (v *View) finallyReserved(ctx context.Context, at uint64, key [32]byte) (bool, error) {
	return reservedAtDepth(ctx, v.Chain, at, v.Policy.FinalityBlocks, key)
}

func reservedAtDepth(ctx context.Context, chain Reader, at, depth uint64, key [32]byte) (bool, error) {
	if at < depth {
		return false, nil
	}
	for _, block := range []uint64{at - depth, at} {
		if ok, err := chain.Reserved(ctx, block, key); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// Sweepable checks one deposit of the snapshot for wallet: revealed for this
// wallet at a finalized canonical block, not swept, reserved against the C-10
// budget at a finalized depth, and before its margin.
func (v *View) Sweepable(s Snapshot, d Deposit, wallet [32]byte) error {
	r := d.Record
	if r.RevealedAt == 0 || d.Reveal.Outpoint != d.Key {
		return ErrNotFound
	}
	if r.Terms.WalletID != wallet || d.Reveal.WalletID != wallet {
		return ErrForeignWallet
	}
	if s.Head.Number < d.Reveal.Block || s.Head.Number-d.Reveal.Block < v.Policy.FinalityBlocks {
		return ErrNotFinal
	}
	if r.SweptAt != 0 {
		return ErrSwept
	}
	if !d.Reserved {
		return ErrUnreserved
	}
	return v.Policy.BeforeMargin(s.MedianTimePast, r)
}

// BeforeMargin checks that a deposit is still before its refund safety
// margin at Bitcoin median-time-past mtp: MTP + margin < locktime.
func (p Policy) BeforeMargin(mtp uint64, r Record) error {
	if mtp+p.RefundMargin >= uint64(r.Terms.RefundLocktime) {
		return fmt.Errorf("%w: median-time-past %d, margin %d, locktime %d", ErrRefundMargin, mtp, p.RefundMargin, r.Terms.RefundLocktime)
	}
	return nil
}
