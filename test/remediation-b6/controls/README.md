# Batch-6 controls

Each control is a `frostNodeStart` binding (`pkg/tbtc/frost_node_b6_binding_test.go`), applied in a scratch worktree by `run-control.sh`, never in the candidate tree:

```
test/remediation-b6/controls/run-control.sh <scratch worktree> <patch> <absolute log path> '<-run regex>'
```

The scratch worktree needs `solidity/ecdsa/build` (and `node_modules`) like the candidate. None of these bindings is a fix: a fix binds the seam to `tbtc.Initialize` and nothing else.

## Controls at the tests' head (the guarded code does not exist yet)

| Patch | What it binds | Expected |
| --- | --- | --- |
| `b6-legacy-only-node.patch` | today's `tbtc.Initialize`, FROST settings ignored | every FROST case fails at its own named DKG assertion (`#26 DKG: ...` or `#26 <case> precondition: ... no node ran the FROST DKG ...`); `TestFrostUnconfigured_NodeUnchanged` passes (base behaviour) |
| `b6-library-dkg.patch` | today's node plus a goroutine running the K-03 library DKG executor (positive control for the DKG oracle and the world after it) | the DKG cases pass; every later case fails at its own sweep or dispatch assertion (`#26 incoming sweep`, `#26 restore`, `#38 ...`, `#26 ... precondition: no node swept`); the rule-5 case fails at `rule 5: ... node initialisation failed` because the binding opens the FROST store unconfigured |
| `b6-unconfigured-frost-read.patch` | today's node plus one FROST-only Bridge read (`frostRegistry()`) when FROST is unconfigured | rule 5 fails at `legacy RPC surface: ... selector 0xddba6dc4 is not in its ABI` |
| `b6-unconfigured-opens-store.patch` | today's node plus `store.Open` on an empty FROST store configuration | rule 5 fails at `rule 5: with FROST unconfigured, the node initialisation failed` |

## Removal controls to run on the fix
The guards below live in code the fix writes, so their removal controls are patches against that code. The reviewer writes each one against the fix's head and records the result; each must fail at the named assertion.

| Guard (issue) | Removal | Must fail at |
| --- | --- | --- |
| leader by unique operator (D2) | FROST wallets' coordination leader = the operator of seat 1 | `D2 window N: the FROST wallet's coordination message was sent by operator(s) ...` |
| dispatch on base windows (#26) | the incoming sweep runs on every new block or a timer, not from the window's checklist | `#26 incoming sweep: a node broadcast sweep ... before any coordination window` or `... no node sent a base coordination message ...` |
| FROST wallets in every window (#26) | coordinate FROST wallets only in windows with a pending sweep | `#26 window N: no node sent a base coordination message for the FROST wallet` |
| restore at startup (#26) | skip loading FROST signers when the node starts | `#26 restore: after every node restarted ...` |
| multi-seat (#26) | run only the first local seat of an operator | `#26 DKG: epoch N's FROST wallet did not reach ReadyUnfunded` (1,1,2) |
| per-wallet isolation (#38) | return a wallet's restored-record or DKG error from node initialisation or the dispatcher | `#26 node 2: the node initialisation ... failed` or `#38 <point>: a node exited ...` |
| DKG does not block signing (#38) | hold the wallet-action lock while any DKG runs | `#38 failed DKG: ... did not settle` |
| no second spend (#38) | re-plan the incoming sweep with a fresh fee after a restart | `#38 signed-before-proof: 2 distinct transactions spend the interrupted plan's input` |
| reservation is not fatal (#39) | treat "main reserved" as a node error | `#39: a node exited ...` or `#39: with an active redemption reservation on wallet A, no node swept wallet B's incoming output` |
| rule 5 | any FROST read or store open while unconfigured | the two rule-5 assertions above |
