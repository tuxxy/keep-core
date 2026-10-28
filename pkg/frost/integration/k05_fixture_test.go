package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
	"github.com/keep-network/keep-core/pkg/operator"
)

// Anvil's public default development accounts 0 and 1. Account 0 deploys and
// is governance, SPV maintainer, depositor, redeemer and cancellation sponsor;
// account 1 is the treasury. These are public test keys only.
const (
	anvilKey0 = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
)

// Local K-05 test values. Every one is an OPEN owner decision (D-02, D-09).
const (
	k05DepositSats   = 1_000_000
	k05SweepFee      = 1_000
	k05Finality      = 2
	k05RoundBlocks   = 400
	k05FeeRate       = 2
	k05MaxRate       = 10
	k05Timeout       = 3600
	k05Confirmations = 1
	// k05CancelAfter is the RebroadcastThenCancel window in Ethereum blocks
	// after the timeout block and finality (D-09, OPEN).
	k05CancelAfter = 20
)

type k05World struct {
	t          *testing.T
	f          *fixture
	core       *coreChain
	btc        *coreSPV
	admin      *ecdsa.PrivateKey
	adminAddr  common.Address
	chain      *eth.FrostRedemptionChain
	bridge     *frostbridgeabi.FrostBridge
	bank       *bind.BoundContract
	miner      string
	records    []dkg.WalletRecord
	wallet     dkg.Descriptor
	walletID   [32]byte
	planner    redemption.Planner
	attesters  []*redemption.Attester
	resolvers  []*redemption.Resolver
	journals   []*hookedJournal
	launches   []string
	maintainer spv.FrostRedemptionMaintainer
	fromBlock  uint64
}

// hookedJournal is the real encrypted store with an optional observer on
// application-record retention (used to place a timeout exactly after every
// operator's worker returned its signature and before acceptance).
type hookedJournal struct {
	*store.Scoped
	mu     sync.Mutex
	onDone func(raw []byte)
	last   []byte
	// failRecord, when set, fails selected application-record writes before
	// they reach the store (simulating a crash or I/O error at that write).
	failRecord func(raw []byte) error
}

func (j *hookedJournal) RetainSigningRecord(ctx context.Context, id [32]byte, raw []byte) error {
	j.mu.Lock()
	fail := j.failRecord
	j.mu.Unlock()
	if fail != nil {
		if err := fail(raw); err != nil {
			return err
		}
	}
	if err := j.Scoped.RetainSigningRecord(ctx, id, raw); err != nil {
		return err
	}
	var done signing.Done
	if done.Unmarshal(raw) == nil && done.Signature != ([64]byte{}) {
		j.mu.Lock()
		hook := j.onDone
		j.last = append([]byte(nil), raw...)
		j.mu.Unlock()
		if hook != nil {
			hook(raw)
		}
	}
	return nil
}

func (j *hookedJournal) setOnDone(h func(raw []byte)) {
	j.mu.Lock()
	j.onDone = h
	j.mu.Unlock()
}

// lastDone returns the most recent durable Done record this operator retained.
func (j *hookedJournal) lastDone() []byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]byte(nil), j.last...)
}

func newK05World(t *testing.T) *k05World {
	t.Helper()
	return newK05WorldWith(t, nil)
}

