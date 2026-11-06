package evidence

// K-09 evidence checks (remediation batch 9): the three evidence gaps left
// open on the K-09 lifecycle run (test/k09/run.sh, pkg/tbtc/k09_*_test.go)
// after run b7-r1. Each check reads a finished run's evidence directory; none
// re-runs a stage, starts a chain or reads a node store.
//
//   - tuxxy/keep-core#22 (K09-1): the keep-core solidity/ecdsa build the world
//     deploys (the FROST registry stack) is hashed into the manifest and tied
//     to the head under test.
//   - tuxxy/keep-core#41 (K09-24): every node log the run collects
//     (nodes/<stage>/operator-N.log) is hashed in the manifest.
//   - tuxxy/keep-core#40 (K09-23): in the ECDSA-to-FROST drain stages
//     (04-drain, 06-restart), the drain is committed by a node operator and
//     signed and broadcast by a base node's MovingFunds action (the legacy
//     ECDSA signing group), not by the harness with the source's FIXTURE key.
//     The composition ledger named in #40 is KC-VER V-05 verification work
//     and is not checked here.
//
// Usage: K09_EVIDENCE=<evidence dir> go test ./test/k09/evidence
// The manifest fields these checks read are specified in README.md here.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// manifest is the part of a K-09 run's manifest.json these checks read.
type manifest struct {
	Candidate struct {
		KeepCoreHead string `json:"keepCoreHead"`
	} `json:"candidate"`
	Dependencies struct {
		EcdsaBuild *ecdsaBuild `json:"ecdsaBuild"`
	} `json:"dependencies"`
	Stages []struct {
		Stage     string `json:"stage"`
		Log       string `json:"log"`
		LogSha256 string `json:"logSha256"`
	} `json:"stages"`
	// NodeLogs maps each collected node log's path, relative to the evidence
	// directory ("nodes/<stage>/operator-N.log"), to its SHA-256 (#41).
	NodeLogs map[string]string `json:"nodeLogs"`
}

// ecdsaBuild is the record of keep-core's solidity/ecdsa build (#22).
type ecdsaBuild struct {
	// BuiltFrom is the commit the build was compiled from; it must be
	// candidate.keepCoreHead (review N3).
	BuiltFrom string `json:"builtFrom"`
	// YarnLockSha256 is the SHA-256 of the solidity/ecdsa/yarn.lock the
	// build's node_modules were installed from.
	YarnLockSha256 string `json:"yarnLockSha256"`
	// Artifacts maps "<source>:<contract>" (Hardhat's fully qualified name)
	// to the artifact JSON's SHA-256 and the ID of its build-info.
	Artifacts map[string]struct {
		Sha256    string `json:"sha256"`
		BuildInfo string `json:"buildInfo"`
	} `json:"artifacts"`
	// BuildInfos maps a build-info ID to the build-info file's SHA-256 and,
	// for every source in its compiler input, the SHA-256 of that source's
	// content as compiled.
	BuildInfos map[string]struct {
		Sha256  string            `json:"sha256"`
		Sources map[string]string `json:"sources"`
	} `json:"buildInfos"`
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func loadManifest(evidence string) (*manifest, error) {
	raw, err := os.ReadFile(filepath.Join(evidence, "manifest.json"))
	if err != nil {
		return nil, err
	}
	m := &manifest{}
	if err := json.Unmarshal(raw, m); err != nil {
		return nil, fmt.Errorf("manifest.json: %v", err)
	}
	return m, nil
}

// repoRoot is the keep-core checkout this test runs in.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// headBlob returns a file's content at a commit, or ok false when the commit
// has no such file.
func headBlob(repo, commit, path string) ([]byte, bool) {
	out, err := exec.Command("git", "-C", repo, "cat-file", "blob", commit+":"+path).Output()
	if err != nil {
		return nil, false
	}
	return out, true
}

// deployCall is a literal b6Deploy("<source>", "<contract>" call: every
// contract the K-09 world deploys from keep-core's solidity/ecdsa build.
var deployCall = regexp.MustCompile(`b6Deploy\(\s*"([^"]+)",\s*"([^"]+)"`)

// requiredArtifacts are the keep-core artifacts the K-09 world at the head
// deploys, from the b6Deploy calls in that head's pkg/tbtc/k09_*.go files.
func requiredArtifacts(repo, head string) ([]string, error) {
	out, err := exec.Command("git", "-C", repo, "ls-tree", "--name-only", head, "pkg/tbtc/").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s pkg/tbtc: %v", head, err)
	}
	seen := map[string]bool{}
	for _, path := range strings.Fields(string(out)) {
		if !strings.HasPrefix(filepath.Base(path), "k09_") || !strings.HasSuffix(path, ".go") {
			continue
		}
		content, ok := headBlob(repo, head, path)
		if !ok {
			return nil, fmt.Errorf("%s:%s unreadable", head, path)
		}
		for _, m := range deployCall.FindAllStringSubmatch(string(content), -1) {
			seen[m[1]+":"+m[2]] = true
		}
	}
	var names []string
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no b6Deploy call in %s:pkg/tbtc/k09_*.go", head)
	}
	return names, nil
}

