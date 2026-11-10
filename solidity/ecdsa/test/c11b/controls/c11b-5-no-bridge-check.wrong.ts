// Control: the builder accepts any Bridge input (it deploys with the legacy
// registry's wallet owner instead of refusing a mismatch).
import { variant } from "./wrong"

const REG = ["function walletOwner() view returns (address)"]
const v = variant(
  (bundle) => bundle,
  async (inputs, hre) => {
    try {
      const legacy = new hre.ethers.Contract(
        inputs.walletRegistry,
        REG,
        hre.ethers.provider
      )
      return { ...inputs, bridge: await legacy.walletOwner() }
    } catch (e) {
      return inputs
    }
  }
)
export default v.default
export const { deployFrostRegistry } = v
