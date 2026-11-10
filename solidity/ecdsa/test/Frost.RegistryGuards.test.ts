/* eslint-disable no-await-in-loop, no-restricted-syntax, no-underscore-dangle */
// Mutation batch M10-dkg: the FROST registry's guards, exercised on the real
// FrostWalletRegistry, FrostDkgValidator, FrostAdmission and SortitionPool
// in one in-process hardhat chain.
//
// FIXTURES (labelled, test-only): FrostTestToken, FrostTestStakeSource and
// FrostTestBeacon (contracts/test/FrostTestDependencies.sol), and
// FrostTestWalletOwner (contracts/test/FrostTestWalletOwner.sol), which
// requests wallets and counts the registry's callbacks in place of the
// Bridge. Operator keys are hardhat's local test accounts. The output key is
// secp256k1 G.x, a public constant.
//
// Block-boundary cases send the transaction with a fixed gas limit, so it is
// mined in the named block even when it reverts, and test the other side of
// the boundary from a snapshot.
import { ethers } from "hardhat"
import { expect } from "chai"

import type { Contract, ContractTransaction } from "ethers"
import type { SignerWithAddress } from "@nomiclabs/hardhat-ethers/signers"

const Q = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
const SNOWFALL = "12".repeat(32)
const DIVISOR = 1000
const SEED_TIMEOUT = 5
const RESULT_TIMEOUT = 30
const CHALLENGE = 3
const READINESS = 10
const POOL_LIMIT = 4
const MINED = { gasLimit: 8_000_000 }
const POOL =
  "@keep-network/sortition-pools/contracts/SortitionPool.sol:SortitionPool"

type Stack = {
  registry: Contract
  validator: Contract
  pool: Contract
  admission: Contract
  source: Contract
  beacon: Contract
  owner: Contract
  operators: SignerWithAddress[]
  outsider: SignerWithAddress
  byAddress: Map<string, SignerWithAddress>
}

// A step that must succeed: a revert becomes a named assertion failure.
async function deploy(name: string, ...args: unknown[]): Promise<Contract> {
  try {
    const contract = await (
      await ethers.getContractFactory(name)
    ).deploy(...args)
    await contract.deployed()
    return contract
  } catch (e) {
    return expect.fail(`deploying ${name} reverted: ${String(e).slice(0, 160)}`)
  }
}

async function snapshot(): Promise<string> {
  return ethers.provider.send("evm_snapshot", [])
}

async function restore(id: string): Promise<void> {
  await ethers.provider.send("evm_revert", [id])
}

// The next transaction lands in `block`.
async function mineTo(block: number): Promise<void> {
  const now = await ethers.provider.getBlockNumber()
  if (block - 1 < now) throw new Error(`block ${block} passed (at ${now})`)
  if (block - 1 > now)
    await ethers.provider.send("hardhat_mine", [
      ethers.utils.hexValue(block - 1 - now),
    ])
}

// A transaction that must succeed; returns its block.
async function mined(
  tx: Promise<ContractTransaction>,
  what: string
): Promise<number> {
  try {
    return (await (await tx).wait()).blockNumber
  } catch (e) {
    return expect.fail(`${what} reverted: ${String(e).slice(0, 160)}`)
  }
}

async function stack(
  admitted = 3,
  ownPool = true,
  enable = true
): Promise<Stack> {
  const signers = await ethers.getSigners()
  const token = await deploy("FrostTestToken")
  const pool = await deploy(POOL, token.address, DIVISOR)
  await pool.deactivateChaosnet()
  const validator = await deploy("FrostDkgValidator", 3, 2, 2, 0, 0)
  const source = await deploy("FrostTestStakeSource")
  const admission = await deploy("FrostAdmission", source.address)
  const beacon = await deploy("FrostTestBeacon")
  const owner = await deploy("FrostTestWalletOwner")
  const registry = await deploy(
    "FrostWalletRegistry",
    pool.address,
    validator.address,
    admission.address,
    beacon.address,
    owner.address,
    [SEED_TIMEOUT, RESULT_TIMEOUT, CHALLENGE, READINESS],
    POOL_LIMIT
  )
  if (ownPool) await pool.transferOwnership(registry.address)
  if (enable)
    await mined(registry.setRequestsEnabled(true), "enabling requests")
  const operators = signers.slice(1, 6)
  for (const o of operators.slice(0, admitted)) {
    await admission.setAllowed(o.address, true)
    await source.set(o.address, DIVISOR)
    await mined(
      registry.refreshOperator(o.address),
      "admitting an operator at the divisor"
    )
  }
  return {
    registry,
    validator,
    pool,
    admission,
    source,
    beacon,
    owner,
    operators,
    outsider: signers[10],
    byAddress: new Map(signers.map((s) => [s.address, s])),
  }
}

