//go:build frosttest

package signing

import (
	"context"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"testing"
)

func TestPossibleReleaseIncludesGrantWithoutDone(t *testing.T) {
	e, _, _, _ := sweepExecutor(t)
	journal := e.config.Journal
	e.config.AfterGrant = func(context.Context) error { return errors.New("interrupted after durable grant") }
	if released, err := e.PossibleRelease(context.Background()); err != nil || released {
		t.Fatal("fresh plan already marked released", err)
	}
	if _, err := e.Run(context.Background()); err == nil {
		t.Fatal("grant interruption ignored")
	}
	attempt, err := e.attempt(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = journal.ReadSigningRecord(context.Background(), signingDoneID(snowfallengine.AttemptID(attempt))); err == nil {
		t.Fatal("test unexpectedly retained a done record")
	}
	if released, err := e.PossibleRelease(context.Background()); err != nil || !released {
		t.Fatal("control IncompleteGrant assertion: grant without done was considered safe", err)
	}
}
