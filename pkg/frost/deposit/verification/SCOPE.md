# V-07 scope re-pin (6 November 2026)

Written before any V-07 model or replay code. It compares the boundary contract
`BOUNDARIES.md` (16 October 2026, keep-core `3f01ebcaa`, tbtc-v2 `64b98185`)
with the subjects of this step and fixes what the model and the replay cover.
`BOUNDARIES.md` and `source-pins.json` stay as written; this file supersedes
them for V-07 where they differ.

## Subjects

| Item | Revision |
| --- | --- |
| keep-core | `tuxxy/integration/snowfall-base` `925103a6cf28bd9b4ddeacb4a457b3d4346b9d4f` |
| tbtc-v2 (C-05, C-04) | `tuxxy/integration/frost-base` `39412825454ae9d7d56811ce48cd826c92b821c2` |
| snowfall client (go.mod replace) | `c1847ebdf133` |
| KC-VER rev 02 draft | `kc-ver-02` `51b9a16`, `keep-core-verification-spec.md` blob `ab1815f3` |
| KC-SF-V3 rev 01 | `keep-core-integration-spec.md` blob `98f8589a` (unchanged since `BOUNDARIES.md`) |
| Decisions | `K-09-restart/DECISIONS.md`: VD-07, VD-10 (KC-VER section 7), the D13 refinement (tux, 30 October 2026) |

