#!/usr/bin/env bash
# Remediation batch 17a D35 controls (README.md here; tuxxy/keep-core#37).
# Applies each patch to a SCRATCH checkout of the candidate head, runs the
# two #37 tests (loopback listeners only), records the log and reverts pkg/.
# Never point it at a working checkout.
#
# usage: run-controls.sh <scratch-keep-core-dir> <log-dir> [patch ...]
set -euo pipefail
scratch=$(cd "$1" && pwd)
logs=$(mkdir -p "$2" && cd "$2" && pwd)
shift 2
here=$(cd "$(dirname "$0")" && pwd)
controls=("$@")
if [ ${#controls[@]} -eq 0 ]; then
  controls=(d35-fix.patch d35-wrong-port-zero.patch d35-wrong-field-type.patch d35-wrong-world-reverted.patch
    d35-wrong-helper-loopback.patch r-d35-wrong-always-loopback.patch
    r-r4-fix-helper.patch r-r4-fix-nested-error.patch r-r4-fix-nil-check.patch)
fi
cd "$scratch"
test -z "$(git status --porcelain -- pkg)" || { echo "scratch pkg/ is dirty" >&2; exit 1; }
for c in "${controls[@]}"; do
  echo "== $c"
  git apply "$here/$c"
  go vet ./pkg/net/libp2p/ > "$logs/${c%.patch}.vet.log" 2>&1 || echo "vet failed (see ${c%.patch}.vet.log)"
  go test -count=1 -v -run '^TestB17a' ./pkg/net/libp2p/ ./pkg/tbtc/ > "$logs/${c%.patch}.log" 2>&1 || true
  grep -E -- '^--- (PASS|FAIL)|^(ok|FAIL)\s|_b17a_[a-z_]*test.go:[0-9]+:' "$logs/${c%.patch}.log" | cut -c1-220 || true
  git checkout -- pkg
done
