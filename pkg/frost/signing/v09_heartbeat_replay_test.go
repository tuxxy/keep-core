package signing

// KC-VER V-09 replay of the hb profile (P-K08-HEARTBEAT-DOMAIN, and the
// record-before-grant half of P-K08-EVIDENCE-RETAINED) on the real
// HeartbeatSigner and Executor. Test-only; see
// pkg/frost/enforcement/verification/SCOPE.md section 5.
//
// FIXTURES (labelled, KC-VER H-03): the engine is a stub that pauses at two
// fixture boundaries ("engine-before-claim", "engine-claimed"), keeps the
// store's exclusive claim set as a map (the real store.Claim refuses an
// existing id; S-K02-HOST-4 covers it), and signs with the test key. The
// heartbeat authority is the existing nonceAuthority with the model tick as
// the current nonce. Everything else is the real signer and executor code,
// driven at their existing test-only boundary hooks.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

type v09Step struct {
	N           int             `json:"n"`
	Action      string          `json:"action"`
	Fields      json.RawMessage `json:"fields"`
	Offered     *bool           `json:"offered"`
	Diverges    bool            `json:"diverges"`
	ExpectState json.RawMessage `json:"expect_state"`
}

type v09Trace struct {
	ID         string    `json:"id"`
	Profile    string    `json:"profile"`
	Kind       string    `json:"kind"`
	Property   string    `json:"property"`
	Weakening  string    `json:"weakening"`
	Steps      []v09Step `json:"steps"`
	ClaimSteps []v09Step `json:"claim_model_steps"`
}

// v09Replayed returns the steps replayed on the real code: the trace itself
// for the claim model, the claim-model expectations for a weakened trace
// (H-06), stopping after the first step the claim model does not offer.
func (t v09Trace) v09Replayed() []v09Step {
	if t.Weakening == "None" {
		return t.Steps
	}
	var out []v09Step
	for _, s := range t.ClaimSteps {
		out = append(out, s)
		if s.Offered != nil && !*s.Offered {
			break
		}
	}
	return out
}

type v09Report struct {
	ID       string `json:"id"`
	Replay   string `json:"replay"`
	Steps    int    `json:"steps_replayed"`
	Result   string `json:"result"`
	Detail   string `json:"detail,omitempty"`
	Weakened bool   `json:"weakened"`
}

func v09Traces(t *testing.T, profile string) []v09Trace {
	t.Helper()
	dir := os.Getenv("V09_TRACES")
	if dir == "" {
		dir = filepath.Join("..", "enforcement", "verification", "traces")
	}
	files, err := filepath.Glob(filepath.Join(dir, profile+"-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s traces in %s: %v", profile, dir, err)
	}
	sort.Strings(files)
	var out []v09Trace
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var tr v09Trace
		if err = json.Unmarshal(raw, &tr); err != nil {
			t.Fatal(f, err)
		}
		if tr.Profile == profile {
			out = append(out, tr)
		}
	}
	return out
}

