package signing

import (
	"context"
	"errors"
	"testing"
)

// flakyReservations fails exactly one ReadReservation call (the failAt-th)
// with a provider error and otherwise answers from the stub. It is a test
// fixture: the provider is unreachable once, the reservation never changes.
type flakyReservations struct {
	inner  *reservationStub
	calls  int
	failAt int
}

func (f *flakyReservations) ReadReservation(ctx context.Context, id [32]byte) (ReservationView, error) {
	f.calls++
	if f.calls == f.failAt {
		return ReservationView{}, errors.New("injected provider error: connection reset")
	}
	return f.inner.ReadReservation(ctx, id)
}

// Invariant (KC-VER P-K05-LATE-SIGNATURE-QUARANTINE, adversarial review
// ADV-signing S1): only a signature completed after its reservation left
// Reserved (or changed) is a late signature. When the reservation cannot be
// read after the signature, nothing shows it changed: the executor must not
// report ErrLateSignature, because the K-05 resolver and K-06 sweeper turn
// that error into a permanent quarantine marker (StatusNeedsCancellation /
// StatusQuarantined) for a reservation that is still Reserved. The signature
// is still retained and no result is accepted.
func TestReservationReadErrorAfterSignatureIsNotLate(t *testing.T) {
	e, r, b, _ := executorFixture(t)
	// Reads: 1 claim-time check in Run, 2 grant check in BeforeSigning,
	// 3 the acceptance check after the signature returned.
	flaky := &flakyReservations{inner: r, failAt: 3}
	e.config.Reservations = flaky
	result, err := e.Run(context.Background())
	if err == nil || result != nil {
		t.Fatalf("a signature whose acceptance check could not read the reservation was accepted: %v", err)
	}
	if errors.Is(err, ErrLateSignature) {
		t.Fatalf("ADV-signing S1: a provider read error after the signature was reported as a late signature (reservation still Reserved): %v", err)
	}
	if flaky.calls != 3 {
		t.Fatalf("expected the failure at the post-signature read, reads: %d", flaky.calls)
	}
	// The released signature stays retained (a possible release).
	if released, err := e.PossibleRelease(context.Background()); err != nil || !released {
		t.Fatal("released signature not retained", released, err)
	}
	// Nothing was accepted or broadcast.
	if _, err := e.Result(context.Background()); err == nil {
		t.Fatal("result accepted without a successful reservation check")
	}
	if len(b.sent) != 0 {
		t.Fatal("broadcast without a successful reservation check")
	}
}

// Guard for the same rule: a reservation that was read and is no longer
// Reserved after the signature is still a late signature.
func TestReservationChangedAfterSignatureIsStillLate(t *testing.T) {
	e, r, _, _ := executorFixture(t)
	e.boundary = func(name string) error {
		if name == "after-signature" {
			r.view.State = "TimeoutPending"
		}
		return nil
	}
	if _, err := e.Run(context.Background()); !errors.Is(err, ErrLateSignature) {
		t.Fatalf("a signature completed after the reservation timed out was not late: %v", err)
	}
}
