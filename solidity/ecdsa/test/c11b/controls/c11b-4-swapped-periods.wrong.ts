// Control: seed timeout and challenge period swapped (both still valid).
import { contract, replaceRegistry, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const old = await contract(hre, "FrostWalletRegistry", bundle.registry)
  return replaceRegistry(hre, bundle, inputs, {
    periods: [
      await old.challengePeriod(),
      await old.resultTimeout(),
      await old.seedTimeout(),
      await old.readinessPeriod(),
    ],
  })
})
export default v.default
export const { deployFrostRegistry } = v
