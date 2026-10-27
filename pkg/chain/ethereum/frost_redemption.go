package ethereum

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/keep-network/keep-common/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	legacy "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// FrostRedemptionChain is the K-05 C-04 seam over an explicit Bridge address.
// It is constructed separately from the legacy TbtcChain and enables nothing.
// Reads take an explicit block number; writes wait for the mined receipt and
// treat a revert as an error.
type FrostRedemptionChain struct {
	client     *ethclient.Client
	bridge     *frostbridgeabi.FrostBridge
	legacy     *legacy.Bridge
	address    common.Address
	key        *ecdsa.PrivateKey
	chainID    *big.Int
	txMu       sync.Mutex
	sharedTxMu *sync.Mutex
}

// WithTransactionMutex joins adapters using the same Ethereum signer. Configure
// it before concurrent use; the lock covers nonce selection through receipt.
func (c *FrostRedemptionChain) WithTransactionMutex(mu *sync.Mutex) *FrostRedemptionChain {
	c.sharedTxMu = mu
	return c
}

func NewFrostRedemptionChain(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, bridge common.Address) (*FrostRedemptionChain, error) {
	if client == nil || bridge == (common.Address{}) {
		return nil, errors.New("invalid FROST redemption chain configuration")
	}
	b, err := frostbridgeabi.NewFrostBridge(bridge, client)
	if err != nil {
		return nil, err
	}
	l, err := legacy.NewBridge(bridge, client)
	if err != nil {
		return nil, err
	}
	id, err := client.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	if !id.IsUint64() {
		return nil, errors.New("chain ID width")
	}
	return &FrostRedemptionChain{client: client, bridge: b, legacy: l, address: bridge, key: key, chainID: id}, nil
}

// TxProofDifficultyFactor is the Bridge's actual SPV work multiplier.
func (c *FrostRedemptionChain) TxProofDifficultyFactor() (*big.Int, error) {
	return c.legacy.TxProofDifficultyFactor(&bind.CallOpts{})
}

func (c *FrostRedemptionChain) ChainID() uint64  { return c.chainID.Uint64() }
func (c *FrostRedemptionChain) Bridge() [20]byte { return c.address }

func (c *FrostRedemptionChain) at(ctx context.Context, n uint64) *bind.CallOpts {
	return &bind.CallOpts{Context: ctx, BlockNumber: new(big.Int).SetUint64(n)}
}

func (c *FrostRedemptionChain) Head(ctx context.Context) (redemption.Head, error) {
	h, err := c.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return redemption.Head{}, err
	}
	return redemption.Head{Number: h.Number.Uint64(), Hash: h.Hash()}, nil
}

func (c *FrostRedemptionChain) HeaderHash(ctx context.Context, n uint64) ([32]byte, error) {
	h, err := c.client.HeaderByNumber(ctx, new(big.Int).SetUint64(n))
	if err != nil {
		return [32]byte{}, err
	}
	return h.Hash(), nil
}

func (c *FrostRedemptionChain) Timestamp(ctx context.Context, n uint64) (uint64, error) {
	h, err := c.client.HeaderByNumber(ctx, new(big.Int).SetUint64(n))
	if err != nil {
		return 0, err
	}
	return h.Time, nil
}

func (c *FrostRedemptionChain) Configuration(ctx context.Context, at uint64) (redemption.Configuration, error) {
	r, err := c.bridge.FrostRedemptionConfiguration(c.at(ctx, at))
	if err != nil {
		return redemption.Configuration{}, err
	}
	return redemption.Configuration{BitcoinGenesis: r.BitcoinGenesis, FinalityBlocks: r.FinalityBlocks, MaximumCertificateBlocks: r.MaximumCertificateBlocks, Enabled: r.Enabled, Watchtower: r.Watchtower}, nil
}

func (c *FrostRedemptionChain) MaximumTotalFee(ctx context.Context, at uint64) (uint64, error) {
	r, err := c.legacy.RedemptionParameters(c.at(ctx, at))
	if err != nil {
		return 0, err
	}
	return r.RedemptionTxMaxTotalFee, nil
}

