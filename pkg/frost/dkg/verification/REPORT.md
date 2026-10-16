# V03 chain/readiness verification

All K03-attainable bounded model/replay requirements are complete for the exact source-pinned inactive subject. Independent fourth review found no remaining material defect and conditionally accepted; final matching normal/race evidence closes its conditions in evidence/independent-conditions-closure.json. The positive certified-funded witness remains explicitly later-capability blocked. This is not proof of Go or production qualification.

Subject: original K03 `0b4f169ab4327a747d04531ded1ada13edc99d6d` plus nine nil-default boundary hooks, extra observation seams and reported observer fixes. Source pins identify the final repaired executor, not newer parent K04/V05 code. Executor SHA256 `50569057642a5350cc3e1fed53ef45e02afed808043ccce50fdb0949b04e346a`. Exact client `c1847ebdf133d0570643ff968539dd498069f69f`; actual Rust/cargo1.94.0 and Go1.27.1. Fixture env is review-fixes-2026-10-07/environment.sh; worker SHA256 d775dc36c508559f1bd997ab2552cea3aed94587540f6307352fec4628400765. Temporary modfile replacement only changes sibling location; no dependency upgrade.

## Exact final results

- Ordinary Stateright model: **5,409,590 unique / 20,994,202 generated** states, completed within chosen120s limit; no safety counterexample in this abstraction. **Ten witnesses** and **nine negative controls**; each control finds exactly its declared violation. Counts/times per control in evidence/results.json. Fresh final model command exited0 in 8.660022917s.
- Concrete subprocess replay: **77/77 normal and77/77 race**, **1,822 executed steps each**, **49 full /28 partial host-loss /0 none**. Final command seconds: normal 19.553090250; race 79.550133042. Every child has15s lifetime; package300s; process-group outer600s. No timeout/race report passes. Full public events, model-state projections and checked outcomes in replay[-race]-report.json.
- Final affected real fixtures and observer regressions: normal exit0, 20.261901500s; race exit0, 22.511809500s. TestLostSeatBeforeReadinessExpiresWallet, TestPendingWalletExpiryThenFreshEpoch and TestLostSeatAfterReadinessCertifiesUnderPolicy actually execute with pinned worker/contracts, no skips. Exact-gap, delayed-response, real-removal and joined-cancellation regressions pass.
- Final complete affected DKG unit/projection package also passes under race detection (Go package2.258s,60s test timeout), log evidence/dkg-final-race.log.
- Earlier broader affected FROST normal/race both passed116.525662416s /113.264134250s before final coherent-snapshot refinement. These are historical supporting evidence, not final-source claims. Parent broad336 normal/race tests were not duplicated.

## Predicate disposition

| Predicate | Current bounded inactive subject | Limit |
| --- | --- | --- |
| ONE-DESCRIPTOR | PASS | Expected/canonical finality recognition and full key/wallet/certificate identity; cryptographic collision freedom assumed. |
| NO-EARLY-FUNDING | PASS for inactive rejection; positive witness BLOCKED | K03 stays unfunded. Funding/selectors/movement/deposit future capability is absent and not synthesized. |
| READY-ONLY-RELOADED | PASS | Selected-seat signatures/count, complete durable reload identity, statement survives later loss; actual worker fixtures plus conditional V02 join. Production D02 policy not chosen. |
| INCONSISTENCY-QUARANTINE | PASS | Removal/replacement/challenge and fresh epoch; coherent observer races and late-return classified failures checked. |
| EXPIRY-TOMBSTONE | PASS | Fixed approval+3 deadline, equality/late rejection, retained tombstone and fresh epoch; no liveness theorem. |
| FRESH-EPOCH-DISJOINT | PASS | Separate attempt/descriptor/Q/label identities, old local and chain tombstones retained, actual old re-claim rejected. |
| SUBMITTED-NOT-TERMINAL | PASS | Foreign submission challenged/acceptance continues; real executor remains active until acceptance. |

Ten witness files include ready-unfunded; post-statement loss; keyless pre-readiness loss; candidate loss then fresh ready; quarantine/expiry then fresh ready; foreign submitted acceptance; key save after removed chain before observed quarantine; certificate before local ready save; delayed canonical preapproval reconciliation. Nine controls cover premature first-event recognition, early funding, memory attestation, duplicate signatures, ignored removal, reset deadline, reused old identity, omitted finality reread, and submitted-as-completion. Every available current claim prefix executes; differing weakened expectations are never executed against Go.

## Findings and review

F-V03-01 success originally dropped an already classified monitor failure after ready save; F-V03-02 callback approval knowledge was absent from monitor. Both reported before guards changed. Further reviewed refinements preserve quarantine joined with cancellation and avoid false quarantine from old canonical responses or publication between snapshot/classification. The before-source/failing regressions/minimal final repair patch live in evidence/implementation-defect-ready-return/. Model/harness defects are separately retained and summarized in continuation-run-history.md.

Independent reviews1-3 rejected gaps and drove concrete fixes. Review4 accepts exact source hashes conditionally; independent-conditions-closure.json discharges final run/hash conditions. Self-review and feedback responses retained. Hooks are nil in normal use; no production activation, funding, dependency change, mutation campaign, merge, push or PR.

## Assumptions, exclusions, invalidation

Bounds and pre-run refinements are in EXIT-SCOPE.md; correspondence in CORRESPONDENCE.md. Two nodes/two seats each,3-of4 test policy, two epochs/two descriptors, finality2 and deadline3 ticks, one change/retry and one delayed preapproval read per node/epoch. Authenticated operators, canonical atomic chain views/finality, immutable identities and honest reload statements are assumptions. Synthetic test engine supplies the durable-completion interface, not SNOWFALL crypto proof.

Source-pinned V02 acknowledged durability/status/reload guarantee is explicitly conditional, including provider max_commits3; it is not extrapolated to exhaustive proof of the full lifecycle's five-plus commits. Real replay uses actual current-source stores and full lifecycle commits.28 host-loss traces execute acknowledged boundary kills/reopens only; physical power/cache loss, unsynced I/O/fence restoration remain inherited provider limits. Older consumer store tooling and unrelated #10 freshness are not cited.

Unresolved requirements: positive funded witness and all later funding capabilities; production D02/D09/D11 policy/qualification; universal Go/cryptographic proof, real host power-loss and composition beyond provider bounds. No tooling/environment blocker remains. Any pinned implementation, chain/binding/contract, client/worker, verification tooling, policy, fixture or provider change invalidates this statement until appropriate model/replay rerun. Parent migration must preserve these hashes or revalidate.

Artifacts: S-K03-CHAIN-1.json; evidence/source-pins.json; evidence/manifest.json; evidence/freshness.json; evidence/results.json; evidence/traces/; replay reports and *-run.json; review/independent-fourth.md. Historical checkpoint documents are superseded and archived.
