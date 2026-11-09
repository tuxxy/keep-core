package enforcement

// KC-VER V-09 replay of the enf profile (P-K08-DEFENSE-BEFORE-DEADLINE and the
// survival half of P-K08-EVIDENCE-RETAINED) on the real Defender, and of the
// claim profile (P-K08-NO-SUSPECT-SANCTION) on the real Runner and Claimer.
// Test-only; see verification/SCOPE.md section 5.
//
// FIXTURES (labelled, KC-VER H-03):
//   - the chain is the existing fakeChain (challenge view, lost replies);
//   - in the enf profile the executor is not run: its record stages are fixture
//     writes of the real signing.DefenseRecord into memRecords (the real
//     record-before-grant order is replayed by TestV09HeartbeatReplay in
//     package signing), an issued write survives a process crash and is lost
//     in a host crash (the S-K02-HOST-4 outcome, A-V09-STORE), and a stale
//     restore is a reader that fails closed. Those traces count as partial;
//   - in the claim profile the heartbeat signer is the Runner's `sign` seam
//     returning the round's authenticated presence, a worker error carrying
//     `Suspect` = 3 when the model says so, or a kill after the start record.

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

type v09Step struct {
	N           int             `json:"n"`
	Action      string          `json:"action"`
	Fields      json.RawMessage `json:"fields"`
	Offered     *bool           `json:"offered"`
	ExpectState json.RawMessage `json:"expect_state"`
}

type v09Trace struct {
	ID         string          `json:"id"`
	Profile    string          `json:"profile"`
	Kind       string          `json:"kind"`
	Weakening  string          `json:"weakening"`
	Init       json.RawMessage `json:"init"`
	Steps      []v09Step       `json:"steps"`
	ClaimSteps []v09Step       `json:"claim_model_steps"`
}

func (t v09Trace) replayed() []v09Step {
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
	Note     string `json:"replay_note,omitempty"`
	Steps    int    `json:"steps_replayed"`
	Result   string `json:"result"`
	Detail   string `json:"detail,omitempty"`
	Weakened bool   `json:"weakened"`
}

func v09Traces(t *testing.T, prefix string) []v09Trace {
	t.Helper()
	dir := os.Getenv("V09_TRACES")
	if dir == "" {
		dir = filepath.Join("verification", "traces")
	}
	files, err := filepath.Glob(filepath.Join(dir, prefix+"-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s traces in %s: %v", prefix, dir, err)
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
		out = append(out, tr)
	}
	return out
}

var v09Reports []v09Report

