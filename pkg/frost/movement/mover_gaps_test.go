package movement

// Mutation batch M6-movement: the mover (mover.go). Invariants (KC-SF-V3
// K-07; V-08; the Mover doc comments; base keep-core signer selection,
// tuxxy/keep-core#66):
//   - the current round is the one whose final anchor is the latest one at
//     the head; its anchor is Number*RoundBlocks with that block's hash, and
//     it starts FinalityBlocks later; an incomplete schedule or a short chain
//     has none;
//   - signer seats per round follow base keep-core's qualified-operator step
//     (retry.EvaluateRetryParticipantsForSigning) at the first round of each
//     rotation cycle, listed in ascending order; exactly the selected
//     operators are eligible (the differences from base are listed at
//     TestSelectedFollowsBaseQualifiedOperatorStep);
//   - Sign takes the main lock (by purpose) before the durable attempt
//     record, and the attempt record before the executor runs; any refusal
//     or stop before a step leaves that step undone; a failed run records no
//     signature and no possible release it did not make;
//   - Broadcast fails, and broadcasts nothing, on any failed read before the
//     broadcast;
//   - Resolve settles only on a final proof of this plan, broadcasts a signed
//     plan and never signs it again, abandons only on a terminal refusal,
//     signs at most once per round and only when selected, reconciles every
//     earlier attempt first, and stops on any failed read.
// Unit level on the real Mover. FIXTURE chain, journals and signing host:
// the host journal holds no wallet, so the executor never reaches a grant or
// the worker (gaps_fixture_test.go).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tecdsa/retry"
)

// TestMoverCurrentRound: the round of the latest final anchor, for heads
// across round boundaries; none for a short chain, an incomplete schedule or
// a failed read.
func TestMoverCurrentRound(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	h := newMoverHost(t, g, g.targets[0].Descriptor)
	for _, head := range []uint64{2, 3, 5, 6, 9, 100, 101, 102} {
		g.f.head = head
		var r Round
		err := recovered(func() error { var err error; r, err = h.mover.Current(ctx); return err })
		if err != nil {
			t.Fatalf("head %d: %v", head, err)
		}
		if r.Anchor != r.Number*4 || r.AnchorHash != g.f.hash(r.Anchor) || r.StartBlock != r.Anchor+2 {
			t.Fatalf("head %d: round %+v is not anchored at Number*RoundBlocks with its hash, starting FinalityBlocks later", head, r)
		}
		if r.StartBlock > head || r.StartBlock+4 <= head {
			t.Fatalf("head %d: round %d starts at %d, not the latest final anchor's round", head, r.Number, r.StartBlock)
		}
		if r.Session != ([32]byte{}) {
			t.Fatalf("head %d: a round without a plan has a session", head)
		}
	}
	for name, mutate := range map[string]func(){
		"short chain":       func() { g.f.head = 1 },
		"no finality":       func() { h.mover.Schedule.FinalityBlocks = 0 },
		"no round length":   func() { h.mover.Schedule.RoundBlocks = 0 },
		"empty schedule":    func() { h.mover.Schedule = redemption.Schedule{} },
		"head read fails":   func() { g.fc.failOn("Head") },
		"anchor hash fails": func() { g.fc.failOn("HeaderHash") },
		"anchor hash fails 2": func() {
			g.f.head = 6
			g.fc.failWhen = func(m string, at uint64) bool { return m == "HeaderHash" && at == 4 }
		},
	} {
		g.f.head, g.fc.failWhen = 100, nil
		h.mover.Schedule = redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 4}
		mutate()
		var r Round
		err := recovered(func() error { var err error; r, err = h.mover.Current(ctx); return err })
		if err == nil || errors.Is(err, errPanic) {
			t.Errorf("%s: round %+v: %v", name, r, err)
		}
	}
}

