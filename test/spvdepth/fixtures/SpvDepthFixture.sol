// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;

// FIXTURE for the FROST proof-depth tests (tuxxy/keep-core#28). Not a
// production contract. It exposes the real tbtc-v2 BitcoinTx.validateProof
// (compiled from the same tbtc-v2 build as the deployed Bridge), configured
// with exactly the relay and txProofDifficultyFactor the deployed Bridge
// reports, because no Bridge entrypoint validates a proof on its own.

import "contracts/bridge/BitcoinTx.sol";
import "contracts/bridge/BridgeState.sol";
import "contracts/bridge/IRelay.sol";

interface ISpvDepthBridge {
    function contractReferences()
        external
        view
        returns (
            address bank,
            address relay,
            address ecdsaWalletRegistry,
            address reimbursementPool
        );

    function txProofDifficultyFactor() external view returns (uint256);
}

contract SpvDepthFixture {
    BridgeState.Storage internal self;

    constructor(address bridge) {
        (, address relay, , ) = ISpvDepthBridge(bridge).contractReferences();
        self.relay = IRelay(relay);
        self.txProofDifficultyFactor = uint96(
            ISpvDepthBridge(bridge).txProofDifficultyFactor()
        );
    }

    function validateProof(
        BitcoinTx.Info calldata txInfo,
        BitcoinTx.Proof calldata proof
    ) external view returns (bytes32) {
        return BitcoinTx.validateProof(self, txInfo, proof);
    }
}
