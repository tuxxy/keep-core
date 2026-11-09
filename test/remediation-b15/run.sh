#!/usr/bin/env bash
# Batch-15 acceptance run (tuxxy/keep-core#141, D30).
#   test/remediation-b15/run.sh <evidence dir>
# Needs the LEGACY_* environment of test/legacy/run.sh (Anvil, the 40a11d1d
# baseline and upgraded tbtc-v2 builds, Bitcoin Core). A missing variable is
# a failure, not a skip. Every expected case must appear and pass.
set -euo pipefail
out=$1
mkdir -p "$out"
cd "$(dirname "$0")/../.."
for v in LEGACY_BASELINE_DIR LEGACY_TBTC_REPO LEGACY_ANVIL LEGACY_BITCOIND LEGACY_BITCOIND_SHA256 LEGACY_UPGRADED_DIR LEGACY_UPGRADED_COMMIT; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done
head=$(git rev-parse HEAD)
clean=$([ -z "$(git status --porcelain)" ] && echo true || echo false)
start=$(date +%s)
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -parallel 5 -timeout 5m -json \
  ./pkg/tbtc -run '^TestFrostMovementTerminatedSource(Proposed|Refused)$' \
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
        cases["tbtc/" + e["Test"]] = {"result": e["Action"], "seconds": e.get("Elapsed")}
expected = [
    "tbtc/TestFrostMovementTerminatedSourceProposed/moving-funds",
    "tbtc/TestFrostMovementTerminatedSourceProposed/terminated",
    "tbtc/TestFrostMovementTerminatedSourceProposed/terminated-committed",
    "tbtc/TestFrostMovementTerminatedSourceRefused/live",
    "tbtc/TestFrostMovementTerminatedSourceRefused/terminated-dust",
]
missing = [k for k in expected if k not in cases]
failed = sorted(k for k, v in cases.items() if v["result"] != "pass")
m = {"head": head, "clean": clean, "wallSeconds": wall, "cases": cases, "failed": failed,
     "expected": expected, "missing": missing,
     "upgradedCommit": os.environ["LEGACY_UPGRADED_COMMIT"],
     "accepted": clean and not failed and not missing}
json.dump(m, open(os.path.join(out, "manifest.json"), "w"), indent=1, sort_keys=True)
passed = sum(cases.get(k, {}).get("result") == "pass" for k in expected)
print(f"head {head} clean {clean} wall {wall}s: {passed}/{len(expected)} expected cases pass, {len(missing)} missing, accepted {m['accepted']}")
PY
