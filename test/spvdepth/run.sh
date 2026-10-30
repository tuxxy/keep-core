#!/usr/bin/env bash
# FROST proof-depth fast tier (tuxxy/keep-core#28, D18). Local only.
#
# Runs TestFrostProofDepthFollowsBridgeFactor (its own Anvils on free ports) and
# writes <evidence>/manifest.json. Accepted only when exactly the 16 expected
# cases (2 factors x 4 producers x 2 blocks) ran and passed; a skipped,
# missing or extra case is a failure. The Go test skips without its
# environment, so a green plain `go test` proves nothing for #28.
#
# Usage: test/spvdepth/run.sh <evidence dir>
# Required environment (no defaults):
#   FROST_DEPTH_TBTC_BUILD   a tbtc-v2 build made by test/legacy/build-baseline.sh
#                            at FROST_DEPTH_TBTC_COMMIT
#   FROST_DEPTH_TBTC_COMMIT  the tbtc-v2 integration commit it was built from
#   FROST_DEPTH_ANVIL        Anvil
#   FROST_DEPTH_SOLC         solc 0.8.17 (compiles the SpvDepthFixture wrapper)
set -euo pipefail
evidence=${1:?evidence dir}
for v in FROST_DEPTH_TBTC_BUILD FROST_DEPTH_TBTC_COMMIT FROST_DEPTH_ANVIL FROST_DEPTH_SOLC; do
	[[ -n ${!v:-} ]] || { echo "missing $v" >&2; exit 2; }
done
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$evidence"
evidence=$(cd "$evidence" && pwd)
cd "$root"
head=$(git rev-parse HEAD)
clean=$([[ -z $(git status --porcelain) ]] && echo true || echo false)
set +e
GOMAXPROCS=${GOMAXPROCS:-5} go test -p 5 -json ./pkg/maintainer/spv/ -run '^TestFrostProofDepthFollowsBridgeFactor$' -count=1 -timeout 600s \
	> "$evidence/test.jsonl" 2>&1
set -e
python3 - "$evidence" "$head" "$clean" <<'PY'
import json, sys, hashlib
evidence, head, clean = sys.argv[1], sys.argv[2], sys.argv[3] == "true"
results, top = {}, None
for line in open(f"{evidence}/test.jsonl"):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    if e.get("Action") in ("pass", "fail", "skip") and e.get("Test"):
        results[e["Test"]] = e["Action"]
name = "TestFrostProofDepthFollowsBridgeFactor"
top = results.get(name, "not run")
expected = {f"{name}/factor-{f}/{p}-block-{b}" for f in (6, 2)
            for p in ("movement", "incoming-sweep", "deposit-sweep", "redemption-payment")
            for b in (157954, 157937)}
sub = {k: v for k, v in results.items() if k.count("/") == 2}
manifest = {
    "candidate": {"keepCoreHead": head, "clean": clean},
    "test": "TestFrostProofDepthFollowsBridgeFactor",
    "result": top,
    "subtests": sub,
    "passed": sum(v == "pass" for v in sub.values()),
    "failed": sum(v == "fail" for v in sub.values()),
    "skipped": sum(v == "skip" for v in sub.values()),
    "missing": sorted(expected - set(sub)),
    "unexpected": sorted(set(sub) - expected),
    "logSha256": hashlib.sha256(open(f"{evidence}/test.jsonl", "rb").read()).hexdigest(),
}
manifest["accepted"] = (top == "pass" and set(sub) == expected
                        and all(v == "pass" for v in sub.values()))
json.dump(manifest, open(f"{evidence}/manifest.json", "w"), indent=2)
print(json.dumps({k: manifest[k] for k in ("result", "passed", "failed", "skipped", "accepted")}))
sys.exit(0 if manifest["accepted"] else 1)
PY
