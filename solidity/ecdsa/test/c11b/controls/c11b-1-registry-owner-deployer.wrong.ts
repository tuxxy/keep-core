// Control: the registry stays with the deployer (ownership never handed over).
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const { deployer } = await hre.getNamedAccounts()
  const registry = await contract(hre, "FrostWalletRegistry", bundle.registry)
  await as(hre, inputs.council, async (s) =>
    (await registry.connect(s).transferOwnership(deployer)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
