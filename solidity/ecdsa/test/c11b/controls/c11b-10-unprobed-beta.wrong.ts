// Control (review W5): inside the bundle, an address that no case probes is
// made a chaosnet beta operator of the FROST pool (permanent).
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const unprobed = (await hre.ethers.getSigners())[9].address
  const pool = await contract(hre, "SortitionPool", bundle.sortitionPool)
  await as(hre, inputs.chaosnetOwner, async (s) =>
    (await pool.connect(s).addBetaOperators([unprobed])).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
