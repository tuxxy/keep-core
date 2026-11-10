package deposit

import (
	"bytes"
	"errors"
	"sort"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// faultyPlanner points the world's planner at a fault chain and a faulting
// Bitcoin view (fixtures of gaps_fixture_test.go).
func faultyPlanner(w *world) (*faultChain, *faultUnspent) {
	c := newFaultChain(w.chain)
	btc := &faultUnspent{scriptUnspent: w.btc}
	w.planner.Chain, w.planner.Bitcoin, w.planner.Funding = c, btc, btc
	return c, btc
}

// spend marks a deposit's funding output spent on Bitcoin.
func (w *world) spend(key [32]byte) {
	r := w.chain.records[key]
	w.btc.spent[bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(r.FundingTransactionHash), OutputIndex: r.FundingOutputIndex}] = true
}

// depositKey is the key w.deposit gives tag, before the deposit exists.
func depositKeyOf(tag byte) [32]byte {
	funding := bitcoin.TransactionOutpoint{OutputIndex: uint32(tag)}
	funding.TransactionHash[0], funding.TransactionHash[1] = 0xf0, tag
	return OutpointKey(funding)
}

// TestPlannerSyncedRule: base keep-core's EnsureWalletSyncedBetweenChains,
// as Planner.Synced documents it. Invariant: the wallet is not synced exactly
// when some deposit revealed in the window is recorded, unswept, of this
// wallet, C-10 reserved, spent on Bitcoin, and median-time-past is before
// its refund locktime; every other reveal is skipped and never ends the scan
// early (a blocker after any skipped reveal is still found). A spend at or
// after the locktime may be the refund and does not block. Reveals are read
// in block order (fixture). Refs M5-deposit mutation batch (KC-SF-V3 K-06).
func TestPlannerSyncedRule(t *testing.T) {
	type step func(w *world, blocker [32]byte)
	skipper := func(make func(w *world, key [32]byte)) step {
		return func(w *world, _ [32]byte) {
			key := w.deposit(20, 100_000, 80, [20]byte{}) // revealed before the blocker
			w.spend(key)
			make(w, key)
		}
	}
	for _, tc := range []struct {
		name    string
		blocked bool // the block-95 deposit is spent before its locktime
		extra   step
		synced  bool
	}{
		{"nothing spent", false, nil, true},
		{"spent before the locktime", true, nil, false},
		{"spent at the locktime", true, func(w *world, k [32]byte) { setLocktime(w, k, w.clock.mtp) }, true},
		{"spent after the locktime", true, func(w *world, k [32]byte) { setLocktime(w, k, w.clock.mtp-1) }, true},
		{"spent but unreserved", true, func(w *world, k [32]byte) { w.chain.unreserved = map[[32]byte]bool{k: true} }, true},
		{"spent but already swept", true, func(w *world, k [32]byte) { r := w.chain.records[k]; r.SweptAt = 9; w.chain.records[k] = r }, true},
		{"spent under other wallet terms", true, func(w *world, k [32]byte) {
			r := w.chain.records[k]
			r.Terms.WalletID[0] ^= 1
			w.chain.records[k] = r
		}, true},
		{"spent but never recorded", true, func(w *world, k [32]byte) { delete(w.chain.records, k) }, true},
		{"after an unrecorded reveal", true, skipper(func(w *world, k [32]byte) { delete(w.chain.records, k) }), false},
		{"after a swept deposit", true, skipper(func(w *world, k [32]byte) { r := w.chain.records[k]; r.SweptAt = 9; w.chain.records[k] = r }), false},
		{"after an unreserved deposit", true, skipper(func(w *world, k [32]byte) { w.chain.unreserved = map[[32]byte]bool{k: true} }), false},
		{"after an unspent deposit", true, skipper(func(w *world, k [32]byte) { delete(w.btc.spent, fundingOf(w, k)) }), false},
		{"after a deposit spent after its locktime", true, skipper(func(w *world, k [32]byte) { setLocktime(w, k, w.clock.mtp-1) }), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			faultyPlanner(w)
			w.planner.Clock = w.clock
			w.deposit(1, 100_000, 70, [20]byte{}) // unspent: never blocks
			blocker := w.deposit(2, 100_000, 95, [20]byte{})
			if tc.blocked {
				w.spend(blocker)
			}
			if tc.extra != nil {
				tc.extra(w, blocker)
			}
			err := w.planner.Synced(ctx, 0, 100)
			if tc.synced && err != nil {
				t.Fatalf("wallet reported unsynced (%s): %v", tc.name, err)
			}
			if !tc.synced && !errors.Is(err, ErrWalletNotSynced) {
				t.Fatalf("an unproven wallet spend was not found (%s): %v", tc.name, err)
			}
		})
	}
}

