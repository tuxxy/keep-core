package spv

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// Mutation batch M8 (FrostSweepMaintainer). Fixtures are in
// frost_gaps_fixture_test.go.

// --- ProveSweep ---------------------------------------------------------
//
// Invariant (KC-SF-V3 K-06, P-K06-CREDIT-ONCE; FrostSweepMaintainer doc): a
// confirmed sweep is proven once, through the ordinary route or, with a
// resolution, through the C-04 resolution route. It is "already-credited"
// without a submission when every deposit input is swept; a sweep input
// that is neither a C-05 deposit nor this wallet's custody is refused; the
// proof includes the main exactly when a custody input is spent; a failed
// read, a wrong transaction or a refused submission is an error; a lost
// reply is settled by rereading the deposit records.

type sweepWorld struct {
	f      *faults
	bridge *fakeBridge
	btc    *proofBitcoin
	wallet [32]byte
	m      FrostSweepMaintainer
	// outpoints: the custody main, another custody output of the wallet,
	// two deposits, a foreign custody output and an unknown output.
	main, other, d1, d2, foreign, unknown bitcoin.TransactionOutpoint
}

func newSweepWorld(t *testing.T) *sweepWorld {
	w := &sweepWorld{f: &faults{}, wallet: [32]byte{0x5e}}
	w.bridge = newFakeBridge(w.f)
	w.btc = newProofBitcoin(t, w.f)
	op := func(tag byte) bitcoin.TransactionOutpoint {
		return bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0x10, tag}, OutputIndex: uint32(tag)}
	}
	w.main, w.other, w.d1, w.d2, w.foreign, w.unknown = op(1), op(2), op(3), op(4), op(5), op(6)
	w.bridge.custody = redemption.Custody{MainOutpoint: deposit.OutpointKey(w.main)}
	w.bridge.outputs[deposit.OutpointKey(w.main)] = redemption.CustodyOutput{WalletID: w.wallet, Value: 1}
	w.bridge.outputs[deposit.OutpointKey(w.other)] = redemption.CustodyOutput{WalletID: w.wallet, Value: 1}
	w.bridge.outputs[deposit.OutpointKey(w.foreign)] = redemption.CustodyOutput{WalletID: [32]byte{0xf0}, Value: 1}
	for _, d := range []bitcoin.TransactionOutpoint{w.d1, w.d2} {
		w.bridge.deposits[deposit.OutpointKey(d)] = deposit.Record{RevealedAt: 1}
	}
	// A mined sweep proof credits every deposit it spends.
	w.bridge.submit = func(s submission) error { w.credit(); return nil }
	w.m = FrostSweepMaintainer{Chain: w.bridge, Bitcoin: w.btc, Confirmations: 2}
	return w
}

func (w *sweepWorld) credit() {
	for k, r := range w.bridge.deposits {
		r.SweptAt = 7
		w.bridge.deposits[k] = r
	}
}

func (w *sweepWorld) sweep(t *testing.T, inputs ...bitcoin.TransactionOutpoint) [32]byte {
	return w.btc.confirm(t, gapsTx(0x55, inputs...), 6)
}

