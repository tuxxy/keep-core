// Package runner tests the K-09 lifecycle runner's own mechanisms
// (test/k09/run.sh) on a FIXTURE run: a throwaway keep-core repository with
// this checkout's run.sh and secret patterns, a sibling snowfall repository,
// and stub tools (go, npx, anvil) that build a stub stage binary. The stub
// stages print a passing (or timed-out) TestK09Lifecycle line in well under a
// second, so the whole run takes a few seconds; no chain, node or worker
// starts. Remediation batch 17a, decision D34.
//
// Kind: e2e (it starts run.sh, bash and python3 as processes; it needs git,
// python3, shasum and pgrep, nothing pinned). Each case runs run.sh once.
//
// Invariants (one test each):
//   - #30 (K09-10): after every stage, pass or fail, no process the stage
//     started survives run.sh, and the manifest names, for that stage, the
//     processes the runner had to kill (stages[].leftoverKilled).
//   - #35 (K09-17): the snowfall replacement compiled into the stage binary is
//     bound: the run refuses a dirty snowfall checkout or one that is not at
//     SNOWFALL_COMMIT, and the manifest records repos.snowfall.
//   - #36 (K09-18): manifest.files hashes every file in the evidence
//     directory except manifest.json, and nothing else.
//   - tbtc-v2#19 runner half (K09-19): the build and the stages see an
//     allowlisted environment only (a variable the caller sets but run.sh
//     does not name reaches neither; GOFLAGS is empty for the build), the
//     inputs they need still arrive, and the manifest records the
//     allowlisted environment (manifest.environment).
package runner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const baseline = "40a11d1dcdcf82d3962e430067cfe3f97be81483"

// callerOnly is set in the caller's environment and named nowhere in run.sh.
const callerOnly = "B17A_CALLER_ONLY"

// stageInputs are the variables a K-09 stage process reads (pkg/tbtc
// k09_*/legacy/b6 world): they must still reach every stage.
var stageInputs = []string{
	"SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256", "LEGACY_BASELINE_DIR",
	"LEGACY_TBTC_REPO", "LEGACY_ANVIL", "LEGACY_BITCOIND",
	"LEGACY_BITCOIND_SHA256", "LEGACY_UPGRADED_DIR", "LEGACY_UPGRADED_COMMIT",
	"K09_RUN_ID", "B6_DEBUG_DIR", "PATH", "HOME",
}

var stages = []string{"01-legacy", "02-upgrade", "03-dkg", "04-drain", "05-redemption", "06-restart",
	"07-reservation-timeout", "08-deposit-refund", "09-movement", "10-inactivity",
	"11-cutover-closure", "12-no-inactivity"}

