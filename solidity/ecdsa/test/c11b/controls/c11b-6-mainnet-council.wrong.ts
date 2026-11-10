// Control: the tagged deploy hands the stack to the mainnet Council Safe
// address on every network (REVIEW-FIX-C11 N1).
import fs from "fs"

import { real } from "./wrong"

const REG = ["function walletOwner() view returns (address)"]
const func: any = async (hre: any) => {
  const file = process.env.FROST_REGISTRY_POLICY
  if (!file) throw new Error("Set FROST_REGISTRY_POLICY")
  const { chaosnetOwner } = await hre.getNamedAccounts()
  const wr = await hre.deployments.get("WalletRegistry")
  const legacy = new hre.ethers.Contract(wr.address, REG, hre.ethers.provider)
  return real().deployFrostRegistry(hre, {
    walletRegistry: wr.address,
    bridge: await legacy.walletOwner(),
    council: "0x9F6e831c8F8939DC0C830C6e492e7cEf4f9C2F5f",
    chaosnetOwner,
    policy: JSON.parse(fs.readFileSync(file, "utf8")).policy,
  })
}
Object.defineProperty(func, "tags", { get: () => real().default.tags })
func.skip = (hre: any) => real().default.skip(hre)
export default func
export const deployFrostRegistry = (hre: any, inputs: any) =>
  real().deployFrostRegistry(hre, inputs)
