# K-09 evidence checks (remediation batch 9)

Checks on a finished K-09 run's evidence directory (`test/k09/run.sh`
output) for the three evidence gaps left open after run b7-r1. They read
files only: no chain, no stage re-run, no node store. Local only.

```
K09_EVIDENCE=<evidence dir> go test -count=1 -run 'TestK09Evidence' -v ./test/k09/evidence
```

This runs `TestK09Evidence` (the acceptance checks) and
`TestK09EvidenceControls` (synthetic wrong implementations). Run it in the
keep-core checkout the evidence names: the #22 check compares against
`candidate.keepCoreHead` in that repository.

| Subtest | Issue | Invariant |
| --- | --- | --- |
| `22-ecdsa-build` | tuxxy/keep-core#22 (K09-1) | The manifest records keep-core's `solidity/ecdsa` build, and that build was compiled from `keepCoreHead`: every artifact the world deploys (every `b6Deploy("<source>", "<contract>"` call in the head's `pkg/tbtc/k09_*.go`) is hashed and names a recorded build-info that compiled its source; every keep-core source (`contracts/`, except Hardhat's generated `contracts/hardhat-dependency-compiler/` shims) in every recorded build-info is byte-identical to the file at `keepCoreHead`; `builtFrom` is `keepCoreHead`; the npm dependencies come from the head's `solidity/ecdsa/yarn.lock`. |
| `41-node-log-hashes` | tuxxy/keep-core#41 (K09-24) | The files under `nodes/` are exactly the keys of `manifest.nodeLogs`, each with its SHA-256, and every stage that starts nodes (all but 01 and 02) left at least one node log. |
| `40-drain-signers` | tuxxy/keep-core#40 (K09-23) | In `04-drain` and `06-restart`: the stage log is the one the manifest hashed; it shows no harness FIXTURE drain (`b6 phase drain proven`, from `drainFrom`); it records exactly one drain (`K09DRAIN`); the commitment's sender is one of the stage's node operators; the committer's own node log records base's submission of exactly that commitment transaction and MovingFundsTask's confirmation for the source (so the node's task produced it, not a harness with an operator's public test key: review B1); and a node log of that stage records base's completed MovingFunds broadcast of exactly that transaction for exactly that source wallet. |

The composition ledger named in #40 (producer results against consumer
assumptions) is KC-VER V-05 verification work and is not checked here.

## What the run must record
These are the fields the checks read. Everything else in the manifest is
unchanged.

