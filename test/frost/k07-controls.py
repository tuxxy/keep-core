"""K-07 guard regressions: revert one production guard and require the test
that owns it to fail at its own assertion. Each control runs in its own
throwaway copy of the tree, so they run in parallel and the working tree is
never modified. Run from the keep-core root with the run-k07.sh environment.

Integration controls use the named K-07 tests on real contracts and Core;
the host-only guards that no integration interleaving reaches are owned by
the pkg/frost/movement unit tests (V-08 negative controls, R-07)."""
import concurrent.futures
import json
import os
import sys
import shutil
import subprocess
import tempfile

env = dict(os.environ)
root = os.getcwd()
INTEGRATION = "./pkg/frost/integration"
MOVEMENT = "./pkg/frost/movement"
AUTHORITY = "pkg/frost/movement/authority.go"
controls = [
    # P-K07-COMMITTED-TARGET-ONLY: take the target from current eligibility.
    ("TargetsFromCurrentEligibility", AUTHORITY,
     "		if t.Label != c.Targets[i] ||",
     "		if !w.Eligible() || t.Label != c.Targets[i] ||",
     INTEGRATION, "TestMovementTargetStateRace", "movement signer"),
    # P-K07-COMMITTED-TARGET-ONLY: sign on a commitment not yet final.
    ("PlanTimeCommitmentOnly", AUTHORITY,
     "	if !a.View.Final(s.Head, s.Log.Log) {\n		return signing.ReservationReceipt{}, ErrNotFinal",
     "	if false {\n		return signing.ReservationReceipt{}, ErrNotFinal",
     MOVEMENT, "TestMovementAuthorityUsesTheCommitmentOnly", "unfinal commitment: authorized"),
    # P-K07-NO-STRANDED-MOVEMENT: only Live targets sweep (the legacy filter).
    ("SweepOnlyWhenLive", AUTHORITY,
     "	case StateReadyUnfunded, StateLive, StateMovingFunds, StateClosing, StateTerminated:",
     "	case StateLive:",
     INTEGRATION, "TestMovementTargetStateRace", "incoming-sweep signer"),
    # V-08 reading 2: a quarantined target sweeps without a directive.
    ("QuarantinedWithoutDirective", AUTHORITY,
     "	if t.Wallet.Quarantined {",
     "	if false {",
     MOVEMENT, "TestIncomingAuthorityAndPlan", "quarantined target swept without a directive"),
    # P-K07-INCOMING-ONCE: resubmit an already recorded proof.
    ("ForgetIncomingKey", "pkg/maintainer/spv/frost_movement.go",
     "	if done {\n		action.Kind = \"already-proven\"",
     "	if done && false {\n		action.Kind = \"already-proven\"",
     INTEGRATION, "TestCrossSchemeDrainAndSweep", "a second proof must not resubmit"),
    # P-K07-INCOMING-ONCE: trust a lost reply instead of rereading.
    ("TrustLostReply", "pkg/maintainer/spv/frost_movement.go",
     "		if again, readErr := m.proven(ctx, wallet, hash); readErr != nil || !again {",
     "		if true {",
     INTEGRATION, "TestCrossSchemeDrainAndSweep", "simulated lost reply"),
    # P-K07-CLOSE-ONLY-WHEN-CLEAR: decide from the custody field alone, at
    # both reads.
    ("MainFieldOnly", "pkg/frost/movement/closure.go",
     ["	if open != 0 {\n		return fmt.Errorf(\"%w: %08b\", ErrObligations, open)",
      "	if open != 0 {\n		return fmt.Errorf(\"%w at the current head: %08b\", ErrObligations, open)"],
     ["	if open&OpenCustody != 0 {\n		return fmt.Errorf(\"%w: %08b\", ErrObligations, open)",
      "	if open&OpenCustody != 0 {\n		return fmt.Errorf(\"%w at the current head: %08b\", ErrObligations, open)"],
     INTEGRATION, "TestCloseWithPendingObligations", "closed with an open challenge"),
    # P-K07-CLOSE-ONLY-WHEN-CLEAR: no recheck at the current head.
    ("NoRecheckBeforeNotify", "pkg/frost/movement/closure.go",
     "	if open != 0 {\n		return fmt.Errorf(\"%w at the current head: %08b\", ErrObligations, open)",
     "	if false {\n		return fmt.Errorf(\"%w at the current head: %08b\", ErrObligations, open)",
     MOVEMENT, "TestClosureNeedsAnEmptyLedgerTwice", "late arrival ignored"),
]


def run(control, fault=None, mutate=True):
    name, path, old, new, package, test, marker = control
    if not mutate:
        new = old
    parent = tempfile.mkdtemp(prefix="k07-control-")
    try:
        # go.mod replaces the SNOWFALL client with ../snowfall: keep that layout.
        os.symlink(os.path.realpath(os.path.join(root, "..", "snowfall")), os.path.join(parent, "snowfall"))
        tree = os.path.join(parent, "keep-core")
        shutil.copytree(root, tree, symlinks=True, ignore=shutil.ignore_patterns(".git"))
        target = os.path.join(tree, path)
        src = open(target).read()
        olds = old if isinstance(old, list) else [old]
        news = new if isinstance(new, list) else [new]
        for o, n in zip(olds, news):
            assert src.count(o) == 1, (name, src.count(o))
            src = src.replace(o, n)
        open(target, "w").write(src)
        run_env = dict(env)
        if fault:
            run_env["FROST_K07_CONTROL_FAULT"] = fault
        r = subprocess.run(["go", "test", "-json", "-buildvcs=false", package, "-run", "^" + test + "$", "-count=1", "-timeout", "40m"], cwd=tree, env=run_env, capture_output=True, text=True)
        events = [json.loads(l) for l in r.stdout.splitlines() if l.startswith("{")]
        ran = any(e.get("Action") == "run" and e.get("Test") == test for e in events)
        failed = any(e.get("Action") == "fail" and e.get("Test") == test for e in events)
        output = [e["Output"].strip() for e in events if e.get("Action") == "output" and (e.get("Test") or "").startswith(test) and "_test.go:" in e.get("Output", "")]
        output = [l for l in output if not any(k in l for k in ("fresh contracts", "Satoshi", "exited state", "DKG complete", "second DKG"))]
        # Only the control's own assertion kills it; anything else (build or
        # setup failure, missing prerequisite, skip, unrelated failure) is a
        # harness error, never mutation evidence.
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
    # Failures unrelated to the reverted guard must be ERRORs, never kills.
    ok = True
    env["FROST_K03_ANVIL"] = "/nonexistent/anvil"
    _, _, killed, line = run(controls[4])
    print(f"self-test: missing Anvil -> {'ERROR' if killed is None else killed}; {line}")
    ok = ok and killed is None
    _, _, killed, line = run(controls[1], mutate=False)
    print(f"self-test: unchanged guard -> {'SURVIVED' if killed is False else killed}; {line}")
    ok = ok and killed is False
    raise SystemExit(0 if ok else 1)

if "--only" in sys.argv:
    wanted = set(sys.argv[sys.argv.index("--only") + 1].split(","))
    controls = [c for c in controls if c[0] in wanted]

bad = False
with concurrent.futures.ThreadPoolExecutor(max_workers=len(controls)) as pool:
    for name, test, killed, line in pool.map(run, controls):
        verdict = "ERROR (did not run to an assertion)" if killed is None else ("KILLED" if killed else "SURVIVED")
        print(f"{name}: {verdict} by {test}; {line}", flush=True)
        bad = bad or not killed
raise SystemExit(1 if bad else 0)
