package enforcement

// Mutation batch M3 (MUTATION-REPORT-2026-11-10 sections 3-4, keep-core
// pkg/frost/enforcement defender.go): oracles for the defender's challenge
// read, its per-challenge skips, its record checks and its timeout notifier.
// The chain and record faults come from the labelled fixtures in
// faults_test.go.

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// heartbeatDefense retains this host's heartbeat defense record for nonce
// and returns its digest.
func heartbeatDefense(t *testing.T, r *memRecords, nonce uint64) [32]byte {
	t.Helper()
	digest := signing.HeartbeatDigest(testWallet, nonce)
	retainDefense(t, r, signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: digest, Kind: signing.DefenseHeartbeat, Nonce: nonce})
	return digest
}

// countEach counts each value, so a check is independent of processing order.
func countEach[T comparable](values []T) map[T]int {
	out := map[T]int{}
	for _, v := range values {
		out[v]++
	}
	return out
}

// Invariant: the defender acts at most once per challenged digest per pass,
// however many challenge logs name that digest, and a repeated log never hides
// the challenges logged after it.
// Kills KC-0558fee6b1 (defender.go:65 continue->break) and KC-87a31fb030
// (defender.go:67 seen=false).
func TestDefenderDeduplicatesChallengeLogs(t *testing.T) {
	ctx := context.Background()
	t.Run("defend", func(t *testing.T) {
		f := &fakeChain{head: 100}
		r := &memRecords{m: map[[32]byte][]byte{}}
		first, second := heartbeatDefense(t, r, 40), heartbeatDefense(t, r, 50)
		challenge(f, first, 50, 1000)
		challenge(f, first, 51, 1000) // the same digest logged again
		challenge(f, second, 52, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(actions) != 2 || kindOf(actions, first) != ActionDefended || kindOf(actions, second) != ActionDefended {
			t.Fatalf("one action per distinct digest: %+v", actions)
		}
		if got := countEach(f.heartbeats); len(f.heartbeats) != 2 || got[40] != 1 || got[50] != 1 {
			t.Fatalf("each digest defended exactly once: %v", f.heartbeats)
		}
	})
	t.Run("notify timeouts", func(t *testing.T) {
		f := &fakeChain{head: 100, now: 2000, timeout: 600}
		r := &memRecords{m: map[[32]byte][]byte{}}
		challenge(f, [32]byte{0xe1}, 50, 1000)
		challenge(f, [32]byte{0xe1}, 51, 1000) // the same digest logged again
		challenge(f, [32]byte{0xe2}, 52, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got := countEach(f.timeouts); len(actions) != 2 || len(f.timeouts) != 2 || got[[32]byte{0xe1}] != 1 || got[[32]byte{0xe2}] != 1 {
			t.Fatalf("each expired digest notified exactly once: %+v %x", actions, f.timeouts)
		}
	})
}

// Invariant: a challenge view that differs from its log in wallet or in
// digest (either alone) is refused before anything is submitted.
// Kills KC-1ce662049e (defender.go:72 ||->&&) and KC-4e0bd6aa5a
// (defender.go:73 error->nil).
func TestDefenderRefusesMismatchedChallengeView(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		view func([32]byte, Challenge) Challenge
	}{
		{"wallet differs", func(_ [32]byte, c Challenge) Challenge { c.WalletID = [32]byte{0x78}; return c }},
		{"digest differs", func(_ [32]byte, c Challenge) Challenge { c.Digest = [32]byte{0xdd}; return c }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeChain{head: 100, now: 2000, timeout: 600}
			f := &faultChain{fakeChain: base, view: tc.view}
			r := &memRecords{m: map[[32]byte][]byte{}}
			ours := heartbeatDefense(t, r, 40)
			challenge(base, ours, 50, 1000)
			challenge(base, [32]byte{0xe1}, 51, 1000)
			d := &Defender{Chain: f, Wallet: testWallet, Records: r}
			if actions, err := d.Defend(ctx); err == nil || len(actions) != 0 || len(base.heartbeats) != 0 {
				t.Fatalf("defend accepted a mismatched challenge view: %+v %v", actions, err)
			}
			if actions, err := d.NotifyTimeouts(ctx); err == nil || len(actions) != 0 || len(base.timeouts) != 0 {
				t.Fatalf("timeout notifier accepted a mismatched challenge view: %+v %v", actions, err)
			}
		})
	}
}

