package evidence

// Controls for the batch-9 evidence checks (rule 2: every guard fails at its
// own named assertion when the guard's subject is wrong). Each control
// writes a SYNTHETIC evidence directory (FIXTURE: no run produced it): first
// one a correct run would leave, which must pass all three checks, then one
// per wrong implementation, which must fail exactly the check it targets, at
// that check's named assertion, and pass the other two.
//
// Synthetic content: stage and node logs are short made-up lines; the drain
// source wallet is the public key of the test scalar 0xb9 (public only);
// the build records hash the keep-core sources at this checkout's HEAD.
// Refs tuxxy/keep-core#22, tuxxy/keep-core#40, tuxxy/keep-core#41.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
)

var fixtureStages = []string{"01-legacy", "02-upgrade", "03-dkg", "04-drain", "05-redemption", "06-restart",
	"07-reservation-timeout", "08-deposit-refund", "09-movement", "10-inactivity", "11-cutover-closure", "12-no-inactivity"}

// legacyOperatorAddress is the legacy world's operator FIXTURE (Anvil
// account 1, legacy_world_test.go legacyOperatorKey), which sends the
// harness's drain commitment today (drainFrom).
const legacyOperatorAddress = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"

// fixture is a synthetic evidence directory before it is written.
type fixture struct {
	manifest manifest
	files    map[string]string // path relative to the evidence directory -> content
}

// fixtureWallet is the synthetic drain source: the uncompressed public key
// as base logs it and its PKH. drainTx is a made-up txid.
func fixtureWallet(scalar byte) (logged, pkh string) {
	_, pub := btcec.PrivKeyFromBytes(append(make([]byte, 31), scalar))
	return "0x" + hex.EncodeToString(pub.SerializeUncompressed()), hex.EncodeToString(btcutil.Hash160(pub.SerializeCompressed()))
}

func drainTx(stage string) string { return sha256Hex([]byte("synthetic drain " + stage)) }

// broadcastLine is a base node's zap console line for a completed
// MovingFunds broadcast (pkg/tbtc/wallet.go, moving_funds.go fields).
func broadcastLine(wallet, action, tx string) string {
	return fmt.Sprintf("2026-11-06T10:00:00.000+0100\tINFO\tkeep-tbtc\ttbtc/wallet.go:402\tbroadcasting completed\t{\"wallet\": %q, \"action\": %q, \"step\": \"broadcastTransaction\", \"movingFundsTxHash\": %q}\n", wallet, action, tx)
}

func drainRecordLine(pkh, committer, tx string) string {
	return fmt.Sprintf("    k09_lifecycle_test.go:230: K09DRAIN source=0x%s committer=%s drain=%s\n", pkh, committer, tx)
}