// newK05WorldWith runs beforeDKG after deployment and before the DKG starts,
// so a test can wrap the executors (for example to choose the readiness
// certificate). nil is newK05World.
func newK05WorldWith(t *testing.T, beforeDKG func(*fixture)) *k05World {
	t.Helper()
	f := setupWith(t, "1,1,2", setupOptions{timeout: 15 * time.Minute, mode: "k05"})
	core := startCore(t, f.ctx)
	if beforeDKG != nil {
		beforeDKG(f)
	}
	records, errs := f.run()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("DKG operator %d: %v", i, err)
		}
	}
	// Deterministic Ethereum blocks from here on: transactions auto-mine and
	// the test mines finality blocks explicitly.
	f.stopMining()
	admin, _ := crypto.HexToECDSA(anvilKey0)
	w := &k05World{t: t, f: f, core: core, btc: &coreSPV{core: core, ctx: f.ctx}, admin: admin, adminAddr: crypto.PubkeyToAddress(admin.PublicKey), records: records, wallet: records[0].Descriptor, walletID: dkg.WalletID(records[0].Descriptor.OutputKey)}
	var err error
	if w.chain, err = eth.NewFrostRedemptionChain(f.ctx, f.client, admin, f.deployment.Bridge); err != nil {
		t.Fatal(err)
	}
	if w.bridge, err = frostbridgeabi.NewFrostBridge(f.deployment.Bridge, f.client); err != nil {
		t.Fatal(err)
	}
	bankABI, _ := abi.JSON(strings.NewReader(`[{"type":"function","name":"approveBalance","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"function","name":"balanceOf","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"}]`))
	w.bank = bind.NewBoundContract(f.deployment.Bank, bankABI, f.client, f.client, f.client)
	if err = core.call(f.ctx, "getnewaddress", nil, &w.miner); err != nil {
		t.Fatal(err)
	}
	w.t.Log("K-05: DKG complete; mining Core coinbase maturity")
	w.coreMine(101)
	w.t.Log("K-05: funding through C-05 deposit and threshold-signed sweep")
	w.fund()
	w.host()
	return w
}

func (w *k05World) rpc(method string, args ...any) {
	w.t.Helper()
	var out any
	if err := w.f.rpc.CallContext(w.f.ctx, &out, method, args...); err != nil {
		w.t.Fatal(method, err)
	}
}

// mine produces n Ethereum blocks.
func (w *k05World) mine(n int) {
	w.t.Helper()
	for i := 0; i < n; i++ {
		w.rpc("evm_mine")
	}
}

// warp advances Ethereum time by seconds and mines one block.
func (w *k05World) warp(seconds int64) {
	w.t.Helper()
	w.rpc("evm_increaseTime", seconds)
	w.rpc("evm_mine")
}

func (w *k05World) head() uint64 {
	w.t.Helper()
	h, err := w.chain.Head(w.f.ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	return h.Number
}

// coreMine mines in small batches: one large generate call can exceed the
// 10 s RPC client timeout on a loaded host.
func (w *k05World) coreMine(n int) {
	w.t.Helper()
	for n > 0 {
		batch := n
		if batch > 10 {
			batch = 10
		}
		if err := w.core.call(w.f.ctx, "generatetoaddress", []any{batch, w.miner}, nil); err != nil {
			w.t.Fatal(err)
		}
		n -= batch
	}
}

// send submits one admin transaction and requires a successful receipt.
func (w *k05World) send(call func(*bind.TransactOpts) (*types.Transaction, error)) *types.Receipt {
	w.t.Helper()
	receipt, err := w.trySend(call)
	if err != nil {
		w.t.Fatal(err)
	}
	return receipt
}

func (w *k05World) trySend(call func(*bind.TransactOpts) (*types.Transaction, error)) (*types.Receipt, error) {
	opts, err := bind.NewKeyedTransactorWithChainID(w.admin, new(big.Int).SetUint64(w.chain.ChainID()))
	if err != nil {
		return nil, err
	}
	opts.Context = w.f.ctx
	tx, err := call(opts)
	if err != nil {
		return nil, err
	}
	receipt, err := bind.WaitMined(w.f.ctx, w.f.client, tx)
	if err != nil {
		return nil, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return receipt, errors.New("transaction reverted")
	}
	return receipt, nil
}

func (w *k05World) balance(a common.Address) uint64 {
	w.t.Helper()
	var out []any
	if err := w.bank.Call(&bind.CallOpts{Context: w.f.ctx}, &out, "balanceOf", a); err != nil {
		w.t.Fatal(err)
	}
	return out[0].(*big.Int).Uint64()
}

func (w *k05World) approve(amount uint64) {
	w.t.Helper()
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bank.Transact(o, "approveBalance", w.f.deployment.Bridge, new(big.Int).SetUint64(amount))
	})
}

