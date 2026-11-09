#!/usr/bin/env bash
# K-09 lifecycle demonstration (KC-SF-V3 §6 K-09): the FROST lifecycle driven
# through real node processes, stage by stage. Local only; nothing here runs
# in CI. See test/k09/README.md.
#
# Usage: run.sh <new evidence directory>
# Required environment (no defaults; a missing variable is a failure, not a
# skip): SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256 (the pinned worker),
# SNOWFALL_COMMIT (the commit ../snowfall must be at, clean), the
# LEGACY_* environment of test/legacy/run.sh including LEGACY_UPGRADED_DIR and
# LEGACY_UPGRADED_COMMIT, and K09_ECDSA_NODE_MODULES / K09_ECDSA_NODE_LOCK:
# solidity/ecdsa node_modules and the yarn.lock they were installed from,
# which must be the head's (as test/legacy/build-baseline.sh checks its own).
#
# The run builds from HEAD only (tuxxy/keep-core#22): it exports HEAD with
# git archive, compiles keep-core's solidity/ecdsa (the FROST registry stack
# the world deploys) into a fresh directory of that export, and builds the
# test binary from the same export, so the world reads exactly that build.
# The SNOWFALL Go client (go.mod's ../snowfall replace) is exported the same
# way, with git archive at SNOWFALL_COMMIT, never through the live checkout
# (#35). The build and every stage run under env -i with a named allowlist
# (tbtc-v2#19), recorded in the manifest (environment).
# The manifest records the build (dependencies.ecdsaBuild) and hashes every
# node log (nodeLogs, #41). test/k09/evidence checks both, and the drain
# record of #40, on the evidence directory.
#
# Every stage runs in its own process of one test binary. A stage starts from
# the snapshot its predecessor left in this run (K09_RUN_ID), so a stage
# runs only after its predecessor and reports itself blocked if that failed.
# Independent branches run in two lanes. Every stage runs in its own process
# group; after the stage, pass or fail, the runner kills that group and the
# groups of the stage's orphaned node processes, and records what it had to
# kill (stages[].leftoverKilled, #30).
# The manifest records every stage with its result and times; "accepted" is
# true only with every stage passing, zero skips and a clean tree. After the
# run, a fixed-pattern secret scan covers everything in the evidence
# directory; node stores are never copied into it. manifest.files hashes
# every file in the evidence directory but manifest.json (#36).
set -euo pipefail
umask 077
: "${SNOWFALL_WORKER:?pinned worker required}" "${SNOWFALL_WORKER_SHA256:?pinned worker hash required}"
inputs="SNOWFALL_WORKER SNOWFALL_WORKER_SHA256 SNOWFALL_COMMIT LEGACY_BASELINE_DIR LEGACY_TBTC_REPO LEGACY_ANVIL LEGACY_BITCOIND LEGACY_BITCOIND_SHA256 LEGACY_UPGRADED_DIR LEGACY_UPGRADED_COMMIT K09_ECDSA_NODE_MODULES K09_ECDSA_NODE_LOCK"
for v in $inputs; do
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
head_lock=$(git show HEAD:solidity/ecdsa/yarn.lock | shasum -a 256 | cut -d' ' -f1)
given_lock=$(shasum -a 256 < "$K09_ECDSA_NODE_LOCK" | cut -d' ' -f1)
[ "$head_lock" = "$given_lock" ] || { echo "K09_ECDSA_NODE_LOCK is not HEAD's solidity/ecdsa/yarn.lock" >&2; exit 2; }

# ../snowfall must be a clean checkout at SNOWFALL_COMMIT (#35).
snowfall=$(cd "$kc_root/../snowfall" && pwd)
snowfall_commit=$(git -C "$snowfall" rev-parse --verify --quiet "$SNOWFALL_COMMIT^{commit}" || true)
[ -n "$snowfall_commit" ] && [ "$(git -C "$snowfall" rev-parse HEAD)" = "$snowfall_commit" ] \
  || { echo "../snowfall ($snowfall) is not at SNOWFALL_COMMIT $SNOWFALL_COMMIT" >&2; exit 2; }
[ -z "$(git -C "$snowfall" status --porcelain)" ] || { echo "../snowfall ($snowfall) has uncommitted changes" >&2; exit 2; }

# The build and every stage see only these variables (tbtc-v2#19): the
# inputs above, the Go caches if set, and Go settings that cannot come from
# the caller. The manifest records each name and value (environment).
run_env=(PATH="$PATH" HOME="$HOME")
for v in TMPDIR $inputs GOCACHE GOMODCACHE GOPATH; do
  [ -z "${!v:-}" ] || run_env+=("$v=${!v}")
done
run_env+=(GOFLAGS= GOTOOLCHAIN=local GOWORK=off)

