package deposit

// V-07 trace replay (KC-VER 2.4). Test tooling only: it changes no
// production code and uses the controlled providers of fake_test.go.
//
// Each trace file comes from the V-07 Stateright model
// (verification/model). The environment part of every model state (chain
// reveals and finality, reservation, custody, Bitcoin spends, MTP) is applied
// to the controlled providers before each step; every host step then runs the
// real deposit-package component it names (planner, view and authority, main
// lock and grant guard, ledger, resolution attester) and its outcome is
// compared with the model's, followed by the host's durable state (main lock
// purposes, grant records, ledger kinds). Steps whose component cannot be
// reached from this package (executor internals, store claim, worker,
// maintainer; SCOPE.md L-01..L-05) are projected and make the trace partial.
//
// Weakened-model counterexamples replay with the claim model's expectations
// up to the first step the claim model does not offer (H-06).
//
// V07_TRACES selects the trace directory (default verification/traces) and
// V07_REPORT, when set, receives the replay report as JSON.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// Model encodings (verification/model/src/main.rs).
const (
	v07Unfinal  = 1
	v07Final    = 2
	v07Orphaned = 3

	v07ResUnfinal  = 1
	v07ResReserved = 2
	v07ResPaid     = 3
	v07ResSwept    = 4

	v07Planned      = 0
	v07Locked       = 1
	v07AttemptStage = 2
	v07Intent       = 3
	v07Claimed      = 4
	v07Granted      = 5
	v07Retained     = 6
	v07Result       = 7

	v07MTPBase   = 1_800_000_000
	v07TickSecs  = 1_000
	v07Locktime  = 2 // ticks
	v07HeadBase  = 100
	v07TickBlock = 10
	v07MainValue = 1_000_000
)

type v07Plan struct {
	Deps uint8 `json:"deps"`
	Main uint8 `json:"main"`
	Res  bool  `json:"res"`
	Over bool  `json:"over"`
}

type v07Deposit struct {
	Reveal  uint8 `json:"reveal"`
	Swept   bool  `json:"swept"`
	Credits uint8 `json:"credits"`
	Spender uint8 `json:"spender"`
}

type v07Lock struct {
	Purpose uint8  `json:"purpose"`
	Plans   uint64 `json:"plans"`
	Until   uint8  `json:"until"`
}

type v07Attempt struct {
	Used     bool    `json:"used"`
	Plan     v07Plan `json:"plan"`
	Round    uint8   `json:"round"`
	Stage    uint8   `json:"stage"`
	Terminal bool    `json:"terminal"`
	Failed   bool    `json:"failed"`
	Ledger   bool    `json:"ledger"`
	Claimed  bool    `json:"claimed"`
	Granted  bool    `json:"granted"`
	Retained bool    `json:"retained"`
	Accepted bool    `json:"accepted"`
	Late     bool    `json:"late"`
	Marker   bool    `json:"marker"`
}

type v07State struct {
	Tick         uint8         `json:"tick"`
	Round        uint8         `json:"round"`
	Up           bool          `json:"up"`
	Deposits     [2]v07Deposit `json:"deposits"`
	Res          uint8         `json:"res"`
	CertUntil    uint8         `json:"cert_until"`
	CustodyMain  uint8         `json:"custody_main"`
	CustodySpent [4]bool       `json:"custody_spent"`
	MainExists   [4]bool       `json:"main_exists"`
	MainSpender  [4]uint8      `json:"main_spender"`
	Locks        [4]v07Lock    `json:"locks"`
	Attempts     [2]v07Attempt `json:"attempts"`
}

func v07Initial() v07State {
	s := v07State{Up: true, CustodyMain: 1}
	s.MainExists[1] = true
	return s
}

type v07Step struct {
	N           int             `json:"n"`
	Action      string          `json:"action"`
	Fields      json.RawMessage `json:"fields"`
	Expect      string          `json:"expect"`
	ExpectState *v07State       `json:"expect_state"`
	Diverges    bool            `json:"diverges"`
}

