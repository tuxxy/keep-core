# Legacy-unchanged suite

Remediation batch 1, shared suite (Refs tuxxy/keep-core#73, tuxxy/tbtc-v2#28,
tuxxy/tbtc-v2#31). With FROST not configured, a client against the pre-FROST
Bridge built from tbtc-v2 main `40a11d1d` must behave as at keep-core base
`4c9b65ec4`, under both empty-revert encodings and transient endpoint errors.
The upgraded-Bridge case checks the other half of #73: against an upgraded
Bridge with an ECDSA-to-FROST drain commitment, the client never broadcasts a
transaction the Bridge refuses.

1. `build-baseline.sh <tbtc-v2 repo> <out>` builds `40a11d1d` from
   `git archive` with the repository's Hardhat configuration, and compiles the
   labelled fixtures in `fixtures/`. Pass a third argument (a commit) to build
   the upgraded Bridge the same way.
2. `run.sh <evidence dir>` runs every intended case in its own process, under
   the shared heavy lock (`LEGACY_HEAVY_LOCK`), and writes
   `<evidence dir>/manifest.json`. The manifest binds the candidate keep-core
   head (which must be clean), the test and runner files, both builds, Anvil,
   Bitcoin Core and Go, and lists every intended case with its result. It is
   `"accepted": true` only if all 12 cases ran and passed with no skip;
   otherwise the runner exits non-zero.

**Acceptance for tuxxy/keep-core#73 and the shared legacy gate is that
manifest only.** A plain `go test` without the `LEGACY_*` environment skips
every case and is not evidence.

Every case starts a Bitcoin Core 29.0 regtest node (`LEGACY_BITCOIND`, checked
against `LEGACY_BITCOIND_SHA256`). Core funds the deposit, checks the scripts
and signatures of the sweep and of the moving-funds transaction the client
broadcasts, and mines them; the SPV proofs the Bridge verifies are built from
Core's own blocks.

The tests are `pkg/tbtc/legacy_*_test.go`. The RPC surface oracle
(`legacySurface`) binds each deployed address to the production roles it
plays and permits only selectors from those roles' pinned 40a11d1d ABIs;
`TestLegacyUnchanged_SurfaceOracle` tests the oracle itself. They use only APIs that exist at
keep-core base, so the same files run unchanged on the base client.

`controls/` holds the removal and sensitivity controls (patches against
production code, never applied on this branch) and what each one must break.
