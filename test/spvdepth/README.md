# FROST proof depth (tuxxy/keep-core#28, K09-8, owner decision D18)

**Invariant:** a FROST proof producer derives its proof depth from the Bridge's `txProofDifficultyFactor` and the relay's epoch difficulties, exactly as the Bridge evaluates a proof (`BitcoinTx.determineRequestedDifficulty` and `evaluateProofDifficulty`; in keep-core, the legacy `getProofInfo`):
- With enough confirmations, it submits a proof of the minimal depth the Bridge accepts (d\*).
- With fewer than d\*, it submits nothing.
- An operator-set `Confirmations` is at most a floor.

**Producers covered:**
- `FrostMovementMaintainer.ProveMovement` and `ProveIncomingSweep`;
- `FrostSweepMaintainer.ProveSweep`;
- `FrostRedemptionMaintainer.ProveSettlements`.

**The oracle is the real tbtc-v2 validation:** d\* is the smallest header count it accepts, and d\* − 1 must be refused at `Insufficient accumulated difficulty in header chain`.

**API constraint:** the producers are built only from their existing fields (`Chain`, `Bitcoin`, `Confirmations: 1`). A fix must therefore derive the depth from what they already hold: the Bridge binding (`ethereum.FrostRedemptionChain`) and the Bitcoin chain.

All runs are local only, never in CI.

**Coverage of #28:** this fast tier tests the depth against real difficulty. The existing regtest end-to-end run tests the full path (see the end of this file). The owner's D18 revision of 2026-10-29 dropped the combined single-chain check (a private chain mined at difficulty 1).

## Fast tier (every run, about 10 s)
`pkg/maintainer/spv/frost_proof_depth_test.go`, `TestFrostProofDepthFollowsBridgeFactor`.

- **Data:** recorded public testnet4 headers and transactions in `pkg/maintainer/spv/testdata/testnet4-spv-depth.json`, a labelled FIXTURE of real data.
  - **Source:** `mempool.space/testnet4/api`, fetched 2026-10-29.
  - **Contents:** heights 157937–157990 plus the epoch-start headers 157248 (current) and 155232 (previous). The loader checks every header hash and parent link, and every merkle root against its txids.
  - **Difficulty:** testnet4 interleaves real-difficulty blocks (about 1.04 × 10^9) with BIP94 DIFF1 blocks, so d\* is neither 1 nor the factor:

    | Transaction | Block | d\* at factor 6 | d\* at factor 2 |
    | --- | --- | --- | --- |
    | `275fb3c0…4a25` | 157954 (real difficulty) | 37 | 7 |
    | `18639f6f…c10a` | 157937 (DIFF1; the first real header is at 157941) | 35 | 11 |

    The test requires d\* to differ between the two factors for each block, so the second factor always discriminates.

- **Contracts:**
  - the tbtc-v2 Bridge from a `test/legacy/build-baseline.sh` build of the integration commit, behind a proxy on Anvil, deployed twice (each on its own Anvil): with the production factor 6 and with the test factor 2;
  - FIXTURE `SystemTestRelay`, with both epoch difficulties set from the recorded epoch-start headers;
  - FIXTURE `test/spvdepth/fixtures/SpvDepthFixture.sol`: the real `BitcoinTx.validateProof`, compiled from the same build and configured from the deployed Bridge's own factor and relay.
- **ADAPTER:**
  - `recordedBitcoin` serves the recorded data, with a tip that sets the confirmations;
  - `depthRecorder` embeds the real binding, captures proof submissions, and stands in for the wallet state.

**Run:** `test/spvdepth/run.sh <evidence dir>` writes `manifest.json`. It is accepted only when exactly the 16 expected cases ran and passed (factor 6 and 2 × four producers × two blocks); a skipped, missing or extra case is a failure.

| Variable | Value |
| --- | --- |
| `FROST_DEPTH_TBTC_BUILD` | `build-baseline.sh <tbtc-v2 repo> <out> <integration commit>` output |
| `FROST_DEPTH_TBTC_COMMIT` | that commit |
| `FROST_DEPTH_ANVIL` | Anvil |
| `FROST_DEPTH_SOLC` | solc 0.8.17 |

## Controls
In `controls/`, each applied to a scratch checkout with the test files copied in. Neither patch is a fix.

| Patch | Expected result |
| --- | --- |
| `28-derive-depth.fix` | The producers derive the depth with `getProofInfo` from the binding's Bridge factor and relay epochs. All 16 cases pass |
| `28-ignores-factor.wrong` | The same derivation with the factor hard-coded to 6 instead of read from the Bridge. The 8 factor-2 cases fail at `derived a 37-header proof; the Bridge at factor 2 needs exactly 7` (35 vs 11 for block 157937); the factor-6 cases pass |
| `28-factor-as-count.wrong` | Depth = the factor taken as a confirmation count (the legacy CLI's fallback). All 16 cases fail at `derived a 6-header proof; the Bridge at factor 6 needs exactly N` (2 and N at factor 2) |

The regtest end-to-end run (`pkg/frost/integration` `TestCrossSchemeDrainAndSweep`) is unchanged and stays as component evidence.
