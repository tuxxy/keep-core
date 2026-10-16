# V-04 at K-05 — 15 October 2026

Statement **S-K05-SIGN-1**. Evidence class: **bounded model checking** of a
separate Stateright model, **concrete replay** on real K-05 code, and
**concrete tests**. This is not a proof of the Go source. It is not a merge,
funding, activation or production gate. Self-review only; no independent
review is claimed.

Subject: K-05 `24e870e74` (keep-core #13) with C-04/K-05 tbtc-v2 `ff7735ec`
(tbtc-v2 #7), the SNOWFALL client `c1847ebdf133` and worker beta.1
(`d775dc36…`). Full results, logs, run records, the source manifest and the
statement JSON are in the implementation evidence directory
`V-04/K05-replay-2026-10-15/` (KC-VER VD-03). The committed fixture traces for
the admission replay are in `evidence/V-04-K05-2026-10-15/admission-traces/`.

## Runs

Limits were recorded before each run: 1,800 s wall per command, 16 GiB
process-tree RSS (polled every 250 ms), four model threads, `GOMAXPROCS=4`,
`go test -p 2`. One heavy run at a time. Order: model, admission, admission
replay, normal, race, process, recovery, guard, integration, c04, k05.

| Run | Result | Scope |
| --- | --- | --- |
| model (signing + admission) | attempt 1 blocked: memory ceiling (17.8 GB at 180 s); attempt 2 exit 0, 287 s, peak 16.50 GB; signing claim model 140,043,852 unique / 1,150,552,670 generated, exhaustive; 13 weakenings each break their target; 14/14 configurations pass | signing profile: one wallet, two requests, two attempts, three timeout ticks |
| admission | exit 0, 12.1 s | repaired admission profile, 12 configurations |
| admission-replay | 22/22 traces, exit 0 | real `Attester.lock` and store, controlled C-04 reader |
| normal / race | 172 / 172 pass, 0 fail, 0 skip | V-04 suites in signing, store, snowfallengine, redemption, bitcoin |
| process | 13 pass | twelve subprocess application-boundary traces with real process kills |
| recovery / guard | 4 / 3 pass | durable signature recovery; real-store worker guard |
| integration | pass, 29.3 s | `TestTaprootExactAuthorization`, real worker and Bitcoin Core |
| c04 | 8/8 traces pass (9 test events), 251 s | C-04 projection replay, 8 signing traces on the real Bridge, host, worker and Core |
| c04-control | pass, 71 s; both weakened expectations refused by the real Bridge | weakened expectations of the two C-04 counterexamples must fail |
| k05 | 8 pass, 0 fail, 0 skip, 118 s | K-05 named tests: lifecycle, timeout race, late-signature quarantine |

## Admission replay (new at K-05)

`redemption.TestV04AdmissionTraceReplay` replays every trace of the admission
profile against the **real K-05 attestation slot lock**: `Attester.lock` and
`Attester.History` on the real encrypted `store.Scoped` with its fenced
commit path. A model `Crash` closes and reopens the store. `Attest` and
`Renew` call the lock with the certificate the model names: deadlines 1, 2 and
3 for the original, renewal and replacement. Chain and contract actions
(`Tick`, `FinalizeTick`, `Admit`, `StateSpend`, `PolicyChange`,
`ReadinessChange`) drive a controlled C-04 reader (H-03). Its custody history
follows the model block by block. `ConfirmAbsence` is the authenticated
finalized-absence supplier fact. The controlled reader answers the finalized
custody read for a slot only after the model confirmed absence for that slot.
After every step, the durable slot state is compared with the model's `locks`
and `attestation_history`: the current plan and every plan ever attested,
per slot (H-04).

| Class | Traces | Note |
| --- | --- | --- |
| full | 11 | every witness; all host steps on the real lock |
| partial | 11 | weakened counterexamples, claim-model steps cut at the first divergence (H-06) |
| none | 0 | |

The traces replay 253 steps, 99 of them host steps. Five weakenings remove a
host guard: `ForgetTupleLocksOnCrash`, `OmitTupleLock`,
`ReplaceBeforeInclusiveDeadline`, `ReplaceWithoutFinalizedAbsence` and
`IgnoreOutstandingRenewal`. Their **weakened** expectations are also replayed,
and each fails on the real lock, as the negative control requires. The six
contract-side weakenings (generation, version, expiry, identity, concurrency)
are controlled inputs here; C-04 tests own those guards.

Not exercised: `Planner.Validate`, the certificate-window check in `Attest`,
key reload before attestation, and seat signing. The lock receives the
finalized head directly; in production, `Attest` derives it as
`head - FinalityBlocks`.

## Finding F-V04-K05-01: model defect, admission replacement rule (repaired)

The first replay used the unrepaired admission model. It ran against both the
13 October trace set and a fresh 15 October set. The 15 October set produced
ten mismatches; the 13 October set produced two. All were preserved before any
change, under `before-admission-model-repair/` in the evidence directory.
Classification (H-07): **model defect**. The model's replacement guard had
three conditions that the implementation does not have, against R-02:

1. It required strict expiry (`tick > deadline`). The implemented and specified
   rule is that the inclusive deadline block is finalized. No admission can
   land in a finalized block, so this is the same fence.
2. It read absence from the *current*, unfinalized counter. The implementation
   reads custody at the finalized block, as ADMISSION-SEMANTICS states.
3. It did not compare the state version. The implementation refuses a
   replacement when finalized custody has a different state version
   ("superseded").

It also counted an identical retransmission of a superseded statement as a
refusal. The implementation returns it idempotently: the statement is already
in the durable history and can no longer be admitted.

Repair, in `model/src/admission.rs`:

- `FinalizeTick` records the counter and version at the finalized tick.
  Custody cannot change at a finalized tick.
- Replacement uses `finalized_absence` (deadline finalized, generation not
  admitted at the finalized tick, same state version).
- Retransmission of a statement already in the history is a no-op.
- The two replacement weakenings now each remove one implemented guard: the
  finalized-deadline loop and the finalized custody read.

The repaired claim model is exhaustive at 10,579,720 states, finds every
witness and has no safety counterexample. Every weakening breaks its target.
All 22 traces then replay. The implementation was not changed.

## Predicates

| Predicate | Bounded model | Concrete at K-05 | Status |
| --- | --- | --- | --- |
| P-K04-AUTHORIZED-ONLY | pass (signing) | c04 replay, integration, guard | pass within bounds |
| P-K05-TERMINAL-ATTEMPT | pass (signing) | process, recovery | pass within bounds |
| P-K05-NO-DOUBLE-OUTCOME | pass (signing) | c04 replay (refund-on-elapsed and refund-on-expiry controls refused by the real Bridge), k05 timeout race | pass within bounds |
| P-K05-LATE-SIGNATURE-QUARANTINE | pass (signing, as modeled) | k05 late-signature test | **blocked** on spec issue tlabs-xyz/FROST-migration-spec#2 (A5) |
| P-HOST-ONE-INTENT-ATTESTATION | pass (admission) | admission replay on the real lock | pass within bounds |
| P-HOST-REPLACEMENT-FINALIZED-ABSENCE | pass (admission, repaired) | admission replay on the real lock | pass within bounds |
| P-C04-* admission predicates | pass (admission) | controlled inputs; C-04 tests own them | model only |

**P-K05-LATE-SIGNATURE-QUARANTINE is blocked.** The predicate text says that a
late valid signature "cannot authorize settlement". C-04 A5 accepts an SPV
payment proof for the exact admitted transaction while the intent is
TimeoutPending or Quarantined. V3 section 4.6 requires that settlement. The
model checks one reading: no new release and no second outcome. The owner has
not chosen a reading. Until issue #2 is decided, V-04 claims neither reading,
and the model result is not cited for this predicate.

## Limits and open items

- Bounds as in SCOPE.md. The admission profile covers one renewal and one
  replacement.
- The signing profile peaks near the 16 GiB ceiling. The same 140,043,852-state
  run peaked at 10.8–13.5 GB on 13 October and at 15.85, 17.8 and 16.50 GB today;
  the variance is in the checker, not the model. Attempt 1 is recorded as
  blocked, and only attempt 2 is cited. A larger bound does not fit these limits.
- The model retry regenerated admission traces with different BFS paths. Both
  sets replay on the real lock: the committed fixture set (22/22) and the
  retry set (22/22, `admission-replay-model-retry-traces.log`).
- The C-04 projection replay is partial: commit-stage, crash and I/O actions
  are covered by the process traces, not on the real Bridge.
- Timeout policy (spec issue #3) is an owner decision. Both K-05 policies are
  implemented; neither is a default.
- A-K04-AGREED-EXECUTOR-CONFIG: K-05 derives the agreement from the chain
  locally. Cross-operator transport is not covered.
- V-03's executor repairs (keep-core #14) are not in K-05. They concern DKG
  readiness, not signing, and do not change any V-04 pin.

## Re-pin at K-05 `e9fc20764` — S-K05-SIGN-2 (supersedes S-K05-SIGN-1)

K-05 #13 moved twice on 15 October:

- **`3c302583d`.** Review fixes in `signing/cancellation.go` (ImmediatelyFinal), `redemption/{ledger,resolver,planner,attestation}.go` and the integration tests. The slot lock gained a history-capacity refusal at 4096 entries. That is far above the model's two entries per slot, so it is an unmodeled guard that the bounded traces never reach.
- **`e9fc20764`.** The same patch, restacked on the K-03/K-04 heads that carry V-03's executor repairs (F-V03-01/02, `pkg/frost/dkg`).

The model sources did not change, so the model results above carry over. Every Go replay and suite reran at `e9fc20764`. A partial run at `3c302583d` was stopped once the head moved; it is archived and not cited.

| Run | Result |
| --- | --- |
| admission-replay | 22/22 (11 full, 11 partial); the 5 host-lock negative controls fail on the real lock |
| normal / race | 172 / 172 pass |
| process / recovery / guard | 13 / 4 / 3 pass |
| integration | pass |
| c04 / c04-control | 8/8 traces / both weakened expectations refused |
| k05 | 8 pass |

Evidence: `V-04/K05-repin-e9fc20764-2026-10-15/`, which holds `S-K05-SIGN-2.json` and its source manifest.
