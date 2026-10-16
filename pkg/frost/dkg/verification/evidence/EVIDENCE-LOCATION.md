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
