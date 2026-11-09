package movement

// V-08 trace replay (KC-VER 2.4). The model in verification/model writes
// traces; this test replays them on the real K-07 host code in this package:
// Commitments.Commit, Planner.Movement, Planner.IncomingSweep,
// Authority.AuthorizeSweep (grant and broadcast recheck) and Closure.Close.
// Environment steps drive the controlled C-06/C-09 chain (the `fake` of
// movement_test.go, wrapped below) from the model's expected state. Steps
// with no reachable real seam (durable claim, worker signature, executor
// prevout check, crash, stale restore, defense, key deletion) are applied as
// modeled, and a trace with any of them counts as partial (H-05).
// Weakened-model counterexamples replay with the claim model's expectations,
// cut at the first step the claim model does not offer (H-06).
//
// Runs only when V08_TRACES names a trace directory
// (pkg/frost/movement/verification/run.py replay).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// v08Sweep and v08State mirror the model's serialized Sweep and State.
type v08Sweep struct {
	Mask          uint8 `json:"mask"`
	Wallet        uint8 `json:"wallet"`
	Main          bool  `json:"main"`
	AllEscalated  bool  `json:"all_escalated"`
	Mined, Proven bool
}

type v08State struct {
	Tick        uint8       `json:"tick"`
	WS          [3]uint8    `json:"ws"`
	Value       [3]bool     `json:"value"`
	MainSpent   [3]bool     `json:"main_spent"`
	Commit      uint8       `json:"commit"`
	CTarget     uint8       `json:"ctarget"`
	Escalated   bool        `json:"escalated"`
	Inc         [2]uint8    `json:"inc"`
	ITarget     [2]uint8    `json:"itarget"`
	IFinal      [2]bool     `json:"ifinal"`
	First       uint8       `json:"first"`
	Sweeps      [2]v08Sweep `json:"sweeps"`
	Challenge   [3]uint8    `json:"challenge"`
	MvPlan      uint8       `json:"mv_plan"`
	SwPlan      v08Sweep    `json:"sw_plan"`
	ClPhase     uint8       `json:"cl_phase"`
	ClWallet    uint8       `json:"cl_wallet"`
	Quarantined bool        `json:"quarantined"`
}

type v08Step struct {
	N         int             `json:"n"`
	Action    string          `json:"action"`
	Fields    json.RawMessage `json:"fields"`
	Expect    string          `json:"expect"`
	State     v08State        `json:"expect_state"`
	Diverges  bool            `json:"diverges"`
	WalletArg *uint8          `json:"wallet"`
}

type v08Trace struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Profile   string    `json:"profile"`
	Property  string    `json:"property"`
	Weakening string    `json:"weakening"`
	Steps     []v08Step `json:"steps"`
	Claim     []v08Step `json:"claim_model_steps"`
}

// Model-only steps: no real seam reachable from this package without
// production changes (SCOPE-2026-11-06.md section 1, S5).
var v08ModelOnly = map[string]string{
	"MvClaim":      "durable claim (signing.Executor, store)",
	"MvSign":       "worker signature (snowfall engine)",
	"SwClaim":      "durable claim (signing.Executor, store)",
	"SwSign":       "worker signature (snowfall engine)",
	"Crash":        "process or host crash (V-01 store)",
	"RestoreStale": "stale store image (V-01 fence)",
	"Reconcile":    "operator reconciliation (V-01 A-09)",
	"Defend":       "C-08 defense (K-08, V-09)",
	"DeleteKey":    "weakening only",
}

// v08Chain is the controlled C-06/C-09 provider: the movement_test.go fake
// with obligations by head (finalized or current) and a finalize knob.
type v08Chain struct {
	*fake
	fin, cur     map[[32]byte]uint64
	finalizeOK   bool
	afterRevert  State
	notified     bool
	descriptorOf map[[20]byte][32]byte
}

func (c *v08Chain) Obligations(_ context.Context, at uint64, w [32]byte) (uint64, error) {
	if at < c.head {
		return c.fin[w], nil
	}
	return c.cur[w], nil
}

