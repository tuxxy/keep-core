package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

type certificate struct {
	id         [32]byte
	seats      []uint16
	references [][32]byte
	signatures []byte
}
type readyGate struct {
	dkg.Chain
	pending chan<- certificate
	release <-chan struct{}
}

func (g readyGate) Ready(ctx context.Context, id [32]byte, seats []uint16, refs [][32]byte, sigs []byte) error {
	c := certificate{id, append([]uint16(nil), seats...), append([][32]byte(nil), refs...), append([]byte(nil), sigs...)}
	select {
	case g.pending <- c:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.Chain.Ready(ctx, id, seats, refs, sigs)
}
func (f *fixture) gateReadiness() (chan certificate, chan struct{}) {
	pending := make(chan certificate, len(f.executors))
	release := make(chan struct{})
	for _, e := range f.executors {
		e.Chain = readyGate{e.Chain, pending, release}
	}
	return pending, release
}
func (f *fixture) certificate(pending <-chan certificate) certificate {
	select {
	case c := <-pending:
		return c
	case <-f.ctx.Done():
		f.t.Fatal("no real reloaded certificate")
		return certificate{}
	}
}
func (f *fixture) certificateCall(c certificate) error {
	data, e := f.registryABI().Pack("submitReadinessV1", c.id, c.seats, c.references, c.signatures)
	if e != nil {
		return e
	}
	_, e = f.client.CallContract(f.ctx, geth.CallMsg{To: &f.deployment.Registry, Data: data}, nil)
	return e
}
func TestFrostReadinessRejectsInvalidCertificate(t *testing.T) {
	f := setup(t, "1,2,3")
	pending, release := f.gateReadiness()
	done := f.start()
	c := f.certificate(pending)
	if err := f.certificateCall(c); err != nil {
		t.Fatal("valid real certificate failed simulation", err)
	}
	w, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, c.id)
	if err != nil {
		t.Fatal(err)
	}
	wrongOperator := c
	wrongOperator.signatures = append([]byte(nil), c.signatures...)
	digest, err := f.executors[0].Chain.ReadinessDigest(f.ctx, w, c.seats[0], c.references[0])
	if err != nil {
		t.Fatal(err)
	}
	// Recoverable, correctly domain-bound signature from another selected
	// operator: only the seat-to-operator check can reject this certificate.
	for _, node := range f.nodes {
		if crypto.PubkeyToAddress(node.key.PublicKey) != w.Descriptor.Operators[c.seats[0]-1] {
			sig, err := dkg.Sign(node.key, digest)
			if err != nil {
				t.Fatal(err)
			}
			copy(wrongOperator.signatures[:65], sig)
			break
		}
	}
	duplicate := certificate{c.id, []uint16{c.seats[0], c.seats[0]}, [][32]byte{c.references[0], c.references[0]}, append(append([]byte(nil), c.signatures[:65]...), c.signatures[:65]...)}
	insufficient := certificate{c.id, c.seats[:1], c.references[:1], c.signatures[:65]}
	foreign := c
	foreign.signatures = make([]byte, len(c.signatures))
	zeroRef := c
	zeroRef.references = append([][32]byte(nil), c.references...)
	zeroRef.references[0] = [32]byte{}
	wrongID := c
	wrongID.id[0] ^= 1
	for name, bad := range map[string]certificate{"wrong operator": wrongOperator, "duplicate seat": duplicate, "insufficient count": insufficient, "foreign signatures": foreign, "zero reference": zeroRef, "foreign wallet": wrongID} {
		if err := f.certificateCall(bad); err == nil {
			t.Fatal("accepted", name)
		}
	}
	w, err = f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, c.id)
	if err != nil || w.State != 1 {
		t.Fatal("invalid certificate changed pending state", err)
	}
	close(release)
	r := f.await(done)
	for _, err := range r.errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestFrostReadinessDeadlineEdges(t *testing.T) {
	for _, atDeadline := range []bool{false, true} {
		t.Run(fmt.Sprint("at-deadline=", atDeadline), func(t *testing.T) {
			f := setup(t, "1,2,3")
			pending, release := f.gateReadiness()
			done := f.start()
			c := f.certificate(pending)
			// Every node has finished acceptance and reload before the exact
			// deadline jump. This test targets the certificate boundary.
			for i := 1; i < len(f.executors); i++ {
				f.certificate(pending)
			}
			w := f.pending()
			f.stopMining()
			if !atDeadline {
				f.mineTo(w.Deadline - 2)
				close(release)
				r := f.await(done)
				for _, err := range r.errs {
					if err != nil {
						t.Fatal("pre-deadline certificate failed", err)
					}
				}
				return
			}
			// Hold node views while moving directly to the equality block. This leaves
			// PendingReady in storage, so rejection is specifically the deadline guard.
			f.chainMu.Lock()
			head, err := f.client.BlockNumber(f.ctx)
			if err != nil {
				f.chainMu.Unlock()
				t.Fatal(err)
			}
			var out interface{}
			err = f.rpc.CallContext(f.ctx, &out, "anvil_mine", fmt.Sprintf("0x%x", w.Deadline-head))
			if err != nil {
				f.chainMu.Unlock()
				t.Fatal(err)
			}
			at, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, c.id)
			if err != nil || at.State != 1 {
				f.chainMu.Unlock()
				t.Fatal("deadline test did not retain PendingReady", err)
			}
			err = f.certificateCall(c)
			f.chainMu.Unlock()
			if err == nil {
				t.Fatal("certificate accepted at deadline")
			}
			close(release)
			r := f.await(done)
			for i, err := range r.errs {
				if !errors.Is(err, dkg.ErrExpired) {
					t.Fatalf("operator %d: expected classified expiry, got %v", i, err)
				}
				saved, loadErr := f.journals[i].LoadWallet(f.ctx)
				if loadErr != nil || saved.State != "Closed" || saved.Quarantined {
					t.Fatalf("operator %d: expired wallet record %+v, load error %v", i, saved, loadErr)
				}
			}
			closed, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, c.id)
			if err != nil || closed.State != 3 {
				t.Fatal("late pending wallet did not expire", err)
			}
		})
	}
}

func (f *fixture) registryABI() abi.ABI {
	a, err := binding.FrostWalletRegistryMetaData.GetAbi()
	if err != nil {
		f.t.Fatal(err)
	}
	return *a
}
