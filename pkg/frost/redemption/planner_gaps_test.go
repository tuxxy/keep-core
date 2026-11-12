package redemption

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// TestDustFloorPerScriptType: the dust floor is Bitcoin Core's default per
// output type, and an output script of no standard type has none (it is
// refused). A P2TR output is exactly OP_1 PUSH32; a near miss is not one.
func TestDustFloorPerScriptType(t *testing.T) {
	defer guard(t)
	p2pkh, _ := bitcoin.PayToPublicKeyHash([20]byte{1})
	p2sh, _ := bitcoin.PayToScriptHash([20]byte{1})
	p2wpkh, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{1})
	p2wsh, _ := bitcoin.PayToWitnessScriptHash([32]byte{1})
	w := newWorld(t)
	p2tr, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	edit := func(s bitcoin.Script, f func([]byte) []byte) bitcoin.Script {
		return bitcoin.Script(f(append([]byte(nil), s...)))
	}
	for _, tc := range []struct {
		name   string
		script bitcoin.Script
		floor  int64
	}{
		{"P2PKH", p2pkh, 546},
		{"P2SH", p2sh, 540},
		{"P2WPKH", p2wpkh, 294},
		{"P2WSH", p2wsh, 330},
		{"P2TR", p2tr, 330},
	} {
		if got, err := DustFloor(tc.script); err != nil || got != tc.floor {
			t.Fatalf("%s: floor %d %v, want %d", tc.name, got, err, tc.floor)
		}
	}
	for _, tc := range []struct {
		name   string
		script bitcoin.Script
	}{
		{"version 2 witness program", edit(p2tr, func(b []byte) []byte { b[0] = 0x52; return b })},
		{"33-byte push", edit(p2tr, func(b []byte) []byte { b[1] = 0x21; return b })},
		{"truncated P2TR", edit(p2tr, func(b []byte) []byte { return b[:33] })},
		{"OP_RETURN", bitcoin.Script{0x6a, 0x01, 0x00}},
	} {
		noPanic(t, "DustFloor", func() {
			if got, err := DustFloor(tc.script); err == nil {
				t.Fatalf("%s: non-standard script given dust floor %d", tc.name, got)
			}
		})
	}
}

// TestNetValueBoundaries (K-05 zero-net rule), through Planner.Build: the
// satoshi domain includes MaxSatoshi itself, a payout exactly at its dust
// floor is standard, and a treasury fee or recipient script outside the
// domain is refused before any plan exists.
func TestNetValueBoundaries(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	edit := func(t *testing.T, f func(*world, *Request)) (*world, *signing.Plan, error) {
		w := newWorld(t)
		// The exact coordinated fee (D19) fixes each request's share at 200.
		w.planner.Fee = 400
		r := w.chain.requests[w.ids[0]]
		f(w, &r)
		w.chain.requests[w.ids[0]] = r
		var plan *signing.Plan
		var err error
		noPanic(t, "Build", func() { plan, _, err = w.planner.Build(ctx, w.chain.head, w.ids) })
		return w, plan, err
	}
	t.Run("gross of exactly MaxSatoshi", func(t *testing.T) {
		defer guard(t)
		_, plan, err := edit(t, func(_ *world, r *Request) {
			r.Gross, r.TreasuryFee = uint64(bitcoin.MaxSatoshi), uint64(bitcoin.MaxSatoshi)-100_000
		})
		if err != nil || plan.Requests()[0].Value != 100_000-200 {
			t.Fatalf("gross of exactly MaxSatoshi refused: %v", err)
		}
	})
	t.Run("payout exactly at the P2WPKH dust floor", func(t *testing.T) {
		defer guard(t)
		_, plan, err := edit(t, func(_ *world, r *Request) { r.Gross, r.TreasuryFee = 294+500+200, 500 })
		if err != nil || plan.Requests()[0].Value != 294 {
			t.Fatalf("payout at the dust floor refused: %v", err)
		}
	})
	t.Run("treasury fee beyond the satoshi domain", func(t *testing.T) {
		defer guard(t)
		if _, plan, err := edit(t, func(_ *world, r *Request) { r.TreasuryFee = uint64(bitcoin.MaxSatoshi) + 1 }); !errors.Is(err, ErrAmountDomain) {
			t.Fatalf("treasury fee beyond the domain: plan %v, %v", plan != nil, err)
		}
	})
	t.Run("recipient script of no standard type", func(t *testing.T) {
		defer guard(t)
		if _, plan, err := edit(t, func(_ *world, r *Request) { r.Script = bitcoin.Script{0x6a, 0x01, 0x00} }); err == nil || plan != nil {
			t.Fatalf("payout to a non-standard script planned: %v", err)
		}
	})
}

