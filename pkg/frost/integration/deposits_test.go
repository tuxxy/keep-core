package integration

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

// expectedCredit is C-05's per-deposit credit for plan: amount minus treasury
// fee minus the fee share (fee / n, remainder on the last deposit).
func expectedCredit(plan *signing.Plan) (credit, treasury uint64) {
	deposits := plan.SweepDeposits()
	fee := uint64(plan.Snapshot().Fee())
	n := uint64(len(deposits))
	for i, d := range deposits {
		share := fee / n
		if i == len(deposits)-1 {
			share += fee % n
		}
		credit += uint64(d.Prevout.Value) - d.TreasuryFee - share
		treasury += d.TreasuryFee
	}
	return credit, treasury
}

// sweepAndProve signs plan on every operator, broadcasts it, mines it and
// proves it through maintainer, and checks the credit and treasury once.
func (w *k06World) sweepAndProve(plan *signing.Plan, number uint64, maintainer spv.FrostSweepMaintainer) {
	t, ctx := w.t, w.f.ctx
	t.Helper()
	launches := []int{w.launchCount(0), w.launchCount(1)}
	for i, err := range w.signSweep(plan, number) {
		if err != nil {
			t.Fatalf("sweep signer %d: %v", i, err)
		}
		if w.launchCount(i) != launches[i]+len(plan.Snapshot().Digests()) {
			t.Fatal("expected one real worker launch per sweep input")
		}
	}
	if err := w.sweepers[1].Broadcast(ctx, plan); err != nil {
		t.Fatal("sweep broadcast:", err)
	}
	w.coreMine(1)
	adminBefore, treasuryBefore := w.balance(w.adminAddr), w.balance(w.f.deployment.Treasury)
	action, err := maintainer.ProveSweep(ctx, w.walletID, plan.BitcoinTransactionHash(), plan.SweepVault(), nil)
	if err != nil || action.Kind != "sweep-proof" {
		t.Fatalf("sweep proof: %+v %v", action, err)
	}
	credit, treasury := expectedCredit(plan)
	if w.balance(w.adminAddr) != adminBefore+credit || w.balance(w.f.deployment.Treasury) != treasuryBefore+treasury {
		t.Fatal("C-05 credit or treasury fee differs from the plan's accounting")
	}
	// P-K06-CREDIT-ONCE: a second proof of the same sweep is not submitted.
	again, err := maintainer.ProveSweep(ctx, w.walletID, plan.BitcoinTransactionHash(), plan.SweepVault(), nil)
	if err != nil || again.Kind != "already-credited" || w.balance(w.adminAddr) != adminBefore+credit {
		t.Fatalf("second proof: %+v %v", again, err)
	}
	custody, err := w.chain.Custody(ctx, w.head(), w.walletID)
	swept := deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: plan.BitcoinTransactionHash()})
	if err != nil || (custody.ActiveReservation == ([32]byte{}) && custody.MainOutpoint != swept) {
		t.Fatal("an unreserved sweep output must be the new main", err)
	}
	if out, err := w.chain.CustodyOutput(ctx, w.head(), swept); err != nil || out.WalletID != w.walletID || out.Spent {
		t.Fatal("sweep output not recognized as custody", err)
	}
}

