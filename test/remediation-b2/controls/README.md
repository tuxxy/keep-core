# Batch 2 controls (keep-core)

Each patch is applied to a scratch checkout of the integration code
(`052b14444`, which is `integration/snowfall-base` `ec1084dd` plus the frozen
batch-1 tests) with the batch-2 test files on top, the named cases are run
through `run.sh <dir> <filter>`, and the patch is reverted. None is ever
committed to production code. "Positive" controls show a test can pass;
"removal" controls show a guard or an assertion fails at its own message.

| Patch | Kind | What it does | Result recorded 2026-10-28 |
| --- | --- | --- | --- |
| `N.fix.patch` | positive and removal | naive drain producer: commits to every ready FROST wallet (C-06 count, ascending), with no check of drain switch, mode, containment, budget or dust; legacy only when it finds none | all 8 `Drains` cases pass (P2TR outputs, Core mines, the upgraded Bridge accepts the proof; both encodings, both fault cases, two targets) and `BridgeUpgradedAfterStartup` passes. Removal: `LegacyFallbackBeforeCutover` contained, drain-off and budget-partial (both modes) fail at "before the cutover, with the drain unusable, the host made no legacy commitment"; all 4 `BelowDust` cases fail at "the host attempted 1 moving-funds commitments for a below-dust main"; `NoRouteRecovery/FrostOnly-live1-contained` fails at "the host attempted 1 moving-funds commitments while no route was admissible" |
| `F.fix.patch` | removal (K09-31) | N, plus: with the drain enabled, never fall back to legacy | all 8 `LegacyFallbackBeforeCutover` cases fail at "the host made no legacy commitment to the Live target" (including zero-eligible, which N passes) |
| `W.fix.patch` | removal (K09-56, failing getter) | N, but a failed FROST read is taken as "no FROST target" | `Drains/FrostOnly-live1-frost-read-faults` fails at "the host committed legacy targets [legacy:…] while an ECDSA-to-FROST drain was admissible after the cutover". `transient-then-success` still passes under W: there every legacy read fails in the same window too, so W errors instead of committing |
| `U.fix.patch` | removal (K09-56, upgrade after construction) | N, but the client decides at startup whether the Bridge is pre-FROST and never revisits it | `BridgeUpgradedAfterStartup` fails at "the running client committed legacy targets [legacy:…] after the cutover" |
| `R-good.fix.patch` | positive (K09-35/K09-42) | with zero Live wallets, reset through the standard generated wrapper on every Bridge | `ResetSubmission` passes; `NoRouteRecovery/FrostOnly-live0-contained` passes both reset periods and fails only at "after the route opened, the host made no drain commitment" (R-good has no drain producer). Removal for the pre-FROST guard: `PreFrostUnchanged/code3-0x` fails at "the client sent a transaction to 0xA51c…91C0 (selector 0xee1dd3ea); base sends none" |
| `R-bad.fix.patch` | removal (K09-42) | the K-09 reset: holds the transaction mutex through `WaitMined`, own keyed transactor | `ResetSubmission` fails at "another transaction of the same client waited more than 10s while the reset was unmined: the reset holds the client's transaction mutex while it is mined" |
| `P1.fix.patch` | removal (K09-33) | zero Live wallets is an error | `PreFrostUnchanged/code3-0x` fails at "the moving-funds task returned an error instead of no result" |
| `P3.fix.patch` | removal (K09-55) | an extra chain-head read in the safety-margin check | `PreFrostUnchanged/code3-0x` fails at "the check made a eth_getBlockByNumber request, which base does not make" |

The pre-FROST controls (R-good, P1, P3) were run on the code-3 case only; the
geth case runs the same assertions.
