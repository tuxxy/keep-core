# Batch-11 controls (tuxxy/keep-core#133, D27)

Run in a scratch worktree only (`B11-tests/keep-core-ctl`, detached at `925103a6`, never pushed), with the runner's `LEGACY_*` environment:
`run-control.sh <scratch> <patch or none> <log>`. It copies `pkg/tbtc/frost_incoming_b11_test.go` in, applies the patch to production code, runs both batch-11 tests, then reverses the patch and removes the copy.

Every patch touches only `pkg/chain/ethereum/frost_movement.go` (`IsFrostIncomingPending`, which the `tbtcpg` finder and the follower validator share, and `validateFrostMovedFundsSweepProposal`). The leader validates its own proposal before returning it (base `tbtcpg`), so a follower-side refusal also shows on the leader.

| Patch | Kind | Expected result |
| --- | --- | --- |
| `none` (integration code) | trivial implementation | `Proposed/escalated` fails at `... incoming output Escalated: no moved funds sweep proposal ...` (NoopProposal); `FollowerValidation` fails at `a follower (...) refused a sweep of the target's Escalated incoming output with fee ...` (1, cap/2, cap, both encodings); `Proposed/pending` passes |
| `b11-fix`: the record is the wallet's and Pending or Escalated | positive | all three pass |
| `b11-any-recorded`: any recorded state of the wallet (Swept too) | wrong fix | `Proposed/pending` and `/escalated` fail at `... after the <state> output was swept: the leader proposed a sweep of ..., which is not sweepable` |
| `b11-any-target`: Escalated accepted whatever its target | wrong fix | `FollowerValidation` fails at `a follower (...) accepted another FROST wallet's proposal sweeping the target's Escalated incoming output` |
| `b11-escalated-skips-fee`: an Escalated output skips the fee checks | wrong fix | `FollowerValidation` fails at `... Escalated incoming output with fee above the on-chain cap` and `... with zero fee` |
| `b11-follower-only`: only the validator accepts Escalated | wrong fix (one side) | `Proposed/escalated` fails at `... no moved funds sweep proposal ...` |
| `b11-leader-only`: the finder accepts Escalated, the validator stays Pending-only | wrong fix (one side) | `Proposed/escalated` fails at `... no moved funds sweep proposal ...` (the leader's self-validation refuses it); `FollowerValidation` fails at `a follower (...) refused ...` |
| `b11-wrong-fee-param` (review r3, REVIEW-TEST-B11 B1): b11-fix, but an Escalated output's fee is checked against `MovingFundsTxMaxTotalFee` | wrong fix | `FollowerValidation` fails at `... Escalated incoming output with fee above the on-chain cap` (both encodings); the test raises only `movingFundsTxMaxTotalFee` to twice the sweep cap |