// TestFrostDepositSweepAndCredit: Go-built deposits equal C-05's constructions;
// real funding, reveal, independent identical planning on every operator,
// authorized threshold BIP342 sweep by the real worker, Core acceptance, SPV
// proof and C-05 credit exactly once. Then a K-05 payment moves the main, and a
// second sweep spends the new main; its proof reply is lost and the
// maintainer still credits once without resubmitting.
func TestFrostDepositSweepAndCredit(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	locktime := w.locktime(k06RefundDelay)
	a := w.deposit(1_000_000, locktime, [20]byte{})
	b := w.deposit(2_000_000, locktime, [20]byte{})
	r := w.round(b.revealBlock + k06Finality)
	plan := w.plan(r)
	if plan.SweepMain() != nil || len(plan.SweepDeposits()) != 2 {
		t.Fatal("first sweep of an unfunded wallet spends both deposits and no main")
	}
	keys := map[[32]byte]bool{a.key: true, b.key: true}
	for _, d := range plan.SweepDeposits() {
		if !keys[d.Key] {
			t.Fatal("sweep plan names an unexpected deposit")
		}
	}
	w.sweepAndProve(plan, r.Number, w.sweeps)
	for i, s := range w.sweepers {
		if status, err := s.Resolve(ctx, plan); err != nil || status != deposit.StatusSwept {
			w.mine(k06Finality)
			if status, err = s.Resolve(ctx, plan); err != nil || status != deposit.StatusSwept {
				t.Fatalf("operator %d: %s %v", i, status, err)
			}
		}
	}

	// A K-05 payment spends the swept main; its change is the new main.
	w.request(100_000, p2wpkh(0x61))
	payment, reservation := w.admit()
	for i, l := range w.locks {
		current, err := l.Current(ctx, w.walletID, deposit.OutpointKey(payment.ConflictInput()))
		if err != nil || current.Purpose != deposit.Redemption || current.Subject != reservation {
			t.Fatalf("operator %d attestation did not take the main lock: %+v %v", i, current, err)
		}
	}
	statuses := make([]redemption.Status, len(w.resolvers))
	errs := make([]error, len(w.resolvers))
	var wg sync.WaitGroup
	for i, res := range w.resolvers {
		wg.Add(1)
		go func(i int, res *redemption.Resolver) {
			defer wg.Done()
			statuses[i], errs[i] = res.Resolve(ctx, payment, reservation)
		}(i, res)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil || statuses[i] != redemption.StatusSigned {
			t.Fatalf("payment operator %d: %s %v", i, statuses[i], errs[i])
		}
	}
	if status, err := w.resolvers[0].Resolve(ctx, payment, reservation); err != nil || status != redemption.StatusBroadcast {
		t.Fatal(status, err)
	}
	w.coreMine(1)
	if actions, err := w.maintainer.ProveSettlements(ctx); err != nil || len(actions) != 1 {
		t.Fatalf("payment proof: %+v %v", actions, err)
	}
	w.mine(k06Finality)
	for _, l := range w.locks {
		if err := l.Close(ctx, w.chain, w.head()-k06Finality, w.walletID, payment.ConflictInput(), "payment proven"); err != nil {
			t.Fatal("close the paid main:", err)
		}
	}

	c := w.deposit(3_000_000, locktime, [20]byte{})
	r = w.round(c.revealBlock + k06Finality)
	plan = w.plan(r)
	custody, _ := w.chain.Custody(ctx, w.head(), w.walletID)
	if m := plan.SweepMain(); m == nil || deposit.OutpointKey(m.Outpoint) != custody.MainOutpoint || m.Outpoint.TransactionHash != bitcoin.Hash(payment.BitcoinTransactionHash()) {
		t.Fatal("the sweep after a confirmed payment must spend the payment's change as main")
	}
	lossy := w.sweeps
	lossy.Chain = &lostReply{Chain: w.chain, lose: true}
	w.sweepAndProve(plan, r.Number, lossy)
}

// fundBySweep gives the wallet its first main output through a K-06 sweep.
func (w *k06World) fundBySweep() bitcoin.TransactionOutpoint {
	w.t.Helper()
	d := w.deposit(1_000_000, w.locktime(3*k06RefundDelay), [20]byte{})
	r := w.round(d.revealBlock + k06Finality)
	plan := w.plan(r)
	w.sweepAndProve(plan, r.Number, w.sweeps)
	return bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(plan.BitcoinTransactionHash())}
}