type v07Trace struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Property   string    `json:"property"`
	Weakening  string    `json:"weakening"`
	Steps      []v07Step `json:"steps"`
	ClaimSteps []v07Step `json:"claim_model_steps"`
}

type v07Report struct {
	ID         string   `json:"id"`
	Property   string   `json:"property"`
	Weakening  string   `json:"weakening"`
	Replayed   int      `json:"steps_replayed"`
	Replay     string   `json:"replay"`
	Projected  []string `json:"projected_steps,omitempty"`
	Mismatches []string `json:"mismatches,omitempty"`
	Matched    bool     `json:"matched"`
	// DivergenceAction is the step where a weakened trace leaves the claim
	// model; DivergenceProjected says whether that step ran projected code.
	DivergenceAction    string `json:"divergence_action,omitempty"`
	DivergenceProjected bool   `json:"divergence_projected"`
	FinalExpect         string `json:"final_expect"`
}

// v07Replayer holds the controlled providers and the real components of one
// host. Crash rebuilds every component over the same durable journal.
type v07Replayer struct {
	t         *testing.T
	w         *world
	keys      [2][32]byte
	records   [2]Record
	reveals   [2]Reveal
	mains     [4]bitcoin.PreviousOutput
	r         [32]byte
	authority *Authority
	ledger    redemption.Ledger
	guard     Guard
	attester  *ResolutionAttester
	plans     [2]*signing.Plan
	rounds    [2]uint8
	// Projections of records this package cannot read (L-01, L-03): the
	// executor's result records and the store's attempt claims.
	results   map[string]bool
	claims    map[string]bool
	projected []string
}

func newV07Replayer(t *testing.T) *v07Replayer {
	w := newWorld(t)
	w.view.Policy.RefundMargin = v07TickSecs
	w.locktime = uint32(v07MTPBase + v07Locktime*v07TickSecs)
	rp := &v07Replayer{t: t, w: w, r: [32]byte{0x99}, results: map[string]bool{}, claims: map[string]bool{}}
	for d := 0; d < 2; d++ {
		key := w.deposit(byte(d+1), 100_000, 0, [20]byte{7})
		rp.keys[d], rp.records[d], rp.reveals[d] = key, w.chain.records[key], w.chain.reveals[key]
		delete(w.chain.records, key)
		delete(w.chain.reveals, key)
	}
	rp.mains[1] = w.fund(v07MainValue)
	script := rp.mains[1].PublicKeyScript
	rp.mains[2] = bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xab}}, Value: v07MainValue, PublicKeyScript: script}
	rp.mains[3] = bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xac}}, Value: v07MainValue, PublicKeyScript: script}
	rp.rebuild()
	return rp
}

// rebuild creates every in-memory host component again over the durable
// journal: a process restart.
func (rp *v07Replayer) rebuild() {
	w := rp.w
	w.view = &View{Chain: w.chain, Clock: w.clock, Policy: w.view.Policy}
	w.lock = &MainLock{Journal: w.journal}
	w.planner = Planner{Chain: w.chain, Bitcoin: w.btc, Funding: w.btc, Clock: w.clock, Lock: w.lock, Wallet: w.wallet, Genesis: w.planner.Genesis, Policy: w.view.Policy, FeeRate: 2, MaximumSatPerVByte: 10}
	rp.authority = &Authority{View: w.view}
	rp.ledger = redemption.Ledger{Journal: w.journal}
	rp.guard = Guard{Lock: w.lock}
	rp.attester = &ResolutionAttester{Chain: w.chain, Lock: w.lock, Authority: rp.authority, Key: testECDSA(rp.t), Seats: []uint16{1}}
}

func (rp *v07Replayer) head(s v07State) uint64 {
	return v07HeadBase + uint64(s.Tick)*v07TickBlock
}

func (rp *v07Replayer) mainKey(m uint8) [32]byte {
	return OutpointKey(rp.mains[m].Outpoint)
}

func (rp *v07Replayer) funding(d int) bitcoin.TransactionOutpoint {
	r := rp.records[d]
	return bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(r.FundingTransactionHash), OutputIndex: r.FundingOutputIndex}
}

