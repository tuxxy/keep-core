#!/usr/bin/env bash
# Batch-16 control run, in a scratch keep-core worktree only (detached, never
# pushed), with the LEGACY_* environment of test/remediation-b16/run.sh:
#   run-control.sh <scratch worktree> <patch or "none"> <log>
# Copies the batch-16 test file in, applies the patch to production code,
# runs the batch-16 cases, then reverses the patch and removes the copy.
# The contract control (README) is "none" with LEGACY_UPGRADED_DIR and
# LEGACY_UPGRADED_COMMIT pointing at the scratch 1-based Bridge build.
set -euo pipefail
scratch=$1 patch=$2 log=$3
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../../.." && pwd)
test_file=pkg/tbtc/frost_movement_seat_b16_test.go
cp "$src/$test_file" "$scratch/$test_file"
cleanup() {
  if [ "$patch" != none ]; then git -C "$scratch" apply -R "$here/$patch.patch" || true; fi
  rm -f "$scratch/$test_file"
}
trap cleanup EXIT
if [ "$patch" != none ]; then git -C "$scratch" apply "$here/$patch.patch"; fi
cd "$scratch"
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -parallel 6 -timeout 5m -v ./pkg/tbtc \
  -run '^TestFrostMovementLeaderOwnCommitment$' > "$log" 2>&1
status=$?
set -e
grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$log" || true
grep -m3 -E 'keep-core#146: ' "$log" | cut -c1-400 || true
echo "exit $status"