// refund signs the depositor refund from C-05's historical refund
// construction and the refund key only; no operator is involved.
func (w *k06World) refund(d funded) *bitcoin.Transaction {
	t, ctx := w.t, w.f.ctx
	t.Helper()
	contract, err := w.bridge.FrostDepositRefundConstruction(&bind.CallOpts{Context: ctx}, d.key)
	if err != nil {
		t.Fatal(err)
	}
	w.sameConstruction(d.terms, d.construction, contract)
	c := deposit.Construction{OutputKey: contract.Output.Key, Root: contract.Output.Root, Parity: contract.Output.Parity, Receipt: contract.Receipt, WalletLeaf: contract.WalletLeaf, RefundLeaf: contract.RefundLeaf, WalletControl: contract.WalletControl, RefundControl: contract.RefundControl}
	destination, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{0x77})
	tx, err := deposit.Refund(d.terms, c, d.funding, destination, 1_000, w.refundKey)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func (w *k06World) launchTotal() int {
	n := 0
	for i := range w.f.nodes {
		n += w.launchCount(i)
	}
	return n
}

// TestDepositorRefundWithoutOperators: with every operator process idle, the
// depositor refunds from public C-05 data and the refund key. Bitcoin refuses
// the refund before median-time-past passes the locktime and accepts it after.
// A refunded deposit is never planned again, a plan made before the refund is
// abandoned without a worker launch, and nothing is credited.
func TestDepositorRefundWithoutOperators(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	locktime := w.locktime(2 * k06RefundMargin)
	d := w.deposit(1_000_000, locktime, [20]byte{})
	r := w.round(d.revealBlock + k06Finality)
	stale := w.plan(r)
	before := w.balance(w.adminAddr)
	tx := w.refund(d)
	if err := w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err == nil || !strings.Contains(err.Error(), "non-final") {
		t.Fatal("Bitcoin accepted the refund before its locktime:", err)
	}
	w.moveBitcoinTime(uint64(locktime) + 1)
	if err := w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err != nil {
		t.Fatal("refund rejected after the locktime:", err)
	}
	w.coreMine(1)
	if unspent, err := w.core.Unspent(ctx, d.funding); err != nil || unspent {
		t.Fatal("refund did not spend the deposit", err)
	}
	if w.launchTotal() != 0 {
		t.Fatal("an operator worker ran during the refund")
	}
	for i, s := range w.sweepers {
		if status, err := s.Resolve(ctx, stale); err != nil || status != deposit.StatusAbandoned {
			t.Fatalf("operator %d: plan of a refunded deposit: %s %v", i, status, err)
		}
		next := w.round(r.Anchor + 1)
		if _, err := s.Plan(ctx, w.fromBlock, next); !errors.Is(err, deposit.ErrNothingToSweep) {
			t.Fatal("refunded deposit planned again:", err)
		}
	}
	if w.launchTotal() != 0 || w.balance(w.adminAddr) != before {
		t.Fatal("refunded deposit reached a worker or was credited")
	}
	record, err := w.chain.Deposit(ctx, w.head(), d.key)
	if err != nil || record.SweptAt != 0 {
		t.Fatal("refunded deposit marked swept", err)
	}
}

