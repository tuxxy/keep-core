# K-09 implementation completion contract

Recorded 26 October 2026 before implementation of the remaining integration
paths. Governing KC-SF-V3 sections 6 (K-09), 7 and 8.3 and KC-VER rev02 are
pinned at 51b9a168e6b6c73d4d471f7d302267a065407fb5. Starting paired heads:
keep-core 40d546b4996bfd8e0157fb0ccc714a8eaed7db64;
tbtc-v2 7a0ae879992e9c5c4baaf5e22ed3171e69858ddb.
This extends BOUNDARIES.md and K09-COVERAGE.json; it does not grant production
gates, owner parameter decisions or formal verification results.

## Public legacy upgrade and FROST drain

Create the initial proxy on immutable 76f790511, request/publish the legacy
wallet through its registry callback, fund a supported legacy deposit with
Core, reveal and prove its sweep through production entrypoints. Retain its
actual custody/obligations across an upgrade to the current implementation.
Only then configure and run real worker FROST DKG/readiness, switch creation
mode and contain exposure. Public legacy movement must remain callable and
produce the exact incoming outpoint consumed by the real FROST signer and
proof maintainer. No Bridge/Bank storage injection may create these precursors;
creating FROST state before temporarily installing the old implementation is
not an acceptable substitute.

Check original liabilities and policy across upgrade, exact rejection reasons
and unchanged financial/custody/obligation frames, positive post-cutover exits,
transaction/value conservation and duplicate consumption. Reuse existing test
hooks and add assertion markers public-legacy-funded, legacy-upgraded,
legacy-cutover-contained and public-frost-drain-swept, in that order.
Controlled legacy registry membership, stake/beacon, local fees/timing and
regtest relay are explicit adapters. Bind baseline artifacts to 76f790511 and
current artifacts to actual compiler inputs/outputs before accepting evidence.

## Separate nodes and real dispatch

Move runtime construction and lifecycle dispatch into a reusable host package
called by a local-only executable entrypoint. Start distinct OS processes,
independent real stores/workers, authenticated libp2p and the production
operator eligibility firewall. The parent harness may drive public chains and
submit public user requests; it must not invoke each process's DKG/signing/
defense component methods on behalf of the runtime. Public job inputs may
coordinate deterministic local schedules; operator secrets and opaque worker
records must remain process-local. Default production activation stays fenced.

Exercise allowed peers and rejected ineligible peers, repeated dispatch,
process interruption/restart and cancellation. Reuse durable ownership and
component authorization; dispatch must not bypass planning, reservation,
readiness, epoch, custody or containment checks. Fault adapters remain test-only.

## Realistic FROST producer/consumer proof

Use the actual FROST proof producer with realistic headers and production
Bridge proof factor, and submit the resulting proof to the full relevant
FROST entrypoint. Pin the vector/chain and distinguish its trust assumptions.
No result is inferred from a BitcoinTx library-only vector or a test assembler.
Positive acceptance accompanies insufficient work/depth, retarget/relay range,
wrong inclusion or inconsistent custody refusals with relevant unchanged state.
A controlled relay observation remains explicit and is not relay qualification.

## Execution and evidence

Small focused controls weaken a single new guard in disposable copies and must
fail at the owning assertion; harness failures remain errors. Record limits
before runs. Core/tss jobs run alone, at most 8 Go cores, 900 seconds/stage and
8/16 GiB Node heaps unless a scoped run records a justified different bound.
Use private diagnostics, generated public summaries, pinned binaries, exact
source manifests and local chains. Never print/decode worker records or keys.
No shared dependency links/build directories are changed.

Commit topical changes and focused passing results before proceeding to the
next path. Update coverage only with observed evidence. Models/replays and the
final mutation campaign are separate; the latter still needs an explicit
request after V-05 closure. Stop at the K-09 milestone for review before later
release/activation/retirement milestones or any external deployment.
