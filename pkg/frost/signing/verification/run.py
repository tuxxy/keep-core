#!/usr/bin/env python3
"""Local bounded run recorder. No CI, dependency changes, or production mutations."""
import datetime, hashlib, json, os, pathlib, subprocess, sys, time
ROOT = pathlib.Path(__file__).resolve().parents[4]
# V04_OUT selects the evidence directory (KC-VER VD-03: results live under the
# implementation evidence tree); the default is the original 13 October run.
OUT = pathlib.Path(os.environ.get('V04_OUT', str(ROOT / 'pkg/frost/signing/verification/evidence/V-04-2026-10-13')))
OUT.mkdir(parents=True, exist_ok=True)
LIMITS = {'wall_seconds_per_command': 1800, 'rss_ceiling_bytes': 16*1024**3, 'model_threads':4, 'GOMAXPROCS':4, 'go_package_parallelism':2, 'wallets':1,'requests':2,'attempts':2,'timeout_ticks':3,'commit_stages':6,'max_acknowledged_commits':10}
def stamp(): return datetime.datetime.now(datetime.timezone.utc).isoformat()
def rss(pid):
    rows=[list(map(int,x.split())) for x in subprocess.check_output(['ps','-axo','pid=,ppid=,rss='],text=True).splitlines()]
    ids={pid}
    while True:
        more={p for p,parent,_ in rows if parent in ids}
        if more<=ids:break
        ids|=more
    return sum(k*1024 for p,_,k in rows if p in ids)

