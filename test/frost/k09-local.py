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
ANVIL_SHA = "b20eaa1ca89974f84baf1267a12d619e9724566bbef8588b83b5c5422872418e"
DEPLOYMENT_ARTIFACTS = json.loads(Path(__file__).with_name("k09-artifacts.json").read_text())
GO_CASES = {
    "joined": ["TestFrostCompleteLocalLifecycle"],
    "offline-refund": ["TestFrostLifecycleRefundWithSignersOffline"],
    "refund-timeout": ["TestDepositorRefundWithoutOperators", "TestReservationTimeoutRace"],
    "fraud-recovery": ["TestFrostFraudPublicAdmissionDefenseTimeout"],
    "legacy-drain": ["TestCrossSchemeDrainAndSweep"],
    "public-legacy": ["TestPublicLegacyUpgradeCutoverDrainAndSweep"],
    "nodes": ["TestFrostNodeProcessesDKGAndDispatch", "TestFrostNodeCandidateEpochInterruption", "TestFrostNodeApprovedEpochInterruption", "TestFrostNodeIncomingDuringStalledDKG", "TestFrostNodeSignedIncomingInterruption", "TestFrostNodeReservationDefersWithoutExit"],
    "node-recovery": ["TestFrostNodeCertifiedEpochInterruption", "TestFrostNodeCertificateSubmitInterruption", "TestFrostNodeUnsignedIncomingReplansAfterRestart", "TestFrostNodePrecertificationRecovery", "TestFrostNodeMissingPreKeyIsLost", "TestFrostNodeGrantedIncomingStaysPinned", "TestFrostNodeDifferentFeesAgree", "TestLegacyHostResetsBlockedDrain"],
    "proof-depth": ["TestFrostDifficultyProofDepth", "TestGetProofInfo"],
    "realistic-proof": ["TestFrostRetainedMainnetMovementProof", "TestFrostRetainedMainnetSweepProof", "TestFrostRetainedMainnetRedemptionProof"],
}
JOINED_CHECKPOINTS = ["k09-ready", "k09-credit", "k09-payment", "k09-heartbeat", "k09-inactivity", "k09-cutover", "k09-movement", "k09-restarted-defense", "k09-closure", "k09-post-cutover-dkg"]
PUBLIC_LEGACY_CHECKPOINTS = ["public-legacy-funded", "legacy-upgraded", "legacy-drain-committed", "legacy-cutover-contained", "public-frost-drain-swept"]
REQUIRED = ["compile", "compile-registry", "artifact-binding", "controls", "node-guards", "cutover-guards", "review5-guards", "config-controls", "build", "legacy-regression", *GO_CASES, "spv", "realistic-spv", "upgrade", "movement-regression", "legacy-bridge"]

def review_coverage():
    ledger = json.loads((KC / "pkg/frost/integration/verification/K09-COVERAGE.json").read_text())
    expected = {"K09-" + str(i) for i in range(1, 39)}
    rows = {r["id"]: r for r in ledger["feedback"]}
    aliases = {r["id"] for r in ledger.get("reviewAliases", [])}
    if not expected <= rows.keys() or aliases != {"K09A-B01", "K09A-B02", "K09A-C01", "K09A-C02", "K09A-C03"}:
        raise ValueError("review coverage IDs missing")
    # Author-implemented changes still await review; never silently remove
    # those IDs from the open exit list on the author's own assertion.
    closed = {"fixed in round 3 review", "fixed; reviewed"}
    return [key for key in sorted(expected & rows.keys()) if rows[key].get("resolution") not in closed]


