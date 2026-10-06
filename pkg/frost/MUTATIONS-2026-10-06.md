# Early integration mutation cycle — 6 October 2026

The user explicitly requested this cycle now. It is an early local check on the
current integration. It does not complete K-09 or replace its review of the final
code. No runtime or protocol code changed in this cycle.

Across both repositories, the initial pass caught **38 of 56** selected mutants
and left **18 survivors**. The added tests catch all 18. Final resolved result:
**56 caught, zero unresolved survivors or build failures**. These are hand-selected
single-fault controls, not an exhaustive mutation score or a correctness proof.

Each mutant used a separate Go source overlay. Worktree production files stayed
unchanged and their hashes were checked. Each selected test command first passed
without mutation, with no skips. A kill requires a failing test assertion; a build
error or timeout cannot count. The rerun had two wall-clock timeouts (SF01-08 and
SF01-09), both retained and then caught in serial reruns with the same test selector.
No timeout result was converted to a kill. An earlier runner preflight rejected an
empty test-name selection before any mutant ran; the selector was corrected.

The first clean concurrent check exposed a test-isolation fault: a keep-core
process check counted a worker from the client suite. The transport test helper
now copies the hash-verified worker into its own temporary directory. Process
checks and crash injection use that test-specific path. The clean suites below
were then rerun; the earlier failure remains in the evidence.
The first client race run also reached its 120-second suite limit. The clean
rerun allows 300 seconds per suite and 600 seconds including compilation.
On 6 October, the concurrent host race run reached the 60-second DKG operation
deadline. The serial retry and the original-helper control also failed. At 15:44
the machine had a load average of 166.66 and 12,735.62 MiB of swap in use. These
failures remain in the evidence; none is counted as a pass.

On 7 October, the unchanged host race suite passed all 102 test/case events with
zero failures or skips at a load average of 3.8–4.9. The real-worker reload test
took 2.02 seconds. The normal host suite, normal/race client suites, publication
race tests and vet also passed. **The local host race validation item is closed.**
The operation deadline remains 60 seconds. Mutation limits remain 60 seconds per
selected run and 150 seconds overall. This does not qualify production capacity.

## Repository scope

Production source: `18e63b378b3c3b5ee24f8cd7bf909631f1f00454`. The tests in this commit extend that source.
This repository contributes **27 caught controls** to the total.

The old identity fixture wrote a claim, so the newer journal-presence guard also
rejected the reset. The new empty-store case isolates the immutable identity check.
The new acceptance test permits the approved profile and checks that unknown or
empty profiles never reach the host callback or mark acceptance as called. It
exercises the host adapter directly because the real worker emits only its pinned
profile. Existing checks catch the other 25 controls, including the zero-fence
restore fix and publication cancellation.