// signDigest is TEST-ONLY funding: the unguarded K-01 engine signs one
// BIP342 deposit-sweep digest with the real threshold worker so the wallet
// reaches funded custody through the actual C-05 reveal and sweep proof path.
// K-06 owns the authorized host sweep path; this is not K-05 signing evidence.
func (w *k05World) signDigest(digest [32]byte, label string) [64]byte {
	w.t.Helper()
	signatures := make([][64]byte, len(w.f.nodes))
	errs := make([]error, len(w.f.nodes))
	var wg sync.WaitGroup
	for i, node := range w.f.nodes {
		wg.Add(1)
		go func(i int, node nodeConfig) {
			defer wg.Done()
			journal := w.f.journals[i]
			engine, err := snowfallengine.New(node.worker)
			if err != nil {
				errs[i] = err
				return
			}
			key, err := journal.LoadKey(w.f.ctx)
			if err != nil {
				errs[i] = err
				return
			}
			status, err := journal.DKGStatus(w.f.ctx)
			if err != nil {
				errs[i] = err
				return
			}
			attempt, err := journal.Domain().NewAttempt("sign", sha256.Sum256([]byte("k05-test-funding/"+label)), 1)
			if err != nil {
				errs[i] = err
				return
			}
			request := w.f.executors[i].Request
			tr, err := transport.New(w.f.ctx, transport.Config{Domain: journal.Domain(), Attempt: attempt, ID: snowfallengine.AttemptID(attempt), LocalSeats: request.LocalSeats, Roster: request.Group.Roster, LocalPublicKey: operator.MarshalUncompressed(&node.private.PublicKey), Membership: node.membership, Network: node.network})
			if err != nil {
				errs[i] = err
				return
			}
			defer tr.Close()
			c := key.Candidate
			signatures[i], errs[i] = engine.Sign(w.f.ctx, frost.SigningRequest{Group: frost.Group{Roster: c.Roster, Threshold: c.Threshold, Quorum: uint16(len(c.Roster)), Epoch: c.Epoch}, LocalSeats: status.LocalSeats, Selected: c.Roster, Attempt: attempt, Key: key, Message: digest}, frost.Providers{Transport: tr, Store: journal, Acceptance: noAcceptance{}})
		}(i, node)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			w.t.Fatalf("test-only funding signer %d: %v", i, err)
		}
	}
	return signatures[0]
}

type noAcceptance struct{}

func (noAcceptance) WaitCandidateAcceptance(context.Context, frost.Candidate) (frost.Receipt, error) {
	return frost.Receipt{}, errors.New("signing cannot admit a DKG candidate")
}

func wireTx(t *testing.T, tx *bitcoin.Transaction) *wire.MsgTx {
	t.Helper()
	m := wire.NewMsgTx(2)
	if err := m.Deserialize(bytes.NewReader(tx.Serialize(bitcoin.Witness))); err != nil {
		t.Fatal(err)
	}
	return m
}

