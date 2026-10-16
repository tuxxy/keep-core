# Scope, finite bounds and obligations

KC-SF-V3 revision 01 and KC-VER revision 01 govern this checkpoint. Initial
subject is `4e73838af5fd1a97ef8b047616cfbd9eb01e9824`. Parent-owned recovery
repairs require a new pin and all affected reruns before citing current results.
Independent adversarial review is Popper's 2026-10-13 report; verification-agent
model and correspondence review is self-review, not another independent review.

B4: one wallet, two requests, two distinct attempt identities, one shared host
journal authority, three chain ticks. Each attempt has a separate immutable
receipt, intent and claim, then returned signature, completion retention and
aggregate retention. Ten acknowledged commits is the maximum (two attempts,
five record kinds); identical acknowledgements are abstracted. Recovery actions
never obtain a new signing grant. No unbounded liveness claim is made.

Two attempts are abstract worker opportunities; they can serve the same request
(a fresh retry) or different requests. Concrete K-04 multi-input coverage uses
two input-specific attempts on one plan. These abstractions do not establish
batch entitlement accounting; C-04 must join the request set and shared conflict
input in the real ledger. Three ticks is a test deadline, not production policy.

Reservation: absent, unfinalized, finalized, timeout-pending. Exact plan fields
collapse to matching/changed, receipt anchors to two distinguishable values,
and valid/foreign/missing authorization or callback to four grant modes.
Finalization is not signature validity. A grant sets possible-release before
worker return: crash does not erase it. Bitcoin payment can arrive despite host
quarantine. Cancellation makes competing payment impossible under the explicit
same-conflict-input/Bitcoin finality assumption, then permits refund. Elapsed
time alone permits an unreserved refund only at the abstract finality boundary.

## Owned assumptions and exclusions

- A-K04-AGREED-EXECUTOR-CONFIG: K-05 coordination/signing leads own agreement on
  plan, reservation, domain, session, start block, selected seats and policies.
- A-C04-CURRENT-FINALITY: C-04/K-05 adapter owners supply an internally consistent
  canonical head, certificate/request validation and finality. The helper
  FrostReservationCertificate is authority validation only. No real reservation
  ledger, funded-contract witness or contract accounting result is claimed.
- A-CONFLICT-FINALITY: Bitcoin/contract owners supply sound inclusion proofs,
  one consumed shared conflict input and custody-preserving cancellation.
  Those rules are abstract guards awaiting C-04 and Bitcoin/contract checks.
- A-CRYPTO: correct SNOWFALL nonce discipline and BIP340; the model does not
  decode secret records or prove the worker. Signature validity is tested at
  concrete seams and with the hash-checked real worker/Core fixture.
- A-STORE: independent non-expiring fencing, sync semantics, exclusive ownership
  and immutable terminal history are supplied by the K-02 store boundary. Each
  application commit has six offered stages (write, file sync, install, directory
  sync, fence, acknowledgement). This component model exposes interruption at
  each stage but **does not duplicate the full durable/volatile disk kernel**,
  corruption, clone/restore, uncertain fence replies or process-cache recovery.
  Its pre-ack effects are conservatively unavailable for authorizing a grant;
  the existing store trace suite is concrete supporting evidence, not a new
  V-01 model qualification. Composition with a freshly pinned V-01/V-02 kernel
  remains open. Unsupported stages remain replay limitations under KC-VER.
- Real filesystem power loss, production fencing, deployed correspondence,
  production policy and contract settlement remain outside this checkpoint.

No production mutation campaign was run. Negative controls change only the
independent model. Historical defective-source regression tests run unchanged
production source; they do not mutate it.

## Additional recovery predicate

P-K04-DURABLE-SIGNATURE-RECOVERY is the local recovery obligation exposed by the
independent review: caller cancellation after a valid worker return cannot by
itself skip retention; acknowledged complete per-input records can reconstruct
an exact result after restart without signing again. A process death before
retention or an actual I/O failure can still leave only possible-release intent;
that uncertainty must remain pending. Partial input sets never authorize a
complete transaction. Bounded persistence duration is a concrete provider limit,
not a guarantee that an unreliable filesystem will acknowledge a write.

