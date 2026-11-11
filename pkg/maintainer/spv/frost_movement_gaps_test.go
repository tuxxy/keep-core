package spv

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Mutation batch M8 (FrostMovementMaintainer). Fixtures are in
// frost_gaps_fixture_test.go.
//
// Invariant (KC-SF-V3 K-07, P-K07-INCOMING-ONCE; FrostMovementMaintainer
// doc): a movement or incoming-sweep proof is submitted exactly once, only
// for a transaction confirmed to the Bridge's proof depth and not yet
// recorded; a transaction recorded for this wallet is "already-proven"
// without a submission; one recorded for another wallet, a failed read, a
// wrong transaction from Bitcoin or a refused submission is an error; and a
// submission whose reply is lost is settled by rereading the record.

type movementEntry struct {
	name string
	kind string
	sent string
	run  func(m FrostMovementMaintainer, wallet, hash [32]byte) (FrostAction, error)
}

var movementEntries = []movementEntry{
	{"movement", "movement-proof", "movement", func(m FrostMovementMaintainer, wallet, hash [32]byte) (FrostAction, error) {
		return m.ProveMovement(context.Background(), wallet, hash)
	}},
	{"incoming sweep", "incoming-sweep-proof", "incoming-sweep", func(m FrostMovementMaintainer, wallet, hash [32]byte) (FrostAction, error) {
		return m.ProveIncomingSweep(context.Background(), wallet, hash)
	}},
}

type movementWorld struct {
	f      *faults
	bridge *fakeBridge
	btc    *proofBitcoin
	wallet [32]byte
	hash   [32]byte
	m      FrostMovementMaintainer
}

// newMovementWorld: one transaction with six confirmations over headers of
// relay difficulty 32; factor 2 needs two headers; Confirmations 2 is the
// floor. A mined proof records the transaction for the wallet.
func newMovementWorld(t *testing.T) *movementWorld {
	w := &movementWorld{f: &faults{}, wallet: [32]byte{0xaa}}
	w.bridge = newFakeBridge(w.f)
	w.btc = newProofBitcoin(t, w.f)
	w.hash = w.btc.confirm(t, gapsTx(1), 6)
	w.bridge.submit = func(s submission) error {
		w.bridge.proven[s.transaction] = w.wallet
		return nil
	}
	w.m = FrostMovementMaintainer{Chain: w.bridge, Bitcoin: w.btc, Confirmations: 2}
	return w
}

func (w *movementWorld) run(e movementEntry) (FrostAction, error) {
	var action FrostAction
	err := recovered(func() error {
		var err error
		action, err = e.run(w.m, w.wallet, w.hash)
		return err
	})
	return action, err
}

