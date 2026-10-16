#!/usr/bin/env python3
"""Finite local runner. Pins clients/tools; kills the whole command group on timeout."""
import datetime,hashlib,json,os,pathlib,signal,subprocess,sys,time
ROOT=pathlib.Path(__file__).resolve().parents[4]; OUT=ROOT/'pkg/frost/dkg/verification/evidence'
CLIENT='/Users/tux/threshold/integration-worktrees/K-00-SF-01-K-01/snowfall/clients/go'
RUST='/Users/tux/.rustup/toolchains/1.94.0-aarch64-apple-darwin/bin'
base=['go','test','-modfile=.v03-cache/verification.mod']
commands={'model':['cargo','run','--offline','--locked','--release','--manifest-path','pkg/frost/dkg/verification/model/Cargo.toml','--','pkg/frost/dkg/verification/evidence']}
for name,packages,pattern in [('normal',['./pkg/frost/...'],None),('replay',['./pkg/frost/dkg/verification/replay'],'TestV03SubprocessReplay'),('projection',['./pkg/frost/dkg'],'TestV03'),('integration',['./pkg/frost/dkg','./pkg/frost/integration'],'TestLostSeatBeforeReadinessExpiresWallet|TestLostSeatAfterReadinessCertifiesUnderPolicy|TestPendingWalletExpiryThenFreshEpoch|TestFinishSuccessRetainsClassifiedObserverFailure|TestMonitorReconcilesOverlappingApprovalObservation|TestMonitorPublicationAfterObservationSnapshot')]:
 command=base+packages+(['-run',pattern] if pattern else [])+['-count=1','-timeout=300s','-v'];commands[name]=command;commands['race' if name=='normal' else name+'-race']=base[:2]+['-race']+command[2:]
name=sys.argv[1]; cache=ROOT/'.v03-cache';cache.mkdir(exist_ok=True)
(cache/'verification.mod').write_text((ROOT/'go.mod').read_text().replace('../snowfall/clients/go',CLIENT));(cache/'verification.sum').write_bytes((ROOT/'go.sum').read_bytes())
env=os.environ.copy();env.update(PATH=RUST+':'+env['PATH'],RUSTC=RUST+'/rustc',GOCACHE=str(cache/'build'),GOPROXY='off')
if name.startswith('replay'):env['V03_REPLAY_REPORT']=str(OUT/(name+'-report.json'))
source_files=[ROOT/'pkg/frost/dkg/executor.go',ROOT/'pkg/frost/dkg/outcome_test.go',ROOT/'pkg/frost/dkg/verification/model/src/main.rs',ROOT/'pkg/frost/dkg/verification/replay/replay_test.go',ROOT/'pkg/frost/dkg/verification/run.py']
source_hashes={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in source_files}
limit=600;start=datetime.datetime.now(datetime.timezone.utc).isoformat();clock=time.monotonic()
with (OUT/(name+'-retained.log')).open('w') as log:
 proc=subprocess.Popen(commands[name],cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 try:code=proc.wait(timeout=limit)
 except (subprocess.TimeoutExpired,KeyboardInterrupt) as failure:
  code=124 if isinstance(failure,subprocess.TimeoutExpired) else 130
  try:os.killpg(proc.pid,signal.SIGKILL)
  except ProcessLookupError:pass
  proc.wait()
record=dict(source_hashes=source_hashes,command=commands[name],environment_allowlist={k:env.get(k) for k in ['PATH','RUSTC','GOCACHE','GOPROXY','FROST_K03_TBTC','FROST_K03_ANVIL','SNOWFALL_WORKER','SNOWFALL_WORKER_SHA256','V03_REPLAY_REPORT']},deadline_seconds=limit,start_utc=start,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),seconds=time.monotonic()-clock,exit_code=code,process_group=proc.pid)
(OUT/(name+'-run.json')).write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record));sys.exit(code)
