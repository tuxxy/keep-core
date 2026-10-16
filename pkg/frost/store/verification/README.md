# K-02 host store verification (tranche 1)

Verification tooling for `pkg/frost/store`. It is not part of the node build
and adds no production code. Read [SCOPE.md](SCOPE.md) first for the exact
predicates, assumptions, bounds and acceptance criteria, then
[TRANSITION-MAP.md](TRANSITION-MAP.md) for how each model transition
corresponds to the Go source.

| Path | What it is |
| --- | --- |
| `model/` | Stateright 0.31.0 finite host model (Rust 1.94.0, crate-local lockfile, no SNOWFALL workspace change) |
| `traces/` | Traces written by the model runner: counterexamples of weakened models, witnesses of the claim model, and the declared boundary set |
| `../replay_test.go` | Go replay harness: runs each modeled process as a subprocess of the test binary and compares every step with the trace |

## Run the model

```sh
cd pkg/frost/store/verification/model
cargo build --release --offline
./target/release/k02-host-model --out /tmp/k02-host-model --threads 12 --timeout-secs 600 --max-commits 3
```

The runner checks the claim model (five `always` properties and the
witnesses), then each weakening. It writes `results.json` and `traces/*.json`.
Exit code 0 means: every run completed within its limits, no claim-model safety
property has a counterexample, every witness was discovered, and every
weakening broke its declared property. A run that stops on the timeout is
reported as `completed: false` and is blocked, not a pass.

Choose and record the time and memory limits before each run. Measure the
resident set with `/usr/bin/time -l`.

## Replay the traces on the real store

Copy the runner's `traces/*.json` into `traces/`, then:

```sh
go test -count=1 ./pkg/frost/store -run '^TestReplayTraces$'
go test -count=1 -race ./pkg/frost/store -run '^TestReplayTraces$'
FROST_REPLAY_REPORT=/tmp/replay.json go test -count=1 ./pkg/frost/store -run '^TestReplayTraces$'
```

Weakened-model counterexamples are replayed with the claim model's
expectations (`claim_model_steps`), cut at the first step the claim model does
not offer. `FROST_REPLAY_MODE=weakened` replays a weakened model's own
expectations; it exists only for the historical negative control against the
pre-fix revision `18b492b5482b73cd744670fd6a3bd202d95a8110`.

Traces with `"replay": "partial"` contain a host crash. The harness simulates
the modeled loss (it removes a journal installed by a process paused before its
directory sync) and continues. That tests recovery from the modeled loss, not
the loss itself.

## Limits

This is bounded model checking of a separate model plus concrete replay. It is
not a proof of the Go source and not a production qualification. The results,
limits and remaining gaps of each run are recorded with the integration
evidence, not in this directory.
