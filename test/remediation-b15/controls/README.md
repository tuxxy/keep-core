# Batch-15 controls (tuxxy/keep-core#141, D30)

Run in a scratch worktree only (`B15-tests/keep-core-ctl`, detached at `de1e99f0`, never pushed), with the runner's `LEGACY_*` environment:
`run-control.sh <scratch> <patch or none> <log>`. It copies `pkg/tbtc/frost_movement_terminated_b15_test.go` in, applies the patch to production code, runs both batch-15 tests, then reverses the patch and removes the copy.

Every patch touches only the source-state check of `MovingFundsTask.runFrost` (`pkg/tbtcpg/frost_moving_funds.go`). Followers have no state check of their own (`movement.Planner.Movement`; `movement.Authority` accepts MovingFunds or Terminated), so the fix is on the leader.

| Patch | Kind | Expected result |
| --- | --- | --- |
| `none` (integration code) | trivial implementation | `Proposed/terminated` and `/terminated-committed` fail at `D30 (tuxxy/keep-core#141): window with leader <op> (code3-0x), <start> source: no movement proposal for the source (got *tbtc.NoopProposal, <nil>)`; `Proposed/moving-funds`, `Refused/live`, `Refused/terminated-dust` pass |
| `b15-fix`: MovingFunds, or Terminated without recorded dust | positive | all five pass |
| `b15-any-state`: no source-state check | wrong fix | `Refused/live` and `Refused/terminated-dust` fail at `... <start> source: proposal generation failed: ...` (the leader tries to commit what the Bridge refuses) |
| `b15-terminated-ignores-dust`: MovingFunds or Terminated, dust not checked | wrong fix ("Terminated without the commitment-state check") | `Refused/terminated-dust` fails at `... terminated-dust source: proposal generation failed: ... error while submitting moving funds commitment ...` |
| `b15-terminated-only`: Terminated replaces MovingFunds | wrong fix (guard) | `Proposed/moving-funds` fails at `... moving-funds source: no movement proposal for the source (got *tbtc.NoopProposal, <nil>)` |

Not a control: skipping `ProvenAt` for a Terminated source is equivalent in every reachable state. A proven movement leaves custody 0 (FrostMovement.prove), so the leader's positive-balance check refuses it as well, and custody credited later reopens a new period with nothing proven (FrostMovement.reopen).
