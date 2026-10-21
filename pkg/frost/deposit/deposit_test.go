package deposit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

var ctx = context.Background()

func TestPlannerPendingIsDeterministic(t *testing.T) {
	w := newWorld(t)
	vault := [20]byte{7}
	a := w.deposit(1, 100_000, 90, vault)
	b := w.deposit(2, 200_000, 91, vault)
	w.deposit(3, 300_000, 99, vault)       // not final at anchor 100
	w.deposit(4, 400_000, 92, [20]byte{8}) // other vault
	late := w.deposit(5, 500_000, 92, vault)
	r := w.chain.records[late]
	r.Terms.RefundLocktime = uint32(1_800_000_000 + 100*12 + 3600) // at the planning margin
	w.chain.records[late] = r
	swept := w.deposit(6, 600_000, 92, vault)
	r = w.chain.records[swept]
	r.SweptAt = 5
	w.chain.records[swept] = r
	refunded := w.deposit(7, 700_000, 92, vault)
	w.btc.spent[bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(w.chain.records[refunded].FundingTransactionHash), OutputIndex: 7}] = true
	// C-10: a deposit without an exposure reservation is never planned.
	unreserved := w.deposit(8, 800_000, 92, vault)
	w.chain.unreserved = map[[32]byte]bool{unreserved: true}
	// A reservation first present at block 99 is not final at anchor 100.
	fresh := w.deposit(9, 900_000, 92, vault)
	// One first present at 98 is exactly FinalityBlocks deep: planned.
	final := w.deposit(10, 1_000_000, 92, vault)
	w.chain.reservedAt = map[[32]byte]uint64{fresh: 99, final: 98}
	keys, err := w.planner.Pending(ctx, 0, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[32]byte]bool{a: true, b: true, final: true}
	if len(keys) != 3 {
		t.Fatalf("pending %x", keys)
	}
	for i, key := range keys {
		if !want[key] || (i > 0 && string(keys[i-1][:]) > string(key[:])) {
			t.Fatalf("pending %x", keys)
		}
	}
}

func TestPlannerMainRule(t *testing.T) {
	t.Run("unreserved main is included", func(t *testing.T) {
		w := newWorld(t)
		main := w.fund(1_000_000)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if err != nil {
			t.Fatal(err)
		}
		if m := plan.SweepMain(); m == nil || m.Outpoint != main.Outpoint {
			t.Fatal("C-05 requires the unreserved main")
		}
	})
	t.Run("reserved main is left out", func(t *testing.T) {
		w := newWorld(t)
		w.fund(1_000_000)
		w.chain.custody.ActiveReservation = [32]byte{0x99}
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if err != nil || plan.SweepMain() != nil {
			t.Fatal("deposit-only sweep expected", err)
		}
	})
	t.Run("main held by a redemption", func(t *testing.T) {
		w := newWorld(t)
		main := w.fund(1_000_000)
		key := w.deposit(1, 100_000, 90, [20]byte{})
		if err := w.lock.AcquireRedemption(ctx, w.id, main.Outpoint, [32]byte{0x99}, 200); err != nil {
			t.Fatal(err)
		}
		if _, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{}); !errors.Is(err, ErrMainBusy) {
			t.Fatal("sweep planned on an attested main:", err)
		}
	})
	t.Run("mixed vaults", func(t *testing.T) {
		w := newWorld(t)
		a := w.deposit(1, 100_000, 90, [20]byte{1})
		b := w.deposit(2, 100_000, 90, [20]byte{2})
		if _, err := w.planner.Build(ctx, 100, [][32]byte{a, b}, [32]byte{}); err == nil {
			t.Fatal("two vaults in one sweep")
		}
	})
}

