#!/usr/bin/env bash
# Batch-13 control run, in a scratch keep-core worktree only (detached, never
# pushed), with the LEGACY_* environment of test/remediation-b13/run.sh:
#   run-control.sh <scratch worktree> <patch or "none"> <log>
# Copies the batch-13 test files in, applies the patch to production code,
# runs the batch-13 cases, then reverses the patch and removes the copies.
set -euo pipefail
scratch=$1 patch=$2 log=$3
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../../.." && pwd)
files="pkg/tbtc/frost_redemption_b13_test.go pkg/tbtc/frost_redemption_b13_export_test.go"
for f in $files; do cp "$src/$f" "$scratch/$f"; done
cleanup() {
  if [ "$patch" != none ]; then git -C "$scratch" apply -R "$here/$patch.patch" || true; fi
  for f in $files; do rm -f "$scratch/$f"; done
}
trap cleanup EXIT
if [ "$patch" != none ]; then git -C "$scratch" apply "$here/$patch.patch"; fi
cd "$scratch"
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -parallel 5 -timeout 5m -v ./pkg/tbtc \
  -run '^TestFrostRedemptionLeaderMatchesFollower$' > "$log" 2>&1
status=$?
set -e
grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$log" || true
grep -E 'R-1 \(tuxxy|precondition|setup:' "$log" | sed -E 's/^ +//' | cut -c1-260 || true
echo "exit $status"
