# V-07 results (6 November 2026), statement S-K06-DEPOSIT-1

Bounded model checking and concrete replay of KC-VER rev 02 V-07 on
keep-core `925103a6` and tbtc-v2 `39412825`. Not a proof. Scope, guards,
limitations and open questions: [`SCOPE.md`](SCOPE.md). Evidence:
`implementation/K-06/v07-model-2026-11-06/` (with `SHA256SUMS`).
Self-review only (VD-04).

## Bound

One wallet, two deposits, one main output (later outputs tracked), one
redemption reservation, refund locktime at tick 2, ticks 0 to 2, margin one
tick (D-09 test value, OPEN), two sweep attempt slots, two rounds; crashes at
every host stage; result and marker writes may fail persisted or not. This is
the V-07 bound with no reduction.

## Model (Stateright 0.31.0, Rust 1.94.0, single thread, 840 s deadline)

| Run | Result | States (unique / generated) | Depth | Seconds | Peak RSS |
| --- | --- | --- | --- | --- | --- |
| claim model | exhaustive, completed before deadline | 42,972,245 / 189,277,542 | 43 | 127.9 | 6.4 GB (whole model step) |

| Predicate | Claim model | Weakenings (each stops at its first counterexample) |
| --- | --- | --- |
| P-K06-SWEEP-AUTHORIZED-ONLY | pass | `SignWithoutRevealFinality` (33,783 states), `PlanTimeMarginOnly` (117,080; also breaks REFUND-RACE), `AcceptFeeAboveCap` (129,443): all break it |
| P-K06-ONE-MAIN-PURPOSE | pass | `DropMainLock` (116,364), `LockFromUnfinalizedReservation` (309,555): both break it |
| P-K06-REFUND-RACE | pass | `NoRefundMargin` (129,157; also breaks SWEEP-AUTHORIZED-ONLY), `AcceptLateSweepSignature` (506,124): both break it |
| P-K06-CREDIT-ONCE | pass | `ForgetConsumptionOnRetry` (4,720,967) breaks it |
| P-K06-TERMINAL-SWEEP-ATTEMPT | pass | `ReuseSweepAttempt` (3,582,836), `TrustLedgerMarkerOnly` (6,322,927): both break it |
| P-K06-D13-NO-SIDE-SWEEP (proposed, not in KC-VER) | **fail** (counterexample) | none (the code has no guard to remove) |

All seven witnesses were discovered: authorized sweep credited, sweep after a
confirmed payment on the new main, resolution sweep resolves the reservation,
refund mined with the host stopped, sweep before the margin credited after the
locktime, lost proof reply credits once, fresh attempt succeeds after a
failure.

Exhaustive weakened runs (the exactness matrix) are **blocked**: the first one
passed 15 GB at 106 s and was stopped before the 20 GiB ceiling. Runbook in the
evidence (`RUNBOOK-exactness.md`).

## Replay (`v07_replay_test.go`)

| Set | Traces | full | partial | none | Mismatches |
| --- | --- | --- | --- | --- | --- |
| normal | 18 | 4 | 14 | 0 | 0 |
| `-race` | 18 | 4 | 14 | 0 | 0 |

Full: `AcceptFeeAboveCap`, `DropMainLock`, `LockFromUnfinalizedReservation`,
`NoRefundMargin` (their divergence is in the real planner or main lock).
Partial: every trace that reaches the executor, the store claim, the worker or
the maintainer (SCOPE.md L-01 to L-05). Weakened traces replay with the claim
model's expectations up to their divergence (H-06); `replay-report.json`
records whether the divergence step was projected. A harness control with two
corrupted traces fails on both.

Affected suites pass normally and under `-race`: `pkg/frost/deposit` (all),
`pkg/frost/signing` (`Sweep`), `pkg/maintainer/spv` (`Frost|ReserveDeposit`).

## Finding

**F-V07-1, implementation defect (D13 refinement).** The claim model reaches a
deposit-only sweep grant while the wallet's main has an admitted reservation:
`Planner.Build` plans it, `Authority.mainRule` accepts a no-main plan while
`ActiveReservation != 0`, and `Guard.Grant` has no main to check. The trace
replays on the real code with matching outcomes, and a scratch test (not
committed) reproduces it. C-05 at `39412825` also still accepts such a proof.
Only the node duty (`pkg/tbtc/frost_node_duties.go`) refuses, at its
coordination-block read. Already tracked by tuxxy/tbtc-v2#24 (tests in
tuxxy/tbtc-v2#52 and tuxxy/keep-core#105, unfrozen). Not repaired here.

## Freshness

`python3 pkg/frost/deposit/verification/check-freshness.py --repo
spec=<FROST-migration-spec-kcver02>@kc-ver-02 --repo tbtc-v2=<tbtc-v2>@39412825`
reports S-K06-DEPOSIT-1 fresh. Four controls report it stale: tbtc-v2 at
`64b98185`, keep-core at `925103a6`, KC-VER at `e8936025`, and no spec
repository given.
