# Local K-03 milestone test

This test runs `tbtc.ExecuteFrostDKG` with real worker processes, authenticated
libp2p, encrypted journals, and fresh contracts. It deploys the FROST registry,
validator, admission adapter and independent sortition pool from keep-core,
plus the matching tbtc-v2 Bridge behind a real transparent proxy. EIP-170 stays
on. Only the external beacon, stake source and pool reward token are local test
fixtures. The five unused legacy Bridge references are local test addresses;
this DKG test does not exercise Bitcoin deposits or legacy settlement.

Read `pkg/frost/dkg/README.md` for the protocol and frozen encoding rules.

## Prerequisites and execution

Use matching K-03 branches of keep-core, Snowfall and tbtc-v2. The checkout names
are normally siblings: `keep-core`, `snowfall`, `tbtc-v2`. The temporary local Go
replacement points to `../snowfall/clients/go`; its declared version must identify
the same completed-key reload API. Distribution without that replacement is D-01.

Install each Solidity package's dependencies from its checked-in lock with
`COREPACK_ENABLE_AUTO_PIN=0 corepack yarn install --immutable`. No dependency
versions are changed by this milestone. The checked-in generator uses the
geth version in keep-core's `go.mod` and creates separate FROST bindings.

Set these environment variables before running `test/frost/run-local.sh`:

- `FROST_K03_TBTC`: absolute tbtc-v2 checkout path.
- `FROST_K03_SNOWFALL`: optional Snowfall checkout path (default `../snowfall`).
- `FROST_K03_ANVIL`: Anvil executable path.
- `SNOWFALL_WORKER`: the approved worker executable path.
- `SNOWFALL_WORKER_SHA256`: its exact hex SHA-256.
- `SNOWFALL_CODEC`: independent conformance codec executable path.

Pass an output directory as the first argument to retain logs. The script
compiles both contract trees, checks that regenerated bindings match, runs the
contract and populated-upgrade tests, then normal/race host and client suites,
legacy Go packages and vet. Missing opt-in variables fail the script. Running
Go tests directly without the integration environment skips external fixtures;
a run with skips does not establish this milestone's exit.

The reviewed local environment used Go 1.27.1, Node 22.14.0, Solidity 0.8.17,
and Anvil 1.4.4. Each Anvil instance has a distinct chain ID and uses `--hardfork cancun`.
This matches the header format supported by the repository's geth 1.13.15.
Later-fork RPC support is outside this local witness. Hardhat's older tbtc-v2
version warns about Node 22; its retained tests must be checked for actual errors.

The worker SHA-256 used in this cycle is
`d775dc36c508559f1bd997ab2552cea3aed94587540f6307352fec4628400765`.
The worker and Rust protocol were not modified. Every operator gets a separate,
hash-identical worker executable so crash tests cannot kill another test's worker.

## Acceptance matrix

| Test | Observable condition |
| --- | --- |
| `TestFrostDKGPublicApprovalAndReadiness` | Real entrypoint reaches public and local ReadyUnfunded; Q and descriptor match. Runs seat patterns 1/2/3 and 1/1/2. |
| `TestForeignResultIsChallenged` | A foreign public submission is challenged; honest DKG still completes. |
| `TestSubmittedDoesNotCancelAcceptance` | Submission does not cancel the worker's acceptance callback. |
| `TestApprovalReorg` | Revert an observed approval before finality; quarantine and no completed key. |
| `TestReadinessCannotFundEarly` | Pending and ready-but-unfunded wallets are excluded from funding. |
| `TestLostSeatBeforeReadinessExpiresWallet` | Kill before the worker readiness statement; all affected seats are lost and the pending wallet expires. |
| `TestLostSeatAfterReadinessCertifiesUnderPolicy` | Kill after worker readiness but before durable KeyReady; survivors certify without the lost seat under the explicit policy. |
| `TestPendingWalletExpiryThenFreshEpoch` | Expiry retains identity/Q tombstones; the same contracts and stores complete a new epoch. |
| `TestFrostDKGCandidateCrashRequiresFreshEpoch` | Candidate-boundary crash cannot resume; public DKG expiry permits a fresh epoch. |
| `TestFrostReadinessRejectsInvalidCertificate` | A real reloaded certificate succeeds; duplicate seats, too few seats, zero references, foreign signatures and wrong wallet reject. |
| `TestFrostReadinessDeadlineEdges` | Certification just before the deadline succeeds; equality rejects and closes unfunded. |
| `TestUnknownSuiteRejectedBeforePublication` | Only the exact approved suite maps to byte 1; unknown strings are rejected before storage or publication. |
| `Frost.Registry.test.ts`, `Frost.Readiness.test.ts` | Independent result/readiness ABI vectors; wrong registry/pool/epoch/profile, invalid points and roster mutations. |
| tbtc-v2 `Frost.Identity.test.ts` | Callback authority, exact output routing, occupied identities, domain/curve rejection, and populated proxy upgrade preserve legacy data. |
| Snowfall `reload_test.go` | Fresh-process load of completed keys, public binding, corrupt opaque completion rejection and no signing advance/release. |

The fixture's block jumps and reorgs serialize node views to avoid asking Anvil
for unretained intermediate batch states. Normal views still pin by block hash;
no RPC answer, contract state, DKG result, or certificate is substituted.
The per-test fence survives store reopen only within that test process. Host-loss
fence durability and production filesystem permissions still need D-09 review.

## Release limits

This establishes the local K-03 path only. The fixture's N=3/t=2/r=2, failure
reserve, two-block finality depth and block periods are test choices, not approved
production settings. No funds, production network or active-selection flag is
used. C-07 primitive qualification, SF-03 capacity and D-01/D-02/D-09/D-11 decisions
remain release gates. Historical K-02 bounded-model evidence is not extended to
this source by the integration tests. Long model and final mutation campaigns
are not run here.
