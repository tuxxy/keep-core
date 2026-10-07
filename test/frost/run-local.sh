#!/usr/bin/env bash
# Local K-03 qualification. Dependencies must already be installed from locks.
set -euo pipefail
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
: "${FROST_K03_TBTC:?Set the matching tbtc-v2 worktree}"
: "${SNOWFALL_WORKER:?Set the compatible real worker}"
: "${SNOWFALL_WORKER_SHA256:?Set its reviewed SHA-256}"
: "${SNOWFALL_CODEC:?Set the independent codec executable}"
: "${FROST_K03_ANVIL:?Set the Anvil executable}"
sf_root=${FROST_K03_SNOWFALL:-"$kc_root/../snowfall"}
evidence_dir=${1:-$(mktemp -d)}
mkdir -p "$evidence_dir"
evidence_dir=$(cd "$evidence_dir" && pwd)
export COREPACK_ENABLE_AUTO_PIN=0
(cd "$kc_root/solidity/ecdsa" && TEST_USE_STUBS_ECDSA=false corepack yarn hardhat test test/Frost.Registry.test.ts test/Frost.Readiness.test.ts) > "$evidence_dir/contracts.log" 2>&1
(cd "$FROST_K03_TBTC/solidity" && TEST_USE_STUBS_TBTC=false corepack yarn hardhat test test/bridge/Frost.Identity.test.ts) > "$evidence_dir/bridge.log" 2>&1
(cd "$FROST_K03_TBTC/solidity" && USE_EXTERNAL_DEPLOY=true TEST_USE_STUBS_TBTC=true corepack yarn hardhat test --no-compile test/bridge/Bridge.Wallets.test.ts) > "$evidence_dir/legacy-wallets.log" 2>&1
cd "$kc_root"
go run ./test/frost/generate ./solidity/ecdsa/build/contracts/frost "$evidence_dir/contracts.go"
cmp pkg/chain/ethereum/frostabi/contracts.go "$evidence_dir/contracts.go"
go test -json ./pkg/frost/... -count=1 > "$evidence_dir/host-normal.jsonl" 2>&1
go test -race -json ./pkg/frost/... -count=1 > "$evidence_dir/host-race.jsonl" 2>&1
go test -json ./pkg/tecdsa/... ./pkg/tbtc/... ./pkg/bitcoin/... ./pkg/chain/ethereum -count=1 > "$evidence_dir/legacy-go.jsonl" 2>&1
go vet ./pkg/frost/... ./pkg/chain/ethereum ./pkg/tbtc > "$evidence_dir/host-vet.log" 2>&1
cd "$sf_root/clients/go"
go test -json ./... -count=1 > "$evidence_dir/client-normal.jsonl" 2>&1
go test -race -json ./... -count=1 > "$evidence_dir/client-race.jsonl" 2>&1
go vet ./... > "$evidence_dir/client-vet.log" 2>&1
printf 'Evidence: %s\n' "$evidence_dir"
