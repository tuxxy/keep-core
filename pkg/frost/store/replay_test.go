package store

// Trace replay harness for the K-02 host model (verification/model). TEST ONLY.
//
// Each modeled process runs as a subprocess of this test binary in lockstep
// with the driver. The subprocess pauses at the store's named I/O boundaries
// and the driver decides, step by step, whether to continue, inject an I/O
// error, or kill it. Environment actions (fence reset, stale restore, clone,
// simulated host crash) are applied by the driver. Outcomes are compared with
// the model's expectations at every step; a successful open also compares the
// loaded state with the model's snapshot.
//
// Traces live in verification/traces/*.json and are written by the model
// runner. Nothing here is a production API or a recovery tool.

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
)

// ---------------------------------------------------------------------------
// Trace format (mirrors verification/model/src/lib.rs TraceStep / Trace)
// ---------------------------------------------------------------------------

type replaySnapshot struct {
	Claims int     `json:"claims"`
	Dkg    [2]bool `json:"dkg"`
	Slots  [2]*int `json:"slots"`
}

type replayStep struct {
	N           int             `json:"n"`
	Action      string          `json:"action"`
	Proc        *int            `json:"proc"`
	Image       *int            `json:"image"`
	Attempt     *int            `json:"attempt"`
	Purpose     *string         `json:"purpose"`
	Domain      *int            `json:"domain"`
	Slot        *int            `json:"slot"`
	Value       *int            `json:"value"`
	Step        *string         `json:"step"`
	FailSync    *bool           `json:"fail_sync"`
	Expect      *string         `json:"expect"`
	Pause       *string         `json:"pause"`
	ExpectState *replaySnapshot `json:"expect_state"`
	Diverges    *bool           `json:"diverges"`
}

type replayTrace struct {
	ID              string       `json:"id"`
	Kind            string       `json:"kind"`
	Property        string       `json:"property"`
	Weakening       string       `json:"weakening"`
	MaxCommits      int          `json:"max_commits"`
	Replay          string       `json:"replay"`
	ReplayNote      string       `json:"replay_note"`
	Steps           []replayStep `json:"steps"`
	ClaimModelSteps []replayStep `json:"claim_model_steps"`
}

// ---------------------------------------------------------------------------
// Concrete mapping of abstract model values
// ---------------------------------------------------------------------------

func replayAttemptID(a int) [32]byte { return [32]byte{0xA0 + byte(a)} }
func replayDomain(d int) frost.Domain {
	dom := testDomain()
	dom.Epoch += uint64(d)
	return dom
}

var replayRecordID = [32]byte{0x50}
var replayLockAttempt = [32]byte{0x77}

func replayRecordWrite(v int) frost.Write {
	return frost.Write{Kind: "candidate-record", Seat: 1, ID: replayRecordID, Payload: []byte(fmt.Sprintf("value-%d", v))}
}
func replayLockWrite(v int) frost.Write {
	return frost.Write{Kind: "lock", Seat: 1, ID: [32]byte{0x60 + byte(v)}, Payload: []byte("lock-payload"), Slot: &frost.LockSlot{Attempt: replayLockAttempt, Kind: "round-one"}}
}
func replayWrite(slot, v int) frost.Write {
	if slot == 0 {
		return replayRecordWrite(v)
	}
	return replayLockWrite(v)
}

// replayFence is diskFence plus a test knob: when the flag file exists at
// Advance time, the authority commits the advance, removes the flag and
// returns an error. This is the "committed but lost response" outcome.
type replayFence struct{ path string }
type replayLease struct {
	inner    FenceLease
	loseFlag string
}

func replayLoseFlag(fencePath string) string { return fencePath + ".lose-response" }

func (f replayFence) Acquire(ctx context.Context, id [32]byte) (FenceLease, error) {
	l, e := diskFence{f.path}.Acquire(ctx, id)
	if e != nil {
		return nil, e
	}
	return &replayLease{l, replayLoseFlag(f.path)}, nil
}
func (l *replayLease) Head(ctx context.Context) (Point, error) { return l.inner.Head(ctx) }
func (l *replayLease) Advance(ctx context.Context, before, after Point) error {
	if e := l.inner.Advance(ctx, before, after); e != nil {
		return e
	}
	if _, e := os.Stat(l.loseFlag); e == nil {
		os.Remove(l.loseFlag)
		return errors.New("fence response lost after commit")
	}
	return nil
}
func (l *replayLease) Close() error { return l.inner.Close() }

// ---------------------------------------------------------------------------
// Subprocess: one modeled process
// ---------------------------------------------------------------------------

