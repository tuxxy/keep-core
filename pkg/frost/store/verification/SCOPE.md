# K-02 host store verification, tranche 1: scope record

Date: 6 October 2026. Author: Fable 5.1, implementation agent. Owner: tux.
Status: scope recorded before implementation. Results are in `RESULTS.md`
when the tranche completes.

## 1. Scope extension statement

This record adds narrowly scoped host-side verification of the keep-core K-02
store and fencing protocol to the KC-SF-V3 integration work.

KC-SF-V3 revision 01, source `44f1356f5b292ad2e78b1d1fa303cd04018d1f8a`,
does not require this work. Section 8.1 and the SC-03 and SC-08 claim records
state "Proof evidence, required only where named: None" and name no host
proof predicate. This tranche is an **owner-requested extension** made on
6 October 2026. It does not change the published specification. It is not an
existing V3 requirement.

The extension does not authorize: new SNOWFALL cryptographic models, Kani
reruns, candidate recovery, a general proof campaign, mutation campaigns
(K-09 only), fuzz campaigns, SF-03, K-03, or contract changes.

Evidence classes used, in the words of section 8.1: **bounded finite-state
model checking** of a separate host model (not the Go source), **concrete
tests** (trace replay against the real store, the existing process-crash
harness, the race detector), and **human review** (the transition-to-source
correspondence map). None of these is a formal proof of the Go source. None
is a production qualification.

## 2. Source and tool state at the start

| Item | Value |
| --- | --- |
| Specification | `44f1356f5b292ad2e78b1d1fa303cd04018d1f8a`, local clean checkout matches |
| keep-core worktree | `/Users/tux/threshold/integration-worktrees/K-00-SF-01-K-01/keep-core`, branch `integration/k02-durable-transport`, head `18e63b378b3c3b5ee24f8cd7bf909631f1f00454`, clean. Matches the reviewed K-02 head and the `tuxxy` remote. |
| snowfall worktree | `/Users/tux/threshold/integration-worktrees/K-00-SF-01-K-01/snowfall`, head `98e35afd09393b4ba3c7a4b1db392457d1a6fa07`, clean. Matches the reviewed SF-01 head. The branch is now `integration/sf03-capacity`, a new branch created after the review record that still points at the SF-01 commit. This is other work in progress. It is recorded here and not changed by this tranche. |
| K-02 pre-fix revision | `18b492b5482b73cd744670fd6a3bd202d95a8110` (parent `aee9893`): the K-02 commit before the zero-fence guard, kept in the reflog. Its `pkg/frost/store/store.go` is blob `4635a8a46b684127ec04a34062d4e5b049d10fee`; `store_test.go` is blob `c140ce739d3acb1e320fcad1967c32c7811ae54b`. The earlier reconciliation head `472cbdc11bfe64b9fc8ef6eaca2199d03ed30802` has the same two blobs. The review cycle's `k02-before` run used `18b492b54`. The current head differs from it only in the two store files. |
| Current store source | `store.go` blob `6088d55c8b2c9254b9d559d033b3116d792e1cd5` |
| Go | `go1.27.1 darwin/arm64` (GOTOOLCHAIN=auto; `go.mod` says 1.24.0) |
| Rust for the model | `1.94.0-aarch64-apple-darwin` (stable, the SNOWFALL pin), pinned by a crate-local `rust-toolchain.toml` |
| Stateright | `=0.31.0`, checksum `fd1157f21b11916f90fe1f2ac9a8d0e09a8813b28701584141060f414eedf6ba` (already in the SNOWFALL lockfile) |
| Machine | Darwin 24.6.0, 16 cores, 128 GiB |

The SNOWFALL agreement model, core crates and workspace lockfile are not
changed. The host model is a separate crate inside keep-core with its own
lockfile.

## 3. Predicates and verification IDs

The five predicates use the owner's wording. Each has a stable ID. The model
property names, witness names and trace IDs below are the ones used in the
model crate and the trace files.