func fundingOf(w *world, key [32]byte) bitcoin.TransactionOutpoint {
	r := w.chain.records[key]
	return bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(r.FundingTransactionHash), OutputIndex: r.FundingOutputIndex}
}

// setLocktime sets the refund locktime of a recorded deposit.
func setLocktime(w *world, key [32]byte, locktime uint64) {
	r := w.chain.records[key]
	r.Terms.RefundLocktime = uint32(locktime)
	w.chain.records[key] = r
}

// TestPlannerSyncedGatesPendingAndBuild: an unsynced wallet plans nothing.
// Invariant: Pending and Build both refuse with ErrWalletNotSynced, and every
// failed read of the sync check fails it rather than reporting the wallet
// synced. Refs M5-deposit mutation batch.
func TestPlannerSyncedGatesPendingAndBuild(t *testing.T) {
	setup := func(t *testing.T) (*world, *faultChain, *faultUnspent, [32]byte) {
		w := newWorld(t)
		c, btc := faultyPlanner(w)
		w.planner.Clock = w.clock
		ok := w.deposit(1, 100_000, 90, [20]byte{})
		w.spend(w.deposit(2, 100_000, 95, [20]byte{}))
		return w, c, btc, ok
	}
	t.Run("pending", func(t *testing.T) {
		w, _, _, _ := setup(t)
		if keys, err := w.planner.Pending(ctx, 0, 100, 0); !errors.Is(err, ErrWalletNotSynced) || keys != nil {
			t.Fatalf("an unsynced wallet listed pending deposits: %x %v", keys, err)
		}
	})
	t.Run("build", func(t *testing.T) {
		w, _, _, ok := setup(t)
		if plan, err := w.planner.Build(ctx, 100, [][32]byte{ok}, [32]byte{}); !errors.Is(err, ErrWalletNotSynced) || plan != nil {
			t.Fatal("an unsynced wallet planned a sweep:", err)
		}
	})
	for _, tc := range []struct {
		name string
		set  func(*world, *faultChain, *faultUnspent)
	}{
		{"reveals unreadable", func(_ *world, c *faultChain, _ *faultUnspent) { c.set("Reveals", true) }},
		{"record unreadable", func(_ *world, c *faultChain, _ *faultUnspent) { c.set("Deposit", true) }},
		{"reservation unreadable", func(_ *world, c *faultChain, _ *faultUnspent) { c.set("Reserved", true) }},
		{"funding unreadable", func(_ *world, _ *faultChain, b *faultUnspent) { b.failUnspent = true }},
		{"median-time-past unreadable", func(w *world, _ *faultChain, _ *faultUnspent) { w.planner.Clock = errClock{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, c, btc, _ := setup(t)
			tc.set(w, c, btc)
			if err := w.planner.Synced(ctx, 0, 100); err == nil || errors.Is(err, ErrWalletNotSynced) {
				t.Fatalf("a failed sync read (%s) was not reported as a failure: %v", tc.name, err)
			}
		})
	}
}

// TestPlannerPendingEligibility: Pending lists only deposits the planner
// could sweep at the anchor. Invariant: a reveal without a C-05 record is
// never listed (and never planned), and any failed read while checking
// eligibility fails Pending instead of silently dropping or keeping the
// deposit. Refs M5-deposit mutation batch.
func TestPlannerPendingEligibility(t *testing.T) {
	t.Run("reveal without record", func(t *testing.T) {
		w := newWorld(t)
		listed := w.deposit(1, 100_000, 90, [20]byte{})
		unrecorded := w.deposit(2, 100_000, 90, [20]byte{})
		delete(w.chain.records, unrecorded)
		keys, err := w.planner.Pending(ctx, 0, 100, 0)
		if err != nil || len(keys) != 1 || keys[0] != listed {
			t.Fatalf("pending listed a reveal without a C-05 record: %x %v", keys, err)
		}
		if _, err = w.planner.Build(ctx, 100, [][32]byte{unrecorded}, [32]byte{}); err == nil {
			t.Fatal("a reveal without a C-05 record was planned")
		}
	})
	for _, tc := range []struct {
		name string
		set  func(*faultChain, *faultUnspent)
	}{
		{"reveal log unreadable", func(c *faultChain, _ *faultUnspent) { c.set("Reveals", true) }},
		{"record unreadable", func(c *faultChain, _ *faultUnspent) { c.set("Deposit", true) }},
		{"reveal unreadable", func(c *faultChain, _ *faultUnspent) { c.set("Reveal", true) }},
		{"anchor timestamp unreadable", func(c *faultChain, _ *faultUnspent) { c.set("Timestamp", true) }},
		{"reservation unreadable", func(c *faultChain, _ *faultUnspent) { c.set("Reserved", true) }},
		{"funding unreadable", func(_ *faultChain, b *faultUnspent) { b.failUnspent = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			c, btc := faultyPlanner(w)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			tc.set(c, btc)
			if keys, err := w.planner.Pending(ctx, 0, 100, 0); err == nil {
				t.Fatalf("a failed eligibility read (%s) was hidden: %x", tc.name, keys)
			}
			if plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{}); err == nil && tc.name != "reveal log unreadable" {
				t.Fatalf("a failed eligibility read (%s) still planned a sweep: %v", tc.name, plan != nil)
			}
		})
	}
}

