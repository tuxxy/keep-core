package tbtcpg_test

// Everything in this file is a test FIXTURE for the FROST branches of the
// proposal tasks (deposit sweep, redemption, moving funds, moved funds
// sweep): an in-memory FROST Bridge, a host chain (the package's LocalChain
// plus the FROST seams) and a Bitcoin chain (the package's LocalBitcoinChain
// plus a script query and header clock). The code under test is each task's
// Run and the real K-05/K-06/K-07 planners it calls.
//
// Every read the fixtures serve goes through one fault injector: with
// failAt = n the n-th read (counted across the host, the Bridge and Bitcoin)
// fails, and firedOp names it. Tests loop n = 1, 2, ... until a run no longer
// reaches the fault, so they do not depend on the order or number of reads.

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

var errGapsRead = errors.New("fixture: injected read fault")

type gFaults struct {
	reads   int
	failAt  int
	fired   bool
	firedOp string
}

func (f *gFaults) read(op string) error {
	f.reads++
	if f.failAt != 0 && f.reads == f.failAt {
		f.fired, f.firedOp = true, op
		return fmt.Errorf("%w (%s)", errGapsRead, op)
	}
	return nil
}

// headerHash is the fixture's canonical Ethereum header hash at a height.
func headerHash(n uint64) [32]byte {
	return [32]byte{0x77, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

// gCommit is one submitted C-06 commitment.
type gCommit struct {
	wallet  [32]byte
	seat    uint16
	targets [][20]byte
}

// gBridge is a FIXTURE FROST Bridge reader. Methods outside the proposal
// tasks' paths are not implemented (the embedded nil interface panics).
type gBridge struct {
	tbtc.FrostBridgeChain
	f *gFaults

	head uint64
	now  uint64

	custody redemption.Custody
	outputs map[[32]byte]redemption.CustodyOutput
	intents map[[32]byte]redemption.Intent

	reveals  []deposit.Reveal
	deposits map[[32]byte]deposit.Record
	reserved map[[32]byte]bool

	wallets    map[[20]byte]movement.Wallet
	lifecycle  movement.Lifecycle
	commitment movement.Commitment
	logBlock   uint64
	counters   movement.Counters
	params     movement.Parameters
	ready      [][32]byte
	commits    []gCommit
	commitErr  error

	events   []redemption.RequestEvent
	requests map[[32]byte]redemption.Request
}

func newGBridge(f *gFaults) *gBridge {
	return &gBridge{
		f: f, head: 1_000, now: uint64(time.Now().Unix()),
		outputs:  map[[32]byte]redemption.CustodyOutput{},
		intents:  map[[32]byte]redemption.Intent{},
		deposits: map[[32]byte]deposit.Record{},
		reserved: map[[32]byte]bool{},
		wallets:  map[[20]byte]movement.Wallet{},
		requests: map[[32]byte]redemption.Request{},
	}
}

func (b *gBridge) Head(context.Context) (redemption.Head, error) {
	if err := b.f.read("head"); err != nil {
		return redemption.Head{}, err
	}
	return redemption.Head{Number: b.head, Hash: headerHash(b.head)}, nil
}

func (b *gBridge) HeaderHash(_ context.Context, n uint64) ([32]byte, error) {
	if err := b.f.read("header-hash"); err != nil {
		return [32]byte{}, err
	}
	return headerHash(n), nil
}

func (b *gBridge) Timestamp(context.Context, uint64) (uint64, error) {
	if err := b.f.read("timestamp"); err != nil {
		return 0, err
	}
	return b.now, nil
}

func (b *gBridge) Custody(context.Context, uint64, [32]byte) (redemption.Custody, error) {
	if err := b.f.read("custody"); err != nil {
		return redemption.Custody{}, err
	}
	return b.custody, nil
}

func (b *gBridge) CustodyOutput(_ context.Context, _ uint64, key [32]byte) (redemption.CustodyOutput, error) {
	if err := b.f.read("custody-output"); err != nil {
		return redemption.CustodyOutput{}, err
	}
	return b.outputs[key], nil
}

func (b *gBridge) Intent(_ context.Context, _ uint64, id [32]byte) (redemption.Intent, error) {
	if err := b.f.read("intent"); err != nil {
		return redemption.Intent{}, err
	}
	return b.intents[id], nil
}

func (b *gBridge) Reveals(_ context.Context, _ [32]byte, from, to uint64) ([]deposit.Reveal, error) {
	if err := b.f.read("reveals"); err != nil {
		return nil, err
	}
	var out []deposit.Reveal
	for _, r := range b.reveals {
		if r.Block >= from && r.Block <= to {
			out = append(out, r)
		}
	}
	return out, nil
}

func (b *gBridge) Deposit(_ context.Context, _ uint64, key [32]byte) (deposit.Record, error) {
	if err := b.f.read("deposit"); err != nil {
		return deposit.Record{}, err
	}
	r, ok := b.deposits[key]
	if !ok {
		return deposit.Record{}, deposit.ErrNotFound
	}
	return r, nil
}

func (b *gBridge) Reveal(_ context.Context, key [32]byte) (deposit.Reveal, error) {
	if err := b.f.read("reveal"); err != nil {
		return deposit.Reveal{}, err
	}
	for _, r := range b.reveals {
		if r.Outpoint == key {
			return r, nil
		}
	}
	return deposit.Reveal{}, deposit.ErrNotFound
}

func (b *gBridge) Reserved(_ context.Context, _ uint64, key [32]byte) (bool, error) {
	if err := b.f.read("reserved"); err != nil {
		return false, err
	}
	return b.reserved[key], nil
}

func (b *gBridge) Wallet(_ context.Context, _ uint64, label [20]byte) (movement.Wallet, error) {
	if err := b.f.read("wallet"); err != nil {
		return movement.Wallet{}, err
	}
	w, ok := b.wallets[label]
	if !ok {
		return movement.Wallet{}, movement.ErrNotFound
	}
	return w, nil
}

func (b *gBridge) Lifecycle(context.Context, uint64, [32]byte) (movement.Lifecycle, error) {
	if err := b.f.read("lifecycle"); err != nil {
		return movement.Lifecycle{}, err
	}
	return b.lifecycle, nil
}

func (b *gBridge) Commitment(context.Context, uint64, [32]byte, uint32) (movement.Commitment, error) {
	if err := b.f.read("commitment"); err != nil {
		return movement.Commitment{}, err
	}
	return b.commitment, nil
}

func (b *gBridge) CommitmentLog(_ context.Context, wallet [32]byte, period uint32) (movement.CommitmentLog, error) {
	if err := b.f.read("commitment-log"); err != nil {
		return movement.CommitmentLog{}, err
	}
	return movement.CommitmentLog{Log: movement.Log{Block: b.logBlock, BlockHash: headerHash(b.logBlock)}, Wallet: wallet, Period: period}, nil
}

func (b *gBridge) Counters(context.Context, uint64, [32]byte) (movement.Counters, error) {
	if err := b.f.read("counters"); err != nil {
		return movement.Counters{}, err
	}
	return b.counters, nil
}

func (b *gBridge) MovementParameters(context.Context, uint64) (movement.Parameters, error) {
	if err := b.f.read("movement-parameters"); err != nil {
		return movement.Parameters{}, err
	}
	return b.params, nil
}

func (b *gBridge) ReadyWallets(context.Context, uint64, uint64) ([][32]byte, error) {
	if err := b.f.read("ready-wallets"); err != nil {
		return nil, err
	}
	return b.ready, nil
}

func (b *gBridge) SubmitCommitment(_ context.Context, wallet [32]byte, seat uint16, targets [][20]byte) error {
	b.commits = append(b.commits, gCommit{wallet: wallet, seat: seat, targets: targets})
	return b.commitErr
}

func (b *gBridge) RequestEvents(_ context.Context, _ [32]byte, from, to uint64) ([]redemption.RequestEvent, error) {
	if err := b.f.read("request-events"); err != nil {
		return nil, err
	}
	var out []redemption.RequestEvent
	for _, e := range b.events {
		if e.Block >= from && e.Block <= to {
			out = append(out, e)
		}
	}
	return out, nil
}

func (b *gBridge) Request(_ context.Context, _ uint64, id [32]byte) (redemption.Request, error) {
	if err := b.f.read("request"); err != nil {
		return redemption.Request{}, err
	}
	return b.requests[id], nil
}

// gBlockCounter is a FIXTURE block counter.
type gBlockCounter struct {
	*tbtcpg.MockBlockCounter
	f     *gFaults
	block uint64
}

func (c *gBlockCounter) CurrentBlock() (uint64, error) {
	if err := c.f.read("current-block"); err != nil {
		return 0, err
	}
	return c.block, nil
}

// gHost is a FIXTURE host chain: the package's LocalChain plus every FROST
// seam the proposal tasks use (FrostBridgeConnector,
// FrostRedemptionRequestChain, FrostIncomingChain).
type gHost struct {
	*tbtcpg.LocalChain
	f *gFaults

	label     [20]byte
	outputKey [32]byte
	frost     bool
	// frostAnswers, if set, are the successive FROST answers of the
	// resolver (a registry read at successive blocks), then frost.
	frostAnswers []bool
	bridge       *gBridge

	depositSweepMaxSize uint16
	redemptionMaxSize   uint16
	minAge              uint32
	block               uint64
	mfParams            tbtc.MovingFundsParameters
	redemptionParams    tbtc.RedemptionParameters

	// pendingScripts are the wallet's pending FROST redemption requests,
	// requested at pendingBlocks.
	pendingScripts  []bitcoin.Script
	pendingBlocks   []uint64
	reservedScripts []bitcoin.Script
	reservedHeld    bool

	// incoming are the recorded incoming outputs, oldest first, with the
	// block each was recorded at.
	incoming        []bitcoin.TransactionOutpoint
	incomingBlock   map[bitcoin.TransactionOutpoint]uint64
	incomingPending map[bitcoin.TransactionOutpoint]bool
	hasMain         bool
	// validated is the moved funds sweep proposal the on-chain validator
	// accepts; any other is refused.
	validated *tbtc.MovedFundsSweepProposal
}

func newGHost(f *gFaults, bridge *gBridge) *gHost {
	lc := tbtcpg.NewLocalChain()
	// A real chain never reports a zero block time; the legacy paths divide
	// by it.
	lc.SetAverageBlockTime(12 * time.Second)
	return &gHost{
		LocalChain: lc,
		f:          f,
		label:      [20]byte{0x24},
		outputKey:  [32]byte{0x51, 0x02},
		frost:      true,
		bridge:     bridge,
		minAge:     60,
		block:      1_000,
		mfParams: tbtc.MovingFundsParameters{
			TxMaxTotalFee: 1_000_000, Timeout: 30 * 24 * 3600, SweepTxMaxTotalFee: 1_000_000,
		},
		redemptionParams: tbtc.RedemptionParameters{TxMaxTotalFee: 1_000_000},
		incomingBlock:    map[bitcoin.TransactionOutpoint]uint64{},
		incomingPending:  map[bitcoin.TransactionOutpoint]bool{},
	}
}

func (h *gHost) walletID() [32]byte { return dkg.WalletID(h.outputKey) }

func (h *gHost) GetDepositSweepMaxSize() (uint16, error) { return h.depositSweepMaxSize, nil }
func (h *gHost) GetRedemptionMaxSize() (uint16, error)   { return h.redemptionMaxSize, nil }

func (h *gHost) GetDepositMinAge() (uint32, error) {
	if err := h.f.read("deposit-min-age"); err != nil {
		return 0, err
	}
	return h.minAge, nil
}

func (h *gHost) BlockCounter() (chain.BlockCounter, error) {
	if err := h.f.read("block-counter"); err != nil {
		return nil, err
	}
	return &gBlockCounter{MockBlockCounter: tbtcpg.NewMockBlockCounter(), f: h.f, block: h.block}, nil
}

func (h *gHost) GetMovingFundsParameters() (tbtc.MovingFundsParameters, error) {
	if err := h.f.read("moving-funds-parameters"); err != nil {
		return tbtc.MovingFundsParameters{}, err
	}
	return h.mfParams, nil
}

func (h *gHost) GetRedemptionParameters() (tbtc.RedemptionParameters, error) {
	if err := h.f.read("redemption-parameters"); err != nil {
		return tbtc.RedemptionParameters{}, err
	}
	return h.redemptionParams, nil
}

func (h *gHost) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	if err := h.f.read("resolve"); err != nil {
		return [32]byte{}, false, err
	}
	frost := h.frost
	if len(h.frostAnswers) > 0 {
		frost, h.frostAnswers = h.frostAnswers[0], h.frostAnswers[1:]
	}
	return h.outputKey, frost && label == h.label, nil
}

func (h *gHost) FrostBridge(context.Context) (tbtc.FrostBridgeChain, error) {
	if err := h.f.read("bind"); err != nil {
		return nil, err
	}
	return h.bridge, nil
}

func (h *gHost) PendingFrostRedemptions(_ [32]byte, from uint64, limit int) ([]bitcoin.Script, error) {
	if err := h.f.read("pending-redemptions"); err != nil {
		return nil, err
	}
	var out []bitcoin.Script
	for i, s := range h.pendingScripts {
		if h.pendingBlocks[i] >= from && len(out) < limit {
			out = append(out, s)
		}
	}
	return out, nil
}

func (h *gHost) ReservedFrostRedemptions([32]byte) ([]bitcoin.Script, bool, error) {
	if err := h.f.read("reserved-redemptions"); err != nil {
		return nil, false, err
	}
	return h.reservedScripts, h.reservedHeld, nil
}

func (h *gHost) PastFrostIncomingOutputs(_ [32]byte, from uint64) ([]bitcoin.TransactionOutpoint, error) {
	if err := h.f.read("incoming-outputs"); err != nil {
		return nil, err
	}
	var out []bitcoin.TransactionOutpoint
	for _, o := range h.incoming {
		if h.incomingBlock[o] >= from && h.incomingBlock[o] <= h.block {
			out = append(out, o)
		}
	}
	return out, nil
}

func (h *gHost) IsFrostIncomingPending(_ [32]byte, outpoint bitcoin.TransactionOutpoint) (bool, error) {
	if err := h.f.read("incoming-pending"); err != nil {
		return false, err
	}
	return h.incomingPending[outpoint], nil
}

func (h *gHost) FrostWalletHasMain([32]byte) (bool, error) {
	if err := h.f.read("has-main"); err != nil {
		return false, err
	}
	return h.hasMain, nil
}

func (h *gHost) ValidateMovedFundsSweepProposal(_ [20]byte, p *tbtc.MovedFundsSweepProposal) error {
	if err := h.f.read("validate-moved-funds-sweep"); err != nil {
		return err
	}
	if h.validated == nil || p.MovingFundsTxHash != h.validated.MovingFundsTxHash ||
		p.MovingFundsTxOutputIndex != h.validated.MovingFundsTxOutputIndex || p.SweepTxFee.Sign() <= 0 {
		return errors.New("fixture: moved funds sweep proposal refused")
	}
	return nil
}

// gPlainHost is a FIXTURE host chain without any FROST seam.
type gPlainHost struct {
	*tbtcpg.LocalChain
}

func (gPlainHost) GetDepositSweepMaxSize() (uint16, error) { return 5, nil }
func (gPlainHost) GetRedemptionMaxSize() (uint16, error)   { return 5, nil }

// gBitcoin is a FIXTURE Bitcoin chain: the package's LocalBitcoinChain plus
// the script query (unspent outputs, mempool included) and a header clock
// whose median-time-past is mtp.
type gBitcoin struct {
	*tbtcpg.LocalBitcoinChain
	f       *gFaults
	unspent []bitcoin.PreviousOutput
	mtp     uint32
}

func newGBitcoin(f *gFaults) *gBitcoin {
	btc := &gBitcoin{LocalBitcoinChain: tbtcpg.NewLocalBitcoinChain(), f: f, mtp: uint32(time.Now().Unix())}
	for blocks := uint32(1); blocks <= 32; blocks++ {
		btc.SetEstimateSatPerVByteFee(blocks, 2)
	}
	return btc
}

func (c *gBitcoin) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	if err := c.f.read("unspent"); err != nil {
		return nil, err
	}
	var out []bitcoin.PreviousOutput
	for _, u := range c.unspent {
		if string(u.PublicKeyScript) == string(script) {
			out = append(out, u)
		}
	}
	return out, nil
}

