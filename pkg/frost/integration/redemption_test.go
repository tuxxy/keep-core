package integration

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

func p2wpkh(b byte) []byte { s, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{b}); return s }
func p2pkh(b byte) []byte  { s, _ := bitcoin.PayToPublicKeyHash([20]byte{b}); return s }

// TestFrostRedemptionLifecycle: funded FROST custody (real DKG key, real C-05
// sweep), two funded requests discovered from Bridge logs, independent
// attestation by every operator after a fresh-worker key reload, C-04
// admission, chain-derived executor agreement, real threshold signing, Core
// acceptance, maintainer SPV proof and settlement; SC-05 equations; replay and
// double-refund refusal.
func TestFrostRedemptionLifecycle(t *testing.T) {
	w := newK05World(t)
	ctx := w.f.ctx
	treasuryBefore := w.balance(w.f.deployment.Treasury)
	adminBefore := w.balance(w.adminAddr)
	custodyBefore, _ := w.chain.Custody(ctx, w.head(), w.walletID)
	const gross = 100_000
	ids := [][32]byte{w.request(gross, p2wpkh(0x41)), w.request(gross, p2pkh(0x42))}
	if w.balance(w.adminAddr) != adminBefore-2*gross {
		t.Fatal("request escrow not debited")
	}
	head := w.head()
	proposed, requests, err := tbtcpg.ProposeFrostRedemption(ctx, w.planner, w.fromBlock, head, 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].ID != ids[0] || requests[1].ID != ids[1] {
		t.Fatal("discovery order or set")
	}
	plan, reservation := w.admit()
	if plan.Commitment() != proposed.Commitment() {
		t.Fatal("independent planning is not deterministic")
	}
	launchesBefore := []int{w.launchCount(0), w.launchCount(1)}
	// Both operators sign concurrently (the protocol needs every selected seat).
	statuses := make([]redemption.Status, len(w.resolvers))
	errs := make([]error, len(w.resolvers))
	var wg sync.WaitGroup
	for i, r := range w.resolvers {
		wg.Add(1)
		go func(i int, r *redemption.Resolver) {
			defer wg.Done()
			statuses[i], errs[i] = r.Resolve(ctx, plan, reservation)
		}(i, r)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil || statuses[i] != redemption.StatusSigned {
			t.Fatalf("operator %d: %s %v", i, statuses[i], errs[i])
		}
		if w.launchCount(i) != launchesBefore[i]+1 {
			t.Fatal("expected exactly one real worker launch for one input")
		}
	}
	status, err := w.resolvers[0].Resolve(ctx, plan, reservation)
	if err != nil || status != redemption.StatusBroadcast {
		t.Fatal(status, err)
	}
	w.coreMine(1)
	actions, err := w.maintainer.ProveSettlements(ctx)
	if err != nil || len(actions) != 1 || actions[0].Kind != "payment-proof" {
		t.Fatalf("%+v %v", actions, err)
	}
	h := w.head()
	intent, _ := w.chain.Intent(ctx, h, reservation)
	if intent.State != redemption.IntentPaid {
		t.Fatal("intent not paid", intent.State)
	}
	// SC-05: recipient values, fee, change, escrow and liability.
	paid := w.coreTx(bitcoin.Hash(plan.BitcoinTransactionHash()))
	var treasuryFees, payouts uint64
	for i, r := range plan.Requests() {
		req, _ := w.chain.Request(ctx, h, r.ID)
		if req.State != redemption.RequestPaid || paid.Outputs[i].Value != r.Value || !bytes.Equal(paid.Outputs[i].PublicKeyScript, req.Script) {
			t.Fatal("recipient output differs from the funded request")
		}
		share := int64(req.Gross-req.TreasuryFee) - r.Value
		if share <= 0 || uint64(share) > req.MaximumBitcoinFee {
			t.Fatal("fee share outside the captured cap")
		}
		treasuryFees += req.TreasuryFee
		payouts += uint64(r.Value)
	}
	fee := uint64(plan.Snapshot().Fee())
	change := uint64(paid.Outputs[len(plan.Requests())].Value)
	if custodyBefore.Value != payouts+fee+change {
		t.Fatal("conservation: main != payouts + fee + change")
	}
	custody, _ := w.chain.Custody(ctx, h, w.walletID)
	if custody.Value != change || custody.ActiveReservation != ([32]byte{}) || custody.StateVersion != custodyBefore.StateVersion+1 || custody.Generation != 1 || custody.MainOutpoint != redemption.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: paid.Hash(), OutputIndex: uint32(len(plan.Requests()))}) {
		t.Fatalf("custody after payment %+v", custody)
	}
	escrow, err := w.bridge.FrostRedemptionEscrow(nil, w.walletID)
	if err != nil || escrow.Gross != 0 || escrow.NetLiability != 0 {
		t.Fatal("escrow or liability left after settlement", escrow, err)
	}
	if w.balance(w.f.deployment.Treasury) != treasuryBefore+treasuryFees || w.balance(w.f.deployment.Bridge) != 0 || w.balance(w.adminAddr) != adminBefore-2*gross {
		t.Fatal("Bank frame: treasury fee, Bridge escrow or redeemer balance")
	}
	// Replay and double refund reject.
	info, proof, err := maintainerProof(w, plan.BitcoinTransactionHash())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.chain.SubmitPaymentProof(ctx, reservation, info, proof); err == nil {
		t.Fatal("payment proof replay accepted")
	}
	for _, id := range ids {
		if w.chain.RefundUnreserved(ctx, id) == nil || w.chain.RefundResolved(ctx, id) == nil || w.chain.NotifyTimeout(ctx, id) == nil {
			t.Fatal("paid request refundable")
		}
	}
	if status, err = w.resolvers[1].Resolve(ctx, plan, reservation); err != nil || status != redemption.StatusSettled {
		t.Fatal(status, err)
	}
	entries, _ := w.resolvers[0].Ledger.Entries(ctx, reservation)
	kinds := map[redemption.Kind]bool{}
	for _, e := range entries {
		kinds[e.Kind] = true
	}
	if !kinds[redemption.KindSigned] || !kinds[redemption.KindBroadcast] || !kinds[redemption.KindPossibleRelease] {
		t.Fatalf("durable ledger %+v", entries)
	}
	// The next slot is reachable from the new main (generation 2, version 2).
	next := w.request(gross, p2wpkh(0x43))
	plan2, reservation2 := w.admit()
	if plan2.Generation() != 2 || plan2.StateVersion() != 2 || plan2.Requests()[0].ID != next {
		t.Fatal("next reservation slot")
	}
	for i, err := range w.signAll(plan2, reservation2) {
		if err != nil {
			t.Fatalf("second redemption signer %d: %v", i, err)
		}
	}
	if err = w.resolvers[1].Broadcast(ctx, plan2, reservation2); err != nil {
		t.Fatal(err)
	}
	w.coreMine(1)
	if actions, err = w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 {
		t.Fatalf("%+v %v", actions, err)
	}
	t.Logf("lifecycle: reservation=%x tx=%s fee=%d payouts=%d change=%d; second slot reservation=%x paid", reservation, paid.Hash().Hex(bitcoin.ReversedByteOrder), fee, payouts, change, reservation2)
}

