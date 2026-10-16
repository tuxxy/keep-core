#!/usr/bin/env bash
# K-04 local qualification. K-03 contracts must already be compiled.
set -euo pipefail
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
: "${FROST_K03_TBTC:?Set the matching tbtc-v2 worktree}"
: "${FROST_K03_ANVIL:?Set Anvil}"
: "${SNOWFALL_WORKER:?Set the real worker}"
: "${SNOWFALL_WORKER_SHA256:?Set its SHA-256}"
: "${SNOWFALL_CODEC:?Set the independent codec}"
: "${FROST_K04_BITCOIND:?Set Bitcoin Core 29.0 bitcoind}"
: "${FROST_K04_BITCOIND_SHA256:?Set its reviewed SHA-256}"
evidence_dir=${1:-$(mktemp -d)}
mkdir -p "$evidence_dir"
evidence_dir=$(cd "$evidence_dir" && pwd)
export GOMAXPROCS=${GOMAXPROCS:-8}
cd "$kc_root"
node --experimental-strip-types test/frost/check-k04-intent.ts > "$evidence_dir/intent-typescript.log" 2>&1
go build -p 2 ./... > "$evidence_dir/build.log" 2>&1
go test -p 2 -json ./pkg/frost/... -count=1 > "$evidence_dir/host-normal.jsonl" 2>&1
go test -p 2 -race -json ./pkg/frost/... -count=1 > "$evidence_dir/host-race.jsonl" 2>&1
go test -p 2 -json ./pkg/bitcoin/... ./pkg/tbtc/... ./pkg/tecdsa/... ./pkg/chain/ethereum -count=1 > "$evidence_dir/legacy-normal.jsonl" 2>&1
go test -p 2 -race -json ./pkg/bitcoin ./pkg/tbtc -count=1 > "$evidence_dir/routing-race.jsonl" 2>&1
go vet ./pkg/frost/... ./pkg/bitcoin/... ./pkg/tbtc/... ./pkg/chain/ethereum > "$evidence_dir/vet.log" 2>&1
printf 'K-04 evidence: %s\n' "$evidence_dir"
