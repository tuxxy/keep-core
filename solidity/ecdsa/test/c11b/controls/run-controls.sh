#!/usr/bin/env bash
# C-11b removal controls (tuxxy/tbtc-v2#58, C-11b part).
#
# Usage (from solidity/ecdsa):
#   C11B_TBTC=<tbtc-v2 checkout> test/c11b/controls/run-controls.sh <control>|all
#
# Every control needs the implementation, deploy/18_deploy_frost_registry.ts
# (or C11B_REAL_BUILDER pointing at another builder). A control PASSES when
# its target case FAILS at the expected named assertion; a `.right.ts`
# positive control PASSES when the whole suite passes with it. Each control
# swaps the builder for its variant through C11B_BUILDER.
set -euo pipefail
cd "$(dirname "$0")/../../.."
: "${C11B_TBTC:?Set C11B_TBTC (see test/c11b/README.md)}"
mkdir -p .c11b-tmp/controls
export TMPDIR="$PWD/.c11b-tmp/controls"
export USE_EXTERNAL_DEPLOY=true C11B_RUN=1
unset TEST_USE_STUBS_ECDSA DEPLOY_FROST_REGISTRY FROST_REGISTRY_POLICY
T=test/c11b/Frost.RegistryDeploy.test.ts
DIR=test/c11b/controls

# name -> case grep|expected named assertion
spec() {
  case "$1" in
  c11b-1-registry-owner-deployer) echo "3 authority|authority: FrostWalletRegistry owner is the council" ;;
  c11b-1-admission-owner-deployer) echo "3 authority|authority: FrostAdmission owner is the council" ;;
  c11b-1-pool-owner-council) echo "3 authority|authority: FROST pool owner is the FROST registry" ;;
  c11b-1-chaosnet-owner-deployer) echo "3 authority|authority: FROST pool chaosnet owner is the chaosnetOwner named account" ;;
  c11b-2-requests-on) echo "2 disabled|disabled: registry requests off" ;;
  c11b-2-admits-operator) echo "2 disabled|disabled: no FROST admission granted" ;;
  c11b-3-no-beacon-step) echo "3 steps|steps: the legacy registry's beacon authorizes the FROST registry after the council steps" ;;
  c11b-4-wrong-bridge) echo "4 wiring\\]|wiring: registry wallet owner is the Bridge that owns the legacy WalletRegistry" ;;
  c11b-4-wrong-bridge@8) echo "8 line-up|line-up: BridgeGovernance.configureFrostRegistry accepts the bundle registry" ;;
  c11b-4-wrong-beacon) echo "4 wiring\\]|wiring: registry beacon is the legacy registry's beacon" ;;
  c11b-4-stake-source-token-staking) echo "4 wiring\\]|wiring: admission stake source is the legacy WalletRegistry" ;;
  c11b-4-swapped-periods) echo "4 wiring\\]|wiring: registry seedTimeout is the policy input" ;;
  c11b-4-legacy-pool) echo "1 deploys|registry uses the bundle pool" ;;
  c11b-4-reimbursement) echo "4 wiring\\]|wiring: the FROST registry has no ReimbursementPool authorization" ;;
  c11b-5-no-bridge-check) echo "5 inputs|inputs: refuses a Bridge that is not the legacy registry's wallet owner (a contract)" ;;
  c11b-6-mainnet-council) echo "6 per-network|per-network: registry owner is the governance named account" ;;
  c11b-7-transient-admission-owner) echo "7 window|window: no outsider call changes FROST state during the bundle" ;;
  c11b-9-swapped-addresses) echo "9 bytecode|bytecode: runtime code of validator equals its artifact" ;;
  c11b-10-legacy-mutation) echo "10 legacy|legacy: every legacy view, slot and authorization is unchanged" ;;
  c11b-10-window-toggle) echo "10 legacy|legacy: bundle and steps emit only the named events" ;;
  c11b-10-unprobed-beta) echo "10 legacy|legacy: bundle and steps emit only the named events" ;;
  c11b-10-redundant-off) echo "|PASSES" ;;
  *) return 1 ;;
  esac
}
ALL="c11b-1-registry-owner-deployer c11b-1-admission-owner-deployer c11b-1-pool-owner-council c11b-1-chaosnet-owner-deployer c11b-2-requests-on c11b-2-admits-operator c11b-3-no-beacon-step c11b-4-wrong-bridge c11b-4-wrong-bridge@8 c11b-4-wrong-beacon c11b-4-stake-source-token-staking c11b-4-swapped-periods c11b-4-legacy-pool c11b-4-reimbursement c11b-5-no-bridge-check c11b-6-mainnet-council c11b-7-transient-admission-owner c11b-9-swapped-addresses c11b-10-legacy-mutation c11b-10-window-toggle c11b-10-unprobed-beta c11b-10-redundant-off"

node test/c11b/check-tbtc-build.js "$C11B_TBTC" b0b02e2ccfcadaff05b15163120f503bc823cc82
npx hardhat compile >/dev/null

one() {
  local name="$1" file="${1%@*}" grep expected log
  IFS='|' read -r grep expected <<<"$(spec "$name")"
  log=".c11b-tmp/control-${name//@/-at-}.log"
  if [ "$expected" = PASSES ]; then
    C11B_BUILDER="$DIR/$file.right.ts" npx hardhat test --no-compile "$T" >"$log" 2>&1 || true
    if grep -q " 13 passing" "$log" && ! grep -q " failing" "$log"; then
      echo "PASS $name: the whole suite passes (positive control)"
    else
      echo "FAIL $name: a correct variant fails the suite (log $log)"
      return 1
    fi
    return 0
  fi
  C11B_BUILDER="$DIR/$file.wrong.ts" npx hardhat test --no-compile "$T" --grep "$grep" >"$log" 2>&1 || true
  if grep -q " failing" "$log" && grep -qF -- "$expected" "$log"; then
    echo "PASS $name: fails at \"$expected\""
  else
    echo "FAIL $name: expected a failure at \"$expected\" (log $log)"
    return 1
  fi
}

if [ "${1:-}" = all ]; then
  status=0
  for name in $ALL; do one "$name" || status=1; done
  exit $status
fi
spec "${1:-}" >/dev/null || { echo "usage: $0 <control>|all ($ALL)"; exit 2; }
one "$1"