// Boundaries at which the model has no stage; the driver passes them silently.
var replayTransparent = map[string]bool{
	"before-claim": true, "after-file-sync": true, "after-install": true,
	"after-directory-sync": true, "after-fence": true, "after-claim": true,
	"after-identical-sync": true,
}

func abstractState(s *Store) (replaySnapshot, error) {
	var out replaySnapshot
	for a := 0; a < 2; a++ {
		id := replayAttemptID(a)
		if _, ok := s.state.Claims[hex.EncodeToString(id[:])]; ok {
			out.Claims |= 1 << a
		}
	}
	for d := 0; d < 2; d++ {
		v := &Scoped{s, replayDomain(d)}
		if _, ok := s.state.DKGs[v.prefix()]; ok {
			out.Dkg[d] = true
		}
	}
	v0 := &Scoped{s, replayDomain(0)}
	if r, ok := s.state.Records[v0.recordKey(replayRecordID)]; ok {
		plain, e := s.open(r.Ciphertext, recordHeader(r))
		if e != nil {
			return out, e
		}
		var v int
		if _, e = fmt.Sscanf(string(plain), "value-%d", &v); e != nil {
			return out, e
		}
		out.Slots[0] = &v
	}
	if key, ok := s.state.Locks[v0.slot(replayLockWrite(0))]; ok {
		for v := 0; v < 2; v++ {
			if key == v0.recordKey(replayLockWrite(v).ID) {
				vv := v
				out.Slots[1] = &vv
			}
		}
	}
	return out, nil
}

func formatSnapshot(s replaySnapshot) string {
	slot := func(p *int) string {
		if p == nil {
			return "-"
		}
		return strconv.Itoa(*p)
	}
	return fmt.Sprintf("claims=%d dkg=%t,%t slots=%s,%s", s.Claims, s.Dkg[0], s.Dkg[1], slot(s.Slots[0]), slot(s.Slots[1]))
}

func classify(e error) string {
	switch {
	case e == nil:
		return "ok"
	case errors.Is(e, ErrBusy):
		return "busy"
	case errors.Is(e, ErrQuarantined):
		return "quarantined"
	case errors.Is(e, ErrClaimed):
		return "claimed"
	case errors.Is(e, ErrDKGPending):
		return "dkg_pending"
	default:
		return "error " + strings.ReplaceAll(e.Error(), "\n", " ")
	}
}

// TestStoreReplayHelper is the subprocess body. It is inert unless
// FROST_STORE_REPLAY is set by the driver.
func TestStoreReplayHelper(t *testing.T) {
	if os.Getenv("FROST_STORE_REPLAY") == "" {
		return
	}
	out := bufio.NewWriter(os.Stdout)
	say := func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
		out.Flush()
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var s *Store
	var releases []func()
	control := make(chan string)
	// runOp executes one store operation while relaying boundary pauses.
	runOp := func(op func() string) {
		s.boundary = func(name string) error {
			say("paused %s", name)
			switch cmd := <-control; cmd {
			case "continue":
				return nil
			case "io-error":
				return errors.New("injected I/O uncertainty")
			case "crash":
				os.Exit(73)
			default:
				say("result error unknown control %q", cmd)
				os.Exit(2)
			}
			return nil
		}
		done := make(chan string, 1)
		go func() { done <- op() }()
		for {
			select {
			case r := <-done:
				say("result %s", r)
				return
			case line, ok := <-lines:
				if !ok {
					os.Exit(3)
				}
				control <- line
			}
		}
	}
	ctx := context.Background()
	for line := range lines {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "open":
			c := configAt(f[1], f[2])
			c.Fence = replayFence{f[2]}
			var e error
			s, e = Open(ctx, c)
			if e != nil {
				s = nil
				say("result %s", classify(e))
				continue
			}
			st, e := abstractState(s)
			if e != nil {
				say("result error %v", e)
				continue
			}
			say("result opened %s", formatSnapshot(st))
		case "close":
			e := s.Close()
			if e == nil {
				say("result closed")
			} else {
				say("result %s", classify(e))
			}
		case "release":
			if len(releases) == 0 {
				say("result error no live claim")
				continue
			}
			releases[len(releases)-1]()
			releases = releases[:len(releases)-1]
			say("result released")
		case "claim":
			attempt, _ := strconv.Atoi(f[1])
			domain, _ := strconv.Atoi(f[3])
			purpose := f[2]
			v, e := s.Scope(replayDomain(domain))
			if e != nil {
				say("result %s", classify(e))
				continue
			}
			runOp(func() string {
				release, e := v.Claim(ctx, replayAttemptID(attempt), purpose, []byte("replay intent"))
				if e != nil {
					return classify(e)
				}
				releases = append(releases, release)
				return "claim_ok"
			})
		case "put":
			slot, _ := strconv.Atoi(f[1])
			value, _ := strconv.Atoi(f[2])
			v, e := s.Scope(replayDomain(0))
			if e != nil {
				say("result %s", classify(e))
				continue
			}
			runOp(func() string {
				r, e := v.Put(ctx, replayWrite(slot, value))
				if e != nil {
					return classify(e)
				}
				switch r {
				case frost.Durable:
					return "durable"
				case frost.Identical:
					return "identical"
				case frost.Conflict:
					return "conflict"
				}
				return fmt.Sprintf("error put result %d", r)
			})
		case "crash":
			os.Exit(73)
		case "exit":
			return
		default:
			say("result error unknown command %q", f[0])
		}
	}
}