// sync applies the environment of model state s to the controlled providers.
func (rp *v07Replayer) sync(s v07State) {
	w := rp.w
	h := rp.head(s)
	w.chain.head = h
	w.clock.set(v07MTPBase + uint64(s.Tick)*v07TickSecs)
	for _, n := range []uint64{h, h + 2} {
		w.chain.times[n] = v07MTPBase + uint64(s.Tick)*v07TickSecs
	}
	for d := 0; d < 2; d++ {
		key, dep := rp.keys[d], s.Deposits[d]
		switch dep.Reveal {
		case v07Unfinal, v07Final:
			rec, rev := rp.records[d], rp.reveals[d]
			rev.Block = h
			if dep.Reveal == v07Final {
				rev.Block = h - 2
			}
			rev.BlockHash = w.chain.hash(rev.Block)
			rec.SweptAt = 0
			if dep.Swept {
				rec.SweptAt = h
			}
			w.chain.records[key], w.chain.reveals[key] = rec, rev
		default:
			delete(w.chain.records, key)
			delete(w.chain.reveals, key)
		}
		w.btc.spent[rp.funding(d)] = dep.Spender != 0
	}
	active := s.Res == v07ResUnfinal || s.Res == v07ResReserved
	custody := redemption.Custody{StateVersion: 1, Value: v07MainValue}
	if s.CustodyMain != 0 {
		custody.MainOutpoint = rp.mainKey(s.CustodyMain)
	}
	if active {
		custody.ActiveReservation = rp.r
	}
	w.chain.custody = custody
	w.btc.outputs = nil
	for m := uint8(1); m < 4; m++ {
		w.chain.outputs[rp.mainKey(m)] = redemption.CustodyOutput{WalletID: w.id, Value: v07MainValue, Spent: s.CustodySpent[m]}
		w.btc.spent[rp.mains[m].Outpoint] = s.MainSpender[m] != 0
		if s.MainExists[m] && s.MainSpender[m] == 0 {
			w.btc.outputs = append(w.btc.outputs, rp.mains[m])
		}
	}
	descriptorHash, err := dkg.DescriptorHash(w.wallet)
	if err != nil {
		rp.t.Fatal(err)
	}
	certificate := redemption.Certificate{WalletID: w.id, DescriptorHash: descriptorHash, ConflictTxHash: [32]byte(rp.mains[1].Outpoint.TransactionHash), ConflictOutputIndex: rp.mains[1].Outpoint.OutputIndex}
	switch s.Res {
	case v07ResUnfinal, v07ResReserved:
		w.chain.intents[rp.r] = redemption.Intent{ID: rp.r, State: redemption.IntentReserved, Certificate: certificate}
	case v07ResPaid:
		w.chain.intents[rp.r] = redemption.Intent{ID: rp.r, State: redemption.IntentPaid, Certificate: certificate}
	case v07ResSwept:
		w.chain.intents[rp.r] = redemption.Intent{ID: rp.r, State: redemption.IntentConflictResolved, Certificate: certificate}
	default:
		delete(w.chain.intents, rp.r)
	}
}

func (rp *v07Replayer) project(name string) {
	rp.projected = append(rp.projected, name)
}

func v07AttemptID(p *signing.Plan, round uint8) string {
	id := SweepID(p)
	return fmt.Sprintf("%x/%d", id[:8], round)
}

// prevouts projects Executor.bitcoin (L-01): every prevout unspent.
func (rp *v07Replayer) prevouts(p *signing.Plan) bool {
	for _, d := range p.SweepDeposits() {
		if rp.w.btc.spent[d.Prevout.Outpoint] {
			return false
		}
	}
	if m := p.SweepMain(); m != nil && rp.w.btc.spent[m.Outpoint] {
		return false
	}
	return true
}