func TestFrostSweepProofOutcomes(t *testing.T) {
	refused := errors.New("fixture: refused")
	vault := [20]byte{0x7a}
	tests := []struct {
		name        string
		setup       func(t *testing.T, w *sweepWorld) [32]byte
		credited    bool // "already-credited"
		wantErr     bool
		submits     int
		includeMain bool
	}{
		{"main then an unswept deposit includes the main", func(t *testing.T, w *sweepWorld) [32]byte {
			return w.sweep(t, w.main, w.d1)
		}, false, false, 1, true},
		{"deposits only do not include the main", func(t *testing.T, w *sweepWorld) [32]byte {
			return w.sweep(t, w.d1, w.d2)
		}, false, false, 1, false},
		{"another custody output of the wallet includes the main", func(t *testing.T, w *sweepWorld) [32]byte {
			return w.sweep(t, w.other, w.d1)
		}, false, false, 1, true},
		{"one deposit still unswept is proven", func(t *testing.T, w *sweepWorld) [32]byte {
			r := w.bridge.deposits[deposit.OutpointKey(w.d1)]
			r.SweptAt = 3
			w.bridge.deposits[deposit.OutpointKey(w.d1)] = r
			return w.sweep(t, w.d1, w.d2)
		}, false, false, 1, false},
		{"every deposit swept is already credited", func(t *testing.T, w *sweepWorld) [32]byte {
			w.credit()
			return w.sweep(t, w.main, w.d1, w.d2)
		}, true, false, 0, false},
		{"a foreign custody input is refused", func(t *testing.T, w *sweepWorld) [32]byte {
			return w.sweep(t, w.foreign, w.d1)
		}, false, true, 0, false},
		{"an unknown input is refused", func(t *testing.T, w *sweepWorld) [32]byte {
			return w.sweep(t, w.unknown, w.d1)
		}, false, true, 0, false},
		{"zero confirmations is refused", func(t *testing.T, w *sweepWorld) [32]byte {
			w.m.Confirmations = 0
			return w.sweep(t, w.d1)
		}, false, true, 0, false},
		{"below the operator floor waits", func(t *testing.T, w *sweepWorld) [32]byte {
			w.m.Confirmations = 7
			return w.sweep(t, w.d1)
		}, false, true, 0, false},
		{"Bitcoin returning another transaction is refused", func(t *testing.T, w *sweepWorld) [32]byte {
			hash := w.sweep(t, w.d1)
			w.btc.substitute[bitcoin.Hash(hash)] = gapsTx(0x56, w.d1)
			return hash
		}, false, true, 0, false},
		{"refused submission is an error", func(t *testing.T, w *sweepWorld) [32]byte {
			w.bridge.submit = func(submission) error { return refused }
			return w.sweep(t, w.d1)
		}, false, true, 1, false},
		{"lost reply is settled by the credit", func(t *testing.T, w *sweepWorld) [32]byte {
			w.bridge.submit = func(submission) error { w.credit(); return refused }
			return w.sweep(t, w.d1)
		}, false, false, 1, false},
	}
	for _, route := range []string{"ordinary", "resolution"} {
		for _, test := range tests {
			t.Run(route+"/"+test.name, func(t *testing.T) {
				w := newSweepWorld(t)
				hash := test.setup(t, w)
				var resolution *FrostResolution
				wantKind, wantSent, wantReservation := "sweep-proof", "sweep", [32]byte{}
				if route == "resolution" {
					wantReservation = [32]byte{0x4e}
					resolution = &FrostResolution{Authorization: deposit.ResolutionAuthorization{ReservationID: wantReservation, TransactionHash: hash, Vault: vault}}
					wantKind, wantSent = "sweep-resolution-proof", "sweep-resolution"
				}
				if test.credited {
					wantKind = "already-credited"
				}
				var action FrostAction
				err := recovered(func() error {
					var err error
					action, err = w.m.ProveSweep(context.Background(), w.wallet, hash, vault, resolution)
					return err
				})
				if (err != nil) != test.wantErr || errors.Is(err, errFixtureRead) || errors.Is(err, errFixturePanic) {
					t.Fatalf("%s: error %v, expected error %v", test.name, err, test.wantErr)
				}
				if waits := test.name == "below the operator floor waits"; errors.Is(err, ErrSweepNotConfirmed) != waits {
					t.Fatalf("%s: error %v, expected waiting (not confirmed) %v", test.name, err, waits)
				}
				if len(w.bridge.sent) != test.submits {
					t.Fatalf("%s: %d submissions, expected %d", test.name, len(w.bridge.sent), test.submits)
				}
				for _, s := range w.bridge.sent {
					if s.kind != wantSent || s.transaction != hash || s.reservation != wantReservation ||
						s.sweep.WalletID != w.wallet || s.sweep.Vault != vault || s.sweep.IncludeMain != test.includeMain {
						t.Fatalf("%s: submitted %s of %x (reservation %x, wallet %x, vault %x, includeMain %v), expected %s of %x (reservation %x, includeMain %v)",
							test.name, s.kind, s.transaction, s.reservation, s.sweep.WalletID, s.sweep.Vault, s.sweep.IncludeMain, wantSent, hash, wantReservation, test.includeMain)
					}
				}
				if !test.wantErr && (action.Kind != wantKind || action.Transaction != hash || action.Reservation != wantReservation) {
					t.Fatalf("%s: action %q %x reservation %x, expected %q %x reservation %x", test.name, action.Kind, action.Transaction, action.Reservation, wantKind, hash, wantReservation)
				}
			})
		}
	}
}

// TestFrostSweepProofReadFaults: every failed read is an error and nothing
// is submitted.
func TestFrostSweepProofReadFaults(t *testing.T) {
	for n := 1; n < 200; n++ {
		w := newSweepWorld(t)
		hash := w.sweep(t, w.main, w.other, w.d1, w.d2)
		w.f.failAt = n
		err := recovered(func() error {
			_, err := w.m.ProveSweep(context.Background(), w.wallet, hash, [20]byte{}, nil)
			return err
		})
		if !w.f.fired {
			if n == 1 {
				t.Fatal("fixture: no read was made")
			}
			return
		}
		if err == nil {
			t.Fatalf("read %d failed, but the sweep proof reported success", n)
		}
		if errors.Is(err, ErrSweepNotConfirmed) && w.f.firedOp != "confirmations" {
			t.Fatalf("read %d failed and was reported as not confirmed: %v", n, err)
		}
		if len(w.bridge.sent) != 0 {
			t.Fatalf("read %d failed, but %d proofs were submitted", n, len(w.bridge.sent))
		}
	}
	t.Fatal("fixture: reads never ended")
}

