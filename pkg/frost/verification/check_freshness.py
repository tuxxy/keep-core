#!/usr/bin/env python3
"""Check the V-05 composition ledger: source pins, evidence provenance, cited
component statements (V-01 to V-04) and closure eligibility.

Never discharges a composition row. A match means the pinned bytes are the
current bytes; it is not a model result or a semantic argument.

Exit codes (distinct, KC-VER V-05 review L5):
  0  selected checks pass (strict: structurally eligible for closure)
  1  drift: a selected source, a reviewed specification or a cited
     component statement is stale, superseded or missing its fresh result
  2  strict only: the ledger is still open or incomplete (no drift)
  3  invalid ledger, provenance or input, including command-line errors
"""
import argparse
import glob
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

FORMAT = 3
EVIDENCE_KINDS = ("source", "execution", "model", "review")
# Kinds that may move a row out of "open" (review L4): a run record or a
# model statement whose bytes are pinned. Sources and review text never do.
CLOSING_KINDS = ("execution", "model")
# File-hash references under --evidence-root, for retained run evidence that
# is not in a Git repository.
EVIDENCE_REPOSITORY = "evidence"
CHECKER_TIMEOUT = 300

REQUIRED_OBLIGATIONS = {
    "A-K04-AGREED-EXECUTOR-CONFIG", "A-GUARDED-END-BEFORE-RELEASE",
    "A-READY-FRESH-RELOAD", "A-BUILDER-FROZEN-BEFORE-SIGN", "A-CHAIN-FINALITY",
    "A-FENCE-STORAGE", "A-OPERATOR-ATTEST-AFTER-VALIDATION",
    "A-ONE-INTENT-PER-STATE-VERSION", "A-STORED-DESCRIPTOR-ONLY",
    "A-FEE-INCLUSION-ESCALATION", "A-DUPLICATE-REQUEST-STATE-GUARD",
    "A-READY-SEAT-ELIGIBILITY", "A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION",
    "A-SPV-MAINTAINER-RELAY", "A-K06-CANONICAL-SWEEP-BUILDER",
    "A-K05-TIMEOUT-POLICY", "A-K05-LATE-SIGNATURE-SETTLEMENT",
    "A-K05-CERTIFICATE-TRANSPORT", "A-TARGET-CHAIN-GAS-CAP",
    "A-XONLY-SINGLE-VALIDATION",
}


def object_hash(value):
    return isinstance(value, str) and bool(re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", value))


def sha256_hex(value):
    return isinstance(value, str) and bool(re.fullmatch(r"[0-9a-f]{64}", value))


def nonempty_text(value):
    return isinstance(value, str) and bool(value.strip())


def relative_path(value):
    if not nonempty_text(value):
        raise ValueError("path is required")
    relative = PurePosixPath(value)
    if relative.is_absolute() or ".." in relative.parts:
        raise ValueError("path must be relative without '..': %s" % value)
    return relative


def valid_reference(source):
    return isinstance(source, dict) and nonempty_text(source.get("repository")) and nonempty_text(source.get("path")) and object_hash(source.get("revision")) and object_hash(source.get("blob"))


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True, stderr=subprocess.PIPE).strip()


# Committed objects are immutable, so a resolved (checkout, commit, path) is
# cached for the life of the process.
RESOLVED = {}


def verify_reference(source, repositories):
    """Verify historical identity only; matching hashes do not prove semantics."""
    if not valid_reference(source):
        raise ValueError("reference requires non-null path/revision/blob provenance")
    repo = source["repository"]
    if repo not in repositories:
        raise ValueError("provide --repo %s=/path/to/checkout" % repo)
    root = repositories[repo].resolve()
    relative_path(source["path"])
    key = (str(root), source["revision"], source["path"])
    if key not in RESOLVED:
        try:
            # A blob/tree object is not an acceptable substitute for a commit.
            if git(root, "cat-file", "-t", source["revision"]) != "commit":
                raise ValueError("reference revision must identify a commit")
            actual = git(root, "rev-parse", "--verify", source["revision"] + ":" + source["path"])
            RESOLVED[key] = (actual, git(root, "cat-file", "-t", actual))
        except subprocess.CalledProcessError as error:
            raise ValueError("unresolvable reference %s:%s:%s" %
                             (repo, source["revision"], source["path"])) from error
    actual, kind = RESOLVED[key]
    if kind != "blob" or actual != source["blob"]:
        raise ValueError("revision/path blob mismatch for %s:%s:%s" %
                         (repo, source["revision"], source["path"]))
    return root


