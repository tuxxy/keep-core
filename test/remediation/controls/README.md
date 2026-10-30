# Batch-4 removal controls (tests only)

Each patch removes or weakens one guard in production code (or, for the `42-*`
runner controls, in a test file) and must make the named test fail at its own
assertion. Apply one patch at a time to a scratch checkout of this branch, run
the listed command, and revert. Never apply these to a branch that is pushed.

Refs tuxxy/keep-core#42, tuxxy/keep-core#46, tuxxy/keep-core#52, tuxxy/keep-core#74, tuxxy/keep-core#86 (A-READY-SEAT-ELIGIBILITY).

| Patch | Guard removed | Run | Expected failure |
| --- | --- | --- | --- |
| `46-52-stub-startup.patch` | stub: binds `frostNodeStartup` to a startup that does nothing, so the restart cases pass the missing-startup assertion and reach their own | `TestSeatRestartAfterKill` (heavy) | the five single-seat restore cases: `#46 <case>: after restart the stored wallet is not the certified wallet as ReadyUnfunded`; `multi-seat/after-key-ready` at `#46 ...: no successful completed-key reload in a fresh worker process was observed during the restarted node's startup`, `... did not take part in a FROST signature with the readiness-certified seats` and `... were not restored into readiness, so the wallet was never certified`. The two `pre-key` cases and `reload-fails` pass (the store reports the loss; nothing attests) |
| `46-metadata-only-startup.patch` | wrong fix (Astra review B1): a startup that writes `ReadyUnfunded` and, for the multi-seat operator, certifies from stored public references, without any worker run or signature | `TestSeatRestartAfterKill` (heavy) | `#46 multi-seat/after-key-ready: no successful completed-key reload in a fresh worker process was observed during the restarted node's startup` and `#46 multi-seat/after-key-ready/reload-fails: the restored seats [2 3] attested readiness although every completed-key reload in a fresh worker failed`. The single-seat cases pass: their restored seat is outside the certified set and never attests (#86) |
| `46-reload-then-certify-stub.patch` | positive control: the same startup preceded by the production fresh-worker reload (`redemption.FreshWorkerReload`), stopping on a failed reload. Not the fix | `TestSeatRestartAfterKill` (heavy) | all 9 cases pass, including the witness signature with the certified seats [2 3] |
| `46-52-stub-startup.patch` + `52-lost-not-reported.patch` | `DKGStatus` reports `InProgress` instead of `SeatsLost` for a claim with no key and no live owner | `TestSeatRestartAfterKill/pre-key`, `TestSeatRestartAfterKill/multi-seat/pre-key` (heavy) | `#52 pre-key: seats [3] killed between the RegisteredPendingReady save and SaveKey are not all reported lost after restart` (multi-seat: `seats [2 3]`) |
| `52-startup-waits-deadline.patch` | stub: a startup that never reports and waits out its context (the K09-37 shape) | same two cases | `#52 <case>: the restarted node's startup ran to its deadline instead of reporting the loss` |
| `G08-no-ready-key-binding.patch` | `SaveWallet` ReadyUnfunded no longer checks the durable key (review mutant K03-G08) | `go test ./pkg/frost/store -run TestReadyWalletMustMatchDurableKey` | `G08 <field>: ReadyUnfunded was saved against a durable key of another wallet` |
| `G11-no-approval-receipt.patch` | `SaveWallet` no longer requires the approval receipt (K03-G11) | `go test ./pkg/frost/store -run TestWalletApprovalReceiptRequired` | `G11 <field>: a <state> record without a complete approval receipt was saved` (all 18 cases) |
| `G13-no-suite-check.patch` | `NewDescriptor` no longer checks the suite string (K03-G13) | `go test ./pkg/frost/dkg -run TestUnknownSuiteRefusedBeforeAnyExternalOperation` | `G13 <suite>: NewDescriptor accepted an unknown suite` and `... reached [journal.SaveWallet] before rejection` |
| `R10-expiry-after-deadline.patch` | `expireWallet` uses `>` instead of `>=` (K03-R10). Needs the registry recompiled: see below | `TestFrostExpiryAtExactDeadline` (heavy) | `R10: expiry in exactly the deadline block did not close the pending wallet` |
| `42-config-gate-after-validation.patch` | `ReadConfig` checks the FROST gate only after full validation | `go test ./config -run TestFrostEnabledRefusedByTheNodeGate` | `#42: an enabled FROST configuration was not refused by the node gate first` |
| `42-start-gate-after-connect.patch` | `start` checks the FROST gate only after `ethereum.Connect` | `go test ./cmd -run TestStartRefusesFrostAtTheNodeGate` | `#42: start did not refuse FROST at the node gate first` |
| `42-drop-required-case.patch` | removes a required case from `./config` | `test/remediation/check-config-cmd.sh <dir>` (after the stale assertions are updated) | `NOT ACCEPTED: required case missing: ... TestFrostEnabledRefusedByTheNodeGate` |
| `42-skip-case.patch` | skips a FROST-off case in `./config` | same | `NOT ACCEPTED: skip: ... TestInactiveFrostHasNoEffect` |
| `86-refuse-every-seat.patch` | sensitivity: `Attest` refuses every attestation, so a blanket refusal cannot pass `TestUncertifiedSeatDoesNotAttest` | `TestUncertifiedSeatDoesNotAttest` (heavy, needs `FROST_K04_BITCOIND`) | `#86 positive control: the certified seats did not attest` |
| `86-sweep-refuse-every-seat.patch` | sensitivity: `Sweeper.Sign` refuses every seat, so a blanket refusal cannot pass `TestUncertifiedSeatDoesNotSign` | `TestUncertifiedSeatDoesNotSign` (heavy, needs `FROST_K04_BITCOIND`) | `#86 signing positive control: the certified seats did not sign the deposit sweep` |

"Heavy" runs need the K-03 environment (`FROST_K03_TBTC`, `FROST_K03_ANVIL`,
`SNOWFALL_WORKER`, `SNOWFALL_WORKER_SHA256`). Each case starts its own Anvil
and Core on free ports, so cases run in parallel.

The restart stubs are new files in `pkg/frost/integration`; apply one at a
time and delete the file afterwards (`git apply -R` also works).

**R10 recompile.** `deploy.cjs` deploys the registry from
`solidity/ecdsa/build`. In the scratch checkout, replace that directory with a
copy of the build, apply the patch, and run:

```
python3 -I test/remediation/controls/solc-overlay.py <solc 0.8.17> \
  solidity/ecdsa/build/build-info/<id>.json contracts/frost/FrostWalletRegistry.sol \
  solidity/ecdsa/contracts/frost/FrostWalletRegistry.sol \
  solidity/ecdsa/build/contracts/frost/FrostWalletRegistry.sol/FrostWalletRegistry.json
```

It recompiles only that contract from the build's own standard-JSON input.
With the unpatched source it reproduces the build's bytecode exactly.

**#42 positive control** (not a patch here, because it is the fix itself):
replacing the stale substring `only the local K-01 harness` with the gate's
current wording in `config/frost_test.go` and `cmd/start_frost_test.go` makes
`check-config-cmd.sh` accept (`"accepted": true`, no reasons).
