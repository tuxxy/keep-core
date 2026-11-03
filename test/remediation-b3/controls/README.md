# Batch-3 controls (base binding, after review REVIEW-TEST-B3)

Run in a scratch worktree only (`RM-tests-b3/keep-core-ctl`, detached, never pushed):
`run-control.sh <scratch> <patch> <log> <package> <-run regex>`. It copies the batch-3 test files in, applies the patch, runs, and reverses the patch. `PARALLEL` sets `-parallel` (default 4); the runner's `-parallel 4` and `GOMAXPROCS 4` per package are part of the timing contract. Logs: `RM-tests-b3/logs/controls/`.

The tests call only functions that exist on the integration branch, so there is no entry gate to stub: on the integration code every test runs to its own assertion (see TESTS-BATCH3.md). The integration code is therefore the trivial-implementation control.

| Patch | Kind | Result |
| --- | --- | --- |
| integration code as is | trivial implementation | every failing case fails at its own assertion (TESTS-BATCH3.md, "Cases and now") |
| `b3-50-in-memory`: a release flag kept in a package map (review R1) | wrong fix (#50) | the two same-process cases pass; `mainless-after-restart` and `with-main-after-restart` fail at `possible release: … PossibleRelease reports false` (checked in a fresh process) |
| `b3-50-intent-record`: the intent record retained before any grant counts (the reviewer's fix) | positive (#50) | 4/4 pass, the guard passes, `TestV04ProcessBoundaryReplay` passes |
| `b3-50-grant-marker`: a new marker record | positive (#50) | 4/4 and the guard pass, but it breaks `TestV04ProcessBoundaryReplay` (`unknown public signing record`) |
| `b3-50-always-released` | wrong fix (#50) | `NoPossibleReleaseBeforeTheWorker` 2/2 fail at `no possible release: nothing reached the worker, but PossibleRelease reports true` |
| `b3-45-process-retain`: the Planner keeps its first plan per `View` for the life of the process (review R2) | wrong fix (#45, #61) | the restart guards pass; `NextWindowAfterAbortedWindow` and `AfterMainSpentNoRestart` fail at `incoming duty stalled: 1 incoming outputs still pending after 6 windows` |
| `b3-45-retain-plan`: the Planner keeps its first plan for good | removal (#45, #61) | all four guards fail at `incoming duty stalled …`; `Settles` passes |
| `b3-66-threshold-window`: a threshold-size window of live seats that moves with the round | positive (#66) | `SignsWithOperatorDown` 3/3 pass (5.2–9.5 s); every guard passes |
| `b3-71-proven-only`: Resolve settles a plan only when its own transaction is proven | positive (#71) | `ResolveRecordsOnlyTheProvenSweep` passes |
| `b3-71-record-nothing`: Resolve reports settled and records no outcome (review R4) | wrong fix (#71) | fails at `Resolve reports the plan settled but records no outcome naming the proven transaction` |
| `b3-D19-frost-validate-owned`: a FROST-routed follower validator (record Pending, owned by the label's wallet, fee in (0, cap]) | positive (D19) | `FollowerValidation` passes |
| `b3-D19-frost-validate-unowned`: the same without the ownership check (review R3) | wrong fix (D19) | fails at `a follower accepted a proposal sweeping another wallet's incoming output` |
| `b3-D19-accept-all`: the follower validates nothing | wrong fix (D19) | fails at `a follower accepted a proposal with fee above the on-chain cap`, `… zero fee`, `… wrong output index`, `… unrecorded output`, `… sweeping another wallet's incoming output` |
| `//go:build frosttest` on an owning file | removal (#64) | `owning test … does not run in a default untagged go test` |

No positive control exists for proposal generation (`ProposedEveryWindow`, `DivergentLocalFees`): it needs the FROST routing in `tbtcpg.MovedFundsSweepTask` (review R9).
