#!/usr/bin/env python3
"""Source-bound scoped K-09 checks; statuses never imply full acceptance."""
import argparse,importlib.util,json,os,subprocess,time
from pathlib import Path
sp=importlib.util.spec_from_file_location('k09',Path(__file__).with_name('k09-local.py'));m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
p=argparse.ArgumentParser();p.add_argument('evidence',type=Path);p.add_argument('kind',choices=['unit','unit-production','go','contract','compile','build']);p.add_argument('items',nargs='*');a=p.parse_args()
os.umask(0o077);root=a.evidence.resolve();root.mkdir(mode=0o700,parents=True,exist_ok=False);private=root/'private';private.mkdir(mode=0o700)
env=m.local_environment(os.environ);env['GOMAXPROCS']='8';env['GOFLAGS']='-mod=readonly';env['NODE_OPTIONS']='--max-old-space-size=8192';env['FROST_K09_NODE_LOG_ROOT']=str(private/'nodes');env['FROST_C04_LEGACY_PUBLIC_EVIDENCE']=str(private/'legacy')
if a.kind in ('unit','go'):env['GOFLAGS']+=' -tags=frosttest'
tb=Path(env['FROST_K03_TBTC']);hh=str(tb/'solidity/node_modules/.bin/hardhat');cwd=m.KC
if a.kind=='compile':cmd=[hh,'--config','hardhat.k09.config.ts','--network','hardhat','compile','--force'];cwd=tb/'solidity'
elif a.kind=='contract':cmd=m.hardhat_test(hh,*a.items[1:]);cwd=tb/'solidity'
elif a.kind=='go':cmd=['go','test','-json','-tags','frosttest','-p','2','./pkg/frost/integration','-run','^('+'|'.join(a.items)+')$','-count=1','-timeout','14m']
elif a.kind in ('unit','unit-production'):cmd=['go','test','-json','-tags','frosttest','-p','2',*a.items,'-count=1','-timeout','8m']
else:cmd=['go','build','-p','2','./...']
if a.kind=='unit-production':cmd=[x for x in cmd if x not in ('-tags','frosttest')]
r={'status':'running','scope':'focused concrete check; not full acceptance','command':cmd,'cwd':str(cwd),'limits':{'cores':8,'wallSeconds':900}};m.public_json(root/'result.json',r)
try:
 sources=m.source_snapshot(tb,env);m.public_json(root/'sources.json',sources);r['sourcesSha256']=m.sha(root/'sources.json');r['heads']={k:sources[k]['head'] for k in ('keep-core','tbtc-v2')};r['worktreeStatus']={k:subprocess.check_output(['git','status','--porcelain'],cwd=sources[k]['root'],text=True) for k in r['heads']}
 if a.kind!='compile':
  artifacts=m.artifact_binding(tb,env['FROST_C04_LEGACY_BASELINE_BUILD']);m.public_json(root/'artifacts.json',artifacts);r['artifactsSha256']=m.sha(root/'artifacts.json')
 m.public_json(root/'result.json',r);start=time.monotonic();code=m.run_command(cmd,cwd,env,private/'run.log',timeout=900);r.update(exitCode=code,elapsedSeconds=round(time.monotonic()-start,2));m.require_exit(code);output=(private/'run.log').read_text();m.require_public_output(output)
 if a.kind=='go':m.require_go(output,a.items);r['cases']=a.items
 if a.kind=='contract':m.require_mocha(output,int(a.items[0]));r['contractCases']=int(a.items[0])
 m.require_sources_unchanged(sources,m.source_snapshot(tb,env))
 after=m.artifact_binding(tb,env['FROST_C04_LEGACY_BASELINE_BUILD'])
 if a.kind!='compile':m.require_sources_unchanged(artifacts,after)
 else:m.public_json(root/'artifacts.json',after);r['artifactsSha256']=m.sha(root/'artifacts.json')
 m.require_private_evidence(private/'nodes');r['status']='pass'
except Exception as e:
 r['status']='fail';(private/'error.txt').write_text(type(e).__name__+': '+str(e))
r['privateFiles']={str(f.relative_to(private)):{'sha256':m.sha(f),'bytes':f.stat().st_size} for f in sorted(private.rglob('*')) if f.is_file()};m.public_json(root/'result.json',r);print(json.dumps({k:v for k,v in r.items() if k not in ('worktreeStatus','privateFiles')},indent=2),flush=True);raise SystemExit(0 if r['status']=='pass' else 1)
