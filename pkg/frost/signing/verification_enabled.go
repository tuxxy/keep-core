//go:build frosttest

package signing

import "context"

type verificationConfig struct{ AfterGrant func(context.Context) error }

func (e *Executor) afterGrant(ctx context.Context) error {
	if e.config.AfterGrant != nil {
		return e.config.AfterGrant(ctx)
	}
	return nil
}