def file_sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_file_reference(reference, evidence_root):
    """A retained run record outside Git, pinned by SHA-256 under --evidence-root.

    With a manifest, the manifest bytes are pinned too and, for a named stage,
    the manifest must record this file's hash with a passing result and an
    accepted run. A stage that is not in the manifest refuses (K09-13)."""
    if evidence_root is None:
        raise ValueError("provide --evidence-root for %s references" % EVIDENCE_REPOSITORY)
    if not sha256_hex(reference.get("sha256")):
        raise ValueError("evidence file reference requires sha256")
    path = evidence_root / relative_path(reference.get("path"))
    if not path.is_file() or file_sha256(path) != reference["sha256"]:
        raise ValueError("evidence file missing or changed: %s" % reference["path"])
    manifest = reference.get("manifest")
    if manifest is None:
        return
    if not isinstance(manifest, dict) or not sha256_hex(manifest.get("sha256")):
        raise ValueError("malformed evidence manifest reference")
    manifest_path = evidence_root / relative_path(manifest.get("path"))
    if not manifest_path.is_file() or file_sha256(manifest_path) != manifest["sha256"]:
        raise ValueError("evidence manifest missing or changed: %s" % manifest.get("path"))
    stage = manifest.get("stage")
    if stage is None:
        return
    data = json.loads(manifest_path.read_text())
    entries = [s for s in data.get("stages", []) if s.get("stage") == stage]
    if len(entries) != 1:
        raise ValueError("stage %s not recorded exactly once in %s" % (stage, manifest["path"]))
    entry = entries[0]
    if entry.get("logSha256") != reference["sha256"] or entry.get("result") != "pass" or entry.get("exit") != 0 or data.get("accepted") is not True:
        raise ValueError("stage %s is not a passing accepted record of %s" % (stage, reference["path"]))


def verify_evidence(evidence, repositories, target=None, evidence_root=None):
    if not isinstance(evidence, list):
        raise ValueError("evidence must be a list")
    for reference in evidence:
        if not isinstance(reference, dict) or not nonempty_text(reference.get("scope")):
            raise ValueError("malformed evidence provenance")
        if reference.get("evidence_kind") not in EVIDENCE_KINDS:
            raise ValueError("evidence_kind must be one of %s" % "/".join(EVIDENCE_KINDS))
        if target is not None and not re.search(r"(?<![A-Za-z0-9-])" + re.escape(target) + r"(?![A-Za-z0-9-])", reference["scope"]):
            raise ValueError("evidence scope must identify " + target)
        if reference.get("repository") == EVIDENCE_REPOSITORY:
            if reference["evidence_kind"] == "source":
                raise ValueError("a source reference must be pinned in a Git repository")
            verify_file_reference(reference, evidence_root)
        else:
            if not valid_reference(reference):
                raise ValueError("malformed evidence provenance")
            verify_reference(reference, repositories)
        subject = reference.get("subject")
        if subject is not None:
            if not isinstance(subject, dict) or not subject or any(not object_hash(v) or len(v) != 40 for v in subject.values()):
                raise ValueError("evidence subject must map repositories to full commit IDs")


