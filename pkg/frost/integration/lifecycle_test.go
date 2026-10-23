package integration

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/enforcement"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// TestFrostCompleteLocalLifecycle joins the component boundaries on one
// deployment and two real DKG wallets. No custody or wallet-state storage is
// injected. The harness explicitly dispatches each host component; it is not
// a production daemon scheduler. Separate cases cover operator-free refund,
// reservation timeout, unauthorized fraud timeout and public legacy upgrade.
func TestFrostCompleteLocalLifecycle(t *testing.T) {
	w := newK07World(t)
	e := attachK08World(t, w.k06World)
	ctx := w.f.ctx
	if w.a.id == w.b.id || w.state(w.a.id) != movement.StateReadyUnfunded || w.state(w.b.id) != movement.StateReadyUnfunded {
		t.Fatal("k09-ready: two distinct ready, unfunded wallets required")
	}
	for _, h := range []*walletHost{w.a, w.b} {
		for i, j := range h.journals {
			if _, err := j.LoadKey(ctx); err != nil {
				t.Fatalf("k09-ready: wallet %x operator %d: %v", h.id, i, err)
			}
		}
	}
	// D-09 OPEN local value: closure can mature while the public fraud
	// challenge still has time for a restarted defense.
	localParameters, err := w.legacy.WalletParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateWalletParameters(o, localParameters.WalletCreationPeriod, localParameters.WalletCreationMinBtcBalance, localParameters.WalletCreationMaxBtcBalance, localParameters.WalletClosureMinBtcBalance, localParameters.WalletMaxAge, localParameters.WalletMaxBtcTransfer, 1)
	})
	t.Log("k09-ready: two wallets, durable keys on both real operators")

	// The first main comes only from a Core-funded deposit and real sweep.
	w.fundBySweep()
	if w.state(w.a.id) != movement.StateLive || w.custody(w.a.id).Value == 0 {
		t.Fatal("k09-credit: sweep did not activate funded custody")
	}
	t.Log("k09-credit: deposit -> signed sweep -> SPV -> Bank credit")

	// The reservation must consume that sweep's main, and the payment must
	// become the main used by the later movement.
	w.request(100_000, p2wpkh(0x69))
	payment, reservation := w.admit()
	statuses := make([]redemption.Status, len(w.resolvers))
	errs := make([]error, len(w.resolvers))
	var wg sync.WaitGroup
	for i, resolver := range w.resolvers {
		wg.Add(1)
		go func(i int, resolver *redemption.Resolver) {
			defer wg.Done()
			statuses[i], errs[i] = resolver.Resolve(ctx, payment, reservation)
		}(i, resolver)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil || statuses[i] != redemption.StatusSigned {
			t.Fatalf("k09-payment: signer %d: %s %v", i, statuses[i], errs[i])
		}
	}
	if status, err := w.resolvers[0].Resolve(ctx, payment, reservation); err != nil || status != redemption.StatusBroadcast {
		t.Fatal("k09-payment: broadcast:", status, err)
	}
	w.coreMine(1)
	if actions, err := w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 {
		t.Fatalf("k09-payment: proof: %+v %v", actions, err)
	}
	w.mine(k06Finality)
	for _, lock := range w.locks {
		if err := lock.Close(ctx, w.chain, w.head()-k06Finality, w.a.id, payment.ConflictInput(), "payment proven"); err != nil {
			t.Fatal("k09-payment: close paid main lock:", err)
		}
	}
	mainAfterPayment := w.custody(w.a.id)
	t.Logf("k09-payment: settled %x, change is next custody", payment.BitcoinTransactionHash())

	// Complete one signed heartbeat before the absence window. Its success
	// must not count toward inactivity.
	round := e.nextHeartbeat(0)
	outcomes, heartbeatErrors := e.heartbeat(round, 0, 1)
	for i := range heartbeatErrors {
		if heartbeatErrors[i] != nil || !outcomes[i].Signed {
			t.Fatalf("k09-heartbeat: operator %d: %+v %v", i, outcomes[i], heartbeatErrors[i])
		}
	}
	t.Log("k09-heartbeat: guarded heartbeat on both operators")
	anchor := round.Anchor
	for n := 0; n < k08Failures; n++ {
		round = e.nextHeartbeat(anchor)
		anchor = round.Anchor
		outcomes, heartbeatErrors = e.heartbeat(round, 0)
		if heartbeatErrors[0] == nil || outcomes[0].Signed || !reflect.DeepEqual(outcomes[0].Present, []uint16{1, 2}) {
			t.Fatalf("k09-inactivity: round %d: %+v %v", n, outcomes[0], heartbeatErrors[0])
		}
	}
	claim, _, err := e.claimers[0].Derive(ctx)
	if err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || !claim.HeartbeatFailed {
		t.Fatalf("k09-inactivity: %+v %v", claim, err)
	}
	signatures, err := e.claimers[0].Sign(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	seats, packed, err := redemption.Collect(w.wallet.Threshold, signatures)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.claimers[0].Submit(ctx, claim, seats, packed); err != nil || w.state(w.a.id) != movement.StateMovingFunds {
		t.Fatal("k09-inactivity: claim must start movement:", err)
	}
	t.Log("k09-inactivity: authenticated seat 3 absence -> threshold claim -> MovingFunds")

	// Cut over DKG and contain new exposure before the existing movement.
	// These exits must keep working with both gates closed.
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) { return w.bridge.BeginLegacyDkgDrain(o) })
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.CompleteFrostCutover(o, crypto.Keccak256Hash([]byte("K-09 local cutover; OPEN")))
	})
	mode, err := w.bridge.FrostCreationMode(&bind.CallOpts{Context: ctx})
	if err != nil || mode != 2 {
		t.Fatal("k09-cutover: creation mode must be FrostOnly", mode, err)
	}
	cutoverCustody := w.custody(w.a.id)
	cutoverOpen := w.obligations(w.a.id)
	cutoverCredit := w.balance(w.adminAddr)
	if _, err = w.trySend(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.RequestNewWallet(o, legacyabi.BitcoinTxUTXO{})
	}); err == nil {
		t.Fatal("k09-cutover: new legacy DKG request was accepted")
	}
	afterMode, err := w.bridge.FrostCreationMode(&bind.CallOpts{Context: ctx})
	if err != nil || afterMode != mode || !reflect.DeepEqual(cutoverCustody, w.custody(w.a.id)) || cutoverOpen != w.obligations(w.a.id) || cutoverCredit != w.balance(w.adminAddr) {
		t.Fatal("k09-cutover: rejected legacy creation changed mode, custody, obligations or credit", err)
	}
	controller := bind.NewBoundContract(w.f.deployment.Exposure, c10ABI, w.f.client, w.f.client, w.f.client)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return controller.Transact(o, "contain", crypto.Keccak256Hash([]byte("K-09 local containment; OPEN")))
	})
	if targets := w.commit(w.a); len(targets) != 1 || targets[0] != w.b.label() {
		t.Fatalf("k09-movement: targets %x", targets)
	}
	move := w.movementPlan(w.a)
	if move.MovementMain() == nil || movement.OutpointKey(move.MovementMain().Outpoint) != mainAfterPayment.MainOutpoint {
		t.Fatal("k09-movement: did not consume the settled redemption's change")
	}
	w.settle(w.a, move, w.proofs.ProveMovement)
	w.mine(k06Finality)
	sweepB := w.incomingSweep(w.b)
	w.settle(w.b, sweepB, w.proofs.ProveIncomingSweep)
	remaining := w.custody(w.b.id).Value
	if remaining+uint64(move.Snapshot().Fee())+uint64(sweepB.Snapshot().Fee()) != mainAfterPayment.Value || w.state(w.b.id) != movement.StateLive {
		t.Fatal("k09-movement: value conservation or receiving wallet state")
	}
	t.Log("k09-movement: payment change -> B commitment -> movement -> incoming sweep under containment/cutover")

	// Challenge the actual movement signature. The complete relevant frame
	// must survive a refused close. Restart a store and defend from its
	// retained record, without rebuilding any transaction or recovery state.
	mined := w.coreTx(bitcoin.Hash(move.BitcoinTransactionHash()))
	digest := move.Snapshot().Digests()[0]
	if err = e.admit(digest, mined.Inputs[0].Witness[0]); err != nil {
		t.Fatal("k09-restarted-defense: public challenge:", err)
	}
	params, err := w.legacy.WalletParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	w.warp(uint64(params.WalletClosingPeriod) + 1)
	w.mine(k06Finality)
	beforeA, beforeB := w.custody(w.a.id), w.custody(w.b.id)
	beforeOpen, beforeState := w.obligations(w.a.id), w.state(w.a.id)
	credit, treasury := w.balance(w.adminAddr), w.balance(w.f.deployment.Treasury)
	if err = w.a.closures[0].Close(ctx, w.a.id); !errors.Is(err, movement.ErrObligations) {
		t.Fatal("k09-closure: close with public challenge:", err)
	}
	if !reflect.DeepEqual(beforeA, w.custody(w.a.id)) || !reflect.DeepEqual(beforeB, w.custody(w.b.id)) || beforeOpen != w.obligations(w.a.id) || beforeState != w.state(w.a.id) || credit != w.balance(w.adminAddr) || treasury != w.balance(w.f.deployment.Treasury) {
		t.Fatal("k09-closure: rejected closure changed custody, obligations, state or credits")
	}
	records := e.restart(1)
	defender := &enforcement.Defender{Chain: e.own[1], Wallet: w.a.id, Records: records, From: w.fromBlock}
	challenge, err := w.chain.Challenge(ctx, w.a.id, digest)
	if err != nil {
		t.Fatal(err)
	}
	timeout, err := w.chain.FraudDefeatTimeout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if actions, err := defender.Defend(ctx); err != nil || kinds(actions)[digest] != enforcement.ActionDefended || !e.resolved(digest) {
		t.Fatalf("k09-restarted-defense: %+v %v", actions, err)
	}
	defendedAt, err := w.chain.Timestamp(ctx, w.head())
	if err != nil || defendedAt >= challenge.ReportedAt+timeout {
		t.Fatal("k09-restarted-defense: missed the public challenge deadline", err)
	}
	w.mine(k06Finality)
	if w.obligations(w.a.id) != 0 {
		t.Fatal("k09-closure: obligations remain after defense")
	}
	if err = w.a.closures[0].Close(ctx, w.a.id); err != nil || w.state(w.a.id) != movement.StateClosed {
		t.Fatal("k09-closure: close after defense:", err)
	}
	for _, h := range []*walletHost{w.a, w.b} {
		for i := range w.f.nodes {
			j, err := w.f.roots[i].Scope(h.journals[i].Domain())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = j.LoadKey(ctx); err != nil {
				t.Fatalf("k09-closure: wallet %x operator %d key: %v", h.id, i, err)
			}
		}
	}
	t.Logf("k09-closure: A closed after restarted defense; B retains %d sats; both keys retained", remaining)
}
