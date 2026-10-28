#!/usr/bin/env bash
# Builds the pre-FROST tbtc-v2 baseline the legacy-unchanged suite deploys.
#
# The baseline is tbtc-v2 main at 40a11d1d (owner decision D1). It is taken
# from git with `git archive`, never from a prebuilt directory, and compiled
# with the repository's own Hardhat configuration, so the deployed Bridge is
# byte-for-byte what that commit builds. The labelled fixture contracts in
# test/legacy/fixtures are compiled separately with the same solc version and
# never enter the baseline build.
#
# Usage: build-baseline.sh <tbtc-v2 git repository> <new output directory> [commit]
# The optional commit builds an upgraded (FROST) Bridge instead, for the
# upgraded-Bridge half of tuxxy/keep-core#73; it must use the same yarn.lock.
# Required environment (no defaults):
#   LEGACY_NODE_MODULES  solidity/node_modules installed from the same
#                        yarn.lock as 40a11d1d (checked below by lock hash)
#   LEGACY_NODE_LOCK     the yarn.lock that node_modules was installed from
#   LEGACY_SOLC          solc 0.8.17 executable for the fixtures
set -euo pipefail
umask 077

readonly BASELINE=40a11d1dcdcf82d3962e430067cfe3f97be81483

repository=${1:?tbtc-v2 git repository required}
output=${2:?output directory required}
PIN=$(git -C "$repository" rev-parse --verify "${3:-$BASELINE}^{commit}")
: "${LEGACY_NODE_MODULES:?node_modules installed from the 40a11d1d lock required}"
: "${LEGACY_NODE_LOCK:?the yarn.lock LEGACY_NODE_MODULES was installed from required}"
: "${LEGACY_SOLC:?solc 0.8.17 required}"

here=$(cd "$(dirname "$0")" && pwd)

if [ -e "$output" ] && [ -n "$(ls -A "$output")" ]; then
	echo "output directory $output is not empty" >&2
	exit 1
fi


# The dependency tree must come from the built commit's own lock.
baseline_lock=$(git -C "$repository" show "$PIN:solidity/yarn.lock" | shasum -a 256 | cut -d' ' -f1)
given_lock=$(shasum -a 256 < "$LEGACY_NODE_LOCK" | cut -d' ' -f1)
[ "$baseline_lock" = "$given_lock" ] || { echo "node_modules lock differs from $PIN" >&2; exit 1; }

"$LEGACY_SOLC" --version | grep -q '0\.8\.17+' || { echo "fixtures need solc 0.8.17" >&2; exit 1; }

mkdir -p "$output"
output=$(cd "$output" && pwd)
git -C "$repository" archive --format=tar "$PIN" solidity | tar -x -C "$output"
ln -s "$LEGACY_NODE_MODULES" "$output/solidity/node_modules"

(
	cd "$output/solidity"
	NODE_OPTIONS=--max-old-space-size=8192 npx hardhat compile > "$output/compile.log" 2>&1
)

mkdir -p "$output/fixtures"
"$LEGACY_SOLC" --optimize --optimize-runs 1000 --combined-json abi,bin \
	"$here/fixtures/LegacyFixtures.sol" > "$output/fixtures/LegacyFixtures.json"

# The manifest the Go suite checks before deploying anything.
{
	printf '{\n  "tbtcCommit": "%s",\n  "yarnLockSha256": "%s",\n' "$PIN" "$baseline_lock"
	printf '  "fixturesSha256": "%s",\n' \
		"$(shasum -a 256 < "$here/fixtures/LegacyFixtures.sol" | cut -d' ' -f1)"
	printf '  "solc": "%s"\n}\n' "$("$LEGACY_SOLC" --version | tail -1)"
} > "$output/manifest.json"

echo "baseline $PIN built in $output"
