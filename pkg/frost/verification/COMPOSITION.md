# V-05 incremental composition ledger

Date: 15 October 2026. Basis: KC-SF-V3 revision 01 section 8.3 and KC-VER
revision 01 V-05. This is an open ledger, not a composition safety statement.
No row is discharged and no production gate is closed. The authoritative per-row
records are in `COMPOSITION.json`: producer result, consumer precondition,
joining evidence, model/assumption IDs, separate producer/consumer blob pins,
status, ownership and unresolved correspondence. `composition-source-pins.json`
indexes the same selected sources. Evidence retains its original scope;
this ledger does not turn concrete tests into a source proof.

| Row | Status | Producer → consumer | V-01–V-04 predicate / assumption IDs | Joining evidence / remaining work |
| --- | --- | --- | --- | --- |
| 1 | open | SF candidate → same host publication | P-K03-ONE-DESCRIPTOR; P-K03-SUBMITTED-NOT-TERMINAL | SF vectors/K-03 fixture; V-03 full replay/review pending |
| 2 | open | Completion/store → fresh-reload readiness | P-K03-RELOAD-ONLY-DURABLE; P-K03-READY-ONLY-RELOADED; A-READY-FRESH-RELOAD | Reload/loss fixture and V-02 statement; tool/source alignment and V-03 join pending |
| 3 | open | Approved identity → intended Bridge wallet | P-K03-ONE-DESCRIPTOR; P-K03-EXPIRY-TOMBSTONE; A-CHAIN-FINALITY | Real callbacks/upgrade tests; finality/reorg/readiness/expiry correspondence pending |
| 4 | open | Deposit template/Q → admitted spend/refund | none in V-01–V-04 | Local deposit profile/reveal and draft C-07 verifier implemented; named C-07 predicate qualification, D-04 approval and K-06 join open |
| 5 | open | Funded request/intent → exact signing | P-K04-AUTHORIZED-ONLY; P-HOST-ONE-INTENT-ATTESTATION; P-HOST-REPLACEMENT-FINALIZED-ABSENCE; A-OPERATOR-ATTEST-AFTER-VALIDATION; A-ONE-INTENT-PER-STATE-VERSION; A-STORED-DESCRIPTOR-ONLY; A-DUPLICATE-REQUEST-STATE-GUARD | K-05 provider/planner/attester pinned at `e9fc20764`; V-04 S-K05-SIGN-2 (admission replay on the real slot lock); certificate transport, finality depth and production policy open |
| 6 | open | Signature/frozen transaction → same broadcast/proof | P-K05-LATE-SIGNATURE-QUARANTINE (blocked, spec #2); P-K05-TERMINAL-ATTEMPT; A-BUILDER-FROZEN-BEFORE-SIGN; A-SPV-MAINTAINER-RELAY; A-K05-LATE-SIGNATURE-SETTLEMENT | K-05 resolver/ledger/SPV maintainer pinned; K-05 named tests and V-04 process/recovery; qualified relay/maintainer and the A5 owner decision open |
| 7 | open | Bitcoin custody → one credit/settlement | P-K05-NO-DOUBLE-OUTCOME; A-FEE-INCLUSION-ESCALATION; A-SPV-MAINTAINER-RELAY; A-K05-TIMEOUT-POLICY | V-04 C-04 projection replay on the real Bridge (partial) with negative control; K-05 timeout-race test; timeout policy (spec #3), qualified C-05/C-07, D-08 and C-06 joins open |
| 8 | open | Signature/challenge → exact defense | none in V-01–V-04 | Draft C-07 verifier and retained C-04 authorization/containment implemented; canonical C-08 defense and V-06 qualification open |
| 9 | open | Cleared obligations → retirement | none in V-01–V-04 | C-09 closure/K-12 custody and archive inventory pending |
| 10 | open | Approved source/config → deployment claims | none in V-01–V-04 | C-11/K-10 runtime/fence/layout/build/deployed-state records pending |

Each row's producer/consumer hashes are materialized in `COMPOSITION.json`, not
inferred from a global pin list. A still-unimplemented side has an empty pin list and
an explicit blocker; it is not treated as fresh or discharged. The freshness
checker reports which row and side changed. Matching selected pins does not
establish complete dependency coverage or a model result.

## Published C-04/C-05/C-07 pin refresh (14 October)

Selected tbtc-v2 sources resolve at final C-04
`47ebb2a871552e2c2a25cfc67cd12ac15554f5d4`, whose final legacy-witness-only
successor leaves production unchanged from `c089289cea6cf92e84018e92a1c31457111e9857`.
The inherited published prerequisites are C-05
`f7a5b473836b93a6bfe7d24c049dc6f76fb8cb80` and C-07
`6cb35c0f5894e80b0f189f31ca49effc4efe3b86` (both verified ancestors).
Keep-core selected sources resolve at K-04
`3587ce2a2280261d138625af609c983e0fcd66d4`; Snowfall remains at
`c1847ebdf133d0570643ff968539dd498069f69f`.

The selected manifest remains exactly 59 blobs: 19 keep-core, 4 Snowfall and
36 tbtc-v2. All selected producer/consumer references are rebound to these
current checkout commits; 10 selected blob contents changed since the previous
published selection. Historical joining-evidence references retain their
original commits and scopes. No selection is added, removed or discharged.
Both reviewed specification provenance records are unchanged.

Rows 4/5/7/8 now identify implemented local producers: exact deposit bytes and
reveal, prototype BIP340/TapTweak, common custody, backed escrow and operator
reservation, captured-input payment/cancellation, shared callback guard and
governance quarantine with retained recovery. Row 5 now records the on-chain
producer to host-signer consumer direction. Row 8 still has no canonical C-08
challenge/defense consumer; governance containment is not that defense.

Committed public test sources and boundary documents provide scoped joining
references. They include the 255-request local batch, shared Bank/Vault callback
and rollback paths, and the new unreserved-timeout/finality and admission-deadline
cases. References to these test sources do **not** claim unpublished run logs,
reproduce all executions, or discharge required source/model correspondence. Controlled registry
readiness, known scalar3, prototype verifier and zero integer regtest difficulty
remain explicit local boundaries. The historical 255-request public gas witness is superseded for current-profile
feasibility. The published profile is 64 requests/32 recognized inputs/65 outputs
and up to 100 certificate seats. Published local full-admission collision/gas
fixtures and one-input public/Core financial witnesses are separate scopes;
exact target/watchtower/proof budgets and production gas qualification remain OPEN.

All ten rows and fifteen caller obligations remain **OPEN**. K-05 canonical
providers/eligibility and finalized admission, V-04 recovery/replacement and
signing correspondence, named C-07/V-06 predicate evidence, required full financial-frame tests/review
and source/model correspondence, D-08 legacy reconciliation, C-06 joins and C-11/G-F release qualification remain open.
Owner/monitor/deadline placeholders are unchanged. Matching published sources
and local witnesses is incremental provenance, not milestone completion.

## Caller obligations and operational assumptions

| ID | Required guarantee | Current disposition | Role owner | Named person | Monitoring / escalation policy |
| --- | --- | --- | --- | --- | --- |
| A-K04-AGREED-EXECUTOR-CONFIG | Operators agree plan, reservation, session, start block, selected seats, domain and chain policy | K-05 derives it locally from finalized chain state; cross-operator transport absent | K-05 coordination/signing leads | TBD | Not approved; see operational record in COMPOSITION.json |
| A-GUARDED-END-BEFORE-RELEASE | Worker operation ends before live claim ownership releases; terminal history persists | Engine/store tests and ordering review exist; V-04 source correspondence pending | Engine/storage leads | TBD | Not approved; see operational record in COMPOSITION.json |
| A-READY-FRESH-RELOAD | Readiness collector reloads each counted completed key in a fresh worker | Real K-03 witness exists; V-03 counting correspondence pending | K-03/C-03 leads | TBD | Not approved; see operational record in COMPOSITION.json |
| A-BUILDER-FROZEN-BEFORE-SIGN | Freeze transaction and all prevouts before deriving messages or granting signing | K-04 immutable snapshot/vector/worker traces exist; independent recovery review responded at `05032bb6`; V-04 correspondence remains open | Bitcoin/signing leads | TBD | Not approved; see operational record in COMPOSITION.json |
| A-CHAIN-FINALITY | Chain providers supply current canonical internally consistent state under configured finality | Local policy only; production D-09 unresolved | Chain/security/operations owners | TBD | Not approved; see operational record in COMPOSITION.json |
| A-FENCE-STORAGE | Fence and sync semantics prevent clone/rollback claim reuse | Local model/test service only; production target qualification absent | Storage/operations owners | TBD | Not approved; see operational record in COMPOSITION.json |
| A-OPERATOR-ATTEST-AFTER-VALIDATION | Attest only after exact transaction/request/fee validation and fresh key reload | K-05 `Attest`: validate, reload, lock, sign (named tests; not in V-04 replay) | Signing/C-04 leads | TBD | Record failures and refuse attestation; named monitor pending |
| A-ONE-INTENT-PER-STATE-VERSION | Refuse conflicting attestations until finalized expiry and canonical proof of no prior admission; admitted intents never expire | K-05 durable slot lock implemented; V-04 admission replay 22/22 at bound | Signing/storage leads | TBD | Retain history; replacement requires finalized deadline plus no-admission evidence; inconsistent views refuse and escalate |
| A-READY-SEAT-ELIGIBILITY | Honest hosts use only readiness-certified, not-reported-lost seats; validator does not enforce these sets | Canonical eligibility storage/provider pending | Readiness/signing leads | TBD | Reconcile eligibility and fresh reload; refuse inconsistent state |
| A-STORED-DESCRIPTOR-ONLY | Load stored descriptor and canonical wallet state; eligibility provider/storage remains required | Local Bridge consumer uses stored descriptor; finalized K-05 provider/eligibility correspondence open | Bridge/admission lead | TBD | Reject mismatched authority context; host eligibility obligation is separate |
| A-FEE-INCLUSION-ESCALATION | Resolve inclusion/availability for original payment and every retained cancellation within captured limits | In-envelope alternatives, independent budgets and bounded loser claims implemented; above-envelope policy and canonical/model qualification OPEN | Operator recovery/governance/risk | TBD | Preserve escrow and all alternatives; owner, monitor and deadlines remain unapproved |
| A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION | Establish canonical ownership or reviewed conflict resolution across main-spending purposes | Certified sweep conflict proof and individual refunds implemented; host/archive/provider/model and general movement joins OPEN | K-05 coordination/C-05 sweep/C-06 movement and recovery owners | TBD | Durable history, finality/reorg handling and owner/runbook adoption required |
| A-SPV-MAINTAINER-RELAY | Authenticate every FROST SPV proof caller and qualify relay/work/canonical-chain trust | Gates and real-dispatch refusal tests published; qualified relay/canonical-chain and maintainer availability OPEN | SPV maintainer/relay/security and C-11 release owners | TBD | Maintainer availability and chain inconsistency monitoring unapproved |
| A-K06-CANONICAL-SWEEP-BUILDER | Canonical v2 sweep, include existing unreserved main, never consume reserved main | Rule and focused regressions published; K-06 host-builder correspondence OPEN | K-06 builder/C-05 sweep and coordination owners | TBD | Preserve additive reserved credits and cross-purpose history; runbook unapproved |
| A-DUPLICATE-REQUEST-STATE-GUARD | Each request transitions Requested→Reserved once in one atomic admission | Local atomic admission loop implemented; full source/model and provider correspondence open | C-04 admission lead | TBD | Duplicate or conflicting request reverts complete frame |
| A-K05-TIMEOUT-POLICY | Choose the TimeoutPending host policy and deadlines (spec issue #3) | Both K-05 policies implemented; neither is a default | Owner with K-05 signing/C-04 leads | TBD | No default; escalate unresolved TimeoutPending intents |
| A-K05-LATE-SIGNATURE-SETTLEMENT | Decide whether a late valid signature may settle the exact admitted payment by SPV (spec issue #2, A5) | P-K05-LATE-SIGNATURE-QUARANTINE blocked; V-04 claims neither reading | Owner with C-04/V-04 owners | TBD | No new release or second release; report late signatures |
| A-K05-CERTIFICATE-TRANSPORT | Authenticated cross-operator transport for certificates, seat signatures and cancellation attestations | Absent; local fixture collects in-process | K-05 coordination/transport leads | TBD | Refuse incomplete or conflicting sets; escalate partitions |

V-01/V-02 statement freshness is checked on its own verification branch. K-04
changes the pinned engine interface and the implementation stack does not yet
include the newer #10 model/replay tooling. Reconcile source and tool pins before
citing that statement against a combined integration head. Historical statement
S-K02-HOST-1 is superseded and remains intentionally stale.

At each package exit, replace the remaining obligation with exact witness/model
record paths and both producer/consumer source hashes. Independent adversarial
review and its feedback responses must be retained before marking a row discharged.
The ledger must close before the K-09 checkpoint mutation campaign. Unresolved
operational assumptions need a named decision owner and release-gate record;
merely naming an owner does not qualify deployment.

## Independent review response

The independent ledger review found one stale status: revised K-04 review had
completed while the row still said pending. Corrected above; no row is discharged.
The [K-04 response on PR #11](https://github.com/tuxxy/keep-core/pull/11#issuecomment-6146472395)
records both recovery findings, fixes and independent re-review disposition.
The expanded selected manifest and per-row sources are recorded in
COMPOSITION.json and composition-source-pins.json. It is an incremental selection, not a complete
dependency/freshness manifest. Add exact per-row evidence and all producer/consumer
dependencies before discharge, as the reviewer requested. V-03/V-04 and the
funded C-04/K-05 boundary remain open.

## Specification provenance and freshness scope

The integration `44f1356` and verification `d45afda` identifiers are Git **commit**
IDs in `tlabs-xyz/FROST-migration-spec`, not document hashes or keep-core objects.
Both JSON files now label their source repository, commit kind, original path,
Git blob algorithm/hash and raw SHA-256. Exact snapshots were materialized with
`git show <commit>:<path>` and the commit-to-blob relation checked in that repo.
The reviewed blobs are `98f8589a48ab95d065d5b95307734d66b93618f1` and
`53535870285d22a60472eacfaffbd6e277a1a29b`. There was no governing-content drift;
the earlier fields did not distinguish revision identity from content identity.

Pass both reviewed spec files explicitly with `--spec KC-SF-V3=/path/to/spec`
and `--spec KC-VER=/path/to/companion`, plus `--repo NAME=/path/to/checkout` for
Snowfall and tbtc-v2. Missing provenance/files refuse; content drift returns a
separate spec mismatch. Omitted/duplicate required caller obligations and null
source hashes refuse. An operational-owned assumption needs an assigned person,
named monitor and approved deadline or an explicit not-applicable disposition.
The checker compares selected source/spec content; historical evidence, revision
correspondence and actual model/discharge arguments still require human review.
Strict structural success cannot substitute for that review.

## Claude review response

[Review on PR #12](https://github.com/tuxxy/keep-core/pull/12#issuecomment-6146630522):
F1 adds the complete record schema, explicit statuses and per-side hashes, with
row-specific freshness reporting. F2 adds operator-validation, single-intent,
stored-descriptor and fee/inclusion/escalation obligations. F3 records the
reservation ID, separate uint64 intent counter versus uint32 readiness counter,
stateVersion rules and remaining canonical provider/source correspondence. F4 pins
the normative K-04 document and both vector copies plus C-04 helper code at the
review-fix source. F5 adds named-person and monitoring/escalation placeholders;
none is invented or treated as approved. These fields must be completed before
K-09 closure. No row is discharged by these corrections.

Claude C-04 re-review N1/N2: replacing an unadmitted certificate requires the original inclusive deadline and every outstanding renewal deadline to be finalized, with canonical finalized proof that the slot had no admission at or before any of those deadlines. Retain every attestation and renewal; expiry never releases or replaces an admitted reservation. Bitcoin signing starts only after durable admission. The V-04 replacement witness remains pending. Readiness-certified/not-lost eligibility is an explicit honest-host assumption until canonical storage/provider enforcement exists.

Every source and joining-evidence reference must resolve to its pinned Git blob at the recorded commit. Discharged or operational-owned obligations require a nonempty verified evidence list; the current open obligations have empty lists. These checks establish existence and provenance, not semantic relevance or proof validity. Package-appropriate dependencies and evidence sufficiency remain independent review requirements.

Third-pass nits: evidence scopes must explicitly name their `ROW-N` or caller-obligation ID; genuine but unrelated references refuse. This is an identity check, not semantic relevance. Independent human review and its immutable feedback/disposition records remain mandatory before discharge; strict success only reports structural eligibility. Exit codes: 0 selected checks pass (strict: structurally eligible); 1 source/spec drift; 2 strict ledger still open/incomplete; 3 invalid ledger/provenance/input files. Command-line syntax errors retain argparse exit 2.

## Historical pin-refresh validation commands

Only the three ledger/manifest files changed. The existing checker fixture suite
passed all 10 tests in 15.53 seconds including the outer wrapper; explicit
source/spec freshness passed with exit 0 in 3.20 seconds. Both were capped at
180 seconds. Both reviewed specifications match and there are zero selected
row-source mismatches. A separate read-only check resolved all 56 selected
commit/path/blob references, checked blob kinds and current file hashes, and
matched their per-row index entries. These are structural/provenance checks,
not a new source-proof campaign or model result. All 10 rows and 12 obligations
remain open.

Reproduce the existing checks from the V-05 checkout (each under an external
180-second cap):

```sh
python3 -B pkg/frost/verification/test_freshness.py
python3 -B pkg/frost/verification/check_freshness.py \
  --repo keep-core=/Users/tux/threshold/integration-worktrees/K-00-SF-01-K-01/keep-core \
  --repo snowfall=/Users/tux/threshold/integration-worktrees/K-00-SF-01-K-01/snowfall \
  --repo tbtc-v2=/Users/tux/threshold/integration-worktrees/C-04-reservations \
  --spec KC-SF-V3=/Users/tux/threshold/Research/SNOWFALL/integration-spec-v3/keep-core-integration-spec.md \
  --spec KC-VER=/private/tmp/kc-ver-integration-current.md
```

The `--repo` values are the actual current roots used; specification arguments
name the explicit reviewed files, not inferred repository revisions. The final post-edit `--strict` run returned the expected exit 2 in 2.73
seconds, with zero drift and all rows/obligations still open; it therefore keeps
the ledger structurally ineligible for discharge. No source, owner or obligation status was discharged by this refresh.

Execution records for this refresh are available at
`/private/tmp/v05-pin-refresh-tests-20261014.log`,
`/private/tmp/v05-pin-refresh-freshness-20261014.log` and
`/private/tmp/v05-pin-refresh-strict-20261014.log`. The actual outer command was
`python3 /private/tmp/c05-profile-20261014/run_capped.py 180` followed by each
inner command above; the strict run appended `--strict`. That reused wrapper
bounds and reports the child exit code; it adds no verification predicate.
Original specification commit-to-blob relations were also rechecked in the
existing `Research/SNOWFALL/FROST-migration-spec` Git repository; the explicit
reviewed-file content hashes match their unchanged provenance records.

## Draft C-04 A3/A4/A5 and C-07 C7-3 response

[C-04 review](https://github.com/tuxxy/tbtc-v2/pull/2#issuecomment-6168676791)
and [C-07 review](https://github.com/tuxxy/tbtc-v2/pull/4#issuecomment-6168347321)
remain release correspondence work; these draft records adopt no owner policy.

A3: a signed main-input sweep can confirm after redemption admission and then
be refused at sweep proof time, leaving neither reserved payment nor cancellation
able to spend that main input. Rows 5/7 now carry the required OPEN cross-purpose
serialization obligation. Signing-time fencing/durable history or a reviewed
canonical conflicting-spend resolution must be selected and qualified with the
financial frame and V-04 interleavings. The current proof guard is insufficient.

A3 quorum limitation: local inventories and ordinary threshold certificates do
not by themselves establish cross-purpose serialization. For n=3/t=2 with one
Byzantine operator, competing quorums can intersect only at that Byzantine
operator, who signs both acts. Shared canonical main-output ownership, or a
justified agreement/conflicting-spend resolution protocol, is required; durable
local history alone is insufficient. This remains OPEN K-05/V-04 correspondence
and does not adopt a resolution or owner policy.

A4: cancellation is authorized once, the original payment remains exact, and
captured fee/rate caps survive later policy changes. There is no higher-fee
replacement cancellation route. Fair fee/inclusion, signer availability and
monitored recovery/escalation are OPEN release assumptions, including persistent
fee spikes; neither recipient CPFP nor wallet CPFP is qualified recovery here.

A5: first custody credit initializes stateVersion to 1; later unreserved credit,
confirmed payment/cancellation and quarantine advance it. Reserved additive
credit preserves captured mainOutpoint, intent generation and stateVersion.
Reservation advances the uint64 intent generation, separate from uint32 readiness.
Timeout, cancellation authorization and ordinary policy/configuration changes
do not advance stateVersion. An unreserved sweep invalidates earlier attestation
by advancing version. Late host results must not grant new signing authority;
SPV can settle an exact previously authorized mined payment after timeout or
quarantine. The literal P-K05-LATE-SIGNATURE-QUARANTINE “cannot authorize
settlement” wording needs explicit owner/model correspondence disposition. No
normative specification has been edited or general proof campaign added.

C7-3: rows 1/3/8 now bind keep-core FrostDkgValidator and FrostCurve, plus the
Bridge FrostCurve consumer, to verified published commit/path/blob references.
C-02 calls validX: field range, x³+7, modexp square-root and square-back checks;
it returns a boolean, whereas C-07 lift also constructs the normalized even-Y
point. Named P-C07 evidence and specialist review must cover both implementations
and their consumers. This adds traceability, not predicate qualification or a
DKG implementation migration.

A1: historical 255-request/29M transaction/30M block feasibility and the interim
128-request single-input profile are superseded. The published joint profile is
64 requests/32 recognized inputs/65 outputs/up to 100 certificate seats. The
C-04 boundary document records a full-admission fixture including deliberately
colliding outpoints and genuine certificate verification, separately from the
one-input public/Core settlement witness. Exact target/watchtower/proof budgets
and production gas qualification remain OPEN. These sources do not close A1 or
supply a formal bound.

All ten rows and fifteen obligations remain OPEN; no joining evidence is added
for these release decisions. Selected source pins now match the final published
heads above. Historical drift results below record intermediate checkouts and
are superseded for current freshness by the final refresh check record.

## Historical review-response validation (intermediate draft, 14 October)

The capped checker suite passed 11 tests (14.550s inner; 14.65s wrapper), including
refusal when A-MAIN-SPEND-CROSS-PURPOSE-SERIALIZATION is omitted. Normal freshness
returned exit 1 in 2.89s; strict returned exit 1 in 2.93s. Both reviewed specs
match. Drift is correctly reported in rows 5/7 for parent cap changes to Go
INTENT-V2.md/plan.go and Solidity FrostRedemptionTransaction/FrostSigningIntent,
this C04-BOUNDARIES draft, and the parent public settlement test change. No drift
was hidden by repinning uncommitted code. A read-only check verified all 59
selected published commit/path/blob references and per-row manifest agreement.
All 10 rows and 13 obligations are OPEN.

Exact inner commands are the two commands in the historical validation section
above (the suite is now 11 tests); the strict invocation appends --strict. Each
was launched with `python3 /private/tmp/c05-profile-20261014/run_capped.py 180`
followed by that inner command, from the V-05 checkout. Output artifacts:

- `/private/tmp/c04-v05-review-response-tests-20261014.log`
- `/private/tmp/c04-v05-review-response-freshness-20261014.log`
- `/private/tmp/c04-v05-review-response-strict-20261014.log`
- `/private/tmp/c04-v05-review-response-pin-verification-20261014.log`

These are local check records, not committed joining evidence or a qualification
claim. Parent publication and final source-pin rebind remain required.

## C-05 interim review response amendment

[Review F1–F6](https://github.com/tuxxy/tbtc-v2/pull/5#issuecomment-6169029575)
adds two required OPEN obligations, bringing the current total to 15. All ten
rows remain OPEN; the preceding 13-obligation check record is historical.

F1: rows 4/6/7/8/9 link authenticated SPV-maintainer admission and qualified
relay/work/canonical-chain assumptions. Accumulated work alone is not canonical
Bitcoin membership. C-05/C-04 gates and negative real-Bridge tests are now
implemented/published; relay/canonical-chain qualification and maintainer
availability remain OPEN. Retirement must retain proof/maintainer and
custody history; no maintainer status is an automatic lifecycle discharge.

F2/F6: row 4 records the K-06 canonical v2 builder obligation and selected rule
to include existing unreserved main; reserved main cannot be consumed. Deposit-only
credits during reservation must preserve the captured plan. The C-05 main-input fix and focused regressions are now implemented/published;
K-06 host-builder correspondence remains OPEN. No alternative fragment
consolidation is adopted here.
Row 4 now additionally source-binds already-published Sweep/Custody blobs from
the existing manifest; the selected unique pin count stays 59.

F5: joint input/real Vault mint and callback gas, plus Bitcoin weight, remain
OPEN pending parent measurement and qualified limits. Bank-only extrapolation
does not establish the bound. F3 guard coverage and F4 reproducible isolated
evidence are assigned work; this amendment does not claim their tests complete.

F6: shared spent identities are the intentional cross-scheme replay boundary,
not a newly proposed scheme-separated map. Rows 7/8 retain the legacy-defense
and accounting correspondence review. Reveal refuses nonzero rebateStaking;
rebate ownership/activation remains explicitly unresolved C-11 release work.
Verifier installation is once-only, so replacement requires a reviewed Bridge
upgrade, not a new setter assumed here. pendingReveals includes refunded or
fabricated reveals and is not live custody/rights or retirement evidence; rows
4/9 require the actual lifecycle inventory. No production or C-05 files changed.

Amendment checks: 12 checker tests passed (15.936s inner, 16.03s wrapper),
including refusal if either new obligation is omitted. All 59 published selected
pins resolve, and row references agree with the manifest. Explicit current-root
freshness returned exit 1 in 3.01s: both specs match; pending cap/boundary/test
changes plus the parent FrostBridge gate edit remain visible as source drift.
No draft source was repinned. Both runs used the same exact commands and roots
recorded above, capped at 180s. New output records:

- `/private/tmp/v05-c05-review-amendment-tests-20261014.log`
- `/private/tmp/v05-c05-review-amendment-freshness-20261014.log`

These local logs are not joining evidence. All 10 rows and 15 obligations remain
OPEN; final parent publication/rebinding and assigned review-fix evidence remain
required. No commit, push, publication or production change was made here.

## Rows 6/7 SPV obligation sidecar

Row 6 now explicitly links A-SPV-MAINTAINER-RELAY; row 7 already linked it.
Both consumer preconditions explicitly require authenticated approved maintainer
callers under qualified relay/work/canonical-chain and finality assumptions.
The row 6 remaining record and both Markdown summary rows make that join visible.
Accumulated work alone is not canonical Bitcoin membership; local gate checks
do not qualify K-05/K-07 providers, relay trust or maintainer operations.

No obligation was added or discharged: all 10 rows and 15 obligations remain
OPEN. All per-row source/evidence references and the manifest are unchanged by
this sidecar; parent owns final C-04 history and source-pin refresh.

Sidecar checks (180s caps): existing 12-test checker suite passed (19.832s inner,
19.95s wrapper). Explicit current-root/spec freshness returned exit 1 in 3.48s
for existing source drift in rows 3/4/5/7/8 during parent integration/history
cleanup; both reviewed specifications match. All 15 unresolved obligations
remain OPEN. Exact commands/roots are the previously recorded suite and normal
freshness commands, with the same external capped wrapper. New output artifacts:

- `/private/tmp/v05-row67-spv-sidecar-tests-20261014.log`
- `/private/tmp/v05-row67-spv-sidecar-freshness-20261014.log`

Diff whitespace checks passed. This sidecar changed only COMPOSITION.md and
COMPOSITION.json, with no source/evidence-pin refresh or commit/publication.

## Final published source refresh

The maintainer gate and real-dispatch refusal tests are published in C-04/C-05;
A-SPV-MAINTAINER-RELAY remains OPEN for qualified relay/work/canonical-chain
correspondence and maintainer trust/availability, owner/monitor and release
records. The C-05 v2/main-input rule and focused main-input regressions are also
published; A-K06-CANONICAL-SWEEP-BUILDER remains OPEN for host-builder/cross-purpose
correspondence and the approved runbook. No obligation is operational-owned or
discharged by this implementation-status correction. Rebate ownership/C-11,
install-once verifier upgrade qualification and pendingReveals lifecycle limits
remain unchanged and OPEN.

Only existing selected source pins were refreshed. Historical joining evidence
retains original revisions, including earlier public batches. The final legacy
upgrade witness is available at C-04 head47ebb2a8 but is outside the unchanged
59-path selection; no fresh test execution is inferred from pinning source.


## Resume check — 15 October 2026

The saved final checker run completed all 12 tests (55.64s wrapper); normal
freshness exited 0 and strict closure exited 2 as expected for open rows. Before
publication, explicit current-root/spec freshness was repeated with the command
above: exit 0 in 3.10s, all selected row pins and both specifications matching.
All ten rows and fifteen caller obligations remain OPEN. The selected manifest
pins keep-core3587ce2a2 and tbtc-v2/C-0447ebb2a8; it does not include later work.

A3 requires shared canonical main-output ownership or a reviewed canonical
conflicting-spend resolution. Independent local signer inventories do not
establish that guarantee: with three seats, threshold two and one Byzantine
seat, competing quorums may intersect only at that Byzantine seat. No local
serialization fix or protocol adoption is claimed by this source-pin refresh.


## A3 certified sweep conflict recovery — 15 October 2026

Selected tbtc-v2 references now bind C-04 `b20ed301786b714a0ec4dbd81ce0909fe9ff5619`
(production fix `0254f578`, followed by the passing legacy source rebind). The
manifest now selects 63 paths, adding the resolution certificate and three scoped
conflict test sources. Historical joining evidence retains its original pins.

The new SPV-maintainer-gated route requires a distinct operator certificate for
the captured reservation/main and exact canonical C-05 sweep. It preserves other
custody and makes requests individually refundable. Full escrow/net liability
remain held until each permissionless claim pays its stored redeemer. Original
payment/cancellation history remains intact. Signature/context commitments bind
the full certificate in canonical transaction calldata and its event.

Scoped local runs: actual Core signed-sweep-before-reservation recovery passes;
the 64-request/128-input/100-seat proof uses 14,151,347 gas through Bank and
14,960,190 through TBTCVault under the actual 16,777,216 cap. Maximum individual
claim gas is 104,463. Synthetic gas proofs and known-key/regtest Core evidence
are distinct; they do not qualify canonical chain work, threshold signing or
all proof/debt/target states. The old push-refund maximum failed the cap; its
30M diagnostic is not qualifying evidence.

A3 remains an OPEN composition obligation for canonical host/provider/reorg
correspondence, durable certificate/receipt retrieval, production D-08 adoption,
general movement conflicts and assigned monitoring. A4 and other release gates
remain OPEN. All ten rows and fifteen obligations stay OPEN. No canonical
signing serialization, losing-signature defense or model discharge is claimed.

Before A3 pin publication, explicit current-root/spec freshness exits 0 in the
bounded run: every selected row source and both governing specifications match.
The checker implementation is unchanged; the prior 12 fixture tests remain the
checker validation, and no composition obligation is discharged.


## A4 retained cancellation alternatives — 15 October 2026

Selected tbtc-v2 source pins now bind C-04
`820b5308b38d8b17a40a1d78edc9f8047ffc51f1` (production fix `010219f2`).
The manifest selects 68 paths, adding fixed Admissions/History and the explicit
synthetic history fixture plus alternative/history test sources. Earlier joining
references retain their historical commits/scopes.

A4 has immutable increasing-fee alternatives within the original captured
envelope, independent full sponsor budgets, proof selection by exact stored
transaction/snapshot/fee and one winning burn. First fields remain unchanged;
legacy returned/burned budgets cannot reopen. Terminal unused budgets have
bounded permissionless claims to their stored sponsors. History length does not
enter settlement loops. Actual Core RBF replaces an old mempool transaction and
mines the higher-fee alternative; independent branches also permit either old
expired alternative or original payment to win. Local synthetic history units
are not substituted for funded lifecycle or upgrade proofs.

With64requests/128inputs/100operators and two budgets, Bank/Vault proofs use
14,176,115/14,984,802gas under the real16,777,216cap. Additional100-seat admission
uses6,091,093gas; max request claim104,687 and sponsor claim94,228. Gas proofs
remain synthetic and distinct from the known-key/Core consensus witnesses.

The appended history mapping consumes one reserved Bridge tail slot without
moving existing fields/enum values. Current-source legacy public upgrade replay
passes separately. These source/gas/test facts do not close above-envelope policy,
fee/inclusion/availability assumptions, canonical K05 archive/provider/reorg and
V04 correspondence, production D08/D09 adoption or C08 defenses. All ten rows and
fifteen caller obligations remain OPEN; A4 is not an unconditional inclusion
guarantee. No governing specification or checker algorithm changed.

## C-05 refund-spent review closeout (15 October)

C-05 `12510a6f` includes the F3 test at `44d74992`; C-04 integrates it at
`3accadc5`. Rows 4 and 7 retain exact committed helper/test joining references.
Eight producer Core/accounting cases pass in 33.60s and eleven integrated
Core/public-funding cases pass in 44.68s. Independent patch review found no
material issue. The same wallet spend is valid before an independent refund and
refused afterward; the real refund proof cannot create sweep credit. Controlled
keys/metadata and regtest work remain explicit limitations.

Production source is unchanged from the existing selected pins; their 68-path
selection remains valid. Historical joining records retain their original
revisions. This records the completed local review correction, not K-06, full
package qualification, or a composition discharge. All ten rows and fifteen
obligations remain open. K-05 is explicitly deferred by the owner.


## K-05 rows — 15 October 2026

K-05 (keep-core #13 `e9fc20764`, tbtc-v2 #7 `ff7735ec`) is recorded in rows 5, 6
and 7. Every keep-core selected source now names revision `e9fc20764`. At K-05,
`signing/plan.go`, `executor.go` and `done.go` changed, so rows 5 and 6 were
stale against the K-04 pins. Unchanged files keep their blob. New consumer
sources: the `pkg/frost/redemption` provider, planner, attester, reservation
seam, agreement, reload, resolver and ledger; the Ethereum C-04 binding; and
the SPV maintainer path. Joining evidence: the K-05 named tests and V-04
statement S-K05-SIGN-2 (keep-core #15 `6ae3fd461`). That statement is
bounded model checking plus concrete replay; it is not a source proof.

Caller obligations, 18 in all, all OPEN:

- A-K04-AGREED-EXECUTOR-CONFIG: K-05 derives the agreement from finalized chain
  state locally, but has no transport.
- A-OPERATOR-ATTEST-AFTER-VALIDATION: `Attest` orders validate, reload, lock and
  sign. The K-05 named tests cover it; the V-04 replay does not.
- A-ONE-INTENT-PER-STATE-VERSION: the durable slot lock is implemented, and the
  V-04 admission replay passes 22/22 at its bound.
- New: A-K05-TIMEOUT-POLICY (spec #3), A-K05-LATE-SIGNATURE-SETTLEMENT (spec #2)
  and A-K05-CERTIFICATE-TRANSPORT.

Freshness against a clean K-05 tree (`integration-worktrees/K-05-redemption/v04`,
the K-05 sources unchanged), with snowfall `c1847eb`, tbtc-v2 `ff7735ec` and both
reviewed specs: exit 0; rows 1–7 match, row 8 partial, rows 9–10 unavailable.
Checker tests: 12/12 pass. The tests that use the real ledger now pass the
keep-core checkout, because obligations cite keep-core evidence. A K-05
working tree with uncommitted review edits is correctly reported stale.
No row or obligation is discharged.

Re-pin (same day): every keep-core pin now names K-05 `e9fc20764`. That head
includes Astra's K-05 review fixes and is restacked on K-03 `01e65f81b` /
K-04 `e865fd83c`, which carry V-03's executor repairs F-V03-01/02. The changed
selected blobs are `pkg/frost/dkg/executor.go` (rows 1–3) and the K-05
redemption sources and named tests (rows 5–7). This commit is rebased onto
K-04 `e865fd83c`.
