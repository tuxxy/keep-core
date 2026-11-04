package tbtc

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/enforcement"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// The FROST node path's schedule and fee envelope (KC-SF-V3 D-09 inputs).
// Every operator applies the same values, so no per-node setting can make
// operators diverge (D19); the exact fee of a transaction comes from the
// leader's proposal. These are local test values: OPEN.
const (
	// frostFinalityBlocks is the finality depth FROST duties read chain
	// state at.
	frostFinalityBlocks = 2
	// frostMovementRoundBlocks is the length of one incoming sweep signing
	// round.
	frostMovementRoundBlocks = 10
	// frostDepositSweepRoundBlocks is the length of one deposit sweep
	// signing round. Its authorization rereads every deposit and Bitcoin's
	// median-time-past at each grant, so it gets longer rounds than an
	// incoming sweep.
	frostDepositSweepRoundBlocks = 30
	// frostRedemptionRoundBlocks is the length of one redemption payment
	// signing round.
	frostRedemptionRoundBlocks = 60
	// frostRedemptionCancelAfterBlocks is the RebroadcastThenCancel window.
	frostRedemptionCancelAfterBlocks = 400
	// frostRedemptionDeadlineMargin is the minimum number of seconds left
	// before a request deadline for it to be planned or attested.
	frostRedemptionDeadlineMargin = 600
	// frostMaximumSatPerVByte is the signing envelope's fee-rate ceiling.
	frostMaximumSatPerVByte = 20
	// frostMinimumFee is the redemption certificate's minimum absolute fee.
	frostMinimumFee = 100
	// frostDKGTimeout bounds one FROST DKG execution, from the epoch's
	// selection to readiness.
	frostDKGTimeout = 30 * time.Minute
	// frostRoundTimeout bounds one signing round in time, in addition to its
	// end block.
	frostRoundTimeout = 15 * time.Minute
	// frostRestoreTimeout bounds the restore of one epoch's seats.
	frostRestoreTimeout = 10 * time.Minute
	// frostRedemptionLookBackBlocks is how far back pending FROST redemption
	// requests are looked for, as base looks back for deposits.
	frostRedemptionLookBackBlocks = 216000
	// frostDepositRefundMargin is how many seconds before its refund
	// locktime a FROST deposit stops being swept: base's 24-hour deposit
	// refund safety margin (WalletProposalValidator).
	frostDepositRefundMargin = 24 * 3600
)

// FrostFinalityBlocks is the finality depth FROST duties read chain state at;
// the proposal generator reads FROST wallets at the same depth.
const FrostFinalityBlocks = frostFinalityBlocks

// FrostDepositPolicy is the K-06 deposit timing every FROST member and the
// proposal generator apply: the finality depth and base's refund safety
// margin.
var FrostDepositPolicy = deposit.Policy{
	FinalityBlocks: frostFinalityBlocks,
	RefundMargin:   frostDepositRefundMargin,
}

// FrostConfig is the FROST part of the node configuration: what a node cannot
// read from chain. The zero value means FROST is not configured, and the node
// then behaves as base (no FROST read, store or dispatch; shared rule 5).
type FrostConfig struct {
	// Registry is the FROST wallet registry's address.
	Registry [20]byte
	// Worker is the pinned SNOWFALL worker.
	Worker snowfallengine.WorkerConfig
	// Store is the FROST key store, the same on every restart of the
	// operator.
	Store store.Config
}

// Configured reports whether FROST is configured.
func (fc FrostConfig) Configured() bool {
	return fc.Registry != [20]byte{}
}

// FrostRegistryChain is the FROST wallet registry as the node reads it: the
// DKG seam plus the reads that find the epochs and wallets of this operator.
type FrostRegistryChain interface {
	dkg.Chain
	// CurrentEpoch is the registry's latest DKG epoch (the DKG's start
	// block, D-5).
	CurrentEpoch(ctx context.Context) (uint64, error)
	// ApprovedEpochs lists the epochs whose DKG result the registry
	// approved, oldest first.
	ApprovedEpochs(ctx context.Context) ([]uint64, error)
	// ApprovedWallet is the wallet approved for the epoch, zero if none.
	ApprovedWallet(ctx context.Context, epoch uint64) ([32]byte, error)
	// Wallet is the registry's record of the wallet.
	Wallet(ctx context.Context, id [32]byte) (dkg.Wallet, error)
}

