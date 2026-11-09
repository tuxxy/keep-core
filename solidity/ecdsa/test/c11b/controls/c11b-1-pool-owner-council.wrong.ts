// Control: the FROST pool is owned by the council instead of the registry
// (the base relationship copied wrongly; C-02 "one owner, the FROST registry").
import { as, contract, variant } from "./wrong"

const v = variant(async (bundle, inputs, hre) => {
  const pool = await contract(hre, "SortitionPool", bundle.sortitionPool)
  await as(hre, bundle.registry, async (s) =>
    (await pool.connect(s).transferOwnership(inputs.council)).wait()
  )
  return bundle
})
export default v.default
export const { deployFrostRegistry } = v
