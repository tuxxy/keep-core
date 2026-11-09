# Remediation batch 17a D34 controls (K-09 runner)

These are patches to `test/k09/run.sh` for the tests in `test/k09/runner` (tuxxy/keep-core#30, #35, #36 and the runner half of tuxxy/tbtc-v2#19, under decision D34). The tests run the checkout's `run.sh` on a FIXTURE repository with stub stages, in a few seconds; no chain, node or worker runs.

```
test/k09/runner/controls/run-controls.sh <scratch keep-core checkout> <log dir> [patch ...]
```

| Patch | Implementation | Expected |
| --- | --- | --- |
| `d34-fix.patch` | One correct runner. Each stage runs in its own process group, recorded and killed after the stage. `../snowfall` is pinned by `SNOWFALL_COMMIT`, refused when dirty, exported with `git archive` and recorded in `repos.snowfall`. `manifest.files` hashes every evidence file. The build and stages run under `env -i` with a named allowlist (empty `GOFLAGS` for the build), recorded in `manifest.environment`. | all pass |
| `d34-wrong-tbtc-test-only.patch` | Everything else fixed; cleanup still only `pgrep`s `tbtc.test` (the base). | `StageCleanup` both cases: `K09-10: after run.sh returned, stage 03-dkg's child process ... is still alive` |
| `d34-wrong-kill-on-failure-only.patch` | The stage group is killed only when the stage failed. | `StageCleanup/leak-pass` |
| `d34-wrong-snowfall-record-only.patch` | The snowfall commit is recorded, but a dirty or other checkout is not refused, and the live checkout is still symlinked. | all three `SnowfallBinding` cases (`K09-17: the run accepted ...`; `... saw ../snowfall/ignored.go ...`) |
| `d34-wrong-inventory-logs-only.patch` | `manifest.files` hashes only `.log` files. | `K09-18: evidence file tbtc.test is not hashed in manifest.files` (also `cases.tsv`, `secret-scan-hits.txt`) |
| `d34-wrong-env-denylist.patch` | The caller's environment minus a five-name denylist (the closed K-09's shape). | `K09-19: ... saw B17A_CALLER_ONLY ...` |
| `d34-wrong-env-stages-only.patch` | Allowlist for the stages, but the build inherits the caller's environment. | `K09-19: the stage binary build ... saw B17A_CALLER_ONLY` and `... the caller's GOFLAGS` |

Reviewer probes (`r-d34-*`, from REVIEW-TEST-B17A.md's recheck; each is a full run.sh patch):

| Patch | Implementation | Expected |
| --- | --- | --- |
| `r-d34-fix-goenv.patch` | `d34-fix` plus `go env GOVERSION GOCACHE` under the build allowlist (B1: the stub `go` passes every call but `go test -c` to the real go) | all pass |
| `r-d34-fix-setm.patch` | `set -m` job control instead of the setpgid wrapper | all pass |
| `r-d34-fix-snowfall-object.patch` | `repos.snowfall = {commit, clean}` (R1) | all pass |
| `r-d34-fix-killed-objects.patch` | `leftoverKilled` entries as `{pid, command}` (R1) | all pass |
| `r-d34-fix-pathkill.patch` | kills by the `$LEGACY_ANVIL` / `$LEGACY_BITCOIND` path after each stage (R2: the stand-in child is `$LEGACY_ANVIL`) | all pass |
| `r-d34-wrong-kill-at-exit.patch` | kills each stage's group only at the run's exit (R3) | `StageCleanup` both: `K09-10: stage 03-dkg's child N was still alive when stage 04-drain started` |
| `r-d34-wrong-snowfall-symlink.patch` | checks and records snowfall but compiles the live checkout through the symlink (R3) | `SnowfallBinding/pinned-and-clean`: `K09-17: the stage binary build saw ../snowfall/ignored.go ...` |

Accepted manifest shapes: `stages[].leftoverKilled` entries as `"<pid> <command>"`, a bare pid, or `{"pid": N, ...}`; `repos.snowfall` as the commit string or `{"commit": "<sha>", ...}`.

Results: `K-09-restart/TESTS-BATCH17A.md`.