// TestSelectedFollowsBaseQualifiedOperatorStep: at the first round of every
// rotation cycle the seats are those of the operators that base keep-core's
// qualified-operator step, retry.EvaluateRetryParticipantsForSigning,
// accepts when given every member, the wallet ID seed and the cycle as the
// retry count. Every round lists its seats strictly ascending, as the
// signing engine requires (snowfallengine validSubset).
//
// This is not base's whole signer selection. Base differs in four ways:
//   - it feeds only the ready operators (pkg/tbtc signing_loop.go); here
//     every member counts;
//   - it trims the included members to exactly HonestThreshold with a second
//     shuffle; selected keeps whole operators and can exceed the threshold;
//   - it reshuffles on every attempt; here the shuffle is drawn once per
//     rotation cycle and the start operator rotates within it (mover.go
//     selected doc);
//   - it seeds with the attempt seed; here the seed is the wallet ID.
func TestSelectedFollowsBaseQualifiedOperatorStep(t *testing.T) {
	for _, shape := range []struct {
		seatsPer  []int
		threshold uint16
	}{{[]int{1, 1, 1, 1, 1}, 3}, {[]int{2, 1, 3, 1}, 4}, {[]int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5}, 26}} {
		for wallet := byte(0); wallet < 6; wallet++ {
			d := selectionDescriptor(shape.seatsPer, shape.threshold, wallet)
			// Interleave operators across seats, so seat order and operator
			// order differ.
			for i := range d.Members {
				j := (i * 7) % len(d.Members)
				d.Members[i], d.Members[j] = d.Members[j], d.Members[i]
				d.Operators[i], d.Operators[j] = d.Operators[j], d.Operators[i]
			}
			n := uint64(len(shape.seatsPer))
			id := dkg.WalletID(d.OutputKey)
			seed := int64(uint64(id[0])<<56 | uint64(id[1])<<48 | uint64(id[2])<<40 | uint64(id[3])<<32 | uint64(id[4])<<24 | uint64(id[5])<<16 | uint64(id[6])<<8 | uint64(id[7]))
			// Base sorts chain.Address strings; lower-case hex orders as
			// the address bytes do.
			var members []chain.Address
			for _, op := range d.Operators {
				members = append(members, chain.Address(strings.ToLower(op.Hex())))
			}
			for cycle := uint64(0); cycle < 6; cycle++ {
				accepted, err := retry.EvaluateRetryParticipantsForSigning(members, seed, uint(cycle), uint(shape.threshold))
				if err != nil {
					t.Fatal(err)
				}
				in := map[chain.Address]bool{}
				for _, a := range accepted {
					in[a] = true
				}
				var want []uint16
				for i, a := range members {
					if in[a] {
						want = append(want, uint16(i+1))
					}
				}
				got := selected(d, cycle*n)
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Fatalf("shape %v wallet %d cycle %d: seats %v, base qualified-operator step %v", shape.seatsPer, wallet, cycle, got, want)
				}
			}
			for round := uint64(0); round < 3*n; round++ {
				seats := selected(d, round)
				if !sort.SliceIsSorted(seats, func(i, j int) bool { return seats[i] < seats[j] }) {
					t.Fatalf("shape %v wallet %d round %d: seats %v not ascending", shape.seatsPer, wallet, round, seats)
				}
			}
		}
	}
}

// TestEligibleExactlyTheSelectedOperators: with one seat per operator and
// threshold 2 of 3, exactly two operators are eligible in every round, and
// every operator in some round; a failed status read is an error.
func TestEligibleExactlyTheSelectedOperators(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	d := g.targets[0].Descriptor
	var hosts []*moverHost
	for seat := uint16(1); seat <= 3; seat++ {
		h := newMoverHost(t, g, d)
		h.host.seats = []uint16{seat}
		hosts = append(hosts, h)
	}
	ever := map[int]bool{}
	for round := uint64(0); round < 9; round++ {
		count := 0
		for i, h := range hosts {
			ok, err := h.mover.Eligible(ctx, round)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				count++
				ever[i] = true
			}
		}
		if count != int(d.Threshold) {
			t.Fatalf("round %d: %d operators eligible, want exactly the threshold %d", round, count, d.Threshold)
		}
	}
	if len(ever) != 3 {
		t.Fatalf("only operators %v are ever eligible", ever)
	}
	hosts[0].host.failStatus = true
	if _, err := hosts[0].mover.Eligible(ctx, 0); !errors.Is(err, errInjected) {
		t.Fatalf("a failed status read was not reported: %v", err)
	}
}

// signCase is one plan shape for the Sign tests.
type signCase struct {
	name    string
	build   func(t *testing.T) (*moverHost, *signing.Plan, [32]byte, [32]byte)
	purpose deposit.Purpose
}

