package tbtc_test

// tuxxy/keep-core#57 (K09-42): the closed K-09 reset sender held the client's
// transaction mutex while the reset was mined, and bypassed the standard
// transactor.
//
// Invariant: when the host submits the moving-funds timeout reset, other
// transactions of the same client are not blocked while the reset waits to be
// mined. Here the chain stops mining (Anvil automine off) once the reset is
// pending; another transaction of the same production client, submitted
// through the standard generated wrapper (which takes the shared transaction
// mutex), must get its answer within drainMutexBound, and the reset must
// then confirm and move the source's timeout.
//
// The state is FrostOnly, zero Live wallets, drain contained, reset due. The
// integration contracts already accept the reset there (liveWalletsCount ==
// 0), so this case does not depend on tuxxy/tbtc-v2#20.
//
// Spec: KC-SF-V3 line 474 (K-09: exercise the actual producer-to-consumer
// boundaries). Review 5671041158 (K09-42: "Use the standard submission path
// or release the mutex before waiting").
//
// Not covered: gas bumping of a slow reset, and the nonce after a failed
// mine (both need a delayed-mining EVM model; the review records the same
// limit for the K-09 tests).

import (
	"context"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

const drainMutexBound = 10 * time.Second

// TestDrainProducer_ResetSubmission: see the file comment. Refs
// tuxxy/keep-core#57, tuxxy/keep-core#51.
func TestDrainProducer_ResetSubmission(t *testing.T) {
	setup := drainSetup{mode: modeFrostOnly, live: false, route: routeContained, encoding: revertCode3}
	dw := newDrainWorld(t, setup)
	_, requestedAt, _ := dw.sourceWallet()
	dw.elapseTo(uint64(requestedAt) + uint64(drainResetDelay) + 60)

	ctx := context.Background()
	confirmed, err := dw.admin.NonceAt(ctx, dw.operator, nil)
	if err != nil {
		t.Fatal(err)
	}
	automine := func(on bool) {
		if err := dw.admin.Client().Call(nil, "evm_setAutomine", on); err != nil {
			t.Fatal(err)
		}
	}
	automine(false)
	defer automine(true)

	done := make(chan error, 1)
	go func() {
		_, _, err := tbtcpg.NewMovingFundsTask(dw.client, dw.btc).Run(dw.request())
		done <- err
	}()

	// Wait for the host's reset to be pending.
	var runErr error
	runDone := false
	deadline := time.Now().Add(2 * time.Minute)
	for {
		pending, err := dw.admin.PendingNonceAt(ctx, dw.operator)
		if err != nil {
			t.Fatal(err)
		}
		if pending > confirmed {
			break
		}
		if !runDone {
			select {
			case runErr = <-done:
				runDone = true
			default:
			}
		}
		if runDone || time.Now().After(deadline) {
			t.Fatalf("reset submission (%s): the host submitted no reset while it was due (no admissible route; window error: %v)",
				setup, runErr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Another transaction of the same client, through the standard wrapper.
	// Its arguments are invalid, so it ends at gas estimation; what matters is
	// that it gets there.
	other := make(chan error, 1)
	start := time.Now()
	go func() {
		other <- dw.client.SubmitMovingFundsCommitment(dw.source.pkh, bitcoin.UnspentTransactionOutput{
			Outpoint: &bitcoin.TransactionOutpoint{}, Value: 0}, []uint32{1}, 0, nil)
	}()
	select {
	case <-other:
		t.Logf("reset submission: another client transaction got its answer after %s while the reset was unmined", time.Since(start))
	case <-time.After(drainMutexBound):
		t.Errorf("reset submission (%s): another transaction of the same client waited more than %s while the reset was unmined: "+
			"the reset holds the client's transaction mutex while it is mined", setup, drainMutexBound)
	}

	// Mining resumes: the reset confirms and the host's window ends.
	automine(true)
	stop := dw.mineInBackground()
	defer stop()
	if !runDone {
		select {
		case runErr = <-done:
		case <-time.After(3 * time.Minute):
			t.Fatalf("reset submission (%s): the host's window did not end within 3 minutes after mining resumed", setup)
		}
	}
	waitUntil := time.Now().Add(time.Minute)
	for {
		if _, next, _ := dw.sourceWallet(); next != requestedAt {
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatalf("reset submission (%s): the pending transaction confirmed but the source's timeout did not move (window error: %v)",
				setup, runErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
