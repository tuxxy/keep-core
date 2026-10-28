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
2. `run.sh <evidence dir>` runs every case in its own process, under the
   shared heavy lock (`LEGACY_HEAVY_LOCK`), and fails on any skip.

Every case starts a Bitcoin Core 29.0 regtest node (`LEGACY_BITCOIND`, checked
against `LEGACY_BITCOIND_SHA256`). Core funds the deposit, checks the scripts
and signatures of the sweep and of the moving-funds transaction the client
broadcasts, and mines them; the SPV proofs the Bridge verifies are built from
Core's own blocks.

The tests are `pkg/tbtc/legacy_*_test.go`. They use only APIs that exist at
keep-core base, so the same files run unchanged on the base client.

`controls/` holds the removal and sensitivity controls (patches against
production code, never applied on this branch) and what each one must break.
