#!/usr/bin/env bash
# K-06 local qualification. The tbtc-v2 worktree (C-04 stack plus K-05 and
# K-06) and the keep-core FROST contracts must already be compiled, with
# dependencies installed from their locks. Nothing here runs in CI.
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
# Integration tests are independent (each starts its own Anvil, Core and
# operators), so they run as parallel shards (go-test-shards.sh), and
# independent stages overlap. Every stage must pass.
shards=${FROST_SHARDS:-6}
units=$(go list ./pkg/frost/... | grep -v '/pkg/frost/integration$')
pids=()
stage() { "$@" & pids+=($!); }
wait_all() { local status=0 pid; for pid in "${pids[@]}"; do wait "$pid" || status=1; done; pids=(); return $status; }

# 1. Named tests beside the guard regressions (which use throwaway copies).
stage test/frost/go-test-shards.sh ./pkg/frost/integration 5 "$evidence_dir/named.jsonl" -run '^(TestFrostDepositSweepAndCredit|TestDepositorRefundWithoutOperators|TestDepositTemplateCrossLanguage|TestLateDepositRecovery|TestMainPurposeLockAndResolution)$' -count=1 -timeout 45m
stage sh -c 'python3 test/frost/k06-controls.py > "$1/controls.log" 2>&1 && python3 test/frost/k06-controls.py --self-test >> "$1/controls.log" 2>&1' _ "$evidence_dir"
wait_all
git diff --exit-code -- pkg/frost pkg/maintainer > /dev/null

# 2. The host suite.
go test -p 4 -json $units -count=1 -timeout 60m > "$evidence_dir/host-normal.jsonl" 2>&1
test/frost/go-test-shards.sh ./pkg/frost/integration "$shards" "$evidence_dir/host-normal-integration.jsonl" -parallel 4 -count=1 -timeout 60m

# 3. The host suite under race, beside vet and TypeScript. Race slows the
# operators, and DKG deadlines are blocks mined on a wall-clock tick, so the
# race integration shards run with less parallelism.
stage sh -c 'go test -p 4 -race -json $2 -count=1 -timeout 90m > "$1/host-race.jsonl" 2>&1 && test/frost/go-test-shards.sh ./pkg/frost/integration 3 "$1/host-race-integration.jsonl" -race -parallel 2 -count=1 -timeout 90m' _ "$evidence_dir" "$units"
stage sh -c 'go vet ./pkg/frost/... ./pkg/bitcoin/... ./pkg/tbtc/... ./pkg/tbtcpg/... ./pkg/maintainer/... ./pkg/chain/ethereum ./cmd/... > "$1/vet.log" 2>&1' _ "$evidence_dir"
stage sh -c 'cd "$2/typescript" && (./node_modules/.bin/tsc --noEmit -p tsconfig.json && ./node_modules/.bin/mocha --exit --recursive "test/**/*.test.ts") > "$1/typescript.log" 2>&1' _ "$evidence_dir" "$FROST_K03_TBTC"
wait_all

# 4. Legacy suites alone: the tss-lib DKG tests have fixed CPU time budgets.
go test -p 2 -json ./pkg/bitcoin/... ./pkg/tbtc/... ./pkg/tbtcpg/... ./pkg/maintainer/... ./pkg/tecdsa/... ./pkg/chain/ethereum/... -count=1 -timeout 60m > "$evidence_dir/legacy-normal.jsonl" 2>&1
printf 'K-06 evidence: %s\n' "$evidence_dir"
