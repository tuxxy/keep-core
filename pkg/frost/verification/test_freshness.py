"""Fixture-only controls for row-specific source invalidation."""
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("composition_freshness", Path(__file__).with_name("check_freshness.py"))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


# The real ledger cites keep-core evidence; this checkout is that repository.
LEDGER_REPOSITORIES = {"keep-core": Path(__file__).resolve().parents[3]}


class FreshnessTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.reference = self.commit_fixture(self.root)
        self.repositories = {"fixture": self.root}

    @staticmethod
    def commit_fixture(root):
        def git(*args):
            return subprocess.check_output(["git", "-C", str(root), *args], text=True, stderr=subprocess.PIPE).strip()
        git("init", "--quiet")
        (root / "source.txt").write_text("original fixture\n")
        git("add", "source.txt")
        git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
            "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "--quiet", "-m", "fixture")
        return {"repository": "fixture", "path": "source.txt", "revision": git("rev-parse", "HEAD"),
                "blob": git("rev-parse", "HEAD:source.txt"), "scope": "Synthetic reference only; no semantic discharge"}

    def test_cross_purpose_main_obligation_cannot_be_omitted(self):
        ledger = json.loads(Path(__file__).with_name("COMPOSITION.json").read_text())
        missing = copy.deepcopy(ledger)
        missing["caller_obligations"] = [o for o in ledger["caller_obligations"]
            if o["id"] != "A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION"]
        with self.assertRaises(ValueError):
            checker.check_obligations(missing, LEDGER_REPOSITORIES)
        self.assertIn("A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION",
            checker.check_obligations(ledger, LEDGER_REPOSITORIES))

    def test_funding_review_obligations_cannot_be_omitted(self):
        ledger = json.loads(Path(__file__).with_name("COMPOSITION.json").read_text())
        for obligation in ("A-SPV-MAINTAINER-RELAY", "A-K06-CANONICAL-SWEEP-BUILDER"):
            with self.subTest(obligation=obligation):
                missing = copy.deepcopy(ledger)
                missing["caller_obligations"] = [o for o in missing["caller_obligations"]
                    if o["id"] != obligation]
                with self.assertRaises(ValueError):
                    checker.check_obligations(missing, LEDGER_REPOSITORIES)
                self.assertIn(obligation, checker.check_obligations(ledger, LEDGER_REPOSITORIES))

    def test_required_obligations_and_operational_approval(self):
        ledger = json.loads(Path(__file__).with_name("COMPOSITION.json").read_text())
        for change in ("missing", "empty", "duplicate", "malformed"):
            invalid = copy.deepcopy(ledger)
            if change == "missing":
                del invalid["caller_obligations"]
            elif change == "empty":
                invalid["caller_obligations"] = []
            elif change == "duplicate":
                invalid["caller_obligations"].append(invalid["caller_obligations"][0])
            else:
                del invalid["caller_obligations"][0]["monitoring_escalation"]["named_monitor"]
            with self.assertRaises(ValueError):
                checker.check_obligations(invalid)
        proposed = copy.deepcopy(ledger)
        for obligation in proposed["caller_obligations"]:
            obligation["evidence"] = [dict(self.reference, scope=obligation["id"] + ": fixture-only control")]
            obligation["status"] = "operational-owned"
            obligation["owner"]["person"] = "Fixture owner"
            obligation["owner"]["assignment_status"] = "assigned"
            obligation["monitoring_escalation"]["status"] = "approved"
        self.assertEqual(len(checker.check_obligations(proposed, self.repositories)), len(proposed["caller_obligations"]))
        missing_eligibility = copy.deepcopy(ledger)
        missing_eligibility["caller_obligations"] = [o for o in ledger["caller_obligations"] if o["id"] != "A-READY-SEAT-ELIGIBILITY"]
        with self.assertRaises(ValueError):
            checker.check_obligations(missing_eligibility)
        for obligation in proposed["caller_obligations"]:
            obligation["monitoring_escalation"]["named_monitor"] = "Fixture monitor"
            obligation["monitoring_escalation"]["approved_deadline"] = {"status": "not-applicable", "reason": "fixture-only disposition"}
        self.assertEqual(checker.check_obligations(proposed, self.repositories), [])
        for invalid_text in (None, "", " ", True):
            invalid = copy.deepcopy(proposed)
            invalid["caller_obligations"][0]["monitoring_escalation"]["policy"] = invalid_text
            with self.assertRaises(ValueError):
                checker.check_obligations(invalid, self.repositories)
        for invalid_reason in (None, "", " ", True):
            invalid = copy.deepcopy(proposed)
            invalid["caller_obligations"][0]["monitoring_escalation"]["approved_deadline"]["reason"] = invalid_reason
            self.assertIn(invalid["caller_obligations"][0]["id"], checker.check_obligations(invalid, self.repositories))

    def test_null_blob_cannot_match_missing_source(self):
        ledger = self.closed_fixture()
        ledger["rows"][0]["producer_sources"] = [{"repository": "fixture", "path": "missing.txt", "revision": "a" * 40, "blob": None}]
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "non-null"):
                checker.check_rows(ledger, self.repositories)

    def test_spec_content_drift_is_distinct_from_commit_identifiers(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            files = {}
            records = []
            for name in ("KC-SF-V3", "KC-VER"):
                file = root / (name + ".md")
                data = (name + " fixture\n").encode()
                file.write_bytes(data)
                files[name] = file
                records.append({"name": name, "revision_kind": "git-commit-sha1", "revision": "a" * 40, "blob_algorithm": "git-blob-sha1", "blob": checker.hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest(), "content_sha256": checker.hashlib.sha256(data).hexdigest(), "source_repository": "fixture", "source_path": name + ".md"})
            ledger = {"specifications": records}
            self.assertTrue(all(s["matches"] for s in checker.check_specs(ledger, files)))
            files["KC-VER"].write_text("changed fixture\n")
            report = checker.check_specs(ledger, files)
            self.assertTrue(report[0]["matches"])
            self.assertFalse(report[1]["matches"])
            with self.assertRaises(ValueError):
                checker.check_specs({}, files)

    def test_only_dependent_rows_become_stale(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source.txt"
            reference = self.commit_fixture(root)
            rows = [{"id": i, "status": "open", "producer_result": "fixture", "consumer_precondition": "fixture", "model_links": [], "joining_evidence": [], "owner": {"person": None}, "remaining": ["open"], "dependency_coverage": "selected; incomplete", "producer_sources": [], "consumer_sources": []} for i in range(1, 11)]
            rows[4]["consumer_sources"] = [reference]
            ledger = {"format": 2, "rows": rows}
            self.assertFalse(any(r["mismatches"] for r in checker.check_rows(ledger, {"fixture": root})))
            source.write_text("changed fixture\n")
            result = checker.check_rows(ledger, {"fixture": root})
            self.assertEqual([r["row"] for r in result if r["mismatches"]], [5])
            self.assertEqual(result[4]["mismatches"][0]["side"], "consumer")
            self.assertTrue(all(r["status"] == "open" for r in result))
            source.unlink()
            self.assertEqual(checker.check_rows(ledger, {"fixture": root})[4]["mismatches"][0]["actual"], None)
            malformed = copy.deepcopy(ledger)
            del malformed["rows"][2]["status"]
            with self.assertRaises(ValueError):
                checker.check_rows(malformed, {"fixture": root})

    def closed_fixture(self):
        ledger = json.loads(Path(__file__).with_name("COMPOSITION.json").read_text())
        for row in ledger["rows"]:
            row.update(status="discharged", dependency_coverage="complete", remaining=[],
                       owner={"person": "Fixture owner", "assignment_status": "assigned"},
                       producer_sources=[copy.deepcopy(self.reference)],
                       consumer_sources=[copy.deepcopy(self.reference)],
                       joining_evidence=[dict(self.reference, scope="ROW-%s: fixture-only control" % row["id"])])
        for obligation in ledger["caller_obligations"]:
            obligation.update(status="discharged", evidence=[dict(self.reference, scope=obligation["id"] + ": fixture-only control")])
            obligation["owner"].update(person="Fixture owner", assignment_status="assigned")
        return ledger

    def test_all_historical_references_must_resolve_to_the_pinned_blob(self):
        for location in ("producer_sources", "consumer_sources", "joining_evidence", "obligation"):
            for mutation in ("missing_revision", "missing_path", "fabricated_blob", "noncommit", "tree"):
                with self.subTest(location=location, mutation=mutation):
                    ledger = self.closed_fixture()
                    reference = (ledger["caller_obligations"][0]["evidence"][0] if location == "obligation"
                                 else ledger["rows"][0][location][0])
                    if mutation == "missing_revision":
                        reference["revision"] = "0" * 40
                    elif mutation == "missing_path":
                        reference["path"] = "nonexistent.txt"
                    elif mutation == "fabricated_blob":
                        reference["blob"] = "0" * 40
                    elif mutation == "noncommit":
                        reference["revision"] = self.reference["blob"]
                    else:
                        reference["path"] = "."
                    with self.assertRaises(ValueError):
                        checker.check_obligations(ledger, self.repositories)
                        checker.check_rows(ledger, self.repositories)

    def test_closed_obligations_require_evidence_and_open_empty_is_allowed(self):
        ledger = self.closed_fixture()
        for status in ("discharged", "operational-owned"):
            for evidence in ([], None, "fabricated"):
                with self.subTest(status=status, evidence=evidence):
                    invalid = copy.deepcopy(ledger)
                    invalid["caller_obligations"][0].update(status=status, evidence=evidence)
                    with self.assertRaises(ValueError):
                        checker.check_obligations(invalid, self.repositories)
        ledger["caller_obligations"][0].update(status="open", evidence=[])
        self.assertIn(ledger["caller_obligations"][0]["id"], checker.check_obligations(ledger, self.repositories))

    def test_historical_evidence_does_not_require_current_checkout_content(self):
        ledger = self.closed_fixture()
        for row in ledger["rows"]:
            row["producer_sources"] = []
            row["consumer_sources"] = []
        (self.root / "source.txt").write_text("changed current checkout\n")
        self.assertEqual(checker.check_obligations(ledger, self.repositories), [])
        self.assertFalse(any(r["mismatches"] for r in checker.check_rows(ledger, self.repositories)))

    def test_closed_ledger_cli_rejects_fabricated_evidence(self):
        ledger = self.closed_fixture()
        args = ["python3", "-B", str(Path(__file__).with_name("check_freshness.py")),
                "--repo", "fixture=" + str(self.root), "--strict"]
        for record in ledger["specifications"]:
            path = self.root / (record["name"] + ".md")
            data = (record["name"] + " fixture\n").encode()
            path.write_bytes(data)
            record["blob"] = checker.hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
            record["content_sha256"] = checker.hashlib.sha256(data).hexdigest()
            args.extend(["--spec", record["name"] + "=" + str(path)])
        path = self.root / "ledger.json"
        args.extend(["--ledger", str(path)])
        def probe(data, expected):
            path.write_text(json.dumps(data))
            result = subprocess.run(args, text=True, capture_output=True)
            self.assertEqual(result.returncode, expected, result.stderr + result.stdout)
            self.assertNotIn("Traceback", result.stderr)
        probe(ledger, 0)
        operational = copy.deepcopy(ledger)
        for obligation in operational["caller_obligations"]:
            obligation["status"] = "operational-owned"
            obligation["monitoring_escalation"].update(
                status="approved", named_monitor="Fixture monitor",
                approved_deadline={"status": "not-applicable", "reason": "Fixture only"})
        probe(operational, 0)
        for mutation in ("empty", "missing_revision", "fabricated_blob"):
            invalid = copy.deepcopy(operational)
            evidence = invalid["caller_obligations"][0]["evidence"]
            if mutation == "empty":
                evidence.clear()
            else:
                evidence[0]["revision" if mutation == "missing_revision" else "blob"] = "0" * 40
            probe(invalid, 3)
        for location in ("producer_sources", "consumer_sources", "joining_evidence", "obligation"):
            for mutation in ("missing_revision", "fabricated_blob", "missing_evidence"):
                with self.subTest(location=location, mutation=mutation):
                    invalid = copy.deepcopy(ledger)
                    references = (invalid["caller_obligations"][0]["evidence"] if location == "obligation"
                                  else invalid["rows"][0][location])
                    if mutation == "missing_evidence":
                        references.clear()
                    else:
                        references[0]["revision" if mutation == "missing_revision" else "blob"] = "0" * 40
                    probe(invalid, 2 if mutation == "missing_evidence" and location != "obligation" else 3)

    def test_genuine_reference_with_unrelated_scope_is_rejected(self):
        ledger = self.closed_fixture()
        ledger["rows"][4]["joining_evidence"][0]["scope"] = "ROW-50: genuine but unrelated reference"
        with self.assertRaisesRegex(ValueError, "ROW-5"):
            checker.check_rows(ledger, self.repositories)
        ledger = self.closed_fixture()
        ledger["caller_obligations"][0]["evidence"][0]["scope"] = "generic evidence, no obligation identity"
        with self.assertRaisesRegex(ValueError, "evidence scope"):
            checker.check_obligations(ledger, self.repositories)

    def test_missing_and_unpinned_sources_are_not_discharged(self):
        ledger = {"format": 2, "rows": [{"id": i, "status": "open", "producer_result": "future", "consumer_precondition": "future", "model_links": [], "joining_evidence": [], "owner": {"person": None}, "remaining": ["unimplemented"], "dependency_coverage": "selected; incomplete", "producer_sources": [], "consumer_sources": []} for i in range(1, 11)]}
        result = checker.check_rows(ledger, {})
        self.assertTrue(all(r["unpinned_sides"] == ["producer", "consumer"] for r in result))
        self.assertTrue(all(r["status"] == "open" for r in result))


if __name__ == "__main__":
    unittest.main()