func setRequests(w *world, f func(*Request)) {
	for id, r := range w.chain.requests {
		f(&r)
		w.chain.requests[id] = r
	}
}

// TestRedeemableEachCondition: a request is planned or attested only if it
// is Requested, unreserved, of this wallet and descriptor, not paying the
// wallet itself, past its NotBefore and at least DeadlineMargin seconds from
// its deadline. Each failed condition alone excludes it (Pending skips it,
// Build refuses it); each boundary that is still redeemable is accepted.
func TestRedeemableEachCondition(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	refused := []struct {
		name string
		edit func(*world, *Request)
	}{
		{"not Requested", func(_ *world, r *Request) { r.State = RequestReserved }},
		{"already bound to a reservation", func(_ *world, r *Request) { r.ReservationID = [32]byte{1} }},
		{"another wallet", func(_ *world, r *Request) { r.WalletID[0] ^= 1 }},
		{"another descriptor", func(_ *world, r *Request) { r.DescriptorHash[0] ^= 1 }},
		{"pays the wallet change script", func(w *world, r *Request) { r.Script, _ = bitcoin.PayToTaproot(w.wallet.OutputKey) }},
		{"one second before NotBefore", func(w *world, r *Request) { r.NotBefore = w.chain.time + 1 }},
		{"deadline now, no margin", func(w *world, r *Request) { w.planner.DeadlineMargin = 0; r.Deadline = w.chain.time }},
		{"one second inside the margin", func(w *world, r *Request) { r.Deadline = w.chain.time + w.planner.DeadlineMargin - 1 }},
	}
	for _, tc := range refused {
		t.Run("refused/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			setRequests(w, func(r *Request) { tc.edit(w, r) })
			pending, err := w.planner.Pending(ctx, 0, w.chain.head, 0)
			if err != nil || len(pending) != 0 {
				t.Fatalf("Pending listed a non-redeemable request: %x %v", pending, err)
			}
			if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrNotRedeemable) {
				t.Fatalf("Build planned a non-redeemable request: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*world, *Request)
	}{
		{"exactly at NotBefore", func(w *world, r *Request) { r.NotBefore = w.chain.time }},
		{"exactly the margin left", func(w *world, r *Request) { r.Deadline = w.chain.time + w.planner.DeadlineMargin }},
	} {
		t.Run("accepted/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			setRequests(w, func(r *Request) { tc.edit(w, r) })
			pending, err := w.planner.Pending(ctx, 0, w.chain.head, 0)
			if err != nil || len(pending) != 2 {
				t.Fatalf("Pending dropped a redeemable request: %x %v", pending, err)
			}
			if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err != nil {
				t.Fatalf("Build refused a redeemable request: %v", err)
			}
		})
	}
}

// TestPlannerSurfacesReadFailures: a failed or missing chain or Bitcoin read
// is never taken as a redeemable request, a valid plan or a found custody
// output; the planner refuses and reports the failure.
func TestPlannerSurfacesReadFailures(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		edit func(*world, *faultChain)
	}{
		{"configuration", func(_ *world, f *faultChain) { f.configErr = errInjected }},
		{"custody record", func(_ *world, f *faultChain) { f.custodyErr = errInjected }},
		{"custody output", func(_ *world, f *faultChain) { f.outputErr = errInjected }},
		{"governance fee cap", func(_ *world, f *faultChain) { f.maxFeeErr = errInjected }},
		{"request", func(_ *world, f *faultChain) { f.requestErr = errInjected }},
		{"block timestamp", func(_ *world, f *faultChain) { f.timestampErr = errInjected }},
		{"Bitcoin view", func(w *world, _ *faultChain) { w.planner.Bitcoin = failingUnspent{} }},
	} {
		t.Run("build/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			f := &faultChain{fakeChain: w.chain}
			w.planner.Chain = f
			tc.edit(w, f)
			if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, errInjected) {
				t.Fatalf("Build hid a %s read failure: plan %v, %v", tc.name, plan != nil, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*faultChain)
	}{
		{"configuration", func(f *faultChain) { f.configErr = errInjected }},
		{"custody record", func(f *faultChain) { f.custodyErr = errInjected }},
		{"custody output", func(f *faultChain) { f.outputErr = errInjected }},
		{"governance fee cap", func(f *faultChain) { f.maxFeeErr = errInjected }},
		{"request", func(f *faultChain) { f.requestErr = errInjected }},
	} {
		t.Run("validate/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
			if err != nil {
				t.Fatal(err)
			}
			f := &faultChain{fakeChain: w.chain}
			w.planner.Chain = f
			tc.edit(f)
			if err := w.planner.Validate(ctx, w.chain.head, plan); !errors.Is(err, errInjected) {
				t.Fatalf("Validate hid a %s read failure: %v", tc.name, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*world, *faultChain)
	}{
		{"request events", func(_ *world, f *faultChain) { f.eventsErr = errInjected }},
		{"request", func(_ *world, f *faultChain) { f.requestErr = errInjected }},
		{"block timestamp", func(_ *world, f *faultChain) { f.timestampErr = errInjected }},
	} {
		t.Run("pending/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			f := &faultChain{fakeChain: w.chain}
			w.planner.Chain = f
			tc.edit(w, f)
			if ids, err := w.planner.Pending(ctx, 0, w.chain.head, 0); !errors.Is(err, errInjected) {
				t.Fatalf("Pending hid a %s read failure: %x %v", tc.name, ids, err)
			}
		})
	}
	t.Run("pending/event without a request record", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.chain.events = append(w.chain.events, RequestEvent{RequestID: [32]byte{0xee}, WalletID: dkg.WalletID(w.wallet.OutputKey), Block: 95})
		if ids, err := w.planner.Pending(ctx, 0, w.chain.head, 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a log without a request record was not reported: %x %v", ids, err)
		}
		if _, _, err := w.planner.Build(ctx, w.chain.head, [][32]byte{w.ids[0], {0xee}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Build planned a request with no record: %v", err)
		}
	})
}

type failingUnspent struct{}

func (failingUnspent) UnspentByScript(context.Context, bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	return nil, errInjected
}

// TestPendingListsEachRedeemableOnce: Pending returns each redeemable
// request once in log order, however often it is logged, never more than
// the limit, and skips (without failing) a request that is no longer
// redeemable.
func TestPendingListsEachRedeemableOnce(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	w := newWorld(t)
	walletID := dkg.WalletID(w.wallet.OutputKey)
	a, b := w.ids[0], w.ids[1]
	w.chain.events = []RequestEvent{{RequestID: a, WalletID: walletID, Block: 91}, {RequestID: a, WalletID: walletID, Block: 92}, {RequestID: b, WalletID: walletID, Block: 93}}
	got, err := w.planner.Pending(ctx, 0, w.chain.head, 0)
	if err != nil || len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("duplicate log: pending %x %v, want [a b]", got, err)
	}
	if got, err = w.planner.Pending(ctx, 0, w.chain.head, 1); err != nil || len(got) != 1 || got[0] != a {
		t.Fatalf("limit 1: pending %x %v, want [a]", got, err)
	}
	r := w.chain.requests[a]
	r.State = RequestReserved
	w.chain.requests[a] = r
	if got, err = w.planner.Pending(ctx, 0, w.chain.head, 0); err != nil || len(got) != 1 || got[0] != b {
		t.Fatalf("a reserved request: pending %x %v, want [b]", got, err)
	}
}

// decoyUnspent returns another output of the wallet script first, with the
// main output's value, then the recognized main output (fixture).
type decoyUnspent struct{ decoy, main bitcoin.PreviousOutput }

func (u decoyUnspent) UnspentByScript(context.Context, bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	return []bitcoin.PreviousOutput{u.decoy, u.main}, nil
}

// wrongScriptUnspent returns the main outpoint under another script.
type wrongScriptUnspent struct{ main bitcoin.PreviousOutput }

func (u wrongScriptUnspent) UnspentByScript(context.Context, bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	p := u.main
	p.PublicKeyScript, _ = bitcoin.PayToWitnessPublicKeyHash([20]byte{9})
	return []bitcoin.PreviousOutput{p}, nil
}

// TestBuildSpendsOnlyTheRecognizedMain: the plan spends the output whose
// outpoint is the C-04 main custody key and whose script is the wallet's,
// skipping other outputs of the script; a main whose record is spent, of
// another wallet or of another value is not custody.
func TestBuildSpendsOnlyTheRecognizedMain(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	t.Run("decoy listed first", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		decoy := w.main
		decoy.Outpoint.TransactionHash = bitcoin.Hash{0x44}
		w.planner.Bitcoin = decoyUnspent{decoy: decoy, main: w.main}
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatalf("main behind another wallet output not found: %v", err)
		}
		if plan.ConflictInput() != w.main.Outpoint {
			t.Fatalf("plan spends %v, not the recognized main", plan.ConflictInput())
		}
	})
	t.Run("main outpoint under another script", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Bitcoin = wrongScriptUnspent{w.main}
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrNoCustody) {
			t.Fatalf("main with a foreign script accepted: %v", err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*CustodyOutput)
	}{
		{"spent", func(o *CustodyOutput) { o.Spent = true }},
		{"another wallet", func(o *CustodyOutput) { o.WalletID[0] ^= 1 }},
	} {
		t.Run("custody record "+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			tc.edit(&o)
			w.chain.outputs[k] = o
			if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrNoCustody) {
				t.Fatalf("main whose custody record is %s accepted: %v", tc.name, err)
			}
		})
	}
	t.Run("main too small to size a transaction", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		setMain(w, 1)
		noPanic(t, "Build", func() {
			if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err == nil {
				t.Fatalf("a 1-sat main was planned: %v", plan != nil)
			}
		})
	})
}

