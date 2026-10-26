package integration

import (
	"bytes"
	"context"
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
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

// Local K-06 test values. Every one is an OPEN owner decision (D-04, D-09).
const (
	k06Finality     = 2
	k06RoundBlocks  = 10
	k06RefundMargin = 3600
	k06FeeRate      = 2
	k06MaxRate      = 10
	k06RefundDelay  = 86400
)

// mtpClock is Bitcoin Core's median-time-past with an explicit test override:
// the controlled provider that crosses the refund margin between two exact
// steps (H-03). Without an override it is Core's own value.
type mtpClock struct {
	core     *coreChain
	mu       sync.Mutex
	override uint64
}

func (c *mtpClock) MedianTimePast(ctx context.Context) (uint64, error) {
	c.mu.Lock()
	o := c.override
	c.mu.Unlock()
	if o != 0 {
		return o, nil
	}
	var info struct{ Mediantime uint64 }
	err := c.core.call(ctx, "getblockchaininfo", nil, &info)
	return info.Mediantime, err
}
func (c *mtpClock) set(v uint64) { c.mu.Lock(); c.override = v; c.mu.Unlock() }

type k06World struct {
	*k05World
	clock      *mtpClock
	view       *deposit.View
	locks      []*deposit.MainLock
	sweepers   []*deposit.Sweeper
	resolution []*deposit.ResolutionAttester
	sweeps     spv.FrostSweepMaintainer
	refundKey  *btcec.PrivateKey
	blinding   byte
	// unreservedReveals leaves new deposits without a C-10 reservation, for
	// the tests that reserve them explicitly.
	unreservedReveals bool
}

// newK06World is the K-05 world without its test-only funding: a real DKG
// wallet, ReadyUnfunded, with every K-06 host component on each operator's
// real store and worker. Custody is funded only by K-06 sweeps.
func newK06World(t *testing.T) *k06World {
	return newK06WorldWith(t, k06Options{mode: "k05"})
}

// k06Options lets K-07 deploy its mode and run more DKGs while blocks are
// still mined; afterDKG must leave f.journals and f.executors on the first
// wallet, which this world wires.
type k06Options struct {
	remote    bool
	prepare   func(*fixture, string, string) *coreChain
	mode      string
	beforeDKG func(f *fixture)
	afterDKG  func(f *fixture)
}

func newK06WorldWith(t *testing.T, options k06Options) *k06World {
	t.Helper()
	timeout := 20 * time.Minute
	if options.afterDKG != nil {
		timeout = 30 * time.Minute
	}
	var core *coreChain
	setup := setupOptions{timeout: timeout, mode: options.mode, remote: options.remote}
	if options.prepare != nil {
		setup.prepare = func(f *fixture, url, output string) { core = options.prepare(f, url, output) }
	}
	f := setupWith(t, "1,1,2", setup)
	if options.beforeDKG != nil {
		options.beforeDKG(f)
	}
	if core == nil {
		core = startCore(t, f.ctx)
	}
	records, errs := f.run()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("DKG operator %d: %v", i, err)
		}
	}
	if options.afterDKG != nil {
		options.afterDKG(f)
	}
	f.stopMining()
	admin, _ := crypto.HexToECDSA(anvilKey0)
	k5 := &k05World{t: t, f: f, core: core, btc: &coreSPV{core: core, ctx: f.ctx}, admin: admin, adminAddr: crypto.PubkeyToAddress(admin.PublicKey), records: records, wallet: records[0].Descriptor, walletID: dkg.WalletID(records[0].Descriptor.OutputKey)}
	var err error
	if k5.chain, err = eth.NewFrostRedemptionChain(f.ctx, f.client, admin, f.deployment.Bridge); err != nil {
		t.Fatal(err)
	}
	if k5.bridge, err = frostbridgeabi.NewFrostBridge(f.deployment.Bridge, f.client); err != nil {
		t.Fatal(err)
	}
	bankABI, _ := abi.JSON(strings.NewReader(`[{"type":"function","name":"approveBalance","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},{"type":"function","name":"balanceOf","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"}]`))
	k5.bank = bind.NewBoundContract(f.deployment.Bank, bankABI, f.client, f.client, f.client)
	if err = core.call(f.ctx, "getnewaddress", nil, &k5.miner); err != nil {
		t.Fatal(err)
	}
	t.Log("K-06: DKG complete; mining Core coinbase maturity")
	k5.coreMine(101)
	k5.fromBlock = k5.head()
	k5.host()
	refund := make([]byte, 32)
	refund[31] = 9
	refundKey, _ := btcec.PrivKeyFromBytes(refund)
	w := &k06World{k05World: k5, clock: &mtpClock{core: core}, refundKey: refundKey}
	w.view = &deposit.View{Chain: k5.chain, Clock: w.clock, Policy: deposit.Policy{FinalityBlocks: k06Finality, RefundMargin: k06RefundMargin}}
	for i := range f.nodes {
		lock := &deposit.MainLock{Journal: k5.journals[i]}
		w.locks = append(w.locks, lock)
		// K-05 components take the K-06 main purpose lock.
		k5.attesters[i].MainLock = lock
		k5.resolvers[i].Base.Guard = deposit.Guard{Lock: lock}
		base := k5.resolvers[i].Base
		base.Guard = nil
		planner := deposit.Planner{Chain: k5.chain, Bitcoin: k5.btc, Funding: core, Lock: lock, Wallet: k5.wallet, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Policy: w.view.Policy, FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
		w.sweepers = append(w.sweepers, &deposit.Sweeper{Chain: k5.chain, Planner: planner, Authority: &deposit.Authority{View: w.view}, Lock: lock, Ledger: redemption.Ledger{Journal: k5.journals[i]}, Schedule: redemption.Schedule{FinalityBlocks: k06Finality, RoundBlocks: k06RoundBlocks}, Base: base})
		w.resolution = append(w.resolution, &deposit.ResolutionAttester{Chain: k5.chain, Lock: lock, Authority: &deposit.Authority{View: w.view}, Key: f.nodes[i].key, Seats: f.executors[i].Request.LocalSeats, Reload: k5.attesters[i].Reload})
	}
	w.sweeps = spv.FrostSweepMaintainer{Chain: k5.chain, Bitcoin: k5.btc, Confirmations: 1, Funding: core, Clock: w.clock, RefundMargin: k06RefundMargin}
	if os.Getenv("FROST_K06_CONTROL_FAULT") == "proof-rpc" {
		// Controls-harness self-test only: an outage unrelated to any guard.
		w.sweeps.Chain = proofOutage{k5.chain}
	}
	return w
}