// TestFrostSweepProofSteps: a crash point (the package's boundary seam) before
// the submission or after the credit is seen is reported; one right after
// the submission is settled by the credit.
func TestFrostSweepProofSteps(t *testing.T) {
	for _, test := range []struct {
		step    string
		wantErr bool
		submits int
	}{
		{"before-sweep-proof", true, 0},
		{"sweep-proof-submitted", false, 1},
		{"credit-seen", true, 1},
	} {
		t.Run(test.step, func(t *testing.T) {
			w := newSweepWorld(t)
			hash := w.sweep(t, w.d1)
			w.m.boundary = func(step string) error {
				if step == test.step {
					return errors.New("fixture: crash")
				}
				return nil
			}
			err := recovered(func() error {
				_, err := w.m.ProveSweep(context.Background(), w.wallet, hash, [20]byte{}, nil)
				return err
			})
			if (err != nil) != test.wantErr || errors.Is(err, errFixturePanic) {
				t.Fatalf("crash at %s: error %v, expected error %v", test.step, err, test.wantErr)
			}
			if len(w.bridge.sent) != test.submits {
				t.Fatalf("crash at %s: %d submissions, expected %d", test.step, len(w.bridge.sent), test.submits)
			}
		})
	}
}

// --- ReserveDeposit -----------------------------------------------------
//
// Invariant (KC-SF-V3 K-06/C-10; ReserveDeposit doc): a deposit is reserved
// once, with a proof of its funding at the Bridge's proof depth, only while
// the planner could still sweep it (funding unspent, before the refund
// margin); it needs funding and clock readers; a reserved or swept deposit
// is not resubmitted; a failed read, a wrong funding transaction or a
// refused submission is an error; a lost reply (or a race lost to another
// reservation or a sweep) is settled by rereading the record.

type gapsFunding struct {
	f       *faults
	unspent bool
}

func (g gapsFunding) Unspent(context.Context, bitcoin.TransactionOutpoint) (bool, error) {
	if err := g.f.read(); err != nil {
		return false, err
	}
	return g.unspent, nil
}

type gapsClock struct {
	f   *faults
	mtp uint64
}

func (g gapsClock) MedianTimePast(context.Context) (uint64, error) {
	if err := g.f.read(); err != nil {
		return 0, err
	}
	return g.mtp, nil
}

type reservationWorld struct {
	f      *faults
	bridge *fakeBridge
	btc    *proofBitcoin
	key    [32]byte
	hash   [32]byte
	m      FrostSweepMaintainer
}

const gapsMTP, gapsMargin = 1_000_000, 3_600

func newReservationWorld(t *testing.T) *reservationWorld {
	w := &reservationWorld{f: &faults{}, key: [32]byte{0xde}}
	w.bridge = newFakeBridge(w.f)
	w.btc = newProofBitcoin(t, w.f)
	w.hash = w.btc.confirm(t, gapsTx(0x0f), 6)
	w.bridge.deposits[w.key] = deposit.Record{FundingTransactionHash: w.hash, RevealedAt: 1, Terms: deposit.Terms{RefundLocktime: gapsMTP + gapsMargin + 1}}
	w.bridge.submit = func(s submission) error { w.bridge.reserved[s.reservation] = true; return nil }
	w.m = FrostSweepMaintainer{
		Chain: w.bridge, Bitcoin: w.btc, Confirmations: 2,
		Funding: gapsFunding{f: w.f, unspent: true}, Clock: gapsClock{f: w.f, mtp: gapsMTP}, RefundMargin: gapsMargin,
	}
	return w
}

func (w *reservationWorld) run() (FrostAction, error) {
	var action FrostAction
	err := recovered(func() error {
		var err error
		action, err = w.m.ReserveDeposit(context.Background(), w.key)
		return err
	})
	return action, err
}