// fixture is one FIXTURE run of run.sh.
type fixture struct {
	t        *testing.T
	root     string // holds keep-core/, snowfall/, stubs/, state/, evidence
	repo     string // FIXTURE keep-core repository
	snowfall string // FIXTURE snowfall repository (the ../snowfall sibling)
	state    string // where the stubs write what they saw (outside the evidence)
	evidence string
	env      []string
	snowHead string
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("FIXTURE: %s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// checkout is the keep-core checkout under test: run.sh and the secret
// patterns are read from its working tree, so a control patch applies.
func checkout(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// stageScript is the stub stage binary that the stub `go test -c` writes. It
// records the variable names it sees (never values) and, per the FIXTURE
// plan, may start a child that outlives it (an Anvil stand-in: same process
// group, no Setpgid, as pkg/tbtc/legacy_world_test.go starts Anvil and
// bitcoind) and may time out as `go test -timeout` does.
const stageScript = `#!/usr/bin/env bash
state=%q
stage=$(printf '%%s\n' "$@" | sed -n 's/^\^TestK09Lifecycle\$\/\^\(.*\)\$$/\1/p')
env | cut -d= -f1 | sort > "$state/env-stage-$stage"
plan=$(grep "^$stage " "$state/plan" | cut -d' ' -f2 || true)
case "$plan" in
  leak-pass|leak-timeout)
    sleep 300 </dev/null >/dev/null 2>&1 &
    echo $! > "$state/child-$stage" ;;
esac
echo "=== RUN   TestK09Lifecycle/$stage"
case "$plan" in
  *timeout)
    echo "panic: test timed out after 12m0s"
    exit 2 ;;
esac
echo "--- PASS: TestK09Lifecycle/$stage (0.01s)"
exit 0
`

func newFixture(t *testing.T, plan map[string]string) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, root: root, repo: filepath.Join(root, "keep-core"),
		snowfall: filepath.Join(root, "snowfall"), state: filepath.Join(root, "state"),
		evidence: filepath.Join(root, "evidence")}
	kc := checkout(t)
	runSh, err := os.ReadFile(filepath.Join(kc, "test/k09/run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	patterns, err := os.ReadFile(filepath.Join(kc, "test/k09/secret-patterns.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// FIXTURE keep-core: run.sh, the secret patterns, an ECDSA lock file and
	// a pkg/tbtc with no K-09 deploy calls.
	write(t, filepath.Join(f.repo, "test/k09/run.sh"), string(runSh), 0o755)
	write(t, filepath.Join(f.repo, "test/k09/secret-patterns.txt"), string(patterns), 0o644)
	write(t, filepath.Join(f.repo, "solidity/ecdsa/yarn.lock"), "# FIXTURE lock\n", 0o644)
	write(t, filepath.Join(f.repo, "pkg/tbtc/doc.go"), "package tbtc\n", 0o644)
	run(t, f.repo, "git", "init", "-q")
	run(t, f.repo, "git", "add", ".")
	run(t, f.repo, "git", "commit", "-q", "-m", "FIXTURE keep-core")
	// FIXTURE snowfall sibling (go.mod's ../snowfall).
	write(t, filepath.Join(f.snowfall, "README"), "FIXTURE snowfall\n", 0o644)
	run(t, f.snowfall, "git", "init", "-q")
	run(t, f.snowfall, "git", "add", ".")
	run(t, f.snowfall, "git", "commit", "-q", "-m", "FIXTURE snowfall")
	f.snowHead = run(t, f.snowfall, "git", "rev-parse", "HEAD")

	var lines []string
	for stage, p := range plan {
		lines = append(lines, stage+" "+p)
	}
	write(t, filepath.Join(f.state, "plan"), strings.Join(lines, "\n")+"\n", 0o644)
	stubs := filepath.Join(root, "stubs")
	// Stub go: `go version` and `go test -c -o <out> ./pkg/tbtc`. It records
	// the names it sees and GOFLAGS' value, then writes the stub stage.
	write(t, filepath.Join(stubs, "go"), fmt.Sprintf(`#!/usr/bin/env bash
state=%q
if [ "$1" = version ]; then echo "go version b17a-fixture"; exit 0; fi
env | cut -d= -f1 | sort > "$state/env-build"
printf '%%s' "${GOFLAGS-}" > "$state/goflags-build"
out=""; while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
cat > "$out" < "$state/stage-script"
chmod +x "$out"
`, f.state), 0o755)
	write(t, filepath.Join(f.state, "stage-script"), fmt.Sprintf(stageScript, f.state), 0o644)
	write(t, filepath.Join(stubs, "npx"), "#!/usr/bin/env bash\nmkdir -p build\n", 0o755)
	anvil := filepath.Join(root, "tools/anvil")
	write(t, anvil, "#!/usr/bin/env bash\necho 'anvil 0.0.0-b17a-fixture'\n", 0o755)
	bitcoind := filepath.Join(root, "tools/bitcoind")
	write(t, bitcoind, "FIXTURE bitcoind\n", 0o755)
	worker := filepath.Join(root, "tools/worker")
	write(t, worker, "FIXTURE worker\n", 0o755)
	upgraded := "1111111111111111111111111111111111111111"
	write(t, filepath.Join(root, "baseline/manifest.json"), `{"tbtcCommit":"`+baseline+`"}`, 0o644)
	write(t, filepath.Join(root, "upgraded/manifest.json"), `{"tbtcCommit":"`+upgraded+`"}`, 0o644)
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	bd, _ := os.ReadFile(bitcoind)
	wk, _ := os.ReadFile(worker)
	tmp := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	f.env = []string{
		"PATH=" + stubs + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + tmp,
		"SNOWFALL_WORKER=" + worker, "SNOWFALL_WORKER_SHA256=" + sha(wk),
		"SNOWFALL_COMMIT=" + f.snowHead,
		"LEGACY_BASELINE_DIR=" + filepath.Join(root, "baseline"),
		"LEGACY_UPGRADED_DIR=" + filepath.Join(root, "upgraded"),
		"LEGACY_UPGRADED_COMMIT=" + upgraded,
		"LEGACY_TBTC_REPO=" + f.repo,
		"LEGACY_ANVIL=" + anvil,
		"LEGACY_BITCOIND=" + bitcoind, "LEGACY_BITCOIND_SHA256=" + sha(bd),
		"K09_ECDSA_NODE_MODULES=" + filepath.Join(root, "node_modules"),
		"K09_ECDSA_NODE_LOCK=" + filepath.Join(f.repo, "solidity/ecdsa/yarn.lock"),
		callerOnly + "=1",
		"GOFLAGS=-tags=b17a_caller",
	}
	t.Cleanup(f.killChildren)
	return f
}

// killChildren stops, by PID, any stub child a run left behind.
func (f *fixture) killChildren() {
	matches, _ := filepath.Glob(filepath.Join(f.state, "child-*"))
	for _, m := range matches {
		raw, _ := os.ReadFile(m)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 1 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// result is one finished FIXTURE run.
type result struct {
	exit     int
	output   string
	manifest map[string]any
}

// runK09 runs the FIXTURE repository's run.sh in its own process group,
// bounded at 60 s, and reads the manifest if it wrote one.
func (f *fixture) runK09() result {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(f.repo, "test/k09/run.sh"), f.evidence)
	cmd.Dir = f.repo
	cmd.Env = f.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		f.t.Fatalf("FIXTURE: run.sh did not finish in 60 s:\n%s", out)
	}
	// The run's own process group is swept only at cleanup, after the
	// assertions: sweeping it here would hide a leaked stage child.
	if cmd.Process != nil {
		pgid := cmd.Process.Pid
		f.t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	}
	r := result{output: string(out)}
	if exitErr, ok := err.(*exec.ExitError); ok {
		r.exit = exitErr.ExitCode()
	} else if err != nil {
		f.t.Fatalf("FIXTURE: run.sh: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(f.evidence, "manifest.json")); err == nil {
		if err := json.Unmarshal(raw, &r.manifest); err != nil {
			f.t.Fatalf("FIXTURE: manifest.json: %v", err)
		}
	}
	return r
}

// requireManifest fails the case if the run wrote no manifest (a fixture or
// setup problem, not the property under test).
func (r result) requireManifest(t *testing.T) map[string]any {
	t.Helper()
	if r.manifest == nil {
		t.Fatalf("FIXTURE: run.sh wrote no manifest (exit %d):\n%s", r.exit, r.output)
	}
	return r.manifest
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func childPID(t *testing.T, f *fixture, stage string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.state, "child-"+stage))
	if err != nil {
		t.Fatalf("FIXTURE: stage %s did not start its child: %v", stage, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func stageEntry(t *testing.T, m map[string]any, stage string) map[string]any {
	t.Helper()
	list, _ := m["stages"].([]any)
	for _, s := range list {
		e, _ := s.(map[string]any)
		if e["stage"] == stage {
			return e
		}
	}
	t.Fatalf("FIXTURE: the manifest has no stage %s", stage)
	return nil
}

// TestK09RunnerStageCleanup: after every stage, pass or fail, no process the
// stage started survives run.sh, and the manifest names the ones the runner
// killed for that stage.
//
// Refs tuxxy/keep-core#30
func TestK09RunnerStageCleanup(t *testing.T) {
	t.Parallel()
	for _, plan := range []string{"leak-timeout", "leak-pass"} {
		plan := plan
		t.Run(plan, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, map[string]string{"03-dkg": plan})
			r := f.runK09()
			m := r.requireManifest(t)
			pid := childPID(t, f, "03-dkg")
			// A killed child may need a moment to be reaped by init.
			deadline := time.Now().Add(2 * time.Second)
			for alive(pid) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if alive(pid) {
				t.Errorf("K09-10: after run.sh returned, stage 03-dkg's child process %d (%s; an Anvil/bitcoind stand-in in the stage's process group) is still alive", pid, plan)
			}
			killed, _ := stageEntry(t, m, "03-dkg")["leftoverKilled"].([]any)
			found := false
			for _, k := range killed {
				if strings.HasPrefix(fmt.Sprint(k), strconv.Itoa(pid)+" ") || fmt.Sprint(k) == strconv.Itoa(pid) {
					found = true
				}
			}
			if !found {
				t.Errorf("K09-10: the manifest's stage 03-dkg leftoverKilled %v does not name the child %d the runner had to kill (%s)", killed, pid, plan)
			}
			// A stage that leaked nothing records nothing.
			if other, _ := stageEntry(t, m, "02-upgrade")["leftoverKilled"].([]any); len(other) != 0 {
				t.Errorf("K09-10: stage 02-upgrade started no child, but leftoverKilled is %v", other)
			}
		})
	}
}

func problems(r result) string {
	if r.manifest == nil {
		return ""
	}
	return fmt.Sprint(r.manifest["problems"])
}

// TestK09RunnerSnowfallBinding: the snowfall replacement compiled into the
// stage binary is pinned and recorded.
//
// Refs tuxxy/keep-core#35
func TestK09RunnerSnowfallBinding(t *testing.T) {
	t.Parallel()
	t.Run("pinned-and-clean", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, nil)
		r := f.runK09()
		m := r.requireManifest(t)
		repos, _ := m["repos"].(map[string]any)
		if got := fmt.Sprint(repos["snowfall"]); got != f.snowHead {
			t.Errorf("K09-17: manifest.repos.snowfall is %q; the run compiled snowfall at %s and must record it", got, f.snowHead)
		}
		if strings.Contains(strings.ToLower(problems(r)), "snowfall") || r.exit != 0 {
			t.Errorf("K09-17: a clean snowfall at SNOWFALL_COMMIT was refused (exit %d, problems %s):\n%s", r.exit, problems(r), r.output)
		}
	})
	refused := func(t *testing.T, f *fixture, what string) {
		t.Helper()
		r := f.runK09()
		said := strings.ToLower(r.output + problems(r))
		if r.exit == 0 || !strings.Contains(said, "snowfall") {
			t.Errorf("K09-17: the run accepted %s (exit %d; refusal must name snowfall):\n%s", what, r.exit, r.output)
		}
	}
	t.Run("dirty", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, nil)
		write(t, filepath.Join(f.snowfall, "README"), "FIXTURE snowfall, uncommitted edit\n", 0o644)
		refused(t, f, "a snowfall checkout with an uncommitted edit")
	})
	t.Run("other-commit", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, nil)
		for i, e := range f.env {
			if strings.HasPrefix(e, "SNOWFALL_COMMIT=") {
				f.env[i] = "SNOWFALL_COMMIT=2222222222222222222222222222222222222222"
			}
		}
		refused(t, f, "a snowfall checkout that is not at SNOWFALL_COMMIT")
	})
}

