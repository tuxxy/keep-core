#!/usr/bin/env bash
# K-09 lifecycle demonstration (KC-SF-V3 §6 K-09): the FROST lifecycle driven
# through real node processes, stage by stage. Local only; nothing here runs
# in CI. See test/k09/README.md.
#
# Usage: run.sh <new evidence directory>
# Required environment (no defaults; a missing variable is a failure, not a
# skip): SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256 (the pinned worker), the
# LEGACY_* environment of test/legacy/run.sh including LEGACY_UPGRADED_DIR and
# LEGACY_UPGRADED_COMMIT, and keep-core's solidity/ecdsa/build (the FROST
# registry stack).
#
# Every stage runs in its own process of one test binary. A stage starts from
# the snapshot its predecessor left in this run (K09_RUN_ID), so the stages
# run in order and a stage whose predecessor failed reports itself blocked.
# The manifest records every stage with its result and times; "accepted" is
# true only with every stage passing, zero skips and a clean tree. After the
# run, a fixed-pattern secret scan covers everything in the evidence
# directory; node stores are never copied into it.
set -euo pipefail
umask 077
: "${SNOWFALL_WORKER:?pinned worker required}" "${SNOWFALL_WORKER_SHA256:?pinned worker hash required}"
for v in LEGACY_BASELINE_DIR LEGACY_TBTC_REPO LEGACY_ANVIL LEGACY_BITCOIND LEGACY_BITCOIND_SHA256 LEGACY_UPGRADED_DIR LEGACY_UPGRADED_COMMIT; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done
evidence=${1:?evidence directory required}
if [ -e "$evidence" ]; then
  echo "$evidence exists: evidence directories are never overwritten" >&2
  exit 2
fi
mkdir -p "$evidence"
evidence=$(cd "$evidence" && pwd)
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$kc_root"
[ -d solidity/ecdsa/build ] || { echo "solidity/ecdsa/build required" >&2; exit 2; }

run_id="$(date +%Y%m%d%H%M%S)-$$"
# Node directories (stores included) stay outside the evidence and go at
# exit; only node.log files are copied in.
scratch=$(mktemp -d)
cleanup() {
  # Process-group cleanup: every node process runs in its own group; none
  # may outlive the run.
  pids=$(pgrep -f -- "$evidence/tbtc.test" || true)
  [ -z "$pids" ] || kill -9 $pids 2>/dev/null || true
  rm -rf "$scratch"
  find "$HOME/Library/Caches/keep-core-b6-tests" -maxdepth 2 -name "k09-$run_id-*" -exec rm -rf {} + 2>/dev/null || true
}
trap cleanup EXIT

go test -c -o "$evidence/tbtc.test" ./pkg/tbtc

stages=(01-legacy 02-upgrade 03-dkg 04-drain 05-redemption 06-restart
  07-reservation-timeout 08-deposit-refund 09-movement 10-inactivity 11-cutover-closure)
results="$evidence/cases.tsv"
: > "$results"
start=$(date +%s)
for stage in "${stages[@]}"; do
  status=0
  mkdir -p "$scratch/$stage"
  # Two Go cores for the test process; each of the three node processes runs
  # at GOMAXPROCS 1 (shared rules: at most 8 Go cores).
  K09_RUN_ID=$run_id B6_DEBUG_DIR="$scratch/$stage" GOMAXPROCS=2 \
    "$evidence/tbtc.test" -test.v -test.count=1 -test.timeout 10m \
    -test.run "^TestK09Lifecycle\$/^${stage}\$" > "$evidence/$stage.log" 2>&1 || status=$?
  printf '%s\t%s.log\t%s\n' "$stage" "$stage" "$status" >> "$results"
  grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$evidence/$stage.log" | sed "s|^|$stage: |" || true
  # Node logs only (never stores).
  for log in "$scratch/$stage"/case-*/operator-*/node.log; do
    [ -f "$log" ] || continue
    operator=$(basename "$(dirname "$log")")
    mkdir -p "$evidence/nodes/$stage"
    cp "$log" "$evidence/nodes/$stage/$operator.log"
  done
  rm -rf "${scratch:?}/$stage"
done
wall=$(( $(date +%s) - start ))

# Leftover processes: none may survive a stage.
leftover=$(pgrep -f -- "$evidence/tbtc.test" || true)

# Fixed-pattern secret scan over the whole evidence directory (the test
# binary excluded: it holds the test keys by construction).
secrets=0
grep -rlF -f test/k09/secret-patterns.txt --exclude=tbtc.test "$evidence" > "$scratch/secret-hits" || true
[ ! -s "$scratch/secret-hits" ] || secrets=$(wc -l < "$scratch/secret-hits" | tr -d ' ')
cp "$scratch/secret-hits" "$evidence/secret-scan-hits.txt"

python3 - "$evidence" "$kc_root" "$wall" "$secrets" "${leftover:-}" <<'EOF'
import hashlib, json, os, re, subprocess, sys
evidence, root, wall, secrets, leftover = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), sys.argv[5]
env = os.environ
def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()
def git(repo, *args):
    return subprocess.run(["git", "-C", repo, *args], capture_output=True, text=True, check=True).stdout.strip()