// correctFixture is what a correct run leaves: the ECDSA build recorded from
// sources identical to HEAD, every node log hashed, and in each drain stage
// a record whose commitment sender is node operator 1 and whose drain a
// node's MovingFunds action broadcast for the source.
func correctFixture(t *testing.T, repo string) *fixture {
	t.Helper()
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{files: map[string]string{}}
	f.manifest.Candidate.KeepCoreHead = strings.TrimSpace(string(head))
	required, err := requiredArtifacts(repo, f.manifest.Candidate.KeepCoreHead)
	if err != nil {
		t.Fatal(err)
	}
	b := &ecdsaBuild{
		Artifacts: map[string]struct {
			Sha256    string `json:"sha256"`
			BuildInfo string `json:"buildInfo"`
		}{},
		BuildInfos: map[string]struct {
			Sha256  string            `json:"sha256"`
			Sources map[string]string `json:"sources"`
		}{},
	}
	lock, _ := headBlob(repo, f.manifest.Candidate.KeepCoreHead, "solidity/ecdsa/yarn.lock")
	b.YarnLockSha256 = sha256Hex(lock)
	for i, name := range required {
		source := name[:strings.LastIndex(name, ":")]
		id := fmt.Sprintf("%032x", i+1)
		sources := map[string]string{
			// A generated dependency-compiler shim is not compared with git.
			"contracts/hardhat-dependency-compiler/@keep-network/sortition-pools/contracts/SortitionPool.sol": sha256Hex([]byte("shim")),
		}
		if keepCoreSource(source) {
			content, ok := headBlob(repo, f.manifest.Candidate.KeepCoreHead, "solidity/ecdsa/"+source)
			if !ok {
				t.Fatalf("setup: HEAD has no solidity/ecdsa/%s", source)
			}
			sources[source] = sha256Hex(content)
		} else {
			sources[source] = sha256Hex([]byte("npm " + source))
		}
		b.BuildInfos[id] = struct {
			Sha256  string            `json:"sha256"`
			Sources map[string]string `json:"sources"`
		}{sha256Hex([]byte("build-info " + id)), sources}
		b.Artifacts[name] = struct {
			Sha256    string `json:"sha256"`
			BuildInfo string `json:"buildInfo"`
		}{sha256Hex([]byte("artifact " + name)), id}
	}
	f.manifest.Dependencies.EcdsaBuild = b
	wallet, pkh := fixtureWallet(0xb9)
	for _, stage := range fixtureStages {
		log := fmt.Sprintf("=== RUN   TestK09Lifecycle/%s\n    synthetic stage log\n", stage)
		if stage == "04-drain" || stage == "06-restart" {
			log += drainRecordLine(pkh, operatorAddress(1).Hex(), drainTx(stage))
		}
		f.files[stage+".log"] = log
		if stage == "01-legacy" || stage == "02-upgrade" {
			continue
		}
		for op := 1; op <= 3; op++ {
			node := fmt.Sprintf("2026-11-06T10:00:00.000+0100\tINFO\tkeep-tbtc\ttbtc/registry.go:95\tsynthetic node %d\n", op)
			if (stage == "04-drain" || stage == "06-restart") && op == 2 {
				node += broadcastLine(wallet, "MovingFunds", drainTx(stage))
			}
			f.files[fmt.Sprintf("nodes/%s/operator-%d.log", stage, op)] = node
		}
	}
	return f
}

// write writes the fixture, hashing the stage logs and node logs as a
// correct manifest does, and returns the directory.
func (f *fixture) write(t *testing.T, hashNodeLogs bool) string {
	t.Helper()
	dir := t.TempDir()
	if hashNodeLogs {
		f.manifest.NodeLogs = map[string]string{}
	}
	f.manifest.Stages = nil
	for path, content := range f.files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if hashNodeLogs && strings.HasPrefix(path, "nodes/") {
			f.manifest.NodeLogs[path] = sha256Hex([]byte(content))
		}
	}
	for _, stage := range fixtureStages {
		if content, ok := f.files[stage+".log"]; ok {
			f.manifest.Stages = append(f.manifest.Stages, struct {
				Stage     string `json:"stage"`
				Log       string `json:"log"`
				LogSha256 string `json:"logSha256"`
			}{stage, stage + ".log", sha256Hex([]byte(content))})
		}
	}
	f.rewrite(t, dir)
	return dir
}

