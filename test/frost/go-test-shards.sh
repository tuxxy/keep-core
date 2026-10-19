#!/usr/bin/env bash
# Run the tests of one Go package as parallel shards, one go test process per
# shard, and merge their JSON output. Every integration test brings up its own
# Anvil, Bitcoin Core and operator processes on free ports, so shards are
# independent. Long tests each get their own shard in addition to <shards>.
#
# usage: go-test-shards.sh <package> <shards> <output.jsonl> [-run REGEX] [go test flags...]
#
# Without -run, every test the package lists is run. The script fails if any
# shard fails or if a selected test did not report a pass.
set -euo pipefail
package=$1
shards=$2
output=$3
shift 3
filter='.*'
if [ "${1:-}" = "-run" ]; then
  filter=$2
  shift 2
fi
# Tests known to dominate the package; each runs alone.
long='^(TestV04C04ProjectionReplay|TestV04C04WeakenedExpectationsFail|TestReservationTimeoutRace|TestCrossSchemeDrainAndSweep|TestMovementTargetStateRace|TestMovementCommittedFeeCap|TestNoTargetAndDustRecovery|TestCloseWithPendingObligations|TestFrostFraudPublicAdmissionDefenseTimeout|TestHeartbeatDomainAndDefense|TestInactivityMemberRouting)$'

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go test -list "$filter" "$package" | grep '^Test' > "$work/tests" || true
if [ ! -s "$work/tests" ]; then
  echo "no tests selected in $package" >&2
  exit 1
fi
# Long tests first, each in its own shard; the rest round-robin.
grep -E "$long" "$work/tests" > "$work/long" || true
grep -vE "$long" "$work/tests" > "$work/short" || true
index=0
while read -r test; do
  echo "$test" > "$work/shard-$index"
  index=$((index + 1))
done < "$work/long"
first_short=$index
count=$(wc -l < "$work/short" | tr -d ' ')
# <shards> is for the short tests; each long test has its own shard on top.
remaining=$shards
[ "$remaining" -gt "$count" ] && remaining=$count
i=0
while read -r test; do
  echo "$test" >> "$work/shard-$((first_short + i % remaining))"
  i=$((i + 1))
done < "$work/short"

pids=()
for file in "$work"/shard-*; do
  regex="^($(paste -sd'|' "$file"))\$"
  go test -json "$@" -run "$regex" "$package" > "$file.jsonl" 2>&1 &
  pids+=($!)
done
status=0
for pid in "${pids[@]}"; do
  wait "$pid" || status=1
done
cat "$work"/shard-*.jsonl > "$output"
# Every selected test must have passed in some shard.
while read -r test; do
  if ! grep -q "\"Action\":\"pass\",\"Package\":\"[^\"]*\",\"Test\":\"$test\"," "$output"; then
    echo "test $test did not pass" >&2
    status=1
  fi
done < "$work/tests"
exit $status
