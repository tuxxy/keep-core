/* eslint-disable no-console */
// C-11b: deploy keep-core's FROST registry stack disabled, with its roles
// handed to the production owner (KC-SF-V3 §7 C-11, C-02; D24).
//
// The stack is FrostSortitionPool (a new SortitionPool, §2 item 7),
// FrostDkgValidator, FrostAdmission and FrostWalletRegistry. All four are
// non-upgradeable: references and policy are constructor immutables, so there
// is no proxy and no initializer to front-run. The shape follows the base ECDSA
// scripts:
//   - 01: the pool takes the legacy pool's reward token and weight divisor,
//     and its chaosnet owner role goes to `chaosnetOwner` (chaosnet stays
//     active, D24 (b));
//   - 03: the pool is owned by the registry;
//   - 10: the registry and the admission adapter go to the `governance`
//     named account (owned directly, D24 (a));
//   - 13: the beacon requester authorization is a council step (D24 (d)).
// Admission reads stake from the legacy WalletRegistry; the FROST registry is
// no TokenStaking application and gets no ReimbursementPool authorization
// (D24 (c)). Requests stay off: the council opens them during activation.
//
// Every input is checked before the first transaction, on every network
// (REVIEW-FIX-C11 N1/N2, REVIEW-TEST-C11B N3). The tagged script takes the
// owners from this network's named accounts, never a mainnet constant.
//
// Run: DEPLOY_FROST_REGISTRY=true FROST_REGISTRY_POLICY=<policy.json> \
//        npx hardhat deploy --network <net> --tags DeployFrostRegistry
// The policy file is `{"policy": {...}}` with the fields of
// FrostRegistryPolicy below. D-02 is OPEN: there is no default policy.

import fs from "fs"

import type { HardhatRuntimeEnvironment } from "hardhat/types"
import type { DeployFunction } from "hardhat-deploy/types"

export type FrostRegistryPolicy = {
  groupSize: number
  threshold: number
  readySeats: number
  falseReadySeats: number
  unavailableSeats: number
  seedTimeout: number
  resultTimeout: number
  challengePeriod: number
  readinessPeriod: number
  maxPoolOperators: number
}

export type FrostRegistryInputs = {
  walletRegistry: string // the legacy ECDSA WalletRegistry proxy
  bridge: string // the Bridge proxy: walletRegistry.walletOwner()
  council: string // the production owner
  chaosnetOwner: string // the FROST pool's chaosnet owner
  policy: FrostRegistryPolicy
}

export type FrostRegistryStep =
  | {
      kind: "council"
      to: string
      value: number
      data: string
      description: string
    }
  | { kind: "wait"; seconds: number; description: string }

export type FrostRegistryBundle = {
  sortitionPool: string
  validator: string
  admission: string
  registry: string
  libraries: Record<string, string>
  steps: FrostRegistryStep[]
}

const UINT16_FIELDS: (keyof FrostRegistryPolicy)[] = [
  "groupSize",
  "threshold",
  "readySeats",
  "falseReadySeats",
  "unavailableSeats",
  "maxPoolOperators",
]
const UINT64_FIELDS: (keyof FrostRegistryPolicy)[] = [
  "seedTimeout",
  "resultTimeout",
  "challengePeriod",
  "readinessPeriod",
]

const LEGACY_REGISTRY_ABI = [
  "function walletOwner() view returns (address)",
  "function randomBeacon() view returns (address)",
  "function sortitionPool() view returns (address)",
  "function staking() view returns (address)",
  "function operatorToStakingProvider(address) view returns (address)",
  "function eligibleStake(address) view returns (uint96)",
]
const LEGACY_POOL_ABI = [
  "function owner() view returns (address)",
  "function rewardToken() view returns (address)",
  "function poolWeightDivisor() view returns (uint256)",
]
const BRIDGE_ABI = [
  "function contractReferences() view returns (address bank, address relay, address ecdsaWalletRegistry, address reimbursementPool)",
]
const BEACON_ABI = [
  "function authorizedRequesters(address) view returns (bool)",
  "function governance() view returns (address)",
  "function owner() view returns (address)",
  "function setRequesterAuthorization(address,bool)",
]
const BEACON_GOVERNANCE_ABI = [
  "function randomBeacon() view returns (address)",
  "function owner() view returns (address)",
  "function setRequesterAuthorization(address,bool)",
]