// keepCoreSource reports whether a compiler-input source path is a keep-core
// source tracked in git: under contracts/, except Hardhat's generated
// dependency-compiler shims (gitignored; they import npm packages and are
// tied to the head through yarn.lock like the packages themselves).
func keepCoreSource(path string) bool {
	return strings.HasPrefix(path, "contracts/") && !strings.HasPrefix(path, "contracts/hardhat-dependency-compiler/")
}

// checkEcdsaBuild (#22): the manifest records the solidity/ecdsa build, and
// that build was compiled from the head under test. Invariant: every
// artifact the world deploys is hashed and names a recorded build-info;
// every keep-core source in each recorded build-info is byte-identical to
// the same file at candidate.keepCoreHead; that build-info compiled the
// artifact's own source; and the npm dependencies were installed from the
// head's yarn.lock.
func checkEcdsaBuild(m *manifest, repo string) []string {
	const tag = "K-09 #22 (K09-1)"
	head := m.Candidate.KeepCoreHead
	if head == "" {
		return []string{tag + ": the manifest names no candidate.keepCoreHead"}
	}
	b := m.Dependencies.EcdsaBuild
	if b == nil {
		return []string{tag + ": the manifest has no dependencies.ecdsaBuild: keep-core's solidity/ecdsa build (the FROST registry stack the world deploys) is not hashed or tied to keepCoreHead " + head}
	}
	var problems []string
	if b.BuiltFrom != head {
		problems = append(problems, fmt.Sprintf("%s: the build's builtFrom %q is not keepCoreHead %s", tag, b.BuiltFrom, head))
	}
	required, err := requiredArtifacts(repo, head)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot list the artifacts the world at %s deploys: %v", tag, head, err)}
	}
	for _, name := range required {
		a, ok := b.Artifacts[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: the world deploys %s, but dependencies.ecdsaBuild.artifacts has no record of it", tag, name))
			continue
		}
		if !hex64.MatchString(a.Sha256) {
			problems = append(problems, fmt.Sprintf("%s: artifact %s has no SHA-256 (%q)", tag, name, a.Sha256))
		}
		info, ok := b.BuildInfos[a.BuildInfo]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: artifact %s names build-info %q, which dependencies.ecdsaBuild.buildInfos does not record", tag, name, a.BuildInfo))
			continue
		}
		source := name[:strings.LastIndex(name, ":")]
		if _, ok := info.Sources[source]; !ok {
			problems = append(problems, fmt.Sprintf("%s: artifact %s names build-info %s, which did not compile its source %s", tag, name, a.BuildInfo, source))
		}
	}
	ids := make([]string, 0, len(b.BuildInfos))
	for id := range b.BuildInfos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		info := b.BuildInfos[id]
		if !hex64.MatchString(info.Sha256) {
			problems = append(problems, fmt.Sprintf("%s: build-info %s has no SHA-256 (%q)", tag, id, info.Sha256))
		}
		if len(info.Sources) == 0 {
			problems = append(problems, fmt.Sprintf("%s: build-info %s records no source hashes", tag, id))
		}
		paths := make([]string, 0, len(info.Sources))
		for p := range info.Sources {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if !hex64.MatchString(info.Sources[p]) {
				problems = append(problems, fmt.Sprintf("%s: build-info %s source %s has no SHA-256 (%q)", tag, id, p, info.Sources[p]))
				continue
			}
			if !keepCoreSource(p) {
				continue
			}
			content, ok := headBlob(repo, head, "solidity/ecdsa/"+p)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: stale build: build-info %s compiled %s, which keepCoreHead %s does not have", tag, id, p, head))
				continue
			}
			if got := sha256Hex(content); got != info.Sources[p] {
				problems = append(problems, fmt.Sprintf("%s: stale build: build-info %s compiled %s with SHA-256 %s, but keepCoreHead %s has %s", tag, id, p, info.Sources[p], head, got))
			}
		}
	}
	if lock, ok := headBlob(repo, head, "solidity/ecdsa/yarn.lock"); !ok {
		problems = append(problems, fmt.Sprintf("%s: keepCoreHead %s has no solidity/ecdsa/yarn.lock", tag, head))
	} else if got := sha256Hex(lock); b.YarnLockSha256 != got {
		problems = append(problems, fmt.Sprintf("%s: the build's yarnLockSha256 %q is not keepCoreHead %s's solidity/ecdsa/yarn.lock (%s)", tag, b.YarnLockSha256, head, got))
	}
	return problems
}

