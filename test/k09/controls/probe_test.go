// Package controls checks K-09 probe records (remediation batch 17c,
// tuxxy/keep-core#60, K09-45): a wrong-behaviour probe result (README.md
// here) may be cited as evidence only through a record that binds it to the
// head it ran on.
//
// Invariant (#60): every cited probe record carries
//   - head (the full commit the probe ran on) and clean (true: the tree had no
//     uncommitted change);
//   - patch.sha256 (the SHA-256 of the probe patch applied; patch.path, when
//     it names a file at head, must have exactly that content);
//   - testFiles: the SHA-256 of every pkg/tbtc/k09_* file at head, and only
//     those (as test/k09/run.sh's manifest.testFiles, keyed by file name);
//   - stage (one of run.sh's stages), log (a file in the record directory)
//     and logSha256 (that file's SHA-256);
//   - failure: the stage's failing line(s), each a test-file assertion line
//     ("<file>_test.go:<n>: ...") that is in the log, and the log shows
//     "--- FAIL: TestK09Lifecycle/<stage>".
//
// The records are written by test/k09/controls/run-probe.sh:
//
//	run-probe.sh <patch> <stage> <new record directory>
//
// It applies the patch to an export of HEAD (never to the checkout), runs
// stages 01-06 and <stage> with go test, and writes <dir>/probe.json and the
// log it names. It refuses an existing record directory. A probe that does
// not fail at <stage>, a patch that does not apply, an unknown stage or a
// dirty tree give no citable record (none, or one checkProbeRecord refuses).
//
// Tests:
//   - TestK09ProbeRecord: checks a real record (K09_PROBE_RECORD=<dir>) in
//     the checkout it names (as test/k09/evidence does). Not part of the
//     batch suite.
//   - TestB17cProbeRecordChecker (unit): the checker refuses a SYNTHETIC
//     record that lacks or misstates any field above, and accepts a correct
//     one. Passes at base (it controls this file's own checker).
//   - TestB17cRunProbe (e2e): run-probe.sh on a FIXTURE repository with a
//     stub go (no chain, node or K-09 stage: the stub prints the stage
//     results the patched test file would give) writes a record the checker
//     accepts and whose every value is the truth. At base it fails at "is
//     missing": there is no run-probe.sh.
package controls

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

var k09Stages = []string{"01-legacy", "02-upgrade", "03-dkg", "04-drain", "05-redemption", "06-restart",
	"07-reservation-timeout", "08-deposit-refund", "09-movement", "10-inactivity",
	"11-cutover-closure", "12-no-inactivity"}

var (
	hex64      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hex40      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	assertLine = regexp.MustCompile(`_test\.go:\d+: `)
)

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// probeRecord is <dir>/probe.json. Pointers distinguish an absent field.
type probeRecord struct {
	Head  *string `json:"head"`
	Clean *bool   `json:"clean"`
	Patch *struct {
		Path   string `json:"path"`
		Sha256 string `json:"sha256"`
	} `json:"patch"`
	TestFiles map[string]string `json:"testFiles"`
	Stage     *string           `json:"stage"`
	Log       *string           `json:"log"`
	LogSha256 *string           `json:"logSha256"`
	Failure   json.RawMessage   `json:"failure"`
}

func gitOut(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	return string(out), err
}

