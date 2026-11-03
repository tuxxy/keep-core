#!/usr/bin/env bash
# Applies one control patch in a scratch keep-core worktree, runs the batch-3
# tests it concerns, and restores the scratch tree. Never in the candidate.
#   test/remediation-b3/controls/run-control.sh <scratch> <patch> <log> <package> <-run regex>
set -euo pipefail
scratch=$1 patch=$2 log=$3 pkg=$4 filter=$5
here=$(cd "$(dirname "$0")/../../.." && pwd)
[ "$(cd "$scratch" && pwd)" != "$here" ] || { echo "refusing to run a control in the candidate tree" >&2; exit 2; }
for f in pkg/frost/signing/possible_release_b3_test.go pkg/frost/signing/untagged_b3_test.go \
  pkg/frost/movement/incoming_b3_test.go pkg/frost/movement/incoming_world_b3_test.go pkg/tbtc/frost_incoming_b3_test.go; do
  cp "$here/$f" "$scratch/$f"
done
cd "$scratch"
git apply "$here/test/remediation-b3/controls/$patch"
set +e
go test "./$pkg" -count=1 -parallel "${PARALLEL:-4}" -run "$filter" -v > "$log" 2>&1
set -e
git apply -R "$here/test/remediation-b3/controls/$patch"
grep -E '^\s*--- |_b3_test.go:[0-9]+:' "$log" | cut -c1-260