func rawTx(t *testing.T, m *wire.MsgTx) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := m.Serialize(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (w *k05World) coreTx(h bitcoin.Hash) *bitcoin.Transaction {
	w.t.Helper()
	raw, err := w.core.Transaction(w.f.ctx, h)
	if err != nil {
		w.t.Fatal(err)
	}
	tx := new(bitcoin.Transaction)
	if err = tx.Deserialize(raw); err != nil {
		w.t.Fatal(err)
	}
	return tx
}

// fund deposits into the C-05 template for the real DKG key, reveals it,
// sweeps it with a real threshold signature and proves the sweep, which makes
// the wallet Live with recognized main custody.
func (w *k05World) fund() {
	t, ctx := w.t, w.f.ctx
	descriptorHash, err := dkg.DescriptorHash(w.wallet)
	if err != nil {
		t.Fatal(err)
	}
	refundSecret := make([]byte, 32)
	refundSecret[31] = 9
	_, refundPub := btcec.PrivKeyFromBytes(refundSecret)
	latest, err := w.f.client.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	terms := frostbridgeabi.FrostDepositProfileTerms{ChainId: new(big.Int).SetUint64(w.chain.ChainID()), Bridge: w.f.deployment.Bridge, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FullWalletId: w.walletID, DescriptorHash: descriptorHash, Depositor: w.adminAddr, Blinding: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, Amount: k05DepositSats, RefundLocktime: uint32(latest.Time + 86400)}
	copy(terms.RefundKey[:], schnorr.SerializePubKey(refundPub))
	construction, err := w.bridge.FrostDepositConstruction(&bind.CallOpts{Context: ctx}, terms)
	if err != nil {
		t.Fatal("deposit construction (wallet must be ReadyUnfunded with capabilities):", err)
	}
	address, err := btcutil.NewAddressTaproot(construction.Output.Key[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	var txid string
	if err = w.core.call(ctx, "sendtoaddress", []any{address.EncodeAddress(), json.Number("0.01")}, &txid); err != nil {
		t.Fatal(err)
	}
	w.coreMine(1)
	fundingHash, _ := bitcoin.NewHashFromString(txid, bitcoin.ReversedByteOrder)
	funding := w.coreTx(fundingHash)
	depositScript := append([]byte{0x51, 0x20}, construction.Output.Key[:]...)
	index := -1
	for i, o := range funding.Outputs {
		if bytes.Equal(o.PublicKeyScript, depositScript) && o.Value == k05DepositSats {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("deposit output not found")
	}
	info := spv.TransactionInfo(funding)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.RevealFrostDeposit(o, terms, frostbridgeabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, uint32(index))
	})
	walletScript, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	sweep := wire.NewMsgTx(2)
	sweep.AddTxIn(&wire.TxIn{PreviousOutPoint: wire.OutPoint{Hash: [32]byte(fundingHash), Index: uint32(index)}, Sequence: 0xfffffffd})
	sweep.AddTxOut(wire.NewTxOut(k05DepositSats-k05SweepFee, walletScript))
	fetcher := txscript.NewCannedPrevOutputFetcher(depositScript, k05DepositSats)
	digest, err := txscript.CalcTapscriptSignaturehash(txscript.NewTxSigHashes(sweep, fetcher), txscript.SigHashDefault, sweep, 0, fetcher, txscript.NewBaseTapLeaf(construction.WalletLeaf))
	if err != nil {
		t.Fatal(err)
	}
	t.Log("K-05: deposit revealed; threshold-signing the sweep")
	signature := w.signDigest([32]byte(digest), "sweep")
	sweep.TxIn[0].Witness = wire.TxWitness{signature[:], construction.WalletLeaf, construction.WalletControl}
	if err = w.core.Broadcast(ctx, rawTx(t, sweep)); err != nil {
		t.Fatal("Core rejected the threshold-signed deposit sweep:", err)
	}
	w.coreMine(1)
	sweepHash := bitcoin.Hash(sweep.TxHash())
	tx, proof, err := bitcoin.AssembleSpvProof(sweepHash, k05Confirmations, w.btc)
	if err != nil {
		t.Fatal(err)
	}
	sweepInfo := spv.TransactionInfo(tx)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.SubmitFrostDepositSweepProof(o, frostbridgeabi.FrostDepositSweepSweep{WalletId: w.walletID, Transaction: frostbridgeabi.BitcoinTxInfo{Version: sweepInfo.Version, InputVector: sweepInfo.InputVector, OutputVector: sweepInfo.OutputVector, Locktime: sweepInfo.Locktime}, Proof: frostbridgeabi.BitcoinTxProof{MerkleProof: proof.MerkleProof, TxIndexInBlock: new(big.Int).SetUint64(uint64(proof.TxIndexInBlock)), BitcoinHeaders: proof.BitcoinHeaders, CoinbasePreimage: proof.CoinbasePreimage, CoinbaseProof: proof.CoinbaseProof}})
	})
	custody, err := w.chain.Custody(ctx, w.head(), w.walletID)
	if err != nil {
		t.Fatal(err)
	}
	treasuryFee := uint64(k05DepositSats / 100)
	if custody.Value != k05DepositSats-k05SweepFee || custody.MainOutpoint != redemption.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: sweepHash}) || custody.StateVersion != 1 || custody.Generation != 0 {
		t.Fatalf("funded custody %+v", custody)
	}
	if w.balance(w.adminAddr) != k05DepositSats-k05SweepFee-treasuryFee || w.balance(w.f.deployment.Treasury) != treasuryFee {
		t.Fatal("deposit credit or treasury fee")
	}
	w.fromBlock = w.head()
	t.Logf("funded FROST custody: Q=%x sweep=%s value=%d (real threshold BIP342 signature; test-only sweep authorization)", w.wallet.OutputKey, sweepHash.Hex(bitcoin.ReversedByteOrder), custody.Value)
}

