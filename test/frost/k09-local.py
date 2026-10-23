#!/usr/bin/env python3
"""Native, sequential K-09a acceptance with explicit limits and fail-closed evidence."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

KC = Path(__file__).resolve().parents[2]
WORKER_SHA = "d775dc36c508559f1bd997ab2552cea3aed94587540f6307352fec4628400765"
CORE_SHA = "f4b9d78a74643c726d022840ca506dc317d41f8825aaef80b7bb705e12c5fda8"
GO_CASES = {
    "joined": ["TestFrostCompleteLocalLifecycle"],
    "offline-refund": ["TestFrostLifecycleRefundWithSignersOffline"],
    "refund-timeout": ["TestDepositorRefundWithoutOperators", "TestReservationTimeoutRace"],
    "fraud-recovery": ["TestFrostFraudPublicAdmissionDefenseTimeout"],
    "legacy-drain": ["TestCrossSchemeDrainAndSweep"],
}
REQUIRED = ["compile", "compile-registry", "artifact-binding", "controls", "build", *GO_CASES, "spv", "upgrade"]


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def require_pin(path, expected):
    if not expected:
        raise ValueError("missing binary pin")
    if sha(path) != expected:
        raise ValueError("binary pin mismatch")


def require_go(log, expected):
    rows = []
    for line in log.splitlines():
        try:
            rows.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    for name in expected:
        outcomes = [r.get("Action") for r in rows if r.get("Test") == name and r.get("Action") in ("pass", "fail", "skip")]
        if outcomes != ["pass"]:
            raise ValueError("required Go case did not pass exactly once: " + name)
    if any(r.get("Action") in ("fail", "skip") for r in rows):
        raise ValueError("a Go case failed or skipped")


def require_mocha(log, count):
    decoder = json.JSONDecoder()
    for match in re.finditer(r"\{", log):
        try:
            data, _ = decoder.raw_decode(log[match.start():])
        except json.JSONDecodeError:
            continue
        if isinstance(data, dict) and "stats" in data and "tests" in data:
            stats = data["stats"]
            if stats.get("passes") != count or stats.get("failures") != 0 or stats.get("pending") != 0 or stats.get("tests") != count:
                raise ValueError("required contract cases failed, skipped or were missing")
            return
    raise ValueError("missing Mocha JSON result")


def require_exit(code):
    if code != 0:
        raise ValueError("stage command failed: " + str(code))


def artifact_binding(tb):
    # Validate each production runtime used by deploy.cjs against its own
    # compiler input and output. A copied or stale build cannot silently pass.
    manifest = {}
    for repo, build, names in [
        (KC / "solidity/ecdsa", KC / "solidity/ecdsa/build", ["FrostWalletRegistry", "FrostDkgValidator", "FrostAdmission"]),
        (tb / "solidity", tb / "solidity/build", ["Bridge", "Bank", "FrostBridge", "FrostVerifier", "FrostExposureController"]),
    ]:
        for name in names:
            matches = list((build / "contracts").rglob(name + ".json"))
            if len(matches) != 1:
                raise ValueError("missing or ambiguous artifact: " + name)
            file = matches[0]
            artifact = json.loads(file.read_text())
            debug = json.loads(file.with_suffix(".dbg.json").read_text())
            info = json.loads((file.parent / debug["buildInfo"]).read_text())
            compiled = info["output"]["contracts"][artifact["sourceName"]][name]
            for key in ("bytecode", "deployedBytecode"):
                if artifact[key] != "0x" + compiled["evm"][key]["object"]:
                    raise ValueError("artifact/compiler output mismatch: " + name)
            if len(artifact["deployedBytecode"][2:]) // 2 > 24576:
                raise ValueError("EIP-170 exceeded: " + name)
            sources = {}
            for source, value in info["input"]["sources"].items():
                if source.startswith("contracts/") and not source.startswith("contracts/hardhat-dependency-compiler/"):
                    if (repo / source).read_text() != value["content"]:
                        raise ValueError("artifact/source mismatch: " + source)
                    sources[source] = sha(repo / source)
            manifest[str(file)] = {"sha256": sha(file), "sources": sources}
            for libraries in artifact.get("linkReferences", {}).values():
                for library in libraries:
                    if library not in names:
                        names.append(library)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("evidence", type=Path)
    parser.add_argument("--only", choices=REQUIRED, action="append", help="partial run; never reports full acceptance")
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=False)
    result = {"scope": "K-09a", "evidenceClass": "concrete testing", "status": "running", "requiredStages": REQUIRED,
              "limits": {"cores": 8, "nodeHeapMiB": 8192, "compileHeapMiB": 16384, "stageWallSeconds": 900},
              "stages": [], "mutationCampaign": "not run; explicit owner request required"}
    result_path = evidence / "result.json"
    env = os.environ.copy()
    current = None
    try:
        for name in ["FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256", "FROST_K04_BITCOIND", "FROST_K04_BITCOIND_SHA256", "FROST_C04_LEGACY_BASELINE_BUILD"]:
            if not env.get(name):
                raise ValueError("missing environment: " + name)
        # The reviewed binaries, not arbitrary caller-selected hashes.
        require_pin(env["SNOWFALL_WORKER"], WORKER_SHA)
        require_pin(env["FROST_K04_BITCOIND"], CORE_SHA)
        if env["SNOWFALL_WORKER_SHA256"] != WORKER_SHA or env["FROST_K04_BITCOIND_SHA256"] != CORE_SHA:
            raise ValueError("environment pins differ from reviewed binaries")
        tb = Path(env["FROST_K03_TBTC"]).resolve()
        env.update({"GOMAXPROCS": "8", "NODE_OPTIONS": "--max-old-space-size=8192",
                    "FROST_C04_LEGACY_PUBLIC_EVIDENCE": str(evidence / "legacy"),
                    "FROST_C04_LEGACY_EVIDENCE": str(evidence / "legacy-piece1")})
        result["environment"] = {k: env[k] for k in ["FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256", "FROST_K04_BITCOIND", "FROST_K04_BITCOIND_SHA256", "FROST_C04_LEGACY_BASELINE_BUILD"]}
        # File hashes bind concrete results to sources, including test adapters;
        # no operator journals, worker transcripts or Core data are copied.
        sources = {}
        for label, repo, patterns in [
            ("keep-core", KC, ["pkg/frost", "pkg/bitcoin", "pkg/chain/ethereum", "pkg/maintainer/spv", "test/frost", "scripts/frost-lifecycle-local.sh", "go.mod", "go.sum"]),
            ("tbtc-v2", tb, ["solidity/contracts", "solidity/test", "solidity/hardhat.config.ts", "solidity/hardhat.k09.config.ts", "solidity/yarn.lock"]),
        ]:
            paths = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "--", *patterns], cwd=repo, text=True).splitlines()
            sources[label] = {"head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repo, text=True).strip(),
                              "files": {p: sha(repo / p) for p in paths if (repo / p).is_file()}}
        (evidence / "sources.json").write_text(json.dumps(sources, indent=2) + "\n")
        result["sourcesSha256"] = sha(evidence / "sources.json")
        for command in [["go", "version"], ["node", "--version"], [env["FROST_K03_ANVIL"], "--version"], [env["FROST_K04_BITCOIND"], "--version"]]:
            result.setdefault("tools", []).append({"command": command, "version": subprocess.check_output(command, text=True, timeout=15).strip()})
        hardhat = str(tb / "solidity/node_modules/.bin/hardhat")
        stages = [("compile", [hardhat, "compile"], tb / "solidity", None)]
        stages += [("compile-registry", [str(KC / "solidity/ecdsa/node_modules/.bin/hardhat"), "compile"], KC / "solidity/ecdsa", None), ("artifact-binding", None, KC, None), ("controls", [sys.executable, str(KC / "test/frost/k09-controls.py"), "--self-test"], KC, None), ("build", ["go", "build", "-p", "2", "./..."], KC, None)]
        for label, names in GO_CASES.items():
            stages.append((label, ["go", "test", "-json", "./pkg/frost/integration", "-run", "^(" + "|".join(names) + ")$", "-count=1", "-timeout", "14m"], KC, names))
        stages += [("spv", [hardhat, "--config", "hardhat.k09.config.ts", "test", "--no-compile", "test/bridge/Frost.LifecycleSpv.test.ts"], tb / "solidity", 6),
                   ("upgrade", [hardhat, "--config", "hardhat.k09.config.ts", "test", "--no-compile", "test/bridge/Frost.LifecycleUpgrade.test.ts", "test/bridge/Frost.LegacyReconciliation.test.ts"], tb / "solidity", 9)]
        for label, command, cwd, expected in stages:
            if args.only and label not in args.only:
                continue
            current = {"name": label, "command": command, "cwd": str(cwd), "wallLimitSeconds": 900, "status": "running"}
            result["stages"].append(current)
            result_path.write_text(json.dumps(result, indent=2) + "\n")
            print(label + ": running", flush=True)
            start = time.monotonic()
            if label == "artifact-binding":
                (evidence / "artifacts.json").write_text(json.dumps(artifact_binding(tb), indent=2) + "\n")
            else:
                log_path = evidence / (label + ".log")
                stage_env = dict(env)
                if label in ("compile", "compile-registry"):
                    stage_env["NODE_OPTIONS"] = "--max-old-space-size=16384"
                with log_path.open("w") as log:
                    process = subprocess.Popen(command, cwd=cwd, env=stage_env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                    try:
                        code = process.wait(timeout=900)
                    except BaseException:
                        os.killpg(process.pid, signal.SIGTERM)
                        try:
                            process.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            os.killpg(process.pid, signal.SIGKILL)
                            process.wait()
                        raise
                current["exitCode"] = code
                require_exit(code)
                text = log_path.read_text()
                if isinstance(expected, list):
                    require_go(text, expected)
                elif isinstance(expected, int):
                    require_mocha(text, expected)
                current["logSha256"] = sha(log_path)
            current.update({"status": "pass", "elapsedSeconds": round(time.monotonic() - start, 2)})
            print(label + ": pass", flush=True)
        # Detect source edits while a run was active. Historical results are
        # reusable only on identical recorded sources, never by branch name.
        for label, repo in [("keep-core", KC), ("tbtc-v2", tb)]:
            if any(not (repo / p).is_file() or sha(repo / p) != pin for p, pin in sources[label]["files"].items()):
                raise ValueError("sources changed during run: " + label)
        result["status"] = "partial" if args.only else "pass"
        return 0
    except Exception as error:
        result.update({"status": "fail", "error": str(error)})
        if current and current["status"] == "running":
            current["status"] = "fail"
        print(str(error), file=sys.stderr)
        return 1
    finally:
        result_path.write_text(json.dumps(result, indent=2) + "\n")


if __name__ == "__main__":
    sys.exit(main())
