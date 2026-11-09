package signing

// Generated-trace subprocess replay (tuxxy/keep-core#82, protocol in
// verification/SCOPE-2026-11-06.md). Each generated V-04 model trace drives
// TestV04ProcessReplayChild, the real executor over the real encrypted store
// in a separate process: environment steps become provider views, the model's
// phases are matched to the child's pauses, interruptions are applied at the
// last pause at or before their model phase, and every pause and final result
// is compared with the model state. Unsupported steps stop the replay and are
// reported as partial (or none when nothing ran), never as full.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type v04GenAttempt struct {
	Request, Phase                                   uint8
	Claimed, Terminal, Retained, Aggregate, Released bool
	Recovered                                        bool
	LateRefused                                      bool `json:"late_refused"`
}
type v04GenRequest struct {
	Reservation, Anchor, Receipt uint8
	Intent, Changed              bool
}
type v04GenState struct {
	Requests [2]v04GenRequest
	Attempts [2]v04GenAttempt
}
type v04GenStep struct {
	N        int
	Attempt  *uint8
	Action   string
	Fields   json.RawMessage
	State    v04GenState `json:"expect_state"`
	Diverges bool
}

// v04PausePhase is the model phase an attempt has reached at each concrete
// pause. Pauses absent here inherit the previous pause's phase
// (before-reservation-read and reservation-seen surround every reservation
// read; transport-cleanup follows the Done commit or a failed Sign).
var v04PausePhase = map[string]uint8{
	"transaction-frozen": 1,
	"receipt-durable":    7, "before-intent-record": 7,
	"intent-durable": 13, "before-defense-record": 13, "defense-durable": 13,
	"claim-durable": 19, "before-worker-start": 19,
	"worker-returned": 21,
	"done-durable":    27, "after-signature": 27, "before-done-accept": 27,
	"aggregate-durable": 33,
}

// Pauses where a controlled failure returns an error (journal acknowledgement
// hooks and executor boundaries); the engine and transport hooks cannot fail.
var v04FailablePause = map[string]bool{
	"receipt-durable": true, "reservation-seen": true, "before-intent-record": true, "intent-durable": true,
	"before-defense-record": true, "defense-durable": true, "before-worker-start": true, "done-durable": true,
	"after-signature": true, "before-done-accept": true, "aggregate-durable": true,
}

var v04NextPhase = map[uint8]uint8{1: 7, 7: 13, 13: 19, 19: 21, 21: 27, 27: 33, 33: 34}

type v04Child struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	in      *json.Encoder
	stdin   io.WriteCloser
	out     *bufio.Scanner
	stderr  *bytes.Buffer
	stopped bool
}

type v04GenReplay struct {
	t        *testing.T
	id       string
	root     string
	steps    []v04GenStep
	k        int
	state    v04GenState
	launched bool
	partial  []string
	stop     string
	compared int
	events   []string
	// request is the abstract request each slot's current call serves.
	request [2]int
}

func (g *v04GenReplay) view(slot int) v04View {
	q := g.state.Requests[g.request[slot]]
	return v04View{Reservation: q.Reservation, Anchor: q.Anchor, Changed: q.Changed}
}

func (g *v04GenReplay) fields(s v04GenStep) []int {
	var raw []any
	if err := json.Unmarshal(s.Fields, &raw); err != nil {
		var one any
		if json.Unmarshal(s.Fields, &one) == nil && one != nil {
			raw = []any{one}
		}
	}
	out := make([]int, len(raw))
	for i, v := range raw {
		switch x := v.(type) {
		case float64:
			out[i] = int(x)
		case bool:
			if x {
				out[i] = 1
			}
		}
	}
	return out
}

func v04EnvAction(a string) bool {
	switch a {
	case "Tick", "Reserve", "Finalize", "Change", "Reanchor", "Pay", "Cancel", "Refund":
		return true
	}
	return false
}

// consume applies one step's resulting model state.
func (g *v04GenReplay) consume() v04GenStep {
	s := g.steps[g.k]
	if (s.Action == "Pay" || s.Action == "Cancel" || s.Action == "Refund") && g.launched {
		g.note(fmt.Sprintf("n=%d %s projected as reservation state only (no C-04 ledger)", s.N, s.Action))
	}
	g.state = s.State
	g.k++
	return s
}

func (g *v04GenReplay) note(reason string) {
	for _, p := range g.partial {
		if p == reason {
			return
		}
	}
	g.partial = append(g.partial, reason)
}

func (g *v04GenReplay) halt(n int, reason string) {
	g.stop = fmt.Sprintf("n=%d %s", n, reason)
}

