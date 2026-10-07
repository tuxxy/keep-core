// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;

import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import "./FrostTypes.sol";
import "./FrostCurve.sol";

/// @notice Separate FROST policy and signature domain. No ECDSA DKG constants.
contract FrostDkgValidator {
    uint8 public constant PROFILE = 1;
    bytes32 public constant RESULT_DOMAIN =
        keccak256("tbtc-v2/frost-result/v1");
    bytes32 public constant READY_DOMAIN = keccak256("tbtc-v2/frost-ready/v1");
    uint16 public immutable groupSize;
    uint16 public immutable threshold;
    uint16 public immutable readySeats;
    uint16 public immutable falseReadySeats;
    uint16 public immutable unavailableSeats;

    // This deliberately has its own binding, despite the legacy-shaped tuple.
    struct Result {
        uint256 submitterMemberIndex;
        bytes groupPubKey;
        uint8[] misbehavedMembersIndices;
        bytes signatures;
        uint256[] signingMembersIndices;
        uint32[] members;
        bytes32 membersHash;
    }

    constructor(
        uint16 n,
        uint16 t,
        uint16 r,
        uint16 b,
        uint16 f
    ) {
        require(n > 0 && n <= 100 && t > n / 2 && t <= n, "FROST group policy");
        require(
            r <= n && uint256(r) >= uint256(t) + b + f,
            "FROST readiness policy"
        );
        groupSize = n;
        threshold = t;
        readySeats = r;
        falseReadySeats = b;
        unavailableSeats = f;
    }

    function resultDigest(
        address registry,
        address pool,
        uint64 epoch,
        Result calldata result
    ) public view returns (bytes32) {
        return
            keccak256(
                abi.encode(
                    RESULT_DOMAIN,
                    block.chainid,
                    registry,
                    pool,
                    epoch,
                    result.groupPubKey,
                    result.misbehavedMembersIndices,
                    result.members,
                    threshold
                )
            );
    }

    function validate(
        address registry,
        address pool,
        uint64 epoch,
        uint32[] calldata members,
        address[] calldata operators,
        Result calldata result
    ) external view returns (bool) {
        uint256 n = groupSize;
        if (
            members.length != n ||
            operators.length != n ||
            result.members.length != n ||
            result.signingMembersIndices.length != n ||
            result.signatures.length != n * 65 ||
            result.groupPubKey.length != 65 ||
            uint8(result.groupPubKey[0]) != PROFILE ||
            result.misbehavedMembersIndices.length != 0 ||
            result.submitterMemberIndex == 0 ||
            result.submitterMemberIndex > n
        ) return false;
        if (
            result.membersHash != keccak256(abi.encode(members)) ||
            keccak256(abi.encode(result.members)) != result.membersHash
        ) return false;
        bytes32 outputKey = bytes32(result.groupPubKey[1:33]);
        if (
            !FrostCurve.validX(outputKey) ||
            bytes32(result.groupPubKey[33:65]) == bytes32(0)
        ) return false;
        bytes32 digest = ECDSA.toEthSignedMessageHash(
            resultDigest(registry, pool, epoch, result)
        );
        for (uint256 i = 0; i < n; i++) {
            if (
                members[i] == 0 ||
                operators[i] == address(0) ||
                result.signingMembersIndices[i] != i + 1
            ) return false;
            (address signer, ECDSA.RecoverError error) = ECDSA.tryRecover(
                digest,
                result.signatures[i * 65:(i + 1) * 65]
            );
            if (error != ECDSA.RecoverError.NoError || signer != operators[i])
                return false;
        }
        return true;
    }

    function readinessDigest(
        bytes32 walletId,
        bytes32 descriptor,
        address registry,
        uint64 epoch,
        uint32 generation,
        uint32 capabilityVersion,
        uint16 seat,
        bytes32 referenceHash
    ) public view returns (bytes32) {
        return
            keccak256(
                abi.encode(
                    READY_DOMAIN,
                    block.chainid,
                    registry,
                    PROFILE,
                    walletId,
                    descriptor,
                    epoch,
                    generation,
                    capabilityVersion,
                    seat,
                    referenceHash
                )
            );
    }
}
