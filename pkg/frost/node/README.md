# Local FROST node

`keep-core frost-local-node <private-config> --listen 127.0.0.1:<port>`
runs an explicitly bounded local rehearsal. It requires loopback Core regtest
and Anvil domains, private operator/store key files, the pinned real worker,
an independent keystore and a separately owned fence authority. The HTTP
listener exposes public status only; it accepts no signing commands.

The process discovers selected DKG epochs and older owned wallets, constructs
the actual Ethereum signer and authenticated libp2p transport, enforces the
production operator eligibility firewall, and dispatches incoming sweeps from
public chain records. Planning, signing, broadcast, proof and final outcome
reconciliation use the existing guarded components. Restart reloads only its
own scoped opaque key through the worker API after rechecking the original
canonical approval. No worker record is decoded or exported.

`TestFrostNodeProcessesDKGAndDispatch` drives only public chains/users from the
parent, witnesses two DKG epochs and the legacy-to-FROST incoming sweep in
separate executables, rejects an authenticated ineligible peer, and restarts
one process with both epoch stores. The local SPV maintainer role is explicitly
aliased to operator 1. Separate focused overlays test owning guard assertions,
including disabling the firewall; transport outage is not rejection evidence.

The automatic scheduler currently covers DKG/readiness and incoming sweeps.
Other lifecycle duties retain the joined component witnesses. This is not a
production all-duty scheduler, production stake/beacon, durable fence service,
production-scale qualification or activation of `start`. Those boundaries are
unchanged. The local fence HTTP adapter exchanges public fence points only.

Per-epoch lost-seat reporting and per-wallet bounded retries isolate recovery
failures. DKG and incoming duties run independently, sharing one Ethereum
transaction mutex per operator so nonce selection cannot race. The local-only
VerificationPause configuration is a test adapter over actual executor
boundaries; it writes only an epoch/boundary marker before waiting for process
interruption. Retained node logs use FROST_K09_NODE_LOG_ROOT, created privately
and scanned/hashed by the acceptance runner; standalone tests use automatically
cleaned test directories.

Incoming plans are retained under the complete wallet-domain identifier before
signing. Restart validates the retained public recipe and reuses a durable
accepted signature. The node also performs public DKG/readiness expiry even
when no local executor can resume the abandoned epoch. FailureStages exposes
fixed stage names only; worker error text never enters public status.

Round-four recovery keeps a completed key reloadable after transient failures,
including a lost worker reply after its durable save. A pending local wallet
record advances to ReadyUnfunded only after exact canonical certification and
successful guarded reload. Candidate/no-key attempts and classified conflicts
remain unavailable; existing quarantine is never silently cleared.

Incoming planning reads the common canonical round anchor. An unsigned stale
recipe can be retired with an append-only abandonment marker only if every
attempt has no durable result or possible release. Signed/possibly released
recipes stay retained until their chain outcome. The incoming/<epoch> status
stage names reservation deferral explicitly. Independent-node test rounds are
100 blocks; component harness rounds remain ten. These fast-chain timing inputs
and the full reserve/pay/prove stale-plan witness remain open qualification.
