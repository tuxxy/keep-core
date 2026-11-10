package enforcement

// Mutation batch M3 (MUTATION-REPORT-2026-11-10 sections 3-4, keep-core
// pkg/frost/enforcement inactivity.go): oracles for the activity rule, the
// claim window (lookback, previous claim, never-started rounds, stop at the
// required failures), the signing seat filter and the submit settlement.
// Faults come from the labelled fixtures in faults_test.go; heartbeats are
// recorded through the real Runner with its `sign` seam (failRound).

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

func gapKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func gapClaimer(t *testing.T, r *Runner, chain Chain, rec Records) *Claimer {
	t.Helper()
	return &Claimer{Chain: chain, Runner: r, Records: rec, LocalSeats: []uint16{1}, Key: gapKey(t), Failures: 2}
}

// deriveSafely runs Derive and reports a panic separately from an error, so a
// refusal is told apart from a crash.
func deriveSafely(c *Claimer) (claim Claim, through uint64, err error, panicked any) {
	defer func() { panicked = recover() }()
	claim, through, err = c.Derive(context.Background())
	return
}

// Invariant (base keep-core heartbeat activity rule, MinimumActivePercent):
// a heartbeat with present*100 < percent*seats is a failure, signed or not;
// at or above that bound it is no failure, and it ends the streak only if
// signed. With no percent every unsigned heartbeat fails.
// Kills KC-dd30f90d43 (:53 * -> /), KC-07b8bc8255 (:53 < -> <=) and
// KC-334b2c7ce4 (:56 false->true).
func TestClaimerActivityRule(t *testing.T) {
	for _, tc := range []struct {
		percent, present int
		signed           bool
		failure, ends    bool
	}{
		{0, 0, false, true, false},
		{0, 4, true, false, true},
		{50, 1, false, true, false},
		{50, 1, true, true, false},
		{50, 2, false, false, false}, // exactly at the bound: 200 == 50*4
		{50, 2, true, false, true},
		{50, 4, true, false, true},
		{100, 3, true, true, false},
		{100, 4, false, false, false},
	} {
		c := &Claimer{MinimumActivePercent: tc.percent}
		o := Outcome{Signed: tc.signed, Present: make([]uint16, tc.present)}
		if failure, ends := c.failed(o, 4); failure != tc.failure || ends != tc.ends {
			t.Errorf("percent %d, %d of 4 present, signed %v: failure=%v ends=%v, want %v %v", tc.percent, tc.present, tc.signed, failure, ends, tc.failure, tc.ends)
		}
	}
	t.Run("through Derive", func(t *testing.T) {
		// Two unsigned heartbeats with 2 of 3 seats present: at 50% that is
		// enough presence, so neither counts and no claim follows; with only
		// seat 1 present both fail and seats 2 and 3 are claimed.
		for _, tc := range []struct {
			present []uint16
			want    []uint16
		}{{[]uint16{1, 2}, nil}, {[]uint16{1}, []uint16{2, 3}}} {
			r, f, rec := runnerFixture(t)
			failRound(t, r, f, 35, tc.present, 0)
			failRound(t, r, f, 45, tc.present, 0)
			c := gapClaimer(t, r, f, rec)
			c.MinimumActivePercent = 50
			claim, _, err := c.Derive(context.Background())
			if tc.want == nil {
				if !errors.Is(err, ErrNoClaim) {
					t.Fatalf("present %v: heartbeats with enough presence were claimed: %+v %v", tc.present, claim, err)
				}
				continue
			}
			if err != nil || !reflect.DeepEqual(claim.Inactive, tc.want) {
				t.Fatalf("present %v: claim %+v (err %v), want %v", tc.present, claim, err, tc.want)
			}
		}
	})
}

// Invariant: Derive needs at least one required failure and a runner; one
// required failure is a valid configuration. An incomplete configuration is
// refused with an error, never a claim or a crash.
// Kills KC-59227ffb86 (:84 < -> <=), KC-73a9948d83 (:84 ||->&&) and
// KC-0c3cb239c9 (:85 err->nil).
func TestClaimerConfiguration(t *testing.T) {
	t.Run("one required failure", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		c := gapClaimer(t, r, f, rec)
		c.Failures = 1
		claim, through, err, panicked := deriveSafely(c)
		if panicked != nil || err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || through != 40 {
			t.Fatalf("Failures=1 must claim one failed round: %+v %d %v %v", claim, through, err, panicked)
		}
	})
	for _, tc := range []struct {
		name     string
		failures int
		runner   bool
	}{{"no required failures", 0, true}, {"negative required failures", -1, true}, {"no runner", 2, false}} {
		t.Run(tc.name, func(t *testing.T) {
			r, f, rec := runnerFixture(t)
			failRound(t, r, f, 35, []uint16{1, 2}, 0)
			failRound(t, r, f, 45, []uint16{1, 2}, 0)
			c := gapClaimer(t, r, f, rec)
			c.Failures = tc.failures
			if !tc.runner {
				c.Runner = nil
			}
			claim, _, err, panicked := deriveSafely(c)
			if panicked != nil {
				t.Fatalf("an incomplete configuration crashed Derive: %v", panicked)
			}
			if err == nil || errors.Is(err, ErrNoClaim) {
				t.Fatalf("an incomplete configuration was not refused as one: %+v %v", claim, err)
			}
		})
	}
}

