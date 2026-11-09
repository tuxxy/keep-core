# Batch-16 controls (tuxxy/keep-core#146)

Run these in a scratch worktree only (`B16-tests/keep-core-ctl`, detached at `1b6ad8de`, never pushed), with the `LEGACY_*` environment from `test/remediation-b16/run.sh`:
`run-control.sh <scratch> <patch or none> <log>`.

The script copies in `pkg/tbtc/frost_movement_seat_b16_test.go`, applies the patch to `pkg/tbtcpg/frost_moving_funds.go`, runs the six cases, then reverses the patch and removes the copy.

| Patch | Kind | Expected result |
| --- | --- | --- |
| `none` (integration code, `i+1`) | trivial implementation | all 6 fail at `tuxxy/keep-core#146: leader <X> ... the leader's own movement commitment was not accepted by the Bridge (... execution reverted: FROST movement operator)` |
| `b16-fix`: `uint16(i)` from the request | positive, fix 1 | 6/6 pass |
| `b16-fix-onchain`: the leader's *highest* 0-based seat in the on-chain descriptor (`source.Wallet.Descriptor.Operators`) | positive, fix 2 (also shows that any seat the leader holds is accepted: A commits at seat 2) | 6/6 pass |
| `b16-wrong-seat0`: always seat 0 | wrong fix | A passes; B and C fail at the named assertion |
| `b16-wrong-distinct`: index among the distinct operators | wrong fix | A and B pass; C fails at the named assertion (its distinct index is 2, which is A's seat) |
| `b16-wrong-contract-1based`: `none` (`i+1` kept) against a Bridge whose `FrostMovement.commit` takes a 1-based seat | wrong fix (contract changed; out of scope) | all 6 fail at `tuxxy/keep-core#146: contract pin: the Bridge refuses operator A (...) committing at its seat 0 ...` |

The contract control needs a scratch tbtc-v2 commit. That commit is `b8ad1fd6`, on 39412825, and it changes only the seat check in `FrostMovement.commit` to `seat > 0 && seat <= length && operators[seat - 1]`. It lives in the detached scratch worktree `B16-tests/tbtc-v2-ctl` and is never pushed. It was built with `test/legacy/build-baseline.sh` into `B16-tests/upgraded-ctl-1based`. Run it with:
`LEGACY_UPGRADED_DIR=<that build> LEGACY_UPGRADED_COMMIT=b8ad1fd670dd20d35b615fd30d73648b639d0c2e run-control.sh <scratch> none <log>`.