run_id="$(date +%Y%m%d%H%M%S)-$$"
# Node directories (stores included) stay outside the evidence and go at
# exit; only node.log files are copied in.
scratch=$(mktemp -d)
cleanup() {
  # Process-group cleanup: every stage and node process leads its own group
  # (a stage's Anvil and bitcoind run in the stage's); none may outlive the
  # run, an interrupted one included (#30). The lanes go first, so no new
  # stage starts; then every recorded stage group and the group of every
  # remaining test-binary process (stages and nodes), never run.sh's own.
  kill -9 $(jobs -p) 2>/dev/null || true
  own=$(ps -o pgid= -p $$ | tr -d ' ')
  groups=$(cat "$scratch/groups" 2>/dev/null || true)
  for p in $(pgrep -f -- "$evidence/tbtc.test" || true); do
    groups="$groups $(ps -o pgid= -p "$p" 2>/dev/null | tr -d ' ' || true)"
  done
  for g in $groups; do
    [ "$g" = "$own" ] || kill -9 -- "-$g" 2>/dev/null || true
  done
  pids=$(pgrep -f -- "$evidence/tbtc.test" || true)
  [ -z "$pids" ] || kill -9 $pids 2>/dev/null || true
  rm -rf "$scratch"
  find "$HOME/Library/Caches/keep-core-b6-tests" -maxdepth 2 -name "k09-$run_id-*" -exec rm -rf {} + 2>/dev/null || true
}
trap cleanup EXIT

# HEAD only: the export the ECDSA build and the test binary both come from.
# go.mod replaces the SNOWFALL Go client with ../snowfall, so the export
# gets the checkout's sibling.
src="$scratch/src"
mkdir -p "$src/keep-core"
git archive --format=tar HEAD | tar -x -C "$src/keep-core"
mkdir -p "$src/snowfall"
git -C "$snowfall" archive --format=tar "$snowfall_commit" | tar -x -C "$src/snowfall"
ln -s "$K09_ECDSA_NODE_MODULES" "$src/keep-core/solidity/ecdsa/node_modules"
(cd "$src/keep-core/solidity/ecdsa" && env -i "${run_env[@]}" NODE_OPTIONS=--max-old-space-size=8192 npx hardhat compile) > "$evidence/ecdsa-build.log" 2>&1 \
  || { echo "solidity/ecdsa build failed (ecdsa-build.log)" >&2; exit 2; }
(cd "$src/keep-core" && env -i "${run_env[@]}" go test -c -o "$evidence/tbtc.test" ./pkg/tbtc)
go_version=$(cd "$src/keep-core" && env -i "${run_env[@]}" go version)

results="$evidence/cases.tsv"
: > "$results"

# stop_stage <stage> <process group> (#30): after the stage, pass or fail,
# kill what it left behind: its own process group (Anvil and bitcoind start
# in it, with no group of their own) and the group of every node process of
# this run whose stage process is gone (nodes lead their own groups; once
# their stage dies, their parent is 1). Each process killed is recorded for
# the stage (killed-<stage>); one still alive after the kill is a problem
# (survived-<stage>).
stop_stage() {
  local stage=$1 groups=$2 g p left i
  for p in $(pgrep -P 1 -f -- "$evidence/tbtc.test" || true); do
    g=$(ps -o pgid= -p "$p" 2>/dev/null | tr -d ' ' || true)
    [ -z "$g" ] || groups="$groups $g"
  done
  : > "$scratch/killed-$stage"
  for g in $groups; do
    for p in $(pgrep -g "$g" || true); do
      echo "$p $(ps -o comm= -p "$p" 2>/dev/null || true)" >> "$scratch/killed-$stage"
    done
    kill -9 -- "-$g" 2>/dev/null || true
  done
  for i in 1 2 3 4 5 6 7 8 9 10; do
    left=""
    for g in $groups; do
      for p in $(pgrep -g "$g" || true); do
        case "$(ps -o stat= -p "$p" 2>/dev/null || true)" in Z*|"") ;; *) left="$left $p" ;; esac
      done
    done
    [ -n "$left" ] || break
    sleep 0.2
  done
  [ -z "$left" ] || echo "$left" > "$scratch/survived-$stage"
}