// FrostBridgeChain is the FROST extension of the Bridge: redemptions (C-04),
// deposits (C-05, read through the movement seam), movement (C-06) and
// heartbeat and inactivity enforcement (C-09).
type FrostBridgeChain interface {
	redemption.Chain
	movement.Chain
	enforcement.Chain
}

// FrostBridgeConnector is implemented by host chains that bind the FROST
// extension of the Bridge for the proposal generator, which reads a FROST
// wallet's deposits and movement through the same seams the members plan
// from. Only a FROST wallet's proposal task binds it.
type FrostBridgeConnector interface {
	FrostTargetResolver
	FrostBridge(ctx context.Context) (FrostBridgeChain, error)
}

// FrostChainConnector is implemented by host chains that can bind the FROST
// registry and Bridge extension for a node with FROST configured.
type FrostChainConnector interface {
	ConnectFrost(
		ctx context.Context,
		registry [20]byte,
	) (FrostRegistryChain, FrostBridgeChain, error)
}

// FrostRedemptionRequestChain is implemented by host chains that read the
// FROST redemption requests (C-04) of a FROST wallet, the counterpart of the
// pending redemption requests of a legacy wallet. A FROST wallet is addressed
// by its output key, as FrostTargetResolver reports it for its label.
type FrostRedemptionRequestChain interface {
	FrostTargetResolver
	// PendingFrostRedemptions returns the output scripts of the wallet's
	// requested and unreserved redemptions requested since startBlock,
	// oldest first, at most limit.
	PendingFrostRedemptions(
		outputKey [32]byte,
		startBlock uint64,
		limit int,
	) ([]bitcoin.Script, error)
	// ReservedFrostRedemptions returns the output scripts of the requests
	// of the wallet's admitted, unresolved reservation, in its order, and
	// whether the wallet holds one.
	ReservedFrostRedemptions(outputKey [32]byte) ([]bitcoin.Script, bool, error)
}

// frostBitcoinChain is the node's Bitcoin chain with the script query the
// FROST planners discover wallet outputs by (KC-SF-V3 K-04 script queries).
type frostBitcoinChain interface {
	bitcoin.Chain
	redemption.ScriptUnspent
}

// frostNode is the FROST part of a node with FROST configured. It runs the
// FROST DKG for every epoch selecting a local seat, restores the saved seats
// at startup, and holds the ready wallets the coordination layer coordinates
// alongside the legacy ones. An error in one epoch or wallet is logged and
// never stops the node or another wallet's duties (#38, #39).
type frostNode struct {
	node     *node
	config   FrostConfig
	registry FrostRegistryChain
	bridge   FrostBridgeChain
	bitcoin  frostBitcoinChain
	store    *store.Store
	signer   *ecdsa.PrivateKey
	self     common.Address
	network  string
	chainID  [32]byte
	genesis  [32]byte

	mutex   sync.Mutex
	scopes  map[uint64]*store.Scoped
	epochs  map[uint64]bool
	wallets map[[32]byte]*frostWallet
}

