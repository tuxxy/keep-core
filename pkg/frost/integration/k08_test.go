package integration

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/enforcement"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// K-08 local test values (OPEN).
const (
	k08Failures        = 2
	k08HeartbeatWindow = 15 * time.Second
)

// k08World is the K-06 world with the K-08 host on every operator: a
// defender and timeout notifier, a heartbeat runner and an inactivity
// claimer, each on the operator's own store, worker, transport and key.
type k08World struct {
	*k06World
	legacy    *legacyabi.Bridge
	own       []*eth.FrostRedemptionChain
	defenders []*enforcement.Defender
	runners   []*enforcement.Runner
	claimers  []*enforcement.Claimer
}

func newK08World(t *testing.T) *k08World {
	t.Helper()
	return attachK08World(t, newK06World(t))
}

// attachK08World installs enforcement on an existing wallet. K-09 uses it
// to join enforcement and movement on the same stores and contract state.
func attachK08World(t *testing.T, custody *k06World) *k08World {
	t.Helper()
	w := &k08World{k06World: custody}
	var err error
	if w.legacy, err = legacyabi.NewBridge(w.f.deployment.Bridge, w.f.client); err != nil {
		t.Fatal(err)
	}
	var seats []uint16
	for i, member := range w.wallet.Members {
		if member != 0 {
			seats = append(seats, uint16(i+1))
		}
	}
	for i, node := range w.f.nodes {
		own, err := eth.NewFrostRedemptionChain(w.f.ctx, w.f.client, node.key, w.f.deployment.Bridge)
		if err != nil {
			t.Fatal(err)
		}
		w.own = append(w.own, own)
		w.defenders = append(w.defenders, &enforcement.Defender{Chain: own, Wallet: w.walletID, Records: w.journals[i], From: w.fromBlock})
		base := w.resolvers[i].Base
		runner := &enforcement.Runner{Chain: own, Schedule: redemption.Schedule{FinalityBlocks: k06Finality, RoundBlocks: k06RoundBlocks}, Wallet: w.wallet, Base: signing.HeartbeatConfig{Journal: w.journals[i], Worker: base.Worker, Selected: seats, Transport: base.Transport, LocalRegtest: true}}
		w.runners = append(w.runners, runner)
		w.claimers = append(w.claimers, &enforcement.Claimer{Chain: own, Runner: runner, Records: w.journals[i], LocalSeats: w.f.executors[i].Request.LocalSeats, Key: node.key, Failures: k08Failures})
	}
	return w
}

// admit submits a public fraud challenge with the C-08 bond.
func (w *k08World) admit(digest [32]byte, signature []byte) error {
	w.t.Helper()
	params, err := w.legacy.FraudParameters(&bind.CallOpts{Context: w.f.ctx})
	if err != nil {
		w.t.Fatal(err)
	}
	_, err = w.trySend(func(o *bind.TransactOpts) (*types.Transaction, error) {
		o.Value = params.FraudChallengeDepositAmount
		return w.bridge.SubmitFrostFraudChallenge(o, w.walletID, digest, signature)
	})
	return err
}

func (w *k08World) resolved(digest [32]byte) bool {
	w.t.Helper()
	c, err := w.chain.Challenge(w.f.ctx, w.walletID, digest)
	if err != nil {
		w.t.Fatal(err)
	}
	return c.Resolved
}

func (w *k08World) walletState() movement.State {
	w.t.Helper()
	wallet, err := w.chain.Wallet(w.f.ctx, w.head(), movement.Label(w.walletID))
	if err != nil {
		w.t.Fatal(err)
	}
	return wallet.State
}

// nextHeartbeat mines until the runners' current round is newer than after.
func (w *k08World) nextHeartbeat(after uint64) enforcement.Round {
	w.t.Helper()
	for {
		r, err := w.runners[0].Current(w.f.ctx)
		if err != nil {
			w.t.Fatal(err)
		}
		if r.Anchor > after {
			return r
		}
		w.mine(1)
	}
}

// heartbeat runs round on the listed operators only, concurrently.
func (w *k08World) heartbeat(round enforcement.Round, operators ...int) ([]enforcement.Outcome, []error) {
	outcomes := make([]enforcement.Outcome, len(operators))
	errs := make([]error, len(operators))
	ctx, cancel := context.WithTimeout(w.f.ctx, k08HeartbeatWindow)
	defer cancel()
	var wg sync.WaitGroup
	for k, i := range operators {
		wg.Add(1)
		go func(k, i int) {
			defer wg.Done()
			outcomes[k], errs[k] = w.runners[i].Run(ctx, round)
		}(k, i)
	}
	wg.Wait()
	return outcomes, errs
}

