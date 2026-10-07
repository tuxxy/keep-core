#!/usr/bin/env python3
"""Freshness check for host verification safety statements (KC-VER V-00).

Each statement in STATEMENTS.json pins the git blob hash of every source file
it depends on. A statement is fresh only when every pinned blob equals the blob
of that path in the current tree. A stale statement must not be cited until
its model and replay rerun on the current source.

Usage, from the repository root or any subdirectory:
  python3 pkg/frost/store/verification/check-freshness.py [--statements PATH] [--rev REV]
Exit code 0: every statement fresh. 1: at least one stale. 2: usage error.
"""
import argparse, json, os, subprocess, sys

def blob(path, rev):
    if rev:
        r = subprocess.run(["git", "rev-parse", f"{rev}:{path}"], capture_output=True, text=True)
    else:
        r = subprocess.run(["git", "hash-object", path], capture_output=True, text=True)
    return r.stdout.strip() if r.returncode == 0 else None

def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--statements", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "STATEMENTS.json"))
    ap.add_argument("--rev", help="check against a committed revision instead of the working tree")
    a = ap.parse_args()
    try:
        statements = json.load(open(a.statements))
    except Exception as e:
        print(f"cannot read statements: {e}", file=sys.stderr)
        return 2
    root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip()
    os.chdir(root)
    stale = 0
    for sid, st in statements.items():
        bad = []
        for path, pinned in st["pins"].items():
            current = blob(path, a.rev)
            if current != pinned:
                bad.append((path, pinned[:9], (current or "missing")[:9]))
        status = "stale" if bad else "fresh"
        if bad:
            stale += 1
        print(f"{sid}: {status} ({st.get('scope', '')})")
        for path, pinned, current in bad:
            print(f"  {path}: pinned {pinned} current {current}")
    return 1 if stale else 0

if __name__ == "__main__":
    sys.exit(main())
