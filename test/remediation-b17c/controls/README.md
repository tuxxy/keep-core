# Remediation batch 17c controls

Patches for the batch 17c tests (tuxxy/keep-core#60, #33, and note N7 of the
batch 17a fix review). Each is a reference fix (`*-fix*`) that must pass its
test, or a cheap wrong fix (`*-wrong-*`) that must fail it at a named
assertion. None is production code to merge.

```
TMPDIR=<scratch tmp> run-controls.sh <scratch keep-core checkout> <log dir> [patch ...]
```

It runs `base` (no patch) and each patch on a scratch checkout of the
candidate head, about 2 minutes in all (each case under 10 s).

| Patch | Test | Expected |
| --- | --- | --- |
| (base) | all three | every wrong case fails: N7 at "the checker accepted"; #33 at "exit 0, want 3"; #60 at "run-probe.sh is missing" |
| `n7-fix` | `TestB17cEvidenceManifestRecords` | pass |
| `n7-wrong-files-presence-only` | same | `files-hash-wrong`, `files-unlisted`, `files-phantom` fail at "the checker accepted" |
| `n7-wrong-files-listed-only` | same | `files-unlisted`, `files-phantom` fail |
| `n7-wrong-env-presence-only` | same | both `environment-extra-*` fail |
| `n7-wrong-snowfall-presence-only` | same | `repos-snowfall-other-commit`, `environment-no-snowfall-commit` fail |
| `n7-wrong-leftover-first-stage-only` | same | `leftover-absent-one-stage` fails |
| `n7-wrong-leftover-nonempty` | same | `correct` fails at "refuses the evidence a correct run.sh leaves" |
| `33-fix-run-manifest` | `test_k09_run_mapping_names_exactly_the_runner_stages` | pass (stages read from the cited run's manifest) |
| `33-fix-run-sh` | same | pass (stages read from `test/k09/run.sh`) |
| `33-wrong-phantom-only` | same | both `*_missing` fail at "exit 0, want 3" |
| `33-wrong-missing-only` | same | both `*_runner_lacks` fail |
| `33-wrong-unmapped-unchecked` | same | `unmapped_stage_runner_lacks` fails |
| `33-wrong-ignores-unmapped` | same | the committed mapping is refused ("3 != 0") |
| `60-fix` | `TestB17cRunProbe` | pass (`go test` on the export) |
| `60-fix-compiled` | same | pass (`go test -c`, then the binary) |
| `60-wrong-hand-record` | same | `record`: the checker refuses (no head, clean, patch, testFiles, logSha256) |
| `60-wrong-patched-hashes` | same | `record`: "testFiles records k09_lifecycle_test.go as ..., but head has" |
| `60-wrong-log-hash-early` | same | `record`: "but logSha256 records" |
| `60-wrong-on-checkout` | same | `record`: "run-probe.sh changed the checkout" |
| `60-wrong-no-stage` | same | `record`: "names no test-file assertion line" (the stage never ran) |
| `60-wrong-overwrite` | same | `existing-record`: "wrote into an existing record directory" |
| `60-wrong-clean-always` | same | `dirty-tree`: "says clean true" |
| `60-wrong-records-pass` | same | `probe-did-not-fail`: "run-probe.sh exited 0" |

Results: `K-09-restart/TESTS-BATCH17C.md`.