// signed projects Sweeper.signedRound (L-01): the sweep-signed marker in the
// real ledger, or an attempt entry whose executor result record is retained.
func (rp *v07Replayer) signed(p *signing.Plan) (bool, error) {
	entries, err := rp.ledger.Entries(ctx, SweepID(p))
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Kind == redemption.KindSweepSigned && e.Transaction == p.BitcoinTransactionHash() {
			return true, nil
		}
	}
	for _, e := range entries {
		if e.Kind == redemption.KindSweepAttempt && e.Transaction == p.BitcoinTransactionHash() {
			if round, err := strconv.ParseUint(e.Detail, 10, 8); err == nil && rp.results[v07AttemptID(p, uint8(round))] {
				return true, nil
			}
		}
	}
	return false, nil
}

func (rp *v07Replayer) entry(p *signing.Plan, kind redemption.Kind, round uint8) redemption.Entry {
	return redemption.Entry{Reservation: SweepID(p), Kind: kind, Block: rp.w.chain.head, Transaction: p.BitcoinTransactionHash(), Detail: strconv.FormatUint(uint64(round), 10)}
}

func v07Fields(raw json.RawMessage) []json.RawMessage {
	var many []json.RawMessage
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return []json.RawMessage{raw}
}

func v07Int(raw json.RawMessage) int {
	var v int
	_ = json.Unmarshal(raw, &v)
	return v
}

func v07Bool(raw json.RawMessage) bool {
	var v bool
	_ = json.Unmarshal(raw, &v)
	return v
}

// exec runs one step on the real host and reports whether the host accepted it.
func (rp *v07Replayer) exec(step v07Step, pre v07State, post *v07State) (bool, error) {
	w := rp.w
	h := rp.head(pre)
	finalized := h - 2
	f := v07Fields(step.Fields)
	switch step.Action {
	case "Reveal", "FinalizeReveal", "OrphanReveal", "Tick", "NextRound", "ForeignCertificate", "Admit",
		"FinalizeAdmission", "OrphanAdmission", "MinePayment", "MineSweep", "Refund", "Restart":
		return true, nil
	case "Attest":
		validUntil := v07HeadBase + uint64(pre.Tick+1)*v07TickBlock
		return v07ok(w.lock.AcquireRedemption(ctx, w.id, rp.mains[1].Outpoint, rp.r, validUntil))
	case "ReleaseUnadmitted":
		return v07ok(w.lock.ReleaseUnadmitted(ctx, w.chain, finalized, w.id, rp.mains[1].Outpoint, rp.r))
	case "PayGrant":
		// Projection: Guard.Grant for a payment plan needs redemption(r) on
		// the main (permitted); the payment plan itself is V-04's.
		rp.project(step.Action)
		current, err := w.lock.Current(ctx, w.id, rp.mainKey(1))
		if err != nil {
			return false, err
		}
		return current.Purpose == Redemption && current.Subject == rp.r, nil
	case "Close":
		m := uint8(v07Int(f[0]))
		return v07ok(w.lock.Close(ctx, w.chain, finalized, w.id, rp.mains[m].Outpoint, "finalized spend"))
	case "Plan", "PlanResolution":
		slot := v07Int(f[0])
		var resolution [32]byte
		over := false
		if step.Action == "PlanResolution" {
			resolution = rp.r
		} else {
			over = v07Bool(f[1])
		}
		w.planner.Fee = 0
		if over {
			w.planner.Fee = 100_000 // every share above maximumSweepFee
		}
		keys, err := w.planner.Pending(ctx, 0, h+2, 0)
		if err != nil {
			return false, nil
		}
		plan, err := w.planner.Build(ctx, h+2, keys, resolution)
		w.planner.Fee = 0
		if err != nil {
			return false, nil
		}
		rp.plans[slot], rp.rounds[slot] = plan, pre.Round
		if post != nil {
			if err := rp.samePlan(plan, post.Attempts[slot].Plan); err != nil {
				return true, err
			}
		}
		return true, nil
	case "Retry":
		// Sweeper.Resolve's decision for the previous slot's plan. The late
		// check, authorization and round check run on real code; signedRound
		// is projected (L-01).
		slot := v07Int(f[0])
		rp.project(step.Action)
		p := rp.plans[slot-1]
		late, err := rp.ledger.Has(ctx, SweepID(p), redemption.KindSweepLateQuarantined, [32]byte{})
		if err != nil || late {
			return false, err
		}
		if signed, err := rp.signed(p); err != nil || signed {
			return false, err
		}
		if _, err := rp.authority.AuthorizeSweep(ctx, p); err != nil {
			return false, nil
		}
		entries, err := rp.ledger.Entries(ctx, SweepID(p))
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.Kind == redemption.KindSweepAttempt && e.Detail == strconv.FormatUint(uint64(pre.Round), 10) {
				return false, nil
			}
		}
		rp.plans[slot], rp.rounds[slot] = p, pre.Round
		return true, nil
	case "Step":
		return rp.stage(v07Int(f[0]), pre)
	case "WorkerFail", "AcceptReadFail":
		rp.project(step.Action)
		return true, nil
	case "ResultFail":
		rp.project(step.Action)
		slot := v07Int(f[0])
		if v07Bool(f[1]) {
			if _, err := rp.authority.AuthorizeSweep(ctx, rp.plans[slot]); err != nil {
				return false, nil
			}
			rp.results[v07AttemptID(rp.plans[slot], rp.rounds[slot])] = true
		}
		return true, nil
	case "MarkerFail":
		rp.project(step.Action)
		slot := v07Int(f[0])
		if v07Bool(f[1]) {
			return v07ok(rp.ledger.Append(ctx, rp.entry(rp.plans[slot], redemption.KindSweepSigned, rp.rounds[slot])))
		}
		return true, nil
	case "Broadcast":
		rp.project(step.Action)
		slot := v07Int(f[0])
		p := rp.plans[slot]
		if signed, err := rp.signed(p); err != nil || !signed {
			return false, err
		}
		if _, err := rp.authority.AuthorizeSweep(ctx, p); err != nil {
			return false, nil
		}
		return v07ok(rp.ledger.Append(ctx, redemption.Entry{Reservation: SweepID(p), Kind: redemption.KindSweepBroadcast, Block: h, Transaction: p.BitcoinTransactionHash()}))
	case "ProveSweep", "ProvePayment", "Resubmit":
		rp.project(step.Action) // L-05: maintainer and contract effects
		return true, nil
	case "Crash":
		rp.rebuild()
		return true, nil
	}
	return false, fmt.Errorf("unknown action %s", step.Action)
}