func v09Record(t *testing.T, r v09Report) {
	v09Reports = append(v09Reports, r)
	if path := os.Getenv("V09_REPLAY_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(v09Reports, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var errV09Crash = errors.New("v09 replay: process crash at boundary")

// ------------------------------------------------------------- enf profile

var v09DefPauses = map[string]bool{"challenge-seen": true, "before-defense": true, "defense-submitted": true}

type v09Defender struct {
	events chan string
	cmds   chan bool
	at     string // pause name, or "returned"
	err    error
}

func (d *v09Defender) pause(name string) error {
	if !v09DefPauses[name] {
		return nil
	}
	d.events <- name
	if !<-d.cmds {
		return errV09Crash
	}
	return nil
}

type v09Quarantine struct {
	r           *memRecords
	quarantined *bool
}

func (q v09Quarantine) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	if *q.quarantined {
		return nil, errors.New("v09 fixture: store quarantined (stale restore)")
	}
	return q.r.ReadSigningRecord(ctx, id)
}

type v09EnfWorld struct {
	t           *testing.T
	f           *fakeChain
	rec         *memRecords
	quarantined bool
	up          bool
	issued      [3]bool
	record      signing.DefenseRecord
	digests     [2][32]byte
	def         *v09Defender
}

func (w *v09EnfWorld) write(i int) {
	id := [32]byte{0x1, byte(i)} // fixture ids for the intent and Done records
	raw := []byte{byte(i)}
	if i == 1 {
		id = signing.DefenseID(w.record.WalletID, w.record.Digest)
		var err error
		if raw, err = w.record.Marshal(); err != nil {
			w.t.Fatal(err)
		}
	}
	if err := w.rec.RetainSigningRecord(context.Background(), id, raw); err != nil {
		w.t.Fatal(err)
	}
	w.issued[i] = false
}

func (w *v09EnfWorld) await() {
	ev := <-w.def.events
	w.def.at = ev
}

func (w *v09EnfWorld) cont(until func(string) bool) {
	for w.def != nil && w.def.at != "returned" && !until(w.def.at) {
		w.def.cmds <- true
		w.await()
	}
}

func (w *v09EnfWorld) crash() {
	if w.def != nil && w.def.at != "returned" {
		w.def.cmds <- false
		w.await()
	}
	w.def = nil
}

type v09EnfExpect struct {
	Ch [2]struct {
		Admitted bool `json:"admitted"`
		Status   int  `json:"status"`
	} `json:"ch"`
	Rec      [3]int `json:"rec"`
	Tick     int    `json:"tick"`
	DefPhase int    `json:"def_phase"`
	Up       bool   `json:"up"`
}

func (w *v09EnfWorld) compare(e v09EnfExpect) error {
	for c := 0; c < 2; c++ {
		got, ok := w.f.challenges[w.digests[c]]
		if ok != e.Ch[c].Admitted {
			return fmt.Errorf("challenge %d admitted %v, model %v", c, ok, e.Ch[c].Admitted)
		}
		if ok && got.Resolved != (e.Ch[c].Status != 0) {
			return fmt.Errorf("challenge %d resolved %v, model status %d", c, got.Resolved, e.Ch[c].Status)
		}
	}
	_, err := w.rec.ReadSigningRecord(context.Background(), signing.DefenseID(w.record.WalletID, w.record.Digest))
	if (err == nil) != (e.Rec[1] == 2) {
		return fmt.Errorf("defense record present %v, model %d", err == nil, e.Rec[1])
	}
	at := "idle"
	if w.def != nil && w.def.at != "returned" {
		at = w.def.at
	}
	switch e.DefPhase {
	case 0:
		if at != "idle" {
			return fmt.Errorf("defender at %s, model idle", at)
		}
	case 1:
		if at != "challenge-seen" && at != "defense-submitted" {
			return fmt.Errorf("defender at %s, model scanning", at)
		}
	case 2:
		if at != "before-defense" {
			return fmt.Errorf("defender at %s, model before-defense", at)
		}
	}
	if w.up != e.Up {
		return fmt.Errorf("host up %v, model %v", w.up, e.Up)
	}
	return nil
}

func v09ReplayEnf(t *testing.T, tr v09Trace, steps []v09Step) (int, bool, error) {
	w := &v09EnfWorld{}
	defer func() { w.crash() }()
	return v09ReplayEnfInto(t, tr, steps, &w)
}

// v09ReplayEnfWorld replays steps and returns the world for a continuation.
func v09ReplayEnfWorld(t *testing.T, tr v09Trace, steps []v09Step) (*v09EnfWorld, error) {
	w := &v09EnfWorld{}
	_, _, err := v09ReplayEnfInto(t, tr, steps, &w)
	return w, err
}

func v09ReplayEnfInto(t *testing.T, tr v09Trace, steps []v09Step, out **v09EnfWorld) (int, bool, error) {
	var init struct {
		Kind int `json:"kind"`
	}
	if err := json.Unmarshal(tr.Init, &init); err != nil {
		t.Fatal(err)
	}
	w := &v09EnfWorld{t: t, f: &fakeChain{head: 10, now: 1000, timeout: 3, challenges: map[[32]byte]Challenge{}}, rec: &memRecords{m: map[[32]byte][]byte{}}, up: true}
	if init.Kind == 1 {
		w.record = signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: signing.HeartbeatDigest(testWallet, 41), Purpose: signing.Heartbeat, Kind: signing.DefenseHeartbeat, Nonce: 41}
	} else {
		tx, prevouts := sweepTx()
		// The fake chain resolves a transaction defense on digest 0x0d.
		w.record = signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: [32]byte{0xd}, Purpose: signing.IncomingSweep, Kind: signing.DefenseSweep, Transaction: tx, Prevouts: prevouts}
	}
	w.digests = [2][32]byte{w.record.Digest, {0xe}}
	*out = w
	partial := false
	matched := 0
	for _, s := range steps {
		var arg json.RawMessage = s.Fields
		var e v09EnfExpect
		if err := json.Unmarshal(s.ExpectState, &e); err != nil {
			return matched, partial, err
		}
		switch s.Action {
		case "Tick":
			w.f.head++
			w.f.now++
		case "ExecStart":
		case "ExecStep":
			var stage string
			_ = json.Unmarshal(arg, &stage)
			switch stage {
			case "IntentIssue":
				w.issued[0] = true
			case "IntentAck":
				w.write(0)
			case "DefenseIssue":
				w.issued[1] = true
			case "DefenseAck":
				w.write(1)
			case "DoneIssue":
				w.issued[2] = true
			case "DoneAck":
				w.write(2)
			}
		case "Crash":
			var host bool
			_ = json.Unmarshal(arg, &host)
			w.crash()
			for i, iss := range w.issued {
				if iss {
					if host {
						w.issued[i] = false
					} else {
						w.write(i)
					}
				}
			}
			w.up = false
		case "Restart":
			w.up = true
		case "Recover", "Settle":
			// The defender reads neither executor recovery nor settlement.
		case "Restore":
			w.quarantined = true
			partial = true
		case "Challenge":
			var c int
			_ = json.Unmarshal(arg, &c)
			challenge(w.f, w.digests[c], w.f.head, w.f.now)
		case "Timeout":
			var c int
			_ = json.Unmarshal(arg, &c)
			ch := w.f.challenges[w.digests[c]]
			ch.Resolved = true
			w.f.challenges[w.digests[c]] = ch
		case "DefStart":
			d := &v09Defender{events: make(chan string), cmds: make(chan bool)}
			w.def = d
			defender := &Defender{Chain: w.f, Wallet: testWallet, Records: v09Quarantine{w.rec, &w.quarantined}, boundary: d.pause}
			go func() {
				_, d.err = defender.Defend(context.Background())
				d.events <- "returned"
			}()
			w.await()
		case "DefNext":
			switch e.DefPhase {
			case 2:
				w.cont(func(at string) bool { return at == "before-defense" })
			case 0:
				w.cont(func(string) bool { return false })
				w.def = nil
			}
		case "DefSubmit":
			var lost bool
			_ = json.Unmarshal(arg, &lost)
			w.f.lostReply = lost
			w.def.cmds <- true
			w.await()
			w.f.lostReply = false
		default:
			return matched, partial, fmt.Errorf("unknown action %s", s.Action)
		}
		if err := w.compare(e); err != nil {
			return matched, partial, fmt.Errorf("step %d %s: %w", s.N, s.Action, err)
		}
		matched++
	}
	return matched, true, nil
}

