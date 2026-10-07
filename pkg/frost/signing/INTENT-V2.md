# K-04 signing-intent commitment, version 2

This is the normative host/client byte contract for the K-04 reservation
commitment under KC-SF-V3 revision 01. It is part of PR #11 and must be reviewed
before V-04 bounds and correspondence are frozen. C-04/K-05 must use these bytes
or introduce a new version and repeat the affected verification. JSON is local
recovery data only; contracts and TypeScript clients do not reproduce Go JSON.

## Encoding

The commitment is one SHA-256 of the following concatenation. Integers are
unsigned and big-endian except where the snapshot sub-format explicitly says
otherwise. No padding, field names, base64, separators or trailing bytes are
allowed. Fixed byte strings contain exactly their stated number of bytes.

| Order | Field | Encoding |
| --- | --- | --- |
| 1 | Domain | Exact ASCII `keep-core/frost/signing-intent/v2/`, no NUL |
| 2 | Version | One byte, `0x02` |
| 3 | Transaction snapshot commitment | 32 bytes, defined below |
| 4 | FROST wallet ID | 32 bytes, the complete K-03 wallet ID |
| 5 | Immutable descriptor hash | 32 bytes, the K-03 deployment-bound hash |
| 6 | Bitcoin genesis hash | 32 bytes in Bitcoin wire/internal hash order |
| 7 | Purpose | One byte, `0x01` = redemption; all other values reject |
| 8 | Generation | uint64 |
| 9 | State version | uint64 |
| 10 | Number of requests | uint16 |
| 11 | Each request in transaction recipient order | ID (32 bytes), net value (uint64 satoshis), script byte length (uint16), exact script bytes |
| 12 | Conflict transaction hash | 32 bytes in Bitcoin wire/internal hash order |
| 13 | Conflict output index | uint32 |
| 14 | Minimum fee | uint64 satoshis |
| 15 | Maximum fee | uint64 satoshis |
| 16 | Maximum fee rate | uint64 satoshis per virtual byte |

Do not reverse wallet IDs, descriptor hashes, keys or SHA-256 output bytes.
Genesis and transaction hashes use the internal bytes stored in `bitcoin.Hash`;
reverse RPC/display hash hex before placing it in these fields. Request order
is significant. Requests are not sorted by ID. Generation and state version
must be positive. The local profile permits 1–255 distinct nonzero request IDs,
positive net amounts and one positive exact-Q change output. The constructor
checks amounts, scripts, fees, count limits and conservation before encoding;
no signed integer is cast to uint64 before that validation. Future purposes,
new fields or changed widths require a new domain/version, not a silent v2 edit.

The snapshot is SHA-256 of:

1. Exact ASCII `keep-core/taproot-snapshot/v1` (no NUL).
2. The 32-byte x-only output key Q, with no second tweak.
3. The complete unsigned transaction in canonical non-witness Bitcoin wire
   serialization, including version, ordered inputs, sequences, outputs and
   locktime. No input has scriptSig or witness data.
4. For every input, in input order: previous value as uint64 **little-endian**,
   then canonical Bitcoin CompactSize script length and exact previous script.

The transaction wire encoding gives its own unambiguous end and input count.
That count also defines the number of previous-output entries. Each declared
prevout must match that input's outpoint and the wallet's exact output script.
`Intent.TransactionHash` is this snapshot commitment, not the Bitcoin txid.

## Fixed interoperability vectors

`testdata/intent-v2.json` contains two public fixtures with full preimages and
commitments. Numeric fields that can need 64 bits are decimal strings in the
fixture file; an implementation must use integer arithmetic, not JavaScript
`Number` conversion. The second fixture uses generation `18446744073709551615`
and state version `9007199254740993`.

| Fixture | Commitment |
| --- | --- |
| redemption | `a80a694a524a7a4a38847b17c1a05c9c7bf7cde52f4bbcac4df084e7dba47628` |
| wide uint64 | `66a1b3894a541f154f5a7e6f49952e0c890cd9fe8b5df1395971ce2c59eb38ea` |

`TestCanonicalIntentV2` checks the Go encoder against the fixed bytes.
`test/frost/check-k04-intent.ts` independently rebuilds the snapshot and intent
with explicit field widths, endianness and `BigInt`, then checks both vectors.
Run it with Node 22: `node --experimental-strip-types test/frost/check-k04-intent.ts`.
`TestCanonicalIntentBindsEveryField` checks each commitment field. Purpose is
fixed to redemption by `NewPlan`; its other-purpose rejection is tested there.

## Recovery and version boundary

The earlier K-04 draft used a v1 JSON-derived hash. Version 2 deliberately gets
a new domain and record version. No implicit migration, attempt reuse or claim
reset is allowed. Previous v1 local test records keep their old identity and
cannot be loaded as v2 results. No production funds used that draft path.

The finalized reservation block/anchor is retained separately before the first
worker grant. It is not part of the pre-reservation commitment: that would be
circular. Its immutable record ID binds the plan and reservation ID. Completed
results carry the same receipt; recovery and broadcast compare it with durable
storage, and a new block/hash refuses even if the provider repeats the same
reservation ID and plan. C-04 need not supply an unstated immutable-ID assumption
for that local fail-closed behavior.
