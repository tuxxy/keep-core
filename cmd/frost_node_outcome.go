package cmd

import (
	"context"
	"errors"
	"net/http"
)

// Classify before canceling the runtime context so an expired bound cannot be
// mistaken for a requested stop when shutdown races another completion.
func localNodeOutcome(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
