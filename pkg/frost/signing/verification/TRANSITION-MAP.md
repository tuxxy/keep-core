# Abstract transition to concrete source correspondence

| Transition | Concrete source/seam | Limits |
| --- | --- | --- |
| Reserve/finalize/change/re-anchor/tick | `Reservations.ReadReservation`; `Executor.reservation` | Controlled provider only; C-04 real ledger absent. Re-anchor retains the original read anchor across commit stages. |
| Start(mode) | `Executor.Run` first checks; guarded `Engine.Sign` missing grant/callback checks | Matching/foreign/missing/callback are distinct. Lower unguarded K-01 constructor excluded. |
| Receipt stages 1–6 | `RetainSigningRecord(receiptID)` and existing store commit hooks | Immutable public receipt; full disk/fence kernel is a composition assumption. |
| Intent stages 7–12 | `RetainSigningRecord(plan.hash, plan.encoded)` | v2 byte equality pinned separately by independent vectors. |
| Claim stages 13–18 | `Engine.claim` before `BeforeSigning` | Real-store terminal-history and real-worker launch-refusal checks. |
| Grant stage 19 | `finalAuthorization.BeforeSigning`, `before-worker-start` | Last wallet/prevout/reservation read. Provider can change after the read; released signature is possible history, not a revocable lease. |
| Worker return stage 20 | `Engine.Sign` returns valid digest/context signature | Crypto abstraction, separately checked with real worker. |
| CancelCaller | Caller context cancellation after return/cleanup | Original source defect retained as failing regression. Parent owns production repair. |
| Done stages 21–26 | Verified per-input Done retention, `after-signature` | Caller cancellation must not prevent the bounded persistence attempt. Actual I/O uncertainty stays fail closed. |
| Post-done stage 27 | `before-done-accept` and reservation reread | Late signature is retained and quarantined. |
| Aggregate stages 27–32 | exact transaction assembly/result retention | Complete inputs required; result is not Bitcoin settlement. |
| Recover | `LoadResult` joining immutable Done records after aggregate absence | Parent fix required. No signing engine call, no claim recycling. |
| Deliver(foreign) | `Done.Verify`, `ValidateResult`, `Broadcast` | Foreign context and stale reservation refuse even if crypto is valid. |
| Crash/process or host; I/O error | exposed boundaries, store suite, restart regressions | Control-flow interruption + real store reopen is partial replay, not OS kill or power loss. |
| Pay/cancel/refund | abstract C-04 consumer transitions | No Go or Solidity ledger replay claim; K-05 gate. |

## Model controls

ReleaseBeforeFinality, OmitExactPlan, OmitFinalGuard,
EmptyAuthorizationStartsWorker and ForgetDurableReceipt target AUTHORIZED-ONLY.
ReuseAttempt targets TERMINAL-ATTEMPT. RefundOnElapsedTime and
RefundOnCertificateExpiry target NO-DOUBLE-OUTCOME. AllowConcurrentReservations
targets ONE-ACTIVE-CUSTODY-RESERVATION. AcceptLateSignature and ForeignDoneContext target
LATE-SIGNATURE-QUARANTINE. CancelledRetention and AggregateOnlyRecovery target
DURABLE-SIGNATURE-RECOVERY. Counterexamples retain the first divergence from the
claim model; their unsafe results are never expected from current Go.