func planned(t *testing.T, w *world, keys ...[32]byte) *signing.Plan {
	t.Helper()
	plan, err := w.planner.Build(ctx, 100, keys, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestAuthoritySweepAuthorizedOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*world, [32]byte)
		want   error
	}{
		{"authorized", func(*world, [32]byte) {}, nil},
		{"reveal not final", func(w *world, k [32]byte) {
			w.chain.head = 91
		}, ErrNotFinal},
		{"reveal reorganized", func(w *world, k [32]byte) {
			w.chain.hashes[90] = [32]byte{0xee}
		}, ErrNotFinal},
		{"already swept", func(w *world, k [32]byte) {
			r := w.chain.records[k]
			r.SweptAt = 9
			w.chain.records[k] = r
		}, ErrSwept},
		{"no exposure reservation", func(w *world, k [32]byte) {
			w.chain.unreserved = map[[32]byte]bool{k: true}
		}, ErrUnreserved},
		// Review of #20: head 100, reveal 90, finality 2. A reservation first
		// present at 99 or 100 is not final; one present at 98 is.
		{"reservation not yet final (99)", func(w *world, k [32]byte) {
			w.chain.reservedAt = map[[32]byte]uint64{k: 99}
		}, ErrUnreserved},
		{"reservation not yet final (100)", func(w *world, k [32]byte) {
			w.chain.reservedAt = map[[32]byte]uint64{k: 100}
		}, ErrUnreserved},
		{"reservation final (98)", func(w *world, k [32]byte) {
			w.chain.reservedAt = map[[32]byte]uint64{k: 98}
		}, nil},
		{"refund margin by median-time-past", func(w *world, k [32]byte) {
			w.clock.set(uint64(w.chain.records[k].Terms.RefundLocktime) - 3600)
		}, ErrRefundMargin},
		{"captured fee changed", func(w *world, k [32]byte) {
			r := w.chain.records[k]
			r.MaximumSweepFee--
			w.chain.records[k] = r
		}, ErrPlanMismatch},
		{"template output differs", func(w *world, k [32]byte) {
			r := w.chain.records[k]
			r.DepositOutputKey[0] ^= 1
			w.chain.records[k] = r
		}, ErrPlanMismatch},
		{"main reserved since planning", func(w *world, k [32]byte) {
			w.chain.custody.ActiveReservation = [32]byte{0x99}
		}, ErrMainBusy},
		{"main replaced since planning", func(w *world, k [32]byte) {
			w.chain.custody.MainOutpoint[0] ^= 1
		}, ErrPlanMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.fund(1_000_000)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			plan := planned(t, w, key)
			tc.change(w, key)
			receipt, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, plan)
			if tc.want == nil {
				if err != nil || receipt.Block != 90 || receipt.Hash != w.chain.hash(90) {
					t.Fatal("authorized sweep refused", err, receipt)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// A deposit-only sweep is refused while an unreserved main exists, so a sweep
// can never strand the main beside a second custody output.
func TestAuthorityRequiresUnreservedMain(t *testing.T) {
	w := newWorld(t)
	w.chain.custody.ActiveReservation = [32]byte{0x99}
	w.fund(1_000_000)
	w.chain.custody.ActiveReservation = [32]byte{0x99}
	key := w.deposit(1, 100_000, 90, [20]byte{})
	plan := planned(t, w, key)
	w.chain.custody.ActiveReservation = [32]byte{}
	if _, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, plan); !errors.Is(err, ErrPlanMismatch) {
		t.Fatal(err)
	}
}

func TestMainLockTransitions(t *testing.T) {
	w := newWorld(t)
	main := w.fund(1_000_000).Outpoint
	r := [32]byte{0x99}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, r, 120); err != nil {
		t.Fatal(err)
	}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, r, 120); err != nil {
		t.Fatal("idempotent attestation refused", err)
	}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, [32]byte{0x98}, 120); !errors.Is(err, ErrMainBusy) {
		t.Fatal("second reservation took the main")
	}
	if err := w.lock.AcquireSweep(ctx, w.id, main, [32]byte{1}); !errors.Is(err, ErrMainBusy) {
		t.Fatal("sweep took a redemption main")
	}
	// Non-admission frees the main only after the deadline is final and the
	// reservation is absent.
	if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 119, w.id, main, r); err == nil {
		t.Fatal("released before the deadline was final")
	}
	w.chain.intents[r] = redemption.Intent{ID: r}
	if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 130, w.id, main, r); err == nil {
		t.Fatal("released an admitted reservation")
	}
	delete(w.chain.intents, r)
	if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 130, w.id, main, r); err != nil {
		t.Fatal(err)
	}
	if err := w.lock.AcquireSweep(ctx, w.id, main, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := w.lock.AcquireSweep(ctx, w.id, main, [32]byte{2}); err != nil {
		t.Fatal("sweep alternative on the same main refused", err)
	}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, r, 300); !errors.Is(err, ErrMainBusy) {
		t.Fatal("redemption attested on a main a released sweep may spend (VD-07 reading 2)")
	}
	if err := w.lock.Close(ctx, w.chain, 130, w.id, main, "sweep proven"); err == nil {
		t.Fatal("closed an unspent main")
	}
	key := OutpointKey(main)
	o := w.chain.outputs[key]
	o.Spent = true
	w.chain.outputs[key] = o
	if err := w.lock.Close(ctx, w.chain, 130, w.id, main, "sweep proven"); err != nil {
		t.Fatal(err)
	}
	if err := w.lock.AcquireSweep(ctx, w.id, main, [32]byte{3}); !errors.Is(err, ErrMainBusy) {
		t.Fatal("closed main reused")
	}
	history, _ := w.lock.History(ctx, w.id, key)
	if len(history) != 5 { // redemption, free, sweep 1, sweep 2, closed
		t.Fatalf("history must be append-only, got %d records", len(history))
	}
}

