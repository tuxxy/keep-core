# K-09 probe records (tuxxy/keep-core#60)

A wrong-behaviour probe (README.md here) may be cited as evidence only through
a record that binds its result to the head it ran on. `run-probe.sh` writes
that record:

```
run-probe.sh <patch> <stage> <new record directory>
```

It needs `test/k09/run.sh`'s environment. It applies the patch to a
`git archive` export of HEAD (never to the checkout) and exports `../snowfall`
at `SNOWFALL_COMMIT`. It builds `solidity/ecdsa` in the export as run.sh does,
then runs stages 01-06 and `<stage>` of `TestK09Lifecycle` in one process. The
record directory holds the patch copy (`probe.patch`), the log (`probe.log`),
the ECDSA build log and `probe.json`. `probe.json` records head, clean, patch
(path, SHA-256, copy), `repos.snowfall`, environment, Go version, testFiles,
stage, predecessors, log, logSha256, exit and failure.

`run-probe.sh` writes no `probe.json` and exits non-zero in any of these cases:
- the tree is dirty;
- the record directory exists;
- the stage is not one of run.sh's stages;
- the patch does not apply;
- a build fails;
- a stage among 01-06 before `<stage>` did not pass;
- `<stage>` did not fail at a test-file assertion line;
- the secret scan matches.

Check a record from the checkout it names:

```
K09_PROBE_RECORD=<record dir> go test -count=1 -run '^TestK09ProbeRecord$' ./test/k09/controls
```

**Trust:** a record carries no signature. A complete but forged `probe.json`
passes the checker. A record binds a probe to its head only because
`run-probe.sh` is its sole writer, so cite only records that `run-probe.sh`
wrote. `TestB17cRunProbe` checks that what it writes is true.

Probe results recorded before #60 (the K-09 run log's probe rows marked
"unbound") have no such record and are not citable as bound evidence.
