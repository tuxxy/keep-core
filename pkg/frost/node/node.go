// Package node owns the independently running local FROST host. Production
// activation is still fenced; the local executable validates both chain domains
// before opening a store, network or worker. Dispatch is based on public chain
// state, with guarded component authorization and process-local opaque records.
package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	log "github.com/ipfs/go-log/v2"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/bitcoin/bitcoincore"
	"github.com/keep-network/keep-core/pkg/chain"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/firewall"
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
	keepnet "github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/net/libp2p"
	"github.com/keep-network/keep-core/pkg/net/retransmission"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

type Config struct {
	LocalRegtest                                      bool
	EthereumURL, BitcoinURL, CookieFile               string
	Registry, Bridge                                  common.Address
	OperatorKeyFile, StoreKeyFile, Keystore, FenceURL string
	StorageID                                         [32]byte
	Worker                                            snowfallengine.WorkerConfig
	Peers                                             []string
	NetworkPort                                       int
	FinalityBlocks, RoundBlocks                       uint64
	FeeRate, MaximumSatPerVByte                       int64
	ProtocolSeconds                                   int
}

// Status contains public observations only. It never exports keys, nonces,
// worker record bytes or application signing records.
type Status struct {
	RejectedPeers    uint64
	State            string
	Addresses, Peers []string
	Wallets          []dkg.WalletRecord
}
type Node struct {
	policy    *observedFirewall
	signer    chain.Signing
	cfg       Config
	ctx       context.Context
	cancel    context.CancelFunc
	client    *ethclient.Client
	registry  *binding.FrostWalletRegistry
	root      *store.Store
	key       *ecdsa.PrivateKey
	private   *operator.PrivateKey
	network   keepnet.Provider
	btc       *bitcoincore.Chain
	admission *eligibility
	chain     *eth.FrostRedemptionChain
	mu        sync.RWMutex
	status    Status
	wallets   map[uint64]*wallet
}
type wallet struct {
	record     dkg.WalletRecord
	journal    *store.Scoped
	request    frost.DKGRequest
	membership *group.MembershipValidator
	mover      *movement.Mover
	plan       *signing.Plan
	signed     bool
}
type eligibility struct {
	ctx      context.Context
	contract *bind.BoundContract
}

func (a *eligibility) IsRecognized(public *operator.PublicKey) (bool, error) {
	var out []any
	address := crypto.PubkeyToAddress(ecdsa.PublicKey{Curve: crypto.S256(), X: public.X, Y: public.Y})
	if err := a.contract.Call(&bind.CallOpts{Context: a.ctx}, &out, "eligibleStake", address); err != nil {
		return false, err
	}
	return len(out) == 1 && out[0].(*big.Int).Sign() > 0, nil
}

type observedFirewall struct {
	keepnet.Firewall
	rejected atomic.Uint64
}

func (f *observedFirewall) Validate(public *operator.PublicKey) error {
	err := f.Firewall.Validate(public)
	if errors.Is(err, firewall.ErrNotRecognized) {
		f.rejected.Add(1)
	}
	return err
}

