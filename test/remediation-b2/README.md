# Remediation batch 2: drain producer tests

Refs tuxxy/keep-core#34 and its constraint issues (keep-core #44, #47, #49,
#51, #54, #55, #57, #62, #67, #68, #69; tbtc-v2 #18, #20, #21, #22). The
contract side is in tbtc-v2 `test/bridge/Frost.Movement.LegacyAfterCutover.test.ts`.

The tests are `pkg/tbtc/drain_*_test.go`. They extend the frozen batch-1
legacy world (`pkg/tbtc/legacy_*_test.go`, `test/legacy/`), which they never
edit. Every case deploys the real tbtc-v2 Bridge compiled from git (the
pre-FROST `40a11d1d` build, or the integration build at
`LEGACY_UPGRADED_COMMIT`) on Anvil, starts a Bitcoin Core 29.0 regtest node,
brings a legacy source into MovingFunds through public calls, and drives the
production client (`chainethereum.Connect`, default configuration) through
`tbtcpg.MovingFundsTask.Run`, once per coordination window.

`run.sh [-j N] <evidence dir> [filter]` runs every intended case in its own
process (own Anvil, Core node, ports and data directories; up to N at once,
default 12, GOMAXPROCS=1 each; about 20 s for all cases with the fast
legacy harness of tuxxy/keep-core#101) under the shared heavy lock and writes `<evidence dir>/manifest.json`. It needs
the same `LEGACY_*` environment as `test/legacy/run.sh`. The manifest is
`"accepted": true` only if all 47 cases ran and passed, unfiltered, at a clean
head. **A plain `go test` skips every case and is not evidence.**

`controls/` holds the removal and sensitivity controls: patches against
production code, never applied on this branch. `controls/README.md` says what
each must break.
