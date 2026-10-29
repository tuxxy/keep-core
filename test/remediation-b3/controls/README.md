# Batch-3 controls (base binding)

Run in a scratch worktree only (`RM-tests-b3/keep-core-ctl`, detached, never pushed):
`run-control.sh <scratch> <patch> <log> <package> <-run regex>`. It copies the batch-3 test files in, applies the patch, runs, and reverses the patch. `PARALLEL` sets `-parallel` (default 4). Logs: `RM-tests-b3/logs/controls/`.

The tests call only functions that exist on the integration branch, so there is no entry gate to stub: on the integration code every test runs to its own assertion (see TESTS-BATCH3.md). The integration code is therefore the trivial-implementation control.

| Patch | Kind | Result |
| --- | --- | --- |
| integration code as is | trivial implementation | `SignsWithOperatorDown` 3/3 fail at `incoming duty stalled …`; `ResolveRecordsOnlyTheProvenSweep` at `settlement records the node's own plan …`; the three `FrostIncomingSweep` cases at `no moved funds sweep proposal for the FROST target's pending incoming output (got *tbtc.NoopProposal, <nil>)` and `a follower refused a leader proposal with fee 1 within the on-chain cap 100000 …`; `PossibleReleaseAfterGrantWithoutDone` 4/4 at its assertion |
| `b3-66-threshold-window.patch`: the Mover signs with a threshold-size window of live seats that moves with the round | positive (#66) | `SignsWithOperatorDown` 3/3 pass (twice at `-parallel 4`; at `-parallel 8` two cases overran the 1 s window bound once), and the guards still pass |
| `b3-45-retain-plan.patch`: the Planner keeps its first incoming plan for good (K09-29) | removal (#45, #61 guards) | `NextWindowAfterFailedWindow` and `AfterMainSpent` fail at `incoming duty stalled: 1 incoming outputs still pending after 6 windows`; `Settles` still passes |
| `b3-71-proven-only.patch`: Resolve settles a plan only when its own transaction is proven | positive (#71) | `ResolveRecordsOnlyTheProvenSweep` and `Settles` pass |
| `b3-D19-accept-all.patch`: the follower validation accepts everything | wrong fix (D19) | `FollowerValidation` fails at `a follower accepted a proposal with fee above the on-chain cap`, `… zero fee`, `… wrong output index`, `… unrecorded output` |
| `b3-50-grant-marker.patch`: a durable marker in BeforeSigning, read by PossibleRelease | positive (#50) | 4/4 pass and the guard passes. It also breaks the existing `TestV04ProcessBoundaryReplay` (`unknown public signing record`): V-04's replay classifies every signing record |
| `b3-50-always-released.patch` | wrong fix (#50) | `NoPossibleReleaseBeforeTheWorker` 2/2 fail at `no possible release: nothing reached the worker, but PossibleRelease reports true` |
| `//go:build frosttest` on an owning file | removal (#64) | `owning test … does not run in a default untagged go test` |

No positive control exists for the coordination side (`FrostIncomingSweep*`): it needs the FROST routing in the proposal task and the chain adapter, which is the work this batch specifies.
