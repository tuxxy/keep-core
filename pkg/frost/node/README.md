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
