// Control: a council step changes the legacy beacon's authorization of the
// legacy registry (an old-registry mutation).
import { council, iface, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => ({
  ...bundle,
  steps: [
    ...bundle.steps,
    council(
      (await hre.deployments.get("RandomBeaconGovernance")).address,
      iface([
        "function setRequesterAuthorization(address,bool)",
      ]).encodeFunctionData("setRequesterAuthorization", [
        inputs.walletRegistry,
        false,
      ]),
      "deauthorize the legacy registry"
    ),
  ],
}))
export default v.default
export const { deployFrostRegistry } = v
