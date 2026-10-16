# Retained run evidence

The repository keeps what the tests, audit and statements read: the
V-04-2026-10-13 checkpoint records with its signing and admission traces,
and the K-05 admission traces. Earlier model snapshots and run history
(`before-*`, `initial-model`, `pre-final-pin-model`, `history` and similar
directories) live outside the repository, as KC-VER VD-03 requires:

    implementation/V-04/checkpoint-2026-10-13-bulk/

Every moved file matches its entry in the in-repo
`V-04-2026-10-13/SHA256SUMS`. Re-pin evidence for K-05 is under
`implementation/V-04/K05-repin-e9fc20764-2026-10-15/`.
