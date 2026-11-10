package evidence

// Remediation batch 17c, note N7 of the batch 17a fix review: the evidence
// check re-checks, on retained evidence, the manifest records that
// test/k09/run.sh (d939d106) writes for #30, #35, #36 and tbtc-v2#19.
// Coordinator decision 1: a new file and TestK09EvidenceRunRecords; the
// frozen evidence_test.go is unchanged. Every error names its field on its
// first line.
//
// Refs tuxxy/keep-core#30, #35, #36, tuxxy/tbtc-v2#19.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// runEnvironmentAllowlist is the set of names test/k09/run.sh may pass to the
// build and the stages (tbtc-v2#19): PATH and HOME, TMPDIR, its required
// inputs, the Go caches, and the Go settings it fixes. Names, not prefixes:
// any other variable (GODEBUG, a loader variable, a caller variable) means
// the run saw an environment run.sh does not allow.
var runEnvironmentAllowlist = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true,
	"SNOWFALL_WORKER": true, "SNOWFALL_WORKER_SHA256": true, "SNOWFALL_COMMIT": true,
	"LEGACY_BASELINE_DIR": true, "LEGACY_TBTC_REPO": true, "LEGACY_ANVIL": true,
	"LEGACY_BITCOIND": true, "LEGACY_BITCOIND_SHA256": true,
	"LEGACY_UPGRADED_DIR": true, "LEGACY_UPGRADED_COMMIT": true,
	"K09_ECDSA_NODE_MODULES": true, "K09_ECDSA_NODE_LOCK": true,
	"GOCACHE": true, "GOMODCACHE": true, "GOPATH": true,
	"GOFLAGS": true, "GOTOOLCHAIN": true, "GOWORK": true,
}

// checkRunRecords returns one problem per run.sh record of the manifest in
// evidence that is absent or untrue of the evidence directory.
func checkRunRecords(evidence string) []string {
	raw, err := os.ReadFile(filepath.Join(evidence, "manifest.json"))
	if err != nil {
		return []string{"manifest.json: " + err.Error()}
	}
	var m struct {
		Files       map[string]string            `json:"files"`
		Environment map[string]json.RawMessage   `json:"environment"`
		Repos       map[string]json.RawMessage   `json:"repos"`
		Stages      []map[string]json.RawMessage `json:"stages"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return []string{"manifest.json does not parse as run.sh's records (files, environment, repos, stages): " + err.Error()}
	}
	var problems []string
	problem := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	// #36: manifest.files hashes exactly the files of the evidence directory
	// other than manifest.json, at every depth.
	if m.Files == nil {
		problem("files (#36): the manifest has no files inventory")
	} else {
		onDisk := map[string]string{}
		walkErr := filepath.WalkDir(evidence, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(evidence, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == "manifest.json" {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			onDisk[rel] = sha256Hex(content)
			return nil
		})
		if walkErr != nil {
			problem("files (#36): cannot read the evidence directory: %v", walkErr)
		}
		for _, p := range sortedKeys(onDisk) {
			want, ok := m.Files[p]
			switch {
			case !ok:
				problem("files (#36): %s is in the evidence but manifest.files does not hash it", p)
			case want != onDisk[p]:
				problem("files (#36): %s has SHA-256 %s, but manifest.files records %s", p, onDisk[p], want)
			}
		}
		for _, p := range sortedKeys(m.Files) {
			if _, ok := onDisk[p]; !ok {
				problem("files (#36): manifest.files lists %s, which the evidence does not have", p)
			}
		}
	}

	// tbtc-v2#19: manifest.environment names only run.sh's allowlist.
	if m.Environment == nil {
		problem("environment (tbtc-v2#19): the manifest has no environment record")
	}
	for _, name := range sortedKeys(m.Environment) {
		if !runEnvironmentAllowlist[name] {
			problem("environment (tbtc-v2#19): manifest.environment names %s, which run.sh's allowlist does not", name)
		}
	}

	// #35: repos.snowfall is the SNOWFALL_COMMIT the run was given.
	snowfallCommit := jsonString(m.Environment["SNOWFALL_COMMIT"])
	snowfall := jsonString(m.Repos["snowfall"])
	switch {
	case snowfall == "":
		problem("repos.snowfall (#35): the manifest records no snowfall commit")
	case snowfallCommit == "":
		problem("repos.snowfall (#35): environment.SNOWFALL_COMMIT is absent, so repos.snowfall %s cannot be checked", snowfall)
	case snowfall != snowfallCommit:
		problem("repos.snowfall (#35): %s is not environment.SNOWFALL_COMMIT %s", snowfall, snowfallCommit)
	}

	// #30: every stage records what the runner killed after it.
	for i, s := range m.Stages {
		if _, ok := s["leftoverKilled"]; !ok {
			problem("stages[%d].leftoverKilled (#30): stage %s has no leftoverKilled record", i, jsonString(s["stage"]))
		}
	}
	return problems
}

func jsonString(raw json.RawMessage) string {
	var s string
	if raw == nil || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestK09EvidenceRunRecords checks K09_EVIDENCE's run.sh records (review N7).
// A missing variable is a failure, not a skip, as in TestK09Evidence.
func TestK09EvidenceRunRecords(t *testing.T) {
	evidence := os.Getenv("K09_EVIDENCE")
	if evidence == "" {
		t.Fatal("K09_EVIDENCE (a K-09 run's evidence directory) is required")
	}
	for _, p := range checkRunRecords(evidence) {
		t.Error(p)
	}
}
