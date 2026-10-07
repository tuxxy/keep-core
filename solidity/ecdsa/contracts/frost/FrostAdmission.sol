// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;

import "@openzeppelin/contracts/access/Ownable.sol";

interface IFrostAdmission {
    function eligibleStake(address operator) external view returns (uint96);
}

interface IFrostStakeSource {
    function operatorToStakingProvider(address operator)
        external
        view
        returns (address);

    function eligibleStake(address stakingProvider)
        external
        view
        returns (uint96);
}

/// @notice Read-only stake adapter with an independent, default-deny FROST allowlist.
/// @dev Changes apply only when the separate pool is unlocked and refreshed.
contract FrostAdmission is IFrostAdmission, Ownable {
    IFrostStakeSource public immutable source;
    mapping(address => bool) public allowed;
    event PermissionUpdated(address indexed operator, bool allowed);

    constructor(IFrostStakeSource _source) {
        require(address(_source) != address(0), "stake source");
        source = _source;
    }

    function setAllowed(address operator, bool value) external onlyOwner {
        require(operator != address(0), "operator");
        allowed[operator] = value;
        emit PermissionUpdated(operator, value);
    }

    function eligibleStake(address operator) external view returns (uint96) {
        if (!allowed[operator]) return 0;
        address provider = source.operatorToStakingProvider(operator);
        return provider == address(0) ? 0 : source.eligibleStake(provider);
    }
}
