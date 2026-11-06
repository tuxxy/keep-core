// Control: a council step authorizes the FROST registry in the
// ReimbursementPool (base deploy 08 copied to the FROST registry).
import { council, iface, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => ({
  ...bundle,
  steps: [
    ...bundle.steps,
    council(
      (await hre.deployments.get("ReimbursementPool")).address,
      iface(["function authorize(address)"]).encodeFunctionData("authorize", [
        bundle.registry,
      ]),
      "authorize the FROST registry in the ReimbursementPool"
    ),
  ],
}))
export default v.default
export const { deployFrostRegistry } = v
