# K-04 adversarial review response — 13 October 2026

Independent review subject: `4e73838af`. Two confirmed medium recovery defects
were found; no wrong-spend, nonce-reuse or guarded-grant bypass was confirmed.
The original review and probe evidence are retained in the local integration
package at `K-04/adversarial-review-2026-10-13/`.

| Finding | Change | Regression evidence |
| --- | --- | --- |
| Cancellation after successful signing could prevent retention | Verify and retain returned completion before cleanup, using a 30-second context independent of caller cancellation; preserve fence and immutable-write checks | Actual encrypted store; cancellation at engine return and cleanup; completion survives and timeout stays quarantined |
| Crash after per-input retention could prevent aggregate recovery | On missing aggregate, verify original durable intent/receipt and all original-context Done records; reconstruct and retain the aggregate without another worker grant | Actual store close/reopen; one-input full recovery; two-input partial refusal/full recovery; original claim remains terminal |

The full signing package passes normally and under the race detector. Store and
engine tests pass; signing vet passes. The changed real-worker integration test
passes with actual K-03 contracts, libp2p, two inputs and Bitcoin Core 29.0 regtest,
including the negative case that refuses a worker launch after the durable claim.
Existing worker/Bitcoin/contract/dependency versions are unchanged.

The initial real-worker attempt failed because its historical temporary Core
binary was absent. The official archive was restored and checked against the
existing pinned archive SHA-256 `34431c582a0399dd42e1276d87d25306cbdde0217f6744bd55a2945986645dda`;
the executable matches `f4b9d78a74643c726d022840ca506dc317d41f8825aaef80b7bb705e12c5fda8`.
The successful rerun establishes the stated fixture result. Sandbox process
inspection failures in engine tests were resolved by the approved local rerun;
they are not counted as successful tests.

No mutation campaign was run. V-04 must pin this revised recovery path, model
cancellation after worker return and crashes before aggregate acknowledgement,
and replay its boundaries. Full C-04 replay, partial-input resolution and operator
agreement remain K-05 requirements. Independent fix re-review remains required
before milestone exit. No formal safety statement or production gate is claimed.
