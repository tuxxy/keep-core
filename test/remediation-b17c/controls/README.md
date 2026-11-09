# Remediation batch 17c controls

Patches for the batch 17c tests (tuxxy/keep-core#60, #33, and note N7 of the
batch 17a fix review). Each is a reference fix (`*-fix*`) that must pass its
test, or a cheap wrong fix (`*-wrong-*`) that must fail it at a named
assertion. `r-*` patches are the batch 17c reviewer's
(`K-09-restart/REVIEW-TEST-B17C.md`). None is production code to merge.

Shapes: the N7 fixes add `test/k09/evidence/runrecords_test.go` with
`TestK09EvidenceRunRecords` (decision 1; frozen `evidence_test.go` untouched).
The #60 fixes build `solidity/ecdsa` in the export as run.sh does (review B1);
`60-fix` also copies the patch into the record and records `repos.snowfall`.

```
TMPDIR=<scratch tmp> run-controls.sh <scratch keep-core checkout> <log dir> [patch ...]
```

It runs `base` (no patch) and each patch on a scratch checkout of the
candidate head, about 8 minutes in all (each case under 5 s; #33 patches run
the whole `test_freshness` module, so a fix must also keep its other tests).

| Patch | Test | Expected |
| --- | --- | --- |
| (base) | all three | every wrong case fails: N7 15/15 at "the checker accepted"; #33 5/5 at "exit 0, want 3"; #60 6/6 at "run-probe.sh is missing" (the checker unit passes 22/22) |
| `n7-fix`, `r-n7-newfile` | `TestB17cEvidenceManifestRecords` | pass |
| `n7-wrong-files-presence-only` | same | `files-hash-wrong`, `files-unlisted`, `files-unlisted-subdir`, `files-phantom` fail at "the checker accepted" |
| `n7-wrong-files-listed-only` | same | `files-unlisted`, `files-unlisted-subdir`, `files-phantom` fail |
| `r-n7-wrong-toplevel` | same | `files-unlisted-subdir` fails |
| `n7-wrong-env-presence-only` | same | the three `environment-extra-*` fail |
| `r-n7-wrong-prefix-allowlist` | same | `environment-extra-go-variable` fails |
| `n7-wrong-snowfall-presence-only` | same | `repos-snowfall-other-commit`, `environment-no-snowfall-commit` fail |
| `n7-wrong-leftover-first-stage-only` | same | `leftover-absent-one-stage` fails |
| `n7-wrong-leftover-nonempty` | same | `correct` fails at "refuses the evidence a correct run.sh leaves" |
| `33-fix-run-manifest` | `test_k09_run_mapping_names_exactly_the_runner_stages` | pass (stages read from the cited run's manifest) |
| `33-fix-run-sh` | same | pass (stages read from `test/k09/run.sh`) |
| `r-33-fix-subject-runsh` | same | pass (stages from `run.sh` at `k09_run_mapping.subject["keep-core"]`) |
| `33-wrong-phantom-only` | same | both `*_missing` fail at "exit 0, want 3" |
| `33-wrong-missing-only` | same | both `*_runner_lacks` and `mapped_stage_renamed` fail |
| `r-33-wrong-count-only` | same | `mapped_stage_renamed` fails ("cites 13-phantom and omits 08-deposit-refund") |
| `33-wrong-unmapped-unchecked` | same | `unmapped_stage_runner_lacks` fails |
| `33-wrong-ignores-unmapped` | same | the committed mapping is refused ("3 != 0") |
| `60-fix` | `TestB17cRunProbe` | pass (`go test` on the export) |
| `60-fix-compiled` | same | pass (`go test -c`, then the binary) |
| `r-60-fix-build`, `r-60-fix-per-stage` | same | pass |
| `r-60-wrong-stage-only` | same | `record`: "the log does not show stage 01-legacy passing before 08-deposit-refund" |
| `60-wrong-hand-record` | same | `record`: the checker refuses (no head, clean, patch, testFiles, logSha256) |
| `60-wrong-patched-hashes` | same | `record`: "testFiles records k09_lifecycle_test.go as ..., but head has" |
| `60-wrong-log-hash-early` | same | `record`: "but logSha256 records" |
| `60-wrong-on-checkout` | same | `record`: "run-probe.sh changed the checkout" |
| `60-wrong-no-stage` | same | `record`: "names no test-file assertion line" (the stage never ran) |
| `60-wrong-overwrite` | same | `existing-record`: "wrote into an existing record directory" |
| `60-wrong-clean-always` | same | `dirty-tree`: "says clean true" |
| `60-wrong-records-pass` | same | `probe-did-not-fail`: "run-probe.sh exited 0" |

Results: `K-09-restart/TESTS-BATCH17C.md`.
