# V-09 transition map (model action to code, KC-VER 1.3 "Correspondence")

Model: `model/src/main.rs`. Code: keep-core `925103a6c`. A model guard exists
only where the code has the same guard (R-02); the guard column names it.

## hb profile (P-K08-HEARTBEAT-DOMAIN)

| Model action | Code | Guard | Replay boundary |
| --- | --- | --- | --- |
| `Tick` | chain head passes a round; `Runner.Current` anchor moves | | `nonceAuthority.current` (fixture authority) |
| `Start(0)` | `signing.NewHeartbeatSigner` with the runner's round anchor as `Nonce`; `Run` starts | message never taken from a caller (`Digest()`) | pause `heartbeat-nonce-seen` |
| `Step(0)` phase 1 | `HeartbeatSigner.authorize` -> `Runner.AuthorizeHeartbeat` | nonce == current anchor | pause `before-heartbeat-record` or return `ErrUnauthorized` |
| `Step(0)` phase 2 | `RetainSigningRecord(intent)` | | pause `before-defense-record` |
| `Step(0)` phase 3 | `RetainSigningRecord(DefenseID, defense)`; attempt in the heartbeat tag; transport | | fixture pause `engine-before-claim` |
| `Step(p)` phase 4 | guarded engine `store.Claim(AttemptID)` | exclusive claim (`ErrClaimed`) | fixture pause `engine-claimed` or return |
| `Misroute(p)` | any host path handing the engine a request with the other purpose's message | | fixture rewrites `r.Message` |
| `Step(0)` phase 5 | `heartbeatAuthorization.BeforeSigning`: DeepEqual with the expected request, decode intent, recompute `HeartbeatDigest`, `authorize` again; worker grant | binding + nonce recheck | fixture records the grant; checks the defense record is durable; pause `after-signature` |
| `Step(1)` phase 5 | `finalAuthorization.BeforeSigning` (DeepEqual, wallet, Bitcoin, reservation, guard) | request equals the plan's | as above |
| `Step(p)` phase 6 | `Done` retained, `after-signature` continues, `Run` returns | | return nil |
| `Crash(p)` | process crash at the paused boundary | | hook returns an error; component discarded |
| `Start(1)` | `NewLocalExecutor(config)`; `Run` through `transaction-frozen` and the V-04 reservation reads | V-04 | pause `before-intent-record` |

Weakenings: `AcceptRawDigest` drops the phase-5 binding for the heartbeat;
`AnyNonce` drops both nonce checks; `ReuseAttempt` lets a retry continue the
last crashed process's claimed attempt instead of a fresh exclusive one;
`SharedAttemptDomain` moves the heartbeat attempt under the transaction tag
(ids still bind the nonce: masked).

## enf profile (P-K08-EVIDENCE-RETAINED, P-K08-DEFENSE-BEFORE-DEADLINE)

| Model action | Code | Replay |
| --- | --- | --- |
| `ExecStart`, `ExecStep(stage)` | executor / heartbeat signer `Run`: intent, `retainDefenses` (or the heartbeat defense), claim, grant, `Done` | fixture writes of the real `signing.DefenseRecord` (order checked on the real code by the hb replay) |
| `Crash(host)` | process crash (issued write survives) or host crash (issued write lost), S-K02-HOST-4 | fixture store outcome; defender pass aborted at its hook |
| `Restart` | new `Defender` value (keeps no cursor) | new component |
| `Recover` | executor recovery (`LoadResult`, `Rebroadcast`) | no-op: the defender does not read it |
| `Restore` | stale image; the store quarantines on open (S-K02-HOST-4) | reader fails closed (fixture); trace counted partial |
| `Settle` | refund wins over a losing sweep; nothing is deleted (`store/signing.go` has no delete) | no-op |
| `Challenge(c)` | C-08 `submit` (`FrostFraudChallengeSubmitted`) | `fakeChain` log and challenge |
| `Timeout(c)` | C-08 `notifyTimeout` by anyone after the deadline | `fakeChain` resolve |
| `DefStart` | `Defender.Defend` -> `open` (logs, one `Challenge` read per digest) | pause `challenge-seen` |
| `DefNext` | loop step: snapshot `Resolved`, `ReadDefense` (missing: not ours; error: failure) | pause `before-defense`, or pass end |
| `DefSubmit(lost)` | `submit` (heartbeat / intent / movement / proven-or-self sweep), reread on error | `fakeChain.lostReply`; pause `defense-submitted` |

The DEFENSE property's host-only search is mirrored on the real code by
`v09Continue`: from the replayed end state, restart and one full `Defend` pass.

## claim profile (P-K08-NO-SUSPECT-SANCTION)

| Model action | Code | Replay |
| --- | --- | --- |
| `Round(r)` per host: `s = 0` | host down: no `Runner.Run` | none |
| `s = 2` | `Runner.Run` writes the start record, the signer dies before the outcome | `sign` seam returns an error with no presence |
| `s = 1` | `Runner.Run` records `Outcome{Signed, Present}`; a worker error may carry `Suspect` | `sign` seam returns the presence, nil or a `frost.Error` with `Suspect` = 3 |
| derived claim | `Claimer.Derive` with `MinimumActivePercent` 70, `Failures` F | compared after every step |
| `Claim` | `Claimer.Derive` (host 1), `Sign` on both hosts (own derivation must match), `Submit` | `fakeChain.NotifyInactivity` |

Weakenings: `ClaimFromSuspect` treats a suspected seat as absent;
`SingleFailureClaim` uses F = 1; `SkipUnknownRound` skips a started round
without outcome instead of ending the window.
