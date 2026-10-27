package cmd

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestFrostLocalDeadlineIsFailure(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, raced := range []error{nil, context.Canceled, context.DeadlineExceeded, http.ErrServerClosed} {
		if err := localNodeOutcome(expired, raced); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("control NodeDeadline assertion: expired runtime reported success", err)
		}
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if err := localNodeOutcome(stopped, context.Canceled); err != nil {
		t.Fatal("operator stop failed", err)
	}
	failure := errors.New("runtime failed")
	if err := localNodeOutcome(context.Background(), failure); !errors.Is(err, failure) {
		t.Fatal("runtime failure hidden", err)
	}
}