**`manifest.dependencies.ecdsaBuild` (#22):**

```json
{
  "builtFrom": "<the commit the build was compiled from: keepCoreHead>",
  "yarnLockSha256": "<sha256 of solidity/ecdsa/yarn.lock the node_modules came from>",
  "artifacts": {
    "<source>:<contract>": {"sha256": "<sha256 of the artifact JSON>", "buildInfo": "<build-info ID>"}
  },
  "buildInfos": {
    "<build-info ID>": {"sha256": "<sha256 of the build-info file>",
                        "sources": {"<compiler input path>": "<sha256 of that source's content>"}}
  }
}
```

`recordEcdsaBuild` in `buildrecord_test.go` produces this record from a
Hardhat build directory (artifact, `.dbg.json`, build-info
`input.sources[*].content`). Because every source of a recorded build-info
must match the head, an incremental build that kept build-infos of older
compilations fails even when the deployed contracts' own imports are current.
Building into a fresh directory from the head passes.

**`manifest.nodeLogs` (#41):** `{"nodes/<stage>/operator-N.log": "<sha256>", ...}`,
one entry per node log file in the evidence, hashed after the copy.

**Drain record (#40):** once a drain stage has checked on chain that the
Bridge accepted the drain's commitment and its proof, it logs one line:

```
K09DRAIN source=0x<20-byte legacy source PKH> committer=0x<sender of submitMovingFundsCommitment> commitTx=0x<that Ethereum transaction> drain=<drain txid, display byte order>
```

The stage requires the commitment to be mined at or after the drain's
coordination window (base's leader commits inside the window).

The check matches it against the node logs: operator N's address is the
address of the test scalar N (the K-09 world's operator keys), and base's
`pkg/tbtc/wallet.go` logs `broadcasting completed` with the fields `wallet`
(uncompressed public key), `action` (`MovingFunds`) and `movingFundsTxHash`.
The committer's log must hold base's
`submitted transaction submitMovingFundsCommitment with id: [<commitTx>]`
and `the moving funds commitment transaction successfully confirmed` with
`"walletPKH": "0x<source>"` (`pkg/tbtcpg/moving_funds.go`); base skips both
when a commitment already exists.

## Controls
`TestK09EvidenceControls` writes SYNTHETIC evidence (FIXTURE, no run made
it): one correct directory, which must pass all three checks, and one per
wrong implementation below, which must fail its own check at the named text
and pass the other two (26 wrong implementations).

| Control | Wrong implementation | Fails at |
| --- | --- | --- |
| `c22-presence-only` | the run only checks `solidity/ecdsa/build` exists (b7-r1) | "the manifest has no dependencies.ecdsaBuild" |
| `c22-stale-source` | build from another worktree: the registry source differs from the head | "stale build: build-info ... but keepCoreHead ... has" |
| `c22-source-not-at-head` | the build compiled a file the head does not have | "..., which keepCoreHead ... does not have" |
| `c22-partial-artifacts` | a deployed artifact is not recorded | "... has no record of it" |
| `c22-unrecorded-build-info` | an artifact names a build-info the record lacks | "which dependencies.ecdsaBuild.buildInfos does not record" |
| `c22-foreign-build-info` | an artifact credited to a build-info that never compiled it | "which did not compile its source" |
| `c22-unhashed-artifact` | an artifact recorded without its hash | "has no SHA-256" |
| `c22-built-from-other` | the build record names another commit (review N3) | "the build's builtFrom" |
| `c22-other-yarn-lock` | node_modules from another yarn.lock | "yarnLockSha256" |
| `c41-stage-logs-only` | only stage logs hashed (b7-r1) | "the manifest has no nodeLogs" |
| `c41-one-log-unhashed` | one node log left out | "is not hashed in manifest.nodeLogs" |
| `c41-hashed-before-copy` | a log changed after it was hashed | "but manifest.nodeLogs records" |
| `c41-phantom-entry` | an entry for a file not in the evidence | "which is not a node log in the evidence" |
| `c41-logs-not-collected` | a stage's node logs not copied in | "started nodes but the evidence has no node log" |
| `c40-fixture-drain` | the harness's FIXTURE drain still runs (b7-r1) | "the stage log shows the harness's FIXTURE drain" |
| `c40-no-record` | no drain record (b7-r1) | "records 0 drains" |
| `c40-two-records` | two drain records in one stage | "records 2 drains" |
| `c40-harness-committer` | the legacy operator FIXTURE commits the drain | "which is not one of the stage's node operators" |
| `c40-harness-commit` | the harness commits from operator 1's public test key; the nodes only sign (review B1) | "does not record base's own commitment" |
| `c40-commit-logged-elsewhere` | the submission is in another operator's log, not the committer's | "does not record base's own commitment" |
| `c40-other-commit-tx` | the committer submitted a different commitment transaction | "does not record base's own commitment" |
| `c40-harness-signed` | the record is written but no node broadcast the drain | "records a completed MovingFunds broadcast of the drain" |
| `c40-other-action` | the txid appears only under another action | "records a completed MovingFunds broadcast of the drain" |
| `c40-other-wallet` | a node broadcast that txid for another wallet | "not for the legacy source" |
| `c40-other-transaction` | the record names a txid the nodes did not broadcast | "records a completed MovingFunds broadcast of the drain" |
| `c40-stage-log-rewritten` | the stage log changed after the manifest hashed it | "but the manifest records" |

**Real-build control (#22):** `TestK09EcdsaBuildOf` records a real build
(`K09_ECDSA_BUILD`, with `yarn.lock` beside it) and checks it against
`K09_HEAD`. Results: `K-09-restart/TESTS-BATCH9.md`.