def check_obligations(ledger, repositories=None, evidence_root=None):
    repositories = {} if repositories is None else repositories
    obligations = ledger.get("caller_obligations")
    if not isinstance(obligations, list):
        raise ValueError("caller_obligations is required")
    ids = [o["id"] for o in obligations]
    if len(set(ids)) != len(ids) or not REQUIRED_OBLIGATIONS.issubset(ids):
        raise ValueError("missing or duplicate required caller obligation IDs")
    unresolved = []
    for obligation in obligations:
        for field in ("requirement", "status", "owner", "monitoring_escalation"):
            if field not in obligation:
                raise ValueError("malformed obligation %s: missing %s" % (obligation["id"], field))
        owner = obligation["owner"]
        policy = obligation["monitoring_escalation"]
        if obligation["status"] not in ("open", "discharged", "operational-owned") or not nonempty_text(obligation["requirement"]) or not isinstance(owner, dict) or not nonempty_text(owner.get("role")) or not isinstance(policy, dict):
            raise ValueError("malformed caller obligation record")
        for field in ("status", "policy", "named_monitor", "approved_deadline"):
            if field not in policy:
                raise ValueError("malformed monitoring policy")
        if not nonempty_text(policy["policy"]):
            raise ValueError("monitoring policy text is required")
        evidence = obligation.get("evidence", [])
        verify_evidence(evidence, repositories, obligation["id"], evidence_root)
        if obligation["status"] in ("discharged", "operational-owned") and not evidence:
            raise ValueError("closed obligation %s requires verified evidence" % obligation["id"])
        assigned = nonempty_text(owner.get("person")) and owner.get("assignment_status") == "assigned"
        approved = policy["status"] == "approved" and nonempty_text(policy["named_monitor"])
        deadline = policy["approved_deadline"]
        deadline_recorded = isinstance(deadline, dict) and (
            (deadline.get("status") == "not-applicable" and nonempty_text(deadline.get("reason"))) or
            (deadline.get("status") == "approved" and isinstance(deadline.get("value"), int) and not isinstance(deadline["value"], bool) and deadline["value"] >= 0 and nonempty_text(deadline.get("unit")) and nonempty_text(deadline.get("anchor")))
        )
        if obligation["status"] not in ("discharged", "operational-owned") or not assigned or (obligation["status"] == "operational-owned" and not (approved and deadline_recorded)):
            unresolved.append(obligation["id"])
    return unresolved


def check_specs(ledger, files):
    specifications = ledger.get("specifications")
    if not isinstance(specifications, list) or {s["name"] for s in specifications} != {"KC-SF-V3", "KC-VER"} or len(specifications) != 2:
        raise ValueError("exact integration/verification spec provenance is required")
    reports = []
    for spec in specifications:
        if spec.get("revision_kind") != "git-commit-sha1" or spec.get("blob_algorithm") != "git-blob-sha1" or not object_hash(spec.get("revision")) or len(spec["revision"]) != 40 or not object_hash(spec.get("blob")) or len(spec["blob"]) != 40 or not re.fullmatch(r"[0-9a-f]{64}", spec.get("content_sha256", "")) or not spec.get("source_repository") or not spec.get("source_path"):
            raise ValueError("invalid specification provenance")
        if spec["name"] not in files:
            raise ValueError("provide --spec %s=/path/to/reviewed/spec" % spec["name"])
        data = files[spec["name"]].read_bytes()
        blob = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        digest = hashlib.sha256(data).hexdigest()
        reports.append({"name": spec["name"], "source_repository": spec["source_repository"], "revision": spec["revision"], "source_path": spec["source_path"], "matches": blob == spec["blob"] and digest == spec["content_sha256"], "actual_blob": blob, "actual_sha256": digest})
    return reports


# --- Component statements (review L1, E6) ---------------------------------
#
# The ledger cites each V-01..V-04 safety statement through a slot that holds
# only the statement ID and how to find and check it. Bound, evidence path and
# scope are read from the statement record itself, so changing the cited ID is
# a one-line edit. Each slot runs that component's own freshness checker; the
# ledger never re-implements its pin set.

def run_checker(root, script, args):
    command = [sys.executable, "-B", str(root / relative_path(script)), *args]
    try:
        result = subprocess.run(command, cwd=str(root), text=True, capture_output=True, timeout=CHECKER_TIMEOUT)
    except subprocess.TimeoutExpired as error:
        raise ValueError("component checker timed out: %s" % script) from error
    return result