// newFrostNode binds the FROST chain, opens the FROST store and restores the
// operator's saved seats, as base newNode loads its stored signers. Wallets
// the registry already certified are restored before it returns; seats of a
// pending wallet are restored in the background, where they take part in
// readiness.
func newFrostNode(
	ctx context.Context,
	n *node,
	config FrostConfig,
	network string,
) (*frostNode, error) {
	connector, ok := n.chain.(FrostChainConnector)
	if !ok {
		return nil, errors.New("chain cannot bind the FROST registry")
	}
	btc, ok := n.btcChain.(frostBitcoinChain)
	if !ok {
		return nil, errors.New("bitcoin chain has no script query")
	}
	registry, bridge, err := connector.ConnectFrost(ctx, config.Registry)
	if err != nil {
		return nil, fmt.Errorf("cannot bind the FROST chain: [%w]", err)
	}
	operatorPrivateKey, _, err := n.chain.OperatorKeyPair()
	if err != nil {
		return nil, fmt.Errorf("cannot get operator key: [%w]", err)
	}
	signer := operatorPrivateKeyToECDSA(operatorPrivateKey)
	parameters, err := registry.Parameters(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read FROST parameters: [%w]", err)
	}
	head, err := bridge.Head(ctx)
	if err != nil {
		return nil, err
	}
	redemptions, err := bridge.Configuration(ctx, head.Number)
	if err != nil {
		return nil, fmt.Errorf("cannot read FROST Bitcoin network: [%w]", err)
	}
	root, err := store.Open(ctx, config.Store)
	if err != nil {
		return nil, fmt.Errorf("cannot open FROST store: [%w]", err)
	}

	fn := &frostNode{
		node:     n,
		config:   config,
		registry: registry,
		bridge:   bridge,
		bitcoin:  btc,
		store:    root,
		signer:   signer,
		self:     crypto.PubkeyToAddress(signer.PublicKey),
		network:  network,
		genesis:  redemptions.BitcoinGenesis,
		scopes:   make(map[uint64]*store.Scoped),
		epochs:   make(map[uint64]bool),
		wallets:  make(map[[32]byte]*frostWallet),
	}
	parameters.ChainID.FillBytes(fn.chainID[:])

	// The epochs this operator may hold seats of: every approved one, and
	// the current one if its DKG is still running.
	epochs, err := registry.ApprovedEpochs(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read approved FROST epochs: [%w]", err)
	}
	current, err := registry.CurrentEpoch(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read FROST epoch: [%w]", err)
	}
	if current != 0 {
		epochs = append(epochs, current)
	}
	for _, epoch := range epochs {
		fn.restoreEpoch(ctx, epoch)
	}

	return fn, nil
}

func operatorPrivateKeyToECDSA(key *operator.PrivateKey) *ecdsa.PrivateKey {
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: crypto.S256(),
			X:     key.PublicKey.X,
			Y:     key.PublicKey.Y,
		},
		D: key.D,
	}
}

// domain is the FROST deployment domain of the epoch. Every operator derives
// the same one from the chain and the Ethereum network.
func (fn *frostNode) domain(epoch uint64) frost.Domain {
	return frost.Domain{
		Network:  fn.network,
		Chain:    fn.chainID,
		Registry: fn.config.Registry,
		Epoch:    epoch,
	}
}

func (fn *frostNode) scope(epoch uint64) (*store.Scoped, error) {
	fn.mutex.Lock()
	defer fn.mutex.Unlock()

	if scoped, ok := fn.scopes[epoch]; ok {
		return scoped, nil
	}
	scoped, err := fn.store.Scope(fn.domain(epoch))
	if err != nil {
		return nil, err
	}
	fn.scopes[epoch] = scoped
	return scoped, nil
}

// claimEpoch marks the epoch handled. It returns false if it already was.
func (fn *frostNode) claimEpoch(epoch uint64) bool {
	fn.mutex.Lock()
	defer fn.mutex.Unlock()

	if fn.epochs[epoch] {
		return false
	}
	fn.epochs[epoch] = true
	return true
}

// localSeats returns the operator's seats in the epoch's selection, with the
// selection's roster and operators.
func (fn *frostNode) localSeats(
	ctx context.Context,
	epoch uint64,
) (dkg.Selection, []uint16, error) {
	selection, err := fn.registry.Selection(ctx, epoch)
	if err != nil {
		return selection, nil, err
	}
	var locals []uint16
	for i, operator := range selection.Operators {
		if operator == fn.self {
			locals = append(locals, uint16(i+1))
		}
	}
	return selection, locals, nil
}

// membership is the group membership validator of the given seat holders.
func (fn *frostNode) membership(
	operators []common.Address,
) *group.MembershipValidator {
	addresses := make([]chain.Address, len(operators))
	for i, operator := range operators {
		addresses[i] = chain.Address(operator.Hex())
	}
	return group.NewMembershipValidator(
		logger,
		addresses,
		fn.node.chain.Signing(),
	)
}

