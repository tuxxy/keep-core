/* eslint-disable no-await-in-loop, no-restricted-syntax, no-continue, no-underscore-dangle, prefer-destructuring */
// C-11b frames, outsider probes, bundle logs and bytecode reproduction.
// Refs tuxxy/tbtc-v2#58 (C-11b part), KC-SF-V3 §7 C-11.
//
// A refusal is read with eth_estimateGas, never eth_call: Hardhat returns
// "0x" from eth_call for a revert without data, which would read as success
// (REVIEW-FIX-C11 N4). A call that does not revert is sent inside an EVM
// snapshot, the frame is read, and the snapshot is reverted: it counts as a
// violation only if it changed the frame.
import hre, { ethers } from "hardhat"

import type { Contract } from "ethers"
import type { Fragment, FunctionFragment, ParamType } from "ethers/lib/utils"

const { utils } = ethers

export const EIP1967_IMPLEMENTATION_SLOT =
  "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"
export const EIP1967_ADMIN_SLOT =
  "0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103"
export const EIP170_LIMIT = 24576

/// The FROST stack artifacts and their fully qualified names.
export const STACK = {
  registry: "contracts/frost/FrostWalletRegistry.sol:FrostWalletRegistry",
  validator: "contracts/frost/FrostDkgValidator.sol:FrostDkgValidator",
  admission: "contracts/frost/FrostAdmission.sol:FrostAdmission",
  sortitionPool:
    "@keep-network/sortition-pools/contracts/SortitionPool.sol:SortitionPool",
} as const
export type StackRole = keyof typeof STACK

const EXTRA = [
  "@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol:TransparentUpgradeableProxy",
]

let probeInterface: utils.Interface | null = null
/// Union of every FROST stack ABI plus the proxy ABI, so that a probe also
/// covers an implementation that puts a contract behind a proxy.
export async function stackInterface(): Promise<utils.Interface> {
  if (probeInterface) return probeInterface
  const fragments: Fragment[] = []
  const seen = new Set<string>()
  for (const fqn of [...Object.values(STACK), ...EXTRA]) {
    const { abi } = await hre.artifacts.readArtifact(fqn)
    for (const f of new utils.Interface(abi).fragments) {
      if (f.type === "constructor") continue
      const key = f.format(utils.FormatTypes.sighash) + f.type
      if (seen.has(key)) continue
      seen.add(key)
      fragments.push(f)
    }
  }
  probeInterface = new utils.Interface(fragments)
  return probeInterface
}

async function codeOf(address: string): Promise<string> {
  return (await ethers.provider.getCode(address)).toLowerCase()
}

/// The runtime code that dispatches calls at `address` (the implementation
/// for an EIP-1967 proxy).
async function dispatchCode(address: string): Promise<string> {
  const impl = await ethers.provider.getStorageAt(
    address,
    EIP1967_IMPLEMENTATION_SLOT
  )
  const code = await codeOf(address)
  if (impl !== ethers.constants.HashZero) {
    return code + (await codeOf(utils.hexDataSlice(impl, 12)))
  }
  return code
}

function hasSelector(code: string, f: FunctionFragment): boolean {
  return code.includes(
    `63${utils.Interface.getSighash(f).slice(2).toLowerCase()}`
  )
}

/// A frame of the FROST stack: code, EIP-1967 slots, every zero-argument
/// view, and every one-address view at each probe address.
export async function stackFrame(
  addresses: string[],
  probeAddresses: string[]
): Promise<Record<string, string>> {
  const iface = await stackInterface()
  const frame: Record<string, string> = {}
  for (const address of addresses) {
    const code = await codeOf(address)
    frame[`${address}:code`] = utils.keccak256(code)
    frame[`${address}:impl`] = await ethers.provider.getStorageAt(
      address,
      EIP1967_IMPLEMENTATION_SLOT
    )
    frame[`${address}:admin`] = await ethers.provider.getStorageAt(
      address,
      EIP1967_ADMIN_SLOT
    )
    if (code === "0x") continue
    const dispatch = await dispatchCode(address)
    for (const f of Object.values(iface.functions)) {
      if (!f.constant || !hasSelector(dispatch, f)) continue
      const argSets: unknown[][] = []
      if (f.inputs.length === 0) argSets.push([])
      else if (f.inputs.length === 1 && f.inputs[0].type === "address")
        probeAddresses.forEach((a) => argSets.push([a]))
      else continue
      for (const args of argSets) {
        let value: string
        try {
          value = await ethers.provider.call({
            to: address,
            data: iface.encodeFunctionData(f, args),
          })
        } catch (e) {
          value = "revert"
        }
        frame[`${address}:${f.name}(${args.join(",")})`] = value
      }
    }
  }
  return frame
}

