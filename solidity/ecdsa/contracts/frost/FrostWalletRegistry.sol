// SPDX-License-Identifier: GPL-3.0-only
pragma solidity 0.8.17;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import "@keep-network/sortition-pools/contracts/SortitionPool.sol";
import "@keep-network/random-beacon/contracts/api/IRandomBeacon.sol";
import "@keep-network/random-beacon/contracts/api/IRandomBeaconConsumer.sol";
import "./FrostAdmission.sol";
import "./FrostDkgValidator.sol";

/// @notice Inactive-by-default FROST registry, independent of the ECDSA pool.
/// @dev All deadlines use Ethereum block numbers. Local policy inputs do not
/// establish a production profile. No reimbursement authority is granted here.
contract FrostWalletRegistry is
    Ownable,
    ReentrancyGuard,
    IRandomBeaconConsumer
{
    enum DkgState {
        Idle,
        AwaitingSeed,
        AwaitingResult,
        Challenge
    }
    enum WalletState {
        Unknown,
        PendingReady,
        ReadyUnfunded,
        Closed
    }
    struct Selection {
        uint32[] members;
        address[] operators;
    }
    struct Wallet {
        FrostTypes.Descriptor descriptor;
        bytes32 resultHash;
        bytes32 descriptorHash;
        uint64 approvalBlock;
        uint64 deadline;
        WalletState state;
    }
    struct EpochSnapshot {
        uint64 blockNumber;
        DkgState state;
        uint64 epoch;
        uint64 submittedAt;
        uint64 resultDeadline;
        bytes32 submittedHash;
        bytes submitted;
        bytes32 approvedId;
        Wallet approved;
    }

    SortitionPool public immutable sortitionPool;
    FrostDkgValidator public immutable validator;
    IFrostAdmission public immutable admission;
    IRandomBeacon public immutable randomBeacon;
    IFrostWalletOwner public immutable walletOwner;
    uint64 public immutable seedTimeout;
    uint64 public immutable resultTimeout;
    uint64 public immutable challengePeriod;
    uint64 public immutable readinessPeriod;
    uint16 public immutable maxPoolOperators;
    bool public requestsEnabled;
    DkgState public state;
    uint64 public epoch;
    uint64 public resultDeadline;
    uint64 public submittedAt;
    bytes32 public submittedHash;
    bytes private submitted;
    address[] private poolOperators;
    mapping(address => bool) private knownOperator;
    mapping(uint64 => Selection) private selections;
    mapping(bytes32 => Wallet) private wallets;
    mapping(bytes32 => bool) public outputKeyUsed;
    mapping(uint64 => bytes32) public approvedWallet;

    event DkgStarted(
        uint64 indexed epoch,
        uint32[] members,
        address[] operators
    );
    event ResultSubmitted(uint64 indexed epoch, bytes32 indexed resultHash);
    event ResultChallenged(uint64 indexed epoch, bytes32 indexed resultHash);
    event ResultApproved(
        uint64 indexed epoch,
        bytes32 indexed resultHash,
        bytes32 indexed walletId,
        bytes32 descriptorHash
    );
    event ReadinessAccepted(
        bytes32 indexed walletId,
        bytes32 descriptorHash,
        uint16[] seats
    );
    event WalletExpired(bytes32 indexed walletId);
    event DkgExpired(uint64 indexed epoch);
    event RequestsEnabled(bool enabled);

    constructor(
        SortitionPool pool,
        FrostDkgValidator policy,
        IFrostAdmission eligibility,
        IRandomBeacon beacon,
        IFrostWalletOwner bridge,
        uint64[4] memory periods,
        uint16 poolLimit
    ) {
        require(
            address(pool) != address(0) &&
                address(policy) != address(0) &&
                address(eligibility) != address(0) &&
                address(beacon) != address(0) &&
                address(bridge) != address(0),
            "FROST references"
        );
        require(
            periods[0] > 0 &&
                periods[1] > periods[2] &&
                periods[2] > 0 &&
                periods[3] > 0 &&
                poolLimit > 0,
            "FROST policy"
        );
        sortitionPool = pool;
        validator = policy;
        admission = eligibility;
        randomBeacon = beacon;
        walletOwner = bridge;
        seedTimeout = periods[0];
        resultTimeout = periods[1];
        challengePeriod = periods[2];
        readinessPeriod = periods[3];
        maxPoolOperators = poolLimit;
    }

    function setRequestsEnabled(bool enabled) external onlyOwner {
        require(
            !enabled || sortitionPool.owner() == address(this),
            "FROST pool owner"
        );
        requestsEnabled = enabled;
        emit RequestsEnabled(enabled);
    }

    function refreshOperator(address operator) external {
        require(state == DkgState.Idle, "FROST pool locked");
        uint96 stake = admission.eligibleStake(operator);
        if (!knownOperator[operator]) {
            require(
                stake >= sortitionPool.poolWeightDivisor() &&
                    stake > 0 &&
                    poolOperators.length < maxPoolOperators,
                "FROST admission"
            );
            knownOperator[operator] = true;
            poolOperators.push(operator);
        }
        refresh(operator, stake);
    }

    function refresh(address operator, uint96 stake) private {
        if (sortitionPool.isOperatorInPool(operator))
            sortitionPool.updateOperatorStatus(operator, stake);
        else if (stake >= sortitionPool.poolWeightDivisor() && stake > 0)
            sortitionPool.insertOperator(operator, stake);
    }

    // Refresh every admitted operator before locking. No stale admission weight
    // can enter this selection. The configured pool bound must fit deployment gas.
    function requestNewWallet() external nonReentrant {
        require(
            msg.sender == address(walletOwner) && requestsEnabled,
            "FROST request denied"
        );
        require(
            state == DkgState.Idle && block.number > epoch,
            "FROST DKG active"
        );
        for (uint256 i = 0; i < poolOperators.length; i++)
            refresh(
                poolOperators[i],
                admission.eligibleStake(poolOperators[i])
            );
        sortitionPool.lock();
        epoch = uint64(block.number);
        state = DkgState.AwaitingSeed;
        randomBeacon.requestRelayEntry(this);
    }

    function __beaconCallback(uint256 seed, uint256) external override {
        require(
            msg.sender == address(randomBeacon) &&
                state == DkgState.AwaitingSeed &&
                block.number < uint256(epoch) + seedTimeout,
            "FROST seed denied"
        );
        Selection storage selection = selections[epoch];
        selection.members = sortitionPool.selectGroup(
            validator.groupSize(),
            bytes32(seed)
        );
        selection.operators = sortitionPool.getIDOperators(selection.members);
        resultDeadline = uint64(block.number) + resultTimeout;
        state = DkgState.AwaitingResult;
        emit DkgStarted(epoch, selection.members, selection.operators);
    }

    function selection(uint64 startBlock)
        external
        view
        returns (uint32[] memory, address[] memory)
    {
        return (
            selections[startBlock].members,
            selections[startBlock].operators
        );
    }

    function submitDkgResult(FrostDkgValidator.Result calldata result)
        external
        nonReentrant
    {
        require(
            state == DkgState.AwaitingResult &&
                block.number + challengePeriod < resultDeadline,
            "FROST submission closed"
        );
        Selection storage selected = selections[epoch];
        require(
            result.submitterMemberIndex > 0 &&
                result.submitterMemberIndex <= selected.operators.length &&
                selected.operators[result.submitterMemberIndex - 1] ==
                msg.sender,
            "FROST submitter"
        );
        // Full validation remains publicly challengeable. Bound storage cost.
        require(
            result.groupPubKey.length == 65 &&
                result.signatures.length <=
                uint256(validator.groupSize()) * 65 &&
                result.members.length <= validator.groupSize() &&
                result.signingMembersIndices.length <= validator.groupSize() &&
                result.misbehavedMembersIndices.length <= validator.groupSize(),
            "FROST result bounds"
        );
        submitted = abi.encode(result);
        submittedHash = keccak256(submitted);
        submittedAt = uint64(block.number);
        state = DkgState.Challenge;
        emit ResultSubmitted(epoch, submittedHash);
    }

    function submittedResult()
        external
        view
        returns (FrostDkgValidator.Result memory)
    {
        require(state == DkgState.Challenge, "FROST no submission");
        return abi.decode(submitted, (FrostDkgValidator.Result));
    }

    function isResultValid(
        FrostDkgValidator.Result calldata result,
        uint64 startBlock
    ) external view returns (bool) {
        Selection storage selected = selections[startBlock];
        return
            validator.validate(
                address(this),
                address(sortitionPool),
                startBlock,
                selected.members,
                selected.operators,
                result
            );
    }

    function challengeDkgResult() external nonReentrant {
        require(
            state == DkgState.Challenge &&
                block.number < uint256(submittedAt) + challengePeriod,
            "FROST challenge closed"
        );
        Selection storage selected = selections[epoch];
        require(
            !validator.validate(
                address(this),
                address(sortitionPool),
                epoch,
                selected.members,
                selected.operators,
                abi.decode(submitted, (FrostDkgValidator.Result))
            ),
            "FROST valid result"
        );
        emit ResultChallenged(epoch, submittedHash);
        delete submitted;
        delete submittedHash;
        state = DkgState.AwaitingResult;
    }

    function approveDkgResult() external nonReentrant {
        require(
            state == DkgState.Challenge &&
                block.number >= uint256(submittedAt) + challengePeriod &&
                block.number < resultDeadline,
            "FROST approval closed"
        );
        Selection storage selected = selections[epoch];
        FrostDkgValidator.Result memory result = abi.decode(
            submitted,
            (FrostDkgValidator.Result)
        );
        require(
            validator.validate(
                address(this),
                address(sortitionPool),
                epoch,
                selected.members,
                selected.operators,
                result
            ),
            "FROST invalid result"
        );
        bytes memory payload = result.groupPubKey;
        bytes32 outputKey;
        bytes32 snowfallDescriptor;
        assembly {
            outputKey := mload(add(payload, 33))
            snowfallDescriptor := mload(add(payload, 65))
        }
        require(!outputKeyUsed[outputKey], "FROST duplicate key");
        bytes32 id = FrostTypes.walletId(outputKey);
        Wallet storage wallet = wallets[id];
        wallet.descriptor = FrostTypes.Descriptor(
            FrostTypes.SCHEME,
            FrostTypes.PROFILE,
            block.chainid,
            address(this),
            epoch,
            selected.members,
            selected.operators,
            validator.threshold(),
            snowfallDescriptor,
            outputKey
        );
        wallet.descriptorHash = FrostTypes.descriptorHash(wallet.descriptor);
        wallet.resultHash = submittedHash;
        wallet.approvalBlock = uint64(block.number);
        wallet.deadline = uint64(block.number) + readinessPeriod;
        wallet.state = WalletState.PendingReady;
        outputKeyUsed[outputKey] = true;
        approvedWallet[epoch] = id;
        emit ResultApproved(epoch, submittedHash, id, wallet.descriptorHash);
        state = DkgState.Idle;
        delete submitted;
        delete submittedHash;
        sortitionPool.unlock();
        walletOwner.__snowfallWalletCreated(
            abi.encode(wallet.descriptor),
            wallet.deadline
        );
    }

    /// @notice Read related state atomically while submissions are approved.
    function epochView(uint64 startBlock)
        external
        view
        returns (EpochSnapshot memory)
    {
        bytes32 id = approvedWallet[startBlock];
        return
            EpochSnapshot(
                uint64(block.number),
                state,
                epoch,
                submittedAt,
                resultDeadline,
                submittedHash,
                submitted,
                id,
                wallets[id]
            );
    }

    function wallet(bytes32 id) external view returns (Wallet memory) {
        return wallets[id];
    }

    function submitReadinessV1(
        bytes32 id,
        uint16[] calldata seats,
        bytes32[] calldata references,
        bytes calldata signatures
    ) external nonReentrant {
        Wallet storage w = wallets[id];
        require(
            w.state == WalletState.PendingReady && block.number < w.deadline,
            "FROST readiness closed"
        );
        require(
            seats.length >= validator.readySeats() &&
                seats.length <= validator.groupSize() &&
                references.length == seats.length &&
                signatures.length == seats.length * 65,
            "FROST readiness count"
        );
        uint16 previous;
        for (uint256 i = 0; i < seats.length; i++) {
            uint16 seat = seats[i];
            require(
                seat > previous &&
                    seat <= w.descriptor.operators.length &&
                    references[i] != bytes32(0),
                "FROST readiness seat"
            );
            previous = seat;
            bytes32 digest = readinessDigest(w, id, seat, references[i]);
            (address signer, ECDSA.RecoverError error) = ECDSA.tryRecover(
                ECDSA.toEthSignedMessageHash(digest),
                signatures[i * 65:(i + 1) * 65]
            );
            require(
                error == ECDSA.RecoverError.NoError &&
                    signer == w.descriptor.operators[seat - 1],
                "FROST readiness signer"
            );
        }
        w.state = WalletState.ReadyUnfunded;
        emit ReadinessAccepted(id, w.descriptorHash, seats);
        walletOwner.__snowfallWalletReady(id, w.descriptorHash, 1, 1);
    }

    function readinessDigest(
        Wallet storage w,
        bytes32 id,
        uint16 seat,
        bytes32 referenceHash
    ) private view returns (bytes32) {
        return
            validator.readinessDigest(
                id,
                w.descriptorHash,
                address(this),
                w.descriptor.epoch,
                1,
                1,
                seat,
                referenceHash
            );
    }

    function expireWallet(bytes32 id) external nonReentrant {
        Wallet storage w = wallets[id];
        require(
            w.state == WalletState.PendingReady && block.number >= w.deadline,
            "FROST expiry denied"
        );
        w.state = WalletState.Closed;
        emit WalletExpired(id);
        walletOwner.__snowfallWalletExpired(id);
    }

    function expireDkg() external nonReentrant {
        require(
            (state == DkgState.AwaitingSeed &&
                block.number >= uint256(epoch) + seedTimeout) ||
                ((state == DkgState.AwaitingResult ||
                    state == DkgState.Challenge) &&
                    block.number >= resultDeadline),
            "FROST DKG expiry denied"
        );
        state = DkgState.Idle;
        delete submitted;
        delete submittedHash;
        sortitionPool.unlock();
        emit DkgExpired(epoch);
    }

    // Rewards cannot be redirected by a caller. No arbitrary external target or
    // reimbursement path exists; a later production package must qualify those.
    function withdrawRewards() external nonReentrant returns (uint96) {
        return sortitionPool.withdrawRewards(msg.sender, msg.sender);
    }
}
