# K-09 wrong-behaviour probes (batch-7 acceptance stages)

The node does none of these duties yet (findings F1-F3), so a wrong node
cannot be run. Each probe instead makes the wrong outcome happen on the real
chains, as a wrong node would, and shows that the stage's own assertion
rejects it. Apply one patch to `pkg/tbtc/k09_lifecycle_test.go` in a scratch
tree (never on the branch) and run:

```
GOMAXPROCS=2 go test -count=1 -timeout 14m -run '^TestK09Lifecycle$/^(0[1-6]|<stage>)' -v ./pkg/tbtc
```

| Patch | Stage | Wrong behaviour (review item) | Must fail at |
| --- | --- | --- | --- |
| `b1-deposit-any-spend.patch` | 08 | A spend of the deposit that is not a sweep with the main (the depositor's refund, confirmed by Core) is taken as the sweep (B1) | `assertDepositSweep`: "the deposit sweep ... does not spend the wallet's main" |
| `b3-commit-only.patch` | 09 | Seat 1 commits A's targets on chain; nothing moves the funds (B3) | "FINDING ... A's targets were committed by window N, but no node broadcast the movement" |
| `b4-wrong-seat.patch` | 10 | A valid claim (threshold seats sign) naming operator 3's seat (B4) | "the inactivity claim names seats [5] ...; want operator 2's seats [3 4]" |
| `b4-no-heartbeat-flag.patch` | 10 | A valid claim naming operator 2's seats with heartbeatFailed false (B4) | "... with heartbeatFailed false; want ... heartbeatFailed true" |
| `b5-absolute-70.patch` | 12 | With 4 of 5 seats active, a heartbeat-failure claim of operator 3's seat, as base's absolute rule (70 members) would make (B5) | "with 4 of 5 seats active (80%, above the 70% activity threshold), an inactivity claim reached the Bridge" |

B2 (the 2-hour deposit age) has no probe: it is a precondition a correct
node needs, not a rejection. Base checks `block.timestamp > revealedAt +
DEPOSIT_MIN_AGE` (`WalletProposalValidator.sol`), and stage 08 moves chain
time past it before the window. Results: K09-RUN.md.
