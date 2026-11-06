/* eslint-disable no-await-in-loop, global-require, import/no-dynamic-require, @typescript-eslint/no-var-requires */
// Shared helper for the C-11b `.wrong` builder variants (removal controls).
// Each variant wraps the real builder and breaks one property; run it with
// `C11B_BUILDER=test/c11b/controls/<variant>.wrong.ts` (see run-controls.sh).
// The variants need the implementation (deploy/18_deploy_frost_registry.ts).
// C11B_REAL_BUILDER points them at another builder (a reference) instead.
//
// A variant that must leave an end state the real builder cannot (an owner
// other than the council, a switch already on) reaches it by impersonating
// the account that holds the role, on the local Hardhat network only. It
// stands for a builder that left that state behind.
import path from "path"

import { utils } from "ethers"

import type { FrostRegistryBundle, FrostRegistryInputs } from "../c11b-world"

const BUILDER = path.resolve(
  __dirname,
  "../../../deploy/18_deploy_frost_registry.ts"
)
// Loaded on use, so that `hardhat test` over the whole tree does not need the
// builder to exist.
// eslint-disable-next-line @typescript-eslint/no-var-requires, global-require, import/no-dynamic-require
export const real = () =>
  require(process.env.C11B_REAL_BUILDER
    ? path.resolve(process.env.C11B_REAL_BUILDER)
    : BUILDER)

export type Change = (
  bundle: FrostRegistryBundle,
  inputs: FrostRegistryInputs,
  hre: any
) => Promise<FrostRegistryBundle> | FrostRegistryBundle

/// A variant: the real bundle, changed by `change`.
export function variant(
  change: Change,
  wrapInputs?: (
    inputs: FrostRegistryInputs,
    hre: any
  ) => Promise<FrostRegistryInputs>
) {
  const deployFrostRegistry = async (hre: any, inputs: FrostRegistryInputs) =>
    change(
      await real().deployFrostRegistry(
        hre,
        wrapInputs ? await wrapInputs(inputs, hre) : inputs
      ),
      inputs,
      hre
    )
  const func: any = async (hre: any) => real().default(hre)
  Object.defineProperty(func, "tags", { get: () => real().default.tags })
  func.skip = (hre: any) => real().default.skip(hre)
  return { default: func, deployFrostRegistry }
}

/// Runs `act` as `address` (Hardhat impersonation; local network only).
export async function as<T>(
  hre: any,
  address: string,
  act: (signer: any) => Promise<T>
): Promise<T> {
  await hre.network.provider.send("hardhat_impersonateAccount", [address])
  await hre.network.provider.send("hardhat_setBalance", [
    address,
    "0x56bc75e2d63100000",
  ])
  const signer = await hre.ethers.getSigner(address)
  const result = await act(signer)
  await hre.network.provider.send("hardhat_stopImpersonatingAccount", [address])
  return result
}

export async function contract(hre: any, name: string, address: string) {
  return hre.ethers.getContractAt(name, address)
}

/// Deploys a replacement FrostWalletRegistry from the deployer with the
/// bundle's wiring, `override` applied, owned by the council.
export async function replaceRegistry(
  hre: any,
  bundle: FrostRegistryBundle,
  inputs: FrostRegistryInputs,
  override: Partial<{
    pool: string
    validator: string
    admission: string
    beacon: string
    bridge: string
    periods: number[]
    limit: number
  }>
): Promise<FrostRegistryBundle> {
  const { deployer } = await hre.getNamedAccounts()
  const old = await contract(hre, "FrostWalletRegistry", bundle.registry)
  const factory = await hre.ethers.getContractFactory(
    "FrostWalletRegistry",
    await hre.ethers.getSigner(deployer)
  )
  const registry = await factory.deploy(
    override.pool ?? (await old.sortitionPool()),
    override.validator ?? (await old.validator()),
    override.admission ?? (await old.admission()),
    override.beacon ?? (await old.randomBeacon()),
    override.bridge ?? (await old.walletOwner()),
    override.periods ?? [
      await old.seedTimeout(),
      await old.resultTimeout(),
      await old.challengePeriod(),
      await old.readinessPeriod(),
    ],
    override.limit ?? (await old.maxPoolOperators())
  )
  await registry.deployed()
  await (await registry.transferOwnership(inputs.council)).wait()
  // A wrong builder records what it deployed, as the real one does.
  await hre.deployments.save("FrostWalletRegistry", {
    address: registry.address,
    abi: (await hre.artifacts.readArtifact("FrostWalletRegistry")).abi,
  })
  return { ...bundle, registry: registry.address }
}

export const council = (to: string, data: string, description: string) => ({
  kind: "council" as const,
  to,
  value: 0,
  data,
  description,
})
export const iface = (signatures: string[]) => new utils.Interface(signatures)
