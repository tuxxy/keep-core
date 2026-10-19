package signing

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/btcsuite/btcd/txscript"
)

// requireDefenses fails unless every digest of e's plan has its defense
// record; engines call it before signing.
func requireDefenses(e *Executor) error {
	p := e.config.Plan
	for _, digest := range p.snapshot.Digests() {
		if _, err := ReadDefense(context.Background(), e.config.Journal, p.WalletID(), digest); err != nil {
			return errors.New("defense record not durable before the grant")
		}
	}
	return nil
}

// Every released digest is defensible before the grant, with the C-08 route
// its purpose needs (P-K08-EVIDENCE-RETAINED).
func TestDefenseRecordBeforeGrant(t *testing.T) {
	ctx := context.Background()
	t.Run("payment names its reservation", func(t *testing.T) {
		e, reservation, _, engine := executorFixture(t)
		var missing error
		engine.before = func() { missing = requireDefenses(e) }
		if _, err := e.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if missing != nil {
			t.Fatal(missing)
		}
		p := e.config.Plan
		r, err := ReadDefense(ctx, e.config.Journal, p.WalletID(), p.snapshot.Digests()[0])
		if err != nil || r.Kind != DefenseIntent || r.Ref != reservation.view.ID || r.Input != 0 || r.LeafHash != ([32]byte{}) || r.Transaction.Hash() != p.BitcoinTransactionHash() || len(r.Prevouts) != len(p.snapshot.PreviousOutputs()) {
			t.Fatalf("payment defense %+v %v", r, err)
		}
	})
	t.Run("sweep keeps the deposit leaf of each script-path input", func(t *testing.T) {
		e, _, _, engine := sweepExecutor(t)
		var missing error
		engine.before = func() {
			if missing == nil {
				missing = requireDefenses(e)
			}
		}
		if _, err := e.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if missing != nil {
			t.Fatal(missing)
		}
		p := e.config.Plan
		spends := p.snapshot.Spends()
		for i, digest := range p.snapshot.Digests() {
			r, err := ReadDefense(ctx, e.config.Journal, p.WalletID(), digest)
			if err != nil || r.Kind != DefenseSweep || r.Input != uint32(i) {
				t.Fatalf("sweep defense %d: %+v %v", i, r, err)
			}
			want := [32]byte{}
			if spends[i] != nil {
				want = txscript.NewBaseTapLeaf(spends[i].Leaf).TapHash()
			}
			if r.LeafHash != want {
				t.Fatalf("input %d leaf hash %x, want %x", i, r.LeafHash, want)
			}
		}
	})
	t.Run("movement names its period", func(t *testing.T) {
		proposal, _, _ := movementFixtureWithFunding(t)
		plan, err := NewMovementPlan(proposal)
		if err != nil {
			t.Fatal(err)
		}
		records, err := plan.defenses([32]byte{0x6e})
		if err != nil || len(records) != 1 || records[0].Kind != DefenseMovement || binary.BigEndian.Uint32(records[0].Ref[28:]) != plan.MovementPeriod() || [28]byte(records[0].Ref[:28]) != [28]byte{} {
			t.Fatalf("movement defense %+v %v", records, err)
		}
	})
	t.Run("a refused plan records nothing it could not defend", func(t *testing.T) {
		e, a, _, _ := sweepExecutor(t)
		a.err = errors.New("refused")
		if _, err := e.Run(ctx); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
		if requireDefenses(e) == nil {
			t.Fatal("a plan refused before its intent record wrote defenses")
		}
	})
}
