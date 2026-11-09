# tuxxy/tbtc-v2#24, keep-core side: single main (D13 refinement)

Refs tuxxy/tbtc-v2#24. Owner decision: `DECISIONS.md` "D13 refinement" (tux,
2026-10-30). The contract side is in the tbtc-v2 PR for the same issue.

**Invariant.** While a FROST wallet's main is reserved for a redemption
(C-04 `activeReservation != 0`), the honest K-06 host neither plans nor
authorizes an ordinary deposit sweep. Deposits wait until the reservation
resolves (payment proof, cancellation or sweep resolution), as an ECDSA
wallet's deposits wait for its main. The C-04 sweep-resolution route (a sweep
naming the active reservation and spending its main) is unchanged (KC-SF-V3
§4.6). A wallet with no main and no reservation still sweeps deposit-only. So
recognized custody is always exactly the main. Supports KC-VER
P-K06-ONE-MAIN-PURPOSE and P-K06-CREDIT-ONCE.

## Tests

Unit tests in `pkg/frost/deposit` use the fake C-05/C-04 reader of
`fake_test.go` (a fixture) with the real `Planner`, `Authority` and `Sweeper`.
The leader test in `pkg/tbtcpg` uses fixture host, Bridge and Bitcoin chains
with the real `DepositSweepTask` and K-06 planner. They need no worker and
run in under a second. Kind: **unit** (not listed in `test/suites/e2e.txt`).

