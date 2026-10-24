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

K-09 primarily adds orchestration. Review corrections also wire the FROST CLI
to the existing difficulty-aware proof policy (see K09-COVERAGE.json). Reuse the component hooks verbatim:
transaction-frozen, before-intent-record, before-defense-record,
before-worker-start, after-signature, before-done-accept, challenge-seen,
defense-record-read, before-defense, defense-submitted, before-timeout-notify,
timeout-notified, heartbeat-outcome-recorded, claim-derived, before-claim-submit,
claim-submitted. Existing storage/fence faults stay test adapters.

Joined lifecycle checkpoints are test-only assertion names, not production
hooks: k09-ready, k09-credit, k09-payment, k09-heartbeat, k09-inactivity,
k09-cutover, k09-movement, k09-restarted-defense, k09-closure,
k09-post-cutover-dkg. Their order and component
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

Two in-process operator bundles with firewall.Disabled; three seats, threshold two (one operator has two
seats); at least two real DKG wallets. Local finality two blocks, signing
round ten blocks; D-02/D-09/D-11 values are OPEN test values. D-03 sanctions
are notification only, D-05 uses AUTH_SELF, and D-10 uses local ceilings.
Per-stage limits must be recorded before execution. Core-backed and tss-lib
jobs run alone. No exhaustive interleaving, host-loss durability, production
scale, production funding gate or proof of the Go source is claimed.

## Readings (OPEN for owner review)

1. K-09a demonstrates the lifecycle; K-09 exit remains incomplete until the
   separately authorized K-09b and required verification/composition exits.
2. Local scheduling invokes real host components explicitly. Real node process dispatch and firewall qualification remain OPEN K-09
   exit requirements. No K-11 deferral is approved by this local record.
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
retains source/command/log hashes. Thirteen throwaway guard controls, their harness self-test, and a config-only
Hardhat control cover the runner guards. Production artifact binding
walks all shared fully qualified deployment roots and linked libraries.

A separate offline-refund witness removes operator signing resources before
constructing the refund. The contract side supplies seven reachable public
legacy upgrade cases and six production-factor SPV cases. The scoped
acceptance record is `test/frost/K09.md`; `COMPOSITION-K09A.json` records the
concrete joins without promoting them into V-05 discharge or source proofs.
K-09b has not run. All readings above remain OPEN for owner review.


## Review corrections (23 October 2026)

K09A-B01/C01/C02/B02/C03 are addressed by the local runner and paired config:

- Hardhat tests and compilation select the local network explicitly. The K-09
  config removes external networks, disables forking after dotenv loads, and
  rejects external selection. The runner removes inherited deployment inputs.
- Every owned stage process group is terminated on success, nonzero exit,
  timeout or interruption. TERM/KILL escalation does not depend on leader exit.
- Artifact binding compares all compiler input sources, including package
  imports, and records compiler/input/build-info identities. It is checked
  again after execution. Local Go replacements are resolved recursively and
  hashed along with the complete host package tree and in-tree dependencies.
  Final enumeration catches additions and deletions, including ignored Go files.
- Public evidence contains only generated case/count/status and provenance
  summaries. Raw stdout/stderr, exception text, and generated legacy artifacts
  stay outside that tree in a fresh 0700 private directory (files use umask 077).
  Unknown producer fields are never copied into public summaries. Recognized
  secret-material labels fail the run and remain quarantined. That additional
  detector cannot recognize every encoding; opaque records are never decoded
  or automatically exported, and private diagnostics are not certified safe
  to publish. Inspect the private directory only under the secrecy policy.

The focused controls exercise stale imports, edited local replacement code,
ignored added Go files, external network/fork inputs, child survival after
success/failure/timeout, forbidden output and the actual runner export path.
No final mutation campaign or new model result is implied by these controls.

## Second-review corrections (24 October 2026)

See K09-COVERAGE.json for every review item and OPEN exit ownership. The
legacy-to-FROST drain is explicitly a seeded component control. The public
upgrade suite is separate, and does not discharge the missing cutover join.
The SIGKILL witness covers an active durable claim and same-store defense
recovery under a surviving controlled fence, not machine or authority loss.
The shared deployment inventory includes independently deployed package
contracts; source binding includes root/internal/cmd/config Go inputs and
actual compiler source/embed dependencies. Ten machine checkpoints must pass
in order. Historical runs do not substitute for fresh changed-source evidence.
