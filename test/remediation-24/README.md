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
They need no worker and run in under a second.

| Test | Checks |
| --- | --- |
| `TestPlannerRefusesOrdinarySweepWhileReserved` | `Planner.Build` and `Sweeper.Plan` (the host's round planning) return `ErrMainBusy` and no plan while the main is reserved, in every unresolved state (Reserved, TimeoutPending, Quarantined). The `reservation without main` subtest is fixture state that C-04 cannot reach. |
| `TestAuthorityRefusesNoMainPlanWhileReserved` | A plan without main (built while the wallet had no main) is refused by `AuthorizeSweep` with a terminal error (`ErrMainBusy` or `ErrPlanMismatch`) once the main is reserved, in every unresolved state. Same fixture-only subtest. |
| `TestResolutionRouteSurvivesSingleMain` | Positive control: the resolution plan for the active reservation is built with exactly the reserved main and authorized in every unresolved state (Quarantined with a cancel directive). |
| `TestSingleMainBoundaries` | No main and no reservation: deposit-only, authorized. Unreserved main: included. A deposit refused while reserved is swept with the new main after a payment proof. |

Integration tests in `pkg/frost/integration/deposits_test.go` (real contracts
on Anvil, Bitcoin Core regtest, the pinned worker; `test/frost/K06.md`):
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

Unchanged, with the reason:
- `pkg/frost/deposit/resolution_test.go`: it uses only the resolution route.
- `pkg/frost/signing/sweep_test.go:112`: a deposit-only `SweepProposal` is still valid for a wallet without main, and the plan type does not know about reservations.
- `pkg/maintainer/spv/frost_sweeps.go`: `includeMain` comes from a sweep that was already broadcast (the prover), not from a host decision.

## Running

```
test/remediation-24/run.sh <evidence dir>             # acceptance at this tree
test/remediation-24/run.sh --controls <evidence dir>  # controls, throwaway worktree
```

Acceptance writes `manifest.json`. It is `"accepted": true` only if all 16 unit
cases and the 3 integration tests ran and passed, with zero skips, `go test
./pkg/frost/deposit/... ./pkg/frost/signing/... ./pkg/maintainer/spv/...`
passed, and the tree is clean. The integration cases need the K-06 environment
(`FROST_K03_TBTC`, `FROST_K03_ANVIL`, `SNOWFALL_WORKER(_SHA256)`,
`FROST_K04_BITCOIND(_SHA256)`), with `solidity/ecdsa/{build,node_modules}` and
`../snowfall` present. Without it they skip and the manifest is not accepted.
Each integration test takes 28 to 46 s. Most of that is the shared K-06 world
setup (DKG and Core coinbase maturity), not the new steps.

The legacy suite (`test/legacy/run.sh`) is untouched. No `pkg/tbtc` or
`test/legacy` file changes. The fix changes only `Planner.Build` and
`deposit.mainRule`, and no production code constructs `deposit.Planner`,
`Authority` or `Sweeper`. They are built only for a configured FROST host
(today only in tests), so the FROST-off path cannot reach them.

## Controls

The patches are against production code at the integration head and are never
applied on this branch. `--controls` applies each one in a throwaway worktree
and runs the unit cases.

| Patch | Expected result |
| --- | --- |
| `24-host-single-main.fix.patch` | Minimal fix: the planner returns `ErrMainBusy` while reserved (unless it is a resolution); `mainRule` refuses `main == nil` while reserved. All cases pass. |
| `24-planner-only.wrong.patch` | Authority unchanged. Fails at `single_main_test.go` "sweep without main authorized while the main is reserved" (every state, and the fixture-only case). |
| `24-authority-only.wrong.patch` | Planner unchanged. Fails at "ordinary sweep planned while the main is reserved" (planner, host round, boundaries) and `deposit_test.go` "ordinary sweep planned while the main is reserved". |
| `24-planner-includes-reserved-main.wrong.patch` | The planner includes the reserved main in an ordinary sweep instead of refusing. Fails at "ordinary sweep planned while the main is reserved". |
| `24-planner-refuses-resolution.wrong.patch` | Over-broad: the planner also refuses the resolution route. Fails at "resolution sweep of the active reservation not planned". |
| `24-authority-refuses-resolution.wrong.patch` | Over-broad: the authority also refuses the resolution route. Fails at "resolution sweep of the active reservation refused". |
| `24-planner-reserved-state-only.wrong.patch` | The planner refuses only in `Reserved`. Fails at "ordinary sweep planned while the main is reserved (TimeoutPending)" and "(Quarantined)". |
| `24-authority-reserved-state-only.wrong.patch` | The authority refuses only in `Reserved`. Fails at "sweep without main authorized while the main is reserved (TimeoutPending)" and "(Quarantined)". |

In the integration tests, `planRefused` also catches every wrong patch that
leaves the planner planning while reserved. `planner-only` is caught only by
the authority unit test, because no integration step authorizes a no-main plan
while a reservation is active.
