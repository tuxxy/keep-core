#!/usr/bin/env bash
# Batch-6 acceptance run (tuxxy/keep-core#26 #38 #39, FROST leader rule, rule 5).
#   test/remediation-b6/run.sh <evidence dir>
# Needs SNOWFALL_WORKER and SNOWFALL_WORKER_SHA256 (the pinned worker), the
# LEGACY_* environment of test/legacy/run.sh with LEGACY_UPGRADED_DIR and
# LEGACY_UPGRADED_COMMIT, and keep-core's solidity/ecdsa/build (the FROST
# registry stack); see RM-tests-b6/env.sh. A missing variable is a failure,
# not a skip. Two cases at a time with GOMAXPROCS 2; each case runs three node
# processes at GOMAXPROCS 1 (8 Go cores in all). Every expected case must
# appear and pass.
set -euo pipefail
out=$1
mkdir -p "$out"
cd "$(dirname "$0")/../.."
: "${SNOWFALL_WORKER:?pinned worker required}" "${SNOWFALL_WORKER_SHA256:?pinned worker hash required}"
for v in LEGACY_BASELINE_DIR LEGACY_TBTC_REPO LEGACY_ANVIL LEGACY_BITCOIND LEGACY_BITCOIND_SHA256 LEGACY_UPGRADED_DIR LEGACY_UPGRADED_COMMIT; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done
[ -d solidity/ecdsa/build ] || { echo "solidity/ecdsa/build required" >&2; exit 2; }
head=$(git rev-parse HEAD)
clean=$([ -z "$(git status --porcelain)" ] && echo true || echo false)
start=$(date +%s)
set +e
GOMAXPROCS=2 go test -count=1 -parallel "${PARALLEL:-2}" -json ./pkg/tbtc \
  -run '^(TestFrostNodeDispatch_(DKG|IncomingSweepByLeader|RestoreAtStartup)|TestFrostNodeIsolation_(CrashPoints|FailedDKGDoesNotBlockSweep|ReservationOnOtherWallet)|TestFrostUnconfigured_NodeUnchanged)$' \
  > "$out/tbtc.json" 2>&1
set -e
wall=$(( $(date +%s) - start ))
python3 - "$out" "$head" "$clean" "$wall" <<'PY'
import json, sys, os
out, head, clean, wall = sys.argv[1], sys.argv[2], sys.argv[3] == "true", int(sys.argv[4])
cases = {}
for line in open(os.path.join(out, "tbtc.json")):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    if e.get("Test") and e.get("Action") in ("pass", "fail", "skip"):
        # a skip (missing environment) is never acceptance
        cases[e["Test"]] = {"result": e["Action"], "seconds": e.get("Elapsed")}
expected = [
    "TestFrostNodeDispatch_DKG/1,2,3",
    "TestFrostNodeDispatch_DKG/1,1,2",
    "TestFrostNodeDispatch_IncomingSweepByLeader",
    "TestFrostNodeDispatch_RestoreAtStartup",
    "TestFrostNodeIsolation_CrashPoints/candidate",
    "TestFrostNodeIsolation_CrashPoints/certified",
    "TestFrostNodeIsolation_CrashPoints/signed-before-proof",
    "TestFrostNodeIsolation_FailedDKGDoesNotBlockSweep",
    "TestFrostNodeIsolation_ReservationOnOtherWallet",
    "TestFrostUnconfigured_NodeUnchanged/code3-0x",
    "TestFrostUnconfigured_NodeUnchanged/geth-32000-nodata",
]
missing = [k for k in expected if k not in cases]
failed = sorted(k for k, v in cases.items() if v["result"] != "pass")
m = {"head": head, "clean": clean, "wallSeconds": wall, "cases": cases, "failed": failed,
     "expected": expected, "missing": missing,
     "accepted": clean and not failed and not missing}
json.dump(m, open(os.path.join(out, "manifest.json"), "w"), indent=1, sort_keys=True)
passed = sum(cases.get(k, {}).get("result") == "pass" for k in expected)
print(f"head {head} clean {clean} wall {wall}s: {passed}/{len(expected)} expected cases pass, {len(missing)} missing, accepted {m['accepted']}")
PY