// TestPlannerPendingSelection: Pending selects, in ascending key order, the
// eligible deposits of the smallest key's vault, at most limit of them when
// limit > 0, and at most MaxSweepInputs-1 so the main always fits (C-05
// MAX_SWEEP_INPUTS). Invariant: a key of another vault between two keys of
// the selected vault does not end the selection; limit and the input cap
// are exact. Refs M5-deposit mutation batch.
func TestPlannerPendingSelection(t *testing.T) {
	t.Run("another vault in between", func(t *testing.T) {
		w := newWorld(t)
		tags := []byte{1, 2, 3, 4, 5}
		sort.Slice(tags, func(i, j int) bool {
			a, b := depositKeyOf(tags[i]), depositKeyOf(tags[j])
			return bytes.Compare(a[:], b[:]) < 0
		})
		// Ascending keys: A B A B A.
		var want [][32]byte
		for i, tag := range tags {
			vault := [20]byte{0xa}
			if i%2 == 1 {
				vault = [20]byte{0xb}
			}
			key := w.deposit(tag, 100_000, 90, vault)
			if vault == ([20]byte{0xa}) {
				want = append(want, key)
			}
		}
		keys, err := w.planner.Pending(ctx, 0, 100, 0)
		if err != nil || !sameKeys(keys, want) {
			t.Fatalf("selection stopped at a key of another vault: got %x, want %x (%v)", keys, want, err)
		}
	})
	t.Run("limit", func(t *testing.T) {
		w := newWorld(t)
		var all [][32]byte
		for tag := byte(1); tag <= 3; tag++ {
			all = append(all, w.deposit(tag, 100_000, 90, [20]byte{}))
		}
		sortKeys(all)
		keys, err := w.planner.Pending(ctx, 0, 100, 2)
		if err != nil || !sameKeys(keys, all[:2]) {
			t.Fatalf("limit 2 selected %d deposits: %x (%v)", len(keys), keys, err)
		}
	})
	t.Run("input cap leaves room for the main", func(t *testing.T) {
		w := newWorld(t)
		var all [][32]byte
		for tag := 1; tag <= signing.MaxSweepInputs; tag++ {
			all = append(all, w.deposit(byte(tag), 100_000, 90, [20]byte{}))
		}
		sortKeys(all)
		keys, err := w.planner.Pending(ctx, 0, 100, 0)
		if err != nil || !sameKeys(keys, all[:signing.MaxSweepInputs-1]) {
			t.Fatalf("%d eligible deposits: selected %d, want %d (%v)", len(all), len(keys), signing.MaxSweepInputs-1, err)
		}
	})
}