| ID | Predicate | Primary mapping | Supporting mapping | Model `always` property | Witnesses required |
| --- | --- | --- | --- | --- | --- |
| P-K02-OWNER | At most one process holds valid authority for a storage identity under the specified fencing contract. Local clones must not obtain independent authority. | SC-03 (concurrent local seats, crash or restore cannot bypass the one-package rule; exclusive-host premise), SIG-02 | Section 3.2 "Exclusive process ownership ... must survive restart"; K-02 exit "no execution can resume a claimed signing attempt" | `owner_exclusive` | W-BUSY, W-FRESH |
| P-K02-TERMINAL | Once an attempt claim succeeds, release, restart, scope changes and stale-disk restore cannot make that attempt reusable. | SIG-02, SC-03 ("the failed attempt stays terminal"; section 4.4 "Never recycle a claimed attempt") | SC-08 (tombstone retained; a seat lost before KeyReady counts as lost) | `terminal_claims` | W-FRESH, W-REOPEN, W-FRESH-AFTER-CRASH |
| P-K02-IMMUTABLE | A stored record or lock slot cannot later acknowledge conflicting contents. Identical writes must establish durability before returning success. | SC-03 (one signing package per nonce; the SF-01 lock/record contract is the host mechanism), SIG-02 | Section 4.4 compare-and-insert rule | `immutable_slots` (conflicting ack) and the Identical case of `durable_acks` | W-IDENTICAL, W-CONFLICT |
| P-K02-DURABLE | Successful acknowledgement implies committed, durable state. Reopening either preserves that state or quarantines; it must not silently expose an earlier usable state. | SC-08 (defined recovery; no transition discards the only required evidence), SC-03 (crash or restore cannot bypass) | Section 4.4 "A file-write return alone is not durable completion" | `durable_acks` (ack implies fenced and durable journal; no stale state exposed on reopen) | W-REOPEN, W-QUARANTINE, W-STALE-RESTORE-QUARANTINE |
| P-K02-UNCERTAIN | A fence update that commits but returns an error cannot permit continued operation from stale local state. | SC-03 (K-02 safe state: I/O uncertainty quarantines the attempt), SC-08 (quarantine is a nonterminal state with an owned recovery transition) | `Fence.Advance` contract in `fence.go` | `uncertain_fail_closed` | W-FENCE-LOST |

These are host-side obligations. They do not prove cryptographic nonce
uniqueness. That property is E-02 SNOWFALL evidence with its own limits.
SC-03's "at most one signing package per nonce material" composes the host
predicates above with the worker-side contract; this tranche verifies only the
host side.

## 4. Guarantees and assumptions

### 4.1 Guarantees claimed, within the bounds of section 5

The five predicates, as model-checked properties over the finite host model,
replayed on the real store for every discovered counterexample and the
declared representative trace set, with the stated Go test and race results.

### 4.2 Assumptions, outside the guarantee

| ID | Assumption |
| --- | --- |
| A-01 | The independent fencing authority provides the documented exclusive, non-expiring lease (`Acquire` excludes every other process, including a clone on another host) and durable compare-and-advance with linearizable semantics. Its state is never cloned or restored together with the keystore. |
| A-02 | A crashed process performs no further action. Process death releases its leases. A paused or partitioned process is not a crash; the lease never times out. Lease timeouts are not modeled and are forbidden by the fence contract. |
| A-03 | Filesystem: a file sync makes the written bytes durable; a directory sync makes the installed name durable; `os.Link` does not overwrite. Before the matching sync, a host crash may lose the bytes or the name. After it, it does not. |
| A-04 | A stored snapshot that changed in any way is detected. The SHA-256 digest named by the fence and the AEAD tag make any change detectable. The model represents this as exact content identity: a read returns the committed content or a detectable corruption, never a plausible different snapshot. |
| A-05 | **Not both histories lost.** No safety claim is made for executions in which the authority was reset to a zero head and the disk image used by the opening process has no journal. The model marks such executions (`both_lost`) and the predicates are not asserted after that point. The model shows the case is reachable, so it is a documented exclusion, not an impossibility. |
| A-06 | One storage identity per store. Identity creation (`bindIdentity`) is atomic and durable; it has no injected boundaries in the implementation. |
| A-07 | The host never decodes payloads. Key derivation and AEAD are correct. Secret management is deployment work. |
| A-08 | Stateright's 64-bit fingerprint is collision-free over the explored states, and its BFS is exhaustive for the bounded configuration when the run finished before its deadline. Stateright's own `is_done` is also true after a timeout, so the runner records a run as complete only when it was done before the deadline (see §10). |
| A-09 | Reconciliation after quarantine is an operator transition owned by deployment (SC-08). It is not modeled. Quarantine is fail-closed; availability after quarantine is not claimed. |

