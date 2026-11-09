#!/usr/bin/env bash
# Batch-17b acceptance run for tuxxy/keep-core#151 (the FROST epoch claim).
#   test/remediation-b17b/run.sh <evidence dir>
# Unit cases need nothing. The e2e cases need the pinned real worker:
# SNOWFALL_WORKER and SNOWFALL_WORKER_SHA256. A missing variable is a
# failure, not a skip. Every expected case must appear and pass.
# tuxxy/keep-core#154 runs separately: test/remediation-b17b/run-funding.sh.
set -euo pipefail
out=$1
mkdir -p "$out"
cd "$(dirname "$0")/../.."
for v in SNOWFALL_WORKER SNOWFALL_WORKER_SHA256; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done
head=$(git rev-parse HEAD)
clean=$([ -z "$(git status --porcelain)" ] && echo true || echo false)
start=$(date +%s)
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -timeout 5m -json ./pkg/tbtc \
  -run '^TestFrostNodeB17b' > "$out/tbtc.json" 2>&1
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
unit = ["TestFrostNodeB17bPreJournalRunErrorReleasesEpochClaim/" + c for c in
        ("prepare-parameters", "prepare-selection", "prepare-selection-unknown", "engine-client", "startup-restore/prepare-selection")]
unit += ["TestFrostNodeB17bJournalClaimedErrorNeverRerunsDKG"]
e2e = ["TestFrostNodeB17bSuccessfulDKGKeepsEpochClaim", "TestFrostNodeB17bKeyStoredErrorNeverRerunsDKG"]
expected = unit + e2e
missing = [k for k in expected if k not in cases]
failed = sorted(k for k in expected if k in cases and cases[k]["result"] != "pass")
m = {"head": head, "clean": clean, "wallSeconds": wall, "cases": cases, "failed": failed,
     "expected": expected, "unit": unit, "e2e": e2e, "missing": missing,
     "accepted": clean and not failed and not missing}
json.dump(m, open(os.path.join(out, "manifest.json"), "w"), indent=1, sort_keys=True)
for k in expected:
    print("%-80s %s" % (k, cases.get(k, {}).get("result", "MISSING")))
passed = sum(cases.get(k, {}).get("result") == "pass" for k in expected)
print(f"head {head} clean {clean} wall {wall}s: {passed}/{len(expected)} expected cases pass, {len(missing)} missing, accepted {m['accepted']}")
PY