# run_stage <stage> <test GOMAXPROCS>
run_stage() {
  local stage=$1 procs=$2 status=0
  mkdir -p "$scratch/$stage"
  # Each of the three node processes runs at GOMAXPROCS 1; with the test
  # process at <procs>, a lane uses procs+3 Go cores (shared rules: at most
  # 8 in all, so two lanes run the test process at 1).
  # The stage process leads its own process group (#30): python3 sets it
  # and execs the test binary.
  env -i "${run_env[@]}" K09_RUN_ID=$run_id B6_DEBUG_DIR="$scratch/$stage" GOMAXPROCS=$procs \
    python3 -c 'import os, sys; os.setpgid(0, 0); os.execv(sys.argv[1], sys.argv[1:])' \
    "$evidence/tbtc.test" -test.v -test.count=1 -test.timeout 12m \
    -test.run "^TestK09Lifecycle\$/^${stage}\$" > "$evidence/$stage.log" 2>&1 &
  local pgid=$!
  echo "$pgid" >> "$scratch/groups"
  wait "$pgid" || status=$?
  stop_stage "$stage" "$pgid"
  printf '%s\t%s.log\t%s\n' "$stage" "$stage" "$status" >> "$results"
  grep -E -- '^\s*--- (PASS|FAIL|SKIP): TestK09Lifecycle/' "$evidence/$stage.log" | sed "s|^|$stage: |" || true
  # Node logs only (never stores).
  for log in "$scratch/$stage"/case-*/operator-*/node.log; do
    [ -f "$log" ] || continue
    operator=$(basename "$(dirname "$log")")
    mkdir -p "$evidence/nodes/$stage"
    cp "$log" "$evidence/nodes/$stage/$operator.log"
  done
  rm -rf "${scratch:?}/$stage"
}
# run_lane <stage>...: the stages in order, two lanes at a time.
run_lane() {
  for stage in "$@"; do run_stage "$stage" 1; done
}

start=$(date +%s)
# The main chain to 04 alone; 07 (from 04) beside 05 and 06; then the
# branches of 06 in two lanes. A stage's predecessor always finished first.
for stage in 01-legacy 02-upgrade 03-dkg 04-drain; do run_stage "$stage" 2; done
run_lane 07-reservation-timeout &
lane=$!
run_lane 05-redemption 06-restart
wait "$lane"
# The two heartbeat stages (six heartbeat windows each) go in separate lanes.
run_lane 12-no-inactivity 09-movement &
lane=$!
run_lane 10-inactivity 08-deposit-refund 11-cutover-closure
wait "$lane"
wall=$(( $(date +%s) - start ))

# Leftover processes: none may survive a stage.
leftover=$(pgrep -f -- "$evidence/tbtc.test" || true)

# Fixed-pattern secret scan over the whole evidence directory (the test
# binary excluded: it holds the test keys by construction).
secrets=0
grep -rlF -f test/k09/secret-patterns.txt --exclude=tbtc.test "$evidence" > "$scratch/secret-hits" || true
[ ! -s "$scratch/secret-hits" ] || secrets=$(wc -l < "$scratch/secret-hits" | tr -d ' ')
cp "$scratch/secret-hits" "$evidence/secret-scan-hits.txt"

python3 - "$evidence" "$kc_root" "$wall" "$secrets" "${leftover:-}" "$src/keep-core" "$scratch" "$snowfall_commit" "$go_version" "${run_env[@]}" <<'EOF'
import hashlib, json, os, re, subprocess, sys
evidence, root, wall, secrets, leftover, src = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), sys.argv[5], sys.argv[6]
scratch, snowfall_commit, go_version = sys.argv[7], sys.argv[8], sys.argv[9]
run_env = dict(a.split("=", 1) for a in sys.argv[10:])
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
def ecdsa_build():
    # The artifacts the world deploys (every b6Deploy call in the K-09 files
    # of the export), each with its build-info and that build-info's compiled
    # sources, as test/k09/evidence/README.md specifies (#22).
    build = os.path.join(src, "solidity/ecdsa/build")
    names = set()
    for f in os.listdir(os.path.join(src, "pkg/tbtc")):
        if f.startswith("k09_") and f.endswith(".go"):
            names.update(m.group(1) + ":" + m.group(2) for m in re.finditer(r'b6Deploy\(\s*"([^"]+)",\s*"([^"]+)"', open(os.path.join(src, "pkg/tbtc", f)).read()))
    artifacts, infos = {}, {}
    for name in sorted(names):
        source, contract = name.rsplit(":", 1)
        base = os.path.join(build, source, contract)
        info_path = os.path.normpath(os.path.join(os.path.dirname(base), json.load(open(base + ".dbg.json"))["buildInfo"]))
        info_id = os.path.basename(info_path)[:-len(".json")]
        artifacts[name] = {"sha256": sha(base + ".json"), "buildInfo": info_id}
        if info_id not in infos:
            data = json.load(open(info_path))
            infos[info_id] = {"sha256": sha(info_path),
                              "sources": {p: hashlib.sha256(v["content"].encode()).hexdigest() for p, v in data["input"]["sources"].items()}}
    return {"builtFrom": head, "yarnLockSha256": sha(env["K09_ECDSA_NODE_LOCK"]), "artifacts": artifacts, "buildInfos": infos}
