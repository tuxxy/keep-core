# Private local dependency pin

SNOWFALL client and worker source commit:
`2b3ad6e58c382861b5c4cebffe8343bbf40e9355`.
Module version in go.mod: `v0.0.0-20261006111731-2b3ad6e58c38`.
This is a commit-derived Go pseudo-version, not a published release tag.
Source branch: `threshold-network/snowfall:integration/sf02-recovery`.

The local replacement is `../snowfall/clients/go`. Keep the sibling worktree
on that exact commit. A replacement uses local files; verify the commit and
clean status before testing. Only the SNOWFALL adapter imports this module.

Local worker SHA-256:
`09e9f88ce5ffcf0260b65d619827db7cf9b0a50d16697305d57c0380208688bb`.
Built with Rust/Cargo 1.94.0, `cargo build --locked --release -p snowfall-worker`,
for `aarch64-apple-darwin`, with default production features and no `test-utils`.
This native artifact is not a qualified Linux, musl/Alpine or production release.
The SF-02 implementation evidence package records its build and test results.

Recovery uses worker bootstrap version 2 and the `recover-dkg` operation.
Existing signing and new DKG use version 1. The guarded adapter probes recovery
capability before it can claim an attempt. The earlier beta.1 artifact cannot
serve this path. Frozen candidate, completion and digest encodings are unchanged.
Preserve compatible worker, client and host releases while a wallet has obligations.

D-01 is pending: repository access, license compatibility and public distribution
need an accountable owner decision. This replacement supports private local
development only. No new public module tag is claimed.
