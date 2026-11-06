package evidence

// Real-build control for tuxxy/keep-core#22: records a real Hardhat build of
// keep-core's solidity/ecdsa the way a K-09 manifest must (README.md,
// "dependencies.ecdsaBuild"), then runs the #22 check on that record against
// a head. It reads public build outputs only (artifacts, build-info sources).
//
//	K09_ECDSA_BUILD=<solidity/ecdsa/build dir> K09_HEAD=<commit> \
//	  go test -run '^TestK09EcdsaBuildOf$' ./test/k09/evidence
//
// Not part of the acceptance run (TestK09Evidence*); it fails without its
// variables, as TestK09Evidence does.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordEcdsaBuild records the named artifacts of a Hardhat build directory:
// each artifact's SHA-256 and build-info ID (from its .dbg.json), and for
// each build-info its SHA-256 and the SHA-256 of every compiled source.
func recordEcdsaBuild(build string, names []string, yarnLock []byte) (*ecdsaBuild, error) {
	b := &ecdsaBuild{
		YarnLockSha256: sha256Hex(yarnLock),
		Artifacts: map[string]struct {
			Sha256    string `json:"sha256"`
			BuildInfo string `json:"buildInfo"`
		}{},
		BuildInfos: map[string]struct {
			Sha256  string            `json:"sha256"`
			Sources map[string]string `json:"sources"`
		}{},
	}
	for _, name := range names {
		i := strings.LastIndex(name, ":")
		dir := filepath.Join(build, filepath.FromSlash(name[:i]))
		artifact, err := os.ReadFile(filepath.Join(dir, name[i+1:]+".json"))
		if err != nil {
			return nil, err
		}
		dbgRaw, err := os.ReadFile(filepath.Join(dir, name[i+1:]+".dbg.json"))
		if err != nil {
			return nil, err
		}
		var dbg struct {
			BuildInfo string `json:"buildInfo"`
		}
		if err := json.Unmarshal(dbgRaw, &dbg); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		id := strings.TrimSuffix(filepath.Base(dbg.BuildInfo), ".json")
		b.Artifacts[name] = struct {
			Sha256    string `json:"sha256"`
			BuildInfo string `json:"buildInfo"`
		}{sha256Hex(artifact), id}
		if _, ok := b.BuildInfos[id]; ok {
			continue
		}
		infoRaw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(dbg.BuildInfo)))
		if err != nil {
			return nil, err
		}
		var info struct {
			Input struct {
				Sources map[string]struct {
					Content string `json:"content"`
				} `json:"sources"`
			} `json:"input"`
		}
		if err := json.Unmarshal(infoRaw, &info); err != nil {
			return nil, fmt.Errorf("build-info %s: %v", id, err)
		}
		sources := map[string]string{}
		for path, s := range info.Input.Sources {
			sources[path] = sha256Hex([]byte(s.Content))
		}
		b.BuildInfos[id] = struct {
			Sha256  string            `json:"sha256"`
			Sources map[string]string `json:"sources"`
		}{sha256Hex(infoRaw), sources}
	}
	return b, nil
}

// TestK09EcdsaBuildOf records the build at K09_ECDSA_BUILD (with the yarn.lock
// beside it) and checks it against K09_HEAD.
func TestK09EcdsaBuildOf(t *testing.T) {
	build, head := os.Getenv("K09_ECDSA_BUILD"), os.Getenv("K09_HEAD")
	if build == "" || head == "" {
		t.Fatal("K09_ECDSA_BUILD (a solidity/ecdsa build directory) and K09_HEAD are required")
	}
	repo, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	names, err := requiredArtifacts(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(build, "..", "yarn.lock"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := recordEcdsaBuild(build, names, lock)
	if err != nil {
		t.Fatal(err)
	}
	m := &manifest{}
	m.Candidate.KeepCoreHead = head
	m.Dependencies.EcdsaBuild = b
	for name, a := range b.Artifacts {
		t.Logf("recorded %s -> build-info %s", name, a.BuildInfo)
	}
	for _, p := range checkEcdsaBuild(m, repo) {
		t.Error(p)
	}
}
