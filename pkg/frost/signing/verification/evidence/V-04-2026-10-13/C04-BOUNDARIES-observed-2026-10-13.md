# C-04 reservation and settlement boundaries

Work in progress on `integration/c04-redemption-reservations`, based on K-03
`76f79051`. Governing documents: KC-SF-V3 revision 01 sections 4.6 and C-04;
KC-VER revision 01 V-04 and V-05. No C-04 exit is claimed by this file.

## Certificate boundary

`FrostReservationCertificate` validates the existing Ethereum operator signature
mechanism against the wallet's frozen descriptor. It does not introduce a new
threshold signing scheme. Signatures use Ethereum personal-sign over the
Keccak-256 hash of the Solidity ABI encoding of:

1. `keccak256("tbtc-v2/frost-redemption-reservation/v1")`;
2. the current Ethereum chain ID;
3. the Bridge proxy address;
4. the reservation tuple defined in the library.

The tuple binds the full wallet ID and descriptor hash, uint64 intent generation
and state version, K-04 binary intent commitment, request ID commitment,
reserved conflict outpoint and fee policy. The request ID commitment is
`keccak256(abi.encode(bytes32[] requestIds))` in the exact plan order. The
consumer must recompute it from current request records and recompute the
K-04 commitment from their authorized transaction fields. Supplied hashes alone
do not establish funded requests, exact amounts, recipient scripts or custody.

`FrostSigningIntent` reconstructs the K-04 v2 commitment from the snapshot,
genesis, reservation fields and ordered net recipient entitlements. Its two
fixed vectors match the Go/TypeScript fixtures, including generation at uint64
maximum and state version above JavaScript's safe integer range. It validates
recipient script shapes and nonzero distinct request identities. The consumer
must still derive those entitlements from escrow, validate fee policy and build
the snapshot from exact transaction bytes and recognized custody prevouts.
The encoder is not a substitute for those funded-state and Bitcoin checks.

Seats are one-based descriptor positions. Their indices must increase strictly;
each index counts once. A repeated operator address is allowed when one operator
holds multiple distinct selected seats. Certificate quorum is the descriptor's
signing threshold, not the full-roster DKG result quorum.

Only a descriptor loaded from the registered wallet may be passed to validation.
The caller must compare current wallet generation, state version and capability,
check request eligibility and funding, and atomically install the reservation.
The library is not an externally callable reservation or a funding adapter.

## Required consumer transitions

An unreserved timeout cannot refund until the configured finality boundary.
A finalized reservation is required before the host grants worker signing.
A reserved timeout enters TimeoutPending; elapsed time alone cannot refund it.
Payment proof settles payment once. A confirmed cancellation consumes the same
reserved conflict input, preserves custody under the authorized fee/output
policy, updates custody accounting once, and only then permits refund.
Every alternative or replacement must share that conflict input. An assertion
that no signature escaped cannot cancel a reservation.

## Work still required for this milestone

- Integrate funded request escrow and reservation state into additive Bridge
  storage and dispatch, with unchanged legacy request identities and fees.
- Implement canonical transaction/intent checks, request eligibility, operator
  certificate admission and replay rejection through public entrypoints.
- Implement timeout, payment proof and custody-preserving cancellation proof,
  complete accounting and rollback frames, plus losing-signature defenses.
- Add current-head Go reservation adapter and K-05 operator agreement; exercise
  public node and maintainer paths against actual contracts and Bitcoin Core.
- Replay V-04 against these concrete transitions; record V-05 correspondence.
- Resolve D-06/D-08 release policy and legacy outstanding-intent reconciliation;
  local test values do not approve production refunds or accrued deadline changes.

Required witnesses are funded request/reserve/sign/mine/prove/settle; timeout
before and after reservation/share release/broadcast; late valid payment;
confirmed cancellation followed by refund; crash recovery and unchanged legacy
settlement. Required rejection checks preserve the full financial state frame.
Mutation testing is deferred to the agreed checkpoints.
