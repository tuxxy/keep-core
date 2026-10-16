# Checkpoint limitations and V-05 joining obligations

The model's application commits expose stages but abstract the durability kernel
as a supplier guarantee. It is not the KC-VER shared disk/fence model extended
with signing state. The inherited store subprocess trace suite is concrete
supporting evidence; it does not discharge all crash/fence assumptions for this
component. A later shared-kernel composition and lockstep subprocess executor
replay must preserve process-cache versus host-loss distinctions. Current
generated executor projections are labeled partial; store reopen is not process
kill. The separate twelve application-boundary subprocess scenarios do execute
process kills, without extending the claim to disk-stage or host power loss.

Each abstract request has one signing digest. Two attempts may serve two
requests or a failed/fresh retry of one request. A concrete K-04 plan may batch
two requests and have two inputs; that all-input completion join is exercised
by the parent regression and real-worker fixture, not established by the
abstract per-attempt aggregate flags. The actual real-store tests check both
one-of-two refusal and two-of-two reconstruction. An extended batch model and
C-04 entitlement/escrow accounting remain a K-05/V-05 composition item.

No-double-outcome uses abstract payment/cancellation finality and common-input
exclusion. It establishes no deployed contract behavior. In particular, the
certificate helper can validate operator authority without any funded request,
reservation, timeout, settlement or refund state. The C-04 boundary document
requires those consumer transitions; V-04 never counts the helper as their
implementation. The contract side also needs the VF checks.

The controlled reservation provider is trusted to return a consistent current
canonical snapshot. Its callback is not an atomic lock against a later chain
transition. Possible release persists across that transition; the honest host
can quarantine but cannot revoke a Bitcoin signature. A-K04-AGREED-EXECUTOR-CONFIG
is still owned by the K-05 coordination/signing leads.

Model checking establishes only finite safety and reachable witnesses. It does
not establish fairness, availability, cryptographic soundness, target capacity,
production storage-class crash behavior or production policy.

The renewal extension covers one fully observed outstanding renewal only. It
does not prove completeness of remote renewal history, arbitrary renewal
sequences, actual host certificate locks or real funded consumer transitions.

At K-05 (RESULTS-K05.md) the admission traces replay on the real slot lock and
store; the contract side of every admission predicate stays a controlled input.
The lock is given the finalized head directly; `Attest` derives it from the
configured finality depth, which stays an assumption (A-CHAIN-FINALITY, D-09).
The C-04 projection replay runs contract-visible actions on the real Bridge
and is partial. P-K05-LATE-SIGNATURE-QUARANTINE is blocked on spec issue
tlabs-xyz/FROST-migration-spec#2. The timeout policy (#3) and cross-operator
certificate transport stay open.
