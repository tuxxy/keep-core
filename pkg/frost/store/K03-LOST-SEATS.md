# K-03 component: terminal local-seat status

Historical component record. The complete local milestone now includes public
approval, fresh-worker reload, readiness and expiry; see
[`RESULTS-2026-10-07.md`](../../../test/frost/RESULTS-2026-10-07.md).
The component counts and open-work list below describe the earlier checkpoint.

This change implements the local lost-seat report required by KC-SF-V3 revision
01, section 4.3. It supports SC-03 and SC-08. It does not complete K-03's public
approval, finality, readiness certificate or wallet expiry path.

## Status contract

`Journal.DKGStatus` returns the deployment and epoch domain, the retained attempt
ID, original local seat IDs, a state, and an explicit lost-seat list. The caller
must supply the original domain through `Store.Scope`. This is not a discovery
API for every wallet on disk.

| Durable and live facts | Result |
| --- | --- |
| No DKG claim for the domain | `ErrMissing`; no seat assertion |
| Claim exists, its live owner is present, no key is saved | `InProgress`; no lost seats |
| Claim exists, no live owner, no key is saved | `SeatsLost`; every original local seat is lost |
| A completed key is saved | `KeyStored`; no lost seats |
| Store, fence or retained request is uncertain/invalid | Error; no seat assertion |

`KeyStored` is local metadata status. It is not a readiness attestation. The
operator must still reload the completed records in a fresh worker, and the
future C-03 path must bind and verify its certificate before readiness counts.

## Crash behavior

The marker is derived from the existing immutable, encrypted DKG request and
saved key. It does not need a final write by a dying process. Live ownership
exists only in memory and is never restored on open. The guarded engine ends
the worker operation before it releases its claim. A caller of `Claim` must
preserve that order. Release removes only live ownership. It retains the attempt,
domain reservation, opaque records and locks.

The first `SaveKey` now requires that domain's live DKG claim. It checks epoch,
profile, full roster, threshold, the number of local references, and nonempty
reference bytes. It does not decode those references or cryptographic records;
the checked client and later fresh-worker reload establish their contents.
A late save after release or reopen returns `ErrDKGLost`. Identical already-saved
keys remain idempotent. A different saved key remains a conflict.

There is no snapshot-format or worker-protocol change. The store reads the
typed host request already written by the guarded engine. An older arbitrary
test-only claim without that request cannot produce a seat assertion; querying
it quarantines the handle instead of inventing seat identities. Initial key
insertion without a live claim is no longer supported.

If a key write has an uncertain result, the current handle stays quarantined.
Reopen uses the independent fence to determine whether that exact key snapshot
committed. A committed key reports `KeyStored`; an earlier committed claim with
no key reports `SeatsLost`. The test authority is not a production fence service.

## Verification

- `TestDKGLostSeatsSurviveReopen`: sparse original seat IDs, terminal loss,
  immutable records and locks, no ID/epoch reuse, and fresh-epoch success.
- `TestDKGKeyMustMatchClaim`: changed group/profile/epoch and missing, extra or
  empty local references cannot reach durable key state.
- `TestDKGStatusRejectsInvalidDurableIntent`: malformed group and domain facts
  cannot become a seat report.
- `TestDKGStatusConcurrentRelease`: readers and claim release agree under the
  race detector.
- `TestDKGKeyWriteUncertainty`: fence advancement determines the result after
  reopening; the uncertain live handle makes no assertion.
- `TestDKGHostCrashReportsSeats`: a killed host at 13 explicit boundaries from
  claim through key acknowledgement. Each subprocess confirms the intended
  boundary before death. These are real local filesystem calls, not power-loss
  qualification for a production storage class.
- `TestCrashedDKGNeedsFreshEpoch`: the real worker's candidate-boundary crash
  reports all three local seats lost before and after store reopen; a fresh
  epoch completes, reloads and produces an independently verified signature.

The complete FROST suite passes normally and with the race detector: 141 case
and subtest passes each, no failures or skips. The final focused store race run
passes 90 cases after adding the subprocess-boundary witness. The affected
ECDSA, tBTC and Bitcoin packages pass 620 cases, and inactive configuration
checks pass 3, with no failures or skips. FROST static checks pass.

A source-overlay regression control that removes only the live-owner check
fails at the intended late-key assertion. It does not edit production files or
add a mutation campaign. The interrupted legacy run is retained as incomplete;
the figures above use the completed rerun after the owner resumed work.

Exact commands, source hashes and machine-readable logs are retained in the
local integration evidence directory `K-03/lost-seats-2026-10-07`.

## Remaining K-03 work

Public C-01/C-02/C-03 contracts and their frozen profile constants, result
publication/challenges, approval finality and reorg checks, typed wallet loading,
fresh-worker readiness collection, deadline-edge expiry, and full real-contract
lost-seat scenarios remain open. This component sends no chain transaction,
enables no production dispatch and grants no funding or deployment gate.

The separate K-02 verification PR checks a model and source pinned before this
change. Its results do not automatically prove these new status or `SaveKey`
guards. No Stateright or other formal campaign was run for this component.