// Invariant: a failure while reading the open challenges (or a stop at one
// of its boundaries) is returned by both Defend and NotifyTimeouts, and
// nothing is submitted.
// Kills KC-9de3901ba0 (:51), KC-fb98ab26f6 (:55), KC-6fdb770144 (:59),
// KC-d3e1da8619 (:70), KC-e601f22a7f (:78), KC-f6aa502f6e (:91) and
// KC-6a21edf78e (:171), all err->nil.
func TestDefenderReturnsChallengeReadFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		fault    func(*faultChain)
		boundary string
	}{
		{"boundary before the challenge read", nil, "before-challenge-read"},
		{"head", func(f *faultChain) { f.headErr = errInjected }, ""},
		{"challenge logs", func(f *faultChain) { f.logsErr = errInjected }, ""},
		{"challenge view", func(f *faultChain) { f.challengeErr = errInjected }, ""},
		{"boundary after the challenges were seen", nil, "challenge-seen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeChain{head: 100, now: 2000, timeout: 600}
			f := &faultChain{fakeChain: base}
			if tc.fault != nil {
				tc.fault(f)
			}
			r := &memRecords{m: map[[32]byte][]byte{}}
			challenge(base, heartbeatDefense(t, r, 40), 50, 1000)
			challenge(base, [32]byte{0xe1}, 51, 1000)
			d := &Defender{Chain: f, Wallet: testWallet, Records: r}
			if tc.boundary != "" {
				d.boundary = failAt(tc.boundary)
			}
			if _, err := d.Defend(ctx); !errors.Is(err, errInjected) || len(base.heartbeats) != 0 {
				t.Fatalf("Defend hid a challenge read failure: %v, %d defenses", err, len(base.heartbeats))
			}
			if _, err := d.NotifyTimeouts(ctx); !errors.Is(err, errInjected) || len(base.timeouts) != 0 {
				t.Fatalf("NotifyTimeouts hid a challenge read failure: %v, %d notices", err, len(base.timeouts))
			}
		})
	}
}

// Invariant: a challenge the defender skips (already resolved, or with an
// unreadable defense record) never stops the challenges after it; a stop at
// any defense boundary is returned.
// Kills KC-c8adbd3c8d (:101 continue->break), KC-c5611b3fa6 (:111
// continue->break), KC-8e4dfe2c3e (:114), KC-a6dc29c074 (:117) and
// KC-cceacfd7b4 (:128) err->nil.
func TestDefenderSkipsDoNotStopThePass(t *testing.T) {
	ctx := context.Background()
	t.Run("a resolved challenge first", func(t *testing.T) {
		f := &fakeChain{head: 100}
		r := &memRecords{m: map[[32]byte][]byte{}}
		resolved := heartbeatDefense(t, r, 30)
		challenge(f, resolved, 50, 1000)
		c := f.challenges[resolved]
		c.Resolved = true
		f.challenges[resolved] = c
		ours := heartbeatDefense(t, r, 40)
		challenge(f, ours, 51, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx)
		if err != nil || kindOf(actions, resolved) != ActionResolved || kindOf(actions, ours) != ActionDefended || len(f.heartbeats) != 1 {
			t.Fatalf("a resolved challenge stopped the pass: %+v %v", actions, err)
		}
	})
	t.Run("an unreadable defense record first", func(t *testing.T) {
		f := &fakeChain{head: 100}
		r := &memRecords{m: map[[32]byte][]byte{}}
		broken := [32]byte{0xd}
		r.m[signing.DefenseID(testWallet, broken)] = []byte("not a defense record") // fixture: corrupt record
		challenge(f, broken, 50, 1000)
		ours := heartbeatDefense(t, r, 40)
		challenge(f, ours, 51, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx)
		if err == nil || kindOf(actions, broken) != ActionFailed {
			t.Fatalf("an unreadable record must be reported: %+v %v", actions, err)
		}
		if kindOf(actions, ours) != ActionDefended || len(f.heartbeats) != 1 {
			t.Fatalf("an unreadable record stopped the pass: %+v", actions)
		}
	})
	for _, step := range []string{"defense-record-read", "before-defense", "defense-submitted"} {
		t.Run("boundary "+step, func(t *testing.T) {
			f := &fakeChain{head: 100}
			r := &memRecords{m: map[[32]byte][]byte{}}
			challenge(f, heartbeatDefense(t, r, 40), 50, 1000)
			d := &Defender{Chain: f, Wallet: testWallet, Records: r, boundary: failAt(step)}
			actions, err := d.Defend(ctx)
			if !errors.Is(err, errInjected) || len(actions) != 0 {
				t.Fatalf("a stop at %s was not returned: %+v %v", step, actions, err)
			}
			if submitted := len(f.heartbeats) == 1; submitted != (step == "defense-submitted") {
				t.Fatalf("a stop at %s submitted=%v", step, submitted)
			}
		})
	}
}