// TestLateDepositRecovery covers the refund race and the dead sweep.
//
// Dead sweep (VD-07 reading 2): a main-spending sweep is signed but not
// broadcast; its deposit is then refunded, so it can never confirm. The main
// stays with the sweep purpose: a redemption cannot be attested on it, and a
// new sweep alternative without the refunded deposit spends it. After that
// sweep is proven the old main closes and the new main accepts a redemption.
//
// Late signature: median-time-past crosses the refund margin after the
// workers returned and before acceptance. Every operator quarantines the
// signature, nothing is broadcast, and the depositor refund settles it.
func TestLateDepositRecovery(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	m1 := w.fundBySweep()

	short := w.locktime(2 * k06RefundMargin)
	e := w.deposit(1_000_000, short, [20]byte{})
	r := w.round(e.revealBlock + k06Finality)
	dead := w.plan(r)
	if m := dead.SweepMain(); m == nil || m.Outpoint != m1 || len(dead.SweepDeposits()) != 1 {
		t.Fatal("sweep must spend the unreserved main and the deposit")
	}
	for i, err := range w.signSweep(dead, r.Number) {
		if err != nil {
			t.Fatalf("operator %d: %v", i, err)
		}
	}
	// The operators stop before broadcast. A redemption now cannot take the
	// main a released sweep may spend.
	w.request(100_000, p2wpkh(0x62))
	payment, _, err := w.proposal(w.head())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.attesters[0].Attest(ctx, payment, w.head()+100); !errors.Is(err, deposit.ErrMainBusy) {
		t.Fatal("redemption attested on a main held by a released sweep:", err)
	}
	w.moveBitcoinTime(uint64(short) + 1)
	if err = w.core.Broadcast(ctx, w.refund(e).Serialize(bitcoin.Witness)); err != nil {
		t.Fatal(err)
	}
	w.coreMine(1)
	if err = w.sweepers[0].Broadcast(ctx, dead); err == nil {
		t.Fatal("dead sweep broadcast after its deposit was refunded")
	}
	f := w.deposit(2_000_000, w.locktime(3*k06RefundDelay), [20]byte{})
	r = w.round(f.revealBlock + k06Finality)
	alternative := w.plan(r)
	if m := alternative.SweepMain(); m == nil || m.Outpoint != m1 || len(alternative.SweepDeposits()) != 1 || alternative.SweepDeposits()[0].Key != f.key {
		t.Fatal("alternative must spend the same main without the refunded deposit")
	}
	w.sweepAndProve(alternative, r.Number, w.sweeps)
	w.mine(k06Finality)
	for _, l := range w.locks {
		if current, _ := l.Current(ctx, w.walletID, deposit.OutpointKey(m1)); current.Purpose != deposit.SweepMain {
			t.Fatal("the sweep purpose must hold the main until a finalized spend")
		}
		if err = l.Close(ctx, w.chain, w.head()-k06Finality, w.walletID, m1, "sweep proven"); err != nil {
			t.Fatal(err)
		}
	}
	payment, reservation := w.admit()
	if payment.ConflictInput() != (bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(alternative.BitcoinTransactionHash())}) || reservation == ([32]byte{}) {
		t.Fatal("redemption must reserve the new main")
	}

	// Late signature at the margin: the main is reserved, so this sweep is
	// deposit-only.
	late := w.locktime(3 * k06RefundDelay)
	g := w.deposit(1_000_000, late, [20]byte{})
	r = w.round(g.revealBlock + k06Finality)
	plan := w.plan(r)
	if plan.SweepMain() != nil {
		t.Fatal("a reserved main must stay out of the sweep")
	}
	for _, j := range w.journals {
		j.setOnDone(func([]byte) { w.clock.set(uint64(late) - k06RefundMargin) })
	}
	for i, err := range w.signSweep(plan, r.Number) {
		if !errors.Is(err, signing.ErrLateSignature) {
			t.Fatalf("operator %d accepted a signature past the margin: %v", i, err)
		}
	}
	for _, j := range w.journals {
		j.setOnDone(nil)
	}
	for i, s := range w.sweepers {
		if status, err := s.Resolve(ctx, plan); err != nil || status != deposit.StatusQuarantined {
			t.Fatalf("operator %d: %s %v", i, status, err)
		}
		if err := s.Broadcast(ctx, plan); err == nil {
			t.Fatal("quarantined sweep broadcast")
		}
	}
	if unspent, err := w.core.Unspent(ctx, g.funding); err != nil || !unspent {
		t.Fatal("late sweep reached Bitcoin", err)
	}
	w.clock.set(0)
	w.moveBitcoinTime(uint64(late) + 1)
	if err = w.core.Broadcast(ctx, w.refund(g).Serialize(bitcoin.Witness)); err != nil {
		t.Fatal("refund of the quarantined deposit:", err)
	}
	w.coreMine(1)
	if record, err := w.chain.Deposit(ctx, w.head(), g.key); err != nil || record.SweptAt != 0 {
		t.Fatal("refunded deposit credited", err)
	}
}

