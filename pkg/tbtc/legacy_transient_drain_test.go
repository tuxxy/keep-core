package tbtc_test

// Remediation batch 1b, tuxxy/keep-core#73 fix review W1.
//
// Invariant: a FROST-unconfigured client against an upgraded Bridge never
// broadcasts a moving-funds transaction the Bridge refuses, also when its
// endpoint fails transiently. A failed read is not an answer: a transient
// error on wallets(target) must not make a FROST drain target look like a
// legacy wallet, which would build a P2WPKH output to a FROST label.
//
// The world is the upgraded-Bridge drain of legacy_upgraded_test.go. Between
// the recorder and Anvil sits a fault ADAPTER that fails the first occurrence
// of every distinct contract read (target and full calldata, so
// wallets(source) and wallets(target) are different reads) in each phase:
// once while the client proposes, and again from the start of execution.
// Every repeat succeeds. The client may refuse the action (fail closed for
// this call, D4(c)); whatever it broadcasts, the Bridge must accept.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

// legacyFaults is an ADAPTER in front of Anvil that fails the first
// occurrence of every distinct eth_call (target and full calldata) since the
// last Arm with a transient error: HTTP 503, or a JSON-RPC internal error on
// the websocket. Transactions and every other method pass through.
type legacyFaults struct {
	upstream string
	server   *httptest.Server

	mutex  sync.Mutex
	armed  bool
	seen   map[string]bool
	failed map[string]int
}

func newLegacyFaults(t *testing.T, upstream string) *legacyFaults {
	f := &legacyFaults{upstream: upstream, failed: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// URL is the adapter's HTTP endpoint; websockets upgrade on the same address.
func (f *legacyFaults) URL() string { return f.server.URL }

// Arm starts a new phase: every distinct read fails once more.
func (f *legacyFaults) Arm() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.armed = true
	f.seen = map[string]bool{}
}

// Failed reports how many times the read was failed.
func (f *legacyFaults) Failed(key string) int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.failed[key]
}

// FailedAt reports how many reads of the contract at address were failed.
func (f *legacyFaults) FailedAt(address string) int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	total := 0
	for key, n := range f.failed {
		if strings.HasPrefix(key, strings.ToLower(address)+":") {
			total += n
		}
	}
	return total
}

func legacyCallKey(request legacyRPCRequest) string {
	if request.Method != "eth_call" || len(request.Params) == 0 {
		return ""
	}
	var message struct {
		To    string `json:"to"`
		Data  string `json:"data"`
		Input string `json:"input"`
	}
	if json.Unmarshal(request.Params[0], &message) != nil {
		return ""
	}
	data := message.Input
	if data == "" {
		data = message.Data
	}
	return strings.ToLower(message.To) + ":" + strings.ToLower(data)
}

func (f *legacyFaults) admit(requests []legacyRPCRequest) bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if !f.armed {
		return false
	}
	fail := false
	for _, request := range requests {
		if key := legacyCallKey(request); key != "" && !f.seen[key] {
			f.seen[key] = true
			f.failed[key]++
			fail = true
		}
	}
	return fail
}

