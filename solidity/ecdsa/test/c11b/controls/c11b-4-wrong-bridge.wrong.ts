// Control: the registry is bound to a contract that is not the Bridge owning
// the legacy registry (the legacy WalletRegistryGovernance).
import { replaceRegistry, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) =>
  replaceRegistry(hre, bundle, inputs, {
    bridge: (await hre.deployments.get("WalletRegistryGovernance")).address,
  })
)
export default v.default
export const { deployFrostRegistry } = v
