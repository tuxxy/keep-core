# V-04 verification milestone — 13 October 2026

Completed the authorized local checkpoint on normative K-04 source `5c595620d2ec1e2a73ebd811c937c695d162a35d`.
The production executor is byte-identical to `05032bb6b51371d88b1e4a07823ed6e61283dfff`;
its SHA-256 is `ee8096bcd3f8cb6cfe99c75b307dfdf34ca20ef0830a56d1426e06d94e139d64`. No production
code, dependency, activation, policy, PR or remote branch was changed. This is
bounded model checking and concrete test evidence, **not proof of Go** and not
funded C-04 qualification. The full C-04/K-05 replay gate remains open.

## Frozen limits and actual results

One wallet, two requests, two distinct signing attempts, three timeout ticks,
one shared host journal authority, six offered commit stages and at most ten
acknowledged application commits. Exact context collapses to distinguishable
matching/changed identities. The separate proposed admission profile uses two
intent generations, two custody versions, two complete attestation statements,
three certificate variants/deadlines, one renewal, one host crash, one replacement and one retained
authenticated absence-proof slot. Four issued-live observer bits qualify the
replacement witness; finalized block labels are 1, 2 and 3. No liveness or production timeout
policy is inferred from these bounds.

Limits were recorded before runs: 1,800 seconds per command, four model threads,
16 GiB process-tree RSS sampled every 250 ms, Go GOMAXPROCS=4/package parallelism=2,
and 15 seconds per subprocess child. Stateright 0.31.0, Rust 1.94.0, exact Snowfall
client `c1847ebdf133d0570643ff968539dd498069f69f`; other versions are in
`evidence/V-04-2026-10-13/toolchain.json`. Offline model build and private Go
replacement restore the existing pins; production go.mod/go.sum are unchanged.

| Final claim profile | Unique states | Generated states | Maximum depth | Safety/witness/control results |
| --- | ---: | ---: | ---: | --- |
| Signing + abstract reservation | 140,043,852 | 1,150,552,670 | 87 | 6 safety predicates hold; 10/10 witnesses; 13/13 targeted controls |
| Proposed admission + host attestation | 10,935,380 | 79,635,949 | 25 | 7 safety predicates hold; 11/11 witnesses; 11/11 targeted controls |

Both claim explorations completed exhaustively at their bounds. The signing
profile/controls run exited 0 in 231.198143500 seconds, peak tree RSS
10,784,096,256 bytes. Subsequent witness-only changes affected
admission.rs; its final affected-only rerun exited 0 in 12.203323834
seconds, peak tree RSS 1,550,614,528 bytes. No deadline/resource
stop occurred in either accepted run. Their respective modules and actual run
hashes are checked separately; the earlier admission variant in model.log is
historical and is not the final admission result. The active-custody predicate
is shared: twelve distinct safety predicate IDs. All **21 witnesses** and
**24 controls** were found. The controls are independent model weakenings, not
a production mutation campaign. All 45 discovered traces, safe prefixes and
first divergence markers are retained.

| Actual final concrete run | Result | Wall seconds | Scope |
| --- | --- | ---: | --- |
| Focused normal | exit 0, no skips | 8.944338750 | signing, real store/adapter, parent recovery joins, Bitcoin vectors |
| Focused race | exit 0, no skips | 13.058261792 | same scope, race detector |
| TypeScript intent vectors | exit 0 | 0.270207625 | unchanged Go v2 fixed binary vectors |
| Real worker/Core/Anvil fixture | exit 0, no skips | 20.000749417 | TestTaprootExactAuthorization, claim flip refusal and positive signing/broadcast |

Each final normal/race log has 149 test pass events including subtests and one
no-op child entrypoint. Excluding that helper, there are 19 substantive top-level
tests; these are not 149 independent model traces. The fixture run predates the
normative documentation-only amendment; every production file recorded by that
run was checked against the final source. Parent whole-host runs were not repeated
or counted as this agent's runs. Actual commands, UTC timestamps, environment
allowlist, source hashes, exit codes and RSS appear in `*-planned.json` and
`*-actual.json`, with complete logs. No substantive test was skipped.

## Replay and predicate coverage

- AUTHORIZED-ONLY: finality, exact plan/prevouts, missing intent/callback,
  immutable receipt anchor and post-claim final guard controls. Real-store launch
  observer refuses without startup and reaches the same launcher with fresh
  authorized input; the real-worker fixture supplies the genuine crypto witness.
- TERMINAL-ATTEMPT: durable claim cannot be reused after refusal, process kill or
  reopen. Fresh agreed identities can succeed using the same journal. Pre-claim
  activity is not claimed as durable spent history.
- LATE-SIGNATURE-QUARANTINE: possible release survives timeout/re-anchor, valid
  completion is retained, stale/foreign completion cannot authorize broadcast.
- DURABLE-SIGNATURE-RECOVERY: cancellation before host reception of a valid return
  and during cleanup retains Done; Done-only restart reconstructs without a new
  engine call. Parent one-of-two input refusal and two-of-two reconstruction tests
  also passed in the focused suite.
