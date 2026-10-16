package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

type k06Directives map[[32]byte]redemption.Directive

func (d k06Directives) Directive(_ context.Context, r [32]byte) (redemption.Directive, bool, error) {
	out, ok := d[r]
	return out, ok, nil
}

// TestResolutionSweepNeedsDirectiveAfterContainment: a resolution attested
// while Reserved can be neither signed nor broadcast after the wallet is
// contained, until every operator holds a governance cancel directive. Two
// orderings: contained before signing, and contained after signing.
func TestResolutionSweepNeedsDirectiveAfterContainment(t *testing.T) {
	for _, signFirst := range []bool{false, true} {
		name := "attest-contain-sign"
		if signFirst {
			name = "sign-contain-broadcast"
		}
		t.Run(name, func(t *testing.T) {
			w := newK06World(t)
			ctx := w.f.ctx
			w.fundBySweep()
			w.request(100_000, p2wpkh(0x64))
			_, reservation := w.admit()
			h := w.deposit(1_000_000, w.locktime(3*k06RefundDelay), [20]byte{})
			r := w.round(h.revealBlock + k06Finality)
			plan, err := w.sweepers[0].Planner.Build(ctx, r.Anchor, [][32]byte{h.key}, reservation)
			if err != nil {
				t.Fatal(err)
			}
			var parts []map[uint16][]byte
			var auth deposit.ResolutionAuthorization
			for _, a := range w.resolution {
				sigs, au, err := a.Attest(ctx, plan, w.head()+100)
				if err != nil {
					t.Fatal(err)
				}
				parts, auth = append(parts, sigs), au
			}
			seats, signatures, err := redemption.Collect(w.wallet.Threshold, parts...)
			if err != nil {
				t.Fatal(err)
			}
			if signFirst {
				for i, err := range w.signSweep(plan, r.Number) {
					if err != nil {
						t.Fatalf("operator %d: %v", i, err)
					}
				}
			}
			custody, _ := w.chain.Custody(ctx, w.head(), w.walletID)
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return w.bridge.QuarantineFrostWallet(o, w.walletID, custody.StateVersion, crypto.Keccak256Hash([]byte("issuance fault")), crypto.Keccak256Hash([]byte("evidence")))
			})
			launches := w.launchTotal()
			if signFirst {
				for i, s := range w.sweepers {
					if err := s.Broadcast(ctx, plan); err == nil {
						t.Fatalf("operator %d broadcast a contained resolution sweep", i)
					}
				}
			} else {
				for i, err := range w.signSweep(plan, r.Number) {
					if !errors.Is(err, signing.ErrUnauthorized) {
						t.Fatalf("operator %d signed a contained resolution sweep: %v", i, err)
					}
				}
			}
			if w.launchTotal() != launches {
				t.Fatal("a worker started for a contained resolution sweep")
			}
			if unspent, err := w.core.Unspent(ctx, plan.SweepMain().Outpoint); err != nil || !unspent {
				t.Fatal("the contained main was spent", err)
			}
			directives := k06Directives{reservation: {Reservation: reservation, Class: "issuance-fault", Action: redemption.DirectiveCancel}}
			for _, s := range w.sweepers {
				s.Authority.Directives = directives
			}
			if !signFirst {
				for i, err := range w.signSweep(plan, r.Number) {
					if err != nil {
						t.Fatalf("operator %d with a cancel directive: %v", i, err)
					}
				}
			}
			if err = w.sweepers[0].Broadcast(ctx, plan); err != nil {
				t.Fatal("broadcast with a cancel directive:", err)
			}
			w.coreMine(1)
			action, err := w.sweeps.ProveSweep(ctx, w.walletID, plan.BitcoinTransactionHash(), plan.SweepVault(), &spv.FrostResolution{Authorization: auth, Seats: seats, Signatures: signatures})
			if err != nil || action.Kind != "sweep-resolution-proof" {
				t.Fatalf("resolution proof: %+v %v", action, err)
			}
			if intent, _ := w.chain.Intent(ctx, w.head(), reservation); intent.State != redemption.IntentConflictResolved {
				t.Fatal("reservation not resolved", intent.State)
			}
		})
	}
}