// Invariant: Derive returns every failure it meets on its way (boundary stop,
// nonce read, previous claim record, current round, outcome record) and
// yields no claim from it.
// Kills KC-a191647f32 (:82), KC-4302a85db2 (:91), KC-57747263ba (:99),
// KC-49939fdc6b (:103), KC-a812935995 (:108), KC-3ffb40d942 (:138) and
// KC-abbe9943f7 (:178), all err->nil.
func TestClaimerDeriveFaults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fault func(c *Claimer, f *faultChain, rec *faultRecords, wallet [32]byte)
	}{
		{"boundary before the claim read", func(c *Claimer, _ *faultChain, _ *faultRecords, _ [32]byte) { c.boundary = failAt("before-claim-read") }},
		{"inactivity nonce", func(_ *Claimer, f *faultChain, _ *faultRecords, _ [32]byte) { f.nonceErr = errInjected }},
		{"corrupt previous claim record", func(_ *Claimer, f *faultChain, rec *faultRecords, w [32]byte) {
			f.nonce = 1
			rec.m[claimRecordID(w, 0)] = []byte("not a claim record") // fixture record
		}},
		{"previous claim record unreadable", func(_ *Claimer, f *faultChain, rec *faultRecords, w [32]byte) {
			f.nonce = 1
			rec.readErr[claimRecordID(w, 0)] = errInjected
		}},
		{"no current round", func(_ *Claimer, f *faultChain, _ *faultRecords, _ [32]byte) { f.head = 1 }},
		{"corrupt outcome in the window", func(_ *Claimer, _ *faultChain, rec *faultRecords, w [32]byte) {
			rec.m[outcomeID(w, 40)] = []byte("not an outcome") // fixture record
		}},
		{"boundary after the claim was derived", func(c *Claimer, _ *faultChain, _ *faultRecords, _ [32]byte) { c.boundary = failAt("claim-derived") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, base, mem := runnerFixture(t)
			failRound(t, r, base, 35, []uint16{1, 2}, 0)
			failRound(t, r, base, 45, []uint16{1, 2}, 0)
			f, rec := &faultChain{fakeChain: base}, newFaultRecords(mem)
			c := gapClaimer(t, r, f, rec)
			tc.fault(c, f, rec, dkg.WalletID(r.Wallet.OutputKey))
			claim, _, err, panicked := deriveSafely(c)
			if panicked != nil || err == nil || errors.Is(err, ErrNoClaim) || claim.Inactive != nil {
				t.Fatalf("a Derive failure was hidden: %+v %v %v", claim, err, panicked)
			}
		})
	}
}

// Invariant: a claim's window starts after the previous claim's window: with
// no previous claim at nonce 0 (no record is consulted below nonce 0), and
// with a previous nonce whose record this host never retained, from the
// start; otherwise only heartbeats strictly after its Through count.
// Kills KC-564fdc254f (:94 > -> >=), KC-af48721701 (:102 negated) and
// KC-437f070745 (:123 <= -> <).
func TestClaimerPreviousWindow(t *testing.T) {
	ctx := context.Background()
	t.Run("nonce 0 has no previous claim", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		wallet := dkg.WalletID(r.Wallet.OutputKey)
		// Fixture: a record under the wrapped nonce 0-1 must not be read as a
		// previous claim.
		raw, _ := json.Marshal(claimRecord{Through: 1 << 40})
		rec.m[claimRecordID(wallet, ^uint64(0))] = raw
		claim, through, err := gapClaimer(t, r, f, rec).Derive(ctx)
		if err != nil || claim.Nonce != 0 || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || through != 40 {
			t.Fatalf("the first claim was narrowed by a wrapped record: %+v %d %v", claim, through, err)
		}
	})
	t.Run("a previous claim another operator submitted", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		f.nonce = 1 // claim 0 was submitted without this host's record
		claim, through, err := gapClaimer(t, r, f, rec).Derive(ctx)
		if err != nil || claim.Nonce != 1 || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || through != 40 {
			t.Fatalf("a missing previous record must not block the claim: %+v %d %v", claim, through, err)
		}
	})
	t.Run("the previous window's last heartbeat is not counted again", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		c := gapClaimer(t, r, f, rec)
		claim, through, err := c.Derive(ctx)
		if err != nil || through != 40 {
			t.Fatal(claim, through, err)
		}
		sigs, err := c.Sign(ctx, claim) // retains the window through 40
		if err != nil {
			t.Fatal(err)
		}
		seats := make([]uint16, 0, len(sigs))
		for s := range sigs {
			seats = append(seats, s)
		}
		if err = c.Submit(ctx, claim, seats, nil); err != nil || f.nonce != 1 {
			t.Fatal(err)
		}
		failRound(t, r, f, 55, []uint16{1, 2}, 0)
		if next, _, err := c.Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatalf("heartbeat 40, already claimed, was counted again: %+v %v", next, err)
		}
		failRound(t, r, f, 65, []uint16{1, 2}, 0)
		next, through, err := c.Derive(ctx)
		if err != nil || next.Nonce != 1 || through != 60 || !reflect.DeepEqual(next.Inactive, []uint16{3}) {
			t.Fatalf("two new failures after the window must claim: %+v %d %v", next, through, err)
		}
	})
}

