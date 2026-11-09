#!/usr/bin/env bash
# Remediation batch 17c controls (README.md here; tuxxy/keep-core#60, #33 and
# batch 17a fix-review note N7). Applies each patch to a SCRATCH checkout of
# the candidate head, runs only the batch 17c test it targets (each a few
# seconds; no chain, node, K-09 stage or model), records the log and reverts
# the patch. Never point it at a working checkout.
#
# usage: run-controls.sh <scratch-keep-core-dir> <log-dir> [patch ...]
# With no patch named, it runs "base" (no patch) and every patch here.
set -euo pipefail
scratch=$(cd "$1" && pwd)
logs=$(mkdir -p "$2" && cd "$2" && pwd)
shift 2
here=$(cd "$(dirname "$0")" && pwd)
controls=("$@")
if [ ${#controls[@]} -eq 0 ]; then
  controls=(base)
  for p in "$here"/*.patch; do controls+=("$(basename "$p")"); done
fi
cd "$scratch"
test -z "$(git status --porcelain)" || { echo "scratch checkout is dirty" >&2; exit 1; }
suite() {
  case "$1" in
    n7-*) go test -count=1 -v -run '^TestB17cEvidenceManifestRecords$' ./test/k09/evidence/ ;;
    33-*) (cd pkg/frost/verification && python3 -B -m unittest -v test_freshness.FreshnessTests.test_k09_run_mapping_names_exactly_the_runner_stages) ;;
    60-*) go test -count=1 -v -run '^TestB17c' ./test/k09/controls/ ;;
    base) go test -count=1 -v -run '^TestB17c' ./test/k09/evidence/ ./test/k09/controls/
          (cd pkg/frost/verification && python3 -B -m unittest -v test_freshness.FreshnessTests.test_k09_run_mapping_names_exactly_the_runner_stages) ;;
  esac
}
for c in "${controls[@]}"; do
  echo "== $c"
  [ "$c" = base ] || git apply "$here/$c"
  suite "${c%.patch}" > "$logs/${c%.patch}.log" 2>&1 || true
  grep -E -- '^\s*--- (PASS|FAIL)|^(ok|FAIL|OK|FAILED)\b|_test\.go:[0-9]+:|^AssertionError' "$logs/${c%.patch}.log" | cut -c1-220 | sort -u || true
  git checkout -- .
  git clean -fdq -- test/k09/controls/run-probe.sh
done