// manyRequests replaces the world's requests with n funded P2WPKH requests.
func manyRequests(w *world, n int) {
	walletID := dkg.WalletID(w.wallet.OutputKey)
	descriptorHash, _ := dkg.DescriptorHash(w.wallet)
	w.chain.requests = map[[32]byte]Request{}
	w.chain.events = nil
	w.ids = nil
	for i := 0; i < n; i++ {
		recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{byte(i), byte(i >> 8), 0x5a})
		id := [32]byte{0xa1, byte(i), byte(i >> 8)}
		w.chain.requests[id] = Request{ID: id, WalletID: walletID, DescriptorHash: descriptorHash, Gross: 100_000, TreasuryFee: 500, MaximumBitcoinFee: 2000, Deadline: w.chain.time + 3600, FinalityBlocks: 2, State: RequestRequested, Script: recipient}
		w.ids = append(w.ids, id)
	}
}

// TestBuildRequestCountAndConfiguration: a plan has 1..MaxRedemptionRequests
// requests; redemptions must be enabled on the planner's own network, and the
// wallet must have a main output and no active reservation. Each refusal
// holds on its own.
func TestBuildRequestCountAndConfiguration(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	t.Run("no requests", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		noPanic(t, "Build", func() {
			if plan, _, err := w.planner.Build(ctx, w.chain.head, nil); err == nil || plan != nil {
				t.Fatalf("an empty plan was built: %v", err)
			}
		})
	})
	t.Run("exactly the maximum", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		manyRequests(w, signing.MaxRedemptionRequests)
		setMain(w, 10_000_000)
		w.chain.maxTotalFee = 20_000
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err != nil {
			t.Fatalf("%d requests refused: %v", signing.MaxRedemptionRequests, err)
		}
	})
	t.Run("one above the maximum", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		manyRequests(w, signing.MaxRedemptionRequests+1)
		setMain(w, 10_000_000)
		w.chain.maxTotalFee = 20_000
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrNotRedeemable) {
			t.Fatalf("%d requests: %v", signing.MaxRedemptionRequests+1, err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*world)
	}{
		{"redemptions disabled", func(w *world) { w.chain.config.Enabled = false }},
		{"another network", func(w *world) { w.chain.config.BitcoinGenesis[0] ^= 1 }},
		{"active reservation", func(w *world) {
			c := w.chain.custody[dkg.WalletID(w.wallet.OutputKey)]
			c.ActiveReservation = [32]byte{1}
			w.chain.custody[dkg.WalletID(w.wallet.OutputKey)] = c
		}},
		{"no main output", func(w *world) {
			c := w.chain.custody[dkg.WalletID(w.wallet.OutputKey)]
			c.MainOutpoint = [32]byte{}
			w.chain.custody[dkg.WalletID(w.wallet.OutputKey)] = c
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			tc.edit(w)
			if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err == nil || plan != nil {
				t.Fatalf("plan built: %v", err)
			}
		})
	}
}