func maintainerProof(w *k05World, hash [32]byte) (redemption.BitcoinInfo, redemption.BitcoinProof, error) {
	tx, proof, err := bitcoin.AssembleSpvProof(bitcoin.Hash(hash), k05Confirmations, w.btc)
	if err != nil {
		return redemption.BitcoinInfo{}, redemption.BitcoinProof{}, err
	}
	return redemption.BitcoinInfo{Version: tx.SerializeVersion(), InputVector: tx.SerializeInputs(), OutputVector: tx.SerializeOutputs(), Locktime: tx.SerializeLocktime()}, redemption.BitcoinProof{MerkleProof: proof.MerkleProof, TxIndexInBlock: uint64(proof.TxIndexInBlock), BitcoinHeaders: proof.BitcoinHeaders, CoinbasePreimage: proof.CoinbasePreimage, CoinbaseProof: proof.CoinbaseProof}, nil
}

func (w *k05World) mainUnspent() bool {
	w.t.Helper()
	custody, _ := w.chain.Custody(w.f.ctx, w.head(), w.walletID)
	script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	outs, err := w.btc.UnspentByScript(w.f.ctx, script)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, o := range outs {
		if redemption.OutpointKey(o.Outpoint) == custody.MainOutpoint {
			return true
		}
	}
	return false
}

