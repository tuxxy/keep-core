#!/usr/bin/env bash
# K-07 local qualification. The tbtc-v2 worktree (C-04 stack plus K-05, K-06,
# C-06, C-08 and C-09) and the keep-core FROST contracts must already be
# compiled, with dependencies installed from their locks. Nothing here runs
# in CI.
set -euo pipefail
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
: "${FROST_K03_TBTC:?Set the matching tbtc-v2 worktree}"
: "${FROST_K03_ANVIL:?Set Anvil}"
: "${SNOWFALL_WORKER:?Set the real worker}"
: "${SNOWFALL_WORKER_SHA256:?Set its SHA-256}"
: "${FROST_K04_BITCOIND:?Set Bitcoin Core 29.0 bitcoind}"
: "${FROST_K04_BITCOIND_SHA256:?Set its reviewed SHA-256}"
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

# Every stage covers what K-07 changed, nothing broader:
#  - the K-07 named tests and their guard regressions (the new evidence);
#  - unit tests of every Go package K-07 edited, with race on the packages
#    that gained concurrent code;
#  - the K-06 named tests, whose deposit decoder and fixture K-07 changed;
#  - the C-06/C-08/C-09 contract suites and the legacy suites of the
#    contracts they edit (MovingFunds, Wallets, Fraud);
#  - vet of the edited packages.
# The TypeScript SDK, the tss-lib DKG suites and K-03 to K-05 integration are
# untouched by K-07 and are not rerun here.
changed="./pkg/frost/signing ./pkg/frost/deposit ./pkg/frost/redemption ./pkg/frost/movement ./pkg/maintainer/spv ./pkg/chain/ethereum"
stage test/frost/go-test-shards.sh ./pkg/frost/integration 1 "$evidence_dir/named.jsonl" -run '^(TestCrossSchemeDrainAndSweep|TestMovementTargetStateRace|TestMovementCommittedFeeCap|TestNoTargetAndDustRecovery|TestCloseWithPendingObligations)$' -count=1 -timeout 60m
stage sh -c 'python3 test/frost/k07-controls.py > "$1/controls.log" 2>&1 && python3 test/frost/k07-controls.py --self-test >> "$1/controls.log" 2>&1' _ "$evidence_dir"
stage sh -c 'cd "$2/solidity" && USE_EXTERNAL_DEPLOY=true TEST_USE_STUBS_TBTC=true NODE_OPTIONS=--max-old-space-size=8192 npx hardhat test --no-compile test/bridge/Frost.Movement.test.ts test/bridge/Frost.Fraud.test.ts test/bridge/Frost.Lifecycle.test.ts test/bridge/Bridge.MovingFunds.test.ts test/bridge/Bridge.Wallets.test.ts test/bridge/Bridge.Frauds.test.ts > "$1/contracts.log" 2>&1' _ "$evidence_dir" "$FROST_K03_TBTC"
stage sh -c 'go test -p 4 -json $2 -count=1 -timeout 30m > "$1/units.jsonl" 2>&1 && go test -p 4 -race -json ./pkg/frost/movement ./pkg/frost/signing ./pkg/frost/deposit -count=1 -timeout 30m > "$1/units-race.jsonl" 2>&1 && go test -json ./pkg/tbtc -run "MovingFunds" -count=1 -timeout 30m > "$1/tbtc-moving-funds.jsonl" 2>&1 && go vet $2 ./pkg/tbtc ./pkg/frost/integration > "$1/vet.log" 2>&1' _ "$evidence_dir" "$changed"
wait_all
git diff --exit-code -- pkg/frost pkg/maintainer > /dev/null

# K-06 regression: its named tests on the changed decoder and fixture.
test/frost/go-test-shards.sh ./pkg/frost/integration 5 "$evidence_dir/k06-named.jsonl" -run '^(TestFrostDepositSweepAndCredit|TestDepositorRefundWithoutOperators|TestDepositTemplateCrossLanguage|TestLateDepositRecovery|TestMainPurposeLockAndResolution)$' -count=1 -timeout 45m
printf 'K-07 evidence: %s\n' "$evidence_dir"