func (c *gBitcoin) GetTransaction(hash bitcoin.Hash) (*bitcoin.Transaction, error) {
	if err := c.f.read("transaction"); err != nil {
		return nil, err
	}
	return c.LocalBitcoinChain.GetTransaction(hash)
}

func (c *gBitcoin) GetTransactionConfirmations(ctx context.Context, hash bitcoin.Hash) (uint, error) {
	if err := c.f.read("confirmations"); err != nil {
		return 0, err
	}
	return c.LocalBitcoinChain.GetTransactionConfirmations(ctx, hash)
}

func (c *gBitcoin) GetLatestBlockHeight() (uint, error) {
	if err := c.f.read("tip"); err != nil {
		return 0, err
	}
	return 20, nil
}

func (c *gBitcoin) GetBlockHeader(uint) (*bitcoin.BlockHeader, error) {
	if err := c.f.read("header"); err != nil {
		return nil, err
	}
	return &bitcoin.BlockHeader{Time: c.mtp}, nil
}

func (c *gBitcoin) EstimateSatPerVByteFee(blocks uint32) (int64, error) {
	if err := c.f.read("fee"); err != nil {
		return 0, err
	}
	return c.LocalBitcoinChain.EstimateSatPerVByteFee(blocks)
}

// gDeposit adds one revealed FROST deposit of the host's wallet: funding
// transaction with one output of a distinct script, confirmed, unspent,
// reserved under C-10, final, old enough and far from its refund locktime.
// It returns the deposit key.
func gDeposit(h *gHost, btc *gBitcoin, tag byte, maxFee uint64) [32]byte {
	script := bitcoin.Script{0x51, 0x20, tag}
	funding := &bitcoin.Transaction{
		Version: 2,
		Inputs:  []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xee, tag}}, Sequence: 0xffffffff}},
		Outputs: []*bitcoin.TransactionOutput{{Value: 100_000, PublicKeyScript: script}},
	}
	hash := funding.Hash()
	outpoint := bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: 0}
	key := deposit.OutpointKey(outpoint)
	btc.SetTransaction(hash, funding)
	btc.SetTransactionConfirmations(hash, tbtc.DepositSweepRequiredFundingTxConfirmations)
	btc.unspent = append(btc.unspent, bitcoin.PreviousOutput{Outpoint: outpoint, Value: 100_000, PublicKeyScript: script})
	b := h.bridge
	b.deposits[key] = deposit.Record{
		Terms:                  deposit.Terms{WalletID: h.walletID(), Amount: 100_000, RefundLocktime: uint32(b.now + 30*24*3600)},
		FundingTransactionHash: [32]byte(hash),
		WalletKey:              h.outputKey,
		MaximumSweepFee:        maxFee,
		RevealedAt:             b.now - 2*3600,
	}
	b.reveals = append(b.reveals, deposit.Reveal{Outpoint: key, WalletID: h.walletID(), Block: b.head - 100})
	b.reserved[key] = true
	return key
}