func v09WriteReport(t *testing.T, reports []v09Report) {
	t.Helper()
	if path := os.Getenv("V09_REPLAY_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(reports, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var errV09Crash = errors.New("v09 replay: process crash at boundary")
var errV09Claimed = errors.New("v09 replay fixture: attempt already claimed")

type v09Proc struct {
	events chan string
	cmds   chan bool
	phase  int
	err    error
	phases map[string]int
}

func (p *v09Proc) pause(name string) error {
	if _, ok := p.phases[name]; !ok {
		return nil // an unmodeled boundary passes
	}
	p.events <- name
	if !<-p.cmds {
		return errV09Crash
	}
	return nil
}

var v09HbPhases = map[string]int{"heartbeat-nonce-seen": 1, "before-heartbeat-record": 2, "before-defense-record": 3, "engine-before-claim": 4, "engine-claimed": 5, "after-signature": 6}
var v09TxPhases = map[string]int{"before-intent-record": 2, "before-defense-record": 3, "engine-before-claim": 4, "engine-claimed": 5, "after-signature": 6}

type v09HbWorld struct {
	t         *testing.T
	authority *nonceAuthority
	base      HeartbeatConfig
	txConfig  Config
	private   *btcec.PrivateKey
	journal   *journalStub
	txJournal *journalStub
	wallet    [32]byte
	txDigest  [32]byte
	claims    map[[32]byte]bool
	grants    map[[32]byte][32]byte
	misroute  [2]bool
	procs     [2]*v09Proc
	missing   []string
}

type v09Engine struct {
	frost.Engine
	w       *v09HbWorld
	p       int
	journal *journalStub
}

func (e *v09Engine) Sign(ctx context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	proc := e.w.procs[e.p]
	if err := proc.pause("engine-before-claim"); err != nil {
		return [64]byte{}, err
	}
	id := snowfallengine.AttemptID(r.Attempt)
	if e.w.claims[id] {
		return [64]byte{}, errV09Claimed
	}
	e.w.claims[id] = true
	if err := proc.pause("engine-claimed"); err != nil {
		return [64]byte{}, err
	}
	if e.w.misroute[e.p] {
		if e.p == 0 {
			r.Message = e.w.txDigest
		} else {
			r.Message = HeartbeatDigest(e.w.wallet, e.w.authority.current)
		}
	}
	if err := p.SigningAuthorization.BeforeSigning(ctx, r); err != nil {
		return [64]byte{}, err
	}
	// Record before grant (P-K08-EVIDENCE-RETAINED): the defense record of the
	// granted message is durable in the component's own journal.
	if _, ok := e.journal.records[DefenseID(e.w.wallet, r.Message)]; !ok {
		e.w.missing = append(e.w.missing, fmt.Sprintf("proc %d granted %x without a defense record", e.p, r.Message[:4]))
	}
	e.w.grants[id] = r.Message
	s, err := schnorr.Sign(e.w.private, r.Message[:])
	var raw [64]byte
	if err == nil {
		copy(raw[:], s.Serialize())
	}
	return raw, err
}

func (w *v09HbWorld) start(p int) {
	proc := &v09Proc{events: make(chan string), cmds: make(chan bool), phases: v09HbPhases}
	if p == 1 {
		proc.phases = v09TxPhases
	}
	w.procs[p] = proc
	w.misroute[p] = false
	var run func(context.Context) error
	if p == 0 {
		c := w.base
		c.Nonce = w.authority.current
		c.Session = [32]byte{byte(c.Nonce)}
		h, err := NewHeartbeatSigner(c)
		if err != nil {
			w.t.Fatal(err)
		}
		h.engine = &v09Engine{w: w, p: 0, journal: w.journal}
		h.boundary = proc.pause
		run = func(ctx context.Context) error { _, err := h.Run(ctx); return err }
	} else {
		e, err := NewLocalExecutor(w.txConfig)
		if err != nil {
			w.t.Fatal(err)
		}
		e.engine = &v09Engine{w: w, p: 1, journal: w.txJournal}
		e.boundary = proc.pause
		run = func(ctx context.Context) error { _, err := e.Run(ctx); return err }
	}
	go func() {
		proc.err = run(context.Background())
		proc.events <- "returned"
	}()
	w.await(p)
}

func (w *v09HbWorld) await(p int) {
	proc := w.procs[p]
	ev := <-proc.events
	if ev != "returned" {
		proc.phase = proc.phases[ev]
		return
	}
	switch {
	case proc.err == nil:
		proc.phase = 7
	case errors.Is(proc.err, errV09Crash):
		proc.phase = 9
	default:
		proc.phase = 8
	}
}

func (w *v09HbWorld) advance(p int, ok bool) {
	w.procs[p].cmds <- ok
	w.await(p)
}

// finish crashes any process still paused so its goroutine ends.
func (w *v09HbWorld) finish() {
	for p, proc := range w.procs {
		if proc != nil && proc.phase >= 1 && proc.phase <= 6 {
			w.advance(p, false)
		}
	}
}

type v09HbExpect struct {
	Tick  int `json:"tick"`
	Procs [2]struct {
		Phase int `json:"phase"`
	} `json:"procs"`
	Claims uint8    `json:"claims"`
	Grants [8]uint8 `json:"grants"`
}

func (w *v09HbWorld) compare(e v09HbExpect) error {
	for p := 0; p < 2; p++ {
		got := 0
		if w.procs[p] != nil {
			got = w.procs[p].phase
		}
		if got != e.Procs[p].Phase {
			return fmt.Errorf("proc %d phase %d, model %d", p, got, e.Procs[p].Phase)
		}
	}
	if int(w.authority.current) != 40+e.Tick+1 {
		return fmt.Errorf("nonce %d, model tick %d", w.authority.current, e.Tick)
	}
	claims := 0
	for i := 0; i < 8; i++ {
		if e.Claims&(1<<i) != 0 {
			claims++
		}
	}
	if len(w.claims) != claims {
		return fmt.Errorf("claims %d, model %d", len(w.claims), claims)
	}
	var want, got []int
	for _, g := range e.Grants {
		if g != 0 {
			want = append(want, int(g))
		}
	}
	for _, m := range w.grants {
		k := -1
		if m == w.txDigest {
			k = 9
		}
		for n := 1; n <= 3; n++ {
			if m == HeartbeatDigest(w.wallet, uint64(40+n)) {
				k = n
			}
		}
		got = append(got, k)
	}
	sort.Ints(want)
	sort.Ints(got)
	if fmt.Sprint(want) != fmt.Sprint(got) {
		return fmt.Errorf("grants %v, model %v", got, want)
	}
	if len(w.missing) > 0 {
		return errors.New(strings.Join(w.missing, "; "))
	}
	return nil
}

// v09ReplayHb replays steps on a fresh real signer and executor and returns
// the first mismatch with the model's expected state, with the number of
// steps that matched.
func v09ReplayHb(t *testing.T, steps []v09Step) (int, error) {
	h, authority, _ := heartbeatFixture(t)
	tx, _, _, _ := executorFixture(t)
	authority.current = 41
	_, private, _ := planFixture(t)
	w := &v09HbWorld{t: t, authority: authority, base: h.config, txConfig: tx.config, private: private, journal: h.config.Journal.(*journalStub), txJournal: tx.config.Journal.(*journalStub), wallet: h.intent.WalletID, txDigest: tx.config.Plan.snapshot.Digests()[0], claims: map[[32]byte]bool{}, grants: map[[32]byte][32]byte{}}
	defer w.finish()
	matched := 0
	for _, s := range steps {
		var arg int
		_ = json.Unmarshal(s.Fields, &arg)
		switch s.Action {
		case "Tick":
			authority.current++
		case "Start":
			w.start(arg)
		case "Step":
			w.advance(arg, true)
		case "Crash":
			w.advance(arg, false)
		case "Misroute":
			w.misroute[arg] = true
		default:
			return matched, fmt.Errorf("unknown action %s", s.Action)
		}
		var e v09HbExpect
		if err := json.Unmarshal(s.ExpectState, &e); err != nil {
			return matched, err
		}
		if err := w.compare(e); err != nil {
			return matched, fmt.Errorf("step %d %s: %w", s.N, s.Action, err)
		}
		matched++
	}
	return matched, nil
}

// TestV09HeartbeatReplay replays every hb-profile trace (claim-model traces
// fully, weakened ones up to their claim-model divergence, H-06) on the real
// signer and executor and compares each step's outcome with the model.
func TestV09HeartbeatReplay(t *testing.T) {
	var reports []v09Report
	for _, tr := range v09Traces(t, "hb") {
		rep := v09Report{ID: tr.ID, Replay: "full", Weakened: tr.Weakening != "None"}
		t.Run(tr.ID, func(t *testing.T) {
			n, err := v09ReplayHb(t, tr.v09Replayed())
			rep.Steps = n
			if err != nil {
				rep.Result, rep.Detail = "mismatch", err.Error()
				t.Error(err)
				return
			}
			rep.Result = "match"
		})
		reports = append(reports, rep)
	}
	v09WriteReport(t, reports)
}

// TestV09HeartbeatReplayDiscriminates is the harness's own negative control:
// a weakened trace's full expectations (the weakened model's states) must not
// match the current code, or the replay could not see the guard.
func TestV09HeartbeatReplayDiscriminates(t *testing.T) {
	for _, tr := range v09Traces(t, "hb") {
		if tr.Weakening == "None" || tr.Kind != "counterexample" {
			continue
		}
		t.Run(tr.ID, func(t *testing.T) {
			if _, err := v09ReplayHb(t, tr.Steps); err == nil {
				t.Fatal("the weakened expectations matched the real code")
			}
		})
	}
}
