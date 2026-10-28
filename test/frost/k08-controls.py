"""K-08 guard regressions: revert one production guard and require the test
that owns it to fail at its own assertion. Each control runs in its own
throwaway copy of the tree, so they run in parallel and the working tree is
never modified. Run from the keep-core root with the run-k08.sh environment.

Host-only guards are owned by the pkg/frost/signing and pkg/frost/enforcement
unit tests; the defense routes that need C-08 itself are owned by the named
K-08 test on real contracts and Core (V-09 negative controls, R-07)."""
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
SIGNING = "./pkg/frost/signing"
ENFORCEMENT = "./pkg/frost/enforcement"
HEARTBEAT = "pkg/frost/signing/heartbeat.go"
DEFENDER = "pkg/frost/enforcement/defender.go"
RUNNER = "pkg/frost/enforcement/heartbeat.go"
CLAIMER = "pkg/frost/enforcement/inactivity.go"
controls = [
    # P-K08-HEARTBEAT-DOMAIN: release whatever message the grant carries.
    ("AcceptRawDigest", HEARTBEAT,
     "	if err != nil || intent != h.intent || HeartbeatDigest(intent.WalletID, intent.Nonce) != r.Message {",
     "	if err != nil || intent != h.intent {",
     SIGNING, "TestHeartbeatSignerDomain", "the guard accepted a raw digest"),
    # P-K08-HEARTBEAT-DOMAIN: heartbeat attempts share the transaction domain.
    ("SharedAttemptDomain", HEARTBEAT,
     "	raw := append([]byte(\"keep-core/frost/heartbeat-attempt/v1/\"), id[:]...)",
     "	raw := append([]byte(\"keep-core/frost/transaction-attempt/v1/\"), id[:]...)",
     SIGNING, "TestHeartbeatSignerDomain", "heartbeat attempt is not in the heartbeat domain"),
    # P-K08-HEARTBEAT-DOMAIN: no current-nonce check.
    ("AnyNonce", HEARTBEAT,
     "	if err = h.config.Authority.AuthorizeHeartbeat(ctx, h.intent.WalletID, h.intent.Nonce); err != nil {",
     "	if err = h.config.Authority.AuthorizeHeartbeat(ctx, h.intent.WalletID, h.intent.Nonce); false {",
     SIGNING, "TestHeartbeatSignerDomain", "stale heartbeat reached the worker"),
    # P-K08-EVIDENCE-RETAINED: write the defense record after signing.
    ("RecordAfterGrant", "pkg/frost/signing/executor.go",
     ["	if err = e.retainDefenses(ctx); err != nil {\n		return nil, err\n	}\n	e.mu.Lock()",
      "	id := e.resultID()"],
     ["	e.mu.Lock()",
      "	if err = e.retainDefenses(ctx); err != nil {\n		return nil, err\n	}\n	id := e.resultID()"],
     SIGNING, "TestDefenseRecordBeforeGrant", "defense record not durable before the grant"),
    # P-K08-DEFENSE-BEFORE-DEADLINE: defend only once a record that only
    # executor recovery would write exists.
    ("DefendAfterRecovery", DEFENDER,
     "		if err = d.step(\"defense-record-read\"); err != nil {",
     "		if _, rerr := d.Records.ReadSigningRecord(ctx, c.Digest); rerr != nil {\n			actions = append(actions, Action{c.Digest, ActionNotOurs})\n			continue\n		}\n		if err = d.step(\"defense-record-read\"); err != nil {",
     INTEGRATION, "TestFrostFraudPublicAdmissionDefenseTimeout", "proven sweep defense"),
    # P-K08-DEFENSE-BEFORE-DEADLINE: only mined transactions are defended.
    ("ProvenOnly", DEFENDER,
     "		defense.Authorization = AuthSelf",
     "		defense.Authorization = AuthProven",
     INTEGRATION, "TestFrostFraudPublicAdmissionDefenseTimeout", "defense after restart"),
    # Reading 6: the notifier times out a digest the host could defend.
    ("TimeoutOwnDigest", DEFENDER,
     "		if _, err := signing.ReadDefense(ctx, d.Records, d.Wallet, c.Digest); !errors.Is(err, store.ErrMissing) {",
     "		if _, err := signing.ReadDefense(ctx, d.Records, d.Wallet, c.Digest); err != nil && false {",
     ENFORCEMENT, "TestTimeoutNotifier", "the host timed out a digest it could defend"),
    # P-K08-NO-SUSPECT-SANCTION: drop the worker's Suspect seat from presence.
    ("ClaimFromSuspect", RUNNER,
     ["	\"github.com/keep-network/keep-core/pkg/frost/dkg\"",
      "Present: result.Present, Signature"],
     ["	\"github.com/keep-network/keep-core/pkg/frost\"\n	\"github.com/keep-network/keep-core/pkg/frost/dkg\"",
      "Present: func() []uint16 {\n		var fe *frost.Error\n		if !errors.As(err, &fe) {\n			return result.Present\n		}\n		var out []uint16\n		for _, s := range result.Present {\n			if s != fe.Suspect {\n				out = append(out, s)\n			}\n		}\n		return out\n	}(), Signature"],
     ENFORCEMENT, "TestInactivityClaimFromPresenceOnly", "a suspect hint produced a claim"),
    # P-K08-NO-SUSPECT-SANCTION: claim from the last failed heartbeat alone.
    ("SingleFailureClaim", CLAIMER,
     "	var failed []Outcome\n",
     "	var failed []Outcome\n	c.Failures = 1\n",
     ENFORCEMENT, "TestInactivityClaimFromPresenceOnly", "one failure produced a claim"),
    # P-K08-NO-SUSPECT-SANCTION: sign a proposal without deriving it.
    ("SignWithoutOwnView", CLAIMER,
     "	if !reflect.DeepEqual(own, proposed) {",
     "	if !reflect.DeepEqual(own, own) {",
     ENFORCEMENT, "TestInactivityClaimFromPresenceOnly", "signed a claim this host's records do not support"),
    # P-K08-NO-SUSPECT-SANCTION: skip a started round whose outcome was lost.
    ("SkipUnknownRound", CLAIMER,
     "startID(wallet, anchor)); serr == nil {\n				break",
     "startID(wallet, anchor)); serr == nil {\n				continue",
     ENFORCEMENT, "TestInactivityClaimFromPresenceOnly", "failures on both sides of an unknown round were claimed as consecutive"),
    # P-K08-NO-SUSPECT-SANCTION: rederive a retained claim on retry.
    ("RederiveRetainedClaim", CLAIMER,
     "	raw, err := c.Records.ReadSigningRecord(ctx, claimRecordID(proposed.WalletID, proposed.Nonce))\n",
     "	raw, err := []byte(nil), store.ErrMissing\n",
     ENFORCEMENT, "TestInactivityClaimFromPresenceOnly", "a retried claim conflicted with its own retained record"),
    # P-K08-DEFENSE-BEFORE-DEADLINE: one failed defense ends the pass.
    ("AbortOnFirstFailure", DEFENDER,
     "				failures = append(failures, fmt.Errorf(\"defense of %x: %w\", c.Digest, err))\n				continue",
     "				return actions, fmt.Errorf(\"defense of %x: %w\", c.Digest, err)",
     ENFORCEMENT, "TestDefenderRoutes", "an earlier failed defense blocked a later valid one"),
]


def run(control, fault=None, mutate=True):
    name, path, old, new, package, test, marker = control
    if not mutate:
        new = old
    parent = tempfile.mkdtemp(prefix="k08-control-")
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
            run_env["FROST_K08_CONTROL_FAULT"] = fault
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
    _, _, killed, line = run(controls[5])
    print(f"self-test: missing Anvil -> {'ERROR' if killed is None else killed}; {line}")
    ok = ok and killed is None
    _, _, killed, line = run(controls[0], mutate=False)
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
