#!/usr/bin/env bash
# Applies one batch-6 control patch (a frostNodeStart binding) in a scratch
# keep-core worktree, runs the cases it concerns, and removes it again. Never
# in the candidate tree.
#   test/remediation-b6/controls/run-control.sh <scratch> <patch> <log> <-run regex>
set -euo pipefail
scratch=$1 patch=$2 log=$3 filter=$4
here=$(cd "$(dirname "$0")/../../.." && pwd)
[ "$(cd "$scratch" && pwd)" != "$here" ] || { echo "refusing to run a control in the candidate tree" >&2; exit 2; }
for f in pkg/tbtc/frost_node_b6_seam_test.go pkg/tbtc/frost_node_b6_world_test.go pkg/tbtc/frost_node_b6_export_test.go \
  pkg/tbtc/frost_node_b6_test.go pkg/tbtc/frost_node_b6_isolation_test.go pkg/tbtc/frost_node_b6_unconfigured_test.go; do
  cp "$here/$f" "$scratch/$f"
done
cd "$scratch"
git apply "$here/test/remediation-b6/controls/$patch"
set +e
GOMAXPROCS=2 go test ./pkg/tbtc -count=1 -parallel "${PARALLEL:-2}" -run "$filter" -v > "$log" 2>&1
set -e
git apply -R "$here/test/remediation-b6/controls/$patch"
grep -E '^\s*--- |frost_node_b6_[a-z]*_?test.go:[0-9]+: (#|D2|rule|setup)' "$log" | cut -c1-260
