package evidence

// Remediation batch 17c, note N7 of the batch 17a fix review
// (K-09-restart/REVIEW-FIX-B17A.md): the evidence checker TestK09Evidence
// must re-check, on retained evidence, the manifest fields that the re-frozen
// test/k09/run.sh (d939d106) writes for #30, #35, #36 and tbtc-v2#19.
//
// Invariant: TestK09Evidence (run as test/k09/evidence/README.md documents:
// `-run TestK09Evidence` with K09_EVIDENCE) refuses an evidence directory
// whose manifest
//   - files (#36): is absent, or does not hash exactly the files in the
//     evidence directory other than manifest.json, each with its SHA-256;
//   - environment (tbtc-v2#19): is absent, or names a variable outside
//     run.sh's allowlist (PATH, HOME, TMPDIR, run.sh's inputs, GOCACHE,
//     GOMODCACHE, GOPATH, GOFLAGS, GOTOOLCHAIN, GOWORK);
//   - repos.snowfall (#35): is absent, or is not environment.SNOWFALL_COMMIT;
//   - stages[].leftoverKilled (#30): is absent from any stage;
// and accepts the evidence a correct run leaves (positive case). An error
// line of the check must name the field (or the file or variable at fault)
// on its first line, the line go test prefixes with "<file>_test.go:<n>: ".
//
// Where the fix goes (coordinator decision 1, 2026-11-10): a new file,
// test/k09/evidence/runrecords_test.go, as TestK09EvidenceRunRecords; the
// frozen evidence_test.go stays unchanged. Any TestK09Evidence<...> function
// but TestK09EvidenceControls is run, so that shape passes.
//
// Kind: e2e of the checker (each case re-runs this package's test binary as a
// subprocess on a SYNTHETIC evidence directory: FIXTURE, no K-09 run made it;
// no chain, node or stage). The subprocess runs every TestK09Evidence* test
// but TestK09EvidenceControls, so the check may live in TestK09Evidence or in
// a new TestK09Evidence<...> function; the test names no checker function.
// Each case takes well under a second.
//
// Base failure: the checker does not read these fields, so every wrong case
// is accepted (exit 0) and fails at "the checker accepted".
//
// Refs tuxxy/keep-core#30, tuxxy/keep-core#35, tuxxy/keep-core#36,
// tuxxy/tbtc-v2#19 (review N7).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// b17cSnowfall is the FIXTURE snowfall commit the synthetic run records.
const b17cSnowfall = "c1847ebdf133d0570643ff968539dd498069f69f"

// b17cEnvironment is the environment a correct run.sh records: exactly its
// allowlist (FIXTURE values; no secret).
func b17cEnvironment() map[string]any {
	return map[string]any{
		"PATH": "/usr/bin:/bin", "HOME": "/Users/fixture", "TMPDIR": "/private/tmp/fixture",
		"SNOWFALL_WORKER": "/fixture/snowfall", "SNOWFALL_WORKER_SHA256": sha256Hex([]byte("worker")),
		"SNOWFALL_COMMIT":     b17cSnowfall,
		"LEGACY_BASELINE_DIR": "/fixture/baseline", "LEGACY_TBTC_REPO": "/fixture/tbtc-v2",
		"LEGACY_ANVIL": "/fixture/anvil", "LEGACY_BITCOIND": "/fixture/bitcoind",
		"LEGACY_BITCOIND_SHA256": sha256Hex([]byte("bitcoind")),
		"LEGACY_UPGRADED_DIR":    "/fixture/upgraded", "LEGACY_UPGRADED_COMMIT": "8a9e9e5aea836c3e6369cd9878df67f7650be6de",
		"K09_ECDSA_NODE_MODULES": "/fixture/node_modules", "K09_ECDSA_NODE_LOCK": "/fixture/yarn.lock",
		"GOCACHE": "/fixture/go-build", "GOMODCACHE": "/fixture/go/pkg/mod", "GOPATH": "/fixture/go",
		"GOFLAGS": "", "GOTOOLCHAIN": "local", "GOWORK": "off",
	}
}

