# K-08 / V-09 predicate and replay boundary contract

Recorded on 19 October 2026, before any K-08 code. This is the V-09 scope
document that KC-VER section 4 requires before K-08 code. It restates the four
V-09 predicates as host invariants and fixes the boundary hook names, the
controlled providers, the negative controls, the witnesses and the bounds. It
is an implementation contract for review. **It is not a model result.** The
model is checked before K-08 merges. The replay runs at the K-08 exit.

K-08 is one milestone. The contract side (a self-transfer defense in C-08 and
an inactivity route in C-09) lands in tbtc-v2 above C-09; the host side lands
here above K-07. D-03, D-05 and D-07 are used as test values marked OPEN.

## Governing sources

| Source | Revision | Blob |
| --- | --- | --- |
| KC-SF-V3 `keep-core-integration-spec.md` (K-08, C-07, C-08, C-09, SC-03, SC-07, SC-08, D-03, D-05, D-09) | spec PR #4 `51b9a16` | `98f8589a` |
| KC-VER rev 02 `keep-core-verification-spec.md` (V-09, VD-08, VD-10) | spec PR #4 `51b9a16`, unmerged | `ab1815f3` |
| keep-core base (top of stack #16, K-07) | `aac237a73` | see `source-pins.json` |
| tbtc-v2 base (top of stack #8, C-09) | `677da343` | see `source-pins.json` |

## What the code does today

I read the legacy heartbeat and fraud paths and the C-08 and C-09 code at the
bases above before fixing the hooks.

| Rule | Where | Consequence for K-08 |
| --- | --- | --- |
| A legacy heartbeat is 16 bytes, `0xff` ×8 then 8 bytes derived from the wallet and a block; the host checks the format on chain before signing `hash256(message)` | `pkg/tbtc/heartbeat.go`, `Heartbeat.isValidHeartbeatMessage` | the legacy route is unchanged; FROST wallets are not in legacy coordination |
| Legacy heartbeat failures are counted in memory; at 3 the members sign an inactivity claim from the signing activity report and submit it to the ECDSA registry, which calls back the Bridge and moves a `Live` wallet's funds | `heartbeat.go`, `inactivity.go`, `WalletRegistry.notifyOperatorInactivity`, `Wallets.notifyWalletHeartbeatFailed` | no FROST equivalent exists: the FROST registry and C-09 have no inactivity or heartbeat-failure route |
| No keep-core client watches or defends fraud challenges, legacy or FROST | generated bindings only | K-08 adds the first defender |
| C-08 admits any valid BIP340 signature under Q over any digest, keyed `(domain, scheme, walletId, digest)`, bond = `fraudChallengeDepositAmount` | `FrostFraud.submit` | a public signature can be challenged by anyone |
| A transaction defense is accepted for a proven transaction, a transaction named by an admitted redemption intent (payment, cancellation alternative, sweep resolution), or an exact C-06 movement | `FrostFraud.authorize` | **a signed deposit sweep or incoming sweep that never mines has no defense** (C08-BOUNDARIES, D-05) |
| A heartbeat defense is `sha256(T ‖ T ‖ walletId ‖ uint64 nonce)` with `T = sha256("tbtc-v2/frost-heartbeat/v1")`; any nonce is accepted | `FrostFraud.defeatHeartbeat` | the nonce rule is the host's (C08-BOUNDARIES assigns it to K-08) |
| An unresolved challenge times out after `fraudChallengeDefeatTimeout`; the wallet becomes `Terminated`, the bond is refunded and `FrostSanctionNotified("fraud-defeat-timeout")` is emitted | `FrostFraud.notifyTimeout` | notification only (D-03); a defense is accepted until the timeout is notified |
| The executor signs only Bitcoin transaction digests from a plan snapshot | `signing/executor.go` | a heartbeat needs its own purpose, attempt domain and authority |
| Released signatures keep their intent and `Done` record under immutable claims, never deleted; there is no index from `(walletId, digest)` to a record | `store/signing.go`, `executor.go` | the defender cannot find the record for a challenged digest |
| `Error.Suspect` is copied from the worker and consumed nowhere | `pkg/frost/engine.go`, `snowfallengine/engine.go` | it must stay unconsumed by enforcement |

## Routes

| Act | Legacy ECDSA wallet | FROST wallet |
| --- | --- | --- |
| Heartbeat signing | legacy, unchanged | new purpose `heartbeat`, guarded engine grant |
| Fraud admission | legacy, unchanged | C-08, unchanged |
| Transaction defense | none in keep-core (unchanged) | new defender: proven, intent, movement, or the new self-transfer kind |
| Heartbeat defense | none in keep-core (unchanged) | new defender, `defeatFrostFraudChallengeWithHeartbeat` |
| Timeout | anyone (unchanged) | anyone; the host notifies only digests it holds no record for |
| Inactivity claim | legacy registry, unchanged | new C-09 route over the immutable descriptor |

## Predicates as host invariants

Vocabulary. "Release" means a worker receives a signing grant (the K-04
meaning). "Admission" means C-08 accepting a fraud challenge. "Current
heartbeat nonce" means the anchor block of the current agreed round, derived
from the canonical chain exactly as K-06 and K-07 derive sweep and movement
rounds (reading 1). "Defense record" means the durable record, keyed by
`(walletId, digest)`, that holds everything a defense submission needs.

**P-K08-HEARTBEAT-DOMAIN.** A heartbeat grant is issued only if, in the read
after the durable claim (final read at `before-worker-start`):

- the 32-byte message equals `sha256(T ‖ T ‖ walletId ‖ uint64 nonce)` recomputed by
  the host from the heartbeat intent, never taken from a caller;
- the nonce is the current heartbeat nonce;
- the attempt is in the heartbeat attempt domain, which no transaction purpose
  uses, so a claimed heartbeat attempt can never carry a transaction digest
  and a transaction attempt can never carry a heartbeat.

No API signs a caller-supplied digest as a heartbeat. A retry after a crash
reuses the same claim and the same message; a changed message is a different
attempt and passes the same checks or is refused.

**P-K08-EVIDENCE-RETAINED.** For every digest this host releases, the defense
record is durable **before** the grant (`before-defense-record` precedes
`before-worker-start`) and survives crash, restart and restore. It covers
payments, cancellation alternatives, losing replacements, deposit sweeps,
incoming sweeps, movements and heartbeats. Together with the intent and the
`Done` record it is enough to build the defense without any other local
state. Following VD-08, nothing is deleted before the K-12 retirement
predicate holds; K-08 adds no delete path.

**P-K08-DEFENSE-BEFORE-DEADLINE.** For every admitted challenge on a digest
this host holds a defense record for, a defense submission is reachable
before `reportedAt + fraudChallengeDefeatTimeout` in every interleaving of
chain ticks, host crash and restart within the bounds. The defender reads only
the chain and the defense records: it does not wait for executor recovery,
round agreement, the main lock or any ledger replay. A lost submission reply
is settled by rereading the challenge, never by assuming failure. Assumption:
inclusion within the deadline margin (D-09).

**P-K08-NO-SUSPECT-SANCTION.** An inactivity claim names only seats that were
absent from the authenticated presence record of every heartbeat in the
claim's failure window, and is submitted only with the signatures of at least
`threshold` seats outside the claimed set (reading 3). `Error.Suspect`, a
signing failure alone, or any other unauthenticated observation never adds a
seat to a claim. A fraud timeout is notified only by contract rule (an
admitted, unresolved challenge past its deadline).

## Proposed components

These names are proposals for the K-08 code. The hooks are binding; the type
names may change in review.

| Component | Package | Role |
| --- | --- | --- |
| Defense record | `pkg/frost/signing` | written by the executor for every input digest before the grant |
| Heartbeat signer | `pkg/frost/signing` | purpose `heartbeat`; intent `(walletId, nonce)`; heartbeat attempt domain; guarded engine grant; authority rechecks the nonce |
| Presence | `pkg/frost/transport` | the seats whose authenticated ephemeral keys arrived in an attempt |
| Heartbeat runner | `pkg/frost/enforcement` | runs one heartbeat per round and records its outcome and presence |
| Inactivity claimer | `pkg/frost/enforcement` | derives the claim from outcome records, signs it with the operator key, submits it idempotently |
| Defender | `pkg/frost/enforcement` | reads challenges for the host's wallets, builds the defense from the record, submits it idempotently |
| Timeout notifier | `pkg/frost/enforcement` | notifies an expired challenge on a digest the host holds no record for |
| Chain seam | `pkg/chain/ethereum` | C-08 and C-09 enforcement reads and writes |
| Self-transfer defense | tbtc-v2 C-08 | new authorization kind (reading 2) |
| Inactivity route | tbtc-v2 C-09 | new entrypoint (reading 3) |

## Boundary hooks

Each component gets the K-04 test-only `boundary func(string) error`, nil in
production. Names in **bold** are new.

| Component | Hooks, in order |
| --- | --- |
| Executor (shared) | `transaction-frozen`, `before-intent-record`, **`before-defense-record`**, `before-worker-start`, `after-signature`, `before-done-accept` |
| Heartbeat signer | **`heartbeat-nonce-seen`**, **`before-heartbeat-record`**, then `before-defense-record`, `before-worker-start`, `after-signature` |
| Heartbeat runner | **`before-heartbeat`**, **`heartbeat-outcome-recorded`** |
| Inactivity claimer | **`before-claim-read`**, **`claim-derived`**, **`claim-signed`**, **`before-claim-submit`**, **`claim-submitted`** |
| Defender | **`before-challenge-read`**, **`challenge-seen`**, **`defense-record-read`**, **`before-defense`**, **`defense-submitted`** |
| Timeout notifier | **`before-timeout-notify`**, **`timeout-notified`** |

## Controlled providers (H-03)

| Provider | Knobs |
| --- | --- |
| Challenge view | a challenge admitted, defended, timed out, at any step; the deadline tick |
| Chain writer | a defense or claim lands and the reply is lost; reverts "challenge resolved" or "stale nonce" |
| Transport | a seat's ephemeral key arrives or not per attempt |
| Worker | returns an error with an arbitrary `Suspect` set |
| Bitcoin | a sweep mines, loses to a refund, or stays unmined |
| Store | the K-02 fence and stage faults, plus restore of a stale image |

## Negative controls (model weakenings, R-07)

Each must break exactly its predicate. Each also gets a failing Go regression
where the guard is real code (revert the guard, the test fails).

| Predicate | Weakening | Breaks |
| --- | --- | --- |
| HEARTBEAT-DOMAIN | `AcceptRawDigest` (KC-VER control: the heartbeat signer takes the message from the caller) | a transaction digest is released as a heartbeat |
| HEARTBEAT-DOMAIN | `SharedAttemptDomain` (KC-VER control: heartbeat attempts use the transaction domain) | one claim reused across purposes |
| HEARTBEAT-DOMAIN | `AnyNonce` (no current-nonce recheck) | a stale heartbeat is released |
| EVIDENCE-RETAINED | `DeleteAtSettlement` (KC-VER control) | a losing replacement cannot be defended after its refund |
| EVIDENCE-RETAINED | `RecordAfterGrant` (defense record written after signing) | crash between grant and record loses the defense |
| DEFENSE-BEFORE-DEADLINE | `DefendAfterRecovery` (KC-VER control) | no defense while recovery is blocked |
| DEFENSE-BEFORE-DEADLINE | `ProvenOnly` (only mined transactions defended) | an unmined sweep times out |
| DEFENSE-BEFORE-DEADLINE | `AbortOnFirstFailure` (one failed defense ends the pass) | a later defensible challenge misses its deadline |
| NO-SUSPECT-SANCTION | `ClaimFromSuspect` (KC-VER control) | an active seat is claimed inactive |
| NO-SUSPECT-SANCTION | `SingleFailureClaim` (absent in the last heartbeat only) | a seat absent once is claimed |
| NO-SUSPECT-SANCTION | `SkipUnknownRound` (a started round with a lost outcome is skipped) | failures on both sides of a signed heartbeat count as consecutive |
| NO-SUSPECT-SANCTION | `RederiveRetainedClaim` (a retry rederives the claim window) | the operator can never sign its claim again |

## Witnesses (`sometimes`)

- A heartbeat is signed, admitted as a challenge, and defended.
- A losing replacement is defended after its refund: a deposit sweep that lost
  to the depositor's refund, never mined, is challenged and defended.
- A defense lands before the timeout, after a host restart.
- Two challenges at once, the first undefendable: the second is still defended.
- A heartbeat is signed and its outcome write is interrupted: the round ends
  the failure window.
- A claim is signed, the signatures are lost, more heartbeats fail, and the
  retry signs the same claim and window.
- A real unauthorized signature is admitted and times out; the wallet is
  terminated and keeps its duties.
- A real inactivity claim with evidence moves a `Live` wallet to `MovingFunds`
  and names exactly the absent seat and its operator.
- Legacy ECDSA heartbeat and fraud behaviour is unchanged.

## Bounds (from KC-VER V-09)

- One wallet.
- Two released signatures (one authorized, one unauthorized).
- Two challenges.
- Deadline at tick 3.

Crashes are possible at every hook above, plus restore of a stale store image.
No liveness claim is made beyond the witnesses.

## Readings

Decided by the implementer on 19 October 2026 for owner review.

1. **Heartbeat nonce.** The nonce is the anchor block of the current round
   (`RoundBlocks`, `FinalityBlocks` as for K-06 and K-07), so every operator
   derives it from the canonical chain alone and a crash cannot move it. The
   host signs only the current nonce. C-08 accepts any nonce in the domain: a
   heartbeat signature authorizes nothing but itself, so an old one is
   harmless and stays defensible. The legacy 1/16 heartbeat probability and
   the background scheduler are not part of K-08.
2. **Self-transfer defense (D-05 test value, OPEN).** C-08 gains a fourth
   authorization kind for an unmined (or unproven) sweep. Every input must be
   the wallet's own (a key-path output of Q, or a revealed deposit of this
   wallet on its wallet leaf); the only output must pay Q; and the fee must be
   at most the sum of the deposit inputs' `maximumSweepFee` (fixed at reveal)
   plus the largest `sweepFeeCap` among key-path inputs that are recorded
   incoming outputs of this wallet (`movedFundsSweepTxMaxTotalFee` when C-06
   recorded the output). Any other key-path input, such as the main, adds
   nothing, as in the C-05 rule where a deposit sweep's fee is covered by its
   deposits. SIGHASH_DEFAULT commits to every prevout, so the supplied
   prevouts are the signed ones. Such a transaction moves value only from the
   wallet to itself under the sweep fee policy; it is not fraud. Every part of
   the cap is fixed when its input was recorded, so no parameter change
   revokes a defense; the host plans an incoming sweep within the smaller of
   the live cap (which the C-06 proof checks) and its outputs' recorded caps.
3. **Inactivity evidence.** A seat is present in a heartbeat attempt when its
   authenticated ephemeral key arrived on the attempt's transport (the
   membership-checked network key). Each heartbeat records its outcome and
   presence durably. A claim names the seats absent from every heartbeat in
   the last `F` consecutive failed heartbeats (`F` = 2 in tests, OPEN; legacy
   uses 3). Each operator signs the claim with its operator key only if its
   own records derive the same set, and only for its seats outside the set.
   C-09 verifies at least `threshold` distinct seat signatures from the
   descriptor's operators, the per-wallet inactivity nonce, a strictly
   ascending claimed set within the roster, and that no signer is claimed.
