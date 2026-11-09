# Batch-13 controls (tuxxy/keep-core#142, FSM analysis R-1)

Run in a scratch worktree only (`B13-tests/keep-core-ctl`, detached at `de1e99f0`, never pushed), with the runner's `LEGACY_*` environment:
`run-control.sh <scratch> <patch or none> <log>`. It copies `pkg/tbtc/frost_redemption_b13_test.go` and `pkg/tbtc/frost_redemption_b13_export_test.go` in, applies the patch to production code, runs `TestFrostRedemptionLeaderMatchesFollower`, then reverses the patch and removes the copies.

The patches are controls, not fixes. Leader-side patches touch `pkg/tbtcpg/redemptions.go` (`findPendingFrostRedemptions`) or `pkg/chain/ethereum/frost_node.go` (`PendingFrostRedemptions`); the follower-side patch touches `pkg/frost/redemption/planner.go` (`redeemable`). Every assertion is prefixed `R-1 (tuxxy/keep-core#142): `.

| Patch | Kind | Expected result |
| --- | --- | --- |
| `none` (integration code) | trivial implementation | `ready` passes; `delayed` fails at `R1 delayed by the watchtower: leader (code3-0x) proposed [R1(delayed) R2 R3], want exactly the requests a follower accepts, oldest first: [R2 R3]`; `delayed-alone` at `only R1, delayed by the watchtower: leader (code3-0x) proposed a redemption of [R1(delayed)], but no request is one a follower accepts`; `expired` at `R1 one second inside the deadline margin: leader (geth-32000-nodata) proposed [R1(expiring) R2 R3], want ... [R2 R3]` |
| `b13-fix`: the leader's finder goes through `redemption.Planner.Pending` with the wallet's descriptor from the Bridge (`movement.Chain.Wallet`) and the members' 600 s margin, at the head | positive (the analysis's shape) | all four pass |
| `b13-fix-predicate`: the leader's own finder adds NotBefore, deadline and margin at the head's timestamp, no Planner | positive (second shape) | all four pass |
| `b13-no-margin`: `b13-fix` with `DeadlineMargin: 0` | wrong fix | `expired` fails at `R1 one second inside the deadline margin: ... proposed [R1(expiring) R2 R3] ...` |
| `b13-no-notbefore`: deadline and margin, no NotBefore | wrong fix | `delayed` and `delayed-alone` fail at their first leader assertion (as `none`) |
| `b13-notbefore-strict`: `now <= NotBefore` refused | wrong boundary | `ready` fails at `three ready requests: leader (code3-0x) proposed no redemption (got *tbtc.NoopProposal) ...`; `delayed` at `at R1's NotBefore: ... proposed [R2 R3], want ... [R1(delayed) R2 R3]`; `delayed-alone` at `only R1, at its NotBefore: ... proposed no redemption ...` |
| `b13-margin-strict`: `Deadline - now <= 600` refused | wrong boundary | `expired` fails at `R1 exactly the margin before its deadline: ... proposed [R2 R3], want ... [R1(expiring) R2 R3]` |
| `b13-wallclock`: all three conditions judged by the local clock instead of the chain's | wrong fix | `delayed`, `delayed-alone`, `expired` fail at their first leader assertion (as `none`) |
| `b13-follower-weakened`: the follower drops NotBefore and the deadline checks to match the base leader | wrong direction | `delayed`, `delayed-alone`, `expired` fail at `precondition, <window>: a follower (code3-0x) accepted a proposal naming R1(...)` |