func TestGuardResolutionOrdersGrants(t *testing.T) {
	w := newWorld(t)
	mainPrev := w.fund(1_000_000)
	r := [32]byte{0x99}
	w.chain.custody.ActiveReservation = r
	key := w.deposit(1, 100_000, 90, [20]byte{})
	resolution, err := w.planner.Build(ctx, 100, [][32]byte{key}, r)
	if err != nil {
		t.Fatal(err)
	}
	guard := Guard{Lock: w.lock}
	if err = w.lock.AcquireRedemption(ctx, w.id, mainPrev.Outpoint, r, 200); err != nil {
		t.Fatal(err)
	}
	if err = guard.Permit(ctx, resolution, [32]byte{0x5e}); !errors.Is(err, ErrMainBusy) {
		t.Fatal("resolution sweep permitted before the resolution attestation", err)
	}
	// A resolution racing a grant: the grant read the redemption state, then
	// the resolution took the next record number first. The grant's insert
	// is refused, so it cannot be ordered after the resolution.
	history, _ := w.lock.History(ctx, w.id, OutpointKey(mainPrev.Outpoint))
	if err = w.lock.Resolve(ctx, w.id, mainPrev.Outpoint, r, resolution.BitcoinTransactionHash()); err != nil {
		t.Fatal(err)
	}
	stale := Entry{Purpose: Redemption, Grant: true, Subject: [32]byte{0x55}}
	if err = w.lock.insert(ctx, w.id, OutpointKey(mainPrev.Outpoint), len(history), stale); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatal("stale grant inserted after the resolution:", err)
	}
	if err = guard.Grant(ctx, resolution, [32]byte{0x5e}, 0); err != nil {
		t.Fatal("resolution sweep refused after attestation", err)
	}
	if err = w.lock.AcquireRedemption(ctx, w.id, mainPrev.Outpoint, r, 200); !errors.Is(err, ErrMainBusy) {
		t.Fatal("redemption reacquired after resolution")
	}
}

// Review finding: renewing an attestation with a later deadline extends the
// lock, so a finalized earlier deadline does not release the main.
func TestMainLockRenewalExtendsDeadline(t *testing.T) {
	w := newWorld(t)
	main := w.fund(1_000_000).Outpoint
	r := [32]byte{0x99}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, r, 120); err != nil {
		t.Fatal(err)
	}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, r, 200); err != nil {
		t.Fatal(err)
	}
	if err := w.lock.AcquireRedemption(ctx, w.id, main, r, 150); err != nil {
		t.Fatal("an earlier deadline must stay idempotent:", err)
	}
	if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 130, w.id, main, r); err == nil {
		t.Fatal("released before the renewed deadline was final")
	}
	if err := w.lock.ReleaseUnadmitted(ctx, w.chain, 200, w.id, main, r); err != nil {
		t.Fatal(err)
	}
}
