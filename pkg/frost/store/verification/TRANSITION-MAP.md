# Transition-to-source map: K-02 host model to `pkg/frost/store`

Model: `model/src/lib.rs` (crate `k02-host-model`). Source: `store.go`,
`fence.go`, `lease_unix.go` at keep-core `18e63b378b3c3b5ee24f8cd7bf909631f1f00454`
(`store.go` blob `6088d55c8`). Line numbers refer to that revision. Replay:
`../replay_test.go`. This map is human review evidence (KC-SF-V3 §8.1); it is
not a mechanical refinement proof.

## 1. State correspondence

| Model | Implementation | Notes |
| --- | --- | --- |
| `Fence.head: Point{seq, uid}` | authority head `store.Point{Sequence, Digest}` (`fence.go:9`) | `uid` stands for the SHA-256 digest; the random seal nonce (`store.go:266-272`) makes real digests unique per commit |
| `Fence.lease: Option<proc>` | exclusive lease from `Fence.Acquire` (`fence.go:20-22`) | in tests: flock on `<fence>.lock` (`store_test.go:28-38`) |
| `Image.local_lock` | `owner.lock` flock (`store.go:108-125`, `lease_unix.go:10`) | released by `Close` (`store.go:466`) or process death |
| `Image.identity` | `<root>/identity` (`bindIdentity`, `store.go:671-707`) | one storage identity in the model |
| `Image.journals[]` | `<root>/<seq>-<digest>.journal` files (`store.go:280-282`) | `content_durable` after `syncFile` (`:374`); `name_durable` after `syncDirectory` (`:395`) |
| `Journal.corrupt` | digest or AEAD check fails at open (`store.go:179-191`) | the model never produces a plausible different snapshot (assumption A-04) |
| `Snapshot.claims` | `snapshot.Claims[hex(id)]` (`store.go:60`, `:578-580`) | global per storage identity, not per domain |
| `Snapshot.dkg[d]` | `snapshot.DKGs[prefix(d)]` (`:61`, `:583-584`, `:600-601`) | per domain |
| `Snapshot.slots[0]` | `Records[recordKey(R)]` for one record ID (`:510-526`) | value = payload bytes |
| `Snapshot.slots[1]` | `Locks[slot]` + `Records[key]` for one `(seat, attempt, kind)` (`:505-507`, `:538-539`) | value = which record ID holds the slot |
| `Proc.open` | a live `*Store` returned by `Open` | |
| `Proc.poisoned` | `Store.poisoned` set by `fail` (`:289-295`) | `check` returns `ErrQuarantined` (`:297-299`) |
| `Proc.uncertain` | no field; the handle is poisoned after an `Advance` error (`:404-406`) | model-only distinction between a rejected and a lost-response update |
| `Proc.point`, `Proc.state` | `Store.point`, `Store.state` (`:72-73`) | updated together after the fence (`:410-411`) |
| `Proc.live_claims` | `Store.active` (`:75`, `:609`, `:611`) | blocks `Close` (`:461-463`) |
| `Proc.commit: Commit{stage}` | the stack frame of `commit` (`:340-416`) | stages below |
| `History.acked_*` | observer only | what callers were told |

## 2. Action correspondence

| Model action | Implementation | Observable result |
| --- | --- | --- |
| `Open{proc, image}` | `Open` (`store.go:90-194`): config check, private dirs, `owner.lock` flock (`:108-125`, `ErrBusy`), `Fence.Acquire` (`:131-139`, `ErrBusy` releases the lock), `bindIdentity` (`:151`), `Head` (`:154-157`), zero head: digest must be zero (`:159-161`), no `*.journal` may exist (`:164-172`, the `18e63b3` guard), else empty state (`:173`); non-zero head: read, digest, AEAD, decode, field checks (`:175-191`) | `Opened` / `Busy` / `Quarantined`; on `Opened` the loaded state must equal `expect_state` |
| `Close{proc}` | `Close` (`:455-472`) | `Closed` / `Busy` when `active != 0` |
| `Release{proc}` | the closure returned by `Claim` (`:611`): `active--` only | none; the claim stays in the journal |
| `Begin{Claim}` | `Claim` (`:567-612`): `check` (`:574`), `ErrClaimed` (`:579-581`), `ErrDKGPending` (`:582-589`), `before-claim`, seal intent, `commit` | `Claimed` / `DkgPending` / `Quarantined` immediately, or a commit in progress |
| `Begin{Put}` | `Put` (`:493-545`): `check` (`:500`), lock-slot held by another ID → `Conflict` (`:505-507`), existing record with different header or payload → `Conflict` (`:510-521`), identical → `syncCurrent` then `Identical` (`:522-525`), else `commit` → `Durable` (`:541-544`) | `Conflict` / `Identical` / `Quarantined`, or a commit in progress |
| `Begin{Put, fail_sync}` | identical write with an injected error at `before-identical-sync` (`:433`) | `Quarantined`; the handle is poisoned |
| `Step::Write` | `f.Write(raw)` (`:368`) | stage `Written` |
| `Step::FileSync` | `syncFile(f)` (`:374`), `f.Close()` (`:380`) | stage `FileSynced`; content durable |
| `Step::Install` | `install = os.Link(tmp, journal)` (`:386`, `:150`) | stage `Installed`; name present, not durable |
| `Step::DirSync` | `syncDirectory(root)` (`:395`) | stage `DirSynced`; name durable |
| `Step::Fence` | `fence.Advance(ctx, s.point, point)` (`:404`); the authority compares the old head and installs the new one (`fence.go:26-28`) | success: head advances, stage `Fenced`; compare failure: `fail` → `Quarantined` |
| `Step::FenceLost` | `Advance` commits, then returns an error (the documented uncertainty, `fence.go:26-27`) | `fail` → `Quarantined` (model: `LostResponse`); head advanced |
| `Step::Ack` | `s.point = point; s.state = next` (`:410-411`), `before-acknowledgement` (`:412`), `return nil` (`:415`); for `Claim` also `after-claim` and `active++` (`:606-609`) | `ClaimOk` / `Durable` |
| `IoError{proc}` at stage *S* | the boundary hook at the pause of *S* returns an error → `fail` (`:289-295`) | `Quarantined`; the handle is poisoned; no acknowledgement |
| `ProcessCrash{proc}` | the subprocess exits with code 73 at the pause, or while idle | lease and lock released by the OS; OS caches survive |
| `HostCrash` | not executable in a test | model: names not yet directory-synced vanish, content not yet file-synced is corrupt, every process stops |
| `FenceReset` | the authority is reprovisioned to the zero head (test: overwrite the fence file) | detected by `check` (`:306-309`) or at `Open` (`:164-172`) |
| `RestoreStale{image}` | the newest journal file is removed while the authority survives (`TestSnapshotRestoreQuarantine`) | `Open` quarantines (`:175-178`) |
| `Clone` | copy the keystore directory to another path with the same storage ID (`TestExclusiveAttemptOwner`, clone case) | the clone has its own `owner.lock`; only the fence excludes it |