// v09Continue is the SC-08 conditional reachability on the real code: from
// the replayed state, with a defense due (before the deadline tick, store
// readable, A admitted and unresolved, A's defense record durable), host-only
// actions (restart, one full Defend pass) resolve A's challenge.
func v09Continue(t *testing.T, tr v09Trace, steps []v09Step) (bool, string) {
	if len(steps) == 0 {
		return false, "no steps"
	}
	var e v09EnfExpect
	if err := json.Unmarshal(steps[len(steps)-1].ExpectState, &e); err != nil {
		t.Fatal(err)
	}
	if !(e.Tick < 3 && e.Ch[0].Admitted && e.Ch[0].Status == 0 && e.Rec[1] == 2) {
		return false, "no defense due at the end of the trace"
	}
	w, err := v09ReplayEnfWorld(t, tr, steps)
	if err != nil {
		t.Fatal(err)
	}
	if w.quarantined {
		return false, "store quarantined"
	}
	w.crash()
	w.up = true
	defender := &Defender{Chain: w.f, Wallet: testWallet, Records: v09Quarantine{w.rec, &w.quarantined}}
	actions, err := defender.Defend(context.Background())
	if !w.f.challenges[w.digests[0]].Resolved {
		t.Errorf("%s: real defender did not resolve A from the replayed state: %v %v", tr.ID, actions, err)
		return true, "not defended"
	}
	return true, "defended"
}

