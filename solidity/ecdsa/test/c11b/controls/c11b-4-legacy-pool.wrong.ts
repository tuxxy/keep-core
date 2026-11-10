// Control: the registry uses the legacy ECDSA pool (§2 item 7 forbids it).
import { replaceRegistry, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) =>
  replaceRegistry(hre, bundle, inputs, {
    pool: (await hre.deployments.get("EcdsaSortitionPool")).address,
  })
)
export default v.default
export const { deployFrostRegistry } = v