// terms is the deposit an honest depositor would request, with a fresh
// blinding value so every deposit has its own output.
func (w *k06World) terms(amount uint64, locktime uint32, vault [20]byte) deposit.Terms {
	w.t.Helper()
	descriptorHash, err := dkg.DescriptorHash(w.wallet)
	if err != nil {
		w.t.Fatal(err)
	}
	w.blinding++
	t := deposit.Terms{ChainID: new(big.Int).SetUint64(w.chain.ChainID()), Bridge: w.f.deployment.Bridge, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), WalletID: w.walletID, DescriptorHash: descriptorHash, Depositor: w.adminAddr, Vault: vault, Blinding: [8]byte{0x06, w.blinding}, Amount: amount, RefundLocktime: locktime}
	copy(t.RefundKey[:], schnorr.SerializePubKey(w.refundKey.PubKey()))
	return t
}

func abiTerms(t deposit.Terms) frostbridgeabi.FrostDepositProfileTerms {
	return frostbridgeabi.FrostDepositProfileTerms{ChainId: t.ChainID, Bridge: common.Address(t.Bridge), BitcoinGenesis: t.BitcoinGenesis, FullWalletId: t.WalletID, DescriptorHash: t.DescriptorHash, Depositor: common.Address(t.Depositor), Vault: common.Address(t.Vault), Blinding: t.Blinding, Amount: t.Amount, RefundKey: t.RefundKey, RefundLocktime: t.RefundLocktime, ExtraData: t.ExtraData}
}

// sameConstruction requires the Go template to equal the C-05 contract's.
func (w *k06World) sameConstruction(t deposit.Terms, c deposit.Construction, contract frostbridgeabi.FrostDepositAdmissionConstruction) {
	w.t.Helper()
	if contract.Output.Key != c.OutputKey || contract.Output.Root != c.Root || contract.Output.Parity != c.Parity || contract.Receipt != c.Receipt || !bytes.Equal(contract.WalletLeaf, c.WalletLeaf) || !bytes.Equal(contract.RefundLeaf, c.RefundLeaf) || !bytes.Equal(contract.WalletControl, c.WalletControl) || !bytes.Equal(contract.RefundControl, c.RefundControl) {
		w.t.Fatalf("Go and C-05 deposit constructions differ:\nGo  %+v\nC05 %+v", c, contract)
	}
}

type funded struct {
	terms        deposit.Terms
	construction deposit.Construction
	funding      bitcoin.TransactionOutpoint
	key          [32]byte
	revealBlock  uint64
	// readyBlock holds both the reveal and the C-10 reservation: a sweep
	// plan anchored finality blocks later may include the deposit.
	readyBlock uint64
}

