#!/usr/bin/env python3
"""Check selected V-05 per-row source pins; never discharge a composition row."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess

REQUIRED_OBLIGATIONS = {
    "A-K04-AGREED-EXECUTOR-CONFIG", "A-GUARDED-END-BEFORE-RELEASE",
    "A-READY-FRESH-RELOAD", "A-BUILDER-FROZEN-BEFORE-SIGN", "A-CHAIN-FINALITY",
    "A-FENCE-STORAGE", "A-OPERATOR-ATTEST-AFTER-VALIDATION",
    "A-ONE-INTENT-PER-STATE-VERSION", "A-STORED-DESCRIPTOR-ONLY",
    "A-FEE-INCLUSION-ESCALATION", "A-DUPLICATE-REQUEST-STATE-GUARD",
    "A-READY-SEAT-ELIGIBILITY", "A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION",
    "A-SPV-MAINTAINER-RELAY", "A-K06-CANONICAL-SWEEP-BUILDER",
    "A-K05-TIMEOUT-POLICY", "A-K05-LATE-SIGNATURE-SETTLEMENT",
    "A-K05-CERTIFICATE-TRANSPORT",
}


def object_hash(value):
    return isinstance(value, str) and bool(re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", value))


def nonempty_text(value):
    return isinstance(value, str) and bool(value.strip())


def valid_reference(source):
    return isinstance(source, dict) and nonempty_text(source.get("repository")) and nonempty_text(source.get("path")) and object_hash(source.get("revision")) and object_hash(source.get("blob"))


def verify_reference(source, repositories):
    """Verify historical identity only; matching hashes do not prove semantics."""
    if not valid_reference(source):
        raise ValueError("reference requires non-null path/revision/blob provenance")
    repo = source["repository"]
    if repo not in repositories:
        raise ValueError("provide --repo %s=/path/to/checkout" % repo)
    root = repositories[repo].resolve()
    relative = PurePosixPath(source["path"])
    if relative.is_absolute() or ".." in relative.parts:
        raise ValueError("source path must be repository-relative")
    try:
        # A blob/tree object is not an acceptable substitute for a commit.
        kind = subprocess.check_output(
            ["git", "-C", str(root), "cat-file", "-t", source["revision"]],
            text=True, stderr=subprocess.PIPE,
        ).strip()
        if kind != "commit":
            raise ValueError("reference revision must identify a commit")
        actual = subprocess.check_output(
            ["git", "-C", str(root), "rev-parse", "--verify",
             source["revision"] + ":" + source["path"]],
            text=True, stderr=subprocess.PIPE,
        ).strip()
        kind = subprocess.check_output(
            ["git", "-C", str(root), "cat-file", "-t", actual],
            text=True, stderr=subprocess.PIPE,
        ).strip()
    except subprocess.CalledProcessError as error:
        raise ValueError("unresolvable reference %s:%s:%s" %
                         (repo, source["revision"], source["path"])) from error
    if kind != "blob" or actual != source["blob"]:
        raise ValueError("revision/path blob mismatch for %s:%s:%s" %
                         (repo, source["revision"], source["path"]))
    return root


def verify_evidence(evidence, repositories, target=None):
    if not isinstance(evidence, list):
        raise ValueError("evidence must be a list")
    for reference in evidence:
        if not valid_reference(reference) or not nonempty_text(reference.get("scope")):
            raise ValueError("malformed evidence provenance")
        if target is not None and not re.search(r"(?<![A-Za-z0-9-])" + re.escape(target) + r"(?![A-Za-z0-9-])", reference["scope"]):
            raise ValueError("evidence scope must identify " + target)
        verify_reference(reference, repositories)


def check_obligations(ledger, repositories=None):
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
        verify_evidence(evidence, repositories, obligation["id"])
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


def check_rows(ledger, repositories):
    if ledger.get("format") != 2:
        raise ValueError("unsupported ledger format")
    rows = ledger.get("rows", [])
    if [row.get("id") for row in rows] != list(range(1, 11)):
        raise ValueError("ledger must contain all ten composition rows in order")
    reports = []
    for row in rows:
        for field in ("status", "producer_result", "consumer_precondition", "model_links", "joining_evidence", "owner", "remaining"):
            if field not in row:
                raise ValueError("row %s missing %s" % (row["id"], field))
        if row["status"] not in ("open", "blocked", "discharged") or any(not nonempty_text(row[field]) for field in ("producer_result", "consumer_precondition", "dependency_coverage")) or not isinstance(row["owner"], dict):
            raise ValueError("malformed composition row")
        if any(not isinstance(row[field], list) for field in ("model_links", "joining_evidence", "remaining")) or any(not nonempty_text(value) for value in row["model_links"] + row["remaining"]):
            raise ValueError("malformed composition evidence/model lists")
        verify_evidence(row["joining_evidence"], repositories, "ROW-%s" % row["id"])
        mismatches = []
        unpinned = []
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
                path = (root / source["path"]).resolve()
                path.relative_to(root)
                if path.is_file():
                    actual = subprocess.check_output(
                        ["git", "-C", str(root), "hash-object", "--no-filters", "--", str(path)],
                        text=True,
                    ).strip()
                else:
                    actual = None
                if actual is None or actual != source["blob"]:
                    mismatches.append({"side": side, "repository": repo, "path": source["path"], "expected": source["blob"], "actual": actual})
        assigned = nonempty_text(row["owner"].get("person")) and row["owner"].get("assignment_status") == "assigned"
        selected = "stale" if mismatches else ("unavailable" if len(unpinned) == 2 else "partial-match" if unpinned else "match")
        reports.append({"row": row["id"], "status": row["status"], "selected_pins": selected, "unpinned_sides": unpinned, "mismatches": mismatches, "dependency_coverage": row["dependency_coverage"], "unassigned_owner": not assigned, "remaining_open": bool(row["remaining"]), "missing_joining_evidence": not row["joining_evidence"]})
    return reports


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ledger", type=Path, default=Path(__file__).with_name("COMPOSITION.json"))
    parser.add_argument("--repo", action="append", default=[], metavar="NAME=CHECKOUT")
    parser.add_argument("--spec", action="append", default=[], metavar="NAME=FILE")
    parser.add_argument("--strict", action="store_true", help="also refuse open/unpinned/incomplete rows; source matches alone never close a row")
    args = parser.parse_args()
    repositories = {"keep-core": Path(__file__).resolve().parents[3]}
    try:
        for value in args.repo:
            name, path = value.split("=", 1)
            repositories[name] = Path(path)
        ledger = json.loads(args.ledger.read_text())
        unresolved_obligations = check_obligations(ledger, repositories)
        specs = {}
        for value in args.spec:
            name, path = value.split("=", 1)
            if name in specs:
                raise ValueError("duplicate specification input")
            specs[name] = Path(path)
        specification_reports = check_specs(ledger, specs)
        reports = check_rows(ledger, repositories)
    except (ValueError, KeyError, TypeError, AttributeError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(3, "freshness error: %s\n" % error)
    print(json.dumps({"scope": "selected source/spec content and historical source/evidence references only; not a model or semantic discharge result", "rows": reports, "specifications": specification_reports, "unresolved_obligations": unresolved_obligations}, indent=2))
    if any(row["mismatches"] for row in reports) or any(not spec["matches"] for spec in specification_reports):
        return 1
    if args.strict and (unresolved_obligations or any(row["status"] != "discharged" or row["unpinned_sides"] or row["dependency_coverage"] != "complete" or row["unassigned_owner"] or row["remaining_open"] or row["missing_joining_evidence"] for row in reports)):
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