func sortKeys(keys [][32]byte) {
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
}

// sameKeys compares in order: ascending key order is part of the contract.
func sameKeys(a, b [][32]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPlannerMainDiscovery: the main input is the recognized C-05 custody
// output, found by exact script on Bitcoin. Invariant: another output to the
// same script listed first does not hide it; a main that Bitcoin does not
// list, or whose custody record is spent, of another wallet or of another
// value, is refused with ErrNoCustody; a failed read refuses the plan with
// that provider failure (so the caller retries instead of replanning).
// Refs M5-deposit mutation batch.
func TestPlannerMainDiscovery(t *testing.T) {
	t.Run("another output to the wallet script first", func(t *testing.T) {
		w := newWorld(t)
		main := w.fund(1_000_000)
		decoy := bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xab}}, Value: 5_000, PublicKeyScript: main.PublicKeyScript}
		w.btc.outputs = []bitcoin.PreviousOutput{decoy, main}
		key := w.deposit(1, 100_000, 90, [20]byte{})
		plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if err != nil || plan.SweepMain() == nil || plan.SweepMain().Outpoint != main.Outpoint {
			t.Fatal("the recognized main was not found behind another wallet output:", err)
		}
	})
	for _, tc := range []struct {
		name string
		set  func(*world, *faultChain, *faultUnspent)
		want error // nil: any error
	}{
		{"main not on Bitcoin", func(w *world, _ *faultChain, _ *faultUnspent) { w.btc.outputs = nil }, redemption.ErrNoCustody},
		{"custody output spent", func(w *world, _ *faultChain, _ *faultUnspent) {
			editMainOutput(w, func(o *redemption.CustodyOutput) { o.Spent = true })
		}, redemption.ErrNoCustody},
		{"custody output of another wallet", func(w *world, _ *faultChain, _ *faultUnspent) {
			editMainOutput(w, func(o *redemption.CustodyOutput) { o.WalletID[0] ^= 1 })
		}, redemption.ErrNoCustody},
		{"custody output of another value", func(w *world, _ *faultChain, _ *faultUnspent) {
			editMainOutput(w, func(o *redemption.CustodyOutput) { o.Value++ })
		}, redemption.ErrNoCustody},
		{"Bitcoin script lookup unreadable", func(_ *world, _ *faultChain, b *faultUnspent) { b.failByScript = true }, errInjected},
		{"custody output unreadable", func(_ *world, c *faultChain, _ *faultUnspent) { c.set("CustodyOutput", true) }, errInjected},
		{"custody unreadable", func(_ *world, c *faultChain, _ *faultUnspent) { c.set("Custody", true) }, errInjected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			c, btc := faultyPlanner(w)
			w.fund(1_000_000)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			tc.set(w, c, btc)
			plan, err := w.planner.Build(ctx, 100, [][32]byte{key}, [32]byte{})
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("a sweep was planned without a recognized main (%s): plan %v, %v", tc.name, plan != nil, err)
			}
		})
	}
}

func editMainOutput(w *world, f func(*redemption.CustodyOutput)) {
	o := w.chain.outputs[w.chain.custody.MainOutpoint]
	f(&o)
	w.chain.outputs[w.chain.custody.MainOutpoint] = o
}

