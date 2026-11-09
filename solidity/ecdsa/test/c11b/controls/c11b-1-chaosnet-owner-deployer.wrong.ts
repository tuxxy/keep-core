// Control: the pool's chaosnet owner role stays with the deployer.
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const { deployer } = await hre.getNamedAccounts()
  const pool = await contract(hre, "SortitionPool", bundle.sortitionPool)
  await as(hre, inputs.chaosnetOwner, async (s) =>
    (await pool.connect(s).transferChaosnetOwnerRole(deployer)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
