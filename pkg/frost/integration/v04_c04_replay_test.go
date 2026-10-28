package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// V-04 C-04 projection replay (KC-VER H-01..H-07). Each generated trace's
// contract-visible actions (Reserve, Finalize, Tick, Pay, Cancel, Refund) run
// on the real deployed C-04 Bridge with the K-05 host, real stores, the real
// worker and Bitcoin Core. Start/Deliver drive real joint signing, held after
// every Done record is durable until the model's Deliver. Change and Reanchor
// are applied through a controlled chain-view knob over the real chain (H-03);
// commit-stage, crash, I/O, caller-cancel, recover and retry actions are not
// replayed here (the K-04 process traces cover them), so every replay is
// classified "partial". Request and attempt outcomes are compared at every
// mapped step with the claim model's expect_state.

var c04Traces = []string{
	"None-W-payment-settles",
	"None-W-cancel-then-refund",
	"None-W-unreserved-refund",
	"None-W-next-reservation-after-confirmed-outcome",
	"None-W-sign-after-admission-certificate-expiry",
	"RefundOnElapsedTime-P-K05-NO-DOUBLE-OUTCOME",
	"RefundOnCertificateExpiry-P-K05-NO-DOUBLE-OUTCOME",
	"AllowConcurrentReservations-P-C04-ONE-ACTIVE-CUSTODY-RESERVATION",
}

type v04Trace struct {
	ID, Kind, Property, Weakening string
	Steps                         []v04Step `json:"steps"`
	Claim                         []v04Step `json:"claim_model_steps"`
}
type v04Step struct {
	N        int             `json:"n"`
	Action   string          `json:"action"`
	Fields   json.RawMessage `json:"fields"`
	Diverges bool            `json:"diverges"`
	State    v04State        `json:"expect_state"`
}
type v04State struct {
	Tick     int `json:"tick"`
	Requests []struct {
		Reservation int  `json:"reservation"`
		Paid        bool `json:"paid"`
		Cancel      bool `json:"cancel"`
		Refund      bool `json:"refund"`
	} `json:"requests"`
	Attempts []struct {
		Accepted    bool `json:"accepted"`
		Quarantined bool `json:"quarantined"`
		Delivered   bool `json:"delivered"`
	} `json:"attempts"`
}

// knobReader is the controlled chain view: everything is read from the real
// chain, except that a "changed" reservation reports Quarantined (no longer
// authorizing payment, still unresolved) and a "reanchored" one reports an
// admission block hash that differs from the canonical header.
type knobReader struct {
	redemption.Reader
	mu                  sync.Mutex
	changed, reanchored map[[32]byte]bool
}

func (k *knobReader) Intent(ctx context.Context, at uint64, id [32]byte) (redemption.Intent, error) {
	i, err := k.Reader.Intent(ctx, at, id)
	k.mu.Lock()
	defer k.mu.Unlock()
	if err == nil && k.changed[id] && i.State == redemption.IntentReserved {
		i.State = redemption.IntentQuarantined
	}
	return i, err
}
func (k *knobReader) Admission(ctx context.Context, id [32]byte) (redemption.Admission, error) {
	a, err := k.Reader.Admission(ctx, id)
	k.mu.Lock()
	defer k.mu.Unlock()
	if err == nil && k.reanchored[id] {
		a.BlockHash[0] ^= 0xff
	}
	return a, err
}

type v04Attempt struct {
	request int
	release chan struct{}
	done    chan struct{}
	errs    []error
}

type v04Replay struct {
	t            *testing.T
	w            *k05World
	knob         *knobReader
	resolvers    []*redemption.Resolver
	base         uint64
	tick         int
	ticks        uint64
	requests     [2][32]byte
	plans        [2]*signing.Plan
	reservations [2][32]byte
	attempts     map[int]*v04Attempt
	signatures   map[[32]byte][64]byte
	sigMu        sync.Mutex
	partial      []string
	compared     int
	delivered    int
}

func (r *v04Replay) note(s string) {
	for _, p := range r.partial {
		if p == s {
			return
		}
	}
	r.partial = append(r.partial, s)
}

