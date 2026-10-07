/* eslint-disable no-await-in-loop, no-restricted-syntax */
// Contract transitions, deployment links and upgrade snapshots are sequential.
import { ethers } from "hardhat"
import { expect } from "chai"

const Q = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
const SNOWFALL = "12".repeat(32)
async function vector(repeated = false) {
  const [registry, pool, a, b, c] = await ethers.getSigners()
  const validator = await (
    await ethers.getContractFactory("FrostDkgValidator")
  ).deploy(3, 2, 2, 0, 0)
  const members = repeated ? [1, 1, 2] : [1, 2, 3]
  const operators = repeated ? [a, a, b] : [a, b, c]
  const result = {
    submitterMemberIndex: 1,
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
  const valid = (
    r = result,
    epoch = 77,
    reg = registry.address,
    sortition = pool.address
  ) =>
    validator.validate(
      reg,
      sortition,
      epoch,
      members,
      operators.map((o) => o.address),
      r
    )
  return {
    validator,
    result,
    digest,
    valid,
    registry,
    pool,
    operators,
    members,
  }
}
describe("FROST registry verifier boundary", () => {
  it("pins profile, payload, original seat order, and exact result digest bytes", async () => {
    const v = await vector()
    expect(await v.validator.PROFILE()).to.equal(1)
    expect(await v.valid()).to.equal(true)
    const chain = (await ethers.provider.getNetwork()).chainId
    expect(v.digest).to.equal(
      ethers.utils.keccak256(
        ethers.utils.defaultAbiCoder.encode(
          [
            "bytes32",
            "uint256",
            "address",
            "address",
            "uint64",
            "bytes",
            "uint8[]",
            "uint32[]",
            "uint16",
          ],
          [
            ethers.utils.id("tbtc-v2/frost-result/v1"),
            chain,
            v.registry.address,
            v.pool.address,
            77,
            v.result.groupPubKey,
            [],
            v.members,
            2,
          ]
        )
      )
    )
    expect(await v.valid(v.result, 78)).to.equal(false)
    expect(await v.valid(v.result, 77, v.pool.address)).to.equal(false)
    expect(
      await v.valid(v.result, 77, v.registry.address, v.registry.address)
    ).to.equal(false)
    for (const delta of [
      { groupPubKey: `0x02${Q}${SNOWFALL}` },
      { groupPubKey: `0x01${"ff".repeat(32)}${SNOWFALL}` },
      { groupPubKey: `0x01${"00".repeat(32)}${SNOWFALL}` },
      { groupPubKey: `0x01${Q}${"00".repeat(32)}` },
      { members: [2, 1, 3] },
      { signingMembersIndices: [1, 1, 3] },
      { signingMembersIndices: [1, 2] },
      { signatures: "0x" },
      { misbehavedMembersIndices: [1] },
    ])
      expect(await v.valid({ ...v.result, ...delta })).to.equal(false)
  })
  it("counts the original seats when an operator owns several seats", async () => {
    const v = await vector(true)
    expect(await v.valid()).to.equal(true)
    expect(
      await v.valid({ ...v.result, signingMembersIndices: [1, 2] })
    ).to.equal(false)
  })
  it("rejects unsupported majority and availability policies", async () => {
    const factory = await ethers.getContractFactory("FrostDkgValidator")
    for (const args of [
      [0, 0, 0, 0, 0],
      [101, 51, 51, 0, 0],
      [3, 1, 2, 0, 0],
      [3, 2, 2, 1, 0],
      [3, 2, 3, 1, 1],
      [3, 2, 4, 0, 0],
    ])
      await expect(factory.deploy(...args)).to.be.reverted
    const v = await factory.deploy(3, 2, 3, 1, 0)
    expect(await v.readySeats()).to.equal(3)
  })
})
