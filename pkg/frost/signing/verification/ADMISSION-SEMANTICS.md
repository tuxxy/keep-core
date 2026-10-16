# Frozen abstract admission join, 13 October 2026

The user clarified this join after the independent C-04/V-05 review. The
producer implementation remains parent-owned. This file changes no Go intent
bytes, contract code, dependency, funding route or production policy.

- `Intent.Generation` and `ReservationView.Generation` are the same independent
  **uint64 intent-generation counter**, not C-03's uint32 readiness generation.
  The counter is initially zero. Admission requires the supplied generation to
  be counter + 1 and increments the counter atomically with reservation install.
  There is at most one active custody reservation for the wallet. Timeout and
  certificate expiry do not free it; a confirmed custody outcome resolves it.
- Custody admission establishes uint64 stateVersion 1. A confirmed custody spend
  or certified seat/capability policy change increments it. Certificates for a
  different current version refuse. An authorization-invalidating version change
  quarantines an unresolved reservation; it cannot continue reporting Reserved
  with its old version. C-03 readiness metadata alone is not this
  counter; the two widths and meanings remain distinct.
- The host needs a durable compare-and-insert attestation lock keyed by wallet,
  intent generation and state version. A different complete intent/certificate
  message cannot receive that host's attestation for the same tuple while its
  certificate can still be admitted or has been admitted. Identical retransmission
  is idempotent. A different intent may replace an **expired, unadmitted** statement
  only after the inclusive original deadline **and every outstanding renewal deadline**
  are finalized and authenticated canonical
  finalized state proves absence of admission at or before that deadline. The
  previous attestation history is retained; expiry never replaces an admitted
  reservation or releases its escrow. Bitcoin signing requires durable admission. The model uses two complete-message identities;
  it does not turn the K-04 per-plan record into an already existing tuple lock.
- Certificate v2 binds uint64 validUntilBlock. Admission after that bound refuses.
  The bound is inclusive: admission at equality is valid. The provider checks
  certificate authority/expiry at the authenticated historical admission block;
  current reservation state governs release. Expiry does not cancel an admitted
  reservation, revoke a Bitcoin signature or permit a reserved refund.
- The required reservation identity is exactly
  `keccak256(abi.encode(keccak256("tbtc-v2/frost-reservation-id/v1"), chainId,
  Bridge, walletId, descriptorHash, uint64 generation, uint64 stateVersion,
  intentCommitment))`. Certificate expiry is covered by the certificate, not
  silently added to this identity or the Go v2 intent preimage.

`model/src/admission.rs` is a separate small finite consumer/attestation model.
It has one wallet, two generation labels, two state-version labels, two intent identities and three certificate variants, three ticks, three deadline labels and one host crash. The
state starts with custody version 1 and intent counter 0; abstract custody
admission is an input precondition, not a funded-contract witness. Eleven
weakenings target counter admission, readiness-counter aliasing, stale version,
forgotten or omitted host locks, expiry, identity context, concurrent active custody reservations and premature/unproven
replacement. Two added witnesses require expired-unadmitted replacement followed
by admission of the different complete intent, and admitted-expiry replacement
refusal. The replacement bound is one replacement, one retained absence proof,
three finalized block labels; the original/renewal/replacement certificates bind deadlines 1/2/3.
Finalized blocks cannot gain a later admission. All absence proofs are
authenticated canonical-finality supplier inputs, not elapsed-time inference.

Authority-valid certificates are inputs. A signature-valid certificate alone
supplies none of the funded request, exact intent, custody, fee, escrow,
capability or current-state guards. Admission is an abstract atomic contract
transition; real funded consumer entrypoints and rollback/accounting frames
are still missing and require C-04/VF/K-05 work.

The ID model uses exact tuples and collision freedom, not ABI bytes or a Keccak
proof. Cert-v2 ABI/width/domain/deadline vectors belong to C-04. The existing
Go/TypeScript intent-v2 vectors remain separately pinned, including uint64
maximum generation and state version above JavaScript's safe integer range.

K-04 already compares reservation generation/state version with immutable
intent fields, but it does not allocate the counter, collect a certificate,
implement the tuple attestation lock or read a real funded reservation ledger.
Those are explicit K-05/C-04 joining obligations. No model guard in this profile
is claimed as an implemented K-04 guard. The original signing model continues
to assume a matching finalized current reservation, with these fields covered
by its exact-context match abstraction.

N2: readiness-certified/not-lost is an honest-host assumption
(A-HOST-READINESS-NOT-LOST) until canonical on-chain state exists. It is not
a predicate of certificate signature validation. This model does not derive
readiness or durable local seats from the certificate. Hosts read reservation
ID from the authoritative chain record; independent ABI derivation awaits a
cross-language vector. The modeled exact tuple is only a declared identity join.

Renewal extension: one outstanding same-intent renewal advances original
deadline 1 to 2; a different intent's certificate uses deadline 3. Renewal
history is durable in the abstraction and clears older absence observations.
The maximum of all modeled outstanding deadlines gates replacement. Two renewal
witnesses and IgnoreOutstandingRenewal check that fence. This is a one-renewal
finite profile, not a proof of arbitrary histories or completeness of remote
renewal knowledge. Real suppliers must authenticate every outstanding deadline
and canonical finalized absence; they remain missing C04/K05 implementation.

## K-05 replay amendment, 15 October 2026

The admission profile is now replayed on the real K-05 slot lock
(`redemption.TestV04AdmissionTraceReplay`; see RESULTS-K05.md). The replay found
a model defect, F-V04-K05-01. The model's replacement guard was stricter than
the rule above and than `Attester.lock`: it required strict expiry and read
absence from the current, unfinalized counter. The model now applies the rule
as stated. Replacement needs every prior inclusive deadline finalized, plus
finalized custody that shows the generation unadmitted and the same state
version. An identical retransmission of a statement already in the history is
idempotent. Custody does not change at a finalized tick. No Go intent bytes,
contract code or production policy changed.
