# K-09 lifecycle demonstration

KC-SF-V3 §6 K-09: the FROST lifecycle driven through real node processes on
the integration branch. Local only; nothing here runs in CI.

```
source <env with SNOWFALL_WORKER*, LEGACY_*, K09_ECDSA_NODE_*>; test/k09/run.sh <new evidence dir>
K09_EVIDENCE=<that dir> go test -count=1 -run 'TestK09Evidence' ./test/k09/evidence
```

The run builds from HEAD alone: `git archive HEAD`, a fresh `solidity/ecdsa`
build in that export (the FROST registry stack the world deploys), and the
test binary from the same export (tuxxy/keep-core#22).

## What runs
`pkg/tbtc/k09_lifecycle_test.go`, one stage per process, in this order:

| Stage | Starts from | What the stage asserts (chain state and Bitcoin Core) |
| --- | --- | --- |
| 01-legacy | built | 40a11d1d Bridge: legacy source in MovingFunds with a main; no FROST surface |
| 02-upgrade | 01 | proxy upgraded in place; legacy wallets and Bank balance unchanged; LegacyOpen |
| 03-dkg | 02 | nodes take a 5-seat wallet {1,1,2,2,3} (threshold 3) to ReadyUnfunded |
| 04-drain | 03 | in a base moving-funds window the nodes, holding the legacy source's tECDSA signers (tecdsatest FIXTURE shares), commit the ECDSA-to-FROST drain to the FROST wallet alone (a node operator sends it) and sign and broadcast it (base MovingFunds); maintainer proves it; source Closing; `K09DRAIN` record (#40). Then nodes sweep the incoming output in a base window; Bridge main = sweep output, Core confirmed |
| 05-redemption | 04 | nodes reserve and pay a redemption; Core pays the redeemer; Bridge main = change |
| 06-restart | 05 | all nodes SIGKILLed right after startup (not mid-action: batch 4 owns that matrix) and restarted; the restored legacy signers drain a second legacy source (as 04, `K09DRAIN`); a second incoming sweep settles; no DKG result submitted |
| 07-reservation-timeout | 04 | nodes reserve, then go down; keeper times the request out; wallet MovingFunds |
| 08-deposit-refund | 06 | after base's 2-hour deposit age, nodes sweep a revealed, reserved FROST deposit with the main (one spender); maintainer proves it; Bridge main = sweep output; depositor credited deposit - treasury fee - sweep fee; reservation released. Depositor refunds alone after locktime |
| 09-movement | 06 | aged wallet A in MovingFunds, target B ready: nodes commit A's targets and move A's main to B (one spender); maintainer proves it; A's custody 0, B holds the pending incoming output |
| 10-inactivity | 06 | operator 2 (two seats, 60% active) down for up to six heartbeat windows: exactly one claim, naming operator 2's seats and operator, heartbeatFailed; the wallet moves funds |
| 11-cutover-closure | 06 | cutover to FrostOnly; legacy creation refused; nodes still make a FROST wallet; legacy source and an empty FROST wallet close |
| 12-no-inactivity | 06 | operator 3 (one seat, 80% active, above the 70% threshold) down for the same six heartbeat windows: no claim |

Stages 01-04 run in order; 07 runs beside 05-06; after 06, 12 and 09 run in
one lane and 10, 08, 11 in another (each test process at GOMAXPROCS 1, so two lanes
use 8 Go cores). Wrong-behaviour probes for 08-10 and 12:
`test/k09/controls/README.md`.

The harness is depositor, redeemer, SPV maintainer, keeper and governance
(D17) with the admin key; nodes are started through the same `tbtc.Initialize`
call `cmd/start.go` makes (batch 6 seam). World, fixtures and snapshots:
`pkg/tbtc/k09_world_test.go`, on batch 6's world; the legacy ECDSA side
(tecdsatest groups seeded into the nodes, the registry fixture's member, the
EcdsaDkgValidator FIXTURE): `pkg/tbtc/k09_ecdsa_test.go`. The harness signs
only setup transactions with a legacy group (funding a source's main).

## Evidence
The run writes `manifest.json` (both repo heads, pins, the ECDSA build record
`dependencies.ecdsaBuild`, every stage's result and times, the node-log hashes
`nodeLogs`, the secret scan, leftover processes), one log per stage and the
node logs (`nodes/<stage>/operator-N.log`; never node stores).
`test/k09/evidence` checks the build record, the node-log hashes and the
drain records on the evidence directory. `accepted` is
true only with every stage passing, zero skips, a clean tree, no secret-scan
hit and no leftover process. A stage that fails at an assertion starting
`FINDING` found missing or wrong production behaviour; see K09-RUN.md.