## 3. Stage to boundary (where a replaying subprocess waits)

| Stage | Disk state | Pause boundary (`store.go`) | Boundaries passed without a model step |
| --- | --- | --- | --- |
| `BeforeWrite` | temp file created, empty | `before-write` (`:365`) | `before-claim` (`:590`) for claims |
| `Written` | temp content in page cache | `before-file-sync` (`:371`) | |
| `FileSynced` | temp content durable | `before-install` (`:383`) | `after-file-sync` (`:377`): same disk state |
| `Installed` | journal name present, not durable | `before-directory-sync` (`:392`) | `after-install` (`:389`) |
| `DirSynced` | journal name durable | `before-fence` (`:401`) | `after-directory-sync` (`:398`) |
| `Fenced` | authority head advanced | `before-acknowledgement` (`:412`) | `after-fence` (`:407`) |
| (acknowledged) | local state updated, result returned | — | `after-claim` (`:606`) |

An I/O error injected at a pause makes the boundary hook return an error
before the step's operation runs. The disk state is therefore the state of the
previous stage. `TestDurabilityOperationErrorsPreventAcknowledgement` covers
the case where the operation itself fails after starting.

## 4. Outcome vocabulary

| Model | Go | Harness token |
| --- | --- | --- |
| `Opened` | `Open` returns a store | `opened <state>` |
| `Closed` | `Close` returns nil | `closed` |
| `ClaimOk` | `Claim` returns a release function | `claim_ok` |
| `Durable` / `Identical` / `Conflict` | `frost.Durable` / `frost.Identical` / `frost.Conflict` | `durable` / `identical` / `conflict` |
| `Busy` | `ErrBusy` | `busy` |
| `Quarantined`, `LostResponse` | `ErrQuarantined` | `quarantined` |
| `Claimed` | `ErrClaimed` | `claimed` |
| `DkgPending` | `ErrDKGPending` | `dkg_pending` |
| `Crashed` | exit code 73 | process exit |

## 5. Replay limitations

| Model behavior | Replay | Reason |
| --- | --- | --- |
| `HostCrash` | partial (`replay: "partial"`) | a test cannot drop the OS page cache; the harness removes the files the model declares not durable (a journal installed by a process paused at `before-directory-sync`), kills every subprocess and continues. This tests recovery from the modeled loss, not the loss itself |
| `Journal.corrupt` from a host crash before file sync | not reproduced | the correct order syncs the file before installing, so no trace reaches it; `TestRecordContextAndCiphertext/tamper` covers a corrupt journal |
| `Proc.uncertain` | not observable | the store reports `ErrQuarantined` for both a rejected and a lost-response update; the harness compares `lost_response` as `quarantined` |
| authority `Head()` error | collapsed into a rejected update | both quarantine (`:154-157`, `:306-309`) |
| `SaveKey`/`LoadKey` | not modeled | gap G-01 |
| context cancellation | not modeled | `TestCancelledStoreDoesNotClaim` |
| the memory authority of the transport tests | not modeled | test-only provider, not the production contract |

## 6. Where the model is weaker than the source

The model does not represent: file modes and symlink checks, the AEAD
headers and key derivation, the 32 MiB / 1 MiB limits, the sequence-exhaustion
check, the `.pending-*` temporary files (never read by `Open`), payload bytes
(only two abstract values per slot), more than one seat, and the DKG
reservation's interaction with `Keys`. Each is covered only by the existing
unit tests or is out of scope for this tranche.