// Invariant: a transaction defense is submitted only from a complete record
// (a transaction, and an input index inside its prevouts) of a known kind,
// and a sweep whose proof cannot be read is not submitted; each refusal is
// reported as a failure, never as a defense.
// Kills KC-fdf451f3c3 (:139 ||->&&), KC-d38567b621 (:139 >=->>),
// KC-378f7482fe (:140), KC-2ee39059b5 (:153) and KC-bcd7d05a47 (:160)
// err->nil.
func TestDefenderRefusesIncompleteDefenseRecords(t *testing.T) {
	ctx := context.Background()
	tx, prevouts := sweepTx()
	for _, tc := range []struct {
		name      string
		kind      string
		tx        bool
		input     uint32
		provenErr error
		defended  bool
	}{
		{"complete intent record", signing.DefenseIntent, true, 0, nil, true},
		{"no transaction", signing.DefenseIntent, false, 0, nil, false},
		{"input equal to the prevout count", signing.DefenseIntent, true, 1, nil, false},
		{"input past the prevout count", signing.DefenseIntent, true, 2, nil, false},
		{"unknown kind", "bogus", true, 0, nil, false},
		{"sweep proof unreadable", signing.DefenseSweep, true, 0, errInjected, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeChain{head: 100, proven: map[[32]byte][32]byte{}}
			f := &faultChain{fakeChain: base, provenErr: tc.provenErr}
			r := &memRecords{m: map[[32]byte][]byte{}}
			rec := signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: [32]byte{0xd}, Kind: tc.kind, Prevouts: prevouts, Input: tc.input}
			if tc.tx {
				rec.Transaction = tx
			}
			retainDefense(t, r, rec)
			challenge(base, rec.Digest, 50, 1000)
			actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx)
			if tc.defended {
				if err != nil || kindOf(actions, rec.Digest) != ActionDefended || len(base.defenses) != 1 {
					t.Fatalf("a complete record was not defended: %+v %v", actions, err)
				}
				return
			}
			if err == nil || kindOf(actions, rec.Digest) != ActionFailed {
				t.Fatalf("a refused defense was reported as %+v (err %v)", actions, err)
			}
			if len(base.defenses) != 0 {
				t.Fatalf("a refused defense was submitted: %+v", base.defenses)
			}
		})
	}
}

