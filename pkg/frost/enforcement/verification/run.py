#!/usr/bin/env python3
"""Local bounded V-09 run recorder (KC-VER 2.5). No CI, no Kani, no mutation.

Every command records its limits and inputs before it starts
(<name>-planned.json) and its exit, time and peak tree RSS after
(<name>-actual.json), with its log beside them.

Usage (from anywhere):
  V09_OUT=<evidence dir> python3 pkg/frost/enforcement/verification/run.py MODE
MODE: build | hb | enf | claim | claim-f3 | replay | race | suites | suites-race

The Go modes use a private modfile that points the snowfall client replace
at V09_SNOWFALL (default: ../snowfall next to the repository root), so no
checked-in file changes.
"""
import datetime, hashlib, json, os, pathlib, signal, subprocess, sys, time

ROOT = pathlib.Path(__file__).resolve().parents[4]
OUT = pathlib.Path(os.environ['V09_OUT'])
OUT.mkdir(parents=True, exist_ok=True)
TOOLCHAIN = '/Users/tux/.rustup/toolchains/1.94.0-aarch64-apple-darwin/bin'
LIMITS = {
    'wall_seconds_per_command': 900,
    'model_check_deadline_seconds': int(os.environ.get('V09_DEADLINE_SECONDS', '800')),
    'rss_ceiling_bytes': 20 * 1024**3,
    'model_threads': 4,
    'GOMAXPROCS': 4,
    'CARGO_BUILD_JOBS': 4,
    'go_package_parallelism': 2,
    'go_test_timeout': '10m',
}
MODEL = 'pkg/frost/enforcement/verification/model'
REPLAY_TESTS = {
    'signing': '^TestV09HeartbeatReplay',
    'enforcement': '^TestV09',
}
SUITES = {
    './pkg/frost/enforcement': '.',
    './pkg/frost/signing': '^(TestHeartbeat|TestDefense|TestV09)',
    './pkg/frost/transport': '.',
}


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def tree_rss(pid):
    rows = [list(map(int, x.split())) for x in subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss='], text=True).splitlines()]
    ids = {pid}
    while True:
        more = {p for p, parent, _ in rows if parent in ids}
        if more <= ids:
            break
        ids |= more
    return sum(k * 1024 for p, _, k in rows if p in ids)


def sha(paths):
    return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def run(name, cmd, extra_env=None):
    env = os.environ.copy()
    env.update({'GOMAXPROCS': '4', 'CARGO_BUILD_JOBS': '4', 'PATH': TOOLCHAIN + ':' + env['PATH']})
    env.update(extra_env or {})
    for suffix in ('.log', '-planned.json', '-actual.json'):
        if (OUT / (name + suffix)).exists():
            raise SystemExit(f'{name}{suffix} exists in {OUT}; evidence is never overwritten')
    record = {
        'name': name, 'command': cmd, 'cwd': str(ROOT), 'start': stamp(), 'limits': LIMITS,
        'environment_allowlist': {k: env[k] for k in ['GOMAXPROCS', 'CARGO_BUILD_JOBS', 'V09_DEADLINE_SECONDS', 'V09_TRACES', 'V09_REPLAY_REPORT', 'GOFLAGS'] if k in env},
        'model_sha256': sha((ROOT / MODEL / 'src').rglob('*.rs')),
        'go_sha256': sha([p for d in ['pkg/frost/enforcement', 'pkg/frost/signing', 'pkg/frost/transport', 'pkg/frost/store', 'pkg/frost/snowfallengine'] for p in (ROOT / d).glob('*.go')]),
    }
    (OUT / f'{name}-planned.json').write_text(json.dumps(record, indent=2) + '\n')
    start = time.monotonic()
    with (OUT / f'{name}.log').open('w') as log:
        p = subprocess.Popen(cmd, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        peak, reason = 0, None
        while p.poll() is None:
            peak = max(peak, tree_rss(p.pid))
            if peak > LIMITS['rss_ceiling_bytes']:
                reason = 'memory ceiling'
            if time.monotonic() - start >= LIMITS['wall_seconds_per_command']:
                reason = 'wall deadline'
            if reason:
                os.killpg(p.pid, signal.SIGKILL)
                p.wait()
                break
            time.sleep(0.25)
    record.update(end=stamp(), seconds=time.monotonic() - start, exit_code=p.returncode, stopped_for=reason,
                  peak_tree_rss_bytes=peak, memory_monitor_interval_seconds=0.25)
    (OUT / f'{name}-actual.json').write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps({k: record[k] for k in ['name', 'exit_code', 'seconds', 'stopped_for', 'peak_tree_rss_bytes']}), flush=True)
    return p.returncode == 0


def modfile():
    snowfall = pathlib.Path(os.environ.get('V09_SNOWFALL', str(ROOT.parent / 'snowfall'))).resolve()
    mf = OUT / 'run.mod'
    if not mf.exists():
        mf.write_text((ROOT / 'go.mod').read_text().replace('../snowfall/clients/go', str(snowfall / 'clients/go')))
        (OUT / 'run.sum').write_bytes((ROOT / 'go.sum').read_bytes())
    return mf


def go(name, race, packages):
    cmd = ['go', 'test', '-mod=readonly', '-modfile=' + str(modfile()), '-p', '2', '-count=1', '-timeout=' + LIMITS['go_test_timeout'], '-v']
    if race:
        cmd.append('-race')
    ok = True
    for pkg, pattern in packages.items():
        ok &= run(f'{name}-{pkg.strip("./").replace("/", "-")}', cmd + ['-run', pattern, pkg])
    return ok


def main():
    (OUT / 'limits.json').write_text(json.dumps(LIMITS, indent=2) + '\n')
    mode = sys.argv[1]
    if mode == 'build':
        ok = run('model-build', ['cargo', 'build', '--offline', '--release', '--manifest-path', MODEL + '/Cargo.toml'])
    elif mode in ('hb', 'enf', 'claim', 'claim-f3'):
        ok = run('model-' + mode, [MODEL + '/target/release/v09-enforcement-model', str(OUT), mode])
    elif mode in ('replay', 'race'):
        traces = str(OUT / 'traces')
        os.environ['V09_TRACES'] = traces
        packages = {'./pkg/frost/signing': REPLAY_TESTS['signing'], './pkg/frost/enforcement': REPLAY_TESTS['enforcement']}
        ok = True
        for pkg, pattern in packages.items():
            os.environ['V09_REPLAY_REPORT'] = str(OUT / f'replay-{mode}-{pkg.split("/")[-1]}.json')
            ok &= go(f'replay-{mode}', mode == 'race', {pkg: pattern})
    elif mode in ('suites', 'suites-race'):
        ok = go(mode, mode == 'suites-race', SUITES)
    else:
        raise SystemExit(__doc__)
    raise SystemExit(0 if ok else 1)


if __name__ == '__main__':
    main()
