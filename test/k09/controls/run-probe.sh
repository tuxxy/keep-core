#!/usr/bin/env bash
# K-09 wrong-behaviour probe bound to a head (tuxxy/keep-core#60, K09-45).
# Local only; nothing here runs in CI. See README.md here.
#
# Usage: run-probe.sh <patch> <stage> <new record directory>
#
# <stage> is a full stage name from test/k09/run.sh at HEAD (for example
# 08-deposit-refund). The probe applies <patch> to a git archive export of
# HEAD (never to the checkout), builds solidity/ecdsa in that export as
# run.sh does, builds the test binary from the export, and runs stages 01-06
# and <stage> of TestK09Lifecycle in one process (README.md's probe command).
#
# Required environment: run.sh's (SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256,
# SNOWFALL_COMMIT, the LEGACY_* inputs, K09_ECDSA_NODE_MODULES and
# K09_ECDSA_NODE_LOCK, which must be HEAD's solidity/ecdsa/yarn.lock). The
# SNOWFALL Go client is exported with git archive at SNOWFALL_COMMIT. The
# build and the probe see only run.sh's allowlist (tbtc-v2#19).
#
# The record directory holds probe.patch (a copy of the patch), probe.log,
# ecdsa-build.log and probe.json: head, clean, patch {path, sha256, copy},
# repos.snowfall, environment, go, testFiles (SHA-256 of each pkg/tbtc/k09_*
# file at head), stage, log, logSha256, exit and failure (the stage's failing
# test-file lines). test/k09/controls/probe_test.go (TestK09ProbeRecord)
# checks a record.
#
# No citable record (no probe.json) is left, and the exit status is non-zero,
# when: the tree has uncommitted changes, the record directory exists, the
# stage is unknown, the patch does not apply, a build fails, a stage before
# <stage> among 01-06 did not pass, <stage> did not fail at a test-file
# assertion, or the secret scan matches.
#
# Trust (PROBE-RECORDS.md): a record is unsigned; it binds a probe to its
# head only because this script is its sole writer.
set -euo pipefail
umask 077
patch_arg=${1:?usage: run-probe.sh <patch> <stage> <new record directory>}
stage=${2:?usage: run-probe.sh <patch> <stage> <new record directory>}
record=${3:?usage: run-probe.sh <patch> <stage> <new record directory>}

kc_root=$(cd "$(dirname "$0")/../../.." && pwd)
cd "$kc_root"

# The runner's stages, from run.sh at HEAD.
stages=$(git show HEAD:test/k09/run.sh | python3 -c '
import re, sys
m = re.search(r"^order = \[(.*?)\]", sys.stdin.read(), re.S | re.M)
print(" ".join(re.findall(r"\"([^\"]+)\"", m.group(1))) if m else "")')
[ -n "$stages" ] || { echo "test/k09/run.sh at HEAD names no stages" >&2; exit 2; }
case " $stages " in
  *" $stage "*) ;;
  *) echo "unknown stage $stage (run.sh's stages: $stages)" >&2; exit 2 ;;
esac
[ -f "$patch_arg" ] || { echo "no patch $patch_arg" >&2; exit 2; }
patch=$(cd "$(dirname "$patch_arg")" && pwd)/$(basename "$patch_arg")
if [ -e "$record" ]; then
  echo "$record exists: probe records are never overwritten" >&2
  exit 2
fi

inputs="SNOWFALL_WORKER SNOWFALL_WORKER_SHA256 SNOWFALL_COMMIT LEGACY_BASELINE_DIR LEGACY_TBTC_REPO LEGACY_ANVIL LEGACY_BITCOIND LEGACY_BITCOIND_SHA256 LEGACY_UPGRADED_DIR LEGACY_UPGRADED_COMMIT K09_ECDSA_NODE_MODULES K09_ECDSA_NODE_LOCK"
for v in $inputs; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done

