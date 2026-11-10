package deposit

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// errClock is a fixture Bitcoin clock that cannot be read.
type errClock struct{}

func (errClock) MedianTimePast(context.Context) (uint64, error) { return 0, errInjected }

// TestViewReadRefusesIncompleteView: the deposit view is one consistent read
// of chain, finality and Bitcoin time; it cannot read without all three.
// Invariant: a view missing its chain, its clock or its finality depth is
// refused with an error, never a crash and never a snapshot. Refs M5-deposit
// mutation batch (KC-SF-V3 K-06).
func TestViewReadRefusesIncompleteView(t *testing.T) {
	w := newWorld(t)
	key := w.deposit(1, 100_000, 90, [20]byte{})
	for _, tc := range []struct {
		name string
		view *View
	}{
		{"no chain", &View{Clock: w.clock, Policy: w.view.Policy}},
		{"no clock", &View{Chain: w.chain, Policy: w.view.Policy}},
		{"no finality depth", &View{Chain: w.chain, Clock: w.clock, Policy: Policy{RefundMargin: 3600}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := recovered(func() error { _, err := tc.view.Read(ctx, w.id, [][32]byte{key}); return err })
			if err == nil {
				t.Fatalf("an incomplete deposit view (%s) produced a snapshot", tc.name)
			}
			if len(err.Error()) > 6 && err.Error()[:6] == "panic:" {
				t.Fatalf("an incomplete deposit view (%s) crashed instead of refusing: %v", tc.name, err)
			}
		})
	}
}

// TestViewReadFailsOnEveryFailedRead: a snapshot is only as good as its
// reads. Invariant: any failed chain or clock read, a missing deposit record
// or reveal, a reveal past the head, a head that is reorganized during the
// read, and a stop at any view boundary all fail the read; none yields a
// snapshot. The reader is the fault-injecting fixture of
// gaps_fixture_test.go. Refs M5-deposit mutation batch.
func TestViewReadFailsOnEveryFailedRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*world, *faultChain, *View, *[32]byte)
		want  error // nil: any error
	}{
		{"head unreadable", func(_ *world, c *faultChain, _ *View, _ *[32]byte) { c.set("Head", true) }, nil},
		{"deposit record missing", func(_ *world, _ *faultChain, _ *View, k *[32]byte) { k[0] ^= 1 }, ErrNotFound},
		{"reveal unreadable", func(_ *world, c *faultChain, _ *View, _ *[32]byte) { c.set("Reveal", true) }, nil},
		{"reveal block hash unreadable", func(_ *world, c *faultChain, _ *View, _ *[32]byte) {
			c.hashFail = func(n uint64) bool { return n == 90 }
		}, nil},
		{"reservation unreadable", func(_ *world, c *faultChain, _ *View, _ *[32]byte) { c.set("Reserved", true) }, nil},
		{"reservation unreadable at the final depth", func(_ *world, c *faultChain, _ *View, _ *[32]byte) { c.reservedErrAt = 98 }, nil},
		{"custody unreadable", func(_ *world, c *faultChain, _ *View, _ *[32]byte) { c.set("Custody", true) }, nil},
		{"head hash unreadable on recheck", func(_ *world, c *faultChain, _ *View, _ *[32]byte) {
			c.hashFail = func(n uint64) bool { return n == 100 }
		}, nil},
		{"head reorganized during the read", func(w *world, c *faultChain, _ *View, _ *[32]byte) {
			calls := 0
			c.hashAt = func(n uint64) [32]byte {
				if n == 100 {
					if calls++; calls > 1 {
						return [32]byte{0xee}
					}
				}
				return w.chain.hash(n)
			}
		}, redemption.ErrInconsistentRead},
		{"reveal past the head", func(w *world, _ *faultChain, _ *View, k *[32]byte) {
			r := w.chain.reveals[*k]
			r.Block, r.BlockHash = 101, w.chain.hash(101)
			w.chain.reveals[*k] = r
		}, redemption.ErrInconsistentRead},
		{"median-time-past unreadable", func(_ *world, _ *faultChain, v *View, _ *[32]byte) { v.Clock = errClock{} }, nil},
		{"stop before the deposit read", stopAt("before-deposit-read"), nil},
		{"stop after the deposits are seen", stopAt("deposits-seen"), nil},
		{"stop after the margin is seen", stopAt("refund-margin-seen"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			c := newFaultChain(w.chain)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			v := &View{Chain: c, Clock: w.clock, Policy: w.view.Policy}
			tc.setup(w, c, v, &key)
			s, err := v.Read(ctx, w.id, [][32]byte{key})
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("a failed read (%s) produced a snapshot of %d deposits: %v", tc.name, len(s.Deposits), err)
			}
		})
	}
}

