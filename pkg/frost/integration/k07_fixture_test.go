package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
	"github.com/keep-network/keep-core/pkg/operator"
)

// K-07 local test values (D-09, OPEN).
const (
	k07DrainValue = 1_000_000
	k07DrainFee   = 2_000
)

// walletHost is one FROST wallet's host components on every operator.
type walletHost struct {
	desc     dkg.Descriptor
	id       [32]byte
	journals []*hookedJournal
	locks    []*deposit.MainLock
	movers   []*movement.Mover
	planners []movement.Planner
	commits  []movement.Commitments
	closures []movement.Closure
}

func (h *walletHost) label() [20]byte { return movement.Label(h.id) }

// k07World is the K-06 world (wallet A funded only by sweeps) plus a second
// real DKG wallet B on the same operators, the K-07 host on both, and a legacy
// ECDSA wallet whose operators are the tss-lib fixture key shares.
type k07World struct {
	*k06World
	a, b   *walletHost
	view   *movement.View
	proofs spv.FrostMovementMaintainer
	legacy *legacyabi.Bridge
	// Optional public-observation hook for real-node interruption tests. It
	// never receives a node store, worker or component method.
	observeIncoming func()
}

func newK07World(t *testing.T) *k07World {
	return newK07WorldPrepared(t, "k07", nil)
}

func newK07WorldPrepared(t *testing.T, mode string, prepare func(*fixture, string, string) *coreChain, remote ...bool) *k07World {
	t.Helper()
	var bJournals []*store.Scoped
	var bRequests []frost.DKGRequest
	var bRecord dkg.WalletRecord
	k6 := newK06WorldWith(t, k06Options{mode: mode, prepare: prepare, remote: len(remote) > 0 && remote[0], afterDKG: func(f *fixture) {
		aJournals, aExecutors, aEpoch := f.journals, f.executors, f.deployment.Epoch
		t.Log("K-07: second DKG for wallet B")
		f.nextEpoch()
		records, errs := f.run()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("wallet B DKG operator %d: %v", i, err)
			}
		}
		bJournals, bRecord = f.journals, records[0]
		for _, e := range f.executors {
			bRequests = append(bRequests, e.Request)
		}
		f.journals, f.executors, f.deployment.Epoch = aJournals, aExecutors, aEpoch
	}})
	w := &k07World{k06World: k6}
	w.view = &movement.View{Chain: k6.chain, Policy: movement.Policy{FinalityBlocks: k06Finality}}
	w.proofs = spv.FrostMovementMaintainer{Chain: k6.chain, Bitcoin: k6.btc, Confirmations: 1}
	var err error
	if w.legacy, err = legacyabi.NewBridge(k6.f.deployment.Bridge, k6.f.client); err != nil {
		t.Fatal(err)
	}
	var aRequests []frost.DKGRequest
	for _, e := range k6.f.executors {
		aRequests = append(aRequests, e.Request)
	}
	w.a = w.host(k6.wallet, k6.journals, aRequests, k6.locks)
	var hooked []*hookedJournal
	for _, j := range bJournals {
		hooked = append(hooked, &hookedJournal{Scoped: j})
	}
	w.b = w.host(bRecord.Descriptor, hooked, bRequests, nil)
	return w
}

func (w *k07World) host(desc dkg.Descriptor, journals []*hookedJournal, requests []frost.DKGRequest, locks []*deposit.MainLock) *walletHost {
	t := w.t
	h := &walletHost{desc: desc, id: dkg.WalletID(desc.OutputKey), journals: journals}
	for i, node := range w.f.nodes {
		i, node := i, node
		journal := journals[i]
		lock := &deposit.MainLock{Journal: journal}
		if locks != nil {
			lock = locks[i]
		}
		h.locks = append(h.locks, lock)
		worker, _ := observeWorkerLaunch(t, node.worker)
		request := requests[i]
		base := signing.Config{Journal: journal, Bitcoin: w.core, Worker: worker, MinimumConfirmations: 1, LocalRegtest: true, Transport: func(ctx context.Context, a frost.Attempt) (frost.Transport, func(), error) {
			tr, err := transport.New(ctx, transport.Config{Domain: journal.Domain(), Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: request.LocalSeats, Roster: request.Group.Roster, LocalPublicKey: operator.MarshalUncompressed(&node.private.PublicKey), Membership: node.membership, Network: node.network})
			if err != nil {
				return nil, nil, err
			}
			return tr, tr.Close, nil
		}}
		planner := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
		h.planners = append(h.planners, planner)
		h.movers = append(h.movers, &movement.Mover{Chain: w.chain, Authority: &movement.Authority{View: w.view}, Lock: lock, Ledger: redemption.Ledger{Journal: journal}, Schedule: redemption.Schedule{FinalityBlocks: k06Finality, RoundBlocks: k06RoundBlocks}, Wallet: desc, Base: base})
		own, err := eth.NewFrostRedemptionChain(w.f.ctx, w.f.client, node.key, w.f.deployment.Bridge)
		if err != nil {
			t.Fatal(err)
		}
		h.commits = append(h.commits, movement.Commitments{Chain: own, Planner: planner, Seat: request.LocalSeats[0] - 1})
		h.closures = append(h.closures, movement.Closure{Chain: w.chain, Policy: w.view.Policy, Journal: journal})
	}
	return h
}