func (g *v04GenReplay) launch(config v04ProcessConfig) *v04Child {
	t := g.t
	t.Helper()
	config.Root = g.root
	path := filepath.Join(g.root, "config.json")
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestV04ProcessReplayChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "V04_PROCESS_REPLAY="+path, "GORACE=atexit_sleep_ms=0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &v04Child{cmd: cmd, cancel: cancel, in: json.NewEncoder(stdin), stdin: stdin, out: bufio.NewScanner(stdout), stderr: &bytes.Buffer{}}
	c.out.Buffer(make([]byte, 4096), 65536)
	cmd.Stderr = c.stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	g.launched = true
	return c
}

func (c *v04Child) next(t *testing.T) v04ProcessObservation {
	t.Helper()
	if !c.out.Scan() {
		t.Fatalf("child ended without a final observation: %s", c.stderr.String())
	}
	var o v04ProcessObservation
	if err := json.Unmarshal(c.out.Bytes(), &o); err != nil {
		t.Fatalf("child framing %q: %v", c.out.Text(), err)
	}
	return o
}

func (c *v04Child) kill() {
	if c.stopped {
		return
	}
	c.stopped = true
	_ = c.cmd.Process.Kill()
	c.stdin.Close()
	_ = c.cmd.Wait()
	c.cancel()
}

func (c *v04Child) finish(t *testing.T) {
	c.stopped = true
	c.stdin.Close()
	if err := c.cmd.Wait(); err != nil {
		t.Fatalf("child failed %v stderr=%s", err, c.stderr.String())
	}
	c.cancel()
}

// compare checks the public durable facts of a pause or a final observation
// against the model state (KC-VER H-04).
func (g *v04GenReplay) compare(where string, o v04ProcessObservation, slot int, run bool) {
	g.t.Helper()
	a, q := g.state.Attempts[slot], g.state.Requests[g.request[slot]]
	type pair struct {
		name          string
		model, actual bool
	}
	// One plan fixture serves both abstract requests (distinct reservation
	// IDs), so the concrete intent record is shared: compare it per plan.
	intent := g.state.Requests[0].Intent || g.state.Requests[1].Intent
	if g.state.Requests[0].Intent != g.state.Requests[1].Intent && g.state.Requests[1-g.request[slot]].Intent {
		g.note("intent compared per plan: the other request's intent is the same concrete record")
	}
	checks := []pair{{"receipt", q.Receipt != 0, o.Receipt}, {"intent", intent, o.Intent}, {"done", a.Retained, o.Done}, {"aggregate", a.Aggregate, o.Aggregate}}
	if run {
		checks = append(checks, pair{"claim", a.Claimed, o.ClaimAck})
	}
	for _, c := range checks {
		if c.model != c.actual {
			g.t.Errorf("trace %s %s: %s model=%v actual=%v (observation %+v, model %+v)", g.id, where, c.name, c.model, c.actual, o, a)
		}
	}
	if o.Done && !o.DoneValid {
		g.t.Errorf("trace %s %s: retained Done does not verify", g.id, where)
	}
	g.compared++
	g.events = append(g.events, fmt.Sprintf("%s@k%d", where, g.k))
}

// consumeTo consumes environment steps and this slot's steps whose resulting
// phase does not pass phase.
func (g *v04GenReplay) consumeTo(slot int, phase uint8) {
	for g.k < len(g.steps) {
		s := g.steps[g.k]
		if v04EnvAction(s.Action) {
			g.consume()
			continue
		}
		next := s.State.Attempts[slot]
		// The aggregate acknowledgement ends the call: its step is terminal.
		if s.Action == "Step" && s.Attempt != nil && int(*s.Attempt) == slot && ((!next.Terminal && next.Phase <= phase) || (phase == 33 && next.Aggregate)) {
			g.consume()
			continue
		}
		return
	}
}

// interruption looks ahead from the cursor, past environment steps and this
// slot's steps that stay below next, for an interruption of this slot. It
// returns the index of the interrupting step, or -1.
func (g *v04GenReplay) interruption(slot int, next uint8) int {
	for j := g.k; j < len(g.steps); j++ {
		s := g.steps[j]
		if v04EnvAction(s.Action) {
			continue
		}
		if s.Attempt == nil || int(*s.Attempt) != slot {
			return -1
		}
		switch s.Action {
		case "Step":
			if s.State.Attempts[slot].Terminal || s.State.Attempts[slot].Phase >= next {
				return -1
			}
		case "Crash", "IoError", "CancelCaller":
			return j
		default:
			return -1
		}
	}
	return -1
}

