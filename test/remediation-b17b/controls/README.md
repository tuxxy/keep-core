# Batch-17b controls (tuxxy/keep-core#151)

Run these in a scratch worktree only (`B17b-tests/keep-core-ctl`, detached at `1e3e6ea6`, never pushed), with `SNOWFALL_WORKER` and `SNOWFALL_WORKER_SHA256` set (the e2e cases):
`run-control.sh <scratch> <patch or none> <log>`.

The script copies in `pkg/tbtc/frost_node_b17b_claim_test.go` and `pkg/tbtc/frost_node_b17b_success_test.go`, applies the patch to `pkg/tbtc/frost_node.go`, runs the 8 cases, then reverses the patch and removes the copies. All patches are made against `1e3e6ea6`.

| Patch | Kind | Expected result |
| --- | --- | --- |
| `none` (integration code: release only before `ExecuteFrostDKG`) | today's code | the 5 `PreJournalRunErrorReleasesEpochClaim` cases fail at `... after the <fault> error inside Run before the journal claim, no later block started epoch 100's DKG executor again: the epoch stayed claimed`; the other 3 pass |
| `b17b-fix`: release on every error return, never on success | positive, fix 1 | 8/8 pass |
| `b17b-fix-status`: on an error, release only while `DKGStatus` returns `ErrMissing` | positive, fix 2 | 8/8 pass |
| `b17b-wrong-release-after-success`: release unconditionally | wrong fix (b) | `SuccessfulDKGKeepsEpochClaim` fails at `... after a successful DKG, later blocks started 1 restores of epoch 100's live wallet, want 0`; the other 7 pass |
| `b17b-wrong-rerun-after-claim`: fix 1, plus one direct rerun of `executeDKG` on an error | wrong fix (c) | `JournalClaimedErrorNeverRerunsDKG` and `KeyStoredErrorNeverRerunsDKG` fail at their named assertions (`... started 2 times, want exactly 1`); the 5 pre-journal cases stop at the harness check `the first attempt started 2 executors before any later block`; success passes |