// Requests a wallet; returns the epoch (request block).
async function request(s: Stack): Promise<number> {
  return mined(s.owner.request(s.registry.address), "requestNewWallet")
}

// Delivers the beacon seed; returns the fulfil block.
async function seed(s: Stack): Promise<number> {
  return mined(s.beacon.fulfill(7), "seed delivery")
}

type DkgResult = {
  submitterMemberIndex: number
  groupPubKey: string
  misbehavedMembersIndices: number[]
  signatures: string
  signingMembersIndices: number[]
  members: number[]
  membersHash: string
}
type Signed = { result: DkgResult; operators: string[]; epoch: number }

// The local test account for an operator address.
function account(s: Stack, address: string): SignerWithAddress {
  const signer = s.byAddress.get(address)
  if (signer === undefined) return expect.fail(`no test account ${address}`)
  return signer
}

async function signedResult(
  s: Stack,
  delta: Partial<DkgResult> = {}
): Promise<Signed> {
  const epoch = (await s.registry.epoch()).toNumber()
  const [raw, operators] = await s.registry.selection(epoch)
  const members = raw.map(Number)
  const result: DkgResult = {
    submitterMemberIndex: 1,
    groupPubKey: `0x01${Q}${SNOWFALL}`,
    misbehavedMembersIndices: [],
    signatures: "0x",
    signingMembersIndices: [1, 2, 3],
    members,
    membersHash: ethers.utils.keccak256(
      ethers.utils.defaultAbiCoder.encode(["uint32[]"], [members])
    ),
    ...delta,
  }
  const digest = await s.validator.resultDigest(
    s.registry.address,
    s.pool.address,
    epoch,
    result
  )
  if (delta.signatures === undefined)
    result.signatures = ethers.utils.hexConcat(
      await Promise.all(
        operators.map((o: string) =>
          account(s, o).signMessage(ethers.utils.arrayify(digest))
        )
      )
    )
  return { result, operators, epoch }
}

function submitter(s: Stack, signed: Signed, index: number) {
  return s.registry.connect(account(s, signed.operators[index - 1]))
}

// Submits a signed result; returns the submission block.
async function submit(s: Stack, signed: Signed): Promise<number> {
  return mined(
    submitter(s, signed, signed.result.submitterMemberIndex).submitDkgResult(
      signed.result
    ),
    "submitDkgResult"
  )
}

type Approved = {
  id: string
  descriptorHash: string
  epoch: number
  operators: string[]
  approvedAt: number
}

async function approve(s: Stack, snowfall = SNOWFALL): Promise<Approved> {
  await request(s)
  await seed(s)
  const signed = await signedResult(s, {
    groupPubKey: `0x01${Q}${snowfall}`,
  })
  const submittedAt = await submit(s, signed)
  await mineTo(submittedAt + CHALLENGE)
  const approvedAt = await mined(
    s.registry.approveDkgResult(),
    "approveDkgResult at the challenge end"
  )
  const id = await s.registry.approvedWallet(signed.epoch)
  const w = await s.registry.wallet(id)
  return {
    id,
    descriptorHash: w.descriptorHash,
    epoch: signed.epoch,
    operators: signed.operators,
    approvedAt,
  }
}

async function readiness(
  s: Stack,
  a: Approved,
  seats: number[],
  signerOf: (seat: number) => SignerWithAddress = (seat) =>
    account(s, a.operators[seat - 1])
): Promise<[string, number[], string[], string]> {
  const references = seats.map((seat) => ethers.utils.id(`reference ${seat}`))
  const signatures: string[] = []
  for (let i = 0; i < seats.length; i++) {
    const digest = await s.validator.readinessDigest(
      a.id,
      a.descriptorHash,
      s.registry.address,
      a.epoch,
      1,
      1,
      seats[i],
      references[i]
    )
    signatures.push(
      await signerOf(seats[i]).signMessage(ethers.utils.arrayify(digest))
    )
  }
  return [a.id, seats, references, ethers.utils.hexConcat(signatures)]
}

