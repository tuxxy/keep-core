// Control: the bundle leaves registry requests on.
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const registry = await contract(hre, "FrostWalletRegistry", bundle.registry)
  await as(hre, inputs.council, async (s) =>
    (await registry.connect(s).setRequestsEnabled(true)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
