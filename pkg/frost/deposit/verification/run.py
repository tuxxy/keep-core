#!/usr/bin/env python3
"""V-07 local runner (KC-VER 2.5). Local only; never CI.

Usage, from the keep-core repository root:
  V07_OUT=<evidence dir> python3 pkg/frost/deposit/verification/run.py STEP [STEP...]

Steps:
  toolchain   record toolchain versions and source heads
  build       build the model crate offline (release)
  model       claim model (exhaustive) and every weakening to its first
              counterexample (R-08), single-threaded so traces are shortest
  exactness   every weakening explored exhaustively (4 threads) to record
              which properties it breaks
  traces      copy the model traces into verification/traces
  replay      TestV07Replay (normal), report replay-report.json
  replay-race TestV07Replay under the race detector
  suites      the affected Go suites, normal
  suites-race the affected Go suites under the race detector

Before each step it writes <step>-planned.json with the command, the
environment allowlist and the limits (900 s wall, 4 threads, 20 GiB
process-tree RSS, polled every 250 ms). After it, <step>-actual.json with
the exit code, times and the peak RSS, and <step>.log. A timeout or a
resource stop is blocked, never pass.
"""
import json, os, shutil, subprocess, sys, time
from datetime import datetime, timezone

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", "..", ".."))
MODEL = os.path.join(HERE, "model")
SCRATCH_TARGET = os.environ.get("V07_TARGET", os.path.join(MODEL, "target"))
OUT = os.environ.get("V07_OUT")
WALL = 900
RSS_LIMIT = 20 * 1024 ** 3
DEADLINE = 840  # model deadline inside the 900 s wall (R-09)
ALLOW = ["PATH", "HOME", "GOPATH", "GOCACHE", "GOMODCACHE", "CARGO_HOME", "RUSTUP_HOME", "TMPDIR"]
SUITES = [
    ("./pkg/frost/deposit/", "."),
    ("./pkg/frost/signing/", "Sweep"),
    ("./pkg/maintainer/spv/", "Frost|ReserveDeposit"),
]


def now():
    return datetime.now(timezone.utc).isoformat()


def env(extra):
    e = {k: os.environ[k] for k in ALLOW if k in os.environ}
    e.update({"GOMAXPROCS": "4", "CARGO_BUILD_JOBS": "4", "GOFLAGS": "-mod=readonly"})
    e.update(extra)
    return e


def tree_rss(pid):
    out = subprocess.run(["ps", "-A", "-o", "pid=,ppid=,rss="], capture_output=True, text=True).stdout
    kids, rss = {}, {}
    for line in out.splitlines():
        p, pp, r = (int(x) for x in line.split())
        kids.setdefault(pp, []).append(p)
        rss[p] = r * 1024
    total, stack = 0, [pid]
    while stack:
        p = stack.pop()
        total += rss.get(p, 0)
        stack.extend(kids.get(p, []))
    return total


def run(step, cmd, extra=None, cwd=ROOT):
    e = env(extra or {})
    plan = {"step": step, "command": cmd, "cwd": os.path.relpath(cwd, ROOT), "env": {k: v for k, v in e.items() if k not in ("PATH", "HOME")},
            "env_allowlist": ALLOW, "limits": {"wall_seconds": WALL, "threads": 4, "rss_bytes": RSS_LIMIT, "rss_poll_seconds": 0.25},
            "planned_at": now()}
    json.dump(plan, open(os.path.join(OUT, f"{step}-planned.json"), "w"), indent=2)
    log = open(os.path.join(OUT, f"{step}.log"), "w")
    start, t0 = now(), time.time()
    p = subprocess.Popen(cmd, cwd=cwd, env=e, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    peak, stop = 0, None
    while p.poll() is None:
        peak = max(peak, tree_rss(p.pid))
        if peak > RSS_LIMIT:
            stop = "rss"
        elif time.time() - t0 > WALL:
            stop = "wall"
        if stop:
            os.killpg(p.pid, 9)
            p.wait()
            break
        time.sleep(0.25)
    actual = {"step": step, "start": start, "end": now(), "seconds": round(time.time() - t0, 3), "exit_code": p.returncode,
              "peak_tree_rss_bytes": peak, "stopped": stop, "blocked": stop is not None}
    json.dump(actual, open(os.path.join(OUT, f"{step}-actual.json"), "w"), indent=2)
    print(json.dumps(actual))
    return p.returncode == 0 and stop is None


def binary():
    return os.path.join(SCRATCH_TARGET, "release", "v07-deposit-model")


def main():
    if not OUT:
        sys.exit("set V07_OUT to the evidence directory")
    os.makedirs(OUT, exist_ok=True)
    ok = True
    for step in sys.argv[1:]:
        if step == "toolchain":
            info = {}
            for name, cmd in [("go", ["go", "version"]), ("cargo", ["cargo", "+1.94.0", "--version"]), ("rustc", ["rustc", "+1.94.0", "--version"]),
                              ("python", [sys.executable, "--version"]), ("keep-core", ["git", "-C", ROOT, "rev-parse", "HEAD"]),
                              ("keep-core-status", ["git", "-C", ROOT, "status", "--short"]),
                              ("snowfall", ["git", "-C", os.path.join(ROOT, "..", "snowfall"), "rev-parse", "HEAD"])]:
                info[name] = subprocess.run(cmd, capture_output=True, text=True).stdout.strip()
            info["stateright"] = "0.31.0 (Cargo.lock)"
            info["recorded_at"] = now()
            json.dump(info, open(os.path.join(OUT, "toolchain.json"), "w"), indent=2)
        elif step == "build":
            ok &= run("model-build", ["cargo", "+1.94.0", "build", "--release", "--offline", "--locked"], {"CARGO_TARGET_DIR": SCRATCH_TARGET}, cwd=MODEL)
        elif step == "model":
            ok &= run("model", [binary(), OUT, str(DEADLINE), "1", "all", "stop"])
        elif step == "exactness":
            os.makedirs(os.path.join(OUT, "exactness"), exist_ok=True)
            for control in ["SignWithoutRevealFinality", "PlanTimeMarginOnly", "AcceptFeeAboveCap", "DropMainLock",
                            "LockFromUnfinalizedReservation", "NoRefundMargin", "AcceptLateSweepSignature",
                            "ForgetConsumptionOnRetry", "ReuseSweepAttempt", "TrustLedgerMarkerOnly"]:
                ok &= run(f"exactness-{control}", [binary(), os.path.join(OUT, "exactness"), str(DEADLINE), "4", control, "exhaustive"])
        elif step == "traces":
            dst = os.path.join(HERE, "traces")
            shutil.rmtree(dst, ignore_errors=True)
            shutil.copytree(os.path.join(OUT, "traces"), dst)
        elif step in ("replay", "replay-race"):
            race = ["-race"] if step == "replay-race" else []
            report = os.path.join(OUT, f"{step}-report.json")
            ok &= run(step, ["go", "test", *race, "-count=1", "-run", "^TestV07Replay$", "-v", "./pkg/frost/deposit/"], {"V07_REPORT": report})
        elif step in ("suites", "suites-race"):
            race = ["-race"] if step == "suites-race" else []
            for pkg, pattern in SUITES:
                name = f"{step}-{pkg.strip('./').replace('/', '-')}"
                ok &= run(name, ["go", "test", *race, "-count=1", "-run", pattern, pkg])
        else:
            sys.exit(f"unknown step {step}")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
