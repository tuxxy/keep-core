// Control (review W1): inside the bundle, registry requests are turned on and
// off again, and an address that no case probes is allowed and disallowed.
// The end state is correct; only the bundle's events show it.
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const unprobed = (await hre.ethers.getSigners())[9].address
  const registry = await contract(hre, "FrostWalletRegistry", bundle.registry)
  const admission = await contract(hre, "FrostAdmission", bundle.admission)
  await as(hre, inputs.council, async (s) => {
    await (await registry.connect(s).setRequestsEnabled(true)).wait()
    await (await registry.connect(s).setRequestsEnabled(false)).wait()
    await (await admission.connect(s).setAllowed(unprobed, true)).wait()
    await (await admission.connect(s).setAllowed(unprobed, false)).wait()
  })
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