| Control | Deliberate fault | Initial → final | Detecting test |
| --- | --- | --- | --- |
| K02-M01 | Skip file sync | killed → caught | `TestDurabilityOperationErrorsPreventAcknowledgement/file-sync` |
| K02-M02 | Skip directory sync | killed → caught | `TestDurabilityOperationErrorsPreventAcknowledgement/directory-sync` |
| K02-M03 | Skip atomic snapshot install | killed → caught | `TestDurabilityOperationErrorsPreventAcknowledgement/install` |
| K02-M04 | Ignore a changed live fence | killed → caught | `TestLiveFenceChangeQuarantines` |
| K02-M05 | Allow a claimed attempt | killed → caught | `TestExclusiveAttemptOwner` |
| K02-M06 | Allow a competing DKG | killed → caught | `TestPendingDKGCannotBeReplaced` |
| K02-M07 | Ignore a changed snapshot digest | killed → caught | `TestIdenticalWriteChecksDurableBytes` |
| K02-M08 | Skip seat membership authentication | killed → caught | `TestSeatAttributionAndPrivateRoundTwo` |
| K02-M09 | Skip deployment-domain binding | killed → caught | `TestForeignDomainAndEnvelopeBinding/domain` |
| K02-M10 | Skip outer attempt binding | killed → caught | `TestForeignDomainAndEnvelopeBinding/attempt` |
| K02-M11 | Send local private packets onto pubsub | killed → caught | `TestSeatAttributionAndPrivateRoundTwo` |
| K02-M12 | Disable envelope deduplication | killed → caught | `TestSeatAttributionAndPrivateRoundTwo` |
| K02-M13 | Disable pending-ciphertext capacity | killed → caught | `TestPendingCiphertextBoundAndDeduplication` |
| K02-M14 | Skip inner/outer sender binding | killed → caught | `TestForeignDomainAndEnvelopeBinding/sender` |
| K02-M15 | Ignore active publication cancellation | killed → caught | `TestPublicationHonorsContext` |
| K02-M16 | Ignore queued publication cancellation | killed → caught | `TestQueuedPublicationHonorsContext` |
| K02-M17 | Skip immutable storage-root identity | survived → caught | `TestStorageIdentityCannotResetClaims/empty_store_identity` |
| K02-M18 | Allow journal files under a zero fence | killed → caught | `TestFenceResetQuarantinesExistingJournal` |
| K02-M19 | Skip external fence advance | killed → caught | `TestDurableCompareInsert` |
| K01-01 | Omit chain identity | killed → caught | `TestAttemptDomainBinding/chain` |
| K01-02 | Omit registry identity | killed → caught | `TestAttemptDomainBinding/registry` |
| K01-03 | Omit wallet identity | killed → caught | `TestAttemptDomainBinding/wallet` |
| K01-04 | Omit epoch identity | killed → caught | `TestAttemptDomainBinding/epoch` |
| K01-05 | Ignore attempt start block | killed → caught | `TestAttemptDomainBinding/start` |
| K01-06 | Alias the worker environment slice | killed → caught | `TestWorkerPinAndEnvironment/exact` |
| K01-07 | Allow an unapproved candidate profile | survived → caught | `TestMutationAcceptanceRequiresApprovedProfile/other-valid-profile` |
| K01-08 | Allow node activation | killed → caught | `TestInactiveFrostHasNoEffect` |

## Verification — 7 October review rerun

The rerun used the unchanged reviewed heads: snowfall
`19d0d13bcd0bce2d8bda9753de657e7f437b19d3` and keep-core
`8a51512db51d72b840562de2359c1e1cea2e303d`. This follow-up changes documentation
only, so the tested source remains byte-identical.

- normal: pass; 102 test/case pass events, 0 failures, 0 skips.
- race: pass; 102 test/case pass events, 0 failures, 0 skips.
- publication-race: pass; 2 test/case pass events, 0 failures, 0 skips.
- vet: pass; 0 test/case pass events, 0 failures, 0 skips.

The review rerun used Go 1.27.1 on macOS arm64 with the retained real worker
and codec where required. Worker SHA-256: `d775dc36c508559f1bd997ab2552cea3aed94587540f6307352fec4628400765`.
Codec SHA-256: `efe27c5116eb85c7bef42a0b1af3817ce7fc31e7de83d947da8055db792fc782`.
The Go client retains its local replacement in keep-core. No artifact, production
capacity, deployment target or funding gate is qualified by this cycle.

## Local reproduction and evidence

The retained integration evidence directory is
`integration-spec-v2/implementation/mutation-2026-10-06/` in the integration
workspace. It holds the Python 3.9.6 standard-library runner, exact mutation specs,
source hashes and test patches, baseline logs, per-mutant overlays/diffs/output,
initial and rerun results, serial retries, clean/race/vet results and SHA-256 manifest.
The runner's repository and artifact paths identify the retained local worktrees.
`K02-seeds.py` retains the original 17 controls as data; its runner is not executed.
The codec binary is also retained in the evidence archive.
The `review-2026-10-07/` subdirectory holds the fresh rerun logs, `run.sh`, and
`status.log` with commands, exit codes, timings, heads and load samples. The
6 October failed runs remain intact. No new mutation campaign ran for this
review. Use a fresh `--round` name to rerun the mutation runner; `--jobs 1` runs
serially. It never edits the source files.

For each selected source change the runner executes:

```text
go test -overlay <one-mutant-overlay.json> -count=1 -json <package> -run <selector> -timeout=60s
```

A separate 150-second process limit bounds compilation plus test time. Long runs
remain local. No CI mutation job, cryptographic mutation sweep, model, proof, Kani
or fuzz campaign was added. SF-02 remains retired. The SF-03 budget, protocol choice
and production target remain open in [keep-core #5](https://github.com/tuxxy/keep-core/issues/5).