// b17cEvidence writes the synthetic evidence a correct run leaves: the batch
// 9 correct fixture (it passes #22, #40 and #41), plus cases.tsv and
// secret-scan-hits.txt as run.sh writes them, and the run.sh manifest fields
// repos, environment, stages[].leftoverKilled and files. edit changes the
// manifest (as JSON) and the directory after the inventory (files) is taken,
// as a wrong run, or a later edit of the evidence, would.
func b17cEvidence(t *testing.T, repo string, edit func(m map[string]any, dir string)) string {
	t.Helper()
	f := correctFixture(t, repo)
	var tsv strings.Builder
	for _, s := range fixtureStages {
		tsv.WriteString(s + "\t" + s + ".log\t0\n")
	}
	f.files["cases.tsv"] = tsv.String()
	f.files["secret-scan-hits.txt"] = ""
	dir := f.write(t, true)
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	head := f.manifest.Candidate.KeepCoreHead
	m["repos"] = map[string]any{"keepCore": head, "tbtcV2Upgraded": "8a9e9e5aea836c3e6369cd9878df67f7650be6de",
		"tbtcV2Baseline": "40a11d1dcdcf82d3962e430067cfe3f97be81483", "snowfall": b17cSnowfall}
	m["environment"] = b17cEnvironment()
	for _, s := range m["stages"].([]any) {
		stage := s.(map[string]any)
		stage["leftoverKilled"] = []any{}
		if stage["stage"] == "04-drain" {
			// FIXTURE: run.sh records "<pid> <comm>" per process it killed.
			stage["leftoverKilled"] = []any{"41234 anvil", "41240 bitcoind"}
		}
	}
	{
		files := map[string]any{}
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			rel = filepath.ToSlash(rel)
			if rel == "manifest.json" {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[rel] = sha256Hex(content)
			return nil
		})
		m["files"] = files
	}
	if edit != nil {
		edit(m, dir)
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// checkerVerdict runs the documented evidence check (every TestK09Evidence*
// test but the synthetic controls) on dir in a subprocess of this test
// binary, bounded at 60 s, and returns its exit status and its error lines.
func checkerVerdict(t *testing.T, dir string) (int, []string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run", "^TestK09Evidence", "-test.skip", "^TestK09EvidenceControls$",
		"-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "K09_EVIDENCE="+dir)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("FIXTURE: the evidence check did not finish in 60 s:\n%s", out)
	}
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("FIXTURE: running the evidence check: %v", err)
	}
	if !strings.Contains(string(out), "=== RUN   TestK09Evidence") {
		t.Fatalf("FIXTURE: the subprocess ran no TestK09Evidence test:\n%s", out)
	}
	var problems []string
	for _, line := range strings.Split(string(out), "\n") {
		if regexp.MustCompile(`_test\.go:\d+: `).MatchString(line) {
			problems = append(problems, strings.TrimSpace(line))
		}
	}
	return code, problems, string(out)
}

type b17cCase struct {
	name string
	// words: one must appear in a checker error line, so the refusal is
	// about this field, not another check.
	words []string
	edit  func(m map[string]any, dir string)
}

func stageEntry(m map[string]any, stage string) map[string]any {
	for _, s := range m["stages"].([]any) {
		if s.(map[string]any)["stage"] == stage {
			return s.(map[string]any)
		}
	}
	panic("FIXTURE: no stage " + stage)
}