stages = []
order = ["01-legacy", "02-upgrade", "03-dkg", "04-drain", "05-redemption", "06-restart", "07-reservation-timeout",
         "08-deposit-refund", "09-movement", "10-inactivity", "11-cutover-closure", "12-no-inactivity"]
rows = sorted((line.rstrip("\n").split("\t") for line in open(os.path.join(evidence, "cases.tsv"))), key=lambda r: order.index(r[0]))
if [r[0] for r in rows] != order:
    problems.append("the run did not invoke every intended stage exactly once")
for stage, log, status in rows:
    text = open(os.path.join(evidence, log)).read()
    skipped = "--- SKIP" in text
    ran = re.search(r"--- PASS: TestK09Lifecycle/" + re.escape(stage) + r" ", text) is not None
    result = "pass" if status == "0" and ran and not skipped else ("skip" if skipped else "fail")
    times = {k: float(v) for k, v in re.findall(r"K09TIME stage=\S+ (load|body|save)=([0-9.]+)", text)}
    elapsed = re.search(r"--- \w+: TestK09Lifecycle/" + re.escape(stage) + r" \(([0-9.]+)s\)", text)
    # Every test-file message but timing and phase lines (review N5).
    failure = [l.strip() for l in text.splitlines()
               if re.search(r"_test\.go:\d+: ", l) and not re.search(r"K09TIME|b6 phase", l)]
    stages.append({"stage": stage, "log": log, "logSha256": sha(os.path.join(evidence, log)), "exit": int(status),
                   "result": result, "seconds": float(elapsed.group(1)) if elapsed else None, "phases": times,
                   "failure": failure[:3],
                   "leftoverKilled": [l.strip() for l in open(os.path.join(scratch, "killed-" + stage)) if l.strip()]})
    survived = os.path.join(scratch, "survived-" + stage)
    if os.path.exists(survived):
        problems.append(f"stage {stage}: processes survived the kill: {open(survived).read().strip()}")
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
    "repos": {"keepCore": head, "tbtcV2Upgraded": env["LEGACY_UPGRADED_COMMIT"], "tbtcV2Baseline": "40a11d1dcdcf82d3962e430067cfe3f97be81483",
              "snowfall": snowfall_commit},
    # The whole environment of the build and the stages (each stage adds
    # K09_RUN_ID, B6_DEBUG_DIR and GOMAXPROCS; the ECDSA build NODE_OPTIONS).
    "environment": run_env,
    "testFiles": {f: sha(os.path.join(root, "pkg/tbtc", f)) for f in sorted(os.listdir(os.path.join(root, "pkg/tbtc"))) if f.startswith("k09_")},
    "runner": {"runSh": sha(os.path.join(root, "test/k09/run.sh")), "secretPatterns": sha(os.path.join(root, "test/k09/secret-patterns.txt"))},
    "dependencies": {
        "baseline": baseline, "upgraded": upgraded,
        "worker": {"path": env["SNOWFALL_WORKER"], "sha256": env["SNOWFALL_WORKER_SHA256"]},
        "bitcoind": {"path": env["LEGACY_BITCOIND"], "sha256": env["LEGACY_BITCOIND_SHA256"]},
        "anvil": {"path": env["LEGACY_ANVIL"], "sha256": sha(env["LEGACY_ANVIL"]),
                  "version": subprocess.run([env["LEGACY_ANVIL"], "--version"], capture_output=True, text=True).stdout.splitlines()[0]},
        "go": go_version,
        "ecdsaBuild": ecdsa_build(),
    },
    "wallSeconds": wall, "intendedStages": len(stages), "passed": passed, "skipped": skips, "stages": stages,
    "secretScan": {"patterns": "test/k09/secret-patterns.txt", "filesMatched": secrets},
    # Every node log, hashed after the copy (#41).
    "nodeLogs": {os.path.relpath(os.path.join(d, f), evidence): sha(os.path.join(d, f))
                 for d, _, files in os.walk(os.path.join(evidence, "nodes")) for f in sorted(files)},
    "leftoverProcesses": leftover or None,
    "problems": problems, "accepted": not problems,
}
# Every file in the evidence directory but the manifest itself (#36).
manifest["files"] = {os.path.relpath(os.path.join(d, f), evidence): sha(os.path.join(d, f))
                     for d, _, files in os.walk(evidence) for f in sorted(files)
                     if os.path.relpath(os.path.join(d, f), evidence) != "manifest.json"}
with open(os.path.join(evidence, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=2)
print(f"manifest: {passed}/{len(stages)} stages passed, {skips} skipped, wall {wall}s, secret hits {secrets}, accepted={manifest['accepted']}")
for p in problems:
    print("  " + p)
sys.exit(0 if manifest["accepted"] else 1)
EOF
