// Control: admission reads TokenStaking instead of the legacy WalletRegistry.
import { replaceRegistry, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const { deployer } = await hre.getNamedAccounts()
  const factory = await hre.ethers.getContractFactory(
    "FrostAdmission",
    await hre.ethers.getSigner(deployer)
  )
  const admission = await factory.deploy(
    (
      await hre.deployments.get("TokenStaking")
    ).address
  )
  await admission.deployed()
  await (await admission.transferOwnership(inputs.council)).wait()
  return {
    ...(await replaceRegistry(hre, bundle, inputs, {
      admission: admission.address,
    })),
    admission: admission.address,
  }
})
export default v.default
export const { deployFrostRegistry } = v
