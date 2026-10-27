//go:build !frosttest

package node

import (
	"context"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

type verificationConfig struct{}

func (*Node) boundary(context.Context, uint64, string, ...[32]byte) error { return nil }
func (*Node) configureVerification(*signing.Config, uint64)               {}
