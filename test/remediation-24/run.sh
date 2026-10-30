#!/usr/bin/env bash
# tuxxy/tbtc-v2#24, keep-core side: the K-06 single-main rule (D13 refinement,
# tux 2026-10-30). Local only; nothing here runs in CI.
#
# Usage:
#   run.sh <evidence dir>             acceptance at the current tree
#   run.sh --controls <evidence dir>  fix and wrong controls, each applied in a
#                                     throwaway worktree (never this tree)
#
# Acceptance runs every intended case: the pkg/frost/deposit unit cases
# (fixture chain, real planner/authority/sweeper) and the three amended K-06
# integration tests (real contracts on Anvil, Bitcoin Core regtest and the
# pinned worker). The integration cases need the K-06 environment
# (FROST_K03_TBTC, FROST_K03_ANVIL, SNOWFALL_WORKER, SNOWFALL_WORKER_SHA256,
# FROST_K04_BITCOIND, FROST_K04_BITCOIND_SHA256; see test/frost/K06.md);
# without it they skip and the manifest is not accepted. manifest.json is
# "accepted": true only if every intended case ran and passed, zero skips,
# at a clean head.
set -euo pipefail
umask 077
mode=accept
if [ "${1:-}" = "--controls" ]; then
	mode=controls
	shift
fi
evidence=${1:?evidence directory required}
mkdir -p "$evidence"
evidence=$(cd "$evidence" && pwd)
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
controls_dir="$kc_root/test/remediation-24/controls"
cd "$kc_root"
export GOMAXPROCS=${GOMAXPROCS:-8}

unit_run='^(TestPlannerRefusesOrdinarySweepWhileReserved|TestAuthorityRefusesNoMainPlanWhileReserved|TestResolutionRouteSurvivesSingleMain|TestSingleMainBoundaries|TestPlannerMainRule|TestAuthorityRequiresUnreservedMain)$'
unit_cases=()
for s in Reserved TimeoutPending Quarantined reservation_without_main; do
	unit_cases+=("TestPlannerRefusesOrdinarySweepWhileReserved/$s" "TestAuthorityRefusesNoMainPlanWhileReserved/$s")
done
for s in Reserved TimeoutPending Quarantined; do
	unit_cases+=("TestResolutionRouteSurvivesSingleMain/$s")
done
unit_cases+=("TestSingleMainBoundaries/no_main,_no_reservation:_deposit-only"
	"TestSingleMainBoundaries/unreserved_main_is_included"
	"TestSingleMainBoundaries/deposit_waits_for_the_reservation,_then_sweeps_with_the_new_main"
	"TestPlannerMainRule/reserved_main_refuses_an_ordinary_sweep"
	"TestAuthorityRequiresUnreservedMain")
integration_run='^(TestFrostDepositSweepAndCredit|TestLateDepositRecovery|TestMainPurposeLockAndResolution)$'
integration_cases=(TestFrostDepositSweepAndCredit TestLateDepositRecovery TestMainPurposeLockAndResolution)

if [ "$mode" = controls ]; then
	scratch=$(mktemp -d "${TMPDIR:-/tmp}/rm24-controls.XXXXXX")
	tree="$scratch/keep-core"
	git worktree add -q --detach "$tree" HEAD
	trap 'git -C "$kc_root" worktree remove --force "$tree"; rm -rf "$scratch"' EXIT
	# go.mod replaces the Snowfall client with ../snowfall.
	ln -s "$(cd "$kc_root/../snowfall" && pwd)" "$scratch/snowfall"
	: > "$evidence/controls.tsv"
	for patch in "$controls_dir"/*.patch; do
		name=$(basename "$patch" .patch)
		git -C "$tree" checkout -q -- .
		git -C "$tree" apply "$patch"
		status=0
		(cd "$tree" && go test -count=1 -v -run "$unit_run" ./pkg/frost/deposit/) > "$evidence/$name.log" 2>&1 || status=$?
		failures=$(grep -E '^\s+single_main_test.go:[0-9]+: |^\s+deposit_test.go:[0-9]+: ' "$evidence/$name.log" | sed 's/^ *//' | cut -c1-160 | sort -u | tr '\n' '|' || true)
		printf '%s\t%s\t%s\n' "$name" "$status" "$failures" >> "$evidence/controls.tsv"
		printf '%s: exit %s %s\n' "$name" "$status" "$failures"
	done
	exit 0
fi

go test -count=1 -v -run "$unit_run" ./pkg/frost/deposit/ > "$evidence/unit.log" 2>&1 || true
go test -count=1 ./pkg/frost/deposit/... ./pkg/frost/signing/... ./pkg/maintainer/spv/... > "$evidence/packages.log" 2>&1 || true
go test -count=1 -v -timeout 14m -run "$integration_run" ./pkg/frost/integration/ > "$evidence/integration.log" 2>&1 || true

python3 - "$evidence" "$kc_root" "${unit_cases[@]}" -- "${integration_cases[@]}" <<'PY'
import hashlib, json, os, re, subprocess, sys
evidence, root = sys.argv[1], sys.argv[2]
rest = sys.argv[3:]
split = rest.index("--")
unit, integration = rest[:split], rest[split + 1:]
def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()
def git(*args):
    return subprocess.run(["git", "-C", root, *args], capture_output=True, text=True, check=True).stdout.strip()
def result(text, name):
    if re.search(r"--- SKIP: " + re.escape(name) + r" ", text):
        return "skip"
    if re.search(r"--- PASS: " + re.escape(name) + r" ", text):
        return "pass"
    if re.search(r"--- FAIL: " + re.escape(name) + r" ", text):
        return "fail"
    return "missing"
problems = []
if git("status", "--porcelain"):
    problems.append("the candidate tree has uncommitted changes")
cases = []
for log, names in (("unit.log", unit), ("integration.log", integration)):
    text = open(os.path.join(evidence, log)).read()
    for n in names:
        cases.append({"case": n, "log": log, "result": result(text, n)})
packages = open(os.path.join(evidence, "packages.log")).read()
packages_ok = "FAIL" not in packages and len(re.findall(r"^ok\s", packages, re.M)) >= 3
if not packages_ok:
    problems.append("go test ./pkg/frost/deposit/... ./pkg/frost/signing/... ./pkg/maintainer/spv/... did not pass")
passed = sum(c["result"] == "pass" for c in cases)
skipped = sum(c["result"] == "skip" for c in cases)
if passed != len(cases):
    problems.append(f"{len(cases) - passed} of {len(cases)} intended cases did not pass ({skipped} skipped)")
tests = ["pkg/frost/deposit/single_main_test.go", "pkg/frost/deposit/deposit_test.go", "pkg/frost/integration/deposits_test.go"]
manifest = {
    "suite": "tuxxy/tbtc-v2#24 keep-core single main (D13 refinement)",
    "candidate": {"keepCoreHead": git("rev-parse", "HEAD"), "clean": not git("status", "--porcelain")},
    "testFiles": {f: sha(os.path.join(root, f)) for f in tests},
    "runSh": sha(os.path.join(root, "test/remediation-24/run.sh")),
    "go": subprocess.run(["go", "version"], capture_output=True, text=True).stdout.strip(),
    "intendedCases": len(cases), "passed": passed, "skipped": skipped, "cases": cases,
    "packagesPassed": packages_ok, "problems": problems, "accepted": not problems,
}
with open(os.path.join(evidence, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=2)
print(f"manifest: {passed}/{len(cases)} passed, {skipped} skipped, accepted={manifest['accepted']}")
for p in problems:
    print("  " + p)
sys.exit(0 if manifest["accepted"] else 1)
PY