// code maps real chain state of one model request to the model encoding:
// 0 absent, 1 unfinalized, 2 finalized, 3 timeout-pending, 4 paid,
// 5 cancelled, 6 refunded.
func (r *v04Replay) code(i int) (code int, paid, cancel, refund bool) {
	ctx, w := r.w.f.ctx, r.w
	h := w.head()
	q, err := w.chain.Request(ctx, h, r.requests[i])
	if err != nil {
		r.t.Fatal(err)
	}
	if q.ReservationID != ([32]byte{}) {
		intent, err := w.chain.Intent(ctx, h, q.ReservationID)
		if err != nil {
			r.t.Fatal(err)
		}
		switch intent.State {
		case redemption.IntentPaid:
			return 4, true, false, false
		case redemption.IntentCancelled:
			// C-04 refunds atomically with the cancellation proof.
			return 5, false, true, q.State == redemption.RequestRefunded
		case redemption.IntentTimeoutPending, redemption.IntentQuarantined:
			return 3, false, false, false
		case redemption.IntentReserved:
			if h-intent.AdmittedBlock >= k05Finality {
				return 2, false, false, false
			}
			return 1, false, false, false
		}
		r.t.Fatalf("unmapped intent state %d", intent.State)
	}
	if q.State == redemption.RequestRefunded {
		return 6, false, false, true
	}
	return 0, false, false, false
}

func (r *v04Replay) compare(step v04Step) error {
	r.compared++
	for i := range r.requests {
		want := step.State.Requests[i]
		code, paid, cancel, refund := r.code(i)
		if want.Cancel && cancel && want.Reservation == 5 && !want.Refund && refund {
			// Refinement R-CANCEL-ATOMIC-REFUND: the model refunds in a later
			// step; C-04 refunds in the same proof transaction.
			continue
		}
		if want.Refund && want.Cancel && cancel && refund {
			continue
		}
		if code != want.Reservation || paid != want.Paid || cancel != want.Cancel || refund != want.Refund {
			return fmt.Errorf("step %d %s request %d: real (%d paid=%v cancel=%v refund=%v) model (%d paid=%v cancel=%v refund=%v)", step.N, step.Action, i, code, paid, cancel, refund, want.Reservation, want.Paid, want.Cancel, want.Refund)
		}
	}
	return nil
}

func (r *v04Replay) mineTo(block uint64) error {
	h := r.w.head()
	if h > block {
		return fmt.Errorf("replay limitation: head %d already past tick boundary %d", h, block)
	}
	r.w.mine(int(block - h))
	return nil
}

func (r *v04Replay) reserve(i int, deadline int) error {
	ctx, w := r.w.f.ctx, r.w
	head := w.head()
	plan, _, err := w.planner.Build(ctx, head, [][32]byte{r.requests[i]})
	if err != nil {
		return err
	}
	validUntil := r.base + uint64(deadline)*r.ticks + r.ticks - 1
	var parts []map[uint16][]byte
	for _, a := range w.attesters {
		sigs, err := a.Attest(ctx, plan, validUntil)
		if err != nil {
			return err
		}
		parts = append(parts, sigs)
	}
	seats, signatures, err := redemption.Collect(w.wallet.Threshold, parts...)
	if err != nil {
		return err
	}
	certificate, _ := redemption.CertificateFor(plan, validUntil)
	proposal := redemption.ReservationProposal{Certificate: certificate, Transaction: redemption.BitcoinInfo{Version: plan.Snapshot().Unsigned().SerializeVersion(), InputVector: plan.Snapshot().Unsigned().SerializeInputs(), OutputVector: plan.Snapshot().Unsigned().SerializeOutputs(), Locktime: plan.Snapshot().Unsigned().SerializeLocktime()}, Seats: seats, Signatures: signatures}
	for _, p := range plan.Snapshot().PreviousOutputs() {
		proposal.Inputs = append(proposal.Inputs, redemption.UTXO{TransactionHash: p.Outpoint.TransactionHash, OutputIndex: p.Outpoint.OutputIndex, Value: uint64(p.Value)})
	}
	for _, q := range plan.Requests() {
		proposal.Requests = append(proposal.Requests, redemption.ProposalRequest{ID: q.ID, Value: uint64(q.Value), Script: q.Script})
	}
	if err = w.chain.Reserve(ctx, proposal); err != nil {
		return err
	}
	q, err := w.chain.Request(ctx, w.head(), r.requests[i])
	if err != nil || q.ReservationID == ([32]byte{}) {
		return fmt.Errorf("reservation not recorded: %v", err)
	}
	r.plans[i], r.reservations[i] = plan, q.ReservationID
	return nil
}