def require_cutover_controls(controls):
    expected = [{"guard": name, "status": "killed at owning assertion"} for name in ("LegacyBudgetRecovery", "LegacyRouteAvailable")]
    if controls.get("status") != "pass" or controls.get("positiveCases") != 10 or controls.get("guard") != "LegacyTimeoutRecovery" or controls.get("outcome") != "killed at owning assertion":
        raise ValueError("bound cutover recovery control missing")
    if controls.get("controls") != expected:
        raise ValueError("bound cutover recovery control missing: budget or available-route control")
    return {"guard": "LegacyTimeoutRecovery", "status": "killed at owning assertion", "subcontrols": expected}


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

    public_case = next((name for name in expected if name in ("TestPublicLegacyUpgradeCutoverDrainAndSweep", "TestFrostNodeProcessesDKGAndDispatch")), None)
    if public_case:
        checkpoints = [point for row in rows if row.get("Action") == "output" and row.get("Test") == public_case
                       for point in re.findall(r"K09-PUBLIC:([a-z0-9-]+)", row.get("Output", ""))]
        if checkpoints != PUBLIC_LEGACY_CHECKPOINTS:
            raise ValueError("public legacy checkpoints missing, duplicated or out of order")


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


def artifact_binding(tb, baseline=None):
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
    if baseline is not None:
        legacy = json.loads(subprocess.check_output(["node", str(KC / "test/frost/k09-baseline.cjs"), str(tb), str(baseline)], text=True, timeout=60))
        manifest.update({"legacy-baseline:" + file: value for file, value in legacy.items()})
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
    allowed = {"PATH", "HOME", "TMPDIR", "USER", "LOGNAME", "LANG", "LC_ALL", "TERM", "SHELL",
               "GOPATH", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "CGO_ENABLED", "CC", "CXX", "SDKROOT", "MACOSX_DEPLOYMENT_TARGET",
               "FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256",
               "FROST_K04_BITCOIND", "FROST_K04_BITCOIND_SHA256", "FROST_C04_LEGACY_BASELINE_BUILD",
               "FROST_C04_DIGEST_BINARY", "FROST_C04_DIGEST_SHA256", "FROST_K09_NODE_LOG_ROOT"}
    env = {key: value for key, value in parent.items() if key in allowed}
    # CLI --network is also supplied. Remove fork variables before config load;
    # the K-09 config additionally disables forks after dotenv has loaded.
    env.update({"HARDHAT_NETWORK": "hardhat", "GOWORK": "off", "GOFLAGS": "-mod=readonly",
                "GOPROXY": "off", "GOSUMDB": "off"})
    for name in ("FORKING_URL", "FORKING_BLOCK", "CHAIN_API_URL", "CONTRACT_OWNER_ACCOUNT_PRIVATE_KEY", "ACCOUNTS_PRIVATE_KEYS"):
        env.pop(name, None)
    return env