func signCases() []signCase {
	return []signCase{
		{"incoming without main", func(t *testing.T) (*moverHost, *signing.Plan, [32]byte, [32]byte) {
			g := newGapWorld(t)
			plan, target, _ := g.incomingPlan(t, false)
			return newMoverHost(t, g, target.Descriptor), plan, target.ID, [32]byte{}
		}, deposit.Free},
		{"incoming with main", func(t *testing.T) (*moverHost, *signing.Plan, [32]byte, [32]byte) {
			g := newGapWorld(t)
			plan, target, _ := g.incomingPlan(t, true)
			return newMoverHost(t, g, target.Descriptor), plan, target.ID, OutpointKey(plan.MovementMain().Outpoint)
		}, deposit.SweepMain},
		{"movement", func(t *testing.T) (*moverHost, *signing.Plan, [32]byte, [32]byte) {
			g := newGapWorld(t)
			plan := g.committedPlan(t)
			return newMoverHost(t, g, g.source.Descriptor), plan, g.source.ID, OutpointKey(g.main.Outpoint)
		}, deposit.MovementMain},
	}
}

// currentRound is the round of the fixture schedule (finality 2, rounds of
// 4) at the head, as TestMoverCurrentRound pins it.
func (h *moverHost) currentRound() uint64 { return (h.f.head - 2) / 4 }

func (h *moverHost) round(t *testing.T) uint64 {
	t.Helper()
	var r Round
	err := recovered(func() error { var err error; r, err = h.mover.Current(context.Background()); return err })
	if err != nil {
		t.Fatal("current round:", err)
	}
	return r.Number
}

// TestSignOrder: a Sign that reaches the executor takes the lock by the
// plan's purpose, records its attempt, runs once, and records neither a
// signature nor a possible release the failed run did not make.
func TestSignOrder(t *testing.T) {
	ctx := context.Background()
	for _, c := range signCases() {
		t.Run(c.name, func(t *testing.T) {
			h, plan, wallet, main := c.build(t)
			n := h.round(t)
			var result *signing.Result
			err := recovered(func() error { var err error; result, err = h.sign(ctx, plan, n); return err })
			if !errors.Is(err, errNoWallet) || result != nil {
				t.Fatalf("the fixture run must fail at the wallet read: %v", err)
			}
			if got := h.attempts(t, plan); fmt.Sprint(got) != fmt.Sprint([]string{strconv.FormatUint(n, 10)}) || h.host.runs() != 1 {
				t.Fatalf("attempts %v and %d runs, want one attempt of round %d and one run", got, h.host.runs(), n)
			}
			if h.has(t, plan, redemption.KindMovementSigned) || h.has(t, plan, redemption.KindPossibleRelease) {
				t.Fatal("a failed run before any release recorded a signature or a possible release")
			}
			if main != ([32]byte{}) {
				if got := h.locked(t, wallet, main); got != c.purpose {
					t.Fatalf("main lock %q, want %q", got, c.purpose)
				}
			}
		})
	}
}