// mround mines until the movers' current round anchors at or after block
// after and is final.
func (w *k07World) mround(h *walletHost, after uint64) movement.Round {
	w.t.Helper()
	for {
		r, err := h.movers[0].Current(w.f.ctx)
		if err != nil {
			w.t.Fatal(err)
		}
		if r.Anchor >= after {
			return r
		}
		w.mine(1)
	}
}

// signAll runs every operator's mover for one round concurrently.
func (w *k07World) signAll(h *walletHost, plan *signing.Plan, number uint64) []error {
	errs := make([]error, len(h.movers))
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(w.f.ctx, 120*time.Second)
	defer cancel()
	for i, m := range h.movers {
		wg.Add(1)
		go func(i int, m *movement.Mover) {
			defer wg.Done()
			_, errs[i] = m.Sign(ctx, plan, number)
		}(i, m)
	}
	wg.Wait()
	return errs
}

// plans derives a plan on every operator and requires them equal.
func (w *k07World) plans(h *walletHost, build func(movement.Planner) (*signing.Plan, error)) *signing.Plan {
	w.t.Helper()
	var first *signing.Plan
	for i, p := range h.planners {
		plan, err := build(p)
		if err != nil {
			w.t.Fatalf("operator %d plan: %v", i, err)
		}
		if first != nil && plan.Commitment() != first.Commitment() {
			w.t.Fatal("independent movement planning is not deterministic")
		}
		first = plan
	}
	return first
}

// settle signs plan in the next round on every operator, broadcasts it, mines
// it and proves it once through the maintainer.
func (w *k07World) settle(h *walletHost, plan *signing.Plan, prove func(context.Context, [32]byte, [32]byte) (spv.FrostAction, error)) {
	t := w.t
	t.Helper()
	r := w.mround(h, w.head())
	for i, err := range w.signAll(h, plan, r.Number) {
		if err != nil {
			t.Fatalf("%s signer %d: %v", plan.Purpose(), i, err)
		}
	}
	if err := h.movers[1].Broadcast(w.f.ctx, plan); err != nil {
		t.Fatal("broadcast:", err)
	}
	w.coreMine(1)
	action, err := prove(w.f.ctx, h.id, plan.BitcoinTransactionHash())
	if err != nil || action.Kind == "already-proven" {
		t.Fatalf("first proof: %+v %v", action, err)
	}
	again, err := prove(w.f.ctx, h.id, plan.BitcoinTransactionHash())
	if err != nil || again.Kind != "already-proven" {
		t.Fatalf("a second proof must not resubmit: %+v %v", again, err)
	}
	w.mine(int(k06Finality))
	for i, m := range h.movers {
		status, err := m.Resolve(w.f.ctx, plan)
		if err != nil || status != movement.StatusSettled {
			t.Fatalf("operator %d outcome: %s %v", i, status, err)
		}
	}
}

func (w *k07World) custody(id [32]byte) redemption.Custody {
	w.t.Helper()
	c, err := w.chain.Custody(w.f.ctx, w.head(), id)
	if err != nil {
		w.t.Fatal(err)
	}
	return c
}

func (w *k07World) state(id [32]byte) movement.State {
	w.t.Helper()
	wallet, err := w.chain.Wallet(w.f.ctx, w.head(), movement.Label(id))
	if err != nil {
		w.t.Fatal(err)
	}
	return wallet.State
}

