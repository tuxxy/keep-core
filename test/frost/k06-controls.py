"""K-06 guard regressions: revert one production guard at a time and require
its named test to fail. The source is restored after every run. Run from the
keep-core root with the run-k06.sh environment."""
import os
import subprocess

env = dict(os.environ)
controls=[
 ("NoRefundMargin","pkg/frost/deposit/view.go","	if s.MedianTimePast+v.Policy.RefundMargin >= uint64(r.Terms.RefundLocktime) {","	if false {","TestLateDepositRecovery"),
 ("DropMainLockGrant","pkg/frost/deposit/lock.go","	if err = permitted(history, plan, reservation); err != nil {\n		return err\n	}\n	current := state(history)","	current := state(history)","TestMainPurposeLockAndResolution"),
 ("SweepTakesRedemptionMain","pkg/frost/deposit/lock.go","	if current.Purpose != Free && current.Purpose != SweepMain {","	if current.Purpose == Closed {","TestMainPurposeLockAndResolution"),
 ("ResubmitCreditedSweep","pkg/maintainer/spv/frost_sweeps.go","	if swept {\n		action.Kind = \"already-credited\"","	if false {\n		action.Kind = \"already-credited\"","TestFrostDepositSweepAndCredit"),
 ("TrustLostReply","pkg/maintainer/spv/frost_sweeps.go","		if again, _, readErr := m.credited(ctx, wallet, tx); readErr != nil || !again {","		if true {","TestFrostDepositSweepAndCredit"),
 ("AttestWithoutMainLock","pkg/frost/redemption/attestation.go","	if a.MainLock != nil {\n		id :=","	if false {\n		id :=","TestLateDepositRecovery"),
 ("RefundedDepositPlanned","pkg/frost/deposit/planner.go","	return r, unspent, err","	return r, unspent || true, err","TestDepositorRefundWithoutOperators"),
]
survived = False
for name,path,old,new,test in controls:
    src=open(path).read()
    assert src.count(old)==1,(name,src.count(old))
    open(path,'w').write(src.replace(old,new))
    try:
        r=subprocess.run(["go","test","./pkg/frost/integration","-run","^"+test+"$","-count=1","-timeout","20m"],env=env,capture_output=True,text=True)
        lines=[l for l in r.stdout.splitlines() if "_test.go:" in l and "fresh contracts" not in l and "Satoshi" not in l and "exited state" not in l and "DKG complete" not in l]
        print(f"{name}: {'KILLED' if r.returncode!=0 else 'SURVIVED'} by {test}; {lines[-1].strip() if lines else ''}",flush=True)
        survived = survived or r.returncode == 0
    finally:
        open(path,'w').write(src)
raise SystemExit(1 if survived else 0)
