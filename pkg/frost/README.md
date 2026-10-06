# Local FROST integration — K-01

This package is inactive in node dispatch. `Frost.Enabled` defaults to false.
Setting it to true is rejected before chain access or credential handling.
Existing ECDSA dispatch and storage are unchanged.

Only `snowfallengine` imports the SNOWFALL client. Each DKG or signing call
creates one client and one worker operation for all local seats. The caller
supplies a canonical selected group, admitted DKG participants, local seats,
agreed attempt context and an exact 32-byte signing message. The host must
authorize that message before `Sign`. The adapter checks the approved profile,
uses the client's frozen attempt and lock helpers, and verifies the final BIP340
signature with btcd. It never decodes secret candidate or completion records.

The worker needs an absolute path and a nonzero SHA-256 pin. The client checks
the file for each operation before spawn. Keep that file immutable and protect
its parent directory. The pin check does not defend against an attacker who can
replace the file between hash and execution. `Env` is a copied explicit list;
nil and empty supply an empty environment. Do not supply passwords or the full
parent environment. Context cancellation stops and reaps the worker. Stderr is
drained and discarded; errors contain bounded public diagnostics.

The test harness has local authenticated-seat queues, memory storage, and a
clearly labeled test acceptance adapter. It proves no production transport
security, storage durability, chain finality or crash recovery. It creates no
production wallet record and uses no RPC or funds. Candidate recovery is rejected by owner decision. A worker or host crash before
durable KeyReady loses every local seat; use a fresh epoch. SF-03 and K-02 remain
separate work.

## Run the local harness

From this worktree, with the sibling SNOWFALL checkout pinned as recorded in
`DEPENDENCY.md`:

```sh
SNOWFALL_WORKER=/absolute/path/to/snowfall \
SNOWFALL_WORKER_SHA256=<exact-64-hex-artifact-hash> \
go test -count=1 -v ./pkg/frost/...
```

The host must permit local process inspection for exit checks. The real tests
skip only when the explicit worker opt-in is absent or `-short` is used. If the
opt-in is partial, the file is missing, or the hash differs, the test fails.
Skips and timeouts never satisfy K-01. A normal 2-of-3 case signs with seats 1
and 3. A separate two-node case holds seats 1 and 2 in one process. Both compare
the candidate and key, reload opaque records, verify the exact message, reject
a changed message, and check worker exit. Negative cases cover environment
scrubbing, wrong/missing worker, malformed input/frame, cancellation and false
acceptance.
