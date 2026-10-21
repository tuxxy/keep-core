#!/usr/bin/env bash
# C-10 local qualification. The tbtc-v2 worktree (the K-08 stack plus the
# C-10 commits) and the keep-core FROST contracts must already be compiled,
# with dependencies installed from their locks. The legacy reconciliation
# suite needs the compiled 76f7905 baseline (FROST_C04_LEGACY_BASELINE_BUILD)
# and a clean tbtc-v2 HEAD. Nothing here runs in CI.
set -euo pipefail
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
: "${FROST_K03_TBTC:?Set the matching tbtc-v2 worktree}"
: "${FROST_K03_ANVIL:?Set Anvil}"
: "${SNOWFALL_WORKER:?Set the real worker}"
: "${SNOWFALL_WORKER_SHA256:?Set its SHA-256}"
: "${FROST_K04_BITCOIND:?Set Bitcoin Core 29.0 bitcoind}"
: "${FROST_K04_BITCOIND_SHA256:?Set its reviewed SHA-256}"
: "${FROST_C04_LEGACY_BASELINE_BUILD:?Set the compiled 76f7905 baseline build}"
evidence_dir=${1:-$(mktemp -d)}
mkdir -p "$evidence_dir"
evidence_dir=$(cd "$evidence_dir" && pwd)
export GOMAXPROCS=${GOMAXPROCS:-8}
cd "$kc_root"
# The binding must equal a fresh generation from the compiled interface.
go run ./test/frost/generate-bridge "$FROST_K03_TBTC/solidity/build/contracts/frost" "$evidence_dir/bridge.go"
cmp pkg/chain/ethereum/frostbridgeabi/bridge.go "$evidence_dir/bridge.go"
go build -p 2 ./... > "$evidence_dir/build.log" 2>&1
pids=()
stage() { "$@" & pids+=($!); }
wait_all() { local status=0 pid; for pid in "${pids[@]}"; do wait "$pid" || status=1; done; pids=(); return $status; }

# Every stage covers what C-10 changed, nothing broader:
#  - the C-10 named test and the contract guard controls (the new evidence);
#  - the earlier named tests whose paths C-10 now gates: every K-06 test (the
#    planner sweeps only reserved deposits, and the fixture reserves), every
#    K-07 test (a drain reserves at its commitment), the K-05 lifecycle (a
#    direct sweep is charged at its proof) and the K-08 fraud test (sweeps
#    and defenses under the controller);
#  - the contract suites that execute changed code (listed below), with the
#    Bitcoin Core and upgrade suites in their own sequential stage;
#  - unit tests of the edited Go packages, race on the deposit planner and
#    the maintainer, and vet.
# ./pkg/tbtcpg: the legacy moving-funds proposal resolves the latest
# commitment, since a C-10 drain release lets a source commit again.
changed="./pkg/frost/deposit ./pkg/maintainer/spv ./pkg/chain/ethereum ./pkg/tbtcpg"
# Contract suites that execute code C-10 changed: the reveal, the sweep
# credit (plain, vault, resolution and gas-bounded routes), the gates, the
# drain and release, the dispatch chain, the storage layout and upgrade, and
# the legacy Wallets, MovingFunds, DepositSweep and Redemption libraries
# (the last two release a drain when their proof replaces its main). Suites
# that only fund through a changed helper in setup, or never reach C-10 code,
# are left out.
b=test/bridge
contracts="$b/Frost.CutoverExposure.test.ts $b/Frost.DepositAdmission.test.ts $b/Frost.DepositSweep.test.ts $b/Frost.DepositSweep.Guards.test.ts $b/Frost.DepositSweep.MainRequired.test.ts $b/Frost.DepositSweep.Gas.test.ts $b/Frost.FundingDispatch.test.ts $b/Frost.Movement.test.ts $b/Frost.Fraud.test.ts $b/Frost.Lifecycle.test.ts $b/Frost.SweepConflictResolution.test.ts $b/Frost.SweepConflictResolution.Gas.test.ts $b/Frost.Identity.test.ts $b/Bridge.MovingFunds.test.ts $b/Bridge.Wallets.test.ts $b/Bridge.Deposit.test.ts $b/Bridge.Redemption.test.ts"
core="$b/Frost.PublicFunding.test.ts $b/Frost.PublicVaultCallback.test.ts $b/Frost.PublicNegativeGuards.test.ts $b/Frost.PublicGasCap.test.ts $b/Frost.PublicMaximumBatch.test.ts $b/Frost.SweepConflictResolution.Core.test.ts $b/Frost.LegacyReconciliation.test.ts"
named='^(TestFrostSweepWithinExposureBudget|TestFrostDepositSweepAndCredit|TestDepositorRefundWithoutOperators|TestLateDepositRecovery|TestMainPurposeLockAndResolution|TestDepositTemplateCrossLanguage|TestCrossSchemeDrainAndSweep|TestMovementTargetStateRace|TestMovementCommittedFeeCap|TestNoTargetAndDustRecovery|TestCloseWithPendingObligations|TestFrostRedemptionLifecycle|TestFrostFraudPublicAdmissionDefenseTimeout)$'
stage test/frost/go-test-shards.sh ./pkg/frost/integration 5 "$evidence_dir/named.jsonl" -run "$named" -count=1 -timeout 60m
stage sh -c 'cd "$2/solidity" && python3 scripts/frost-c10-controls.py > "$1/controls.log" 2>&1 && python3 scripts/frost-c10-controls.py --self-test >> "$1/controls.log" 2>&1' _ "$evidence_dir" "$FROST_K03_TBTC"
stage sh -c 'go test -p 4 -json $2 -count=1 -timeout 30m > "$1/units.jsonl" 2>&1 && go test -p 4 -race -json ./pkg/frost/deposit ./pkg/maintainer/spv -count=1 -timeout 30m > "$1/units-race.jsonl" 2>&1 && go vet $2 ./pkg/frost/integration > "$1/vet.log" 2>&1' _ "$evidence_dir" "$changed"
wait_all
# The contract suites run after the Go and control stages, alone and in
# sequence: under that load Hardhat's node aborted and Bitcoin Core missed
# its 5 s RPC deadline (first run, 21 October 2026). The one-process
# batch needs more than 8 GB of heap.
sh -c 'cd "$2/solidity" && USE_EXTERNAL_DEPLOY=true TEST_USE_STUBS_TBTC=true NODE_OPTIONS=--max-old-space-size=16384 npx hardhat test --no-compile $3 > "$1/contracts.log" 2>&1' _ "$evidence_dir" "$FROST_K03_TBTC" "$contracts"
sh -c 'cd "$2/solidity" && for f in $3; do echo "== $f"; USE_EXTERNAL_DEPLOY=true TEST_USE_STUBS_TBTC=true NODE_OPTIONS=--max-old-space-size=8192 npx hardhat test --no-compile "$f" || exit 1; done > "$1/contracts-core.log" 2>&1' _ "$evidence_dir" "$FROST_K03_TBTC" "$core"
git diff --exit-code -- pkg/frost pkg/maintainer > /dev/null
printf 'C-10 evidence: %s\n' "$evidence_dir"