func (c *FrostRedemptionChain) Request(ctx context.Context, at uint64, id [32]byte) (redemption.Request, error) {
	o := c.at(ctx, at)
	r, err := c.bridge.FrostRedemptionRequest(o, id)
	if err != nil {
		return redemption.Request{}, err
	}
	if r.State == 0 {
		return redemption.Request{}, redemption.ErrNotFound
	}
	p, err := c.bridge.FrostRedemptionPolicy(o, id)
	if err != nil {
		return redemption.Request{}, err
	}
	return redemption.Request{ID: id, WalletID: r.WalletId, DescriptorHash: r.DescriptorHash, ReservationID: r.ReservationId, Redeemer: r.Redeemer, Gross: r.Gross, TreasuryFee: r.TreasuryFee, MaximumBitcoinFee: r.MaximumBitcoinFee, Deadline: r.Deadline, FinalityBlocks: r.FinalityBlocks, TimeoutBlock: r.TimeoutBlock, State: redemption.RequestState(r.State), Script: append([]byte(nil), r.Script...), NotBefore: p.NotBefore}, nil
}

func (c *FrostRedemptionChain) Intent(ctx context.Context, at uint64, id [32]byte) (redemption.Intent, error) {
	r, err := c.bridge.FrostReservation(c.at(ctx, at), id)
	if err != nil {
		return redemption.Intent{}, err
	}
	if r.State == 0 {
		return redemption.Intent{}, redemption.ErrNotFound
	}
	k := r.Certificate
	out := redemption.Intent{ID: id, Certificate: redemption.Certificate{WalletID: k.WalletId, DescriptorHash: k.DescriptorHash, Generation: k.Generation, StateVersion: k.StateVersion, IntentCommitment: k.IntentCommitment, RequestIDsHash: k.RequestIdsHash, ConflictTxHash: k.ConflictTxHash, ConflictOutputIndex: k.ConflictOutputIndex, MinimumFee: k.MinimumFee, MaximumFee: k.MaximumFee, MaximumSatPerVByte: k.MaximumSatPerVByte, ValidUntilBlock: k.ValidUntilBlock}, AdmittedBlock: r.AdmittedBlock, Snapshot: r.Snapshot, TransactionHash: r.TransactionHash, State: redemption.IntentState(r.State), RequestIDs: append([][32]byte(nil), r.RequestIds...), Payouts: append([]uint64(nil), r.Payouts...), Seats: append([]uint16(nil), r.Seats...), ReservedNet: r.ReservedNet}
	for _, u := range r.Inputs {
		out.Inputs = append(out.Inputs, redemption.UTXO{TransactionHash: u.TxHash, OutputIndex: u.TxOutputIndex, Value: u.TxOutputValue})
	}
	return out, nil
}

func (c *FrostRedemptionChain) Custody(ctx context.Context, at uint64, wallet [32]byte) (redemption.Custody, error) {
	r, err := c.bridge.FrostWalletCustody(c.at(ctx, at), wallet)
	if err != nil {
		return redemption.Custody{}, err
	}
	return redemption.Custody{Generation: r.Generation, StateVersion: r.StateVersion, Value: r.Value, MainOutpoint: r.MainOutpoint, ActiveReservation: r.ActiveReservation, CancellationExposure: r.CancellationExposure}, nil
}

func (c *FrostRedemptionChain) CustodyOutput(ctx context.Context, at uint64, key [32]byte) (redemption.CustodyOutput, error) {
	r, err := c.bridge.FrostCustodyOutput(c.at(ctx, at), key)
	if err != nil {
		return redemption.CustodyOutput{}, err
	}
	return redemption.CustodyOutput{WalletID: r.WalletId, Value: r.Value, Spent: r.Spent}, nil
}

func (c *FrostRedemptionChain) Alternatives(ctx context.Context, at uint64, id [32]byte) ([]redemption.Alternative, error) {
	o := c.at(ctx, at)
	n, err := c.bridge.FrostCancellationCount(o, id)
	if err != nil {
		return nil, err
	}
	if !n.IsUint64() || n.Uint64() > 1024 {
		return nil, errors.New("cancellation history bound")
	}
	var out []redemption.Alternative
	for i := uint64(0); i < n.Uint64(); i++ {
		h, err := c.bridge.FrostCancellationHash(o, id, new(big.Int).SetUint64(i))
		if err != nil {
			return nil, err
		}
		r, err := c.bridge.FrostCancellationAlternative(o, id, h)
		if err != nil {
			return nil, err
		}
		out = append(out, redemption.Alternative{TransactionHash: r.TransactionHash, Snapshot: r.Snapshot, Fee: r.Fee, Sponsor: r.Sponsor, AdmittedBlock: r.AdmittedBlock, ValidUntilBlock: r.ValidUntilBlock, Seats: append([]uint16(nil), r.Seats...), BudgetSettled: r.BudgetSettled})
	}
	return out, nil
}

func (c *FrostRedemptionChain) filter(ctx context.Context, from, to uint64) *bind.FilterOpts {
	return &bind.FilterOpts{Context: ctx, Start: from, End: &to}
}