// stage runs one sweeper/executor stage of a slot.
func (rp *v07Replayer) stage(slot int, pre v07State) (bool, error) {
	w := rp.w
	p := rp.plans[slot]
	a := pre.Attempts[slot]
	round := rp.rounds[slot]
	switch a.Stage {
	case v07Planned:
		if p.SweepResolution() != ([32]byte{}) {
			_, _, err := rp.attester.Attest(ctx, p, rp.head(pre)+50)
			return v07ok(err)
		}
		if m := p.SweepMain(); m != nil {
			return v07ok(w.lock.AcquireSweep(ctx, p.WalletID(), m.Outpoint, p.Commitment()))
		}
		return true, nil
	case v07Locked:
		return v07ok(rp.ledger.Append(ctx, rp.entry(p, redemption.KindSweepAttempt, round)))
	case v07AttemptStage:
		rp.project("Step:executor-start")
		if _, err := rp.authority.AuthorizeSweep(ctx, p); err != nil || !rp.prevouts(p) {
			return false, nil
		}
		return v07ok(rp.guard.Permit(ctx, p, SweepID(p)))
	case v07Intent:
		rp.project("Step:store-claim")
		id := v07AttemptID(p, round)
		if rp.claims[id] {
			return false, nil
		}
		rp.claims[id] = true
		return true, nil
	case v07Claimed:
		rp.project("Step:grant")
		if _, err := rp.authority.AuthorizeSweep(ctx, p); err != nil || !rp.prevouts(p) {
			return false, nil
		}
		return v07ok(rp.guard.Grant(ctx, p, SweepID(p), 0))
	case v07Granted:
		rp.project("Step:worker")
		return true, nil
	case v07Retained:
		rp.project("Step:acceptance")
		if _, err := rp.authority.AuthorizeSweep(ctx, p); err != nil {
			// Sweeper.Sign: a late signature is quarantined only after a
			// terminal refusal on the recheck.
			_, recheck := rp.authority.AuthorizeSweep(ctx, p)
			if !terminal(recheck) {
				return false, fmt.Errorf("acceptance refusal is not terminal: %v", recheck)
			}
			if err := rp.ledger.Append(ctx, rp.entry(p, redemption.KindSweepLateQuarantined, round)); err != nil {
				return false, err
			}
			return true, nil
		}
		rp.results[v07AttemptID(p, round)] = true
		return true, nil
	case v07Result:
		return v07ok(rp.ledger.Append(ctx, rp.entry(p, redemption.KindSweepSigned, round)))
	}
	return false, fmt.Errorf("stage %d has no step", a.Stage)
}

