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
| `d34-wrong-snowfall-record-only.patch` | The snowfall commit is recorded, but a dirty or other checkout is not refused. | `SnowfallBinding/dirty` and `/other-commit`: `K09-17: the run accepted ...` |
| `d34-wrong-inventory-logs-only.patch` | `manifest.files` hashes only `.log` files. | `K09-18: evidence file tbtc.test is not hashed in manifest.files` (also `cases.tsv`, `secret-scan-hits.txt`) |
| `d34-wrong-env-denylist.patch` | The caller's environment minus a five-name denylist (the closed K-09's shape). | `K09-19: ... saw B17A_CALLER_ONLY ...` |
| `d34-wrong-env-stages-only.patch` | Allowlist for the stages, but the build inherits the caller's environment. | `K09-19: the stage binary build ... saw B17A_CALLER_ONLY` and `... the caller's GOFLAGS` |

Results: `K-09-restart/TESTS-BATCH17A.md`.
