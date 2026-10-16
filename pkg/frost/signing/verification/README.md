# V-04 local verification checkpoint

This tooling checks an independent finite signing/reservation model with
Stateright 0.31.0 and Rust 1.94.0. It runs locally; nothing is added to CI.
The accompanying evidence records actual source pins, limits, witnesses,
negative controls and concrete correspondence. It is not proof of Go and is
not a production, merge, funding or C-04 acceptance gate.

Run `python3 pkg/frost/signing/verification/run.py model` from the assigned
repository. It builds offline and records a plan before execution, a 1,800 s
wall deadline, four threads and a 16 GiB process-tree RSS ceiling monitored at
250 ms. A timeout or resource stop is blocked. The thirteen signing/reservation weakenings and eleven admission weakenings
stop on their declared counterexample. The claim run
must finish exhaustively and discover every witness.

`vectors`, `normal`, `race`, `guard`, `recovery`, and `integration`, and `process` run the
concrete checks. Each rerun archives previous logs. Integration requires the
K-04 fixture's public environment. The recovered Core executable must match
both the archive and executable hashes in `../INTENT-V2.md`'s companion
`test/frost/K04.md`. Temporary runtime artifacts and a private Go modfile are
ignored; the declared dependency is not upgraded. Exact dependency restoration:

```
git -C /path/to/snowfall archive c1847ebdf133 clients/go |
  tar -x -C pkg/frost/signing/verification/dependency-source
```

`TestV04TraceProjection` consumes discovered traces and checks executor seam
projections against current unweakened Go. These are **partial**, not lockstep
subprocess replay. Contract-only paths are **none**. Separate real-store and
real-worker checks are concrete evidence, not a reason to relabel projections
as full. At K-05 the C-04 projection replay runs contract-visible actions on
the real Bridge; it is also partial (RESULTS-K05.md).

At K-05, `V04_OUT=<evidence dir> python3 pin.py` writes the source manifest
first. `admission-replay` replays the admission traces on the real K-05
`Attester.lock` and store. `c04` and `c04-control` replay the C-04 projection
and its negative control on the real Bridge. `k05` runs the K-05 named tests.
See RESULTS-K05.md (statement S-K05-SIGN-1).

`process` runs the twelve declared subprocess application traces, including
actual process kills and store reopens. It is a distinct trace set from the
generated model's storage-stage projections. `admission.rs` checks the
C-04/host-certificate join in ADMISSION-SEMANTICS.md. Since K-05, its traces
replay on the real slot lock.

See RESULTS.md and evidence/V-04-2026-10-13/safety-statement.json for the final
checkpoint. `admission` reruns only the proposed admission/host-attestation
profile after an affected-module change. `audit-evidence.py` checks final profile
pins, discoveries and replay classifications; check-freshness.py checks source.