// restart closes operator i's store and reopens it from disk, as a process
// restart does, and returns the reopened records.
func (w *k08World) restart(i int) *store.Scoped {
	w.t.Helper()
	domain := w.f.journals[i].Domain()
	if err := w.f.roots[i].Close(); err != nil {
		w.t.Fatal(err)
	}
	root, err := store.Open(w.f.ctx, w.f.rootConfigs[i])
	if err != nil {
		w.t.Fatal("reopen:", err)
	}
	w.f.roots[i] = root
	scoped, err := root.Scope(domain)
	if err != nil {
		w.t.Fatal(err)
	}
	return scoped
}

// signSweepTx signs plan on every operator and returns the signed sweep
// without broadcasting it.
func (w *k08World) signSweepTx(plan *signing.Plan, number uint64) *bitcoin.Transaction {
	w.t.Helper()
	results := make([]*signing.Result, len(w.sweepers))
	errs := make([]error, len(w.sweepers))
	ctx, cancel := context.WithTimeout(w.f.ctx, 120*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, s := range w.sweepers {
		wg.Add(1)
		go func(i int, s *deposit.Sweeper) {
			defer wg.Done()
			results[i], errs[i] = s.Sign(ctx, plan, number)
		}(i, s)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			w.t.Fatalf("operator %d: %v", i, err)
		}
	}
	return results[0].Transaction
}

// theftDigest is the BIP341 SIGHASH_ALL|ANYONECANPAY message of a spend of
// main to a key the wallet never authorized: a non-default malicious mode.
func theftDigest(t *testing.T, main bitcoin.TransactionOutpoint, prev *bitcoin.TransactionOutput) [32]byte {
	t.Helper()
	tx := wire.NewMsgTx(2)
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint((*chainhash.Hash)(&main.TransactionHash), main.OutputIndex), nil, nil))
	thief, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{0x7e})
	tx.AddTxOut(wire.NewTxOut(prev.Value-1_000, thief))
	fetcher := txscript.NewCannedPrevOutputFetcher(prev.PublicKeyScript, prev.Value)
	digest, err := txscript.CalcTaprootSignatureHash(txscript.NewTxSigHashes(tx, fetcher), txscript.SigHashAll|txscript.SigHashAnyOneCanPay, tx, 0, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	return [32]byte(digest)
}

