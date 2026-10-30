#!/usr/bin/env bash
# Legacy regression gate for ./config and ./cmd (spec gate G-M, "legacy
# regression passes"). Refs tuxxy/keep-core#42.
#
# Runs both packages in full, once, and writes <evidence>/manifest.json. It is
# accepted only if both packages pass, no test in them fails or skips, and every
# required FROST-off case below ran and passed. Exit status is non-zero unless
# accepted. Any runner that claims the legacy regression (run-local.sh, the
# K-09 harness) must call this script and fail with it.
set -euo pipefail
kc_root=$(cd "$(dirname "$0")/../.." && pwd)
evidence_dir=${1:?usage: check-config-cmd.sh <evidence dir>}
mkdir -p "$evidence_dir"
evidence_dir=$(cd "$evidence_dir" && pwd)
cd "$kc_root"
status=0
go test -json -count=1 -p "${CHECK_GO_PARALLEL:-4}" ./config/ ./cmd/ > "$evidence_dir/config-cmd.jsonl" 2> "$evidence_dir/config-cmd.stderr" || status=$?
head=$(git rev-parse HEAD)
clean=true
[ -z "$(git status --porcelain --untracked-files=no)" ] || clean=false
python3 -I - "$evidence_dir" "$status" "$head" "$clean" <<'PY'
import json, sys
evidence, status, head, clean = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4] == "true"
packages = ["github.com/keep-network/keep-core/config", "github.com/keep-network/keep-core/cmd"]
required = {
    "github.com/keep-network/keep-core/config": ["TestInactiveFrostHasNoEffect", "TestFrostActivationRejectedBeforeCredentials", "TestFrostEnabledRefusedByTheNodeGate"],
    "github.com/keep-network/keep-core/cmd": ["TestInactiveFrostHasNoEffect", "TestStartRefusesFrostAtTheNodeGate"],
}
tests, outcome = {}, {}
for line in open(f"{evidence}/config-cmd.jsonl"):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    if e.get("Action") not in ("pass", "fail", "skip"):
        continue
    if e.get("Test"):
        tests[(e["Package"], e["Test"])] = e["Action"]
    else:
        outcome[e["Package"]] = e["Action"]
reasons = []
for p in packages:
    if outcome.get(p) != "pass":
        reasons.append(f"package {p}: {outcome.get(p, 'not run')}")
for (p, t), a in sorted(tests.items()):
    if a != "pass":
        reasons.append(f"{a}: {p} {t}")
for p, names in required.items():
    for t in names:
        if tests.get((p, t)) is None:
            reasons.append(f"required case missing: {p} {t}")
manifest = {
    "check": "config-cmd legacy regression (tuxxy/keep-core#42)",
    "keepCoreHead": head, "clean": clean, "goTestExit": status,
    "packages": {p: outcome.get(p, "not run") for p in packages},
    "required": {f"{p} {t}": tests.get((p, t), "missing") for p, names in required.items() for t in names},
    "testsRun": len(tests),
    "failed": sum(1 for a in tests.values() if a == "fail"),
    "skipped": sum(1 for a in tests.values() if a == "skip"),
    "reasons": reasons,
    "accepted": status == 0 and not reasons,
}
json.dump(manifest, open(f"{evidence}/manifest.json", "w"), indent=2)
print(json.dumps({k: manifest[k] for k in ("accepted", "packages", "testsRun", "failed", "skipped")}))
for r in reasons:
    print("NOT ACCEPTED:", r)
sys.exit(0 if manifest["accepted"] else 1)
PY
