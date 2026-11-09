// Control: the bundle admits an operator (the first staked world operator).
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const admission = await contract(hre, "FrostAdmission", bundle.admission)
  const operator = (await hre.ethers.getSigners())[14 + 4].address
  await as(hre, inputs.council, async (s) =>
    (await admission.connect(s).setAllowed(operator, true)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