def find_statement(slot, repositories, evidence_root):
    """The first search pattern that finds the record wins, so a statement
    kept both in the repository and in its evidence folder is not ambiguous;
    within that pattern the match must be unique."""
    matches = []
    for pattern in slot["statement"].get("search", []):
        if matches:
            break
        location, _, template = pattern.partition(":")
        relative_path(template.replace("{id}", "x"))
        if location == EVIDENCE_REPOSITORY:
            if evidence_root is None:
                raise ValueError("provide --evidence-root to locate %s" % slot["id"])
            base = evidence_root
        elif location in repositories:
            base = repositories[location].resolve()
        else:
            raise ValueError("provide --repo %s=/path/to/checkout" % location)
        matches += [Path(p) for p in glob.glob(str(base / template.replace("{id}", slot["id"])))]
    if len(matches) != 1:
        raise ValueError("statement %s must resolve to exactly one record, found %d" % (slot["id"], len(matches)))
    record = json.loads(matches[0].read_text())
    if record.get("id") != slot["id"]:
        raise ValueError("statement record %s does not carry its ID" % matches[0])
    return matches[0], record


def checker_args(checker, repositories):
    """Checker arguments; "{repo:NAME}" is replaced by that --repo checkout,
    so a component checker that pins external files (tbtc-v2, the specs)
    checks them against the same checkouts as the ledger."""
    args = checker.get("args", [])
    if not isinstance(args, list) or any(not isinstance(a, str) for a in args):
        raise ValueError("checker args must be a list of strings")
    out = []
    for arg in args:
        for name in re.findall(r"\{repo:([^}]+)\}", arg):
            if name not in repositories:
                raise ValueError("provide --repo %s=/path/to/checkout" % name)
            arg = arg.replace("{repo:%s}" % name, str(repositories[name].resolve()))
        out.append(arg)
    return out


def field(record, dotted):
    value = record
    for part in dotted.split("."):
        if not isinstance(value, dict) or part not in value:
            return None
        value = value[part]
    return value


def check_statement(slot, repositories, evidence_root):
    for key in ("slot", "component", "id", "checker", "statement"):
        if key not in slot:
            raise ValueError("component statement slot missing %s" % key)
    if not re.fullmatch(r"S-[A-Z0-9]+-[A-Z0-9]+-[0-9]+", slot["id"]):
        raise ValueError("statement ID %r is not S-<component>-<n>" % slot["id"])
    checker = slot["checker"]
    repo = checker.get("repository")
    if repo not in repositories:
        raise ValueError("provide --repo %s=/path/to/checkout" % repo)
    root = repositories[repo].resolve()
    report = {"slot": slot["slot"], "component": slot["component"], "id": slot["id"], "checker": checker.get("path")}
    kind = checker.get("kind")
    if kind == "store-statements":
        statements_file = root / relative_path(slot["statement"].get("file"))
        statements = json.loads(statements_file.read_text())
        if slot["id"] not in statements:
            raise ValueError("statement %s is not in %s" % (slot["id"], slot["statement"]["file"]))
        record = statements[slot["id"]]
        report.update(statement_record=slot["statement"]["file"], bound=record.get("bound", record.get("scope")), evidence=record.get("evidence"))
        result = run_checker(root, checker.get("path"), checker_args(checker, repositories))
        if result.returncode not in (0, 1, 2):
            raise ValueError("component checker %s failed: %s" % (checker.get("path"), result.stderr.strip()[-300:]))
        lines = {}
        for line in result.stdout.splitlines():
            # Any status other than "fresh" (stale, unchecked, ...) is not fresh.
            match = re.match(r"^(S-[A-Z0-9-]+): (superseded by (\S+)|[a-z][a-z-]*)", line)
            if match:
                lines[match.group(1)] = match.group(2)
        if slot["id"] not in lines:
            raise ValueError("component checker did not report %s" % slot["id"])
        status = lines[slot["id"]]
        report["freshness"] = "superseded" if status.startswith("superseded") else status
        detail = []
        for line in result.stdout.split(slot["id"] + ":", 1)[1].splitlines()[1:]:
            if not line.startswith("  "):
                break
            detail.append(line.strip())
        report["detail"] = detail[:40]
        if status.startswith("superseded"):
            report["detail"] = ["cited statement is %s; update the slot ID" % status]
    elif kind == "statement-file":
        path, record = find_statement(slot, repositories, evidence_root)
        if record.get("superseded_by"):
            report.update(freshness="superseded", detail=["superseded by %s; update the slot ID" % record["superseded_by"]])
            return report
        args = []
        for arg in checker_args(checker, repositories):
            if arg.startswith("{statement:") and arg.endswith("}"):
                value = field(record, arg[len("{statement:"):-1])
                if not nonempty_text(value):
                    raise ValueError("statement %s lacks %s" % (slot["id"], arg))
                base = evidence_root if value.startswith("implementation/") else root
                if base is None:
                    raise ValueError("provide --evidence-root for %s" % slot["id"])
                args.append(str(base / relative_path(value)))
            else:
                args.append(arg)
        report.update(statement_record=str(path), bound=record.get("bound", record.get("bounds")),
                      evidence=str(path.parent))
        result = run_checker(root, checker.get("path"), args)
        try:
            output = json.loads(result.stdout)
        except json.JSONDecodeError as error:
            raise ValueError("component checker %s output is not JSON: %s" % (checker.get("path"), result.stderr.strip()[-300:])) from error
        if output.get("statement") not in (None, slot["id"]):
            raise ValueError("component checker %s reports statement %s, not %s" % (checker.get("path"), output.get("statement"), slot["id"]))
        fresh = output.get("fresh") is True or output.get("status") == "fresh"
        if result.returncode not in (0, 1) or fresh != (result.returncode == 0):
            raise ValueError("component checker %s exit %d disagrees with its output" % (checker.get("path"), result.returncode))
        report["freshness"] = "fresh" if fresh else "stale"
        stale = output.get("stale", output.get("mismatches", []))
        report["detail"] = [s if isinstance(s, str) else s.get("path") for s in stale][:40]
    else:
        raise ValueError("unknown component checker kind %r" % kind)
    return report