# A record binds a clean head only.
head=$(git rev-parse HEAD)
[ -z "$(git status --porcelain)" ] || { echo "the keep-core tree has uncommitted changes: a probe record binds a clean head" >&2; exit 2; }
head_lock=$(git show HEAD:solidity/ecdsa/yarn.lock | shasum -a 256 | cut -d' ' -f1)
given_lock=$(shasum -a 256 < "$K09_ECDSA_NODE_LOCK" | cut -d' ' -f1)
[ "$head_lock" = "$given_lock" ] || { echo "K09_ECDSA_NODE_LOCK is not HEAD's solidity/ecdsa/yarn.lock" >&2; exit 2; }
snowfall=$(cd "$kc_root/../snowfall" && pwd)
snowfall_commit=$(git -C "$snowfall" rev-parse --verify --quiet "$SNOWFALL_COMMIT^{commit}" || true)
[ -n "$snowfall_commit" ] || { echo "../snowfall ($snowfall) has no commit SNOWFALL_COMMIT $SNOWFALL_COMMIT" >&2; exit 2; }

# run.sh's allowlist (tbtc-v2#19), plus the probe's GOMAXPROCS.
run_env=(PATH="$PATH" HOME="$HOME")
for v in TMPDIR $inputs GOCACHE GOMODCACHE GOPATH; do
  [ -z "${!v:-}" ] || run_env+=("$v=${!v}")
done
run_env+=(GOFLAGS= GOTOOLCHAIN=local GOWORK=off)

scratch=$(mktemp -d)
# probe_pid: the probe's process group while it may still be signalled;
# cleared once stop_probe has handled it, so no group ID that may since have
# been reused is signalled (as run.sh's stop_stage). snapshot_pid keeps the
# PID for the snapshot removal.
probe_pid=""
snapshot_pid=""
# stop_probe: kill the probe's own group and the group of every node process
# of this probe (nodes lead their own groups), never this script's own group.
stop_probe() {
  local own groups p g
  own=$(ps -o pgid= -p $$ | tr -d ' ')
  groups=$probe_pid
  for p in $(pgrep -f -- "$scratch/tbtc.test" || true); do
    groups="$groups $(ps -o pgid= -p "$p" 2>/dev/null | tr -d ' ' || true)"
  done
  for g in $groups; do
    [ "$g" = "$own" ] || kill -9 -- "-$g" 2>/dev/null || true
  done
  probe_pid=""
}
cleanup() {
  # Only an interrupted probe still has its group here.
  stop_probe
  rm -rf "$scratch"
  [ -z "$snapshot_pid" ] || find "$HOME/Library/Caches/keep-core-b6-tests" -maxdepth 2 -name "k09-local-$snapshot_pid-*" -exec rm -rf {} + 2>/dev/null || true
}
trap cleanup EXIT

# The patch is read once (review N3): the probe applies, hashes and records
# this copy, so a later change to <patch> cannot reach the record.
cp "$patch" "$scratch/probe.patch"

# HEAD only, with the patch: the export the build and the binary come from.
src="$scratch/src"
mkdir -p "$src/keep-core" "$src/snowfall"
git archive --format=tar HEAD | tar -x -C "$src/keep-core"
git -C "$snowfall" archive --format=tar "$snowfall_commit" | tar -x -C "$src/snowfall"
(cd "$src/keep-core" && git apply "$scratch/probe.patch") || { echo "the patch does not apply at $head" >&2; exit 2; }
ln -s "$K09_ECDSA_NODE_MODULES" "$src/keep-core/solidity/ecdsa/node_modules"
(cd "$src/keep-core/solidity/ecdsa" && env -i "${run_env[@]}" NODE_OPTIONS=--max-old-space-size=8192 npx hardhat compile) > "$scratch/ecdsa-build.log" 2>&1 \
  || { echo "solidity/ecdsa build failed:" >&2; tail -20 "$scratch/ecdsa-build.log" >&2; exit 2; }
(cd "$src/keep-core" && env -i "${run_env[@]}" go test -c -o "$scratch/tbtc.test" ./pkg/tbtc) > "$scratch/go-build.log" 2>&1 \
  || { echo "the test binary does not build:" >&2; tail -20 "$scratch/go-build.log" >&2; exit 2; }
go_version=$(cd "$src/keep-core" && env -i "${run_env[@]}" go version)

mkdir -p "$(dirname "$record")"
mkdir "$record" || { echo "$record exists: probe records are never overwritten" >&2; exit 2; }
record=$(cd "$record" && pwd)
cp "$scratch/probe.patch" "$record/probe.patch"
cp "$scratch/ecdsa-build.log" "$record/ecdsa-build.log"

# Stages 01-06 and <stage>, in one process (no K09_RUN_ID: the test keeps
# its snapshots in this process), run from the package directory as go test
# does. The process leads its own group.
before=""
for s in 01-legacy 02-upgrade 03-dkg 04-drain 05-redemption 06-restart; do
  [ "$s" = "$stage" ] && break
  before="$before|$s"
