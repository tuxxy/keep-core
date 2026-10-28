#!/usr/bin/env bash
# Legacy-unchanged suite: a FROST-unconfigured client against the pre-FROST
# Bridge built from tbtc-v2 40a11d1d. Local only; nothing here runs in CI.
#
# This runner is the acceptance gate for tuxxy/keep-core#73 and the shared
# legacy-unchanged suite. A plain `go test` without the LEGACY_* environment
# skips every case and proves nothing. Acceptance is the manifest.json this
# script writes, with "accepted": true, at the candidate head.
#
# Usage: run.sh <evidence directory>
# Required environment (no defaults):
#   LEGACY_BASELINE_DIR  output of test/legacy/build-baseline.sh
#   LEGACY_TBTC_REPO     tbtc-v2 git repository holding 40a11d1d
#   LEGACY_ANVIL         Anvil executable
#   LEGACY_UPGRADED_DIR  build-baseline.sh output for the upgraded Bridge
#   LEGACY_UPGRADED_COMMIT  the tbtc-v2 commit that build was made from
#   LEGACY_BITCOIND      Bitcoin Core 29.0 bitcoind
#   LEGACY_BITCOIND_SHA256  its reviewed SHA-256
#   LEGACY_HEAVY_LOCK    lock file serializing Core-backed runs (SHARED-RULES)
#
# Every case runs in its own process, so state a client keeps in package
# variables (a cached probe, say) cannot leak from one case into another.
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
# Every case runs a Bitcoin Core node: hold the shared heavy lock throughout.
if [ -z "${LEGACY_HOLDS_LOCK:-}" ]; then
	LEGACY_HOLDS_LOCK=1 exec /usr/bin/lockf -k "$LEGACY_HEAVY_LOCK" bash "$0" "$@"
fi
evidence=${1:?evidence directory required}
mkdir -p "$evidence"
evidence=$(cd "$evidence" && pwd)
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$kc_root"

go test -c -o "$evidence/tbtc.test" ./pkg/tbtc

# The intended cases. The manifest requires every one of them to be present
# and passing; there is no other way to be accepted.
cases=(
	'^TestLegacyUnchanged_SurfaceOracle$'
	'^TestLegacyUnchanged_EmptyRevertEncodings$'
)
for encoding in code3-0x geth-32000-nodata; do
	cases+=("^TestLegacyUnchanged_UpgradedBridgeDrain$/^${encoding}$")
	cases+=("^TestLegacyUnchanged_UpgradedBridgeDrainTransient$/^${encoding}\\+transient-then-success$")
	for variant in "" "+transient-then-success"; do
		cases+=("^TestLegacyUnchanged_Startup$/^${encoding}${variant//+/\\+}$")
		cases+=("^TestLegacyUnchanged_MovingFunds$/^${encoding}${variant//+/\\+}$")
	done
done
for state in MovingFunds Closing Closed Terminated; do
	cases+=("^TestLegacyUnchanged_NonLiveTargets$/^${state}$")
done

results="$evidence/cases.tsv"
: > "$results"
for pattern in "${cases[@]}"; do
	name=$(printf '%s' "$pattern" | tr -c 'A-Za-z0-9_+-' '_')
	status=0
	GOMAXPROCS=${GOMAXPROCS:-8} "$evidence/tbtc.test" -test.v -test.count=1 \
		-test.timeout 5m -test.run "$pattern" > "$evidence/$name.log" 2>&1 || status=$?
	printf '%s\t%s\t%s\n' "$pattern" "$name.log" "$status" >> "$results"
	grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$evidence/$name.log" | sed "s|^|$name: |" || true
done

python3 - "$evidence" "$kc_root" <<'EOF'
import hashlib, json, os, re, subprocess, sys
evidence, root = sys.argv[1], sys.argv[2]
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
    text = open(os.path.join(evidence, log)).read()
    skipped = "--- SKIP" in text
    leaf = pattern.split("$/^")[-1].strip("^$").replace("\\+", "+")
    ran = re.search(r"--- PASS: \S*" + re.escape(leaf), text) is not None
    result = "pass" if status == "0" and ran and not skipped else ("skip" if skipped else "fail")
    cases.append({"pattern": pattern, "log": log, "logSha256": sha(os.path.join(evidence, log)),
                  "exit": int(status), "result": result})
passed = sum(c["result"] == "pass" for c in cases)
skips = sum(c["result"] == "skip" for c in cases)
if passed != len(cases):
    problems.append(f"{len(cases) - passed} of {len(cases)} intended cases did not pass")
tests = sorted(f for f in os.listdir(os.path.join(root, "pkg/tbtc")) if f.startswith("legacy_") and f.endswith("_test.go"))
anvil_version = subprocess.run([env["LEGACY_ANVIL"], "--version"], capture_output=True, text=True).stdout.splitlines()[0]
manifest = {
    "suite": "keep-core legacy-unchanged (remediation batches 1 and 1b)",
    "candidate": {"keepCoreHead": head, "clean": not dirty},
    "testFiles": {f: sha(os.path.join(root, "pkg/tbtc", f)) for f in tests},
    "runner": {"runSh": sha(os.path.join(root, "test/legacy/run.sh")),
               "fixtures": sha(os.path.join(root, "test/legacy/fixtures/LegacyFixtures.sol"))},
    "dependencies": {
        "baseline": baseline, "upgraded": upgraded,
        "anvil": {"path": env["LEGACY_ANVIL"], "sha256": sha(env["LEGACY_ANVIL"]), "version": anvil_version},
        "bitcoind": {"path": env["LEGACY_BITCOIND"], "sha256": bitcoind},
        "go": subprocess.run(["go", "version"], capture_output=True, text=True).stdout.strip(),
        "tbtcRepo": env["LEGACY_TBTC_REPO"],
    },
    "intendedCases": len(cases), "passed": passed, "skipped": skips, "cases": cases,
    "problems": problems, "accepted": not problems,
}
with open(os.path.join(evidence, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=2)
print(f"manifest: {passed}/{len(cases)} passed, {skips} skipped, accepted={manifest['accepted']}")
for p in problems:
    print("  " + p)
sys.exit(0 if manifest["accepted"] else 1)
EOF