func TestFrostMovementProofOutcomes(t *testing.T) {
	other := [32]byte{0xbb}
	refused := errors.New("fixture: refused")
	tests := []struct {
		name    string
		setup   func(w *movementWorld)
		kind    string // "" uses the entry's own kind
		wantErr bool
		submits int
		// waits: the error is ErrMovementNotConfirmed (retry later), not a
		// failure.
		waits bool
	}{
		{"unproven confirmed transaction is proven once", func(w *movementWorld) {}, "", false, 1, false},
		{"recorded for this wallet is not resubmitted", func(w *movementWorld) { w.bridge.proven[w.hash] = w.wallet }, "already-proven", false, 0, false},
		{"recorded for another wallet is refused", func(w *movementWorld) { w.bridge.proven[w.hash] = other }, "", true, 0, false},
		{"zero confirmations is refused", func(w *movementWorld) { w.m.Confirmations = 0 }, "", true, 0, false},
		{"below the operator floor waits", func(w *movementWorld) { w.m.Confirmations = 7 }, "", true, 0, true},
		{"below the floor waits even when the Bridge cannot be read", func(w *movementWorld) {
			w.m.Confirmations = 7
			w.bridge.factorErr = errors.New("fixture: Bridge down")
		}, "", true, 0, true},
		{"headers outside the relay epochs wait", func(w *movementWorld) { w.bridge.current, w.bridge.previous = 64, 64 }, "", true, 0, true},
		{"Bitcoin returning another transaction is refused", func(w *movementWorld) {
			w.btc.substitute[bitcoin.Hash(w.hash)] = gapsTx(2)
		}, "", true, 0, false},
		{"refused submission is an error", func(w *movementWorld) {
			w.bridge.submit = func(submission) error { return refused }
		}, "", true, 1, false},
		{"lost reply is settled by the record", func(w *movementWorld) {
			w.bridge.submit = func(s submission) error { w.bridge.proven[s.transaction] = w.wallet; return refused }
		}, "", false, 1, false},
		{"lost reply recorded for another wallet is an error", func(w *movementWorld) {
			w.bridge.submit = func(s submission) error { w.bridge.proven[s.transaction] = other; return refused }
		}, "", true, 1, false},
	}
	for _, e := range movementEntries {
		for _, test := range tests {
			t.Run(e.name+"/"+test.name, func(t *testing.T) {
				w := newMovementWorld(t)
				test.setup(w)
				action, err := w.run(e)
				if (err != nil) != test.wantErr {
					t.Fatalf("%s: error %v, expected error %v", test.name, err, test.wantErr)
				}
				if errors.Is(err, errFixtureRead) || errors.Is(err, errFixturePanic) {
					t.Fatalf("%s: unexpected %v", test.name, err)
				}
				if errors.Is(err, ErrMovementNotConfirmed) != test.waits {
					t.Fatalf("%s: error %v, expected waiting (not confirmed) %v", test.name, err, test.waits)
				}
				if got := len(w.bridge.sent); got != test.submits {
					t.Fatalf("%s: %d submissions, expected %d", test.name, got, test.submits)
				}
				for _, s := range w.bridge.sent {
					if s.kind != e.sent || s.transaction != w.hash {
						t.Fatalf("%s: submitted %s of %x, expected %s of %x", test.name, s.kind, s.transaction, e.sent, w.hash)
					}
				}
				want := test.kind
				if want == "" {
					want = e.kind
				}
				if !test.wantErr && (action.Kind != want || action.Transaction != w.hash) {
					t.Fatalf("%s: action %q %x, expected %q %x", test.name, action.Kind, action.Transaction, want, w.hash)
				}
			})
		}
	}
}

// TestFrostMovementProofReadFaults: every failed read (Bridge or Bitcoin) is
// an error and nothing is submitted.
func TestFrostMovementProofReadFaults(t *testing.T) {
	for _, e := range movementEntries {
		t.Run(e.name, func(t *testing.T) {
			for n := 1; n < 200; n++ {
				w := newMovementWorld(t)
				w.f.failAt = n
				_, err := w.run(e)
				if !w.f.fired {
					if n == 1 {
						t.Fatal("fixture: no read was made")
					}
					return
				}
				if err == nil {
					t.Fatalf("read %d failed, but the proof reported success", n)
				}
				if errors.Is(err, ErrMovementNotConfirmed) && w.f.firedOp != "confirmations" {
					t.Fatalf("read %d failed and was reported as not confirmed: %v", n, err)
				}
				if len(w.bridge.sent) != 0 {
					t.Fatalf("read %d failed, but %d proofs were submitted", n, len(w.bridge.sent))
				}
			}
			t.Fatal("fixture: reads never ended")
		})
	}
}

// TestFrostMovementProofSteps: a crash point (the package's boundary seam)
// that fails before the submission or after the record is seen is reported;
// before the submission nothing is sent.
func TestFrostMovementProofSteps(t *testing.T) {
	steps := map[string][3]string{
		"movement":       {"before-movement-proof", "movement-proof-submitted", "incoming-recorded"},
		"incoming sweep": {"before-sweep-proof", "sweep-proof-submitted", "credit-seen"},
	}
	for _, e := range movementEntries {
		names := steps[e.name]
		t.Run(e.name+"/all steps run in order", func(t *testing.T) {
			w := newMovementWorld(t)
			var seen []string
			w.m.boundary = func(step string) error { seen = append(seen, step); return nil }
			if _, err := w.run(e); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(seen) != 3 || seen[0] != names[0] || seen[1] != names[1] || seen[2] != names[2] {
				t.Fatalf("steps %v, expected %v", seen, names)
			}
		})
		for i, failing := range []string{names[0], names[2]} {
			t.Run(e.name+"/"+failing+" fails", func(t *testing.T) {
				w := newMovementWorld(t)
				w.m.boundary = func(step string) error {
					if step == failing {
						return errors.New("fixture: crash")
					}
					return nil
				}
				if _, err := w.run(e); err == nil {
					t.Fatalf("a failure at %s was not reported", failing)
				}
				if want := i; len(w.bridge.sent) != want {
					t.Fatalf("%d submissions after a failure at %s, expected %d", len(w.bridge.sent), failing, want)
				}
			})
		}
	}
}