// ---------------------------------------------------------------------------
// Driver
// ---------------------------------------------------------------------------

type replayProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	exited chan error
	paused string // boundary name while paused, else ""
}

type replayHarness struct {
	t      *testing.T
	root   string
	fence  string
	images [2]string
	procs  map[int]*replayProc
	// model stage and image per proc, for the simulated host crash
	stage map[int]string
	image map[int]int
	log   []string
}

func newReplayHarness(t *testing.T) *replayHarness {
	t.Helper()
	root := t.TempDir()
	h := &replayHarness{t: t, root: root, fence: filepath.Join(root, "fence"), procs: map[int]*replayProc{}, stage: map[int]string{}, image: map[int]int{}}
	b, _ := json.Marshal(Point{})
	must(t, os.WriteFile(h.fence, b, 0600))
	for i := range h.images {
		h.images[i] = filepath.Join(root, fmt.Sprintf("image%d", i), "keystore")
	}
	must(t, os.MkdirAll(filepath.Dir(h.images[0]), 0700))
	t.Cleanup(h.killAll)
	return h
}

func (h *replayHarness) logf(format string, args ...any) {
	h.log = append(h.log, fmt.Sprintf(format, args...))
}

func (h *replayHarness) failf(format string, args ...any) {
	h.t.Helper()
	msg := fmt.Sprintf(format, args...)
	h.t.Fatalf("%s\nreplay log:\n  %s", msg, strings.Join(h.log, "\n  "))
}

func (h *replayHarness) spawn(p int) *replayProc {
	h.t.Helper()
	exe, e := os.Executable()
	must(h.t, e)
	cmd := exec.Command(exe, "-test.run=^TestStoreReplayHelper$")
	cmd.Env = append(os.Environ(), "FROST_STORE_REPLAY=1")
	stdin, e := cmd.StdinPipe()
	must(h.t, e)
	stdout, e := cmd.StdoutPipe()
	must(h.t, e)
	cmd.Stderr = os.Stderr
	must(h.t, cmd.Start())
	rp := &replayProc{cmd: cmd, stdin: stdin, lines: make(chan string, 64), exited: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			rp.lines <- sc.Text()
		}
		close(rp.lines)
	}()
	go func() { rp.exited <- cmd.Wait() }()
	h.procs[p] = rp
	return rp
}

func (h *replayHarness) killAll() {
	for p, rp := range h.procs {
		if rp.cmd.Process != nil {
			rp.cmd.Process.Kill()
		}
		<-rp.exited
		delete(h.procs, p)
	}
}

func (h *replayHarness) proc(p int) *replayProc {
	if rp, ok := h.procs[p]; ok {
		return rp
	}
	return h.spawn(p)
}

func (h *replayHarness) send(p int, line string) {
	h.t.Helper()
	rp := h.proc(p)
	h.logf("P%d <- %s", p, line)
	if _, e := io.WriteString(rp.stdin, line+"\n"); e != nil {
		h.failf("P%d write %q: %v", p, line, e)
	}
}

// read returns the next protocol line ("paused X" or "result ...").
func (h *replayHarness) read(p int) string {
	h.t.Helper()
	rp := h.proc(p)
	select {
	case line, ok := <-rp.lines:
		if !ok {
			h.failf("P%d closed its output", p)
		}
		h.logf("P%d -> %s", p, line)
		return line
	case <-time.After(30 * time.Second):
		h.failf("P%d timed out", p)
	}
	return ""
}