// TestSignStopsBeforeEachStep: each refusal or stop leaves every later step
// undone: no lock before the agreement, no attempt record before the lock,
// no run before the attempt record.
func TestSignStopsBeforeEachStep(t *testing.T) {
	ctx := context.Background()
	type stop struct {
		name     string
		setup    func(h *moverHost, plan *signing.Plan, wallet, main [32]byte) uint64
		lockFree bool
	}
	stops := []stop{
		{"incomplete schedule", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			n := h.currentRound()
			h.mover.Schedule.RoundBlocks = 0
			return n
		}, true},
		{"anchor hash read fails", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			n := h.currentRound()
			h.fc.failWhen = func(m string, at uint64) bool { return m == "HeaderHash" && at == n*4 }
			return n
		}, true},
		{"head read fails", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			n := h.currentRound()
			h.fc.failOn("Head")
			return n
		}, true},
		{"round not yet final", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			return h.currentRound() + 1
		}, true},
		{"stop at agreement", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			h.mover.boundary = stopAt("agreement-derived")
			return h.currentRound()
		}, true},
		{"main held by another plan", func(h *moverHost, plan *signing.Plan, wallet, _ [32]byte) uint64 {
			if main := plan.MovementMain(); main != nil {
				if err := h.lock.AcquireRedemption(context.Background(), wallet, main.Outpoint, [32]byte{0xee}, 1_000); err != nil {
					panic(err)
				}
			}
			return h.currentRound()
		}, false},
		{"executor configuration incomplete", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			h.mover.Base.Bitcoin = nil
			return h.currentRound()
		}, false},
		{"stop before the attempt", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			h.mover.boundary = stopAt("before-sign")
			return h.currentRound()
		}, false},
		{"attempt record fails", func(h *moverHost, _ *signing.Plan, _, _ [32]byte) uint64 {
			h.ledgerJ.failWrites = true
			return h.currentRound()
		}, false},
	}
	for _, c := range signCases() {
		for _, s := range stops {
			t.Run(c.name+"/"+s.name, func(t *testing.T) {
				h, plan, wallet, main := c.build(t)
				if s.name == "main held by another plan" && main == ([32]byte{}) {
					return // a plan without a main takes no lock
				}
				n := s.setup(h, plan, wallet, main)
				err := recovered(func() error { _, err := h.sign(ctx, plan, n); return err })
				h.fc.failWhen = nil
				if err == nil || errors.Is(err, errPanic) || errors.Is(err, errNoWallet) {
					t.Fatalf("Sign did not stop before the run: %v", err)
				}
				if got := h.attempts(t, plan); len(got) != 0 || h.host.runs() != 0 {
					t.Fatalf("attempts %v and %d runs after a stop before the attempt", got, h.host.runs())
				}
				if s.lockFree && main != ([32]byte{}) {
					if got := h.locked(t, wallet, main); got != deposit.Free {
						t.Fatalf("main lock %q after a stop before the lock", got)
					}
				}
			})
		}
	}
	// The round starting exactly at the head is final: Sign proceeds.
	h, plan, _, _ := signCases()[0].build(t)
	r := h.round(t)
	h.f.head = r*4 + 2
	if _, err := h.sign(ctx, plan, r); !errors.Is(err, errNoWallet) || h.host.runs() != 1 {
		t.Fatalf("a round starting at the head did not sign: %v", err)
	}
	// A movement whose commitment the authority refuses takes no lock.
	g := newGapWorld(t)
	mplan := g.committedPlan(t)
	mh := newMoverHost(t, g, g.source.Descriptor)
	cm := g.f.commitments[g.source.ID]
	cm.ProvenAt = 1
	g.f.commitments[g.source.ID] = cm
	if _, err := mh.sign(ctx, mplan, mh.round(t)); !errors.Is(err, ErrSettled) || mh.locked(t, g.source.ID, OutpointKey(g.main.Outpoint)) != deposit.Free || len(mh.attempts(t, mplan)) != 0 {
		t.Fatalf("a refused movement took the lock or recorded an attempt: %v", err)
	}
}

// resolveHost is an incoming sweep (without a main unless withMain) and its
// target's mover at head 100.
func resolveHost(t *testing.T, withMain bool) (*moverHost, *signing.Plan, [32]byte) {
	t.Helper()
	g := newGapWorld(t)
	plan, target, key := g.incomingPlan(t, withMain)
	return newMoverHost(t, g, target.Descriptor), plan, key
}

// TestResolveSignsOncePerRound: a fresh plan signs in the current round; a
// second Resolve in that round waits; the next round signs again.
func TestResolveSignsOncePerRound(t *testing.T) {
	ctx := context.Background()
	h, plan, _ := resolveHost(t, false)
	n := h.round(t)
	if _, err := h.resolve(ctx, plan); !errors.Is(err, errNoWallet) {
		t.Fatalf("a fresh plan did not sign: %v", err)
	}
	status, err := h.resolve(ctx, plan)
	if status != StatusWaiting || err != nil || h.host.runs() != 1 {
		t.Fatalf("second Resolve in round %d: %q, %v, %d runs; want waiting without a run", n, status, err, h.host.runs())
	}
	h.f.head += 4
	if _, err = h.resolve(ctx, plan); !errors.Is(err, errNoWallet) || h.host.runs() != 2 {
		t.Fatalf("the next round did not sign: %v, %d runs", err, h.host.runs())
	}
	want := []string{strconv.FormatUint(n, 10), strconv.FormatUint(n+1, 10)}
	if got := h.attempts(t, plan); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("attempts %v, want %v", got, want)
	}
	// An operator outside the selection waits; a failed status read fails.
	h.f.head += 4
	h.host.seats = nil
	if status, err = h.resolve(ctx, plan); status != StatusWaiting || err != nil || h.host.runs() != 2 {
		t.Fatalf("an operator outside the selection: %q, %v, %d runs", status, err, h.host.runs())
	}
	h.host.failStatus = true
	if _, err = h.resolve(ctx, plan); !errors.Is(err, errInjected) || h.host.runs() != 2 {
		t.Fatalf("a failed status read: %v, %d runs", err, h.host.runs())
	}
}

