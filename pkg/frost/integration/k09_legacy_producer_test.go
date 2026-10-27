package integration

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// This adapter supplies public chain observations to the unchanged task API.
// Selection, count, fee estimation and commitment production run in tbtcpg.
// The legacy membership and local mining clock remain explicit fixture seams.
type publicLegacyProducer struct {
	tbtcpg.Chain // Unexpected/unimplemented operations fail instead of faking success.
	w            *k07World
	validator    *legacyabi.WalletProposalValidator
}
type publicLegacyBitcoin struct{ *coreSPV }

func (b publicLegacyBitcoin) GetTxHashesForPublicKeyHash(pkh [20]byte) ([]bitcoin.Hash, error) {
	address, err := btcutil.NewAddressWitnessPubKeyHash(pkh[:], &chaincfg.RegressionNetParams)
	if err != nil {
		return nil, err
	}
	var scan struct{ Unspents []struct{ Txid string } }
	if err = b.core.call(b.ctx, "scantxoutset", []any{"start", []string{"addr(" + address.EncodeAddress() + ")"}}, &scan); err != nil {
		return nil, err
	}
	var out []bitcoin.Hash
	for _, utxo := range scan.Unspents {
		h, err := bitcoin.NewHashFromString(utxo.Txid, bitcoin.ReversedByteOrder)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

type publicLegacyBlocks struct {
	chain.BlockCounter
	w *k07World
}

func (b publicLegacyBlocks) CurrentBlock() (uint64, error) {
	return b.w.f.client.BlockNumber(b.w.f.ctx)
}
func (b publicLegacyBlocks) WaitForBlockHeight(height uint64) error {
	for {
		at, err := b.CurrentBlock()
		if err != nil {
			return err
		}
		if at >= height {
			return nil
		}
		b.w.mine(1)
	}
}
func (c *publicLegacyProducer) BlockCounter() (chain.BlockCounter, error) {
	return publicLegacyBlocks{w: c.w}, nil
}
func (c *publicLegacyProducer) MovingFundsNow() (time.Time, error) {
	header, err := c.w.f.client.HeaderByNumber(c.w.f.ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(header.Time), 0), nil
}
func (c *publicLegacyProducer) GetWallet(pkh [20]byte) (*tbtc.WalletChainData, error) {
	v, err := c.w.legacy.Wallets(&bind.CallOpts{Context: c.w.f.ctx}, pkh)
	if err != nil {
		return nil, err
	}
	return &tbtc.WalletChainData{EcdsaWalletID: v.EcdsaWalletID, MainUtxoHash: v.MainUtxoHash, PendingRedemptionsValue: v.PendingRedemptionsValue, MovingFundsRequestedAt: time.Unix(int64(v.MovingFundsRequestedAt), 0), PendingMovedFundsSweepRequestsCount: v.PendingMovedFundsSweepRequestsCount, State: tbtc.WalletState(v.State), MovingFundsTargetWalletsCommitmentHash: v.MovingFundsTargetWalletsCommitmentHash}, nil
}
func (c *publicLegacyProducer) GetMovingFundsParameters() (tbtc.MovingFundsParameters, error) {
	v, err := c.w.legacy.MovingFundsParameters(&bind.CallOpts{Context: c.w.f.ctx})
	return tbtc.MovingFundsParameters{TxMaxTotalFee: v.MovingFundsTxMaxTotalFee, Timeout: v.MovingFundsTimeout, TimeoutResetDelay: v.MovingFundsTimeoutResetDelay}, err
}
func (c *publicLegacyProducer) GetWalletParameters() (tbtc.WalletParameters, error) {
	v, err := c.w.legacy.WalletParameters(&bind.CallOpts{Context: c.w.f.ctx})
	return tbtc.WalletParameters{MaxBtcTransfer: v.WalletMaxBtcTransfer}, err
}
func (c *publicLegacyProducer) GetLiveWalletsCount() (uint32, error) {
	return c.w.legacy.LiveWalletsCount(&bind.CallOpts{Context: c.w.f.ctx})
}
func (c *publicLegacyProducer) FrostDrainPolicy() (tbtc.FrostDrainPolicy, error) {
	return eth.ReadFrostDrainPolicy(c.w.f.ctx, c.w.f.client, c.w.f.deployment.Bridge)
}
func (c *publicLegacyProducer) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	for _, host := range []*walletHost{c.w.a, c.w.b} {
		if host.label() == label {
			return host.desc.OutputKey, true, nil
		}
	}
	return [32]byte{}, false, fmt.Errorf("unknown FROST target")
}
func (c *publicLegacyProducer) PastMovingFundsCommitmentSubmittedEvents(filter *tbtc.MovingFundsCommitmentSubmittedEventFilter) ([]*tbtc.MovingFundsCommitmentSubmittedEvent, error) {
	opts := &bind.FilterOpts{Context: c.w.f.ctx}
	var wallets [][20]byte
	if filter != nil {
		opts.Start = filter.StartBlock
		opts.End = filter.EndBlock
		wallets = filter.WalletPublicKeyHash
	}
	it, err := c.w.legacy.FilterMovingFundsCommitmentSubmitted(opts, wallets)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var events []*tbtc.MovingFundsCommitmentSubmittedEvent
	for it.Next() {
		events = append(events, &tbtc.MovingFundsCommitmentSubmittedEvent{WalletPublicKeyHash: it.Event.WalletPubKeyHash, TargetWallets: it.Event.TargetWallets, BlockNumber: it.Event.Raw.BlockNumber})
	}
	return events, it.Error()
}
func (c *publicLegacyProducer) GetDepositMinAge() (uint32, error) { return 0, nil }
func (c *publicLegacyProducer) BuildDepositKey(hash bitcoin.Hash, index uint32) *big.Int {
	var at [4]byte
	binary.BigEndian.PutUint32(at[:], index)
	return new(big.Int).SetBytes(crypto.Keccak256(hash[:], at[:]))
}
func (c *publicLegacyProducer) ComputeMainUtxoHash(utxo *bitcoin.UnspentTransactionOutput) [32]byte {
	var index [4]byte
	var value [8]byte
	binary.BigEndian.PutUint32(index[:], utxo.Outpoint.OutputIndex)
	binary.BigEndian.PutUint64(value[:], uint64(utxo.Value))
	return crypto.Keccak256Hash(utxo.Outpoint.TransactionHash[:], index[:], value[:])
}
func (c *publicLegacyProducer) PastDepositRevealedEvents(filter *tbtc.DepositRevealedEventFilter) ([]*tbtc.DepositRevealedEvent, error) {
	opts := &bind.FilterOpts{Context: c.w.f.ctx}
	var wallets [][20]byte
	if filter != nil {
		opts.Start = filter.StartBlock
		opts.End = filter.EndBlock
		wallets = filter.WalletPublicKeyHash
	}
	it, err := c.w.legacy.FilterDepositRevealed(opts, nil, wallets)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var events []*tbtc.DepositRevealedEvent
	for it.Next() {
		events = append(events, &tbtc.DepositRevealedEvent{FundingTxHash: it.Event.FundingTxHash, FundingOutputIndex: it.Event.FundingOutputIndex, WalletPublicKeyHash: it.Event.WalletPubKeyHash, BlockNumber: it.Event.Raw.BlockNumber})
	}
	return events, it.Error()
}
func (c *publicLegacyProducer) GetDepositRequest(hash bitcoin.Hash, index uint32) (*tbtc.DepositChainRequest, bool, error) {
	v, err := c.w.legacy.Deposits(&bind.CallOpts{Context: c.w.f.ctx}, c.BuildDepositKey(hash, index))
	if err != nil {
		return nil, false, err
	}
	return &tbtc.DepositChainRequest{Amount: v.Amount, RevealedAt: time.Unix(int64(v.RevealedAt), 0), SweptAt: time.Unix(int64(v.SweptAt), 0)}, v.RevealedAt != 0, nil
}
func (c *publicLegacyProducer) GetOperatorID(address chain.Address) (chain.OperatorID, error) {
	if address != chain.Address(c.w.adminAddr.Hex()) {
		return 0, fmt.Errorf("not the fixture legacy member")
	}
	return 1, nil
}
func (c *publicLegacyProducer) SubmitMovingFundsCommitment(wallet [20]byte, main bitcoin.UnspentTransactionOutput, members []uint32, index uint32, targets [][20]byte) error {
	_, err := c.w.trySend(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.w.legacy.SubmitMovingFundsCommitment(o, wallet, legacyabi.BitcoinTxUTXO{TxHash: main.Outpoint.TransactionHash, TxOutputIndex: main.Outpoint.OutputIndex, TxOutputValue: uint64(main.Value)}, members, new(big.Int).SetUint64(uint64(index)), targets)
	})
	return err
}
func (c *publicLegacyProducer) ComputeMovingFundsCommitmentHash(targets [][20]byte) (hash [32]byte) {
	var raw []byte
	for _, label := range targets {
		raw = append(raw, label[:]...)
	}
	copy(hash[:], crypto.Keccak256(raw))
	return
}
func (c *publicLegacyProducer) ValidateMovingFundsProposal(wallet [20]byte, main *bitcoin.UnspentTransactionOutput, proposal *tbtc.MovingFundsProposal) error {
	ok, err := c.validator.ValidateMovingFundsProposal(&bind.CallOpts{Context: c.w.f.ctx}, legacyabi.WalletProposalValidatorMovingFundsProposal{WalletPubKeyHash: wallet, TargetWallets: proposal.TargetWallets, MovingFundsTxFee: proposal.MovingFundsTxFee}, legacyabi.BitcoinTxUTXO3{TxHash: main.Outpoint.TransactionHash, TxOutputIndex: main.Outpoint.OutputIndex, TxOutputValue: uint64(main.Value)})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("proposal refused")
	}
	return nil
}

