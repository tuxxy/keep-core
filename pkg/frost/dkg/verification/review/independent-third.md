**Verdict: not yet accepted.** One material implementation defect remains; final matching normal/race replay and a hash/report refresh alone cannot close it.

**Medium — delayed preapproval can still cause false quarantine.**

In [executor.go:280](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/executor.go:280), the monitor checks `approvalGeneration` under `e.mu`, then unlocks at line 283. It later reacquires the mutex and classifies the snapshot at line 310.

A legal ordering remains:

1. Monitor reads a canonical preapproval snapshot.
2. Monitor checks generation before the callback publishes approval: `overlapped == false`.
3. After the monitor unlocks, the callback installs approval knowledge at lines 431–441.
4. Monitor reaches line 310 with its older snapshot and returns `ErrQuarantined`.

No approval removal occurred. The repaired failure handling can then persist this false quarantine.

The current coverage misses this ordering:

- [outcome_test.go:170](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/outcome_test.go:170) installs callback knowledge **before releasing the first View**, ensuring the overlap check detects it.
- [main.rs:433](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/verification/model/src/main.rs:433) treats `FinishWatch` reconciliation/classification atomically.
- [replay_test.go:641](/Users/tux/threshold/integration-worktrees/V-03-verification/pkg/frost/dkg/verification/replay/replay_test.go:641) holds the captured RPC response, without a seam between the production overlap check and classification.

**Required:** make the decision to reconcile or classify coherent with shared approval knowledge, and add deterministic coverage for publication in this remaining gap. This is a source-derived finding, not an executed reproduction.

The other requested repairs are present:

- Phase 21 includes live actions; trace export validates offered actions.
- The loss witness now requires a crashed node without its key. The retained witness kills node 1 at phase 21 before key installation.
- Delayed preapproval capture and one-read reconciliation exist in model and subprocess replay, subject to the gap above.
- Joined quarantine+cancellation survives success handling.
- Returned `WalletRecord` is compared with the reopened record; normal exits and explicit kills check complete descriptor/hash identity and complete `KeyReady`, including local references.
- Results record ten witnesses and nine passing targeted controls.

I independently verified all **10 production and four archived-tooling SHA256 entries** in the durability join. Its explicit conditional scope is acceptable; neither production funding nor physical host power-loss evidence is required here.

No tests or campaigns were run, and no files were changed.

SHA256 pins, relative to `pkg/frost/dkg/`:

```text
53861a545f3c77ec6ea7f98ff8e2659f860ac35519fa06e98f6293d15685f098  executor.go
465b27a14102e279d5a0465202430af3d72c1caa3e25b076a1fce286f8d25dfc  outcome_test.go
81a2d0e16b54bd5e10bc7f412332f5ede4d88499b52ae17cb99082e070fcc809  verification/model/src/main.rs
216e0f33ae02e75386cd0b20a7d0fe1efd359b0cd4aaf47b8fce831e4b99fb52  verification/replay/replay_test.go
f0368b8ddf9514ae8ef9df28ee2cf1dc22301ef39257a3893ea728d33453a424  verification/review/independent-followup.md
eba90f0d89a74599f267ed01ef932569e8bf2ae8b71ac65d64e59f62ff68e30a  verification/review/responses.md
```