func (c *v08Chain) SubmitCommitment(ctx context.Context, w [32]byte, seat uint16, targets [][20]byte) error {
	if err := c.fake.SubmitCommitment(ctx, w, seat, targets); err != nil {
		return err
	}
	commitment := c.commitments[w]
	for _, l := range targets {
		commitment.DescriptorHashes = append(commitment.DescriptorHashes, c.descriptorOf[l])
	}
	c.commitments[w] = commitment
	return nil
}

func (c *v08Chain) FinalizeClosing(_ context.Context, w [32]byte) error {
	c.notified = true
	l := Label(w)
	wallet := c.wallets[l]
	if c.finalizeOK {
		wallet.State = StateClosed
		c.wallets[l] = wallet
		return nil
	}
	// A revert; the wallet's state is what the model says it is after it
	// (Closed when an earlier notice already closed it, a lost reply).
	wallet.State = c.afterRevert
	c.wallets[l] = wallet
	return errors.New("FROST closure obligations (controlled revert)")
}

type v08World struct {
	t       *testing.T
	c       *v08Chain
	view    *View
	wallets [3]Wallet
	main    bitcoin.PreviousOutput
	// mains[w] is the wallet's current main after a credit.
	mains   [3]*bitcoin.PreviousOutput
	slots   [2][32]byte
	mvPlan  *signing.Plan
	swPlan  *signing.Plan
	journal *journal
	unspent []bitcoin.PreviousOutput
	closure struct {
		fin, cur uint64
	}
}

var v08IncomingTx = [2][32]byte{{0x55, 0x08}, {0x66, 0x08}}
var v08IncomingValue = [2]uint64{290_000, 100_000}

func v08Key(o int) [32]byte {
	return OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(v08IncomingTx[o]), OutputIndex: 0})
}

func newV08World(t *testing.T) *v08World {
	t.Helper()
	f := &fake{head: 100, hashes: map[uint64][32]byte{}, wallets: map[[20]byte]Wallet{}, lifecycles: map[[32]byte]Lifecycle{}, commitments: map[[32]byte]Commitment{}, logs: map[[32]byte]CommitmentLog{}, custody: map[[32]byte]redemption.Custody{}, outputs: map[[32]byte]redemption.CustodyOutput{}, incoming: map[[32]byte]Incoming{}, obligations: map[[32]byte]uint64{}, params: Parameters{MovingFundsTxMaxTotalFee: 5_000, MovingFundsDustThreshold: 20_000, MovedFundsSweepTxMaxTotalFee: 5_000, WalletMaxBtcTransfer: 1_000_000_000}}
	c := &v08Chain{fake: f, fin: map[[32]byte]uint64{}, cur: map[[32]byte]uint64{}, descriptorOf: map[[20]byte][32]byte{}}
	w := &v08World{t: t, c: c, view: &View{Chain: c, Policy: Policy{FinalityBlocks: 2}}, journal: &journal{records: map[[32]byte][]byte{}}}
	var targets []Wallet
	for i, secret := range []byte{10, 20, 21} {
		d := descriptor(t, secret)
		h, err := dkg.DescriptorHash(d)
		if err != nil {
			t.Fatal(err)
		}
		wallet := Wallet{Descriptor: d, ID: dkg.WalletID(d.OutputKey), DescriptorHash: h, CapabilityVersion: 1}
		c.descriptorOf[Label(wallet.ID)] = h
		f.ready = append(f.ready, wallet.ID)
		if i == 0 {
			w.wallets[0] = wallet
		} else {
			targets = append(targets, wallet)
		}
	}
	// T has the lower label (the model's lowest eligible target).
	sort.Slice(targets, func(i, j int) bool {
		a, b := Label(targets[i].ID), Label(targets[j].ID)
		return bytes.Compare(a[:], b[:]) < 0
	})
	w.wallets[1], w.wallets[2] = targets[0], targets[1]
	script, _ := bitcoin.PayToTaproot(w.wallets[0].Descriptor.OutputKey)
	w.main = bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{77}, OutputIndex: 0}, Value: 300_000, PublicKeyScript: script}
	return w
}

var v08States = map[uint8]State{1: StateLive, 2: StateMovingFunds, 3: StateClosing, 4: StateClosed}
var v08Incoming = map[uint8]IncomingState{1: IncomingPending, 2: IncomingEscalated, 3: IncomingSwept}