// checkProbeRecord returns every reason the probe record in dir cannot be
// cited as a result at its head in repo (none: it can).
func checkProbeRecord(dir, repo string) []string {
	const tag = "K-09 #60 (K09-45)"
	raw, err := os.ReadFile(filepath.Join(dir, "probe.json"))
	if err != nil {
		return []string{fmt.Sprintf("%s: no probe record: %v", tag, err)}
	}
	var r probeRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return []string{fmt.Sprintf("%s: probe.json: %v", tag, err)}
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, tag+": "+fmt.Sprintf(format, args...)) }

	head := ""
	switch {
	case r.Head == nil:
		add("the record names no head")
	case !hex40.MatchString(*r.Head):
		add("head %q is not a full commit ID", *r.Head)
	default:
		if t, err := gitOut(repo, "cat-file", "-t", *r.Head); err != nil || strings.TrimSpace(t) != "commit" {
			add("head %s is not a commit of %s", *r.Head, repo)
		} else {
			head = *r.Head
		}
	}
	switch {
	case r.Clean == nil:
		add("the record does not say whether the tree was clean")
	case !*r.Clean:
		add("the probe ran on a tree with uncommitted changes (clean false): the result is not bound to head")
	}
	switch {
	case r.Patch == nil:
		add("the record names no patch")
	case !hex64.MatchString(r.Patch.Sha256):
		add("the patch has no SHA-256 (%q)", r.Patch.Sha256)
	case head != "" && r.Patch.Path != "":
		if content, err := exec.Command("git", "-C", repo, "cat-file", "blob", head+":"+r.Patch.Path).Output(); err == nil && sha(content) != r.Patch.Sha256 {
			add("patch %s at head has SHA-256 %s, but the record says %s", r.Patch.Path, sha(content), r.Patch.Sha256)
		}
	}
	if r.TestFiles == nil {
		add("the record has no testFiles (the pkg/tbtc/k09_* files the probe ran)")
	} else if head != "" {
		listing, err := gitOut(repo, "ls-tree", "--name-only", head, "pkg/tbtc/")
		if err != nil {
			add("cannot list pkg/tbtc at %s: %v", head, err)
		}
		want := map[string]string{}
		for _, p := range strings.Fields(listing) {
			if strings.HasPrefix(filepath.Base(p), "k09_") {
				content, _ := exec.Command("git", "-C", repo, "cat-file", "blob", head+":"+p).Output()
				want[filepath.Base(p)] = sha(content)
			}
		}
		got := map[string]string{}
		for k, v := range r.TestFiles {
			got[filepath.Base(k)] = v
		}
		for _, name := range sortedKeys(want) {
			if v, ok := got[name]; !ok {
				add("testFiles has no hash of pkg/tbtc/%s", name)
			} else if v != want[name] {
				add("testFiles records %s as %s, but head has %s", name, v, want[name])
			}
		}
		for _, name := range sortedKeys(got) {
			if _, ok := want[name]; !ok {
				add("testFiles lists %s, which is not a pkg/tbtc/k09_* file at head", name)
			}
		}
	}
	stage := ""
	if r.Stage == nil {
		add("the record names no stage")
	} else if !contains(k09Stages, *r.Stage) {
		add("stage %q is not a K-09 stage", *r.Stage)
	} else {
		stage = *r.Stage
	}
	log := ""
	switch {
	case r.Log == nil || r.LogSha256 == nil:
		add("the record names no log or no logSha256")
	case filepath.IsAbs(*r.Log) || strings.Contains(*r.Log, ".."):
		add("log %q is not a file in the record directory", *r.Log)
	default:
		content, err := os.ReadFile(filepath.Join(dir, *r.Log))
		switch {
		case err != nil:
			add("log %s: %v", *r.Log, err)
		case sha(content) != *r.LogSha256:
			add("log %s has SHA-256 %s, but logSha256 records %s", *r.Log, sha(content), *r.LogSha256)
		default:
			log = string(content)
		}
	}
	var failure []string
	if len(r.Failure) == 0 || string(r.Failure) == "null" {
		add("the record has no failure line")
	} else {
		var one string
		if json.Unmarshal(r.Failure, &one) == nil {
			failure = []string{one}
		} else if json.Unmarshal(r.Failure, &failure) != nil {
			add("failure is neither a line nor a list of lines")
		}
		named := false
		for _, f := range failure {
			if strings.TrimSpace(f) != "" && assertLine.MatchString(f) {
				named = true
				if log != "" && !strings.Contains(log, strings.TrimSpace(f)) {
					add("failure line %q is not in the log", f)
				}
			}
		}
		if !named {
			add("failure %s names no test-file assertion line", string(r.Failure))
		}
	}
	if log != "" && stage != "" && !regexp.MustCompile(`--- FAIL: TestK09Lifecycle/`+regexp.QuoteMeta(stage)+` `).MatchString(log) {
		add("the log does not show stage %s failing (--- FAIL: TestK09Lifecycle/%s)", stage, stage)
	}
	return problems
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func checkout(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// TestK09ProbeRecord checks a real probe record (K09_PROBE_RECORD) in this
// checkout. A missing variable is a failure, not a skip.
// Refs tuxxy/keep-core#60.
func TestK09ProbeRecord(t *testing.T) {
	dir := os.Getenv("K09_PROBE_RECORD")
	if dir == "" {
		t.Fatal("K09_PROBE_RECORD (a run-probe.sh record directory) is required")
	}
	for _, p := range checkProbeRecord(dir, checkout(t)) {
		t.Error(p)
	}
}

// synthetic writes a SYNTHETIC record (FIXTURE: no probe ran) for this
// checkout's HEAD and the committed probe b1-deposit-any-spend.patch.
func synthetic(t *testing.T, repo string, edit func(r map[string]any, dir string)) string {
	t.Helper()
	head, err := gitOut(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head = strings.TrimSpace(head)
	patch, err := exec.Command("git", "-C", repo, "cat-file", "blob", head+":test/k09/controls/b1-deposit-any-spend.patch").Output()
	if err != nil {
		t.Fatal(err)
	}
	listing, _ := gitOut(repo, "ls-tree", "--name-only", head, "pkg/tbtc/")
	files := map[string]any{}
	for _, p := range strings.Fields(listing) {
		if strings.HasPrefix(filepath.Base(p), "k09_") {
			content, _ := exec.Command("git", "-C", repo, "cat-file", "blob", head+":"+p).Output()
			files[filepath.Base(p)] = sha(content)
		}
	}
	if len(files) == 0 {
		t.Fatal("FIXTURE: HEAD has no pkg/tbtc/k09_* file")
	}
	dir := t.TempDir()
	failLine := "k09_lifecycle_test.go:529: K-09 08-deposit-refund (D13): the deposit sweep 48a5 does not spend the wallet's main"
	log := "=== RUN   TestK09Lifecycle/08-deposit-refund\n    " + failLine + "\n--- FAIL: TestK09Lifecycle/08-deposit-refund (47.80s)\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.log"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	r := map[string]any{
		"head": head, "clean": true,
		"patch":     map[string]any{"path": "test/k09/controls/b1-deposit-any-spend.patch", "sha256": sha(patch)},
		"testFiles": files, "stage": "08-deposit-refund", "log": "probe.log", "logSha256": sha([]byte(log)),
		"exit": 1, "failure": []any{failLine},
	}
	if edit != nil {
		edit(r, dir)
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "probe.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestB17cProbeRecordChecker (unit): a record without any of the binding
// fields, or with one that is false, is refused at its named text; a correct
// record is accepted. Refs tuxxy/keep-core#60.
func TestB17cProbeRecordChecker(t *testing.T) {
	repo := checkout(t)
	if p := checkProbeRecord(synthetic(t, repo, nil), repo); len(p) != 0 {
		t.Fatalf("K-09 #60 checker: a correct SYNTHETIC record is refused: %v", p)
	}
	firstFile := func(r map[string]any) string {
		var names []string
		for k := range r["testFiles"].(map[string]any) {
			names = append(names, k)
		}
		sort.Strings(names)
		return names[0]
	}
	cases := []struct {
		name, want string
		edit       func(r map[string]any, dir string)
	}{
		{"no-head", "names no head", func(r map[string]any, _ string) { delete(r, "head") }},
		{"short-head", "is not a full commit ID", func(r map[string]any, _ string) { r["head"] = r["head"].(string)[:9] }},
		{"unknown-head", "is not a commit of", func(r map[string]any, _ string) { r["head"] = strings.Repeat("e", 40) }},
		{"no-clean", "whether the tree was clean", func(r map[string]any, _ string) { delete(r, "clean") }},
		{"dirty", "uncommitted changes", func(r map[string]any, _ string) { r["clean"] = false }},
		{"no-patch", "names no patch", func(r map[string]any, _ string) { delete(r, "patch") }},
		{"no-patch-hash", "the patch has no SHA-256", func(r map[string]any, _ string) { delete(r["patch"].(map[string]any), "sha256") }},
		{"other-patch-hash", "but the record says", func(r map[string]any, _ string) {
			r["patch"].(map[string]any)["sha256"] = sha([]byte("another patch"))
		}},
		{"no-test-files", "has no testFiles", func(r map[string]any, _ string) { delete(r, "testFiles") }},
		{"test-file-missing", "testFiles has no hash of", func(r map[string]any, _ string) {
			delete(r["testFiles"].(map[string]any), firstFile(r))
		}},
		{"test-file-other-hash", "but head has", func(r map[string]any, _ string) {
			r["testFiles"].(map[string]any)[firstFile(r)] = sha([]byte("patched"))
		}},
		{"test-file-extra", "which is not a pkg/tbtc/k09_* file at head", func(r map[string]any, _ string) {
			r["testFiles"].(map[string]any)["k09_phantom_test.go"] = sha([]byte("phantom"))
		}},
		{"no-stage", "names no stage", func(r map[string]any, _ string) { delete(r, "stage") }},
		{"unknown-stage", "is not a K-09 stage", func(r map[string]any, _ string) { r["stage"] = "13-phantom" }},
		{"no-log-hash", "no logSha256", func(r map[string]any, _ string) { delete(r, "logSha256") }},
		{"log-changed", "but logSha256 records", func(_ map[string]any, dir string) {
			f, _ := os.OpenFile(filepath.Join(dir, "probe.log"), os.O_APPEND|os.O_WRONLY, 0)
			f.WriteString("edited after the record\n")
			f.Close()
		}},
		{"log-outside", "is not a file in the record directory", func(r map[string]any, _ string) { r["log"] = "../probe.log" }},
		{"no-failure", "has no failure line", func(r map[string]any, _ string) { delete(r, "failure") }},
		{"empty-failure", "names no test-file assertion line", func(r map[string]any, _ string) { r["failure"] = []any{} }},
		{"failure-not-in-log", "is not in the log", func(r map[string]any, _ string) {
			r["failure"] = "k09_lifecycle_test.go:1: a line the probe never printed"
		}},
		{"stage-did-not-fail", "does not show stage", func(r map[string]any, _ string) { r["stage"] = "09-movement" }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			problems := checkProbeRecord(synthetic(t, repo, c.edit), repo)
			for _, p := range problems {
				if strings.Contains(p, c.want) {
					return
				}
			}
			t.Errorf("K-09 #60 checker %s: the record is not refused at %q; problems: %v", c.name, c.want, problems)
		})
	}
}

// probeFixture is one FIXTURE keep-core repository with this checkout's
// run-probe.sh, a sibling snowfall repository and a stub go.
type probeFixture struct {
	root, repo, state string
	env               []string
	snowHead          string
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir, name string, args ...string) string {
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

// lifecycleFixture is the FIXTURE k09_lifecycle_test.go. A probe patch adds a
// "B17C-PROBE <stage> <fail|pass>" line; the stub go reports <stage> failing
// at fixtureFailLine only when the tree it runs on has a "fail" line.
const lifecycleFixture = "package tbtc\n\n// FIXTURE K-09 lifecycle test (remediation batch 17c).\n"

func fixtureFailLine(stage string) string {
	return fmt.Sprintf("k09_lifecycle_test.go:525: K-09 %s: FIXTURE probe assertion (B17C)", stage)
}

// probePatch is a git patch adding the marker line to the fixture test file;
// broken gives it a context line the file does not have.
func probePatch(stage, verdict string, broken bool) string {
	context := "// FIXTURE K-09 lifecycle test (remediation batch 17c)."
	if broken {
		context = "// a line the fixture does not have"
	}
	return fmt.Sprintf(`diff --git a/pkg/tbtc/k09_lifecycle_test.go b/pkg/tbtc/k09_lifecycle_test.go
--- a/pkg/tbtc/k09_lifecycle_test.go
+++ b/pkg/tbtc/k09_lifecycle_test.go
@@ -1,3 +1,4 @@
 package tbtc

 %s
+// B17C-PROBE %s %s
`, context, stage, verdict)
}

// stubStages prints the TestK09Lifecycle result of every stage the -run
// pattern selects (Go's per-level unanchored match, by Python re), failing
// the stage named by a "fail" marker in the test file the tree has.
const stubStages = `import os, re, sys
stages = %s
tree = sys.argv[1]
args = sys.argv[2:]
pattern = None
for i, a in enumerate(args):
    for flag in ("-run", "-test.run", "--run", "--test.run"):
        if a == flag and i + 1 < len(args):
            pattern = args[i + 1]
        elif a.startswith(flag + "="):
            pattern = a.split("=", 1)[1]
marker = None
for name in ("pkg/tbtc/k09_lifecycle_test.go", "k09_lifecycle_test.go"):
    path = os.path.join(tree, name)
    if os.path.exists(path):
        m = re.search(r"^// B17C-PROBE (\S+) (fail|pass)$", open(path).read(), re.M)
        marker = m.groups() if m else None
        break
open(os.path.join(%q, "probe-runs"), "a").write("%%s %%s\n" %% (pattern, marker))
# go test splits -run at each "/" outside brackets and parentheses.
levels, depth, current = [], 0, ""
for ch in pattern or "":
    if ch in "([":
        depth += 1
    elif ch in ")]":
        depth -= 1
    if ch == "/" and depth == 0:
        levels.append(current)
        current = ""
    else:
        current += ch
levels.append(current)
sub = levels[1] if len(levels) > 1 else ""
print("=== RUN   TestK09Lifecycle")
code = 0
for stage in stages:
    if sub and not re.search(sub, stage):
        continue
    print("=== RUN   TestK09Lifecycle/" + stage)
    if marker and marker[0] == stage and marker[1] == "fail":
        print("    " + %q.replace("STAGE", stage))
        print("--- FAIL: TestK09Lifecycle/%%s (0.01s)" %% stage)
        code = 1
    else:
        print("--- PASS: TestK09Lifecycle/%%s (0.01s)" %% stage)
print("FAIL" if code else "PASS")
sys.exit(code)
`

func newProbeFixture(t *testing.T) *probeFixture {
	t.Helper()
	kc := checkout(t)
	script, err := os.ReadFile(filepath.Join(kc, "test/k09/controls/run-probe.sh"))
	if err != nil {
		t.Fatalf("K-09 #60: test/k09/controls/run-probe.sh is missing (%v): nothing writes a probe record bound to the head", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &probeFixture{root: root, repo: filepath.Join(root, "keep-core"), state: filepath.Join(root, "state")}
	// FIXTURE keep-core: this checkout's run-probe.sh, run.sh and secret
	// patterns, three k09_* test files and two probe patches.
	write(t, filepath.Join(f.repo, "test/k09/controls/run-probe.sh"), string(script), 0o755)
	for _, p := range []string{"test/k09/run.sh", "test/k09/secret-patterns.txt"} {
		content, err := os.ReadFile(filepath.Join(kc, p))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(f.repo, p), string(content), 0o755)
	}
	write(t, filepath.Join(f.repo, "pkg/tbtc/k09_lifecycle_test.go"), lifecycleFixture, 0o644)
	write(t, filepath.Join(f.repo, "pkg/tbtc/k09_world_test.go"), "package tbtc\n\n// FIXTURE K-09 world.\n", 0o644)
	write(t, filepath.Join(f.repo, "pkg/tbtc/k09_export_test.go"), "package tbtc\n\n// FIXTURE K-09 export.\n", 0o644)
	write(t, filepath.Join(f.repo, "pkg/tbtc/doc.go"), "package tbtc\n", 0o644)
	write(t, filepath.Join(f.repo, "test/k09/controls/b17c-fail.patch"), probePatch("08-deposit-refund", "fail", false), 0o644)
	write(t, filepath.Join(f.repo, "test/k09/controls/b17c-pass.patch"), probePatch("08-deposit-refund", "pass", false), 0o644)
	write(t, filepath.Join(f.repo, "test/k09/controls/b17c-broken.patch"), probePatch("08-deposit-refund", "fail", true), 0o644)
	write(t, filepath.Join(f.repo, "solidity/ecdsa/yarn.lock"), "# FIXTURE lock\n", 0o644)
	write(t, filepath.Join(f.repo, "go.mod"), "module github.com/keep-network/keep-core\n\ngo 1.21\n\nreplace github.com/threshold-network/snowfall/clients/go => ../snowfall/clients/go\n", 0o644)
	run(t, f.repo, "git", "init", "-q")
	run(t, f.repo, "git", "add", ".")
	run(t, f.repo, "git", "commit", "-q", "-m", "FIXTURE keep-core")
	snowfall := filepath.Join(root, "snowfall")
	write(t, filepath.Join(snowfall, "clients/go/go.mod"), "module github.com/threshold-network/snowfall/clients/go\n\ngo 1.21\n", 0o644)
	run(t, snowfall, "git", "init", "-q")
	run(t, snowfall, "git", "add", ".")
	run(t, snowfall, "git", "commit", "-q", "-m", "FIXTURE snowfall")
	f.snowHead = run(t, snowfall, "git", "rev-parse", "HEAD")

	// Stub go: `go test -c -o <out>` writes a stub test binary that prints
	// the stages the patched tree gives; `go test` (no -c) prints them
	// itself. Every other call runs the real go.
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("FIXTURE: no go on PATH: %v", err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("FIXTURE: no python3 on PATH: %v", err)
	}
	stagesPy, _ := json.Marshal(k09Stages)
	write(t, filepath.Join(f.state, "stages.py"), fmt.Sprintf(stubStages, stagesPy, f.state, fixtureFailLine("STAGE")), 0o644)
	stubs := filepath.Join(root, "stubs")
	write(t, filepath.Join(stubs, "go"), fmt.Sprintf(`#!/usr/bin/env bash
state=%q
py=%q
[ "${1-}" = test ] || exec %q "$@"
shift
if printf '%%s\n' "$@" | grep -qx -- -c; then
  out=""; for ((i=1; i<=$#; i++)); do [ "${!i}" = -o ] && { j=$((i+1)); out=${!j}; }; done
  tree=$(pwd)
  printf '#!/usr/bin/env bash\nexec %%q %%q %%q "$@"\n' "$py" "$state/stages.py" "$tree" > "$out"
  chmod +x "$out"
  exit 0
fi
exec "$py" "$state/stages.py" "$(pwd)" "$@"
`, f.state, python, realGo), 0o755)
	tools := filepath.Join(root, "tools")
	write(t, filepath.Join(tools, "anvil"), "#!/usr/bin/env bash\necho 'anvil 0.0.0-b17c-fixture'\n", 0o755)
	write(t, filepath.Join(tools, "bitcoind"), "#!/usr/bin/env bash\necho 'FIXTURE bitcoind v0.0.0-b17c'\n", 0o755)
	write(t, filepath.Join(tools, "worker"), "#!/usr/bin/env bash\necho 'FIXTURE worker 0.0.0-b17c'\n", 0o755)
	upgraded := "1111111111111111111111111111111111111111"
	write(t, filepath.Join(root, "baseline/manifest.json"), `{"tbtcCommit":"40a11d1dcdcf82d3962e430067cfe3f97be81483"}`, 0o644)
	write(t, filepath.Join(root, "upgraded/manifest.json"), `{"tbtcCommit":"`+upgraded+`"}`, 0o644)
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	bd, _ := os.ReadFile(filepath.Join(tools, "bitcoind"))
	wk, _ := os.ReadFile(filepath.Join(tools, "worker"))
	f.env = []string{
		"PATH=" + stubs + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + filepath.Join(root, "tmp"),
		"SNOWFALL_WORKER=" + filepath.Join(tools, "worker"), "SNOWFALL_WORKER_SHA256=" + sha(wk),
		"SNOWFALL_COMMIT=" + f.snowHead,
		"LEGACY_BASELINE_DIR=" + filepath.Join(root, "baseline"),
		"LEGACY_UPGRADED_DIR=" + filepath.Join(root, "upgraded"),
		"LEGACY_UPGRADED_COMMIT=" + upgraded,
		"LEGACY_TBTC_REPO=" + f.repo,
		"LEGACY_ANVIL=" + filepath.Join(tools, "anvil"),
		"LEGACY_BITCOIND=" + filepath.Join(tools, "bitcoind"), "LEGACY_BITCOIND_SHA256=" + sha(bd),
		"K09_ECDSA_NODE_MODULES=" + filepath.Join(root, "node_modules"),
		"K09_ECDSA_NODE_LOCK=" + filepath.Join(f.repo, "solidity/ecdsa/yarn.lock"),
	}
	return f
}

// probe runs the FIXTURE repository's run-probe.sh, bounded at 60 s.
func (f *probeFixture) probe(t *testing.T, patch, stage, record string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(f.repo, "test/k09/controls/run-probe.sh"),
		filepath.Join(f.repo, patch), stage, record)
	cmd.Dir = f.repo
	cmd.Env = f.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if ctx.Err() != nil {
		t.Fatalf("FIXTURE: run-probe.sh did not finish in 60 s:\n%s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), string(out)
	} else if err != nil {
		t.Fatalf("FIXTURE: run-probe.sh: %v", err)
	}
	return 0, string(out)
}

// citable reports whether record holds a record the checker accepts.
func citable(record, repo string) bool {
	if _, err := os.Stat(filepath.Join(record, "probe.json")); err != nil {
		return false
	}
	return len(checkProbeRecord(record, repo)) == 0
}

// TestB17cRunProbe (e2e): run-probe.sh writes records bound to the head, and
// gives no citable record when the probe cannot be bound or did not fail.
// Refs tuxxy/keep-core#60.
func TestB17cRunProbe(t *testing.T) {
	t.Run("record", func(t *testing.T) {
		t.Parallel()
		f := newProbeFixture(t)
		record := filepath.Join(f.root, "record")
		code, out := f.probe(t, "test/k09/controls/b17c-fail.patch", "08-deposit-refund", record)
		raw, err := os.ReadFile(filepath.Join(record, "probe.json"))
		if err != nil {
			t.Fatalf("K-09 #60 record: run-probe.sh wrote no probe.json (exit %d): %v\n%s", code, err, out)
		}
		for _, p := range checkProbeRecord(record, f.repo) {
			t.Errorf("K-09 #60 record: the checker refuses the record run-probe.sh wrote: %s", p)
		}
		var r map[string]any
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		head := run(t, f.repo, "git", "rev-parse", "HEAD")
		if r["head"] != head || r["clean"] != true {
			t.Errorf("K-09 #60 record: head %v clean %v, want %s true", r["head"], r["clean"], head)
		}
		patch, _ := os.ReadFile(filepath.Join(f.repo, "test/k09/controls/b17c-fail.patch"))
		if p, _ := r["patch"].(map[string]any); p == nil || p["sha256"] != sha(patch) {
			t.Errorf("K-09 #60 record: patch %v, want sha256 %s (the patch applied)", r["patch"], sha(patch))
		}
		want := map[string]string{}
		for _, n := range []string{"k09_export_test.go", "k09_lifecycle_test.go", "k09_world_test.go"} {
			content, _ := os.ReadFile(filepath.Join(f.repo, "pkg/tbtc", n))
			want[n] = sha(content)
		}
		got := map[string]string{}
		if files, ok := r["testFiles"].(map[string]any); ok {
			for k, v := range files {
				got[filepath.Base(k)], _ = v.(string)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("K-09 #60 record: testFiles %v, want the head's pkg/tbtc/k09_* files %v", got, want)
		}
		if r["stage"] != "08-deposit-refund" {
			t.Errorf("K-09 #60 record: stage %v, want 08-deposit-refund", r["stage"])
		}
		// The failing line is the one the patched tree gives: the stub
		// prints it only when the probe's marker is in the tree it ran.
		failure, _ := json.Marshal(r["failure"])
		if !strings.Contains(string(failure), fixtureFailLine("08-deposit-refund")) {
			t.Errorf("K-09 #60 record: failure %s, want the patched stage's line %q", failure, fixtureFailLine("08-deposit-refund"))
		}
		runs, _ := os.ReadFile(filepath.Join(f.state, "probe-runs"))
		if !strings.Contains(string(runs), "08-deposit-refund") {
			t.Errorf("K-09 #60 record: the probe never ran on a tree with the patch (stub runs: %q)", runs)
		}
		// Never on the checkout.
		if status := run(t, f.repo, "git", "status", "--porcelain"); status != "" {
			t.Errorf("K-09 #60 record: run-probe.sh changed the checkout: %s", status)
		}
	})
	t.Run("dirty-tree", func(t *testing.T) {
		t.Parallel()
		f := newProbeFixture(t)
		write(t, filepath.Join(f.repo, "pkg/tbtc/k09_world_test.go"), "package tbtc\n\n// FIXTURE uncommitted edit.\n", 0o644)
		record := filepath.Join(f.root, "record")
		f.probe(t, "test/k09/controls/b17c-fail.patch", "08-deposit-refund", record)
		if raw, err := os.ReadFile(filepath.Join(record, "probe.json")); err == nil {
			var r map[string]any
			if json.Unmarshal(raw, &r) == nil && r["clean"] != false {
				t.Errorf("K-09 #60 dirty-tree: the record of a probe run with uncommitted changes says clean %v", r["clean"])
			}
		}
		if citable(record, f.repo) {
			t.Errorf("K-09 #60 dirty-tree: a probe run with uncommitted changes left a citable record")
		}
	})
	t.Run("existing-record", func(t *testing.T) {
		t.Parallel()
		f := newProbeFixture(t)
		record := filepath.Join(f.root, "record")
		write(t, filepath.Join(record, "retained.txt"), "FIXTURE retained evidence\n", 0o644)
		code, _ := f.probe(t, "test/k09/controls/b17c-fail.patch", "08-deposit-refund", record)
		entries, _ := os.ReadDir(record)
		if code == 0 || len(entries) != 1 {
			t.Errorf("K-09 #60 existing-record: run-probe.sh wrote into an existing record directory (exit %d, entries %d): records are never overwritten", code, len(entries))
		}
	})
	for _, c := range []struct{ name, patch, stage string }{
		{"patch-does-not-apply", "test/k09/controls/b17c-broken.patch", "08-deposit-refund"},
		{"probe-did-not-fail", "test/k09/controls/b17c-pass.patch", "08-deposit-refund"},
		{"unknown-stage", "test/k09/controls/b17c-fail.patch", "13-phantom"},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newProbeFixture(t)
			record := filepath.Join(f.root, "record")
			code, _ := f.probe(t, c.patch, c.stage, record)
			if code == 0 {
				t.Errorf("K-09 #60 %s: run-probe.sh exited 0", c.name)
			}
			if citable(record, f.repo) {
				t.Errorf("K-09 #60 %s: run-probe.sh left a citable record", c.name)
			}
		})
	}
}