// TestResolveSignedPlanIsNeverSignedAgain: with a signed marker the plan
// goes to broadcast; a failed broadcast is reported, never hidden, and no
// new attempt starts.
func TestResolveSignedPlanIsNeverSignedAgain(t *testing.T) {
	ctx := context.Background()
	h, plan, _ := resolveHost(t, false)
	// FIXTURE: a signed marker without a durable result stands in for any
	// broadcast failure (the result fails validation).
	h.plant(t, plan, redemption.KindMovementAttempt, "20")
	h.plant(t, plan, redemption.KindMovementSigned, "20")
	status, err := h.resolve(ctx, plan)
	if err == nil || status == StatusBroadcast || h.host.runs() != 0 || len(h.attempts(t, plan)) != 1 || h.has(t, plan, redemption.KindMovementBroadcast) {
		t.Fatalf("a signed plan whose broadcast fails: %q, %v, %d runs, attempts %v", status, err, h.host.runs(), h.attempts(t, plan))
	}
}

// TestBroadcastNeedsADurableSignature: Broadcast refuses without a signed
// marker, with an incomplete executor, after any single failed ledger read
// (with or without a marker) and with an unreadable round anchor; it
// broadcasts and records nothing. The planted signed marker without a
// durable result is a FIXTURE state (the marker is a cache of the executor's
// result); every failure here comes before any result read.
func TestBroadcastNeedsADurableSignature(t *testing.T) {
	ctx := context.Background()
	h, plan, _ := resolveHost(t, false)
	if err := h.broadcast(ctx, plan); err == nil || errors.Is(err, errPanic) || h.has(t, plan, redemption.KindMovementBroadcast) {
		t.Fatalf("broadcast without a signature: %v", err)
	}
	h.plant(t, plan, redemption.KindMovementSigned, "20")
	h.mover.Base.Bitcoin = nil
	if err := h.broadcast(ctx, plan); err == nil || errors.Is(err, errPanic) || h.has(t, plan, redemption.KindMovementBroadcast) {
		t.Fatalf("broadcast with an incomplete executor: %v", err)
	}
	for _, marked := range []bool{false, true} {
		for n := 1; ; n++ {
			if n > 20 {
				t.Fatal("fixture: the read fault never left Broadcast's ledger reads")
			}
			h, plan, _ := resolveHost(t, false)
			if marked {
				h.plant(t, plan, redemption.KindMovementSigned, "20")
			}
			h.ledgerJ.armRead(n)
			err := h.broadcast(ctx, plan)
			if !h.ledgerJ.wasFired() {
				break
			}
			if err == nil || errors.Is(err, errPanic) || h.has(t, plan, redemption.KindMovementBroadcast) {
				t.Fatalf("marked %v: Broadcast after failed ledger read %d: %v", marked, n, err)
			}
		}
	}
	h, plan, _ = resolveHost(t, false)
	h.plant(t, plan, redemption.KindMovementSigned, "20")
	h.fc.failWhen = func(m string, at uint64) bool { return m == "HeaderHash" && at == 20*4 }
	if err := h.broadcast(ctx, plan); err == nil || errors.Is(err, errPanic) || h.has(t, plan, redemption.KindMovementBroadcast) {
		t.Fatalf("Broadcast with an unreadable round anchor: %v", err)
	}
}

