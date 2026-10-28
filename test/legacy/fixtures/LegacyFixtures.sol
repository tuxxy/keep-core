// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;

// FIXTURES for the legacy-unchanged suite (test/legacy). None of these is a
// production contract. The Bridge, Bank, WalletProposalValidator and proxy
// they sit next to are the real tbtc-v2 contracts built from git at
// 40a11d1dcdcf82d3962e430067cfe3f97be81483 (pre-FROST main).

interface ILegacyBridgeCallbacks {
    function __ecdsaWalletCreatedCallback(
        bytes32 ecdsaWalletID,
        bytes32 publicKeyX,
        bytes32 publicKeyY
    ) external;

    function __ecdsaWalletHeartbeatFailedCallback(
        bytes32 ecdsaWalletID,
        bytes32 publicKeyX,
        bytes32 publicKeyY
    ) external;
}

/// @dev FIXTURE standing in for the ECDSA WalletRegistry, the RandomBeacon
/// and TokenStaking roles the client reads at startup. No DKG, sortition or
/// stake. Wallets reach the Bridge only through its public registry
/// callbacks; this contract never writes Bridge storage.
contract LegacyWalletRegistryFixture {
    address private immutable owner = msg.sender;
    address public bridge;
    address public member;
    bool public creating;

    modifier onlyOwner() {
        require(msg.sender == owner, "Fixture owner");
        _;
    }

    function bind(address _bridge, address _member) external onlyOwner {
        require(bridge == address(0), "Fixture bound");
        bridge = _bridge;
        member = _member;
    }

    // Startup reads (RandomBeacon, WalletRegistry and TokenStaking roles).
    function sortitionPool() external view returns (address) {
        return address(this);
    }

    function staking() external view returns (address) {
        return address(this);
    }

    // The sortition pool role: every operator has member ID 1, and the pool
    // is never locked by a DKG (the upgraded Bridge reads it, C-10).
    function getOperatorID(address) external pure returns (uint32) {
        return 1;
    }

    function isLocked() external pure returns (bool) {
        return false;
    }

    // Calls the pre-FROST Bridge makes into its ECDSA wallet registry.
    function getWalletCreationState() external view returns (uint8) {
        return creating ? 1 : 0;
    }

    function requestNewWallet() external {
        require(msg.sender == bridge && !creating, "Fixture request");
        creating = true;
    }

    function isWalletMember(
        bytes32,
        uint32[] calldata,
        address operator,
        uint256
    ) external view returns (bool) {
        return operator == member;
    }

    function seize(
        uint96,
        uint256,
        address,
        bytes32,
        uint32[] calldata
    ) external {}

    function closeWallet(bytes32) external {}

    // Owner-driven public callbacks.
    function publish(
        bytes32 id,
        bytes32 x,
        bytes32 y
    ) external onlyOwner {
        creating = false;
        ILegacyBridgeCallbacks(bridge).__ecdsaWalletCreatedCallback(id, x, y);
    }

    function heartbeatFailed(
        bytes32 id,
        bytes32 x,
        bytes32 y
    ) external onlyOwner {
        ILegacyBridgeCallbacks(bridge).__ecdsaWalletHeartbeatFailedCallback(
            id,
            x,
            y
        );
    }
}

/// @dev FIXTURE relay. Regtest integer difficulty is zero, so the Bridge's
/// own SPV checks (merkle inclusion, header chain, proof of work against each
/// header's target) still run on the synthetic regtest blocks.
contract LegacyRegtestRelayFixture {
    function getCurrentEpochDifficulty() external pure returns (uint256) {
        return 0;
    }

    function getPrevEpochDifficulty() external pure returns (uint256) {
        return 0;
    }
}
