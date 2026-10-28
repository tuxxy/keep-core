# Controls

Each patch is applied to a scratch checkout, the named cases are run, and the
patch is reverted. None is ever committed to production code.

| Patch | Applied to | Must fail | Result recorded 2026-10-28 |
| --- | --- | --- | --- |
| `A-no-frost-resolution.patch` | integration `ec1084dd` | `UpgradedBridgeDrain` (both encodings); `MovingFunds` must pass | `MovingFunds` 4/4 pass; `UpgradedBridgeDrain` fails: "output 0 pays a legacy script to FROST target", "the Bridge refuses ...: FROST drain mixes schemes" (rechecked with Bitcoin Core: Core mines both, only the Bridge refuses the drain) |
| `B-chain-clock-margin.base.patch` | base `4c9b65ec4` | `MovingFunds` | fails at "legacy moving-funds proposal": the task made no proposal (safety margin measured on chain time) |
| `C-code3-only-probe.patch` | integration `ec1084dd` | `MovingFunds/geth-*` at the execution assertion; all cases at the RPC surface assertion | as expected: geth cases "error while resolving FROST target wallets: [execution reverted]"; code-3 cases surface only |
| `D-cached-probe.patch` | integration `ec1084dd` | `MovingFunds/*+transient-then-success` at the execution assertion (one process per case) | as expected: "error while resolving FROST target wallets: [transient: upstream unavailable]" |
| `E-startup-probe.patch` | integration `ec1084dd` | `Startup` at the RPC surface assertion | all 4 fail: "eth_call to Bridge@40a11d1d selector 0xb9d91db7 is not in its ABI" |
| `F-overspend.base.patch` | base `4c9b65ec4` | `MovingFunds` at the Bitcoin Core assertion | fails: "Bitcoin Core refused a broadcast: ... sendrawtransaction: Core error -26: bad-txns-in-belowout" (the outputs exceed the main; nothing but Core checks this) |
| oracle: registry roles also permit the fixture's own ABI (test-code mutation of `newLegacySurface`) | integration `ec1084dd` | `SurfaceOracle` fixture-getter cases | both fail: "surface oracle: accepted a call outside the 40a11d1d production surface" (`bridge()`, `member()`) |
