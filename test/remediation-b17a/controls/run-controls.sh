#!/usr/bin/env bash
# Remediation batch 17a keep-core controls (README.md here; tuxxy/keep-core#58,
# #53). Applies each wrong implementation to a SCRATCH checkout of the
# candidate head, runs ./test/remediation-b17a, records the log and reverts.
# Never point it at a working checkout: it runs `git checkout -- .` there.
#
# usage: run-controls.sh <scratch-keep-core-dir> <log-dir> [control ...]
set -euo pipefail
scratch=$(cd "$1" && pwd)
logs=$(mkdir -p "$2" && cd "$2" && pwd)
shift 2
here=$(cd "$(dirname "$0")" && pwd)
controls=("$@")
if [ ${#controls[@]} -eq 0 ]; then
  controls=(b17a-58-wrong-func-hook.patch b17a-58-wrong-interface-hook.patch b17a-58-wrong-local-node-command.patch)
fi
cd "$scratch"
test -z "$(git status --porcelain -- pkg cmd config)" || { echo "scratch checkout is dirty" >&2; exit 1; }
for c in "${controls[@]}"; do
  echo "== $c"
  git apply "$here/$c"
  GOMAXPROCS=8 go test -count=1 -v ./test/remediation-b17a/ > "$logs/${c%.patch}.log" 2>&1 || true
  grep -E -- '^(--- (PASS|FAIL)|ok|FAIL)|constraints_test.go:[0-9]+:' "$logs/${c%.patch}.log" | cut -c1-240 || true
  git checkout -- pkg cmd config
done