// Invariant: the timeout notifier returns its own chain read failures and
// boundary stops, notifies each expired unauthorized challenge whatever was
// skipped or refused before it, and counts a refused notice as done only
// when the chain shows the challenge resolved.
// Kills KC-7f011f9760 (:175), KC-2e499447e3 (:179), KC-7d5b9184ac (:203),
// KC-5443ec2dd3 (:214) err->nil; KC-8ca7004b55 (:189), KC-f71e6dbd31
// (:200), KC-4feecf4587 (:210) continue->break; KC-2ad8324232 (:207 !=->==)
// and KC-07a4649653 (:207 ||->&&).
func TestTimeoutNotifierFaultsAndSkips(t *testing.T) {
	ctx := context.Background()
	expired, other, pending := [32]byte{0xe1}, [32]byte{0xe3}, [32]byte{0xe2}
	newWorld := func() (*fakeChain, *faultChain, *memRecords) {
		base := &fakeChain{head: 100, now: 1600, timeout: 600}
		return base, &faultChain{fakeChain: base, notifyTimeoutErr: map[[32]byte]error{}}, &memRecords{m: map[[32]byte][]byte{}}
	}
	for _, tc := range []struct {
		name     string
		fault    func(*faultChain)
		boundary string
	}{
		{"fraud defeat timeout", func(f *faultChain) { f.timeoutErr = errInjected }, ""},
		{"head timestamp", func(f *faultChain) { f.timestampErr = errInjected }, ""},
		{"boundary before the notice", nil, "before-timeout-notify"},
		{"boundary after the notice", nil, "timeout-notified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, f, r := newWorld()
			if tc.fault != nil {
				tc.fault(f)
			}
			challenge(base, expired, 50, 1000)
			d := &Defender{Chain: f, Wallet: testWallet, Records: r}
			if tc.boundary != "" {
				d.boundary = failAt(tc.boundary)
			}
			actions, err := d.NotifyTimeouts(ctx)
			if !errors.Is(err, errInjected) || len(actions) != 0 {
				t.Fatalf("the notifier hid a failure: %+v %v", actions, err)
			}
			if notified := len(base.timeouts) == 1; notified != (tc.boundary == "timeout-notified") {
				t.Fatalf("notified=%v after a failure at %s", notified, tc.name)
			}
		})
	}
	t.Run("deadline boundary", func(t *testing.T) {
		base, f, r := newWorld()
		challenge(base, expired, 50, 1000) // deadline 1600 = now
		challenge(base, pending, 51, 1001) // deadline 1601 > now
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
		if err != nil || kindOf(actions, expired) != ActionTimedOut || kindOf(actions, pending) != ActionPending {
			t.Fatalf("deadline boundary: %+v %v", actions, err)
		}
	})
	t.Run("a resolved challenge first", func(t *testing.T) {
		base, f, r := newWorld()
		challenge(base, other, 50, 1000)
		c := base.challenges[other]
		c.Resolved = true
		base.challenges[other] = c
		challenge(base, expired, 51, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
		if err != nil || kindOf(actions, other) != ActionResolved || kindOf(actions, expired) != ActionTimedOut || len(base.timeouts) != 1 {
			t.Fatalf("a resolved challenge stopped the pass: %+v %v", actions, err)
		}
	})
	t.Run("a pending challenge first", func(t *testing.T) {
		base, f, r := newWorld()
		challenge(base, pending, 50, 1001)
		challenge(base, expired, 51, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
		if err != nil || kindOf(actions, pending) != ActionPending || kindOf(actions, expired) != ActionTimedOut || len(base.timeouts) != 1 {
			t.Fatalf("a pending challenge stopped the pass: %+v %v", actions, err)
		}
	})
	t.Run("a lost notice reply is settled by rereading", func(t *testing.T) {
		base, f, r := newWorld()
		base.lostReply = true
		challenge(base, expired, 50, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
		if err != nil || kindOf(actions, expired) != ActionTimedOut {
			t.Fatalf("a notice resolved on chain was reported as %+v (err %v)", actions, err)
		}
	})
	t.Run("a refused notice is reported and does not stop the pass", func(t *testing.T) {
		base, f, r := newWorld()
		f.notifyTimeoutErr[expired] = errInjected
		challenge(base, expired, 50, 1000)
		challenge(base, other, 51, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
		if !errors.Is(err, errInjected) || kindOf(actions, expired) != ActionFailed {
			t.Fatalf("a refused notice was reported as %+v (err %v)", actions, err)
		}
		if kindOf(actions, other) != ActionTimedOut || !base.challenges[other].Resolved {
			t.Fatalf("a refused notice stopped the pass: %+v", actions)
		}
	})
}
