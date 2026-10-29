#!/usr/bin/env bash
# Remediation batch 2: the ECDSA-to-FROST drain producer (tuxxy/keep-core#34)
# and its constraint issues, against real contracts built from git, real
# Anvil and real Bitcoin Core regtest. Local only; nothing here runs in CI.
#
# This runner is the acceptance gate for the batch-2 keep-core tests. A plain
# `go test` without the LEGACY_* environment skips every case and proves
# nothing. Acceptance is the manifest.json this script writes, with
# "accepted": true, at the candidate head.
#
# Usage: run.sh [-j N] <evidence directory> [case filter (extended regex)]
# A filter runs a subset for diagnosis; such a manifest is never accepted.
# -j N runs up to N cases at once (default 12), each with GOMAXPROCS=1.
# Environment: the same variables as test/legacy/run.sh (no defaults).
#
# Every case runs in its own process, with its own Anvil, Bitcoin Core node,
# ports and data directories, so state a client keeps in package variables
# cannot leak from one case into another, and cases can run in parallel.
set -euo pipefail
umask 077
: "${LEGACY_BASELINE_DIR:?run test/legacy/build-baseline.sh first}"
: "${LEGACY_TBTC_REPO:?tbtc-v2 git repository required}"
: "${LEGACY_ANVIL:?Anvil required}"
: "${LEGACY_UPGRADED_DIR:?upgraded Bridge build required}"
: "${LEGACY_UPGRADED_COMMIT:?upgraded Bridge commit required}"
: "${LEGACY_BITCOIND:?Bitcoin Core bitcoind required}"
: "${LEGACY_BITCOIND_SHA256:?its SHA-256 required}"
: "${LEGACY_HEAVY_LOCK:?heavy lock file required}"
if [ -z "${LEGACY_HOLDS_LOCK:-}" ]; then
	LEGACY_HOLDS_LOCK=1 exec /usr/bin/lockf -k "$LEGACY_HEAVY_LOCK" bash "$0" "$@"
fi
jobs=12
if [ "${1:-}" = "-j" ]; then
	jobs=${2:?-j needs a number}
	shift 2
fi
evidence=${1:?evidence directory required}
filter=${2:-}
mkdir -p "$evidence"
evidence=$(cd "$evidence" && pwd)
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$kc_root"

go test -c -o "$evidence/tbtc.test" ./pkg/tbtc

# The intended cases. Subtest names are fixed in the test files.
cases=()
for c in FrostOnly-live1-code3-0x FrostOnly-live1-geth-32000-nodata FrostOnly-live1-transient-then-success FrostOnly-live1-frost-read-faults \
	FrostOnly-live0 LegacyOpen-live0 LegacyDraining-live0 FrostOnly-live1-two-targets \
	LegacyOpen-live1 LegacyDraining-live1 LegacyOpen-live1-frost-read-faults LegacyDraining-live1-frost-read-faults; do
	cases+=("^TestDrainProducer_Drains$/^${c}$")
done
for mode in LegacyOpen LegacyDraining; do
	for route in contained drain-off zero-eligible budget-partial; do
		cases+=("^TestDrainProducer_LegacyFallbackBeforeCutover$/^${mode}-${route}$")
	done
done
for route in contained drain-off zero-eligible budget-exhausted; do
	for live in live1 live0; do
		cases+=("^TestDrainProducer_NoRouteRecovery$/^FrostOnly-${live}-${route}$")
	done
done
cases+=("^TestDrainProducer_NoRouteRecovery$/^FrostOnly-live1-contained-geth-32000-nodata$")
for mode in LegacyOpen LegacyDraining; do
	for route in budget-partial budget-exhausted contained drain-off zero-eligible; do
		cases+=("^TestDrainProducer_NoRouteRecovery$/^${mode}-live0-${route}$")
	done
done
for c in FrostOnly-live1 FrostOnly-live0 LegacyOpen-live0 LegacyDraining-live0; do
	cases+=("^TestDrainProducer_BelowDust$/^${c}$")
done
for mode in FrostOnly LegacyOpen LegacyDraining; do
	cases+=("^TestDrainProducer_ShortIndex$/^${mode}-live1$")
done
for encoding in code3-0x geth-32000-nodata; do
	cases+=("^TestDrainProducer_PreFrostUnchanged$/^${encoding}$")
done
cases+=('^TestDrainProducer_BridgeUpgradedAfterStartup$')

# One case: its own process, GOMAXPROCS=1; records its exit status.
run_case() {
	local pattern=$1 name status=0
	name=$(printf '%s' "$pattern" | tr -c 'A-Za-z0-9_+-' '_')
	GOMAXPROCS=1 "$evidence/tbtc.test" -test.v -test.count=1 \
		-test.timeout 12m -test.run "$pattern" > "$evidence/$name.log" 2>&1 || status=$?
	printf '%s\n' "$status" > "$evidence/$name.status"
	grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$evidence/$name.log" | grep -v '^ \{4,\}' | sed "s|^|$name: |" || true
}
export -f run_case
export evidence


selected=()
for pattern in "${cases[@]}"; do
	if [ -n "$filter" ] && ! printf '%s' "$pattern" | grep -Eq -- "$filter"; then
		continue
	fi
	selected+=("$pattern")
