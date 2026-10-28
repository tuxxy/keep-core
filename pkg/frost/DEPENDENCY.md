# Private local dependency pin

SNOWFALL client commit: `98e35afd09393b4ba3c7a4b1db392457d1a6fa07`.
Module version recorded in go.mod: `v0.0.0-20261006124617-98e35afd0939`.
This is a commit-derived Go pseudo-version, not a published release tag.

The local replacement is `../snowfall/clients/go`. Keep the sibling worktree
on that exact commit. A replacement uses the local files; verify the commit
and clean status before testing. The actual worker source is unchanged at
`bf9ae8527656937401e2ebdfa2d9734ec8c5e93a`.

Local worker SHA-256:
`d775dc36c508559f1bd997ab2552cea3aed94587540f6307352fec4628400765`.
Built with Rust/Cargo 1.94.0, `cargo build --locked --release -p snowfall-worker`,
for `aarch64-apple-darwin`, with the default production feature set and no
`test-utils`. This native local artifact is not a qualified Linux, musl/Alpine,
or production release. Full artifact and toolchain records are retained in the
K-00-SF-01-K-01 implementation evidence package.

D-01 is pending: repository access, license compatibility and public distribution
need an accountable owner decision. This replacement authorizes private local
development only. No new public module tag is claimed.

The final release pin will be the merged SF-01 commit or its tag after D-01 is resolved.
