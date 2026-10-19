package enforcement

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Action kinds reported by the defender and the timeout notifier.
const (
	ActionDefended = "defended"
	ActionResolved = "already-resolved"
	ActionNotOurs  = "not-ours"
	ActionPending  = "before-deadline"
	ActionTimedOut = "timeout-notified"
	ActionFailed   = "failed"
)

type Action struct {
	Digest [32]byte
	Kind   string
}

// Defender defends the challenges on one wallet's released digests. It reads
// only the chain and the defense records: it never waits for executor
// recovery, round agreement, the main lock or a ledger replay
// (P-K08-DEFENSE-BEFORE-DEADLINE).
type Defender struct {
	Chain   Chain
	Wallet  [32]byte
	Records signing.RecordReader
	// From is the first block scanned for challenge logs (the wallet's
	// registration block or later).
	From     uint64
	boundary func(string) error
}

func (d *Defender) step(name string) error {
	if d.boundary != nil {
		return d.boundary(name)
	}
	return nil
}

// open lists the unresolved challenges on this wallet at head.
func (d *Defender) open(ctx context.Context) ([]Challenge, uint64, error) {
	if err := d.step("before-challenge-read"); err != nil {
		return nil, 0, err
	}
	head, err := d.Chain.Head(ctx)
	if err != nil {
		return nil, 0, err
	}
	logs, err := d.Chain.ChallengeLogs(ctx, d.Wallet, d.From, head.Number)
	if err != nil {
		return nil, 0, err
	}
	var out []Challenge
	seen := map[[32]byte]bool{}
	for _, l := range logs {
		if seen[l.Digest] {
			continue
		}
		seen[l.Digest] = true
		c, err := d.Chain.Challenge(ctx, d.Wallet, l.Digest)
		if err != nil {
			return nil, 0, err
		}
		if c.WalletID != d.Wallet || c.Digest != l.Digest {
			return nil, 0, errors.New("challenge view does not match its log")
		}
		out = append(out, c)
	}
	if err = d.step("challenge-seen"); err != nil {
		return nil, 0, err
	}
	return out, head.Number, nil
}

// Defend submits a defense for every unresolved challenge on a digest this
// host holds a defense record for. A lost reply is settled by rereading the
// challenge, never by resubmitting blindly. One challenge that cannot be
// defended never blocks the others: its error is reported with the rest, and
// the pass stops early only when the context ends.
func (d *Defender) Defend(ctx context.Context) ([]Action, error) {
	challenges, head, err := d.open(ctx)
	if err != nil {
		return nil, err
	}
	var actions []Action
	var failures []error
	for _, c := range challenges {
		if err = ctx.Err(); err != nil {
			return actions, errors.Join(append(failures, err)...)
		}
		if c.Resolved {
			actions = append(actions, Action{c.Digest, ActionResolved})
			continue
		}
		record, err := signing.ReadDefense(ctx, d.Records, d.Wallet, c.Digest)
		if errors.Is(err, store.ErrMissing) {
			actions = append(actions, Action{c.Digest, ActionNotOurs})
			continue
		}
		if err != nil {
			actions = append(actions, Action{c.Digest, ActionFailed})
			failures = append(failures, fmt.Errorf("defense record of %x: %w", c.Digest, err))
			continue
		}
		if err = d.step("defense-record-read"); err != nil {
			return actions, err
		}
		if err = d.step("before-defense"); err != nil {
			return actions, err
		}
		if err = d.submit(ctx, head, record); err != nil {
			again, rerr := d.Chain.Challenge(ctx, d.Wallet, c.Digest)
			if rerr != nil || !again.Resolved {
				actions = append(actions, Action{c.Digest, ActionFailed})
				failures = append(failures, fmt.Errorf("defense of %x: %w", c.Digest, err))
				continue
			}
		}
		if err = d.step("defense-submitted"); err != nil {
			return actions, err
		}
		actions = append(actions, Action{c.Digest, ActionDefended})
	}
	return actions, errors.Join(failures...)
}

func (d *Defender) submit(ctx context.Context, head uint64, r signing.DefenseRecord) error {
	if r.Kind == signing.DefenseHeartbeat {
		return d.Chain.DefeatChallengeWithHeartbeat(ctx, r.WalletID, r.Nonce)
	}
	if r.Transaction == nil || int(r.Input) >= len(r.Prevouts) {
		return errors.New("incomplete defense record")
	}
	defense := Defense{WalletID: r.WalletID, Transaction: r.Transaction, Prevouts: r.Prevouts, Input: r.Input, LeafHash: r.LeafHash, Ref: r.Ref}
	switch r.Kind {
	case signing.DefenseIntent:
		defense.Authorization = AuthIntent
	case signing.DefenseMovement:
		defense.Authorization = AuthMovement
	case signing.DefenseSweep:
		// A proven sweep is defended by its proof; an unmined or unproven
		// one (for example a sweep that lost to a refund) as a self-transfer.
		proven, err := d.Chain.ProvenTransaction(ctx, head, [32]byte(r.Transaction.Hash()))
		if err != nil {
			return err
		}
		defense.Authorization = AuthSelf
		if proven == r.WalletID {
			defense.Authorization = AuthProven
		}
	default:
		return fmt.Errorf("unknown defense kind %q", r.Kind)
	}
	return d.Chain.DefeatChallenge(ctx, defense)
}

// NotifyTimeouts notifies every challenge on this wallet that is past its
// deadline and on a digest this host holds no defense record for, so it never
// races its own defense (reading 6). The contract decides the effect.
func (d *Defender) NotifyTimeouts(ctx context.Context) ([]Action, error) {
	challenges, head, err := d.open(ctx)
	if err != nil {
		return nil, err
	}
	timeout, err := d.Chain.FraudDefeatTimeout(ctx)
	if err != nil {
		return nil, err
	}
	now, err := d.Chain.Timestamp(ctx, head)
	if err != nil {
		return nil, err
	}
	var actions []Action
	var failures []error
	for _, c := range challenges {
		if err = ctx.Err(); err != nil {
			return actions, errors.Join(append(failures, err)...)
		}
		if c.Resolved {
			actions = append(actions, Action{c.Digest, ActionResolved})
			continue
		}
		if _, err := signing.ReadDefense(ctx, d.Records, d.Wallet, c.Digest); !errors.Is(err, store.ErrMissing) {
			if err != nil {
				actions = append(actions, Action{c.Digest, ActionFailed})
				failures = append(failures, fmt.Errorf("defense record of %x: %w", c.Digest, err))
			}
			continue // ours: defend it, never time it out
		}
		if now < c.ReportedAt+timeout {
			actions = append(actions, Action{c.Digest, ActionPending})
			continue
		}
		if err = d.step("before-timeout-notify"); err != nil {
			return actions, err
		}
		if err = d.Chain.NotifyChallengeTimeout(ctx, d.Wallet, c.Digest); err != nil {
			again, rerr := d.Chain.Challenge(ctx, d.Wallet, c.Digest)
			if rerr != nil || !again.Resolved {
				actions = append(actions, Action{c.Digest, ActionFailed})
				failures = append(failures, fmt.Errorf("timeout of %x: %w", c.Digest, err))
				continue
			}
		}
		if err = d.step("timeout-notified"); err != nil {
			return actions, err
		}
		actions = append(actions, Action{c.Digest, ActionTimedOut})
	}
	return actions, errors.Join(failures...)
}
