package signing

import (
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Review P1: a certified cancellation must be minable now. A non-final
// alternative at the top of the fee envelope would block every later
// alternative (which must pay strictly more) and strand recovery.
func TestCancellationPlanRequiresImmediateFinality(t *testing.T) {
	p, _, _ := planFixture(t)
	payment, err := NewPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	change := p.Prevouts[0].PublicKeyScript
	cancellation := func(sequence, locktime uint32) *bitcoin.Transaction {
		point := p.Prevouts[0].Outpoint
		return &bitcoin.Transaction{Version: 2, Locktime: locktime, Inputs: []*bitcoin.TransactionInput{{Outpoint: &point, Sequence: sequence}}, Outputs: []*bitcoin.TransactionOutput{{Value: 100000 - 1000, PublicKeyScript: change}}}
	}
	for _, tc := range []struct {
		name               string
		sequence, locktime uint32
		ok                 bool
	}{
		{"final now", 0xfffffffd, 0, true},
		{"locktime disabled by final sequences", 0xffffffff, 0xffffffff, true},
		{"far-future absolute locktime", 0xfffffffd, 0xffffffff, false},
		{"any nonzero absolute locktime (conservative)", 0xfffffffd, 1, false},
		{"relative locktime enabled", 0x00000001, 0, false},
		{"relative time lock enabled", 0x00400001, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCancellationPlan(payment, cancellation(tc.sequence, tc.locktime))
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v err=%v", tc.ok, err)
			}
		})
	}
}