def build(path):
    manifest = os.path.join(path, "manifest.json")
    return {"dir": path, "manifestSha256": sha(manifest), "manifest": json.load(open(manifest))}
problems = []
head = git(root, "rev-parse", "HEAD")
dirty = git(root, "status", "--porcelain")
if dirty:
    problems.append("the keep-core tree has uncommitted changes")
if sha(env["LEGACY_BITCOIND"]) != env["LEGACY_BITCOIND_SHA256"]:
    problems.append("bitcoind does not match LEGACY_BITCOIND_SHA256")
if sha(env["SNOWFALL_WORKER"]) != env["SNOWFALL_WORKER_SHA256"]:
    problems.append("the worker does not match SNOWFALL_WORKER_SHA256")
baseline, upgraded = build(env["LEGACY_BASELINE_DIR"]), build(env["LEGACY_UPGRADED_DIR"])
if baseline["manifest"].get("tbtcCommit") != "40a11d1dcdcf82d3962e430067cfe3f97be81483":
    problems.append("the baseline build is not tbtc-v2 40a11d1d")
if upgraded["manifest"].get("tbtcCommit") != env["LEGACY_UPGRADED_COMMIT"]:
    problems.append("the upgraded build is not LEGACY_UPGRADED_COMMIT")
stages = []
for line in open(os.path.join(evidence, "cases.tsv")):
    stage, log, status = line.rstrip("\n").split("\t")
    text = open(os.path.join(evidence, log)).read()
    skipped = "--- SKIP" in text
    ran = re.search(r"--- PASS: TestK09Lifecycle/" + re.escape(stage) + r" ", text) is not None
    result = "pass" if status == "0" and ran and not skipped else ("skip" if skipped else "fail")
    times = {k: float(v) for k, v in re.findall(r"K09TIME stage=\S+ (load|body|save)=([0-9.]+)", text)}
    elapsed = re.search(r"--- \w+: TestK09Lifecycle/" + re.escape(stage) + r" \(([0-9.]+)s\)", text)
    failure = [l.strip() for l in text.splitlines() if re.search(r"k09_\w+_test\.go:\d+: (FINDING|K-09|setup)", l)]
    stages.append({"stage": stage, "log": log, "logSha256": sha(os.path.join(evidence, log)), "exit": int(status),
                   "result": result, "seconds": float(elapsed.group(1)) if elapsed else None, "phases": times,
                   "failure": failure[:3]})
passed = sum(s["result"] == "pass" for s in stages)
skips = sum(s["result"] == "skip" for s in stages)
if passed != len(stages):
    problems.append(f"{len(stages) - passed} of {len(stages)} stages did not pass")
if secrets:
    problems.append(f"secret scan: {secrets} file(s) match a fixed pattern (secret-scan-hits.txt)")
if leftover:
    problems.append(f"node processes outlived the run: {leftover}")
tbtc = env["LEGACY_TBTC_REPO"]
manifest = {
    "suite": "keep-core K-09 lifecycle demonstration",
    "candidate": {"keepCoreHead": head, "clean": not dirty, "branch": git(root, "rev-parse", "--abbrev-ref", "HEAD")},
    "repos": {"keepCore": head, "tbtcV2Upgraded": env["LEGACY_UPGRADED_COMMIT"], "tbtcV2Baseline": "40a11d1dcdcf82d3962e430067cfe3f97be81483"},
    "testFiles": {f: sha(os.path.join(root, "pkg/tbtc", f)) for f in sorted(os.listdir(os.path.join(root, "pkg/tbtc"))) if f.startswith("k09_")},
    "runner": {"runSh": sha(os.path.join(root, "test/k09/run.sh")), "secretPatterns": sha(os.path.join(root, "test/k09/secret-patterns.txt"))},
    "dependencies": {
        "baseline": baseline, "upgraded": upgraded,
        "worker": {"path": env["SNOWFALL_WORKER"], "sha256": env["SNOWFALL_WORKER_SHA256"]},
        "bitcoind": {"path": env["LEGACY_BITCOIND"], "sha256": env["LEGACY_BITCOIND_SHA256"]},
        "anvil": {"path": env["LEGACY_ANVIL"], "sha256": sha(env["LEGACY_ANVIL"]),
                  "version": subprocess.run([env["LEGACY_ANVIL"], "--version"], capture_output=True, text=True).stdout.splitlines()[0]},
        "go": subprocess.run(["go", "version"], capture_output=True, text=True).stdout.strip(),
    },
    "wallSeconds": wall, "intendedStages": len(stages), "passed": passed, "skipped": skips, "stages": stages,
    "secretScan": {"patterns": "test/k09/secret-patterns.txt", "filesMatched": secrets},
    "leftoverProcesses": leftover or None,
    "problems": problems, "accepted": not problems,
}
with open(os.path.join(evidence, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=2)
print(f"manifest: {passed}/{len(stages)} stages passed, {skips} skipped, wall {wall}s, secret hits {secrets}, accepted={manifest['accepted']}")
for p in problems:
    print("  " + p)
sys.exit(0 if manifest["accepted"] else 1)
EOF