describe("FROST registry guards (mutation batch M10-dkg)", () => {
  // Invariant: the validator policy accepts n up to 100 and t up to n, both
  // bounds inclusive. Rows FrostDkgValidator.sol:38 (n <= 100, t <= n).
  it("validator policy accepts 100 seats and a threshold equal to the group", async () => {
    const wide = await deploy("FrostDkgValidator", 100, 51, 51, 0, 0)
    expect(await wide.groupSize()).to.equal(100)
    const unanimous = await deploy("FrostDkgValidator", 3, 3, 3, 0, 0)
    expect(await unanimous.threshold()).to.equal(3)
  })

  // Invariant: a result's submitter index names one of the n seats, the last
  // one included. Row FrostDkgValidator.sol:91.
  it("validator accepts the last seat as submitter and no seat past it", async () => {
    const [registry, pool, a, b, c] = await ethers.getSigners()
    const validator = await deploy("FrostDkgValidator", 3, 2, 2, 0, 0)
    const members = [1, 2, 3]
    const operators = [a, b, c]
    const result = {
      submitterMemberIndex: 3,
      groupPubKey: `0x01${Q}${SNOWFALL}`,
      misbehavedMembersIndices: [],
      signatures: "0x",
      signingMembersIndices: [1, 2, 3],
      members,
      membersHash: ethers.utils.keccak256(
        ethers.utils.defaultAbiCoder.encode(["uint32[]"], [members])
      ),
    }
    const digest = await validator.resultDigest(
      registry.address,
      pool.address,
      77,
      result
    )
    result.signatures = ethers.utils.hexConcat(
      await Promise.all(
        operators.map((o) => o.signMessage(ethers.utils.arrayify(digest)))
      )
    )
    const valid = (r: typeof result) =>
      validator.validate(
        registry.address,
        pool.address,
        77,
        members,
        operators.map((o) => o.address),
        r
      )
    expect(await valid(result)).to.equal(true)
    expect(await valid({ ...result, submitterMemberIndex: 4 })).to.equal(false)
    expect(await valid({ ...result, submitterMemberIndex: 0 })).to.equal(false)
  })

  // Invariant: the registry is built only with every reference set and
  // every period and the pool bound non-zero, with the result timeout longer
  // than the challenge period. Rows FrostWalletRegistry.sol:112, :120-125.
  it("registry constructor refuses each missing reference or period", async () => {
    const [x] = await ethers.getSigners()
    const ok = x.address
    const zero = ethers.constants.AddressZero
    const factory = await ethers.getContractFactory("FrostWalletRegistry")
    const refs = [ok, ok, ok, ok, ok]
    for (let i = 0; i < refs.length; i++) {
      const changed = [...refs]
      changed[i] = zero
      await expect(factory.deploy(...changed, [5, 30, 3, 10], 4)).to.be.reverted
    }
    for (const [periods, limit] of [
      [[0, 30, 3, 10], 4],
      [[5, 3, 3, 10], 4],
      [[5, 30, 0, 10], 4],
      [[5, 30, 3, 0], 4],
      [[5, 30, 3, 10], 0],
    ] as [number[], number][])
      await expect(factory.deploy(...refs, periods, limit)).to.be.reverted
    const minimal = await deploy(
      "FrostWalletRegistry",
      ...refs,
      [1, 2, 1, 1],
      1
    )
    expect(await minimal.challengePeriod()).to.equal(1)
  })

  // Invariant: the admission adapter needs a stake source, and only its
  // owner may allow a non-zero operator. Rows FrostAdmission.sol:30, :34, :35.
  it("admission adapter refuses a zero source, a stranger and a zero operator", async () => {
    const [, other, operator] = await ethers.getSigners()
    const factory = await ethers.getContractFactory("FrostAdmission")
    await expect(factory.deploy(ethers.constants.AddressZero)).to.be.reverted
    const source = await deploy("FrostTestStakeSource")
    await source.set(operator.address, 5)
    const admission = await factory.deploy(source.address)
    await expect(admission.connect(other).setAllowed(operator.address, true)).to
      .be.reverted
    await expect(admission.setAllowed(ethers.constants.AddressZero, true)).to.be
      .reverted
    expect(await admission.eligibleStake(operator.address)).to.equal(0)
    await mined(admission.setAllowed(operator.address, true), "setAllowed")
    expect(await admission.eligibleStake(operator.address)).to.equal(5)
  })

  // Invariant: only the owner toggles requests, and requests are enabled
  // only while the registry owns its pool. Rows FrostWalletRegistry.sol:140,
  // :141.
  it("requests are toggled only by the owner and only with the pool owned", async () => {
    const unowned = await stack(0, false, false)
    await expect(unowned.registry.setRequestsEnabled(true)).to.be.reverted
    await mined(unowned.registry.setRequestsEnabled(false), "disabling")
    const s = await stack(0)
    await expect(s.registry.connect(s.outsider).setRequestsEnabled(false)).to.be
      .reverted
    expect(await s.registry.requestsEnabled()).to.equal(true)
  })

  // Invariant: an operator is admitted only with eligible stake of at least
  // the pool divisor (inclusive), up to the pool bound. Rows
  // FrostWalletRegistry.sol:153, :154, :156.
  it("admits operators at exactly the divisor, up to the pool bound", async () => {
    const s = await stack(0)
    const [first, second, third, fourth, fifth] = s.operators
    await expect(s.registry.refreshOperator(first.address)).to.be.reverted
    await s.admission.setAllowed(first.address, true)
    await s.source.set(first.address, DIVISOR - 1)
    await expect(s.registry.refreshOperator(first.address)).to.be.reverted
    for (const o of [first, second, third, fourth]) {
      await s.admission.setAllowed(o.address, true)
      await s.source.set(o.address, DIVISOR)
      await mined(
        s.registry.refreshOperator(o.address),
        "admission at the divisor"
      )
      expect(await s.pool.isOperatorInPool(o.address)).to.equal(true)
    }
    await s.admission.setAllowed(fifth.address, true)
    await s.source.set(fifth.address, DIVISOR)
    await expect(s.registry.refreshOperator(fifth.address)).to.be.reverted
  })

  describe("with three admitted operators", () => {
    let s: Stack
    let snap: string
    before(async () => {
      s = await stack()
    })
    beforeEach(async () => {
      snap = await snapshot()
    })
    afterEach(async () => {
      await restore(snap)
    })

    // Invariant: a known operator that left the pool re-enters at exactly
    // the divisor, and no operator is refreshed while a DKG holds the pool.
    // Rows FrostWalletRegistry.sol:168 (>=), :150.
    it("re-enters a known operator at the divisor, never during a DKG", async () => {
      const fourth = s.operators[3]
      await s.admission.setAllowed(fourth.address, true)
      await s.source.set(fourth.address, DIVISOR)
      await mined(s.registry.refreshOperator(fourth.address), "admission")
      await s.source.set(fourth.address, 0)
      await mined(s.registry.refreshOperator(fourth.address), "removal")
      expect(await s.pool.isOperatorInPool(fourth.address)).to.equal(false)
      await s.source.set(fourth.address, DIVISOR)
      await mined(s.registry.refreshOperator(fourth.address), "re-entry")
      expect(await s.pool.isOperatorInPool(fourth.address)).to.equal(true)
      await s.source.set(fourth.address, 0)
      await mined(s.registry.refreshOperator(fourth.address), "removal")
      await request(s)
      await expect(s.registry.refreshOperator(fourth.address)).to.be.reverted
    })

    // Invariant: only the wallet owner requests a wallet, only while
    // requests are enabled. Rows FrostWalletRegistry.sol:175, :183.
    it("only the wallet owner requests, only while enabled", async () => {
      await expect(s.registry.connect(s.outsider).requestNewWallet()).to.be
        .reverted
      await s.registry.setRequestsEnabled(false)
      await expect(s.owner.request(s.registry.address)).to.be.reverted
      await mined(s.registry.setRequestsEnabled(true), "enabling requests")
      const epoch = await request(s)
      expect(await s.registry.epoch()).to.equal(epoch)
      expect(await s.registry.state()).to.equal(1)
    })

    // Invariant: only the beacon delivers the seed, strictly before
    // epoch + seedTimeout. Rows FrostWalletRegistry.sol:195, :198.
    it("takes the seed only from the beacon and before the seed timeout", async () => {
      const epoch = await request(s)
      await expect(s.registry.connect(s.outsider).__beaconCallback(1, 0)).to.be
        .reverted
      const before = await snapshot()
      await mineTo(epoch + SEED_TIMEOUT)
      await expect(s.beacon.fulfill(7, MINED)).to.be.reverted
      await restore(before)
      await mineTo(epoch + SEED_TIMEOUT - 1)
      await seed(s)
      expect(await s.registry.state()).to.equal(2)
    })

    // Invariant: a result is submitted by its named member, once, at least
    // a challenge period before the deadline, within the storage bounds
    // (each bound inclusive). Rows FrostWalletRegistry.sol:227, :229, :233,
    // :235, :241, :243, :245, :246, :247.
    it("bounds result submission", async () => {
      await request(s)
      const seeded = await seed(s)
      const signed = await signedResult(s)
      await expect(
        s.registry.connect(s.outsider).submitDkgResult(signed.result)
      ).to.be.reverted
      const short = await signedResult(s, {
        groupPubKey: `0x01${Q}${SNOWFALL.slice(2)}`,
      })
      await expect(submitter(s, short, 1).submitDkgResult(short.result)).to.be
        .reverted
      const late = await snapshot()
      await mineTo(seeded + RESULT_TIMEOUT - CHALLENGE)
      await expect(
        submitter(s, signed, 1).submitDkgResult(signed.result, MINED)
      ).to.be.reverted
      await restore(late)
      const wide = await snapshot()
      const misbehaved = await signedResult(s, {
        misbehavedMembersIndices: [1, 2, 3],
      })
      await submit(s, misbehaved)
      expect(await s.registry.state()).to.equal(3)
      await restore(wide)
      const last = await signedResult(s, { submitterMemberIndex: 3 })
      await mineTo(seeded + RESULT_TIMEOUT - CHALLENGE - 1)
      await submit(s, last)
      expect(await s.registry.state()).to.equal(3)
      await expect(submitter(s, signed, 1).submitDkgResult(signed.result)).to.be
        .reverted
    })

    // Invariant: only an invalid result is challenged, only strictly within
    // its challenge period. Rows FrostWalletRegistry.sol:283, :285, :289.
    it("challenges only an invalid result within its period", async () => {
      await request(s)
      await seed(s)
      const valid = await signedResult(s)
      await submit(s, valid)
      await expect(s.registry.challengeDkgResult()).to.be.reverted
      const s2 = await stack()
      await request(s2)
      await seed(s2)
      const invalid = await signedResult(s2, {
        signingMembersIndices: [1, 1, 3],
      })
      const at = await submit(s2, invalid)
      const late = await snapshot()
      await mineTo(at + CHALLENGE)
      await expect(s2.registry.challengeDkgResult(MINED)).to.be.reverted
      await restore(late)
      await mineTo(at + CHALLENGE - 1)
      await mined(s2.registry.challengeDkgResult(), "challenge in period")
      expect(await s2.registry.state()).to.equal(2)
    })

    // Invariant: a valid result is approved from submittedAt + challenge
    // (inclusive) to the result deadline (exclusive); an invalid result or a
    // reused output key is never approved. Rows FrostWalletRegistry.sol:307,
    // :309, :310, :318, :336.
    it("approves only a valid, fresh result inside its window", async () => {
      await request(s)
      const seeded = await seed(s)
      const signed = await signedResult(s)
      const at = await submit(s, signed)
      await expect(s.registry.approveDkgResult()).to.be.reverted
      const deadline = await snapshot()
      await mineTo(seeded + RESULT_TIMEOUT)
      await expect(s.registry.approveDkgResult(MINED)).to.be.reverted
      await restore(deadline)
      await mineTo(at + CHALLENGE)
      await mined(s.registry.approveDkgResult(), "approval at challenge end")
      expect(await s.owner.created()).to.equal(1)
      // A second DKG with the same output key is refused at approval.
      await request(s)
      await seed(s)
      const reused = await signedResult(s, {
        groupPubKey: `0x01${Q}${"34".repeat(32)}`,
      })
      const reusedAt = await submit(s, reused)
      await mineTo(reusedAt + CHALLENGE)
      await expect(s.registry.approveDkgResult()).to.be.reverted
    })

    // Invariant: a submitted result that the validator refuses is never
    // approved, even after its challenge period. Row
    // FrostWalletRegistry.sol:318.
    it("never approves an invalid result", async () => {
      await request(s)
      await seed(s)
      const invalid = await signedResult(s, {
        signingMembersIndices: [1, 1, 3],
      })
      const at = await submit(s, invalid)
      await mineTo(at + CHALLENGE)
      await expect(s.registry.approveDkgResult()).to.be.reverted
    })

    // Invariant: readiness is accepted once, for a pending wallet before its
    // deadline, with between readySeats and groupSize strictly increasing
    // seats of the descriptor, each signed by its own operator. Rows
    // FrostWalletRegistry.sol:401, :402, :405, :406, :407, :413, :415, :416,
    // :417, :427.
    it("accepts readiness only as the certificate rules allow", async () => {
      const a = await approve(s)
      const submitReady = (
        args: [string, number[], string[], string],
        overrides = {}
      ) => s.registry.submitReadinessV1(...args, overrides)
      await expect(submitReady(await readiness(s, a, [2]))).to.be.reverted
      await expect(submitReady(await readiness(s, a, [1, 1]))).to.be.reverted
      await expect(submitReady(await readiness(s, a, [2, 1]))).to.be.reverted
      await expect(submitReady(await readiness(s, a, [1, 2], () => s.outsider)))
        .to.be.reverted
      const late = await snapshot()
      await mineTo(a.approvedAt + READINESS)
      await expect(submitReady(await readiness(s, a, [1, 2]), MINED)).to.be
        .reverted
      await restore(late)
      const all = await snapshot()
      await mined(
        submitReady(await readiness(s, a, [1, 2, 3])),
        "readiness of every seat"
      )
      expect(await s.owner.ready()).to.equal(1)
      await restore(all)
      await mineTo(a.approvedAt + READINESS - 1)
      await mined(
        submitReady(await readiness(s, a, [2, 3])),
        "readiness of readySeats seats before the deadline"
      )
      expect((await s.registry.wallet(a.id)).state).to.equal(2)
      await expect(submitReady(await readiness(s, a, [1, 2]))).to.be.reverted
    })

    // Invariant: a pending wallet expires at its deadline (inclusive), not
    // before. Rows FrostWalletRegistry.sol:459, :460.
    it("expires a pending wallet from its deadline", async () => {
      const a = await approve(s)
      await expect(s.registry.expireWallet(a.id)).to.be.reverted
      await mineTo(a.approvedAt + READINESS)
      await mined(s.registry.expireWallet(a.id), "expiry at the deadline")
      expect(await s.owner.expired()).to.equal(1)
    })

    // Invariant: a DKG expires only from AwaitingSeed at epoch + seedTimeout
    // or from AwaitingResult/Challenge at the result deadline (inclusive);
    // never while Idle. Rows FrostWalletRegistry.sol:469, :471, :474.
    it("expires a DKG only at its timeouts", async () => {
      await expect(s.registry.expireDkg()).to.be.reverted
      const epoch = await request(s)
      const seedSide = await snapshot()
      await mineTo(epoch + SEED_TIMEOUT - 1)
      await expect(s.registry.expireDkg(MINED)).to.be.reverted
      await restore(seedSide)
      await mineTo(epoch + SEED_TIMEOUT)
      await mined(s.registry.expireDkg(), "seed timeout expiry")
      expect(await s.registry.state()).to.equal(0)
      await request(s)
      const seeded = await seed(s)
      const resultSide = await snapshot()
      await mineTo(seeded + RESULT_TIMEOUT - 1)
      await expect(s.registry.expireDkg(MINED)).to.be.reverted
      await restore(resultSide)
      await mineTo(seeded + RESULT_TIMEOUT)
      await mined(s.registry.expireDkg(), "result deadline expiry")
      expect(await s.registry.state()).to.equal(0)
    })
  })

  // Invariant: no second wallet is requested while a DKG is in progress.
  // An empty pool keeps the pool refresh out of the way. Row
  // FrostWalletRegistry.sol:179.
  it("refuses a second request during a DKG", async () => {
    const s = await stack(0)
    await request(s)
    await expect(s.owner.request(s.registry.address)).to.be.reverted
  })
})