// host builds each operator's K-05 components on its real store and worker.
func (w *k05World) host() {
	t := w.t
	w.planner = redemption.Planner{Chain: w.chain, Bitcoin: w.btc, Wallet: w.wallet, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Fees: redemption.FeePolicy{FeeRate: k05FeeRate, MaximumSatPerVByte: k05MaxRate, Minimum: 100}, DeadlineMargin: 600}
	w.maintainer = spv.FrostRedemptionMaintainer{Chain: w.chain, Bitcoin: w.btc, Confirmations: k05Confirmations, Wallet: w.walletID, FromBlock: w.fromBlock}
	for i, node := range w.f.nodes {
		i, node := i, node
		journal := &hookedJournal{Scoped: w.f.journals[i]}
		w.journals = append(w.journals, journal)
		worker, marker := observeWorkerLaunch(t, node.worker)
		w.launches = append(w.launches, marker)
		ledger := redemption.Ledger{Journal: journal}
		head := func(ctx context.Context) (uint64, error) {
			h, err := w.chain.Head(ctx)
			return h.Number, err
		}
		w.attesters = append(w.attesters, &redemption.Attester{Planner: w.planner, Journal: journal, Ledger: ledger, Key: node.key, Seats: w.f.executors[i].Request.LocalSeats, Reload: redemption.FreshWorkerReload(node.worker, journal, head)})
		w.resolvers = append(w.resolvers, &redemption.Resolver{Chain: w.chain, Ledger: ledger, Schedule: redemption.Schedule{FinalityBlocks: k05Finality, RoundBlocks: k05RoundBlocks, CancelAfterBlocks: k05CancelAfter}, Policy: redemption.RebroadcastThenCancel, Base: signing.Config{Journal: journal, Bitcoin: w.core, Worker: worker, MinimumConfirmations: 1, LocalRegtest: true, Transport: func(ctx context.Context, a frost.Attempt) (frost.Transport, func(), error) {
			request := w.f.executors[i].Request
			tr, err := transport.New(ctx, transport.Config{Domain: journal.Domain(), Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: request.LocalSeats, Roster: request.Group.Roster, LocalPublicKey: operator.MarshalUncompressed(&node.private.PublicKey), Membership: node.membership, Network: node.network})
			if err != nil {
				return nil, nil, err
			}
			return tr, tr.Close, nil
		}}})
	}
}

func (w *k05World) launchCount(i int) int {
	raw, err := os.ReadFile(w.launches[i])
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return strings.Count(string(raw), "start\n")
}