- NO-DOUBLE-OUTCOME and ONE-ACTIVE-CUSTODY-RESERVATION: **abstract C-04 only**.
  Reserved timeout and certificate expiry never independently permit refund.
  Payment/cancellation finality and a common conflict input are supplier assumptions.
- Admission generation/version/tuple-lock/expiry/ID predicates: **proposed consumer
  and attestation profile only**, with no existing Bridge/Go enforcement claim.

`replay-ledger.json` records twelve complete **declared application-boundary**
subprocess scenarios in each normal/race run, with every public observation:
authorized signing, foreign completion, post-claim timeout/re-anchor,
cancellation at valid return/cleanup, four actual process kills at receipt,
worker-return, Done acknowledgement and aggregate acknowledgement, late Done
quarantine, and fresh agreed attempt after claim failure. The actual executor,
encrypted store and separately durable test fence run in child processes; crypto
is controlled valid BIP340. These traces are a distinct set from generated
model traces. They do not execute worker IPC or host power loss, and do not
establish the full durable/volatile disk and fence fault kernel.

Generated signing traces: **0 full, 13 partial, 10 none**. Generated admission
traces: **0 full, 0 partial, 22 none**. The two no-projection worker/attempt controls
have separate real-store evidence; they remain none in that projection harness.
Financial and consumer-only paths remain none because the real C-04 ledger does
not exist. `TRANSITION-MAP.md`, `LIMITATIONS.md`, `ADMISSION-SEMANTICS.md` and the
correspondence ledger identify the joining obligations and owners.

## Normative semantics and honest assumptions

Generation is an independent uint64 intent counter, initially zero, with atomic
counter+1 admission. Custody stateVersion starts at one and increments on confirmed
custody spend or certified authorization-invalidating policy change. It is not
C-03 uint32 readiness generation. One active custody reservation is serialized;
timeout or certificate expiry does not free its slot. The proposed host tuple
lock prevents a conflicting complete statement for the same wallet,
intent generation and state version while a certificate is still admissible or
has been admitted, including after crash. The finalized expired-unadmitted
replacement exception below retains the earlier attestation history.

`validUntilBlock` limits **new admission inclusively**. Provider certificate
verification belongs at the authenticated historical admission block; current
reservation state governs release. An admitted reservation and its Bitcoin
signature do not expire with the certificate. The exact reservation ID is the
Keccak/ABI tuple recorded in ADMISSION-SEMANTICS.md; the model assumes identity
collision freedom and does not prove ABI/Keccak. Go v2 bytes/vectors are unchanged. Hosts read the logical reservation ID from
the authoritative chain record; independent host ABI derivation requires a
future cross-language vector.
The admission profile does not export a signing grant or prove quarantine status;
its version-changing policy action conservatively retains the active slot. A
real consumer must quarantine unresolved old-version authorization and must not
report it Reserved. Literal Bridge slots, consumer transitions, certificate
collection and tuple locks remain declared requirements, not enforced in K-04.

N1 witness **W-expired-unadmitted-replaced-and-admitted: found**. Its original
statement was issued for current counter+1/current custody version while the
certificate was unexpired, in a current unfinalized block, with no active
reservation: it could actually have been admitted at issuance. It subsequently
expires past its inclusive deadline; canonical authenticated finalized absence
at/before that deadline permits one different complete intent to be attested and
then admitted. The model retains both attestation histories. No finalized block
can gain a later admission. **W-admitted-expiry-refuses-replacement: found**.
ReplaceBeforeInclusiveDeadline and ReplaceWithoutFinalizedAbsence both produce
the named REPLACEMENT-FINALIZED-ABSENCE counterexample. Expiry never replaces an
admitted reservation or releases its escrow. The primary signing model requires
finalized/durable admission before a signing grant. N1 concrete consumer replay
is **none**, not a new implemented host or Bridge guarantee.

N2 is **A-HOST-READINESS-NOT-LOST**, owned by honest-host/K05 readiness and
coordination implementers until canonical on-chain state exists. Certificate
signature validation does not establish readiness-certified/not-lost; no such
validator predicate or canonical-state proof is claimed here.

A-STORE supplies immutable, fenced durable acknowledgements and exclusive
non-expiring authority. The model abstracts that supplier instead of extending
the complete KC-VER shared disk/fence kernel; clone/restore, committed-with-lost
reply, process-cache versus host-loss and production fencing qualification are
open composition items. Existing reference storage tooling/vocabulary and
inherited concrete store trace tests are supporting evidence, not a new V-01
proof. A-CRYPTO, A-C04-CURRENT-FINALITY, A-CONFLICT-FINALITY and
A-K04-AGREED-EXECUTOR-CONFIG remain owned assumptions in SCOPE.md. The real fixture
is controlled test value. Authority helper validation is not funded reservation
accounting. The 30-second independent persistence context is cooperative;
actual I/O failure or process death before Done can preserve possible release
without preserving the exact signature. No stronger cancellation guarantee is claimed.

## Review response, failures and freshness

