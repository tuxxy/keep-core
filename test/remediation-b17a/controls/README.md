# Remediation batch 17a controls (keep-core)

Wrong implementations for the batch 17a guards in
`test/remediation-b17a/constraints_test.go` (tuxxy/keep-core#58, #53). Both
guards pass at the base 1e3e6ea6 by design: these issues are remediation
constraints, not defects present now. Each patch reintroduces what the
closed K-09 had and must fail its guard at the named assertion.

```
test/remediation-b17a/controls/run-controls.sh <scratch keep-core checkout> <log dir> [patch ...]
```

| Patch | Wrong implementation | Must fail at |
| --- | --- | --- |
| `b17a-58-wrong-func-hook.patch` | a callback field in `tbtc.FrostConfig` (VerificationPause/AfterGrant shape) | `K09-43: keep-client configuration carries fault-injection or pause hooks ...: Config.Tbtc.Frost.OnDKGStep (func ...)` |
| `b17a-58-wrong-interface-hook.patch` | an injected observer interface in `tbtc.FrostConfig` | `... Config.Tbtc.Frost.Observer (interface ...)` |
| `b17a-58-wrong-local-node-command.patch` | the closed K-09's `frost-local-node` command in the root command | `K09-43: keep-client's top-level commands are [...frost-local-node...]` and `K09-43: keep-client registers the command "frost-local-node"` |

Results: `K-09-restart/TESTS-BATCH17A.md`.
