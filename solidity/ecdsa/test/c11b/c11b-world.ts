/* eslint-disable no-await-in-loop, no-restricted-syntax, no-console */
// C-11b world: the base keep-core ECDSA stack deployed by base's own deploy
// scripts (`deployments.fixture()`, governance handed over by scripts 10, 11,
// 13 and 14), two operators staked on the legacy WalletRegistry, and the
// tbtc-v2 Bridge that owns that registry.
//
// Refs tuxxy/tbtc-v2#58 (C-11b part), KC-SF-V3 §7 C-11 and C-02.
//
// FIXTURE (labelled): the Bridge is the tbtc-v2 FROST Bridge at the C-11a
// merge (integration/frost-base 39412825), deployed fresh behind a
// TransparentUpgradeableProxy and initialized with this world's real legacy
// WalletRegistry and ReimbursementPool. It stands for the Bridge after C-11a's
// upgrade; the upgrade path itself is C-11a's (tuxxy/tbtc-v2#59, #60). Its
// BridgeGovernance is the 39412825 version with the D22 forwarders, owned by
// the council stand-in. The relay is a FIXTURE address (no SPV is exercised).
//
// The tbtc-v2 artifacts come from a pinned checkout named by C11B_TBTC (see
// README.md). Nothing is read from /tmp.
import fs from "fs"
import path from "path"
import { execFileSync } from "child_process"

import hre, { deployments, ethers, getNamedAccounts } from "hardhat"

import { stake } from "../utils/operators"