func v07ok(err error) (bool, error) {
	return err == nil, nil
}

// samePlan compares a real plan with the model's abstract plan.
func (rp *v07Replayer) samePlan(p *signing.Plan, m v07Plan) error {
	var deps uint8
	for _, d := range p.SweepDeposits() {
		for i := 0; i < 2; i++ {
			if d.Key == rp.keys[i] {
				deps |= 1 << i
			}
		}
	}
	var main uint8
	if sm := p.SweepMain(); sm != nil {
		for m := uint8(1); m < 4; m++ {
			if sm.Outpoint == rp.mains[m].Outpoint {
				main = m
			}
		}
	}
	res := p.SweepResolution() != ([32]byte{})
	if deps != m.Deps || main != m.Main || res != m.Res {
		return fmt.Errorf("plan deposits %b main %d resolution %v, model %b %d %v", deps, main, res, m.Deps, m.Main, m.Res)
	}
	return nil
}

var v07Purposes = map[uint8]Purpose{0: Free, 1: Redemption, 2: Resolution, 3: SweepMain, 4: Closed}

// compare checks the host's durable state against a model state.
func (rp *v07Replayer) compare(s v07State) []string {
	w := rp.w
	var out []string
	grants := map[uint8]int{}
	for slot, a := range s.Attempts {
		if a.Used && a.Granted && a.Plan.Main != 0 && rp.plans[slot] != nil {
			grants[a.Plan.Main]++
		}
	}
	for m := uint8(1); m < 4; m++ {
		history, err := w.lock.History(ctx, w.id, rp.mainKey(m))
		if err != nil {
			return append(out, err.Error())
		}
		if got, want := state(history).Purpose, v07Purposes[s.Locks[m].Purpose]; got != want {
			out = append(out, fmt.Sprintf("main %d lock %s, model %s", m, got, want))
		}
		n := 0
		for _, e := range history {
			if e.Grant {
				n++
			}
		}
		if n != grants[m] {
			out = append(out, fmt.Sprintf("main %d grant records %d, model %d", m, n, grants[m]))
		}
	}
	for slot, a := range s.Attempts {
		p := rp.plans[slot]
		if !a.Used || p == nil {
			continue
		}
		entries, err := rp.ledger.Entries(ctx, SweepID(p))
		if err != nil {
			return append(out, err.Error())
		}
		has := map[redemption.Kind]bool{}
		attempt := false
		for _, e := range entries {
			has[e.Kind] = true
			if e.Kind == redemption.KindSweepAttempt && e.Detail == strconv.FormatUint(uint64(a.Round), 10) {
				attempt = true
			}
		}
		// The ledger is per plan; the model flags are per attempt.
		late, marker := false, false
		for _, b := range s.Attempts {
			if b.Used && b.Plan == a.Plan {
				late = late || b.Late
				marker = marker || b.Marker
			}
		}
		if attempt != a.Ledger {
			out = append(out, fmt.Sprintf("slot %d sweep-attempt %v, model %v", slot, attempt, a.Ledger))
		}
		if has[redemption.KindSweepLateQuarantined] != late {
			out = append(out, fmt.Sprintf("slot %d sweep-late-quarantined %v, model %v", slot, has[redemption.KindSweepLateQuarantined], late))
		}
		if has[redemption.KindSweepSigned] != marker {
			out = append(out, fmt.Sprintf("slot %d sweep-signed %v, model %v", slot, has[redemption.KindSweepSigned], marker))
		}
	}
	return out
}