// advance drives the subprocess until it reports the wanted pause or a
// result. Transparent boundaries are continued automatically. A pause at
// "before-identical-sync" is answered with io-error when failSync is set.
func (h *replayHarness) advance(p int, wantPause string, failSync bool) (pause string, result string) {
	h.t.Helper()
	rp := h.proc(p)
	for {
		line := h.read(p)
		if strings.HasPrefix(line, "result ") {
			rp.paused = ""
			return "", strings.TrimPrefix(line, "result ")
		}
		if !strings.HasPrefix(line, "paused ") {
			continue // test framework noise
		}
		b := strings.TrimPrefix(line, "paused ")
		rp.paused = b
		if b == wantPause {
			return b, ""
		}
		if b == "before-identical-sync" {
			if failSync {
				h.send(p, "io-error")
			} else {
				h.send(p, "continue")
			}
			continue
		}
		if replayTransparent[b] {
			h.send(p, "continue")
			continue
		}
		h.failf("P%d paused at unexpected boundary %q (wanted %q)", p, b, wantPause)
	}
}

func (h *replayHarness) waitExit(p int, want int) {
	h.t.Helper()
	rp := h.proc(p)
	select {
	case e := <-rp.exited:
		if exitCode(e) != want {
			h.failf("P%d exit %v, want %d", p, e, want)
		}
	case <-time.After(30 * time.Second):
		h.failf("P%d did not exit", p)
	}
	h.logf("P%d exited %d", p, want)
	delete(h.procs, p)
	delete(h.stage, p)
}

func (h *replayHarness) expectOutcome(step replayStep, got string) {
	h.t.Helper()
	if step.Expect == nil {
		return
	}
	want := *step.Expect
	// Model-only distinctions that the store reports as one error.
	if want == "lost_response" {
		want = "quarantined"
	}
	gotHead := strings.Fields(got)[0]
	if gotHead != want {
		h.failf("step %d %s: outcome %q, model expects %q", step.N, step.Action, got, want)
	}
	if step.ExpectState != nil {
		want := formatSnapshot(*step.ExpectState)
		gotState := strings.TrimPrefix(got, "opened ")
		if gotState != want {
			h.failf("step %d open: loaded state %q, model expects %q", step.N, gotState, want)
		}
	}
}

func (h *replayHarness) journals(image int) []string {
	root := filepath.Join(h.images[image], "snowfall")
	names, _ := filepath.Glob(filepath.Join(root, "*.journal"))
	sort.Strings(names) // 20-digit zero-padded sequence prefix sorts numerically
	return names
}

func (h *replayHarness) removeNewestJournal(image int) {
	h.t.Helper()
	names := h.journals(image)
	if len(names) == 0 {
		h.failf("image %d has no journal to remove", image)
	}
	must(h.t, os.Remove(names[len(names)-1]))
	h.logf("removed %s", filepath.Base(names[len(names)-1]))
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}

