# K-09 lifecycle demonstration

KC-SF-V3 §6 K-09: the FROST lifecycle driven through real node processes on
the integration branch. Local only; nothing here runs in CI.

```
source <env with SNOWFALL_WORKER*, LEGACY_*>; test/k09/run.sh <new evidence dir>
```

## What runs
`pkg/tbtc/k09_lifecycle_test.go`, one stage per process, in this order:

| Stage | Starts from | What the stage asserts (chain state and Bitcoin Core) |
| --- | --- | --- |
| 01-legacy | built | 40a11d1d Bridge: legacy source in MovingFunds with a main; no FROST surface |
| 02-upgrade | 01 | proxy upgraded in place; legacy wallets and Bank balance unchanged; LegacyOpen |
| 03-dkg | 02 | nodes take a 5-seat wallet {1,1,2,2,3} (threshold 3) to ReadyUnfunded |
| 04-drain | 03 | Setup by the harness (FIXTURE key): the ECDSA-to-FROST drain, proven, source Closing. Asserted: nodes sweep the incoming output in a base window; Bridge main = sweep output, Core confirmed |
| 05-redemption | 04 | nodes reserve and pay a redemption; Core pays the redeemer; Bridge main = change |
| 06-restart | 05 | all nodes SIGKILLed right after startup (not mid-action: batch 4 owns that matrix) and restarted; a second incoming sweep settles; no DKG result submitted |
| 07-reservation-timeout | 04 | nodes reserve, then go down; keeper times the request out; wallet MovingFunds |
| 08-deposit-refund | 06 | after base's 2-hour deposit age, nodes sweep a revealed, reserved FROST deposit with the main (one spender); maintainer proves it; Bridge main = sweep output; depositor credited deposit - treasury fee - sweep fee; reservation released. Depositor refunds alone after locktime |
| 09-movement | 06 | aged wallet A in MovingFunds, target B ready: nodes commit A's targets and move A's main to B (one spender); maintainer proves it; A's custody 0, B holds the pending incoming output |
| 10-inactivity | 06 | operator 2 (two seats, 60% active) down for up to six heartbeat windows: exactly one claim, naming operator 2's seats and operator, heartbeatFailed; the wallet moves funds |
| 11-cutover-closure | 06 | cutover to FrostOnly; legacy creation refused; nodes still make a FROST wallet; legacy source and an empty FROST wallet close |
| 12-no-inactivity | 06 | operator 3 (one seat, 80% active, above the 70% threshold) down for the same six heartbeat windows: no claim |

Stages 01-04 run in order; 07 runs beside 05-06; after 06, 12 runs in one lane
and 10, 08, 09, 11 in another (each test process at GOMAXPROCS 1, so two lanes
use 8 Go cores). Wrong-behaviour probes for 08-10 and 12:
`test/k09/controls/README.md`.

The harness is depositor, redeemer, SPV maintainer, keeper and governance
(D17) with the admin key; nodes are started through the same `tbtc.Initialize`
call `cmd/start.go` makes (batch 6 seam). World, fixtures and snapshots:
`pkg/tbtc/k09_world_test.go`, on batch 6's world.

## Evidence
The run writes `manifest.json` (both repo heads, pins, every stage's result
and times, the secret scan, leftover processes), one log per stage and the
node logs (`nodes/<stage>/operator-N.log`; never node stores). `accepted` is
true only with every stage passing, zero skips, a clean tree, no secret-scan
hit and no leftover process. A stage that fails at an assertion starting
`FINDING` found missing or wrong production behaviour; see K09-RUN.md.
