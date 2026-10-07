# K-04 exact transaction authorization

This package is the inactive, local K-04 signing path. It accepts a complete
redemption proposal and builds an immutable plan before starting a worker.
`NewLocalExecutor` requires the Bitcoin regtest genesis and an explicit local
opt-in. The production node does not dispatch FROST transactions.

## Authorization and signing

1. `NewPlan` validates the immutable K-03 descriptor, complete ordered prevouts,
   exact recipients, one exact-Q change output, conflict input and fee policy.
   It copies every public input. Its versioned binary commitment also binds the wallet,
   deployment, Bitcoin genesis, request IDs, purpose, generation and state version.
   [INTENT-V2.md](INTENT-V2.md) fixes the encoding; JSON is local recovery data only.
2. The executor loads the durable ready wallet and completed local seats. It
   resolves each previous transaction, checks its hash, index, value and full
   script, then checks confirmations and unspent state. Decoder counts are
   bounded before allocation. No HASH160 wallet query is used.
3. The reservation provider must return one consistent snapshot at the current
   canonical head. The executor requires `Reserved`, the exact plan commitment and state version,
   the configured finality depth, and an unchanged block receipt. It durably pins
   the first receipt under the plan/reservation ID before retaining the
   complete public intent through the existing encrypted, fenced claim path.
4. Each input gets a separate attempt bound to the plan, reservation, agreed
   session, input index and start block. The guarded engine claims that attempt
   durably. The engine refuses a missing intent or callback before claiming.
   Its authorization callback then rechecks the wallet, Bitcoin inputs
   and reservation immediately before worker startup.
5. The real worker produces a 64-byte BIP340 signature. The host verifies it for
   the exact 32-byte digest, including leading zeros. Typed completion data binds
   the wallet, deployment, network, purpose, reservation, plan, input and attempt.
   A signature that arrives after a reservation change is retained and refused.
6. Each completed result is retained. `LoadResult` verifies it and its durable
   reservation receipt before recovery.
   `Broadcast` copies and revalidates the result and reservation before sending
   the exact bytes. Recovery does not generate another signature.

The Bitcoin profile is version 2, exact `OP_1 PUSH32 Q` key-path spends,
SIGHASH_DEFAULT, no annex and one 64-byte witness item per input. Q is already
SNOWFALL's output key. No second TapTweak is applied. Local bounds are 256 inputs,
256 outputs, 100,000 stripped transaction bytes and 100,000 bytes per resolved
previous transaction. These are local limits, not Bitcoin consensus limits.
Only redemption is admitted here. Requests contain resolved net entitlements;
legacy recipient script policy is unchanged. Other purposes require their own
validators before this package can admit them.

## Provider obligations and next milestone

The local reservation provider is a controlled test boundary. It is not a
C-04 contract adapter. C-04/K-05 must derive requests and fees from funded chain
state, verify the reservation certificate and request state, enforce a consistent
canonical read, and map that state to this plan commitment. Event matching alone
is insufficient. The two-block finality depth, confirmation count and N=3/t=2
fixture are test policy, not approved production settings.

K-05 also owns reservation acquisition, retries, timeout/conflict resolution,
settlement/cancellation and production routing. Cross-operator agreement on
`Session`, `StartBlock`, selected seats and executor configuration is also a
K-05 deliverable. K-04 injects those values in the fixture; it implements no
FROST coordination protocol. Retaining a late signature here
does not establish those lifecycle properties. The K-03 `ReadyUnfunded` wallet
is funded manually with regtest-only coins for this local witness; this is not
a production funding transition. No mainnet funds or active-selection switch
are involved.

`WalletIdentity` and `bitcoin.Signature` are explicit tagged unions. The existing
ECDSA transaction executor uses the common typed route and keeps its original
signer and serializer. FROST uses its own context-checked completion record;
it never enters the ECDSA coordination/done protocol or receives a fabricated
ECDSA public key. Cache and monitor identities are scheme-aware. Monitoring keeps the full
32-byte FROST wallet ID. `ECDSAPublicKeyHash` refuses FROST, and `FrostWalletID`
refuses ECDSA; no truncated FROST value is exposed as a Bridge lookup label.

The predicate and test hook contract is in `verification/BOUNDARIES.md`.
V-04 model checking and correspondence review are still required before merge.
Tests do not extend any earlier bounded model statement to this new path.