// TestV09DefenseReplay replays every enf-profile trace on the real Defender.
func TestV09DefenseReplay(t *testing.T) {
	for _, tr := range v09Traces(t, "enf") {
		rep := v09Report{ID: tr.ID, Replay: "partial", Note: "defender real; executor record stages and store outcomes are fixtures (A-V09-STORE)", Weakened: tr.Weakening != "None"}
		t.Run(tr.ID, func(t *testing.T) {
			n, _, err := v09ReplayEnf(t, tr, tr.replayed())
			rep.Steps = n
			if err != nil {
				rep.Result, rep.Detail = "mismatch", err.Error()
				t.Error(err)
				return
			}
			rep.Result = "match"
			if due, how := v09Continue(t, tr, tr.replayed()); due {
				rep.Detail = "host-only continuation: " + how
			}
		})
		v09Record(t, rep)
	}
}

// ----------------------------------------------------------- claim profile

type v09Round struct {
	S       [2]int  `json:"s"`
	Live3   bool    `json:"live3"`
	Signed  bool    `json:"signed"`
	Sig     [2]bool `json:"sig"`
	Suspect bool    `json:"suspect"`
}

type v09ClaimExpect struct {
	Accepted bool   `json:"accepted"`
	Claimed  uint8  `json:"claimed"`
	Derived  [2]int `json:"derived"`
	Rounds   int    `json:"rounds"`
}

type v09Host struct {
	runner  *Runner
	claimer *Claimer
	rec     *memRecords
}

func v09Mask(seats []uint16) int {
	m := 0
	for _, s := range seats {
		m |= 1 << (s - 1)
	}
	return m
}

func v09ReplayClaim(t *testing.T, tr v09Trace, steps []v09Step) (int, error) {
	failures := 2
	if tr.Profile == "claim-f3" {
		failures = 3
	}
	base, f, _ := runnerFixture(t)
	f.head = 0
	var hosts [2]v09Host
	for h := 0; h < 2; h++ {
		rec := &memRecords{m: map[[32]byte][]byte{}}
		r := &Runner{Chain: f, Schedule: base.Schedule, Wallet: base.Wallet, Base: signing.HeartbeatConfig{Journal: recordsJournal{r: rec}}}
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		hosts[h] = v09Host{runner: r, rec: rec, claimer: &Claimer{Chain: f, Runner: r, Records: rec, LocalSeats: []uint16{uint16(h + 1)}, Key: (*ecdsa.PrivateKey)(key), Failures: failures, Lookback: 64, MinimumActivePercent: 70}}
	}
	ctx := context.Background()
	rounds := 0
	matched := 0
	for _, s := range steps {
		var e v09ClaimExpect
		if err := json.Unmarshal(s.ExpectState, &e); err != nil {
			return matched, err
		}
		switch s.Action {
		case "Round":
			var rr v09Round
			if err := json.Unmarshal(s.Fields, &rr); err != nil {
				return matched, err
			}
			rounds++
			f.head = uint64(rounds)*base.Schedule.RoundBlocks + base.Schedule.FinalityBlocks
			var present []uint16
			for i, p := range []bool{rr.S[0] != 0, rr.S[1] != 0, rr.Live3} {
				if p {
					present = append(present, uint16(i+1))
				}
			}
			for h := 0; h < 2; h++ {
				host := hosts[h]
				round, err := host.runner.Current(ctx)
				if err != nil {
					return matched, err
				}
				switch rr.S[h] {
				case 0:
					continue // down: never starts the round
				case 2:
					host.runner.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
						return signing.HeartbeatResult{}, errors.New("process killed after the start record")
					}
				default:
					signed := rr.Sig[h]
					suspect := rr.Suspect
					host.runner.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
						result := signing.HeartbeatResult{Present: append([]uint16(nil), present...)}
						if signed {
							return result, nil
						}
						e := &frost.Error{Category: frost.ProtocolFailure}
						if suspect {
							e.Suspect = 3
						}
						return result, e
					}
				}
				_, _ = host.runner.Run(ctx, round)
			}
		case "Claim":
			claim, _, err := hosts[0].claimer.Derive(ctx)
			if err == nil {
				var seats []uint16
				var sigs []byte
				for h := 0; h < 2 && err == nil; h++ {
					var own map[uint16][]byte
					own, err = hosts[h].claimer.Sign(ctx, claim)
					for seat, sig := range own {
						seats = append(seats, seat)
						sigs = append(sigs, sig...)
					}
				}
				if err == nil && len(seats) >= 2 {
					err = hosts[0].claimer.Submit(ctx, claim, seats, sigs)
				}
			}
		default:
			return matched, fmt.Errorf("unknown action %s", s.Action)
		}
		// Compare each host's derivation and the chain's accepted claim.
		for h := 0; h < 2; h++ {
			got := 0
			if c, _, err := hosts[h].claimer.Derive(ctx); err == nil {
				got = v09Mask(c.Inactive)
			} else if !errors.Is(err, ErrNoClaim) {
				return matched, fmt.Errorf("step %d host %d derive: %v", s.N, h+1, err)
			}
			if !e.Accepted && got != e.Derived[h] {
				return matched, fmt.Errorf("step %d %s: host %d derives %03b, model %03b", s.N, s.Action, h+1, got, e.Derived[h])
			}
		}
		accepted := len(f.claims) == 1
		if accepted != e.Accepted || (accepted && v09Mask(f.claims[0].Inactive) != int(e.Claimed)) {
			return matched, fmt.Errorf("step %d %s: accepted %v %v, model %v %03b", s.N, s.Action, accepted, f.claims, e.Accepted, e.Claimed)
		}
		matched++
	}
	return matched, nil
}

