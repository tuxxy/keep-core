package movement

// Mutation batch M6-movement: the C-06 view (view.go). Invariants (KC-SF-V3
// K-07; V-08 P-K07-COMMITTED-TARGET-ONLY, P-K07-INCOMING-ONCE):
//   - a view reads one consistent snapshot: any failed read, a log above the
//     head, a non-canonical log or a head that moved refuses the whole read;
//   - a record is final exactly when the head is at least FinalityBlocks
//     above its block;
//   - Target with keys returns a record for every key, or fails.
// Unit level on the real View over the fixture chain (gaps_fixture_test.go).

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

var errBoundary = errors.New("boundary stop (fixture)")

// TestViewRefusesIncompleteConfiguration: a view without a chain or without
// a finality depth refuses every read; it never reads with depth zero.
func TestViewRefusesIncompleteConfiguration(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	for name, v := range map[string]*View{
		"no chain":       {Policy: Policy{FinalityBlocks: 2}},
		"zero finality":  {Chain: g.fc},
		"neither is set": {},
	} {
		err := recovered(func() error { _, err := v.Source(ctx, g.source.ID); return err })
		if err == nil || errors.Is(err, errPanic) {
			t.Errorf("%s: Source read with an incomplete view: %v", name, err)
		}
		err = recovered(func() error { _, err := v.Target(ctx, g.targets[0].ID, nil); return err })
		if err == nil || errors.Is(err, errPanic) {
			t.Errorf("%s: Target read with an incomplete view: %v", name, err)
		}
	}
}

// TestViewFinalBoundary: final exactly when head - block >= FinalityBlocks;
// never for a block above the head.
func TestViewFinalBoundary(t *testing.T) {
	v := &View{Policy: Policy{FinalityBlocks: 3}}
	for _, c := range []struct {
		head, block uint64
		want        bool
	}{{100, 97, true}, {100, 98, false}, {100, 0, true}, {100, 100, false}, {100, 101, false}, {3, 0, true}, {2, 0, false}} {
		if got := v.Final(redemption.Head{Number: c.head}, Log{Block: c.block}); got != c.want {
			t.Errorf("Final(head %d, block %d) with depth 3 = %v, want %v", c.head, c.block, got, c.want)
		}
	}
}

// TestViewSourceStopsOnAnyFailedRead: every read of the source snapshot that
// fails refuses the whole snapshot with that failure.
func TestViewSourceStopsOnAnyFailedRead(t *testing.T) {
	ctx := context.Background()
	for _, method := range []string{"Head", "Wallet", "Lifecycle", "Commitment", "CommitmentLog", "HeaderHash@log", "HeaderHash@head", "Custody", "CustodyOutput", "MovementParameters", "Wallet@target"} {
		t.Run(method, func(t *testing.T) {
			g := newGapWorld(t)
			g.committedPlan(t)
			logBlock, head := g.f.logs[g.source.ID].Block, g.f.head
			target := g.f.commitments[g.source.ID].Targets[1]
			g.fc.failWhen = func(m string, at uint64) bool {
				switch method {
				case "HeaderHash@log":
					return m == "HeaderHash" && at == logBlock
				case "HeaderHash@head":
					return m == "HeaderHash" && at == head
				case "Wallet@target":
					return m == fmt.Sprintf("Wallet/%x", target)
				case "Wallet":
					return m == fmt.Sprintf("Wallet/%x", Label(g.source.ID))
				}
				return m == method
			}
			s, err := g.view.Source(ctx, g.source.ID)
			if !errors.Is(err, errInjected) {
				t.Fatalf("a failed %s read did not refuse the source snapshot: %v (snapshot %d targets)", method, err, len(s.Targets))
			}
		})
	}
}

// TestViewSourceRefusesInconsistentReads: a commitment log above the head,
// a non-canonical log and a head that moved during the read refuse.
func TestViewSourceRefusesInconsistentReads(t *testing.T) {
	ctx := context.Background()
	t.Run("log above the head", func(t *testing.T) {
		g := newGapWorld(t)
		g.committedPlan(t)
		l := g.f.logs[g.source.ID]
		l.Block = g.f.head + 1
		l.BlockHash = g.f.hash(l.Block)
		g.f.logs[g.source.ID] = l
		if _, err := g.view.Source(ctx, g.source.ID); !errors.Is(err, redemption.ErrInconsistentRead) {
			t.Fatalf("a commitment log above the head was read: %v", err)
		}
	})
	t.Run("non-canonical log", func(t *testing.T) {
		g := newGapWorld(t)
		g.committedPlan(t)
		g.f.hashes[g.f.logs[g.source.ID].Block] = [32]byte{0xee}
		if _, err := g.view.Source(ctx, g.source.ID); !errors.Is(err, ErrNotFinal) {
			t.Fatalf("a non-canonical commitment log was read: %v", err)
		}
	})
	t.Run("head moved", func(t *testing.T) {
		g := newGapWorld(t)
		g.committedPlan(t)
		g.fc.headHash = &[32]byte{0xaa}
		if _, err := g.view.Source(ctx, g.source.ID); !errors.Is(err, redemption.ErrInconsistentRead) {
			t.Fatalf("a snapshot whose head moved was returned: %v", err)
		}
	})
}