// nodeStages are the stages that start node processes (every stage after
// 01-legacy and 02-upgrade), so each must leave node logs.
func nodeStages(m *manifest) []string {
	var stages []string
	for _, s := range m.Stages {
		if s.Stage != "01-legacy" && s.Stage != "02-upgrade" {
			stages = append(stages, s.Stage)
		}
	}
	return stages
}

// checkNodeLogs (#41): every node log in the evidence is hashed in the
// manifest, and only those. Invariant: the set of files under nodes/ equals
// the keys of manifest.nodeLogs, each value is the file's SHA-256, and every
// stage that starts nodes left at least one node log (so the inventory is
// not vacuously complete).
func checkNodeLogs(m *manifest, evidence string) []string {
	const tag = "K-09 #41 (K09-24)"
	var onDisk []string
	root := filepath.Join(evidence, "nodes")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(evidence, path)
			onDisk = append(onDisk, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(onDisk)
	var problems []string
	for _, stage := range nodeStages(m) {
		found := false
		for _, p := range onDisk {
			if strings.HasPrefix(p, "nodes/"+stage+"/") {
				found = true
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("%s: stage %s started nodes but the evidence has no node log under nodes/%s/", tag, stage, stage))
		}
	}
	if m.NodeLogs == nil {
		return append(problems, fmt.Sprintf("%s: the manifest has no nodeLogs: none of the %d node logs under nodes/ is hashed (stage logs only have logSha256)", tag, len(onDisk)))
	}
	for _, p := range onDisk {
		want, ok := m.NodeLogs[p]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: node log %s is not hashed in manifest.nodeLogs", tag, p))
			continue
		}
		content, err := os.ReadFile(filepath.Join(evidence, p))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", tag, err))
			continue
		}
		if got := sha256Hex(content); got != want {
			problems = append(problems, fmt.Sprintf("%s: node log %s has SHA-256 %s, but manifest.nodeLogs records %s", tag, p, got, want))
		}
	}
	listed := make([]string, 0, len(m.NodeLogs))
	for p := range m.NodeLogs {
		listed = append(listed, p)
	}
	sort.Strings(listed)
	for _, p := range listed {
		if _, err := os.Stat(filepath.Join(evidence, filepath.FromSlash(p))); err != nil || !strings.HasPrefix(p, "nodes/") {
			problems = append(problems, fmt.Sprintf("%s: manifest.nodeLogs lists %s, which is not a node log in the evidence", tag, p))
		}
	}
	return problems
}

// drainStages are the stages whose ECDSA-to-FROST drain must come from the
// ECDSA signing group in the nodes (#40).
var drainStages = []string{"04-drain", "06-restart"}

// fixtureDrainMarker is the phase line the harness's FIXTURE drain logs
// (frost_node_b6_world_test.go drainFrom: the legacy source's test key signs
// the drain and the harness mines it).
const fixtureDrainMarker = "b6 phase drain proven"

// drainRecord is the line a drain stage logs once it has checked on chain
// that the Bridge accepted the commitment and the proof of the drain:
//
//	K09DRAIN source=0x<20-byte wallet PKH> committer=0x<address that sent
//	submitMovingFundsCommitment> commitTx=0x<that Ethereum transaction>
//	drain=<drain txid, display byte order>
var drainRecord = regexp.MustCompile(`K09DRAIN source=0x([0-9a-f]{40}) committer=(0x[0-9a-fA-F]{40}) commitTx=(0x[0-9a-f]{64}) drain=([0-9a-f]{64})\s*$`)

