# Batch-3 controls

Run in a scratch worktree only (`RM-tests-b3/keep-core-ctl`, detached, never pushed).
Logs: `RM-tests-b3/logs/controls/`. Tests at `0bafacc5a`.

## Incoming coordination (`pkg/frost/movement`)
`run-control.sh <scratch> <mode> <log> [-run regex]` drops `control.go.txt` into the scratch tree with `controlMode` set. Every mode passes the entry-point check.

| Mode | Kind | Result |
| --- | --- | --- |
| `stub` (seat 1 leads, the leader publishes a placeholder, nothing signed) | trivial entry point | every case fails at its own assertion: `incoming duty stalled …` (SweepSettles 2, OutputFinalBetweenTicks 2, LeaderCrashAtPublish 3, RecipeRetired leader-only, OperatorDown 3), `the incoming leader never rotates`, `TENTATIVE (D2 by seat) …`. The three grant cases (GrantPins, PinnedWaits, RecipeRetired all-granted) stop at their named precondition `operator 0 never reached signing (no grant)`: a stub that never signs cannot create the granted state. `naive` reaches their own assertions. |
| `naive` (existing K-07 Planner + `Mover.Resolve`, every live seat signs) | positive for the guards, removal for the rest | **pass:** SweepSettles 2/2, OutputFinalBetweenTicks 2/2, LeaderCrashAtPublish 3/3 (the harness signs, broadcasts, mines and settles with the real worker). **fail:** GrantPinsRecipe at `a possibly released recipe was replaced: the first output was swept together with an output that became final only after every seat granted the earlier recipe`; PinnedRecipeWaits at `… replaced without chain evidence`; RecipeRetired 2/2 at `operator 0 settlement record names 0000…, want the proven sweep …`; OperatorDown 3/3 at `incoming duty stalled` (every seat must sign); LeaderRotates and the tentative share test. Full run 23 s wall, slowest case 7.3 s. |
| `retain` (naive + first plan kept for good: the K09-29 / K09-41 defect) | removal (#45, #56, #63) | OutputFinalBetweenTicks 2/2 and LeaderCrashAtPublish 3/3 fail at `incoming duty stalled: 2 incoming outputs still pending after 6 rounds`; SweepSettles 2/2 still pass |
| `record-first` (records its first plan as the settlement: K09-58) | removal (#71) | RecipeRetired 2/2 fail at `operator N recorded <hash> as the settled incoming sweep, but no such transaction was proven` |
| `record-proven` (records its last plan only once proven) | positive (#61, #71) | RecipeRetired 2/2 pass |
| `rotate-seat` (leader = live seat `round mod n`) | positive (#66, D2 by seat) | LeaderRotates and LeaderShareBySeat_Tentative pass |
| `rotate-operator` (leader = an operator in turn) | isolation of the tentative part | LeaderRotates passes; only LeaderShareBySeat_Tentative fails (`operator 0 with 30 of 100 seats led 0.200 …`, `the 30-seat operator led 800 rounds, not more than the 10-seat operator's 800`) |

No positive control exists for GrantPinsRecipe, PinnedRecipeWaits or OperatorDown: each needs the real coordinator (pinned recipes, a threshold cohort of live seats), which is the work this batch specifies.

## Possible release (`pkg/frost/signing`, #50)
| Patch | Kind | Result |
| --- | --- | --- |
| integration code as is | removal | `TestPossibleReleaseAfterGrantWithoutDone` 4/4 fail at `possible release: the attempt passed the grant … but PossibleRelease reports false` |
| `b3-50-grant-marker.patch` (a durable marker written in BeforeSigning, read by PossibleRelease) | positive | 4/4 pass and the guard passes. It also breaks the existing `TestV04ProcessBoundaryReplay` (`unknown public signing record`): V-04's replay classifies every signing record, so a fix that adds a record kind must fit it |
| `b3-50-always-released.patch` (PossibleRelease always true) | wrong fix | `TestNoPossibleReleaseBeforeTheWorker` 2/2 fail at `no possible release: nothing reached the worker, but PossibleRelease reports true` |

## #64
Adding `//go:build frosttest` to `possible_release_b3_test.go` fails `TestBatch3OwningTestsRunUntagged` at `owning test …/possible_release_b3_test.go does not run in a default untagged go test`.
