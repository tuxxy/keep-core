#!/usr/bin/env python3
"""V-08 local run recorder (KC-VER 2.5). Local only: no CI, no Kani, no
mutation, no Docker. Every command is recorded before it starts
(<name>-planned.json) and after it ends (<name>-actual.json), with the limits
of the 2026-11-06 catch-up brief: 4 model threads, GOMAXPROCS=4,
CARGO_BUILD_JOBS=4, 900 s wall and a 20 GiB tree RSS ceiling enforced by a
monitor that kills the process group.

Usage (from anywhere):
  run.py OUT model [profile control max_tick]
  run.py OUT go-normal | go-race | replay | replay-race
Evidence never overwrites: an existing record of the same name stops the run.
"""
import datetime, hashlib, json, os, pathlib, signal, subprocess, sys, time

ROOT = pathlib.Path(__file__).resolve().parents[4]
VER = ROOT / 'pkg/frost/movement/verification'
TOOLCHAIN = pathlib.Path.home() / '.rustup/toolchains/1.94.0-aarch64-apple-darwin/bin'
LIMITS = {
    'wall_seconds_per_command': 900,
    'rss_ceiling_bytes': 20 * 1024**3,
    'model_threads': 4,
    'GOMAXPROCS': 4,
    'CARGO_BUILD_JOBS': 4,
    'go_package_parallelism': 1,
    'memory_monitor_interval_seconds': 0.25,
    'bound': {'wallets': 'S, T plus alternative A', 'moved_outputs': 2, 'ticks': 3},
}
GO_TESTS = '^(TestV08|TestCommitmentChoosesLowestEligibleLabels|TestNoEligibleTargetRefusesCommitment|TestMovementAuthorityUsesTheCommitmentOnly|TestMovementUsesCommittedFeeCap|TestIncomingSweepUsesRecordedFeeCap|TestIncomingAuthorityAndPlan|TestIncomingAuthorityMatchesRecordsByKey|TestCommittedMovementSupersedesAbortedSweep|TestClosureNeedsAnEmptyLedgerTwice|TestSelection)'


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def tree_rss(pid):
    rows = []
    for line in subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss='], text=True).splitlines():
        p, parent, rss = map(int, line.split())
        rows.append((p, parent, rss))
    ids = {pid}
    while True:
        more = {p for p, parent, _ in rows if parent in ids}
        if more <= ids:
            break
        ids |= more
    return sum(rss * 1024 for p, _, rss in rows if p in ids)


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(out, name, cmd, env_extra=None):
    for suffix in ('.log', '-planned.json', '-actual.json'):
        if (out / (name + suffix)).exists():
            raise SystemExit(f'{name}{suffix} exists; evidence is never overwritten')
    env = os.environ.copy()
    env.update({'GOMAXPROCS': '4', 'CARGO_BUILD_JOBS': '4', 'PATH': f'{TOOLCHAIN}:{env["PATH"]}'})
    env.update(env_extra or {})
    record = {
        'name': name, 'command': cmd, 'cwd': str(ROOT), 'start': stamp(), 'limits': LIMITS,
        'environment_allowlist': {k: env[k] for k in ['GOMAXPROCS', 'CARGO_BUILD_JOBS'] + sorted(env_extra or {})},
        'model_sha256': {str(p.relative_to(ROOT)): sha256(p) for p in sorted((VER / 'model/src').glob('*.rs'))},
    }
    (out / f'{name}-planned.json').write_text(json.dumps(record, indent=2) + '\n')
    start = time.monotonic()
    peak, reason = 0, None
    with (out / f'{name}.log').open('w') as log:
        p = subprocess.Popen(cmd, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        while p.poll() is None:
            try:
                peak = max(peak, tree_rss(p.pid))
            except subprocess.CalledProcessError:
                pass
            if peak > LIMITS['rss_ceiling_bytes']:
                reason = 'memory ceiling'
            elif time.monotonic() - start >= LIMITS['wall_seconds_per_command']:
                reason = 'wall deadline'
            if reason:
                os.killpg(p.pid, signal.SIGKILL)
                p.wait()
                break
            time.sleep(LIMITS['memory_monitor_interval_seconds'])
    record.update(end=stamp(), seconds=time.monotonic() - start, exit_code=p.returncode,
                  stopped_for=reason, peak_tree_rss_bytes=peak)
    (out / f'{name}-actual.json').write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps({k: record[k] for k in ['name', 'exit_code', 'seconds', 'stopped_for', 'peak_tree_rss_bytes']}), flush=True)
    return p.returncode == 0 and reason is None


def main():
    out = pathlib.Path(sys.argv[1]).resolve()
    mode = sys.argv[2]
    out.mkdir(parents=True, exist_ok=True)
    limits = out / 'limits.json'
    if not limits.exists():
        limits.write_text(json.dumps(LIMITS, indent=2) + '\n')
    if mode == 'model':
        extra = sys.argv[3:]
        manifest = str(VER / 'model/Cargo.toml')
        build = 'model-build'
        ok = (out / f'{build}-actual.json').exists() or run(out, build, ['cargo', 'build', '--offline', '--release', '--manifest-path', manifest])
        name = 'model' if not extra else 'model-' + '-'.join(extra)
        ok = ok and run(out, name, [str(VER / 'model/target/release/v08-movement-model'), str(out)] + extra)
    elif mode in ('go-normal', 'go-race', 'replay', 'replay-race'):
        cmd = ['go', 'test', '-count=1', '-p', '1', '-timeout=600s']
        if mode.endswith('race'):
            cmd.append('-race')
        if mode.startswith('replay'):
            cmd += ['-run', '^TestV08TraceReplay$', '-v', './pkg/frost/movement']
            env = {'V08_TRACES': str(out / 'traces'), 'V08_REPLAY_REPORT': str(out / f'{mode}-report.json')}
        else:
            cmd += ['-run', GO_TESTS, './pkg/frost/movement']
            env = {}
        ok = run(out, mode, cmd, env)
    else:
        raise SystemExit(__doc__)
    raise SystemExit(0 if ok else 1)


if __name__ == '__main__':
    main()