// Invariant: Derive scans at most Lookback rounds back from the current one,
// skips rounds that never started, and stops as soon as it has the required
// failures (older heartbeats do not change the claim).
// Kills KC-a8757f2f40 (:121 k<lookback -> <=), KC-5a5ac1bee5 and
// KC-c2cfd1acdf (:121 &&->||), KC-cadfa9234f (:121 len(failed) < -> <=) and
// KC-56e73fec25 (:135 continue->break).
func TestClaimerLookbackWindow(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		lookback uint64
		claims   bool
	}{{1, false}, {2, true}, {0, true}} { // 0 is the default, 64
		t.Run(fmt.Sprintf("lookback %d", tc.lookback), func(t *testing.T) {
			r, f, rec := runnerFixture(t)
			failRound(t, r, f, 35, []uint16{1, 2}, 0)
			failRound(t, r, f, 45, []uint16{1, 2}, 0)
			c := gapClaimer(t, r, f, rec)
			c.Lookback = tc.lookback
			claim, _, err := c.Derive(ctx)
			if tc.claims != (err == nil) {
				t.Fatalf("lookback %d: claim %+v, err %v", tc.lookback, claim, err)
			}
			if !tc.claims && !errors.Is(err, ErrNoClaim) {
				t.Fatalf("lookback %d: %v", tc.lookback, err)
			}
		})
	}
	t.Run("stops at the required failures", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 25, []uint16{1, 2, 3}, 0) // older failure, seat 3 present
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		claim, through, err := gapClaimer(t, r, f, rec).Derive(ctx)
		if err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || through != 40 {
			t.Fatalf("a heartbeat older than the required failures changed the claim: %+v %d %v", claim, through, err)
		}
	})
	t.Run("a round that never started is skipped", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 25, []uint16{1, 2}, 0)
		// Round 30 never ran: no start record, no outcome.
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		claim, through, err := gapClaimer(t, r, f, rec).Derive(ctx)
		if err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || through != 40 {
			t.Fatalf("a never-started round ended the window: %+v %d %v", claim, through, err)
		}
	})
}

// Invariant: a vacant seat (member 0) is never claimed and never stops the
// seats after it from being claimed.
// Kills KC-1a4c104af3 (:155 continue->break).
func TestClaimerSkipsVacantSeats(t *testing.T) {
	r, f, rec := runnerFixture(t)
	r.Wallet.Members = []uint32{11, 0, 13}
	failRound(t, r, f, 35, []uint16{1}, 0)
	failRound(t, r, f, 45, []uint16{1}, 0)
	claim, _, err := gapClaimer(t, r, f, rec).Derive(context.Background())
	if err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) {
		t.Fatalf("seat 3 after a vacant seat 2: %+v %v", claim, err)
	}
}

