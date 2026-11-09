#!/usr/bin/env python3
"""Freshness check for host verification safety statements (KC-VER V-00).

V-08 copy of pkg/frost/store/verification/check-freshness.py (branch
verification/v0102-repin-2026-11-06, the package-manifest design), extended
with "external" pins for the other repositories a V-08 statement depends on
(tbtc-v2 contracts, the KC-VER and KC-SF-V3 specs). Each external pin names a
repository key, a ref, a path and a blob; it is fresh when the blob at the
ref equals the pin. Repository paths come from the statement's "repositories"
map and can be overridden with --repo KEY=PATH.

A statement in STATEMENTS.json pins its sources in two ways:

- "pins": the git blob hash of single files (model sources, the replay
  harness, the lockfile). A pin is fresh when the file's current blob equals it.
- "manifests" (since S-K02-HOST-4 / S-K03-STORE-3, issue tuxxy/keep-core#75):
  a directory, its kind, and the sorted list of every matching file in it with
  its blob. Kind "go-package" means every non-test "*.go" file directly in the
  directory; kind "rust-src" means every "*.rs" file directly in it. A manifest
  is fresh only when the current file list is exactly the pinned list and
  every blob matches, so adding, deleting or renaming a file makes the
  statement stale, not only changing a pinned one.

A stale statement must not be cited until its model and replay rerun on the
current source. A statement with "superseded_by" is reported as superseded and
is not checked or cited.

Usage, from the repository root or any subdirectory:
  python3 pkg/frost/store/verification/check-freshness.py [--statements PATH]
      [--rev REV | --root DIR] [--print-manifest DIR KIND] [--repo KEY=PATH]
--rev checks a committed revision instead of the working tree. --root checks a
plain directory tree (for example a scratch copy) instead of the enclosing git
work tree. --print-manifest prints the current manifest of one directory as
JSON, for writing a new statement.
Exit code 0: every statement fresh. 1: at least one stale. 2: usage error.
"""
import argparse, json, os, subprocess, sys

KINDS = {
    "go-package": lambda n: n.endswith(".go") and not n.endswith("_test.go"),
    "rust-src": lambda n: n.endswith(".rs"),
}


def blob(path, rev):
    if rev:
        r = subprocess.run(["git", "rev-parse", f"{rev}:{path}"], capture_output=True, text=True)
    else:
        if not os.path.isfile(path):
            return None
        r = subprocess.run(["git", "hash-object", "--no-filters", path], capture_output=True, text=True)
    return r.stdout.strip() if r.returncode == 0 else None


def listing(directory, kind, rev):
    """Sorted names of the files of this kind directly in directory."""
    match = KINDS[kind]
    if rev:
        r = subprocess.run(["git", "ls-tree", "--name-only", f"{rev}:{directory}"], capture_output=True, text=True)
        names = r.stdout.split() if r.returncode == 0 else []
        # ls-tree lists subdirectories too; keep blobs only.
        names = [n for n in names if subprocess.run(["git", "cat-file", "-t", f"{rev}:{directory}/{n}"], capture_output=True, text=True).stdout.strip() == "blob"]
    else:
        names = [n for n in os.listdir(directory) if os.path.isfile(os.path.join(directory, n))] if os.path.isdir(directory) else []
    return sorted(n for n in names if match(n))


def manifest(directory, kind, rev):
    return {n: blob(f"{directory}/{n}", rev) for n in listing(directory, kind, rev)}


def external(st, repos):
    bad = []
    for pin in st.get("external", []):
        repo = repos.get(pin["repository"]) or st.get("repositories", {}).get(pin["repository"])
        if not repo or not os.path.isdir(repo):
            bad.append(f"{pin['repository']}:{pin['path']}: repository not found")
            continue
        r = subprocess.run(["git", "-C", repo, "rev-parse", f"{pin['ref']}:{pin['path']}"], capture_output=True, text=True)
        current = r.stdout.strip() if r.returncode == 0 else None
        if current != pin["blob"]:
            bad.append(f"{pin['repository']}:{pin['ref']}:{pin['path']}: pinned {pin['blob'][:9]} current {(current or 'missing')[:9]}")
    return bad


def check(st, rev):
    bad = []
    for path, pinned in st.get("pins", {}).items():
        current = blob(path, rev)
        if current != pinned:
            bad.append(f"{path}: pinned {pinned[:9]} current {(current or 'missing')[:9]}")
    for directory, m in st.get("manifests", {}).items():
        kind = m["kind"]
        if kind not in KINDS:
            bad.append(f"{directory}: unknown manifest kind {kind}")
            continue
        pinned = m["files"]
        current = manifest(directory, kind, rev)
        for n in sorted(set(current) - set(pinned)):
            bad.append(f"{directory}/{n}: added (not in the pinned manifest)")
        for n in sorted(set(pinned) - set(current)):
            bad.append(f"{directory}/{n}: deleted (pinned {pinned[n][:9]})")
        for n in sorted(set(pinned) & set(current)):
            if pinned[n] != current[n]:
                bad.append(f"{directory}/{n}: pinned {pinned[n][:9]} current {(current[n] or 'missing')[:9]}")
    return bad


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--statements", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "STATEMENTS.json"))
    g = ap.add_mutually_exclusive_group()
    g.add_argument("--rev", help="check against a committed revision instead of the working tree")
    g.add_argument("--root", help="check a plain directory tree instead of the enclosing git work tree")
    ap.add_argument("--print-manifest", nargs=2, metavar=("DIR", "KIND"))
    ap.add_argument("--repo", action="append", default=[], metavar="KEY=PATH")
    a = ap.parse_args()
    try:
        statements = json.load(open(a.statements))
    except Exception as e:
        print(f"cannot read statements: {e}", file=sys.stderr)
        return 2
    if a.root:
        root = a.root
    else:
        root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip()
    if not root or not os.path.isdir(root):
        print("cannot find the tree root", file=sys.stderr)
        return 2
    os.chdir(root)
    if a.print_manifest:
        d, kind = a.print_manifest
        if kind not in KINDS:
            print(f"unknown kind {kind}", file=sys.stderr)
            return 2
        print(json.dumps({d: {"kind": kind, "files": manifest(d, kind, a.rev)}}, indent=2))
        return 0
    stale = 0
    for sid, st in statements.items():
        if st.get("superseded_by"):
            # A superseded statement is never cited; its successor carries the claim.
            print(f"{sid}: superseded by {st['superseded_by']}")
            continue
        bad = check(st, a.rev) + external(st, dict(x.split("=", 1) for x in a.repo))
        if bad:
            stale += 1
        print(f"{sid}: {'stale' if bad else 'fresh'} ({st.get('scope', '')})")
        for line in bad:
            print(f"  {line}")
    return 1 if stale else 0


if __name__ == "__main__":
    sys.exit(main())
