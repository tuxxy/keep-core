#!/usr/bin/env bash
# Batch-17b control run (tuxxy/keep-core#151), in a scratch keep-core
# worktree only (detached at 1e3e6ea6, never pushed):
#   run-control.sh <scratch worktree> <patch or "none"> <log>
# Copies the two batch-17b test files in, applies the patch to production
# code, runs the batch-17b cases (the e2e ones need SNOWFALL_WORKER and
# SNOWFALL_WORKER_SHA256), then reverses the patch and removes the copies.
set -euo pipefail
scratch=$1 patch=$2 log=$3
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../../.." && pwd)
files="pkg/tbtc/frost_node_b17b_claim_test.go pkg/tbtc/frost_node_b17b_success_test.go"
for f in $files; do cp "$src/$f" "$scratch/$f"; done
cleanup() {
  if [ "$patch" != none ]; then git -C "$scratch" apply -R "$here/$patch.patch" || true; fi
  for f in $files; do rm -f "$scratch/$f"; done
}
trap cleanup EXIT
if [ "$patch" != none ]; then git -C "$scratch" apply "$here/$patch.patch"; fi
cd "$scratch"
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -timeout 5m -v ./pkg/tbtc \
  -run '^TestFrostNodeB17b' > "$log" 2>&1
status=$?
set -e
grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$log" || true
grep -m3 -E 'tuxxy/keep-core#151\): ' "$log" | cut -c1-260 || true
echo "exit $status"