Popper's independent K-04 adversarial review and independent fix-response review
are separately pinned. This agent's regressions fail on original
`4e73838af5fd1a97ef8b047616cfbd9eb01e9824`: caller cancellation loses returned
Done and Done-only recovery reports missing. They pass on the parent-owned
recovery fix. No parent production repair was independently edited here.
Fixed binary encoding, missing authorization refusal, durable receipts and
real-store post-claim guard ordering were independently checked in this package.

Verification-agent self-review is separate from that independent review. Retained
historical variants document receipt-anchor correction, precise isolated controls,
terminal state quotient, mutex ownership, inclusive admission/active serialization,
and an aborted resource-limited run. The latter observed 17,546,199,040 bytes RSS,
was killed after 139.498 seconds, and has no completed state-count claim. Failed
synthetic launch fixtures and wrong dependency/toolchain diagnostics remain setup
failures, not code findings. Missing historical Core was a tool diagnostic; the
hash-matched official 29.0 archive restored it before the successful fixture.

The final evidence audit additionally caught and repaired two **harness/model
observer defects**: admission comparison skipped offered-action membership, and
a final-guard projection mapped only timeout while the discovered trace reached
confirmed cancellation/refund. Corrected traces preserve first divergence,
trace-selected attempt and every non-Reserved state. The final model was rerun
for the extraction correction; final normal/race replay was rerun for the
projection correction. Prior failed evidence is retained, not overwritten.

The replacement extension also retains an intentionally interrupted setup run
(exit -9, 127.262019375 seconds; no completed claim credited): self-review moved
a finalized-block immutability check from host attestation to admission. Initial
replacement preflights had weaker witnesses; the final witness observer requires
originally admissible live issuance at counter+1/current version, not an already
expired or final-block-closed statement. No stronger protocol guard was invented
for that witness correction. The affected admission profile was rerun after each
correction; unchanged primary signing transitions did not require repetition.

`audit-evidence.py` verifies discoveries, all first-divergence markers, parsed
subprocess observations, classifications, model hashes and concrete input hashes.
`check-freshness.py` accepts the current manifest and rejects a separate copied
manifest with exactly one changed executor hash. No production mutation is used.
Final pins cover source, encoding/vectors, model, harness, dependency and fixture
inputs; documentary C-04 snapshots are identified as observations of a moving
parent-owned subject. Historical run-manifest hashes stay historical; final
documentation/evidence metadata is repinned without pretending to rerun a model
for prose changes. `SHA256SUMS` seals the retained evidence, including old runs.

The authorized verification milestone is complete. Full C-04 consumer replay,
shared-storage-kernel composition, funded accounting, K-05 agreement/partial-input
resolution, production fencing/storage class and cryptographic contract proof
remain separate owned gates. No production mutation campaign, push, PR, merge,
dependency upgrade or production activation was performed.

## Renewal amendment and independent V05 nit review

Normative source is now `5c595620d2ec1e2a73ebd811c937c695d162a35d`. The rebase changed only
INTENT-V2.md and its source-hash manifest; all executable/vector bytes remain
identical. The requirement now includes **every outstanding renewal deadline**,
not just the original certificate. The prior model had no renewal action; its
replacement result could not establish that broader requirement. No such
coverage was inferred from a source repin.

The affected admission profile was extended and rerun with a frozen **one-renewal**
bound: two intent identities and three certificate variants, original intent
certificate deadline 1, same-intent renewal deadline 2, different-intent
replacement certificate deadline 3. One renewal and one replacement are allowed
for the wallet across the bounded run. Renewal history remains retained and
invalidates older absence observations. Replacement requires expiry past the
maximum original/renewal deadline, finalization of those deadlines, and
authenticated canonical absence at/before them. Admitted expiry still never
releases or replaces a reservation. The new positive witnesses
W-outstanding-renewal-blocks-replacement and
W-renewal-deadline-finalized-before-replacement-admission were found;
IgnoreOutstandingRenewal produced its named counterexample.
Expiry witnesses explicitly require a time strictly beyond the admitted
certificate's recorded deadline; equality is not called expiry.

This establishes only the finite, fully tracked **single-renewal** abstraction.
Arbitrary renewal histories, omitted/unknown remote renewals, actual host
attestation storage, threshold certificate aggregation, authenticated chain
absence production and Bridge consumer enforcement remain unqualified. The
producer must provide complete renewal history and canonical finalized absence;
the model does not prove those suppliers. N1/N2 and real C04/K05 replay gates
remain as described above. No Go, primary signing transition or concrete
harness changed; their accepted runs were retained and source-checked rather
than unnecessarily repeated.

Independently reviewed V05 checker/test nit delta read-only. **No actionable
finding** in the reviewed scope-ID/exit-code changes. Ten tests passed with
zero failures/errors, plus eight additional CLI probes: valid closed 0,
wrong row/obligation scope 3, strict-open 2, source drift 1, invalid scope plus
drift 3. Scope-ID binding is lexical; it does not establish semantic relevance
or discharge any composition obligation. Actual working-tree hashes and probe
outputs are retained in evidence/V-04-2026-10-13/v05-nit-independent-review/
REVIEW.md, actual.json, tests.log and probes.json. No V05 file was edited.
