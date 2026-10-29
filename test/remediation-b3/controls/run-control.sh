#!/usr/bin/env bash
# Runs the batch-3 incoming tests against one CONTROL entry point in a scratch
# worktree. Never in the candidate tree.
#   test/remediation-b3/controls/run-control.sh <scratch keep-core worktree> <mode> <log> [-run regex]
set -euo pipefail
scratch=$1 mode=$2 log=$3
filter=${4:-'^TestIncoming(SweepSettles|OutputFinalBetweenTicks|LeaderCrashAtPublish|GrantPinsRecipe|PinnedRecipeWaitsWithoutChainEvidence|RecipeRetiredAfterMainSpent|SettlesWithOperatorDown|LeaderRotates|LeaderShareBySeat_Tentative)$'}
here=$(cd "$(dirname "$0")/../../.." && pwd)
[ "$(cd "$scratch" && pwd)" != "$here" ] || { echo "refusing to run a control in the candidate tree" >&2; exit 2; }
cp "$here"/pkg/frost/movement/incoming_b3_test.go "$here"/pkg/frost/movement/incoming_world_b3_test.go "$scratch"/pkg/frost/movement/
sed "s/const controlMode = \"MODE\"/const controlMode = \"$mode\"/" "$here"/test/remediation-b3/controls/control.go.txt > "$scratch"/pkg/frost/movement/zz_control_b3.go
cd "$scratch"
set +e
go test ./pkg/frost/movement/ -count=1 -parallel 8 -run "$filter" -v > "$log" 2>&1
code=$?
set -e
rm -f pkg/frost/movement/zz_control_b3.go
grep -E '^\s*--- |_b3_test.go:[0-9]+:' "$log" | cut -c1-260
exit 0
