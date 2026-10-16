# K-06 deposits, sweeps and depositor refunds

This package is the host side of KC-SF-V3 K-06. It has no production dispatch:
like K-04 and K-05, it runs only with an explicit `LocalRegtest` executor. The
V-07 predicates and hook names are fixed in
[`verification/BOUNDARIES.md`](verification/BOUNDARIES.md).

## Pieces

| File | Role |
| --- | --- |
| `template.go` | The proposed D-04 template: receipt, wallet and refund leaves, NUMS internal key, BIP341 tweak, control blocks. Byte-equal to C-05 `FrostDepositProfile`. |
| `refund.go` | The depositor refund, built from public data and the refund key only. |
| `view.go` | One consistent read of reveals, deposit records and custody, plus Bitcoin median-time-past. |
| `planner.go` | The round's sweep, built deterministically from finalized C-05 state at the anchor block. |
| `authority.go` | `signing.SweepAuthority`: the per-grant check of every deposit and of the C-05 main rule. |
| `lock.go` | The VD-07 main purpose lock and the executor grant guard. |
| `sweeper.go` | Rounds, signing, broadcast, quarantine and outcome, recorded in the K-05 ledger. |
| `resolution.go` | The C-04 sweep-resolution certificate, signed after the lock moves to resolution. |

## Rules the host enforces

1. A sweep grant needs these, read after the durable claim:
   - every deposit is revealed at a finalized canonical block for this wallet;
   - every deposit is unswept;
   - Bitcoin median-time-past plus the refund margin is before each refund locktime;
   - every input is exactly the C-05 record and the D-04 construction for Q;
   - the main follows the C-05 rule.
2. A main output belongs to one purpose at a time:
   - **Redemption.** Taken at attestation. Freed only by finalized non-admission or a finalized spend.
   - **Sweep.** Taken at the claim. It stays until a finalized spend, even when the sweep can no longer confirm.
   - **Resolution.** Taken before the certificate is signed. After that, no payment or cancellation grant for the reservation is issued.
3. A sweep signature that completes after its authorization lapsed is quarantined and never broadcast.
4. The maintainer proves a sweep at most once. It settles a lost reply by rereading C-05.

The fixed contract vectors are in `testdata/deposit-profile-v1.json`, a
byte-for-byte copy from tbtc-v2 with its SHA-256 pinned in the test. The local
acceptance record is `test/frost/K06.md`.
