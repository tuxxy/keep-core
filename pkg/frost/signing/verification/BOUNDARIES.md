# K-04 / V-04 predicate and replay boundary contract

Recorded before K-04 implementation on 7 October 2026. Base: keep-core
`0b4f169ab4327a747d04531ded1ada13edc99d6d`. Governing KC-SF-V3 revision 01;
KC-VER revision 01 at specification repository `d45afdab9108cbd0156ad68cc5697ece737a7b1c`.
This is an implementation contract for verification review. It is not a model result.

## Predicate

P-K04-AUTHORIZED-ONLY: the host grants a signing operation only for the exact
locally built immutable transaction, complete validated prevouts, wallet Q,
network, purpose, requests, state version, generation and fee policy named by
a finalized reservation. Missing, stale, changed or conflicting observations
refuse before worker startup. Done messages must bind that same authorization,
input index, attempt and digest, and carry a valid BIP340 signature under Q.

P-K05-TERMINAL-ATTEMPT is reused as a caller obligation: the guarded engine
claims before worker startup, ends the worker before releasing live ownership,
and retains its claim forever. The claim must retain the public authorization
and exact unsigned transaction so a possible signature cannot be forgotten.
K-05/C-04 own reservation conflict resolution, timeout quarantine, settlement
and cancellation; K-04 cannot establish P-K05-NO-DOUBLE-OUTCOME by itself.

## Implemented boundaries and controlled providers

The signing executor has a test-only `boundary func(string) error`, nil in
normal use. Boundaries are `transaction-frozen`, `before-reservation-read`,
`reservation-seen`, `before-intent-record`, `before-worker-start`,
`after-signature`, `before-done-accept`, and `before-broadcast`. The guarded
engine's existing journal claim and store commit hooks expose the I/O stages.
A provider can return a missing, unfinalized, changed or timed-out reservation;
a Bitcoin provider can change or omit any prevout. Test adapters are local only.
The final reservation/purpose read runs after the durable claim and immediately
before the worker receives its grant. Every input gets a distinct agreed attempt.

## Correspondence and ownership

`pkg/bitcoin` freezes bytes and computes default key-path sighashes; the signing
executor checks purpose and reservation; the guarded engine retains its exact
public intent and calls the last authorization check before begin; typed done
handling validates context before accepting a signature; the transaction is
script-checked again before broadcast. A callback returning success is a trusted
provider boundary, not proof of finality. C-04 must later discharge that assumption
with its real finalized reservation API. Local tests use an explicit controlled
reservation and test-value Bitcoin Core regtest; production dispatch stays off.

Implementation owner: this K-04 task. V-04 model owner and hook review: pending
coordination with the verification lead. Model checking gates merge under
KC-VER; full V-04 replay with C-04 gates the K-05 exit. Bounds, witnesses,
negative controls and execution limits must be recorded before any model run.
No new SNOWFALL proof or worker change is included.

## Concrete correspondence for V-04 review

| Boundary | Concrete observation | Required model distinction |
| --- | --- | --- |
| `transaction-frozen` | `Plan` copies unsigned bytes, ordered prevouts, policy and descriptor commitment. | Exact authorized plan versus a different plan; do not equate authorization with signature validity. |
| `before-reservation-read` / `reservation-seen` | Provider read, finality/state/context checks, pinned receipt. | Missing, unfinalized, canonical, orphaned, changed generation and timeout states. |
| `before-intent-record` | `RetainSigningRecord` uses the existing encrypted, fenced immutable claim path. | Acknowledged durable intent versus an uncertain write. |
| engine `Claim` | The complete `SigningRequest` contains the exact public authorization and digest. | Claimed attempts remain terminal on callback or worker failure. |
| `before-worker-start` | Final callback runs after claim and rechecks inputs and reservation. | No grant before all checks; provider state can change between reads. |
| `after-signature` / `before-done-accept` | Per-input signature is retained, then reservation is checked again. | Released/possibly released signature cannot be forgotten after timeout. |
| `before-broadcast` | Private result copy, exact context, script/witness verification, reservation reread. | Signed bytes, broadcast and settlement are separate events. |

Tests exercise these boundaries with controlled reservations and Bitcoin views.
`TestSigningAuthorizationAfterDurableClaim` observes the real adapter ordering.
The real integration fixture observes each durable worker grant and compares its
intent, digest and attempt with the immutable plan and verified result. No opaque
worker record is decoded. `store.go` and its fencing commit protocol are unchanged;
new application records use `pkg/frost/store/signing.go`.

Suggested V-04 negative controls are: omit the exact-plan match, omit the final
pre-worker check, omit the terminal claim, accept foreign completion context,
and allow a late signature into the broadcast path. Each must break its declared
predicate. This is a handoff proposal, not a completed model campaign. The model
owner must freeze finite bounds, witnesses, assumptions and resource limits,
review the transition map, and pin the source manifest before the model run.
Full C-04 reservation lifecycle replay is still a K-05 exit requirement.

## Fable review decisions, before the V-04 freeze

F1: exact plan identity is the SHA-256 of the version-2 binary contract in
`../INTENT-V2.md`. Go and independent TypeScript implementations match fixed
vectors, including wide uint64 values. JSON is not the reservation wire format.
Pin this encoding and both vectors in V-04. A version change invalidates the
plan-equality correspondence and requires rechecking it.

F2: guarded `Engine.Sign` now requires both a nonempty intent and a callback
before any claim. There is no guarded bypass option. The lower transport tests
supply explicit raw-digest test grants; they are not purpose-authorizing callers.
The original unguarded `New` constructor remains the named K-01 local harness;
it is outside guarded production use. Model a missing grant/callback as refusal,
not as an assumption that all callers remember to supply one. Add the
`EmptyAuthorizationStartsWorker` negative control.

F3: the first finalized receipt is an immutable, encrypted, fenced application
record keyed by plan plus reservation. It is written before worker authorization.
Results include the receipt. Fresh executors must load it and refuse a changed
block/hash; broadcast cannot recreate a missing receipt. Model this durable
anchor and crashes around its acknowledgement. Do not assume C-04 reservation
IDs are immutable to discharge this property.

F6: the real integration test flips reservation state only after the real store
acknowledges the real engine's claim. The guard refuses, the claim stays terminal,
and the per-test launch marker remains absent. The same launcher then executes
the hash-checked real worker for the positive control. Removing the final guard
must fail this real-provider test, not only a stub test.

### Named coordination assumption: A-K04-AGREED-EXECUTOR-CONFIG

Owner/deliverable: K-05 coordination and signing implementation leads. The K-04
fixture supplies one agreed session, start block, selected-seat set and executor
configuration (plan, reservation, domain, chain/finality policy). K-04 implements
no cross-operator protocol to agree them. V-04 must label that as a controlled
input assumption until K-05 implements and tests the agreement. Carry it into
the V-05 ledger. Production routing cannot close this assumption by copying the
fixture's constants. No FROST call enters legacy ECDSA coordination/done code.