// ledger is the model's C-09 rows for wallet w (the provider's knob).
func v08Ledger(s v08State, w int, finalized bool) uint64 {
	var open uint64
	if s.Value[w] {
		open |= OpenCustody
	}
	for o := 0; o < 2; o++ {
		if int(s.ITarget[o]) == w && (s.Inc[o] == 1 || s.Inc[o] == 2) && (!finalized || s.IFinal[o]) {
			open |= OpenPendingIncoming
		}
	}
	if (s.Commit == 1 && !finalized) || s.Commit == 2 {
		if int(s.CTarget) == w {
			open |= OpenCommittedIncoming
		}
		if w == 0 {
			open |= OpenUnprovenCommitment
		}
	}
	if (s.Challenge[w] == 1 && !finalized) || s.Challenge[w] == 2 {
		open |= OpenChallenge
	}
	return open
}

// sync sets the controlled chain and Bitcoin from a model state.
func (w *v08World) sync(s v08State) {
	f := w.c.fake
	eligible := uint64(0)
	for i := 0; i < 3; i++ {
		wallet := w.wallets[i]
		wallet.State = v08States[s.WS[i]]
		f.wallets[Label(wallet.ID)] = wallet
		if i > 0 && s.WS[i] == 1 {
			eligible++
		}
		w.c.fin[wallet.ID] = v08Ledger(s, i, true)
		w.c.cur[wallet.ID] = v08Ledger(s, i, false)
	}
	f.counters.EligibleTargets = eligible
	var unspentOuts []bitcoin.PreviousOutput
	source := w.wallets[0].ID
	if s.Value[0] && w.mains[0] == nil {
		key := OutpointKey(w.main.Outpoint)
		f.custody[source] = redemption.Custody{StateVersion: 2, Value: 300_000, MainOutpoint: key}
		f.outputs[key] = redemption.CustodyOutput{WalletID: source, Value: 300_000}
		if !s.MainSpent[0] {
			unspentOuts = append(unspentOuts, w.main)
		}
	}
	for i := 0; i < 3; i++ {
		id := w.wallets[i].ID
		if i == 0 && s.Value[0] && w.mains[0] == nil {
			continue
		}
		if !s.Value[i] {
			f.custody[id] = redemption.Custody{StateVersion: 3}
			continue
		}
		m := w.mains[i]
		if m == nil {
			w.t.Fatalf("model credits wallet %d with no replayed main", i)
		}
		key := OutpointKey(m.Outpoint)
		f.custody[id] = redemption.Custody{StateVersion: 3, Value: uint64(m.Value), MainOutpoint: key}
		f.outputs[key] = redemption.CustodyOutput{WalletID: id, Value: uint64(m.Value)}
		if !s.MainSpent[i] {
			unspentOuts = append(unspentOuts, *m)
		}
	}
	w.unspent = unspentOuts
	delete(f.commitments, source)
	delete(f.logs, source)
	if s.Commit != 0 {
		label := Label(w.wallets[s.CTarget].ID)
		c := Commitment{MainOutpoint: OutpointKey(w.main.Outpoint), MaxFee: f.params.MovingFundsTxMaxTotalFee, Targets: [][20]byte{label}, DescriptorHashes: [][32]byte{w.c.descriptorOf[label]}, Escalated: s.Escalated}
		block := f.head
		if s.Commit >= 2 {
			block = f.head - 2
		}
		if s.Commit == 3 {
			c.ProvenAt = 1
		}
		f.commitments[source] = c
		f.logs[source] = CommitmentLog{Log: Log{Block: block, BlockHash: f.hash(block)}, Wallet: source}
	}
	f.incoming = map[[32]byte]Incoming{}
	f.incomingLog = nil
	order := []int{0, 1}
	if s.First == 2 {
		order = []int{1, 0}
	}
	for _, o := range order {
		if s.Inc[o] == 0 {
			continue
		}
		target := w.wallets[s.ITarget[o]].ID
		key := v08Key(o)
		f.incoming[key] = Incoming{Target: target, Source: source, Value: v08IncomingValue[o], State: v08Incoming[s.Inc[o]], SweepFeeCap: 5_000}
		block := f.head
		if s.IFinal[o] {
			block = f.head - 2
		}
		f.incomingLog = append(f.incomingLog, IncomingLog{Log: Log{Block: block, BlockHash: f.hash(block)}, Outpoint: key, Target: target, Source: source, Value: v08IncomingValue[o], TransactionHash: v08IncomingTx[o], OutputIndex: 0})
	}
}

