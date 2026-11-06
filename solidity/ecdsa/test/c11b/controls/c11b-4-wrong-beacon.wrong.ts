// Control: the registry uses a beacon other than the legacy registry's
// (RandomBeaconChaosnet in the default world, which uses RandomBeacon).
import { replaceRegistry, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) =>
  replaceRegistry(hre, bundle, inputs, {
    beacon: (await hre.deployments.get("RandomBeaconChaosnet")).address,
  })
)
export default v.default
export const { deployFrostRegistry } = v
