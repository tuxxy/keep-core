# K-02 authenticated transport

This adapter uses the existing keep-core broadcast provider and membership
validator. Use the canonical `Domain.NewAttempt` and
`snowfallengine.AttemptID`; `NewGuarded` rejects a transport with a different
binding. Each node has one transport for all of its local seats.

A stable network/chain/registry/wallet-or-epoch/purpose topic is reused across
retries. Every packet separately binds the exact attempt, protocol version,
sender, recipient and kind. Retrying does not allocate another permanent
libp2p topic. Membership checks bind the authenticated operator public key to
the exact selected seat, including multiple seats for one operator.

The public protobuf schema is `message.proto`. Its small explicit codec uses
protobuf wire primitives and rejects unknown fields, duplicate fields,
noncanonical ordering/varints and oversized encodings. Worker envelopes remain
byte-exact. Only their public routing fields are read.

For each attempt and local seat, create a fresh ephemeral ECDH key using the
existing `pkg/crypto/ephemeral` mechanism. Authenticate its announcement through
the existing channel. Remote private round-two packets are encrypted with that
mechanism, including the complete outer context header inside the authenticated
plaintext. Only the recipient can decrypt. Same-node private delivery enters
the local receive queue without a public send. A delayed key announcement
permits bounded buffering of ciphertext; it never turns ciphertext into an
unauthenticated worker input.

`New` requires a deadline. Sends retain that operation context through
retransmission. Both transport serialization and libp2p publication respect
cancellation. Call `Close` when the operation ends. Duplicate worker envelopes
are idempotent. Overflow fails the operation instead of allowing unbounded
queues or silently losing authenticated traffic.

Local defaults and hard limits:

| Resource | Default | Hard limit |
| --- | ---: | ---: |
| Incoming and pending-ciphertext entries | 256 each | 4096 each |
| Distinct sent/received envelopes per attempt | 4096 each | 65536 each |
| Worker envelope | 8448 bytes | 8448 bytes |
| Outer packet | 8960 bytes | 8960 bytes |
| Seat IDs supported by this pinned adapter | 1–100 | 1–100 |

These limits match or narrow the current client boundary. They do not qualify
every allowed production seat allocation. SF-03 and the final deployment
profile remain required. Test providers and test finality never establish
production chain approval.

Run focused checks with `go test -count=1 ./pkg/frost/transport -run
'TestSeatAttribution|TestAttemptReplay|TestReordered|TestPending|TestForeign'`.
The real-worker test also checks libp2p exchange, encrypted journal reopen,
permanent signing attempts, fresh-worker key reload, independent BIP340
verification and topic reuse. Set the explicit worker path and hash as shown in
the parent README; a skipped real-worker test is not acceptance.