// request funds one FROST redemption from the admin Bank balance.
func (w *k05World) request(gross uint64, script []byte) [32]byte {
	w.t.Helper()
	w.approve(gross)
	receipt := w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.RequestFrostRedemption(o, w.walletID, script, gross, w.adminAddr)
	})
	for _, l := range receipt.Logs {
		if e, err := w.bridge.ParseFrostRedemptionRequested(*l); err == nil {
			return e.RequestId
		}
	}
	w.t.Fatal("no FrostRedemptionRequested log")
	return [32]byte{}
}

// admit discovers, plans, attests (every operator, fresh-worker reload first)
// and reserves the given pending requests, then mines to finality.
func (w *k05World) admit() (*signing.Plan, [32]byte) {
	w.t.Helper()
	ctx := w.f.ctx
	head := w.head()
	plan, _, err := w.proposal(head)
	if err != nil {
		w.t.Fatal(err)
	}
	var parts []map[uint16][]byte
	for _, a := range w.attesters {
		sigs, err := a.Attest(ctx, plan, head+100)
		if err != nil {
			w.t.Fatal("attest:", err)
		}
		parts = append(parts, sigs)
	}
	seats, signatures, err := redemption.Collect(w.wallet.Threshold, parts...)
	if err != nil {
		w.t.Fatal(err)
	}
	certificate, err := redemption.CertificateFor(plan, head+100)
	if err != nil {
		w.t.Fatal(err)
	}
	proposal := redemption.ReservationProposal{Certificate: certificate, Transaction: spv.TransactionInfo(plan.Snapshot().Unsigned()), Seats: seats, Signatures: signatures}
	for _, p := range plan.Snapshot().PreviousOutputs() {
		proposal.Inputs = append(proposal.Inputs, redemption.UTXO{TransactionHash: p.Outpoint.TransactionHash, OutputIndex: p.Outpoint.OutputIndex, Value: uint64(p.Value)})
	}
	for _, r := range plan.Requests() {
		proposal.Requests = append(proposal.Requests, redemption.ProposalRequest{ID: r.ID, Value: uint64(r.Value), Script: r.Script})
	}
	if err = w.chain.Reserve(ctx, proposal); err != nil {
		w.t.Fatal("reserve:", err)
	}
	admissions, err := w.chain.Admissions(ctx, w.walletID, w.fromBlock, w.head())
	if err != nil {
		w.t.Fatal(err)
	}
	var reservation [32]byte
	for _, a := range admissions {
		intent, err := w.chain.Intent(ctx, w.head(), a.ReservationID)
		if err == nil && intent.Certificate.IntentCommitment == plan.Commitment() {
			reservation = a.ReservationID
		}
	}
	if reservation == ([32]byte{}) {
		w.t.Fatal("admission log not found")
	}
	w.mine(k05Finality)
	return plan, reservation
}

func (w *k05World) proposal(head uint64) (*signing.Plan, []redemption.Request, error) {
	ids, err := w.planner.Pending(w.f.ctx, w.fromBlock, head, 0)
	if err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("no redeemable FROST requests")
	}
	return w.planner.Build(w.f.ctx, head, ids)
}

// signAll runs every operator's resolver signing step concurrently.
func (w *k05World) signAll(plan *signing.Plan, reservation [32]byte) []error {
	errs := make([]error, len(w.resolvers))
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(w.f.ctx, 90*time.Second)
	defer cancel()
	for i, r := range w.resolvers {
		wg.Add(1)
		go func(i int, r *redemption.Resolver) {
			defer wg.Done()
			_, errs[i] = r.Sign(ctx, plan, reservation)
		}(i, r)
	}
	wg.Wait()
	return errs
}

