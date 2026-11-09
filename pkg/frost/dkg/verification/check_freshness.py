#!/usr/bin/env python3
"""V-03 freshness check. Usage: check_freshness.py [pins.json]

Default pins: evidence/source-pins-2026-11-06.json (S-K03-CHAIN-2).
A pin is either a file (path relative to the keep-core root, or absolute)
compared by SHA-256, or a git blob in another repository
({"repo", "rev", "path", "git_blob"}) compared with `git rev-parse rev:path`,
so specification and client pins never depend on a copy under /tmp.
"""
import hashlib, json, pathlib, subprocess, sys

root = pathlib.Path(__file__).resolve().parents[4]
default = root / 'pkg/frost/dkg/verification/evidence/source-pins-2026-11-06.json'
pins = json.loads(pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else default).read_text())
stale = []
for item in pins['files']:
    if 'repo' in item:
        blob = subprocess.run(['git', '-C', item['repo'], 'rev-parse', item['rev'] + ':' + item['path']],
                              capture_output=True, text=True).stdout.strip()
        if blob != item['git_blob']:
            stale.append(item['repo'] + '@' + item['rev'] + ':' + item['path'])
        continue
    p = pathlib.Path(item['path'])
    p = p if p.is_absolute() else root / p
    if not p.exists() or hashlib.sha256(p.read_bytes()).hexdigest() != item['sha256']:
        stale.append(item['path'])
print(json.dumps({'statement': pins.get('statement'), 'fresh': not stale, 'stale': stale,
                  'scope': 'Exact V03 production, tooling, client, fixture and specification pins; scope remains bounded and conditional'}, indent=2))
sys.exit(bool(stale))
