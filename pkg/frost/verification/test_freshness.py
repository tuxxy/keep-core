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


def real_ledger():
    """The committed ledger with obligation evidence removed: these tests check
    obligation identity and approval rules, and the evidence names other
    repositories that are not checked out beside this one."""
    ledger = json.loads(Path(__file__).with_name("COMPOSITION.json").read_text())
    for obligation in ledger["caller_obligations"]:
        obligation["evidence"] = []
    return ledger


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
                "blob": git("rev-parse", "HEAD:source.txt"), "scope": "Synthetic reference only; no semantic discharge",
                "evidence_kind": "execution"}

    def test_cross_purpose_main_obligation_cannot_be_omitted(self):
        ledger = real_ledger()
        missing = copy.deepcopy(ledger)
        missing["caller_obligations"] = [o for o in ledger["caller_obligations"]
            if o["id"] != "A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION"]
        with self.assertRaises(ValueError):
            checker.check_obligations(missing, LEDGER_REPOSITORIES)
        self.assertIn("A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION",
            checker.check_obligations(ledger, LEDGER_REPOSITORIES))

    def test_funding_review_obligations_cannot_be_omitted(self):
        ledger = real_ledger()
        for obligation in ("A-SPV-MAINTAINER-RELAY", "A-K06-CANONICAL-SWEEP-BUILDER",
                           "A-TARGET-CHAIN-GAS-CAP", "A-XONLY-SINGLE-VALIDATION"):
            with self.subTest(obligation=obligation):
                missing = copy.deepcopy(ledger)
                missing["caller_obligations"] = [o for o in missing["caller_obligations"]
                    if o["id"] != obligation]
                with self.assertRaises(ValueError):
                    checker.check_obligations(missing, LEDGER_REPOSITORIES)
                self.assertIn(obligation, checker.check_obligations(ledger, LEDGER_REPOSITORIES))

    def test_required_obligations_and_operational_approval(self):
        ledger = real_ledger()
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
            ledger = {"format": 3, "rows": rows}
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
        ledger = real_ledger()
        ledger["component_statements"] = []
        ledger["review_items"] = []
        for obligation in ledger["caller_obligations"]:
            obligation["statements"] = []
        for row in ledger["rows"]:
            row.update(status="discharged", dependency_coverage="complete", remaining=[], statements=[], blockers=[],
                       owner={"person": "Fixture owner", "assignment_status": "assigned"},
                       producer_sources=[copy.deepcopy(self.reference)],
                       consumer_sources=[copy.deepcopy(self.reference)],
                       joining_evidence=[dict(self.reference, scope="ROW-%s: fixture-only control" % row["id"])])
            for evidence in row["joining_evidence"]:
                evidence.pop("subject", None)
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
                    # A discharged row without its joining record is invalid (L4), not
                    # merely open; an unpinned source side only keeps it open.
                    probe(invalid, 2 if mutation == "missing_evidence" and location in ("producer_sources", "consumer_sources") else 3)

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
        ledger = {"format": 3, "rows": [{"id": i, "status": "open", "producer_result": "future", "consumer_precondition": "future", "model_links": [], "joining_evidence": [], "owner": {"person": None}, "remaining": ["unimplemented"], "dependency_coverage": "selected; incomplete", "producer_sources": [], "consumer_sources": []} for i in range(1, 11)]}
        result = checker.check_rows(ledger, {})
        self.assertTrue(all(r["unpinned_sides"] == ["producer", "consumer"] for r in result))
        self.assertTrue(all(r["status"] == "open" for r in result))

    # --- evidence_kind and closing records (review L4) -------------------

    def test_evidence_kind_is_required_and_closed_set(self):
        ledger = self.closed_fixture()
        for value in (None, "", "test", "Execution"):
            with self.subTest(kind=value):
                invalid = copy.deepcopy(ledger)
                if value is None:
                    del invalid["rows"][0]["joining_evidence"][0]["evidence_kind"]
                else:
                    invalid["rows"][0]["joining_evidence"][0]["evidence_kind"] = value
                with self.assertRaisesRegex(ValueError, "evidence_kind"):
                    checker.check_rows(invalid, self.repositories)
        invalid = copy.deepcopy(ledger)
        del invalid["caller_obligations"][0]["evidence"][0]["evidence_kind"]
        with self.assertRaisesRegex(ValueError, "evidence_kind"):
            checker.check_obligations(invalid, self.repositories)

    def test_row_cannot_leave_open_on_source_or_review_records(self):
        ledger = self.closed_fixture()
        for status in ("discharged", "blocked"):
            for kinds in (["source"], ["review"], ["source", "review"]):
                with self.subTest(status=status, kinds=kinds):
                    invalid = copy.deepcopy(ledger)
                    row = invalid["rows"][3]
                    row["status"] = status
                    row["joining_evidence"] = [dict(self.reference, evidence_kind=k, scope="ROW-4: fixture") for k in kinds]
                    with self.assertRaisesRegex(ValueError, "row 4 cannot leave open"):
                        checker.check_rows(invalid, self.repositories)
        for kind in ("execution", "model"):
            valid = copy.deepcopy(ledger)
            valid["rows"][3]["joining_evidence"] = [dict(self.reference, evidence_kind=kind, scope="ROW-4: fixture")]
            self.assertTrue(checker.check_rows(valid, self.repositories)[3]["has_execution_or_model"])
        still_open = copy.deepcopy(ledger)
        still_open["rows"][3].update(status="open", joining_evidence=[dict(self.reference, evidence_kind="source", scope="ROW-4: fixture")])
        self.assertFalse(checker.check_rows(still_open, self.repositories)[3]["has_execution_or_model"])

    def evidence_tree(self):
        root = self.root / "evidence-root"
        run = root / "implementation" / "run"
        run.mkdir(parents=True)
        log = run / "05-stage.log"
        log.write_text("--- PASS: fixture stage\n")
        digest = checker.file_sha256(log)
        manifest = {"accepted": True, "stages": [{"stage": "05-stage", "log": "05-stage.log", "logSha256": digest, "exit": 0, "result": "pass"}]}
        (run / "manifest.json").write_text(json.dumps(manifest))
        record = {"repository": "evidence", "path": "implementation/run/05-stage.log", "sha256": digest,
                  "manifest": {"path": "implementation/run/manifest.json", "sha256": checker.file_sha256(run / "manifest.json"), "stage": "05-stage"},
                  "evidence_kind": "execution", "scope": "ROW-5: fixture run stage"}
        return root, run, manifest, record

    def test_execution_record_is_pinned_by_file_and_manifest(self):
        root, run, manifest, record = self.evidence_tree()
        checker.verify_evidence([record], self.repositories, "ROW-5", root)
        with self.assertRaisesRegex(ValueError, "evidence-root"):
            checker.verify_evidence([record], self.repositories, "ROW-5", None)
        for mutation in ("log", "manifest_bytes", "stage_missing", "stage_failed", "not_accepted", "source_kind", "escape"):
            with self.subTest(mutation=mutation):
                root, run, manifest, item = self.evidence_tree() if False else (root, run, copy.deepcopy(manifest), copy.deepcopy(record))
                (run / "05-stage.log").write_text("--- PASS: fixture stage\n")
                (run / "manifest.json").write_text(json.dumps(manifest))
                item["manifest"]["sha256"] = checker.file_sha256(run / "manifest.json")
                if mutation == "log":
                    (run / "05-stage.log").write_text("--- FAIL: edited\n")
                elif mutation == "manifest_bytes":
                    item["manifest"]["sha256"] = "0" * 64
                elif mutation in ("stage_missing", "stage_failed", "not_accepted"):
                    if mutation == "stage_missing":
                        item["manifest"]["stage"] = "99-absent"
                    elif mutation == "stage_failed":
                        manifest["stages"][0]["result"] = "fail"
                    else:
                        manifest["accepted"] = False
                    (run / "manifest.json").write_text(json.dumps(manifest))
                    item["manifest"]["sha256"] = checker.file_sha256(run / "manifest.json")
                elif mutation == "source_kind":
                    item["evidence_kind"] = "source"
                else:
                    item["path"] = "../outside.log"
                with self.assertRaises(ValueError):
                    checker.verify_evidence([item], self.repositories, "ROW-5", root)

    def test_execution_subject_drift_is_reported(self):
        ledger = self.closed_fixture()
        before = self.reference["revision"]
        (self.root / "source.txt").write_text("changed after the run\n")
        subprocess.check_call(["git", "-C", str(self.root), "-c", "user.name=F", "-c", "user.email=f@example.invalid",
                               "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qam", "change"])
        after = subprocess.check_output(["git", "-C", str(self.root), "rev-parse", "HEAD"], text=True).strip()
        row = ledger["rows"][4]
        current = dict(self.reference, revision=after, blob=subprocess.check_output(["git", "-C", str(self.root), "rev-parse", "HEAD:source.txt"], text=True).strip())
        row["consumer_sources"] = [current]
        row["producer_sources"] = [current]
        row["joining_evidence"][0]["subject"] = {"fixture": before}
        report = checker.check_rows(ledger, self.repositories)[4]
        self.assertEqual(report["execution_subject_drift"], [{"repository": "fixture", "path": "source.txt", "subject": before}])
        row["joining_evidence"][0]["subject"] = {"fixture": after}
        self.assertEqual(checker.check_rows(ledger, self.repositories)[4]["execution_subject_drift"], [])

    # --- cited component statements (review L1, E6) ----------------------

    STORE_CHECKER = Path(__file__).resolve().parents[1] / "store" / "verification" / "check-freshness.py"

    def statement_fixture(self):
        """A fixture repository carrying the real store checker and a statement
        that pins one file, plus a JSON-style checker and statement file."""
        root = self.root
        (root / "pinned.go").write_text("package fixture\n")
        (root / "store").mkdir()
        (root / "store" / "check-freshness.py").write_text(self.STORE_CHECKER.read_text())
        blob = subprocess.check_output(["git", "-C", str(root), "hash-object", "pinned.go"], text=True).strip()
        statements = {"S-FIX-HOST-1": {"scope": "fixture bound", "evidence": "implementation/fixture/", "pins": {"pinned.go": blob}, "superseded_by": "S-FIX-HOST-2"},
                      "S-FIX-HOST-2": {"scope": "fixture bound", "evidence": "implementation/fixture/", "pins": {"pinned.go": blob}}}
        (root / "store" / "STATEMENTS.json").write_text(json.dumps(statements))
        digest = checker.file_sha256(root / "pinned.go")
        (root / "chain").mkdir()
        (root / "chain" / "pins.json").write_text(json.dumps({"pinned.go": digest}))
        (root / "chain" / "check.py").write_text(
            "import hashlib,json,pathlib,sys\n"
            "root=pathlib.Path(__file__).resolve().parents[1]\n"
            "pins=json.loads((root/'chain/pins.json').read_text())\n"
            "stale=[p for p,d in pins.items() if not (root/p).exists() or hashlib.sha256((root/p).read_bytes()).hexdigest()!=d]\n"
            "print(json.dumps({'fresh':not stale,'stale':stale}))\n"
            "sys.exit(bool(stale))\n")
        (root / "chain" / "S-FIX-CHAIN-1.json").write_text(json.dumps({"id": "S-FIX-CHAIN-1", "bound": "fixture"}))
        subprocess.check_call(["git", "-C", str(root), "add", "-A"])
        subprocess.check_call(["git", "-C", str(root), "-c", "user.name=F", "-c", "user.email=f@example.invalid",
                               "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "statements"])
        return [
            {"slot": "store", "component": "V-01", "id": "S-FIX-HOST-2",
             "checker": {"kind": "store-statements", "repository": "fixture", "path": "store/check-freshness.py"},
             "statement": {"file": "store/STATEMENTS.json"}},
            {"slot": "chain", "component": "V-03", "id": "S-FIX-CHAIN-1",
             "checker": {"kind": "statement-file", "repository": "fixture", "path": "chain/check.py", "args": []},
             "statement": {"search": ["fixture:chain/{id}.json"]}},
        ]

    def test_cited_statement_goes_stale_when_a_pinned_blob_changes(self):
        slots = self.statement_fixture()
        reports = checker.check_statements({"component_statements": slots}, self.repositories, None)
        self.assertEqual({k: v["freshness"] for k, v in reports.items()}, {"store": "fresh", "chain": "fresh"})
        self.assertEqual(reports["store"]["bound"], "fixture bound")
        (self.root / "pinned.go").write_text("package fixture // changed\n")
        reports = checker.check_statements({"component_statements": slots}, self.repositories, None)
        self.assertEqual({k: v["freshness"] for k, v in reports.items()}, {"store": "stale", "chain": "stale"})
        self.assertEqual(len(reports["store"]["detail"]), 1)
        self.assertIn("pinned.go", reports["store"]["detail"][0])
        self.assertEqual(reports["chain"]["detail"], ["pinned.go"])

    def test_superseded_unknown_or_ambiguous_statements(self):
        slots = self.statement_fixture()
        superseded = copy.deepcopy(slots)
        superseded[0]["id"] = "S-FIX-HOST-1"
        self.assertEqual(checker.check_statements({"component_statements": superseded}, self.repositories, None)["store"]["freshness"], "superseded")
        for slot_index, bad in ((0, "S-FIX-HOST-9"), (1, "S-FIX-CHAIN-9"), (0, "not-an-id")):
            with self.subTest(bad=bad):
                invalid = copy.deepcopy(slots)
                invalid[slot_index]["id"] = bad
                with self.assertRaises(ValueError):
                    checker.check_statements({"component_statements": invalid}, self.repositories, None)
        duplicate = copy.deepcopy(slots)
        duplicate[1]["slot"] = "store"
        with self.assertRaises(ValueError):
            checker.check_statements({"component_statements": duplicate}, self.repositories, None)
        unknown_kind = copy.deepcopy(slots)
        unknown_kind[1]["checker"]["kind"] = "trust-me"
        with self.assertRaises(ValueError):
            checker.check_statements({"component_statements": unknown_kind}, self.repositories, None)

    def test_rows_and_obligations_cite_only_known_slots(self):
        ledger = self.closed_fixture()
        ledger["rows"][1]["statements"] = ["missing-slot"]
        with self.assertRaisesRegex(ValueError, "unknown component statement slot"):
            checker.check_rows(ledger, self.repositories, None, {})
        ledger = self.closed_fixture()
        ledger["caller_obligations"][0]["statements"] = ["missing-slot"]
        with self.assertRaisesRegex(ValueError, "unknown component statement slot"):
            checker.check_obligation_citations(ledger, {})

    def write_specs(self, ledger, args):
        for record in ledger["specifications"]:
            path = self.root / (record["name"] + ".md")
            data = (record["name"] + " fixture\n").encode()
            path.write_bytes(data)
            record["blob"] = checker.hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
            record["content_sha256"] = checker.hashlib.sha256(data).hexdigest()
            args.extend(["--spec", record["name"] + "=" + str(path)])

    def test_cli_exit_codes_for_stale_statement_blockers_and_usage(self):
        slots = self.statement_fixture()
        self.reference = self.commit_reference()
        ledger = self.closed_fixture()
        ledger["component_statements"] = slots
        ledger["rows"][1]["statements"] = ["store", "chain"]
        ledger["rows"][1]["blockers"] = [{"gate": "fixture gate", "spec": "fixture", "blocks": "fixture", "missing": "fixture",
                                          "slot": "chain", "owner": {"role": "fixture lead", "person": None}}]
        args = ["python3", "-B", str(Path(__file__).with_name("check_freshness.py")), "--repo", "fixture=" + str(self.root)]
        self.write_specs(ledger, args)
        path = self.root / "ledger.json"
        args.extend(["--ledger", str(path)])
        def probe(data, expected, *extra):
            path.write_text(json.dumps(data))
            result = subprocess.run(args + list(extra), text=True, capture_output=True)
            self.assertEqual(result.returncode, expected, result.stderr + result.stdout[-2000:])
            self.assertNotIn("Traceback", result.stderr)
            return result
        probe(ledger, 0, "--strict")
        # Negative control: change a blob the cited statements pin.
        (self.root / "pinned.go").write_text("package fixture // changed\n")
        out = json.loads(probe(ledger, 1).stdout)
        self.assertEqual(out["stale_statements"], ["S-FIX-CHAIN-1", "S-FIX-HOST-2"])
        self.assertEqual(out["rows"][1]["active_blockers"], ["fixture gate"])
        probe(ledger, 1, "--strict")
        subprocess.check_call(["git", "-C", str(self.root), "checkout", "-q", "--", "pinned.go"])
        unslotted = copy.deepcopy(ledger)
        del unslotted["rows"][1]["blockers"][0]["slot"]
        out = json.loads(probe(unslotted, 2, "--strict").stdout)
        self.assertEqual(out["rows"][1]["active_blockers"], ["fixture gate"])
        probe(unslotted, 0)
        malformed = copy.deepcopy(ledger)
        malformed["rows"][1]["blockers"][0]["owner"] = {"person": "x"}
        probe(malformed, 3)
        # Review L5: a usage error is invalid input (3), distinct from open (2).
        self.assertEqual(subprocess.run(args[:3] + ["--no-such-flag"], capture_output=True).returncode, 3)
        self.assertEqual(subprocess.run(args[:3] + ["--strict", "--ledger"], capture_output=True).returncode, 3)

    def commit_reference(self):
        head = subprocess.check_output(["git", "-C", str(self.root), "rev-parse", "HEAD"], text=True).strip()
        return dict(self.reference, revision=head, blob=subprocess.check_output(["git", "-C", str(self.root), "rev-parse", "HEAD:source.txt"], text=True).strip())

    def test_adapters_for_the_pending_component_checkers(self):
        slots = self.statement_fixture()
        root = self.root
        # A store-style checker that pins an external file and needs --repo,
        # printing "unchecked" without it (the V-07..V-09 checker shape).
        (root / "ext").mkdir()
        (root / "ext" / "check.py").write_text(
            "import sys\n"
            "repos=[a.split('=',1)[1] for a in sys.argv[1:] if '=' in a]\n"
            "print('S-FIX-EXT-1: ' + ('fresh (fixture)' if repos else 'unchecked (fixture)'))\n"
            "sys.exit(0 if repos else 2)\n")
        (root / "ext" / "STATEMENTS.json").write_text(json.dumps({"S-FIX-EXT-1": {"bound": "fixture", "evidence": "x"}}))
        ext = {"slot": "ext", "component": "V-09", "id": "S-FIX-EXT-1",
               "checker": {"kind": "store-statements", "repository": "fixture", "path": "ext/check.py"},
               "statement": {"file": "ext/STATEMENTS.json"}}
        self.assertEqual(checker.check_statement(ext, self.repositories, None)["freshness"], "unchecked")
        ext["checker"]["args"] = ["--repo", "tbtc-v2={repo:fixture}"]
        self.assertEqual(checker.check_statement(ext, self.repositories, None)["freshness"], "fresh")
        ext["checker"]["args"] = ["--repo", "x={repo:absent}"]
        with self.assertRaisesRegex(ValueError, "--repo absent"):
            checker.check_statement(ext, self.repositories, None)
        # The first search pattern that finds the record wins (a record kept
        # in the repository and in its evidence folder is not ambiguous).
        evidence = root / "evidence-root" / "implementation" / "V-03" / "run"
        evidence.mkdir(parents=True)
        (evidence / "S-FIX-CHAIN-1.json").write_text(json.dumps({"id": "S-FIX-CHAIN-1"}))
        chain = copy.deepcopy(slots[1])
        chain["statement"]["search"].append("evidence:implementation/V-03/*/{id}.json")
        self.assertEqual(checker.check_statement(chain, self.repositories, root / "evidence-root")["freshness"], "fresh")
        chain["statement"]["search"] = ["fixture:chain/*.json"]
        (root / "chain" / "S-FIX-CHAIN-9.json").write_text(json.dumps({"id": "S-FIX-CHAIN-9"}))
        with self.assertRaisesRegex(ValueError, "exactly one"):
            checker.check_statement(chain, self.repositories, None)
        # A JSON checker that names a different statement refuses.
        (root / "chain" / "S-FIX-CHAIN-9.json").unlink()
        (root / "chain" / "check.py").write_text("import json\nprint(json.dumps({'statement': 'S-FIX-CHAIN-2', 'fresh': True}))\n")
        with self.assertRaisesRegex(ValueError, "reports statement S-FIX-CHAIN-2"):
            checker.check_statement(slots[1], self.repositories, None)

    # --- the committed ledger ----------------------------------------------

    def test_committed_ledger_shape(self):
        ledger = json.loads(Path(__file__).with_name("COMPOSITION.json").read_text())
        self.assertEqual(ledger["format"], checker.FORMAT)
        slots = {s["slot"] for s in ledger["component_statements"]}
        self.assertEqual(slots, {"V-01-store", "V-02-store", "V-03-chain", "V-04-signing", "V-07-deposit", "V-08-movement", "V-09-enforcement"})
        for row in ledger["rows"]:
            self.assertEqual(row["status"], "open")
            for evidence in row["joining_evidence"]:
                self.assertIn(evidence["evidence_kind"], checker.EVIDENCE_KINDS)
            self.assertTrue(set(row.get("statements", [])) <= slots)
            for blocker in row.get("blockers", []):
                self.assertTrue(blocker["owner"]["role"].strip())
        # L1: rows 2 and 3 and the two storage obligations cite statements.
        rows = {r["id"]: r for r in ledger["rows"]}
        self.assertIn("V-02-store", rows[2]["statements"])
        self.assertIn("V-02-store", rows[3]["statements"])
        obligations = {o["id"]: o for o in ledger["caller_obligations"]}
        self.assertIn("V-01-store", obligations["A-FENCE-STORAGE"]["statements"])
        self.assertIn("V-01-store", obligations["A-GUARDED-END-BEFORE-RELEASE"]["statements"])
        self.assertEqual(checker.check_review_items(real_ledger())[0]["obligation"], "A-SPV-MAINTAINER-RELAY")


if __name__ == "__main__":
    unittest.main()
