/* eslint-disable no-await-in-loop, no-restricted-syntax, no-continue, @typescript-eslint/no-unused-expressions */
// C-11b acceptance: deploy keep-core's FROST registry stack disabled, with
// governance handed to the production owner. Second half of KC-SF-V3 §7 C-11.
//
// Refs tuxxy/tbtc-v2#58 (C-11b part), tuxxy/keep-core C-11b issue (see the
// PR), KC-SF-V3 §7 C-11 (sequence: "deploy disabled new registry/pool/
// adapter"; G-D only), C-02 ("deploy with requests disabled"; "give the new
// pool one owner, the FROST registry"; "an old-registry mutation is
// forbidden"), §2 item 7 (separate registry and pool).
//
// Base first: the stack is deployed the way keep-core's ECDSA stack is
// (deploy 01 pool with the T reward token and the 1e18 weight divisor and the
// chaosnet owner role handed over, 02 validator, 03 registry with the pool
// handed to the registry, 10 governance to the `governance` named account,
// 13 beacon requester authorization by the beacon's governance). The builder
// is the production deploy script `deploy/18_deploy_frost_registry.ts`,
// loaded dynamically so its absence fails each case at its first named
// assertion. Owner-policy picks are D24 in DECISIONS.md.
import hre, { deployments, ethers, getNamedAccounts } from "hardhat"
import { expect } from "chai"

import {
  BUILDER_ENV,
  BUILDER_FILE,
  BUILDER_TAG,
  POLICY,
  POLICY_ENV,
  POLICY_FILE,
  buildWorld,
  loadBuilder,
} from "./c11b-world"
import {
  EIP1967_ADMIN_SLOT,
  EIP1967_IMPLEMENTATION_SLOT,
  EIP170_LIMIT,
  STACK,
  frameDiff,
  logsSince,
  maskImmutables,
  probeMutations,
  reproduce,
  stackFrame,
  withTransactionHook,
  wordAt,
} from "./c11b-probe"

import type { Contract } from "ethers"
import type {
  Builder,
  FrostRegistryBundle,
  FrostRegistryInputs,
  World,
} from "./c11b-world"
import type { Reproduction, StackRole } from "./c11b-probe"

const { AddressZero } = ethers.constants
const MAINNET_COUNCIL = "0x9f6e831c8f8939dc0c830c6e492e7cef4f9c2f5f"
const DEPLOYMENT_NAMES: Record<StackRole, string> = {
  registry: "FrostWalletRegistry",
  validator: "FrostDkgValidator",
  admission: "FrostAdmission",
  sortitionPool: "FrostSortitionPool",
}
const ROLES = Object.keys(STACK) as StackRole[]

// ---------------------------------------------------------------------------
// Fixtures (hardhat-deploy snapshots: chain state and deployment records).
// ---------------------------------------------------------------------------
const worldFixture = deployments.createFixture(async () => buildWorld())

async function requireBuilder(): Promise<Builder> {
  const builder = await loadBuilder()
  expect(builder, `C-11b builder present: ${BUILDER_FILE}`).to.not.equal(null)
  return builder as Builder
}

type Deployed = {
  w: World
  bundle: FrostRegistryBundle
  startBlock: number
}
let activeBuilder: Builder | null = null
const bundleFixture = deployments.createFixture(async (): Promise<Deployed> => {
  const w = await worldFixture()
  const startBlock = (await ethers.provider.getBlockNumber()) + 1
  const bundle = await (activeBuilder as Builder).deployFrostRegistry(
    hre,
    w.inputs
  )
  return { w, bundle, startBlock }
})
const stepsFixture = deployments.createFixture(async (): Promise<Deployed> => {
  const d = await bundleFixture()
  await runSteps(d.w, d.bundle)
  return d
})

async function deployed(withSteps: boolean): Promise<Deployed> {
  activeBuilder = await requireBuilder()
  return withSteps ? stepsFixture() : bundleFixture()
}

async function runSteps(w: World, bundle: FrostRegistryBundle) {
  for (let i = 0; i < bundle.steps.length; i++) {
    const step = bundle.steps[i]
    if (step.kind === "council") {
      await (
        await w.council.sendTransaction({
          to: step.to,
          value: step.value,
          data: step.data,
        })
      ).wait()
    } else if (step.kind === "wait") {
      await ethers.provider.send("evm_increaseTime", [step.seconds])
      await ethers.provider.send("evm_mine", [])
    } else {
      expect.fail(`bundle step ${i} has a known kind: ${JSON.stringify(step)}`)
    }
  }
}