func loopback(endpoint string) bool {
	u, e := url.Parse(endpoint)
	return e == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.Port() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func ReadConfig(reader io.Reader) (Config, error) {
	var c Config
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&c)
	return c, err
}
func New(ctx context.Context, c Config) (n *Node, err error) {
	if !c.LocalRegtest || !loopback(c.EthereumURL) || !loopback(c.BitcoinURL) || !loopback(c.FenceURL) || c.Registry == (common.Address{}) || c.Bridge == (common.Address{}) || c.FinalityBlocks == 0 || c.RoundBlocks == 0 || c.FeeRate <= 0 || c.MaximumSatPerVByte < c.FeeRate || c.ProtocolSeconds <= 0 || c.ProtocolSeconds > 900 {
		return nil, errors.New("explicit bounded local FROST configuration required")
	}
	for _, peer := range c.Peers {
		if !strings.HasPrefix(peer, "/ip4/127.0.0.1/tcp/") || strings.Contains(peer, "/p2p-circuit") || strings.Contains(peer, "/dns") {
			return nil, errors.New("local FROST bootstrap required")
		}
	}
	child, cancel := context.WithCancel(ctx)
	n = &Node{cfg: c, ctx: child, cancel: cancel, status: Status{State: "starting"}, wallets: map[uint64]*wallet{}}
	allocated := n
	defer func() {
		if err != nil {
			allocated.Close()
		}
	}()
	n.btc, err = bitcoincore.NewLocal(child, c.BitcoinURL, c.CookieFile, c.FeeRate)
	if err != nil {
		return nil, err
	}
	client, dialErr := rpc.DialOptions(child, c.EthereumURL, rpc.WithHTTPClient(&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("local Ethereum redirect refused") }}))
	err = dialErr
	if err == nil {
		n.client = ethclient.NewClient(client)
	}
	if err != nil {
		return nil, err
	}
	// Anvil domains are deliberately outside public chain IDs. A local URL alone
	// is insufficient if it forwards to an external Ethereum provider.
	id, err := n.client.ChainID(child)
	if err != nil || !id.IsUint64() || id.Uint64() < 100000 {
		return nil, errors.New("local Ethereum chain domain required")
	}
	var version string
	if err = n.client.Client().CallContext(child, &version, "web3_clientVersion"); err != nil || !strings.HasPrefix(strings.ToLower(version), "anvil/") {
		return nil, errors.New("local Anvil provider required")
	}
	raw, err := os.ReadFile(c.OperatorKeyFile)
	if err != nil {
		return nil, errors.New("operator key unavailable")
	}
	n.key, err = crypto.HexToECDSA(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("operator key encoding")
	}
	n.private = &operator.PrivateKey{PublicKey: operator.PublicKey{Curve: operator.Secp256k1, X: n.key.X, Y: n.key.Y}, D: n.key.D}
	n.signer, err = eth.NewFrostOperatorSigner(n.key)
	if err != nil {
		return nil, err
	}
	n.registry, err = binding.NewFrostWalletRegistry(c.Registry, n.client)
	if err != nil {
		return nil, err
	}
	address, err := n.registry.Admission(&bind.CallOpts{Context: child})
	if err != nil {
		return nil, err
	}
	contractABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"eligibleStake","inputs":[{"type":"address"}],"outputs":[{"type":"uint96"}],"stateMutability":"view"}]`))
	if err != nil {
		return nil, err
	}
	n.admission = &eligibility{child, bind.NewBoundContract(address, contractABI, n.client, n.client, n.client)}
	policy := firewall.AnyApplicationPolicy([]firewall.Application{n.admission}, firewall.EmptyAllowList())
	if err = policy.Validate(&n.private.PublicKey); err != nil {
		return nil, err
	}
	material, err := os.ReadFile(c.StoreKeyFile)
	if err != nil || len(material) != 32 {
		return nil, errors.New("store key provisioning unavailable")
	}
	var storeKey [32]byte
	copy(storeKey[:], material)
	n.root, err = store.Open(child, store.Config{Keystore: c.Keystore, StorageID: c.StorageID, Key: storeKey, Fence: localFence{c.FenceURL}})
	if err != nil {
		return nil, err
	}
	n.chain, err = eth.NewFrostRedemptionChain(child, n.client, n.key, c.Bridge)
	if err != nil {
		return nil, err
	}
	n.policy = &observedFirewall{Firewall: policy}
	n.network, err = libp2p.Connect(child, libp2p.Config{Peers: c.Peers, Port: c.NetworkPort}, n.private, n.policy, retransmission.NewTimeTicker(child, 100*time.Millisecond))
	if err != nil {
		return nil, err
	}
	for _, address := range n.network.ConnectionManager().AddrStrings() {
		if strings.Contains(address, "/ip4/127.0.0.1/") {
			n.status.Addresses = append(n.status.Addresses, address)
		}
	}
	return n, nil
}
func (n *Node) Close() {
	if n == nil {
		return
	}
	n.cancel()
	if n.root != nil {
		_ = n.root.Close()
	}
	if n.client != nil {
		n.client.Close()
	}
	if n.btc != nil {
		n.btc.Close()
	}
}
func (n *Node) Status() Status {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := n.status
	if n.policy != nil {
		out.RejectedPeers = n.policy.rejected.Load()
	}
	out.Addresses = append([]string(nil), out.Addresses...)
	out.Wallets = append([]dkg.WalletRecord(nil), out.Wallets...)
	if n.network != nil {
		out.Peers = n.network.ConnectionManager().ConnectedPeers()
	}
	return out
}
func (n *Node) setState(value string) { n.mu.Lock(); n.status.State = value; n.mu.Unlock() }
func (n *Node) membership(selection dkg.Selection) *group.MembershipValidator {
	addresses := make([]chain.Address, len(selection.Operators))
	for i, address := range selection.Operators {
		addresses[i] = chain.Address(address.Hex())
	}
	return group.NewMembershipValidator(log.Logger("keep-frost-node"), addresses, n.signer)
}
func (n *Node) transport(ctx context.Context, w *wallet, attempt frost.Attempt) (frost.Transport, func(), error) {
	tr, err := transport.New(ctx, transport.Config{Domain: w.journal.Domain(), Attempt: attempt, ID: snowfallengine.AttemptID(attempt), LocalSeats: w.request.LocalSeats, Roster: w.request.Group.Roster, LocalPublicKey: operator.MarshalUncompressed(&n.private.PublicKey), Membership: w.membership, Network: n.network})
	if err != nil {
		return nil, nil, err
	}
	return tr, tr.Close, nil
}
func (n *Node) loadEpoch(ctx context.Context, epoch uint64) error {
	adapter, err := eth.NewFrostChain(ctx, n.client, n.key, n.cfg.Registry, n.cfg.Bridge)
	if err != nil {
		return err
	}
	parameters, err := adapter.Parameters(ctx)
	if err != nil {
		return err
	}
	if err = parameters.Validate(); err != nil {
		return err
	}
	selection, err := adapter.Selection(ctx, epoch)
	if err != nil {
		return err
	}
	if len(selection.Members) == 0 {
		return nil
	} // Await the beacon-frozen selection; do not start or lose seats early.
	if err = selection.Validate(parameters); err != nil {
		return err
	}
	address := crypto.PubkeyToAddress(n.key.PublicKey)
	var seats []uint16
	for i, selected := range selection.Operators {
		if selected == address {
			seats = append(seats, uint16(i+1))
		}
	}
	if len(seats) == 0 {
		return nil
	}
	var chainID [32]byte
	parameters.ChainID.FillBytes(chainID[:])
	domain := frost.Domain{Network: "FROST/local-regtest", Chain: chainID, Registry: [20]byte(n.cfg.Registry), Epoch: epoch}
	journal, err := n.root.Scope(domain)
	if err != nil {
		return err
	}
	session := sha256.Sum256([]byte("keep-core/frost/local-node/dkg/v1"))
	attempt, err := domain.NewAttempt("dkg", session, epoch)
	if err != nil {
		return err
	}
	w := &wallet{journal: journal, membership: n.membership(selection), request: frost.DKGRequest{Group: frost.Group{Roster: selection.Roster(), Threshold: parameters.Threshold, Quorum: parameters.GroupSize, Epoch: epoch}, LocalSeats: seats, Participants: selection.Roster(), Attempt: attempt}}
	engine, err := snowfallengine.NewGuarded(n.cfg.Worker, journal)
	if err != nil {
		return err
	}
	record, loadErr := journal.LoadWallet(ctx)
	if errors.Is(loadErr, store.ErrMissing) {
		n.setState("dkg")
		tr, closeTransport, err := n.transport(ctx, w, attempt)
		if err != nil {
			return err
		}
		defer closeTransport()
		bus, err := dkg.NewNetworkBus(ctx, n.network, w.membership, domain.ID(), epoch)
		if err != nil {
			return err
		}
		execution := &dkg.Executor{Chain: adapter, Engine: engine, Journal: journal, Bus: bus, Signer: n.key, Request: w.request, Providers: frost.Providers{Transport: tr, Store: journal}, FinalityBlocks: n.cfg.FinalityBlocks, PollInterval: 50 * time.Millisecond}
		record, err = execution.Run(ctx)
		if err != nil {
			return err
		}
	} else if loadErr != nil {
		return loadErr
	} else {
		if record.State != "ReadyUnfunded" || record.Quarantined {
			return errors.New("wallet is not dispatchable after restart")
		}
		view, err := adapter.View(ctx, epoch)
		if err != nil || view.ApprovedID != record.ID || view.Approved.State != 2 {
			return errors.New("restart wallet approval changed")
		}
		key, err := journal.LoadKey(ctx)
		if err != nil {
			return err
		}
		var reloadSession [32]byte
		if _, err = rand.Read(reloadSession[:]); err != nil {
			return err
		}
		reload, err := domain.NewAttempt("sign", reloadSession, view.Head)
		if err != nil {
			return err
		}
		tr, stop, err := n.transport(ctx, w, reload)
		if err != nil {
			return err
		}
		defer stop()
		acceptance := readyAcceptance{chain: adapter, parameters: parameters, selection: selection, record: record}
		if _, err = acceptance.WaitCandidateAcceptance(ctx, key.Candidate); err != nil {
			return err
		}
		if err = engine.ReloadKey(ctx, frost.SigningRequest{Group: w.request.Group, LocalSeats: seats, Selected: selection.Roster(), Attempt: reload, Key: key}, frost.Providers{Transport: tr, Store: journal, Acceptance: acceptance}); err != nil {
			return err
		}
	}
	w.record = record
	view := &movement.View{Chain: n.chain, Policy: movement.Policy{FinalityBlocks: n.cfg.FinalityBlocks}}
	base := signing.Config{Journal: journal, Bitcoin: n.btc, Worker: n.cfg.Worker, MinimumConfirmations: 1, LocalRegtest: true, Transport: func(ctx context.Context, a frost.Attempt) (frost.Transport, func(), error) {
		return n.transport(ctx, w, a)
	}}
	w.mover = &movement.Mover{Chain: n.chain, Authority: &movement.Authority{View: view}, Lock: &deposit.MainLock{Journal: journal}, Ledger: redemption.Ledger{Journal: journal}, Schedule: redemption.Schedule{FinalityBlocks: n.cfg.FinalityBlocks, RoundBlocks: n.cfg.RoundBlocks}, Wallet: record.Descriptor, Base: base}
	n.wallets[epoch] = w
	n.mu.Lock()
	n.status.Wallets = append(n.status.Wallets, record)
	n.status.State = "ready"
	n.mu.Unlock()
	return nil
}

// Run owns discovery and dispatch. The parent process supplies public chain
// activity, not calls into this node's component methods or opaque records.
func (n *Node) Run(ctx context.Context) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		epoch, err := n.registry.Epoch(&bind.CallOpts{Context: ctx})
		if err != nil {
			return err
		}
		if epoch > 0 && n.wallets[epoch] == nil {
			operation, stop := context.WithTimeout(ctx, time.Duration(n.cfg.ProtocolSeconds)*time.Second)
			err = n.loadEpoch(operation, epoch)
			stop()
			if err != nil {
				return err
			}
		}
		// Recover older owned wallets from public registry events as well as
		// the latest epoch. Their keys are only loaded through the scoped API.
		head, err := n.chain.Head(ctx)
		if err != nil {
			return err
		}
		ids, err := n.chain.ReadyWallets(ctx, 0, head.Number)
		if err != nil {
			return err
		}
		for _, id := range ids {
			public, err := n.chain.Wallet(ctx, head.Number, movement.Label(id))
			if err != nil {
				return err
			}
			owned := false
			for _, address := range public.Descriptor.Operators {
				if address == crypto.PubkeyToAddress(n.key.PublicKey) {
					owned = true
				}
			}
			if owned && n.wallets[public.Descriptor.Epoch] == nil {
				operation, stop := context.WithTimeout(ctx, time.Duration(n.cfg.ProtocolSeconds)*time.Second)
				err = n.loadEpoch(operation, public.Descriptor.Epoch)
				stop()
				if err != nil {
					return err
				}
			}
		}
		for _, wallet := range n.wallets {
			if err = n.incoming(ctx, wallet); err != nil {
				return err
			}
		}
	}
}
func (n *Node) incoming(ctx context.Context, w *wallet) error {
	operation, stop := context.WithTimeout(ctx, time.Duration(n.cfg.ProtocolSeconds)*time.Second)
	defer stop()
	if w.plan == nil {
		planner := movement.Planner{View: w.mover.Authority.View, Bitcoin: n.btc, Wallet: w.record.Descriptor, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: n.cfg.FeeRate, MaximumSatPerVByte: n.cfg.MaximumSatPerVByte}
		plan, err := planner.IncomingSweep(operation)
		if errors.Is(err, movement.ErrNothingIncoming) {
			return nil
		}
		if err != nil {
			return err
		}
		w.plan = plan
	}
	// Another selected operator can finish the proof while this process is
	// still returning from signing. Reconcile that exact plan before retrying
	// authorization against its now-consumed inputs.
	settled, err := n.reconcileIncoming(operation, w)
	if err != nil || settled {
		return err
	}
	if !w.signed {
		round, err := w.mover.Current(operation)
		if err != nil {
			return err
		}
		n.setState("signing")
		if _, err = w.mover.Sign(operation, w.plan, round.Number); err != nil {
			if done, reconcileErr := n.reconcileIncoming(operation, w); reconcileErr == nil && done {
				return nil
			}
			return err
		}
		w.signed = true
	}
	// The lowest selected seat owns public broadcast/proof submission. Every
	// operator independently signs and reconciles its durable intent.
	if w.request.LocalSeats[0] == 1 {
		exists, err := n.btc.Transaction(operation, bitcoin.Hash(w.plan.BitcoinTransactionHash()))
		if err != nil || len(exists) == 0 {
			if err = w.mover.Broadcast(operation, w.plan); err != nil {
				return err
			}
			n.setState("broadcast")
		}
		confirmations, err := n.btc.Confirmations(operation, bitcoin.Hash(w.plan.BitcoinTransactionHash()))
		if err != nil {
			return err
		}
		if confirmations > 0 {
			maintainer := spv.FrostMovementMaintainer{Chain: n.chain, Bitcoin: n.btc, Confirmations: 1}
			if _, err = maintainer.ProveIncomingSweep(operation, w.record.ID, w.plan.BitcoinTransactionHash()); err != nil {
				return err
			}
		}
	}
	status, err := w.mover.Resolve(operation, w.plan)
	if err != nil {
		return err
	}
	if status == movement.StatusSettled {
		w.plan = nil
		w.signed = false
		n.setState("settled")
	}
	return nil
}

// A proven exact outpoint is a terminal chain outcome, even if this node
// missed its peer's final signing acknowledgement. Wait for finality before
// recording the outcome; never turn an unrelated signing failure into success.
func (n *Node) reconcileIncoming(ctx context.Context, w *wallet) (bool, error) {
	head, err := n.chain.Head(ctx)
	if err != nil {
		return false, err
	}
	for _, input := range w.plan.IncomingOutputs() {
		record, err := n.chain.Incoming(ctx, head.Number, movement.OutpointKey(input.Outpoint))
		if err != nil {
			return false, err
		}
		if record.State != movement.IncomingSwept {
			return false, nil
		}
	}
	if head.Number < n.cfg.FinalityBlocks {
		return true, nil
	}
	for _, input := range w.plan.IncomingOutputs() {
		record, err := n.chain.Incoming(ctx, head.Number-n.cfg.FinalityBlocks, movement.OutpointKey(input.Outpoint))
		if err != nil {
			return false, err
		}
		if record.State != movement.IncomingSwept {
			return true, nil
		}
	}
	status, err := w.mover.Resolve(ctx, w.plan)
	if err != nil {
		return false, err
	}
	if status != movement.StatusSettled {
		return false, errors.New("proven incoming plan did not reconcile")
	}
	w.plan, w.signed = nil, false
	n.setState("settled")
	return true, nil
}
