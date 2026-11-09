#!/usr/bin/env bash
# Batch-11 control run, in a scratch keep-core worktree only (detached, never
# pushed), with the LEGACY_* environment of test/remediation-b11/run.sh:
#   run-control.sh <scratch worktree> <patch or "none"> <log>
# Copies the batch-11 test file in, applies the patch to production code,
# runs the batch-11 cases, then reverses the patch and removes the copy.
set -euo pipefail
scratch=$1 patch=$2 log=$3
here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../../.." && pwd)
test_file=pkg/tbtc/frost_incoming_b11_test.go
cp "$src/$test_file" "$scratch/$test_file"
cleanup() {
  if [ "$patch" != none ]; then git -C "$scratch" apply -R "$here/$patch.patch" || true; fi
  rm -f "$scratch/$test_file"
}
trap cleanup EXIT
if [ "$patch" != none ]; then git -C "$scratch" apply "$here/$patch.patch"; fi
cd "$scratch"
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -parallel 4 -v ./pkg/tbtc \
  -run '^TestFrostIncomingSweepEscalated(Proposed|FollowerValidation)$' > "$log" 2>&1
status=$?
set -e
grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$log" || true
echo "exit $status"