// TestBuildFeeBoundaries (D19, K-05 fee domain): the fee-rate envelope holds
// even when the coordinated exact fee replaces rate sizing; the fee may equal
// the certificate minimum or the governance maximum; the change may equal the
// P2TR dust floor; an overflowing fee rate never wraps into an admissible fee.
func TestBuildFeeBoundaries(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	t.Run("zero fee rate with an exact fee", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fees.FeeRate, w.planner.Fee = 0, 500
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrFeeDomain) {
			t.Fatalf("zero fee rate accepted: %v", err)
		}
	})
	t.Run("zero certificate minimum", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fees.Minimum = 0
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrFeeDomain) {
			t.Fatalf("zero minimum fee accepted: %v", err)
		}
	})
	t.Run("rate equal to the envelope", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fees.FeeRate = w.planner.Fees.MaximumSatPerVByte
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err != nil {
			t.Fatalf("rate at the envelope refused: %v", err)
		}
	})
	t.Run("fee equal to the minimum", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fee = w.planner.Fees.Minimum
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil || plan.Snapshot().Fee() != w.planner.Fees.Minimum {
			t.Fatalf("fee at the minimum refused: %v", err)
		}
	})
	t.Run("fee equal to the governance maximum", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fees.MaximumSatPerVByte = 1000
		w.chain.maxTotalFee = 3000
		w.planner.Fee = int64(w.chain.maxTotalFee)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil || plan.Snapshot().Fee() != int64(w.chain.maxTotalFee) {
			t.Fatalf("fee at the governance maximum refused: %v", err)
		}
	})
	t.Run("change equal to the P2TR dust floor", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fee = 400
		// 2 x (100,000 - 500 - 200) paid, 400 fee, 330 change.
		setMain(w, 2*99_300+400+330)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatalf("change at the dust floor refused: %v", err)
		}
		if out := plan.Snapshot().Unsigned().Outputs; out[len(out)-1].Value != 330 {
			t.Fatalf("change %d", out[len(out)-1].Value)
		}
	})
	t.Run("exact fee above the certificate rate", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		w.planner.Fee = 4000
		if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err == nil || plan != nil {
			t.Fatalf("fee above the certificate rate planned: %v", err)
		}
	})
	t.Run("largest rate inside the satoshi domain, exact fee", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal(err)
		}
		// The coordinated exact fee (D19, the override Rebuild sets) pays the
		// plan; the rate only has to pass the overflow guard, inclusively.
		rate := bitcoin.MaxSatoshi / plan.Snapshot().VirtualSize()
		w.planner.Fees.FeeRate, w.planner.Fees.MaximumSatPerVByte, w.planner.Fee = rate, rate, 500
		if _, _, err := w.planner.Build(ctx, w.chain.head, w.ids); err != nil {
			t.Fatalf("rate %d (MaxSatoshi/vsize) with an exact fee refused: %v", rate, err)
		}
	})
	t.Run("overflowing fee rate", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal(err)
		}
		vsize := plan.Snapshot().VirtualSize()
		// The smallest rate whose product with vsize wraps past 2^64, plus
		// enough steps to land the wrapped fee inside [Minimum, maximum].
		rate := uint64(math.MaxUint64)/uint64(vsize) + 1
		for int64(rate*uint64(vsize)) < w.planner.Fees.Minimum {
			rate++
		}
		w.planner.Fees.FeeRate, w.planner.Fees.MaximumSatPerVByte = int64(rate), int64(rate)
		if plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids); !errors.Is(err, ErrFeeDomain) {
			t.Fatalf("rate %d wrapped into a planned fee: %v %v", rate, plan != nil, err)
		}
	})
}