// restoreEpoch restores the operator's seats of an epoch at node startup. A
// completed key is restored (Restore never reruns the DKG); seats whose host
// died before the key was stored are lost for that wallet; an epoch whose
// DKG is still awaiting a result and holds no local claim is joined.
func (fn *frostNode) restoreEpoch(ctx context.Context, epoch uint64) {
	if !fn.claimEpoch(epoch) {
		return
	}
	epochLogger := logger.With(zap.Uint64("frostEpoch", epoch))

	selection, locals, err := fn.localSeats(ctx, epoch)
	if err != nil {
		epochLogger.Errorf("cannot read FROST selection: [%v]", err)
		return
	}
	if len(locals) == 0 {
		return
	}
	journal, err := fn.scope(epoch)
	if err != nil {
		epochLogger.Errorf("cannot open FROST store scope: [%v]", err)
		return
	}

	status, err := journal.DKGStatus(ctx)
	if err != nil {
		// No local claim: the DKG of this epoch never started here.
		view, viewErr := fn.registry.View(ctx, epoch)
		if viewErr != nil {
			epochLogger.Errorf("cannot read FROST DKG state: [%v]", viewErr)
			return
		}
		if view.Epoch == epoch && view.State == frostDkgAwaitingResult {
			go fn.executeDKG(ctx, selection, locals, journal)
		}
		return
	}

	switch status.State {
	case frost.DKGSeatsLost:
		epochLogger.Warnf(
			"FROST seats [%v] were lost: their host stopped before the key "+
				"was stored",
			status.LostSeats,
		)
		return
	case frost.DKGKeyStored:
	default:
		epochLogger.Warnf("FROST DKG is [%s]; nothing to restore", status.State)
		return
	}

	id, err := fn.registry.ApprovedWallet(ctx, epoch)
	if err == nil && id != ([32]byte{}) {
		if wallet, err := fn.registry.Wallet(ctx, id); err == nil &&
			wallet.State == frostWalletReadyUnfunded {
			// Certified: restored before the node starts, as base loads
			// its signers.
			fn.restore(ctx, epoch, journal)
			return
		}
	}
	// Pending: the seats take part in readiness in the background.
	go fn.restore(ctx, epoch, journal)
}

// The FROST registry's DKG and wallet states.
const (
	frostDkgAwaitingResult   = 2
	frostWalletReadyUnfunded = 2
)

// restore runs the batch-4 startup restore for one epoch and adds the wallet
// to coordination once it is ready.
func (fn *frostNode) restore(
	ctx context.Context,
	epoch uint64,
	journal *store.Scoped,
) {
	epochLogger := logger.With(zap.Uint64("frostEpoch", epoch))

	selection, err := fn.registry.Selection(ctx, epoch)
	if err != nil {
		epochLogger.Errorf("cannot read FROST selection: [%v]", err)
		return
	}

	restoreCtx, cancel := context.WithTimeout(ctx, frostRestoreTimeout)
	defer cancel()

	_, record, err := StartFrostNode(restoreCtx, FrostNodeStartup{
		Chain:      fn.registry,
		Journal:    journal,
		Worker:     fn.config.Worker,
		Network:    fn.node.netProvider,
		Membership: fn.membership(selection.Operators),
		Signer:     fn.signer,
	})
	if err != nil {
		epochLogger.Errorf("cannot restore FROST seats: [%v]", err)
		return
	}

	fn.addWallet(epoch, journal, record)
}

// runDKGLoop watches the registry's current epoch, block by block, and runs
// the FROST DKG for every new epoch selecting a local seat. Each epoch runs
// on its own; a failing or stuck DKG never blocks another epoch or a wallet.
func (fn *frostNode) runDKGLoop(ctx context.Context) error {
	blockCounter, err := fn.node.chain.BlockCounter()
	if err != nil {
		return err
	}

	go func() {
		for range blockCounter.WatchBlocks(ctx) {
			epoch, err := fn.registry.CurrentEpoch(ctx)
			if err != nil {
				logger.Warnf("cannot read FROST epoch: [%v]", err)
				continue
			}
			if epoch != 0 {
				fn.joinDKGIfEligible(ctx, epoch)
			}
		}
	}()

	return nil
}

