# Batch 2 controls (keep-core)

Each patch is applied to a scratch checkout of the integration code
(`052b14444`, which is `integration/snowfall-base` `ec1084dd` plus the frozen
batch-1 tests) with the batch-2 test files on top, the named cases are run
through `run.sh <dir> <filter>`, and the patch is reverted. None is ever
committed to production code. "Positive" controls show a test can pass;
"removal" controls show a guard or an assertion fails at its own message.

| Patch | Kind | What it does | Result recorded 2026-10-28, re-recorded 2026-10-29 on the fast harness |
| --- | --- | --- | --- |
| `N.fix.patch` | positive and removal | naive drain producer: commits to every ready FROST wallet (C-06 count, ascending), with no check of drain switch, mode, containment, budget or dust; legacy only when it finds none | all 12 `Drains` cases pass (P2TR outputs, Core mines, the upgraded Bridge accepts the proof; both encodings, both fault cases, two targets) and `BridgeUpgradedAfterStartup` passes. Removal: `LegacyFallbackBeforeCutover` contained, drain-off and budget-partial (both modes) fail at "before the cutover, with the drain unusable, the host made no legacy commitment"; all 4 `BelowDust` cases fail at "the host attempted 1 moving-funds commitments for a below-dust main"; every `NoRouteRecovery` case except the zero-eligible live0 ones fails at "before the reset is due: the host attempted 1 moving-funds commitments while no route was admissible" (FrostOnly-live1-zero-eligible at "the host committed legacy targets … after the cutover"); re-recorded 2026-10-29 after the keeper-reset change |
| `L.fix.patch` | removal (D16) | N, but before the cutover a legacy Live target is preferred to the drain (the pre-D16 reading) | the 8 earlier `Drains` cases pass; all 4 D16 cases (`LegacyOpen-live1`, `LegacyDraining-live1` and their `frost-read-faults` variants) fail at "the host committed legacy targets [legacy:…] while an ECDSA-to-FROST drain was admissible (before the cutover; D16 requires the drain)" |
| `S.fix.patch` | removal (D6, review R6) | N, but while its log-built index shows fewer targets than the main needs, fall back to legacy | all 3 `ShortIndex` cases fail at "window 1: with its target index short, the host committed legacy targets [legacy:…]" |
| `C.fix.patch` | positive (review R14, Astra's probe) | N, but the target index is built from blocks and receipts, not eth_getLogs, so the lagging log index does not make it short | all 3 `ShortIndex` cases pass (4.2–4.3 s): the host commits at once, and the test accepts it because the commitment names exactly both eligible targets, ascending, with no legacy target |
| `V.fix.patch` | positive (D6, review R6) | N, but while the index is short, wait (return an error) | all 3 `ShortIndex` cases pass (4.0–4.2 s each, re-recorded 2026-10-29 after R14); the lagging index withheld 2 logs in each. N itself fails them at "window 1: with its target index short, the host attempted 1 moving-funds commitments (an undersized drain or a legacy commitment …)"; S still fails all 3 at the legacy assertion after R14 |
| `F.fix.patch` | removal (K09-31) | N, plus: with the drain enabled, never fall back to legacy | all 8 `LegacyFallbackBeforeCutover` cases fail at "the host made no legacy commitment to the Live target" (including zero-eligible, which N passes) |
| `W.fix.patch` | removal (K09-56, failing getter; D6) | N, but a failed FROST read is taken as "no FROST target" | `Drains/FrostOnly-live1-frost-read-faults`, `LegacyOpen-live1-frost-read-faults` and `LegacyDraining-live1-frost-read-faults` fail at "the host committed legacy targets [legacy:…] while an ECDSA-to-FROST drain was admissible (after the cutover)" (FrostOnly) or "(before the cutover; D16 requires the drain)". `transient-then-success` still passes under W: there every legacy read fails in the same window too, so W errors instead of committing |
| `U.fix.patch` | removal (K09-56, upgrade after construction) | N, but the client decides at startup whether the Bridge is pre-FROST and never revisits it | `BridgeUpgradedAfterStartup` fails at "the running client committed legacy targets [legacy:…] after the cutover" |
| `PT.fix.patch` | removal (pre-FROST guard) | a host that sends the moving-funds reset when no Live wallet exists, through the generated wrapper (formerly R-good, kept only for this guard) | `PreFrostUnchanged` fails in both encodings at "pre-FROST, zero Live wallets: the client sent a transaction to 0xA51c…91C0 (selector 0xee1dd3ea); base sends none" |
| `Q.fix.patch` | sensitivity (keeper reset) | an idle host: the moving-funds task never proposes, so only the test's third-party keeper acts | `NoRouteRecovery`: the live0 cases pass both keeper resets and the refused timeout notifications and fail only at "after the route opened, the host made no drain commitment"; the FrostOnly live1 cases fail at "period 1: a keeper's resetMovingFundsTimeout is refused with no admissible route: the source can be terminated (execution reverted: Live wallets count must be zero)", the contract half (tuxxy/tbtc-v2#20). Slowest case 6.7 s run alone |
| `P1.fix.patch` | removal (K09-33) | zero Live wallets is an error | `PreFrostUnchanged/code3-0x` fails at "the moving-funds task returned an error instead of no result" |
| `P3.fix.patch` | removal (K09-55) | an extra chain-head read in the safety-margin check | `PreFrostUnchanged/code3-0x` fails at "the check made a eth_getBlockByNumber request, which base does not make" |

The pre-FROST controls (PT, P1, P3) were run on the code-3 case only; the
geth case runs the same assertions.

Removed on 2026-10-29 with `TestDrainProducer_ResetSubmission`: R-bad (the K-09
reset holding the transaction mutex) and R-good as a reset-sender positive.
The timeout reset is a keeper job, as in base (DECISIONS.md "Standing
default"); tuxxy/keep-core#51, #54, #57 and #65 are closed as moot.
