package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// coreChain is test-only. All RPC traffic uses an isolated loopback regtest.
// C-04's reservation is a controlled provider; this is not contract evidence.
type coreChain struct {
	url, cookie string
	client      *http.Client
}

func (c *coreChain) call(ctx context.Context, method string, args []any, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "1.0", "id": "k04", "method": method, "params": args})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	parts := strings.SplitN(c.cookie, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid test RPC cookie")
	}
	request.SetBasicAuth(parts[0], parts[1])
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope struct {
		Result json.RawMessage
		Error  *struct {
			Code    int
			Message string
		}
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("Core %s: %d %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, result)
}
func startCore(t *testing.T, ctx context.Context) *coreChain {
	t.Helper()
	path, pin := os.Getenv("FROST_K04_BITCOIND"), os.Getenv("FROST_K04_BITCOIND_SHA256")
	if path == "" || pin == "" {
		t.Skip("K-04 requires a pinned Bitcoin Core binary")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != pin {
		t.Fatal("Bitcoin Core pin mismatch")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir := t.TempDir()
	log, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	processCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(processCtx, path, "-regtest", "-server", "-listen=0", "-discover=0", "-dnsseed=0", "-connect=0", "-rpcbind=127.0.0.1", "-rpcallowip=127.0.0.1", fmt.Sprintf("-rpcport=%d", port), "-datadir="+dir, "-fallbackfee=0.0002", "-txindex=1", "-par=2", "-printtoconsole=1")
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	c := &coreChain{url: fmt.Sprintf("http://127.0.0.1:%d", port), client: &http.Client{Timeout: 10 * time.Second}}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = c.call(stopCtx, "stop", nil, nil)
		cancel()
		_ = command.Wait()
		c.client.CloseIdleConnections()
	})
	ready := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		cookie, e := os.ReadFile(filepath.Join(dir, "regtest/.cookie"))
		if e != nil {
			continue
		}
		c.cookie = strings.TrimSpace(string(cookie))
		if e = c.call(ctx, "getblockchaininfo", nil, nil); e == nil {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("Bitcoin Core startup failed; see", log.Name())
	}
	var version struct {
		Version    int
		Subversion string
	}
	if err = c.call(ctx, "getnetworkinfo", nil, &version); err != nil || version.Version != 290000 {
		t.Fatalf("expected Bitcoin Core 29.0: %+v %v", version, err)
	}
	t.Logf("Bitcoin Core %s binary SHA256=%s", version.Subversion, pin)
	if err = c.call(ctx, "createwallet", []any{"k04"}, nil); err != nil {
		t.Fatal(err)
	}
	return c
}
func (c *coreChain) Genesis(ctx context.Context) ([32]byte, error) {
	var text string
	if err := c.call(ctx, "getblockhash", []any{0}, &text); err != nil {
		return [32]byte{}, err
	}
	h, err := bitcoin.NewHashFromString(text, bitcoin.ReversedByteOrder)
	return [32]byte(h), err
}
func (c *coreChain) Transaction(ctx context.Context, h bitcoin.Hash) ([]byte, error) {
	var text string
	if err := c.call(ctx, "getrawtransaction", []any{h.Hex(bitcoin.ReversedByteOrder)}, &text); err != nil {
		return nil, err
	}
	return hex.DecodeString(text)
}
func (c *coreChain) Confirmations(ctx context.Context, h bitcoin.Hash) (uint, error) {
	var result struct{ Confirmations uint }
	err := c.call(ctx, "getrawtransaction", []any{h.Hex(bitcoin.ReversedByteOrder), true}, &result)
	return result.Confirmations, err
}
func (c *coreChain) Unspent(ctx context.Context, p bitcoin.TransactionOutpoint) (bool, error) {
	var result json.RawMessage
	err := c.call(ctx, "gettxout", []any{p.TransactionHash.Hex(bitcoin.ReversedByteOrder), p.OutputIndex, true}, &result)
	return len(result) > 0 && string(result) != "null", err
}
func (c *coreChain) Broadcast(ctx context.Context, raw []byte) error {
	encoded := hex.EncodeToString(raw)
	var checks []struct {
		Allowed      bool
		RejectReason string `json:"reject-reason"`
	}
	if err := c.call(ctx, "testmempoolaccept", []any{[]string{encoded}}, &checks); err != nil {
		return err
	}
	if len(checks) != 1 || !checks[0].Allowed {
		return fmt.Errorf("Core rejected signed transaction: %+v", checks)
	}
	return c.call(ctx, "sendrawtransaction", []any{encoded}, nil)
}

type localReservation struct{ view signing.ReservationView }

func (r localReservation) ReadReservation(context.Context, [32]byte) (signing.ReservationView, error) {
	return r.view, nil
}

// Observe only the public grant passed through the real guarded engine.
// Embedded application-record methods retain their normal store receiver.
type observedSigningJournal struct {
	*store.Scoped
	mu         sync.Mutex
	grants     []observedGrant
	afterClaim func()
}
type observedGrant struct {
	Message       [32]byte
	Authorization []byte
	Attempt       frost.Attempt
}

func (j *observedSigningJournal) Claim(ctx context.Context, id [32]byte, purpose string, raw []byte) (func(), error) {
	release, err := j.Scoped.Claim(ctx, id, purpose, raw)
	if err != nil {
		return nil, err
	}
	var grant observedGrant
	if purpose != "sign" {
		release()
		return nil, fmt.Errorf("unexpected signing claim purpose")
	}
	if err = json.Unmarshal(raw, &grant); err != nil {
		release()
		return nil, err
	}
	j.mu.Lock()
	j.grants = append(j.grants, grant)
	j.mu.Unlock()
	if j.afterClaim != nil {
		j.afterClaim()
	}
	return release, nil
}

func TestTaprootExactAuthorization(t *testing.T) {
	f := setup(t, "1,1,2")
	core := startCore(t, f.ctx)
	records, errs := f.run()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("DKG operator %d: %v", i, err)
		}
	}
	var miner string
	if err := core.call(f.ctx, "getnewaddress", nil, &miner); err != nil {
		t.Fatal(err)
	}
	if err := core.call(f.ctx, "generatetoaddress", []any{101, miner}, nil); err != nil {
		t.Fatal(err)
	}
	q := records[0].Descriptor.OutputKey
	address, err := btcutil.NewAddressTaproot(q[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	script, err := bitcoin.PayToTaproot(q)
	if err != nil {
		t.Fatal(err)
	}
	var prevouts []bitcoin.PreviousOutput
	for n := 0; n < 2; n++ {
		var txid string
		// json.Number preserves the exact decimal funding value.
		if err = core.call(f.ctx, "sendtoaddress", []any{address.EncodeAddress(), json.Number("0.01")}, &txid); err != nil {
			t.Fatal(err)
		}
		h, err := bitcoin.NewHashFromString(txid, bitcoin.ReversedByteOrder)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := core.Transaction(f.ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		tx := new(bitcoin.Transaction)
		if err = tx.Deserialize(raw); err != nil {
			t.Fatal(err)
		}
		found := false
		for i, out := range tx.Outputs {
			if bytes.Equal(out.PublicKeyScript, script) && out.Value == 1_000_000 {
				prevouts = append(prevouts, bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: h, OutputIndex: uint32(i)}, Value: out.Value, PublicKeyScript: append(bitcoin.Script(nil), out.PublicKeyScript...)})
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing exact-Q funding output")
		}
	}
	if err = core.call(f.ctx, "generatetoaddress", []any{1, miner}, nil); err != nil {
		t.Fatal(err)
	}
	recipient, _ := bitcoin.PayToWitnessPublicKeyHash([20]byte{91})
	tx := &bitcoin.Transaction{Version: 2, Outputs: []*bitcoin.TransactionOutput{{Value: 1_500_000, PublicKeyScript: recipient}, {Value: 498_000, PublicKeyScript: script}}}
	for _, p := range prevouts {
		point := p.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &point, Sequence: 0xffffffff})
	}
	// Force an actual worker digest with a leading zero. Final sequences make
	// these locktimes valid without waiting for the corresponding block.
	for ; tx.Locktime < 65536; tx.Locktime++ {
		digest, err := bitcoin.TaprootSignatureHash(tx, prevouts, 0)
		if err != nil {
			t.Fatal(err)
		}
		if digest[0] == 0 {
			break
		}
	}
	if tx.Locktime == 65536 {
		t.Fatal("no leading-zero digest fixture")
	}
	plan, err := signing.NewPlan(signing.Proposal{Wallet: records[0].Descriptor, BitcoinGenesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Purpose: signing.Redemption, Generation: 1, StateVersion: 1, Requests: []signing.Request{{ID: [32]byte{7}, Value: 1_500_000, Script: recipient}}, ConflictInput: prevouts[0].Outpoint, Fees: signing.FeePolicy{Minimum: 2000, Maximum: 2000, MaximumSatPerVByte: 20}, Transaction: tx, Prevouts: prevouts})
	if err != nil {
		t.Fatal(err)
	}
	reservation := localReservation{signing.ReservationView{ID: [32]byte{8}, Commitment: plan.Commitment(), State: "Reserved", Block: 10, Head: 12, BlockHash: [32]byte{10}, CanonicalBlockHash: [32]byte{10}, HeadHash: [32]byte{12}, Generation: 1, StateVersion: 1}}
	configs := make([]signing.Config, len(f.nodes))
	launches := make([]string, len(f.nodes))
	observed := make([]*observedSigningJournal, len(f.nodes))
	executors := make([]*signing.Executor, len(f.nodes))
	results := make([]*signing.Result, len(f.nodes))
	signErrors := make([]error, len(f.nodes))
	for i, node := range f.nodes {
		i, node := i, node
		observed[i] = &observedSigningJournal{Scoped: f.journals[i]}
		worker, marker := observeWorkerLaunch(t, node.worker)
		launches[i] = marker
		config := signing.Config{Plan: plan, Reservation: reservation.view.ID, Session: [32]byte{33}, StartBlock: 77, FinalityBlocks: 2, MinimumConfirmations: 1, Journal: observed[i], Reservations: reservation, Bitcoin: core, Worker: worker, Selected: []uint16{1, 3}, LocalRegtest: true, Transport: func(ctx context.Context, a frost.Attempt) (frost.Transport, func(), error) {
			request := f.executors[i].Request
			tr, err := transport.New(ctx, transport.Config{Domain: f.journals[i].Domain(), Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: request.LocalSeats, Roster: request.Group.Roster, LocalPublicKey: operator.MarshalUncompressed(&node.private.PublicKey), Membership: node.membership, Network: node.network})
			if err != nil {
				return nil, nil, err
			}
			return tr, tr.Close, nil
		}}
		configs[i] = config
		executors[i], err = signing.NewLocalExecutor(config)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !t.Run("claim flip refuses before real worker launch", func(t *testing.T) {
		changing := &flipReservation{view: reservation.view}
		journal := &observedSigningJournal{Scoped: f.journals[0], afterClaim: func() { changing.closed.Store(true) }}
		config := configs[0]
		config.Journal = journal
		config.Reservations = changing
		config.Session = [32]byte{55}
		config.StartBlock = 78
		refused, err := signing.NewLocalExecutor(config)
		if err != nil {
			t.Fatal(err)
		}
		bounded, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		if _, err = refused.Run(bounded); !errors.Is(err, signing.ErrUnauthorized) {
			t.Fatal("claim flip did not produce authorization refusal", err)
		}
		if len(journal.grants) != 1 {
			t.Fatal("refusal did not follow exactly one real durable worker claim")
		}
		if _, err = os.Stat(launches[0]); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused worker was launched", err)
		}
		grant := journal.grants[0]
		if release, err := f.journals[0].Claim(f.ctx, snowfallengine.AttemptID(grant.Attempt), "sign", []byte("must not overwrite")); !errors.Is(err, store.ErrClaimed) {
			if release != nil {
				release()
			}
			t.Fatal("refused claim is not terminal", err)
		}
		changing.closed.Store(false)
		journal.afterClaim = nil
		fresh, err := signing.NewLocalExecutor(config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fresh.Run(bounded); err == nil {
			t.Fatal("fresh executor reused refused attempt")
		}
		if _, err = os.Stat(launches[0]); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("retry launched a worker", err)
		}
	}) {
		t.FailNow()
	}
	identity, err := tbtc.NewFrostWalletIdentity(records[0].Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i, e := range executors {
		wg.Add(1)
		go func(i int, e *signing.Executor) {
			defer wg.Done()
			results[i], signErrors[i] = tbtc.ExecuteFrostTransaction(f.ctx, identity, e)
			if signErrors[i] != nil {
				f.cancel()
			}
		}(i, e)
	}
	wg.Wait()
	for i, err := range signErrors {
		if err != nil {
			t.Fatalf("real signing operator %d: %v (all=%v)", i, err, signErrors)
		}
	}
	for i, result := range results {
		markers, err := os.ReadFile(launches[i])
		if err != nil || string(markers) != "start\nstart\n" {
			t.Fatal("positive real-worker launch control failed", err, string(markers))
		}
		if err = plan.Snapshot().ValidateSigned(result.Transaction); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(result.Transaction.Serialize(), results[0].Transaction.Serialize()) {
			t.Fatal("operators disagree on signed transaction")
		}
		retained, err := f.journals[i].ReadSigningRecord(f.ctx, plan.Commitment())
		if err != nil || !bytes.Equal(retained, plan.IntentBytes()) {
			t.Fatal("intent not retained", err)
		}
		recovered, err := executors[i].LoadResult(f.ctx)
		if err != nil || !bytes.Equal(recovered.Transaction.Serialize(), result.Transaction.Serialize()) {
			t.Fatal("result not recoverable", err)
		}
		if len(observed[i].grants) != len(result.Done) {
			t.Fatal("worker grant count differs from transaction inputs")
		}
		for input, done := range result.Done {
			grant := observed[i].grants[input]
			if grant.Message != done.Context.Message || snowfallengine.AttemptID(grant.Attempt) != done.Context.Attempt || !bytes.Equal(grant.Authorization, plan.IntentBytes()) {
				t.Fatal("builder-to-worker authorization trace mismatch")
			}

			if done.Context.Message != plan.Snapshot().Digests()[input] || len(result.Transaction.Inputs[input].Witness) != 1 || len(result.Transaction.Inputs[input].Witness[0]) != 64 {
				t.Fatal("worker digest or witness changed")
			}
		}
	}
	// A valid signature from a changed context and a post-approval mutation
	// must both stop before Core receives any transaction.
	altered := *results[0]
	altered.Done = append([]signing.Done(nil), altered.Done...)
	altered.Done[0].Context.Attempt[0] ^= 1
	if err = executors[0].Broadcast(f.ctx, &altered); err == nil {
		t.Fatal("foreign attempt accepted")
	}
	raw, _ := json.Marshal(results[0])
	var changed signing.Result
	_ = json.Unmarshal(raw, &changed)
	changed.Transaction.Outputs[0].Value--
	if err = executors[0].Broadcast(f.ctx, &changed); err == nil {
		t.Fatal("post-approval transaction mutation accepted")
	}
	recoveredExecutor, err := signing.NewLocalExecutor(configs[0])
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recoveredExecutor.LoadResult(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	reanchored := reservation
	reanchored.view.Block++
	reanchored.view.Head++
	foreignConfig := configs[0]
	foreignConfig.Reservations = reanchored
	foreignExecutor, err := signing.NewLocalExecutor(foreignConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = foreignExecutor.Broadcast(f.ctx, recovered); !errors.Is(err, signing.ErrUnauthorized) {
		t.Fatal("real store lost reservation anchor on executor recovery", err)
	}
	if err = recoveredExecutor.Broadcast(f.ctx, recovered); err != nil {
		t.Fatal(err)
	}
	if err = core.call(f.ctx, "generatetoaddress", []any{1, miner}, nil); err != nil {
		t.Fatal(err)
	}
	confirmations, err := core.Confirmations(f.ctx, results[0].Transaction.Hash())
	if err != nil || confirmations < 1 {
		t.Fatal("Core did not mine FROST transaction", err)
	}
	t.Logf("exact authorization: Q=%x plan=%x digests=%x txid=%s; 2 inputs, 64-byte witnesses, Core accepted and mined", q, plan.Commitment(), plan.Snapshot().Digests(), results[0].Transaction.Hash().Hex(bitcoin.ReversedByteOrder))
}

type flipReservation struct {
	view   signing.ReservationView
	closed atomic.Bool
}

func (r *flipReservation) ReadReservation(context.Context, [32]byte) (signing.ReservationView, error) {
	v := r.view
	if r.closed.Load() {
		v.State = "TimeoutPending"
	}
	return v, nil
}

// A test-only launcher records starts, then execs the exact hash-checked real
// worker. The positive control signs through this same launcher twice. This
// catches even a short-lived unauthorized process that a ps snapshot can miss.
func observeWorkerLaunch(t *testing.T, original snowfallengine.WorkerConfig) (snowfallengine.WorkerConfig, string) {
	t.Helper()
	raw, err := os.ReadFile(original.Path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(raw) != original.SHA256 {
		t.Fatal("real worker hash changed")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "starts")
	launcher := filepath.Join(dir, "worker-launcher")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := []byte("#!/bin/sh\numask 077\nprintf 'start\\n' >> " + quote(marker) + " || exit 125\nexec " + quote(original.Path) + " \"$@\"\n")
	if err = os.WriteFile(launcher, script, 0700); err != nil {
		t.Fatal(err)
	}
	config := original
	config.Path = launcher
	config.SHA256 = sha256.Sum256(script)
	return config, marker
}