// TestMainPurposeLockAndResolution: a reserved main stays with its
// redemption. A deposit-only sweep is credited beside it. The C-04 sweep-
// resolution route moves every operator's lock to resolution before it signs
// the certificate; afterwards no payment grant reaches a worker. The
// resolution sweep spends the reserved main, is proven through C-04, and the
// reserved requests become refundable and are refunded.
func TestMainPurposeLockAndResolution(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	m1 := w.fundBySweep()
	request := w.request(100_000, p2wpkh(0x63))
	payment, reservation := w.admit()
	for _, l := range w.locks {
		if err := l.AcquireSweep(ctx, w.walletID, m1, [32]byte{1}); !errors.Is(err, deposit.ErrMainBusy) {
			t.Fatal("ordinary sweep took a reserved main:", err)
		}
	}
	g := w.deposit(1_000_000, w.locktime(3*k06RefundDelay), [20]byte{})
	r := w.round(g.revealBlock + k06Finality)
	side := w.plan(r)
	if side.SweepMain() != nil {
		t.Fatal("a reserved main must stay out of an ordinary sweep")
	}
	w.sweepAndProve(side, r.Number, w.sweeps)

	h := w.deposit(1_000_000, w.locktime(3*k06RefundDelay), [20]byte{})
	r = w.round(h.revealBlock + k06Finality)
	resolution, err := w.sweepers[0].Planner.Build(ctx, r.Anchor, [][32]byte{h.key}, reservation)
	if err != nil {
		t.Fatal(err)
	}
	if m := resolution.SweepMain(); m == nil || m.Outpoint != m1 {
		t.Fatal("resolution sweep must spend the reserved main")
	}
	// The resolution is attested by every operator inside the first durable
	// payment claim: after the payment passed its start-time checks, before
	// any worker grant. The grant-time lock check must refuse every operator.
	validUntil := w.head() + 100
	var parts []map[uint16][]byte
	var auth deposit.ResolutionAuthorization
	var attestErr error
	attest := func() {
		for _, a := range w.resolution {
			sigs, au, err := a.Attest(ctx, resolution, validUntil)
			if err != nil {
				attestErr = err
				return
			}
			parts, auth = append(parts, sigs), au
		}
	}
	hook := &claimHook{onClaim: attest}
	bases := make([]signing.Config, len(w.resolvers))
	for i, res := range w.resolvers {
		bases[i] = res.Base
		res.Base.Journal = &claimHook{hookedJournal: w.journals[i], onClaim: func() { hook.once.Do(attest) }}
	}
	launches := w.launchTotal()
	for i, err := range w.signAll(payment, reservation) {
		if err == nil || !strings.Contains(err.Error(), "main purpose lock") {
			t.Fatalf("operator %d payment grant raced past the resolution: %v", i, err)
		}
	}
	for i, res := range w.resolvers {
		res.Base = bases[i]
	}
	if attestErr != nil || len(parts) != len(w.resolution) {
		t.Fatal("resolution attestation inside the payment claim:", attestErr)
	}
	// Afterwards the start-time check refuses before any claim.
	for i, err := range w.signAll(payment, reservation) {
		if err == nil || !strings.Contains(err.Error(), "main purpose lock") {
			t.Fatalf("operator %d payment after resolution: %v", i, err)
		}
	}
	if w.launchTotal() != launches {
		t.Fatal("a payment worker started after the resolution attestation")
	}
	seats, signatures, err := redemption.Collect(w.wallet.Threshold, parts...)
	if err != nil {
		t.Fatal(err)
	}
	for i, err := range w.signSweep(resolution, r.Number) {
		if err != nil {
			t.Fatalf("operator %d resolution sweep: %v", i, err)
		}
	}
	if err = w.sweepers[0].Broadcast(ctx, resolution); err != nil {
		t.Fatal(err)
	}
	w.coreMine(1)
	action, err := w.sweeps.ProveSweep(ctx, w.walletID, resolution.BitcoinTransactionHash(), resolution.SweepVault(), &spv.FrostResolution{Authorization: auth, Seats: seats, Signatures: signatures})
	if err != nil || action.Kind != "sweep-resolution-proof" {
		t.Fatalf("resolution proof: %+v %v", action, err)
	}
	intent, err := w.chain.Intent(ctx, w.head(), reservation)
	if err != nil || intent.State != redemption.IntentConflictResolved {
		t.Fatal("reservation not resolved by the sweep", intent.State, err)
	}
	before := w.balance(w.adminAddr)
	actions, err := w.maintainer.ProcessRequests(ctx)
	if err != nil || len(actions) != 1 || actions[0].Kind != "resolved-refund" || actions[0].Request != request {
		t.Fatalf("resolved refund: %+v %v", actions, err)
	}
	if w.balance(w.adminAddr) != before+100_000 {
		t.Fatal("resolved request not refunded in full")
	}
	custody, _ := w.chain.Custody(ctx, w.head(), w.walletID)
	if custody.MainOutpoint != deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(resolution.BitcoinTransactionHash())}) || custody.ActiveReservation != ([32]byte{}) {
		t.Fatal("resolution sweep output must be the unreserved main")
	}
	w.mine(k06Finality)
	for _, l := range w.locks {
		if err = l.Close(ctx, w.chain, w.head()-k06Finality, w.walletID, m1, "resolution sweep proven"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDepositTemplateCrossLanguage: the Go D-04 construction equals C-05's
// (receipt, both leaves, root, C-07 output key and parity, both control
// blocks) for the real wallet Q across CScriptNum widths. Both refuse a
// block-height locktime. Bitcoin Core then spends the template both ways: a
// 4-byte-locktime deposit by the authorized threshold sweep, and a
// 5-byte-locktime deposit by the operator-free refund once median-time-past
// passes it. The fixed contract vectors are checked by the deposit package
// unit tests; the TypeScript SDK checks the same vectors.
func TestDepositTemplateCrossLanguage(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	for _, locktime := range []uint32{w.locktime(3 * k06RefundDelay), 0x7fffffff, 0x80000000, 0xffffffff} {
		for _, amount := range []uint64{3_000, 1_000_000, 2_100_000_000_000_000} {
			terms := w.terms(amount, locktime, [20]byte{})
			c, err := deposit.Build(terms, w.wallet.OutputKey)
			if err != nil {
				t.Fatal(err)
			}
			contract, err := w.bridge.FrostDepositConstruction(&bind.CallOpts{Context: ctx}, abiTerms(terms))
			if err != nil {
				t.Fatal(err)
			}
			w.sameConstruction(terms, c, contract)
		}
	}
	height := w.terms(1_000_000, 499_999_999, [20]byte{})
	if _, err := deposit.Build(height, w.wallet.OutputKey); err == nil {
		t.Fatal("Go accepted a block-height refund locktime")
	}
	if _, err := w.bridge.FrostDepositConstruction(&bind.CallOpts{Context: ctx}, abiTerms(height)); err == nil {
		t.Fatal("C-05 accepted a block-height refund locktime")
	}
	four := w.deposit(1_000_000, 0x7fffffff, [20]byte{})
	r := w.round(four.revealBlock + k06Finality)
	w.sweepAndProve(w.plan(r), r.Number, w.sweeps)
	five := w.deposit(1_000_000, 0x80000000, [20]byte{})
	if len(five.construction.RefundLeaf) != 76 {
		t.Fatal("expected a five-byte CLTV push")
	}
	tx := w.refund(five)
	if err := w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err == nil {
		t.Fatal("five-byte refund accepted before its locktime")
	}
	w.moveBitcoinTime(0x80000000 + 1)
	if err := w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err != nil {
		t.Fatal("Core rejected the five-byte CLTV refund:", err)
	}
	w.coreMine(1)
}
