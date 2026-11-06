# V-03 re-pin at `integration/snowfall-base` 925103a6 (6 November 2026)

Written before any model or replay change. Records which production changes
since the V-03 pin (`02e798de2`) the model already covers, which it does not,
and the predicate clauses, witnesses and weakening added for the new
behaviour. Everything marked **proposed, not in KC-VER** is a verification
record for the owner to accept or reject; it is not a spec decision.

## 1. Changes since the pin

`git log 02e798de2..925103a6` over the pinned files touches three of them.

| File | Commits | Inside the model? |
| --- | --- | --- |
| `pkg/frost/dkg/executor.go` | `9e654b477` startup restore | Run path: yes, unchanged (see 1.1). `Restore`: **no**, new behaviour (section 2). |
| `pkg/chain/ethereum/frost.go` | `b4a81afe2`, `ac160ffb0` | Outside: the production chain adapter's send mechanics. The model's chain transactions are atomic and their outcome is read back (`reconcileWallet`, postcondition read); the adapter still returns only after a receipt or an error. Exercised by the real-contract suites, not by the controlled adapter. |
| `pkg/frost/integration/integration_test.go` | `8a8b01e6c`, `a43981d08`, `f37c540d8`, `1e600d142` | Test fixture only (K-07/K-08/C-10 deploy modes, external host processes). The default K-03 path is unchanged. |

### 1.1 Run is behaviour-preserving

`9e654b477` splits `Run` into `prepare`, the DKG call, and a shared tail
`ready(ctx, view, key, locals, fail)`. On the `Run` path:

- `prepare` performs the same reads and checks in the same order and returns
  the same errors.
- `ready` is called with `fail = finishFailure`, so every failure is handled as
  before.
- The new branches are dead on `Run`: `restoring` is false, so collection is
  never skipped and `errCertifiedElsewhere` is never returned; `collect`
  returns at least `ReadySeats > 0` votes, so `len(votes) > 0` holds; the wallet
  is `RegisteredPendingReady` before the tail, so the `SaveWallet` guard always
  saves.
- `domain.NewAttempt` became `e.Journal.Domain().NewAttempt`, the same value.
- All nine VD-06 hooks and the six added test-only hooks fire at the same
  points.

Check: the 77 unchanged traces replay on 925103a6 with matching outcomes
(baseline run recorded in the evidence directory) before any tooling change.

## 2. New behaviour: `Executor.Restore` (node startup)

Called by `tbtc.StartFrostNode` in a fresh process for one journal epoch.
It never reruns the DKG.

| Go step | Effect |
| --- | --- |
| `Journal.DKGStatus` | `DKGSeatsLost` returns at once, nothing written. Not `DKGKeyStored`: error. |
| `LoadWallet` | quarantined or `Closed`: returned unchanged. Not `RegisteredPendingReady` / `ReadyUnfunded`: error. |
| `LoadKey`, `Parameters`, `Selection`, `prepare` | rebuilds the request from the chain selection and the saved seats. |
| monitor and receiver start | in-memory approval knowledge (`approvalSeen`, observed ID/hash) starts **empty**; the descriptor is the saved one. |
| `checkedApproval` | compares the chain view with the **saved** record: mismatch quarantines, closed or past-deadline expires (as `finishFailure`). |
| `ready` tail | reload in a fresh worker (`after-key-reload`), `before-readiness-collect`; collection is skipped if the restore read saw the wallet certified, and stops with `errCertifiedElsewhere` if a later collection read sees it certified; then the certificate path or straight to the postcondition read; `SaveWallet` only if not already `ReadyUnfunded`. |
| failures | `finishRestore`: expiry and quarantine are recorded as `Run` records them; any other error leaves the saved wallet as it was. |

Hooks on the restore path: `after-key-reload`, `before-readiness-collect`,
`before-certificate-submit`, `after-certificate-submit`,
`after-readiness-postcondition`, `after-wallet-ready-save`. The other hooks
(`candidate-ready`, `before/after-publish`, `approval-seen`,
`before/after-finality-reread`, `before-key-ready-save`, `after-key-ready`,
`after-post-key-read`) cannot fire: there is no DKG, publication or acceptance
on this path.

### 2.1 Correspondence (added rows)

