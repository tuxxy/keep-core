#!/usr/bin/env bash
# Runs keep-core's unit or end-to-end suite.
#
#   test/suites/run.sh unit  [go test flags]   every test not in e2e.txt
#   test/suites/run.sh e2e   [go test flags]   every e2e.txt test (helpers excluded)
#   test/suites/run.sh check                   every e2e.txt entry names a real test
#
# The split is by name, not by file or build tag: frozen tests are checked by
# path, and batch-3 tests must run untagged. e2e.txt lists each end-to-end
# test with what it needs; every other test is a unit test.
#
# unit needs no external program, chain or pinned build, and fails if any of
# its tests skips (a test that skips without its tools belongs in e2e.txt).
# e2e needs the environment its tests name (see test/frost/README.md and the
# runbooks); pkg/frost/integration and pkg/tbtc run as shards.
#
# Packages that are acceptance checkers run by their own runbooks are in
# neither suite: test/k09/evidence (test/k09/run.sh) and test/k09/controls
# (probe records, test/k09/controls/run-probe.sh). Their batch tests run
# through test/remediation-b17c/controls/run-controls.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
manifest=test/suites/e2e.txt
excluded='^github.com/keep-network/keep-core/test/k09/(evidence|controls)$'
mode=${1:?usage: run.sh unit|e2e|check [go test flags]}
shift

# names <package> [all|tests]: the package's e2e.txt names as one alternation.
names() {
  awk -v p="$1" -v w="${2:-all}" '
    /^#/ || NF < 2 { next }
    $1 == p && (w == "all" || $3 != "helper") { printf "%s%s", sep, $2; sep = "|" }
  ' "$manifest"
}
packages() { awk '!/^#/ && NF >= 2 { print $1 }' "$manifest" | sort -u; }

work=$(mktemp -d)
pids=()
trap 'rm -rf "$work"' EXIT

# report <json files...>: per-package result lines, failed and skipped tests.
report() {
  local status=0
  grep -h -o -E '"Output":"(ok  |FAIL)\\t[^"]*' "$@" | sed 's/"Output":"//; s/\\t/  /g; s/\\n$//' | sort -u
  if grep -h -q '"Action":"fail"' "$@"; then
    echo "failed tests:"
    grep -h -o '"Action":"fail","Package":"[^"]*","Test":"[^"]*"' "$@" | sed 's/.*Package":"\([^"]*\)","Test":"\([^"]*\)"/  \1 \2/' | sort -u
    status=1
  fi
  if [ "$mode" = unit ] && grep -h -q '"Action":"skip","Package":"[^"]*","Test"' "$@"; then
    echo "skipped tests (add them to $manifest, or give them what they need):"
    grep -h -o '"Action":"skip","Package":"[^"]*","Test":"[^"]*"' "$@" | sed 's/.*Package":"\([^"]*\)","Test":"\([^"]*\)"/  \1 \2/' | sort -u
    status=1
  fi
  return $status
}

case "$mode" in
check)
  status=0
  for p in $(packages); do
    go test -list . "./$p" > "$work/list" 2>&1 || { cat "$work/list"; exit 1; }
    for t in $(names "$p" | tr '|' ' '); do
      grep -qx "$t" "$work/list" || { echo "$manifest: no test $t in $p"; status=1; }
    done
  done
  [ $status = 0 ] && echo "$manifest: $(grep -c '^[^#]' "$manifest") entries, all present"
  exit $status
  ;;
unit)
  # Packages without e2e tests run together; each package with e2e tests runs
  # alone with its own -skip, because a name can repeat across packages.
  listed=$(packages | sed 's|^|github.com/keep-network/keep-core/|')
  plain=$(go list ./... | grep -v -E "$excluded" | grep -v -x -F "$listed")
  go test -json "$@" $plain > "$work/plain.json" 2>&1 &
  pids+=($!)
  for p in $(packages); do
    go test -json -skip "^($(names "$p"))\$" "$@" "./$p" > "$work/$(echo "$p" | tr / _).json" 2>&1 &
    pids+=($!)
  done
  for pid in "${pids[@]}"; do wait "$pid"; done
  report "$work"/*.json
  ;;
e2e)
  # Small packages together, then each sharded package on its own: the B6
  # node-process tests in pkg/tbtc miss their windows when they share the
  # machine with pkg/frost/integration's shards.
  for p in $(packages); do
    regex="^($(names "$p" tests))\$"
    [ "$regex" = '^()$' ] && continue
    case "$p" in pkg/frost/integration | pkg/tbtc) continue ;; esac
    go test -json -run "$regex" "$@" "./$p" > "$work/$(echo "$p" | tr / _).json" 2>&1 &
    pids+=($!)
  done
  for pid in "${pids[@]}"; do wait "$pid"; done
  test/frost/go-test-shards.sh ./pkg/frost/integration 5 "$work/pkg_frost_integration.json" \
    -run "^($(names pkg/frost/integration tests))\$" -parallel 4 "$@"
  test/frost/go-test-shards.sh ./pkg/tbtc 6 "$work/pkg_tbtc.json" -run "^($(names pkg/tbtc tests))\$" "$@"
  report "$work"/*.json
  ;;
*)
  echo "usage: run.sh unit|e2e|check [go test flags]" >&2
  exit 2
  ;;
esac