// cancel builds, attests, admits, signs, broadcasts and proves the exact
// cancellation of an admitted payment (fee 1,000 sat inside the envelope).
func (w *k05World) cancel(payment *signing.Plan, reservation [32]byte, fee int64) *signing.Plan {
	w.t.Helper()
	ctx := w.f.ctx
	unsigned := payment.Snapshot().Unsigned()
	var total int64
	for _, p := range payment.Snapshot().PreviousOutputs() {
		total += p.Value
	}
	script, _ := bitcoin.PayToTaproot(w.wallet.OutputKey)
	tx := &bitcoin.Transaction{Version: 2, Inputs: unsigned.Inputs, Outputs: []*bitcoin.TransactionOutput{{Value: total - fee, PublicKeyScript: script}}}
	plan, err := signing.NewCancellationPlan(payment, tx)
	if err != nil {
		w.t.Fatal(err)
	}
	validUntil := w.head() + 100
	var parts []map[uint16][]byte
	var auth redemption.CancellationAuthorization
	for _, a := range w.attesters {
		sigs, au, err := a.AttestCancellation(ctx, payment, plan, reservation, validUntil)
		if err != nil {
			w.t.Fatal("cancellation attest:", err)
		}
		parts, auth = append(parts, sigs), au
	}
	seats, signatures, err := redemption.Collect(w.wallet.Threshold, parts...)
	if err != nil {
		w.t.Fatal(err)
	}
	w.approve(uint64(fee))
	if err = w.chain.AuthorizeCancellation(ctx, auth, spv.TransactionInfo(plan.Snapshot().Unsigned()), seats, signatures); err != nil {
		w.t.Fatal("authorize cancellation:", err)
	}
	w.mine(k05Finality)
	for i, err := range w.signAll(plan, reservation) {
		if err != nil {
			w.t.Fatalf("cancellation signer %d: %v", i, err)
		}
	}
	if err = w.resolvers[0].Broadcast(ctx, plan, reservation); err != nil {
		w.t.Fatal("cancellation broadcast:", err)
	}
	w.coreMine(1)
	actions, err := w.maintainer.ProveSettlements(ctx)
	if err != nil || len(actions) != 1 || actions[0].Kind != "cancellation-proof" {
		w.t.Fatalf("cancellation proof: %+v %v", actions, err)
	}
	return plan
}

// coreSPV adapts the isolated regtest node to bitcoin.Chain for proof assembly
// and to redemption.ScriptUnspent for P2TR discovery by exact script.
type coreSPV struct {
	core *coreChain
	ctx  context.Context
}

var _ bitcoin.Chain = (*coreSPV)(nil)
var _ redemption.ScriptUnspent = (*coreSPV)(nil)