def check_statements(ledger, repositories, evidence_root):
    slots = ledger.get("component_statements")
    if not isinstance(slots, list):
        raise ValueError("component_statements is required")
    names = [s.get("slot") for s in slots]
    if len(set(names)) != len(names) or any(not nonempty_text(n) for n in names):
        raise ValueError("component statement slots must be unique and named")
    return {slot["slot"]: check_statement(slot, repositories, evidence_root) for slot in slots}


def cited_slots(record, statements, label):
    cited = record.get("statements", [])
    if not isinstance(cited, list) or any(slot not in statements for slot in cited):
        raise ValueError("%s cites an unknown component statement slot" % label)
    return cited


def check_blockers(row, statements):
    blockers = row.get("blockers", [])
    if not isinstance(blockers, list):
        raise ValueError("row %s blockers must be a list" % row["id"])
    reports = []
    for blocker in blockers:
        owner = blocker.get("owner") if isinstance(blocker, dict) else None
        if not isinstance(blocker, dict) or any(not nonempty_text(blocker.get(f)) for f in ("gate", "spec", "blocks", "missing")) or not isinstance(owner, dict) or not nonempty_text(owner.get("role")):
            raise ValueError("row %s has a malformed blocker" % row["id"])
        slot = blocker.get("slot")
        if slot is not None and slot not in statements:
            raise ValueError("row %s blocker cites an unknown slot" % row["id"])
        # A gate with a statement slot clears only while that statement is
        # fresh; a gate without one has no result and stays active.
        active = slot is None or statements[slot]["freshness"] != "fresh"
        reports.append({"gate": blocker["gate"], "slot": slot, "active": active,
                        "owner_role": owner["role"], "owner_assigned": nonempty_text(owner.get("person")) and owner.get("assignment_status") == "assigned"})
    return reports