func (w *v08World) planner(i int) Planner {
	return Planner{View: w.view, Bitcoin: unspent{w.unspent}, Wallet: w.wallets[i].Descriptor, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: 2, MaximumSatPerVByte: 20}
}

func (w *v08World) authorize(plan *signing.Plan) error {
	_, err := (&Authority{View: w.view}).AuthorizeSweep(context.Background(), plan)
	return err
}

func (w *v08World) planSweep(i int, want v08Sweep) (*signing.Plan, error) {
	plan, err := w.planner(i).IncomingSweep(context.Background())
	if err != nil {
		return nil, err
	}
	var mask uint8
	for _, in := range plan.IncomingOutputs() {
		for o := 0; o < 2; o++ {
			if OutpointKey(in.Outpoint) == v08Key(o) {
				mask |= 1 << o
			}
		}
	}
	if mask != want.Mask || (plan.MovementMain() != nil) != want.Main {
		return nil, fmt.Errorf("incoming plan inputs %b main %v, model %b main %v", mask, plan.MovementMain() != nil, want.Mask, want.Main)
	}
	return plan, nil
}

func (w *v08World) planMovement(target uint8) (*signing.Plan, error) {
	plan, err := w.planner(0).Movement(context.Background())
	if err != nil {
		return nil, err
	}
	got := plan.MovementTargets()
	if len(got) != 1 || got[0].Label != Label(w.wallets[target].ID) {
		return nil, fmt.Errorf("movement plan pays %x, model %d", got, target)
	}
	return plan, nil
}

// closeWith runs Closure.Close with the recorded reads and the finalize knob.
func (w *v08World) closeWith(wallet uint8, finalizeOK bool, after v08State, archive bool) (error, []string) {
	id := w.wallets[wallet].ID
	w.c.fin[id], w.c.cur[id] = w.closure.fin, w.closure.cur
	w.c.finalizeOK = finalizeOK
	w.c.afterRevert = v08States[after.WS[wallet]]
	w.c.notified = false
	var hooks []string
	c := Closure{Chain: w.c, Policy: Policy{FinalityBlocks: 2}, Journal: w.journal, boundary: func(name string) error {
		hooks = append(hooks, name)
		if name == "before-archive" && !archive {
			return errors.New("modeled crash before the archive")
		}
		return nil
	}}
	return c.Close(context.Background(), id), hooks
}

type v08Outcome struct {
	ID          string   `json:"id"`
	Class       string   `json:"class"`
	Steps       int      `json:"steps_replayed"`
	Compared    int      `json:"steps_compared_on_real_code"`
	ModelOnly   []string `json:"model_only_steps,omitempty"`
	Cut         bool     `json:"cut_at_divergence"`
	Mismatch    string   `json:"mismatch,omitempty"`
	Expectation string   `json:"expectations"`
}

