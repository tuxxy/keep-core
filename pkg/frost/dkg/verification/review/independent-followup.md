**Not yet accepted, even for the bounded inactive K03 scope.** Three concrete findings remain. This was read-only; I ran no tests, campaigns, or activation actions.

1. **High — Worker-completion waiting is excluded from model exploration.**

   In [main.rs:120](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/verification/model/src/main.rs:120), `active()` excludes phase **21**. `WorkerReady` enters that phase at line 351, but monitor, process-crash, and host-crash actions are offered only for active phases at line 218.

   Consequently, a node waiting for the remaining worker-ready messages cannot observe removal/expiry or crash in the explored model. This is precisely a relevant lost-seat interleaving.

   The boundary exporter bypasses action availability by calling `next_state` directly. The retained `B-PROCESS-0-before-key-ready-save.json:3032` actually records **`claim_available: false`**, yet replay executes that step and reports a match. Thus the concrete boundary execution exists, but its corresponding transition was excluded from the exhaustive claim exploration.

   **Required:** include phase 21 in the appropriate live actions, validate action availability for every ordinary exported trace, and regenerate affected evidence. Strengthen the pre-readiness-loss witness to require the intended worker/key boundary: its current predicate permits loss after both keys and C03 statements already exist.

2. **Medium — F-V03-02 introduces false quarantine for an ordinary delayed pre-approval snapshot.**

   [executor.go:272](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/executor.go:272) obtains a monitor snapshot before consulting shared approval knowledge; lines 296–299 then compare that snapshot against knowledge concurrently installed by the callback at lines 414–422.

   A legal ordering is:

   - Monitor captures a canonical pre-approval view.
   - Callback reads the later approval and shares its ID/hash.
   - Monitor finishes processing its older view and reports “approval was removed or replaced.”

   No removal occurred. This can cancel DKG and persist quarantine. The real adapter permits this ordering: [frost.go:132](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/chain/ethereum/frost.go:132) pins a snapshot to its starting head; its final check verifies that historical head remains canonical, not that it is still latest.

   Replay cannot exercise this: [replay_test.go:625](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/verification/replay/replay_test.go:625) waits for the monitor permit **before** obtaining the view at line 673. The model likewise makes observation and classification atomic.

   **Required:** distinguish an overlapping older read from evidence of removal—for example, retain observation ordering and reconcile overlapping reads. Add deterministic delayed-snapshot coverage while retaining the existing genuine-removal regression. This is a source-derived finding, not an executed reproduction.

3. **Medium — Returned terminal records and crash-state identities remain incompletely compared.**

   [replay_test.go:962](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/verification/replay/replay_test.go:962) returns the terminal event, but callers discard it. `finish` checks only its outcome string. Reopening the journal now verifies useful durable state, but does not compare the returned `WalletRecord` against that durable record.

   Additionally:

   - `compareWallet` at line 1529 omits `DescriptorHash`, `ResultHash`, and complete descriptor equality.
   - Terminal key checking at line 1573 compares only the Snowfall descriptor.
   - `inspectKilled` at line 1043 checks key presence and status enum, without checking the retained wallet or complete key identity.

   A correct error string and durable lifecycle rank can therefore pass alongside an incorrect returned record or unchecked identity fields. The report’s complete `matched_state` object is broader than the comparisons performed.

   **Required:** compare terminal return records with reopened records and the expected identity/receipt; apply the relevant wallet/key comparisons after kills; explicitly distinguish checked projections from unobserved model fields.

The earlier findings are substantially improved. Independent history fields replace `bad`; certificate, read, reload, persistence, and return stages are separated; worker-ready and C03 messages are distinct; certificate subsets are modeled; retained Q and attempt admission checks now exist. H06 correctly distinguishes claim-action availability from changed weakened results.

The retained before-source, first incomplete repair, failing logs, passing regression, and current source support **F-V03-01’s repair and F-V03-02’s original removal scenario**. Finding 2 above concerns an additional ordering introduced by sharing observations.

I independently recomputed all production and archived-tooling entries in `durability-join.json`; they match. The three-commit provider limit is now explicitly separated from the lifecycle durability assumption. That is acceptable as a conditional interface assumption, not an exhaustive lifecycle durability proof. Parent child deadlines and cancellation-aware hook waits address the principal earlier harness issue.

