/* eslint-disable no-await-in-loop, no-restricted-syntax */
// Contract transitions, deployment links and upgrade snapshots are sequential.
import { ethers } from "hardhat"
import { expect } from "chai"

// State transition, duplicate-seat and deadline witnesses use the actual
// Bridge and registry in test/frost/run-local.sh. These tests pin the ABI domain.
describe("FROST readiness certificate domain", () => {
  it("binds identity, descriptor, registry, epoch, generation, capability, seat and reference", async () => {
    const [registry, other] = await ethers.getSigners()
    const v = await (
      await ethers.getContractFactory("FrostDkgValidator")
    ).deploy(3, 2, 2, 0, 0)
    const args = [
      ethers.utils.id("wallet"),
      ethers.utils.id("descriptor"),
      registry.address,
      77,
      1,
      1,
      2,
      ethers.utils.id("reference"),
    ]
    const digest = await v.readinessDigest(...args)
    expect(digest).to.equal(
      ethers.utils.keccak256(
        ethers.utils.defaultAbiCoder.encode(
          [
            "bytes32",
            "uint256",
            "address",
            "uint8",
            "bytes32",
            "bytes32",
            "uint64",
            "uint32",
            "uint32",
            "uint16",
            "bytes32",
          ],
          [
            ethers.utils.id("tbtc-v2/frost-ready/v1"),
            (await ethers.provider.getNetwork()).chainId,
            registry.address,
            1,
            args[0],
            args[1],
            77,
            1,
            1,
            2,
            args[7],
          ]
        )
      )
    )
    for (const [index, value] of [
      [0, ethers.utils.id("other wallet")],
      [1, ethers.utils.id("other descriptor")],
      [2, other.address],
      [3, 78],
      [4, 2],
      [5, 2],
      [6, 1],
      [7, ethers.utils.id("other reference")],
    ] as any) {
      const changed = [...args]
      changed[index] = value
      expect(await v.readinessDigest(...changed)).not.to.equal(digest)
    }
  })
})
