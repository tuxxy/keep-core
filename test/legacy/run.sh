#!/usr/bin/env bash
# Legacy-unchanged suite: a FROST-unconfigured client against the pre-FROST
# Bridge built from tbtc-v2 40a11d1d. Local only; nothing here runs in CI.
#
# Usage: run.sh <evidence directory>
# Required environment (no defaults):
#   LEGACY_BASELINE_DIR  output of test/legacy/build-baseline.sh
#   LEGACY_TBTC_REPO     tbtc-v2 git repository holding 40a11d1d
#   LEGACY_ANVIL         Anvil executable
#   LEGACY_UPGRADED_DIR  build-baseline.sh output for the upgraded Bridge
#   LEGACY_UPGRADED_COMMIT  the tbtc-v2 commit that build was made from
#   LEGACY_BITCOIND      Bitcoin Core 29.0 bitcoind
#   LEGACY_BITCOIND_SHA256  its reviewed SHA-256
#   LEGACY_HEAVY_LOCK    lock file serializing Core-backed runs (SHARED-RULES)
#
# Every case runs in its own process, so state a client keeps in package
# variables (a cached probe, say) cannot leak from one case into another.
# A skipped case fails the run: a skip is not a pass.
set -euo pipefail
umask 077
: "${LEGACY_BASELINE_DIR:?run test/legacy/build-baseline.sh first}"
: "${LEGACY_TBTC_REPO:?tbtc-v2 git repository required}"
: "${LEGACY_ANVIL:?Anvil required}"
: "${LEGACY_UPGRADED_DIR:?upgraded Bridge build required}"
: "${LEGACY_UPGRADED_COMMIT:?upgraded Bridge commit required}"
: "${LEGACY_BITCOIND:?Bitcoin Core bitcoind required}"
: "${LEGACY_BITCOIND_SHA256:?its SHA-256 required}"
: "${LEGACY_HEAVY_LOCK:?heavy lock file required}"
# Every case runs a Bitcoin Core node: hold the shared heavy lock throughout.
if [ -z "${LEGACY_HOLDS_LOCK:-}" ]; then
	LEGACY_HOLDS_LOCK=1 exec /usr/bin/lockf -k "$LEGACY_HEAVY_LOCK" bash "$0" "$@"
fi
evidence=${1:?evidence directory required}
mkdir -p "$evidence"
evidence=$(cd "$evidence" && pwd)
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$kc_root"

go test -c -o "$evidence/tbtc.test" ./pkg/tbtc

cases=(
	'^TestLegacyUnchanged_SurfaceOracle$'
	'^TestLegacyUnchanged_EmptyRevertEncodings$'
)
for encoding in code3-0x geth-32000-nodata; do
	cases+=("^TestLegacyUnchanged_UpgradedBridgeDrain$/^${encoding}$")
	for variant in "" "+transient-then-success"; do
		cases+=("^TestLegacyUnchanged_Startup$/^${encoding}${variant//+/\\+}$")
		cases+=("^TestLegacyUnchanged_MovingFunds$/^${encoding}${variant//+/\\+}$")
	done
done

status=0
for pattern in "${cases[@]}"; do
	name=$(printf '%s' "$pattern" | tr -c 'A-Za-z0-9_+-' '_')
	if ! GOMAXPROCS=${GOMAXPROCS:-8} "$evidence/tbtc.test" -test.v -test.count=1 \
		-test.timeout 5m -test.run "$pattern" > "$evidence/$name.log" 2>&1; then
		status=1
	fi
	if grep -q -- '--- SKIP' "$evidence/$name.log" || ! grep -q -- '^--- \(PASS\|FAIL\)' "$evidence/$name.log"; then
		echo "legacy-unchanged: $pattern skipped or did not run" >&2
		status=1
	fi
	grep -E -- '^\s*--- (PASS|FAIL)' "$evidence/$name.log" | sed "s|^|$name: |"
done
exit $status