// TestFrostFraudPublicAdmissionDefenseTimeout: real released signatures are
// admitted publicly and defended by the host from its records alone: a proven
// script-path deposit sweep, and both signatures of a sweep that lost to the
// depositor's refund and never mined, defended after a store restart. A real
// threshold signature over a SIGHASH_ALL|ANYONECANPAY theft is admitted, the
// host cannot defend it, and after the deadline the host notifies the
// timeout: the wallet is terminated with a notification-only sanction.
// Duplicate and invalid admissions are refused.
func TestFrostFraudPublicAdmissionDefenseTimeout(t *testing.T) {
	w := newK08World(t)
	ctx := w.f.ctx

	d := w.deposit(1_000_000, w.locktime(3*k06RefundDelay), [20]byte{})
	r := w.round(d.readyBlock + k06Finality)
	proven := w.plan(r)
	w.sweepAndProve(proven, r.Number, w.sweeps)
	main := bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(proven.BitcoinTransactionHash())}
	minedSweep := w.coreTx(main.TransactionHash)
	digest := proven.Snapshot().Digests()[0]
	if err := w.admit(digest, minedSweep.Inputs[0].Witness[0]); err != nil {
		t.Fatal("public admission of a mined script-path sweep signature:", err)
	}
	if err := w.admit(digest, minedSweep.Inputs[0].Witness[0]); err == nil {
		t.Fatal("a duplicate challenge was admitted")
	}
	forged := append([]byte(nil), minedSweep.Inputs[0].Witness[0]...)
	forged[5] ^= 1
	if err := w.admit([32]byte{0x0f}, forged); err == nil {
		t.Fatal("an invalid signature was admitted")
	}
	actions, err := w.defenders[1].Defend(ctx)
	if err != nil || len(actions) != 1 || actions[0].Kind != enforcement.ActionDefended || !w.resolved(digest) {
		t.Fatalf("proven sweep defense: %+v %v", actions, err)
	}

	// An unauthorized real signature in a non-default mode, made with the
	// unguarded test engine.
	theft := theftDigest(t, main, minedSweep.Outputs[0])
	theftSignature := w.signDigest(theft, "k08-theft")
	if err = w.admit(theft, theftSignature[:]); err != nil {
		t.Fatal("admission of an unauthorized signature:", err)
	}

	// A losing replacement: the sweep of main and a short deposit is signed
	// and broadcast, then the depositor's refund replaces it and mines.
	short := w.locktime(2 * k06RefundMargin)
	e := w.deposit(1_000_000, short, [20]byte{})
	r = w.round(e.readyBlock + k06Finality)
	dead := w.plan(r)
	if m := dead.SweepMain(); m == nil || m.Outpoint != main {
		t.Fatal("the losing sweep must spend the main")
	}
	deadTx := w.signSweepTx(dead, r.Number)
	// The sweep reaches the mempool only once the refund is valid, so the
	// refund can replace it before any block includes it.
	w.moveBitcoinTime(uint64(short) + 1)
	if err = w.core.Broadcast(ctx, deadTx.Serialize(bitcoin.Witness)); err != nil {
		t.Fatal("broadcast of the sweep:", err)
	}
	if err = w.core.Broadcast(ctx, w.refund(e).Serialize(bitcoin.Witness)); err != nil {
		t.Fatal("the refund must replace the sweep:", err)
	}
	w.coreMine(1)
	if unspent, err := w.core.Unspent(ctx, main); err != nil || !unspent {
		t.Fatal("the losing sweep mined", err)
	}
	for i, dg := range dead.Snapshot().Digests() {
		if err = w.admit(dg, deadTx.Inputs[i].Witness[0]); err != nil {
			t.Fatalf("admission of losing sweep input %d: %v", i, err)
		}
	}

	// Defense before the deadline after a restart, from the reopened store.
	records := w.restart(0)
	defender := &enforcement.Defender{Chain: w.own[0], Wallet: w.walletID, Records: records, From: w.fromBlock}
	if actions, err = defender.NotifyTimeouts(ctx); err != nil || kinds(actions)[theft] != enforcement.ActionPending {
		t.Fatalf("before the deadline: %+v %v", actions, err)
	}
	if actions, err = defender.Defend(ctx); err != nil {
		t.Fatal("defense after restart:", err)
	}
	defended := 0
	for _, a := range actions {
		if a.Kind == enforcement.ActionDefended {
			defended++
		}
	}
	for _, dg := range dead.Snapshot().Digests() {
		if !w.resolved(dg) {
			t.Fatal("a losing sweep signature was left undefended")
		}
	}
	if defended != 2 || w.resolved(theft) {
		t.Fatalf("defended %d; the theft must stay actionable", defended)
	}

	// Timeout of the unauthorized signature.
	params, err := w.legacy.FraudParameters(&bind.CallOpts{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	w.warp(int64(params.FraudChallengeDefeatTimeout) + 1)
	from := w.head()
	if actions, err = defender.NotifyTimeouts(ctx); err != nil || kinds(actions)[theft] != enforcement.ActionTimedOut {
		t.Fatalf("timeout: %+v %v", actions, err)
	}
	for _, a := range actions {
		if a.Digest != theft && a.Kind == enforcement.ActionTimedOut {
			t.Fatal("the host timed out a digest it could defend")
		}
	}
	if !w.resolved(theft) || w.walletState() != movement.StateTerminated {
		t.Fatal("the timeout must terminate the wallet")
	}
	it, err := w.bridge.FilterFrostSanctionNotified(&bind.FilterOpts{Start: from, Context: ctx}, [][32]byte{w.walletID})
	if err != nil {
		t.Fatal(err)
	}
	sanctions := 0
	for it.Next() {
		if it.Event.Reason == crypto.Keccak256Hash([]byte("fraud-defeat-timeout")) {
			sanctions++
		}
	}
	if sanctions != 1 {
		t.Fatalf("%d fraud sanctions, want 1", sanctions)
	}
}

// TestHeartbeatDomainAndDefense: every operator signs the current round's
// heartbeat through the guarded heartbeat purpose. The host digest equals
// C-08's, the signature is admitted publicly and defended in the heartbeat
// domain, and a stale round is refused without a worker grant.
func TestHeartbeatDomainAndDefense(t *testing.T) {
	w := newK08World(t)
	ctx := w.f.ctx
	round := w.nextHeartbeat(0)
	outcomes, errs := w.heartbeat(round, 0, 1)
	for i, err := range errs {
		if err != nil || !outcomes[i].Signed {
			t.Fatalf("operator %d heartbeat: %+v %v", i, outcomes[i], err)
		}
	}
	if !reflect.DeepEqual(outcomes[0].Present, []uint16{1, 2, 3}) {
		t.Fatalf("presence %v", outcomes[0].Present)
	}
	digest := signing.HeartbeatDigest(w.walletID, round.Anchor)
	onchain, err := w.bridge.FrostHeartbeatDigest(&bind.CallOpts{Context: ctx}, w.walletID, round.Anchor)
	if err != nil || onchain != digest {
		t.Fatal("host heartbeat digest differs from C-08", err)
	}
	if err = w.admit(digest, outcomes[0].Signature[:]); err != nil {
		t.Fatal("admission of a real heartbeat signature:", err)
	}
	actions, err := w.defenders[1].Defend(ctx)
	if err != nil || len(actions) != 1 || actions[0].Kind != enforcement.ActionDefended || !w.resolved(digest) {
		t.Fatalf("heartbeat defense: %+v %v", actions, err)
	}
	next := w.nextHeartbeat(round.Anchor)
	if next.Anchor == round.Anchor {
		t.Fatal("no new round")
	}
	stale := round
	stale.Anchor, stale.Number = round.Anchor-k06RoundBlocks, round.Number-1
	launches := w.launchTotal()
	if _, errs = w.heartbeat(stale, 0); !errors.Is(errs[0], signing.ErrUnauthorized) || w.launchTotal() != launches {
		t.Fatal("a stale heartbeat reached a worker", errs[0])
	}
	if _, err = enforcement.ReadOutcome(ctx, w.journals[0], w.walletID, stale.Anchor); !errors.Is(err, store.ErrMissing) {
		t.Fatal("a refused heartbeat recorded an outcome")
	}
}

// TestInactivityMemberRouting: the operator holding seat 3 is offline for two
// heartbeat rounds. The other operator's authenticated presence records name
// exactly seat 3; its seats 1 and 2 (the threshold) sign the claim; C-09
// verifies it against the descriptor, moves the Live wallet to MovingFunds
// and names seat 3 and its operator. A replay and an under-threshold claim
// are refused.
func TestInactivityMemberRouting(t *testing.T) {
	w := newK08World(t)
	ctx := w.f.ctx
	w.fundBySweep()
	if w.walletState() != movement.StateLive {
		t.Fatal("wallet must be Live")
	}
	var anchor uint64
	for k := 0; k < k08Failures; k++ {
		round := w.nextHeartbeat(anchor)
		anchor = round.Anchor
		outcomes, errs := w.heartbeat(round, 0)
		if errs[0] == nil || outcomes[0].Signed || !reflect.DeepEqual(outcomes[0].Present, []uint16{1, 2}) {
			t.Fatalf("round %d: %+v %v", k, outcomes[0], errs[0])
		}
	}
	claim, _, err := w.claimers[0].Derive(ctx)
	if err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || !claim.HeartbeatFailed {
		t.Fatalf("claim %+v %v", claim, err)
	}
	host, err := enforcement.ClaimDigest(w.own[0].ChainID(), w.own[0].Bridge(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if onchain, err := w.own[0].InactivityClaimDigest(ctx, claim); err != nil || onchain != host {
		t.Fatal("host claim digest differs from C-09", err)
	}
	signatures, err := w.claimers[0].Sign(ctx, claim)
	if err != nil || len(signatures) != 2 {
		t.Fatalf("signatures %v %v", signatures, err)
	}
	one := map[uint16][]byte{1: signatures[1]}
	seats, packed, err := redemption.Collect(1, one)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.own[0].NotifyInactivity(ctx, claim, seats, packed); err == nil {
		t.Fatal("a claim below the threshold was accepted")
	}
	seats, packed, err = redemption.Collect(w.wallet.Threshold, signatures)
	if err != nil {
		t.Fatal(err)
	}
	from := w.head()
	if err = w.claimers[0].Submit(ctx, claim, seats, packed); err != nil {
		t.Fatal(err)
	}
	if w.walletState() != movement.StateMovingFunds {
		t.Fatal("heartbeat failure must move the Live wallet's funds")
	}
	it, err := w.bridge.FilterFrostOperatorInactivityNotified(&bind.FilterOpts{Start: from, Context: ctx}, [][32]byte{w.walletID})
	if err != nil {
		t.Fatal(err)
	}
	notified := 0
	for it.Next() {
		notified++
		e := it.Event
		if !reflect.DeepEqual(e.InactiveSeats, []uint16{3}) || len(e.InactiveOperators) != 1 || e.InactiveOperators[0] != crypto.PubkeyToAddress(w.f.nodes[1].key.PublicKey) || !e.HeartbeatFailed {
			t.Fatalf("attribution %+v", e)
		}
	}
	if notified != 1 {
		t.Fatalf("%d inactivity notifications", notified)
	}
	if err = w.claimers[0].Submit(ctx, claim, seats, packed); err != nil {
		t.Fatal("a submitted claim must settle as submitted:", err)
	}
	if err = w.own[0].NotifyInactivity(ctx, claim, seats, packed); err == nil {
		t.Fatal("a replayed claim was accepted")
	}
	if _, _, err = w.claimers[0].Derive(ctx); !errors.Is(err, enforcement.ErrNoClaim) {
		t.Fatal("the claimed window produced a second claim", err)
	}
}

func kinds(actions []enforcement.Action) map[[32]byte]string {
	out := map[[32]byte]string{}
	for _, a := range actions {
		out[a.Digest] = a.Kind
	}
	return out
}
