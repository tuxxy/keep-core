# K-05 FROST redemption host

This package connects the K-04 exact-authorization executor to the C-04 Bridge
redemption API (tbtc-v2 `IFrostBridge`). It implements KC-SF-V3 K-05 and the
host side of section 4.6. It is local and inactive. No production policy value
is chosen here. Each open owner value is listed in "Open items".

## Components

| File | Role |
| --- | --- |
| `chain.go` | `Chain`, the one narrow C-04 seam: consistent canonical-head reads, event discovery and the permissionless writes. Production uses `pkg/chain/ethereum` (`FrostRedemptionChain`). Replay uses a controlled implementation. |
| `reservations.go` | `ChainReservations` implements `signing.Reservations` from the authoritative chain record. It maps the C-04 intent enum to the executor's state strings, the admission block and its canonical hash, the head and the counters. |
| `planner.go` | Builds a `signing.Proposal` from funded chain requests. It derives net values from the captured gross, treasury fee and fee share. It discovers P2TR inputs by the exact `OP_1 PUSH32 Q` script. It rejects zero or negative nets, invalid fee domains and overflow before any signature request. It sizes the fee from the actual Taproot transaction weight. |
| `attestation.go` | The honest attester. It rebuilds the plan independently, checks the certificate tuple, and takes a durable compare-and-insert lock per `(wallet, generation, stateVersion)`. History is kept and the replacement rule is enforced. It then signs the C-04 certificate digest. |
| `agreement.go` | Discharges `A-K04-AGREED-EXECUTOR-CONFIG`. Session, start block, selected seats and retry schedule are derived only from the finalized on-chain admission record, so every operator computes the same values with no extra protocol. |
| `ledger.go` | Durable signed-intent ledger. These records are append-only and never erased: attested, admitted, possible release, signed, broadcast, late signature quarantined, escalation and outcome. Each record goes through `RetainSigningRecord` (immutable, identical writes idempotent). |
| `resolver.go` | The per-reservation recovery state machine (below). |
| `reload.go` | Fresh-worker reload of the completed key before every attestation (the C-04 honest-host rule). |
| `../signing/cancellation.go` | Exact cancellation plans. Each spends the stored ordered inputs into one exact-Q output with a fee inside the original envelope. The attester requires that fee to be strictly greater than every admitted alternative. The executor grants the worker only after the C-04 alternative is admitted and finalized. |

The maintainer side lives in `pkg/maintainer/spv/frost_redemptions.go`:
payment and cancellation proofs, timeout notices, unreserved and resolved
refunds, and budget claims. It is exposed as `maintainer-cli frost-redemptions`.

## Resolver states

| Chain intent | Local ledger | Host action |
| --- | --- | --- |
| Reserved | no signed result | Sign at the agreed start block (executor `Run`), using the agreed config |
| Reserved | signed result | Broadcast the exact frozen transaction; the maintainer proves it |
| TimeoutPending / Quarantined | signed before the timeout | Policy `RebroadcastThenCancel` or `AlwaysCancel` ([spec issue #3](https://github.com/tlabs-xyz/FROST-migration-spec/issues/3), OPEN) |
| TimeoutPending / Quarantined | possible release, no result, or a late result | Never sign or broadcast the payment again; record quarantine; build and attest a cancellation |
| Paid / Cancelled / ConflictResolved | any | Record the outcome; claim unused budgets |

A signature that completes after the reservation leaves `Reserved` is
recorded as `LateSignatureQuarantined`. The host never broadcasts it.
Historical SPV settlement of an exact admitted payment stays a contract
decision. That is [spec issue #2](https://github.com/tlabs-xyz/FROST-migration-spec/issues/2)
(A5), still OPEN, and K-05 claims neither reading.

## Named boundaries (V-04 replay hooks)

The K-04 executor hooks keep their names. K-05 adds `before-rebroadcast` to the
executor. These hooks use the same `boundary func(string) error` pattern and
are nil in production:

- Attester: `before-attest-lock`, `after-attest-lock`, `before-cancel-attest`.
- Resolver: `agreement-derived`, `before-sign`, `after-sign`,
  `before-payment-broadcast`, `before-cancel-broadcast`, `timeout-seen`,
  `outcome-seen`.

Cancellation signing reuses the executor's own hooks under the cancellation
plan.

## Open items (owner decisions; local test values only)

- D-02, D-11: the seat and threshold policy (local 3 seats, threshold 2).
- D-06: the recipient set (legacy script types only).
- D-08: the legacy transition. FROST and ECDSA requests are kept apart and tested; production adoption is OPEN.
- D-09: finality and fees (local 2 blocks; a fixture fee rate).
- `A-FEE-INCLUSION-ESCALATION`: the escalation window, monitor and above-envelope recovery.
- Spec issues #2 (A5) and #3 (the TimeoutPending policy).