// TestViewSourceStopsAtEachBoundary: a stop at any replay boundary refuses
// the snapshot.
func TestViewSourceStopsAtEachBoundary(t *testing.T) {
	ctx := context.Background()
	for _, step := range []string{"before-movement-read", "movement-seen", "targets-seen"} {
		g := newGapWorld(t)
		g.committedPlan(t)
		g.view.boundary = func(name string) error {
			if name == step {
				return errBoundary
			}
			return nil
		}
		if _, err := g.view.Source(ctx, g.source.ID); !errors.Is(err, errBoundary) {
			t.Errorf("stop at %s did not refuse the source snapshot: %v", step, err)
		}
	}
}

// TestCommitmentRecordedAtTheHeadIsReused: a commitment log at the head
// block is canonical, only not yet final. A second Commit in the block that
// recorded it reuses it instead of failing or resubmitting.
func TestCommitmentRecordedAtTheHeadIsReused(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	first, err := g.commitments().Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.f.logs[g.source.ID].Block != g.f.head {
		t.Fatal("fixture: the commitment log must be at the head")
	}
	g.f.submitted = nil
	again, err := g.commitments().Commit(ctx)
	if err != nil || g.f.submitted != nil || !sameLabels(first, again) {
		t.Fatalf("a commitment recorded at the head block was not reused: %v (resubmitted %v)", err, g.f.submitted != nil)
	}
	s, err := g.view.Source(ctx, g.source.ID)
	if err != nil || s.Log == nil || g.view.Final(s.Head, s.Log.Log) {
		t.Fatalf("a commitment at the head must be read and not final: %v", err)
	}
}

// TestViewTargetStopsOnAnyFailedRead: every read of the target snapshot that
// fails refuses it with that failure.
func TestViewTargetStopsOnAnyFailedRead(t *testing.T) {
	ctx := context.Background()
	for _, method := range []string{"Head", "Wallet", "Custody", "MovementParameters", "IncomingLogs", "HeaderHash@log", "Incoming"} {
		for _, keyed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s keyed %v", method, keyed), func(t *testing.T) {
				g := newGapWorld(t)
				target := g.targets[0]
				key := g.addIncoming(target, 0x31, 50_000, 90)
				g.fc.failWhen = func(m string, at uint64) bool {
					if method == "HeaderHash@log" {
						return m == "HeaderHash" && at == 90
					}
					return m == method
				}
				var keys [][32]byte
				if keyed {
					keys = [][32]byte{key}
				}
				if _, err := g.view.Target(ctx, target.ID, keys); !errors.Is(err, errInjected) {
					t.Fatalf("a failed %s read did not refuse the target snapshot: %v", method, err)
				}
			})
		}
	}
}

// TestViewTargetRecords: keys select exactly their records, wherever they
// are in the log; a key without a log is ErrNotFound; a non-canonical log
// refuses; a stop at any boundary refuses.
func TestViewTargetRecords(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	target := g.targets[0]
	first := g.addIncoming(target, 0x31, 50_000, 90)
	second := g.addIncoming(target, 0x32, 60_000, 91)
	read, err := g.view.Target(ctx, target.ID, [][32]byte{second})
	if err != nil || len(read.Incoming) != 1 || read.Incoming[0].Log.Outpoint != second || read.Incoming[0].Record.Value != 60_000 {
		t.Fatalf("keyed read of the second log returned %d records: %v", len(read.Incoming), err)
	}
	all, err := g.view.Target(ctx, target.ID, nil)
	if err != nil || len(all.Incoming) != 2 {
		t.Fatalf("unkeyed read returned %d of 2 records: %v", len(all.Incoming), err)
	}
	both, err := g.view.Target(ctx, target.ID, [][32]byte{first, second})
	if err != nil || len(both.Incoming) != 2 {
		t.Fatalf("keyed read of both logs returned %d records: %v", len(both.Incoming), err)
	}
	missing := OutpointKey(incomingOutpoint(0x39, 0))
	if r, err := g.view.Target(ctx, target.ID, [][32]byte{first, missing}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a key without a log was not refused: %v (%d records)", err, len(r.Incoming))
	}
	g.f.hashes[91] = [32]byte{0xee}
	if _, err := g.view.Target(ctx, target.ID, [][32]byte{second}); !errors.Is(err, ErrNotFinal) {
		t.Fatalf("a non-canonical incoming log was read: %v", err)
	}
	delete(g.f.hashes, 91)
	for _, step := range []string{"before-movement-read", "incoming-seen"} {
		g.view.boundary = func(name string) error {
			if name == step {
				return errBoundary
			}
			return nil
		}
		if _, err := g.view.Target(ctx, target.ID, [][32]byte{first}); !errors.Is(err, errBoundary) {
			t.Errorf("stop at %s did not refuse the target snapshot: %v", step, err)
		}
	}
}