// Invariant: Sign signs only for local seats outside the claimed set,
// whatever their order; it returns every failure (no claim, an unusable
// retained claim, a signing key that cannot sign) and signs nothing then.
// Kills KC-9f5e5598b7 (:216 ||->&&), KC-18527455b5 (:219 continue->break),
// KC-84ba58aa25 (:194), KC-a02ac34f34 (:210) and KC-fbc16e42c0 (:223)
// err->nil.
func TestClaimerSignFaults(t *testing.T) {
	ctx := context.Background()
	derived := func(t *testing.T) (*Claimer, Claim) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		c := gapClaimer(t, r, f, rec)
		claim, _, err := c.Derive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return c, claim
	}
	for _, local := range [][]uint16{{1, 3}, {3, 1}, {3, 2, 1}} {
		t.Run(fmt.Sprintf("local seats %v", local), func(t *testing.T) {
			c, claim := derived(t)
			c.LocalSeats = local
			sigs, err := c.Sign(ctx, claim)
			if err != nil {
				t.Fatal(err)
			}
			var signed []uint16
			for s := range sigs {
				signed = append(signed, s)
			}
			sort.Slice(signed, func(i, j int) bool { return signed[i] < signed[j] })
			var want []uint16
			for _, s := range local {
				if s != 3 {
					want = append(want, s)
				}
			}
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if !reflect.DeepEqual(signed, want) {
				t.Fatalf("signed seats %v, want %v (seat 3 is claimed)", signed, want)
			}
		})
	}
	t.Run("no claim", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		c := gapClaimer(t, r, f, rec)
		proposed := Claim{WalletID: dkg.WalletID(r.Wallet.OutputKey), Inactive: []uint16{3}, HeartbeatFailed: true}
		if sigs, err := c.Sign(ctx, proposed); !errors.Is(err, ErrNoClaim) || sigs != nil {
			t.Fatalf("signed without a supported claim: %v %v", sigs, err)
		}
	})
	t.Run("retained claim with no inactive seats", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		c := gapClaimer(t, r, f, rec)
		proposed := Claim{WalletID: dkg.WalletID(r.Wallet.OutputKey), HeartbeatFailed: true}
		// Fixture: a retained record this host never writes (Derive never
		// yields an empty claim); its digest cannot be built.
		raw, _ := json.Marshal(claimRecord{Claim: proposed, Through: 40})
		rec.m[claimRecordID(proposed.WalletID, 0)] = raw
		if sigs, err := c.Sign(ctx, proposed); err == nil || sigs != nil {
			t.Fatalf("signed an empty claim: %v %v", sigs, err)
		}
	})
	t.Run("signing key cannot sign", func(t *testing.T) {
		c, claim := derived(t)
		c.Key = &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: crypto.S256()}, D: big.NewInt(0)} // fixture: invalid key
		if sigs, err := c.Sign(ctx, claim); err == nil || sigs != nil {
			t.Fatalf("signing failure hidden: %v %v", sigs, err)
		}
	})
}

// Invariant: Submit returns a boundary stop or nonce read failure without
// submitting, counts a failed submission as done only when the chain nonce
// moved past the claim, and returns the failure otherwise.
// Kills KC-7040145df6 (:254), KC-f73affd9f7 (:258) err->nil; KC-102eb129e6
// (:265 !=->==), KC-e72d3c28c4 (:265 ||->&&), KC-fb1ca0ddb1 (:265 <= -> <),
// KC-51d4c8e3e9 (:265 <= -> >) and KC-efbc99f487 (:266 err->nil).
func TestClaimerSubmitSettlement(t *testing.T) {
	ctx := context.Background()
	newWorld := func(t *testing.T) (*Claimer, *fakeChain, *faultChain, Claim) {
		r, base, rec := runnerFixture(t)
		f := &faultChain{fakeChain: base}
		c := gapClaimer(t, r, f, rec)
		return c, base, f, Claim{WalletID: dkg.WalletID(r.Wallet.OutputKey), Inactive: []uint16{3}, HeartbeatFailed: true}
	}
	t.Run("boundary before submit", func(t *testing.T) {
		c, base, _, claim := newWorld(t)
		c.boundary = failAt("before-claim-submit")
		if err := c.Submit(ctx, claim, []uint16{1}, nil); !errors.Is(err, errInjected) || len(base.claims) != 0 {
			t.Fatalf("a stop before submit: %v, %d submitted", err, len(base.claims))
		}
	})
	t.Run("nonce read failure", func(t *testing.T) {
		c, base, f, claim := newWorld(t)
		f.nonceErr = errInjected
		if err := c.Submit(ctx, claim, []uint16{1}, nil); !errors.Is(err, errInjected) || len(base.claims) != 0 {
			t.Fatalf("a nonce read failure: %v, %d submitted", err, len(base.claims))
		}
	})
	t.Run("a lost reply is settled by the nonce", func(t *testing.T) {
		c, base, _, claim := newWorld(t)
		base.lostReply = true
		if err := c.Submit(ctx, claim, []uint16{1}, nil); err != nil || base.nonce != 1 {
			t.Fatalf("a submission the chain accepted was reported failed: %v", err)
		}
	})
	t.Run("a refused submission is returned", func(t *testing.T) {
		c, base, f, claim := newWorld(t)
		f.notifyInactivityErr = errInjected
		if err := c.Submit(ctx, claim, []uint16{1}, nil); !errors.Is(err, errInjected) || base.nonce != 0 {
			t.Fatalf("a refused submission was reported as %v", err)
		}
	})
}
