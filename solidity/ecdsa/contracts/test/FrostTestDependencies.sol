// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;
import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@keep-network/random-beacon/contracts/api/IRandomBeaconConsumer.sol";

// Deterministic local test inputs only. These contracts replace neither the
// FROST registry/validator/pool nor the Bridge being exercised.
contract FrostTestToken is ERC20 {
    constructor() ERC20("Test", "TEST") {}
}

contract FrostTestStakeSource {
    mapping(address => uint96) public stakes;

    function set(address operator, uint96 stake) external {
        stakes[operator] = stake;
    }

    function operatorToStakingProvider(address operator)
        external
        pure
        returns (address)
    {
        return operator;
    }

    function eligibleStake(address operator) external view returns (uint96) {
        return stakes[operator];
    }
}

contract FrostTestBeacon {
    IRandomBeaconConsumer public consumer;

    function requestRelayEntry(IRandomBeaconConsumer callback) external {
        require(msg.sender == address(callback), "callback sender");
        consumer = callback;
    }

    function fulfill(uint256 seed) external {
        IRandomBeaconConsumer callback = consumer;
        delete consumer;
        callback.__beaconCallback(seed, block.number);
    }
}