### 4.3 Explicit checks on detectable inconsistencies

The model and the replay set include: zero fence head with an existing journal
(must quarantine), zero head with a non-zero digest (must quarantine), head
naming a missing journal after stale restore (must quarantine), a changed fence
head during operation (must quarantine on the next operation), a fence update
with a lost response (must quarantine the handle), and a second process or
clone during live ownership (must be refused).

## 5. Model bounds, abstractions and reductions

| Item | Bound or choice |
| --- | --- |
| Processes | 2, distinguishable (no symmetry reduction) |
| Disk images | primary plus at most 1 clone (copy of the primary's durable state) |
| Attempt identities | 2 |
| Domains | 2 for the DKG reservation; claims are global per storage identity as in `store.go` |
| Slots | 1 immutable record slot and 1 lock slot, each with 2 possible values |
| Commits | planned: at most 4 committed or attempted snapshots in one execution (unique-id budget). Used: 3, see §10 |
| Fence resets | at most 1 |
| Clones | at most 1 |
| Stale restores | bounded by the number of journals (a restore removes the newest durable journal) |
| Commit stages | 7, matching the Go boundaries: before-write, written, file-synced, installed, directory-synced, fenced, acknowledged |
| Fence outcomes | 3: committed with response, rejected (compare failed), committed with lost response |
| Crash kinds | process crash (process memory lost, OS caches survive) and host crash (every process stops, unsynced bytes and names are lost) |
| I/O errors | at every commit step and at the identical-write resync |

Abstractions: the snapshot digest is `(sequence, unique id)`; the random seal
nonce makes real digests unique, which the unique id models. A lock slot held by
a different record ID and a record held with a different payload both collapse
to one `Conflict` outcome. One seat. Temporary `.pending-*` files are not
modeled; `Open` never reads them. An authority `Head()` error collapses into the
rejected outcome; both quarantine. Context cancellation is not modeled. The
store mutex serializes operations, so each process runs one operation at a
time. `SaveKey`/`LoadKey` are out of scope for this tranche (gap G-01).
Identical-write resync is modeled but is not distinguishable from a no-op in
the correct protocol; its requirement is covered only by the concrete test
`TestDurableCompareInsert`.

State fingerprint: Stateright hashes the complete model state. No symmetry or
partial-order reduction is used.

Execution limits chosen before each run: 600 s wall-clock per configuration;
abort if maximum resident set exceeds 16 GiB (measured by `/usr/bin/time -l`);
12 checker threads. A run that stops on either limit is **blocked**, not a
pass.

## 6. Model negative controls

The model has a `Weakening` parameter. The unweakened model is the claim. Each
weakening removes one guard and must produce a counterexample for its declared
property. These are model-level controls that show the properties are not
vacuous. They are not implementation mutants; no Go source is mutated.

| Weakening | Declared failing property | Implementation counterpart |
| --- | --- | --- |
| `ZeroHeadIgnoresJournals` | `terminal_claims` | The historical defect fixed in `18e63b3`; replayed as the negative regression control against pre-fix revision `18b492b54` |
| `AckBeforeFence` | `durable_acks` | none (never in the source) |
| `NoDirSync` (planned as `FenceBeforeDirSync`, see §10) | `durable_acks` | none |
| `IgnoreAdvanceError` | `uncertain_fail_closed` | none |
| `LocalLockOnly` (open proceeds when the fence lease is held elsewhere) | `owner_exclusive` | none |
| `ReleaseClearsClaim` | `terminal_claims` | none |
| `PutOverwrites` (added during implementation, see §10) | `immutable_slots` | none |

## 7. Acceptance criteria

A predicate is **pass** only if all of the following hold:

1. The unweakened model's `always` property has no counterexample and the
   checker reports completion within the limits of section 5.
2. Every witness named for it in section 3 is discovered.
3. Its declared weakening produces a counterexample.
4. Every discovered counterexample trace, and every trace of the declared
   representative set, replays on the current Go store with matching outcomes
   at every step, or is recorded as a replay limitation with a reason.
5. The affected Go suites pass normally and under the race detector.

A predicate is **blocked** if any exploration is incomplete, times out or hits
an unsupported step. It is **fail** if a replay mismatch is confirmed as an
implementation defect. A model or implementation that rejects everything does
not pass: W-FRESH, W-IDENTICAL, W-REOPEN and W-FRESH-AFTER-CRASH must be
discovered and must replay.

## 8. Negative regression control

Defect: a zero fence head with an existing identity and journal opened as an
empty store, which made committed claims reusable. Fixed at `18e63b3`.
Control: replay the model counterexample `CE-TERMINAL-ZEROHEAD` at pre-fix
revision `18b492b5482b73cd744670fd6a3bd202d95a8110` in a scratch worktree with
the same replay harness. The pre-fix run must fail at the reopen step and admit
the claim reuse. The current source must quarantine. Both results are retained.

## 9. Out of scope

SF-03, K-03, contract work, candidate recovery, SNOWFALL model or Kani work,
mutation testing (K-09), fuzzing, a production fence service, the
`SaveKey`/`LoadKey` path, the transport package, the memory authority used in
transport tests, FROST activation, deployment and funding.

## 10. Revisions made during implementation

This record was written before implementation. The items below changed while
the model and harness were built. They are listed here instead of being
rewritten into the sections above.

1. **Commit bound 3, not 4.** The claim model has 1.15 M unique states at
   two commits and 32.3 M at three (28× per added commit). Four commits
   extrapolate to roughly 900 M states and far more than 16 GiB, so the bound
   was not attempted. The run at three commits is the claimed bound.
2. **State-space reductions.** The first model kept a per-process last outcome
   and seven witness history flags; at two commits it had 14.2 M states. The
   final model keeps one global last-outcome record and derives every witness
   from the current state, with one history bit (`reopened`) and the acked
   facts. Signing claims are offered in domain 0 only, because a signing
   claim's state does not depend on the domain; DKG claims use both domains.
3. **Model defect found and repaired (finding F-01).** The first observer
   required the authority head to equal the acknowledged point at the moment
   of acknowledgement. A fence reset by the environment between a successful
   advance and the acknowledgement made that check fail. The reset is an
   environment fault that the next operation or open detects; it does not make
   the acknowledgement false. The observer now checks that the authority
   accepted the advance to that point and that the journal is durable. The
   counterexample is preserved under `model-defect-observer/`.
4. **Weakening renamed.** `FenceBeforeDirSync` became `NoDirSync` (the
   directory sync is skipped). Advancing the fence before the directory sync
   but still before the acknowledgement breaks no stated predicate; it only
   risks quarantine after a host crash. Skipping the sync breaks
   `durable_acks`, which is the intended control.
5. **Weakened runs stop early.** A weakened model stops at the first
   counterexample of its declared property. It is a negative control; an
   exhaustive exploration of a broken model adds no evidence. The claim model
   is always explored exhaustively.
6. **Completion flag.** Stateright reports `is_done` after a timeout as well.
   The runner marks a run complete only when it finished before its deadline
   and records `timed_out` separately.
7. **Witness list.** Sixteen witnesses plus the A-05 boundary witness
   `w_both_lost`; `w_progress_after_crash` became `w_progress_after_orphan`
   (a fresh claim acknowledged while an orphan journal from an interrupted
   commit exists).
8. **Separate worktrees.** Another session occupied both retained worktrees
   (branch `integration/mutation-regressions` with staged changes). This
   tranche used fresh worktrees: keep-core at
   `/Users/tux/threshold/integration-worktrees/K-02-host-verification/keep-core`
   on `integration/k02-host-verification` from `18e63b3`, and a detached
   snowfall checkout at `98e35af` next to it for the module replacement. The
   retained worktrees were not changed.
9. **Host crash replay.** Traces with a host crash replay partially: the
   harness removes the journal the model declares not durable and continues.
10. **Declared control for P-K02-IMMUTABLE.** The planned table had no
    weakening whose declared failure is `immutable_slots`; that property was
    only broken as a by-product of other weakenings, and once weakened runs
    stop at their declared property that by-product is not guaranteed to
    appear. `PutOverwrites` removes the compare guard of `Put` (a write to an
    occupied slot with different contents commits instead of returning
    `Conflict`) and is declared to break `immutable_slots`.
