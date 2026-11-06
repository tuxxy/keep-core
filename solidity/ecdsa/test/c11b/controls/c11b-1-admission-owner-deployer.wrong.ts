// Control: the admission adapter stays with the deployer.
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const { deployer } = await hre.getNamedAccounts()
  const admission = await contract(hre, "FrostAdmission", bundle.admission)
  await as(hre, inputs.council, async (s) =>
    (await admission.connect(s).transferOwnership(deployer)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