func (r *v04Replay) start(attempt, request, mode int) {
	if mode != 0 {
		r.t.Fatalf("weakened grant mode %d is outside the C-04 projection set", mode)
	}
	for _, a := range r.attempts {
		select {
		case <-a.done:
		default:
			r.note("concurrent model attempts: only the first in-flight attempt drives real signing")
			return
		}
	}
	a := &v04Attempt{request: request, release: make(chan struct{}), done: make(chan struct{}), errs: make([]error, len(r.resolvers))}
	r.attempts[attempt] = a
	plan, reservation := r.plans[request], r.reservations[request]
	var arrived sync.WaitGroup
	arrived.Add(len(r.w.journals))
	for _, j := range r.w.journals {
		j := j
		var once sync.Once
		j.setOnDone(func(raw []byte) {
			var d signing.Done
			// Hold only this attempt's payment Done; cancellation and other
			// plans proceed normally while the attempt waits for Deliver.
			if d.Unmarshal(raw) != nil || d.Context.Plan != plan.Commitment() {
				return
			}
			once.Do(arrived.Done)
			<-a.release
		})
	}
	go func() {
		defer close(a.done)
		var wg sync.WaitGroup
		for i, res := range r.resolvers {
			wg.Add(1)
			go func(i int, res *redemption.Resolver) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.w.f.ctx, 3*time.Minute)
				defer cancel()
				var result *signing.Result
				result, a.errs[i] = res.Sign(ctx, plan, reservation)
				_ = result
			}(i, res)
		}
		wg.Wait()
	}()
	// Wait until every operator's worker returned and its Done is durable.
	waited := make(chan struct{})
	go func() { arrived.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-a.done:
		r.t.Fatalf("attempt %d ended before release: %v", attempt, a.errs)
	case <-time.After(3 * time.Minute):
		r.t.Fatal("signing did not reach a durable Done record")
	}
	// Capture the released signature (the model's "possible" payment).
	if sig, err := r.releasedSignature(plan); err == nil {
		r.sigMu.Lock()
		r.signatures[reservation] = sig
		r.sigMu.Unlock()
	} else {
		r.t.Fatal(err)
	}
}

// releasedSignature returns the signature in operator 0's durable Done record
// for the plan's only input (the model's possible payment).
func (r *v04Replay) releasedSignature(plan *signing.Plan) ([64]byte, error) {
	raw := r.w.journals[0].lastDone()
	if raw == nil {
		return [64]byte{}, errors.New("no durable Done record observed")
	}
	var d signing.Done
	if err := d.Unmarshal(raw); err != nil {
		return [64]byte{}, err
	}
	if d.Context.Plan != plan.Commitment() {
		return [64]byte{}, errors.New("Done record of another plan")
	}
	return d.Signature, nil
}

func (r *v04Replay) deliver(attempt int, foreign bool, want v04State) error {
	a := r.attempts[attempt]
	if a == nil {
		r.note("Deliver of an attempt that did not drive real signing")
		return nil
	}
	if foreign {
		r.note("foreign Done context is covered by the K-04 seam replay, not here")
	}
	close(a.release)
	<-a.done
	for _, j := range r.w.journals {
		j.setOnDone(nil)
	}
	if foreign {
		return nil
	}
	accepted := true
	for _, err := range a.errs {
		if err != nil {
			accepted = false
			if !errors.Is(err, signing.ErrLateSignature) {
				return fmt.Errorf("attempt %d: unexpected signing error %v", attempt, a.errs)
			}
		}
	}
	r.delivered++
	m := want.Attempts[attempt]
	if accepted != m.Accepted || (!accepted) != m.Quarantined {
		return fmt.Errorf("attempt %d: real accepted=%v model accepted=%v quarantined=%v", attempt, accepted, m.Accepted, m.Quarantined)
	}
	return nil
}

