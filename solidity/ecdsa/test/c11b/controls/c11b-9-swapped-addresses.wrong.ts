// Control: the bundle reports the admission adapter as the validator (and the
// other way round); the wiring through the registry is untouched.
import { variant } from "./wrong"

const v = variant((bundle) => ({
  ...bundle,
  validator: bundle.admission,
  admission: bundle.validator,
}))
export default v.default
export const { deployFrostRegistry } = v
