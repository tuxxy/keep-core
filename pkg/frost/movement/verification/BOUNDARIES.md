# K-07 / V-08 predicate and replay boundary contract

Recorded on 19 October 2026, before any K-07 code. This is the V-08 scope
document that KC-VER section 4 requires before K-07 code. It restates the five
V-08 predicates as host invariants and fixes the boundary hook names, the
controlled providers, the negative controls, the witnesses and the bounds. It
is an implementation contract for review. **It is not a model result.** The
model is checked before K-07 merges. The replay runs at the K-07 exit.

K-07 is one milestone. Movement and closure land in one stack: C-06, C-08 and
C-09 in tbtc-v2, then host movement and host closure here. D-03 and D-07 are
used as test values marked OPEN.

## Governing sources

| Source | Revision | Blob |
| --- | --- | --- |
| KC-SF-V3 `keep-core-integration-spec.md` (K-07, C-06, C-08, C-09, D-03, D-07) | spec PR #4 `51b9a16` | `98f8589a` |
| KC-VER rev 02 `keep-core-verification-spec.md` (V-08, VD-08) | spec PR #4 `51b9a16`, unmerged | `ab1815f3` |
| keep-core base (top of stack #16, K-06) | `cb7ae6a2c` | see `source-pins.json` |
| tbtc-v2 base (top of stack #8, K-06 TypeScript) | `2c4c3fb4` | see `source-pins.json` |

## What the legacy contracts do today

I read `MovingFunds.sol` and `Wallets.sol` at `2c4c3fb4` before fixing the
hooks. C-06 and C-09 keep these rules for legacy wallets and add the FROST
routes beside them.

| Rule | Where | Consequence for K-07 |
| --- | --- | --- |
| A commitment is accepted only in `MovingFunds`, with no pending redemptions and no pending incoming sweeps, once per wallet | `submitMovingFundsCommitment` | the host commits only from a clear source |
| Target count is `min(live wallets, ceil(balance / walletMaxBtcTransfer))`; targets strictly ascending, all `Live`, not the source | same | the planner derives the same list from chain state |
| The commitment is `keccak256(abi.encodePacked(targets))` over 20-byte labels | same | C-06 extends it to bind each FROST target's descriptor hash |
| One input (the main); every output is a committed target in order; each output in `[min, min + rem]` of the equal split; total fee at most the commitment's `maxFee` (`movingFundsTxMaxTotalFee` when committed) | `submitMovingFundsProof`, `processMovingFundsTxOutputs` | host builds `(value - fee) / n`, remainder on the last output |
| Each output becomes a moved-funds sweep request keyed `keccak256(txHash ‖ uint32 index)` with the target's pending counter incremented | same | the same key as `spentMainUTXOs` and FROST custody |
| **The target's state is not rechecked at proof time** | same | a target that left `Live` still receives a request |
| A moved-funds sweep is accepted only from a `Live` or `MovingFunds` target | `resolveMovedFundsSweepingWallet` | a `Closing` target cannot sweep |
| A moved-funds sweep timeout is accepted only for `Live`, `MovingFunds` or `Terminated` targets | `notifyWalletMovedFundsSweepTimeout` | a `Closing` target cannot time out either |
| `moveFunds` goes straight to `Closing` when there is no main, and `notifyMovingFundsBelowDust` goes to `Closing`, both ignoring pending incoming sweeps | `Wallets.moveFunds`, `notifyWalletMovingFundsBelowDust` | **the stranded-moved-funds counterexample** |
| Closing → Closed checks only elapsed time | `notifyWalletClosingPeriodElapsed` | closure has no obligation check |

**The stranded case.** A fresh target with no main receives moved funds while
it moves to `Closing` (by `notifyWalletCloseable`, heartbeat failure or a
redemption timeout). Its sweep and its sweep timeout both revert from then on.
The request stays `Pending` forever, the counter never reaches zero, the
wallet still finalizes to `Closed`, and the BTC sits at the target with no
on-chain path. `TestNoTargetAndDustRecovery` reproduces it on the legacy route
before showing the FROST recovery.

**FROST state today.** `FrostWallets` declares `MovingFunds`, `Closing` and
`Terminated`, but no code sets them. C-06 and C-09 are their first users.
FROST custody keys outputs with the same `keccak256(txHash ‖ uint32 index)`
as the legacy `spentMainUTXOs`, so one replay space already spans both schemes.

## Routes

| Route | Signer | Commitment and proof | Target sweep |
| --- | --- | --- | --- |
| ECDSA → ECDSA | legacy | legacy, unchanged (pending legacy commitments only) | legacy |
| ECDSA → FROST (drain) | legacy | legacy commitment extended for FROST targets; P2TR outputs | FROST sweep with an incoming input |
| FROST → FROST | FROST, new purpose `movement` | C-06 FROST commitment and proof | FROST sweep with an incoming input |
| FROST → ECDSA | none | **refused** | none |

After cutover a new commitment names only eligible FROST targets. An existing
legal legacy commitment still completes.

## Predicates as host invariants

The vocabulary is the set of events the hooks below make observable. "Release"
means a worker receives a signing grant (the K-04 meaning). "Finalized" means
at or below the D-09 finality depth on the canonical chain. "Commitment" means
the C-06 record: ordered target labels, each target's descriptor hash, the
source main outpoint and the fee cap.

**P-K07-COMMITTED-TARGET-ONLY.** A movement grant is issued only if all of
these held in the movement view read after the durable claim (`movement-seen`,
final read at `before-worker-start`):

- the source is in `MovingFunds` and its commitment is recorded at a finalized block;
- the transaction spends exactly the source main and nothing else;
- its outputs are, in order, `OP_1 PUSH32 Q_i` for the committed targets, where
  `Q_i` is read from the **committed descriptor hash**, never from the target's
  current record;
- the amounts are the equal split of `main - fee` with the remainder on the
  last output, and the fee is within the committed cap.

The target's current state is read but **never used to choose or reorder
outputs**. A target that becomes ineligible for new funding after the
commitment is still paid exactly as committed. A recovery transition (below)
is a Bitcoin spend signed by the target's own key; no host action reassigns a
committed output to another wallet's accounting.

**P-K07-INCOMING-ONCE.** Each moved output (`keccak256(movementTxHash ‖ index)`)
is consumed into target custody at most once across sweep retries, restarts,
replays, a lost proof reply and both proof handlers (legacy and FROST). On the
host the target sweep maintainer treats "already consumed" as success and
never plans a second sweep with an incoming outpoint whose proof it submitted
until that submission is resolved on chain. The contract side is the shared
`spentMainUTXOs` key (contract obligation, C-06).

**P-K07-NO-STRANDED-MOVEMENT.** Every committed movement is in exactly one of:

- `settled`: movement proven and every incoming output consumed into a target;
- `pending`: movement unproven with a reachable signature, or an incoming
  output with a reachable target sweep;
- `escalated`: the bounded retry or escalation state (timeout, recovery).

Host rules:

- a target with incoming pending at the finalized head is never treated as closable;
- the target sweeper picks up incoming outputs in every target state except
  `Closed` (and `Closed` is unreachable while incoming is pending);
- an under-dust residual gets a recorded disposition (reading 4) before the
  source may close.

**P-K07-CLOSE-ONLY-WHEN-CLEAR.** The host treats a wallet as closable only
when the closure ledger, read at one finalized head, is empty:

| Obligation | Read from |
| --- | --- |
| Main output (or a recorded dust disposition) | FROST custody |
| Pending outgoing movement | C-06 commitment and movement record |
| Pending incoming moved outputs | C-06 incoming counter |
| Redemption escrow, open requests, active reservation | C-04 |
| Open fraud challenges | C-08 |

A late arrival after the check (an incoming transfer proof or a fraud
challenge) reopens it: the host rechecks at `before-closure-notify` and the contract
rechecks at the `Closing → Closed` transition. Closing never deletes key material.
Revealed deposits are not an obligation: reveals never block. A late sweep
of a real deposit reopens a `Closing` or `Closed` wallet into `MovingFunds` in
a new period (reading 5).

**P-K07-SIGNER-RETENTION.** The completed key and the signing evidence stay
loadable until the closure predicate holds **and** every challenge window over
a digest this wallet released is finalized, across crash, restart and restore.
This follows the VD-08 rule: nothing is deleted automatically before the K-12
retirement predicate holds. The host's closure action is `archive`, which stops
new purposes and keeps the key loadable; there is no host delete path in K-07.

## Proposed components

These names are proposals for the K-07 code. The hooks are binding; the type
names may change in review.

| Component | Package | Role |
| --- | --- | --- |
| Movement view | `pkg/frost/movement` | reads the source wallet, its commitment, target records by committed descriptor hash, custody and incoming state from one canonical snapshot |
| Movement planner | `pkg/frost/movement` | derives targets for a new commitment from chain state; builds the exact movement transaction from a recorded commitment |
| Commitment submitter | `pkg/frost/movement` | submits the C-06 commitment as a FROST member; idempotent |
| Movement purpose in signing | `pkg/frost/signing` | new purpose `movement`; key-path main input; intent v2 kind byte 3 |
| Mover | `pkg/frost/movement` | agreement, claim, sign, broadcast and outcome, on the K-05 ledger |
| Main purpose lock | `pkg/frost/deposit` | new purpose `movement(commitment)` |
| Incoming sweep | `pkg/frost/deposit` | the K-06 planner and sweeper with incoming outputs as inputs beside deposits |
| Movement maintainer | `pkg/maintainer/spv` | proves movements and incoming sweeps idempotently |
| Closure checker | `pkg/frost/movement` | reads the closure ledger, notifies the contract, archives the wallet |
| Legacy drain | `pkg/tbtc` | legacy target selection and transaction assembly with P2TR outputs for FROST targets |

## Boundary hooks

Each component gets the K-04 test-only `boundary func(string) error`, nil in
production. Names in **bold** are new.

| Component | Hooks, in order |
| --- | --- |
| Movement view | **`before-movement-read`**, **`movement-seen`**, **`targets-seen`** |
| Movement planner | **`before-movement-plan`**, then the executor's `transaction-frozen` |
| Commitment submitter | **`before-commitment`**, **`commitment-submitted`** |
| Main lock | `before-main-lock`, `main-lock-acquired`, `before-main-lock-transfer`, `main-lock-transferred` (K-06 names) |
| Mover | `agreement-derived`, `before-sign`, `after-sign`, `outcome-seen` (K-05 names) |
| Executor (shared) | `before-intent-record`, `before-worker-start` (final reread of movement, targets and lock), `after-signature`, `before-done-accept`, `before-broadcast`, `before-rebroadcast` |
| Incoming sweep | the K-06 sweeper hooks plus **`incoming-seen`** in the deposit view |
| Movement maintainer | **`before-movement-proof`**, **`movement-proof-submitted`**, **`incoming-recorded`**; the K-06 `before-sweep-proof`, `sweep-proof-submitted`, `credit-seen` for incoming sweeps |
| Closure checker | **`before-closure-read`**, **`closure-ledger-seen`**, **`before-closure-notify`**, **`closure-notified`**, **`before-archive`**, **`archived`** |

New ledger kinds for the K-05 signed-intent ledger: `movement-attempt`,
`movement-signed`, `movement-broadcast`, `movement-outcome`. The last three
are recovery kinds and may use the reserve. Incoming sweeps use the K-06
sweep kinds.

## Controlled providers (H-03)

| Provider | Knobs |
| --- | --- |
| Movement view | commitment pending, finalized, orphaned; target state Live, ReadyUnfunded, MovingFunds, Closing, Terminated, quarantined, at any step |
| Closure ledger | each obligation row appears or clears between any two hooks |
| Bitcoin | movement and incoming sweep mined, conflicting, unmined |
| Chain writer | proof commits and the reply is lost; proof reverts "already consumed" |
| Store | the K-02 fence and stage faults, plus restore of a stale image |

## Negative controls (model weakenings, R-07)

Each must break exactly its predicate. Each also gets a failing Go regression
where the guard is real code (revert the guard, the test fails).

| Predicate | Weakening | Breaks |
| --- | --- | --- |
| COMMITTED-TARGET-ONLY | `TargetsFromCurrentEligibility` (KC-VER control) | funds follow a target state change |
| COMMITTED-TARGET-ONLY | `KeyFromCurrentRecord` (Q read from the target's current record) | output to a key the commitment did not bind |
| COMMITTED-TARGET-ONLY | `PlanTimeCommitmentOnly` (commitment checked at plan, not at `before-worker-start`) | grant on an orphaned commitment |
| INCOMING-ONCE | `ForgetIncomingKey` (KC-VER control) | double credit |
| NO-STRANDED-MOVEMENT | `CloseWithIncomingPending` (KC-VER control) | target closed with incoming pending |
| NO-STRANDED-MOVEMENT | `SweepOnlyWhenLive` (the legacy target-state filter) | incoming unreachable from `Closing` |
| CLOSE-ONLY-WHEN-CLEAR | `MainFieldOnly` (KC-VER control) | closure with incoming, escrow or a challenge open |
| CLOSE-ONLY-WHEN-CLEAR | `NoRecheckBeforeNotify` | closure after a late arrival |
| SIGNER-RETENTION | `DeleteKeyAtClosing` (KC-VER control) | defense impossible after closure begins |

## Witnesses (`sometimes`)

- A movement completes while its target becomes ineligible for new funding.
- An incoming sweep after a lost proof reply credits once.
- The legacy stranded case, then its FROST recovery.
- A late deposit after the closure check is still sweepable or refundable.
- A revealed deposit never blocks the source; a late sweep of it reopens a
  `Closing` or `Closed` wallet into `MovingFunds` in a new period.
- A defense after closure begins (signature evidence and key loadable).
- An ECDSA → FROST drain is proven and swept by the FROST target.

## Bounds (from KC-VER V-08)

- Two wallets plus one alternative target.
- Two moved outputs.
- Three ticks.

Crashes are possible at every hook above, plus restore of a stale store image.
No liveness claim is made beyond the witnesses.

## Readings

Reviewed by the implementer at the owner's request on 19 October 2026;
readings 2, 5 and 6 were tightened in that review.

1. **Recovery is a spend by the target's key.** C-06 needs a recovery transition
   for an unavailable target. Only the target's key can spend a committed
   output, so the transition is the target's own signers sweeping the incoming
   output (into its own custody, then onward by an ordinary movement). The
   contract never moves a pending incoming output into another wallet's
   accounting without an SPV-proven spend. If the target cannot sign at all,
   the movement stays `escalated` and counts against the target, which
   therefore cannot close. Governance handling of a lost target is D-03
   territory and OPEN.
2. **Target duty outlives eligibility.** A committed FROST target sweeps
   incoming in `ReadyUnfunded`, `Live`, `MovingFunds` and `Closing`. This is
   the FROST fix for the stranded case. A quarantined target sweeps incoming
   only under a governance directive, the same rule as
   P-K05-CONTAINMENT-NEEDS-DIRECTIVE; until then the incoming output stays
   `escalated` and blocks the target's closure. The legacy route keeps its rules; a
   legacy target is never chosen after cutover.
3. **Eligible target.** A new commitment names FROST wallets that are
   `ReadyUnfunded` or `Live`, not quarantined, `capabilityVersion > 0`, and not
   the source, in ascending label order. The count follows the legacy rule with
   FROST wallets counted. A transfer into a `ReadyUnfunded` wallet is its first
   funding (G-F).
4. **Dust (D-07 test value, OPEN).** A source whose main is below the dust
   threshold records `retained-dust` instead of moving it. The closure ledger
   counts a recorded dust main as cleared. The key stays loadable under
   SIGNER-RETENTION, so the dust stays spendable, and K-12 inventories it.
5. **Late deposits (D-07 test value, OPEN).** Reveals are refused once a wallet
   leaves `ReadyUnfunded` or `Live` (the C-05 rule today). A funded but
   unrevealed deposit is recovered by the operator-free depositor refund.
   **Reveals never block.** A deposit revealed earlier blocks neither the
   commitment nor closure, and deposits do not expire. The host may still
   sweep it (FROST sweeps stay accepted in `MovingFunds`, as legacy sweeps
   are), or the depositor refunds it. A late sweep of a real deposit reopens a
   `Closing` or `Closed` wallet into `MovingFunds` in a new period, and the
   swept value moves on by an ordinary movement. A proven sweep that spends a
   committed main retires that commitment and opens the new period. The other
   late arrivals that can reopen a closure check are an incoming transfer
   proof and a fraud challenge.
6. **Legacy signer in the fixture.** The FROST fixture has no legacy ECDSA
   wallet. The drain tests register one through a fixture ECDSA registry
   contract and sign its movement with real tss-lib, in process, over the
   pre-generated key shares in `pkg/internal/tecdsatest`. The K-07 change on
   the legacy side is target selection and transaction assembly, which run as
   real code; the legacy wallet's DKG and sortition are out of scope.

## Assumptions carried to V-05

- A-K07-MOVEMENT-VIEW: one internally consistent C-06 snapshot at the canonical head, as for A-C04-CURRENT-FINALITY.
- A-K07-CLOSURE-LEDGER: C-09 exposes every closure obligation row at one head.
- Contract obligations (KC-VER table): C-06 binds targets by descriptor hash; consumes each incoming key once across schemes; accepts incoming sweeps per reading 2. Reveals never block; a proven late deposit sweep reopens a `Closing` or `Closed` wallet into `MovingFunds` in a new period, and a proven spend of a committed main retires that commitment. C-09 refuses `Closing → Closed` unless the ledger is empty. C-08 reports open challenges.

## Not covered

- Fee escalation for movements beyond the committed cap.
- Reorganizations deeper than the finality depth.
- The governance handling of a target that cannot sign (reading 1).
- Executed power loss and the production fence.

## As implemented (19 October 2026)

The K-07 code uses every hook name above except `before-late-quarantine`
(movements have no refund race). These choices are not in the predicates and
need the model owner's attention at the V-08 freeze:

- **One authority for both plans.** `movement.Authority` implements the
  executor's `SweepAuthority` for movements and incoming sweeps; the
  executor treats every host-authorized plan alike (K-06 sweeps included).
  A movement's anchor is the commitment's log block; an incoming sweep's is
  its latest incoming log block.
- **Committed keys only.** The authority compares each planned target key
  with the descriptor read under the committed label and requires its
  descriptor hash to equal the committed one. It never reads eligibility,
  quarantine or state of a target. `TestMovementTargetStateRace` changes the
  target's state inside the first signing claim.
- **Lock purposes.** A movement takes the new `movement` purpose on the
  source main; an incoming sweep takes the K-06 `sweep` purpose on the
  target main. Grant records are as in K-06.
- **A committed movement supersedes an aborted sweep** (review fix). A K-06
  sweep that took the main and then aborted (its deposit later refunded)
  leaves the main under `sweep`, while C-06 commits that unspent main. The
  movement takes it over only with the authority's evidence: a finalized,
  unproven commitment naming exactly this main. The lock records "committed
  movement supersedes sweep". The old sweep signature and the movement spend
  the same main, so Bitcoin admits at most one; a proven sweep retires the
  commitment on chain and opens a new period. A sweep never takes a
  movement's main. `TestCommittedMovementSupersedesAbortedSweep` (unit).
- **Incoming records by key** (review fix). The view returns incoming
  records in log order and the plan lists inputs in key order. The authority
  matches them by outpoint key, so a valid sweep is never refused for its log
  order. `TestIncomingAuthorityMatchesRecordsByKey` (unit).
- **Target selection.** A new commitment names the lowest eligible labels
  by the C-06 count; every operator derives the same list from the readiness
  logs. A recorded commitment of the current period is reused, never
  resubmitted.
- **Fees.** Movement and incoming sweep fees are the fee rate times the
  virtual size. A movement is capped at the `maxFee` its commitment recorded,
  in planning and in the authority, so a later parameter change never yields
  a fee the proof or the C-08 defense rejects; an incoming sweep at the
  current moved funds sweep cap.
- **Closure.** `movement.Closure` reads the C-09 obligation bits at the
  finalized head, rereads them at the current head before the notice, and
  archives a local record. Nothing deletes a key or signing record. Bit 16
  (pending reveals) is unused: reveals never block closure.
- **Legacy drain.** `pkg/tbtc` pays P2TR to FROST targets when the chain
  resolves them through `walletScheme` and the immutable descriptor. The
  integration test signs the drain with real tss-lib over the tecdsatest
  shares and sets the legacy wallet's main UTXO and MovingFunds as
  controlled storage markers. After the commitment, the real
  `WalletProposalValidator` accepts the drain proposal, and the legacy
  host's commitment hash equals the one the Bridge stored.
- **Not implemented here.** A background scheduler that drives commitments,
  movers and closure on its own; quarantine directives other than the K-05
  `Directives` source; heartbeat failure and inactivity (K-08).

The local acceptance record is `test/frost/K07.md`.
