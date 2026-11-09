#!/usr/bin/env bash
# Batch-17b run for tuxxy/keep-core#154: TestReadinessCannotFundEarly on the
# K-03 environment against one tbtc-v2 build.
#   test/remediation-b17b/run-funding.sh <evidence dir>
# The K-03 environment picks its tbtc-v2 build from FROST_K03_TBTC: an
# absolute path to a tbtc-v2 checkout whose solidity/build holds compiled
# artifacts (test/frost/deploy.cjs deploys Bridge and the FROST libraries
# from <FROST_K03_TBTC>/solidity/build/contracts/...). keep-core's own
# registry contracts come from this checkout's solidity/ecdsa/build. Both
# must be compiled first (corepack yarn hardhat compile). Also needs
# FROST_K03_ANVIL and the pinned worker (SNOWFALL_WORKER, _SHA256).
set -euo pipefail
out=$1
mkdir -p "$out"
cd "$(dirname "$0")/../.."
for v in FROST_K03_TBTC FROST_K03_ANVIL SNOWFALL_WORKER SNOWFALL_WORKER_SHA256; do
  [ -n "${!v:-}" ] || { echo "$v required" >&2; exit 2; }
done
[ -d "$FROST_K03_TBTC/solidity/build/contracts" ] || { echo "FROST_K03_TBTC has no compiled build" >&2; exit 2; }
[ -d solidity/ecdsa/build/contracts ] || { echo "solidity/ecdsa is not compiled" >&2; exit 2; }
tb=$(git -C "$FROST_K03_TBTC" rev-parse HEAD)
dirty=$(git -C "$FROST_K03_TBTC" status --porcelain -- solidity/contracts | wc -l | tr -d ' ')
echo "tbtc-v2 build $tb (dirty contract files: $dirty)" | tee "$out/build.txt"
set +e
GOMAXPROCS=${GOMAXPROCS:-8} go test -count=1 -timeout 5m -v ./pkg/frost/integration \
  -run '^TestReadinessCannotFundEarly$' > "$out/funding.log" 2>&1
status=$?
set -e
grep -E -- '^(--- |\s+failures_test.go:|ok|FAIL)' "$out/funding.log" || true
if grep -q -- '--- SKIP' "$out/funding.log"; then echo "skipped: not acceptance" >&2; exit 2; fi
echo "exit $status"
exit $status