func (w *k05World) requireRefundRefused(ids ...[32]byte) {
	w.t.Helper()
	for _, id := range ids {
		if w.chain.RefundUnreserved(w.f.ctx, id) == nil || w.chain.RefundResolved(w.f.ctx, id) == nil {
			w.t.Fatal("reserved request refunded without a payment or cancellation proof")
		}
	}
}

func (w *k05World) requireRefunded(adminBefore uint64, gross uint64, ids ...[32]byte) {
	w.t.Helper()
	for _, id := range ids {
		r, _ := w.chain.Request(w.f.ctx, w.head(), id)
		if r.State != redemption.RequestRefunded {
			w.t.Fatal("request not refunded", r.State)
		}
	}
	if got := w.balance(w.adminAddr); got != adminBefore+gross*uint64(len(ids)) {
		w.t.Fatalf("refund amount: got %d want %d", got, adminBefore+gross*uint64(len(ids)))
	}
}

// TestReservationTimeoutRace exercises the timeout before reservation, after
// reservation, after share release, after signing (rebroadcast policy) and
// after broadcast, on one funded wallet.
func TestReservationTimeoutRace(t *testing.T) {
	w := newK05World(t)
	ctx := w.f.ctx
	const gross = 50_000
	t.Run("before reservation", func(t *testing.T) {
		id := w.request(gross, p2wpkh(0x51))
		before := w.balance(w.adminAddr)
		w.warp(k05Timeout + 1)
		if _, _, err := w.proposal(w.head()); err == nil {
			t.Fatal("timed-out request planned")
		}
		actions, err := w.maintainer.ProcessRequests(ctx)
		if err != nil || len(actions) != 1 || actions[0].Kind != "timeout" {
			t.Fatalf("%+v %v", actions, err)
		}
		if w.chain.RefundUnreserved(ctx, id) == nil {
			t.Fatal("unreserved refund before its finality boundary")
		}
		w.mine(k05Finality)
		if actions, err = w.maintainer.ProcessRequests(ctx); err != nil || len(actions) != 1 || actions[0].Kind != "unreserved-refund" {
			t.Fatalf("%+v %v", actions, err)
		}
		w.requireRefunded(before, gross, id)
	})
	t.Run("after reservation before signing", func(t *testing.T) {
		id := w.request(gross, p2wpkh(0x52))
		payment, reservation := w.admit()
		w.warp(k05Timeout + 1)
		if actions, err := w.maintainer.ProcessRequests(ctx); err != nil || len(actions) != 1 || actions[0].Kind != "timeout" {
			t.Fatalf("%+v %v", actions, err)
		}
		launches := w.launchCount(0)
		if _, err := w.resolvers[0].Sign(ctx, payment, reservation); !errors.Is(err, signing.ErrUnauthorized) {
			t.Fatal("signing granted after timeout:", err)
		}
		if w.launchCount(0) != launches {
			t.Fatal("worker launched for a timed-out reservation")
		}
		for _, r := range w.resolvers {
			if s, err := r.Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusNeedsCancellation {
				t.Fatal(s, err)
			}
		}
		w.requireRefundRefused(id)
		before := w.balance(w.adminAddr)
		w.cancel(payment, reservation, 1000)
		intent, _ := w.chain.Intent(ctx, w.head(), reservation)
		if intent.State != redemption.IntentCancelled {
			t.Fatal("not cancelled")
		}
		// The sponsor already paid the 1,000 sat budget before this baseline.
		w.requireRefunded(before-1000, gross, id)
		if !w.mainUnspent() {
			t.Fatal("cancellation output is not the new recognized main")
		}
	})
	t.Run("after share release", func(t *testing.T) {
		id := w.request(gross, p2wpkh(0x53))
		payment, reservation := w.admit()
		w.warp(k05Timeout + 1)
		timeoutAfterRelease(w, id)
		errs := w.signAll(payment, reservation)
		for _, j := range w.journals {
			j.setOnDone(nil)
		}
		for i, err := range errs {
			if !errors.Is(err, signing.ErrLateSignature) {
				t.Fatalf("operator %d: expected late-signature quarantine, got %v", i, err)
			}
		}
		if _, err := w.core.Transaction(ctx, bitcoin.Hash(payment.BitcoinTransactionHash())); err == nil {
			t.Fatal("late-signed payment reached Bitcoin")
		}
		w.requireRefundRefused(id)
		before := w.balance(w.adminAddr)
		w.cancel(payment, reservation, 1000)
		w.requireRefunded(before-1000, gross, id)
	})
	t.Run("after signing, rebroadcast policy", func(t *testing.T) {
		id := w.request(gross, p2wpkh(0x54))
		payment, reservation := w.admit()
		for i, err := range w.signAll(payment, reservation) {
			if err != nil {
				t.Fatalf("signer %d: %v", i, err)
			}
		}
		w.warp(k05Timeout + 1)
		if actions, err := w.maintainer.ProcessRequests(ctx); err != nil || len(actions) != 1 {
			t.Fatalf("%+v %v", actions, err)
		}
		w.resolvers[1].Policy = redemption.AlwaysCancel
		defer func() { w.resolvers[1].Policy = redemption.RebroadcastThenCancel }()
		if s, err := w.resolvers[1].Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusNeedsCancellation {
			t.Fatal("AlwaysCancel:", s, err)
		}
		if _, err := w.core.Transaction(ctx, bitcoin.Hash(payment.BitcoinTransactionHash())); err == nil {
			t.Fatal("AlwaysCancel broadcast the payment")
		}
		if err := w.resolvers[0].Broadcast(ctx, payment, reservation); !errors.Is(err, signing.ErrUnauthorized) {
			t.Fatal("ordinary broadcast after timeout:", err)
		}
		if s, err := w.resolvers[0].Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusBroadcast {
			t.Fatal("RebroadcastThenCancel:", s, err)
		}
		w.requireRefundRefused(id)
		w.coreMine(1)
		if actions, err := w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 || actions[0].Kind != "payment-proof" {
			t.Fatalf("%+v %v", actions, err)
		}
		r, _ := w.chain.Request(ctx, w.head(), id)
		if r.State != redemption.RequestPaid {
			t.Fatal("historical payment did not settle in TimeoutPending")
		}
		w.requireRefundRefused(id)
	})
	t.Run("after broadcast", func(t *testing.T) {
		id := w.request(gross, p2wpkh(0x55))
		payment, reservation := w.admit()
		for i, err := range w.signAll(payment, reservation) {
			if err != nil {
				t.Fatalf("signer %d: %v", i, err)
			}
		}
		if err := w.resolvers[0].Broadcast(ctx, payment, reservation); err != nil {
			t.Fatal(err)
		}
		w.warp(k05Timeout + 1)
		if actions, err := w.maintainer.ProcessRequests(ctx); err != nil || len(actions) != 1 {
			t.Fatalf("%+v %v", actions, err)
		}
		w.requireRefundRefused(id)
		w.coreMine(1)
		if actions, err := w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 || actions[0].Kind != "payment-proof" {
			t.Fatalf("%+v %v", actions, err)
		}
		w.requireRefundRefused(id)
		if _, _, err := w.attesters[0].AttestCancellation(ctx, payment, mustCancellation(t, w, payment), reservation, w.head()+10); err == nil {
			t.Fatal("cancellation attested after payment settled")
		}
	})
}