export function frameDiff(
  before: Record<string, string>,
  after: Record<string, string>,
  allowed: (key: string) => boolean = () => false
): string[] {
  const keys = Object.keys(before).concat(
    Object.keys(after).filter((k) => !(k in before))
  )
  return keys
    .filter((k) => before[k] !== after[k] && !allowed(k))
    .map((k) => `${k}: ${before[k]} -> ${after[k]}`)
}

function argument(
  p: ParamType,
  variant: number,
  actor: string,
  peer: string
): unknown {
  if (p.baseType === "array") {
    return variant === 0
      ? []
      : [argument(p.arrayChildren, variant, actor, peer)]
  }
  if (p.baseType === "tuple") {
    return p.components.map((c) => argument(c, variant, actor, peer))
  }
  if (p.type === "address") {
    return [ethers.constants.AddressZero, actor, peer][variant]
  }
  if (p.type === "bool") return variant !== 0
  if (p.type.startsWith("uint")) {
    const bits = Number(p.type.slice(4) || 256)
    return [0, 1, ethers.BigNumber.from(2).pow(bits).sub(1)][variant]
  }
  if (p.type.startsWith("int")) return variant
  if (p.type === "bytes") return ["0x", "0x01", `0x${"11".repeat(65)}`][variant]
  if (p.type.startsWith("bytes")) {
    const n = Number(p.type.slice(5))
    return `0x${["00", "11", "ff"][variant].repeat(n)}`
  }
  if (p.type === "string") return ["", "a", "b"][variant]
  throw new Error(`no probe argument for ${p.type}`)
}

/// Calls every state-changing function of the stack ABI at every address,
/// from `actor`, with three argument sets: all zero; the actor's own address
/// with true/1; `peer` with true/max. Returns each call that was not refused
/// and changed `frame()`.
export async function probeMutations(
  addresses: string[],
  actor: string,
  peer: string,
  frame: () => Promise<Record<string, string>>,
  label: string
): Promise<string[]> {
  const iface = await stackInterface()
  const violations: string[] = []
  let before: Record<string, string> | null = null
  for (const address of addresses) {
    const code = await codeOf(address)
    if (code === "0x") continue
    const dispatch = await dispatchCode(address)
    for (const f of Object.values(iface.functions)) {
      if (f.constant || !hasSelector(dispatch, f)) continue
      const variants = f.inputs.length === 0 ? 1 : 3
      for (let variant = 0; variant < variants; variant++) {
        const args = f.inputs.map((p) => argument(p, variant, actor, peer))
        const data = iface.encodeFunctionData(f, args)
        let gas
        try {
          gas = await ethers.provider.estimateGas({
            from: actor,
            to: address,
            data,
          })
        } catch (e) {
          continue // refused
        }
        if (!before) before = await frame()
        const snapshot = await ethers.provider.send("evm_snapshot", [])
        try {
          await ethers.provider.send("hardhat_impersonateAccount", [actor])
          const signer = await ethers.getSigner(actor)
          await (
            await signer.sendTransaction({
              to: address,
              data,
              gasLimit: gas.mul(2),
            })
          ).wait()
          const diff = frameDiff(before, await frame())
          if (diff.length > 0) {
            violations.push(
              `${label}: ${f.name}(${args
                .map((a) => JSON.stringify(a))
                .join(",")}) at ${address} by ${actor} changed ${diff.join(
                "; "
              )}`
            )
          }
        } catch (e) {
          // reverted when sent: refused
        } finally {
          await ethers.provider.send("evm_revert", [snapshot])
        }
      }
    }
  }
  return violations
}

// ---------------------------------------------------------------------------
// Window probe: runs `probe` after every transaction the builder sends.
// ---------------------------------------------------------------------------
export async function withTransactionHook<T>(
  run: () => Promise<T>,
  afterTransaction: (receipt: any) => Promise<void>
): Promise<T> {
  // hre.network.provider is a lazy adapter around a backwards-compatibility
  // adapter whose request, send and sendAsync all call its inner provider's
  // request: that is the one place every transaction passes.
  await hre.network.provider.request({ method: "eth_chainId" })
  let provider: any = hre.network.provider
  if (provider.provider) provider = provider.provider
  if (provider._provider) provider = provider._provider
  const original = provider.request.bind(provider)
  let busy = false
  provider.request = async (args: { method: string; params?: unknown[] }) => {
    const result = await original(args)
    if (
      !busy &&
      (args.method === "eth_sendTransaction" ||
        args.method === "eth_sendRawTransaction")
    ) {
      busy = true
      try {
        const receipt = await original({
          method: "eth_getTransactionReceipt",
          params: [result],
        })
        await afterTransaction(receipt)
      } finally {
        busy = false
      }
    }
    return result
  }
  try {
    return await run()
  } finally {
    provider.request = original
  }
}

