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
import tempfile
import time

KC = Path(__file__).resolve().parents[2]
WORKER_SHA = "d775dc36c508559f1bd997ab2552cea3aed94587540f6307352fec4628400765"
DIGEST_SHA = "6be09a87119822348daf05d2177855c72a03294c99628d9c746829d4e58b84ba"
CORE_SHA = "f4b9d78a74643c726d022840ca506dc317d41f8825aaef80b7bb705e12c5fda8"
DEPLOYMENT_ARTIFACTS = json.loads(Path(__file__).with_name("k09-artifacts.json").read_text())
GO_CASES = {
    "joined": ["TestFrostCompleteLocalLifecycle"],
    "offline-refund": ["TestFrostLifecycleRefundWithSignersOffline"],
    "refund-timeout": ["TestDepositorRefundWithoutOperators", "TestReservationTimeoutRace"],
    "fraud-recovery": ["TestFrostFraudPublicAdmissionDefenseTimeout"],
    "legacy-drain": ["TestCrossSchemeDrainAndSweep"],
    "proof-depth": ["TestFrostDifficultyProofDepth", "TestGetProofInfo"],
}
JOINED_CHECKPOINTS = ["k09-ready", "k09-credit", "k09-payment", "k09-heartbeat", "k09-inactivity", "k09-cutover", "k09-movement", "k09-restarted-defense", "k09-closure", "k09-post-cutover-dkg"]
REQUIRED = ["compile", "compile-registry", "artifact-binding", "controls", "config-controls", "build", *GO_CASES, "spv", "upgrade"]


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
    if "TestFrostCompleteLocalLifecycle" in expected:
        checkpoints = [point for row in rows if row.get("Action") == "output" and row.get("Test") == "TestFrostCompleteLocalLifecycle"
                       for point in re.findall(r"K09-CHECKPOINT:([a-z0-9-]+)", row.get("Output", ""))]
        if checkpoints != JOINED_CHECKPOINTS:
            raise ValueError("lifecycle checkpoints missing, duplicated or out of order")


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
    manifest, infos, inputs = {}, {}, {}
    for label, repo in [("keep-core", KC / "solidity/ecdsa"), ("tbtc-v2", tb / "solidity")]:
        build = repo / "build"
        pending = list(DEPLOYMENT_ARTIFACTS[label])
        seen = set()
        while pending:
            qualified = pending.pop()
            if qualified in seen:
                continue
            seen.add(qualified)
            source_name, name = qualified.rsplit(":", 1)
            file = build / source_name / (name + ".json")
            if not file.is_file():
                raise ValueError("missing qualified artifact: " + qualified)
            artifact = json.loads(file.read_text())
            debug = json.loads(file.with_suffix(".dbg.json").read_text())
            info_path = (file.parent / debug["buildInfo"]).resolve()
            if info_path not in infos:
                info = json.loads(info_path.read_text())
                infos[info_path] = (info, sha(info_path), hashlib.sha256(json.dumps(info["input"], sort_keys=True).encode()).hexdigest())
            info, info_sha, input_sha = infos[info_path]
            if artifact["sourceName"] != source_name:
                raise ValueError("artifact identity mismatch: " + qualified)
            compiled = info["output"]["contracts"][source_name][name]
            if artifact.get("abi") != compiled.get("abi"):
                raise ValueError("artifact ABI mismatch: " + qualified)
            for key in ("bytecode", "deployedBytecode"):
                if artifact[key] != "0x" + compiled["evm"][key]["object"]:
                    raise ValueError("artifact/compiler output mismatch: " + name)
            if len(artifact["deployedBytecode"][2:]) // 2 > 24576:
                raise ValueError("EIP-170 exceeded: " + name)
            sources = {}
            for source, value in info["input"]["sources"].items():
                source_path = repo / source if source.startswith("contracts/") else repo / "node_modules" / source
                if not source_path.is_file():
                    raise ValueError("missing compiler input: " + source)
                if source_path not in inputs:
                    content = source_path.read_bytes()
                    inputs[source_path] = (content.decode(), hashlib.sha256(content).hexdigest())
                if inputs[source_path][0] != value["content"]:
                    raise ValueError("artifact/source mismatch: " + source)
                sources[source] = {"path": str(source_path.resolve()), "sha256": inputs[source_path][1]}
            manifest[str(file)] = {"sha256": sha(file), "sources": sources,
                                   "buildInfoSha256": info_sha,
                                   "compiler": info.get("solcLongVersion", info.get("solcVersion")),
                                   "inputSha256": input_sha}
            for source, libraries in artifact.get("linkReferences", {}).items():
                pending.extend(source + ":" + library for library in libraries)
    return manifest