func (w *k07World) produceLegacyDrain(lw legacyWallet) (*walletHost, int64) {
	w.t.Helper()
	validator, err := legacyabi.NewWalletProposalValidator(w.f.deployment.Legacy.Validator, w.f.client)
	if err != nil {
		w.t.Fatal(err)
	}
	c := &publicLegacyProducer{w: w, validator: validator}
	if count, err := c.GetLiveWalletsCount(); err != nil || count != 0 {
		w.t.Fatal("expected no legacy Live targets", err)
	}
	parameters, err := c.GetMovingFundsParameters()
	if err != nil {
		w.t.Fatal(err)
	}
	margin := uint64(24 * 3600)
	if uint64(parameters.Timeout)/2 < margin {
		margin = uint64(parameters.Timeout) / 2
	}
	w.warp(margin + 1)
	task := tbtcpg.NewMovingFundsTask(c, publicLegacyBitcoin{w.btc})
	if resetMode := os.Getenv("FROST_K09_CHECK_RESET"); resetMode != "" {
		controller := bind.NewBoundContract(w.f.deployment.Exposure, c10ABI, w.f.client, w.f.client, w.f.client)
		if resetMode == "partial-budget" {
			var out []interface{}
			if err := controller.Call(&bind.CallOpts{Context: w.f.ctx}, &out, "custodyReserved"); err != nil {
				w.t.Fatal(err)
			}
			reserved := out[0].(uint64)
			out = nil
			if err := controller.Call(&bind.CallOpts{Context: w.f.ctx}, &out, "issuanceReserved"); err != nil {
				w.t.Fatal(err)
			}
			issuance := out[0].(uint64)
			ceiling := reserved + lw.utxo.TxOutputValue - 1
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return controller.Transact(o, "openBudgetPeriod", ceiling, issuance, crypto.Keccak256Hash([]byte("host partial budget")))
			})
		} else {
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return controller.Transact(o, "contain", crypto.Keccak256Hash([]byte("host no-route recovery")))
			})
		}
		for i := 0; i < 3; i++ {
			delta := uint64(parameters.Timeout) - 1
			if i == 0 {
				delta = uint64(parameters.TimeoutResetDelay) + 1
			}
			w.warp(delta)
			before, err := c.GetWallet(lw.pkh)
			if err != nil {
				w.t.Fatal(err)
			}
			if proposal, valid, err := task.Run(&tbtc.CoordinationProposalRequest{WalletPublicKeyHash: lw.pkh}); err != nil || valid || proposal != nil {
				w.t.Fatal("host did not reset blocked drain", err)
			}
			after, err := c.GetWallet(lw.pkh)
			if err != nil || !after.MovingFundsRequestedAt.After(before.MovingFundsRequestedAt) || after.MainUtxoHash != before.MainUtxoHash {
				w.t.Fatal("host reset lost custody or did not advance", err)
			}
			if _, err := w.trySend(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return w.legacy.NotifyMovingFundsTimeout(o, lw.pkh, []uint32{1})
			}); err == nil || !strings.Contains(err.Error(), "Moving funds has not timed out yet") {
				w.t.Fatal("control HostTimeoutReason assertion: wrong timeout refusal after reset", err)
			}
		}
		if resetMode == "partial-budget" {
			var out []interface{}
			if err := controller.Call(&bind.CallOpts{Context: w.f.ctx}, &out, "custodyReserved"); err != nil {
				w.t.Fatal(err)
			}
			reserved := out[0].(uint64)
			out = nil
			if err := controller.Call(&bind.CallOpts{Context: w.f.ctx}, &out, "issuanceReserved"); err != nil {
				w.t.Fatal(err)
			}
			issuance := out[0].(uint64)
			ceiling := reserved + lw.utxo.TxOutputValue
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return controller.Transact(o, "openBudgetPeriod", ceiling, issuance, crypto.Keccak256Hash([]byte("host budget reopened")))
			})
		} else {
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return controller.Transact(o, "lift", crypto.Keccak256Hash([]byte("host recovery reopen")))
			})
		}
		w.warp(margin + 1)
	}
	operator := chain.Address(w.adminAddr.Hex())
	result, valid, err := task.Run(&tbtc.CoordinationProposalRequest{WalletPublicKeyHash: lw.pkh, WalletOperators: []chain.Address{operator}, ExecutingOperator: operator})
	if err != nil || !valid {
		w.t.Fatal("actual legacy drain producer failed", err)
	}
	proposal, ok := result.(*tbtc.MovingFundsProposal)
	if !ok || len(proposal.TargetWallets) != 1 {
		w.t.Fatal("unexpected legacy drain proposal")
	}
	for _, host := range []*walletHost{w.a, w.b} {
		if proposal.TargetWallets[0] == host.label() {
			w.t.Log("K09-PUBLIC:legacy-drain-committed")
			return host, proposal.MovingFundsTxFee.Int64()
		}
	}
	w.t.Fatal("producer chose a non-FROST target")
	return nil, 0
}