// atAdmission answers request and custody reads as of a block (fixture): from
// the admission block on, the requests are Reserved under the reservation and
// the custody slot is taken.
func atAdmission(w *world, f *faultChain, admitted uint64, reservation [32]byte) {
	f.requestAt = func(at uint64, r Request) Request {
		if at >= admitted {
			r.State, r.ReservationID = RequestReserved, reservation
		}
		return r
	}
	walletID := dkg.WalletID(w.wallet.OutputKey)
	before := w.chain.custody[walletID]
	f.custodyAt = func(at uint64, wallet [32]byte) (Custody, bool) {
		if wallet != walletID || at < admitted {
			return Custody{}, false
		}
		c := before
		c.Generation, c.ActiveReservation = before.Generation+1, reservation
		return c, true
	}
}

// TestRebuildReadsStateBeforeAdmission (ADV-D2): a member without a retained
// plan rebuilds from chain state at the block before admission (afterwards
// the requests are Reserved); it refuses a record with no admission block,
// no requests or payouts that do not pair with the requests, and it reports
// any read or build failure instead of returning no plan and no error.
func TestRebuildReadsStateBeforeAdmission(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	admitted := func(t *testing.T, w *world) Intent {
		t.Helper()
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal(err)
		}
		intent := Intent{AdmittedBlock: w.chain.head + 1, RequestIDs: w.ids, Certificate: Certificate{IntentCommitment: plan.Commitment()}}
		for _, r := range plan.Requests() {
			intent.Payouts = append(intent.Payouts, uint64(r.Value))
		}
		return intent
	}
	t.Run("state at the admission block is not used", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		intent := admitted(t, w)
		f := &faultChain{fakeChain: w.chain}
		w.planner.Chain = f
		atAdmission(w, f, intent.AdmittedBlock, [32]byte{0x5e})
		if _, err := w.planner.Rebuild(ctx, intent); err != nil {
			t.Fatalf("rebuild from the block before admission failed: %v", err)
		}
	})
	t.Run("gross of exactly MaxSatoshi", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		setRequests(w, func(r *Request) {
			r.Gross, r.TreasuryFee = uint64(bitcoin.MaxSatoshi), uint64(bitcoin.MaxSatoshi)-100_000
		})
		intent := admitted(t, w)
		if _, err := w.planner.Rebuild(ctx, intent); err != nil {
			t.Fatalf("admitted request of MaxSatoshi gross not rebuilt: %v", err)
		}
	})
	t.Run("a request whose fee share is zero", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		// A one-sat fee splits as shares 0 and 1: the first payout is its
		// whole gross minus treasury fee.
		w.planner.Fees.Minimum, w.planner.Fee = 1, 1
		intent := admitted(t, w)
		rebuilder := w.planner
		rebuilder.Fee = 0
		if _, err := rebuilder.Rebuild(ctx, intent); err != nil {
			t.Fatalf("admitted payout equal to gross minus treasury fee refused: %v", err)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*Intent)
	}{
		{"no admission block", func(i *Intent) { i.AdmittedBlock = 0 }},
		{"no requests", func(i *Intent) { i.RequestIDs, i.Payouts = nil, nil }},
		{"an extra payout", func(i *Intent) { i.Payouts = append(i.Payouts, 1) }},
		{"a missing payout", func(i *Intent) { i.Payouts = i.Payouts[:1] }},
		{"payout above gross", func(i *Intent) { i.Payouts[0] = 100_000 }},
	} {
		t.Run("refused/"+tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			intent := admitted(t, w)
			tc.edit(&intent)
			noPanic(t, "Rebuild", func() {
				if plan, err := w.planner.Rebuild(ctx, intent); err == nil || plan != nil {
					t.Fatalf("rebuilt from an unrebuildable record: %v", err)
				}
			})
		})
	}
	t.Run("refused/request read fails", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		intent := admitted(t, w)
		w.planner.Chain = &faultChain{fakeChain: w.chain, requestErr: errInjected}
		if plan, err := w.planner.Rebuild(ctx, intent); !errors.Is(err, errInjected) || plan != nil {
			t.Fatalf("request read failure hidden: %v", err)
		}
	})
	t.Run("refused/build fails", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		intent := admitted(t, w)
		w.chain.config.Enabled = false
		noPanic(t, "Rebuild", func() {
			if plan, err := w.planner.Rebuild(ctx, intent); err == nil || plan != nil {
				t.Fatalf("rebuild with redemptions disabled: %v", err)
			}
		})
	})
}