func (f *fixture) rewrite(t *testing.T, dir string) {
	t.Helper()
	raw, err := json.MarshalIndent(f.manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

// checks runs the three checks on a written directory.
func checks(t *testing.T, dir, repo string) map[string][]string {
	t.Helper()
	m, err := loadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]string{
		"22": checkEcdsaBuild(m, repo),
		"41": checkNodeLogs(m, dir),
		"40": checkDrainSigners(m, dir),
	}
}

// control is one wrong implementation: what it changes in a correct run's
// evidence, the check that must catch it and that check's assertion text.
type control struct {
	name, check, assertion string
	// before changes the fixture before it is written; after changes the
	// written directory (for changes made after the manifest hashed it).
	before func(f *fixture)
	after  func(t *testing.T, f *fixture, dir string)
}

func editArtifact(f *fixture, name string, edit func(sha, id string) (string, string)) {
	a := f.manifest.Dependencies.EcdsaBuild.Artifacts[name]
	a.Sha256, a.BuildInfo = edit(a.Sha256, a.BuildInfo)
	f.manifest.Dependencies.EcdsaBuild.Artifacts[name] = a
}

const (
	registryArtifact  = "contracts/frost/FrostWalletRegistry.sol:FrostWalletRegistry"
	admissionArtifact = "contracts/frost/FrostAdmission.sol:FrostAdmission"
	tokenArtifact     = "contracts/test/FrostTestDependencies.sol:FrostTestToken"
)

var controls = []control{
	// #22: keep-core's solidity/ecdsa build.
	{name: "c22-presence-only", check: "22", assertion: "the manifest has no dependencies.ecdsaBuild",
		before: func(f *fixture) { f.manifest.Dependencies.EcdsaBuild = nil }},
	{name: "c22-stale-source", check: "22", assertion: "stale build: build-info",
		// A build from another worktree: the registry was compiled from a
		// source that differs from keepCoreHead's (as the build the b7-r1 run
		// used, build-info 8eca2f21, did for FrostWalletRegistry.sol).
		before: func(f *fixture) {
			id := f.manifest.Dependencies.EcdsaBuild.Artifacts[registryArtifact].BuildInfo
			f.manifest.Dependencies.EcdsaBuild.BuildInfos[id].Sources["contracts/frost/FrostWalletRegistry.sol"] = sha256Hex([]byte("older registry"))
		}},
	{name: "c22-source-not-at-head", check: "22", assertion: "which keepCoreHead",
		before: func(f *fixture) {
			id := f.manifest.Dependencies.EcdsaBuild.Artifacts[registryArtifact].BuildInfo
			f.manifest.Dependencies.EcdsaBuild.BuildInfos[id].Sources["contracts/frost/FrostRemovedSince.sol"] = sha256Hex([]byte("gone"))
		}},
	{name: "c22-partial-artifacts", check: "22", assertion: "but dependencies.ecdsaBuild.artifacts has no record of it",
		before: func(f *fixture) { delete(f.manifest.Dependencies.EcdsaBuild.Artifacts, registryArtifact) }},
	{name: "c22-unrecorded-build-info", check: "22", assertion: "which dependencies.ecdsaBuild.buildInfos does not record",
		before: func(f *fixture) {
			editArtifact(f, registryArtifact, func(sha, _ string) (string, string) { return sha, "ffffffffffffffffffffffffffffffff" })
		}},
	{name: "c22-foreign-build-info", check: "22", assertion: "which did not compile its source",
		// The admission artifact is credited to a build-info that never
		// compiled FrostAdmission.sol (hashes taken from the wrong place).
		before: func(f *fixture) {
			other := f.manifest.Dependencies.EcdsaBuild.Artifacts[tokenArtifact].BuildInfo
			editArtifact(f, admissionArtifact, func(sha, _ string) (string, string) { return sha, other })
		}},
	{name: "c22-unhashed-artifact", check: "22", assertion: "has no SHA-256",
		before: func(f *fixture) {
			editArtifact(f, registryArtifact, func(_, id string) (string, string) { return "", id })
		}},
	{name: "c22-other-yarn-lock", check: "22", assertion: "yarnLockSha256",
		before: func(f *fixture) { f.manifest.Dependencies.EcdsaBuild.YarnLockSha256 = sha256Hex([]byte("other lock")) }},

	// #41: node-log hashes.
	{name: "c41-stage-logs-only", check: "41", assertion: "the manifest has no nodeLogs",
		after: func(t *testing.T, f *fixture, dir string) { f.manifest.NodeLogs = nil; f.rewrite(t, dir) }},
	{name: "c41-one-log-unhashed", check: "41", assertion: "is not hashed in manifest.nodeLogs",
		after: func(t *testing.T, f *fixture, dir string) {
			delete(f.manifest.NodeLogs, "nodes/10-inactivity/operator-3.log")
			f.rewrite(t, dir)
		}},
	{name: "c41-hashed-before-copy", check: "41", assertion: "but manifest.nodeLogs records",
		// Hashed, then the log changed (hashing the scratch copy before the
		// node finished writing).
		after: func(t *testing.T, f *fixture, dir string) {
			appendFile(t, filepath.Join(dir, "nodes", "05-redemption", "operator-1.log"), "late line\n")
		}},
	{name: "c41-phantom-entry", check: "41", assertion: "which is not a node log in the evidence",
		after: func(t *testing.T, f *fixture, dir string) {
			f.manifest.NodeLogs["nodes/05-redemption/operator-4.log"] = sha256Hex(nil)
			f.rewrite(t, dir)
		}},
	{name: "c41-logs-not-collected", check: "41", assertion: "started nodes but the evidence has no node log",
		before: func(f *fixture) {
			for op := 1; op <= 3; op++ {
				delete(f.files, fmt.Sprintf("nodes/09-movement/operator-%d.log", op))
			}
		}},

	// #40: who committed and signed the ECDSA-to-FROST drain.
	{name: "c40-fixture-drain", check: "40", assertion: "the stage log shows the harness's FIXTURE drain",
		before: func(f *fixture) {
			f.files["04-drain.log"] += "    frost_node_b6_world_test.go:192: b6 phase drain proven                   3.27 s\n"
		}},
	{name: "c40-no-record", check: "40", assertion: "records 0 drains",
		before: func(f *fixture) { f.files["06-restart.log"] = "=== RUN   TestK09Lifecycle/06-restart\n" }},
	{name: "c40-two-records", check: "40", assertion: "records 2 drains",
		before: func(f *fixture) {
			_, pkh := fixtureWallet(0xb9)
			f.files["04-drain.log"] += drainRecordLine(pkh, operatorAddress(1).Hex(), drainTx("04-drain"))
		}},
	{name: "c40-harness-committer", check: "40", assertion: "which is not one of the stage's node operators",
		// The node signs, but the harness's legacy operator FIXTURE commits.
		before: func(f *fixture) {
			_, pkh := fixtureWallet(0xb9)
			f.files["04-drain.log"] = "=== RUN   TestK09Lifecycle/04-drain\n" + drainRecordLine(pkh, legacyOperatorAddress, drainTx("04-drain"))
		}},
	{name: "c40-harness-signed", check: "40", assertion: "records a completed MovingFunds broadcast of the drain",
		// The record is written, but no node's MovingFunds action broadcast
		// the drain: the harness signed it with the source's FIXTURE key.
		before: func(f *fixture) {
			f.files["nodes/06-restart/operator-2.log"] = "2026-11-06T10:00:00.000+0100\tINFO\tkeep-tbtc\ttbtc/registry.go:95\tsynthetic node 2\n"
		}},
	{name: "c40-other-action", check: "40", assertion: "records a completed MovingFunds broadcast of the drain",
		before: func(f *fixture) {
			wallet, _ := fixtureWallet(0xb9)
			f.files["nodes/04-drain/operator-2.log"] = broadcastLine(wallet, "MovedFundsSweep", drainTx("04-drain"))
		}},
	{name: "c40-other-wallet", check: "40", assertion: "not for the legacy source",
		// A node broadcast that transaction, but for another wallet.
		before: func(f *fixture) {
			other, _ := fixtureWallet(0xba)
			f.files["nodes/04-drain/operator-2.log"] = broadcastLine(other, "MovingFunds", drainTx("04-drain"))
		}},
	{name: "c40-other-transaction", check: "40", assertion: "records a completed MovingFunds broadcast of the drain",
		// The nodes moved the source's funds, but the record names a
		// different transaction (the drain the Bridge accepted was not theirs).
		before: func(f *fixture) {
			_, pkh := fixtureWallet(0xb9)
			f.files["06-restart.log"] = "=== RUN   TestK09Lifecycle/06-restart\n" + drainRecordLine(pkh, operatorAddress(3).Hex(), drainTx("harness"))
		}},
	{name: "c40-stage-log-rewritten", check: "40", assertion: "but the manifest records",
		after: func(t *testing.T, f *fixture, dir string) {
			appendFile(t, filepath.Join(dir, "04-drain.log"), "rewritten after the manifest\n")
		}},
}

// TestK09EvidenceControls runs the correct fixture and every wrong one.
func TestK09EvidenceControls(t *testing.T) {
	repo, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("correct", func(t *testing.T) {
		t.Parallel()
		f := correctFixture(t, repo)
		for check, problems := range checks(t, f.write(t, true), repo) {
			for _, p := range problems {
				t.Errorf("control correct: check %s refuses a correct run's evidence: %s", check, p)
			}
		}
	})
	for _, c := range controls {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := correctFixture(t, repo)
			if c.before != nil {
				c.before(f)
			}
			dir := f.write(t, true)
			if c.after != nil {
				c.after(t, f, dir)
			}
			for check, problems := range checks(t, dir, repo) {
				if check != c.check {
					for _, p := range problems {
						t.Errorf("control %s: check %s also fails (it must catch only its own target): %s", c.name, check, p)
					}
					continue
				}
				caught := false
				for _, p := range problems {
					if strings.Contains(p, c.assertion) {
						caught = true
					}
				}
				if !caught {
					t.Errorf("control %s: check %s does not fail at %q; problems: %v", c.name, check, c.assertion, problems)
				}
			}
		})
	}
}
