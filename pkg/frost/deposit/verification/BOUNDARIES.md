# K-06 / V-07 predicate and replay boundary contract

Recorded on 16 October 2026, before any K-06 code. This is the V-07 scope document
that KC-VER section 4 requires before K-06 code. It restates the five V-07
predicates as host invariants and fixes the boundary hook names, the controlled
providers, the negative controls, the witnesses and the bounds. It is an
implementation contract for review. **It is not a model result.** The model is
checked before K-06 merges. The replay runs at the K-06 exit.

## Governing sources

| Source | Revision | Blob |
| --- | --- | --- |
| KC-SF-V3 `keep-core-integration-spec.md` (sections 4.5, 4.6, K-06, C-05, 8.3) | spec `d45afdab` | `98f8589a` |
| KC-VER rev 02 `keep-core-verification-spec.md` (V-07, VD-07, VD-10) | spec PR #4 `e8936025`, unmerged | `2ab227a2` |
| keep-core base (top of stack #16, V-05) | `3f01ebcaa` | see `source-pins.json` |
| tbtc-v2 C-05 and C-04 contracts (top of stack #8, K-05) | `64b98185` | see `source-pins.json` |

KC-VER rev 02 is still under owner review. If it changes V-07, this document is
re-pinned before the model is frozen.

## What the contracts already enforce

I read C-05 `FrostDepositSweep.sol` at `64b98185` before fixing the hooks. The
host plan must match these rules byte for byte. They are tested with vectors and
Bitcoin Core, not modeled (KC-VER 2.1).

| Rule | Contract check | Host consequence |
| --- | --- | --- |
| Version 2, empty scriptSig, at most 128 inputs | `FROST sweep version`, `scriptSig`, `inputs bound` | the planner refuses a larger set |
| Exactly one output, `OP_1 PUSH32 Q` of the wallet | `FROST sweep output count`, `output key` | single output to the descriptor's Q |
| Fee split is `fee / count`, remainder on the **last** deposit, each share at most that deposit's `maximumSweepFee`, and `amount > treasuryFee + share` | `FROST sweep fee capacity` | the host computes the same split; deposit order decides who pays the remainder |
| All deposits in one sweep share one vault | `FROST sweep deposit` | the planner groups by vault |
| Main present and unreserved: the sweep **must** include main. Main reserved: an ordinary sweep **must not** include it | `FROST sweep main required`, `main reserved` | see the main purpose lock below |
| A reserved main is spent only by the resolution route, with an operator certificate over `(reservationId, txHash, vault, validUntilBlock)` | `proveResolution` | the host attests that certificate (new host action, below) |
| Each outpoint is consumed once, across schemes | `spentMainUTXOs`, `sweptAt` | contract side of P-K06-CREDIT-ONCE |
| The refund leaf is `CLTV` on a Unix timestamp (`refundLocktime >= 500000000`) | `FrostDepositProfile.refundLeaf` | the refund margin is compared with Bitcoin median-time-past (BIP113), not block height |

The contract has **no refund-margin check**. Bitcoin enforces the refund race:
whichever of the sweep and the refund mines first wins. The margin is a host
rule only, so P-K06-REFUND-RACE rests on the host.

## Predicates as host invariants

The vocabulary is the set of events the hooks below make observable. "Release"
means a worker receives a signing grant (the K-04 meaning). "Finalized" means at
or below the D-09 finality depth on the canonical chain.

**P-K06-SWEEP-AUTHORIZED-ONLY.** A sweep grant is issued only if all of these
held in the deposit view read after the durable claim (`deposits-seen`, final
read at `before-worker-start`):

- every deposit input is revealed for this wallet at a finalized block, is not
  swept, and its funding output is unspent on Bitcoin;
- `median-time-past + margin < refundLocktime` for every deposit;
- the transaction has one output to Q, the contract's fee split, each share
  within its cap, and one vault;
- the main input is included exactly when the main lock allows it;
- every deposit input's digest is the host-computed BIP342 digest for that
  deposit's exact wallet leaf. The main input uses the K-04 key-path digest.

Anything else refuses before the worker starts. A changed reveal, an orphaned
reveal block or a spent funding output between the reads also refuses.

**P-K06-ONE-MAIN-PURPOSE.** For each `(wallet, main outpoint)` the main lock
holds one purpose at a time: `free`, `redemption(r)`, `resolution(r, txHash)` or
`sweep(plan)`. Rules:

- a grant whose transaction spends the main requires the lock to hold that grant's purpose;
- `free → redemption(r)` at the first durable attestation for a reservation on this main;
- `free → sweep(plan)` at the durable claim of a main-spending sweep;
- `redemption(r) → resolution(r, txHash)` only when the host attests the C-04
  sweep-resolution certificate for that exact reservation and transaction;
- after `resolution(r, …)` is recorded, no payment or cancellation grant for `r` is issued;
- the lock returns to `free` (for the **new** main) only on a finalized chain
  outcome that spends or releases the old main: payment proof, cancellation
  proof, resolution proof, sweep proof, or finalized non-admission of `r` after
  its certificate deadline (see VD-07 reading below).

A deposit-only sweep (no main input) never takes the lock.

**P-K06-REFUND-RACE.**

- No sweep grant is issued for a deposit at or past its margin.
- A sweep signature retained after any of its deposits passed the margin is
  recorded `sweep-late-quarantined` and is never broadcast or rebroadcast by this host.
- A refunded deposit never yields credit (Bitcoin conflict plus C-05 consumption).
- The refund needs no operator action: the refund witness is built from the
  depositor's key and the public construction alone.

**P-K06-CREDIT-ONCE.** Across sweep retries, maintainer restarts, alternative
sweeps, the resolution route and a proof whose reply is lost, each funding
outpoint is credited at most once. On the host this means the maintainer treats
"already consumed" as success and never builds a second sweep that includes an
outpoint whose proof it submitted until that submission is resolved on chain.

**P-K06-TERMINAL-SWEEP-ATTEMPT.**

- A failed, crashed or cancelled sweep attempt stays terminal.
- A retry uses a fresh chain-derived session (`AgreeRound` with purpose `sweep`).
- A durable accepted result is found again from the executor's own records
  even when the `sweep-signed` ledger marker is missing (the K-05 F2 rule).

## Proposed components

These names are proposals for the K-06 code. The hooks are binding; the type
names may change in review.

| Component | Package | Role |
| --- | --- | --- |
| Deposit view | `pkg/frost/deposit` | reads C-05 reveals, finality, sweep and refund state, and the Bitcoin funding output from one canonical snapshot |
| Sweep planner | `pkg/frost/deposit` | builds the exact sweep from finalized reveals; contract fee split; groups by vault; refuses at the margin |
| Main purpose lock | `pkg/frost/deposit` (store records under `pkg/frost/store/signing.go`) | durable compare-and-insert transitions above; append-only history |
| Sweep purpose in signing | `pkg/frost/signing` | new purpose `sweep`; BIP342 script-path inputs beside key-path main; intent v2 extended, new vectors |
| Sweeper | `pkg/frost/deposit` | agreement, claim, sign, broadcast, quarantine and outcome, reusing the K-05 ledger and its recovery reserve |
| Resolution attester | `pkg/frost/deposit` | signs the C-04 sweep-resolution certificate after the lock transition |
| Sweep maintainer | `pkg/maintainer/spv` | proves sweeps (ordinary and resolution) idempotently |
| Refund tool | `pkg/frost/deposit` plus the TB SDK | builds and signs the depositor refund with no operator |

## Boundary hooks

Each component gets the K-04 test-only `boundary func(string) error`, nil in
production. The executor keeps its existing names. Names in **bold** are new.

| Component | Hooks, in order |
| --- | --- |
| Deposit view | **`before-deposit-read`**, **`deposits-seen`**, **`refund-margin-seen`** |
| Sweep planner | **`before-sweep-plan`**, then the executor's `transaction-frozen` |
| Main lock | **`before-main-lock`**, **`main-lock-acquired`**, **`before-main-lock-transfer`**, **`main-lock-transferred`** |
| Sweeper | `agreement-derived`, `before-sign`, `after-sign`, **`before-late-quarantine`**, `outcome-seen` (same names as the K-05 resolver) |
| Executor (shared) | `before-intent-record`, `before-worker-start` (final reread of deposits, margin and lock), `after-signature`, `before-done-accept`, `before-broadcast`, `before-rebroadcast` |
| Resolution attester | **`before-resolution-attest`**, **`after-resolution-attest`** |
| Sweep maintainer | **`before-sweep-proof`**, **`sweep-proof-submitted`**, **`credit-seen`** |

The executor's `before-reservation-read` and `reservation-seen` are not used for
sweeps. The final deposit and lock read runs after the durable claim and
immediately before the worker gets its grant, as in K-04.

New ledger kinds for the K-05 signed-intent ledger: `sweep-attempt`,
`sweep-signed`, `sweep-broadcast`, `sweep-late-quarantined`, `sweep-outcome`.
The last four are recovery kinds and may use the reserve. `sweep-attempt` is ordinary.

## Controlled providers (H-03)

| Provider | Knobs |
| --- | --- |
| Deposit view | reveal pending, finalized, orphaned by reorg, already swept, funding output spent by refund |
| Bitcoin clock | median-time-past set per step, so the margin can be crossed between any two hooks |
| Bitcoin | conflicting spends, first mined wins; refund valid only when MTP passes `refundLocktime` |
| Reservation (from V-04) | none, unfinalized, admitted, timed out, finalized non-admission |
| Chain writer | proof commits and the reply is lost; proof reverts "already consumed" |
| Store | the K-02 fence and stage faults, unchanged |

## Negative controls (model weakenings, R-07)

Each must break exactly its predicate. KC-VER lists "ignore the refund margin"
under both P-K06-SWEEP-AUTHORIZED-ONLY and P-K06-REFUND-RACE. I split it so each
control breaks one predicate. The split is for review at the model freeze.

| Predicate | Weakening | Breaks |
| --- | --- | --- |
| SWEEP-AUTHORIZED-ONLY | `SignWithoutRevealFinality` | grant on an orphaned reveal |
| SWEEP-AUTHORIZED-ONLY | `PlanTimeMarginOnly` (margin checked at plan, not at `before-worker-start`) | grant after the margin passed between reads |
| SWEEP-AUTHORIZED-ONLY | `AcceptFeeAboveCap` | grant with a share above `maximumSweepFee` |
| ONE-MAIN-PURPOSE | `DropMainLock` | sweep and payment both granted on one main |
| ONE-MAIN-PURPOSE | `LockFromUnfinalizedReservation` | lock freed by an orphaned outcome |
| REFUND-RACE | `NoRefundMargin` | grant at or past the margin |
| REFUND-RACE | `AcceptLateSweepSignature` | broadcast of a late signature |
| CREDIT-ONCE | `ForgetConsumptionOnRetry` (host resubmits as new; abstract contract without its consumption key) | double credit |
| TERMINAL-SWEEP-ATTEMPT | `ReuseSweepAttempt` | second grant under one attempt |
| TERMINAL-SWEEP-ATTEMPT | `TrustLedgerMarkerOnly` | a durable result lost after the marker write fails |

Each weakening also gets a failing Go regression where the guard is real code,
as in K-05 (revert the guard, the test fails).

## Witnesses (`sometimes`)

- An authorized sweep is proven and credited.
- A sweep after a confirmed payment spends the new main.
- A resolution sweep refunds the reserved requests.
- A refund mines with all operators stopped.
- A sweep before the margin is credited.
- A retry after a lost proof reply credits once.
- A fresh attempt succeeds after a failure.

## Bounds (from KC-VER V-07)

- One wallet, two deposits, one main output and one redemption reservation.
- Refund locktime at tick 2; three ticks.
- Two sweep attempts.

Crashes are possible at every hook above. No liveness claim is made beyond the
witnesses.

## VD-07 readings for owner review

1. **Finalized non-admission frees the lock.** VD-07 lists proof, cancellation
   and resolution as the outcomes that move the lock. An attested reservation
   that is never admitted must also free it, or the main stays locked. I read it
   as: finalized absence of admission at or before the certificate deadline, the
   same rule K-05 already uses to replace an attestation.
2. **A dead sweep keeps the lock.** If a released main-spending sweep includes
   a deposit that is later refunded, that sweep can never mine. The lock stays
   `sweep(plan)`. Recovery is a new sweep alternative that spends the same main
   without the refunded deposit, under the same lock purpose. A redemption never
   takes the main while a released sweep might still mine.
3. **The resolution certificate is attested by the host.** C-04
   `proveResolution` checks a threshold operator certificate. Signing it is a
   host action, so the lock transition to `resolution(r, txHash)` is durable
   before the host signs the certificate.

## Assumptions carried to V-05

- A-K06-MTP: the Bitcoin provider's median-time-past is canonical and current within the margin. The margin value is a D-09 test value, OPEN.
- A-K06-DEPOSIT-VIEW: one internally consistent C-05 snapshot at the canonical head, as for A-C04-CURRENT-FINALITY.
- A-D04-TEMPLATE: the D-04 bytes, tested by vectors and Bitcoin Core, not modeled.
- P-C07-TAPTWEAK for the deposit output relation (V-06).
- Contract obligations (KC-VER table): C-05 consumes a funding outpoint once and rejects refund-spent deposits. C-04 and C-05 consume the reserved main once on the resolution route and keep escrow.

## Not covered

- Exclusion between different signing quorums. That stays with the C-04 resolution proof route.
- Fee escalation beyond the captured cap.
- Reorganizations deeper than the finality depth.
- Executed power loss and the production fence.
