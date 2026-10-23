# K-09a local lifecycle boundary contract

Recorded 23 October 2026, before implementation. Owner instruction: proceed
with K-09 after the C-10 handover. K-09b, the single final mutation campaign,
remains separate and requires an explicit owner request.

## Governing contract and prerequisites

KC-SF-V3 revision 01 section 6 K-09 and section 8.3 govern this package.
KC-VER revision 02 supplies the component predicates, VD-10 and V-05 joins.
K-03 through K-08 and C-01 through C-10 have local implementations and review
records at the bases in source-pins.json. C-07 production qualification and
SF-03 production-size qualification remain open: this package claims only a
small local lifecycle. It does not complete those prerequisites for release.

No new model predicate is invented for K-09a. Existing component predicates
remain their owners' contracts. Development proceeds before their models;
model checking and replays retain the KC-VER merge/exit schedule. V-05 closes
before K-09b; K-09a records joins without declaring the campaign complete.

## Scope and entry points

- Native scripts/frost-lifecycle-local.sh with exact local tool pins,
  production Bridge/library artifacts, EIP-170 enabled, real worker,
  authenticated libp2p, real per-operator stores, Bitcoin Core and Anvil.
- A joined two-wallet lifecycle using the same chain, stores and wallet keys
  from DKG/readiness through custody, redemption, enforcement and movement.
- Public legacy obligations retained through a real proxy upgrade. Seeded
  legacy markers are permitted only as explicitly labeled component controls;
  they cannot discharge the public-upgrade obligation.
- A separate SPV run at actual validation settings against realistic headers;
  zero-difficulty regtest relay evidence is separately labeled.
- Scoped controls for new guards, a machine-readable stage result and source
  manifest, and a local acceptance record. No CI, Docker or campaign runner.

## Invariants and joining evidence

| ID | Producer -> consumer contract | Required witness |
| --- | --- | --- |
| J09-READY | durable DKG/readiness -> spending host | two distinct descriptors, real worker keys reloaded on each operator |
| J09-CREDIT | deposit construction -> sweep -> Bank | Core accepts the exact transaction; proof credits once and the next operation consumes its main |
| J09-REFUND | depositor template -> operator-free refund | real refund after locktime with operator processes stopped; no sweep credit |
| J09-REDEMPTION | sweep custody -> reservation -> exact settlement/timeout | escrow and main-purpose lock preserved; payment/refund exclusivity and cancellation are checked |
| J09-RECOVERY | durable signing record -> restarted host | reopen the store, retain keys and defense data, then complete a public defense |
| J09-ENFORCEMENT | released signature -> public challenge -> resolution | closure refuses an open challenge; defense settles it before closure; unauthorized challenge times out in a separate reachable case |
| J09-INACTIVITY | authenticated absence -> threshold claim -> movement | the claimed seats and operators match presence; an active seat is never claimed |
| J09-MOVEMENT | legacy/FROST source -> committed target -> incoming sweep | distinct wallets, exact targets, conservation apart from fees, idempotent proofs |
| J09-CUTOVER | one-way creation mode -> continued service | no new legacy DKG after cutoff; existing duties remain callable under containment |
| J09-CLOSURE | complete obligation inventory -> closed wallet | no main (apart from recorded dust), escrow, incoming/outgoing transfer or challenge lost; keys stay loadable |
| J09-UPGRADE | publicly populated legacy state -> upgraded implementation | each pending obligation and its financial/policy frame survives; discharge through public entry points |
| J09-SPV | realistic headers/inclusion -> production validation | valid witness at actual difficulty factor; reject invalid work, links, inclusion and inadequate depth |

## Boundary hooks

K-09 adds test orchestration only. Reuse the component hooks verbatim:
transaction-frozen, before-intent-record, before-defense-record,
before-worker-start, after-signature, before-done-accept, challenge-seen,
defense-record-read, before-defense, defense-submitted, before-timeout-notify,
timeout-notified, heartbeat-outcome-recorded, claim-derived, before-claim-submit,
claim-submitted. Existing storage/fence faults stay test adapters.

Joined lifecycle checkpoints are test-only assertion names, not production
hooks: k09-ready, k09-credit, k09-payment, k09-heartbeat, k09-inactivity,
k09-movement, k09-restarted-defense, k09-closure. Their order and component
outputs must be checked; aggregating disconnected passing suites is not the
joined lifecycle witness.

## Controlled providers, negative controls and witnesses

Use the existing controlled beacon/stake source for small group selection,
regtest relay for local mining, and independent in-memory fence for restart
only. They are assumptions, not production qualification. Real public
contracts, BIP340 verification, transport authentication and Bitcoin spending
must execute. All deliberate faults remain test-only adapters.

New orchestrator guards need focused controls: missing or mismatched tool
hash, missing build/source correspondence, failed or skipped required stage,
and evidence output containing secret worker material must fail closed.
A subprocess failure must be reported as an error, never a killed guard.
Component guards reuse the already recorded K-06/K-07/K-08/C-10 controls on
identical sources; do not rerun their full campaigns without a changed guard.

Positive witnesses accompany every refusal. Reject frames include all
financial, custody and obligation state relevant to the attempted operation;
checking only the revert string is insufficient.

## Bounds and assumptions

Two operator processes; three seats, threshold two (one operator has two
seats); at least two real DKG wallets. Local finality two blocks, signing
round ten blocks; D-02/D-09/D-11 values are OPEN test values. D-03 sanctions
are notification only, D-05 uses AUTH_SELF, and D-10 uses local ceilings.
Per-stage limits must be recorded before execution. Core-backed and tss-lib
jobs run alone. No exhaustive interleaving, host-loss durability, production
scale, production funding gate or proof of the Go source is claimed.

## Readings (OPEN for owner review)

1. K-09a demonstrates the lifecycle; K-09 exit remains incomplete until the
   separately authorized K-09b and required verification/composition exits.
2. Local scheduling invokes real host components explicitly. Production
   daemon scheduling and claim-signature exchange stay K-11 scope; this
   lifecycle must say which operations its harness dispatches.
3. Realistic-header SPV can use retained public Bitcoin transaction/header
   vectors through production validation, separate from the live regtest run.
4. Public-upgrade obligations may use separate reachable legacy fixtures
   when mutually exclusive wallet states cannot coexist in one wallet.
5. A failing existing recovery/enforcement path reopens its owner package;
   it is never bypassed to make the combined run pass.

## Acceptance

Concrete tests and human self-review only, with limits and evidence paths.
The acceptance record must distinguish pass, fail, not run and blocked.
No K-09a acceptance is inferred from historical component results alone.

## As implemented (23 October 2026)

The joined test follows the fixed assertion checkpoints, using the existing
component hooks without adding production hooks. The local closing period is
one second (OPEN) so the restart defense is checked before the fraud deadline.
The runbook dispatches the components, validates every required case and
retains source/command/log hashes. Six throwaway guard controls and their
harness self-test cover the new runner guards. Production artifact binding
walks every linked library (27 artifacts in the recorded run).

A separate offline-refund witness removes operator signing resources before
constructing the refund. The contract side supplies three reachable public
legacy upgrade checkpoints and six production-factor SPV cases. The scoped
acceptance record is `test/frost/K09.md`; `COMPOSITION-K09A.json` records the
concrete joins without promoting them into V-05 discharge or source proofs.
K-09b has not run. All readings above remain OPEN for owner review.
