package integration

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	eth "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/operator"
)

type result struct {
	records []dkg.WalletRecord
	errs    []error
}

func (f *fixture) start() <-chan result {
	done := make(chan result, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); r, e := f.run(); done <- result{r, e} }()
	f.t.Cleanup(func() {
		f.cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			f.t.Error("executor cleanup timed out")
		}
	})
	return done
}
func (f *fixture) await(done <-chan result) result {
	f.t.Helper()
	select {
	case r := <-done:
		return r
	case <-f.ctx.Done():
		f.t.Fatal("DKG test deadline")
		return result{}
	}
}
func (f *fixture) bridgeABI() abi.ABI {
	f.t.Helper()
	raw, e := os.ReadFile(filepath.Join(os.Getenv("FROST_K03_TBTC"), "solidity/build/contracts/frost/IFrostBridge.sol/IFrostBridge.json"))
	if e != nil {
		f.t.Fatal(e)
	}
	var a struct {
		ABI json.RawMessage `json:"abi"`
	}
	if e = json.Unmarshal(raw, &a); e != nil {
		f.t.Fatal(e)
	}
	parsed, e := abi.JSON(strings.NewReader(string(a.ABI)))
	if e != nil {
		f.t.Fatal(e)
	}
	return parsed
}
func (f *fixture) bridgeCall(name string, args ...interface{}) []interface{} {
	f.t.Helper()
	var out []interface{}
	e := bind.NewBoundContract(f.deployment.Bridge, f.bridgeABI(), f.client, f.client, f.client).Call(&bind.CallOpts{Context: f.ctx}, &out, name, args...)
	if e != nil {
		f.t.Fatal(e)
	}
	return out
}
func (f *fixture) send(to common.Address, data []byte) {
	f.t.Helper()
	var accounts []common.Address
	if e := f.rpc.CallContext(f.ctx, &accounts, "eth_accounts"); e != nil {
		f.t.Fatal(e)
	}
	var hash common.Hash
	if e := f.rpc.CallContext(f.ctx, &hash, "eth_sendTransaction", map[string]interface{}{"from": accounts[0], "to": to, "data": "0x" + common.Bytes2Hex(data), "gas": "0x989680"}); e != nil {
		f.t.Fatal(e)
	}
	receipt, e := f.client.TransactionReceipt(f.ctx, hash)
	for errors.Is(e, geth.NotFound) {
		select {
		case <-f.ctx.Done():
			f.t.Fatal("transaction receipt deadline")
		case <-time.After(10 * time.Millisecond):
		}
		receipt, e = f.client.TransactionReceipt(f.ctx, hash)
	}
	if e != nil || receipt.Status != 1 {
		f.t.Fatal("local transaction failed", e)
	}
}
func (f *fixture) mineTo(block uint64) {
	f.chainMu.Lock()
	defer f.chainMu.Unlock()
	f.t.Helper()
	head, e := f.client.BlockNumber(f.ctx)
	if e != nil {
		f.t.Fatal(e)
	}
	if head >= block {
		return
	}
	var out interface{}
	if e = f.rpc.CallContext(f.ctx, &out, "anvil_mine", fmt.Sprintf("0x%x", block-head)); e != nil {
		f.t.Fatal(e)
	}
}
func (f *fixture) pending() dkg.Wallet {
	f.t.Helper()
	var id [32]byte
	var e error
	for id == ([32]byte{}) {
		id, e = f.registry.ApprovedWallet(&bind.CallOpts{Context: f.ctx}, f.deployment.Epoch)
		if e != nil {
			f.t.Fatal("pending approval read", e)
		}
		if id == ([32]byte{}) {
			select {
			case <-f.ctx.Done():
				f.t.Fatal("no approved pending wallet")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	w, e := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
	if e != nil {
		f.t.Fatal(e)
	}
	return w
}
func (f *fixture) nextEpoch() {
	f.t.Helper()
	a := f.bridgeABI()
	data, e := a.Pack("requestNewFrostWallet")
	if e != nil {
		f.t.Fatal(e)
	}
	f.send(f.deployment.Bridge, data)
	beaconABI, _ := abi.JSON(strings.NewReader(`[{"type":"function","name":"fulfill","inputs":[{"name":"seed","type":"uint256"}],"outputs":[]}]`))
	data, _ = beaconABI.Pack("fulfill", big.NewInt(int64(f.deployment.Seed)))
	f.send(f.deployment.Beacon, data)
	epoch, e := f.registry.Epoch(&bind.CallOpts{Context: f.ctx})
	if e != nil {
		f.t.Fatal(e)
	}
	if epoch <= f.deployment.Epoch {
		f.t.Fatal("epoch was reused")
	}
	f.deployment.Epoch = epoch
	chainID, e := f.client.ChainID(f.ctx)
	if e != nil {
		f.t.Fatal(e)
	}
	var chain [32]byte
	chainID.FillBytes(chain[:])
	domain := frost.Domain{Network: "K03/local-anvil", Chain: chain, Registry: [20]byte(f.deployment.Registry), Epoch: epoch}
	_, operators, e := f.registry.Selection(&bind.CallOpts{Context: f.ctx}, epoch)
	if e != nil {
		f.t.Fatal(e)
	}
	f.executors = nil
	f.journals = nil
	for _, n := range f.nodes {
		var locals []uint16
		address := crypto.PubkeyToAddress(n.key.PublicKey)
		for i, a := range operators {
			if a == address {
				locals = append(locals, uint16(i+1))
			}
		}
		if len(locals) == 0 {
			continue
		}
		journal, e := n.root.Scope(domain)
		if e != nil {
			f.t.Fatal(e)
		}
		f.journals = append(f.journals, journal)
		engine, e := snowfallengine.NewGuarded(n.worker, journal)
		if e != nil {
			f.t.Fatal(e)
		}
		attempt, _ := domain.NewAttempt("dkg", [32]byte{23}, epoch)
		roster := []uint16{1, 2, 3}
		tr, e := transport.New(f.ctx, transport.Config{Domain: domain, Attempt: attempt, ID: snowfallengine.AttemptID(attempt), LocalSeats: locals, Roster: roster, LocalPublicKey: operator.MarshalUncompressed(&n.private.PublicKey), Membership: n.membership, Network: n.network})
		if e != nil {
			f.t.Fatal(e)
		}
		f.t.Cleanup(tr.Close)
		bus, e := dkg.NewNetworkBus(f.ctx, n.network, n.membership, domain.ID(), epoch)
		if e != nil {
			f.t.Fatal(e)
		}
		chainView, e := eth.NewFrostChain(f.ctx, f.client, n.key, f.deployment.Registry, f.deployment.Bridge)
		if e != nil {
			f.t.Fatal(e)
		}
		f.executors = append(f.executors, &dkg.Executor{Chain: lockedChain{chainView, &f.chainMu}, Engine: engine, Journal: journal, Bus: bus, Signer: n.key, Request: frost.DKGRequest{Group: frost.Group{Roster: roster, Threshold: 2, Quorum: 3, Epoch: epoch}, LocalSeats: locals, Participants: roster, Attempt: attempt}, Providers: frost.Providers{Transport: tr, Store: journal}, FinalityBlocks: 2, PollInterval: 50 * time.Millisecond})
	}
}

// Only the public envelope's kind is read here; secret record bytes are not parsed.
func messageKind(raw []byte) string {
	for i := 0; i < 2; i++ {
		if len(raw) < 8 {
			return ""
		}
		n := binary.BigEndian.Uint64(raw)
		raw = raw[8:]
		if n > uint64(len(raw)) {
			return ""
		}
		if i == 1 {
			return string(raw[:n])
		}
		raw = raw[n:]
	}
	return ""
}

type hookedTransport struct {
	frost.Transport
	before func(context.Context, uint16, []byte) error
	after  func(context.Context, uint16, []byte) error
}

func (t hookedTransport) Binding() (frost.Attempt, [32]byte) {
	return t.Transport.(interface {
		Binding() (frost.Attempt, [32]byte)
	}).Binding()
}
func (t hookedTransport) Broadcast(ctx context.Context, s uint16, b []byte) error {
	if t.before != nil {
		if e := t.before(ctx, s, b); e != nil {
			return e
		}
	}
	if e := t.Transport.Broadcast(ctx, s, b); e != nil {
		return e
	}
	if t.after != nil {
		return t.after(ctx, s, b)
	}
	return nil
}
func killWorker(path string) error {
	out, e := exec.Command("/bin/ps", "-axo", "pid=,command=").Output()
	if e != nil {
		return e
	}
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && f[1] == path {
			pid, e := strconv.Atoi(f[0])
			if e != nil {
				return e
			}
			p, e := os.FindProcess(pid)
			if e != nil {
				return e
			}
			if e = p.Kill(); e != nil {
				return e
			}
			found = true
		}
	}
	if !found {
		return errors.New("owned worker missing at crash boundary")
	}
	return nil
}
func (f *fixture) crashOnReady(index int, after bool) <-chan error {
	hit := make(chan error, 1)
	var once sync.Once
	hook := func(ctx context.Context, _ uint16, b []byte) error {
		if messageKind(b) != "ready-attestation" {
			return nil
		}
		once.Do(func() {
			// The candidate callback runs independently of the worker's
			// blocked send. Wait until the real public approval is present.
			for {
				id, err := f.registry.ApprovedWallet(&bind.CallOpts{Context: ctx}, f.deployment.Epoch)
				if err != nil {
					hit <- err
					return
				}
				if id != ([32]byte{}) {
					break
				}
				select {
				case <-ctx.Done():
					hit <- ctx.Err()
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
			hit <- killWorker(f.workers[index])
		})
		if !after {
			// Killing the worker does not retract bytes already in the host.
			// Prevent this before-send hook from forwarding its buffered message.
			return errors.New("test worker killed before readiness publication")
		}
		return nil
	}
	h := hookedTransport{Transport: f.executors[index].Providers.Transport}
	if after {
		h.after = hook
	} else {
		h.before = hook
	}
	f.executors[index].Providers.Transport = h
	return hit
}
func (f *fixture) waitCrash(hit <-chan error) {
	f.t.Helper()
	select {
	case e := <-hit:
		if e != nil {
			f.t.Fatal(e)
		}
	case <-f.ctx.Done():
		f.t.Fatal("crash boundary not reached")
	}
}

func TestForeignResultIsChallenged(t *testing.T) {
	f := setup(t, "1,2,3")
	p, e := f.executors[0].Chain.Parameters(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	s, e := f.executors[0].Chain.Selection(f.ctx, f.deployment.Epoch)
	if e != nil {
		t.Fatal(e)
	}
	var q [32]byte
	copy(q[:], crypto.CompressPubkey(&f.nodes[0].key.PublicKey)[1:])
	d, e := dkg.NewDescriptor(p, s, frost.Candidate{Epoch: s.Epoch, Roster: s.Roster(), Threshold: 2, Profile: frost.ApprovedProfile, OutputKey: q, Descriptor: [32]byte{7}})
	if e != nil {
		t.Fatal(e)
	}
	foreign := dkg.NewResult(d)
	if e = f.executors[0].Chain.Submit(f.ctx, foreign); e != nil {
		t.Fatal(e)
	}
	records, errs := f.run()
	for _, e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if records[0].Descriptor.OutputKey == q {
		t.Fatal("foreign candidate accepted")
	}
	it, e := f.registry.FilterResultChallenged(&bind.FilterOpts{Start: s.Epoch, Context: f.ctx}, []uint64{s.Epoch}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer it.Close()
	if !it.Next() {
		t.Fatal("foreign result was not challenged")
	}
}
func TestSubmittedDoesNotCancelAcceptance(t *testing.T) {
	f := setup(t, "1,2,3")
	done := f.start()
	// Observe a Submitted state while the workers are still waiting for finality.
	observed := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		v, e := f.executors[0].Chain.View(f.ctx, f.deployment.Epoch)
		if e != nil {
			t.Fatal(e)
		}
		if v.State == 3 {
			observed = true
			break
		}
	}
	if !observed {
		t.Fatal("submission not observed")
	}
	r := f.await(done)
	for _, e := range r.errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if r.records[0].State != "ReadyUnfunded" {
		t.Fatal("submitted terminated acceptance")
	}
}
func TestReadinessCannotFundEarly(t *testing.T) {
	f := setup(t, "1,2,3")
	hit := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	for _, e := range f.executors {
		e.Providers.Transport = hookedTransport{Transport: e.Providers.Transport, before: func(ctx context.Context, _ uint16, b []byte) error {
			if messageKind(b) == "ready-attestation" {
				once.Do(func() { close(hit) })
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}}
	}
	done := f.start()
	select {
	case <-hit:
	case <-f.ctx.Done():
		t.Fatal("no pending readiness boundary")
	}
	w := f.pending()
	id := dkg.WalletID(w.Descriptor.OutputKey)
	var label [20]byte
	copy(label[:], id[:20])
	if w.State != 1 || f.bridgeCall("isFrostFundingTarget", label)[0].(bool) {
		t.Fatal("pending wallet is fundable")
	}
	close(release)
	r := f.await(done)
	for _, e := range r.errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if f.bridgeCall("isFrostFundingTarget", label)[0].(bool) {
		t.Fatal("ReadyUnfunded wallet is fundable")
	}
}
func lostBeforeReady(t *testing.T, fresh bool) {
	f := setup(t, "1,2,3")
	hit := f.crashOnReady(0, false)
	done := f.start()
	f.waitCrash(hit)
	w := f.pending()
	id := dkg.WalletID(w.Descriptor.OutputKey)
	f.mineTo(w.Deadline)
	r := f.await(done)
	for i, e := range r.errs {
		if e == nil {
			t.Fatalf("operator %d completed without the whole-roster ready statement", i)
		}
		status, e := f.journals[i].DKGStatus(f.ctx)
		if e != nil || status.State != frost.DKGSeatsLost {
			t.Fatalf("operator %d lost seats missing: status=%+v error=%v", i, status, e)
		}
		if _, e = f.journals[i].LoadKey(f.ctx); !errors.Is(e, store.ErrMissing) {
			t.Fatal("incomplete DKG installed a key", e)
		}
	}
	closed, e := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
	if e != nil || closed.State != 3 {
		t.Fatal("pending wallet did not close", e)
	}
	if !fresh {
		return
	}
	oldJournals := append([]*store.Scoped(nil), f.journals...)
	f.nextEpoch()
	records, errs := f.run()
	for _, e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if records[0].ID == id {
		t.Fatal("identity reused")
	}
	retained, e := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
	if e != nil || retained.State != 3 || retained.Descriptor.OutputKey != w.Descriptor.OutputKey {
		t.Fatal("expired identity tombstone lost", e)
	}
	used, e := f.registry.OutputKeyUsed(&bind.CallOpts{Context: f.ctx}, w.Descriptor.OutputKey)
	if e != nil || !used {
		t.Fatal("Q tombstone lost", e)
	}
	for _, j := range oldJournals {
		status, e := j.DKGStatus(f.ctx)
		if e != nil || status.State != frost.DKGSeatsLost {
			t.Fatal("fresh epoch erased old loss", e)
		}
	}
}
func TestLostSeatBeforeReadinessExpiresWallet(t *testing.T) { lostBeforeReady(t, false) }
func TestPendingWalletExpiryThenFreshEpoch(t *testing.T)    { lostBeforeReady(t, true) }
func TestLostSeatAfterReadinessCertifiesUnderPolicy(t *testing.T) {
	f := setup(t, "1,2,3")
	hit := f.crashOnReady(0, true)
	done := f.start()
	f.waitCrash(hit)
	r := f.await(done)
	if r.errs[0] == nil {
		t.Fatal("killed worker completed")
	}
	for i := 1; i < len(r.errs); i++ {
		if r.errs[i] != nil || r.records[i].State != "ReadyUnfunded" {
			t.Fatal("remaining completed seats did not certify", i, r.errs[i])
		}
	}
	status, e := f.journals[0].DKGStatus(f.ctx)
	if e != nil || status.State != frost.DKGSeatsLost || len(status.LostSeats) != 1 {
		t.Fatal("killed seat not reported lost", e)
	}
	if _, e = f.journals[0].LoadKey(f.ctx); !errors.Is(e, store.ErrMissing) {
		t.Fatal("lost seat has a key", e)
	}
}

// Anvil batch-mined empty blocks do not retain every intermediate state. Pause
// reads only around an explicit test jump, so each view uses a retained state.
type lockedChain struct {
	dkg.Chain
	mu *sync.RWMutex
}

func (c lockedChain) View(ctx context.Context, epoch uint64) (dkg.View, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Chain.View(ctx, epoch)
}