def check_rows(ledger, repositories, evidence_root=None, statements=None):
    statements = {} if statements is None else statements
    if ledger.get("format") != FORMAT:
        raise ValueError("unsupported ledger format")
    rows = ledger.get("rows", [])
    if [row.get("id") for row in rows] != list(range(1, 11)):
        raise ValueError("ledger must contain all ten composition rows in order")
    reports = []
    for row in rows:
        for field_name in ("status", "producer_result", "consumer_precondition", "model_links", "joining_evidence", "owner", "remaining"):
            if field_name not in row:
                raise ValueError("row %s missing %s" % (row["id"], field_name))
        if row["status"] not in ("open", "blocked", "discharged") or any(not nonempty_text(row.get(f)) for f in ("producer_result", "consumer_precondition", "dependency_coverage")) or not isinstance(row["owner"], dict):
            raise ValueError("malformed composition row")
        if any(not isinstance(row[f], list) for f in ("model_links", "joining_evidence", "remaining")) or any(not nonempty_text(value) for value in row["model_links"] + row["remaining"]):
            raise ValueError("malformed composition evidence/model lists")
        verify_evidence(row["joining_evidence"], repositories, "ROW-%s" % row["id"], evidence_root)
        kinds = {k: sum(1 for e in row["joining_evidence"] if e["evidence_kind"] == k) for k in EVIDENCE_KINDS}
        cited = cited_slots(row, statements, "row %s" % row["id"])
        closing = any(e["evidence_kind"] in CLOSING_KINDS for e in row["joining_evidence"])
        # Review L4: a row leaves "open" only on a pinned execution or model
        # record; test sources and review text cannot move it.
        if row["status"] != "open" and not closing:
            raise ValueError("row %s cannot leave open without a pinned execution or model record" % row["id"])
        mismatches = []
        unpinned = []
        pinned = {}
        for side in ("producer", "consumer"):
            sources = row.get(side + "_sources")
            if not isinstance(sources, list):
                raise ValueError("row %s missing %s sources" % (row["id"], side))
            if not sources:
                unpinned.append(side)
            for source in sources:
                if not valid_reference(source):
                    raise ValueError("row %s requires non-null path/revision/blob provenance" % row["id"])
                repo = source["repository"]
                root = verify_reference(source, repositories)
                pinned[(repo, source["path"])] = source["blob"]
                path = (root / source["path"]).resolve()
                path.relative_to(root)
                actual = git(root, "hash-object", "--no-filters", "--", str(path)) if path.is_file() else None
                if actual is None or actual != source["blob"]:
                    mismatches.append({"side": side, "repository": repo, "path": source["path"], "expected": source["blob"], "actual": actual})
        # Execution evidence names the heads it ran on. Report every pinned row
        # source whose bytes differ at that head: the run did not execute them.
        subject_drift = []
        for evidence in row["joining_evidence"]:
            for repo, commit in (evidence.get("subject") or {}).items():
                if repo not in repositories:
                    raise ValueError("provide --repo %s=/path/to/checkout" % repo)
                for (pinned_repo, path), blob in sorted(pinned.items()):
                    if pinned_repo != repo:
                        continue
                    try:
                        at_subject = git(repositories[repo].resolve(), "rev-parse", "--verify", "--quiet", commit + ":" + path)
                    except subprocess.CalledProcessError:
                        at_subject = None
                    if at_subject != blob:
                        item = {"repository": repo, "path": path, "subject": commit}
                        if item not in subject_drift:
                            subject_drift.append(item)
        blockers = check_blockers(row, statements)
        stale_statements = [statements[s]["id"] for s in cited if statements[s]["freshness"] != "fresh"]
        assigned = nonempty_text(row["owner"].get("person")) and row["owner"].get("assignment_status") == "assigned"
        selected = "stale" if mismatches else ("unavailable" if len(unpinned) == 2 else "partial-match" if unpinned else "match")
        reports.append({"row": row["id"], "status": row["status"], "selected_pins": selected, "unpinned_sides": unpinned,
                        "mismatches": mismatches, "dependency_coverage": row["dependency_coverage"],
                        "unassigned_owner": not assigned, "remaining_open": bool(row["remaining"]),
                        "missing_joining_evidence": not row["joining_evidence"], "evidence_kinds": kinds,
                        "has_execution_or_model": closing, "statements": [{"slot": s, "id": statements[s]["id"], "freshness": statements[s]["freshness"]} for s in cited],
                        "stale_statements": stale_statements, "execution_subject_drift": subject_drift,
                        "blockers": blockers, "active_blockers": [b["gate"] for b in blockers if b["active"]]})
    return reports


