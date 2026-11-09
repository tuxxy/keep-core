# Batch-16b controls (tuxxy/keep-core#146, Terminated half)

Run these in a scratch worktree only (`B16b-tests/keep-core-ctl`, detached at `80c3c295`, never pushed), with the `LEGACY_*` environment from `test/remediation-b16b/run.sh`:
`run-control.sh <scratch> <patch or none> <log>`.

The script copies in `pkg/tbtc/frost_movement_seat_terminated_b16b_test.go`, applies the patch to production code, runs the twelve cases (`<route>/<encoding>/<leader>`), then reverses the patch and removes the copy. All patches are made against `80c3c295`.

| Patch | Kind | Expected result |
| --- | --- | --- |
| `none` (integration code, `i+1`) | trivial implementation | all 12 fail at `tuxxy/keep-core#146 (Terminated): leader <X> ... Terminated source <route>: the leader's own movement commitment was not accepted by the Bridge (... execution reverted: FROST movement operator)` |
| `b16b-fix`: `uint16(i)` in `frostExecutingSeat` | positive, fix 1 | 12/12 pass |
| `r-fix-adapter` (reviewer, REVIEW-TEST-B16.md): the host stays 1-based and `FrostRedemptionChain.SubmitCommitment` sends `seat-1` | positive, fix 2 (conversion at the chain boundary) | 12/12 pass |
| `b16b-fix-onchain`: the leader's highest 0-based seat in the on-chain descriptor | positive, fix 3 | 12/12 pass |
| `r-wrong-movingfunds-only` (reviewer): renumbers only when the source is MovingFunds | wrong fix; passes batch 16 | all 12 fail at the named assertion |
| `b16b-wrong-requested-only`: renumbers only when a moving-funds request is recorded | wrong fix (keyed on the route) | the 6 `from-live` cases fail at the named assertion; the 6 `from-moving-funds` cases pass |
| `none` against a Bridge whose `FrostMovement.commit` takes a 1-based seat | wrong fix (contract changed; out of scope) | all 12 fail at `tuxxy/keep-core#146 (Terminated): contract pin: the Bridge refuses operator A (...) committing at its seat 0 ...` |

The contract control reuses batch 16's scratch build, read only: `B16-tests/upgraded-ctl-1based` (tbtc-v2 `b8ad1fd6`, never pushed). Run it with:
`LEGACY_UPGRADED_DIR=<that build> LEGACY_UPGRADED_COMMIT=b8ad1fd670dd20d35b615fd30d73648b639d0c2e run-control.sh <scratch> none <log>`.

**Fix interface (as batch 16, review N2):** the test builds `CoordinationProposalRequest` itself. A fix must get the seat from `WalletOperators`/`ExecutingOperator` or from the on-chain descriptor, or convert it at the chain boundary. It must not add a new request field.
