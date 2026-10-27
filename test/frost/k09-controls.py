#!/usr/bin/env python3
"""Focused K-09 runner controls in throwaway copies; no mutation campaign."""
import argparse
import json
import os
import signal
import shutil
import time
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


def probe(source, selected=None):
    runner = runpy.run_path(str(source))
    class Guards(unittest.TestCase):
        def test_missing_pin(self):
            with tempfile.TemporaryDirectory() as tmp:
                file = Path(tmp) / "worker"
                file.write_bytes(b"worker")
                with self.assertRaisesRegex(ValueError, "missing binary pin", msg="control MissingPin assertion"):
                    runner["require_pin"](file, "")

        def test_mismatched_pin(self):
            with tempfile.TemporaryDirectory() as tmp:
                file = Path(tmp) / "worker"
                file.write_bytes(b"worker")
                with self.assertRaisesRegex(ValueError, "binary pin mismatch", msg="control MismatchedPin assertion"):
                    runner["require_pin"](file, "0" * 64)

        def test_skipped_go(self):
            with self.assertRaisesRegex(ValueError, "did not pass", msg="control SkippedGo assertion"):
                runner["require_go"](json.dumps({"Test": "Required", "Action": "skip"}), ["Required"])

        def test_missing_contract_case(self):
            result = {"stats": {"passes": 0, "failures": 0, "pending": 0, "tests": 0}, "tests": []}
            with self.assertRaisesRegex(ValueError, "contract cases", msg="control MissingContractCase assertion"):
                runner["require_mocha"](json.dumps(result), 1)

        def test_checkpoint_order(self):
            name = "TestFrostCompleteLocalLifecycle"
            def log(points):
                return "\n".join([json.dumps({"Action": "output", "Test": name, "Output": "K09-CHECKPOINT:" + point}) for point in points] + [json.dumps({"Action": "pass", "Test": name})])
            points = runner["JOINED_CHECKPOINTS"]
            runner["require_go"](log(points), [name])
            for bad in (points[:-1], points[:1] + points, list(reversed(points))):
                with self.assertRaisesRegex(ValueError, "lifecycle checkpoints", msg="control CheckpointOrder assertion"):
                    runner["require_go"](log(bad), [name])

        def test_public_checkpoint_order(self):
            name = "TestPublicLegacyUpgradeCutoverDrainAndSweep"
            points = runner["PUBLIC_LEGACY_CHECKPOINTS"]
            def log(sequence):
                return "\n".join([json.dumps({"Action": "output", "Test": name, "Output": "K09-PUBLIC:" + point}) for point in sequence] + [json.dumps({"Action": "pass", "Test": name})])
            runner["require_go"](log(points), [name])
            for bad in (points[:-1], points[:1] + points, list(reversed(points))):
                with self.assertRaisesRegex(ValueError, "public legacy checkpoints", msg="control PublicCheckpointOrder assertion"):
                    runner["require_go"](log(bad), [name])

        def test_command_failure(self):
            with self.assertRaisesRegex(ValueError, "stage command failed", msg="control CommandFailure assertion"):
                runner["require_exit"](1)

        def test_artifact_source(self):
            # Create complete, internally consistent compiler outputs, then
            # change a source without rebuilding its artifact.
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                kc, tb = root / "kc", root / "tb"
                for label, repo in [("keep-core", kc / "solidity/ecdsa"), ("tbtc-v2", tb / "solidity")]:
                    for qualified in runner["DEPLOYMENT_ARTIFACTS"][label]:
                        source, name = qualified.rsplit(":", 1)
                        path = repo / source if source.startswith("contracts/") else repo / "node_modules" / source
                        path.parent.mkdir(parents=True, exist_ok=True)
                        path.write_text("original source")
                        artifact = {"sourceName": source, "bytecode": "0x00", "deployedBytecode": "0x00"}
                        dest = repo / "build" / source
                        dest.mkdir(parents=True, exist_ok=True)
                        (dest / (name + ".json")).write_text(json.dumps(artifact))
                        (dest / (name + ".dbg.json")).write_text(json.dumps({"buildInfo": name + "-info.json"}))
                        dependency = repo / "node_modules/@openzeppelin/contracts/Dependency.sol"
                        dependency.parent.mkdir(parents=True, exist_ok=True)
                        dependency.write_text("original dependency")
                        info = {"input": {"sources": {source: {"content": "original source"}, "@openzeppelin/contracts/Dependency.sol": {"content": "original dependency"}}},
                                "output": {"contracts": {source: {name: {"evm": {"bytecode": {"object": "00"}, "deployedBytecode": {"object": "00"}}}}}}}
                        (dest / (name + "-info.json")).write_text(json.dumps(info))
                runner["artifact_binding"].__globals__["KC"] = kc
                self.assertEqual(len(runner["artifact_binding"](tb)), sum(map(len, runner["DEPLOYMENT_ARTIFACTS"].values())))
                if selected == "test_deployed_package_output":
                    artifact_path = tb / "solidity/build/@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol/ProxyAdmin.json"
                    bad = json.loads(artifact_path.read_text())
                    bad["bytecode"] = "0x01"
                    artifact_path.write_text(json.dumps(bad))
                    with self.assertRaisesRegex(ValueError, "artifact/compiler output mismatch", msg="control DeployedPackage assertion"):
                        runner["artifact_binding"](tb)
                    return
                if selected == "test_imported_artifact_source":
                    changed = kc / "solidity/ecdsa/node_modules/@openzeppelin/contracts/Dependency.sol"
                else:
                    changed = kc / "solidity/ecdsa/contracts/frost/FrostAdmission.sol"
                changed.write_text("changed source")
                with self.assertRaisesRegex(ValueError, "artifact/source mismatch", msg="control " + ("ImportedSource" if selected == "test_imported_artifact_source" else "ArtifactSource") + " assertion"):
                    runner["artifact_binding"](tb)


        def test_imported_artifact_source(self):
            nonlocal selected
            previous = selected
            selected = "test_imported_artifact_source"
            try:
                self.test_artifact_source()
            finally:
                selected = previous

        def test_deployed_package_output(self):
            nonlocal selected
            previous = selected
            selected = "test_deployed_package_output"
            try:
                self.test_artifact_source()
            finally:
                selected = previous

        def test_local_network(self):
            inherited = {"HARDHAT_NETWORK": "mainnet", "FORKING_URL": "https://invalid.example", "FORKING_BLOCK": "1", "CHAIN_API_URL": "https://invalid.example", "CONTRACT_OWNER_ACCOUNT_PRIVATE_KEY": "SYNTHETIC_NOT_A_KEY", "FROST_C04_LEGACY_SOURCE_ROOT": "/wrong/source", "UNREVIEWED_SETTING": "1"}
            env = runner["local_environment"](inherited)
            command = runner["hardhat_test"]("hardhat", "fixture.ts")
            self.assertEqual(env["HARDHAT_NETWORK"], "hardhat", "control LocalNetwork assertion")
            self.assertFalse(any(k in env for k in ("FORKING_URL", "FORKING_BLOCK", "CHAIN_API_URL", "CONTRACT_OWNER_ACCOUNT_PRIVATE_KEY", "FROST_C04_LEGACY_SOURCE_ROOT", "UNREVIEWED_SETTING")), "control EnvironmentAllowlist assertion")
            self.assertEqual(command[command.index("--network") + 1], "hardhat")

        def test_forbidden_output(self):
            # Also exercise the public writer, not just the result parser.
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                raw = root / "private"
                raw.mkdir(mode=0o700)
                marker = "secret_share=PRIVATE_SENTINEL"
                (raw / "stage.log").write_text(marker)
                with self.assertRaisesRegex(ValueError, "forbidden secret-bearing", msg="control SecretOutput assertion"):
                    runner["require_private_evidence"](raw)
                public = root / "result.json"
                with self.assertRaisesRegex(ValueError, "forbidden secret-bearing"):
                    runner["public_json"](public, {"Output": marker})
                self.assertFalse(public.exists())
                runner["public_json"](public, {"cases": [{"name": "Required", "status": "pass"}]})
                self.assertNotIn("SENTINEL", public.read_text())

        def test_review_coverage(self):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                ledger = root / "pkg/frost/integration/verification/K09-COVERAGE.json"
                ledger.parent.mkdir(parents=True)
                data = {"feedback": [{"id": "K09-" + str(i), "resolution": "open"} for i in range(1, 39)],
                        "reviewAliases": [{"id": name} for name in ("K09A-B01", "K09A-B02", "K09A-C01", "K09A-C02", "K09A-C03")]}
                ledger.write_text(json.dumps(data))
                with patch.dict(runner["review_coverage"].__globals__, {"KC": root}):
                    self.assertIn("K09-15", runner["review_coverage"]())
                    data["feedback"][14]["resolution"] = "fixed; awaiting review"
                    ledger.write_text(json.dumps(data))
                    self.assertIn("K09-15", runner["review_coverage"]())
                    data["feedback"] = [r for r in data["feedback"] if r["id"] != "K09-15"]
                    ledger.write_text(json.dumps(data))
                    with self.assertRaisesRegex(ValueError, "review coverage IDs missing", msg="control ReviewCoverage assertion"):
                        runner["review_coverage"]()
                # Unknown opaque producer data has no route into this summary.
                runner["require_public_output"]("ordinary public test result")

        def test_proof_handoff_scope(self):
            keys = ("FROST_K09_REALISTIC_PROOF_PRIVATE", "FROST_K09_REALISTIC_SWEEP_PRIVATE", "FROST_K09_REALISTIC_REDEMPTION_PRIVATE")
            env = {key: "/private/test/" + key for key in keys}
            for stage in runner["REQUIRED"]:
                actual = runner["stage_environment"](stage, env)
                self.assertEqual(set(actual) & set(keys), set(keys) if stage in ("realistic-proof", "realistic-spv") else set(), "control ProofHandoffScope assertion")
            self.assertEqual(set(env), set(keys))

        def test_cutover_control_record(self):
            good = {"status": "pass", "positiveCases": 10, "guard": "LegacyTimeoutRecovery", "outcome": "killed at owning assertion"}
            good["controls"] = [{"guard": name, "status": "killed at owning assertion"} for name in ("LegacyBudgetRecovery", "LegacyRouteAvailable")]
            runner["require_cutover_controls"](good)
            for field in good:
                bad = dict(good)
                bad.pop(field)
                with self.assertRaisesRegex(ValueError, "bound cutover recovery control missing", msg="control CutoverControlBinding assertion"):
                    runner["require_cutover_controls"](bad)

        def test_local_replacement(self):
            # Real git enumeration and go.mod JSON parsing; no Go build/test.
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                kc, tb, dep = [root / name for name in ("kc", "tb", "dependency")]
                for repo in (kc, tb, dep):
                    repo.mkdir()
                    subprocess.run(["git", "init", "-q", str(repo)], check=True, capture_output=True)
                    subprocess.run(["git", "-C", str(repo), "-c", "user.name=probe", "-c", "user.email=probe@invalid", "commit", "--allow-empty", "-qm", "probe"], check=True, capture_output=True)
                (kc / "go.mod").write_text("module example.com/main\n\ngo 1.24.0\nreplace example.com/dependency => ../dependency\n")
                (dep / "go.mod").write_text("module example.com/dependency\n\ngo 1.24.0\n")
                target = dep / "original.go"
                target.write_text("package dependency\n")
                runner["source_snapshot"].__globals__["KC"] = kc
                env = runner["local_environment"](os.environ)
                before = runner["source_snapshot"](tb, env)
                self.assertIn("go-local:" + str(dep.resolve()), before)
                target.write_text("package dependency\n// changed\n")
                after = runner["source_snapshot"](tb, env)
                with self.assertRaisesRegex(ValueError, "sources changed", msg="control LocalReplacement assertion"):
                    runner["require_sources_unchanged"](before, after)
                target.write_text("package dependency\n")
                # Include ignored Go inputs, which git alone would miss.
                (dep / ".gitignore").write_text("added.go\n")
                before = runner["source_snapshot"](tb, env)
                (dep / "added.go").write_text("package dependency\n")
                after = runner["source_snapshot"](tb, env)
                with self.assertRaisesRegex(ValueError, "sources changed"):
                    runner["require_sources_unchanged"](before, after)
                (dep / "added.go").unlink()
                runner["require_sources_unchanged"](before, runner["source_snapshot"](tb, env))
                (kc / "internal/probe").mkdir(parents=True)
                native = kc / "internal/probe/probe.go"
                native.write_text("package probe\n")
                before = runner["source_snapshot"](tb, env)
                self.assertIn("internal/probe/probe.go", before["keep-core"]["files"])
                native.write_text("package probe\n// changed\n")
                with self.assertRaisesRegex(ValueError, "sources changed"):
                    runner["require_sources_unchanged"](before, runner["source_snapshot"](tb, env))
                native.write_text("package probe\n")
                (kc / ".gitignore").write_text("internal/probe/added.go\n")
                before = runner["source_snapshot"](tb, env)
                (native.parent / "added.go").write_text("package probe\n")
                with self.assertRaisesRegex(ValueError, "sources changed"):
                    runner["require_sources_unchanged"](before, runner["source_snapshot"](tb, env))

        def test_runner_output_boundary(self):
            # Exercise the actual promotion/error path with real Python children.
            for forbidden in (False, True):
                with self.subTest(forbidden=forbidden), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    required = "TestFrostCompleteLocalLifecycle"
                    output = "secret_share=PRIVATE_SENTINEL" if forbidden else "opaque=PRIVATE_SENTINEL"
                    events = [json.dumps({"Action": "output", "Test": required, "Output": output}), json.dumps({"Action": "pass", "Test": required})]
                    events[1:1] = [json.dumps({"Action": "output", "Test": required, "Output": "K09-CHECKPOINT:" + point}) for point in runner["JOINED_CHECKPOINTS"]]
                    program = "print(" + repr("\n".join(events)) + ")"
                    launch = runner["run_command"]
                    def command_stub(command, cwd, env, log_path):
                        self.assertEqual(env["HARDHAT_NETWORK"], "hardhat")
                        return launch([sys.executable, "-c", program], root, env, log_path, timeout=3, grace=.1)
                    env = {k: str(root) for k in ("FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "FROST_K04_BITCOIND", "FROST_C04_LEGACY_BASELINE_BUILD", "FROST_C04_DIGEST_BINARY")}
                    env.update({"SNOWFALL_WORKER_SHA256": runner["WORKER_SHA"], "FROST_K04_BITCOIND_SHA256": runner["CORE_SHA"]})
                    globals_ = runner["main"].__globals__
                    with patch.dict(os.environ, env), patch.object(sys, "argv", ["runner", str(root / "public"), "--only", "joined"]), patch.dict(globals_, {"require_pin": lambda *args: None, "source_snapshot": lambda *args: {}, "artifact_binding": lambda *args: {}, "review_coverage": lambda: [], "run_command": command_stub}), patch.object(subprocess, "check_output", return_value="1.2.3"):
                        code = runner["main"]()
                    result = json.loads((root / "public/result.json").read_text())
                    private = Path(result["privateEvidence"])
                    try:
                        self.assertEqual(code, 1 if forbidden else 0)
                        self.assertEqual(result["status"], "fail" if forbidden else "partial")
                        self.assertEqual(private.stat().st_mode & 0o777, 0o700)
                        self.assertIn("PRIVATE_SENTINEL", (private / "joined.log").read_text())
                        if not forbidden:
                            self.assertEqual(result["privateFiles"]["joined.log"]["sha256"], runner["sha"](private / "joined.log"))
                            self.assertEqual(result["privateFilesSourcesSha256"], result["sourcesSha256"])
                        for public in (root / "public").iterdir():
                            if public.is_file(): self.assertNotIn("PRIVATE_SENTINEL", public.read_text())
                        self.assertEqual(set(p.name for p in (root / "public").iterdir()), {"result.json", "sources.json", "artifacts.json", "private"})
                    finally:
                        shutil.rmtree(private)

        def test_node_log_boundary(self):
            for forbidden in (False, True):
                with self.subTest(forbidden=forbidden), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    name = "TestFrostNodeProcessesDKGAndDispatch"
                    events = [json.dumps({"Action": "output", "Test": name, "Output": "K09-PUBLIC:" + p}) for p in runner["PUBLIC_LEGACY_CHECKPOINTS"]]
                    events.append(json.dumps({"Action": "pass", "Test": name}))
                    events.extend(json.dumps({"Action": "pass", "Test": case}) for case in runner["GO_CASES"]["nodes"] if case != name)
                    program = "print(" + repr("\n".join(events)) + ")"
                    launch = runner["run_command"]
                    after_stage = []
                    def command_stub(command, cwd, env, log_path):
                        node = Path(env["FROST_K09_NODE_LOG_ROOT"]) / "session" / "operator.log"
                        node.parent.mkdir(parents=True)
                        node.write_text("secret_share=CANARY" if forbidden else "public node status")
                        return launch([sys.executable, "-c", program], root, env, log_path, timeout=3, grace=.1)
                    env = {k: str(root) for k in ("FROST_K03_TBTC", "FROST_K03_ANVIL", "SNOWFALL_WORKER", "FROST_K04_BITCOIND", "FROST_C04_LEGACY_BASELINE_BUILD", "FROST_C04_DIGEST_BINARY")}
                    env.update({"SNOWFALL_WORKER_SHA256": runner["WORKER_SHA"], "FROST_K04_BITCOIND_SHA256": runner["CORE_SHA"]})
                    with patch.dict(os.environ, env), patch.object(sys, "argv", ["runner", str(root / "evidence"), "--only", "nodes"]), patch.dict(runner["main"].__globals__, {"require_pin": lambda *a: None, "source_snapshot": lambda *a: {}, "artifact_binding": lambda *a: {}, "review_coverage": lambda: [], "run_command": command_stub, "require_sources_unchanged": lambda *a: after_stage.append(True)}), patch.object(subprocess, "check_output", return_value="1.2.3"):
                        code = runner["main"]()
                    self.assertEqual(code, 1 if forbidden else 0, "control NodeEvidenceBoundary assertion")
                    if forbidden: self.assertFalse(after_stage, "control NodeEvidenceBoundary assertion")
                    result = json.loads((root / "evidence/result.json").read_text())
                    self.assertEqual(result["stages"][-1]["status"], "fail" if forbidden else "pass")
                    if not forbidden:
                        self.assertIn("nodes/session/operator.log", result["privateFiles"])

        def test_process_cleanup(self):
            # Limits: Python-only process groups, <5s each; exact owned cleanup.
            for leader in ("failure", "timeout", "success"):
                with self.subTest(leader=leader), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    pidfile = root / "child.pid"
                    heartbeat = root / 'heartbeat'
                    child = ("import signal,time\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\n"
                             "p=" + repr(str(heartbeat)) + "\n"
                             "for _ in range(400):\n with open(p, 'a') as f: f.write('.')\n time.sleep(.01)\n")
                    program = ('import subprocess,sys,time; p=subprocess.Popen([sys.executable,"-c",sys.argv[2]]); '
                               'open(sys.argv[1],"w").write(str(p.pid)); time.sleep(.15); ' +
                               ('time.sleep(4)' if leader == "timeout" else 'sys.exit(' + ('1' if leader == "failure" else '0') + ')'))
                    original_popen = subprocess.Popen
                    groups = []
                    def track(*args, **kwargs):
                        process = original_popen(*args, **kwargs)
                        groups.append(process.pid)
                        return process
                    try:
                        with patch.object(subprocess, "Popen", track):
                            command = [sys.executable, "-c", program, str(pidfile), child]
                            if leader == "timeout":
                                with self.assertRaises(subprocess.TimeoutExpired):
                                    runner["run_command"](command, root, os.environ.copy(), root / "stage.log", timeout=.4, grace=.1)
                            else:
                                code = runner["run_command"](command, root, os.environ.copy(), root / "stage.log", timeout=2, grace=.1)
                                self.assertEqual(code, 1 if leader == "failure" else 0)
                        self.assertTrue(heartbeat.is_file(), "child never reached its live positive witness")
                        size = heartbeat.stat().st_size
                        time.sleep(.12)
                        self.assertEqual(size, heartbeat.stat().st_size, "control ProcessCleanup assertion")
                    finally:
                        for group in groups:
                            try:
                                os.killpg(group, signal.SIGKILL)
                            except ProcessLookupError:
                                pass

        def test_successful_witnesses(self):
            runner["require_exit"](0)
            runner["require_go"](json.dumps({"Test": "Required", "Action": "pass"}), ["Required"])
            runner["require_mocha"](json.dumps({"stats": {"passes": 1, "failures": 0, "pending": 0, "tests": 1}, "tests": [{}]}), 1)
            with tempfile.TemporaryDirectory() as tmp:
                file = Path(tmp) / "worker"
                file.write_bytes(b"worker")
                runner["require_pin"](file, runner["sha"](file))

    suite = unittest.defaultTestLoader.loadTestsFromTestCase(Guards) if selected is None else unittest.TestSuite([Guards(selected)])
    return unittest.TextTestRunner(verbosity=2).run(suite).wasSuccessful()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--probe", type=Path)
    parser.add_argument("--case")
    args = parser.parse_args()
    if args.probe:
        return 0 if probe(args.probe, args.case) else 1
    original = HERE / "k09-local.py"
    if not probe(original):
        raise RuntimeError("unchanged runner failed its guard probes")
    text = original.read_text()
    controls = [
        ("MissingPin", "test_missing_pin", '    if not expected:', '    if False:'),
        ("MismatchedPin", "test_mismatched_pin", '    if sha(path) != expected:', '    if False:'),
        ("SkippedGo", "test_skipped_go", '        if outcomes != ["pass"]:', '        if False:'),
        ("MissingContractCase", "test_missing_contract_case", '            if stats.get("passes") != count or stats.get("failures") != 0 or stats.get("pending") != 0 or stats.get("tests") != count:', '            if False:'),
        ("CheckpointOrder", "test_checkpoint_order", '        if checkpoints != JOINED_CHECKPOINTS:', '        if False:'),
        ("PublicCheckpointOrder", "test_public_checkpoint_order", '        if checkpoints != PUBLIC_LEGACY_CHECKPOINTS:', '        if False:'),
        ("CommandFailure", "test_command_failure", '    if code != 0:', '    if False:'),
        ("ArtifactSource", "test_artifact_source", '                if inputs[source_path][0] != value["content"]:', '                if False:'),
        ("ImportedSource", "test_imported_artifact_source", '                if inputs[source_path][0] != value["content"]:', '                if source.startswith("contracts/") and inputs[source_path][0] != value["content"]:'),
        ("DeployedPackage", "test_deployed_package_output", '                if artifact[key] != "0x" + compiled["evm"][key]["object"]:', '                if False:'),
        ("LocalNetwork", "test_local_network", '"HARDHAT_NETWORK": "hardhat",', '"HARDHAT_NETWORK": parent.get("HARDHAT_NETWORK"),'),
        ("EnvironmentAllowlist", "test_local_network", '    env = {key: value for key, value in parent.items() if key in allowed}', '    env = dict(parent)'),
        ("ReviewCoverage", "test_review_coverage", '    if not expected <= rows.keys() or aliases != {"K09A-B01", "K09A-B02", "K09A-C01", "K09A-C02", "K09A-C03"}:', '    if False:'),
        ("SecretOutput", "test_forbidden_output", '    if SECRET_OUTPUT.search(text):', '    if False:'),
        ("ProofHandoffScope", "test_proof_handoff_scope", '    if label not in ("realistic-proof", "realistic-spv"):', '    if False:'),
        ("CutoverControlBinding", "test_cutover_control_record", '    if controls.get("status") != "pass" or controls.get("positiveCases") != 10 or controls.get("guard") != "LegacyTimeoutRecovery" or controls.get("outcome") != "killed at owning assertion":', '    if False:'),
        ("NodeEvidenceBoundary", "test_node_log_boundary", '                if label in ("nodes", "node-recovery", "node-guards"):', '                if False:'),
        ("LocalReplacement", "test_local_replacement", '    if before != after:', '    if False:'),
        ("ProcessCleanup", "test_process_cleanup", '            terminate_group(process, grace)', '            process.poll()'),
    ]
    with tempfile.TemporaryDirectory(prefix="k09-controls-") as tmp:
        for name, case, before, after in controls:
            if text.count(before) != 1:
                raise RuntimeError("guard missing or ambiguous: " + name)
            weakened = text.replace(before, after)
            if name == "MissingPin":
                weakened = weakened.replace('    if sha(path) != expected:', '    if expected and sha(path) != expected:')
            if name == "SkippedGo":
                weakened = weakened.replace('    if any(r.get("Action") in ("fail", "skip") for r in rows):', '    if any(r.get("Action") == "fail" for r in rows):')
            target = Path(tmp) / (name + ".py")
            target.write_text(weakened)
            (Path(tmp) / "k09-artifacts.json").write_text((HERE / "k09-artifacts.json").read_text())
            result = subprocess.run([sys.executable, str(Path(__file__).resolve()), "--probe", str(target), "--case", case], capture_output=True, text=True, timeout=30)
            if result.returncode != 1 or "AssertionError: " not in result.stderr or ("control " + name + " assertion") not in result.stderr or "ERROR:" in result.stderr:
                raise RuntimeError("control is not killed at its own assertion: " + name + "\n" + result.stderr)
            print(name + ": killed at owning assertion")
        if args.self_test:
            # A harness exception must never be counted as a killed guard.
            broken = Path(tmp) / "broken.py"
            broken.write_text("raise RuntimeError('harness outage')\n")
            result = subprocess.run([sys.executable, str(Path(__file__).resolve()), "--probe", str(broken)], capture_output=True, text=True, timeout=30)
            if result.returncode == 0 or "AssertionError: " in result.stderr:
                raise RuntimeError("harness outage misclassified")
            print("self-test: unchanged runner survives; harness outage is an error")
    subprocess.run(["node", str(HERE / "k09-baseline-controls.cjs")], check=True, timeout=30)
    return 0


if __name__ == "__main__":
    sys.exit(main())
