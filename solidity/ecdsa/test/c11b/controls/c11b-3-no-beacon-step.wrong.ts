// Control: the bundle omits the beacon requester authorization.
import { variant } from "./wrong"

const v = variant((bundle) => ({ ...bundle, steps: [] }))
export default v.default
export const { deployFrostRegistry } = v