function stackAddresses(bundle: FrostRegistryBundle): string[] {
  return [
    bundle.registry,
    bundle.validator,
    bundle.admission,
    bundle.sortitionPool,
    ...Object.values(bundle.libraries || {}),
  ]
}

async function stack(bundle: FrostRegistryBundle) {
  const c = async (role: StackRole, address: string) =>
    ethers.getContractAt(
      (await hre.artifacts.readArtifact(STACK[role])).abi,
      address
    )
  return {
    registry: await c("registry", bundle.registry),
    validator: await c("validator", bundle.validator),
    admission: await c("admission", bundle.admission),
    pool: await c("sortitionPool", bundle.sortitionPool),
  }
}

/// Refused = eth_estimateGas reverts (REVIEW-FIX-C11 N4: never eth_call).
async function refused(from: string, to: string, data: string) {
  try {
    await ethers.provider.estimateGas({ from, to, data })
    return false
  } catch (e) {
    return true
  }
}

async function revertReason(from: string, to: string, data: string) {
  try {
    await ethers.provider.estimateGas({ from, to, data })
    return null
  } catch (e: any) {
    return String(e?.error?.message || e?.message || e)
  }
}

function probeAddresses(w: World, bundle?: FrostRegistryBundle): string[] {
  return [
    w.outsider.address,
    w.deployer.address,
    w.council.address,
    w.chaosnetOwner.address,
    ...w.operators.map((o) => o.operator),
    ...(bundle ? [bundle.registry] : []),
  ]
}

/// The legacy frame: every zero-argument view of every legacy contract, the
/// per-operator and per-registry reads, and the proxy slots.
async function legacyFrame(w: World): Promise<Record<string, string>> {
  const frame: Record<string, string> = {}
  for (const [name, contract] of Object.entries(w.legacy)) {
    for (const f of Object.values(contract.interface.functions)) {
      if (!f.constant || f.inputs.length !== 0) continue
      try {
        frame[`${name}.${f.name}`] = JSON.stringify(
          await contract.callStatic[f.format()]()
        )
      } catch (e) {
        frame[`${name}.${f.name}`] = "revert"
      }
    }
    frame[`${name}:impl`] = await ethers.provider.getStorageAt(
      contract.address,
      EIP1967_IMPLEMENTATION_SLOT
    )
    frame[`${name}:admin`] = await ethers.provider.getStorageAt(
      contract.address,
      EIP1967_ADMIN_SLOT
    )
  }
  for (const o of w.operators) {
    frame[`eligibleStake(${o.stakingProvider})`] = (
      await w.walletRegistry.eligibleStake(o.stakingProvider)
    ).toString()
    frame[`operatorToStakingProvider(${o.operator})`] =
      await w.walletRegistry.operatorToStakingProvider(o.operator)
    frame[`legacyPool.isOperatorInPool(${o.operator})`] = String(
      await w.legacyPool.isOperatorInPool(o.operator)
    )
  }
  for (const beacon of [w.randomBeacon, w.randomBeaconChaosnet]) {
    frame[`${beacon.address}.authorizedRequesters(WalletRegistry)`] = String(
      await beacon.authorizedRequesters(w.walletRegistry.address)
    )
  }
  frame["TokenStaking.applicationInfo(WalletRegistry)"] = JSON.stringify(
    await w.tokenStaking.applicationInfo(w.walletRegistry.address)
  )
  frame["ReimbursementPool.isAuthorized(WalletRegistry)"] = String(
    await w.reimbursementPool.isAuthorized(w.walletRegistry.address)
  )
  return frame
}

// The suite needs the pinned tbtc-v2 checkout (C11B_TBTC). A plain
// `yarn test` without it skips the suite; test/c11b/run.sh sets C11B_RUN=1,
// so the acceptance run never skips (a missing C11B_TBTC fails it).
const suite =
  process.env.C11B_TBTC || process.env.C11B_RUN === "1"
    ? describe
    : describe.skip