# Raw process output is private and never copied into the public evidence tree.
# This detector is an additional refusal control, not a general secret oracle.
SECRET_OUTPUT = re.compile(r"(?i)\b(?:secret[_ -]?share|private[_ -]?key|secret[_ -]?nonce|worker[_ -]?record|nonce[_ -]?record)\b[\"']?\s*[:=]|-----BEGIN (?:EC |RSA |OPENSSH )?PRIVATE KEY-----")


def require_public_output(text):
    if SECRET_OUTPUT.search(text):
        raise ValueError("forbidden secret-bearing output; raw evidence quarantined")


def public_json(path, data):
    text = json.dumps(data, indent=2) + "\n"
    require_public_output(text)
    path.write_text(text)


def require_private_evidence(root):
    # Do not decode opaque worker/keystore blobs. Nothing from this tree is
    # automatically exported. Inspect only bounded diagnostic text artifacts.
    for path in root.rglob("*"):
        if path.is_file() and path.suffix in (".log", ".json", ".txt"):
            if path.stat().st_size > 20 * 1024 * 1024:
                raise ValueError("diagnostic too large for bounded evidence check")
            require_public_output(path.read_text(errors="replace"))


def terminate_group(process, grace=10):
    """Reap the leader and terminate descendants even after the leader exits."""
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        process.wait()
        return
    deadline = time.monotonic() + grace
    while time.monotonic() < deadline:
        process.poll()  # Reap the leader; do not mistake its exit for group exit.
        try:
            os.killpg(process.pid, 0)
        except ProcessLookupError:
            process.wait()
            return
        time.sleep(0.05)
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait()


def run_command(command, cwd, env, log_path, timeout=900, grace=10):
    with log_path.open("w") as log:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=log,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            return process.wait(timeout=timeout)
        finally:
            terminate_group(process, grace)


def tree_sources(root, patterns):
    # Git captures tracked/ordinary untracked inputs. Also enumerate ignored
    # native source files: Go includes an ignored added.go just like any other.
    root = root.resolve()
    paths = set(subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "--", *patterns],
        cwd=root, text=True, timeout=30).splitlines())
    native = {".go", ".c", ".h", ".s", ".S", ".cc", ".cpp", ".syso"}
    for pattern in patterns:
        directory = root / pattern
        if directory.is_dir():
            for base, dirs, names in os.walk(directory):
                dirs[:] = [d for d in dirs if d not in (".git", "node_modules")]
                for name in names:
                    path = Path(base) / name
                    if path.suffix in native:
                        paths.add(str(path.relative_to(root)))
    return {p: sha(root / p) for p in sorted(paths) if (root / p).is_file()}


