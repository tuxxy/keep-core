# K-04 response to Fable's review

Review: [PR #11, review 5444761660](https://github.com/tuxxy/keep-core/pull/11#pullrequestreview-5444761660),
against `f4acbe21b7af857de447fe373c3c43ba686edd53`. All six findings are addressed
in this same K-04 milestone PR. The K-03 base and other milestone PRs are unchanged.
V-04 bounded model checking and correspondence review remain merge gates.

| Finding | Resolution | Evidence |
| --- | --- | --- |
| F1: JSON reservation commitment | Version 2 is a fixed binary encoding. The byte contract is in `pkg/frost/signing/INTENT-V2.md`; JSON is local recovery data only. | Two fixed Go/independent TypeScript vectors; wide uint64 control; field mutation checks; F1-JSON negative control. |
| F2: empty authorization on guarded Sign | `NewGuarded` refuses a missing intent or callback before any claim. No bypass option was added. Lower transport tests supply explicit test grants. | Empty intent with/without callback and grant-without-callback checks; F2-EMPTY control. |
| F3: volatile reservation anchor | Save the first finalized block/hash through the fenced immutable store. Bind its record ID to plan/reservation. Results carry that receipt; recovery and broadcast require the durable match. | Fresh-executor and real-store re-anchor refusals, same-anchor positive control, missing/altered receipt tests; F3-ANCHOR/F3-RESULT controls. |
| F4: truncated FROST monitor label | Remove `BridgeLabel`. Distinct accessors return ECDSA HASH160 or full FROST wallet ID and refuse the other scheme. The monitor keeps scheme plus all 32 FROST bytes. | Routing/monitor regression and last-byte identity control; F4-LABEL control. |
| F5: coordination scope | Name `A-K04-AGREED-EXECUTOR-CONFIG` in the verification map. Cross-operator agreement on session, start block, selected seats and executor configuration is a K-05 deliverable. | README, verification map, progress record and PR summary state the same boundary. |
| F6: stub-only refusal oracle | Flip the reservation only after a real store acknowledges the real engine's claim. Observe every launch attempt, require no launch, and retry with a fresh executor to check the terminal claim. | The same launcher executes the hash-checked real worker twice in the successful control; F6-GRANT control fails the real-provider negative subtest. |

The launch observer is test-only: a small shell entrypoint records a marker,
then executes the unchanged, hash-checked worker. Its own bytes are pinned for
that test. This catches short-lived starts that a process snapshot could miss.
No marker is written for the refused attempt or retry. The positive control
uses the same launcher and completes two real input signatures and Core acceptance.

## Negative controls

Each control changes one production condition/encoding, runs its named test,
and restores the source in `finally`. No build failure or timeout counts as a
kill. The retained runner records exact original/mutant hashes and failed tests.

| ID | Deliberate defect | Expected test failure |
| --- | --- | --- |
| F1-JSON | Hash recovery JSON instead of the binary preimage. | `TestCanonicalIntentV2` |
| F2-EMPTY | Permit empty authorization through the guarded entrypoint. | `TestSigningAuthorizationAfterDurableClaim` |
| F3-ANCHOR | Ignore the durable anchor when reading a reservation. | `TestReservationReceiptSurvivesExecutorRecovery` |
| F3-RESULT | Ignore the result's receipt when comparing durable storage. | `TestReservationReceiptSurvivesExecutorRecovery` |
| F4-LABEL | Truncate the monitor's FROST identity to 20 bytes. | `TestMixedWalletRouting` |
| F6-GRANT | Omit the final engine authorization callback. | `TestTaprootExactAuthorization/claim_flip_refuses_before_real_worker_launch` |

All six are killed by assertion failures. No survivor, build-error kill or timeout.
The corrected source is restored before qualification. These are six targeted
review controls, not a full K-09 mutation campaign or a V-04 model check.

Final qualification counts and source pins are in `K04-RESULTS-2026-10-07.md`.
Raw logs and the control runner are retained under
`Research/SNOWFALL/integration-spec-v2/implementation/K-04/review-fixes-2026-10-07/`.
The original review and original K-04 evidence remain intact.