done
selected="${before#|}"
selected="${selected:+$selected|}$stage"
status=0
(cd "$src/keep-core/pkg/tbtc" && exec env -i "${run_env[@]}" GOMAXPROCS=2 \
  python3 -c 'import os, sys; os.setpgid(0, 0); os.execv(sys.argv[1], sys.argv[1:])' \
  "$scratch/tbtc.test" -test.v -test.count=1 -test.timeout 14m \
  -test.run "^TestK09Lifecycle\$/^($selected)\$") > "$record/probe.log" 2>&1 &
probe_pid=$!
snapshot_pid=$probe_pid
wait "$probe_pid" || status=$?
# Straight after the wait, as run.sh's stop_stage (review N1).
stop_probe

# Fixed-pattern secret scan over the record, as run.sh scans its evidence.
if git show HEAD:test/k09/secret-patterns.txt > "$scratch/secret-patterns.txt" \
  && grep -rlF -f "$scratch/secret-patterns.txt" "$record" > "$scratch/secret-hits"; then
  echo "secret scan: files in $record match a fixed pattern; no record written:" >&2
  cat "$scratch/secret-hits" >&2
  exit 1
fi

python3 - "$record" "$kc_root" "$head" "$patch" "$scratch/probe.patch" "$stage" "$status" "$snowfall_commit" "$go_version" "${before#|}" "${run_env[@]}" <<'EOF'
import hashlib, json, os, re, subprocess, sys
record, root, head, patch, patch_copy, stage, status, snowfall, go_version, before = sys.argv[1:11]
run_env = dict(a.split("=", 1) for a in sys.argv[11:])
def sha(b):
    return hashlib.sha256(b).hexdigest()
def git(*args):
    return subprocess.run(["git", "-C", root, *args], capture_output=True, check=True).stdout
log = open(os.path.join(record, "probe.log"), "rb").read()
text = log.decode(errors="replace")
problems = []
# Each stage before <stage> among 01-06 passed, so the failure is the
# probe's own, not a blocked stage or a setup error (review N-60c).
for s in [s for s in before.split("|") if s]:
    if re.search(r"^\s*--- PASS: TestK09Lifecycle/" + re.escape(s) + r" ", text, re.M) is None:
        problems.append("stage %s did not pass before %s" % (s, stage))
if re.search(r"^\s*--- FAIL: TestK09Lifecycle/" + re.escape(stage) + r" ", text, re.M) is None:
    problems.append("stage %s did not fail" % stage)
# The stage's own test-file lines (go test -v prints a subtest's output after
# its === RUN, CONT or NAME line), but timing and phase lines.
failure, inside = [], False
for line in text.splitlines():
    m = re.match(r"^=== (?:RUN|CONT|NAME|PAUSE)\s+(\S+)", line)
    if m:
        inside = m.group(1) == "TestK09Lifecycle/" + stage
        continue
    if inside and re.search(r"_test\.go:\d+: ", line) and not re.search(r"K09TIME|b6 phase", line):
        failure.append(line.strip())
if not failure:
    problems.append("stage %s printed no test-file assertion line" % stage)
if problems:
    print("no probe record written (%s): %s" % (record, "; ".join(problems)), file=sys.stderr)
    sys.exit(1)
rel = os.path.relpath(patch, root)
names = sorted(n for n in git("ls-tree", "--name-only", head, "pkg/tbtc/").decode().split()
               if os.path.basename(n).startswith("k09_"))
out = {
    "head": head, "clean": True,
    "patch": {"path": patch if rel.startswith("..") else rel, "sha256": sha(open(patch_copy, "rb").read()), "copy": "probe.patch"},
    "repos": {"keepCore": head, "snowfall": snowfall},
    "environment": run_env, "go": go_version,
    "testFiles": {os.path.basename(n): sha(git("cat-file", "blob", head + ":" + n)) for n in names},
    "stage": stage, "predecessors": [s for s in before.split("|") if s],
    "log": "probe.log", "logSha256": sha(log), "exit": int(status),
    "failure": failure,
}
with open(os.path.join(record, "probe.json"), "w") as f:
    json.dump(out, f, indent=2)
print("probe %s failed at: %s" % (stage, failure[0]))
EOF
