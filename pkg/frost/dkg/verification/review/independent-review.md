**Independent review disposition: V-03 is not ready for milestone exit.** This bounded, read-only review covers the current uncommitted continuation, not checkpoint `d97974884`. No files were edited; no tests, mutation campaigns, commits, pushes, or activation actions were performed. No applicable `AGENTS.md` was found in the worktree or checked ancestor paths.

The reported real-fixture suite and `B-ALL-NINE-HOOKS` success are useful evidence, but do not resolve the following verification defects.

1. **High — Most safety predicates are satisfied by construction, and their controls explicitly manufacture violations.**  
   `model/src/main.rs:194–215, 262–312, 324–330`.

   ONE-DESCRIPTOR, INCONSISTENCY-QUARANTINE, EXPIRY-TOMBSTONE, FRESH-EPOCH-DISJOINT, and SUBMITTED-NOT-TERMINAL primarily test bits in `bad`. Those bits are set inside the corresponding weakening branches. For example, resetting `tick` explicitly sets the expiry failure bit; terminating on `Foreign` explicitly sets the submitted failure bit. The claim model cannot independently discover those failures through its ordinary state.

   ONE-DESCRIPTOR does not retain descriptor identities for recognized approvals, local keys, certificates, and wallet records. Its `recognized` field is a node bitmask. Consequently, the stated descriptor-agreement predicate is not actually evaluated.

   **Required:** derive invariants from independently recorded protocol state/history. Controls should remove a real guard or change behavior without directly declaring the predicate violated. Retain the exact-one-target control check after this repair.

2. **High — Important concurrent operations are collapsed or missing from the model.**  
   `model/src/main.rs:232–260, 262–275`; `executor.go:112–182, 224–259, 501–527`.

   `Certify` atomically performs chain certification, local readiness, and completion. The executor instead calls `Ready`, reconciles failures, rereads approval, and durably saves the wallet. There is no modeled crash or competing observation between these operations. The store join covers store durability; it does not establish this chain-to-local ordering.

   Likewise, `PostKeyRead` combines approval checking and reload. Readiness collection performs its own periodic `checkedApproval` calls, beyond the modeled monitor action.

   The observer state also differs: model `Observe` sets `seen`, while the executor’s `approval-seen` hook belongs to a callback-local `observedApproval`. The monitor separately maintains `approvalSeen`. Model `Watch` does not record a successful first approval observation. These are materially different concurrent histories.

   **Required:** represent the separate observers and externally interruptible operations, including certificate acceptance versus durable local readiness. Supply an explicit correspondence argument for any remaining atomic abstraction.

3. **High — Replay does not compare the complete declared outcome/state contract.**  
   `replay/replay_test.go:51–60, 797–825, 854–859, 861–1050`.

   `Expect` is decoded but never checked. Terminal outcomes are inferred from selected phase values. `checkBoundary` checks key presence, reload status, and an optional descriptor; `checkWorld` checks chain mode, certification, closure, height, and one wallet-ID relation.

   Submitted state, observed/recognized approval, attestation membership, local ready state, and identity history are not comprehensively compared. Terminal `WalletRecord` values returned by `finish` are discarded. Thus a matching error string can pass despite incorrect persisted quarantine/closure state, and a successful result can pass without checking its final durable wallet record.

   **Required:** compare the modeled outcome explicitly, define an observable projection for every claimed state field, and reopen/check terminal durable records. Report unsupported comparisons as limitations, not full matches.

4. **High — Fresh-epoch replay does not establish tombstone retention or rejection of identity reuse.**  
   `model/src/main.rs:289–307`; `replay/replay_test.go:377–395, 989–1031`.

   The model assigns fresh identities `[2; 4]` and remembers old numeric identities. Replay independently selects predetermined distinct candidates and checks that they differ. Neither exercises an attempted reuse against retained tombstones.

   `world.reset` deletes the old chain wallet, ID, and closure state. The old local-wallet check permits `ErrMissing` and checks only the Snowfall descriptor when a record exists. Its comment says the closed chain tombstone is retained in the report, but the returned report contains no such tombstone.

   **Required:** retain old chain identity/Q/label tombstones across epochs, reject reuse explicitly, and check required old local records after reopening. Compare actual attempt identities, not only manually chosen session inputs.