func replayV08(t *testing.T, tr v08Trace) v08Outcome {
	steps, expectation := tr.Steps, "claim"
	if tr.Weakening != "None" && tr.Weakening != "ProposeEscalated" {
		steps, expectation = tr.Claim, "claim (weakened trace cut at divergence)"
	}
	out := v08Outcome{ID: tr.ID, Expectation: expectation}
	if tr.Weakening == "ProposeEscalated" {
		out.Class, out.Mismatch = "skipped", "probe trace: the proposal trigger it relaxes is in pkg/tbtcpg and pkg/chain/ethereum, outside this seam"
		return out
	}
	w := newV08World(t)
	prev := v08State{WS: [3]uint8{2, 1, 1}, Value: [3]bool{true, false, false}}
	w.sync(prev)
	ctx := context.Background()
	modelOnly := map[string]bool{}
	fail := func(step v08Step, format string, args ...interface{}) v08Outcome {
		out.Class = "mismatch"
		out.Mismatch = fmt.Sprintf("step %d %s: %s", step.N, step.Action, fmt.Sprintf(format, args...))
		return out
	}
	for idx, step := range steps {
		if step.Diverges {
			// The claim model refuses or changes this step differently; the
			// replay stops before it (H-06).
			out.Cut = true
			break
		}
		out.Steps++
		arg := func() uint8 {
			var x uint8
			_ = json.Unmarshal(step.Fields, &x)
			return x
		}
		s := step.State
		switch step.Action {
		case "Commit":
			w.sync(prev)
			targets, err := Commitments{Chain: w.c, Planner: w.planner(0), Seat: 0}.Commit(ctx)
			if err != nil || len(targets) != 1 || targets[0] != Label(w.wallets[s.CTarget].ID) {
				return fail(step, "Commit returned %x, %v; model target %d", targets, err, s.CTarget)
			}
			out.Compared++
		case "MvPlan":
			w.sync(prev)
			plan, err := w.planMovement(s.MvPlan)
			if err != nil {
				return fail(step, "%v", err)
			}
			w.mvPlan = plan
			out.Compared++
		case "MvGrant", "MvBroadcast":
			w.sync(prev)
			if err := w.authorize(w.mvPlan); err != nil {
				return fail(step, "authority refused: %v", err)
			}
			out.Compared++
		case "MvRefuse":
			var authorized bool
			_ = json.Unmarshal(step.Fields, &authorized)
			w.sync(prev)
			err := w.authorize(w.mvPlan)
			if (err == nil) != authorized {
				return fail(step, "authority %v, model authorized %v", err, authorized)
			}
			if authorized {
				// The refusal is the executor's prevout check: modeled.
				modelOnly["MvRefuse(prevout)"] = true
			}
			out.Compared++
		case "MvAtomic":
			w.sync(prev)
			plan, err := w.planMovement(prev.CTarget)
			if err == nil {
				err = w.authorize(plan)
			}
			if err != nil {
				return fail(step, "%v", err)
			}
			w.mvPlan = plan
			modelOnly["MvAtomic(claim, signature, broadcast)"] = true
			out.Compared++
		case "SwPlan":
			w.sync(prev)
			plan, err := w.planSweep(int(arg()), s.SwPlan)
			if err != nil {
				return fail(step, "%v", err)
			}
			w.swPlan = plan
			// The proposal trigger (a Pending output) is pkg/tbtcpg's.
			modelOnly["SwPlan(proposal trigger)"] = true
			out.Compared++
		case "SwAtomic":
			w.sync(prev)
			i := int(arg())
			var want v08Sweep
			for k := 0; k < 2; k++ {
				if prev.Sweeps[k].Mask == 0 {
					want = s.Sweeps[k]
					break
				}
			}
			plan, err := w.planSweep(i, want)
			if err == nil {
				err = w.authorize(plan)
			}
			if err != nil {
				return fail(step, "%v", err)
			}
			w.swPlan = plan
			modelOnly["SwAtomic(claim, signature, broadcast)"] = true
			out.Compared++
			w.broadcastSweep(prev, plan)
		case "SwGrant":
			w.sync(prev)
			if err := w.authorize(w.swPlan); err != nil {
				return fail(step, "authority refused: %v", err)
			}
			out.Compared++
		case "SwRefuse":
			var authorized bool
			_ = json.Unmarshal(step.Fields, &authorized)
			w.sync(prev)
			err := w.authorize(w.swPlan)
			if (err == nil) != authorized {
				return fail(step, "authority %v, model authorized %v", err, authorized)
			}
			if authorized {
				modelOnly["SwRefuse(prevout)"] = true
			}
			out.Compared++
		case "SwBroadcast":
			w.sync(prev)
			if err := w.authorize(w.swPlan); err != nil {
				return fail(step, "broadcast recheck refused: %v", err)
			}
			out.Compared++
			w.broadcastSweep(prev, w.swPlan)
		case "SwAbandon":
			w.sync(prev)
			if err := w.authorize(w.swPlan); err == nil {
				return fail(step, "broadcast recheck authorized an abandoned sweep")
			}
			out.Compared++
		case "ProveSweep":
			var f []json.RawMessage
			_ = json.Unmarshal(step.Fields, &f)
			var i int
			_ = json.Unmarshal(f[0], &i)
			wallet := prev.Sweeps[i].Wallet
			script, _ := bitcoin.PayToTaproot(w.wallets[wallet].Descriptor.OutputKey)
			value := int64(0)
			for o := 0; o < 2; o++ {
				if prev.Sweeps[i].Mask&(1<<o) != 0 {
					value += int64(v08IncomingValue[o])
				}
			}
			if prev.Sweeps[i].Main && w.mains[wallet] != nil {
				value += w.mains[wallet].Value
			}
			w.mains[wallet] = &bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(w.slots[i]), OutputIndex: 0}, Value: value - 1_000, PublicKeyScript: script}
		case "LateCredit":
			i := arg()
			script, _ := bitcoin.PayToTaproot(w.wallets[i].Descriptor.OutputKey)
			w.mains[i] = &bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xde, i}, OutputIndex: 0}, Value: 50_000, PublicKeyScript: script}
		case "ClRead":
			w.closure.fin = v08Ledger(prev, int(arg()), true)
			if w.closure.fin != 0 {
				return fail(step, "model read a non-empty finalized ledger %b as clear", w.closure.fin)
			}
		case "ClRecheck", "ClRecheckRefuse":
			w.closure.cur = v08Ledger(prev, int(prev.ClWallet), false)
			if step.Action == "ClRecheckRefuse" {
				w.sync(prev)
				err, _ := w.closeWith(prev.ClWallet, false, s, false)
				if !errors.Is(err, ErrObligations) || w.c.notified {
					return fail(step, "Close with a late arrival returned %v (notified %v)", err, w.c.notified)
				}
				out.Compared++
			}
		case "ClNotify":
			var closed bool
			_ = json.Unmarshal(step.Fields, &closed)
			w.sync(prev)
			archive := idx+1 < len(steps) && steps[idx+1].Action == "ClArchive"
			err, hooks := w.closeWith(prev.ClWallet, closed, s, archive)
			reached := strings.Contains(strings.Join(hooks, ","), "closure-notified")
			if (s.ClPhase == 3) != reached {
				return fail(step, "Close %v hooks %v; model continues %v", err, hooks, s.ClPhase == 3)
			}
			if s.ClPhase == 3 && archive && err != nil {
				return fail(step, "Close did not archive: %v", err)
			}
			out.Compared++
		case "ClArchive":
			ok, err := IsArchived(ctx, w.journal, w.wallets[prev.ClWallet].ID)
			if err != nil || !ok {
				return fail(step, "no archive record: %v", err)
			}
			out.Compared++
		default:
			if why, ok := v08ModelOnly[step.Action]; ok {
				modelOnly[step.Action+" ("+why+")"] = true
			}
			// Every other step is an environment step: the next sync applies it.
		}
		prev = s
	}
	for k := range modelOnly {
		out.ModelOnly = append(out.ModelOnly, k)
	}
	sort.Strings(out.ModelOnly)
	if len(out.ModelOnly) == 0 {
		out.Class = "full"
	} else {
		out.Class = "partial"
	}
	return out
}