func (c *coreSPV) GetTransaction(h bitcoin.Hash) (*bitcoin.Transaction, error) {
	raw, err := c.core.Transaction(c.ctx, h)
	if err != nil {
		return nil, err
	}
	tx := new(bitcoin.Transaction)
	return tx, tx.Deserialize(raw)
}
func (c *coreSPV) GetTransactionConfirmations(ctx context.Context, h bitcoin.Hash) (uint, error) {
	return c.core.Confirmations(ctx, h)
}
func (c *coreSPV) BroadcastTransaction(tx *bitcoin.Transaction) error {
	return c.core.Broadcast(c.ctx, tx.Serialize(bitcoin.Witness))
}
func (c *coreSPV) GetLatestBlockHeight() (uint, error) {
	var n uint
	return n, c.core.call(c.ctx, "getblockcount", nil, &n)
}
func (c *coreSPV) blockHash(height uint) (string, error) {
	var h string
	return h, c.core.call(c.ctx, "getblockhash", []any{height}, &h)
}
func (c *coreSPV) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	h, err := c.blockHash(height)
	if err != nil {
		return nil, err
	}
	var text string
	if err = c.core.call(c.ctx, "getblockheader", []any{h, false}, &text); err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != bitcoin.BlockHeaderByteLength {
		return nil, fmt.Errorf("header bytes: %v", err)
	}
	header := new(bitcoin.BlockHeader)
	header.Deserialize([bitcoin.BlockHeaderByteLength]byte(raw))
	return header, nil
}
func (c *coreSPV) blockTxids(height uint) ([]string, error) {
	h, err := c.blockHash(height)
	if err != nil {
		return nil, err
	}
	var block struct{ Tx []string }
	return block.Tx, c.core.call(c.ctx, "getblock", []any{h, 1}, &block)
}
func (c *coreSPV) GetTransactionMerkleProof(h bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
	txids, err := c.blockTxids(height)
	if err != nil {
		return nil, err
	}
	target := h.Hex(bitcoin.ReversedByteOrder)
	position := -1
	level := make([][32]byte, len(txids))
	for i, id := range txids {
		raw, err := hex.DecodeString(id)
		if err != nil {
			return nil, err
		}
		for j := range raw {
			level[i][j] = raw[31-j]
		}
		if id == target {
			position = i
		}
	}
	if position < 0 {
		return nil, errors.New("transaction not in block")
	}
	proof := &bitcoin.TransactionMerkleProof{BlockHeight: height, Position: uint(position)}
	index := position
	for len(level) > 1 {
		if len(level)%2 == 1 {
			level = append(level, level[len(level)-1])
		}
		sibling := level[index^1]
		display := make([]byte, 32)
		for j := range display {
			display[j] = sibling[31-j]
		}
		proof.MerkleNodes = append(proof.MerkleNodes, hex.EncodeToString(display))
		next := make([][32]byte, len(level)/2)
		for i := range next {
			first := sha256.Sum256(append(append([]byte(nil), level[2*i][:]...), level[2*i+1][:]...))
			next[i] = sha256.Sum256(first[:])
		}
		level, index = next, index/2
	}
	return proof, nil
}
func (c *coreSPV) GetCoinbaseTxHash(height uint) (bitcoin.Hash, error) {
	txids, err := c.blockTxids(height)
	if err != nil || len(txids) == 0 {
		return bitcoin.Hash{}, fmt.Errorf("coinbase: %v", err)
	}
	return bitcoin.NewHashFromString(txids[0], bitcoin.ReversedByteOrder)
}
func (c *coreSPV) GetTransactionsForPublicKeyHash([20]byte, int) ([]*bitcoin.Transaction, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *coreSPV) GetTxHashesForPublicKeyHash([20]byte) ([]bitcoin.Hash, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *coreSPV) GetMempoolForPublicKeyHash([20]byte) ([]*bitcoin.Transaction, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *coreSPV) GetUtxosForPublicKeyHash([20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *coreSPV) GetMempoolUtxosForPublicKeyHash([20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *coreSPV) EstimateSatPerVByteFee(uint32) (int64, error) { return k05FeeRate, nil }

// UnspentByScript uses Core's scantxoutset on the exact raw script.
func (c *coreSPV) UnspentByScript(ctx context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	var result struct {
		Unspents []struct {
			Txid   string
			Vout   uint32
			Amount json.Number
		}
	}
	if err := c.core.call(ctx, "scantxoutset", []any{"start", []string{"raw(" + hex.EncodeToString(script) + ")"}}, &result); err != nil {
		return nil, err
	}
	var out []bitcoin.PreviousOutput
	for _, u := range result.Unspents {
		h, err := bitcoin.NewHashFromString(u.Txid, bitcoin.ReversedByteOrder)
		if err != nil {
			return nil, err
		}
		sats, err := satoshis(string(u.Amount))
		if err != nil {
			return nil, err
		}
		out = append(out, bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: h, OutputIndex: u.Vout}, Value: sats, PublicKeyScript: append(bitcoin.Script(nil), script...)})
	}
	return out, nil
}

// satoshis parses an exact decimal BTC amount without floating point.
func satoshis(btc string) (int64, error) {
	whole, frac, _ := strings.Cut(btc, ".")
	if len(frac) > 8 {
		return 0, errors.New("amount precision")
	}
	frac += strings.Repeat("0", 8-len(frac))
	v, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok || !v.IsInt64() {
		return 0, errors.New("amount")
	}
	return v.Int64(), nil
}
