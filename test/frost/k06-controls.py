"""K-06 guard regressions: revert one production guard and require its named
test to fail. Each control runs in its own throwaway copy of the tree, so all
of them run in parallel and the working tree is never modified. Run from the
keep-core root with the run-k06.sh environment."""
import concurrent.futures
import json
import os
import sys
import shutil
import subprocess
import tempfile

env = dict(os.environ)
root = os.getcwd()
controls=[
 ("NoRefundMargin","pkg/frost/deposit/view.go","	if s.MedianTimePast+v.Policy.RefundMargin >= uint64(r.Terms.RefundLocktime) {","	if false {","TestLateDepositRecovery", "accepted a signature past the margin"),
 ("DropMainLockGrant","pkg/frost/deposit/lock.go","	if err = permitted(history, plan, reservation); err != nil {\n		return err\n	}\n	current := state(history)","	current := state(history)","TestMainPurposeLockAndResolution", "payment grant raced past the resolution"),
 ("SweepTakesRedemptionMain","pkg/frost/deposit/lock.go","	if current.Purpose != Free && current.Purpose != SweepMain {","	if current.Purpose == Closed {","TestMainPurposeLockAndResolution", "ordinary sweep took a reserved main"),
 ("ResubmitCreditedSweep","pkg/maintainer/spv/frost_sweeps.go","	if swept {\n		action.Kind = \"already-credited\"","	if swept && false {\n		action.Kind = \"already-credited\"","TestFrostDepositSweepAndCredit", "second proof:"),
 ("TrustLostReply","pkg/maintainer/spv/frost_sweeps.go","		if again, _, readErr := m.credited(ctx, wallet, tx); readErr != nil || !again {","		if true {","TestFrostDepositSweepAndCredit", "simulated lost reply after the proof was mined"),
 ("AttestWithoutMainLock","pkg/frost/redemption/attestation.go","	if a.MainLock != nil {\n		id :=","	if false {\n		id :=","TestLateDepositRecovery", "redemption attested on a main held by a released sweep"),
 ("RefundedDepositPlanned","pkg/frost/deposit/planner.go","	return r, unspent, err","	return r, unspent || true, err","TestDepositorRefundWithoutOperators", "refunded deposit planned again"),
]


def run(control, fault=None, mutate=True):
    name, path, old, new, test, marker = control
    if not mutate:
        new = old
    parent = tempfile.mkdtemp(prefix="k06-control-")
    try:
        # go.mod replaces the SNOWFALL client with ../snowfall: keep that layout.
        os.symlink(os.path.realpath(os.path.join(root, "..", "snowfall")), os.path.join(parent, "snowfall"))
        tree = os.path.join(parent, "keep-core")
        shutil.copytree(root, tree, symlinks=True, ignore=shutil.ignore_patterns(".git"))
        target = os.path.join(tree, path)
        src = open(target).read()
        assert src.count(old) == 1, (name, src.count(old))
        open(target, "w").write(src.replace(old, new))
        run_env = dict(env)
        if fault:
            run_env["FROST_K06_CONTROL_FAULT"] = fault
        r = subprocess.run(["go", "test", "-json", "-buildvcs=false", "./pkg/frost/integration", "-run", "^" + test + "$", "-count=1", "-timeout", "20m"], cwd=tree, env=run_env, capture_output=True, text=True)
        events = [json.loads(l) for l in r.stdout.splitlines() if l.startswith("{")]
        ran = any(e.get("Action") == "run" and e.get("Test") == test for e in events)
        failed = any(e.get("Action") == "fail" and e.get("Test") == test for e in events)
        output = [e["Output"].strip() for e in events if e.get("Action") == "output" and e.get("Test") == test and "_test.go:" in e.get("Output", "")]
        output = [l for l in output if not any(k in l for k in ("fresh contracts", "Satoshi", "exited state", "DKG complete"))]
        # Only the control's own assertion kills it. Any other outcome (a
        # build or setup failure, a missing prerequisite, a skip, an
        # unrelated failure) is a harness error, never mutation evidence.
        hit = [l for l in output if marker in l]
        if ran and failed and hit:
            return name, test, True, hit[-1]
        if ran and not failed and r.returncode == 0:
            return name, test, False, "test passed"
        detail = output[-1] if output else ((r.stdout + r.stderr).strip().splitlines() or [""])[-1]
        return name, test, None, detail
    finally:
        shutil.rmtree(parent, ignore_errors=True)

if "--self-test" in sys.argv:
    # Failures unrelated to the mutated guard must be ERRORs, never kills:
    # an unchanged guard with an unrelated proof-provider outage, and a
    # missing prerequisite.
    ok = True
    _, _, killed, line = run(controls[4], fault="proof-rpc", mutate=False)
    print(f"self-test: unchanged guard, proof RPC outage -> {'ERROR' if killed is None else killed}; {line}")
    ok = ok and killed is None
    env["FROST_K03_ANVIL"] = "/nonexistent/anvil"
    _, _, killed, line = run(controls[3])
    print(f"self-test: missing Anvil -> {'ERROR' if killed is None else killed}; {line}")
    ok = ok and killed is None
    raise SystemExit(0 if ok else 1)

bad = False
with concurrent.futures.ThreadPoolExecutor(max_workers=len(controls)) as pool:
    for name, test, killed, line in pool.map(run, controls):
        verdict = "ERROR (did not run to an assertion)" if killed is None else ("KILLED" if killed else "SURVIVED")
        print(f"{name}: {verdict} by {test}; {line}", flush=True)
        bad = bad or not killed
raise SystemExit(1 if bad else 0)