The report contains **76/76 matched entries**, and model results record nine witnesses plus nine targeted controls. Those results do not resolve the omissions above. Historical checkpoint reports and pending final evidence refresh are not grounds for acceptance. Funding success remains explicitly later blocked.

SHA256 pins follow. The principal files and defect-evidence files were rechecked and unchanged. Paths below are relative to `pkg/frost/dkg/verification`, except those beginning `pkg/`.

```text
16fc245ff7db184ad7cfdb56d0dcab1cee7442e0ad456ec4f2dd272d1e20b5b4  pkg/frost/dkg/executor.go
cdad3d1848354773b07feb673215d52d34f3a6437ce00b610b691fb9502764ee  pkg/frost/dkg/outcome_test.go
fe4c98301bbf30afac93c76759df3d9ee27fd4642cfc05036524f8c08c991f1c  model/src/main.rs
322ae3822d995c954949aaf1a30da140d0a02f86b936d3bddafdbfe5c5fc17a4  replay/replay_test.go
da92044b09bc6c8fc9c7694fc0f20480935a9b4023134535cf0c1f2376bfc371  review/independent-review.md
eba90f0d89a74599f267ed01ef932569e8bf2ae8b71ac65d64e59f62ff68e30a  review/responses.md
41a796f6b42bd878ea7728518823a826e0a0a5b48eae34476b022f443a1d0a0d  EXIT-SCOPE.md
92b23d43b60a734f5f44527e08fff38674e7e2af4fb81f985af7cd562b84ef4c  DURABILITY-JOIN.md
89b2cab8c5369fa7bef8cb6832cbcd15e2af6448b48fdfe0d44fe7d656cf0873  run.py
d04c7eda0e841b127b8efa75c8793b4abbf683861445ca6a62943a61d608e0eb  evidence/durability-join.json
d2e6dacc923489420c38bd9f3c58c7328814893b22612d0b4b663ade095f10c1  evidence/replay-report.json
58b550b088f5f812172e5e189c17c458ef67b014bd30db427a2591363bb23a16  evidence/results.json
a61af58092237d0c0d9a85d569644905b01029722da4ce1f9e4fc3807297a98b  evidence/replay-run.json
d70283131da124ae3c523213b208748529a0c7b7ba44c1b4162217a344f4c26f  evidence/normal-run.json
2113035de8a6c0fb1e2dd183fcc5e5e925dc37cde9bc5a42ef5a713afa345b8b  evidence/race-run.json
ff11531a134faa0b057206e6322930a1bdff62393247e815540161214a496b7c  evidence/traces/B-PROCESS-0-before-key-ready-save.json
5ce83fa8e024cf2f0eee23dc3694b32614b2ac3c791149d1b52dee39df2e0f21  evidence/traces/W-LOST-BEFORE-READINESS-EXPIRED-control-0.json
b5297f228abfd0291ac3b7f9bff2f13210d095a562b0aaa7e845ecd71bc0ba5a  pkg/chain/ethereum/frost.go
fcfe1cadca1ea51ed73d3e91007d065afa713b9f540a181b33801770fb4904c2  pkg/frost/integration/failures_test.go
6e6a8031f09b5f62730518f1ddbfd5a557d920917669164e0c4f20eedab21f98  pkg/frost/integration/review_boundaries_test.go
```

Defect-evidence pins, relative to `evidence/implementation-defect-ready-return/`:

```text
5a27c6982b2d137d6a9d548293ac9b8edd032cd87650483ad5ffc2855a1c26b4  FINDING.md
5ef71d1bf625739e3e7e5c077e40e5d624c7fb59f06120d5c61813d5cb213443  executor-before.go.txt
f19e7e717a1d6160a69d0220c8aa7bb41d919341455ab7d7c10fe889c50a5bed  executor-after-drain-before-sharing.go.txt
69be027dee68ab461f094c65b1be5076f797bf8bf49df50a3a2b78d05bd600b9  repair.patch
39da3f54aac01b83b2a0c1cb45f036151c7498eb698d74f5a25891a7870bfdf1  failing-regression.log
e40254e586ed9bcca8dd1ab6c62943f226a05e547a366ecd8140e5406396480b  first-repair-unshared-observer-failure.log
fd77babbcf4b223081f7484422cf1ca97ab5cfd9d89c71e84375a903312ad57a  passing-regression.log
c5c92f66dc29fd8ec12ea8ff2c62f16c522efb1d27b64bd7a3c65cbd31b68969  trace.json
```