// joinDKGIfEligible runs the FROST DKG of a new epoch if it selects a local
// seat, with every local seat (operators hold several seats).
func (fn *frostNode) joinDKGIfEligible(ctx context.Context, epoch uint64) {
	if !fn.claimEpoch(epoch) {
		return
	}
	epochLogger := logger.With(zap.Uint64("frostEpoch", epoch))

	selection, locals, err := fn.localSeats(ctx, epoch)
	if err != nil || len(selection.Operators) == 0 {
		// The selection is not known yet; look again on a later block.
		fn.mutex.Lock()
		delete(fn.epochs, epoch)
		fn.mutex.Unlock()
		if err != nil {
			epochLogger.Warnf("cannot read FROST selection: [%v]", err)
		}
		return
	}
	if len(locals) == 0 {
		return
	}
	journal, err := fn.scope(epoch)
	if err != nil {
		epochLogger.Errorf("cannot open FROST store scope: [%v]", err)
		return
	}

	go fn.executeDKG(ctx, selection, locals, journal)
}

// executeDKG runs the FROST DKG of one epoch for the local seats and adds the
// wallet to coordination once it is ready.
func (fn *frostNode) executeDKG(
	ctx context.Context,
	selection dkg.Selection,
	locals []uint16,
	journal *store.Scoped,
) {
	epoch := selection.Epoch
	epochLogger := logger.With(zap.Uint64("frostEpoch", epoch))

	dkgCtx, cancel := context.WithTimeout(ctx, frostDKGTimeout)
	defer cancel()

	parameters, err := fn.registry.Parameters(dkgCtx)
	if err != nil {
		epochLogger.Errorf("cannot read FROST parameters: [%v]", err)
		return
	}
	roster := selection.Roster()
	domain := journal.Domain()
	attempt, err := domain.NewAttempt("dkg", domain.ID(), epoch)
	if err != nil {
		epochLogger.Errorf("cannot derive FROST DKG attempt: [%v]", err)
		return
	}
	engine, err := snowfallengine.NewGuarded(fn.config.Worker, journal)
	if err != nil {
		epochLogger.Errorf("cannot start FROST worker: [%v]", err)
		return
	}
	membership := fn.membership(selection.Operators)
	attemptTransport, err := transport.New(dkgCtx, transport.Config{
		Domain:         domain,
		Attempt:        attempt,
		ID:             snowfallengine.AttemptID(attempt),
		LocalSeats:     locals,
		Roster:         roster,
		LocalPublicKey: crypto.FromECDSAPub(&fn.signer.PublicKey),
		Membership:     membership,
		Network:        fn.node.netProvider,
	})
	if err != nil {
		epochLogger.Errorf("cannot open FROST DKG transport: [%v]", err)
		return
	}
	defer attemptTransport.Close()
	bus, err := dkg.NewNetworkBus(dkgCtx, fn.node.netProvider, membership, domain.ID(), epoch)
	if err != nil {
		epochLogger.Errorf("cannot open FROST attestation channel: [%v]", err)
		return
	}

	epochLogger.Infof("joining FROST DKG with seats [%v]", locals)

	record, err := ExecuteFrostDKG(dkgCtx, &dkg.Executor{
		Chain:   fn.registry,
		Engine:  engine,
		Journal: journal,
		Bus:     bus,
		Signer:  fn.signer,
		Request: frost.DKGRequest{
			Group: frost.Group{
				Roster:    roster,
				Threshold: parameters.Threshold,
				Quorum:    parameters.GroupSize,
				Epoch:     epoch,
			},
			LocalSeats:   locals,
			Participants: roster,
			Attempt:      attempt,
		},
		Providers:      frost.Providers{Transport: attemptTransport, Store: journal},
		FinalityBlocks: frostFinalityBlocks,
		PollInterval:   frostPollInterval,
	})
	if err != nil {
		epochLogger.Errorf("FROST DKG failed: [%v]", err)
		return
	}

	fn.addWallet(epoch, journal, record)
}

