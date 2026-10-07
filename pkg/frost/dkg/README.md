# K-03: public approval and durable readiness

This package connects one real SNOWFALL DKG to the separate FROST registry and
Bridge. `tbtc.ExecuteFrostDKG` is the explicit local entrypoint. The normal node
configuration still rejects FROST activation. K-04 and funded use are not enabled.

## Lifecycle

1. Read group policy from the FROST validator. Read the frozen original seat
   selection from the FROST pool. Require every seat owned by this operator.
2. Start the authenticated host channel and public-result monitor before DKG.
   Worker traffic uses a different channel. One worker owns all local seats.
3. Check the public candidate against the exact profile, selection and threshold.
   Persist its immutable descriptor. Collect signatures from every original
   seat. The operator of seat 1 publishes the result through the FROST registry.
4. Challenge an invalid foreign result. Keep the acceptance callback alive after
   submission. Approve only the exact agreed result after the challenge period.
5. Read the canonical approval event and an atomic registry snapshot at one block hash. Match the
   registry, epoch, result hash, wallet ID and descriptor. Wait the configured
   finality depth and read again before delivering the worker receipt. An
   observed approval that disappears or changes quarantines the attempt.
6. Let the worker finish; durably install `KeyReady`. Claim a fresh sign-purpose
   attempt and reload every local completed key in a fresh worker. Reload sends
   bootstrap, begin and cancel only. It sends no protocol traffic and allocates
   no signing nonce. A candidate alone cannot pass this loader.
7. Sign each local seat's reference hash. Collect the configured number of
   distinct selected seats and submit the public certificate. Confirm
   `ReadyUnfunded` and persist that state. This is not funding or activation.

The protocol context must have a deadline. An RPC snapshot at a different block
is discarded. Up to five read-only attempts are allowed to obtain a coherent
snapshot; a reorg, bad receipt or persistent mismatch quarantines the attempt.
This does not retry a worker command or resume a DKG.

Competing readiness and expiry transactions use at most five read-only checks
within one second to classify the public wallet state. The executor joins its
monitor and receiver before choosing a failure cause. A clean expiry remains
`Closed` without quarantine even if its local RPC was canceled. Approval
conflicts still take precedence and remain quarantined.

The implementation polls filtered canonical events with storage reads, rather
than relying on unfiltered event notifications. Transport overflow, unknown
profiles, chain mismatch, store failure and worker failure stop this attempt.
There is no retry or resume of candidate records, including after an apparently
successful receipt.

## Frozen encodings

Only `frost.ApprovedProfile` maps to chain profile `1`; the string comparison is
exact. Scheme `2` denotes this FROST descriptor; legacy ECDSA is scheme `1`.
The chain payload is exactly `profile[1] || Q[32] || SnowfallDescriptor[32]`.
The host never parses a secret-bearing candidate record.

`FrostTypes.Descriptor` is the canonical ABI tuple:
`(uint8 scheme, uint8 profile, uint256 chainId, address registry, uint64 epoch,
uint32[] members, address[] operators, uint16 threshold,
bytes32 snowfallDescriptor, bytes32 outputKey)`.
The epoch is the registry request's Ethereum block number. Members retain pool
IDs; array positions define the original one-based protocol seat IDs. Repeated
pool IDs represent multiple seats of one operator and are not deduplicated.

Wallet ID is `keccak256(keccak256("tbtc-v2/frost-wallet-id/v1") || Q)`; its first
20 bytes are the Bridge lookup label. The full ID, Q and descriptor remain stored.
Descriptor hash is `keccak256(abi.encode(DESCRIPTOR_DOMAIN, descriptor))`.
The domain is `keccak256("tbtc-v2/frost-descriptor/v1")`.

Result signatures use Ethereum personal-sign over the ABI hash of
`RESULT_DOMAIN, chainId, registry, pool, epoch, payload, misbehavedIndices,
members, threshold`. `RESULT_DOMAIN` is `keccak256("tbtc-v2/frost-result/v1")`.
The initial profile permits no removed seats and requires signatures at every
original position. The full result hash also commits to the container fields
and ordered signatures. It is checked against the approval event and storage.

Readiness signatures use Ethereum personal-sign over the ABI hash of
`READY_DOMAIN, chainId, registry, profile, walletId, descriptorHash, epoch,
generation, capabilityVersion, seat, referenceHash`.
`READY_DOMAIN` is `keccak256("tbtc-v2/frost-ready/v1")`. Generation and capability
version are both `1` for this inactive milestone. `referenceHash` is Keccak-256
of the opaque local completed-key reference. Solidity and Go use separate,
explicit bindings. The tests check the ABI hashes independently.

## Failure and authority rules

Registry requests and Bridge requests start disabled. Only the Bridge can
request a registry DKG; only the configured beacon can supply its seed. Before
locking the new pool, the registry refreshes every admitted operator's stake.
The bounded allowlist defaults to deny. It reads the existing stake source;
it never mutates the legacy registry or pool. Reward withdrawal pays the calling
operator only. No reimbursement target or automatic refund is supported.

All deadlines use Ethereum block numbers. Seed expiry is request block plus
seed timeout. Result expiry is seed callback block plus result timeout. Submission
must leave a full challenge interval before result expiry. Approval is permitted
at or after submission block plus challenge period, but before result expiry.
Pending readiness expires at approval block plus readiness period. A certificate
at or after that block rejects; collection never extends the deadline.

A worker exit before durable `KeyReady` loses every local seat. `DKGStatus`
retains the original claim and explicitly lists those lost seats. Before a seat
sends its worker readiness statement, its loss blocks full-roster completion.
After that statement, surviving seats can complete and certify if the policy
allows it. Lost seats cannot enter the host certificate. Expiry closes the
pending wallet unfunded and retains the identity/Q tombstones. A fresh epoch
uses a different store domain; it does not clear the failed epoch's claims.

The Bridge stores FROST metadata separately from legacy wallet states and live
counters. Its fixed linked extension accepts only `IFrostBridge` selectors.
Registration is pending, readiness is unfunded, and there is no Live transition
in this milestone. Legacy funding paths see no FROST live wallet. Exact raw and
length-prefixed P2TR outputs resolve through Q; unknown encodings reject.

## Review and qualification boundaries

The local fixture uses N=3, t=2, r=2, b=0, f=0, with both one-seat operators and
an operator holding two seats. These values are not a production policy. The
host verifies `r >= t+b+f`; readiness is an operator assertion, not proof of
honest storage. The fixture replaces only external beacon/stake/token inputs.
Its registry, validator, pool, Bridge/proxy, worker, libp2p and encrypted journal
are real implementations. Its in-memory fence survives test store reopen but
is not evidence of durability through host loss.

Run instructions and the acceptance matrix are in `test/frost/README.md`.
Production qualification still needs D-01 distribution pins, D-02/D-11 group
and abort policy, D-09 storage/finality deployment assumptions, SF-03 capacity,
and the C-07 curve primitive's review/qualification. The curve checks here are
executable local admission checks, not a completed C-07 proof. Historical K-02
model/replay results do not prove this changed source. No new model or mutation
campaign is claimed; the final integration mutation campaign remains K-09.
