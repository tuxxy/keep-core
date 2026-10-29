#!/usr/bin/env bash
# Batch-3 acceptance run (tuxxy/keep-core#45 #50 #56 #61 #63 #64 #66 #70 #71).
#   test/remediation-b3/run.sh <evidence dir>
# Needs SNOWFALL_WORKER and SNOWFALL_WORKER_SHA256 (the pinned worker; see
# RM-tests-b3/env.sh). No Anvil, no Bitcoin Core. Cases run in parallel.
set -euo pipefail
out=$1
mkdir -p "$out"
cd "$(dirname "$0")/../.."
: "${SNOWFALL_WORKER:?pinned worker required}" "${SNOWFALL_WORKER_SHA256:?pinned worker hash required}"
head=$(git rev-parse HEAD)
clean=$([ -z "$(git status --porcelain)" ] && echo true || echo false)
start=$(date +%s)
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -parallel 8 -json \
  ./pkg/frost/signing -run '^(TestPossibleReleaseAfterGrantWithoutDone|TestNoPossibleReleaseBeforeTheWorker|TestBatch3OwningTestsRunUntagged)$' \
  > "$out/signing.json" 2>&1
s1=$?
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -parallel 8 -json \
  ./pkg/frost/movement -run '^TestIncoming(SweepSettles|OutputFinalBetweenTicks|LeaderCrashAtPublish|GrantPinsRecipe|PinnedRecipeWaitsWithoutChainEvidence|RecipeRetiredAfterMainSpent|SettlesWithOperatorDown|LeaderRotates|LeaderShareBySeat_Tentative)$' \
  > "$out/movement.json" 2>&1
s2=$?
set -e
wall=$(( $(date +%s) - start ))
python3 - "$out" "$head" "$clean" "$wall" <<'PY'
import json, sys, os
out, head, clean, wall = sys.argv[1], sys.argv[2], sys.argv[3] == "true", int(sys.argv[4])
cases = {}
for f in ("signing.json", "movement.json"):
    for line in open(os.path.join(out, f)):
        try:
            e = json.loads(line)
        except ValueError:
            continue
        if e.get("Test") and e.get("Action") in ("pass", "fail", "skip"):
            cases[e["Package"].rsplit("/", 1)[-1] + "/" + e["Test"]] = {"result": e["Action"], "seconds": e.get("Elapsed")}
failed = sorted(k for k, v in cases.items() if v["result"] != "pass")
m = {"head": head, "clean": clean, "wallSeconds": wall, "cases": cases, "failed": failed,
     "accepted": clean and not failed and any(cases)}
json.dump(m, open(os.path.join(out, "manifest.json"), "w"), indent=1, sort_keys=True)
print(f"head {head} clean {clean} wall {wall}s: {sum(v['result']=='pass' for v in cases.values())}/{len(cases)} pass, accepted {m['accepted']}")
PY