The V-07 section of KC-VER is byte-identical between the blob `BOUNDARIES.md`
pinned (`2ab227a2`, spec PR #4 `e8936025`) and `ab1815f3`; only the header,
V-04 rows and the revision record changed. The five predicates are therefore
the ones `BOUNDARIES.md` restates.

## Differences from BOUNDARIES.md

Each row is a difference between the 16 October contract and the code at the
subject heads, with how this step treats it.

| # | Area | 16 October contract | Code at `925103a6` / `39412825` | Treatment |
| --- | --- | --- | --- | --- |
| S-01 | D13 refinement | "Main reserved: an ordinary sweep **must not** include it"; a deposit-only sweep beside a reserved main is allowed and never takes the lock. | Unchanged in `pkg/frost/deposit`: `Planner.Build` plans deposit-only while `ActiveReservation != 0`, and `Authority.mainRule` accepts a no-main plan while the main is reserved. C-05 `consumeInputs` still accepts `includeMain == false` while `activeReservation != 0` (`FrostDepositSweep.sol:387-391`). Only the node duty `pkg/tbtc/frost_node_duties.go` `sweepDeposits` refuses, at the coordination block read, and requires the plan to include an existing main. The D13 refinement (30 October) removes the side path on both sides; the remediation is tracked as tuxxy/tbtc-v2#24 with unfrozen tests in tuxxy/tbtc-v2#52 and tuxxy/keep-core#105. | The model keeps the code's guards (R-02): planner and authority as in `pkg/frost/deposit`. A D13 predicate is added below, **proposed, not in KC-VER**. Its expected result at these heads is a counterexample, classified against the tracked remediation. |
| S-02 | C-10 exposure reservations | absent | `View.Sweepable` refuses `ErrUnreserved` unless the deposit holds a C-10 reservation at the head and `FinalityBlocks` earlier; the planner requires the same; `FrostSweepMaintainer.ReserveDeposit` reserves with an SPV proof; C-05 `creditBank` reports to the exposure controller. | Not a V-07 predicate (V-10 owns C-10). The model folds "reserved at a final depth" into "revealed at a finalized block" (both are finality reads of the same snapshot). Recorded as abstraction A-V07-C10. Replay sets every deposit reserved. |
| S-03 | Wallet sync check (review B1) | absent | `Planner.Synced`: no new plan while a reserved, unswept deposit's funding is spent on Bitcoin before its locktime. | Planner-only (not a grant guard). Modeled as a planning refusal. |
| S-04 | Exact proposal fee (D19) | fee from `FeeRate` | `Planner.Fee` carries the leader's exact fee; `signing.NewSweepPlan` still refuses a share above a deposit's `maximumSweepFee`. | The fee-cap guard is at plan construction, not at the grant. The model lets the environment propose an over-cap fee; the honest planner refuses it; weakening `AcceptFeeAboveCap` removes that refusal. |
| S-05 | Late signature handling (ADV-signing S2, `2ee480729`) | a late signature is quarantined | After `ErrLateSignature` the sweeper rechecks the authorization. Only a terminal refusal (`terminal()`) records `sweep-late-quarantined`. An unread authorization returns `ErrUnauthorized`, keeps the retained signature unaccepted, keeps the main lock, and the next round signs again. | Modeled: the acceptance recheck has three outcomes (pass, terminal refusal, read failure). |
| S-06 | Possible release marker (#50) | absent | `Sweeper.Sign` appends `possible-release` after `Run` when the intent was retained. | Not a V-07 predicate (V-04/V-05). Not modeled. |
| S-07 | Main lock purposes | free, redemption, resolution, sweep | adds `movement` and `closed`; `AcquireMovement` may supersede an aborted sweep (K-07, V-08); `Close` needs the custody output spent at the finalized head. | `closed` is modeled. `movement` belongs to V-08 and is not modeled; recorded as gap G-V07-MOVEMENT. |
| S-08 | Readiness-certified seats (`e5fdaef29`) | "every descriptor seat" | `Sweeper.selected` signs only with seats in the accepted readiness certificate. | V-03/V-04 domain; not modeled. |
| S-09 | Defense records (V-09) | absent | executor hook `before-defense-record`; every digest is retained before any grant. | Not a V-07 predicate; not modeled. |
| S-10 | Node dispatch | "no production dispatch" | `pkg/tbtc/frost_node_duties.go` runs sweeps through `Sweeper.Sign` per round (`signInRounds`) and `Sweeper.Broadcast`; it never calls `Sweeper.Resolve`. | The model's retry is `Sweeper.Resolve` (the deposit-package path the predicates name). The node loop is outside the model; see note N-01. |
| S-11 | C-05 contracts | pinned at `64b98185` | `FrostDepositSweep.sol` (C-10 `completeDeposits`, `FrostMovement.reopen`/`onMainSpent`, `provenTransactions`), `FrostDepositAdmission.sol` (exposure admission gate), `FrostFundingState.sol` (`reservedAt`), `FrostRedemptionLedger.sol` changed. Main inclusion, consumption (`sweptAt`, `spentMainUTXOs`), fee split and the resolution route are unchanged in substance. | The abstract contract keeps: consumption once, refund conflict on Bitcoin, the main inclusion rule as at `39412825`, the resolution route. C-10 budget refusals are not modeled. |
| S-12 | Ledger capacity | not mentioned | `redemption.Ledger.Append` refuses ordinary kinds (`sweep-attempt`) beyond capacity; recovery kinds use the reserve. | Not modeled (bound: one plan family). |

Pinned keep-core files whose blob changed since `source-pins.json`:
`pkg/frost/signing/executor.go`, `plan.go`, `pkg/frost/redemption/ledger.go`,
`resolver.go`, `agreement.go`, `attestation.go`, `pkg/bitcoin/taproot_transaction.go`.
Unchanged: `pkg/frost/signing/cancellation.go`, `pkg/frost/store/signing.go`.
Pinned tbtc-v2 files whose blob changed: `FrostDepositSweep.sol`,
`FrostDepositAdmission.sol`, `FrostFundingState.sol`, `FrostRedemptionLedger.sol`.
Unchanged: `FrostDepositProfile.sol`, `FrostSweepResolutionCertificate.sol`, `FrostCustody.sol`.
The new pins are the package manifests in `STATEMENTS.json`.

## Hooks at 925103a6

Every hook name in `BOUNDARIES.md` exists in production code, as an unexported
`boundary func(string) error` field, nil in production:

| Component | Hooks found | Settable from |
| --- | --- | --- |
| `View` (`view.go`) | `before-deposit-read`, `deposits-seen`, `refund-margin-seen` | package `deposit` tests |
| `Planner` (`planner.go`) | `before-sweep-plan` | package `deposit` tests |
| `MainLock` (`lock.go`) | `before-main-lock`, `main-lock-acquired`, `before-main-lock-transfer`, `main-lock-transferred` | package `deposit` tests |
| `Sweeper` (`sweeper.go`) | `agreement-derived`, `before-sign`, `after-sign`, `before-late-quarantine`, `outcome-seen` | package `deposit` tests |
| `ResolutionAttester` (`resolution.go`) | `before-resolution-attest`, `after-resolution-attest` | package `deposit` tests |
| `Executor` (`pkg/frost/signing/executor.go`) | `transaction-frozen`, `before-intent-record`, `before-defense-record` (new), `before-worker-start`, `after-signature`, `before-done-accept`, `before-broadcast`, `before-rebroadcast` | package `signing` tests only |
| `FrostSweepMaintainer` (`pkg/maintainer/spv/frost_sweeps.go`) | `before-sweep-proof`, `sweep-proof-submitted`, `credit-seen`, `before-deposit-reservation` (new), `deposit-reservation-submitted` (new) | package `spv` tests only |

Missing or unreachable, recorded as replay limitations (no hook is added here):

- **L-01 Executor hooks and the worker are unreachable from package `deposit`.**
  The executor's `boundary` and its `engine` are unexported in package
  `signing`. `NewLocalExecutor` needs a SNOWFALL worker. A sweep grant is
  therefore replayed at the exact calls `finalAuthorization.BeforeSigning`
  makes for a sweep (`Authority.AuthorizeSweep`, then `Guard.Grant`), not
  through `Executor.Run`. The executor's wallet and prevout checks
  (`Executor.bitcoin`) and the store attempt claim are not executed. Such
  steps are **partial** (H-05).
- **L-02 No hook between `Guard.Grant`'s record insert and the worker start.**
  A crash there is replayed as a crash after `Guard.Grant` returns.
- **L-03 No hook inside `Sweeper.Sign` between the result record and the
  `sweep-signed` marker.** The lost-marker case is replayed by making the
  controlled ledger journal fail the marker write.
- **L-04 Subprocess replay (H-02) is not used.** Hooks are only settable
  in-package, so the harness is an in-process lockstep test in package
  `deposit` (as `pkg/frost/signing/v04_replay_test.go` is for V-04). Crashes
  are replayed by discarding every in-memory component and rebuilding it over
  the same durable journal.
- **L-05 The maintainer is in package `spv`.** Its hooks are unreachable from
  package `deposit`. Proof steps are replayed against the abstract chain with
  the same rule `ProveSweep` uses (reread before reporting failure); they are
  **partial**.

## Model scope

**Bounds (KC-VER V-07, no reduction):** one wallet, two deposits, one main
output (the initial main `M0`; outputs created by later spends are tracked so
that "a sweep after a confirmed payment uses the new main" is reachable), one
redemption reservation, refund locktime at tick 2, ticks 0 to 2 (three ticks),
two sweep attempt slots. The refund margin is one tick (a D-09 test value,
OPEN): a deposit is sweepable at tick 0, at its margin at tick 1, refundable at
tick 2. Two sweep rounds (0 and 1) for the two attempts.

**Processes:** one honest host (planner, lock, sweeper, executor, attester),
one maintainer, the depositor, and the environment (C-05/C-04 chain, a
foreign quorum that may attest the reservation without this host, Bitcoin).

**Crashes (R-05):** a host crash or process crash stops every in-progress host
operation; durable records written before it survive. Each durable write is
one model step, so a crash lands before or after it. The store's own crash
behaviour (an unacknowledged write may or may not survive) is V-01's
(S-K02-HOST-4) and is discharged there; here a write is either done or not,
and the `sweep-signed` marker and the result record may additionally fail with
an error after being persisted or not.

**Guards modeled (R-02), each with its code location:**

| Guard | Code |
| --- | --- |
| reveal final and canonical at the head | `View.Read`, `View.Sweepable` |
| unswept | `View.Sweepable` (`ErrSwept`) |
| MTP + margin < locktime at every authorization | `Policy.BeforeMargin` via `Authority.AuthorizeSweep` |
| prevouts unspent (deposit funding, main) at start and at grant | `Executor.bitcoin` |
| C-05 main rule | `Authority.mainRule` |
| resolution reservation unresolved | `Authority.resolutionState` |
| fee share within the captured cap | `signing.NewSweepPlan` |
| main lock purposes and grant records | `MainLock`, `Guard.Permit`, `Guard.Grant` |
| finalized non-admission before release | `MainLock.ReleaseUnadmitted` |
| acceptance recheck after the signature | `Executor.Run` (`before-done-accept`), `Sweeper.Sign` |
| late quarantine only on a terminal refusal | `Sweeper.Sign` (`terminal`) |
| result found again from attempt entries | `Sweeper.signedRound` |
| quarantined sweep never broadcast | `Sweeper.Resolve` |
| one attempt per round | `Sweeper.Resolve` (attempt entry for the current round), store attempt claim |
| maintainer rereads before reporting a failed proof | `FrostSweepMaintainer.ProveSweep` |
| contract consumption key, main inclusion, resolution route | C-05 `consumeInput`, `consumeInputs`, `proveResolution` (abstract) |
| planner eligibility, sync check | `Planner.eligible`, `Planner.Synced`, `Planner.Build` |

The planner's finality rule reads relative to its anchor block, which may
itself be unfinal; the model over-approximates it as "reveal present" (more
plans than the code makes). The authority's head-finality rule is the guard.

## Predicates as model observers

Restated from KC-VER V-07 and `BOUNDARIES.md`. "Grant" is the release point
(`BeforeSigning` passing for a sweep: authority, then `Guard.Grant`).

- **P-K06-SWEEP-AUTHORIZED-ONLY.** At every sweep grant: every deposit of the
  plan is revealed at a finalized block, unswept, its funding unspent, and
  before its margin by MTP; the plan's fee shares are within the caps; an
  unreserved existing main is included. (The BIP342 digest for the exact leaf
  is byte-level: K-04/K-06 vectors and Bitcoin Core, outside the model.)
- **P-K06-ONE-MAIN-PURPOSE.** No grant of an ordinary sweep that spends a main
  while a reservation on it is unresolved: admitted on chain (unfinal or
  final), or attested by this host with a certificate that can still be
  admitted (deadline not passed; VD-07 reading 1). A resolution sweep grant
  needs its reservation unresolved. No payment grant for the reservation after
  this host released a sweep that spends its main.
- **P-K06-REFUND-RACE.** No sweep grant at or past the margin; no accepted
  (result) sweep signature whose acceptance ran at or past the margin; no
  broadcast of a quarantined sweep; a refunded deposit is never credited.
- **P-K06-CREDIT-ONCE.** Each deposit is credited at most once.
- **P-K06-TERMINAL-SWEEP-ATTEMPT.** No second grant under one attempt id
  (plan, round); no grant in a new attempt for a plan that already has a
  durable accepted result (the result is found again even when the marker is
  lost).

**Proposed, not in KC-VER (decision D13 refinement, 30 October 2026):**

- **P-K06-D13-NO-SIDE-SWEEP.** The honest host never releases a deposit-only
  sweep grant while the wallet has a recognized main with an unresolved
  reservation on it (admitted, unfinal or final, at the grant's read). Source:
  `DECISIONS.md` "D13 refinement": "the honest K-06 host neither plans nor
  authorizes it". Weakening: none, because the code at `925103a6` has no such
  guard in `pkg/frost/deposit` (R-02). Witness: none beyond the V-07 ones.
  The predicate is reported separately and does not change the five KC-VER
  results.

## Weakenings (R-07)

From `BOUNDARIES.md`, with the guard each removes:

| Weakening | Removes | Declared property |
| --- | --- | --- |
| `SignWithoutRevealFinality` | head-finality of the reveal in the authority | SWEEP-AUTHORIZED-ONLY |
| `PlanTimeMarginOnly` | the MTP margin check at grant and acceptance (planning margin kept) | SWEEP-AUTHORIZED-ONLY |
| `AcceptFeeAboveCap` | the cap check in `NewSweepPlan` | SWEEP-AUTHORIZED-ONLY |
| `DropMainLock` | `MainLock` and `Guard` (no lock taken, no grant check) | ONE-MAIN-PURPOSE |
| `LockFromUnfinalizedReservation` | the finalized-deadline condition of `ReleaseUnadmitted` | ONE-MAIN-PURPOSE |
| `NoRefundMargin` | the margin everywhere (planner and authority use margin 0) | REFUND-RACE |
| `AcceptLateSweepSignature` | the acceptance recheck after the signature | REFUND-RACE |
| `ForgetConsumptionOnRetry` | the C-05 consumption key and the maintainer's reread (it resubmits) | CREDIT-ONCE |
| `ReuseSweepAttempt` | attempt terminality at both of its layers: the ledger round entry checked by `Resolve` and the store claim | TERMINAL-SWEEP-ATTEMPT |
| `TrustLedgerMarkerOnly` | the attempt-entry reconciliation in `signedRound` | TERMINAL-SWEEP-ATTEMPT |

KC-VER puts the margin in both SWEEP-AUTHORIZED-ONLY and REFUND-RACE, so the
two margin weakenings necessarily break both; R-07's "exactly that predicate"
cannot hold for a shared clause (open question Q-V07-1). `ForgetConsumptionOnRetry`
and `ReuseSweepAttempt` remove two code sites each because the rule lives in
two layers; `BOUNDARIES.md` already defined the first that way (Q-V07-2).
Each weakened run stops at the first counterexample of its declared property
(R-08); the other properties discovered by then are recorded, not claimed
exhaustive.

## Witnesses (`sometimes`)

W-authorized-sweep-credited; W-sweep-after-payment-new-main;
W-resolution-sweep-resolves; W-refund-operators-stopped;
W-sweep-before-margin-credited-after-locktime; W-lost-reply-credits-once;
W-fresh-attempt-after-failure.

## Replay classification (H-05)

- **full:** every step runs the real deposit-package component it names
  (planner, view/authority, main lock, guard, ledger, resolution attester) on
  controlled providers, and no step needs the executor, worker or maintainer.
- **partial:** at least one step is projected (L-01, L-03, L-05).
- **none:** a step has no seam at all (contract-only effects are applied to
  the controlled provider, which does not count as executing a component).

## Notes

- **N-01 (node loop, outside the model).** `signInRounds` treats any `Sign`
  error as a failed round and signs the same plan again in the next round.
  If the result record is durable but the `sweep-signed` append fails, the
  node re-signs the identical transaction under a fresh session instead of
  finding the result (`Broadcast` would find it). Same plan and same bytes,
  so no conflicting spend; recorded, not a predicate failure.
- **N-02 (dead sweep holds the main).** A released main-spending sweep whose
  deposit is later refunded keeps `sweep(plan)` until an alternative mines
  (VD-07 reading 2); redemptions on that main wait. Liveness, not modeled.

## Open questions

- **Q-V07-1.** KC-VER lists "ignore the refund margin" under two predicates.
  Should the margin clause belong to one (REFUND-RACE) so that each weakening
  breaks exactly one predicate?
- **Q-V07-2.** Are compound weakenings acceptable where one rule is enforced
  at two layers (`ForgetConsumptionOnRetry`, `ReuseSweepAttempt`), or should
  each layer get its own weakening and a "does not break alone" result?
- **Q-V07-3.** Should P-K06-D13-NO-SIDE-SWEEP enter KC-VER (rev 03) as a V-07
  predicate, with its contract-side obligation for C-05?