// deposit pays the Go-built deposit address from Core, mines it, reveals it to
// C-05 and checks the Go and contract constructions are byte-identical.
func (w *k06World) deposit(amount uint64, locktime uint32, vault [20]byte) funded {
	t, ctx := w.t, w.f.ctx
	t.Helper()
	terms := w.terms(amount, locktime, vault)
	c, err := deposit.Build(terms, w.wallet.OutputKey)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := w.bridge.FrostDepositConstruction(&bind.CallOpts{Context: ctx}, abiTerms(terms))
	if err != nil {
		t.Fatal("C-05 deposit construction:", err)
	}
	w.sameConstruction(terms, c, contract)
	address, err := btcutil.NewAddressTaproot(c.OutputKey[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	var txid string
	if err = w.core.call(ctx, "sendtoaddress", []any{address.EncodeAddress(), json.Number(fmt.Sprintf("%d.%08d", amount/100_000_000, amount%100_000_000))}, &txid); err != nil {
		t.Fatal(err)
	}
	w.coreMine(1)
	hash, _ := bitcoin.NewHashFromString(txid, bitcoin.ReversedByteOrder)
	tx := w.coreTx(hash)
	index := -1
	for i, o := range tx.Outputs {
		if bytes.Equal(o.PublicKeyScript, c.Script()) && o.Value == int64(amount) {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("deposit output not found")
	}
	info := spv.TransactionInfo(tx)
	receipt := w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.bridge.RevealFrostDeposit(o, abiTerms(terms), frostbridgeabi.BitcoinTxInfo{Version: info.Version, InputVector: info.InputVector, OutputVector: info.OutputVector, Locktime: info.Locktime}, uint32(index))
	})
	funding := bitcoin.TransactionOutpoint{TransactionHash: hash, OutputIndex: uint32(index)}
	f := funded{terms: terms, construction: c, funding: funding, key: deposit.OutpointKey(funding), revealBlock: receipt.BlockNumber.Uint64()}
	f.readyBlock = f.revealBlock
	// C-10: the planner sweeps only reserved deposits; reserve through the
	// maintainer once the funding is confirmed, as a host does.
	if !w.unreservedReveals {
		if action, err := w.sweeps.ReserveDeposit(ctx, f.key); err != nil || action.Kind != "deposit-reservation" {
			t.Fatalf("deposit reservation: %+v %v", action, err)
		}
		f.readyBlock = w.head()
	}
	return f
}

// locktime is a refund time the given seconds after the current Ethereum time.
func (w *k06World) locktime(seconds uint64) uint32 {
	w.t.Helper()
	h := w.head()
	now, err := w.chain.Timestamp(w.f.ctx, h)
	if err != nil {
		w.t.Fatal(err)
	}
	return uint32(now + seconds)
}

// round mines Ethereum blocks until the current sweep round's anchor is at or
// after block anchorAfter and is final, and returns that round.
func (w *k06World) round(anchorAfter uint64) deposit.Round {
	w.t.Helper()
	for {
		r, err := w.sweepers[0].Current(w.f.ctx)
		if err != nil {
			w.t.Fatal(err)
		}
		if r.Anchor >= anchorAfter {
			return r
		}
		w.mine(1)
	}
}

// plan derives the round's sweep on every operator and requires them equal.
func (w *k06World) plan(r deposit.Round) *signing.Plan {
	w.t.Helper()
	var first *signing.Plan
	for i, s := range w.sweepers {
		p, err := s.Plan(w.f.ctx, w.fromBlock, r)
		if err != nil {
			w.t.Fatalf("operator %d sweep plan: %v", i, err)
		}
		if first != nil && p.Commitment() != first.Commitment() {
			w.t.Fatal("independent sweep planning is not deterministic")
		}
		first = p
	}
	return first
}

// signSweep runs every operator's sweeper for round concurrently.
func (w *k06World) signSweep(plan *signing.Plan, number uint64) []error {
	errs := make([]error, len(w.sweepers))
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(w.f.ctx, 120*time.Second)
	defer cancel()
	for i, s := range w.sweepers {
		wg.Add(1)
		go func(i int, s *deposit.Sweeper) {
			defer wg.Done()
			_, errs[i] = s.Sign(ctx, plan, number)
		}(i, s)
	}
	wg.Wait()
	return errs
}

// moveBitcoinTime makes Core's median-time-past at least t: blocks are mined at
// a mocked time t until the median of the last eleven passes it.
func (w *k06World) moveBitcoinTime(t uint64) {
	w.t.Helper()
	if err := w.core.call(w.f.ctx, "setmocktime", []any{t}, nil); err != nil {
		w.t.Fatal(err)
	}
	w.coreMine(11)
	mtp, err := (&mtpClock{core: w.core}).MedianTimePast(w.f.ctx)
	if err != nil || mtp < t {
		w.t.Fatalf("median-time-past %d did not reach %d: %v", mtp, t, err)
	}
}

// claimHook runs onClaim once, inside the first durable signing claim: after
// the executor's start-time checks and before the final pre-worker check.
type claimHook struct {
	*hookedJournal
	once    sync.Once
	onClaim func()
}

func (j *claimHook) Claim(ctx context.Context, id [32]byte, purpose string, raw []byte) (func(), error) {
	release, err := j.hookedJournal.Claim(ctx, id, purpose, raw)
	if err == nil && purpose == "sign" && j.onClaim != nil {
		j.once.Do(j.onClaim)
	}
	return release, err
}

// proofOutage fails every head read of the proof maintainer.
type proofOutage struct{ deposit.Chain }

func (proofOutage) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{}, errors.New("temporary proof RPC outage")
}

// lostReply wraps the chain so a mined sweep proof reports a lost reply.
type lostReply struct {
	deposit.Chain
	lose bool
}

func (l *lostReply) SubmitSweepProof(ctx context.Context, p deposit.SweepProof) error {
	if err := l.Chain.SubmitSweepProof(ctx, p); err != nil {
		return err
	}
	if l.lose {
		return errors.New("simulated lost reply after the proof was mined")
	}
	return nil
}
