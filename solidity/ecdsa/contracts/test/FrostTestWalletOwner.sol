// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;

import "../frost/FrostTypes.sol";

interface IFrostWalletRequests {
    function requestNewWallet() external;
}

// Deterministic local test input only: a wallet owner that requests wallets
// and counts the registry's callbacks. It replaces no Bridge logic under test.
contract FrostTestWalletOwner is IFrostWalletOwner {
    uint256 public created;
    uint256 public ready;
    uint256 public expired;

    function request(IFrostWalletRequests registry) external {
        registry.requestNewWallet();
    }

    function __snowfallWalletCreated(bytes calldata, uint64) external {
        created++;
    }

    function __snowfallWalletReady(
        bytes32,
        bytes32,
        uint32,
        uint32
    ) external {
        ready++;
    }

    function __snowfallWalletExpired(bytes32) external {
        expired++;
    }
}