func (c *publicLegacyProducer) ResetLegacyMovingFundsTimeout(source [20]byte, main *bitcoin.UnspentTransactionOutput) error {
	return c.w.chain.ResetLegacyMovingFundsTimeout(source, main)
}
func TestLegacyHostResetsBlockedDrain(t *testing.T) {
	t.Setenv("FROST_K09_CHECK_RESET", "1")
	runPublicLegacyFlow(t, true)
}

func TestLegacyHostPrecutoverBudgetRecovery(t *testing.T) {
	for _, mode := range []uint8{0, 1} {
		t.Run(fmt.Sprintf("mode%d", mode), func(t *testing.T) {
			t.Setenv("FROST_K09_CHECK_RESET", "partial-budget")
			w := newK07World(t)
			if mode == 1 {
				w.send(func(o *bind.TransactOpts) (*types.Transaction, error) { return w.bridge.BeginLegacyDkgDrain(o) })
			}
			lw := w.legacyWallet(k07DrainValue)
			target, fee := w.produceLegacyDrain(lw)
			w.drain(lw, []*walletHost{target}, fee)
			w.mine(int(k06Finality))
			plan := w.incomingSweep(target)
			w.settle(target, plan, w.proofs.ProveIncomingSweep)
			record, err := w.legacy.Wallets(&bind.CallOpts{Context: w.f.ctx}, lw.pkh)
			if err != nil || record.MainUtxoHash != ([32]byte{}) {
				t.Fatal("pre-cutover reset/reopen drain did not clear source", err)
			}
			t.Log("K09-RECOVERY: pre-cutover host repeated verified-main resets, reopened budget and drained", mode)
		})
	}
}