5. **Medium — The synthetic engine omits the worker completion dependency needed for the lost-seat witnesses.**  
   `replay/replay_test.go:221–243`; `model/src/main.rs:228–231, 335–351`; governing keep-core specification §4.3.

   Once acceptance returns, each synthetic engine saves its key independently. It does not represent SNOWFALL’s full-roster ready-message dependency before durable KeyReady. Model `Attest` represents the later C-03 statements.

   Therefore `W-LOST-AFTER-STATEMENT` demonstrates survival after a C-03 statement, and `W-LOST-BEFORE-READINESS-EXPIRED` merely requires a dead process plus closure. These predicates do not themselves establish the named worker-readiness failure scenarios.

   **Required:** distinguish worker ready messages from C-03 attestations. Either model the dependency or explicitly assign these witnesses to the pinned real-worker tests, identifying exact failure boundaries and results.

6. **Medium — V-02 join freshness remains unresolved under the join’s own rule.**  
   `DURABILITY-JOIN.md:3–9`; provider `STATEMENTS.json`.

   I compared the union of pins in `S-K02-HOST-2` and `S-K03-STORE-1` using Git blob hashing. All listed production subject files match, including store, wallet, status, domain, engine, and DKG types. However, the current worktree differs for:

   - `pkg/frost/store/verification/model/src/lib.rs`
   - `pkg/frost/store/verification/model/src/main.rs`
   - `pkg/frost/store/replay_test.go`

   The join currently says **any provider pin mismatch blocks the join**.

   **Required:** retain the full comparison and resolve these tooling mismatches explicitly. If the intended provider is the archived evidence/tooling, validate that exact provider and distinguish its tooling pins from the current consumer’s production subject pins. Do not describe the entire pin set as matching.

7. **Medium — Replay’s stated subprocess deadline is not an enforced wall-clock bound.**  
   `replay/replay_test.go:294–295, 315–329, 624–660`; `run.py:25–27`.

   The child’s 15-second context does not interrupt `scanner.Scan()` at a paused hook. Children use `exec.Command`, without a parent-enforced lifetime. Ordinary deferred cleanup kills them, but a Go test timeout can terminate the parent without running those defers. The outer Python timeout also does not explicitly terminate the descendant process group.

   **Required:** enforce child lifetime independently of cooperative context cancellation, make hook waits cancellation-aware, and terminate/reap descendants on timeout. This is a harness reliability issue, not evidence of a production custody defect.

The nine hook placements follow the documented broad ordering, and separating statements-sent from before-certificate-submit corrects the identified conflation. Host-loss traces are appropriately labeled partial, and weakened traces stop before divergence. Those improvements should remain.

Funding success remains **later-capability blocked**. The current `Fund` action has no ordinary successful transition and its weakening sets a failure bit; it cannot establish coverage of funding, active selection, movement targets, or deposit instructions.

Before exit, retain author responses to these findings, complete the repaired full replay and appropriate race checks, and refresh correspondence, source pins, per-predicate results, and the safety statement. Historical checkpoint reports are not current evidence. This review does not wait for author feedback.

SHA256 pins below identify the reviewed continuation. The three principal code hashes were rechecked at the end and were unchanged. Paths are relative to `/Users/tux/threshold/integration-worktrees/V-03-verification` unless absolute.

```text
23cfdb0bf06cc9c75e85b89537f4d737a0b00f231def4e709d779ed13cd7b880  pkg/frost/dkg/executor.go
b3b24bc9405405397b615b51c02278fe230d62db73b66f28ca0325d5b64f9a1e  pkg/frost/dkg/verification/model/src/main.rs
9b50050b9bbebe8e34dc82f790ae5e442fecf3716d12b0d8dc20d5f59d7b142f  pkg/frost/dkg/verification/replay/replay_test.go
f502f58265f49dcadf1695fd8fd0b599bf393c313cfeb779837a34604f38af67  pkg/frost/dkg/verification/EXIT-SCOPE.md
7832290705a308ffe66c4259a88e30aadd8c24c76869d1da5d124a0eabe6de2d  pkg/frost/dkg/verification/DURABILITY-JOIN.md
db55692e5458e782d7e3703738627adc22a4495b823069c9e5e8cc4635af67da  pkg/frost/dkg/verification/run.py
9b00e5372e792399c9c71bb60d93d478ea93460ebc6c31021909324d33688ba9  /private/tmp/kc-ver-integration-current.md
f659067de053ad23ffd7e48851416333ad96a492b5e12cb7bf962a3b1163039c  /Users/tux/threshold/Research/SNOWFALL/FROST-migration-spec/keep-core-integration-spec.md
```