4. **Sanctions (D-03 test value, OPEN).** An inactivity claim emits the
   claimed seats and their operators with the descriptor hash, plus
   `FrostSanctionNotified("inactivity")`. A heartbeat-failure claim moves a
   `Live` wallet as legacy `notifyWalletHeartbeatFailed` does; other states
   only record it. No seize or reward route exists for FROST.
5. **Collection.** Claim signatures are exchanged out of band as K-05
   attestations are; the tests merge them. A peer-to-peer exchange and the
   background scheduler are not part of K-08.
6. **Timeout notification.** An honest host notifies a timeout only after the
   deadline and only for a digest it holds no defense record for, so it never
   races its own defense.

## Assumptions carried to V-05

- A-K08-CHALLENGE-VIEW: one internally consistent C-08 challenge view at the canonical head.
- A-K08-INCLUSION: a defense submitted before the deadline margin is included before the deadline (D-09).
- Contract obligations: C-08 keeps one canonical challenge per digest and resolves it once (KC-VER table); accepts the self-transfer kind per reading 2; accepts any heartbeat nonce. C-09 verifies inactivity claims per reading 3 and refuses a reused nonce.

## Not covered

- Worst-case gas for 100 seats and 128-input defenses is measured in the contract tests, not modeled.
- A `Terminated` wallet's unproven movement has no timeout route (C-06/C-09 rule, unchanged here).
- Production exchange of claim signatures and the heartbeat scheduler (reading 5).
- An economic sanction route (D-03).
- Executed power loss and the production fence.
