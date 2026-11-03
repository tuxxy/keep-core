# Batch-6 controls

Each control is a `frostNodeStart` binding (`pkg/tbtc/frost_node_b6_binding_test.go`), applied in a scratch worktree, never in the candidate tree. None of them is a fix: a fix binds the seam to `tbtc.Initialize` and nothing else.

- **Stub and rule-5 controls:** apply them on this branch with `run-control.sh`:
  ```
  test/remediation-b6/controls/run-control.sh <scratch worktree> <patch> <absolute log path> '<-run regex>'
  ```
- **Requirement:** the scratch worktree needs `solidity/ecdsa/build` (and `node_modules`), like the candidate.

## Stub and rule-5 controls (this branch)

| Patch | What it binds | Expected |
| --- | --- | --- |
| `b6-legacy-only-node.patch` | today's `tbtc.Initialize`, FROST settings ignored | every FROST case fails at its own named DKG assertion (`#26 DKG: ...`, or a `#26 ... precondition: ... no node ran the FROST DKG ...`); `TestFrostUnconfigured_NodeUnchanged` passes |
| `b6-library-dkg.patch` | today's node plus the K-03 library DKG in the binding | the DKG cases pass; every later case fails at its own sweep or dispatch assertion; rule 5 fails at `rule 5: ... node initialisation failed` (the binding opens the FROST store unconfigured) |
| `b6-unconfigured-frost-read.patch` | today's node plus one FROST-only Bridge read when unconfigured | rule 5 fails at `legacy RPC surface: ... selector 0xddba6dc4 is not in its ABI` |
| `b6-unconfigured-opens-store.patch` | today's node plus `store.Open` on an empty FROST store configuration | rule 5 fails at `rule 5: with FROST unconfigured, the node initialisation failed` |

## Correct-shaped probe binding and removal controls (review: run the never-run steps)
`b6-probe-binding.patch` is a correct-shaped stand-in for the fix. It applies on this branch merged with tuxxy/keep-core#106 (batch 3), and it is the batch-6 review's probe, extended to sweep.

**What it binds:**
- today's `tbtc.Initialize`;
- the library FROST DKG;
- base `coordinate`/`getSeed`/`getLeader` for every ready FROST wallet, keyed by its 20-byte Bridge key, with the real `tbtcpg` generator (#106 routes a FROST label's moved-funds sweep);
- on a validated `MovedFundsSweepProposal`: the K-07 incoming-sweep plan, `movement.Mover` signing in rounds derived from the window, and a broadcast through the node's Bitcoin chain;
- wallets restored once before the binding returns, as base loads its wallets in `newNode`.

**Production change in the scratch tree only:** a 20-byte label override in `coordination.go` (the review's).

**Run it:** apply the patch on a scratch worktree, then `go test ./pkg/tbtc -run '<cases>' -parallel 2` with `RM-tests-b6/env.sh` sourced.

The removal controls are environment switches of the probe. Each must fail at the named assertion. Results are in `K-09-restart/TESTS-BATCH6.md`.

| Switch | Removes | Case | Must fail at |
| --- | --- | --- | --- |
| `RV_B6_SEAT1=1` | leader by unique operator (leader = seat 1's operator) | `IncomingSweepByLeader` | `D2 window N: ... sent by operator(s) ...` |
| `RV_B6_NODEDUP=1` | operator dedup (seat-weighted leader) | `IncomingSweepByLeader` | `D2 window N: ...` |
| `B6CTL_TIMER=1` | dispatch from base windows (sweep on a timer) | `IncomingSweepByLeader` | `#26 incoming sweep: a node broadcast sweep ... before coordination window N` |
| `B6CTL_NORESTORE=1` | restore at startup | `RestoreAtStartup` | `#26 restore: ...` |
| `B6CTL_ONESEAT=1` | multi-seat DKG (first local seat only) | `DKG/1,1,2` | `#26 DKG: ...` |
| `B6CTL_FATAL_RECORD=1` | per-wallet isolation (an unready local record fails node start) | `CrashPoints/certified` | `#26 node 2: the node process exited during startup` (or `... the node initialisation ... failed`) |
| `B6CTL_DKG_LOCK=1` | DKG and signing independent (signing waits for any DKG) | `FailedDKGDoesNotBlockSweep` | `#38 failed DKG: ...` |

**Not killable by one probe switch:**
- **"No second distinct spend":** the K-04 signing executor's mempool-inclusive unspent check also prevents a re-planned sweep, so removing a planner guard alone leaves the assertion passing.
- **#39's reservation steps:** the probe implements no FROST redemption dispatch (K-05 attestation and reservation), so `ReservationOnOtherWallet` stops at `#26 redemption: the nodes did not reserve ...` on the probe. Its world steps are exercised separately; see TESTS-BATCH6.md.