// TestResolveReconcilesEveryEarlierAttempt: before a new round, Resolve
// checks each earlier attempt's round for a durable result; any attempt it
// cannot check stops it before a lock or a new attempt.
func TestResolveReconcilesEveryEarlierAttempt(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		fail func(h *moverHost)
	}{
		"first attempt's anchor unreadable": {func(h *moverHost) {
			h.fc.failWhen = func(m string, at uint64) bool { return m == "HeaderHash" && at == 7*4 }
		}},
		"second attempt's anchor unreadable": {func(h *moverHost) {
			h.fc.failWhen = func(m string, at uint64) bool { return m == "HeaderHash" && at == 8*4 }
		}},
		"executor configuration incomplete": {func(h *moverHost) { h.mover.Base.Bitcoin = nil }},
	} {
		t.Run(name, func(t *testing.T) {
			h, plan, _ := resolveHost(t, true)
			for _, r := range []string{"7", "8"} {
				h.plant(t, plan, redemption.KindMovementAttempt, r)
			}
			c.fail(h)
			status, err := h.resolve(ctx, plan)
			main := plan.MovementMain()
			if err == nil || h.host.runs() != 0 || len(h.attempts(t, plan)) != 2 || h.locked(t, plan.WalletID(), OutpointKey(main.Outpoint)) != deposit.Free {
				t.Fatalf("Resolve went on past an attempt it could not check: %q, %v, %d runs, attempts %v", status, err, h.host.runs(), h.attempts(t, plan))
			}
		})
	}
	// Control: with every earlier attempt checked, the current round signs.
	h, plan, _ := resolveHost(t, true)
	h.plant(t, plan, redemption.KindMovementAttempt, "7")
	if _, err := h.resolve(ctx, plan); !errors.Is(err, errNoWallet) || h.host.runs() != 1 {
		t.Fatalf("control: the current round did not sign after a checked attempt: %v", err)
	}
}

// TestResolveStopsOnAnyLedgerReadFailure: whichever single ledger read
// fails, Resolve fails and records no attempt.
func TestResolveStopsOnAnyLedgerReadFailure(t *testing.T) {
	ctx := context.Background()
	for n := 1; ; n++ {
		if n > 50 {
			t.Fatal("fixture: the read fault never left Resolve's reads")
		}
		h, plan, _ := resolveHost(t, false)
		h.plant(t, plan, redemption.KindMovementAttempt, "7")
		h.ledgerJ.armRead(n)
		status, err := h.resolve(ctx, plan)
		if !h.ledgerJ.wasFired() {
			if !errors.Is(err, errNoWallet) {
				t.Fatalf("control: without a read failure Resolve must sign: %v", err)
			}
			break
		}
		if err == nil || h.host.runs() != 0 || len(h.attempts(t, plan)) != 1 {
			t.Fatalf("ledger read %d failed: %q, %v, %d runs, attempts %v", n, status, err, h.host.runs(), h.attempts(t, plan))
		}
	}
}

// TestResolveAbandonsOnlyOnTerminalRefusals: a refusal after which the plan
// can never be signed abandons it; any other failure is an error and the
// plan stays.
func TestResolveAbandonsOnlyOnTerminalRefusals(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		mutate   func(h *moverHost, key [32]byte)
		terminal bool
	}{
		"record gone": {func(h *moverHost, key [32]byte) { h.f.incoming[key] = Incoming{} }, true},
		"record swept": {func(h *moverHost, key [32]byte) {
			r := h.f.incoming[key]
			r.State = IncomingSwept
			h.f.incoming[key] = r
		}, true},
		"log gone": {func(h *moverHost, _ [32]byte) { h.f.incomingLog = nil }, true},
		"target state": {func(h *moverHost, _ [32]byte) {
			w := h.f.wallets[Label(h.targets[0].ID)]
			w.State = StateCandidate
			h.f.wallets[Label(h.targets[0].ID)] = w
		}, true},
		"record not final":  {func(h *moverHost, _ [32]byte) { h.view.Policy.FinalityBlocks = 20 }, false},
		"wallet read fails": {func(h *moverHost, _ [32]byte) { h.fc.failOn("Wallet") }, false},
		"contained, no directive": {func(h *moverHost, _ [32]byte) {
			w := h.f.wallets[Label(h.targets[0].ID)]
			w.Quarantined = true
			h.f.wallets[Label(h.targets[0].ID)] = w
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			h, plan, key := resolveHost(t, false)
			c.mutate(h, key)
			status, err := h.resolve(ctx, plan)
			if c.terminal && (status != StatusAbandoned || err != nil) {
				t.Fatalf("a terminal refusal did not abandon the plan: %q, %v", status, err)
			}
			if !c.terminal && (err == nil || status == StatusAbandoned) {
				t.Fatalf("a non-terminal failure abandoned the plan or passed: %q, %v", status, err)
			}
			if h.host.runs() != 0 || len(h.attempts(t, plan)) != 0 {
				t.Fatalf("a refused plan signed: %d runs", h.host.runs())
			}
		})
	}
}

