#!/usr/bin/env python3
"""Focused K-09 runner controls in throwaway copies; no mutation campaign."""
import argparse
import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest

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

        def test_command_failure(self):
            with self.assertRaisesRegex(ValueError, "stage command failed", msg="control CommandFailure assertion"):
                runner["require_exit"](1)

        def test_artifact_source(self):
            # Create complete, internally consistent compiler outputs, then
            # change a source without rebuilding its artifact.
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                kc, tb = root / "kc", root / "tb"
                for repo, names in [(kc / "solidity/ecdsa", ["FrostWalletRegistry", "FrostDkgValidator", "FrostAdmission"]),
                                    (tb / "solidity", ["Bridge", "Bank", "FrostBridge", "FrostVerifier", "FrostExposureController"])]:
                    for name in names:
                        source_name = "contracts/" + name + ".sol"
                        (repo / "contracts").mkdir(parents=True, exist_ok=True)
                        (repo / source_name).write_text("original source")
                        artifact = {"sourceName": source_name, "bytecode": "0x00", "deployedBytecode": "0x00"}
                        dest = repo / "build" / source_name
                        dest.mkdir(parents=True, exist_ok=True)
                        (dest / (name + ".json")).write_text(json.dumps(artifact))
                        (dest / (name + ".dbg.json")).write_text(json.dumps({"buildInfo": "info.json"}))
                        info = {"input": {"sources": {source_name: {"content": "original source"}}},
                                "output": {"contracts": {source_name: {name: {"evm": {"bytecode": {"object": "00"}, "deployedBytecode": {"object": "00"}}}}}}}
                        (dest / "info.json").write_text(json.dumps(info))
                runner["artifact_binding"].__globals__["KC"] = kc
                self.assertEqual(len(runner["artifact_binding"](tb)), 8)
                (kc / "solidity/ecdsa/contracts/FrostAdmission.sol").write_text("changed source")
                with self.assertRaisesRegex(ValueError, "artifact/source mismatch", msg="control ArtifactSource assertion"):
                    runner["artifact_binding"](tb)

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
        ("CommandFailure", "test_command_failure", '    if code != 0:', '    if False:'),
        ("ArtifactSource", "test_artifact_source", '                    if (repo / source).read_text() != value["content"]:', '                    if False:'),
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
    return 0


if __name__ == "__main__":
    sys.exit(main())