// TestK09RunnerEvidenceInventory: manifest.files hashes every file in the
// evidence directory except manifest.json, and nothing else.
//
// Refs tuxxy/keep-core#36
func TestK09RunnerEvidenceInventory(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	r := f.runK09()
	m := r.requireManifest(t)
	recorded, ok := m["files"].(map[string]any)
	if !ok {
		t.Fatalf("K09-18: the manifest has no files inventory (manifest.files); evidence files such as tbtc.test, ecdsa-build.log and cases.tsv are unhashed")
	}
	actual := map[string]string{}
	err := filepath.Walk(f.evidence, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(f.evidence, path)
		if rel == "manifest.json" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		actual[rel] = sha(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range actual {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if got := fmt.Sprint(recorded[n]); got != actual[n] {
			t.Errorf("K09-18: evidence file %s is not hashed in manifest.files (recorded %q, file sha256 %s)", n, got, actual[n])
		}
	}
	for n := range recorded {
		if _, ok := actual[n]; !ok {
			t.Errorf("K09-18: manifest.files names %s, which is not a file in the evidence directory", n)
		}
	}
	if _, ok := recorded["manifest.json"]; ok {
		t.Errorf("K09-18: manifest.files hashes manifest.json itself")
	}
}

func names(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("FIXTURE: %v", err)
	}
	defer file.Close()
	seen := map[string]bool{}
	s := bufio.NewScanner(file)
	for s.Scan() {
		seen[s.Text()] = true
	}
	return seen
}

// TestK09RunnerEnvironment: the build and the stages run with an allowlisted
// environment, the inputs they need still arrive, and the manifest records
// the allowlist.
//
// Refs tuxxy/tbtc-v2#19 (runner half)
func TestK09RunnerEnvironment(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	r := f.runK09()
	m := r.requireManifest(t)
	build := names(t, filepath.Join(f.state, "env-build"))
	if build[callerOnly] {
		t.Errorf("K09-19: the stage binary build (go test -c) saw %s, a variable the caller set and run.sh does not name", callerOnly)
	}
	if flags, _ := os.ReadFile(filepath.Join(f.state, "goflags-build")); len(flags) != 0 {
		t.Errorf("K09-19: the stage binary build saw the caller's GOFLAGS=%q (it changes the binary)", flags)
	}
	for _, stage := range stages {
		seen := names(t, filepath.Join(f.state, "env-stage-"+stage))
		if seen[callerOnly] {
			t.Errorf("K09-19: stage %s saw %s, a variable the caller set and run.sh does not name", stage, callerOnly)
		}
		for _, need := range stageInputs {
			if !seen[need] {
				t.Errorf("K09-19: stage %s did not receive %s, which the stage reads (allowlist too narrow)", stage, need)
			}
		}
	}
	recorded, ok := m["environment"].(map[string]any)
	if !ok {
		t.Fatalf("K09-19: the manifest does not record the environment the build and stages ran with (manifest.environment)")
	}
	if _, ok := recorded[callerOnly]; ok {
		t.Errorf("K09-19: manifest.environment records %s, which is not allowlisted", callerOnly)
	}
	for _, e := range f.env {
		name, value, _ := strings.Cut(e, "=")
		if strings.HasPrefix(name, "LEGACY_") || strings.HasPrefix(name, "SNOWFALL_") {
			if got := fmt.Sprint(recorded[name]); got != value {
				t.Errorf("K09-19: manifest.environment records %s=%q, the run had %q", name, got, value)
			}
		}
	}
}
