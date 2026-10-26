package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

type k09LegacyOrigin struct {
	wallet     legacyWallet
	script     []byte
	requestKey *big.Int
	request    legacyabi.RedemptionRedemptionRequest
	timeout    uint32
}

// bootstrapPublicLegacy runs BEFORE any FROST Bridge state or DKG exists.
// Every legacy custody/obligation precursor is a public production call on
// the immutable baseline, and all Bitcoin spends use real tss-lib signatures.
func bootstrapPublicLegacy(t *testing.T, origin *k09LegacyOrigin) func(*fixture, string, string) *coreChain {
	return func(f *fixture, url, output string) *coreChain {
		t.Helper()
		if f.deployment.Epoch != 0 {
			t.Fatal("FROST epoch existed before legacy baseline precursor")
		}
		ctx := f.ctx
		core := startCore(t, ctx)
		f.core = core
		admin, err := crypto.HexToECDSA(anvilKey0)
		if err != nil {
			t.Fatal(err)
		}
		base := &k05World{t: t, f: f, core: core, btc: &coreSPV{core: core, ctx: ctx}, admin: admin, adminAddr: crypto.PubkeyToAddress(admin.PublicKey)}
		base.chain, err = eth.NewFrostRedemptionChain(ctx, f.client, admin, f.deployment.Bridge)
		if err != nil {
			t.Fatal(err)
		}
		base.bank = bind.NewBoundContract(f.deployment.Bank, mustABI(t, `[{"type":"function","name":"approveBalance","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"function","name":"balanceOf","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"}]`), f.client, f.client, f.client)
		w := &k07World{k06World: &k06World{k05World: base}}
		w.legacy, err = legacyabi.NewBridge(f.deployment.Bridge, f.client)
		if err != nil {
			t.Fatal(err)
		}
		if err = core.call(ctx, "getnewaddress", nil, &w.miner); err != nil {
			t.Fatal(err)
		}
		w.coreMine(101)
		key := w.drainSigner(map[string]any{})
		x, err := hex.DecodeString(key.PublicKeyX)
		if err != nil {
			t.Fatal(err)
		}
		y, err := hex.DecodeString(key.PublicKeyY)
		if err != nil {
			t.Fatal(err)
		}
		pkh, err := hex.DecodeString(key.PublicKeyHash)
		if err != nil {
			t.Fatal(err)
		}
		copy(origin.wallet.pkh[:], pkh)
		w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.RequestNewWallet(o, legacyabi.BitcoinTxUTXO{})
		})
		registry := bind.NewBoundContract(f.deployment.Legacy.Registry, mustABI(t, `[{"type":"function","name":"publish","inputs":[{"type":"bytes32"},{"type":"bytes32"},{"type":"bytes32"}],"outputs":[],"stateMutability":"nonpayable"}]`), f.client, f.client, f.client)
		w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
			return registry.Transact(o, "publish", crypto.Keccak256Hash([]byte("K09 public legacy origin")), common.BytesToHash(x), common.BytesToHash(y))
		})
		deposit := &tbtc.Deposit{Depositor: chain.Address(w.adminAddr.Hex()), WalletPublicKeyHash: origin.wallet.pkh, BlindingFactor: [8]byte{9, 26}, RefundPublicKeyHash: [20]byte{9}}
		binary.LittleEndian.PutUint32(deposit.RefundLocktime[:], uint32(time.Now().Unix()+16*86400))
		script, err := deposit.Script()
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(script)
		address, err := btcutil.NewAddressWitnessScriptHash(hash[:], &chaincfg.RegressionNetParams)
		if err != nil {
			t.Fatal(err)
		}
		const fundingValue = k07DrainValue + k07DrainFee
		var txid string
		if err = core.call(ctx, "sendtoaddress", []any{address.EncodeAddress(), json.Number(fmt.Sprintf("%d.%08d", fundingValue/100_000_000, fundingValue%100_000_000))}, &txid); err != nil {
			t.Fatal(err)
		}
		w.coreMine(1)
		txHash, err := bitcoin.NewHashFromString(txid, bitcoin.ReversedByteOrder)
		if err != nil {
			t.Fatal(err)
		}
		funding := w.coreTx(txHash)
		locking := append([]byte{0, 32}, hash[:]...)
		index := -1
		for i, out := range funding.Outputs {
			if bytes.Equal(out.PublicKeyScript, locking) && out.Value == fundingValue {
				index = i
			}
		}
		if index < 0 {
			t.Fatal("Core did not create the exact legacy deposit")
		}
		reveal := legacyabi.DepositDepositRevealInfo{FundingOutputIndex: uint32(index), BlindingFactor: deposit.BlindingFactor, WalletPubKeyHash: deposit.WalletPublicKeyHash, RefundPubKeyHash: deposit.RefundPublicKeyHash, RefundLocktime: deposit.RefundLocktime}
		info := spv.TransactionInfo(funding)
		w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.RevealDeposit(o, legacyabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, reveal)
		})
		if w.balance(w.adminAddr) != 0 {
			t.Fatal("reveal created Bank credit")
		}
		signed := w.drainSigner(map[string]any{"Funding": hex.EncodeToString(funding.Serialize()), "OutputIndex": uint32(index), "Deposit": deposit, "Fee": k07DrainFee})
		raw, err := hex.DecodeString(signed.Transaction)
		if err != nil {
			t.Fatal(err)
		}
		sweep := new(bitcoin.Transaction)
		if err = sweep.Deserialize(raw); err != nil {
			t.Fatal(err)
		}
		if err = core.Broadcast(ctx, sweep.Serialize()); err != nil {
			t.Fatal("Core refused real legacy deposit sweep:", err)
		}
		w.coreMine(1)
		proved, proof, err := bitcoin.AssembleSpvProof(sweep.Hash(), 1, w.btc)
		if err != nil {
			t.Fatal(err)
		}
		info = spv.TransactionInfo(proved)
		w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.SubmitDepositSweepProof(o, legacyabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, legacyProof(proof), legacyabi.BitcoinTxUTXO{}, common.Address{})
		})
		origin.wallet.funding = sweep
		origin.wallet.utxo = legacyabi.BitcoinTxUTXO{TxHash: sweep.Hash(), TxOutputIndex: 0, TxOutputValue: uint64(sweep.Outputs[0].Value)}
		if origin.wallet.utxo.TxOutputValue != k07DrainValue {
			t.Fatal("legacy sweep did not establish expected main")
		}
		const escrow = 20_000
		w.approve(escrow)
		origin.script = append([]byte{22, 0, 20}, deposit.RefundPublicKeyHash[:]...)
		w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
			return w.legacy.RequestRedemption(o, origin.wallet.pkh, origin.wallet.utxo, origin.script, escrow)
		})
		origin.requestKey = new(big.Int).SetBytes(crypto.Keccak256(crypto.Keccak256(origin.script), origin.wallet.pkh[:]))
		opts := &bind.CallOpts{Context: ctx}
		origin.request, err = w.legacy.PendingRedemptions(opts, origin.requestKey)
		if err != nil {
			t.Fatal(err)
		}
		if origin.request.RequestedAmount != escrow || origin.request.RequestedAt == 0 || w.balance(f.deployment.Bridge) != escrow {
			t.Fatal("public legacy escrow missing")
		}
		parameters, err := w.legacy.RedemptionParameters(opts)
		if err != nil {
			t.Fatal(err)
		}
		origin.timeout = parameters.RedemptionTimeout
		before, err := w.legacy.Wallets(opts, origin.wallet.pkh)
		if err != nil {
			t.Fatal(err)
		}
		credit, treasury := w.balance(w.adminAddr), w.balance(f.deployment.Treasury)
		t.Log("K09-PUBLIC:public-legacy-funded")
		_, file, _, _ := runtime.Caller(0)
		command := exec.CommandContext(ctx, "node", filepath.Join(filepath.Dir(file), "../../../test/frost/k09-finish.cjs"), url, os.Getenv("FROST_K03_TBTC"), output, "0x"+hex.EncodeToString(chaincfg.RegressionNetParams.GenesisHash[:]))
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("baseline upgrade and FROST configuration: %v\n%s", err, out)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &f.deployment); err != nil {
			t.Fatal(err)
		}
		after, err := w.legacy.Wallets(opts, origin.wallet.pkh)
		if err != nil {
			t.Fatal(err)
		}
		request, err := w.legacy.PendingRedemptions(opts, origin.requestKey)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) || request != origin.request || w.balance(w.adminAddr) != credit || w.balance(f.deployment.Treasury) != treasury || w.balance(f.deployment.Bridge) != escrow {
			t.Fatal("upgrade changed actual legacy custody, liabilities or credits")
		}
		t.Log("K09-PUBLIC:legacy-upgraded")
		return core
	}
}

