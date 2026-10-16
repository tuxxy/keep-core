package redemption

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// TestZeroNetAndFeeBoundaries is the K-05 named check that zero or negative
// nets, invalid fee domains and overflow are refused before any plan (and so
// before any signature request) exists. The legacy half is in pkg/tbtc.
func TestZeroNetAndFeeBoundaries(t *testing.T) {
	ctx := context.Background()
	base := func() Request {
		recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{1})
		return Request{Gross: 10_000, TreasuryFee: 100, MaximumBitcoinFee: 1000, Script: recipient}
	}
	for _, tc := range []struct {
		name  string
		edit  func(*Request)
		share int64
		want  error
	}{
		{"treasury above gross wraps", func(r *Request) { r.TreasuryFee = r.Gross + 1 }, 0, ErrZeroNet},
		{"zero net exactly", func(r *Request) { r.MaximumBitcoinFee = 1_000_000 }, 9_900, ErrZeroNet},
		{"negative net", func(r *Request) { r.MaximumBitcoinFee = 1_000_000 }, 9_901, ErrZeroNet},
		{"share above captured cap", func(*Request) {}, 1001, ErrFeeDomain},
		{"negative share", func(*Request) {}, -1, ErrFeeDomain},
		{"dust output", func(r *Request) { r.Gross = 393 }, 0, ErrDustOutput},
		{"gross beyond satoshi domain", func(r *Request) { r.Gross = uint64(bitcoin.MaxSatoshi) + 1 }, 0, ErrAmountDomain},
		{"cap beyond satoshi domain", func(r *Request) { r.MaximumBitcoinFee = ^uint64(0) }, 0, ErrAmountDomain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.edit(&r)
			if _, err := netValue(r, tc.share); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("positive control", func(t *testing.T) {
		if v, err := netValue(base(), 1000); err != nil || v != 8900 {
			t.Fatalf("got %d %v", v, err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*world)
		want error
	}{
		{"zero fee rate", func(w *world) { w.planner.Fees.FeeRate = 0 }, ErrFeeDomain},
		{"fee rate above envelope", func(w *world) { w.planner.Fees.FeeRate = 11 }, ErrFeeDomain},
		{"fee rate overflow", func(w *world) {
			w.planner.Fees.FeeRate = bitcoin.MaxSatoshi
			w.planner.Fees.MaximumSatPerVByte = bitcoin.MaxSatoshi
		}, ErrFeeDomain},
		{"fee above governance total", func(w *world) { w.chain.maxTotalFee = 50 }, ErrFeeDomain},
		{"fee below certificate minimum", func(w *world) { w.planner.Fees.Minimum = 100_000 }, ErrFeeDomain},
		{"share above captured cap", func(w *world) {
			for id, r := range w.chain.requests {
				r.MaximumBitcoinFee = 1
				w.chain.requests[id] = r
			}
		}, ErrFeeDomain},
		{"zero net after fee", func(w *world) {
			for id, r := range w.chain.requests {
				r.Gross, r.TreasuryFee, r.MaximumBitcoinFee = 700, 600, 1000
				w.chain.requests[id] = r
			}
		}, ErrZeroNet},
		{"custody cannot cover change", func(w *world) {
			w.main.Value = 150_000
			w.planner.Bitcoin = fakeUnspent{w.main}
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Value = 150_000
			w.chain.outputs[k] = o
		}, ErrInsufficient},
		{"main output not discoverable by script", func(w *world) { w.planner.Bitcoin = fakeUnspent{} }, ErrNoCustody},
		{"custody value mismatch", func(w *world) {
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Value++
			w.chain.outputs[k] = o
		}, ErrNoCustody},
		{"request past deadline margin", func(w *world) { w.chain.time += 3600 - 599 }, ErrNotRedeemable},
		{"request not yet allowed", func(w *world) {
			for id, r := range w.chain.requests {
				r.NotBefore = w.chain.time + 1
				w.chain.requests[id] = r
			}
		}, ErrNotRedeemable},
		{"request already reserved", func(w *world) {
			for id, r := range w.chain.requests {
				r.State = RequestReserved
				w.chain.requests[id] = r
			}
		}, ErrNotRedeemable},
	} {
		t.Run("plan/"+tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.edit(w)
			if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPlannerBuildsAndValidatesExactEntitlements(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	plan, requests, err := w.planner.Build(ctx, w.chain.head, w.ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || plan.Generation() != 1 || plan.StateVersion() != 1 || OutpointKey(plan.ConflictInput()) != OutpointKey(w.main.Outpoint) {
		t.Fatal("plan slot or conflict input")
	}
	fee := plan.Snapshot().Fee()
	var shareTotal int64
	for i, r := range plan.Requests() {
		share := int64(requests[i].Gross-requests[i].TreasuryFee) - r.Value
		if share < 0 || share > int64(requests[i].MaximumBitcoinFee) {
			t.Fatal("share outside cap")
		}
		shareTotal += share
	}
	if shareTotal != fee || fee != w.planner.Fees.FeeRate*plan.Snapshot().VirtualSize() {
		t.Fatalf("shares %d fee %d", shareTotal, fee)
	}
	if err = w.planner.Validate(ctx, w.chain.head, plan); err != nil {
		t.Fatal(err)
	}
	pending, err := w.planner.Pending(ctx, 0, w.chain.head, 0)
	if err != nil || len(pending) != 2 || pending[0] != w.ids[0] {
		t.Fatalf("pending %v %v", pending, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*world)
	}{
		{"request reserved meanwhile", func(w *world) {
			r := w.chain.requests[w.ids[0]]
			r.State = RequestReserved
			w.chain.requests[w.ids[0]] = r
		}},
		{"captured cap lower than share", func(w *world) {
			r := w.chain.requests[w.ids[1]]
			r.MaximumBitcoinFee = 1
			w.chain.requests[w.ids[1]] = r
		}},
		{"slot already admitted", func(w *world) {
			c := w.chain.custody[plan.WalletID()]
			c.Generation = 1
			w.chain.custody[plan.WalletID()] = c
		}},
		{"state version advanced", func(w *world) {
			c := w.chain.custody[plan.WalletID()]
			c.StateVersion = 2
			w.chain.custody[plan.WalletID()] = c
		}},
		{"main output spent", func(w *world) {
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Spent = true
			w.chain.outputs[k] = o
		}},
		{"governance fee cap lowered", func(w *world) { w.chain.maxTotalFee = 100 }},
		{"redemptions disabled", func(w *world) { w.chain.config.Enabled = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w2 := newWorld(t)
			tc.edit(w2)
			if err := w2.planner.Validate(ctx, w2.chain.head, plan); err == nil {
				t.Fatal("stale or invalid plan accepted")
			}
		})
	}
}
