//go:build !frosttest

package signing

import "context"

type verificationConfig struct{}

func (*Executor) afterGrant(context.Context) error { return nil }