// TestValidateEachCondition: the attester's independent check refuses a
// supplied plan when any single condition fails, and accepts each boundary
// that is still valid.
func TestValidateEachCondition(t *testing.T) {
	defer guard(t)
	ctx := context.Background()
	t.Run("no plan", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		noPanic(t, "Validate", func() {
			if err := w.planner.Validate(ctx, w.chain.head, nil); err == nil {
				t.Fatal("nil plan accepted")
			}
		})
	})
	t.Run("a cancellation plan", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		payment := directPlan(t, w, []int64{99_300, 99_300}, 1_000_000-199_000, 0)
		walletScript, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
		point := w.main.Outpoint
		cancel, err := signing.NewCancellationPlan(payment, &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: 0xfffffffd}}, Outputs: []*bitcoin.TransactionOutput{{Value: 999_000, PublicKeyScript: walletScript}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.planner.Validate(ctx, w.chain.head, cancel); err == nil {
			t.Fatal("cancellation validated as a redemption")
		}
	})
	t.Run("a plan of another network", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal(err)
		}
		w.planner.Genesis[0] ^= 1
		w.chain.config.BitcoinGenesis = w.planner.Genesis
		if err := w.planner.Validate(ctx, w.chain.head, plan); err == nil {
			t.Fatal("regtest plan validated on another network")
		}
	})
	t.Run("a plan of another wallet", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
		if err != nil {
			t.Fatal(err)
		}
		other := w.planner
		other.Wallet.OutputKey = otherKey(t)
		if err := other.Validate(ctx, w.chain.head, plan); err == nil {
			t.Fatal("plan of another wallet accepted")
		}
	})
	type edit func(*world, *signing.Plan)
	for _, tc := range []struct {
		name string
		edit edit
	}{
		{"conflict input is not the main", func(w *world, _ *signing.Plan) {
			c := w.chain.custody[dkg.WalletID(w.wallet.OutputKey)]
			c.MainOutpoint[0] ^= 1
			w.chain.custody[dkg.WalletID(w.wallet.OutputKey)] = c
		}},
		{"input of another wallet", func(w *world, _ *signing.Plan) {
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.WalletID[0] ^= 1
			w.chain.outputs[k] = o
		}},
		{"input of another value", func(w *world, _ *signing.Plan) {
			k := OutpointKey(w.main.Outpoint)
			o := w.chain.outputs[k]
			o.Value++
			w.chain.outputs[k] = o
		}},
		{"fee rate above the attester's envelope", func(w *world, _ *signing.Plan) { w.planner.Fees.MaximumSatPerVByte-- }},
		{"minimum below the attester's", func(w *world, _ *signing.Plan) { w.planner.Fees.Minimum++ }},
		{"request script changed on chain", func(w *world, _ *signing.Plan) {
			r := w.chain.requests[w.ids[0]]
			r.Script, _ = bitcoin.PayToWitnessPublicKeyHash([20]byte{0x77})
			w.chain.requests[w.ids[0]] = r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer guard(t)
			w := newWorld(t)
			plan, _, err := w.planner.Build(ctx, w.chain.head, w.ids)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(w, plan)
			if err := w.planner.Validate(ctx, w.chain.head, plan); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
	t.Run("fee shares do not sum to the fee", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		// Payouts imply 200-sat shares (400 total) but the change leaves a
		// 500-sat fee.
		plan := directPlan(t, w, []int64{99_300, 99_300}, 1_000_000-199_100, 0)
		if err := w.planner.Validate(ctx, w.chain.head, plan); err == nil {
			t.Fatal("shares unequal to the fee accepted")
		}
	})
	t.Run("change exactly at the P2TR dust floor", func(t *testing.T) {
		defer guard(t)
		w := newWorld(t)
		setMain(w, 199_330)
		plan := directPlan(t, w, []int64{99_300, 99_300}, 330, 0)
		if err := w.planner.Validate(ctx, w.chain.head, plan); err != nil {
			t.Fatalf("change at the dust floor refused: %v", err)
		}
	})
}

// otherKey is a valid x-only key that is not the unit wallet's.
func otherKey(t *testing.T) [32]byte {
	t.Helper()
	k := newWorld(t).wallet.OutputKey
	for i := byte(1); i != 0; i++ {
		k[31]++
		if _, err := bitcoin.PayToTaproot(k); err == nil {
			return k
		}
	}
	t.Fatal("no other key")
	return k
}
