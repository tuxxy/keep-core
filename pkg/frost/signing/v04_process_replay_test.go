package signing

// Process replay of declared K-04 application-boundary traces. The subprocess
// runs the actual executor and encrypted store. Wallet/key providers and crypto
// execution are explicit controlled seams; no worker IPC or power loss claim.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"golang.org/x/sys/unix"
)

type v04ProcessFence struct{ path string }
type v04ProcessLease struct {
	path string
	lock *os.File
}

func (f v04ProcessFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	lock, err := os.OpenFile(f.path+".lease", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, store.ErrBusy
	}
	return &v04ProcessLease{f.path, lock}, nil
}
func (l *v04ProcessLease) Head(context.Context) (store.Point, error) {
	var p store.Point
	b, err := os.ReadFile(l.path)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}
func (l *v04ProcessLease) Advance(ctx context.Context, b, a store.Point) error {
	p, err := l.Head(ctx)
	if err != nil {
		return err
	}
	if p != b || a.Sequence != b.Sequence+1 {
		return store.ErrQuarantined
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path+".next", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(l.path+".next", l.path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(l.path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (l *v04ProcessLease) Close() error { return l.lock.Close() }

type v04ProcessConfig struct {
	Root, Mode               string
	Timeout, Reanchor, Fresh bool
}
type v04Decision struct{ Timeout, Reanchor, Cancel, Fail bool }
type v04ProcessObservation struct {
	Boundary                                              string
	Receipt, Intent, Done, DoneValid, Aggregate, ClaimAck bool
	CryptoCalls                                           int
	Unauthorized, Late, Missing, Claimed, ForeignRefused  bool
	Error                                                 string
}
type v04ProcessJournal struct {
	*v04RecoveryJournal
	after func([32]byte) error
}

func (j *v04ProcessJournal) RetainSigningRecord(c context.Context, id [32]byte, b []byte) error {
	if err := j.v04RecoveryJournal.RetainSigningRecord(c, id, b); err != nil {
		return err
	}
	if j.after != nil {
		return j.after(id)
	}
	return nil
}

func TestV04ProcessReplayChild(t *testing.T) {
	path := os.Getenv("V04_PROCESS_REPLAY")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config v04ProcessConfig
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	e, reservation, _, stub := executorFixture(t)
	if config.Fresh {
		e.config.Session[0]++
		e.config.StartBlock++
	}
	if config.Timeout {
		reservation.view.State = "TimeoutPending"
	}
	if config.Reanchor {
		reservation.view.BlockHash[0]++
		reservation.view.CanonicalBlockHash = reservation.view.BlockHash
	}
	c := store.Config{Keystore: filepath.Join(config.Root, "keys"), StorageID: [32]byte{91}, Key: [32]byte{92}, Fence: v04ProcessFence{filepath.Join(config.Root, "fence")}}
	ledger, err := store.Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	scoped, err := ledger.Scope(e.config.Journal.Domain())
	if err != nil {
		t.Fatal(err)
	}
	base := &v04RecoveryJournal{journalStub: e.config.Journal.(*journalStub), durable: scoped}
	journal := &v04ProcessJournal{v04RecoveryJournal: base}
	e.config.Journal = journal
	engine := &v04RecoveryEngine{journal: base, private: stub.private}
	e.engine = engine
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	claimed := false
	observe := func(name string, runErr error) v04ProcessObservation {
		o := v04ProcessObservation{Boundary: name, CryptoCalls: engine.calls, ClaimAck: claimed}
		_, err := journal.ReadSigningRecord(context.Background(), e.receiptID())
		o.Receipt = err == nil
		_, err = journal.ReadSigningRecord(context.Background(), e.Plan().Commitment())
		o.Intent = err == nil
		doneRaw, err := journal.ReadSigningRecord(context.Background(), v04DoneID(e))
		o.Done = err == nil
		if o.Done {
			var done Done
			if done.Unmarshal(doneRaw) == nil {
				attempt, _ := e.attempt(0)
				_, verifyErr := done.Verify(e.doneContext(0, attempt), e.Plan().Snapshot().OutputKey())
				o.DoneValid = verifyErr == nil
			}
		}
		_, err = journal.ReadSigningRecord(context.Background(), e.resultID())
		o.Aggregate = err == nil
		if runErr != nil {
			o.Error = runErr.Error()
		}
		o.Unauthorized = errors.Is(runErr, ErrUnauthorized)
		o.Late = errors.Is(runErr, ErrLateSignature)
		o.Missing = errors.Is(runErr, store.ErrMissing)
		o.Claimed = errors.Is(runErr, store.ErrClaimed)
		return o
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hook := func(name string) error {
		if err := encoder.Encode(observe(name, nil)); err != nil {
			return err
		}
		var d v04Decision
		if err := decoder.Decode(&d); err != nil {
			return err
		}
		if d.Timeout {
			reservation.view.State = "TimeoutPending"
		}
		if d.Reanchor {
			reservation.view.BlockHash[0]++
			reservation.view.CanonicalBlockHash = reservation.view.BlockHash
		}
		if d.Cancel {
			cancel()
		}
		if d.Fail {
			return errors.New("controlled pre-grant interruption")
		}
		return nil
	}
	e.boundary = hook
	journal.after = func(id [32]byte) error {
		switch id {
		case e.receiptID():
			return hook("receipt-durable")
		case e.Plan().Commitment():
			return hook("intent-durable")
		case v04DoneID(e):
			return hook("done-durable")
		case e.resultID():
			return hook("aggregate-durable")
		}
		return fmt.Errorf("unknown public signing record")
	}
	engine.afterClaim = func() {
		claimed = true
		if err := hook("claim-durable"); err != nil {
			t.Fatal(err)
		}
	}
	engine.signed = func() {
		if err := hook("worker-returned"); err != nil {
			t.Fatal(err)
		}
	}
	e.config.Transport = func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
		return transportStub{}, func() {
			if err := hook("transport-cleanup"); err != nil {
				t.Fatal(err)
			}
		}, nil
	}
	var result *Result
	if config.Mode == "recover" {
		result, err = e.LoadResult(ctx)
	} else {
		result, err = e.Run(ctx)
	}
	out := observe("final", err)
	if config.Mode == "foreign" && err == nil {
		result.Done[0].Context.Attempt[0] ^= 1
		out.ForeignRefused = e.ValidateResult(result) != nil && e.Broadcast(ctx, result) != nil
	}
	if config.Mode == "recover" && err == nil {
		if check := e.ValidateResult(result); check != nil {
			t.Fatal(check)
		}
	}
	if err := encoder.Encode(out); err != nil {
		t.Fatal(err)
	}
	ledger.Close()
	os.Exit(0)
}

type v04ProcessCase struct {
	Name, At, Decision, AfterMode            string
	Kill, AfterTimeout, AfterReanchor, Fresh bool
	Expect                                   string
}

func TestV04ProcessBoundaryReplay(t *testing.T) {
	cases := []v04ProcessCase{
		{Name: "authorized-release", Expect: "success"},
		{Name: "foreign-done-context", AfterMode: "foreign", Expect: "foreign"},
		{Name: "post-claim-timeout", At: "before-worker-start", Decision: "timeout", AfterMode: "run", Expect: "unauthorized-and-terminal"},
		{Name: "post-claim-reanchor", At: "before-worker-start", Decision: "reanchor", Expect: "unauthorized"},
		{Name: "cancel-at-worker-return", At: "worker-returned", Decision: "cancel", AfterMode: "recover", Expect: "retained-and-recoverable"},
		{Name: "cancel-during-cleanup", At: "transport-cleanup", Decision: "cancel", AfterMode: "recover", Expect: "retained-and-recoverable"},
		{Name: "kill-after-worker-return", At: "worker-returned", Kill: true, AfterMode: "run", Expect: "missing-done-terminal"},
		{Name: "kill-after-done-ack", At: "done-durable", Kill: true, AfterMode: "recover", Expect: "recovered-without-signing"},
		{Name: "kill-after-aggregate-ack", At: "aggregate-durable", Kill: true, AfterMode: "recover", Expect: "recovered-without-signing"},
		{Name: "receipt-survives-kill-and-reanchor", At: "receipt-durable", Kill: true, AfterMode: "run", AfterReanchor: true, Expect: "unauthorized"},
		{Name: "late-done-quarantine", At: "after-signature", Decision: "timeout", AfterMode: "recover", AfterTimeout: true, Expect: "late"},
		{Name: "fresh-agreed-attempt-after-failed-claim", At: "before-worker-start", Decision: "fail", AfterMode: "run", Fresh: true, Expect: "fresh-success"},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "fence"), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			var observations []v04ProcessObservation
			var mu sync.Mutex
			launch := func(config v04ProcessConfig, interrupt bool) v04ProcessObservation {
				t.Helper()
				config.Root = root
				path := filepath.Join(root, "config.json")
				raw, _ := json.Marshal(config)
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
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
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				decisions := json.NewEncoder(stdin)
				scanner := bufio.NewScanner(stdout)
				scanner.Buffer(make([]byte, 4096), 65536)
				var last v04ProcessObservation
				hit := false
				killed := false
				for scanner.Scan() {
					var event v04ProcessObservation
					if err = json.Unmarshal(scanner.Bytes(), &event); err != nil {
						t.Fatalf("child framing %q: %v stderr=%s", scanner.Text(), err, stderr.String())
					}
					last = event
					mu.Lock()
					observations = append(observations, event)
					mu.Unlock()
					// Compare each offered application boundary with its concrete public facts.
					switch event.Boundary {
					case "receipt-durable":
						if !event.Receipt {
							t.Fatal("receipt acknowledgement absent")
						}
					case "intent-durable", "claim-durable", "before-worker-start":
						if !event.Receipt || !event.Intent {
							t.Fatal("authority before receipt/intent")
						}
					case "worker-returned":
						if !event.Receipt || !event.Intent || !event.ClaimAck || event.CryptoCalls != 1 {
							t.Fatal("returned signature lacks exact durable claim")
						}
					case "done-durable", "after-signature":
						if !event.Done || !event.DoneValid {
							t.Fatal("durable completion/context lost")
						}
					case "aggregate-durable":
						if !event.DoneValid || !event.Aggregate {
							t.Fatal("aggregate lacks verified completion")
						}
					}
					if event.Boundary == "final" {
						break
					}
					decision := v04Decision{}
					if interrupt && event.Boundary == tc.At {
						hit = true
						if tc.Kill {
							cmd.Process.Kill()
							killed = true
							break
						}
						decision.Timeout = tc.Decision == "timeout"
						decision.Reanchor = tc.Decision == "reanchor"
						decision.Cancel = tc.Decision == "cancel"
						decision.Fail = tc.Decision == "fail"
					}
					if err = decisions.Encode(decision); err != nil {
						t.Fatal(err)
					}
				}
				stdin.Close()
				waitErr := cmd.Wait()
				if killed {
					if waitErr == nil {
						t.Fatal("kill did not terminate child")
					}
				} else if waitErr != nil {
					t.Fatalf("child failed %v stderr=%s last=%+v", waitErr, stderr.String(), last)
				}
				if interrupt && tc.At != "" && !hit {
					t.Fatalf("boundary %s not reached", tc.At)
				}
				if !killed && last.Boundary != "final" {
					t.Fatal("missing final observation")
				}
				return last
			}
			mode := "run"
			if tc.Expect == "foreign" {
				mode = "foreign"
			}
			first := launch(v04ProcessConfig{Mode: mode}, true)
			var second v04ProcessObservation
			if tc.AfterMode != "" && tc.Expect != "foreign" {
				second = launch(v04ProcessConfig{Mode: tc.AfterMode, Timeout: tc.AfterTimeout, Reanchor: tc.AfterReanchor, Fresh: tc.Fresh}, false)
			}
			switch tc.Expect {
			case "success":
				if first.Error != "" || !first.DoneValid || !first.Aggregate || first.CryptoCalls != 1 {
					t.Fatal(first)
				}
			case "foreign":
				if !first.ForeignRefused || !first.DoneValid {
					t.Fatal(first)
				}
			case "unauthorized-and-terminal":
				if !first.Unauthorized || first.CryptoCalls != 0 || !second.Claimed || second.CryptoCalls != 0 {
					t.Fatal(first, second)
				}
			case "unauthorized":
				o := first
				if tc.Kill {
					o = second
				}
				if !o.Unauthorized || o.CryptoCalls != 0 {
					t.Fatal(o)
				}
			case "retained-and-recoverable":
				if !first.DoneValid || !first.Late || !second.Aggregate || second.Error != "" || second.CryptoCalls != 0 {
					t.Fatal(first, second)
				}
			case "missing-done-terminal":
				if first.Done || !first.Intent || !second.Claimed || second.CryptoCalls != 0 {
					t.Fatal(first, second)
				}
			case "recovered-without-signing":
				if !first.DoneValid || !second.Aggregate || second.Error != "" || second.CryptoCalls != 0 {
					t.Fatal(first, second)
				}
			case "late":
				if !first.Late || !first.DoneValid || !second.Late || second.CryptoCalls != 0 {
					t.Fatal(first, second)
				}
			case "fresh-success":
				if first.Error == "" || first.CryptoCalls != 0 || second.Error != "" || !second.Aggregate || second.CryptoCalls != 1 {
					t.Fatal(first, second)
				}
			default:
				t.Fatal("unknown expected frame")
			}
			raw, err := json.Marshal(observations)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("V04_PROCESS_REPLAY id=%s class=full-application-boundary scope=controlled-crypto-real-store-process-kill events=%s", tc.Name, raw)
		})
	}
}