func (c *FrostRedemptionChain) RequestEvents(ctx context.Context, wallet [32]byte, from, to uint64) ([]redemption.RequestEvent, error) {
	it, err := c.bridge.FilterFrostRedemptionRequested(c.filter(ctx, from, to), nil, [][32]byte{wallet})
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []redemption.RequestEvent
	for it.Next() {
		if it.Event.Raw.Removed {
			continue
		}
		out = append(out, redemption.RequestEvent{RequestID: it.Event.RequestId, WalletID: it.Event.WalletId, Block: it.Event.Raw.BlockNumber})
	}
	return out, it.Error()
}

func (c *FrostRedemptionChain) Admissions(ctx context.Context, wallet [32]byte, from, to uint64) ([]redemption.Admission, error) {
	it, err := c.bridge.FilterFrostReservationAdmitted(c.filter(ctx, from, to), nil, [][32]byte{wallet})
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []redemption.Admission
	for it.Next() {
		if it.Event.Raw.Removed {
			continue
		}
		out = append(out, redemption.Admission{ReservationID: it.Event.ReservationId, TransactionHash: it.Event.TransactionHash, Block: it.Event.Raw.BlockNumber, BlockHash: it.Event.Raw.BlockHash})
	}
	return out, it.Error()
}

// Admission returns the admission log of one reservation. The log's block hash
// is compared with the canonical header by the caller.
func (c *FrostRedemptionChain) Admission(ctx context.Context, id [32]byte) (redemption.Admission, error) {
	it, err := c.bridge.FilterFrostReservationAdmitted(&bind.FilterOpts{Context: ctx}, [][32]byte{id}, nil)
	if err != nil {
		return redemption.Admission{}, err
	}
	defer it.Close()
	var found []redemption.Admission
	for it.Next() {
		if !it.Event.Raw.Removed {
			found = append(found, redemption.Admission{ReservationID: id, TransactionHash: it.Event.TransactionHash, Block: it.Event.Raw.BlockNumber, BlockHash: it.Event.Raw.BlockHash})
		}
	}
	if err = it.Error(); err != nil {
		return redemption.Admission{}, err
	}
	if len(found) != 1 {
		return redemption.Admission{}, fmt.Errorf("%w: %d admission logs", redemption.ErrNotFound, len(found))
	}
	return found[0], nil
}

func (c *FrostRedemptionChain) CancellationAdmission(ctx context.Context, id, hash [32]byte) (redemption.Admission, error) {
	it, err := c.bridge.FilterFrostCancellationAuthorized(&bind.FilterOpts{Context: ctx}, [][32]byte{id})
	if err != nil {
		return redemption.Admission{}, err
	}
	defer it.Close()
	var found []redemption.Admission
	for it.Next() {
		if !it.Event.Raw.Removed && it.Event.TransactionHash == hash {
			found = append(found, redemption.Admission{ReservationID: id, TransactionHash: hash, Block: it.Event.Raw.BlockNumber, BlockHash: it.Event.Raw.BlockHash})
		}
	}
	if err = it.Error(); err != nil {
		return redemption.Admission{}, err
	}
	if len(found) != 1 {
		return redemption.Admission{}, fmt.Errorf("%w: %d cancellation logs", redemption.ErrNotFound, len(found))
	}
	return found[0], nil
}

func (c *FrostRedemptionChain) send(ctx context.Context, call func(*bind.TransactOpts) (*types.Transaction, error)) error {
	if c.key == nil {
		return errors.New("read-only FROST redemption chain")
	}
	mu := &c.txMu
	if c.sharedTxMu != nil {
		mu = c.sharedTxMu
	}
	mu.Lock()
	defer mu.Unlock()
	opts, err := bind.NewKeyedTransactorWithChainID(c.key, c.chainID)
	if err != nil {
		return err
	}
	opts.Context = ctx
	tx, err := call(opts)
	if err != nil {
		return err
	}
	receipt, err := bind.WaitMined(ctx, c.client, tx)
	if err != nil {
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("FROST redemption transaction %s reverted", tx.Hash())
	}
	return nil
}

func info(i redemption.BitcoinInfo) frostbridgeabi.BitcoinTxInfo {
	return frostbridgeabi.BitcoinTxInfo{Version: i.Version, InputVector: i.InputVector, OutputVector: i.OutputVector, Locktime: i.Locktime}
}
func proof(p redemption.BitcoinProof) frostbridgeabi.BitcoinTxProof {
	return frostbridgeabi.BitcoinTxProof{MerkleProof: p.MerkleProof, TxIndexInBlock: new(big.Int).SetUint64(p.TxIndexInBlock), BitcoinHeaders: p.BitcoinHeaders, CoinbasePreimage: p.CoinbasePreimage, CoinbaseProof: p.CoinbaseProof}
}