// frostPollInterval is how often FROST executors poll the chain.
const frostPollInterval = 50 * time.Millisecond

// frostWallet is a ready FROST wallet with local seats: what the node
// coordinates and signs for.
type frostWallet struct {
	node       *frostNode
	id         [32]byte
	label      [20]byte
	epoch      uint64
	descriptor dkg.Descriptor
	localSeats []uint16
	journal    *store.Scoped

	coordinationExecutor *coordinationExecutor
	reservations         *frostReservationChannel

	// actionMutex allows one action at a time per wallet, as base's wallet
	// dispatcher does.
	actionMutex sync.Mutex
}

// addWallet adds a ready wallet to coordination.
func (fn *frostNode) addWallet(
	epoch uint64,
	journal *store.Scoped,
	record dkg.WalletRecord,
) {
	walletLogger := logger.With(
		zap.Uint64("frostEpoch", epoch),
		zap.String("frostWallet", fmt.Sprintf("0x%x", record.ID)),
	)
	if record.Quarantined || record.State != "ReadyUnfunded" {
		walletLogger.Warnf(
			"FROST wallet is [%s] (quarantined [%v]); not coordinated",
			record.State,
			record.Quarantined,
		)
		return
	}

	fn.mutex.Lock()
	_, exists := fn.wallets[record.ID]
	fn.mutex.Unlock()
	if exists {
		return
	}

	wallet, err := fn.newFrostWallet(epoch, journal, record)
	if err != nil {
		walletLogger.Errorf("cannot set up FROST wallet: [%v]", err)
		return
	}

	fn.mutex.Lock()
	fn.wallets[record.ID] = wallet
	fn.mutex.Unlock()

	walletLogger.Infof("FROST wallet ready with local seats [%v]", wallet.localSeats)
}

func (fn *frostNode) newFrostWallet(
	epoch uint64,
	journal *store.Scoped,
	record dkg.WalletRecord,
) (*frostWallet, error) {
	descriptor := record.Descriptor

	var label [20]byte
	copy(label[:], record.ID[:20])

	var localSeats []uint16
	operators := make([]chain.Address, len(descriptor.Operators))
	for i, operator := range descriptor.Operators {
		operators[i] = chain.Address(operator.Hex())
		if operator == fn.self {
			localSeats = append(localSeats, uint16(i+1))
		}
	}
	if len(localSeats) == 0 {
		return nil, errors.New("no local seats")
	}

	membership := fn.membership(descriptor.Operators)
	operatorAddress, err := fn.node.operatorAddress()
	if err != nil {
		return nil, err
	}

	coordinationChannel, err := fn.node.netProvider.BroadcastChannelFor(
		fmt.Sprintf("%s-frost-%x-coordination", ProtocolName, label),
	)
	if err != nil {
		return nil, err
	}
	coordinationChannel.SetUnmarshaler(func() net.TaggedUnmarshaler {
		return &coordinationMessage{}
	})
	if err := coordinationChannel.SetFilter(membership.IsInGroup); err != nil {
		return nil, err
	}

	members := make([]group.MemberIndex, len(localSeats))
	for i, seat := range localSeats {
		members[i] = group.MemberIndex(seat)
	}

	wallet := &frostWallet{
		node:       fn,
		id:         record.ID,
		label:      label,
		epoch:      epoch,
		descriptor: descriptor,
		localSeats: localSeats,
		journal:    journal,
	}

	wallet.coordinationExecutor = newFrostCoordinationExecutor(
		fn.node.chain,
		label,
		operators,
		members,
		operatorAddress,
		fn.node.proposalGenerator,
		coordinationChannel,
		membership,
		fn.node.protocolLatch,
		fn.node.waitForBlockHeight,
	)

	wallet.reservations, err = newFrostReservationChannel(
		fn.node.netProvider,
		label,
		descriptor,
		membership,
	)
	if err != nil {
		return nil, err
	}

	return wallet, nil
}

