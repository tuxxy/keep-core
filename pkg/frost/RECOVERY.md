# Recover a pending FROST candidate

FROST remains inactive in node dispatch. These local components do not enable
funding, approval transactions or wallet activation.

Open a protected store and scope it to the original deployment domain. Supply an
independent, non-expiring fence as described in the storage contract. Construct
`NewGuarded` with the exact recovery-capable worker hash. It checks the worker
capability before it returns an engine.

For a new DKG, pass the same `RecoveryJournal` to the engine and transport. The
client saves the public recovery handle before it confirms local candidates or
sends readiness. The transport saves readiness and original operator attribution
before sending or delivering it. The original request remains in the encrypted
claim journal.

After a restart:

1. Open the same scoped journal using its original storage identity and key.
2. Read `RecoveryIntent` to obtain the original public request and agreed attempt
   for transport setup. Use the original operator-to-seat membership.
3. Create the transport with `RecoveryOnly: true` and that same journal. It
   validates and replays saved readiness. It accepts only readiness traffic.
4. Call `RecoverPending` with this transport and the same finalized acceptance
   provider. It loads the original request, acquires exclusive recovery ownership,
   and rejects concurrent starts or a different request.
5. Use the returned key only after `SaveKey` installs the metadata and references
   in one durable transaction. A fresh signing operation must use a new attempt.

If the host died before it saved the handle, `PendingCandidates` lists the opaque
record IDs. `InspectRecovery`, inside the client adapter, validates their public
identity before the handle is restored. Keep-core never parses secret records.
An incomplete local set or missing peer readiness cannot create a usable wallet.

Readiness evidence records a trusted host authentication decision. It is not an
engine-level signature proof. Recovery checks the saved deployment domain,
attempt, sender, envelope kind and original operator membership. A candidate
record does not establish peer identity.

The new journal fields are additive under the existing encrypted snapshot
format. Older strict readers reject snapshots containing these fields. Preserve
a compatible worker, client and host release while a wallet has obligations.
Encryption protects stored data; only the independent fence establishes freshness.
Production fence service, key provisioning, backup policy, disk limits and SF-03
runtime/capacity qualification remain pending.