// stopAt sets a view boundary that stops the read at name (a crash point).
func stopAt(name string) func(*world, *faultChain, *View, *[32]byte) {
	return func(_ *world, _ *faultChain, v *View, _ *[32]byte) {
		v.boundary = func(at string) error {
			if at == name {
				return errInjected
			}
			return nil
		}
	}
}

// TestViewRevealAtHeadIsConsistentButNotFinal: a reveal in the head block is
// a consistent read (it is not past the head) and is not final. Invariant:
// Read accepts it and Sweepable refuses it as ErrNotFinal, not as an
// inconsistent read. Refs M5-deposit mutation batch.
func TestViewRevealAtHeadIsConsistentButNotFinal(t *testing.T) {
	w := newWorld(t)
	key := w.deposit(1, 100_000, 100, [20]byte{})
	s, err := w.view.Read(ctx, w.id, [][32]byte{key})
	if err != nil {
		t.Fatal("a reveal in the head block was refused as an inconsistent read:", err)
	}
	if err = w.view.Sweepable(s, s.Deposits[0], w.id); !errors.Is(err, ErrNotFinal) {
		t.Fatal("a reveal in the head block was sweepable:", err)
	}
}

// TestViewReservationDepthBoundary: a C-10 reservation counts only when it
// is open at the head and already open FinalityBlocks earlier (review of
// #20). Invariant: at head == FinalityBlocks the earlier block is genesis
// and is still read (reserved); with a depth past the chain the reservation
// cannot be final (not reserved, no error). Refs M5-deposit mutation batch.
func TestViewReservationDepthBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth uint64
		want  bool
	}{
		{"depth equal to the head", 100, true},
		{"depth one past the head", 101, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			v := &View{Chain: w.chain, Clock: w.clock, Policy: Policy{FinalityBlocks: tc.depth, RefundMargin: 3600}}
			s, err := v.Read(ctx, w.id, [][32]byte{key})
			if err != nil {
				t.Fatal(err)
			}
			if s.Deposits[0].Reserved != tc.want {
				t.Fatalf("reservation at depth %d of head 100: reserved %v, want %v", tc.depth, s.Deposits[0].Reserved, tc.want)
			}
		})
	}
}

// TestSweepableRefusesEachIdentityFault: a deposit input must be revealed,
// under its own key, for this wallet, and final. Invariant: each single
// fault (no reveal record, a reveal log of another key, terms or reveal log
// of another wallet) is refused with its own refusal; a reveal exactly
// FinalityBlocks deep is final and accepted. Refs M5-deposit mutation batch.
func TestSweepableRefusesEachIdentityFault(t *testing.T) {
	w := newWorld(t)
	key := w.deposit(1, 100_000, 90, [20]byte{})
	w.chain.head = 92 // reveal 90, finality 2: exactly final
	s, err := w.view.Read(ctx, w.id, [][32]byte{key})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Deposit)
		want   error
	}{
		{"exactly final", func(*Deposit) {}, nil},
		{"not revealed in the record", func(d *Deposit) { d.Record.RevealedAt = 0 }, ErrNotFound},
		{"reveal log of another key", func(d *Deposit) { d.Reveal.Outpoint[0] ^= 1 }, ErrNotFound},
		{"terms of another wallet", func(d *Deposit) { d.Record.Terms.WalletID[0] ^= 1 }, ErrForeignWallet},
		{"reveal log of another wallet", func(d *Deposit) { d.Reveal.WalletID[0] ^= 1 }, ErrForeignWallet},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := s.Deposits[0]
			tc.change(&d)
			err := w.view.Sweepable(s, d, w.id)
			if tc.want == nil && err != nil {
				t.Fatal("a final, revealed deposit of this wallet was refused:", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("deposit with %s was sweepable: %v", tc.name, err)
			}
		})
	}
}
