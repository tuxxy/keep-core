#!/usr/bin/env bash
# K-05 local qualification. The tbtc-v2 worktree (C-04 stack plus K-05) and the
# keep-core FROST contracts must already be compiled; dependencies installed
# from their locks. Nothing here runs in CI.
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
go test -p 2 -json ./pkg/frost/integration -run '^(TestFrostRedemptionLifecycle|TestReservationTimeoutRace|TestLateSignatureQuarantine)$' -count=1 -timeout 45m > "$evidence_dir/named.jsonl" 2>&1
go test -p 2 -json ./pkg/frost/redemption ./pkg/tbtc -run '^TestZeroNetAndFeeBoundaries$' -count=1 > "$evidence_dir/zero-net.jsonl" 2>&1
go test -p 2 -json ./pkg/frost/... -count=1 -timeout 60m > "$evidence_dir/host-normal.jsonl" 2>&1
go test -p 2 -race -json ./pkg/frost/... -count=1 -timeout 90m > "$evidence_dir/host-race.jsonl" 2>&1
go test -p 2 -json ./pkg/bitcoin/... ./pkg/tbtc/... ./pkg/tbtcpg/... ./pkg/maintainer/... ./pkg/tecdsa/... ./pkg/chain/ethereum/... -count=1 -timeout 60m > "$evidence_dir/legacy-normal.jsonl" 2>&1
go vet ./pkg/frost/... ./pkg/bitcoin/... ./pkg/tbtc/... ./pkg/tbtcpg/... ./pkg/maintainer/... ./pkg/chain/ethereum ./cmd/... > "$evidence_dir/vet.log" 2>&1
(cd "$FROST_K03_TBTC/typescript" && ./node_modules/.bin/tsc --noEmit -p tsconfig.json && ./node_modules/.bin/mocha --exit --recursive 'test/**/*.test.ts') > "$evidence_dir/typescript.log" 2>&1
printf 'K-05 evidence: %s\n' "$evidence_dir"