// commitmentProduced reports whether a node log records base's own
// production of the commitment (review B1): the chain client's submission
// of exactly that transaction (pkg/chain/ethereum/tbtc/gen/contract
// Bridge.go) and MovingFundsTask's confirmation for the source wallet
// (pkg/tbtcpg/moving_funds.go). base skips both when a commitment already
// exists, as when a harness sent it.
func commitmentProduced(path, commitTx, source string) (submitted, confirmed bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, false, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "submitted transaction submitMovingFundsCommitment with id: ["+commitTx+"]") {
			submitted = true
		}
		if strings.Contains(line, "the moving funds commitment transaction successfully confirmed") &&
			strings.Contains(line, `"walletPKH": "0x`+source+`"`) {
			confirmed = true
		}
	}
	return submitted, confirmed, nil
}

// operatorAddress is node operator N's address: the K-09 world gives
// operator N the scalar N as its key (frost_node_b6_world_test.go
// b6Operators; k09_world_test.go setUpFrost). Public test scalars.
func operatorAddress(n int) common.Address {
	key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(int64(n)).Bytes(), 32))
	if err != nil {
		panic(err)
	}
	return crypto.PubkeyToAddress(key.PublicKey)
}

var operatorLog = regexp.MustCompile(`^operator-([0-9]+)\.log$`)

// movingFundsBroadcast is a base node's completed broadcast of a MovingFunds
// action's transaction (pkg/tbtc/wallet.go broadcastTransaction, logged by
// moving_funds.go's broadcastTxLogger).
type movingFundsBroadcast struct {
	Wallet string `json:"wallet"`
	Action string `json:"action"`
	TxHash string `json:"movingFundsTxHash"`
}

// broadcasts returns the MovingFunds broadcasts a node log records.
func broadcasts(path string) ([]movingFundsBroadcast, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var found []movingFundsBroadcast
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "\tbroadcasting completed\t{") {
			continue
		}
		var b movingFundsBroadcast
		if json.Unmarshal([]byte(line[strings.Index(line, "\tbroadcasting completed\t{")+len("\tbroadcasting completed\t"):]), &b) != nil {
			continue
		}
		if b.Action == "MovingFunds" {
			found = append(found, b)
		}
	}
	return found, scanner.Err()
}

// walletPKH is the 20-byte public key hash of a logged wallet public key
// (base marshalPublicKey: uncompressed), as base's bitcoin.PublicKeyHash
// computes it (HASH160 of the compressed key).
func walletPKH(logged string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(logged, "0x"))
	if err != nil {
		return "", err
	}
	key, err := btcec.ParsePubKey(raw)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(btcutil.Hash160(key.SerializeCompressed())), nil
}