def stage_environment(label, env):
    result = dict(env)
    # These files are exclusive producer/consumer handoffs. A broad package
    # regression must not run an integration helper with the final export path.
    if label not in ("realistic-proof", "realistic-spv"):
        for name in ("FROST_K09_REALISTIC_PROOF_PRIVATE", "FROST_K09_REALISTIC_SWEEP_PRIVATE", "FROST_K09_REALISTIC_REDEMPTION_PRIVATE"):
            result.pop(name, None)
    if label == "legacy-bridge":
        result.update(USE_EXTERNAL_DEPLOY="true", TEST_USE_STUBS_TBTC="true")
    return result


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
    private = evidence / "private"
    private.mkdir(mode=0o700)
    result = {"scope": "K-09a", "evidenceClass": "concrete testing", "status": "running", "requiredStages": REQUIRED,
              "limits": {"cores": 8, "nodeHeapMiB": 8192, "compileHeapMiB": 16384, "stageWallSeconds": 900},
              "privateEvidence": str(private), "publicEvidencePolicy": "generated summaries only; raw logs/artifacts remain private",
              "k09ExitStatus": "open", "openExitRequirements": ["owner review of bounded K-09 implementation and composition assumptions", "KC-VER models/replays and authoritative V-05 closure", "explicitly requested K-09b campaign after V-05 closure"],
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
        require_pin(env["FROST_K03_ANVIL"], ANVIL_SHA)
        env["FROST_C04_DIGEST_SHA256"] = DIGEST_SHA
        if env["SNOWFALL_WORKER_SHA256"] != WORKER_SHA or env["FROST_K04_BITCOIND_SHA256"] != CORE_SHA:
            raise ValueError("environment pins differ from reviewed binaries")
        tb = Path(env["FROST_K03_TBTC"]).resolve()
        result["openReviewFindings"] = review_coverage()
        result["openExitRequirements"] += result["openReviewFindings"]
        result["anvilSha256"] = ANVIL_SHA
        env.update({"GOMAXPROCS": "8", "NODE_OPTIONS": "--max-old-space-size=8192",
                    "FROST_K09_NODE_LOG_ROOT": str(private / "nodes"),
                    "FROST_C04_LEGACY_SOURCE_ROOT": str(tb),
                    "FROST_K09_REALISTIC_VECTOR": str(tb / "solidity/test/data/k09-taproot-retarget.json"),
                    "FROST_K09_REALISTIC_PROOF_PRIVATE": str(private / "realistic-proof.json"),
                    "FROST_K09_REALISTIC_SWEEP_PRIVATE": str(private / "realistic-sweep.json"),
                    "FROST_K09_REALISTIC_REDEMPTION_VECTOR": str(tb / "solidity/test/data/k09-redemption-retarget.json"),
                    "FROST_K09_REALISTIC_REDEMPTION_PRIVATE": str(private / "realistic-redemption.json"),
                    "FROST_C04_LEGACY_PUBLIC_EVIDENCE": str(private / "legacy"),
                    "FROST_C04_LEGACY_EVIDENCE": str(private / "legacy-piece1")})
        result["environment"] = {k: env[k] for k in ["FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "SNOWFALL_WORKER_SHA256", "FROST_K04_BITCOIND", "FROST_K04_BITCOIND_SHA256", "FROST_C04_LEGACY_BASELINE_BUILD", "FROST_C04_DIGEST_BINARY", "FROST_C04_DIGEST_SHA256", "HARDHAT_NETWORK", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB"]}
        result["environment"].update({key: env[key] for key in ("FROST_C04_LEGACY_SOURCE_ROOT", "FROST_K09_NODE_LOG_ROOT")})
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
        result["legacyBridgeExclusions"] = []
        hardhat = str(tb / "solidity/node_modules/.bin/hardhat")
        stages = [("compile", [hardhat, "--config", "hardhat.k09.config.ts", "--network", "hardhat", "compile", "--force"], tb / "solidity", None)]
        stages += [("compile-registry", [str(KC / "solidity/ecdsa/node_modules/.bin/hardhat"), "--network", "hardhat", "compile"], KC / "solidity/ecdsa", None), ("artifact-binding", None, KC, None), ("controls", [sys.executable, str(KC / "test/frost/k09-controls.py"), "--self-test"], KC, None), ("config-controls", ["node", "test/bridge/k09-config-control.cjs"], tb / "solidity", None), ("build", ["go", "build", "-p", "2", "./..."], KC, None)]
        stages.insert(4, ("node-guards", [sys.executable, str(KC / "test/frost/k09-node-controls.py"), str(private / "node-guards"), "--firewall"], KC, None))
        stages.insert(5, ("cutover-guards", [sys.executable, str(KC / "test/frost/k09-cutover-controls.py"), str(private / "cutover-guards")], KC, None))
        stages.insert(6, ("review5-guards", [sys.executable, str(KC / "test/frost/k09-review5-controls.py"), str(private / "review5-guards")], KC, None))
        stages.append(("legacy-regression", ["go", "test", "-p", "2", "./config", "./cmd", "./pkg/tbtcpg", "./pkg/tbtc", "./pkg/chain/ethereum", "./pkg/net/libp2p", "./pkg/maintainer/spv", "-count=1", "-timeout", "8m"], KC, None))
        for label, names in GO_CASES.items():
            stages.append((label, ["go", "test", "-json", "./pkg/maintainer/spv" if label in ("proof-depth", "realistic-proof") else "./pkg/frost/integration", "-run", "^(" + "|".join(names) + ")$", "-count=1", "-timeout", "14m"], KC, names))
        stages += [("spv", hardhat_test(hardhat, "test/bridge/Frost.LifecycleSpv.test.ts"), tb / "solidity", 6),
                   ("realistic-spv", hardhat_test(hardhat, "test/bridge/Frost.LifecycleRealisticSpv.test.ts", "test/bridge/Frost.LifecycleRealisticObligations.test.ts"), tb / "solidity", 7),
                   ("upgrade", hardhat_test(hardhat, "test/bridge/Frost.LifecycleUpgrade.test.ts", "test/bridge/Frost.LegacyReconciliation.test.ts"), tb / "solidity", 14),
                   ("movement-regression", hardhat_test(hardhat, "test/bridge/Frost.Movement.test.ts"), tb / "solidity", 33),
                   ("legacy-bridge", [hardhat, "--config", "hardhat.k09.legacy.config.ts", "--network", "hardhat", "test", "--no-compile", "test/bridge/Bridge.Wallets.test.ts", "test/bridge/Bridge.MovingFunds.test.ts"], tb / "solidity", 269)]
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
            if (label in GO_CASES or label in ("spv", "realistic-spv", "upgrade", "movement-regression", "legacy-bridge", "cutover-guards")) and artifacts is None:
                artifacts = artifact_binding(tb, env["FROST_C04_LEGACY_BASELINE_BUILD"])
                public_json(evidence / "artifacts.json", artifacts)
            if label == "artifact-binding":
                artifacts = artifact_binding(tb, env["FROST_C04_LEGACY_BASELINE_BUILD"])
                public_json(evidence / "artifacts.json", artifacts)
            else:
                log_path = private / (label + ".log")
                stage_env = stage_environment(label, env)
                if label in ("compile", "compile-registry"):
                    stage_env["NODE_OPTIONS"] = "--max-old-space-size=16384"
                code = run_command(command, cwd, stage_env, log_path)
                current["exitCode"] = code
                require_exit(code)
                text = log_path.read_text()
                require_public_output(text)
                if label == "node-guards":
                    controls = json.loads((private / "node-guards/result.json").read_text())
                    if controls.get("status") != "pass" or not any(c.get("guard") == "NodeFirewall" and c.get("status") == "killed at owning assertion" for c in controls.get("controls", [])):
                        raise ValueError("bound firewall control missing")
                    current["controls"] = [{"guard": c["guard"], "status": c["status"]} for c in controls["controls"]]
                    current["sourcesSha256"] = result["sourcesSha256"]
                if label == "review5-guards":
                    controls = json.loads((private / "review5-guards/result.json").read_text())
                    expected_controls = [{"guard": name, "status": "killed at owning assertion"} for name in ("IncompleteGrant", "InterruptedClaim", "IncomingCeiling", "DrainBudget", "HostTimeoutReset", "NodeDeadline")]
                    if controls.get("status") != "pass" or controls.get("controls") != expected_controls:
                        raise ValueError("round-five owning controls missing")
                    current["controls"] = expected_controls
                    current["sourcesSha256"] = result["sourcesSha256"]
                if label == "cutover-guards":
                    controls = json.loads((private / "cutover-guards/result.json").read_text())
                    current["controls"] = [require_cutover_controls(controls)]
                    current["sourcesSha256"] = result["sourcesSha256"]
                if label in ("nodes", "node-recovery", "node-guards"):
                    require_private_evidence(private / "nodes")
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
            require_sources_unchanged(artifacts, artifact_binding(tb, env["FROST_C04_LEGACY_BASELINE_BUILD"]))
        require_private_evidence(private)
        result["privateFiles"] = {str(path.relative_to(private)): {"sha256": sha(path), "bytes": path.stat().st_size}
                                  for path in sorted(private.rglob("*")) if path.is_file()}
        result["privateFilesSourcesSha256"] = result["sourcesSha256"]
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
            # Retain a complete hash inventory on failed/quarantined runs too;
            # never promote their contents or label them accepted evidence.
            result["privateFiles"] = {str(path.relative_to(private)): {"sha256": sha(path), "bytes": path.stat().st_size}
                                      for path in sorted(private.rglob("*")) if path.is_file()}
            if "sourcesSha256" in result:
                result["privateFilesSourcesSha256"] = result["sourcesSha256"]
            public_json(result_path, result)
        finally:
            os.umask(old_umask)


if __name__ == "__main__":
    sys.exit(main())