import type { Contract } from "ethers"
import type { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers"
// eslint-disable-next-line @typescript-eslint/no-var-requires
const { checkTbtcBuild } = require("./check-tbtc-build")

// ---------------------------------------------------------------------------
// The builder under test (the production deploy script, defined by these
// tests; see README.md for the interface).
// ---------------------------------------------------------------------------
export const BUILDER_FILE = "deploy/18_deploy_frost_registry.ts"
export const BUILDER_TAG = "DeployFrostRegistry"
export const BUILDER_ENV = "DEPLOY_FROST_REGISTRY"
export const POLICY_ENV = "FROST_REGISTRY_POLICY"
export const ECDSA_ROOT = path.resolve(__dirname, "../..")
export const POLICY_FILE = path.join(__dirname, "policy.test-values.json")

/// D-02 OPEN: every value below is a TEST VALUE, not a production policy.
/// The values are pairwise distinct so a swapped wire shows.
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
export const POLICY: FrostRegistryPolicy = JSON.parse(
  fs.readFileSync(POLICY_FILE, "utf8")
).policy

export type FrostRegistryInputs = {
  walletRegistry: string // the legacy ECDSA WalletRegistry proxy
  bridge: string // the Bridge proxy; must be walletRegistry.walletOwner()
  council: string // the production owner (the `governance` named account)
  chaosnetOwner: string // the FROST pool's chaosnet owner (named account)
  policy: FrostRegistryPolicy // D-02 OPEN
}
export type FrostRegistryStep =
  | {
      kind: "council"
      to: string
      value: number | string
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
export type Builder = {
  default: {
    (h: typeof hre): Promise<unknown>
    tags?: string[]
    skip?: (h: typeof hre) => Promise<boolean>
  }
  deployFrostRegistry: (
    h: typeof hre,
    inputs: FrostRegistryInputs
  ) => Promise<FrostRegistryBundle>
}

/// The builder module, or null when the file does not exist yet. A control
/// run points C11B_BUILDER at a `.wrong` variant (controls/run-controls.sh).
export function builderPath(): string {
  return process.env.C11B_BUILDER
    ? path.resolve(process.env.C11B_BUILDER)
    : path.join(ECDSA_ROOT, BUILDER_FILE)
}
export async function loadBuilder(): Promise<Builder | null> {
  const file = builderPath()
  if (!fs.existsSync(file)) return null
  // eslint-disable-next-line import/no-dynamic-require, global-require
  return import(file)
}

// ---------------------------------------------------------------------------
// tbtc-v2 artifacts (FIXTURE source, pinned).
// ---------------------------------------------------------------------------
export const TBTC_PIN_COMMIT = "39412825454ae9d7d56811ce48cd826c92b821c2"
export const TBTC_PIN_TREE = "b0b02e2ccfcadaff05b15163120f503bc823cc82"

export function tbtcRoot(): string {
  const root = process.env.C11B_TBTC
  if (!root) {
    throw new Error(
      "C11B_TBTC is not set: point it at a tbtc-v2 checkout of " +
        `${TBTC_PIN_COMMIT}, compiled (see test/c11b/README.md)`
    )
  }
  const tree = execFileSync("git", ["-C", root, "rev-parse", "HEAD^{tree}"], {
    encoding: "utf8",
  }).trim()
  if (tree !== TBTC_PIN_TREE) {
    throw new Error(
      `C11B_TBTC tree ${tree} is not the pinned tbtc-v2 tree ${TBTC_PIN_TREE}`
    )
  }
  return root
}

type TbtcArtifact = {
  abi: any[]
  bytecode: string
  linkReferences: Record<string, Record<string, { start: number }[]>>
}
/// Every tbtc-v2 artifact the world reads, checked against the pinned tree
/// (check-tbtc-build.js, review N4) once the world is built.
const loadedTbtc: [string, string][] = []
export function tbtcArtifact(source: string, name: string): TbtcArtifact {
  if (!loadedTbtc.some(([s, n]) => s === source && n === name))
    loadedTbtc.push([source, name])
  return JSON.parse(
    fs.readFileSync(
      path.join(tbtcRoot(), "solidity/build", source, `${name}.json`),
      "utf8"
    )
  )
}

/// Deploys a tbtc-v2 artifact, linking every library it references (each
/// library deployed once per world).
async function deployTbtc(
  from: SignerWithAddress,
  source: string,
  name: string,
  args: unknown[],
  libraries: Map<string, string>
): Promise<Contract> {
  const a = tbtcArtifact(source, name)
  let { bytecode } = a
  for (const [file, names] of Object.entries(a.linkReferences || {})) {
    for (const [lib, positions] of Object.entries(names)) {
      const key = `${file}:${lib}`
      if (!libraries.has(key)) {
        libraries.set(
          key,
          (await deployTbtc(from, file, lib, [], libraries)).address
        )
      }
      const address = (libraries.get(key) as string).slice(2).toLowerCase()
      for (const p of positions) {
        bytecode =
          bytecode.slice(0, 2 + p.start * 2) +
          address +
          bytecode.slice(2 + (p.start + 20) * 2)
      }
    }
  }
  const c = await new ethers.ContractFactory(a.abi, bytecode, from).deploy(
    ...args
  )
  await c.deployed()
  return c
}

// ---------------------------------------------------------------------------
// The world.
// ---------------------------------------------------------------------------
export type World = {
  deployer: SignerWithAddress
  council: SignerWithAddress // the `governance` named account (stand-in)
  chaosnetOwner: SignerWithAddress
  esdm: SignerWithAddress
  outsider: SignerWithAddress
  tbtcDeployer: SignerWithAddress
  walletRegistry: Contract
  walletRegistryGovernance: Contract
  legacyPool: Contract
  randomBeacon: Contract
  randomBeaconGovernance: Contract
  randomBeaconChaosnet: Contract
  tokenStaking: Contract
  reimbursementPool: Contract
  t: Contract
  bridge: Contract // FIXTURE: tbtc-v2 39412825 Bridge (Bridge + IFrostBridge ABI)
  bridgeGovernance: Contract
  bridgeProxyAdmin: Contract
  operators: { operator: string; stakingProvider: string }[]
  inputs: FrostRegistryInputs
  legacy: Record<string, Contract> // every legacy contract, for frames
}

async function at(name: string): Promise<Contract> {
  const d = await deployments.get(name)
  return ethers.getContractAt(d.abi, d.address)
}

export async function buildWorld(): Promise<World> {
  // Base deploy scripts only: the FROST script must not run in the fixture.
  delete process.env[BUILDER_ENV]
  await deployments.fixture()

  const named = await getNamedAccounts()
  const deployer = await ethers.getSigner(named.deployer)
  const council = await ethers.getSigner(named.governance)
  const chaosnetOwner = await ethers.getSigner(named.chaosnetOwner)
  const esdm = await ethers.getSigner(named.esdm)
  const all = await ethers.getSigners()
  // Unnamed accounts 5..9 (named accounts are 1..4; operators start at 14).
  const outsider = all[7]
  const tbtcDeployer = all[8]
  const relayFixture = all[9]
  const treasury = all[6]

  const walletRegistry = await at("WalletRegistry")
  const walletRegistryGovernance = await at("WalletRegistryGovernance")
  const legacyPool = await at("EcdsaSortitionPool")
  const randomBeacon = await at("RandomBeacon")
  const randomBeaconGovernance = await at("RandomBeaconGovernance")
  const randomBeaconChaosnet = await at("RandomBeaconChaosnet")
  const tokenStaking = await at("TokenStaking")
  const reimbursementPool = await at("ReimbursementPool")
  const t = await at("T")

  // Two operators with real legacy stake (TokenStaking path), registered on
  // the legacy WalletRegistry. They are not in any pool.
  const signers = all.slice(14)
  const operators: { operator: string; stakingProvider: string }[] = []
  const amount = await walletRegistry.minimumAuthorization()
  for (let i = 0; i < 2; i++) {
    const [owner, provider, operator, beneficiary, authorizer] = [
      signers[i],
      signers[2 + i],
      signers[4 + i],
      signers[6 + i],
      signers[8 + i],
    ]
    await stake(
      t as any,
      tokenStaking as any,
      walletRegistry as any,
      owner,
      provider,
      amount.mul(2),
      beneficiary,
      authorizer
    )
    await (
      await walletRegistry.connect(provider).registerOperator(operator.address)
    ).wait()
    operators.push({
      operator: operator.address,
      stakingProvider: provider.address,
    })
  }

  // FIXTURE Bridge (see the header).
  const libraries = new Map<string, string>()
  const bank = await deployTbtc(
    tbtcDeployer,
    "contracts/bank/Bank.sol",
    "Bank",
    [],
    libraries
  )
  const implementation = await deployTbtc(
    tbtcDeployer,
    "contracts/bridge/Bridge.sol",
    "Bridge",
    [],
    libraries
  )
  const bridgeProxyAdmin = await deployTbtc(
    tbtcDeployer,
    "@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol",
    "ProxyAdmin",
    [],
    libraries
  )
  const initializer = implementation.interface.encodeFunctionData(
    "initialize",
    [
      bank.address,
      relayFixture.address,
      treasury.address,
      walletRegistry.address,
      reimbursementPool.address,
      1,
    ]
  )
  const proxy = await deployTbtc(
    tbtcDeployer,
    "@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol",
    "TransparentUpgradeableProxy",
    [implementation.address, bridgeProxyAdmin.address, initializer],
    libraries
  )
  const bridgeAbi = [
    ...tbtcArtifact("contracts/bridge/Bridge.sol", "Bridge").abi,
    ...tbtcArtifact("contracts/frost/IFrostBridge.sol", "IFrostBridge").abi,
  ].filter(
    (f, i, list) =>
      f.type !== "constructor" &&
      list.findIndex(
        (g) =>
          g.type === f.type &&
          g.name === f.name &&
          JSON.stringify(g.inputs) === JSON.stringify(f.inputs)
      ) === i
  )
  const bridge = new ethers.Contract(proxy.address, bridgeAbi, tbtcDeployer)
  const bridgeGovernance = await deployTbtc(
    tbtcDeployer,
    "contracts/bridge/BridgeGovernance.sol",
    "BridgeGovernance",
    [bridge.address, 172800],
    libraries
  )
  await (await bridgeGovernance.transferOwnership(council.address)).wait()
  await (
    await bridge
      .connect(tbtcDeployer)
      .transferGovernance(bridgeGovernance.address)
  ).wait()
  await (await bridgeProxyAdmin.transferOwnership(esdm.address)).wait()

  // Base: the council initializes the legacy registry's wallet owner (the
  // Bridge), as tbtc-v2's deploy does through WalletRegistryGovernance.
  await (
    await walletRegistryGovernance
      .connect(council)
      .initializeWalletOwner(bridge.address)
  ).wait()

  // FIXTURE provenance: the artifacts above come from the pinned tree.
  checkTbtcBuild(tbtcRoot(), TBTC_PIN_TREE, loadedTbtc)

  return {
    deployer,
    council,
    chaosnetOwner,
    esdm,
    outsider,
    tbtcDeployer,
    walletRegistry,
    walletRegistryGovernance,
    legacyPool,
    randomBeacon,
    randomBeaconGovernance,
    randomBeaconChaosnet,
    tokenStaking,
    reimbursementPool,
    t,
    bridge,
    bridgeGovernance: bridgeGovernance.connect(council),
    bridgeProxyAdmin,
    operators,
    inputs: {
      walletRegistry: walletRegistry.address,
      bridge: bridge.address,
      council: council.address,
      chaosnetOwner: chaosnetOwner.address,
      policy: POLICY,
    },
    legacy: {
      WalletRegistry: walletRegistry,
      WalletRegistryGovernance: walletRegistryGovernance,
      EcdsaSortitionPool: legacyPool,
      RandomBeacon: randomBeacon,
      RandomBeaconGovernance: randomBeaconGovernance,
      RandomBeaconChaosnet: randomBeaconChaosnet,
      TokenStaking: tokenStaking,
      ReimbursementPool: reimbursementPool,
      T: t,
      Bridge: bridge,
      BridgeGovernance: bridgeGovernance,
      Bank: bank,
      BridgeProxyAdmin: bridgeProxyAdmin,
    },
  }
}
