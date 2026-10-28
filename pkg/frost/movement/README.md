# pkg/frost/movement

K-07 fund movement and wallet closure on the host. Inactive in production:
like K-04 to K-06 it runs only with an explicit `LocalRegtest` executor.

| File | Role |
| --- | --- |
| `chain.go` | the C-06/C-09 seam (`Reader`, `Writer`), mirrored records and closure bits |
| `view.go` | one consistent read of a source (commitment, targets) or a target (incoming) |
| `planner.go` | target selection, the one-shot commitment, exact movement and incoming sweep plans |
| `authority.go` | `SweepAuthority` for both plans, rechecked at every grant |
| `mover.go` | rounds, lock, ledger, signing, broadcast and outcome |
| `closure.go` | the closure check at the finalized and the current head; local archive |

The predicates, hooks and controls are in `verification/BOUNDARIES.md`
(V-08). The acceptance record is `test/frost/K07.md`.
