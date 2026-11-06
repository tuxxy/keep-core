#!/usr/bin/env python3
"""Finite local V-03 runner (re-pin 2026-11-06).

Usage: V03_OUT=<evidence dir> run.py <name>

The model writes its traces and results into the in-repo evidence directory
(the replay reads them from there) and the runner copies them to V03_OUT.
Logs, planned limits and run records go to V03_OUT only. Limits are written
before the command starts; the whole process group is killed at the
deadline. The snowfall client is the go.mod replace (../snowfall/clients/go,
a checkout of the pinned client revision); no modfile.
"""
import datetime, json, os, pathlib, re, shutil, signal, subprocess, sys, time

ROOT = pathlib.Path(__file__).resolve().parents[4]
REPO_EVIDENCE = ROOT / 'pkg/frost/dkg/verification/evidence'
RUST = '/Users/tux/.rustup/toolchains/1.94.0-aarch64-apple-darwin/bin'
LIMITS = {'deadline_seconds': 900, 'go_test_timeout': '600s', 'gomaxprocs': 4, 'cargo_build_jobs': 4,
          'model_threads': 1, 'model_seconds_per_run': 120, 'max_rss_gib': 20,
          'replay_subprocess_seconds': 15, 'replay_wait_seconds': 10}

go = ['go', 'test']
tail = ['-count=1', '-timeout=' + LIMITS['go_test_timeout'], '-v']
commands = {
    'model': ['cargo', 'run', '--offline', '--locked', '--release', '--manifest-path',
              'pkg/frost/dkg/verification/model/Cargo.toml', '--', str(REPO_EVIDENCE)],
}
suites = {
    # Subprocess replay of every model trace on the real executor.
    'replay': (['./pkg/frost/dkg/verification/replay'], '^TestV03SubprocessReplay$'),
    # Executor seams: model-read projection, observer and outcome units.
    'dkg': (['./pkg/frost/dkg'], 'TestV03|TestReconcileWalletOutcome|TestFinishFailureDrainsAndPrefersClassification|'
            'TestCollectRejectsWrongOperatorSignature|TestFinishSuccessRetainsClassifiedObserverFailure|'
            'TestMonitorReconcilesOverlappingApprovalObservation|TestMonitorPublicationAfterObservationSnapshot|'
            'TestUnknownSuite'),
    # Real contracts, real worker: K-03 lost-seat/expiry paths and the
    # startup restore (Executor.Restore through tbtc.StartFrostNode).
    'integration': (['./pkg/frost/integration'], '^(TestLostSeatBeforeReadinessExpiresWallet|'
                    'TestLostSeatAfterReadinessCertifiesUnderPolicy|TestPendingWalletExpiryThenFreshEpoch|'
                    'TestSeatRestartAfterKill)$'),
    # Production chain adapter send path changed since the pin.
    'chain': (['./pkg/chain/ethereum'], '^TestFrost'),
}
for name, (packages, pattern) in suites.items():
    # Real-contract cases each run Anvil, three workers and a host process;
    # at GOMAXPROCS=4 under the race detector the default -parallel starves
    # the first DKG before its kill point. Two at a time keeps them in budget.
    extra = ['-parallel=2'] if name == 'integration' else []
    commands[name] = go + packages + ['-run', pattern] + extra + tail
    commands[name + '-race'] = go + ['-race'] + packages + ['-run', pattern] + extra + tail

name = sys.argv[1]
out = pathlib.Path(os.environ['V03_OUT'])
out.mkdir(parents=True, exist_ok=True)
env = os.environ.copy()
env.update(PATH=RUST + ':' + env['PATH'], RUSTC=RUST + '/rustc', GOPROXY='off',
           GOMAXPROCS=str(LIMITS['gomaxprocs']), CARGO_BUILD_JOBS=str(LIMITS['cargo_build_jobs']))
env.pop('GOFLAGS', None)
if name.startswith('replay'):
    env['V03_REPLAY_REPORT'] = str(out / (name + '-report.json'))
allow = ['PATH', 'RUSTC', 'GOPROXY', 'GOMAXPROCS', 'CARGO_BUILD_JOBS', 'CARGO_TARGET_DIR', 'GOCACHE',
         'FROST_K03_TBTC', 'FROST_K03_ANVIL', 'SNOWFALL_WORKER', 'SNOWFALL_WORKER_SHA256', 'V03_REPLAY_REPORT']


def rev(path):
    return subprocess.run(['git', '-C', str(path), 'rev-parse', 'HEAD'], capture_output=True, text=True).stdout.strip()


command = ['/usr/bin/time', '-l'] + commands[name]
planned = dict(name=name, command=commands[name], limits=LIMITS, keep_core=rev(ROOT),
               snowfall_client=rev(ROOT.parent / 'snowfall'),
               environment_allowlist={k: env.get(k) for k in allow},
               planned_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
(out / (name + '-planned.json')).write_text(json.dumps(planned, indent=2) + '\n')
start = datetime.datetime.now(datetime.timezone.utc).isoformat()
clock = time.monotonic()
log_path = out / (name + '.log')
with log_path.open('w') as log:
    proc = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        code = proc.wait(timeout=LIMITS['deadline_seconds'])
    except (subprocess.TimeoutExpired, KeyboardInterrupt) as failure:
        code = 124 if isinstance(failure, subprocess.TimeoutExpired) else 130
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        proc.wait()
text = log_path.read_text(errors='replace')
rss = re.findall(r'(\d+)\s+maximum resident set size', text)
record = dict(planned, start_utc=start, end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
              seconds=time.monotonic() - clock, exit_code=code,
              peak_rss_bytes_top_process=int(rss[-1]) if rss else None)
if name == 'model':
    shutil.copy(REPO_EVIDENCE / 'results.json', out / 'results.json')
    if (out / 'traces').exists():
        shutil.rmtree(out / 'traces')
    shutil.copytree(REPO_EVIDENCE / 'traces', out / 'traces')
(out / (name + '-run.json')).write_text(json.dumps(record, indent=2) + '\n')
print(json.dumps({k: record[k] for k in ('name', 'exit_code', 'seconds', 'peak_rss_bytes_top_process')}))
sys.exit(code)