func (r *v04Replay) pay(i int) error {
	w, ctx := r.w, r.w.f.ctx
	r.sigMu.Lock()
	sig, ok := r.signatures[r.reservations[i]]
	r.sigMu.Unlock()
	if !ok {
		return errors.New("no released signature to pay with")
	}
	s, err := bitcoin.NewSchnorrSignature(sig[:])
	if err != nil {
		return err
	}
	tx, err := r.plans[i].Snapshot().AddSignatures([]bitcoin.Signature{s})
	if err != nil {
		return err
	}
	if err = w.core.Broadcast(ctx, tx.Serialize(bitcoin.Witness)); err != nil {
		return err
	}
	w.coreMine(1)
	actions, err := w.maintainer.ProveSettlements(ctx)
	if err != nil {
		return err
	}
	if len(actions) != 1 || actions[0].Kind != "payment-proof" {
		return fmt.Errorf("payment proof actions %+v", actions)
	}
	return nil
}

func (r *v04Replay) refund(i int) error {
	w, ctx := r.w, r.w.f.ctx
	q, err := w.chain.Request(ctx, w.head(), r.requests[i])
	if err != nil {
		return err
	}
	if q.State == redemption.RequestRefunded {
		return nil
	}
	if q.ReservationID != ([32]byte{}) {
		// Reserved requests have no elapsed-time or expiry refund.
		if w.chain.RefundUnreserved(ctx, r.requests[i]) == nil || w.chain.RefundResolved(ctx, r.requests[i]) == nil {
			return errors.New("reserved request refunded")
		}
		return errRefused
	}
	if q.State == redemption.RequestRequested {
		if err = w.chain.NotifyTimeout(ctx, r.requests[i]); err != nil {
			return err
		}
	}
	w.mine(k05Finality)
	return w.chain.RefundUnreserved(ctx, r.requests[i])
}

var errRefused = errors.New("refused by the real system")