function refuse(message: string): never {
  throw new Error(`DeployFrostRegistry: ${message}; nothing deployed`)
}

export function readPolicy(file: string | undefined): FrostRegistryPolicy {
  if (!file) {
    refuse(
      "set FROST_REGISTRY_POLICY to a JSON file with a policy object; " +
        "D-02 is OPEN, so there is no default policy"
    )
  }
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"))
  if (!parsed || typeof parsed.policy !== "object" || parsed.policy === null) {
    refuse(`FROST_REGISTRY_POLICY file ${file} has no policy object`)
  }
  return parsed.policy
}

function checkPolicy(policy: FrostRegistryPolicy) {
  if (!policy || typeof policy !== "object") refuse("policy missing")
  const integer = (key: keyof FrostRegistryPolicy, max: number) => {
    const v = policy[key]
    if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0 || v > max) {
      refuse(`policy.${key} must be an integer in [0, ${max}], got ${v}`)
    }
  }
  UINT16_FIELDS.forEach((key) => integer(key, 0xffff))
  UINT64_FIELDS.forEach((key) => integer(key, Number.MAX_SAFE_INTEGER))
}

/// Deploys the FROST registry stack disabled and hands its roles over.
/// Returns the addresses and the council steps that finish the bundle.
export async function deployFrostRegistry(
  hre: HardhatRuntimeEnvironment,
  inputs: FrostRegistryInputs
): Promise<FrostRegistryBundle> {
  const { deployments, getNamedAccounts, ethers, helpers } = hre
  const { deployer } = await getNamedAccounts()
  const { provider } = ethers
  const { AddressZero } = ethers.constants
  const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase()
  const hasCode = async (a: string) => (await provider.getCode(a)) !== "0x"
  const address = (label: string, value: string) => {
    if (!ethers.utils.isAddress(value) || same(value, AddressZero)) {
      refuse(`${label} must be a nonzero address, got ${value}`)
    }
    return ethers.utils.getAddress(value)
  }
  const read = async <T>(label: string, fn: () => Promise<T>): Promise<T> => {
    try {
      return await fn()
    } catch (e) {
      return refuse(label)
    }
  }

  // --- Inputs, all checked before the first transaction. -----------------
  const walletRegistry = address("walletRegistry", inputs.walletRegistry)
  const bridge = address("bridge", inputs.bridge)
  const council = address("council", inputs.council)
  const chaosnetOwner = address("chaosnetOwner", inputs.chaosnetOwner)
  checkPolicy(inputs.policy)
  const { policy } = inputs

  // The legacy registry: a WalletRegistry whose pool it owns (base 03).
  if (!(await hasCode(walletRegistry))) {
    refuse(`walletRegistry ${walletRegistry} has no code`)
  }
  const legacy = new ethers.Contract(
    walletRegistry,
    LEGACY_REGISTRY_ABI,
    provider
  )
  const notRegistry = `walletRegistry ${walletRegistry} is not a WalletRegistry`
  const walletOwner: string = await read(notRegistry, () =>
    legacy.walletOwner()
  )
  const beacon: string = await read(notRegistry, () => legacy.randomBeacon())
  const legacyPoolAddress: string = await read(notRegistry, () =>
    legacy.sortitionPool()
  )
  await read(notRegistry, () => legacy.staking())
  await read(notRegistry, () => legacy.operatorToStakingProvider(AddressZero))
  await read(notRegistry, () => legacy.eligibleStake(AddressZero))
  if (!(await hasCode(legacyPoolAddress)) || !(await hasCode(beacon))) {
    refuse(`${notRegistry}: its pool or beacon has no code`)
  }
  const legacyPool = new ethers.Contract(
    legacyPoolAddress,
    LEGACY_POOL_ABI,
    provider
  )
  const legacyPoolOwner: string = await read(notRegistry, () =>
    legacyPool.owner()
  )
  if (!same(legacyPoolOwner, walletRegistry)) {
    refuse(`${notRegistry}: its sortition pool is not owned by it`)
  }
  const rewardToken: string = await read(notRegistry, () =>
    legacyPool.rewardToken()
  )
  const poolWeightDivisor = await read(notRegistry, () =>
    legacyPool.poolWeightDivisor()
  )

  // The Bridge: the legacy registry's wallet owner, and a Bridge that names
  // that registry as its ECDSA registry.
  if (same(walletOwner, AddressZero) || !same(walletOwner, bridge)) {
    refuse(
      `bridge ${bridge} is not the legacy registry's wallet owner ` +
        `(${walletOwner})`
    )
  }
  if (!(await hasCode(bridge))) refuse(`bridge ${bridge} has no code`)
  const references: { ecdsaWalletRegistry: string } = await read(
    `bridge ${bridge} has no contractReferences()`,
    () => new ethers.Contract(bridge, BRIDGE_ABI, provider).contractReferences()
  )
  if (!same(references.ecdsaWalletRegistry, walletRegistry)) {
    refuse(
      `bridge ${bridge} uses ECDSA registry ${references.ecdsaWalletRegistry}, ` +
        `not ${walletRegistry}`
    )
  }

  // The beacon step's target and its authority: RandomBeacon through its
  // governance contract (base 13), or RandomBeaconChaosnet owned directly
  // (base 05/06). Any other beacon is refused.
  const beaconContract = new ethers.Contract(beacon, BEACON_ABI, provider)
  await read(`beacon ${beacon} has no authorizedRequesters()`, () =>
    beaconContract.authorizedRequesters(AddressZero)
  )
  let beaconTarget: string
  let beaconAuthority: string
  let beaconGovernance: string | null = null
  try {
    beaconGovernance = await beaconContract.governance()
  } catch (e) {
    beaconGovernance = null
  }
  if (beaconGovernance !== null) {
    const governance = beaconGovernance
    if (!(await hasCode(governance))) {
      refuse(`beacon ${beacon} governance ${governance} has no code`)
    }
    const gov = new ethers.Contract(governance, BEACON_GOVERNANCE_ABI, provider)
    const governed: string = await read(
      `beacon governance ${governance} has no randomBeacon()`,
      () => gov.randomBeacon()
    )
    if (!same(governed, beacon)) {
      refuse(
        `beacon governance ${governance} governs ${governed}, not ${beacon}`
      )
    }
    beaconTarget = governance
    beaconAuthority = await read(
      `beacon governance ${governance} has no owner()`,
      () => gov.owner()
    )
  } else {
    beaconTarget = beacon
    beaconAuthority = await read(
      `beacon ${beacon} has neither governance() nor owner()`,
      () => beaconContract.owner()
    )
  }
  if (!same(beaconAuthority, council)) {
    // Not a refusal: the step is the council's own call and fails safe at
    // execution. The runbook must route it through the beacon's authority.
    deployments.log(
      `WARNING: the beacon's authority ${beaconAuthority} is not the ` +
        `council ${council}; the beacon step must be sent by ${beaconAuthority}`
    )
  }

  // The constructors' own policy rules, simulated before anything is sent so
  // that a bad policy leaves no partial stack. eth_estimateGas, not eth_call:
  // a Hardhat eth_call can report a reverted creation as success
  // (REVIEW-FIX-C11 N4). The registry's references are
  // the addresses this deploy creates when nothing is reused; its
  // constructor only requires them nonzero.
  const nonce = await provider.getTransactionCount(deployer)
  const predicted = [1, 2].map((i) =>
    ethers.utils.getContractAddress({ from: deployer, nonce: nonce + i })
  )
  const periods = [
    policy.seedTimeout,
    policy.resultTimeout,
    policy.challengePeriod,
    policy.readinessPeriod,
  ]
  const validatorArgs = [
    policy.groupSize,
    policy.threshold,
    policy.readySeats,
    policy.falseReadySeats,
    policy.unavailableSeats,
  ]
  const simulate = async (contract: string, args: unknown[]) => {
    const factory = await ethers.getContractFactory(contract)
    const tx = factory.getDeployTransaction(...args)
    try {
      await provider.estimateGas({ from: deployer, data: tx.data })
    } catch (e) {
      refuse(
        `${contract} constructor refuses the policy: ${(e as Error).message}`
      )
    }
  }
  await simulate("FrostDkgValidator", validatorArgs)
  await simulate("FrostWalletRegistry", [
    ethers.utils.getContractAddress({ from: deployer, nonce }),
    predicted[0],
    predicted[1],
    beacon,
    bridge,
    periods,
    policy.maxPoolOperators,
  ])

  // --- Deploy (base 01, 02, 03). ------------------------------------------
  const opts = { from: deployer, log: true, waitConfirmations: 1 }
  const sortitionPool = await deployments.deploy("FrostSortitionPool", {
    ...opts,
    contract: "SortitionPool",
    args: [rewardToken, poolWeightDivisor],
  })
  const validator = await deployments.deploy("FrostDkgValidator", {
    ...opts,
    args: validatorArgs,
  })
  const admission = await deployments.deploy("FrostAdmission", {
    ...opts,
    args: [walletRegistry],
  })
  const registry = await deployments.deploy("FrostWalletRegistry", {
    ...opts,
    args: [
      sortitionPool.address,
      validator.address,
      admission.address,
      beacon,
      bridge,
      periods,
      policy.maxPoolOperators,
    ],
  })

  // --- Hand the roles over (base 01, 03, 10; D24). -------------------------
  // The pool to the registry, its chaosnet role to chaosnetOwner, and the
  // registry and admission to the council. Requests and admissions stay off.
  await helpers.ownable.transferOwnership(
    "FrostSortitionPool",
    registry.address,
    deployer
  )
  const currentChaosnetOwner: string = await deployments.read(
    "FrostSortitionPool",
    "chaosnetOwner"
  )
  if (!same(currentChaosnetOwner, chaosnetOwner)) {
    await deployments.execute(
      "FrostSortitionPool",
      opts,
      "transferChaosnetOwnerRole",
      chaosnetOwner
    )
  }
  await helpers.ownable.transferOwnership("FrostAdmission", council, deployer)
  await helpers.ownable.transferOwnership(
    "FrostWalletRegistry",
    council,
    deployer
  )

  // --- Council steps (base 05/13). -----------------------------------------
  const steps: FrostRegistryStep[] = [
    {
      kind: "council",
      to: beaconTarget,
      value: 0,
      data: new ethers.utils.Interface(BEACON_ABI).encodeFunctionData(
        "setRequesterAuthorization",
        [registry.address, true]
      ),
      description: `${
        `authorize FrostWalletRegistry ${registry.address} as a requester ` +
        `of the legacy registry's beacon ${beacon}`
      }${beaconTarget === beacon ? "" : ` through ${beaconTarget}`}`,
    },
  ]

  return {
    sortitionPool: sortitionPool.address,
    validator: validator.address,
    admission: admission.address,
    registry: registry.address,
    libraries: {},
    steps,
  }
}

const func: DeployFunction = async (hre: HardhatRuntimeEnvironment) => {
  const { deployments, getNamedAccounts, ethers } = hre
  const { governance, chaosnetOwner } = await getNamedAccounts()

  const policy = readPolicy(process.env.FROST_REGISTRY_POLICY)

  const WalletRegistry = await deployments.get("WalletRegistry")
  const walletOwner: string = await new ethers.Contract(
    WalletRegistry.address,
    LEGACY_REGISTRY_ABI,
    ethers.provider
  ).walletOwner()

  const bundle = await deployFrostRegistry(hre, {
    walletRegistry: WalletRegistry.address,
    bridge: walletOwner,
    council: governance,
    chaosnetOwner,
    policy,
  })

  deployments.log(
    `FROST registry stack deployed disabled: registry ${bundle.registry}, ` +
      `validator ${bundle.validator}, admission ${bundle.admission}, ` +
      `pool ${bundle.sortitionPool}`
  )
  bundle.steps.forEach((step, i) => {
    if (step.kind === "council") {
      deployments.log(
        `council step ${i}: ${step.description}\n` +
          `  to: ${step.to}\n  value: ${step.value}\n  data: ${step.data}`
      )
    } else {
      deployments.log(`step ${i}: wait ${step.seconds} s: ${step.description}`)
    }
  })

  return true
}

export default func

func.tags = ["DeployFrostRegistry"]
func.dependencies = ["WalletRegistry"]
func.id = "deploy_frost_registry"
func.skip = async (): Promise<boolean> =>
  process.env.DEPLOY_FROST_REGISTRY !== "true"