## Model self-review and retained attempts

The first component model passed at 4,060,291 unique states. Self-review tightened
its fresh-after-failure witness to require a durably claimed failure and the same
request, then corrected receipt retention to preserve the read anchor rather
than reading a newer anchor at acknowledgement. Earlier outputs remain under
`initial-model`, `before-anchor-correction` and `before-recovery-extension` and
are historical, not the final result.

The user requested the independently confirmed post-worker recovery cases.
The first expanded run exceeded the memory ceiling: 17,546,199,040 bytes observed
at the 250 ms monitor, killed after 139.498 s. It is blocked and has no completed
state count. The second expanded run quotients terminal program-counter values
and dead pre-claim bindings; no offered continuation uses those values. Every
durable fact and later delivery/recovery context remains. Limits and identity
bounds are unchanged. This reduction is verification-agent self-review.

The initial real-store launch-control test had malformed/missing synthetic
reference records; it could not reach startup in its positive control. Both
failed logs are retained. Public synthetic references and non-secret record
payloads repair that oracle. No real secret-bearing record is decoded. The
same launcher then starts in the positive control; the refusal path and
post-reopen claim rejection pass. The independent real-worker/Core fixture
also runs; neither is conflated with full model trace replay.

## Final application replay and admission extension

Twelve declared subprocess application-boundary traces now exercise the actual
executor, real encrypted store and separately durable test fence. Four traces
kill a process after receipt, worker return, Done acknowledgement or aggregate
acknowledgement. Each continuation/reopen compares public record and signature
context facts. The crypto seam returns valid BIP340 signatures; worker IPC is
covered separately by the pinned real-worker/Core witness. These complete
application-boundary traces do not relabel generated stage-by-stage model
projections as full storage-kernel replay or claim executed host power loss.

See ADMISSION-SEMANTICS.md for the user's clarified uint64 intent counter,
state-version, tuple lock, certificate deadline and reservation-ID join. Its
separate model checks proposed consumer/host-attestation rules only. No actual
C-04 funded ledger or host certificate tuple lock exists in the K-04 subject.
The Go v2 intent bytes and vectors are unchanged. Certificate admission is inclusive at its deadline; historical admission
validation and current reservation state are distinct.

The normative source is now d956fb1c (documentation/hash metadata only above the
05032bb6b executor bytes). The primary model serializes one active custody
reservation, keeps paid/cancelled/refunded states terminal, retains certificate
deadlines as historical-admission context and includes a signature-after-expiry
witness. RefundOnCertificateExpiry breaks NO-DOUBLE-OUTCOME; allowing a second
active reservation breaks the new abstract serialization predicate. These are
consumer requirements, not guards implemented in the current Go/Bridge path.

Final self-review also corrected the journal mutex abstraction: two waiting
writers must not mutually block before either acquires the mutex. Ownership now
spans one six-stage commit; it is released on acknowledgement or interruption.
Generated counterexample traces mark the first differing claim-model outcome,
not only the first unavailable action. Earlier variants remain historical.

## N1 replacement and N2 assumption amendment

The frozen N1 extension permits one different complete statement after an
expired-unadmitted certificate only with finalized inclusive deadline plus
authenticated canonical absence of admission at/before it; prior history is
retained. Admitted expiry never permits replacement. Signing requires durable
admission in the primary model. Bounds/witnesses/controls are frozen in
replacement-scope-freeze.json before execution. N2 readiness-certified/not-lost
is A-HOST-READINESS-NOT-LOST, owned by honest-host/K05 readiness implementers
until canonical state exists, and is not a certificate validator predicate.

## Renewal amendment bound

5c595620d changes normative documentation/hash metadata only. Old profile had no
renewal action. Frozen renewal-scope-freeze.json adds one same-intent renewal
(1 -> 2), replacement certificate deadline 3, two intents/three certificate
variants, three ticks, one renewal and one replacement. Every outstanding modeled
deadline must be finalized with canonical absence before replacement. Producer
completeness of renewal history is an owned C04/K05 assumption; arbitrary or
unobserved renewal sequences are not covered. Only admission.rs was affected
and rerun; primary signing/Go source and their accepted tests are unchanged.