func (r *v04Replay) apply(step v04Step) error {
	var f []int
	_ = json.Unmarshal(step.Fields, &f)
	var one int
	_ = json.Unmarshal(step.Fields, &one)
	switch step.Action {
	case "Tick":
		r.tick++
		if r.tick == 3 {
			r.w.warp(k05Timeout + 1)
			for i := range r.requests {
				q, err := r.w.chain.Request(r.w.f.ctx, r.w.head(), r.requests[i])
				if err != nil {
					return err
				}
				if q.State == redemption.RequestRequested || q.State == redemption.RequestReserved {
					if err = r.w.chain.NotifyTimeout(r.w.f.ctx, r.requests[i]); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return r.mineTo(r.base + uint64(r.tick)*r.ticks)
	case "Reserve":
		if err := r.reserve(f[0], f[1]); err != nil {
			return fmt.Errorf("%w: %v", errRefused, err)
		}
		return nil
	case "Finalize":
		r.w.mine(k05Finality)
		return nil
	case "Change":
		r.knob.mu.Lock()
		r.knob.changed[r.reservations[one]] = true
		r.knob.mu.Unlock()
		return nil
	case "Reanchor":
		r.knob.mu.Lock()
		r.knob.reanchored[r.reservations[one]] = true
		r.knob.mu.Unlock()
		return nil
	case "Start":
		r.start(f[0], f[1], f[2])
		return nil
	case "Pay":
		return r.pay(one)
	case "Cancel":
		r.w.cancel(r.plans[one], r.reservations[one], 1000)
		return nil
	case "Refund":
		return r.refund(one)
	case "Step", "CancelCaller", "Recover", "Crash", "IoError", "Retry":
		r.note("commit-stage, crash, I/O, caller-cancel, recover and retry actions are covered by the K-04 process traces")
		return nil
	}
	return fmt.Errorf("unmapped action %s", step.Action)
}

// TestV04C04WeakenedExpectationsFail is the H-06 negative control: replaying
// a counterexample with the weakened model's own expectations must fail on
// the current source at the divergent step.
func TestV04C04WeakenedExpectationsFail(t *testing.T) {
	replayC04(t, []string{"RefundOnElapsedTime-P-K05-NO-DOUBLE-OUTCOME", "AllowConcurrentReservations-P-C04-ONE-ACTIVE-CUSTODY-RESERVATION"}, true)
}

func TestV04C04ProjectionReplay(t *testing.T) {
	replayC04(t, c04Traces, false)
}

func replayC04(t *testing.T, names []string, weakened bool) {
	dir := os.Getenv("V04_TRACES")
	if dir == "" {
		_, file, _, _ := runtime.Caller(0)
		dir = filepath.Join(filepath.Dir(file), "../signing/verification/evidence/V-04-2026-10-13/traces")
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var trace v04Trace
			if err = json.Unmarshal(raw, &trace); err != nil {
				t.Fatal(err)
			}
			// H-06: a counterexample replays with the claim model's
			// expectations, cut at the first step the claim model refuses.
			steps := trace.Steps
			if trace.Kind == "counterexample" && !weakened {
				steps = trace.Claim
			}
			w := newK05World(t)
			r := &v04Replay{t: t, w: w, knob: &knobReader{Reader: w.chain, changed: map[[32]byte]bool{}, reanchored: map[[32]byte]bool{}}, ticks: 20, attempts: map[int]*v04Attempt{}, signatures: map[[32]byte][64]byte{}}
			for _, res := range w.resolvers {
				c := *res
				c.Chain = r.knob
				r.resolvers = append(r.resolvers, &c)
			}
			r.requests[0] = w.request(50_000, p2wpkh(0x71))
			r.requests[1] = w.request(50_000, p2wpkh(0x72))
			r.base = w.head()
			defer func() {
				for _, a := range r.attempts {
					select {
					case <-a.release:
					default:
						close(a.release)
					}
					<-a.done
				}
			}()
			for _, step := range steps {
				var err error
				if step.Action == "Deliver" {
					var fields []json.RawMessage
					_ = json.Unmarshal(step.Fields, &fields)
					var attempt int
					var foreign bool
					_ = json.Unmarshal(fields[0], &attempt)
					_ = json.Unmarshal(fields[1], &foreign)
					err = r.deliver(attempt, foreign, step.State)
				} else {
					err = r.apply(step)
				}
				if weakened && step.Diverges {
					// The weakened model expects this step to succeed.
					if err == nil {
						t.Fatalf("weakened expectation held on current source at step %d", step.N)
					}
					t.Logf("V04_C04_CONTROL id=%s step=%d action=%s weakened-expectation-failed: %v", trace.ID, step.N, step.Action, err)
					break
				}
				if step.Diverges {
					// The claim model does not offer this step: the real
					// system must refuse it and leave the outcome unchanged.
					if !errors.Is(err, errRefused) {
						t.Fatalf("H-06 divergence step %d %s was not refused: %v", step.N, step.Action, err)
					}
					t.Logf("V04_C04_REPLAY id=%s divergence step=%d action=%s refused", trace.ID, step.N, step.Action)
					break
				}
				if err != nil {
					t.Fatalf("step %d %s: %v", step.N, step.Action, err)
				}
				if step.Action != "Step" && step.Action != "Start" {
					if err = r.compare(step); err != nil {
						t.Fatal(err)
					}
				}
			}
			sort.Strings(r.partial)
			reason := strings.ReplaceAll(strings.Join(r.partial, "; "), " ", "_")
			t.Logf("V04_C04_REPLAY id=%s class=partial scope=real-C04-real-host-real-worker-Core reason=%s steps=%d compared=%d delivered=%d", trace.ID, reason, len(steps), r.compared, r.delivered)
		})
	}
}
