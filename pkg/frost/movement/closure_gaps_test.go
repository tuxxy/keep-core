package movement

// Mutation batch M6-movement: closure (closure.go). Invariants (KC-SF-V3
// K-07, C-09; V-08 P-K07-CLOSE-ONLY-WHEN-CLEAR, P-K07-SIGNER-RETENTION):
//   - the closure ledger is read at the finalized head (head - finality), and
//     a chain shorter than the finality depth has none;
//   - Close notifies only after an empty ledger at the finalized head and
//     again at the current head; any failed read or stop refuses;
//   - a failed FinalizeClosing is success only when the wallet reads Closed
//     afterwards (a lost reply);
//   - the archive record is written once; a failed write is success only
//     when the record is already there; IsArchived reports a read failure.
// Unit level on the real Closure over the fixture chain and journal.

import (
	"context"
	"errors"
	"testing"
)

func (g *gapWorld) closure(j *faultJournal) Closure {
	return Closure{Chain: g.fc, Policy: Policy{FinalityBlocks: 2}, Journal: j}
}

// TestClosureReadsTheFinalizedLedger: the first read is at head - finality,
// down to a chain exactly as long as the finality depth; a shorter chain
// refuses.
func TestClosureReadsTheFinalizedLedger(t *testing.T) {
	ctx := context.Background()
	for _, head := range []uint64{100, 2} {
		g := newGapWorld(t)
		g.f.head = head
		if _, err := g.closure(newFaultJournal()).Obligations(ctx, g.source.ID); err != nil {
			t.Fatalf("head %d: %v", head, err)
		}
		if len(g.fc.obligationsAt) != 1 || g.fc.obligationsAt[0] != head-2 {
			t.Fatalf("head %d: ledger read at %v, want the finalized head %d", head, g.fc.obligationsAt, head-2)
		}
	}
	g := newGapWorld(t)
	g.fc.failOn("Head")
	if _, err := g.closure(newFaultJournal()).Obligations(ctx, g.source.ID); !errors.Is(err, errInjected) {
		t.Fatalf("a failed head read was not reported: %v", err)
	}
	g.fc.failOn()
	g.f.head = 1
	err := recovered(func() error { _, err := g.closure(newFaultJournal()).Obligations(ctx, g.source.ID); return err })
	if err == nil || errors.Is(err, errPanic) || len(g.fc.obligationsAt) != 0 {
		t.Fatalf("a chain shorter than the finality depth was read: %v at %v", err, g.fc.obligationsAt)
	}
}

// TestCloseStopsOnAnyFailure: every failed read and every boundary stop
// before the notice refuses the closure and notifies nothing; a stop after
// the notice refuses before the archive is written.
func TestCloseStopsOnAnyFailure(t *testing.T) {
	ctx := context.Background()
	reads := []string{"Head", "Head@current", "Obligations", "Obligations@current", "Wallet"}
	for _, read := range reads {
		t.Run("read "+read, func(t *testing.T) {
			g := newGapWorld(t)
			j := newFaultJournal()
			head := g.f.head
			g.fc.failWhen = func(m string, at uint64) bool {
				switch read {
				case "Obligations":
					return m == "Obligations" && at == head-2
				case "Obligations@current":
					return m == "Obligations" && at == head
				case "Head@current":
					return m == "Head" && g.fc.obligationReads.Load() > 0
				}
				return m == read
			}
			if err := g.closure(j).Close(ctx, g.source.ID); !errors.Is(err, errInjected) || g.f.finalized != 0 || len(j.records) != 0 {
				t.Fatalf("closed (notified %d, archived %d) after a failed %s read: %v", g.f.finalized, len(j.records), read, err)
			}
		})
	}
	for _, step := range []string{"before-closure-read", "closure-ledger-seen", "before-closure-notify", "closure-notified", "before-archive"} {
		t.Run("stop "+step, func(t *testing.T) {
			g := newGapWorld(t)
			j := newFaultJournal()
			c := g.closure(j)
			c.boundary = stopAt(step)
			err := c.Close(ctx, g.source.ID)
			if !errors.Is(err, errBoundary) || len(j.records) != 0 {
				t.Fatalf("a stop at %s did not stop the closure before the archive: %v", step, err)
			}
			notifies := step == "closure-notified" || step == "before-archive"
			if (g.f.finalized != 0) != notifies {
				t.Fatalf("a stop at %s notified %d times", step, g.f.finalized)
			}
		})
	}
}

// TestCloseFinalizeFailure: a failed notification is success only when the
// wallet reads Closed afterwards; a failed reread refuses.
func TestCloseFinalizeFailure(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		applied   bool
		rereadErr string
		ok        bool
	}{
		"lost reply, closed":          {true, "", true},
		"refused, still closing":      {false, "", false},
		"lost reply, head read fails": {true, "Head", false},
		"refused, wallet read fails":  {false, "Wallet", false},
		"lost reply, wallet fails":    {true, "Wallet", false},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGapWorld(t)
			j := newFaultJournal()
			g.fc.finalizeErr, g.fc.finalizeApplies = errInjected, c.applied
			cl := g.closure(j)
			// Reads fail only after the notification was sent.
			g.fc.failWhen = func(m string, _ uint64) bool { return g.fc.finalizes.Load() > 0 && m == c.rereadErr }
			err := cl.Close(ctx, g.source.ID)
			archived, _ := IsArchived(ctx, j, g.source.ID)
			if c.ok && (err != nil || !archived) {
				t.Fatalf("a closure whose notice was mined was refused: %v", err)
			}
			if !c.ok && (err == nil || archived) {
				t.Fatalf("a failed notice was reported as closed (archived %v): %v", archived, err)
			}
		})
	}
}

// TestCloseArchive: the archive write is idempotent; a failed write is
// success only when the record is already there; IsArchived reports a read
// failure and a missing record.
func TestCloseArchive(t *testing.T) {
	ctx := context.Background()
	g := newGapWorld(t)
	j := newFaultJournal()
	if archived, err := IsArchived(ctx, j, g.source.ID); err != nil || archived {
		t.Fatalf("an empty journal reports the wallet archived: %v, %v", archived, err)
	}
	j.failWrites = true
	if err := g.closure(j).Close(ctx, g.source.ID); !errors.Is(err, errInjected) {
		t.Fatalf("a failed archive write without a record was success: %v", err)
	}
	j.failWrites = false
	if err := g.closure(j).Close(ctx, g.source.ID); err != nil {
		t.Fatal(err)
	}
	if archived, err := IsArchived(ctx, j, g.source.ID); err != nil || !archived {
		t.Fatalf("not archived: %v", err)
	}
	j.failWrites = true
	if err := g.closure(j).Close(ctx, g.source.ID); err != nil {
		t.Fatalf("a failed archive write with the record already there was refused: %v", err)
	}
	j.failWrites = false
	j.failReads = true
	if archived, err := IsArchived(ctx, j, g.source.ID); err == nil {
		t.Fatalf("a failed archive read was reported as %v without an error", archived)
	}
	if err := recovered(func() error {
		jw := newFaultJournal()
		jw.failWrites, jw.failReads = true, true
		return g.closure(jw).Close(ctx, g.source.ID)
	}); !errors.Is(err, errInjected) {
		t.Fatalf("a failed archive write and read was success: %v", err)
	}
}
