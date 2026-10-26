#!/usr/bin/env python3
"""Focused node guards via disposable Go overlays; not the final campaign."""
import argparse
import json
import os
from pathlib import Path
import runpy
import subprocess
import tempfile
import time

KC=Path(__file__).resolve().parents[2]
RUNNER=runpy.run_path(str(KC/'test/frost/k09-local.py'))

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('evidence',type=Path)
 parser.add_argument('--firewall',action='store_true',help='one isolated Core/tss integration guard control')
 args=parser.parse_args();root=args.evidence.resolve();root.mkdir(mode=0o700,parents=True,exist_ok=False)
 os.umask(0o077)
 env=RUNNER['local_environment'](os.environ);env['GOMAXPROCS']='8'
 cases=[
 ('NodeLocalGate','pkg/frost/node/node.go','if !c.LocalRegtest ||','TestNodeLocalGatePrecedesCredentials','./pkg/frost/node'),
 ('NodeBootstrapGate','pkg/frost/node/node.go','if !strings.HasPrefix(peer,','TestNodeBootstrapGatePrecedesCredentials','./pkg/frost/node'),
 ('NodeReloadCandidate','pkg/frost/node/reload.go','if hash != a.record.DescriptorHash','TestNodeReloadRequiresOriginalApproval','./pkg/frost/node'),
 ('NodeReloadApproval','pkg/frost/node/reload.go','if view.ApprovedID != a.record.ID','TestNodeReloadRequiresOriginalApproval','./pkg/frost/node'),
 ('CoreRegtestDomain','pkg/bitcoin/bitcoincore/local.go','if genesis != [32]byte','TestLocalCoreDomainAndKnownTransaction','./pkg/bitcoin/bitcoincore'),
 ('CoreKnownTransaction','pkg/bitcoin/bitcoincore/local.go','if other.Deserialize(known) == nil','TestLocalCoreDomainAndKnownTransaction','./pkg/bitcoin/bitcoincore'),
 ]
 if args.firewall:cases.append(('NodeFirewall','pkg/frost/node/node.go','n.private, n.policy, retransmission.NewTimeTicker','TestFrostNodeProcessesDKGAndDispatch','./pkg/frost/integration'))
 record={'scope':'focused owning guards, not K09b','limits':{'cores':8,'unitWallSeconds':120,'firewallWallSeconds':900},'controls':[],'status':'running'}
 def save():(root/'result.json').write_text(json.dumps(record,indent=2)+'\n')
 save()
 positive=['go','test','-p','2','./pkg/frost/node','./pkg/bitcoin/bitcoincore','-count=1','-timeout','2m']
 code=RUNNER['run_command'](positive,KC,env,root/'positive.log',timeout=120);RUNNER['require_exit'](code)
 with tempfile.TemporaryDirectory(prefix='k09-overlay-')as tmp:
  tmp=Path(tmp)
  for name,relative,needle,test,package in cases:
   source=KC/relative;original=source.read_text();lines=original.splitlines();matches=[i for i,line in enumerate(lines)if needle in line]
   if len(matches)!=1:raise RuntimeError('guard selector is ambiguous: '+name)
   index=matches[0]
   if name=='NodeFirewall':lines[index]=lines[index].replace('n.private, n.policy,','n.private, firewall.Disabled,')
   else:
    line=lines[index];assert line.rstrip().endswith('{');condition=line.strip()[3:-1].strip();lines[index]=line[:len(line)-len(line.lstrip())]+'if false && ('+condition+') {'
   altered=tmp/(name+'.go');altered.write_text('\n'.join(lines)+'\n')
   overlay=tmp/(name+'.json');overlay.write_text(json.dumps({'Replace':{str(source):str(altered)}}))
   current=dict(env);current['GOFLAGS']='-mod=readonly -overlay='+str(overlay)
   item={'guard':name,'status':'running','wallLimitSeconds':900 if name=='NodeFirewall' else 120};record['controls'].append(item);save()
   start=time.monotonic();log=root/(name+'.log')
   code=RUNNER['run_command'](['go','test','-p','2',package,'-run','^'+test+'$','-count=1','-timeout','14m' if name=='NodeFirewall' else '2m'],KC,current,log,timeout=item['wallLimitSeconds'])
   text=log.read_text();expected='control '+name+' assertion'
   if code==0 or expected not in text or '[build failed]'in text or 'panic:'in text or 'test timed out'in text:raise RuntimeError('guard did not fail at owning assertion: '+name)
   item.update(status='killed at owning assertion',elapsedSeconds=round(time.monotonic()-start,2));save();print(name+': killed at owning assertion',flush=True)
 record['status']='pass';save();return 0
if __name__=='__main__':raise SystemExit(main())
