#!/usr/bin/env python3
import hashlib,json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[4]
pins=json.loads((root/'pkg/frost/dkg/verification/evidence/source-pins.json').read_text())
stale=[]
for item in pins['files']:
 p=pathlib.Path(item['path']);p=p if p.is_absolute() else root/p
 if not p.exists() or hashlib.sha256(p.read_bytes()).hexdigest()!=item['sha256']:stale.append(item['path'])
print(json.dumps({'fresh':not stale,'stale':stale,'scope':'Exact V03 production, tooling, client, fixture and specification pins; scope remains bounded and conditional'},indent=2))
sys.exit(bool(stale))
