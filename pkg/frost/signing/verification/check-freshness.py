#!/usr/bin/env python3
"""Check statement pins against the actual files, never just Git HEAD.

Pass the statement's source-manifest.json (or set V04_OUT to its evidence
directory). There is no default: the in-repository 13 October manifest is
historical and always stale. A git_blobs entry is checked in its own
repository: the pinned commit must hold the blob, and so must the tracked ref
(for example tuxxy/integration/frost-base), so a later change there is stale.
"""
import hashlib,json,os,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parents[4]
if len(sys.argv)>1:manifest=pathlib.Path(sys.argv[1])
elif os.environ.get('V04_OUT'):manifest=pathlib.Path(os.environ['V04_OUT'])/'source-manifest.json'
else:raise SystemExit('usage: check-freshness.py <source-manifest.json> (or V04_OUT=<evidence dir>)')
m=json.loads(manifest.read_text());bad=[]
for group in ('files','dependency_files','verification_files','external'):
 for name,digest in m.get(group,{}).items():
  p=pathlib.Path(name);p=p if p.is_absolute() else root/p
  if not p.is_file() or hashlib.sha256(p.read_bytes()).hexdigest()!=digest:bad.append({'group':group,'path':name})
for name,b in m.get('git_blobs',{}).items():
 for ref in (b['commit'],b.get('track',b['commit'])):
  r=subprocess.run(['git','-C',b['repo'],'rev-parse',ref+':'+b['path']],capture_output=True,text=True)
  if r.returncode!=0 or r.stdout.strip()!=b['blob']:bad.append({'group':'git_blobs','path':name,'ref':ref});break
print(json.dumps({'status':'stale' if bad else 'fresh','subject':m['head'],'mismatches':bad},indent=2))
raise SystemExit(1 if bad else 0)
