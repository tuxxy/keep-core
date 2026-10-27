#!/usr/bin/env python3
"""Focused round-five owning assertion controls; not K09b."""
import importlib.util,json,os,sys,tempfile,time
from pathlib import Path
sp=importlib.util.spec_from_file_location('k09',Path(__file__).with_name('k09-local.py'));m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
os.umask(0o077);root=Path(sys.argv[1]);root.mkdir(mode=0o700,parents=True,exist_ok=False)
env=m.local_environment(os.environ);env['GOMAXPROCS']='8'
cases=[
 ('IncompleteGrant','pkg/frost/signing/executor.go','{signingGrantID(id), signingDoneID(id)}','{signingDoneID(id)}','./pkg/frost/signing','TestPossibleReleaseIncludesGrantWithoutDone'),
 ('InterruptedClaim','pkg/frost/signing/executor.go','claimed, err := claims.HasClaim(ctx, id)','claimed, err := claims.HasClaim(ctx, id); claimed = false','./pkg/frost/signing','TestPossibleReleaseIncludesInterruptedClaim'),
 ('IncomingCeiling','pkg/frost/node/recipe.go','if ceiling <= 0 ||','if false && (ceiling <= 0) || false &&','./pkg/frost/node','TestIncomingRecipeEnforcesLocalCeiling'),
 ('DrainBudget','pkg/tbtc/moving_funds.go','p.RemainingCustody >= balance','true','./pkg/tbtcpg','TestBudgetExhaustionFallsBackBeforeCutover'),
 ('HostTimeoutReset','pkg/tbtcpg/moving_funds.go','recovery.ResetLegacyMovingFundsTimeout(source, main)','func() error { _ = recovery; return nil }()','./pkg/tbtcpg','TestHostSubmitsDueLegacyReset'),
 ('NodeDeadline','cmd/frostlocalnode.go','if errors.Is(ctx.Err(), context.DeadlineExceeded) {','if false && errors.Is(ctx.Err(), context.DeadlineExceeded) {','./cmd','TestFrostLocalDeadlineIsFailure'),
]
r={'status':'running','kind':'focused owning controls','limits':{'cores':8,'wallSecondsPerCommand':120},'controls':[]}
def save():(root/'result.json').write_text(json.dumps(r,indent=2))
save()
with tempfile.TemporaryDirectory(prefix='k09-r5-overlay-') as temp:
 for name,relative,before,after,pkg,test in cases:
  item={'guard':name,'status':'running'};r['controls'].append(item);save()
  positive=root/(name+'-positive.log');code=m.run_command(['go','test','-p','2',pkg,'-run','^'+test+'$','-count=1','-timeout','2m'],m.KC,env,positive,timeout=120);m.require_exit(code);m.require_public_output(positive.read_text())
  source=m.KC/relative;original=source.read_text();assert original.count(before)==1,(name,original.count(before));altered=Path(temp)/(name+'.go');altered.write_text(original.replace(before,after));overlay=Path(temp)/(name+'.json');overlay.write_text(json.dumps({'Replace':{str(source):str(altered)}}))
  mutated=dict(env);mutated['GOFLAGS']='-mod=readonly -overlay='+str(overlay)
  log=root/(name+'-mutant.log');code=m.run_command(['go','test','-p','2',pkg,'-run','^'+test+'$','-count=1','-timeout','2m'],m.KC,mutated,log,timeout=120);text=log.read_text();m.require_public_output(text)
  assert code!=0 and 'control '+name+' assertion' in text and '[build failed]' not in text and 'panic:' not in text and 'test timed out' not in text,name
  item['status']='killed at owning assertion';save();print(name+': killed at owning assertion',flush=True)
r['status']='pass';save()
