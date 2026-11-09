#!/usr/bin/env bash
# Remediation batch 17a D34 controls (README.md here; tuxxy/keep-core#30 #35
# #36, tuxxy/tbtc-v2#19). Applies each run.sh patch to a SCRATCH checkout of
# the candidate head, runs ./test/k09/runner (stub stages, a few seconds),
# records the log and reverts test/k09/run.sh. Never point it at a working
# checkout.
#
# usage: run-controls.sh <scratch-keep-core-dir> <log-dir> [patch ...]
set -euo pipefail
scratch=$(cd "$1" && pwd)
logs=$(mkdir -p "$2" && cd "$2" && pwd)
shift 2
here=$(cd "$(dirname "$0")" && pwd)
controls=("$@")
if [ ${#controls[@]} -eq 0 ]; then
  controls=(d34-fix.patch d34-wrong-tbtc-test-only.patch d34-wrong-kill-on-failure-only.patch
    d34-wrong-snowfall-record-only.patch d34-wrong-inventory-logs-only.patch
    d34-wrong-env-denylist.patch d34-wrong-env-stages-only.patch)
fi
cd "$scratch"
test -z "$(git status --porcelain -- test/k09/run.sh)" || { echo "scratch run.sh is dirty" >&2; exit 1; }
for c in "${controls[@]}"; do
  echo "== $c"
  git apply "$here/$c"
  go test -count=1 -v ./test/k09/runner/ > "$logs/${c%.patch}.log" 2>&1 || true
  grep -E -- '^\s*--- (PASS|FAIL)|^(ok|FAIL)\s|runner_test.go:[0-9]+:' "$logs/${c%.patch}.log" | cut -c1-200 | sort -u || true
  git checkout -- test/k09/run.sh
done