| Test | Kind | Checks |
| --- | --- | --- |
| `TestFrostDepositSweepLeaderWaitsWhileMainReserved` (`pkg/tbtcpg/frost_deposit_sweep_d13_test.go`) | unit | The window's leader (FROST branch of `DepositSweepTask.Run`) proposes the deposit with no main and with an unreserved main (positive controls), and proposes nothing while the main is reserved, in Reserved, TimeoutPending and Quarantined. Batch 6 already guards the leader, so this passes at the integration head; it keeps the guard (and a state-only variant) from regressing. |
| `TestPlannerRefusesOrdinarySweepWhileReserved` | unit | `Planner.Build` and `Sweeper.Plan` (the host's round planning) return `ErrMainBusy` and no plan while the main is reserved, in every unresolved state (Reserved, TimeoutPending, Quarantined). |
| `TestAuthorityRefusesNoMainPlanWhileReserved` | unit | A plan without main (built while the wallet had no main) is refused by `AuthorizeSweep` once the main is reserved, in every unresolved state. The test accepts `ErrMainBusy` or `ErrPlanMismatch`, provided the error is terminal (the sweeper abandons the plan and replans after the reservation resolves). Coordinator's choice: the decision does not fix the sentinel. |
| `TestResolutionRouteSurvivesSingleMain` | unit | Positive control: the resolution plan for the active reservation is built with exactly the reserved main and authorized in every unresolved state (Quarantined with a cancel directive). |
| `TestSingleMainBoundaries` | unit | No main and no reservation: deposit-only, authorized. Unreserved main: included. A deposit refused while reserved is swept with the new main after a payment proof. |

Integration tests in `pkg/frost/integration/deposits_test.go`, kind **e2e**
(already listed in `test/suites/e2e.txt`; real contracts on Anvil, Bitcoin
Core regtest, the pinned worker; `test/frost/K06.md`):
`planRefused` requires every operator's `Sweeper.Plan` to refuse with
`ErrMainBusy` while the main is reserved, and `sweepAndProve` requires every
ordinary sweep proof to leave custody equal to its single unreserved main.

### Existing tests amended

- `pkg/frost/deposit/deposit_test.go` `TestPlannerMainRule/reserved main is left out`: it asserted the old rule (a deposit-only plan while reserved). It now asserts the refusal (`reserved main refuses an ordinary sweep`).
- `pkg/frost/deposit/deposit_test.go` `TestAuthorityRequiresUnreservedMain`: it built its no-main plan while the main was reserved, which the new rule no longer plans. It now builds it while the wallet has no main, then funds an unreserved main; the assertion (`ErrPlanMismatch`) is unchanged.
- `pkg/frost/integration/deposits_test.go`:
  - `TestMainPurposeLockAndResolution`: the deposit-only sweep credited beside the reserved main is replaced by the refusal. After the resolution sweep is proven, the deposit that waited is swept with the resolution output as main.
  - `TestLateDepositRecovery`: the late-signature case used a deposit-only sweep beside a reserved main. Now the deposit is refused while reserved, the payment is proven, and the late sweep spends the payment's change as main. The named assertions used by `test/frost/k06-controls.py` are kept.
  - `TestFrostDepositSweepAndCredit`: the second deposit is now revealed while the main is reserved (after the payment is confirmed on Bitcoin but before its C-04 proof). It is refused, then swept with the new main after the proof.
  - `sweepAndProve`: the custody check tolerated a reserved main beside the sweep output. It now requires the sweep output to be the unreserved main holding all custody.
  - The payment steps were moved into the `broadcastPayment` and `provePayment` helpers, which two tests now use.

Not changed here, but a fix will break it (needs a decision; see below):
- `pkg/frost/deposit/v07_replay_test.go` `TestV07Replay`: four V-07 model
  traces (`None-P-K06-D13-NO-SIDE-SWEEP`, `ForgetConsumptionOnRetry-P-K06-CREDIT-ONCE`,
  `None-W-fresh-attempt-after-failure`, `None-W-refund-operators-stopped`)
  plan a deposit-only sweep beside a reserved main (F-V07-1). Under the fix
  `Planner.Build` refuses that step and the replay dereferences the missing
  plan (panic in `stage`). The traces come from the V-07 model, which must
  adopt D13 (P-K06-D13-NO-SIDE-SWEEP) and be re-run; that is verification
  work, outside this batch.

Unchanged, with the reason:
- `pkg/frost/deposit/resolution_test.go`: it uses only the resolution route.
- `pkg/frost/signing/sweep_test.go:112`: a deposit-only `SweepProposal` is still valid for a wallet without main, and the plan type does not know about reservations.
- `pkg/maintainer/spv/frost_sweeps.go`: `includeMain` comes from a sweep that was already broadcast (the prover), not from a host decision.

## Running

```
test/remediation-24/run.sh <evidence dir>             # acceptance at this tree
test/remediation-24/run.sh --controls <evidence dir>  # controls, throwaway worktree
```

Acceptance writes `manifest.json`. It is `"accepted": true` only if all 19 unit
cases (14 deposit, 5 leader) and the 3 integration tests ran and passed, with zero skips, `go test
./pkg/frost/deposit/... ./pkg/frost/signing/... ./pkg/maintainer/spv/...`
passed, and the tree is clean. The integration cases need the K-06 environment
(`FROST_K03_TBTC`, `FROST_K03_ANVIL`, `SNOWFALL_WORKER(_SHA256)`,
`FROST_K04_BITCOIND(_SHA256)`), with `solidity/ecdsa/{build,node_modules}` and
`../snowfall` present. Without it they skip and the manifest is not accepted.
Each integration test takes 13 to 20 s (2026-11-09), over the 10 s case
budget. Most of that is the shared K-06 world setup (DKG and Core coinbase
maturity), not the new steps.

The legacy suite (`test/legacy/run.sh`) is untouched. No `pkg/tbtc` or
`test/legacy` file changes. The fix changes only `Planner.Build` and
`deposit.mainRule`. Since batch 6 the FROST node builds `deposit.Planner`,
`Authority` and `Sweeper` in `pkg/tbtc/frost_node_duties.go` (the member's
`sweepDeposits`) and `pkg/tbtcpg/frost_deposit_sweep.go` (the leader), only
for a FROST wallet of a node with FROST configured, so the FROST-off path
cannot reach them. Both already refuse at their coordination-block custody
read while the main is reserved; the authority is still called before every
signing round and before broadcast, which is where a reservation that lands
after the coordination block is caught.

## Controls

The patches are against production code at the integration head and are never
applied on this branch. `--controls` applies each one in a throwaway worktree
and runs the unit cases.

| Patch | Expected result |
| --- | --- |
| `24-host-single-main.fix.patch` | Minimal fix: the planner returns `ErrMainBusy` while reserved (unless it is a resolution); `mainRule` refuses `main == nil` while reserved. All cases pass. |
| `24-planner-only.wrong.patch` | Authority unchanged. Fails at `single_main_test.go` "sweep without main authorized while the main is reserved" (every state). |
| `24-authority-only.wrong.patch` | Planner unchanged. Fails at "ordinary sweep planned while the main is reserved" (planner, host round, boundaries) and `deposit_test.go` "ordinary sweep planned while the main is reserved". |
| `24-planner-includes-reserved-main.wrong.patch` | The planner includes the reserved main in an ordinary sweep instead of refusing. Fails at "ordinary sweep planned while the main is reserved". |
| `24-planner-refuses-resolution.wrong.patch` | Over-broad: the planner also refuses the resolution route. Fails at "resolution sweep of the active reservation not planned". |
| `24-authority-refuses-resolution.wrong.patch` | Over-broad: the authority also refuses the resolution route. Fails at "resolution sweep of the active reservation refused". |
| `24-planner-reserved-state-only.wrong.patch` | The planner refuses only in `Reserved`. Fails at "ordinary sweep planned while the main is reserved (TimeoutPending)" and "(Quarantined)". |
| `24-authority-reserved-state-only.wrong.patch` | The authority refuses only in `Reserved`. Fails at "sweep without main authorized while the main is reserved (TimeoutPending)" and "(Quarantined)". |
| `24-leader-no-reservation-check.wrong.patch` | The host fix, with the leader's reservation check removed (removal control for the batch-6 guard). Fails only at `frost_deposit_sweep_d13_test.go` "D13 leader: an ordinary deposit sweep was proposed while the main is reserved" (Reserved, TimeoutPending, Quarantined). |
| `24-leader-reserved-state-only.wrong.patch` | The host fix, with a leader that waits only while the intent is `Reserved`. Fails only at "D13 leader: ... (TimeoutPending)" and "(Quarantined)". |

Cross-repository wrong fixes need no patch here: a Bridge-only fix leaves this
tree as it is, so the planner and authority cases fail; a host-only fix leaves
the tbtc-v2 tests (tuxxy/tbtc-v2#52) failing.

In the integration tests, `planRefused` also catches every wrong patch that
leaves the planner planning while reserved. `planner-only` is caught only by
the authority unit test, because no integration step authorizes a no-main plan
while a reservation is active.