func mustCancellation(t *testing.T, w *k05World, payment *signing.Plan) *signing.Plan {
	unsigned := payment.Snapshot().Unsigned()
	var total int64
	for _, p := range payment.Snapshot().PreviousOutputs() {
		total += p.Value
	}
	script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	plan, err := signing.NewCancellationPlan(payment, &bitcoin.Transaction{Version: 2, Inputs: unsigned.Inputs, Outputs: []*bitcoin.TransactionOutput{{Value: total - 1000, PublicKeyScript: script}}})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// timeoutAfterRelease makes the C-04 timeout land exactly after every
// operator's worker returned its signature share result (the Done record is
// durable) and before any operator re-reads the reservation to accept it.
func timeoutAfterRelease(w *k05World, id [32]byte) {
	var mu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	for _, j := range w.journals {
		j.setOnDone(func([]byte) {
			mu.Lock()
			arrived++
			last := arrived == len(w.journals)
			mu.Unlock()
			if last {
				if err := w.chain.NotifyTimeout(w.f.ctx, id); err != nil {
					w.t.Error("timeout notice:", err)
				}
				close(release)
			}
			<-release
		})
	}
}

// TestLateSignatureQuarantine: a signature completed after the reservation
// left Reserved is durably quarantined, is never broadcast or rebroadcast,
// cannot be recovered into a result, cannot start a second release, and the
// entitlement resolves only by a proven conflicting cancellation.
func TestLateSignatureQuarantine(t *testing.T) {
	w := newK05World(t)
	ctx := w.f.ctx
	const gross = 60_000
	id := w.request(gross, p2wpkh(0x61))
	payment, reservation := w.admit()
	w.warp(k05Timeout + 1)
	timeoutAfterRelease(w, id)
	errs := w.signAll(payment, reservation)
	for _, j := range w.journals {
		j.setOnDone(nil)
	}
	for i, err := range errs {
		if !errors.Is(err, signing.ErrLateSignature) {
			t.Fatalf("operator %d: %v", i, err)
		}
	}
	for i, r := range w.resolvers {
		// A fresh ledger view over the same durable store sees the quarantine.
		fresh := redemption.Ledger{Journal: w.journals[i]}
		late, err := fresh.Has(ctx, reservation, redemption.KindLateQuarantined, payment.BitcoinTransactionHash())
		if err != nil || !late {
			t.Fatal("late signature not durably quarantined", err)
		}
		released, err := fresh.Has(ctx, reservation, redemption.KindPossibleRelease, payment.BitcoinTransactionHash())
		if err != nil || !released {
			t.Fatal("possible release not recorded", err)
		}
		if err = r.Broadcast(ctx, payment, reservation); err == nil {
			t.Fatal("quarantined signature broadcast")
		}
		launches := w.launchCount(i)
		if _, err = r.Sign(ctx, payment, reservation); !errors.Is(err, signing.ErrUnauthorized) {
			t.Fatal("second release granted:", err)
		}
		if w.launchCount(i) != launches {
			t.Fatal("second release launched a worker")
		}
		if s, err := r.Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusNeedsCancellation {
			t.Fatal(s, err)
		}
	}
	// Even with the rebroadcast policy, no durable accepted result exists.
	if _, err := w.core.Transaction(ctx, bitcoin.Hash(payment.BitcoinTransactionHash())); err == nil {
		t.Fatal("quarantined payment reached Bitcoin")
	}
	w.requireRefundRefused(id)
	before := w.balance(w.adminAddr)
	w.cancel(payment, reservation, 1000)
	w.requireRefunded(before-1000, gross, id)
	for _, r := range w.resolvers {
		if s, err := r.Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusSettled {
			t.Fatal(s, err)
		}
	}
	// The payment can no longer be proven: its conflict input is spent.
	if _, _, err := maintainerProof(w, payment.BitcoinTransactionHash()); err == nil {
		t.Fatal("quarantined payment provable")
	}
}

// Review P2: the executor stores the accepted result before the resolver's
// signed marker. Losing that marker (crash or I/O error) must not hide the
// result: Resolve and Broadcast reconcile it from the recorded attempt round,
// both while Reserved and after a timeout under RebroadcastThenCancel.
func TestSignedResultRecoveredWithoutLedgerMarker(t *testing.T) {
	w := newK05World(t)
	ctx := w.f.ctx
	failSigned := func(on bool) {
		for _, j := range w.journals {
			j.mu.Lock()
			if on {
				j.failRecord = func(raw []byte) error {
					if bytes.Contains(raw, []byte(`"Kind":"signed"`)) {
						return errors.New("injected I/O error at the signed marker")
					}
					return nil
				}
			} else {
				j.failRecord = nil
			}
			j.mu.Unlock()
		}
	}
	for _, timeout := range []bool{false, true} {
		id := w.request(50_000, p2wpkh(0x81))
		plan, reservation := w.admit()
		failSigned(true)
		for i, err := range w.signAll(plan, reservation) {
			if err == nil {
				t.Fatalf("operator %d: marker failure not injected", i)
			}
		}
		failSigned(false)
		for _, r := range w.resolvers {
			if ok, _ := r.Ledger.Has(ctx, reservation, redemption.KindSigned, [32]byte{}); ok {
				t.Fatal("signed marker unexpectedly present")
			}
		}
		if timeout {
			w.warp(k05Timeout + 1)
			if actions, err := w.maintainer.ProcessRequests(ctx); err != nil || len(actions) != 1 {
				t.Fatalf("%+v %v", actions, err)
			}
		}
		launches := w.launchCount(0)
		status, err := w.resolvers[0].Resolve(ctx, plan, reservation)
		if err != nil || status != redemption.StatusBroadcast {
			t.Fatalf("timeout=%v: accepted result not recovered: %s %v", timeout, status, err)
		}
		if w.launchCount(0) != launches {
			t.Fatal("recovery started a worker")
		}
		if ok, _ := w.resolvers[0].Ledger.Has(ctx, reservation, redemption.KindSigned, plan.BitcoinTransactionHash()); !ok {
			t.Fatal("signed marker not reconciled")
		}
		w.coreMine(1)
		if actions, err := w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 || actions[0].Kind != "payment-proof" {
			t.Fatalf("%+v %v", actions, err)
		}
		r, _ := w.chain.Request(ctx, w.head(), id)
		if r.State != redemption.RequestPaid {
			t.Fatal("recovered payment not settled")
		}
	}
}

// Review P2 (re-review): with ordinary ledger space exhausted on every
// operator, the complete recovery path still runs on the reserve through
// the real flows: timeout resolution, cancellation attestation, cancellation
// sign attempt, signing, broadcast, proof and refund. A further ordinary
// payment attempt record is refused. Capacity is scaled down so the boundary
// is reached with few real store writes; the rule is the production rule.
func TestLedgerRecoveryHeadroom(t *testing.T) {
	w := newK05World(t)
	ctx := w.f.ctx
	const gross = 50_000
	id := w.request(gross, p2wpkh(0x91))
	payment, reservation := w.admit()
	w.warp(k05Timeout + 1)
	if actions, err := w.maintainer.ProcessRequests(ctx); err != nil || len(actions) != 1 {
		t.Fatalf("%+v %v", actions, err)
	}
	const reserve = 16
	for i := range w.resolvers {
		// Prior ordinary history (payment attempt records of earlier rounds).
		for round := 0; round < 5; round++ {
			if err := w.resolvers[i].Ledger.Append(ctx, redemption.Entry{Reservation: reservation, Kind: redemption.KindSignAttempt, Transaction: payment.BitcoinTransactionHash(), Detail: fmt.Sprint(100 + round)}); err != nil {
				t.Fatal(err)
			}
		}
		entries, err := w.resolvers[i].Ledger.Entries(ctx, reservation)
		if err != nil {
			t.Fatal(err)
		}
		l := redemption.Ledger{Journal: w.journals[i], Capacity: len(entries) + reserve, Reserve: reserve}
		w.resolvers[i].Ledger = l
		w.attesters[i].Ledger = l
		if err = l.Append(ctx, redemption.Entry{Reservation: reservation, Kind: redemption.KindSignAttempt, Transaction: payment.BitcoinTransactionHash(), Detail: "999"}); err == nil {
			t.Fatal("ordinary attempt record consumed the recovery reserve")
		}
	}
	for _, r := range w.resolvers {
		if s, err := r.Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusNeedsCancellation {
			t.Fatal("timeout resolution on the reserve:", s, err)
		}
	}
	before := w.balance(w.adminAddr)
	w.cancel(payment, reservation, 1000)
	w.requireRefunded(before-1000, gross, id)
	for i, r := range w.resolvers {
		if s, err := r.Resolve(ctx, payment, reservation); err != nil || s != redemption.StatusSettled {
			t.Fatalf("operator %d outcome on the reserve: %s %v", i, s, err)
		}
		entries, _ := r.Ledger.Entries(ctx, reservation)
		capacity := r.Ledger.Capacity
		if len(entries) > capacity {
			t.Fatal("ledger exceeded capacity")
		}
		t.Logf("operator %d: %d of %d records after recovery (reserve %d)", i, len(entries), capacity, reserve)
	}
}