func TestFrostDepositReservationOutcomes(t *testing.T) {
	refused := errors.New("fixture: refused")
	swept := func(w *reservationWorld) {
		r := w.bridge.deposits[w.key]
		r.SweptAt = 9
		w.bridge.deposits[w.key] = r
	}
	tests := []struct {
		name    string
		setup   func(w *reservationWorld)
		kind    string
		wantErr bool
		submits int
	}{
		{"an eligible deposit is reserved once", func(w *reservationWorld) {}, "deposit-reservation", false, 1},
		{"a reserved deposit is not resubmitted", func(w *reservationWorld) { w.bridge.reserved[w.key] = true }, "already-reserved", false, 0},
		{"a swept deposit is not resubmitted", swept, "already-swept", false, 0},
		{"zero confirmations is refused", func(w *reservationWorld) { w.m.Confirmations = 0 }, "", true, 0},
		{"no funding reader is refused", func(w *reservationWorld) { w.m.Funding = nil }, "", true, 0},
		{"no clock is refused", func(w *reservationWorld) { w.m.Clock = nil }, "", true, 0},
		{"funding below the operator floor waits", func(w *reservationWorld) { w.m.Confirmations = 7 }, "", true, 0},
		{"a spent funding output is refused", func(w *reservationWorld) { w.m.Funding = gapsFunding{f: w.f} }, "", true, 0},
		{"a deposit inside its refund margin is refused", func(w *reservationWorld) {
			w.m.Clock = gapsClock{f: w.f, mtp: gapsMTP + 1}
		}, "", true, 0},
		{"Bitcoin returning another funding transaction is refused", func(w *reservationWorld) {
			w.btc.substitute[bitcoin.Hash(w.hash)] = gapsTx(0x0e)
		}, "", true, 0},
		{"a refused submission is an error", func(w *reservationWorld) {
			w.bridge.submit = func(submission) error { return refused }
		}, "", true, 1},
		{"a lost reply is settled by the reservation", func(w *reservationWorld) {
			w.bridge.submit = func(s submission) error { w.bridge.reserved[w.key] = true; return refused }
		}, "already-reserved", false, 1},
		{"a race lost to a sweep is settled", func(w *reservationWorld) {
			w.bridge.submit = func(submission) error { swept(w); return refused }
		}, "already-swept", false, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newReservationWorld(t)
			test.setup(w)
			action, err := w.run()
			if (err != nil) != test.wantErr || errors.Is(err, errFixtureRead) || errors.Is(err, errFixturePanic) {
				t.Fatalf("%s: error %v, expected error %v", test.name, err, test.wantErr)
			}
			if waits := test.name == "funding below the operator floor waits"; errors.Is(err, ErrFundingNotConfirmed) != waits {
				t.Fatalf("%s: error %v, expected waiting (not confirmed) %v", test.name, err, waits)
			}
			if len(w.bridge.sent) != test.submits {
				t.Fatalf("%s: %d submissions, expected %d", test.name, len(w.bridge.sent), test.submits)
			}
			for _, s := range w.bridge.sent {
				if s.kind != "deposit-reservation" || s.reservation != w.key || s.transaction != w.hash {
					t.Fatalf("%s: submitted %s for %x of %x, expected a reservation for %x of %x", test.name, s.kind, s.reservation, s.transaction, w.key, w.hash)
				}
			}
			if !test.wantErr && (action.Kind != test.kind || action.Reservation != w.key || action.Transaction != w.hash) {
				t.Fatalf("%s: action %q %x %x, expected %q %x %x", test.name, action.Kind, action.Reservation, action.Transaction, test.kind, w.key, w.hash)
			}
		})
	}
}

// TestFrostDepositReservationReadFaults: every failed read (Bridge, Bitcoin,
// funding or clock) is an error and nothing is submitted.
func TestFrostDepositReservationReadFaults(t *testing.T) {
	for n := 1; n < 200; n++ {
		w := newReservationWorld(t)
		w.f.failAt = n
		_, err := w.run()
		if !w.f.fired {
			if n == 1 {
				t.Fatal("fixture: no read was made")
			}
			return
		}
		if err == nil {
			t.Fatalf("read %d failed, but the reservation reported success", n)
		}
		// Only a failed confirmations read (an unknown transaction) may be
		// reported as "not confirmed yet"; any other failed read is a
		// failure, not a wait.
		if errors.Is(err, ErrFundingNotConfirmed) && w.f.firedOp != "confirmations" {
			t.Fatalf("read %d failed and was reported as not confirmed: %v", n, err)
		}
		if len(w.bridge.sent) != 0 {
			t.Fatalf("read %d failed, but %d reservations were submitted", n, len(w.bridge.sent))
		}
	}
	t.Fatal("fixture: reads never ended")
}

// TestFrostDepositReservationSteps: a crash point before the submission is
// reported and nothing is sent.
func TestFrostDepositReservationSteps(t *testing.T) {
	w := newReservationWorld(t)
	w.m.boundary = func(step string) error {
		if step == "before-deposit-reservation" {
			return errors.New("fixture: crash")
		}
		return nil
	}
	if _, err := w.run(); err == nil || len(w.bridge.sent) != 0 {
		t.Fatalf("a crash before the reservation: error %v, %d submissions; expected an error and none", err, len(w.bridge.sent))
	}
}