func legacyProof(proof *bitcoin.SpvProof) legacyabi.BitcoinTxProof {
	return legacyabi.BitcoinTxProof{MerkleProof: proof.MerkleProof, TxIndexInBlock: big.NewInt(int64(proof.TxIndexInBlock)), BitcoinHeaders: proof.BitcoinHeaders, CoinbasePreimage: proof.CoinbasePreimage, CoinbaseProof: proof.CoinbaseProof}
}

func TestPublicLegacyUpgradeCutoverDrainAndSweep(t *testing.T) { runPublicLegacyFlow(t, false) }

func runPublicLegacyFlow(t *testing.T, remote bool) *k07World {
	var origin k09LegacyOrigin
	w := newK07WorldPrepared(t, "k09-public", bootstrapPublicLegacy(t, &origin), remote)
	ctx := w.f.ctx
	opts := &bind.CallOpts{Context: ctx}
	request, err := w.legacy.PendingRedemptions(opts, origin.requestKey)
	if err != nil || request != origin.request {
		t.Fatal("legacy original request/deadline did not survive DKG", err)
	}
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) { return w.bridge.BeginLegacyDkgDrain(o) })
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.CompleteFrostCutover(o, crypto.Keccak256Hash([]byte("K09 public legacy cutover; OPEN")))
	})
	// Original legacy timeout is still enforced after cutover; the exact
	// refund then puts this publicly funded legacy wallet into MovingFunds.
	before, err := w.legacy.Wallets(opts, origin.wallet.pkh)
	if err != nil {
		t.Fatal(err)
	}
	credit, treasury, escrow := w.balance(w.adminAddr), w.balance(w.f.deployment.Treasury), w.balance(w.f.deployment.Bridge)
	if _, err = w.trySend(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.NotifyRedemptionTimeout(o, origin.wallet.pkh, []uint32{1}, origin.script)
	}); err == nil || !strings.Contains(err.Error(), "Redemption request has not timed out") {
		t.Fatal("expected original legacy timeout guard:", err)
	}
	after, err := w.legacy.Wallets(opts, origin.wallet.pkh)
	if err != nil || !reflect.DeepEqual(before, after) || w.balance(w.adminAddr) != credit || w.balance(w.f.deployment.Treasury) != treasury || w.balance(w.f.deployment.Bridge) != escrow {
		t.Fatal("early legacy timeout changed financial/custody frame", err)
	}
	deadline := uint64(origin.request.RequestedAt) + uint64(origin.timeout) + 1
	now, err := w.chain.Timestamp(ctx, w.head())
	if err != nil || now >= deadline {
		t.Fatal("fixture already crossed original deadline", err)
	}
	w.warp(deadline - now)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.NotifyRedemptionTimeout(o, origin.wallet.pkh, []uint32{1}, origin.script)
	})
	if w.balance(w.adminAddr) != credit+escrow || w.balance(w.f.deployment.Bridge) != 0 || w.balance(w.f.deployment.Treasury) != treasury {
		t.Fatal("legacy original timeout lost escrow")
	}
	w.commitDrain(origin.wallet, []*walletHost{w.a})
	controller := bind.NewBoundContract(w.f.deployment.Exposure, c10ABI, w.f.client, w.f.client, w.f.client)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return controller.Transact(o, "contain", crypto.Keccak256Hash([]byte("K09 public legacy containment; OPEN")))
	})
	t.Log("K09-PUBLIC:legacy-cutover-contained")
	drain := w.drain(origin.wallet, []*walletHost{w.a})
	key := movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: drain.Hash()})
	incoming, err := w.chain.Incoming(ctx, w.head(), key)
	if err != nil || incoming.State != movement.IncomingPending || incoming.Target != w.a.id {
		t.Fatal("public legacy drain did not create exact FROST incoming obligation", err)
	}
	w.mine(int(k06Finality))
	var fee uint64
	if remote {
		fee = waitRemoteIncoming(w, origin.wallet.utxo.TxOutputValue-k07DrainFee)
	} else {
		sweep := w.incomingSweep(w.a)
		w.settle(w.a, sweep, w.proofs.ProveIncomingSweep)
		fee = uint64(sweep.Snapshot().Fee())
	}
	custody := w.custody(w.a.id)
	if custody.Value != origin.wallet.utxo.TxOutputValue-k07DrainFee-fee {
		t.Fatal("public cross-scheme custody/value conservation")
	}
	incoming, err = w.chain.Incoming(ctx, w.head(), key)
	if err != nil || incoming.State != movement.IncomingSwept {
		t.Fatal("incoming obligation not consumed", err)
	}
	mode, err := w.bridge.FrostCreationMode(opts)
	if err != nil || mode != 2 {
		t.Fatal("creation mode rolled back", err)
	}
	t.Log("K09-PUBLIC:public-frost-drain-swept")
	return w
}
