# K-03 review fixes — 7 October 2026

The expiry defect and requested test gaps are fixed. A clean public expiry now
returns `ErrExpired` and saves `Closed` without quarantine. The independent
review's former K-04 test follow-ups are included in this K-03 milestone.

## Behavior change

Competing readiness/expiry calls can revert before the winning transaction is
visible. The executor now re-reads at most five times within one second. It
checks the same immutable approval throughout. `Closed` means expiry; a
`ReadyUnfunded` outcome with the same approval means another readiness call succeeded.
Changed approval or unresolved state remains quarantined. It never retries a
worker command or resubmits the failed transaction.

Every failure path after protocol startup joins the monitor and receiver and
drains their errors. A classified expiry takes precedence over cancellation
or a losing transaction error. An approval conflict still takes precedence
over expiry. The final journal write therefore uses the classified outcome.

The strengthened deadline test failed on baseline `295af33335acc91fba951be9d1357540825b3675`
with a canceled RPC. It passes after the fix. Both deadline equality and live
survivors of a lost seat now assert `ErrExpired`, durable `Closed`, and no
quarantine. The killed node's earlier failure remains distinct.

## Stronger tests

- The reorg occurs three seconds after first approval observation, on the
  fixture context. Every executor exits before the test inspects any journal.
  No key may exist, and each retained candidate must be quarantined.
- Registry mutations are re-signed over the changed payload by the correct
  operators. The canonical-x case uses p+1, with a valid x=1 positive control,
  so the field bound is the only rejecting point predicate.
- The legacy invalid-point case has a distinct, unused label. The readiness
  test includes a recoverable, domain-correct signature by the wrong operator.
- Cross-scheme label occupancy is checked in both directions. Because a hash
  collision cannot practically be generated, the test forces only the occupied
  state using compiler storage layout. It checks the actual callback rejects
  without changing that state or live count, then removes the injection and
  requires the identical input to succeed. This is a synthetic collision control.
- Both registry and Bridge preserve Q reservation across epochs and after
  unfunded expiry. A valid result cannot approve before its challenge window.
- A correctly signed conflicting candidate quarantines the local DKG. The
  authenticated bus rejects a seat claimed by another sender; the collector
  separately rejects a wrong-operator signature.
- Host and client reject a subset of local reload seats. On the host, rejection
  leaves the claim available for a successful full-set reload in a fresh worker.

## Verification

| Check | Result |
| --- | --- |
| Complete FROST host suite | 246 normal; 246 race; no failures or skips |
| Complete Snowfall client suite, real worker and codec | 188 normal; 188 race; no failures or skips |
| Registry/readiness contract tests | Four passing |
| Bridge identity, collision, expiry and populated upgrade tests | Five passing |
| Go vet, host and client | Pass |
| TypeScript lint on changed tests | Zero errors; Bridge has 29 retained warnings |
| Selected retained mutation controls | 12/12 killed by test assertions |

Mutation IDs: G16, S01, R01, R02, R12, R08b, R08, R09, R14, T02, T03 and T10,
all with prefix `K03-`. This includes all six controls named in the feedback.
The sites come from the retained review runner. Some judges now name the new
focused tests. Go uses source overlays; Solidity sources are restored and
recompiled after each control. The final generated binding matches the restored
contracts. No compile failure, timeout or skipped baseline counts as a kill.

For the full independent rerun, update the optional-case judges so the retained
runner includes the new tests:

| Mutant | Focused judge |
| --- | --- |
| G17 | `TestSignedConflictingResultQuarantinesAttempt` |
| G26 | `TestReloadRequiresEveryDurableLocalSeat` |
| G36 | `TestCollectRejectsWrongOperatorSignature` in `pkg/frost/dkg` |
| R09 | `TestRegistryRejectsDuplicateOutputKeyAcrossEpochs` |
| R14 | `TestApprovalRequiresChallengeWindow` |
| T02/T03 | `Frost.Identity.test.ts`, grep `rejects cross-scheme` |

G16/G19 keep `TestApprovalReorg`; S01's existing Reload filter includes the new
subset test. T10's existing identity filter includes the corrected invalid-point
case. The twelve-control script and exact commands are retained with the evidence.

This is a selected rerun, not a new score for all 48 review mutants. Fable's full
48-control rerun is still pending. The earlier full legacy Go and Bridge wallet
results remain historical evidence; those packages/contracts were not changed
by these fixes and were not rerun here. No new formal-model exploration is claimed.

## Source and retained evidence

All changes remain in the existing K-03 milestone PRs: keep-core #8, Snowfall
#48 and tbtc-v2 #1. Each has one topical commit. The previous heads are retained
for comparison; there is no extra component PR.

The tests ran before the final metadata-only amend. The local client replacement
resolves to the same tested source; the final declared peer pin and documentation
were then updated without changing that source.

The matching peer commits are:

- snowfall: `c1847ebdf133d0570643ff968539dd498069f69f`.
- tbtc-v2: `76f79051104a5173becf967eb71cfc448cc765e4`.

`source-sha256.json` identifies all 172 checked source files. Exact test logs,
the pre-fix failure, mutation patches/commands/results, and post-publication
revisions are retained under `implementation/K-03/review-fixes-2026-10-07`.
The initial `RESULTS-2026-10-07.md` remains a historical publication record.

The production gates are unchanged: D-01 distribution, D-02/D-11 group and abort
policy, D-09 runtime/storage/fence/finality assumptions, SF-03 capacity and C-07
primitive qualification. The code remains inactive and unfunded. The extra
tests do not implement K-04 or complete production activation.
