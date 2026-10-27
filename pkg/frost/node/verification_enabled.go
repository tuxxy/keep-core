//go:build frosttest

package node

import (
	"context"
	"fmt"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"os"
)

type verificationConfig struct{ VerificationPause *VerificationPause }

// VerificationPause is an explicit local-only fault adapter. It pauses a real
// component boundary after writing public progress, without reading worker data.
type VerificationPause struct {
	Epoch            uint64
	Boundary, Marker string
}

func (n *Node) boundary(ctx context.Context, epoch uint64, name string, witness ...[32]byte) error {
	p := n.cfg.VerificationPause
	if p == nil || (p.Epoch != 0 && p.Epoch != epoch) || p.Boundary != name {
		return nil
	}
	file, err := os.OpenFile(p.Marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(file, "epoch=%d boundary=%s\n", epoch, name)
	if err == nil && len(witness) == 1 {
		_, err = fmt.Fprintf(file, "witness=%x\n", witness[0])
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (n *Node) configureVerification(c *signing.Config, epoch uint64) {
	c.AfterGrant = func(ctx context.Context) error { return n.boundary(ctx, epoch, "after-incoming-grant") }
}