| Finite operation | Go | Notes |
| --- | --- | --- |
| `Restart(n)`, key absent | `Restore` returns `DKGSeatsLost` | phase stays dead; reopened store, status and wallet compared unchanged |
| `Restart(n)`, key present | `Restore` up to the `after-key-reload` pause: status, wallet, key loads, `checkedApproval`, `ReloadKey` | atomic step (see 2.3); outcome quarantined (chain not the saved approval), expired (closed or past deadline and uncertified) or paused after reload |
| `BeginReadiness`, `Attest`, `CollectionRead`, `BeforeCertificate`, `Certify`, `Postcondition`, `PersistReady`, `Complete` on a restored node | the shared `ready` tail with `restoring` set | `Postcondition` is offered straight from `before-readiness-collect` when the restore read saw the wallet certified; a `CollectionRead` that sees it certified goes straight to `after-readiness-postcondition` |
| `Watch(n)` on a restored node | restored monitor with empty in-memory approval knowledge | a removed approval (no approved ID in the view) is not compared with the saved record; at result-deadline height the monitor's `ExpireDKG` is refused and the process ends with an unclassified error |
| `ProcessCrash`/`HostCrash` on a restored node | kill at a restore-path hook | the restored process is not restarted again (bound) |

### 2.2 Bounds added

One restart in total, in the first epoch, only from a dead process. (A
first draft allowed one restart per node in every epoch; its claim run
reached 107 million states without finishing inside the 120 s deadline and
was narrowed before any result was used; logs kept in the evidence
directory.) Nodes, seats, epochs, descriptors, ticks, finality and deadline
unchanged.

A restored monitor's read of a removed approval at or after the
result-deadline height (`pre_tick + tick >= 3`) is outside the bound
(finding F-V03-R2, section 7). Below that height it is modeled and replayed.
A restored node's readiness collection counts its own seats and only the
remote statements sent after its restart: the authenticated bus is not
durable across a process restart, and the harness drops statements queued
for the killed process (conservative: it can only remove votes).

### 2.3 Abstractions added

- `Restart` with a key is one step up to the `after-key-reload` pause. Inside
  it are reads and the local reload only; Go exposes no hook before
  `after-key-reload`, so the replay cannot schedule chain actions inside it
  either. The reload is local (it reads no chain state and writes no durable
  state), so moving it next to the read changes no reachable outcome.
- `CollectionRead` that sees the wallet certified on a restored node includes
  the postcondition read. Between the two reads only a removal or
  replacement can change the outcome, and both reads then quarantine.

## 3. Predicate clauses (proposed, not in KC-VER)

KC-VER section 3 V-03 is unchanged in rev 02. The clauses below say how the
existing predicates read on the restore path.