func (w *k07World) obligations(id [32]byte) uint64 {
	w.t.Helper()
	open, err := w.chain.Obligations(w.f.ctx, w.head(), id)
	if err != nil {
		w.t.Fatal(err)
	}
	return open
}

// --- The legacy ECDSA wallet (V-08 reading 6) ---

type drainResult struct {
	PublicKeyX, PublicKeyY, PublicKeyHash, Transaction string
}

// drainSigner runs pkg/tbtc's TestK07DrainSigner: real tss-lib signing over
// the pre-generated key shares. Without funding it returns the key only.
func (w *k07World) drainSigner(request map[string]any) drainResult {
	t := w.t
	t.Helper()
	dir := t.TempDir()
	request["Result"] = filepath.Join(dir, "result.json")
	raw, _ := json.Marshal(request)
	path := filepath.Join(dir, "request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	cmd := exec.CommandContext(w.f.ctx, "go", "test", "./pkg/tbtc", "-run", "^TestK07DrainSigner$", "-count=1")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "FROST_K07_DRAIN_REQUEST="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("legacy tss-lib signer: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(request["Result"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var result drainResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

type legacyWallet struct {
	pkh     [20]byte
	funding *bitcoin.Transaction
	index   uint32
	utxo    legacyabi.BitcoinTxUTXO
}

func (w *k07World) setStorage(slot *big.Int, value common.Hash) {
	w.rpc("anvil_setStorageAt", w.f.deployment.Bridge, common.BigToHash(slot).Hex(), value.Hex())
}

func (w *k07World) storage(slot *big.Int) *big.Int {
	w.t.Helper()
	raw, err := w.f.client.StorageAt(w.f.ctx, w.f.deployment.Bridge, common.BigToHash(slot), nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return new(big.Int).SetBytes(raw)
}

// writeField sets one packed field (controlled marker only).
func (w *k07World) writeField(base *big.Int, f storageField, value *big.Int) {
	slot := new(big.Int).Add(base, big.NewInt(int64(f.Slot)))
	word := w.storage(slot)
	shift := uint(f.Offset * 8)
	mask := new(big.Int).Lsh(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Bytes*8)), big.NewInt(1)), shift)
	word.AndNot(word, mask)
	word.Or(word, new(big.Int).Lsh(value, shift))
	w.setStorage(slot, common.BigToHash(word))
}

// legacyWallet registers the tss-lib fixture key as a legacy wallet through
// the real registry callback, pays it a Core main UTXO, and sets the legacy
// main UTXO hash and MovingFunds as controlled markers (its legacy deposit
// and sweep history is out of scope).
func (w *k07World) legacyWallet(value int64) legacyWallet {
	t, ctx := w.t, w.f.ctx
	t.Helper()
	layout := w.f.deployment.Legacy
	if layout == nil {
		t.Fatal("mode k07 deployment without the legacy layout")
	}
	key := w.drainSigner(map[string]any{})
	x, _ := hex.DecodeString(key.PublicKeyX)
	y, _ := hex.DecodeString(key.PublicKeyY)
	var lw legacyWallet
	pkh, _ := hex.DecodeString(key.PublicKeyHash)
	copy(lw.pkh[:], pkh)
	registryABI := `[{"type":"function","name":"publish","inputs":[{"type":"bytes32"},{"type":"bytes32"},{"type":"bytes32"}],"outputs":[],"stateMutability":"nonpayable"}]`
	registry := bind.NewBoundContract(layout.Registry, mustABI(t, registryABI), w.f.client, w.f.client, w.f.client)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return registry.Transact(o, "publish", crypto.Keccak256Hash([]byte("K-07 legacy wallet")), common.BytesToHash(x), common.BytesToHash(y))
	})
	address, err := btcutil.NewAddressWitnessPubKeyHash(lw.pkh[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	var txid string
	if err = w.core.call(ctx, "sendtoaddress", []any{address.EncodeAddress(), json.Number(fmt.Sprintf("%d.%08d", value/100_000_000, value%100_000_000))}, &txid); err != nil {
		t.Fatal(err)
	}
	w.coreMine(1)
	hash, _ := bitcoin.NewHashFromString(txid, bitcoin.ReversedByteOrder)
	lw.funding = w.coreTx(hash)
	script, _ := bitcoin.PayToWitnessPublicKeyHash(lw.pkh)
	for i, o := range lw.funding.Outputs {
		if string(o.PublicKeyScript) == string(script) {
			lw.index = uint32(i)
		}
	}
	lw.utxo = legacyabi.BitcoinTxUTXO{TxHash: hash, TxOutputIndex: lw.index, TxOutputValue: uint64(value)}
	base := new(big.Int).SetBytes(crypto.Keccak256(common.RightPadBytes(lw.pkh[:], 32), common.LeftPadBytes(big.NewInt(int64(layout.RegisteredWallets)).Bytes(), 32)))
	mainHash := crypto.Keccak256Hash(hash[:], big.NewInt(int64(lw.index)).FillBytes(make([]byte, 4)), new(big.Int).SetUint64(uint64(value)).FillBytes(make([]byte, 8)))
	w.writeField(base, layout.MainUtxoHash, mainHash.Big())
	head, _ := w.chain.Timestamp(ctx, w.head())
	w.writeField(base, layout.MovingFundsRequestedAt, new(big.Int).SetUint64(head))
	w.writeField(base, layout.State, big.NewInt(2)) // legacy MovingFunds
	live := big.NewInt(int64(layout.LiveWalletsCount.Slot))
	count := new(big.Int).Rsh(w.storage(live), uint(layout.LiveWalletsCount.Offset*8))
	count.And(count, big.NewInt(0xffffffff))
	w.writeField(big.NewInt(0), layout.LiveWalletsCount, count.Sub(count, big.NewInt(1)))
	return lw
}

func mustABI(t *testing.T, raw string) abi.ABI {
	t.Helper()
	a, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// commitDrain commits the legacy wallet to targets (as its member, the
// admin) and returns the committed labels.
func (w *k07World) commitDrain(lw legacyWallet, targets []*walletHost) [][20]byte {
	w.t.Helper()
	var labels [][20]byte
	for _, h := range targets {
		labels = append(labels, h.label())
	}
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsCommitment(o, lw.pkh, lw.utxo, []uint32{1}, big.NewInt(0), labels)
	})
	return labels
}

// drain signs the committed drain to targets with tss-lib, broadcasts and
// mines it, and proves it through the legacy moving funds proof.
func (w *k07World) drain(lw legacyWallet, targets []*walletHost, feeOverride ...int64) *bitcoin.Transaction {
	fee := int64(k07DrainFee)
	if len(feeOverride) > 0 {
		fee = feeOverride[0]
	}
	t, ctx := w.t, w.f.ctx
	t.Helper()
	var keys []string
	var hexLabels []string
	for _, h := range targets {
		label := h.label()
		hexLabels = append(hexLabels, hex.EncodeToString(label[:]))
		keys = append(keys, hex.EncodeToString(h.desc.OutputKey[:]))
	}
	result := w.drainSigner(map[string]any{"Funding": hex.EncodeToString(lw.funding.Serialize()), "OutputIndex": lw.index, "Targets": hexLabels, "Keys": keys, "Fee": fee})
	raw, err := hex.DecodeString(result.Transaction)
	if err != nil {
		t.Fatal(err)
	}
	tx := new(bitcoin.Transaction)
	if err = tx.Deserialize(raw); err != nil {
		t.Fatal(err)
	}
	if err = w.core.Broadcast(ctx, tx.Serialize()); err != nil {
		t.Fatal("Core refused the tss-lib drain:", err)
	}
	w.coreMine(1)
	proved, proof, err := bitcoin.AssembleSpvProof(tx.Hash(), 1, w.btc)
	if err != nil {
		t.Fatal(err)
	}
	info := spv.TransactionInfo(proved)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitMovingFundsProof(o, legacyabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, legacyabi.BitcoinTxProof{MerkleProof: proof.MerkleProof, TxIndexInBlock: big.NewInt(int64(proof.TxIndexInBlock)), BitcoinHeaders: proof.BitcoinHeaders, CoinbasePreimage: proof.CoinbasePreimage, CoinbaseProof: proof.CoinbaseProof}, lw.utxo, lw.pkh)
	})
	return tx
}

// movementLostReply reports a lost reply after an incoming sweep proof was
// mined.
type movementLostReply struct{ movement.Chain }

func (l movementLostReply) SubmitIncomingSweepProof(ctx context.Context, wallet [32]byte, p movement.Proof) error {
	if err := l.Chain.SubmitIncomingSweepProof(ctx, wallet, p); err != nil {
		return err
	}
	return errors.New("simulated lost reply after the incoming sweep proof was mined")
}