suite("C-11b FROST registry stack deploy (keep-core, KC-SF-V3 §7 C-11)", () => {
  before(async () => {
    await worldFixture()
  })

  it("[0 interface] the builder is a tagged deploy script that base deploys skip unless DEPLOY_FROST_REGISTRY=true", async () => {
    const b = await requireBuilder()
    expect(
      typeof b.deployFrostRegistry,
      "builder exports deployFrostRegistry(hre, inputs)"
    ).to.equal("function")
    expect(b.default.tags || [], `builder tag ${BUILDER_TAG}`).to.include(
      BUILDER_TAG
    )
    expect(
      typeof b.default.skip,
      "builder has a skip function (base deploys must not run it)"
    ).to.equal("function")
    const saved = process.env[BUILDER_ENV]
    try {
      delete process.env[BUILDER_ENV]
      expect(
        await (b.default.skip as any)(hre),
        `skipped without ${BUILDER_ENV}`
      ).to.equal(true)
      process.env[BUILDER_ENV] = "true"
      expect(
        await (b.default.skip as any)(hre),
        `runs with ${BUILDER_ENV}=true`
      ).to.equal(false)
    } finally {
      if (saved === undefined) delete process.env[BUILDER_ENV]
      else process.env[BUILDER_ENV] = saved
    }
    await worldFixture()
    expect(
      await deployments.getOrNull("FrostWalletRegistry"),
      "the base fixture deploys no FROST registry"
    ).to.not.exist
  })

  it("[1 deploys] the builder deploys the FROST registry, its validator, admission adapter and its own sortition pool, and records them", async () => {
    const { w, bundle } = await deployed(false)
    for (const role of ROLES) {
      const address = (bundle as any)[role] as string
      expect(
        await ethers.provider.getCode(address),
        `bundle ${role} has code`
      ).to.not.equal("0x")
      const record = await deployments.getOrNull(DEPLOYMENT_NAMES[role])
      expect(
        record?.address,
        `deployment record ${DEPLOYMENT_NAMES[role]} is the bundle ${role}`
      ).to.equal(address)
    }
    const s = await stack(bundle)
    expect(
      await s.registry.sortitionPool(),
      "registry uses the bundle pool"
    ).to.equal(bundle.sortitionPool)
    expect(
      bundle.sortitionPool,
      "FROST pool is not the legacy pool (§2 item 7)"
    ).to.not.equal(w.legacyPool.address)
    expect(
      bundle.registry,
      "FROST registry is not the legacy registry"
    ).to.not.equal(w.walletRegistry.address)
  })

  it("[2 disabled] after the bundle and its council steps every FROST capability is off (G-D)", async () => {
    const { w, bundle } = await deployed(true)
    const s = await stack(bundle)
    expect(
      await s.registry.requestsEnabled(),
      "disabled: registry requests off"
    ).to.equal(false)
    expect(await s.registry.state(), "disabled: DKG state Idle").to.equal(0)
    expect(await s.registry.epoch(), "disabled: no DKG epoch").to.equal(0)
    expect(
      await s.pool.operatorsInPool(),
      "disabled: no operator in the FROST pool"
    ).to.equal(0)
    expect(
      await s.pool.isLocked(),
      "disabled: FROST pool unlocked (no DKG)"
    ).to.equal(false)
    expect(
      await s.pool.isChaosnetActive(),
      "disabled: FROST pool chaosnet still active (base deploy 01; D24)"
    ).to.equal(true)
    for (const a of probeAddresses(w)) {
      expect(
        await s.admission.allowed(a),
        `disabled: no FROST admission granted (${a})`
      ).to.equal(false)
      expect(
        await s.pool.isBetaOperator(a),
        `disabled: no FROST beta operator (${a})`
      ).to.equal(false)
    }
    for (const o of w.operators) {
      expect(
        (await w.walletRegistry.eligibleStake(o.stakingProvider)).gt(0),
        "world: the operator has legacy eligible stake"
      ).to.equal(true)
      expect(
        await s.admission.eligibleStake(o.operator),
        "disabled: FROST admission default-deny for a staked operator"
      ).to.equal(0)
      expect(
        await refused(
          w.outsider.address,
          bundle.registry,
          s.registry.interface.encodeFunctionData("refreshOperator", [
            o.operator,
          ])
        ),
        "disabled: a staked operator cannot be refreshed into the FROST pool"
      ).to.equal(true)
    }
    // Even the Bridge (the only requester) cannot start a DKG.
    await ethers.provider.send("hardhat_impersonateAccount", [w.bridge.address])
    await ethers.provider.send("hardhat_setBalance", [
      w.bridge.address,
      "0x56bc75e2d63100000",
    ])
    expect(
      await revertReason(
        w.bridge.address,
        bundle.registry,
        s.registry.interface.encodeFunctionData("requestNewWallet")
      ),
      "disabled: the Bridge's FROST wallet request is refused by the registry"
    ).to.contain("FROST request denied")
    expect(
      await w.bridge.frostRegistry(),
      "disabled: the bundle installs nothing on the Bridge (the C-11a runbook does)"
    ).to.equal(AddressZero)
  })

  it("[3 authority] the production owner holds every role, the pool belongs to the registry, and the deployer holds none", async () => {
    const { w, bundle } = await deployed(true)
    const s = await stack(bundle)
    expect(
      await s.registry.owner(),
      "authority: FrostWalletRegistry owner is the council"
    ).to.equal(w.council.address)
    expect(
      await s.admission.owner(),
      "authority: FrostAdmission owner is the council"
    ).to.equal(w.council.address)
    expect(
      await s.pool.owner(),
      "authority: FROST pool owner is the FROST registry"
    ).to.equal(bundle.registry)
    expect(
      await s.pool.chaosnetOwner(),
      "authority: FROST pool chaosnet owner is the chaosnetOwner named account"
    ).to.equal(w.chaosnetOwner.address)
    for (const address of stackAddresses(bundle)) {
      expect(
        await ethers.provider.getStorageAt(address, EIP1967_ADMIN_SLOT),
        `authority: ${address} is not an upgradeable proxy (immutable stack; D24)`
      ).to.equal(ethers.constants.HashZero)
    }
    // The production owner can operate every switch, both ways.
    const snap = await ethers.provider.send("evm_snapshot", [])
    try {
      const op = w.operators[0].operator
      for (const enabled of [true, false]) {
        for (const [label, target, data] of [
          [
            "registry requests",
            s.registry,
            s.registry.interface.encodeFunctionData("setRequestsEnabled", [
              enabled,
            ]),
          ],
          [
            "FROST admission",
            s.admission,
            s.admission.interface.encodeFunctionData("setAllowed", [
              op,
              enabled,
            ]),
          ],
        ] as [string, Contract, string][]) {
          expect(
            await revertReason(w.council.address, target.address, data),
            `authority: the council sets ${label} ${enabled}`
          ).to.equal(null)
          await (
            await w.council.sendTransaction({ to: target.address, data })
          ).wait()
        }
        expect(
          await s.registry.requestsEnabled(),
          `authority: registry requests now ${enabled}`
        ).to.equal(enabled)
        expect(
          await s.admission.allowed(op),
          `authority: FROST admission now ${enabled}`
        ).to.equal(enabled)
      }
    } finally {
      await ethers.provider.send("evm_revert", [snap])
    }
    const frame = () =>
      stackFrame(stackAddresses(bundle), probeAddresses(w, bundle))
    for (const [who, actor] of [
      ["the deployer", w.deployer.address],
      ["an outsider", w.outsider.address],
    ]) {
      const violations = await probeMutations(
        stackAddresses(bundle),
        actor,
        w.operators[0].operator,
        frame,
        `after the bundle: ${who}`
      )
      expect(
        violations,
        `authority: ${who} changes no FROST state after the bundle`
      ).to.deep.equal([])
    }
  })

  it("[3 steps] the bundle's governance steps are council calls, and after them the legacy registry's beacon authorizes the FROST registry", async () => {
    const { w, bundle } = await deployed(false)
    for (let i = 0; i < bundle.steps.length; i++) {
      const step = bundle.steps[i]
      expect(
        ["council", "wait"],
        `bundle step ${i} is a council call or a wait`
      ).to.include(step.kind)
    }
    await runSteps(w, bundle)
    expect(
      await w.randomBeacon.authorizedRequesters(bundle.registry),
      "steps: the legacy registry's beacon authorizes the FROST registry after the council steps"
    ).to.equal(true)
    expect(
      await w.randomBeacon.authorizedRequesters(w.walletRegistry.address),
      "steps: the legacy registry stays authorized"
    ).to.equal(true)
  })

  it("[4 wiring] the registry is wired to the Bridge that owns the legacy registry, the legacy stake source and the legacy beacon", async () => {
    const { w, bundle } = await deployed(true)
    const s = await stack(bundle)
    const bridge = await w.walletRegistry.walletOwner()
    expect(
      bridge,
      "world: the legacy WalletRegistry's wallet owner is the Bridge FIXTURE"
    ).to.equal(w.bridge.address)
    expect(
      (await w.bridge.contractReferences()).ecdsaWalletRegistry,
      "world: the Bridge's ECDSA registry is the legacy WalletRegistry"
    ).to.equal(w.walletRegistry.address)
    expect(
      await s.registry.walletOwner(),
      "wiring: registry wallet owner is the Bridge that owns the legacy WalletRegistry"
    ).to.equal(bridge)
    expect(
      await s.admission.source(),
      "wiring: admission stake source is the legacy WalletRegistry"
    ).to.equal(w.walletRegistry.address)
    expect(
      await s.registry.randomBeacon(),
      "wiring: registry beacon is the legacy registry's beacon"
    ).to.equal(await w.walletRegistry.randomBeacon())
    expect(
      await s.registry.admission(),
      "wiring: registry admission is the bundle admission"
    ).to.equal(bundle.admission)
    expect(
      await s.registry.validator(),
      "wiring: registry validator is the bundle validator"
    ).to.equal(bundle.validator)
    expect(
      await s.registry.sortitionPool(),
      "wiring: registry pool is the bundle pool"
    ).to.equal(bundle.sortitionPool)
    expect(
      await s.pool.rewardToken(),
      "wiring: FROST pool reward token is the legacy pool's (T)"
    ).to.equal(await w.legacyPool.rewardToken())
    expect(
      (await s.pool.poolWeightDivisor()).toString(),
      "wiring: FROST pool weight divisor is the legacy pool's"
    ).to.equal((await w.legacyPool.poolWeightDivisor()).toString())
    for (const k of [
      "groupSize",
      "threshold",
      "readySeats",
      "falseReadySeats",
      "unavailableSeats",
    ] as const) {
      expect(
        await s.validator[k](),
        `wiring: validator ${k} is the policy input (D-02 OPEN)`
      ).to.equal(POLICY[k])
    }
    for (const k of [
      "seedTimeout",
      "resultTimeout",
      "challengePeriod",
      "readinessPeriod",
      "maxPoolOperators",
    ] as const) {
      expect(
        Number(await s.registry[k]()),
        `wiring: registry ${k} is the policy input (D-02 OPEN)`
      ).to.equal(POLICY[k])
    }
    const app = await w.tokenStaking.applicationInfo(bundle.registry)
    expect(
      app.status,
      "wiring: the FROST registry is not a TokenStaking application"
    ).to.equal(0)
    expect(
      await w.reimbursementPool.isAuthorized(bundle.registry),
      "wiring: the FROST registry has no ReimbursementPool authorization"
    ).to.equal(false)
    // The stake source is live: an allowed operator gets its legacy stake.
    const o = w.operators[0]
    await (
      await s.admission.connect(w.council).setAllowed(o.operator, true)
    ).wait()
    const legacyStake = await w.walletRegistry.eligibleStake(o.stakingProvider)
    expect(legacyStake.gt(0), "world: legacy eligible stake").to.equal(true)
    expect(
      (await s.admission.eligibleStake(o.operator)).toString(),
      "wiring: an allowed operator's FROST stake is its legacy eligible stake"
    ).to.equal(legacyStake.toString())
  })

  it("[4 wiring, chaosnet beacon] when the legacy registry uses RandomBeaconChaosnet, the FROST registry and its authorization follow it", async () => {
    activeBuilder = await requireBuilder()
    const w = await worldFixture()
    // World variant (FIXTURE of mainnet's tag useRandomBeaconChaosnet): the
    // chaosnet beacon is owned by the council, and the council points the
    // legacy registry at it (base deploy 04/06 shape).
    const chaosnet = w.randomBeaconChaosnet
    const owner = await chaosnet.owner()
    if (owner !== w.council.address) {
      await ethers.provider.send("hardhat_impersonateAccount", [owner])
      await ethers.provider.send("hardhat_setBalance", [
        owner,
        "0x56bc75e2d63100000",
      ])
      await (
        await chaosnet
          .connect(await ethers.getSigner(owner))
          .transferOwnership(w.council.address)
      ).wait()
    }
    await (
      await w.walletRegistryGovernance
        .connect(w.council)
        .upgradeRandomBeacon(chaosnet.address)
    ).wait()
    expect(
      await w.walletRegistry.randomBeacon(),
      "world: legacy registry uses the chaosnet beacon"
    ).to.equal(chaosnet.address)
    const bundle = await activeBuilder.deployFrostRegistry(hre, w.inputs)
    const s = await stack(bundle)
    expect(
      await s.registry.randomBeacon(),
      "chaosnet: registry beacon is the legacy registry's beacon"
    ).to.equal(chaosnet.address)
    await runSteps(w, bundle)
    expect(
      await chaosnet.authorizedRequesters(bundle.registry),
      "chaosnet: the chaosnet beacon authorizes the FROST registry after the council steps"
    ).to.equal(true)
    expect(
      await w.randomBeacon.authorizedRequesters(bundle.registry),
      "chaosnet: no authorization on a beacon the registry does not use"
    ).to.equal(false)
  })

  it("[5 inputs] the builder refuses a wrong Bridge, registry or owner before deploying anything", async () => {
    const b = await requireBuilder()
    const w = await worldFixture()
    const cases: [string, Partial<FrostRegistryInputs>][] = [
      [
        "a Bridge that is not the legacy registry's wallet owner (a contract)",
        { bridge: w.walletRegistryGovernance.address },
      ],
      ["a Bridge with no code", { bridge: w.outsider.address }],
      [
        "a WalletRegistry that is not a WalletRegistry",
        { walletRegistry: w.bridge.address },
      ],
      ["a zero council", { council: AddressZero }],
      ["a zero chaosnet owner", { chaosnetOwner: AddressZero }],
    ]
    for (const [label, change] of cases) {
      await worldFixture()
      const nonce = await w.deployer.getTransactionCount()
      let threw = false
      try {
        await b.deployFrostRegistry(hre, { ...w.inputs, ...change })
      } catch (e) {
        threw = true
      }
      expect(threw, `inputs: refuses ${label}`).to.equal(true)
      expect(
        await w.deployer.getTransactionCount(),
        `inputs: ${label}: nothing deployed`
      ).to.equal(nonce)
      expect(
        await deployments.getOrNull("FrostWalletRegistry"),
        `inputs: ${label}: no registry record`
      ).to.not.exist
    }
  })

  it("[6 per-network] the tagged deploy takes the owners from this network's named accounts and refuses without a policy", async () => {
    const b = await requireBuilder()
    const w = await worldFixture()
    const saved = { d: process.env[BUILDER_ENV], p: process.env[POLICY_ENV] }
    try {
      process.env[BUILDER_ENV] = "true"
      delete process.env[POLICY_ENV]
      const nonce = await w.deployer.getTransactionCount()
      let message = ""
      try {
        await b.default(hre)
      } catch (e: any) {
        message = String(e?.message || e)
      }
      expect(
        message,
        `per-network: refuses without ${POLICY_ENV} (D-02 OPEN, no default)`
      ).to.contain(POLICY_ENV)
      expect(
        await w.deployer.getTransactionCount(),
        "per-network: nothing deployed without a policy"
      ).to.equal(nonce)

      process.env[POLICY_ENV] = POLICY_FILE
      await b.default(hre)
      const named = await getNamedAccounts()
      const record = await deployments.getOrNull("FrostWalletRegistry")
      expect(
        record,
        "per-network: the tagged deploy records FrostWalletRegistry"
      ).to.exist
      const registry = await ethers.getContractAt(
        (
          await hre.artifacts.readArtifact(STACK.registry)
        ).abi,
        (record as any).address
      )
      const admission = await ethers.getContractAt(
        (
          await hre.artifacts.readArtifact(STACK.admission)
        ).abi,
        await registry.admission()
      )
      const pool = await ethers.getContractAt(
        (
          await hre.artifacts.readArtifact(STACK.sortitionPool)
        ).abi,
        await registry.sortitionPool()
      )
      expect(
        await registry.owner(),
        "per-network: registry owner is the governance named account"
      ).to.equal(named.governance)
      expect(
        await admission.owner(),
        "per-network: admission owner is the governance named account"
      ).to.equal(named.governance)
      expect(
        await pool.chaosnetOwner(),
        "per-network: pool chaosnet owner is the chaosnetOwner named account"
      ).to.equal(named.chaosnetOwner)
      expect(
        (await registry.owner()).toLowerCase(),
        "per-network: no mainnet council off mainnet"
      ).to.not.equal(MAINNET_COUNCIL)
      expect(
        await registry.walletOwner(),
        "per-network: registry wallet owner is the legacy registry's"
      ).to.equal(await w.walletRegistry.walletOwner())
      expect(
        Number(await registry.readinessPeriod()),
        "per-network: policy read from the policy file"
      ).to.equal(POLICY.readinessPeriod)
    } finally {
      for (const [k, v] of [
        [BUILDER_ENV, saved.d],
        [POLICY_ENV, saved.p],
      ] as const) {
        if (v === undefined) delete process.env[k]
        else process.env[k] = v
      }
    }
  })

  it("[7 window] from the builder's first transaction to the end of the bundle, no outsider can take or change any FROST state", async () => {
    const b = await requireBuilder()
    const w = await worldFixture()
    const created: string[] = []
    const violations: string[] = []
    let transactions = 0
    const bundle = await withTransactionHook(
      () => b.deployFrostRegistry(hre, w.inputs),
      async (receipt) => {
        transactions += 1
        if (receipt?.contractAddress)
          created.push(ethers.utils.getAddress(receipt.contractAddress))
        if (created.length === 0) return
        const frame = () => stackFrame(created, probeAddresses(w))
        violations.push(
          ...(await probeMutations(
            created,
            w.outsider.address,
            w.operators[0].operator,
            frame,
            `after builder transaction ${transactions}`
          ))
        )
      }
    )
    for (const role of ROLES) {
      expect(
        created,
        `window: the probe saw the bundle ${role} created`
      ).to.include((bundle as any)[role])
    }
    expect(
      violations,
      "window: no outsider call changes FROST state during the bundle"
    ).to.deep.equal([])
  })

  it("[8 line-up] the C-11a runbook installs this registry on the Bridge, and a FROST request is still refused at the registry", async () => {
    const { w, bundle } = await deployed(true)
    const s = await stack(bundle)
    const configure = w.bridgeGovernance.interface.encodeFunctionData(
      "configureFrostRegistry",
      [bundle.registry]
    )
    expect(
      await revertReason(
        w.council.address,
        w.bridgeGovernance.address,
        configure
      ),
      "line-up: BridgeGovernance.configureFrostRegistry accepts the bundle registry"
    ).to.equal(null)
    await (
      await w.bridgeGovernance.configureFrostRegistry(bundle.registry)
    ).wait()
    expect(
      await w.bridge.frostRegistry(),
      "line-up: the Bridge's FROST registry is the bundle registry"
    ).to.equal(bundle.registry)
    await (await w.bridgeGovernance.setFrostRequestsEnabled(true)).wait()
    const request = w.bridge.interface.encodeFunctionData(
      "requestNewFrostWallet"
    )
    expect(
      await revertReason(w.outsider.address, w.bridge.address, request),
      "line-up: with Bridge requests on, a FROST wallet request is refused by the registry"
    ).to.contain("FROST request denied")
    expect(
      await revertReason(
        w.council.address,
        bundle.registry,
        s.registry.interface.encodeFunctionData("setRequestsEnabled", [true])
      ),
      "line-up: the production owner can open registry requests (the pool belongs to the registry)"
    ).to.equal(null)
    await (await s.registry.connect(w.council).setRequestsEnabled(true)).wait()
    const reason = await revertReason(
      w.outsider.address,
      w.bridge.address,
      request
    )
    expect(
      reason || "",
      "line-up: once opened, the request passes the registry gate"
    ).to.not.contain("FROST request denied")
    expect(
      reason || "",
      "line-up: once opened, the request passes the beacon authorization"
    ).to.not.contain("Requester must be authorized")
  })

  it("[9 bytecode] each runtime code reproduces its artifact with explicit links and immutables, within EIP-170", async () => {
    const { w, bundle, startBlock } = await deployed(false)
    const libraries = bundle.libraries || {}
    const bundleStart = (await ethers.provider.getBlock(startBlock)).timestamp
    const bundleEnd = (await ethers.provider.getBlock("latest")).timestamp
    const expected: Record<StackRole, Record<string, string>> = {
      registry: {
        sortitionPool: bundle.sortitionPool,
        validator: bundle.validator,
        admission: bundle.admission,
        randomBeacon: await w.walletRegistry.randomBeacon(),
        walletOwner: w.bridge.address,
        seedTimeout: String(POLICY.seedTimeout),
        resultTimeout: String(POLICY.resultTimeout),
        challengePeriod: String(POLICY.challengePeriod),
        readinessPeriod: String(POLICY.readinessPeriod),
        maxPoolOperators: String(POLICY.maxPoolOperators),
      },
      validator: {
        groupSize: String(POLICY.groupSize),
        threshold: String(POLICY.threshold),
        readySeats: String(POLICY.readySeats),
        falseReadySeats: String(POLICY.falseReadySeats),
        unavailableSeats: String(POLICY.unavailableSeats),
      },
      admission: { source: w.walletRegistry.address },
      sortitionPool: {
        rewardToken: await w.legacyPool.rewardToken(),
        poolWeightDivisor: (await w.legacyPool.poolWeightDivisor()).toString(),
        // Rewards: set to block.timestamp in the pool's constructor.
        ineligibleOffsetStart: "deployment time",
      },
    }
    const codes: Record<string, string> = {}
    const reps: Record<string, Reproduction> = {}
    for (const role of ROLES) {
      const address = (bundle as any)[role] as string
      const code = (await ethers.provider.getCode(address)).toLowerCase()
      const rep = await reproduce(STACK[role], libraries)
      codes[role] = code
      reps[role] = rep
      for (const link of rep.links) {
        expect(
          libraries[link.library],
          `bytecode: bundle reports the address of library ${link.library}`
        ).to.not.equal(undefined)
        expect(
          await ethers.provider.getCode(libraries[link.library]),
          `bytecode: linked library has code: ${link.library}`
        ).to.not.equal("0x")
      }
      expect(
        rep.expected.includes("__$"),
        `bytecode: no unlinked placeholder in ${role}`
      ).to.equal(false)
      expect(
        code.includes("__$"),
        `bytecode: no unlinked placeholder on chain in ${role}`
      ).to.equal(false)
      expect(
        maskImmutables(code, rep.immutables),
        `bytecode: runtime code of ${role} equals its artifact (links substituted, immutables masked, metadata kept)`
      ).to.equal(maskImmutables(rep.expected, rep.immutables))
      expect(
        (code.length - 2) / 2,
        `bytecode: ${role} runtime size <= ${EIP170_LIMIT}`
      ).to.be.at.most(EIP170_LIMIT)
    }
    for (const role of ROLES) {
      const code = codes[role]
      for (const im of reps[role].immutables) {
        const want = expected[role][im.name]
        expect(
          want,
          `bytecode: immutable ${role}.${im.name} has an expected value`
        ).to.not.equal(undefined)
        if (want === "deployment time") {
          const at = ethers.BigNumber.from(wordAt(code, im.start)).toNumber()
          expect(
            at >= bundleStart && at <= bundleEnd,
            `bytecode: immutable ${role}.${im.name} is a time within the bundle (${at} in [${bundleStart}, ${bundleEnd}])`
          ).to.equal(true)
          continue
        }
        const value = ethers.BigNumber.from(want)
        expect(
          wordAt(code, im.start),
          `bytecode: immutable ${role}.${im.name} holds ${want}`
        ).to.equal(
          ethers.utils.hexZeroPad(value.toHexString(), 32).toLowerCase()
        )
      }
    }
    for (const [name, address] of Object.entries(libraries)) {
      const size = ((await ethers.provider.getCode(address)).length - 2) / 2
      expect(
        size,
        `bytecode: library ${name} runtime size <= ${EIP170_LIMIT}`
      ).to.be.at.most(EIP170_LIMIT)
    }
  })

  it("[10 legacy] the bundle and its steps change no legacy contract and emit only the named events", async () => {
    const b = await requireBuilder()
    activeBuilder = b
    const w = await worldFixture()
    const before = await legacyFrame(w)
    const startBlock = (await ethers.provider.getBlockNumber()) + 1
    const bundle = await b.deployFrostRegistry(hre, w.inputs)
    await runSteps(w, bundle)
    expect(
      frameDiff(before, await legacyFrame(w)),
      "legacy: every legacy view, slot and authorization is unchanged (an old-registry mutation is forbidden)"
    ).to.deep.equal([])
    const s = await stack(bundle)
    const named: Record<string, { label: string; contract: Contract }> = {}
    Object.entries(w.legacy).forEach(([label, contract]) => {
      named[contract.address] = { label, contract }
    })
    named[bundle.registry] = {
      label: "FrostWalletRegistry",
      contract: s.registry,
    }
    named[bundle.validator] = {
      label: "FrostDkgValidator",
      contract: s.validator,
    }
    named[bundle.admission] = { label: "FrostAdmission", contract: s.admission }
    named[bundle.sortitionPool] = {
      label: "FrostSortitionPool",
      contract: s.pool,
    }
    const beacon = await w.walletRegistry.randomBeacon()
    const unexpected = (await logsSince(startBlock, named))
      .filter((log) => {
        const stackLog = [
          bundle.registry,
          bundle.admission,
          bundle.sortitionPool,
          bundle.validator,
        ].includes(log.address)
        if (
          stackLog &&
          ["OwnershipTransferred", "ChaosnetOwnerRoleTransferred"].includes(
            log.event
          )
        )
          return false
        if (
          log.address === beacon &&
          log.event === "RequesterAuthorizationUpdated" &&
          log.args?.requester === bundle.registry &&
          log.args?.isAuthorized === true
        )
          return false
        return true
      })
      .map(
        (log) =>
          `${log.contract}: ${log.event}${
            log.args ? JSON.stringify(log.args.map((a: any) => String(a))) : ""
          }`
      )
    expect(
      unexpected,
      "legacy: bundle and steps emit only the named events"
    ).to.deep.equal([])
  })
})