// ---------------------------------------------------------------------------
// Logs.
// ---------------------------------------------------------------------------
export type NamedLog = {
  address: string
  contract: string
  event: string
  args: utils.Result | null
}

/// Every log from `fromBlock` to the current block, named through `named`
/// (address -> [label, contract]).
export async function logsSince(
  fromBlock: number,
  named: Record<string, { label: string; contract: Contract | utils.Interface }>
): Promise<NamedLog[]> {
  const logs = await ethers.provider.getLogs({
    fromBlock,
    toBlock: "latest",
  })
  const lower: typeof named = {}
  Object.entries(named).forEach(([a, v]) => {
    lower[a.toLowerCase()] = v
  })
  return logs.map((log) => {
    const entry = lower[log.address.toLowerCase()]
    if (!entry) {
      return {
        address: log.address,
        contract: "unknown",
        event: log.topics[0],
        args: null,
      }
    }
    const iface =
      entry.contract instanceof utils.Interface
        ? entry.contract
        : entry.contract.interface
    try {
      const parsed = iface.parseLog(log)
      return {
        address: log.address,
        contract: entry.label,
        event: parsed.name,
        args: parsed.args,
      }
    } catch (e) {
      return {
        address: log.address,
        contract: entry.label,
        event: log.topics[0],
        args: null,
      }
    }
  })
}

// ---------------------------------------------------------------------------
// Bytecode reproduction.
// ---------------------------------------------------------------------------
export type Reproduction = {
  links: { library: string; offsets: number[] }[]
  immutables: { name: string; start: number; length: number }[]
  expected: string
}

/// The artifact's deployed bytecode with every link reference substituted by
/// the address `libraries` reports, and the immutable ranges named.
export async function reproduce(
  fqn: string,
  libraries: Record<string, string>
): Promise<Reproduction> {
  const [source, name] = fqn.split(":")
  const info = await hre.artifacts.getBuildInfo(fqn)
  if (!info) throw new Error(`no build info for ${fqn}`)
  const evm = (info.output.contracts[source][name] as any).evm.deployedBytecode
  let hex: string = evm.object.toLowerCase()
  const links: Reproduction["links"] = []
  for (const names of Object.values(
    evm.linkReferences as Record<string, Record<string, any[]>>
  )) {
    for (const [library, positions] of Object.entries(names)) {
      links.push({ library, offsets: positions.map((p) => p.start) })
      const address = libraries[library]
      if (!address) continue
      for (const p of positions) {
        hex =
          hex.slice(0, p.start * 2) +
          address.slice(2).toLowerCase() +
          hex.slice((p.start + p.length) * 2)
      }
    }
  }
  const astNames = new Map<number, string>()
  const walk = (node: any) => {
    if (!node || typeof node !== "object") return
    if (node.nodeType === "VariableDeclaration" && typeof node.id === "number")
      astNames.set(node.id, node.name)
    Object.values(node).forEach((v) =>
      Array.isArray(v) ? v.forEach(walk) : walk(v)
    )
  }
  Object.values(info.output.sources).forEach((s: any) => walk(s.ast))
  const immutables: Reproduction["immutables"] = []
  for (const [id, ranges] of Object.entries(
    (evm.immutableReferences || {}) as Record<string, any[]>
  )) {
    for (const r of ranges) {
      immutables.push({
        name: astNames.get(Number(id)) || `ast${id}`,
        start: r.start,
        length: r.length,
      })
    }
  }
  return { links, immutables, expected: `0x${hex}` }
}

export function maskImmutables(
  code: string,
  immutables: Reproduction["immutables"]
): string {
  let hex = code.toLowerCase().slice(2)
  for (const r of immutables) {
    hex =
      hex.slice(0, r.start * 2) +
      "00".repeat(r.length) +
      hex.slice((r.start + r.length) * 2)
  }
  return `0x${hex}`
}

export function wordAt(code: string, start: number): string {
  return `0x${code.slice(2 + start * 2, 2 + (start + 32) * 2)}`
}
