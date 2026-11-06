// Positive control (review C1): a correct builder that also sends a redundant
// "off" (registry requests false) before the end of the bundle. Every case
// must pass.
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const registry = await contract(hre, "FrostWalletRegistry", bundle.registry)
  await as(hre, inputs.council, async (s) =>
    (await registry.connect(s).setRequestsEnabled(false)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