done
started=$(date +%s)
printf '%s\0' "${selected[@]}" | xargs -0 -n 1 -P "$jobs" bash -c 'run_case "$1"' _
export RB2_WALL=$(( $(date +%s) - started )) RB2_JOBS=$jobs
echo "wall time: $RB2_WALL s for ${#selected[@]} cases, -j $jobs"

# cases.tsv in the intended order, whatever order the cases finished in.
results="$evidence/cases.tsv"
: > "$results"
for pattern in "${selected[@]}"; do
	name=$(printf '%s' "$pattern" | tr -c 'A-Za-z0-9_+-' '_')
	status=$(cat "$evidence/$name.status" 2>/dev/null || echo missing)
	printf '%s\t%s\t%s\n' "$pattern" "$name.log" "$status" >> "$results"
done

python3 - "$evidence" "$kc_root" "${#cases[@]}" "$filter" <<'PY'
import hashlib, json, os, re, subprocess, sys
evidence, root, intended, filt = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
env = os.environ
def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()
def git(*args):
    return subprocess.run(["git", "-C", root, *args], capture_output=True, text=True, check=True).stdout.strip()
def build(path):
    manifest = os.path.join(path, "manifest.json")
    return {"dir": path, "manifestSha256": sha(manifest), "manifest": json.load(open(manifest))}
problems = []
if filt:
    problems.append(f"filtered run ({filt!r}): not every intended case was invoked")
head = git("rev-parse", "HEAD")
dirty = git("status", "--porcelain")
if dirty:
    problems.append("the candidate tree has uncommitted changes")
bitcoind = sha(env["LEGACY_BITCOIND"])
if bitcoind != env["LEGACY_BITCOIND_SHA256"]:
    problems.append("bitcoind does not match LEGACY_BITCOIND_SHA256")
baseline = build(env["LEGACY_BASELINE_DIR"])
upgraded = build(env["LEGACY_UPGRADED_DIR"])
if baseline["manifest"].get("tbtcCommit") != "40a11d1dcdcf82d3962e430067cfe3f97be81483":
    problems.append("the baseline build is not tbtc-v2 40a11d1d")
if upgraded["manifest"].get("tbtcCommit") != env["LEGACY_UPGRADED_COMMIT"]:
    problems.append("the upgraded build is not LEGACY_UPGRADED_COMMIT")
cases = []
for line in open(os.path.join(evidence, "cases.tsv")):
    pattern, log, status = line.rstrip("\n").split("\t")
    if status == "missing":
        status = "-1"
    text = open(os.path.join(evidence, log)).read() if os.path.exists(os.path.join(evidence, log)) else ""
    skipped = "--- SKIP" in text
    leaf = pattern.split("$/^")[-1].strip("^$").replace("\\+", "+")
    ran = re.search(r"--- PASS: \S*" + re.escape(leaf), text) is not None
    result = "pass" if status == "0" and ran and not skipped else ("skip" if skipped else "fail")
    failure = [l.strip() for l in text.splitlines() if re.search(r"_test\.go:\d+: ", l)][:3]
    cases.append({"pattern": pattern, "log": log, "logSha256": sha(os.path.join(evidence, log)),
                  "exit": int(status), "result": result, "firstFailure": failure if result == "fail" else []})
passed = sum(c["result"] == "pass" for c in cases)
skips = sum(c["result"] == "skip" for c in cases)
if len(cases) != intended:
    problems.append(f"{len(cases)} of {intended} intended cases were invoked")
if passed != len(cases):
    problems.append(f"{len(cases) - passed} of {len(cases)} invoked cases did not pass")
tests = sorted(f for f in os.listdir(os.path.join(root, "pkg/tbtc"))
               if (f.startswith("legacy_") or f.startswith("drain_")) and f.endswith("_test.go"))
anvil_version = subprocess.run([env["LEGACY_ANVIL"], "--version"], capture_output=True, text=True).stdout.splitlines()[0]
manifest = {
    "suite": "keep-core remediation batch 2 (drain producer, tuxxy/keep-core#34)",
    "candidate": {"keepCoreHead": head, "clean": not dirty},
    "testFiles": {f: sha(os.path.join(root, "pkg/tbtc", f)) for f in tests},
    "runner": {"runSh": sha(os.path.join(root, "test/remediation-b2/run.sh")),
               "fixtures": sha(os.path.join(root, "test/legacy/fixtures/LegacyFixtures.sol"))},
    "dependencies": {
        "baseline": baseline, "upgraded": upgraded,
        "anvil": {"path": env["LEGACY_ANVIL"], "sha256": sha(env["LEGACY_ANVIL"]), "version": anvil_version},
        "bitcoind": {"path": env["LEGACY_BITCOIND"], "sha256": bitcoind},
        "go": subprocess.run(["go", "version"], capture_output=True, text=True).stdout.strip(),
        "tbtcRepo": env["LEGACY_TBTC_REPO"],
    },
    "run": {"jobs": int(env["RB2_JOBS"]), "wallSeconds": int(env["RB2_WALL"]), "gomaxprocsPerCase": 1},
    "intendedCases": intended, "invoked": len(cases), "passed": passed, "skipped": skips, "cases": cases,
    "problems": problems, "accepted": not problems,
}
with open(os.path.join(evidence, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=2)
print(f"manifest: {passed}/{len(cases)} passed of {intended} intended, {skips} skipped, accepted={manifest['accepted']}")
for p in problems:
    print("  " + p)
sys.exit(0 if manifest["accepted"] else 1)
PY