func (f *legacyFaults) serve(w http.ResponseWriter, req *http.Request) {
	if websocket.IsWebSocketUpgrade(req) {
		f.serveWebsocket(w, req)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	requests, _, err := legacyParseRequests(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.admit(requests) {
		http.Error(w, "transient: upstream unavailable", http.StatusServiceUnavailable)
		return
	}
	response, err := http.Post(f.upstream, "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	out, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(out)
}

func (f *legacyFaults) serveWebsocket(w http.ResponseWriter, req *http.Request) {
	upstream, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.upstream, "http"), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, err := legacyUpgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	defer client.Close()
	var clientWrite sync.Mutex
	write := func(message []byte) error {
		clientWrite.Lock()
		defer clientWrite.Unlock()
		return client.WriteMessage(websocket.TextMessage, message)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := upstream.ReadMessage()
			if err != nil {
				return
			}
			if write(message) != nil {
				return
			}
		}
	}()
	for {
		_, message, err := client.ReadMessage()
		if err != nil {
			return
		}
		requests, _, err := legacyParseRequests(message)
		if err != nil {
			return
		}
		if f.admit(requests) {
			for _, request := range requests {
				reply, _ := json.Marshal(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      request.ID,
					"error":   map[string]interface{}{"code": -32603, "message": "transient: upstream unavailable"},
				})
				if write(reply) != nil {
					return
				}
			}
			continue
		}
		if upstream.WriteMessage(websocket.TextMessage, message) != nil {
			return
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

// TestLegacyUnchanged_UpgradedBridgeDrainTransient: see the file comment.
// Refs tuxxy/keep-core#73.
func TestLegacyUnchanged_UpgradedBridgeDrainTransient(t *testing.T) {
	for _, encoding := range legacyEncodings {
		t.Run(encoding.String()+"+transient-then-success", func(t *testing.T) {
			legacyUpgradedDrainTransientRound(t, encoding)
		})
	}
}

func legacyUpgradedDrainTransientRound(t *testing.T, encoding legacyRevertEncoding) {
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	target := &frostTarget{}
	w := newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), setUpFrostDrain(target))

	// The drain commitment, submitted by a wallet member that knows FROST.
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(operatorKey, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.legacy.SubmitMovingFundsCommitment(opts, w.source.pkh, legacyUTXO(w.main),
		[]uint32{1}, big.NewInt(1), [][20]byte{target.label})
	if err != nil {
		t.Fatalf("setup: drain commitment: %v", err)
	}
	if receipt, err := legacyWaitMined(context.Background(), w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: drain commitment not mined: %v", err)
	}
	walletsCall, err := w.bridgeABI.Pack("wallets", target.label)
	if err != nil {
		t.Fatal(err)
	}
	targetRead := strings.ToLower(w.bridge.Hex()) + ":" + hexutil.Encode(walletsCall)

	faults := newLegacyFaults(t, w.anvilURL)
	recorder := newLegacyRecorder(t, faults.URL(), encoding)
	tbtcChain, err := w.connect(recorder)
	if err != nil {
		t.Fatalf("startup against the upgraded Bridge: %v", err)
	}
	stop := w.mineInBackground()
	defer stop()

	operator := chain.Address(w.operator.Hex())
	request := &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.source.pkh,
		WalletOperators:     []chain.Address{operator},
		ExecutingOperator:   operator,
		ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
	}
	task := tbtcpg.NewMovingFundsTask(tbtcChain, w.btc)

	faults.Arm()
	var proposal *tbtc.MovingFundsProposal
	var proposeErr error
	for attempt := 1; attempt <= legacyTransientAttempts && proposal == nil; attempt++ {
		proposeErr = legacyBounded(t, "moving-funds proposal", func() error {
			result, ok, err := task.Run(request)
			if err == nil && ok {
				proposal = result.(*tbtc.MovingFundsProposal)
			}
			return err
		})
	}
	if proposal == nil {
		t.Logf("upgraded Bridge drain: the client made no proposal (%v); nothing was broadcast", proposeErr)
	} else {
		sign := tbtc.LegacySignBatchFn(func(_ context.Context, messages []*big.Int, _ uint64) ([]*tecdsa.Signature, error) {
			return legacySignatures(w.source.key, messages)
		})
		bridgeFailedInProposal := faults.FailedAt(w.bridge.Hex())
		targetFailedInProposal := faults.Failed(targetRead)
		faults.Arm()
		var executeErr error
		for attempt := 1; attempt <= legacyTransientAttempts; attempt++ {
			start, err := w.admin.BlockNumber(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			executeErr = legacyBounded(t, "moving-funds execution", func() error {
				return tbtc.ExecuteMovingFundsActionForTest(tbtcChain, w.btc, &w.source.key.PublicKey,
					[]chain.Address{operator}, sign, proposal, start, w.waitForBlock)
			})
			if executeErr == nil || len(w.btc.Broadcasts()) > 0 {
				break
			}
		}
		if executeErr != nil {
			t.Logf("upgraded Bridge drain: the client refused the action (%v)", executeErr)
		}
		// Precondition: the faults reached the client's Bridge reads during
		// execution. Which reads it makes is the implementation's choice;
		// a client that reads wallets(target) has that read failed once.
		if faults.FailedAt(w.bridge.Hex()) == bridgeFailedInProposal {
			t.Errorf("setup: no Bridge read failed during execution; the case proves nothing")
		}
		t.Logf("upgraded Bridge drain: wallets(%x) failed %d time(s) during execution",
			target.label, faults.Failed(targetRead)-targetFailedInProposal)
	}
	stop()

	for _, broadcast := range w.btc.Broadcasts() {
		for i, output := range broadcast.Outputs {
			if legacyPaysTo(output.PublicKeyScript, target.label) {
				t.Errorf("upgraded Bridge drain after a transient error: output %d pays a legacy script to FROST target %x", i, target.label)
			}
		}
		w.btc.mine(t, broadcast)
		_, err := w.legacy.SubmitMovingFundsProof(w.opts(), legacyTxInfo(broadcast),
			w.btc.proof(t, broadcast.Hash()), legacyUTXO(w.main), w.source.pkh)
		if err != nil {
			t.Errorf("upgraded Bridge drain after a transient error: the Bridge refuses the moving-funds transaction the client broadcast: %v", err)
		} else {
			t.Logf("upgraded Bridge drain after a transient error: the Bridge accepted the broadcast transaction %s",
				broadcast.Hash().Hex(bitcoin.ReversedByteOrder))
		}
	}
	if n := len(w.btc.Broadcasts()); n > 1 {
		t.Errorf("upgraded Bridge drain after a transient error: %d moving-funds transactions broadcast, want at most 1", n)
	}
}