func (rp *v07Replayer) replay(tr v07Trace) v07Report {
	steps := tr.Steps
	if tr.Weakening != "None" {
		steps = tr.ClaimSteps
	}
	rep := v07Report{ID: tr.ID, Property: tr.Property, Weakening: tr.Weakening}
	pre := v07Initial()
	for _, step := range steps {
		rp.sync(pre)
		before := len(rp.projected)
		accepted, err := rp.exec(step, pre, step.ExpectState)
		if step.Diverges {
			var fields bytes.Buffer
			_ = json.Compact(&fields, step.Fields)
			rep.DivergenceAction = fmt.Sprintf("%d %s %s", step.N, step.Action, fields.String())
			rep.DivergenceProjected = len(rp.projected) > before
		}
		rep.Replayed++
		rep.FinalExpect = step.Expect
		if err != nil {
			rep.Mismatches = append(rep.Mismatches, fmt.Sprintf("step %d %s: %v", step.N, step.Action, err))
		}
		expect := step.Expect
		if step.Action == "Step" && step.ExpectState != nil && expect != "refused" {
			// A stage guard that refuses ends the model attempt as failed:
			// in the host's vocabulary that step is a refusal.
			slot := v07Int(v07Fields(step.Fields)[0])
			a, b := pre.Attempts[slot], step.ExpectState.Attempts[slot]
			guard := a.Stage == v07Planned || a.Stage == v07AttemptStage || a.Stage == v07Intent || a.Stage == v07Claimed
			if guard && b.Terminal && b.Failed && !b.Late {
				expect = "refused"
			}
		}
		switch expect {
		case "transition", "different-state":
			if !accepted {
				rep.Mismatches = append(rep.Mismatches, fmt.Sprintf("step %d %s: model transitions, host refused", step.N, step.Action))
			}
		case "refused":
			if accepted {
				rep.Mismatches = append(rep.Mismatches, fmt.Sprintf("step %d %s: model refuses, host accepted", step.N, step.Action))
			}
		}
		if step.ExpectState != nil && step.Expect != "refused" {
			for _, m := range rp.compare(*step.ExpectState) {
				rep.Mismatches = append(rep.Mismatches, fmt.Sprintf("step %d %s: %s", step.N, step.Action, m))
			}
			pre = *step.ExpectState
		}
		if step.Diverges {
			break
		}
	}
	sort.Strings(rp.projected)
	seen := map[string]bool{}
	for _, p := range rp.projected {
		if !seen[p] {
			seen[p] = true
			rep.Projected = append(rep.Projected, p)
		}
	}
	rep.Replay = "full"
	if len(rep.Projected) > 0 {
		rep.Replay = "partial"
	}
	rep.Matched = len(rep.Mismatches) == 0
	return rep
}

func TestV07Replay(t *testing.T) {
	dir := os.Getenv("V07_TRACES")
	explicit := dir != ""
	if !explicit {
		dir = filepath.Join("verification", "traces")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		if !explicit {
			t.Skipf("no V-07 traces in %s yet (run verification/run.py model traces)", dir)
		}
		t.Fatalf("no V-07 traces in %s: %v", dir, err)
	}
	sort.Strings(files)
	var reports []v07Report
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var tr v07Trace
		if err = json.Unmarshal(raw, &tr); err != nil {
			t.Fatal(err)
		}
		t.Run(tr.ID, func(t *testing.T) {
			rep := newV07Replayer(t).replay(tr)
			reports = append(reports, rep)
			for _, m := range rep.Mismatches {
				t.Error(m)
			}
		})
	}
	if out := os.Getenv("V07_REPORT"); out != "" {
		raw, err := json.MarshalIndent(reports, "", "  ")
		if err == nil {
			err = os.WriteFile(out, raw, 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(reports) != len(files) {
		t.Fatalf("replayed %d of %d traces", len(reports), len(files))
	}
}
