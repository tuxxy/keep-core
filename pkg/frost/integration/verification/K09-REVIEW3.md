# Round 3 remediation contract

Recorded before remediation. Subjects: keep-core
9fdcab15bcf38a57daa499d597c2fbb599ee0eb4 and tbtc-v2
85e4505b1a96de6592dd399be314bd544eb5cb0b. Governing KC-SF-V3/KC-VER
revision 02: 51b9a168e6b6c73d4d471f7d302267a065407fb5.
Reviews: keep-core 5653710165; tbtc-v2 5653710870.

The prior public drain test did not exercise the legacy commitment producer:
the harness submitted that commitment through a controlled membership adapter.
The prior 19-stage run is concrete evidence only for those exact old bytes;
it does not discharge K09-15, K09-16 or the other open findings.

Required changes and witnesses, in implementation order:

1. K09-15/16/23: the actual tbtcpg producer discovers eligible immutable FROST
   targets, chooses the contract target count, estimates their actual output
   size and submits the commitment, even with zero legacy Live wallets.
   FrostOnly rejects new legacy targets, but existing legal commitments remain
   provable. Keep old deployments without the FROST extension compatible.
   A public-origin legacy test must invoke MovingFundsTask, validate its
   proposal and continue through actual legacy signing and the FROST sweep.
   Test no eligible target, mixed targets, changed eligibility, existing
   commitment, post-cutover refusal and unchanged state beside a valid drain.
2. K09-6/7/21/22: isolate uncertain or terminal epoch records to their seats;
   retry transient failures with bounds; DKG cannot block other wallets.
   Test actual node/worker interruption at candidate, certification/local-save
   and signing/proof boundaries, including SIGKILL and surviving test-fence
   revocation. Report lost seats and keep unrelated wallet duties live. A
   reservation is normal deferral, not process failure. All-duty process
   coverage stays open until exercised; a helper claim is not a real step.
3. K09-5/8: each pending legacy obligation crosses upgrade and cutover before
   public discharge; include cross-wallet interleaving. Cover actual FROST
   sweep/redemption producer-consumer proofs, and derive node proof depth.
4. K09-18/19/20/24/25/26: retained private evidence root and complete hashes;
   node logs participate in forbidden-output checks; explicit environment
   allowlist and all executable/source pins; loopback transport; config/cmd
   regression stage; bound firewall control in the same acceptance manifest.
5. Track every K09-1..26 and K09A finding with ownership, exact witness and
   current status. Keep inherited, introduced and evidence defects distinct.

Controls weaken one owning guard in disposable copies, require a positive
witness and reject harness failures as kills. Keep Core/tss work serialized,
at most eight Go cores and 900 seconds per command; record limits first.
Private diagnostics remain private, never decode opaque worker records.
No Docker, CI, Kani, final campaign, release or merge is authorized here.
Models/replays and authoritative V-05 closure remain separate open gates.
