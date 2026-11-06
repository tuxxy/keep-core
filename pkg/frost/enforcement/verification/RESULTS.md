# V-09 results, 6 November 2026 (statement S-K08-ENFORCE-1)

Bounded model checking of the V-09 model (`model/`) and concrete replay of
its traces on the real code. Not a proof. Subjects: keep-core
`925103a6c`, tbtc-v2 `39412825`, snowfall `c1847ebdf133`, KC-VER rev 02
draft `51b9a16`. Scope and differences from `BOUNDARIES.md`: `SCOPE.md`.
Correspondence: `TRANSITION-MAP.md`. Evidence (logs, limits, traces, replay
reports, toolchain, heads, `SHA256SUMS`):
`Research/SNOWFALL/integration-spec-v2/implementation/K-08/v09-model-2026-11-06/`.
Reviewer: self-review only.

## Per predicate

| Predicate | Result within scope | Profile, bound | Unique states (claim model) | Weakenings (each breaks its property, no other) | Witnesses | Replay |
| --- | --- | --- | --- | --- | --- | --- |
| P-K08-HEARTBEAT-DOMAIN | **pass** | hb: nonces 1..3, 2 heartbeat + 2 transaction starts, 2 crashes, 1 misroute | 74,254 | `AcceptRawDigest`, `ReuseAttempt`, `AnyNonce`: counterexample; `SharedAttemptDomain`: no counterexample (masked, Q3) | granted at current nonce; granted after crash and retry in a later round; transaction granted | 6/6 full, match (normal, -race); 3/3 weakened expectations do not match |
| P-K08-EVIDENCE-RETAINED | **pass** | enf: A + U, 2 challenges, deadline tick 3, 2 crashes, 1 restore, 1 lost reply | 244,260 | `DeleteAtSettlement`, `RecordAfterGrant`: counterexample, DEFENSE not broken | losing replacement defended after refund | enf 11/11 partial (executor stages are fixtures); record-before-grant checked at every real grant in the hb replay; `DeleteAtSettlement` expectations do not match the code (`RecordAfterGrant` is not observable in the enf replay, logged) |
| P-K08-DEFENSE-BEFORE-DEADLINE | **blocked at host scope** (F4, Q1); component model passes under A-V09-DEFENDER-INVOKED | enf (same run) | 244,260 | `DefendAfterRecovery`, `ProvenOnly`, `AbortOnFirstFailure`: counterexample, EVIDENCE not broken | heartbeat defended; defense after restart before the deadline; first challenge not ours, second defended; lost reply settled by reread; unauthorized signature timed out, wallet terminated | enf 11/11 partial, match; from each DEFENSE counterexample state the real `Defender` (restart, one pass) resolves A |
| P-K08-NO-SUSPECT-SANCTION | **pass** | claim: 3 seats, threshold 2, 3 rounds F = 2; and 4 rounds F = 3 (node value) | 278,464; 17,163,806 | `ClaimFromSuspect`, `SingleFailureClaim`, `SkipUnknownRound`: counterexample at both bounds | real claim names exactly the absent seat; a started round without outcome ends the window | 10/10 full, match (normal, -race); 6/6 weakened expectations do not match |

Every claim-model run completed exhaustively before its 800 s deadline
(R-09); every weakened run stopped at its target, then a second exhaustive
run of the weakened model recorded which other safety properties it breaks
(none). In the hb and claim profiles there is one safety property per
profile, so that second run is informative only in enf.

## Runs (limits recorded before each run: 4 threads, GOMAXPROCS 4, 900 s, 20 GiB)

| Command | Seconds | Peak tree RSS |
| --- | --- | --- |
| model hb (5 checks + 3 exhaustive weakened) | 0.6 | 24 MiB |
| model enf (6 + 5) | 2.1 | 75 MiB |
| model claim (4 + 3) | 0.9 | 207 MiB |
| model claim-f3 (4 + 3) | 24.8 | 3.4 GiB |
| replay signing / enforcement, normal | 1.2 / 1.2 | 0.3 GiB |
| replay signing / enforcement, -race | 2.6 / 2.2 | 0.3 GiB |
| suites enforcement, signing (heartbeat, defense, V-09), transport, normal and -race | 0.9 to 5.5 each | 0.8 GiB |

Replay counts: 27 traces; 16 full, 11 partial, 0 skipped. Partial means the
`Defender` is real and the executor's record stages and the store's crash
outcomes are fixture writes (A-V09-STORE); a stale restore is a reader that
fails closed.

## Findings (KC-VER 2.6)

No implementation defect and no replay mismatch.

- Model defect, found during development by reading `Defender.Defend`, fixed
  before the evidence run: the model's defender skipped a challenge by its
  live status; the code uses the `Resolved` flag of the pass's snapshot and
  settles a meanwhile-resolved challenge by its reread. The model now does
  the same. No counterexample was produced by the defect.
- Observation (D4, by design per DECISIONS F3): an unsigned heartbeat with
  full presence is skipped, so a claim window can span a heartbeat that was
  signed elsewhere if every claim signer's own signing errored. The
  record-level predicate holds; the ground-truth reading would not. For
  owner awareness only.
- Observation (Q3): `SharedAttemptDomain` is masked by attempt ids that bind
  the intent and by exclusive claims; the KC-VER control "reuse the attempt
  for another purpose" is represented by `ReuseAttempt`.
- Notes, outside scope: `executor.go` `PossibleRelease` comment says no
  durable record sits between the intent and the worker (the defense records
  do, D11); `defense.go`, `heartbeat.go` and `defender_test.go` are not
  gofmt-clean at `925103a6c`; a host outside the heartbeat selection writes a
  heartbeat defense record although it never grants (harmless).

## Freshness

`check-freshness.py` (store checker design with package manifests, plus
trace manifests and external pins): S-K08-ENFORCE-1 **fresh** at this branch
with `--repo tbtc-v2=...@39412825 --repo kc-ver-02=...@51b9a16`; stale at
`925103a6` and against tbtc-v2 `677da343` (negative controls in
`freshness.log`).

## Gaps and open questions

Gaps G1 to G5 and questions Q1 to Q4 are in `SCOPE.md` sections 6 and 7.
G4 is closed: F = 3 over four rounds ran (17.2 million states, 6 s).
