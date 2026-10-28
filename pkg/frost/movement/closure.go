package movement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// ErrObligations refuses closure while any closure obligation is open.
var ErrObligations = errors.New("FROST wallet has open closure obligations")

// Closure is the host side of P-K07-CLOSE-ONLY-WHEN-CLEAR and
// P-K07-SIGNER-RETENTION. It treats a wallet as closable only when the C-09
// closure ledger is empty at the finalized head and again at the current head
// right before it notifies the contract, which rechecks at the transition.
// Archiving records the closure locally and deletes nothing: the completed key
// and every signing record stay loadable (VD-08).
type Closure struct {
	Chain    Chain
	Policy   Policy
	Journal  redemption.RecordJournal
	boundary func(string) error
}

func (c Closure) step(name string) error {
	if c.boundary != nil {
		return c.boundary(name)
	}
	return nil
}

// Archived is the local archive record of a closed wallet.
type Archived struct {
	Wallet [32]byte
	Block  uint64
}

func archiveID(wallet [32]byte) [32]byte {
	return sha256.Sum256(append([]byte("keep-core/frost/wallet-archived/v1/"), wallet[:]...))
}

// Obligations returns the closure ledger at the finalized head.
func (c Closure) Obligations(ctx context.Context, wallet [32]byte) (uint64, error) {
	if err := c.step("before-closure-read"); err != nil {
		return 0, err
	}
	head, err := c.Chain.Head(ctx)
	if err != nil {
		return 0, err
	}
	if head.Number < c.Policy.FinalityBlocks {
		return 0, errors.New("chain shorter than finality")
	}
	open, err := c.Chain.Obligations(ctx, head.Number-c.Policy.FinalityBlocks, wallet)
	if err != nil {
		return 0, err
	}
	return open, c.step("closure-ledger-seen")
}

// Close finalizes a Closing or Terminated wallet whose ledger is empty and
// archives it locally. A late arrival between the finalized read and the
// notification refuses; the contract refuses one after it.
func (c Closure) Close(ctx context.Context, wallet [32]byte) error {
	open, err := c.Obligations(ctx, wallet)
	if err != nil {
		return err
	}
	if open != 0 {
		return fmt.Errorf("%w: %08b", ErrObligations, open)
	}
	if err = c.step("before-closure-notify"); err != nil {
		return err
	}
	head, err := c.Chain.Head(ctx)
	if err != nil {
		return err
	}
	if open, err = c.Chain.Obligations(ctx, head.Number, wallet); err != nil {
		return err
	}
	if open != 0 {
		return fmt.Errorf("%w at the current head: %08b", ErrObligations, open)
	}
	w, err := c.Chain.Wallet(ctx, head.Number, Label(wallet))
	if err != nil {
		return err
	}
	if w.State != StateClosed {
		if err = c.Chain.FinalizeClosing(ctx, wallet); err != nil {
			// The reply may be lost after the transaction was mined.
			again, readErr := c.Chain.Head(ctx)
			if readErr != nil {
				return err
			}
			if w, readErr = c.Chain.Wallet(ctx, again.Number, Label(wallet)); readErr != nil || w.State != StateClosed {
				return fmt.Errorf("finalize closing: %w", err)
			}
		}
	}
	if err = c.step("closure-notified"); err != nil {
		return err
	}
	if err = c.step("before-archive"); err != nil {
		return err
	}
	raw, err := json.Marshal(Archived{Wallet: wallet, Block: head.Number})
	if err != nil {
		return err
	}
	if err = c.Journal.RetainSigningRecord(ctx, archiveID(wallet), raw); err != nil {
		if existing, readErr := c.Journal.ReadSigningRecord(ctx, archiveID(wallet)); readErr != nil || len(existing) == 0 {
			return err
		}
	}
	return c.step("archived")
}

// IsArchived reports whether this host archived the wallet.
func IsArchived(ctx context.Context, journal redemption.RecordJournal, wallet [32]byte) (bool, error) {
	_, err := journal.ReadSigningRecord(ctx, archiveID(wallet))
	if errors.Is(err, store.ErrMissing) {
		return false, nil
	}
	return err == nil, err
}