// run replays one step list. It returns the number of steps executed.
func (h *replayHarness) run(steps []replayStep) int {
	h.t.Helper()
	for _, step := range steps {
		p := 0
		if step.Proc != nil {
			p = *step.Proc
		}
		switch step.Action {
		case "open":
			h.image[p] = *step.Image
			h.send(p, fmt.Sprintf("open %s %s", h.images[*step.Image], h.fence))
			_, r := h.advance(p, "", false)
			h.expectOutcome(step, r)
		case "close":
			h.send(p, "close")
			_, r := h.advance(p, "", false)
			h.expectOutcome(step, r)
		case "release":
			h.send(p, "release")
			if _, r := h.advance(p, "", false); r != "released" {
				h.failf("step %d release: %s", step.N, r)
			}
		case "claim", "put":
			if step.Action == "claim" {
				h.send(p, fmt.Sprintf("claim %d %s %d", *step.Attempt, *step.Purpose, *step.Domain))
			} else {
				h.send(p, fmt.Sprintf("put %d %d", *step.Slot, *step.Value))
			}
			want := ""
			if step.Pause != nil {
				want = *step.Pause
			}
			failSync := step.FailSync != nil && *step.FailSync
			pause, r := h.advance(p, want, failSync)
			if step.Pause != nil {
				if pause != want {
					h.failf("step %d %s: got result %q, model expects pause %q", step.N, step.Action, r, want)
				}
				h.stage[p] = pause
			} else {
				if r == "" {
					h.failf("step %d %s: paused at %q, model expects an immediate result", step.N, step.Action, pause)
				}
				h.expectOutcome(step, r)
			}
		case "step":
			if *step.Step == "fence_lost" {
				must(h.t, os.WriteFile(replayLoseFlag(h.fence), []byte("lose"), 0600))
			}
			h.send(p, "continue")
			want := ""
			if step.Pause != nil {
				want = *step.Pause
			}
			pause, r := h.advance(p, want, false)
			if step.Pause != nil {
				if pause != want {
					h.failf("step %d step %s: got result %q, model expects pause %q", step.N, *step.Step, r, want)
				}
				h.stage[p] = pause
			} else {
				if r == "" {
					h.failf("step %d step %s: paused at %q, model expects a result", step.N, *step.Step, pause)
				}
				delete(h.stage, p)
				h.expectOutcome(step, r)
			}
		case "io_error":
			h.send(p, "io-error")
			_, r := h.advance(p, "", false)
			delete(h.stage, p)
			h.expectOutcome(step, r)
		case "process_crash":
			h.send(p, "crash")
			h.waitExit(p, 73)
		case "host_crash":
			// Simulated: the model declares which installed names were not yet
			// durable. Only a journal installed before its directory sync can be
			// lost; that is a subprocess paused at before-directory-sync.
			for q, stage := range h.stage {
				if stage == "before-directory-sync" {
					h.removeNewestJournal(h.image[q])
				}
			}
			h.killAll()
			h.stage = map[int]string{}
			os.Remove(replayLoseFlag(h.fence))
		case "fence_reset":
			b, _ := json.Marshal(Point{})
			must(h.t, os.WriteFile(h.fence, b, 0600))
			h.logf("fence reset to zero")
		case "restore_stale":
			h.removeNewestJournal(*step.Image)
		case "clone":
			must(h.t, os.MkdirAll(filepath.Dir(h.images[1]), 0700))
			if _, e := os.Stat(h.images[0]); errors.Is(e, os.ErrNotExist) {
				// The model allows cloning an image that no process has
				// opened yet: an empty keystore copies to an empty keystore.
				h.logf("cloned empty image 0 to image 1")
			} else {
				must(h.t, copyTree(h.images[0], h.images[1]))
				h.logf("cloned image 0 to image 1")
			}
		default:
			h.failf("step %d: unknown action %q", step.N, step.Action)
		}
	}
	return len(steps)
}

// ---------------------------------------------------------------------------
// Test entry
// ---------------------------------------------------------------------------

type replayReport struct {
	Trace  string `json:"trace"`
	Kind   string `json:"kind"`
	Mode   string `json:"mode"`
	Replay string `json:"replay"`
	Steps  int    `json:"steps"`
	Result string `json:"result"`
	Note   string `json:"note,omitempty"`
}

// TestReplayTraces replays every trace under verification/traces against the
// real store. FROST_REPLAY_MODE=claim (default) replays the claim model's
// expectations; FROST_REPLAY_MODE=weakened replays a weakened model's own
// expectations (used only for the historical negative control).
// FROST_REPLAY_REPORT=<path> writes a machine-readable per-trace report.
func TestReplayTraces(t *testing.T) {
	dir := filepath.Join("verification", "traces")
	files, e := filepath.Glob(filepath.Join(dir, "*.json"))
	must(t, e)
	if len(files) == 0 {
		t.Skip("no traces")
	}
	mode := os.Getenv("FROST_REPLAY_MODE")
	if mode == "" {
		mode = "claim"
	}
	var reports []replayReport
	for _, file := range files {
		raw, e := os.ReadFile(file)
		must(t, e)
		var tr replayTrace
		must(t, json.Unmarshal(raw, &tr))
		t.Run(tr.ID, func(t *testing.T) {
			rep := replayReport{Trace: tr.ID, Kind: tr.Kind, Mode: mode, Replay: tr.Replay}
			defer func() { reports = append(reports, rep) }()
			steps := tr.Steps
			if tr.Weakening != "None" {
				if mode == "claim" {
					steps = tr.ClaimModelSteps
				}
			} else if mode == "weakened" {
				rep.Result = "skip"
				rep.Note = "claim-model trace; nothing weakened to replay"
				t.Skip(rep.Note)
			}
			if tr.Replay == "none" {
				rep.Result = "skip"
				rep.Note = tr.ReplayNote
				t.Skip(tr.ReplayNote)
			}
			if tr.Replay == "partial" {
				rep.Note = tr.ReplayNote
			}
			h := newReplayHarness(t)
			rep.Result = "fail"
			rep.Steps = h.run(steps)
			rep.Result = "pass"
		})
	}
	if path := os.Getenv("FROST_REPLAY_REPORT"); path != "" {
		b, e := json.MarshalIndent(reports, "", "  ")
		must(t, e)
		must(t, os.WriteFile(path, b, 0600))
	}
}