// checkDrainSigners (#40): in each drain stage, the ECDSA-to-FROST drain was
// committed by a node operator and signed and broadcast by a base node's
// MovingFunds action for the legacy source, not made by the harness with the
// source's FIXTURE key. Invariant, per stage: the stage log is the one the
// manifest hashed; it shows no harness FIXTURE drain; it records exactly one
// drain (K09DRAIN); the commitment's sender is one of the stage's node
// operators; and a node log of that stage records a completed MovingFunds
// broadcast of exactly that transaction for exactly that source wallet; and
// the committer's own node log records base's submission of exactly that
// commitment transaction and MovingFundsTask's confirmation for the source
// (review B1).
func checkDrainSigners(m *manifest, evidence string) []string {
	const tag = "K-09 #40 (K09-23)"
	var problems []string
	for _, stage := range drainStages {
		problem := func(format string, args ...interface{}) {
			problems = append(problems, fmt.Sprintf("%s %s: ", tag, stage)+fmt.Sprintf(format, args...))
		}
		log, logSha := "", ""
		for _, s := range m.Stages {
			if s.Stage == stage {
				log, logSha = s.Log, s.LogSha256
			}
		}
		if log == "" {
			problem("the manifest has no stage %s", stage)
			continue
		}
		raw, err := os.ReadFile(filepath.Join(evidence, log))
		if err != nil {
			problem("%v", err)
			continue
		}
		if got := sha256Hex(raw); got != logSha {
			problem("the stage log %s has SHA-256 %s, but the manifest records %s", log, got, logSha)
			continue
		}
		text := string(raw)
		if strings.Contains(text, fixtureDrainMarker) {
			problem("the stage log shows the harness's FIXTURE drain (%q, drainFrom: the legacy source's test key signs the drain and the harness mines it), not a drain signed by the ECDSA signing group in the nodes", fixtureDrainMarker)
		}
		var records [][]string
		for _, line := range strings.Split(text, "\n") {
			if r := drainRecord.FindStringSubmatch(line); r != nil {
				records = append(records, r)
			}
		}
		if len(records) != 1 {
			problem("the stage log records %d drains (K09DRAIN source=... committer=... commitTx=... drain=...), want exactly 1: nothing shows who committed and signed the drain", len(records))
			continue
		}
		source, committer, commitTx, drain := records[0][1], common.HexToAddress(records[0][2]), records[0][3], records[0][4]
		entries, _ := os.ReadDir(filepath.Join(evidence, "nodes", stage))
		committed, committerLog := false, ""
		var operators []string
		for _, e := range entries {
			n := operatorLog.FindStringSubmatch(e.Name())
			if n == nil {
				continue
			}
			var i int
			fmt.Sscanf(n[1], "%d", &i)
			operators = append(operators, operatorAddress(i).Hex())
			if operatorAddress(i) == committer {
				committed, committerLog = true, e.Name()
			}
		}
		if !committed {
			problem("the drain commitment for source 0x%s was sent by %s, which is not one of the stage's node operators %v", source, committer.Hex(), operators)
		} else {
			submitted, confirmed, err := commitmentProduced(filepath.Join(evidence, "nodes", stage, committerLog), commitTx, source)
			switch {
			case err != nil:
				problem("%v", err)
			case !submitted || !confirmed:
				problem("the committer's node log nodes/%s/%s does not record base's own commitment (submitted %s: %v; MovingFundsTask confirmation for 0x%s: %v): the commitment was not produced by the node's MovingFunds task", stage, committerLog, commitTx, submitted, source, confirmed)
			}
		}
		signed, other := false, ""
		for _, e := range entries {
			if operatorLog.FindStringSubmatch(e.Name()) == nil {
				continue
			}
			found, err := broadcasts(filepath.Join(evidence, "nodes", stage, e.Name()))
			if err != nil {
				problem("%v", err)
				continue
			}
			for _, b := range found {
				if b.TxHash != drain {
					continue
				}
				pkh, err := walletPKH(b.Wallet)
				switch {
				case err != nil:
					other = fmt.Sprintf("%s (unreadable wallet key: %v)", e.Name(), err)
				case pkh != source:
					other = fmt.Sprintf("%s for wallet PKH 0x%s", e.Name(), pkh)
				default:
					signed = true
				}
			}
		}
		if !signed {
			if other != "" {
				problem("the drain %s was broadcast by a node's MovingFunds action only as %s, not for the legacy source 0x%s", drain, other, source)
			} else {
				problem("no node log under nodes/%s records a completed MovingFunds broadcast of the drain %s (base wallet.go \"broadcasting completed\", movingFundsTxHash): the ECDSA signing group in the nodes did not sign it", stage, drain)
			}
		}
	}
	return problems
}

// TestK09Evidence checks a finished K-09 run's evidence directory
// (K09_EVIDENCE) for the three batch-9 gaps. A missing variable is a
// failure, not a skip (as test/k09/run.sh treats its inputs).
// Refs tuxxy/keep-core#22, tuxxy/keep-core#40, tuxxy/keep-core#41.
func TestK09Evidence(t *testing.T) {
	evidence := os.Getenv("K09_EVIDENCE")
	if evidence == "" {
		t.Fatal("K09_EVIDENCE (a K-09 run's evidence directory) is required")
	}
	m, err := loadManifest(evidence)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	report := func(t *testing.T, problems []string) {
		for _, p := range problems {
			t.Error(p)
		}
	}
	t.Run("22-ecdsa-build", func(t *testing.T) { report(t, checkEcdsaBuild(m, repo)) })
	t.Run("41-node-log-hashes", func(t *testing.T) { report(t, checkNodeLogs(m, evidence)) })
	t.Run("40-drain-signers", func(t *testing.T) { report(t, checkDrainSigners(m, evidence)) })
}