def run(name,cmd):
    env=os.environ.copy();env['GOMAXPROCS']='4';env['CARGO_BUILD_JOBS']='4';env['RUSTC']='/Users/tux/.rustup/toolchains/1.94.0-aarch64-apple-darwin/bin/rustc';env['PATH']='/Users/tux/.rustup/toolchains/1.94.0-aarch64-apple-darwin/bin:'+env['PATH']
    for previous in [OUT/(name+'.log'),OUT/(name+'-planned.json'),OUT/(name+'-actual.json')]:
        if not previous.exists():continue
        history=OUT/'history'/stamp().replace(':','-');history.mkdir(parents=True,exist_ok=True)
        previous.rename(history/previous.name)
    record={'name':name,'command':cmd,'start':stamp(),'limits':LIMITS,'environment_allowlist':{'GOMAXPROCS':'4','CARGO_BUILD_JOBS':'4','RUSTC':env['RUSTC']},'cwd':str(ROOT)}
    record['input_sha256']={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for base in ['pkg/frost/signing','pkg/frost/snowfallengine','pkg/frost/store','pkg/frost/redemption','pkg/frost/integration','pkg/maintainer/spv','pkg/chain/ethereum'] for p in (ROOT/base).glob('*.go')}
    record['source_manifest_sha256']=hashlib.sha256((OUT/'source-manifest.json').read_bytes()).hexdigest()
    record['model_sha256']={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in (ROOT/'pkg/frost/signing/verification/model/src').rglob('*.rs')}
    for key in ['SNOWFALL_WORKER','SNOWFALL_WORKER_SHA256','FROST_K03_TBTC','FROST_K03_ANVIL','FROST_K04_BITCOIND','FROST_K04_BITCOIND_SHA256','GOCACHE','V04_TRACES','V04_ADMISSION_TRACES','V04_ADMISSION_REPORT']:
        if key in env:record['environment_allowlist'][key]=env[key]
    (OUT/f'{name}-planned.json').write_text(json.dumps(record,indent=2)+'\n')
    start=time.monotonic()
    with (OUT/f'{name}.log').open('w') as log:
        p=subprocess.Popen(cmd,cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
        peak=0;reason=None
        while p.poll() is None:
            peak=max(peak,rss(p.pid))
            if peak>LIMITS['rss_ceiling_bytes']:reason='memory ceiling'
            if time.monotonic()-start>=1800:reason='wall deadline'
            if reason:
                import signal
                os.killpg(p.pid,signal.SIGKILL);p.wait();break
            time.sleep(.25)
        record.update(exit_code=p.returncode,timed_out=reason=='wall deadline',stopped_for=reason,peak_tree_rss_bytes=peak,memory_monitor_interval_seconds=.25)
    record.update(end=stamp(),seconds=time.monotonic()-start)
    (OUT/f'{name}-actual.json').write_text(json.dumps(record,indent=2)+'\n')
    print(json.dumps({k:record[k] for k in ['name','exit_code','seconds','stopped_for','peak_tree_rss_bytes']}),flush=True)
    return record['exit_code']==0
if __name__=='__main__':
    (OUT/'limits.json').write_text(json.dumps(LIMITS,indent=2)+'\n')
    mode=sys.argv[1]
    if mode in ('model','admission'):
        ok=run('model-build',['cargo','build','--offline','--release','--manifest-path','pkg/frost/signing/verification/model/Cargo.toml'])
        if ok:
            binary='v04-signing-model' if mode=='model' else 'admission-check'
            ok=run('model' if mode=='model' else 'admission-model',['pkg/frost/signing/verification/model/target/release/'+binary,str(OUT)])
    elif mode=='vectors':ok=run('typescript',['node','--experimental-strip-types','test/frost/check-k04-intent.ts'])
    elif mode in ('normal','race','guard','integration','recovery','process','c04','c04-control','k05','admission-replay'):
        # Worktree lacks sibling replacement. A private modfile preserves the exact
        # pinned dependency source, without touching go.mod or any other checkout.
        mf=OUT/'run.mod';original=(ROOT/'go.mod').read_text()
        mf.write_text(original.replace('../snowfall/clients/go',str(ROOT/'pkg/frost/signing/verification/dependency-source/clients/go')))
        (OUT/'run.sum').write_bytes((ROOT/'go.sum').read_bytes())
        cmd=['go','test','-mod=readonly','-modfile='+str(mf),'-p','2','-json','-count=1','-timeout=25m']
        if mode=='race':cmd+=['-race']
        if mode=='process':cmd+=['-run','^TestV04ProcessBoundaryReplay$','./pkg/frost/signing']
        elif mode=='recovery':cmd+=['-run','^TestV04(CancelledReturnedSignatureRetained|DoneOnlyRecovery)$','./pkg/frost/signing']
        elif mode=='guard':cmd+=['-run','^TestV04RealStoreWorkerGuard$','./pkg/frost/store']
        elif mode=='admission-replay':cmd+=['-run','^TestV04AdmissionTraceReplay$','./pkg/frost/redemption']
        elif mode=='integration':cmd+=['-run','^TestTaprootExactAuthorization$','./pkg/frost/integration']
        elif mode=='c04':cmd[cmd.index('-timeout=25m')]='-timeout=28m';cmd+=['-run','^TestV04C04ProjectionReplay$','./pkg/frost/integration']
        elif mode=='c04-control':cmd[cmd.index('-timeout=25m')]='-timeout=28m';cmd+=['-run','^TestV04C04WeakenedExpectationsFail$','./pkg/frost/integration']
        elif mode=='k05':cmd[cmd.index('-timeout=25m')]='-timeout=28m';cmd+=['-run','^(TestFrostRedemptionLifecycle|TestReservationTimeoutRace|TestLateSignatureQuarantine)$','./pkg/frost/integration']
        else:cmd+=['-run','^(TestV04|TestCanonicalIntent|TestAuthorizationBoundaries|TestReservationReceipt|TestSignatureDone|TestSigningAuthorizationAfterDurableClaim|TestSigningRecords|TestSigningRecordUncertainty|TestSigningRecordDomain|TestReturnedSignature|TestRecoverResult|TestMultiInput|TestSighashVectors|TestTaprootSnapshot)','./pkg/frost/signing','./pkg/frost/store','./pkg/frost/snowfallengine','./pkg/frost/redemption','./pkg/bitcoin']
        ok=run(mode,cmd)
    else:raise SystemExit('model|admission|vectors|normal|race|process|recovery|guard|integration|c04|c04-control|k05|admission-replay')
    raise SystemExit(0 if ok else 1)