func (w *v08World) broadcastSweep(prev v08State, plan *signing.Plan) {
	for k := 0; k < 2; k++ {
		if prev.Sweeps[k].Mask == 0 {
			w.slots[k] = plan.BitcoinTransactionHash()
			return
		}
	}
}

func TestV08TraceReplay(t *testing.T) {
	dir := os.Getenv("V08_TRACES")
	if dir == "" {
		t.Skip("V08_TRACES not set: run pkg/frost/movement/verification/run.py OUT replay")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no traces in %s: %v", dir, err)
	}
	sort.Strings(files)
	var outcomes []v08Outcome
	counts := map[string]int{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var tr v08Trace
		if err = json.Unmarshal(raw, &tr); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		o := replayV08(t, tr)
		counts[o.Class]++
		outcomes = append(outcomes, o)
		if o.Class == "mismatch" {
			t.Errorf("%s: %s", o.ID, o.Mismatch)
		} else {
			t.Logf("%s: %s (%d steps, %d compared)", o.ID, o.Class, o.Steps, o.Compared)
		}
	}
	if report := os.Getenv("V08_REPLAY_REPORT"); report != "" {
		raw, _ := json.MarshalIndent(map[string]interface{}{"counts": counts, "traces": outcomes}, "", "  ")
		if err := os.WriteFile(report, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("replay counts: %v", counts)
}