// TestPlannerBuildRefusals: Build plans only a valid sweep. Invariant: a
// fee policy with no positive rate or a ceiling below the rate is refused
// even when an exact proposal fee is set; a rate equal to the ceiling is
// allowed; a stop before planning, an unreadable main lock, a key that is
// not eligible, a record whose terms do not build or whose output key is
// not the D-04 template, and inputs above the Bitcoin supply are refused.
// Refs M5-deposit mutation batch.
func TestPlannerBuildRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*world, [32]byte) [][32]byte
		ok   bool
		want error
	}{
		{"zero rate with an exact fee", func(w *world, k [32]byte) [][32]byte { w.planner.FeeRate, w.planner.Fee = 0, 1_000; return nil }, false, nil},
		{"ceiling below the rate with an exact fee", func(w *world, k [32]byte) [][32]byte {
			w.planner.FeeRate, w.planner.MaximumSatPerVByte, w.planner.Fee = 11, 10, 1_000
			return nil
		}, false, nil},
		{"rate equal to the ceiling", func(w *world, k [32]byte) [][32]byte {
			w.planner.FeeRate, w.planner.MaximumSatPerVByte = 2, 2
			return nil
		}, true, nil},
		{"stop before planning", func(w *world, k [32]byte) [][32]byte {
			w.planner.boundary = func(name string) error {
				if name == "before-sweep-plan" {
					return errInjected
				}
				return nil
			}
			return nil
		}, false, errInjected},
		{"main lock unreadable", func(w *world, k [32]byte) [][32]byte {
			j := newFaultJournal()
			j.failReadsFrom = 1
			w.planner.Lock = &MainLock{Journal: j}
			return nil
		}, false, errInjected},
		{"key not eligible", func(w *world, k [32]byte) [][32]byte {
			r := w.chain.records[k]
			r.SweptAt = 3
			w.chain.records[k] = r
			return nil
		}, false, nil},
		{"terms that do not build", func(w *world, k [32]byte) [][32]byte {
			r := w.chain.records[k]
			r.Terms.Depositor = [20]byte{}
			w.chain.records[k] = r
			return nil
		}, false, nil},
		{"output key not the template", func(w *world, k [32]byte) [][32]byte {
			r := w.chain.records[k]
			r.DepositOutputKey[0] ^= 1
			w.chain.records[k] = r
			return nil
		}, false, ErrPlanMismatch},
		{"inputs above the Bitcoin supply", func(w *world, k [32]byte) [][32]byte {
			half := uint64(bitcoin.MaxSatoshi)/2 + 1
			a, b := w.deposit(7, half, 90, [20]byte{}), w.deposit(8, half, 90, [20]byte{})
			return [][32]byte{a, b}
		}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.fund(1_000_000)
			key := w.deposit(1, 100_000, 90, [20]byte{})
			keys := tc.set(w, key)
			if keys == nil {
				keys = [][32]byte{key}
			}
			plan, err := w.planner.Build(ctx, 100, keys, [32]byte{})
			if tc.ok {
				if err != nil {
					t.Fatalf("a valid sweep was refused (%s): %v", tc.name, err)
				}
				return
			}
			if err == nil || plan != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("an invalid sweep was planned (%s): plan %v, %v", tc.name, plan != nil, err)
			}
		})
	}
}

// TestPlannerFeeOverflowBound: the rate sizing FeeRate*vsize must not
// overflow the Bitcoin supply. Invariant: a rate of exactly
// MaxSatoshi/vsize is allowed (an exact proposal fee then sets the fee), and
// one sat/vbyte more is refused. Refs M5-deposit mutation batch.
func TestPlannerFeeOverflowBound(t *testing.T) {
	w := newWorld(t)
	w.fund(1_000_000)
	key := w.deposit(1, 100_000, 90, [20]byte{})
	vsize := planned(t, w, key).Snapshot().VirtualSize()
	bound := bitcoin.MaxSatoshi / vsize
	for _, tc := range []struct {
		rate int64
		ok   bool
	}{{bound, true}, {bound + 1, false}} {
		p := w.planner
		p.FeeRate, p.MaximumSatPerVByte, p.Fee = tc.rate, tc.rate, 1_000
		plan, err := p.Build(ctx, 100, [][32]byte{key}, [32]byte{})
		if tc.ok && err != nil {
			t.Fatalf("rate %d (= MaxSatoshi/vsize %d) refused: %v", tc.rate, vsize, err)
		}
		if !tc.ok && (err == nil || plan != nil) {
			t.Fatalf("rate %d overflows MaxSatoshi at vsize %d and was planned", tc.rate, vsize)
		}
	}
}