var b17cCases = []b17cCase{
	// #36: manifest.files.
	{name: "files-absent", words: []string{"files"},
		edit: func(m map[string]any, _ string) { delete(m, "files") }},
	{name: "files-hash-wrong", words: []string{"cases.tsv"},
		// cases.tsv changed after the inventory hashed it (no other check
		// reads cases.tsv).
		edit: func(m map[string]any, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "cases.tsv"), []byte("01-legacy\t01-legacy.log\t0\n"), 0o644); err != nil {
				panic(err)
			}
		}},
	{name: "files-unlisted", words: []string{"stray.txt"},
		// A file in the evidence that the inventory does not hash.
		edit: func(m map[string]any, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("FIXTURE stray\n"), 0o644); err != nil {
				panic(err)
			}
		}},
	{name: "files-phantom", words: []string{"ghost.log"},
		// An inventory entry for a file the evidence does not have.
		edit: func(m map[string]any, _ string) {
			m["files"].(map[string]any)["ghost.log"] = sha256Hex([]byte("ghost"))
		}},
	// A file the inventory does not hash outside the top level and outside
	// nodes/, which #41 already covers (review N-n7a).
	{name: "files-unlisted-subdir", words: []string{"extra/stray.txt"},
		edit: func(m map[string]any, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "extra"), 0o755); err != nil {
				panic(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "extra", "stray.txt"), []byte("FIXTURE stray\n"), 0o644); err != nil {
				panic(err)
			}
		}},
	// tbtc-v2#19: manifest.environment.
	{name: "environment-absent", words: []string{"environment"},
		edit: func(m map[string]any, _ string) { delete(m, "environment") }},
	{name: "environment-extra-variable", words: []string{"B17C_CALLER_ONLY"},
		// A caller variable run.sh does not name reached the build and stages.
		edit: func(m map[string]any, _ string) { m["environment"].(map[string]any)["B17C_CALLER_ONLY"] = "1" }},
	{name: "environment-extra-loader-variable", words: []string{"DYLD_INSERT_LIBRARIES"},
		edit: func(m map[string]any, _ string) {
			m["environment"].(map[string]any)["DYLD_INSERT_LIBRARIES"] = "/fixture/inject.dylib"
		}},
	// A Go setting run.sh does not pass: the allowlist is by name, not by
	// prefix (review N-n7a).
	{name: "environment-extra-go-variable", words: []string{"GODEBUG"},
		edit: func(m map[string]any, _ string) { m["environment"].(map[string]any)["GODEBUG"] = "asyncpreemptoff=1" }},
	// #35: manifest.repos.snowfall.
	{name: "repos-absent", words: []string{"snowfall"},
		edit: func(m map[string]any, _ string) { delete(m, "repos") }},
	{name: "repos-snowfall-absent", words: []string{"snowfall"},
		edit: func(m map[string]any, _ string) { delete(m["repos"].(map[string]any), "snowfall") }},
	{name: "repos-snowfall-other-commit", words: []string{"snowfall", "SNOWFALL_COMMIT"},
		edit: func(m map[string]any, _ string) {
			m["repos"].(map[string]any)["snowfall"] = "2222222222222222222222222222222222222222"
		}},
	{name: "environment-no-snowfall-commit", words: []string{"snowfall", "SNOWFALL_COMMIT"},
		// Nothing to compare repos.snowfall with.
		edit: func(m map[string]any, _ string) { delete(m["environment"].(map[string]any), "SNOWFALL_COMMIT") }},
	// #30: stages[].leftoverKilled.
	{name: "leftover-absent-one-stage", words: []string{"leftoverKilled"},
		edit: func(m map[string]any, _ string) { delete(stageEntry(m, "07-reservation-timeout"), "leftoverKilled") }},
	{name: "leftover-absent-every-stage", words: []string{"leftoverKilled"},
		edit: func(m map[string]any, _ string) {
			for _, s := range m["stages"].([]any) {
				delete(s.(map[string]any), "leftoverKilled")
			}
		}},
}

// TestB17cEvidenceManifestRecords: see the file comment.
// Refs tuxxy/keep-core#30, #35, #36, tuxxy/tbtc-v2#19 (review N7).
func TestB17cEvidenceManifestRecords(t *testing.T) {
	if os.Getenv("K09_EVIDENCE") != "" {
		t.Skip("subprocess of TestB17cEvidenceManifestRecords")
	}
	repo, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("correct", func(t *testing.T) {
		t.Parallel()
		code, problems, out := checkerVerdict(t, b17cEvidence(t, repo, nil))
		if code != 0 {
			t.Errorf("K-09 N7 correct: the evidence check refuses the evidence a correct run.sh leaves (exit %d): %v\n%s", code, problems, out)
		}
	})
	for _, c := range b17cCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			code, problems, _ := checkerVerdict(t, b17cEvidence(t, repo, c.edit))
			if code == 0 {
				t.Fatalf("K-09 N7 %s: the checker accepted evidence whose manifest is wrong (%s): TestK09Evidence does not check it", c.name, strings.Join(c.words, " / "))
			}
			named := false
			for _, p := range problems {
				for _, w := range c.words {
					if strings.Contains(p, w) {
						named = true
					}
				}
			}
			if !named {
				sort.Strings(problems)
				t.Errorf("K-09 N7 %s: the checker refuses, but no error names %v (it must refuse for this field, not another check): %v", c.name, c.words, problems)
			}
		})
	}
}
