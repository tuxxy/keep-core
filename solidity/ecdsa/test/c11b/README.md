# C-11b: FROST registry stack deploy (acceptance tests)

KC-SF-V3 §7 C-11, second half: deploy keep-core's FROST registry stack
(FrostWalletRegistry, FrostDkgValidator, FrostAdmission and its own
SortitionPool) **disabled**, with governance handed to the production owner.
Refs tuxxy/tbtc-v2#58 (C-11b part). C-11a (the tbtc-v2 Bridge bundle,
script 87) is tuxxy/tbtc-v2#60 at `39412825`.

## Run

```
# once: a tbtc-v2 checkout at the C-11a merge, compiled
git -C <tbtc-v2 repo> worktree add --detach <dir>/tbtc-v2 39412825454ae9d7d56811ce48cd826c92b821c2
ln -s <an installed tbtc-v2 solidity/node_modules> <dir>/tbtc-v2/solidity/node_modules
(cd <dir>/tbtc-v2/solidity && npx hardhat compile)

# from solidity/ecdsa
C11B_TBTC=<dir>/tbtc-v2 test/c11b/run.sh
C11B_TBTC=<dir>/tbtc-v2 test/c11b/controls/run-controls.sh all   # needs the implementation
```

`run.sh` checks the tbtc-v2 tree (`b0b02e2c`), runs without the ECDSA
stubs (the real WalletRegistry, EIP-170 enforced) and keeps temporary files
in `.c11b-tmp/`. A plain `yarn test` without `C11B_TBTC` skips this suite;
`run.sh` sets `C11B_RUN=1`, so the acceptance run never skips.

## The builder interface (defined by these tests)

- File `deploy/18_deploy_frost_registry.ts`, tag `DeployFrostRegistry`,
  `skip` unless `DEPLOY_FROST_REGISTRY=true` (base deploys and
  `deployments.fixture()` never run it).
- `export async function deployFrostRegistry(hre, inputs)` with
  `inputs = { walletRegistry, bridge, council, chaosnetOwner, policy }`:
  - `walletRegistry`: the legacy ECDSA WalletRegistry proxy;
  - `bridge`: the Bridge proxy; it must equal `walletRegistry.walletOwner()`;
  - `council`: the production owner;
  - `chaosnetOwner`: the FROST pool's chaosnet owner;
  - `policy`: `{groupSize, threshold, readySeats, falseReadySeats,
    unavailableSeats, seedTimeout, resultTimeout, challengePeriod,
    readinessPeriod, maxPoolOperators}` (D-02 OPEN).
- It returns `{ sortitionPool, validator, admission, registry, libraries,
  steps }`. `steps` are `{kind: "council", to, value, data, description}` or
  `{kind: "wait", seconds, description}`; the tests run them as the council.
- It records the deployments `FrostWalletRegistry`, `FrostDkgValidator`,
  `FrostAdmission` and `FrostSortitionPool`.
- It refuses, before sending any transaction: a Bridge that is not the legacy
  registry's wallet owner, a Bridge without code, a `walletRegistry` that is
  not a WalletRegistry, a zero council or chaosnet owner.
- The tagged function takes `council` and `chaosnetOwner` from this network's
  named accounts (`governance`, `chaosnetOwner`), the legacy registry from the
  `WalletRegistry` deployment, the Bridge from its `walletOwner()`, and the
  policy from the JSON file named by `FROST_REGISTRY_POLICY` (key `policy`).
  Without that variable it throws a message naming it, before deploying.

## What the cases require (the base pattern each extends)

| Case | Rule |
| --- | --- |
| 0 interface | the production deploy script above, skipped by base deploys |
| 1 deploys | four contracts with code, recorded; an own pool, not the legacy one (§2 item 7) |
| 2 disabled | registry requests off, DKG Idle, empty and unlocked pool with chaosnet active (base 01), default-deny admission even for staked operators, the Bridge's own request refused at the registry, nothing installed on the Bridge |
| 3 authority | registry and admission owned by the council (base 10), pool owned by the registry (base 03), chaosnet owner role to the `chaosnetOwner` account (base 01), no EIP-1967 proxy; the council operates each switch both ways; every state-changing call from the deployer or an outsider is refused or changes nothing |
| 3 steps | steps are council calls; after them the legacy registry's beacon authorizes the FROST registry (base 13) |
| 4 wiring | `walletOwner` is the Bridge owning the legacy registry; admission reads the legacy WalletRegistry; the beacon is the legacy registry's; pool reward token and weight divisor are the legacy pool's; every policy value reaches its immutable; no TokenStaking application and no ReimbursementPool authorization; an allowed operator's FROST stake equals its legacy eligible stake |
| 4 chaosnet | with the legacy registry on RandomBeaconChaosnet, the FROST registry and its authorization follow it |
| 5 inputs | each wrong input refused with the deployer's nonce unchanged and no record |
| 6 per-network | named accounts, no mainnet council off mainnet, policy file required (REVIEW-FIX-C11 N1) |
| 7 window | after every builder transaction, every state-changing call of the stack ABI from an outsider is refused or changes nothing |
| 8 line-up | the council installs the registry through the C-11a BridgeGovernance (`configureFrostRegistry`); with Bridge requests on, a request is refused at the registry; once the council opens the registry, the request passes both the registry gate and the beacon authorization |
| 9 bytecode | runtime code equals the artifact with links substituted and immutables masked (metadata kept); each immutable holds its expected value; each size ≤ 24,576 |
| 10 legacy | no legacy view, slot or authorization changes; the only events are ownership and chaosnet-role transfers on the stack and the beacon's `RequesterAuthorizationUpdated(registry, true)` |

Refusals are read with `eth_estimateGas` (never `eth_call`, REVIEW-FIX-C11
N4). A call that does not revert is sent inside an EVM snapshot and counts
only if it changes the frame.

## FIXTUREs

- The Bridge is the tbtc-v2 FROST Bridge at `39412825`, deployed fresh
  behind a proxy, initialized with this world's real WalletRegistry and
  ReimbursementPool, governed by the 39412825 BridgeGovernance (D22
  forwarders) owned by the council stand-in. It stands for the Bridge after
  C-11a's upgrade; the upgrade path is C-11a's own test.
- The relay is a FIXTURE address; no SPV proof is exercised.
- The policy (`policy.test-values.json`) is TEST VALUES, D-02 OPEN.
- In the chaosnet case, the chaosnet beacon's ownership is moved to the
  council before the council points the legacy registry at it.

## Controls

`controls/run-controls.sh <name>|all` runs each `.wrong.ts` variant (via
`C11B_BUILDER`) and passes when its target case fails at the named assertion.
The variants wrap the real builder (`C11B_REAL_BUILDER` overrides it). A
variant that must leave an end state the real builder cannot reach uses
Hardhat impersonation of the role holder; it stands for a builder that left
that state.