// TestV09ClaimReplay replays every claim-profile trace on the real Runner and
// Claimer of two hosts.
func TestV09ClaimReplay(t *testing.T) {
	for _, tr := range v09Traces(t, "claim") {
		rep := v09Report{ID: tr.ID, Replay: "full", Note: "heartbeat signer is the Runner sign seam (fixture)", Weakened: tr.Weakening != "None"}
		t.Run(tr.ID, func(t *testing.T) {
			n, err := v09ReplayClaim(t, tr, tr.replayed())
			rep.Steps = n
			if err != nil {
				rep.Result, rep.Detail = "mismatch", err.Error()
				t.Error(err)
				return
			}
			rep.Result = "match"
		})
		v09Record(t, rep)
	}
}

// TestV09ReplayDiscriminates is the harness's own negative control: a weakened
// counterexample's full expectations must not match the current code where the
// weakening changes a transition the replay can observe. DEFENSE weakenings
// change only reachability (a later defender pass), which a single trace does
// not show, and RecordAfterGrant changes the executor's order, which this
// replay applies as fixture writes (the real order is checked at every grant
// by the engine fixture of TestV09HeartbeatReplay in package signing); they
// are logged, not asserted. The DEFENSE counterexamples are discriminated by
// the host-only continuation in TestV09DefenseReplay instead.
func TestV09ReplayDiscriminates(t *testing.T) {
	for _, tr := range append(v09Traces(t, "enf"), v09Traces(t, "claim")...) {
		if tr.Weakening == "None" || tr.Kind != "counterexample" {
			continue
		}
		t.Run(tr.ID, func(t *testing.T) {
			var err error
			if tr.Profile == "enf" {
				_, _, err = v09ReplayEnf(t, tr, tr.Steps)
			} else {
				_, err = v09ReplayClaim(t, tr, tr.Steps)
			}
			observable := tr.Profile != "enf" || tr.Weakening == "DeleteAtSettlement"
			if observable && err == nil {
				t.Fatal("the weakened expectations matched the real code")
			}
			if !observable {
				t.Logf("not observable in one trace (reachability only): %v", err)
			}
		})
	}
}
