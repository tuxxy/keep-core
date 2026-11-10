# Retained run evidence

This directory keeps only what the tests and statements read: the replay
traces, source pins, results, summaries and the evidence manifest. The full
run evidence (logs, run records, replay reports, defect archives,
checkpoints and reviewer transcripts) lives outside the repository under
the implementation evidence tree, as KC-VER VD-03 requires:

    implementation/V-03/chain-verification-2026-10-15/

Paths there mirror `pkg/frost/dkg/verification/`, and `SHA256SUMS` covers
every file. Every moved file that `manifest.json` lists matches its
recorded hash.

The 6 November 2026 re-pin (S-K03-CHAIN-2) keeps its full run evidence at

    implementation/V-03/repin-2026-11-06/

with its own `SHA256SUMS`. In this directory, `traces/` and `results.json`
are now the re-pin's model output (the S-K03-CHAIN-1 versions are in git
history at 925103a6), and `source-pins-2026-11-06.json` and
`freshness-2026-11-06.json` are its pins and freshness result. The other
files here belong to S-K03-CHAIN-1.

The 9 November 2026 re-pin (S-K03-CHAIN-3) keeps its full run evidence at

    implementation/V-03/repin-2026-11-09/

`traces/` and `results.json` here are now its model output (the
S-K03-CHAIN-2 versions are in git history at 31ab213f and retained in
`implementation/V-03/repin-2026-11-06/traces/`). `source-pins-2026-11-09.json`
is its pin file; `source-pins-2026-11-06.json` and `freshness-2026-11-06.json`
stay as S-K03-CHAIN-2 recorded them.