// proposalKeys are the deposit keys a sweep proposal names.
func proposalKeys(p *tbtc.DepositSweepProposal) map[[32]byte]bool {
	out := map[[32]byte]bool{}
	for _, k := range p.DepositsKeys {
		out[deposit.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: k.FundingTxHash, OutputIndex: k.FundingOutputIndex})] = true
	}
	return out
}

// errGapsPanic marks a recovered panic: a task must refuse, never crash.
var errGapsPanic = errors.New("task panicked")

// recoverRun runs a task and turns a panic into an errGapsPanic error, so
// that a mutant that crashes instead of refusing fails a named assertion.
func recoverRun(task tbtcpg.ProposalTask, request *tbtc.CoordinationProposalRequest) (proposal tbtc.CoordinationProposal, ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			proposal, ok, err = nil, false, fmt.Errorf("%w: %v", errGapsPanic, r)
		}
	}()
	return task.Run(request)
}

// gHostNoBridge is a FIXTURE host chain that reads FROST redemption requests
// (FrostRedemptionRequestChain) but cannot bind the FROST Bridge.
type gHostNoBridge struct {
	*tbtcpg.LocalChain
	h *gHost
}

func (n gHostNoBridge) GetRedemptionMaxSize() (uint16, error) { return n.h.GetRedemptionMaxSize() }
func (n gHostNoBridge) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	return n.h.FrostTargetOutputKey(label)
}
func (n gHostNoBridge) PendingFrostRedemptions(k [32]byte, from uint64, limit int) ([]bitcoin.Script, error) {
	return n.h.PendingFrostRedemptions(k, from, limit)
}
func (n gHostNoBridge) ReservedFrostRedemptions(k [32]byte) ([]bitcoin.Script, bool, error) {
	return n.h.ReservedFrostRedemptions(k)
}

// gHostNoRequests is a FIXTURE host chain that binds the FROST Bridge
// (FrostBridgeConnector) but cannot read FROST redemption reservations
// (no FrostRedemptionRequestChain).
type gHostNoRequests struct {
	*tbtcpg.LocalChain
	h *gHost
}

func (n gHostNoRequests) GetRedemptionMaxSize() (uint16, error) { return n.h.GetRedemptionMaxSize() }
func (n gHostNoRequests) BlockCounter() (chain.BlockCounter, error) {
	return n.h.BlockCounter()
}
func (n gHostNoRequests) GetRedemptionParameters() (tbtc.RedemptionParameters, error) {
	return n.h.GetRedemptionParameters()
}
func (n gHostNoRequests) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	return n.h.FrostTargetOutputKey(label)
}
func (n gHostNoRequests) FrostBridge(ctx context.Context) (tbtc.FrostBridgeChain, error) {
	return n.h.FrostBridge(ctx)
}

var _ = big.NewInt