def check_obligation_citations(ledger, statements):
    reports = {}
    for obligation in ledger["caller_obligations"]:
        cited = cited_slots(obligation, statements, obligation["id"])
        reports[obligation["id"]] = [s for s in cited if statements[s]["freshness"] != "fresh"]
    return reports


def check_review_items(ledger):
    items = ledger.get("review_items", [])
    obligations = {o["id"] for o in ledger["caller_obligations"]}
    if not isinstance(items, list):
        raise ValueError("review_items must be a list")
    for item in items:
        if not isinstance(item, dict) or any(not nonempty_text(item.get(f)) for f in ("id", "source", "item", "status", "current")) or item.get("obligation") not in obligations:
            raise ValueError("malformed review item")
    return [{"id": i["id"], "obligation": i["obligation"], "status": i["status"]} for i in items]


class Parser(argparse.ArgumentParser):
    def error(self, message):
        # Review L5: usage errors are invalid input (3), never "open" (2).
        self.print_usage(sys.stderr)
        self.exit(3, "%s: error: %s\n" % (self.prog, message))


def main():
    parser = Parser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--ledger", type=Path, default=Path(__file__).with_name("COMPOSITION.json"))
    parser.add_argument("--repo", action="append", default=[], metavar="NAME=CHECKOUT")
    parser.add_argument("--spec", action="append", default=[], metavar="NAME=FILE")
    parser.add_argument("--evidence-root", type=Path, metavar="DIR",
                        help="directory holding implementation/ (retained run and statement evidence)")
    parser.add_argument("--strict", action="store_true", help="also refuse open/unpinned/incomplete rows, active blockers and open obligations; source matches alone never close a row")
    args = parser.parse_args()
    repositories = {"keep-core": Path(__file__).resolve().parents[3]}
    evidence_root = args.evidence_root.resolve() if args.evidence_root else None
    try:
        for value in args.repo:
            name, path = value.split("=", 1)
            repositories[name] = Path(path)
        ledger = json.loads(args.ledger.read_text())
        if ledger.get("format") != FORMAT:
            raise ValueError("unsupported ledger format")
        unresolved_obligations = check_obligations(ledger, repositories, evidence_root)
        specs = {}
        for value in args.spec:
            name, path = value.split("=", 1)
            if name in specs:
                raise ValueError("duplicate specification input")
            specs[name] = Path(path)
        specification_reports = check_specs(ledger, specs)
        statements = check_statements(ledger, repositories, evidence_root)
        reports = check_rows(ledger, repositories, evidence_root, statements)
        obligation_statements = check_obligation_citations(ledger, statements)
        review_items = check_review_items(ledger)
    except (ValueError, KeyError, TypeError, AttributeError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(3, "freshness error: %s\n" % error)
    stale_statements = sorted({s["id"] for s in statements.values() if s["freshness"] != "fresh"})
    print(json.dumps({"scope": "selected source/spec content, cited component statement freshness and historical evidence references only; not a model or semantic discharge result",
                      "component_statements": list(statements.values()), "stale_statements": stale_statements,
                      "rows": reports, "obligation_stale_statements": {k: v for k, v in obligation_statements.items() if v},
                      "specifications": specification_reports, "unresolved_obligations": unresolved_obligations,
                      "review_items": review_items}, indent=2))
    if any(row["mismatches"] for row in reports) or any(not spec["matches"] for spec in specification_reports) or stale_statements:
        return 1
    if args.strict and (unresolved_obligations or any(row["status"] != "discharged" or row["unpinned_sides"] or row["dependency_coverage"] != "complete" or row["unassigned_owner"] or row["remaining_open"] or row["missing_joining_evidence"] or not row["has_execution_or_model"] or row["active_blockers"] or row["execution_subject_drift"] for row in reports)):
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