func (c *FrostRedemptionChain) Reserve(ctx context.Context, p redemption.ReservationProposal) error {
	k := p.Certificate
	in := frostbridgeabi.FrostRedemptionStateReservationProposal{Certificate: frostbridgeabi.FrostReservationCertificateReservation{WalletId: k.WalletID, DescriptorHash: k.DescriptorHash, Generation: k.Generation, StateVersion: k.StateVersion, IntentCommitment: k.IntentCommitment, RequestIdsHash: k.RequestIDsHash, ConflictTxHash: k.ConflictTxHash, ConflictOutputIndex: k.ConflictOutputIndex, MinimumFee: k.MinimumFee, MaximumFee: k.MaximumFee, MaximumSatPerVByte: k.MaximumSatPerVByte, ValidUntilBlock: k.ValidUntilBlock}, Transaction: info(p.Transaction), Seats: p.Seats, Signatures: p.Signatures}
	for _, u := range p.Inputs {
		in.Inputs = append(in.Inputs, frostbridgeabi.BitcoinTxUTXO{TxHash: u.TransactionHash, TxOutputIndex: u.OutputIndex, TxOutputValue: u.Value})
	}
	for _, r := range p.Requests {
		in.Requests = append(in.Requests, frostbridgeabi.FrostSigningIntentRequest{Id: r.ID, Value: r.Value, Script: r.Script})
	}
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) { return c.bridge.ReserveFrostRedemption(o, in) })
}

func (c *FrostRedemptionChain) AuthorizeCancellation(ctx context.Context, a redemption.CancellationAuthorization, tx redemption.BitcoinInfo, seats []uint16, signatures []byte) error {
	auth := frostbridgeabi.FrostCancellationCertificateAuthorization{ReservationId: a.ReservationID, Snapshot: a.Snapshot, TransactionHash: a.TransactionHash, Fee: a.Fee, ValidUntilBlock: a.ValidUntilBlock}
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.AuthorizeFrostCancellation(o, auth, info(tx), seats, signatures)
	})
}

func (c *FrostRedemptionChain) SubmitPaymentProof(ctx context.Context, id [32]byte, tx redemption.BitcoinInfo, p redemption.BitcoinProof) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostRedemptionProof(o, id, info(tx), proof(p))
	})
}

func (c *FrostRedemptionChain) SubmitCancellationProof(ctx context.Context, id [32]byte, tx redemption.BitcoinInfo, p redemption.BitcoinProof) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.SubmitFrostCancellationProof(o, id, info(tx), proof(p))
	})
}

func (c *FrostRedemptionChain) NotifyTimeout(ctx context.Context, id [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.NotifyFrostRedemptionTimeout(o, id)
	})
}

func (c *FrostRedemptionChain) RefundUnreserved(ctx context.Context, id [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.RefundUnreservedFrostRedemption(o, id)
	})
}

func (c *FrostRedemptionChain) RefundResolved(ctx context.Context, id [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.RefundResolvedFrostRedemption(o, id)
	})
}

func (c *FrostRedemptionChain) ClaimCancellationBudget(ctx context.Context, id, hash [32]byte) error {
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.ClaimFrostCancellationBudget(o, id, hash)
	})
}

var _ redemption.Chain = (*FrostRedemptionChain)(nil)

// ConnectFrostRedemption dials the configured node and binds the configured
// Bridge for the K-05 maintainer. It does not touch the legacy tBTC chain.
func ConnectFrostRedemption(ctx context.Context, config ethereum.Config) (*FrostRedemptionChain, error) {
	client, err := ethclient.Dial(config.URL)
	if err != nil {
		return nil, err
	}
	key, err := decryptKey(config)
	if err != nil {
		return nil, err
	}
	bridge, err := config.ContractAddress(BridgeContractName)
	if err != nil {
		return nil, err
	}
	return NewFrostRedemptionChain(ctx, client, key.PrivateKey, bridge)
}

func (c *FrostRedemptionChain) ResetLegacyMovingFundsTimeout(source [20]byte, main *bitcoin.UnspentTransactionOutput) error {
	if main == nil || main.Outpoint == nil || main.Value <= 0 {
		return errors.New("legacy recovery needs current main")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return c.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
		return c.bridge.ResetLegacyMovingFundsTimeout(o, source, frostbridgeabi.BitcoinTxUTXO{TxHash: main.Outpoint.TransactionHash, TxOutputIndex: main.Outpoint.OutputIndex, TxOutputValue: uint64(main.Value)})
	})
}
