# V-09 scope re-pin and model scope (6 November 2026)

Written before any V-09 model code, as KC-VER section 4 and the common
verification brief require. `BOUNDARIES.md` (19 October 2026, before K-08
code, plus its "As implemented" notes) stays as written; this file records
where the integration differs from it and what the V-09 model covers.

## Subjects

| Subject | Revision |
| --- | --- |
| keep-core `tuxxy/integration/snowfall-base` | `925103a6c` |
| tbtc-v2 `tuxxy/integration/frost-base` | `39412825` |
| snowfall client (go.mod replace) | `c1847ebdf133` |
| KC-VER rev 02 draft `keep-core-verification-spec.md` (V-09, VD-08) | `kc-ver-02` `51b9a16`, blob `ab1815f3` (unchanged since `source-pins.json`) |
| KC-SF-V3 `keep-core-integration-spec.md` | `51b9a16`, blob `98f8589a` (unchanged) |

## 1. Pin comparison (`source-pins.json`, recorded at keep-core `aac237a73`, tbtc-v2 `677da343`)

The October pins were taken before the K-08 code, so every K-08 file is
new and several pinned files changed with K-08 itself.

| File | Pinned | Now | Cause |
| --- | --- | --- | --- |
| `pkg/frost/signing/executor.go` | `63369d14` | `02f2877d` | K-08 `13b8992c1` (defense records, `before-defense-record`), `08aaf7489`; then `6ba35f3f6` (#50, retained intent is a possible release), `b43cfd9fa` (unread reservation is not late) |
| `pkg/frost/signing/done.go` | `c5442d47` | `e6ba3064` | K-08 heartbeat purpose |
| `pkg/frost/transport/transport.go` | `4cbf4a30` | `6a5a0dfe` | K-08 `Present()` (authenticated presence) |
| `pkg/chain/ethereum/frostbridgeabi/bridge.go` | `16462e79` | `13d18f2d` | regenerated bindings (K-08, C-10, C-11) |
| `plan.go`, `snowfallengine/engine.go`, `frost/engine.go`, `store/signing.go`, `tbtc/heartbeat.go`, `tbtc/inactivity.go` | | | unchanged |
| tbtc-v2 `FrostFraud.sol` | `ba54eaae` | `abb14e78` | K-08 self-transfer kind `3c44d2cf`; #26 movement defense `6b2c40ec`; output cap 252 |
| tbtc-v2 `FrostLifecycle.sol` | `ed1587e4` | `7db6ca7b` | C-09 obligation ledger `f309a782`; K-08 inactivity route `8f34ab0a`; ADV-C2 (batch 8) |
| tbtc-v2 `FrostMovementState.sol`, `FrostFundingState.sol` | | | changed (C-06/C-10/batch 8); read only for the self-transfer cap and incoming records |
| tbtc-v2 `FrostFraudState.sol`, `bridge/Fraud.sol`, `bridge/Heartbeat.sol` | | | unchanged |

K-08 code itself (`f58a45e67`, 19 October) and the integration differ in
`pkg/frost/enforcement/inactivity.go`, `pkg/frost/signing/heartbeat.go`,
`pkg/frost/signing/executor.go` (above) only; the defender, the chain seam
and `defense.go` are unchanged since K-08.

## 2. Differences from BOUNDARIES.md, recorded before model code

D1. **Fraud defense is not a node duty (F4).** DECISIONS.md 2026-11-04 F4:
"not a node duty ... the defense is a permissionless contract call left to
others, as D17. Dropped from K-09 node scope." At `925103a6`,
`enforcement.Defender.Defend` and `NotifyTimeouts` have no caller outside
tests (`pkg/frost/integration/k08_test.go`); `pkg/tbtc` constructs only the
heartbeat `Runner` and the `Claimer`. The "Defender" and "Timeout notifier"
rows of the BOUNDARIES component table exist as library code only, and the
"background scheduler for heartbeats, defenses and timeouts" is replaced by
the base coordination window for heartbeats alone.
Consequence: the subject of **P-K08-DEFENSE-BEFORE-DEADLINE** ("a defense
submission is reachable" by this host) does not exist in the integrated
node. The predicate is **blocked at host scope (F4)**. The model still
checks the `Defender` component under the explicit assumption
A-V09-DEFENDER-INVOKED (some process runs `Defend` passes with this host's
records), which F4 leaves undischarged. That component result is reported
as conditional evidence only; it does not establish the predicate. Open
question Q1 below.

D2. **Evidence retention still applies.** The executor and the heartbeat
signer write the defense record before every grant (`before-defense-record`
precedes the claim and `before-worker-start`), and no delete path exists
(`store/signing.go` unchanged: records are immutable, never deleted).
**P-K08-EVIDENCE-RETAINED** keeps its subject. Under F4 the records are
retained but only read by the (uninvoked) defender; whether "others" can
defend without them (a losing replacement or an unmined sweep is known only
to the signers) is open question Q2.

D3. **Done record.** KC-VER lists "the intent, the admission, the Done
record and the authorization" as defense records. A Done record exists only
after the worker returned the signature; a grant whose signature never came
back (worker or host crash after release) has intent and defense record but
no Done. The implementation's defense needs only the defense record
(`Defender.submit` reads nothing else; the admission is on chain). The model
checks intent + defense record for every grant, and Done for every
signature returned to the host. Recorded as an interpretation, not a
weakening: BOUNDARIES (owner-reviewed restatement) already says the defense
record is the record durable before the grant.

D4. **Heartbeat activity rule (batch 7, `6b90b6890`, DECISIONS F3).**
`Claimer.MinimumActivePercent` = 70 in the node: a heartbeat with fewer than
70% of the wallet's seats authenticated present is a failure, signed or
not; one with enough presence ends the streak if signed and is skipped
(neither failure nor end) if not. BOUNDARIES' rule was "every unsigned
heartbeat fails, a signed one ends the window". The claim window semantics
changed; the NO-SUSPECT model uses the integrated rule.

D5. **Failure count.** The node uses `heartbeatConsecutiveFailureThreshold`
= 3 (base) and `Lookback` 240 windows; BOUNDARIES used F = 2 (test value).
The model runs F = 2 over three rounds (the BOUNDARIES bound); F = 3 over
four rounds is a larger bound (section 5).

D6. **Presence held for the whole heartbeat (`HoldPresence`).** The node
keeps the attempt transport open until the heartbeat period ends, so every
seat whose authenticated key arrived in the period is present. A host with
no selected seat joins for presence only and returns `ErrNotSelected`
without a grant; the runner records its outcome as unsigned with its
presence. Under D4 an unsigned heartbeat with full presence is skipped, so
a non-selected host never ends a streak by itself; a claim still needs
`threshold` signers whose own records derive it.

D7. **Rotated heartbeat signers (`cd1d64105`, review B2).** The selected
seats are a threshold of whole operators rotated per window
(`redemption.SelectSeats`). Not modeled; it changes who signs, not what a
grant may carry.

D8. **Claim exchange implemented in the node.** BOUNDARIES reading 5 left the
exchange out of K-08; batch 7 adds a per-wallet attestation channel and
ordered submission (`claimInactivity`). The model represents acceptance as
C-09 does: `threshold` seats outside the claimed set, each signer's own
records deriving the same claim. The channel itself is not modeled.

D9. **Heartbeat nonce = coordination block.** The node runs the heartbeat
for the leader's proposal and refuses unless `Runner.Current().Anchor ==
coordinationBlock` (RoundBlocks = `coordinationFrequencyBlocks` 900,
FinalityBlocks = `frostFinalityBlocks` 2). The authority rechecks the
current anchor before the intent record and after the durable claim, as
BOUNDARIES says.

D10. **Executor stages not in the V-04 model.** The defense-record commit
(`before-defense-record`, `retainDefenses`) sits between the intent record
and the claim. The V-04 model does not have it (noted by the V-04 agent);
V-09 models it. The V-04 statement's correspondence clause is not changed
here.

D11. **PossibleRelease comment.** `executor.go` `PossibleRelease` says Run
"keeps no durable record between [the intent] and the worker"; the defense
records are written there. Behaviour is unaffected (the intent alone already
makes a possible release). Note only.

D12. **C-08 contract facts used as assumptions (unchanged in substance).**
One challenge per `(walletId, digest)`; `defeat`/`defeatHeartbeat`/
`notifyTimeout` each require an unresolved challenge and resolve it once; a
defense is accepted until the timeout is notified (no deadline check in
`resolve`); `notifyTimeout` requires `block.timestamp >= reportedAt +
fraudChallengeDefeatTimeout`; any heartbeat nonce is accepted; open
challenges block closure (`FrostLifecycle.obligations` bit 128).
C-09 `notifyInactivity` checks the nonce, a non-empty strictly ascending
claimed set, and at least `threshold` ascending signing seats outside it,
each signed by its descriptor operator.

D13. **`Error.Suspect`** is still consumed nowhere in `pkg/frost` or
`pkg/tbtc` non-test code (`snowfallengine/engine.go:109` copies it).

### Hooks present at `925103a6`

All BOUNDARIES hook names exist as test-only `boundary func(string) error`
fields, nil in production: executor `transaction-frozen`,
`before-intent-record`, `before-defense-record`, `before-worker-start`,
`after-signature`, `before-done-accept` (plus the V-04 hooks); heartbeat
signer `heartbeat-nonce-seen`, `before-heartbeat-record`,
`before-defense-record`, `before-worker-start`, `after-signature`; runner
`before-heartbeat`, `heartbeat-outcome-recorded` (and the `sign` seam);
claimer `before-claim-read`, `claim-derived`, `claim-signed`,
`before-claim-submit`, `claim-submitted`; defender `before-challenge-read`,
`challenge-seen`, `defense-record-read`, `before-defense`,
`defense-submitted`, `before-timeout-notify`, `timeout-notified`. The node
duty in `pkg/tbtc/frost_node_duties.go` has no hooks; it is not replayed.
No hook is added by this work.

## 3. Predicate status before the model

| Predicate | Subject at `925103a6` | Status going in |
| --- | --- | --- |
| P-K08-HEARTBEAT-DOMAIN | `signing.HeartbeatSigner`, guarded engine claim, runner authority | applicable |
| P-K08-EVIDENCE-RETAINED | executor and heartbeat signer records, immutable store | applicable (Q2) |
| P-K08-DEFENSE-BEFORE-DEADLINE | `enforcement.Defender`, not invoked by the node | **blocked at host scope (F4)**; component model under A-V09-DEFENDER-INVOKED (Q1) |
| P-K08-NO-SUSPECT-SANCTION | `enforcement.Runner` + `Claimer` as the node runs them | applicable |

## 4. Model (KC-VER 2.3)

Crate `pkg/frost/enforcement/verification/model` (Stateright 0.31.0, Rust
1.94.0), separate from the Go code (R-01). Three profiles of one V-09 model
shape, each with its own bounded state:

- **hb**: one heartbeat signer and one transaction executor over a shared
  durable claim set, chain ticks moving the current nonce, process crash
  and retry, and a misrouted grant request (any host path handing the
  engine a request whose message is not the one the attempt was built for).
- **enf**: one wallet; released signature A (this host's executor or
  heartbeat signer; kind heartbeat or losing replacement sweep) and
  unauthorized signature U (released without this host's intent); two
  challenges; chain ticks with the deadline at tick 3; host crash, restart,
  blocked executor recovery, stale store restore, lost defense reply; the
  defender and the timeout notifier.
- **claim**: three seats (one per operator), threshold 2, hosts of seats 1
  and 2 keep records, seat 3 is the subject; per heartbeat round the
  environment chooses who is up, whether the round signs, a `Suspect`
  hint, and per-host crash after the start record; claim derivation,
  signing and C-09 acceptance.

**Store reuse (by copy of the kernel's outcome vocabulary).** Each record
or claim write is two model stages (issued, acknowledged). A process crash
between them leaves the write durable or absent (the kernel's uncertain
outcome); a host crash drops it. A stale restore makes the next open
quarantined (fail closed). These are the S-K02-HOST-4 guarantees
P-K02-DURABLE, P-K02-IMMUTABLE and the stale-restore quarantine witness,
fresh at `925103a6` (manager check, 6 November); the V-09 model assumes them
(A-V09-STORE) and does not re-model the commit procedure.

**Predicates as model properties.**

- HEARTBEAT-DOMAIN (always): (i) a grant under the heartbeat purpose carries
  `HB(n)` with `n` the current nonce at the grant's final read; (ii) no grant
  carries a message of the other purpose (no heartbeat attempt with a
  transaction digest, no transaction attempt with a heartbeat digest); (iii)
  no attempt is ever granted two different messages, across crash and retry.
- EVIDENCE-RETAINED (always): whenever the host reads an open (not
  quarantined) store, every granted digest has its intent and defense record,
  and every signature returned to the host has its Done record (D3).
- DEFENSE-BEFORE-DEADLINE (always, component scope): in every reachable state
  before the deadline tick, with the store not quarantined, every admitted
  unresolved challenge on a digest this host holds a defense record for can
  be resolved by a defense submission through host-only actions (restart,
  defender steps) without a tick. This is the SC-08 conditional
  reachability, evaluated as a bounded local search inside an `always`
  property; inclusion within the margin is assumption D-09.
- NO-SUSPECT-SANCTION (always): an accepted claim names only seats absent from
  the authenticated presence of every round each signer counted, each
  signer counted at least F failed rounds, and no counted window spans a
  healthy heartbeat (signed with presence at or above 70%) that the signer
  did not see.

**Weakenings (R-07).** KC-VER controls first, then the BOUNDARIES additions.

| Profile | Weakening | Removes | Must break |
| --- | --- | --- | --- |
| hb | `AcceptRawDigest` (KC-VER) | final binding of the grant to the heartbeat intent (`heartbeatAuthorization.BeforeSigning`) | HEARTBEAT-DOMAIN |
| hb | `ReuseAttempt` (KC-VER "reuse the attempt for another purpose") | a fresh, exclusively claimed attempt per action: a retry after a crash continues the crashed attempt | HEARTBEAT-DOMAIN |
| hb | `AnyNonce` | current-nonce recheck (`Runner.AuthorizeHeartbeat`) | HEARTBEAT-DOMAIN |
| hb | `SharedAttemptDomain` (BOUNDARIES) | the heartbeat attempt tag | expected to produce **no** counterexample: attempt ids also bind the intent and claims are exclusive. Reported as a masked control, not as a pass condition |
| enf | `DeleteAtSettlement` (KC-VER) | VD-08 no-delete | EVIDENCE-RETAINED |
| enf | `RecordAfterGrant` | defense record before the grant | EVIDENCE-RETAINED |
| enf | `DefendAfterRecovery` (KC-VER) | defender independence from executor recovery | DEFENSE-BEFORE-DEADLINE |
| enf | `ProvenOnly` | self-transfer route for unproven sweeps | DEFENSE-BEFORE-DEADLINE |
| enf | `AbortOnFirstFailure` | continue past an undefendable challenge | DEFENSE-BEFORE-DEADLINE |
| claim | `ClaimFromSuspect` (KC-VER) | presence-only derivation | NO-SUSPECT-SANCTION |
| claim | `SingleFailureClaim` | F consecutive failures | NO-SUSPECT-SANCTION |
| claim | `SkipUnknownRound` | a started round without outcome ends the window | NO-SUSPECT-SANCTION |

`RederiveRetainedClaim` (BOUNDARIES) breaks a witness, not a safety
property; it is not run (gap G3).

**Witnesses (sometimes).** hb: a heartbeat granted at the current nonce;
a heartbeat granted after a crash and retry in a later round. enf: a
heartbeat signed, challenged and defended; a losing replacement defended
after its refund; a defense after a host restart before the deadline; two
challenges at once, the first not ours, the second defended; an
unauthorized signature admitted and timed out (wallet terminated). claim: a
real inactivity claim naming exactly the absent seat; a started round with
a lost outcome ending the window.

**Bounds (KC-VER V-09).** One wallet; two released signatures (one
authorized, one unauthorized); two challenges; deadline at tick 3. Plus: hb
two heartbeat starts, one transaction, two crashes; enf two crashes, one
stale restore, one lost reply; claim three rounds, F = 2. Limits: 4 model
threads, 900 s, 20 GiB per command (common brief).

## 5. Replay (KC-VER 2.4)

Traces from the claim models and the weakened counterexamples (cut at the
first step the claim model does not offer, H-06) replay on the real code at
the existing hooks, with controlled providers labelled as fixtures:

- hb traces on `signing.HeartbeatSigner` (package `signing`, test-only file),
  with the existing heartbeat engine stub extended by the store's exclusive
  claim semantics (fixture) and the authority's current nonce as the tick.
- enf traces on `enforcement.Defender` (package `enforcement`, test-only
  file) over the existing fake chain and record store; the executor stages
  of the trace are replayed on `signing.HeartbeatSigner` for a heartbeat A
  (record-before-grant), and are a replay limitation for the sweep kind.
- claim traces on `enforcement.Runner` (its `sign` seam) and `Claimer`
  (Derive, Sign, Submit) for two hosts with separate record stores.

A host crash is replayed as an abort at the hook and a fresh component on
the same record store; a stale restore and the store commit stages are
replay limitations (covered by S-K02-HOST-4 replay).

## 6. Open questions (not decided here)

- Q1 (F4): With fraud defense not a node duty, is P-K08-DEFENSE-BEFORE-DEADLINE
  dropped from V-09, reassigned to a separate defender process (D17-like
  keeper) with its own predicate, or kept as a node duty after all? Until
  answered it is blocked at host scope.
- Q2 (F4 with VD-08): a losing replacement or an unmined sweep is known only
  to the signers, so "others" cannot defend it without the host's defense
  records. Does F4 intend such signatures to time out (wallet terminated),
  or should the records be exported to the defending party?
- Q3: KC-VER's "reuse the attempt for another purpose" control and
  BOUNDARIES' `SharedAttemptDomain`: the latter is masked by attempt ids
  that bind the intent and by exclusive claims. Keep it as a declared
  control?
- Q4: KC-VER lists the Done record among the defense records (D3). Confirm
  the reading "Done when the signature returned".

## 7. Gaps

- G1 Node duty (`frost_node_duties.go` heartbeat and claim exchange) has no
  hooks; not replayed. Its outcome rule is the claimer's (D4).
- G2 Rotated signer selection (D7) and the 30-block presence period are not
  modeled.
- G3 `RederiveRetainedClaim` not run.
- G4 F = 3 (node value) is a larger bound, not run unless measured cheap.
- G5 Sweep-kind executor stages and stale restore are replay limitations.