def source_snapshot(tb, env):
    sources = {}
    for label, repo, patterns in [
        ("keep-core", KC, ["."]),
        ("tbtc-v2", tb, ["solidity/contracts", "solidity/test", "solidity/hardhat.config.ts", "solidity/hardhat.k09.config.ts", "solidity/package.json", "solidity/yarn.lock"]),
    ]:
        sources[label] = {"root": str(repo.resolve()),
                          "head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repo, text=True, timeout=15).strip(),
                          "files": tree_sources(repo, patterns)}
    # Follow every local replacement, including replacements of replacements.
    # GOWORK=off makes these the module roots used by the build/test commands.
    pending, seen = [KC.resolve()], set()
    while pending:
        module = pending.pop()
        if module in seen:
            continue
        seen.add(module)
        metadata = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"], cwd=module, env=env, text=True, timeout=30))
        for replacement in metadata.get("Replace") or []:
            target = replacement["New"]
            if target.get("Version"):
                continue
            root = (module / target["Path"]).resolve()
            if not (root / "go.mod").is_file():
                raise ValueError("missing local Go replacement")
            sources["go-local:" + str(root)] = {"root": str(root), "head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True, timeout=15).strip(), "files": tree_sources(root, ["."])}
            pending.append(root)
    metadata = subprocess.check_output(
        ["go", "list", "-mod=readonly", "-deps", "-test", "-json", "./..."],
        cwd=KC, env=env, text=True, timeout=120)
    decoder, offset, inputs = json.JSONDecoder(), 0, {}
    while offset < len(metadata):
        offset = re.compile(r"\s*").match(metadata, offset).end()
        if offset == len(metadata):
            break
        package, end = decoder.raw_decode(metadata, offset)
        offset = end
        if package.get("ImportPath", "").endswith(".test"):
            continue  # Generated test-main cache file, not a source input.
        directory = Path(package.get("Dir", "."))
        for field in ("GoFiles", "CgoFiles", "CFiles", "HFiles", "SFiles", "SysoFiles", "EmbedFiles", "TestGoFiles", "XTestGoFiles", "TestEmbedFiles", "XTestEmbedFiles"):
            for name in package.get(field, []):
                path = (directory / name).resolve()
                if not path.is_file():
                    raise ValueError("missing Go build input")
                inputs[str(path)] = sha(path)
    sources["go-build-inputs"] = {"root": str(KC.resolve()), "files": dict(sorted(inputs.items()))}
    return sources


def require_sources_unchanged(before, after):
    if before != after:
        raise ValueError("sources changed during run")


def local_environment(parent):
    env = dict(parent)
    # CLI --network is also supplied. Remove fork variables before config load;
    # the K-09 config additionally disables forks after dotenv has loaded.
    env.update({"HARDHAT_NETWORK": "hardhat", "GOWORK": "off", "GOFLAGS": "-mod=readonly",
                "GOPROXY": "off", "GOSUMDB": "off"})
    for name in ("FORKING_URL", "FORKING_BLOCK", "CHAIN_API_URL", "CONTRACT_OWNER_ACCOUNT_PRIVATE_KEY", "ACCOUNTS_PRIVATE_KEYS"):
        env.pop(name, None)
    return env


def hardhat_test(hardhat, *files):
    return [hardhat, "--config", "hardhat.k09.config.ts", "--network", "hardhat", "test", "--no-compile", *files]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("evidence", type=Path)
    parser.add_argument("--only", choices=REQUIRED, action="append", help="partial run; never reports full acceptance")
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=False, mode=0o700)
    old_umask = os.umask(0o077)
    private = Path(tempfile.mkdtemp(prefix="k09-private-"))
    result = {"scope": "K-09a", "evidenceClass": "concrete testing", "status": "running", "requiredStages": REQUIRED,
              "limits": {"cores": 8, "nodeHeapMiB": 8192, "compileHeapMiB": 16384, "stageWallSeconds": 900},
              "privateEvidence": str(private), "publicEvidencePolicy": "generated summaries only; raw logs/artifacts remain private",
              "k09ExitStatus": "open", "openExitRequirements": ["public legacy obligation through upgrade, cutover and containment into FROST drain/incoming sweep (integration lead)", "real node dispatch and firewall qualification (node integration lead)", "realistic production-settings proof assembled by the FROST producer and accepted by its full entry point (SPV integration owner)"],
              "stages": [], "mutationCampaign": "not run; explicit owner request required"}
    result_path = evidence / "result.json"
    env = local_environment(os.environ)
    current, artifacts = None, None
    previous_term = signal.getsignal(signal.SIGTERM)
    def interrupted(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    try:
        for name in ["FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256", "FROST_K04_BITCOIND", "FROST_K04_BITCOIND_SHA256", "FROST_C04_LEGACY_BASELINE_BUILD", "FROST_C04_DIGEST_BINARY"]:
            if not env.get(name):
                raise ValueError("missing environment: " + name)
        require_pin(env["SNOWFALL_WORKER"], WORKER_SHA)
        require_pin(env["FROST_K04_BITCOIND"], CORE_SHA)
        require_pin(env["FROST_C04_DIGEST_BINARY"], DIGEST_SHA)
        env["FROST_C04_DIGEST_SHA256"] = DIGEST_SHA
        if env["SNOWFALL_WORKER_SHA256"] != WORKER_SHA or env["FROST_K04_BITCOIND_SHA256"] != CORE_SHA:
            raise ValueError("environment pins differ from reviewed binaries")
        tb = Path(env["FROST_K03_TBTC"]).resolve()
        env.update({"GOMAXPROCS": "8", "NODE_OPTIONS": "--max-old-space-size=8192",
                    "FROST_C04_LEGACY_PUBLIC_EVIDENCE": str(private / "legacy"),
                    "FROST_C04_LEGACY_EVIDENCE": str(private / "legacy-piece1")})
        result["environment"] = {k: env[k] for k in ["FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256", "FROST_K04_BITCOIND", "FROST_K04_BITCOIND_SHA256", "FROST_C04_LEGACY_BASELINE_BUILD", "FROST_C04_DIGEST_BINARY", "FROST_C04_DIGEST_SHA256", "HARDHAT_NETWORK", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB"]}
        sources = source_snapshot(tb, env)
        public_json(evidence / "sources.json", sources)
        result["sourcesSha256"] = sha(evidence / "sources.json")
        for label, command in [("go", ["go", "version"]), ("node", ["node", "--version"]), ("anvil", [env["FROST_K03_ANVIL"], "--version"]), ("core", [env["FROST_K04_BITCOIND"], "--version"])]:
            version = subprocess.check_output(command, env=env, text=True, timeout=15)
            require_public_output(version)
            # Version output stays private; publish only the version token.
            (private / (label + "-version.log")).write_text(version)
            token = re.search(r"\d+\.\d+(?:\.\d+)?", version)
            if not token:
                raise ValueError("missing tool version")
            result.setdefault("tools", []).append({"tool": label, "version": token[0]})
        hardhat = str(tb / "solidity/node_modules/.bin/hardhat")
        stages = [("compile", [hardhat, "--config", "hardhat.k09.config.ts", "--network", "hardhat", "compile"], tb / "solidity", None)]
        stages += [("compile-registry", [str(KC / "solidity/ecdsa/node_modules/.bin/hardhat"), "--network", "hardhat", "compile"], KC / "solidity/ecdsa", None), ("artifact-binding", None, KC, None), ("controls", [sys.executable, str(KC / "test/frost/k09-controls.py"), "--self-test"], KC, None), ("config-controls", ["node", "test/bridge/k09-config-control.cjs"], tb / "solidity", None), ("build", ["go", "build", "-p", "2", "./..."], KC, None)]
        for label, names in GO_CASES.items():
            stages.append((label, ["go", "test", "-json", "./pkg/maintainer/spv" if label == "proof-depth" else "./pkg/frost/integration", "-run", "^(" + "|".join(names) + ")$", "-count=1", "-timeout", "14m"], KC, names))
        stages += [("spv", hardhat_test(hardhat, "test/bridge/Frost.LifecycleSpv.test.ts"), tb / "solidity", 6),
                   ("upgrade", hardhat_test(hardhat, "test/bridge/Frost.LifecycleUpgrade.test.ts", "test/bridge/Frost.LegacyReconciliation.test.ts"), tb / "solidity", 14)]
        for label, command, cwd, expected in stages:
            if args.only and label not in args.only:
                continue
            current = {"name": label, "command": command, "cwd": str(cwd), "wallLimitSeconds": 900, "status": "running"}
            if label == "legacy-drain":
                current["evidenceClass"] = "seeded component control; not the public upgrade/cutover legacy-to-FROST join"
            result["stages"].append(current)
            public_json(result_path, result)
            print(label + ": running", flush=True)
            start = time.monotonic()
            if (label in GO_CASES or label in ("spv", "upgrade")) and artifacts is None:
                artifacts = artifact_binding(tb)
                public_json(evidence / "artifacts.json", artifacts)
            if label == "artifact-binding":
                artifacts = artifact_binding(tb)
                public_json(evidence / "artifacts.json", artifacts)
            else:
                log_path = private / (label + ".log")
                stage_env = dict(env)
                if label in ("compile", "compile-registry"):
                    stage_env["NODE_OPTIONS"] = "--max-old-space-size=16384"
                code = run_command(command, cwd, stage_env, log_path)
                current["exitCode"] = code
                require_exit(code)
                text = log_path.read_text()
                require_public_output(text)
                if isinstance(expected, list):
                    require_go(text, expected)
                    current["cases"] = [{"name": name, "status": "pass"} for name in expected]
                elif isinstance(expected, int):
                    require_mocha(text, expected)
                    current["caseCounts"] = {"passes": expected, "failures": 0, "pending": 0}
                # No raw output or arbitrary producer JSON is public evidence.
                current["privateLogSha256"] = sha(log_path)
            current.update({"status": "pass", "elapsedSeconds": round(time.monotonic() - start, 2)})
            print(label + ": pass", flush=True)
        require_sources_unchanged(sources, source_snapshot(tb, env))
        if artifacts is not None:
            require_sources_unchanged(artifacts, artifact_binding(tb))
        require_private_evidence(private)
        result["status"] = "partial" if args.only else "pass"
        return 0
    except (Exception, KeyboardInterrupt) as error:
        # Diagnostics may contain arbitrary subprocess material. Never export
        # the exception text, failed-test details or unknown evidence files.
        (private / "error.txt").write_text(str(error))
        result.update({"status": "fail", "error": "run failed; inspect private diagnostics"})
        if current and current["status"] == "running":
            current["status"] = "fail"
        print("run failed; private diagnostics: " + str(private), file=sys.stderr)
        return 1
    finally:
        signal.signal(signal.SIGTERM, previous_term)
        try:
            public_json(result_path, result)
        finally:
            os.umask(old_umask)


if __name__ == "__main__":
    sys.exit(main())
