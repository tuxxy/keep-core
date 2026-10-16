#!/usr/bin/env python3
"""Check statement pins against the actual files, never just Git HEAD."""
import hashlib,json,os,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[4]
manifest=pathlib.Path(os.environ.get('V04_OUT',str(root/'pkg/frost/signing/verification/evidence/V-04-2026-10-13')))/'source-manifest.json'
if len(sys.argv)>1:manifest=pathlib.Path(sys.argv[1])
m=json.loads(manifest.read_text());bad=[]
for group in ('files','dependency_files','verification_files','external'):
 for name,digest in m.get(group,{}).items():
  p=pathlib.Path(name);p=p if p.is_absolute() else root/p
  if not p.is_file() or hashlib.sha256(p.read_bytes()).hexdigest()!=digest:bad.append({'group':group,'path':name})
print(json.dumps({'status':'stale' if bad else 'fresh','subject':m['head'],'mismatches':bad},indent=2))
raise SystemExit(1 if bad else 0)
