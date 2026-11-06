#!/usr/bin/env bash
# C-11b acceptance run (tuxxy/tbtc-v2#58, C-11b part). From solidity/ecdsa:
#   C11B_TBTC=<tbtc-v2 checkout at 39412825, compiled> test/c11b/run.sh
# Real WalletRegistry (no ECDSA stubs, EIP-170 enforced by the network as
# well as by the bytecode case). Temporary files stay in .c11b-tmp/.
set -euo pipefail
cd "$(dirname "$0")/../.."
: "${C11B_TBTC:?Set C11B_TBTC to a tbtc-v2 checkout of 39412825 (see test/c11b/README.md)}"
tree=$(git -C "$C11B_TBTC" rev-parse 'HEAD^{tree}')
[ "$tree" = b0b02e2ccfcadaff05b15163120f503bc823cc82 ] || { echo "C11B_TBTC is not at the pinned tbtc-v2 tree (got $tree)"; exit 2; }
[ -f "$C11B_TBTC/solidity/build/contracts/bridge/Bridge.sol/Bridge.json" ] || { echo "compile $C11B_TBTC/solidity first (npx hardhat compile)"; exit 2; }
# The artifacts must come from that tree (review N4): build-info sources equal
# the tree's blobs, and each artifact equals its build-info output.
node test/c11b/check-tbtc-build.js "$C11B_TBTC" "$tree"
mkdir -p .c11b-tmp
export TMPDIR="$PWD/.c11b-tmp"
export USE_EXTERNAL_DEPLOY=true C11B_RUN=1
unset TEST_USE_STUBS_ECDSA DEPLOY_FROST_REGISTRY FROST_REGISTRY_POLICY
npx hardhat compile >/dev/null
exec npx hardhat test --no-compile test/c11b/Frost.RegistryDeploy.test.ts "$@"
