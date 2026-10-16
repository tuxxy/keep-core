# K-02 durable journal

This inactive component stores opaque SNOWFALL records under
`<keystore>/snowfall`, outside the legacy ECDSA loader. Use `Open`, then `Scope`
with the exact network, chain, registry and wallet/epoch domain. `NewGuarded` in
`snowfallengine` uses the scoped journal. The node activation switch still rejects
FROST. There is no production fence provider in this change.

## Durability and ownership

`Open` requires a private 0700 keystore directory or creates it and synchronizes
its parent. It takes a Unix process lock and an independently exclusive fence
lease. An immutable root identity prevents accidental reuse with a new storage
identity. The fence lease must not expire or transfer while a worker can run.
`Close` refuses while an operation holds its claim lease.

Every update writes an immutable encrypted snapshot. The protocol is: write a
0600 temporary file; synchronize the file; install it by a no-overwrite hard
link; synchronize the directory; compare and durably advance the external
fence; then acknowledge. The fence names the exact sequence and SHA-256 digest.
An interrupted installation that did not advance the fence is an orphan. A
missing, altered, or rolled-back committed snapshot quarantines the store.
Identical writes verify and synchronize the committed bytes again. I/O
uncertainty stops all work on that handle. Reopen and reconcile with the same
independent authority; never reset a claim to regain service.

The authority's identity, head and exclusive ownership must survive separately
from every keystore snapshot or clone. A second file on the same restored disk,
a finalized block number, and the tests' in-memory authority do not meet this
requirement. Tests use a separate file authority for process-death cases and a
clearly labeled memory authority for local worker integration.

## Records, attempts and keys

The caller supplies 32 bytes of high-entropy key material from its secret
manager. This is not a password API. HKDF-SHA256 derives a storage-specific key;
XChaCha20-Poly1305 protects snapshots and each record. Record associated data
binds the version, network, chain, registry, wallet/epoch, seat, kind, record ID
and lock slot. The host never decodes candidate, completion, nonce or share
payloads. Key provisioning, rotation and recovery policy remain deployment work.

Locks compare by `(domain, seat, attempt, kind)`. Immutable records compare by
`(domain, record ID)`. Different bytes return `Conflict`. Claims are permanent
across restarts and domains within a storage identity. They include encrypted
intent bytes. A live claim prevents closing the store before the worker exits.
Signing retries require a fresh agreed attempt. A DKG claim also reserves its
domain permanently, preventing concurrent or later competing candidates. An
abandoned or crashed DKG needs a fresh epoch. If the worker or host dies before
the host durably saves `KeyReady`, every local seat in that attempt is lost.
Release only live ownership. Retain the attempt tombstone and immutable records
and locks. Never repeat the DKG in that domain or resume its candidate.

Accepted public key metadata and opaque references are installed in one durable
snapshot. A new worker reloads candidate and completion records through the
client's checked key-reference path, using only durably installed accepted
keys. A crash after completion but before durable key installation also loses
the local seats. Candidate recovery is rejected by owner decision. The pending
wallet certifies under the C-03 readiness policy or expires unfunded with its
identity tombstone; the group then requests a fresh epoch.

## Qualification limits

The current snapshot limit is 32 MiB; an opaque record is limited to 1 MiB.
Snapshots are append-only and copy the live journal. There is no pruning or
production disk-retention budget yet. These are explicit bounded local
implementation choices, not production capacity claims. Full K-02 qualification
requires SF-03, final group/runtime decisions, a reviewed independent fence
service, secret management, and storage-class crash/power-loss qualification.
The local tests exercise actual process death and filesystem calls on macOS;
they do not simulate loss of power to production storage.

Focused checks:

```sh
go test -count=1 ./pkg/frost/store
```

`TestCrashEveryStoreBoundary` kills a subprocess before/after claim, file sync,
installation, directory sync, fence advancement and acknowledgement.
`TestDurabilityOperationErrorsPreventAcknowledgement` injects failures in the
actual file-sync, install and directory-sync operations. Other checks cover
competing processes, permanent claims, stale restores, context/ciphertext swaps,
accepted references and identical-write durability.