// runChild drives one Run child for slot from its Start step (already consumed).
func (g *v04GenReplay) runChild(slot int, startN int, view v04View, refused bool) {
	t := g.t
	c := g.launch(v04ProcessConfig{Mode: "run", View: &view, Fresh: slot == 1, Request: uint8(g.request[slot])})
	defer c.kill()
	last := uint8(1)
	sentView := view
	for {
		o := c.next(t)
		if o.Boundary == "final" {
			g.consumeUntilTerminal(slot)
			if g.stop != "" {
				return
			}
			g.compare("final", o, slot, true)
			a := g.state.Attempts[slot]
			switch {
			case refused:
				if !o.Unauthorized || o.CryptoCalls != 0 {
					t.Errorf("trace %s n=%d: refused Start ran: %+v", g.id, startN, o)
				}
			case a.Aggregate:
				if o.Error != "" {
					t.Errorf("trace %s: model completed, child failed: %s", g.id, o.Error)
				}
			case a.Claimed && !a.Released:
				if !o.Unauthorized || o.CryptoCalls != 0 {
					t.Errorf("trace %s: model refused the grant, child: %+v", g.id, o)
				}
			default:
				if o.Error == "" {
					t.Errorf("trace %s: model attempt failed, child succeeded", g.id)
				}
			}
			c.finish(t)
			return
		}
		phase, known := v04PausePhase[o.Boundary]
		if !known {
			phase = last
		}
		if refused {
			// The model's Start guard is the concrete first reservation read,
			// which follows the phase-1 pauses; a refused Start writes nothing.
			if phase > 1 {
				t.Errorf("trace %s n=%d: refused Start reached pause %s", g.id, startN, o.Boundary)
				return
			}
			if err := c.in.Encode(v04Decision{}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		first := phase != last || o.Boundary == "transaction-frozen"
		last = phase
		g.consumeTo(slot, phase)
		a := g.state.Attempts[slot]
		if (a.Terminal && !(phase == 33 && a.Aggregate)) || (!a.Terminal && a.Phase < phase) {
			if g.k >= len(g.steps) {
				// The trace ends before this pause: nothing left to compare.
				g.events = append(g.events, "trace-end@"+o.Boundary)
				return
			}
			if !a.Terminal {
				g.haltInside(slot)
				return
			}
			// The model ended this call before the pause: the child must not get here.
			t.Errorf("trace %s: model attempt terminal at k=%d, child reached %s: %+v", g.id, g.k, o.Boundary, o)
			return
		}
		g.compare(o.Boundary, o, slot, true)
		d := v04Decision{}
		// Environment changes wait until after the first reservation read
		// (the model's Start reads its anchor); later pauses precede every
		// later guard read.
		if v := g.view(slot); v != sentView && phase > 1 {
			d.View, sentView = &v, v
		}
		if first {
			if j := g.interruption(slot, v04NextPhase[phase]); j >= 0 {
				s := g.steps[j]
				if !(s.Action == "IoError" && !v04FailablePause[o.Boundary]) {
					for g.k <= j {
						g.consume()
					}
					if g.state.Attempts[slot].Phase != phase && !g.state.Attempts[slot].Terminal {
						g.note("interruption between pauses (stage-level, applied at the preceding pause)")
					}
					if s.Fields != nil && strings.Contains(string(s.Fields), "true") && s.Action == "Crash" {
						g.note("host crash replayed as process kill")
					}
					switch s.Action {
					case "Crash":
						c.kill()
						g.events = append(g.events, fmt.Sprintf("kill@%s", o.Boundary))
						return
					case "IoError":
						d.Fail = true
					case "CancelCaller":
						d.Cancel = true
					}
					g.events = append(g.events, fmt.Sprintf("%s@%s", s.Action, o.Boundary))
				}
			}
		}
		if err := c.in.Encode(d); err != nil {
			t.Fatal(err)
		}
	}
}

// consumeUntilTerminal consumes this slot's remaining steps of the current
// call. Another slot's action first is a concurrent interleaving: stop.
func (g *v04GenReplay) consumeUntilTerminal(slot int) {
	for g.k < len(g.steps) && !g.state.Attempts[slot].Terminal {
		s := g.steps[g.k]
		if v04EnvAction(s.Action) {
			g.consume()
			continue
		}
		if s.Attempt != nil && int(*s.Attempt) == slot && (s.Action == "Step" || s.Action == "IoError" || s.Action == "Crash" || s.Action == "CancelCaller") {
			g.consume()
			continue
		}
		g.haltInside(slot)
		return
	}
}

// haltInside stops at a step the subprocess cannot interleave with this
// slot's running call.
func (g *v04GenReplay) haltInside(slot int) {
	s := g.steps[g.k]
	if s.Attempt != nil && int(*s.Attempt) != slot && (s.Action == "Start" || s.Action == "Step" || s.Action == "Recover") {
		g.halt(s.N, s.Action+" of the other slot inside a running call (concurrent attempts in one host process)")
		return
	}
	g.halt(s.N, s.Action+" inside a running call (not driven: delivery or another call of this slot)")
}

// recoverChild drives LoadResult for slot from its Recover step (consumed).
func (g *v04GenReplay) recoverChild(slot int, n int) {
	t := g.t
	// The reread happens at the first recovery step; environment steps before it count.
	for g.k < len(g.steps) && v04EnvAction(g.steps[g.k].Action) {
		g.consume()
	}
	view := g.view(slot)
	c := g.launch(v04ProcessConfig{Mode: "recover", View: &view, Fresh: slot == 1, Request: uint8(g.request[slot])})
	defer c.kill()
	for {
		o := c.next(t)
		if o.Boundary != "final" {
			if err := c.in.Encode(v04Decision{}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		for g.k < len(g.steps) && !g.state.Attempts[slot].Terminal {
			s := g.steps[g.k]
			if v04EnvAction(s.Action) || (s.Action == "Step" && s.Attempt != nil && int(*s.Attempt) == slot) {
				g.consume()
				continue
			}
			g.halt(s.N, "interruption or other action inside a recovery call (not driven)")
			return
		}
		a := g.state.Attempts[slot]
		if !a.Terminal && !a.Recovered {
			// The trace (a claim-model prefix cut at its divergence) ends
			// inside the recovery call: its outcome is beyond the trace.
			g.events = append(g.events, "trace-end-in-recovery")
			c.finish(t)
			return
		}
		g.compare("recover-final", o, slot, false)
		switch {
		case a.Aggregate:
			if o.Error != "" || o.CryptoCalls != 0 {
				t.Errorf("trace %s n=%d: model recovered, child: %+v", g.id, n, o)
			}
		case a.LateRefused:
			if !o.Late || o.CryptoCalls != 0 {
				t.Errorf("trace %s n=%d: model refused late reconstruction, child: %+v", g.id, n, o)
			}
		}
		c.finish(t)
		return
	}
}

func (g *v04GenReplay) run() {
	running := -1
	for g.k < len(g.steps) && g.stop == "" {
		s := g.steps[g.k]
		if v04EnvAction(s.Action) {
			g.consume()
			continue
		}
		slot := -1
		if s.Attempt != nil {
			slot = int(*s.Attempt)
		}
		switch s.Action {
		case "Start":
			f := g.fields(s)
			if len(f) != 3 || f[1] > 1 {
				g.halt(s.N, "malformed Start")
				return
			}
			g.request[slot] = f[1]
			if f[2] >= 2 {
				g.halt(s.N, "grant mode missing/callback has no Run seam (separate real-store guard test)")
				return
			}
			view := g.view(slot)
			g.consume()
			if f[2] == 1 {
				view.Changed = true
			}
			running = slot
			g.runChild(slot, s.N, view, g.state.Attempts[slot].Phase == 0)
			running = -1
		case "Recover":
			g.consume()
			g.recoverChild(slot, s.N)
		default:
			// Deliver, Retry, or an attempt step with no running child.
			g.halt(s.N, s.Action+" not driven by the subprocess replay")
			return
		}
		_ = running
		if s.Diverges {
			return
		}
	}
}

func TestV04GeneratedTraceProcessReplay(t *testing.T) {
	dir := "verification/evidence/V-04-2026-11-06/traces"
	if env := os.Getenv("V04_SIGNING_TRACES"); env != "" {
		dir = env
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no generated traces in %s: %v", dir, err)
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var trace struct {
				ID, Weakening string
				Steps         []v04GenStep
				Claim         []v04GenStep `json:"claim_model_steps"`
			}
			if err = json.Unmarshal(raw, &trace); err != nil {
				t.Fatal(err)
			}
			steps := trace.Steps
			if trace.Weakening != "None" {
				// KC-VER H-06: claim-model expectations, cut at the first divergence.
				steps = trace.Claim
			}
			g := &v04GenReplay{t: t, id: trace.ID, root: t.TempDir(), steps: steps}
			if err = os.WriteFile(filepath.Join(g.root, "fence"), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			g.run()
			// Full only at the executor's exposed boundaries: storage stages
			// inside one commit are consumed between pauses, not compared one
			// by one (the store's own stage replay is the V-01 harness).
			class := "full-application-boundary"
			switch {
			case !g.launched:
				class = "none"
			case g.stop != "" || len(g.partial) > 0:
				class = "partial"
			}
			reasons := append([]string(nil), g.partial...)
			if g.stop != "" {
				reasons = append(reasons, "stopped "+g.stop)
			}
			t.Logf("V04_GENERATED_PROCESS_REPLAY id=%s class=%s steps=%d/%d compared=%d events=%q reasons=%q", trace.ID, class, g.k, len(steps), g.compared, g.events, reasons)
		})
	}
}