// TestResolveSettlesOnlyOnAFinalProof: an incoming sweep settles on its own
// transaction proven by its wallet at the finalized head; a movement on its
// proven commitment. A proof not yet final, by another wallet, or a failed
// read does not settle; a stop before the outcome record records none.
func TestResolveSettlesOnlyOnAFinalProof(t *testing.T) {
	ctx := context.Background()
	t.Run("incoming", func(t *testing.T) {
		for name, c := range map[string]struct {
			block   uint64
			other   bool
			fail    bool
			settled bool
		}{
			"final proof":      {98, false, false, true},
			"proof not final":  {99, false, false, false},
			"other wallet":     {90, true, false, false},
			"proof read fails": {90, false, true, false},
		} {
			t.Run(name, func(t *testing.T) {
				h, plan, _ := resolveHost(t, false)
				wallet := plan.WalletID()
				if c.other {
					wallet = h.targets[1].ID
				}
				h.fc.proven[plan.BitcoinTransactionHash()] = provenAt{wallet, c.block}
				if c.fail {
					h.fc.failOn("ProvenTransaction")
				}
				status, err := h.resolve(ctx, plan)
				if c.settled != (status == StatusSettled) || c.settled != h.has(t, plan, redemption.KindMovementOutcome) {
					t.Fatalf("proof at %d (head 100): %q, %v; outcome recorded %v", c.block, status, err, h.has(t, plan, redemption.KindMovementOutcome))
				}
				if c.fail && (err == nil || h.host.runs() != 0) {
					t.Fatalf("a failed proof read went on: %q, %v, %d runs", status, err, h.host.runs())
				}
			})
		}
	})
	t.Run("movement", func(t *testing.T) {
		g := newGapWorld(t)
		plan := g.committedPlan(t)
		h := newMoverHost(t, g, g.source.Descriptor)
		if status, _ := h.resolve(ctx, plan); status == StatusSettled || h.has(t, plan, redemption.KindMovementOutcome) {
			t.Fatal("an unproven movement settled")
		}
		g.fc.failWhen = func(m string, at uint64) bool { return m == "Commitment" && at == g.f.head-2 }
		runs := h.host.runs()
		if status, err := h.resolve(ctx, plan); err == nil || status == StatusSettled || h.host.runs() != runs {
			t.Fatalf("a failed commitment read went on: %q, %v", status, err)
		}
		g.fc.failWhen = nil
		cm := g.f.commitments[g.source.ID]
		cm.ProvenAt = 99
		g.f.commitments[g.source.ID] = cm
		h.mover.boundary = stopAt("outcome-seen")
		if _, err := h.resolve(ctx, plan); !errors.Is(err, errBoundary) || h.has(t, plan, redemption.KindMovementOutcome) {
			t.Fatalf("a stop before the outcome record recorded it: %v", err)
		}
		h.mover.boundary = nil
		if status, err := h.resolve(ctx, plan); status != StatusSettled || err != nil || !h.has(t, plan, redemption.KindMovementOutcome) {
			t.Fatalf("a proven movement did not settle: %q, %v", status, err)
		}
	})
}

// TestResolveStopsWhenTheRoundIsUnreadable: a failed read of the head or of
// the current round stops Resolve before any attempt.
func TestResolveStopsWhenTheRoundIsUnreadable(t *testing.T) {
	ctx := context.Background()
	h, plan, _ := resolveHost(t, false)
	h.fc.failOn("Head")
	if status, err := h.resolve(ctx, plan); !errors.Is(err, errInjected) || len(h.attempts(t, plan)) != 0 {
		t.Fatalf("a failed head read: %q, %v", status, err)
	}
	h.fc.failOn()
	anchor := h.round(t) * 4
	h.fc.failWhen = func(m string, at uint64) bool { return m == "HeaderHash" && at == anchor }
	if status, err := h.resolve(ctx, plan); err == nil || h.host.runs() != 0 || len(h.attempts(t, plan)) != 0 {
		t.Fatalf("an unreadable round: %q, %v, attempts %v", status, err, h.attempts(t, plan))
	}
}
