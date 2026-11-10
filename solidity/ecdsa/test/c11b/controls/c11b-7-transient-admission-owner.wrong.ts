// Control: mid-bundle, the admission adapter is handed to a non-governance
// account (the probe's outsider) and taken back before the bundle ends. The
// end state is correct; only the window is open.
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const outsider = (await hre.ethers.getSigners())[7].address
  const admission = await contract(hre, "FrostAdmission", bundle.admission)
  await as(hre, inputs.council, async (s) =>
    (await admission.connect(s).transferOwnership(outsider)).wait()
  )
  await as(hre, outsider, async (s) =>
    (await admission.connect(s).transferOwnership(inputs.council)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