**P-K03-READY-ONLY-RELOADED, restore clause (proposed, not in KC-VER).** A
readiness statement sent by a restored process is preceded by a reload of
the completed key in that same process (KC-SF-V3 4.2 "reloads in a fresh
process", C-03). Model: `restore_attest & !restore_reloaded == 0`, both
masks written only by the restored process. A seat restored with no key
(`DKGSeatsLost`) never attests.

**P-K03-INCONSISTENCY-QUARANTINE, restore clause (proposed, not in KC-VER).**
A restored host knows its approval from its durable wallet record (the
record `Restore` loads and `checkedApproval` compares against). A removed
approval that the restored host's monitor reads is therefore an observed
inconsistency, exactly as for a host whose callback or monitor saw the
approval in memory. Model: `Watch(n)` on a restored node whose view has no
approved wallet (`chain == 2`) records the observation in `inconsistency`;
the quarantine itself follows the Go guard (the in-memory knowledge only).
This is a statement of the existing predicate on a new path, not a new
property; the open question is whether the owner agrees that the durable
record counts as knowledge.

**P-K03-ONE-DESCRIPTOR, P-K03-NO-EARLY-FUNDING, P-K03-EXPIRY-TOMBSTONE.**
No new clause: the restore path writes no descriptor, key or approval
identity, reaches funding only through the unchanged `Fund` action, and
reads the deadline anchored at approval (`Restore` never resets it).

**P-K03-FRESH-EPOCH-DISJOINT.** No new clause: `Restore` makes no DKG claim
and derives no DKG attempt, descriptor, Q or label; its reload attempt is a
fresh `sign` attempt with a random session. Restore of an older epoch's
journal after `Fresh` is outside the bound (one restart in the current
epoch).

**P-K03-SUBMITTED-NOT-TERMINAL.** No new clause: `Restore` requires
`DKGKeyStored`, which exists only after acceptance completed; the restored
monitor never ends the acceptance watcher (there is none).

## 4. Witnesses and control (proposed, not in KC-VER)

Witnesses (`sometimes`), all must be discovered and replay:

- `W-RESTORED-READY`: a restored node attests after its reload and both nodes
  reach ready-unfunded.
- `W-RESTORED-CERTIFIED-ELSEWHERE`: a restored node reaches ready-unfunded
  without attesting, because the wallet was already certified.
- `W-RESTART-LOST-EXPIRED`: a node restarted without a key stays lost and the
  wallet expires uncertified.
- `W-RESTORED-QUARANTINED`: a restored node quarantines after the approval
  changed.
- `W-RESTORED-EXPIRED`: a restored node closes its wallet at the deadline.

Weakening 10 (target P-K03-READY-ONLY-RELOADED): the restored process skips
the reload and goes straight to `before-readiness-collect`. It must break
that predicate and no other claim predicate.

Weakened runs pass when their target is discovered and every other
discovery is one the claim model itself reports (a claim counterexample is
not a weakening artifact).

## 5. Replay additions

- Restore-mode subprocess (`TestV03Node` with `Restore: true`) calling the
  real `Executor.Restore` with the same controlled chain, bus, durable store
  and test engine.
- `Restart` step: reset the killed node's bus queue and per-process gates,
  start the restore subprocess, compare the outcome (`lost` maps to dead),
  the pause hook, and the reopened durable state.
- World tracks reloads and readiness statements sent by restored processes
  and compares them with the model's restore masks.
- Representative boundary traces on the restore path: a restart after each
  key-bearing crash point, and a restored-path return followed by a removed
  approval (`B-RESTORE-READY-RETURN-REMOVAL`, the restore counterpart of
  `B-READY-RETURN-MONITOR-QUARANTINE`).

## 6. Limits (chosen before running)

Model: Stateright 0.31.0 BFS, one thread, 120 s per run, 900 s per command,
20 GiB RSS. Go: `GOMAXPROCS=4`, 600 s test timeout, 900 s per command,
replay subprocess 15 s, per-case wait 10 s. No Kani, mutation, fuzzing,
Docker or CI.

## 7. Findings during the re-pin (classified before repair)

**F-V03-R1, implementation defect (proposed clause of
P-K03-INCONSISTENCY-QUARANTINE).** `Restore` starts the monitor with empty
in-memory approval knowledge and never seeds it from the saved wallet
record. A restored host whose monitor reads a removed approval therefore
does not quarantine, and from `after-wallet-ready-save` returns
`ReadyUnfunded`. The same read on a `Run` host quarantines
(`B-READY-RETURN-MONITOR-QUARANTINE`). Claim counterexample
`P-K03-INCONSISTENCY-QUARANTINE-claim` and boundary trace
`B-RESTORE-READY-RETURN-REMOVAL` replay with matching outcomes, normally
and under the race detector: the Go executor does what the model says.
Not repaired here. Depends on the owner accepting the restore clause in
section 3.

**F-V03-R2, model defect.** Model run 1 said a restored monitor's read of a
removed approval at result-deadline height ends the process with an
unclassified error and no durable change. Go quarantined the record: the
refused `ExpireDKG` cancels the executor context, the paused main goroutine
continues, and from `after-wallet-ready-save` `finishSuccess` routes the
observer error to `finishFailure`, which quarantines every non-expiry error.
From other positions the next context-aware step decides. Repair: that read
is outside the bound (section 2.2). Counterexample and logs:
`finding-model-defect-restore-deadline/` in the evidence directory.

**Pre-existing, not caused by this change (note only).** The controlled
adapter's removed-approval view keeps a result deadline. A `Run` host still
in phase 1 that never saw the approval reads it like any unseen view; at
result-deadline height its monitor would try `ExpireDKG`. The model has no
transition for that read (it is a stutter there) and no trace reaches it.

**Harness note (not a V-03 finding).** The first real-contract race run
(`integration-attempt-1-parallel-default/` in the evidence directory) failed
three `TestSeatRestartAfterKill` cases with "host process did not die by
SIGKILL": at `GOMAXPROCS=4` under the race detector, nine parallel cases
(each with Anvil, three workers and a host process) starved the first DKG
before its kill point, so the restart under test never ran. The runner now
passes `-parallel=2` to the real-contract suite; normal and race then pass.