// coordinatedWallets returns the ready FROST wallets with local seats.
func (fn *frostNode) coordinatedWallets() []*frostWallet {
	fn.mutex.Lock()
	defer fn.mutex.Unlock()

	wallets := make([]*frostWallet, 0, len(fn.wallets))
	for _, wallet := range fn.wallets {
		wallets = append(wallets, wallet)
	}
	return wallets
}

// signingConfig is the operator's base signing configuration for the wallet;
// the executors fill in the plan, session and round per attempt.
func (fw *frostWallet) signingConfig() signing.Config {
	fn := fw.node
	roster := make([]uint16, len(fw.descriptor.Operators))
	for i := range roster {
		roster[i] = uint16(i + 1)
	}
	membership := fn.membership(fw.descriptor.Operators)

	return signing.Config{
		Journal:              fw.journal,
		Bitcoin:              &frostSigningBitcoin{chain: fn.bitcoin, genesis: fn.genesis},
		Worker:               fn.config.Worker,
		MinimumConfirmations: 1,
		// K-04's deployment gate: FROST signing runs only for a Bridge
		// whose FROST Bitcoin network is regtest.
		LocalRegtest: fn.genesis == [32]byte(*chaincfg.RegressionNetParams.GenesisHash),
		Transport: func(
			ctx context.Context,
			attempt frost.Attempt,
		) (frost.Transport, func(), error) {
			t, err := transport.New(ctx, transport.Config{
				Domain:         fw.journal.Domain(),
				Attempt:        attempt,
				ID:             snowfallengine.AttemptID(attempt),
				LocalSeats:     fw.localSeats,
				Roster:         roster,
				LocalPublicKey: crypto.FromECDSAPub(&fn.signer.PublicKey),
				Membership:     membership,
				Network:        fn.node.netProvider,
			})
			if err != nil {
				return nil, nil, err
			}
			return t, t.Close, nil
		},
	}
}

// frostSigningBitcoin is the signing executors' Bitcoin view over the node's
// Bitcoin chain.
type frostSigningBitcoin struct {
	chain   frostBitcoinChain
	genesis [32]byte
}

func (fsb *frostSigningBitcoin) Genesis(context.Context) ([32]byte, error) {
	return fsb.genesis, nil
}

func (fsb *frostSigningBitcoin) Transaction(
	_ context.Context,
	hash bitcoin.Hash,
) ([]byte, error) {
	transaction, err := fsb.chain.GetTransaction(hash)
	if err != nil {
		return nil, err
	}
	return transaction.Serialize(bitcoin.Witness), nil
}

func (fsb *frostSigningBitcoin) Confirmations(
	ctx context.Context,
	hash bitcoin.Hash,
) (uint, error) {
	return fsb.chain.GetTransactionConfirmations(ctx, hash)
}

// Unspent reports whether the outpoint is an unspent output, counting spends
// in the mempool, from the outputs paying its script.
func (fsb *frostSigningBitcoin) Unspent(
	ctx context.Context,
	outpoint bitcoin.TransactionOutpoint,
) (bool, error) {
	transaction, err := fsb.chain.GetTransaction(outpoint.TransactionHash)
	if err != nil {
		// Unknown, not spent.
		return false, err
	}
	if int(outpoint.OutputIndex) >= len(transaction.Outputs) {
		return false, nil
	}
	script := transaction.Outputs[outpoint.OutputIndex].PublicKeyScript
	unspent, err := fsb.chain.UnspentByScript(ctx, script)
	if err != nil {
		return false, err
	}
	for _, output := range unspent {
		if output.Outpoint.TransactionHash == outpoint.TransactionHash &&
			output.Outpoint.OutputIndex == outpoint.OutputIndex {
			return true, nil
		}
	}
	return false, nil
}

func (fsb *frostSigningBitcoin) Broadcast(_ context.Context, raw []byte) error {
	transaction := new(bitcoin.Transaction)
	if err := transaction.Deserialize(raw); err != nil {
		return err
	}
	return fsb.chain.BroadcastTransaction(transaction)
}
