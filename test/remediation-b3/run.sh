#!/usr/bin/env bash
# Batch-3 acceptance run (tuxxy/keep-core#45 #50 #56 #61 #63 #64 #66 #70 #71).
#   test/remediation-b3/run.sh <evidence dir>
# Needs SNOWFALL_WORKER and SNOWFALL_WORKER_SHA256 (the pinned worker) and the
# LEGACY_* environment of test/legacy/run.sh (Anvil, both tbtc-v2 builds,
# Bitcoin Core); see RM-tests-b3/env.sh. A missing variable is a failure, not
# a skip. Cases run in parallel, four at a time per package, GOMAXPROCS 4 per
# package (review R7). Every expected case must appear and pass.
set -euo pipefail
out=$1
mkdir -p "$out"
cd "$(dirname "$0")/../.."
: "${SNOWFALL_WORKER:?pinned worker required}" "${SNOWFALL_WORKER_SHA256:?pinned worker hash required}"
for v in LEGACY_BASELINE_DIR LEGACY_TBTC_REPO LEGACY_ANVIL LEGACY_BITCOIND LEGACY_BITCOIND_SHA256 LEGACY_UPGRADED_DIR LEGACY_UPGRADED_COMMIT; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done
head=$(git rev-parse HEAD)
clean=$([ -z "$(git status --porcelain)" ] && echo true || echo false)
start=$(date +%s)
set +e
GOMAXPROCS=${GOMAXPROCS:-4} go test -count=1 -parallel 4 -json \
  ./pkg/frost/signing -run '^(TestPossibleReleaseAfterGrantWithoutDone|TestNoPossibleReleaseBeforeTheWorker|TestBatch3OwningTestsRunUntagged)$' \
  > "$out/signing.json" 2>&1
s1=$?
GOMAXPROCS=${GOMAXPROCS:-4} go test -count=1 -parallel 4 -json \
  ./pkg/frost/movement -run '^(TestIncomingSweep(Settles|SignsWithOperatorDown|NextWindowAfter(Failed|Aborted)Window|AfterMainSpent|AfterMainSpentNoRestart)|TestResolveRecordsOnlyTheProvenSweep)$' \
  > "$out/movement.json" 2>&1 &
p2=$!
GOMAXPROCS=${GOMAXPROCS:-4} go test -count=1 -parallel 4 -json \
  ./pkg/tbtc -run '^TestFrostIncomingSweep(ProposedEveryWindow|FollowerValidation|DivergentLocalFees)$' \
  > "$out/tbtc.json" 2>&1 &
p3=$!
wait $p2 $p3
set -e
wall=$(( $(date +%s) - start ))
python3 - "$out" "$head" "$clean" "$wall" <<'PY'
import json, sys, os
out, head, clean, wall = sys.argv[1], sys.argv[2], sys.argv[3] == "true", int(sys.argv[4])
cases = {}
for f in ("signing.json", "movement.json", "tbtc.json"):
    for line in open(os.path.join(out, f)):
        try:
            e = json.loads(line)
        except ValueError:
            continue
        if e.get("Test") and e.get("Action") in ("pass", "fail", "skip"):
            # a skip (missing environment) is never acceptance
            cases[e["Package"].rsplit("/", 1)[-1] + "/" + e["Test"]] = {"result": e["Action"], "seconds": e.get("Elapsed")}
# Review R7: the intended cases, by name; a case that stops matching the
# -run filters is missing, never silently dropped.
expected = [
    "signing/TestPossibleReleaseAfterGrantWithoutDone/mainless",
    "signing/TestPossibleReleaseAfterGrantWithoutDone/with-main",
    "signing/TestPossibleReleaseAfterGrantWithoutDone/mainless-after-restart",
    "signing/TestPossibleReleaseAfterGrantWithoutDone/with-main-after-restart",
    "signing/TestNoPossibleReleaseBeforeTheWorker/mainless",
    "signing/TestNoPossibleReleaseBeforeTheWorker/with-main",
    "signing/TestBatch3OwningTestsRunUntagged",
    "movement/TestIncomingSweepSettles/mainless",
    "movement/TestIncomingSweepSettles/with-main",
    "movement/TestIncomingSweepSignsWithOperatorDown/operator-0-down",
    "movement/TestIncomingSweepSignsWithOperatorDown/operator-1-down",
    "movement/TestIncomingSweepSignsWithOperatorDown/operator-2-down",
    "movement/TestIncomingSweepNextWindowAfterFailedWindow",
    "movement/TestIncomingSweepNextWindowAfterAbortedWindow",
    "movement/TestIncomingSweepAfterMainSpent",
    "movement/TestIncomingSweepAfterMainSpentNoRestart",
    "movement/TestResolveRecordsOnlyTheProvenSweep",
    "tbtc/TestFrostIncomingSweepProposedEveryWindow",
    "tbtc/TestFrostIncomingSweepFollowerValidation",
    "tbtc/TestFrostIncomingSweepDivergentLocalFees",
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
