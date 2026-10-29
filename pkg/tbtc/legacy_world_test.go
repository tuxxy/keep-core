package tbtc_test

// The legacy-unchanged world: a real Anvil chain running the real pre-FROST
// tbtc-v2 contracts built from git at 40a11d1d (owner decision D1), a JSON-RPC
// recorder between the client and Anvil, and a real Bitcoin Core 29.0
// regtest node. Core builds the deposit funding, validates and mines every
// transaction, and its blocks supply the headers and merkle proofs the Bridge
// verifies. Everything that is not a production contract, production client
// code or Core is labelled FIXTURE or ADAPTER below.
//
// The world is driven only through public calls: wallets reach the Bridge
// through its registry callbacks, the source wallet is funded by a revealed
// deposit and a swept deposit proven with SPV, and it enters MovingFunds
// through the heartbeat-failure callback. No Bridge storage is written
// directly.
//
// It uses only APIs that also exist at keep-core base 4c9b65ec4, so the same
// suite can be run unchanged against the pre-FROST client.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"

	commonethereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

// legacyBaselineCommit is tbtc-v2 main before any FROST change (D1).
const legacyBaselineCommit = "40a11d1dcdcf82d3962e430067cfe3f97be81483"

// Anvil's default development keys: the deployer and the node's operator.
const (
	legacyAdminKey    = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	legacyOperatorKey = "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
)

// legacyChainLag puts the chain clock this far behind the wall clock. The
// legacy moving-funds safety margin (24 hours) is measured on the client's
// wall clock at base. With the chain this far behind, a client that measured
// it on chain time instead would refuse the proposal (K09-33, K09-55).
const legacyChainLag = 72 * time.Hour

const legacyDepositValue = int64(10_000_000)
const legacySweepFee = int64(2_000)

type legacyEnvironment struct {
	baseline       string
	repo           string
	anvil          string
	bitcoind       string
	bitcoindSHA256 string
}

// upgradedBuild returns the upgraded (FROST) tbtc-v2 build directory and the
// commit it must be built from, for the upgraded-Bridge half of
// tuxxy/keep-core#73. test/legacy/run.sh requires both.
func upgradedBuild(t *testing.T) (string, string) {
	t.Helper()
	dir, commit := os.Getenv("LEGACY_UPGRADED_DIR"), os.Getenv("LEGACY_UPGRADED_COMMIT")
	if dir == "" || commit == "" {
		t.Skip("upgraded Bridge: run through test/legacy/run.sh " +
			"(LEGACY_UPGRADED_DIR, LEGACY_UPGRADED_COMMIT)")
	}
	return dir, commit
}

// legacyEnv reads the suite's environment. test/legacy/run.sh sets it and
// fails the run if any test is skipped, so a skip here is never a pass.
func legacyEnv(t *testing.T) legacyEnvironment {
	t.Helper()
	env := legacyEnvironment{
		baseline:       os.Getenv("LEGACY_BASELINE_DIR"),
		repo:           os.Getenv("LEGACY_TBTC_REPO"),
		anvil:          os.Getenv("LEGACY_ANVIL"),
		bitcoind:       os.Getenv("LEGACY_BITCOIND"),
		bitcoindSHA256: os.Getenv("LEGACY_BITCOIND_SHA256"),
	}
	if env.baseline == "" || env.repo == "" || env.anvil == "" ||
		env.bitcoind == "" || env.bitcoindSHA256 == "" {
		t.Skip("legacy-unchanged suite: run through test/legacy/run.sh " +
			"(LEGACY_BASELINE_DIR, LEGACY_TBTC_REPO, LEGACY_ANVIL, " +
			"LEGACY_BITCOIND, LEGACY_BITCOIND_SHA256)")
	}
	return env
}

// --- Baseline artifacts ------------------------------------------------------

type legacyArtifact struct {
	SourceName     string          `json:"sourceName"`
	ContractName   string          `json:"contractName"`
	ABI            json.RawMessage `json:"abi"`
	Bytecode       string          `json:"bytecode"`
	LinkReferences map[string]map[string][]struct {
		Start  int `json:"start"`
		Length int `json:"length"`
	} `json:"linkReferences"`
}

type legacyBaseline struct {
	t        *testing.T
	dir      string
	repo     string
	commit   string
	verified map[string]bool
}

// openLegacyBaseline checks that the build was made from git at the pinned
// commit: the manifest names it, and every repository source compiled into a
// deployed artifact is byte-identical to `git show <pin>:solidity/<path>`.
func openLegacyBaseline(t *testing.T, env legacyEnvironment) *legacyBaseline {
	t.Helper()
	return openTbtcBuild(t, env, env.baseline, legacyBaselineCommit)
}

// openTbtcBuild opens a build made by test/legacy/build-baseline.sh and binds
// it to the given commit in the same way.
func openTbtcBuild(t *testing.T, env legacyEnvironment, dir, commit string) *legacyBaseline {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("baseline manifest: %v", err)
	}
	var manifest struct {
		TbtcCommit string `json:"tbtcCommit"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("baseline manifest: %v", err)
	}
	if manifest.TbtcCommit != commit {
		t.Fatalf("build %s made from %s, not %s", dir, manifest.TbtcCommit, commit)
	}
	return &legacyBaseline{t: t, dir: dir, repo: env.repo, commit: commit, verified: map[string]bool{}}
}

func (b *legacyBaseline) artifact(source, name string) *legacyArtifact {
	b.t.Helper()
	path := filepath.Join(b.dir, "solidity", "build", source, name+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		b.t.Fatalf("baseline artifact %s:%s: %v", source, name, err)
	}
	var artifact legacyArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		b.t.Fatalf("baseline artifact %s:%s: %v", source, name, err)
	}
	b.verifySources(path)
	return &artifact
}

func (b *legacyBaseline) verifySources(artifactPath string) {
	b.t.Helper()
	raw, err := os.ReadFile(strings.TrimSuffix(artifactPath, ".json") + ".dbg.json")
	if err != nil {
		b.t.Fatalf("baseline debug file: %v", err)
	}
	var debug struct {
		BuildInfo string `json:"buildInfo"`
	}
	if err := json.Unmarshal(raw, &debug); err != nil {
		b.t.Fatal(err)
	}
	infoPath := filepath.Join(filepath.Dir(artifactPath), debug.BuildInfo)
	if b.verified[infoPath] {
		return
	}
	raw, err = os.ReadFile(infoPath)
	if err != nil {
		b.t.Fatalf("baseline build info: %v", err)
	}
	var info struct {
		SolcVersion string `json:"solcVersion"`
		Input       struct {
			Sources map[string]struct {
				Content string `json:"content"`
			} `json:"sources"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		b.t.Fatal(err)
	}
	if info.SolcVersion != "0.8.17" {
		b.t.Fatalf("baseline compiled with solc %s", info.SolcVersion)
	}
	checked := 0
	for source, input := range info.Input.Sources {
		switch {
		case strings.HasPrefix(source, "contracts/hardhat-dependency-compiler/"):
			// hardhat-dependency-compiler writes generated import shims:
			// they may hold nothing but a pragma, a licence and imports.
			for _, line := range strings.Split(input.Content, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "//") &&
					!strings.HasPrefix(line, "pragma ") && !strings.HasPrefix(line, "import ") {
					b.t.Fatalf("build shim %s holds code", source)
				}
			}
		case strings.HasPrefix(source, "contracts/"):
			out, err := exec.Command(
				"git", "-C", b.repo, "show",
				b.commit+":solidity/"+source,
			).Output()
			if err != nil {
				b.t.Fatalf("build source %s not at %s: %v", source, b.commit, err)
			}
			if string(out) != input.Content {
				b.t.Fatalf("build source %s differs from %s", source, b.commit)
			}
		default:
			// A dependency: it must equal the node_modules file installed
			// from the build commit's yarn.lock (build-baseline.sh checks
			// the lock).
			out, err := os.ReadFile(filepath.Join(b.dir, "solidity", "node_modules", source))
			if err != nil || string(out) != input.Content {
				b.t.Fatalf("build dependency source %s differs from node_modules", source)
			}
		}
		checked++
	}
	if checked == 0 {
		b.t.Fatalf("build info %s holds no sources", infoPath)
	}
	b.verified[infoPath] = true
}

// fixture returns a FIXTURE contract compiled from test/legacy/fixtures.
func (b *legacyBaseline) fixture(name string) (abi.ABI, []byte) {
	b.t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.dir, "fixtures", "LegacyFixtures.json"))
	if err != nil {
		b.t.Fatalf("fixtures: %v", err)
	}
	var combined struct {
		Contracts map[string]struct {
			ABI json.RawMessage `json:"abi"`
			Bin string          `json:"bin"`
		} `json:"contracts"`
	}
	if err := json.Unmarshal(raw, &combined); err != nil {
		b.t.Fatal(err)
	}
	for key, contract := range combined.Contracts {
		if strings.HasSuffix(key, ":"+name) {
			parsed, err := abi.JSON(bytes.NewReader(contract.ABI))
			if err != nil {
				b.t.Fatal(err)
			}
			return parsed, common.FromHex(contract.Bin)
		}
	}
	b.t.Fatalf("fixture %s not compiled", name)
	return abi.ABI{}, nil
}

// --- Anvil -----------------------------------------------------------------

func startLegacyAnvil(t *testing.T, env legacyEnvironment) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(
		ctx, env.anvil,
		"--port", fmt.Sprint(port),
		"--hardfork", "cancun",
		"--timestamp", fmt.Sprint(time.Now().Add(-legacyChainLag).Unix()),
		"--silent",
	)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("anvil: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		client, err := ethclient.Dial(url)
		if err == nil {
			_, err = client.ChainID(context.Background())
			client.Close()
			if err == nil {
				return url
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("anvil did not start: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// --- ADAPTER: JSON-RPC recorder ---------------------------------------------

// legacyRevertEncoding is how the endpoint reports a call that reverted
// without data.
type legacyRevertEncoding int

const (
	// Code 3, data "0x": Anvil's own encoding, passed through unchanged.
	revertCode3 legacyRevertEncoding = iota
	// Code -32000, no data: go-ethereum's encoding. The ADAPTER rewrites
	// Anvil's empty reverts into it; reverts that carry data are untouched.
	revertGeth
)

func (e legacyRevertEncoding) String() string {
	if e == revertGeth {
		return "geth-32000-nodata"
	}
	return "code3-0x"
}

type legacyRPCCall struct {
	Method   string
	To       common.Address
	Selector string
	Failed   bool
}

// legacyRecorder is an ADAPTER between the client and Anvil. It records every
// request, can fail the next n messages (HTTP 503, or a JSON-RPC internal
// error on the websocket: a transient endpoint error), and can re-encode
// empty reverts. It never changes a successful
// result.
type legacyRecorder struct {
	t        *testing.T
	upstream string
	encoding legacyRevertEncoding
	server   *httptest.Server

	mutex     sync.Mutex
	calls     []legacyRPCCall
	failNext  int
	failOnce  bool
	seen      map[string]bool
	rewritten int
}

func newLegacyRecorder(t *testing.T, upstream string, encoding legacyRevertEncoding) *legacyRecorder {
	r := &legacyRecorder{t: t, upstream: upstream, encoding: encoding}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.server.Close)
	return r
}

// URL is the recorder's websocket endpoint. The client needs a websocket:
// its block counter subscribes to new heads, as in production.
func (r *legacyRecorder) URL() string { return "ws" + strings.TrimPrefix(r.server.URL, "http") }

// FailNext makes the next n messages fail transiently.
func (r *legacyRecorder) FailNext(n int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.failNext = n
}

// FailEachReadOnce makes the first occurrence of every distinct contract read
// (eth_call by target and selector, eth_getLogs by filter address) fail
// transiently; every repeat succeeds. Transactions are never failed: a lost
// eth_sendRawTransaction is not a transient read error.
func (r *legacyRecorder) FailEachReadOnce() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.failOnce = true
	r.seen = map[string]bool{}
}

// readKey identifies a distinct contract read, or "" for anything else.
func legacyReadKey(request legacyRPCRequest) string {
	if len(request.Params) == 0 {
		return ""
	}
	switch request.Method {
	case "eth_call":
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
		if len(data) < 10 {
			return ""
		}
		return "call:" + strings.ToLower(message.To) + ":" + strings.ToLower(data[:10])
	case "eth_getLogs":
		var filter struct {
			Address json.RawMessage `json:"address"`
		}
		if json.Unmarshal(request.Params[0], &filter) != nil {
			return ""
		}
		return "logs:" + strings.ToLower(string(filter.Address))
	}
	return ""
}

func (r *legacyRecorder) Calls() []legacyRPCCall {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]legacyRPCCall(nil), r.calls...)
}

func (r *legacyRecorder) Rewritten() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.rewritten
}

type legacyRPCRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func (r *legacyRecorder) record(request legacyRPCRequest, failed bool) {
	call := legacyRPCCall{Method: request.Method, Failed: failed}
	switch request.Method {
	case "eth_call", "eth_estimateGas":
		if len(request.Params) > 0 {
			var message struct {
				To    *common.Address `json:"to"`
				Data  string          `json:"data"`
				Input string          `json:"input"`
			}
			if json.Unmarshal(request.Params[0], &message) == nil && message.To != nil {
				call.To = *message.To
				data := message.Input
				if data == "" {
					data = message.Data
				}
				if raw := common.FromHex(data); len(raw) >= 4 {
					call.Selector = hex.EncodeToString(raw[:4])
				}
			}
		}
	case "eth_sendRawTransaction":
		if len(request.Params) > 0 {
			var rawHex string
			tx := new(types.Transaction)
			if json.Unmarshal(request.Params[0], &rawHex) == nil &&
				tx.UnmarshalBinary(common.FromHex(rawHex)) == nil && tx.To() != nil {
				call.To = *tx.To()
				if len(tx.Data()) >= 4 {
					call.Selector = hex.EncodeToString(tx.Data()[:4])
				}
			}
		}
	}
	r.calls = append(r.calls, call)
}

// parse splits a JSON-RPC message into its requests.
func legacyParseRequests(body []byte) ([]legacyRPCRequest, bool, error) {
	var requests []legacyRPCRequest
	trimmed := bytes.TrimSpace(body)
	batch := len(trimmed) > 0 && trimmed[0] == '['
	var err error
	if batch {
		err = json.Unmarshal(body, &requests)
	} else {
		var single legacyRPCRequest
		err = json.Unmarshal(body, &single)
		requests = []legacyRPCRequest{single}
	}
	return requests, batch, err
}

// admit records the requests and reports whether they must fail.
func (r *legacyRecorder) admit(requests []legacyRPCRequest) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	fail := r.failNext > 0
	if fail {
		r.failNext--
	}
	if r.failOnce {
		for _, request := range requests {
			if key := legacyReadKey(request); key != "" && !r.seen[key] {
				r.seen[key] = true
				fail = true
			}
		}
	}
	for _, request := range requests {
		r.record(request, fail)
	}
	return fail
}

var legacyUpgrader = websocket.Upgrader{ReadBufferSize: 1 << 20, WriteBufferSize: 1 << 20}

// serveWebsocket relays one client websocket to Anvil. A failed message is
// answered with a JSON-RPC internal error instead of being forwarded.
func (r *legacyRecorder) serveWebsocket(w http.ResponseWriter, req *http.Request) {
	upstream, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(r.upstream, "http"), nil)
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
			if r.encoding == revertGeth {
				trimmed := bytes.TrimSpace(message)
				message = r.reencode(message, len(trimmed) > 0 && trimmed[0] == '[')
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
		if r.admit(requests) {
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

func (r *legacyRecorder) serve(w http.ResponseWriter, req *http.Request) {
	if websocket.IsWebSocketUpgrade(req) {
		r.serveWebsocket(w, req)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	requests, batch, err := legacyParseRequests(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.admit(requests) {
		http.Error(w, "transient: upstream unavailable", http.StatusServiceUnavailable)
		return
	}

	response, err := http.Post(r.upstream, "application/json", bytes.NewReader(body))
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
	if r.encoding == revertGeth {
		out = r.reencode(out, batch)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(out)
}

// reencode turns every empty revert (code 3, data absent or "0x") into
// go-ethereum's {-32000, "execution reverted"} without data.
func (r *legacyRecorder) reencode(out []byte, batch bool) []byte {
	var responses []map[string]json.RawMessage
	if batch {
		if json.Unmarshal(out, &responses) != nil {
			return out
		}
	} else {
		var single map[string]json.RawMessage
		if json.Unmarshal(out, &single) != nil {
			return out
		}
		responses = []map[string]json.RawMessage{single}
	}
	changed := false
	for _, response := range responses {
		raw, ok := response["error"]
		if !ok {
			continue
		}
		var rpcError struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &rpcError) != nil || rpcError.Code != 3 {
			continue
		}
		var data string
		if len(rpcError.Data) > 0 && json.Unmarshal(rpcError.Data, &data) == nil && data != "0x" && data != "" {
			continue
		}
		response["error"] = json.RawMessage(`{"code":-32000,"message":"execution reverted"}`)
		changed = true
		r.mutex.Lock()
		r.rewritten++
		r.mutex.Unlock()
	}
	if !changed {
		return out
	}
	var encoded []byte
	var err error
	if batch {
		encoded, err = json.Marshal(responses)
	} else {
		encoded, err = json.Marshal(responses[0])
	}
	if err != nil {
		return out
	}
	return encoded
}

// --- Bitcoin Core regtest ----------------------------------------------------

// legacyCore is a real Bitcoin Core regtest node (LEGACY_BITCOIND, pinned by
// LEGACY_BITCOIND_SHA256). Every Bitcoin transaction in the suite is
// submitted to it and mined by it, so Core checks every script and signature.
type legacyCore struct {
	url  string
	user string
	pass string
}

func (c *legacyCore) call(method string, params []interface{}, out interface{}) error {
	if params == nil {
		params = []interface{}{}
	}
	body, err := json.Marshal(map[string]interface{}{"jsonrpc": "1.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.SetBasicAuth(c.user, c.pass)
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return fmt.Errorf("%s: HTTP %d: %v", method, response.StatusCode, err)
	}
	if reply.Error != nil {
		return fmt.Errorf("%s: Core error %d: %s", method, reply.Error.Code, reply.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(reply.Result, out)
}

func legacyFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// startLegacyCore runs bitcoind on datadir. The returned function stops it
// and waits for it to exit; it also runs at cleanup.
func startLegacyCore(t *testing.T, env legacyEnvironment, datadir string) (*legacyCore, func()) {
	t.Helper()
	executable, err := os.ReadFile(env.bitcoind)
	if err != nil {
		t.Fatalf("bitcoind: %v", err)
	}
	if digest := sha256.Sum256(executable); hex.EncodeToString(digest[:]) != env.bitcoindSHA256 {
		t.Fatalf("bitcoind %s does not match its pinned SHA-256", env.bitcoind)
	}
	rpcPort, p2pPort := legacyFreePort(t), legacyFreePort(t)
	core := &legacyCore{url: fmt.Sprintf("http://127.0.0.1:%d", rpcPort), user: "legacy", pass: "legacy-unchanged"}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, env.bitcoind,
		"-regtest", "-datadir="+datadir, "-server", "-listen=0", "-txindex=1",
		fmt.Sprintf("-rpcport=%d", rpcPort), fmt.Sprintf("-port=%d", p2pPort),
		"-rpcuser="+core.user, "-rpcpassword="+core.pass,
		"-fallbackfee=0.0002", "-printtoconsole=0",
	)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("bitcoind: %v", err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = core.call("stop", nil, nil)
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				cancel()
				<-done
			}
			cancel()
		})
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err = core.call("getblockchaininfo", nil, nil); err == nil {
			return core, stop
		}
		if time.Now().After(deadline) {
			t.Fatalf("bitcoind did not start: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type legacyBlock struct {
	header bitcoin.BlockHeader
	txs    []*bitcoin.Transaction
}

// legacyRegtest serves the client through bitcoin.Chain from the real Core
// node: blocks, headers and transactions are read back from Core, and a
// broadcast is Core's sendrawtransaction. The ADAPTER part is only the
// address-history queries, which Core has no index for: they are answered by
// scanning the blocks read from Core and the transactions broadcast here.
type legacyRegtest struct {
	t          *testing.T
	core       *legacyCore
	miner      string
	mutex      sync.Mutex
	blocks     []*legacyBlock
	mempool    []*bitcoin.Transaction
	broadcasts []*bitcoin.Transaction
	rejected   []string
}

var _ bitcoin.Chain = (*legacyRegtest)(nil)

// legacyCoreTemplate returns a stopped regtest datadir whose wallet
// "legacy-unchanged" holds 101 blocks of coinbases, and the wallet's mining
// address. Mining with a loaded wallet costs one disk sync per block (7 s for
// the 101 blocks alone, 25 s next to parallel cases), so the datadir is built
// once per machine and bitcoind binary, and each case starts from a copy.
func legacyCoreTemplate(t *testing.T, env legacyEnvironment) (string, string) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cache, "keep-core-legacy-tests")
	final := filepath.Join(root, "core-"+env.bitcoindSHA256[:16]+"-v1")
	if miner, err := os.ReadFile(filepath.Join(final, "miner")); err == nil {
		return final, string(miner)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	building, err := os.MkdirTemp(root, "building-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(building)
	core, stop := startLegacyCore(t, env, building)
	var miner string
	if err := core.call("createwallet", []interface{}{"legacy-unchanged"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := core.call("getnewaddress", nil, &miner); err != nil {
		t.Fatal(err)
	}
	if err := core.call("generatetoaddress", []interface{}{101, miner}, nil); err != nil {
		t.Fatal(err)
	}
	stop()
	if err := os.WriteFile(filepath.Join(building, "miner"), []byte(miner), 0o600); err != nil {
		t.Fatal(err)
	}
	// Another process may have finished first; either copy will do.
	if err := os.Rename(building, final); err != nil {
		if _, statErr := os.Stat(filepath.Join(final, "miner")); statErr != nil {
			t.Fatalf("Core template: %v", err)
		}
		own, err := os.ReadFile(filepath.Join(final, "miner"))
		if err != nil {
			t.Fatal(err)
		}
		miner = string(own)
	}
	return final, miner
}

// legacyCopyDir copies a stopped datadir.
func legacyCopyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("Core datadir copy: %v", err)
	}
}

func newLegacyRegtest(t *testing.T, env legacyEnvironment) *legacyRegtest {
	t.Helper()
	template, miner := legacyCoreTemplate(t, env)
	datadir := t.TempDir()
	legacyCopyDir(t, template, datadir)
	core, _ := startLegacyCore(t, env, datadir)
	c := &legacyRegtest{t: t, core: core, miner: miner}
	if err := c.core.call("loadwallet", []interface{}{"legacy-unchanged"}, nil); err != nil {
		t.Fatal(err)
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.syncLocked(t)
	return c
}

// syncLocked reads every block Core has that is not read yet.
func (c *legacyRegtest) syncLocked(t *testing.T) {
	t.Helper()
	var count int
	if err := c.core.call("getblockcount", nil, &count); err != nil {
		t.Fatal(err)
	}
	for height := len(c.blocks); height <= count; height++ {
		var hash, rawHex string
		if err := c.core.call("getblockhash", []interface{}{height}, &hash); err != nil {
			t.Fatal(err)
		}
		if err := c.core.call("getblock", []interface{}{hash, 0}, &rawHex); err != nil {
			t.Fatal(err)
		}
		raw, err := hex.DecodeString(rawHex)
		if err != nil {
			t.Fatal(err)
		}
		var message wire.MsgBlock
		if err := message.Deserialize(bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		h := message.Header
		block := &legacyBlock{header: bitcoin.BlockHeader{
			Version:                 h.Version,
			PreviousBlockHeaderHash: bitcoin.Hash(h.PrevBlock),
			MerkleRootHash:          bitcoin.Hash(h.MerkleRoot),
			Time:                    uint32(h.Timestamp.Unix()),
			Bits:                    h.Bits,
			Nonce:                   h.Nonce,
		}}
		if legacyHeaderHash(block.header) != bitcoin.Hash(h.BlockHash()) {
			t.Fatalf("block %d header does not round-trip", height)
		}
		for _, wireTx := range message.Transactions {
			var buffer bytes.Buffer
			if err := wireTx.Serialize(&buffer); err != nil {
				t.Fatal(err)
			}
			tx := new(bitcoin.Transaction)
			if err := tx.Deserialize(buffer.Bytes()); err != nil {
				t.Fatal(err)
			}
			if tx.Hash() != bitcoin.Hash(wireTx.TxHash()) {
				t.Fatalf("block %d transaction does not round-trip", height)
			}
			block.txs = append(block.txs, tx)
		}
		c.blocks = append(c.blocks, block)
	}
	confirmed := map[bitcoin.Hash]bool{}
	for _, tx := range c.allLocked() {
		confirmed[tx.Hash()] = true
	}
	kept := c.mempool[:0]
	for _, tx := range c.mempool {
		if !confirmed[tx.Hash()] {
			kept = append(kept, tx)
		}
	}
	c.mempool = kept
}

func (c *legacyRegtest) sendLocked(tx *bitcoin.Transaction) error {
	var txid string
	err := c.core.call("sendrawtransaction", []interface{}{hex.EncodeToString(tx.Serialize(bitcoin.Witness))}, &txid)
	if err != nil {
		c.rejected = append(c.rejected, fmt.Sprintf("%s: %v", tx.Hash().Hex(bitcoin.ReversedByteOrder), err))
		return err
	}
	c.mempool = append(c.mempool, tx)
	return nil
}

// mine submits the given transactions to Core unless they are already in its
// mempool, mines one block and requires Core to have included each of them.
func (c *legacyRegtest) mine(t *testing.T, txs ...*bitcoin.Transaction) *legacyBlock {
	t.Helper()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, tx := range txs {
		pending := false
		for _, m := range c.mempool {
			pending = pending || m.Hash() == tx.Hash()
		}
		if !pending {
			if err := c.sendLocked(tx); err != nil {
				t.Fatalf("Bitcoin Core refused %s: %v", tx.Hash().Hex(bitcoin.ReversedByteOrder), err)
			}
		}
	}
	if err := c.core.call("generatetoaddress", []interface{}{1, c.miner}, nil); err != nil {
		t.Fatal(err)
	}
	c.syncLocked(t)
	block := c.blocks[len(c.blocks)-1]
	for _, tx := range txs {
		if !legacyBlockHas(block, tx.Hash()) {
			t.Fatalf("Bitcoin Core did not mine %s", tx.Hash().Hex(bitcoin.ReversedByteOrder))
		}
	}
	return block
}

func legacyBlockHas(block *legacyBlock, hash bitcoin.Hash) bool {
	for _, tx := range block.txs {
		if tx.Hash() == hash {
			return true
		}
	}
	return false
}

// pay has Core's wallet pay value to script and mines it; returns the
// funding transaction as Core built it.
func (c *legacyRegtest) pay(t *testing.T, address string, value int64) *bitcoin.Transaction {
	t.Helper()
	var txid string
	amount := json.Number(fmt.Sprintf("%d.%08d", value/100_000_000, value%100_000_000))
	if err := c.core.call("sendtoaddress", []interface{}{address, amount}, &txid); err != nil {
		t.Fatal(err)
	}
	var rawHex string
	if err := c.core.call("getrawtransaction", []interface{}{txid}, &rawHex); err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatal(err)
	}
	tx := new(bitcoin.Transaction)
	if err := tx.Deserialize(raw); err != nil {
		t.Fatal(err)
	}
	c.mutex.Lock()
	c.mempool = append(c.mempool, tx)
	c.mutex.Unlock()
	c.mine(t, tx)
	return tx
}

func (c *legacyRegtest) Rejected() []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]string(nil), c.rejected...)
}

func legacyDoubleSHA(data []byte) bitcoin.Hash {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// legacyHeaderHash is the double SHA-256 of the serialized header, in internal
// byte order. (bitcoin.BlockHeader.Hash is an unimplemented stub.)
func legacyHeaderHash(header bitcoin.BlockHeader) bitcoin.Hash {
	serialized := header.Serialize()
	return legacyDoubleSHA(serialized[:])
}

func legacyMerkleLevel(level []bitcoin.Hash) []bitcoin.Hash {
	if len(level)%2 == 1 {
		level = append(level, level[len(level)-1])
	}
	next := make([]bitcoin.Hash, 0, len(level)/2)
	for i := 0; i < len(level); i += 2 {
		next = append(next, legacyDoubleSHA(append(level[i][:], level[i+1][:]...)))
	}
	return next
}

// legacyMerkleBranch returns the root and the sibling path of leaf index.
func legacyMerkleBranch(leaves []bitcoin.Hash, index int) (bitcoin.Hash, []bitcoin.Hash) {
	level := append([]bitcoin.Hash(nil), leaves...)
	var branch []bitcoin.Hash
	for len(level) > 1 {
		if len(level)%2 == 1 {
			level = append(level, level[len(level)-1])
		}
		branch = append(branch, level[index^1])
		level = legacyMerkleLevel(level)
		index /= 2
	}
	return level[0], branch
}

// proof is the SPV proof the Bridge expects with txProofDifficultyFactor 1.
func (c *legacyRegtest) proof(t *testing.T, txHash bitcoin.Hash) tbtcabi.BitcoinTxProof {
	t.Helper()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, block := range c.blocks {
		leaves := make([]bitcoin.Hash, len(block.txs))
		for i, tx := range block.txs {
			leaves[i] = tx.Hash()
		}
		for index, leaf := range leaves {
			if leaf != txHash {
				continue
			}
			_, branch := legacyMerkleBranch(leaves, index)
			_, coinbaseBranch := legacyMerkleBranch(leaves, 0)
			concat := func(hashes []bitcoin.Hash) []byte {
				var out []byte
				for _, h := range hashes {
					out = append(out, h[:]...)
				}
				return out
			}
			serializedHeader := block.header.Serialize()
			return tbtcabi.BitcoinTxProof{
				MerkleProof:      concat(branch),
				TxIndexInBlock:   big.NewInt(int64(index)),
				BitcoinHeaders:   serializedHeader[:],
				CoinbasePreimage: sha256.Sum256(block.txs[0].Serialize(bitcoin.Standard)),
				CoinbaseProof:    concat(coinbaseBranch),
			}
		}
	}
	t.Fatalf("transaction %s not mined", txHash.Hex(bitcoin.ReversedByteOrder))
	return tbtcabi.BitcoinTxProof{}
}

func (c *legacyRegtest) allLocked() []*bitcoin.Transaction {
	var all []*bitcoin.Transaction
	for _, block := range c.blocks {
		all = append(all, block.txs...)
	}
	return all
}

func (c *legacyRegtest) Broadcasts() []*bitcoin.Transaction {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]*bitcoin.Transaction(nil), c.broadcasts...)
}

func legacyPaysTo(script []byte, publicKeyHash [20]byte) bool {
	p2pkh, _ := bitcoin.PayToPublicKeyHash(publicKeyHash)
	p2wpkh, _ := bitcoin.PayToWitnessPublicKeyHash(publicKeyHash)
	return bytes.Equal(script, p2pkh) || bytes.Equal(script, p2wpkh)
}

func (c *legacyRegtest) findLocked(hash bitcoin.Hash) *bitcoin.Transaction {
	for _, tx := range append(c.allLocked(), c.mempool...) {
		if tx.Hash() == hash {
			return tx
		}
	}
	return nil
}

func (c *legacyRegtest) touchesLocked(tx *bitcoin.Transaction, publicKeyHash [20]byte) bool {
	for _, output := range tx.Outputs {
		if legacyPaysTo(output.PublicKeyScript, publicKeyHash) {
			return true
		}
	}
	for _, input := range tx.Inputs {
		previous := c.findLocked(input.Outpoint.TransactionHash)
		if previous != nil && int(input.Outpoint.OutputIndex) < len(previous.Outputs) &&
			legacyPaysTo(previous.Outputs[input.Outpoint.OutputIndex].PublicKeyScript, publicKeyHash) {
			return true
		}
	}
	return false
}

func (c *legacyRegtest) spentLocked(outpoint *bitcoin.TransactionOutpoint, includeMempool bool) bool {
	txs := c.allLocked()
	if includeMempool {
		txs = append(txs, c.mempool...)
	}
	for _, tx := range txs {
		for _, input := range tx.Inputs {
			if input.Outpoint.TransactionHash == outpoint.TransactionHash &&
				input.Outpoint.OutputIndex == outpoint.OutputIndex {
				return true
			}
		}
	}
	return false
}

func (c *legacyRegtest) utxosLocked(txs []*bitcoin.Transaction, publicKeyHash [20]byte) []*bitcoin.UnspentTransactionOutput {
	var utxos []*bitcoin.UnspentTransactionOutput
	for _, tx := range txs {
		for i, output := range tx.Outputs {
			outpoint := &bitcoin.TransactionOutpoint{TransactionHash: tx.Hash(), OutputIndex: uint32(i)}
			if legacyPaysTo(output.PublicKeyScript, publicKeyHash) && !c.spentLocked(outpoint, true) {
				utxos = append(utxos, &bitcoin.UnspentTransactionOutput{Outpoint: outpoint, Value: output.Value})
			}
		}
	}
	return utxos
}

func (c *legacyRegtest) GetTransaction(hash bitcoin.Hash) (*bitcoin.Transaction, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if tx := c.findLocked(hash); tx != nil {
		return tx, nil
	}
	return nil, fmt.Errorf("transaction %s not found", hash.Hex(bitcoin.ReversedByteOrder))
}

func (c *legacyRegtest) GetTransactionConfirmations(_ context.Context, hash bitcoin.Hash) (uint, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for height, block := range c.blocks {
		for _, tx := range block.txs {
			if tx.Hash() == hash {
				return uint(len(c.blocks) - height), nil
			}
		}
	}
	for _, tx := range c.mempool {
		if tx.Hash() == hash {
			return 0, nil
		}
	}
	return 0, fmt.Errorf("transaction %s not found", hash.Hex(bitcoin.ReversedByteOrder))
}

// BroadcastTransaction is Core's sendrawtransaction: Core validates every
// script and signature before the transaction enters its mempool.
func (c *legacyRegtest) BroadcastTransaction(tx *bitcoin.Transaction) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.broadcasts = append(c.broadcasts, tx)
	if c.findLocked(tx.Hash()) != nil {
		return nil
	}
	return c.sendLocked(tx)
}

func (c *legacyRegtest) GetLatestBlockHeight() (uint, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return uint(len(c.blocks) - 1), nil
}

func (c *legacyRegtest) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if int(height) >= len(c.blocks) {
		return nil, fmt.Errorf("block %d not found", height)
	}
	header := c.blocks[height].header
	return &header, nil
}

func (c *legacyRegtest) GetTransactionMerkleProof(hash bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if int(height) >= len(c.blocks) {
		return nil, fmt.Errorf("block %d not found", height)
	}
	block := c.blocks[height]
	leaves := make([]bitcoin.Hash, len(block.txs))
	for i, tx := range block.txs {
		leaves[i] = tx.Hash()
	}
	for index, leaf := range leaves {
		if leaf == hash {
			_, branch := legacyMerkleBranch(leaves, index)
			nodes := make([]string, len(branch))
			for i, node := range branch {
				nodes[i] = node.Hex(bitcoin.ReversedByteOrder)
			}
			return &bitcoin.TransactionMerkleProof{BlockHeight: height, MerkleNodes: nodes, Position: uint(index)}, nil
		}
	}
	return nil, fmt.Errorf("transaction not in block %d", height)
}

func (c *legacyRegtest) GetTransactionsForPublicKeyHash(publicKeyHash [20]byte, limit int) ([]*bitcoin.Transaction, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var txs []*bitcoin.Transaction
	for _, tx := range c.allLocked() {
		if c.touchesLocked(tx, publicKeyHash) {
			txs = append(txs, tx)
		}
	}
	if len(txs) > limit {
		txs = txs[len(txs)-limit:]
	}
	return txs, nil
}

func (c *legacyRegtest) GetTxHashesForPublicKeyHash(publicKeyHash [20]byte) ([]bitcoin.Hash, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var hashes []bitcoin.Hash
	for _, tx := range c.allLocked() {
		if c.touchesLocked(tx, publicKeyHash) {
			hashes = append(hashes, tx.Hash())
		}
	}
	return hashes, nil
}

func (c *legacyRegtest) GetMempoolForPublicKeyHash(publicKeyHash [20]byte) ([]*bitcoin.Transaction, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var txs []*bitcoin.Transaction
	for _, tx := range c.mempool {
		if c.touchesLocked(tx, publicKeyHash) {
			txs = append(txs, tx)
		}
	}
	return txs, nil
}

func (c *legacyRegtest) GetUtxosForPublicKeyHash(publicKeyHash [20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.utxosLocked(c.allLocked(), publicKeyHash), nil
}

func (c *legacyRegtest) GetMempoolUtxosForPublicKeyHash(publicKeyHash [20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.utxosLocked(c.mempool, publicKeyHash), nil
}

func (c *legacyRegtest) EstimateSatPerVByteFee(uint32) (int64, error) {
	return 1, nil
}

func (c *legacyRegtest) GetCoinbaseTxHash(height uint) (bitcoin.Hash, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if int(height) >= len(c.blocks) {
		return bitcoin.Hash{}, fmt.Errorf("block %d not found", height)
	}
	return c.blocks[height].txs[0].Hash(), nil
}

// --- Deployment and public setup ---------------------------------------------

// legacyWallet is a legacy ECDSA wallet whose key the test holds (FIXTURE
// signer in place of tECDSA).
type legacyWallet struct {
	key *ecdsa.PrivateKey
	id  [32]byte
	pkh [20]byte
}

func newLegacyWallet(t *testing.T, seed byte) legacyWallet {
	t.Helper()
	ethKey, err := crypto.ToECDSA(common.LeftPadBytes([]byte{seed}, 32))
	if err != nil {
		t.Fatal(err)
	}
	// Wallet keys live on tecdsa.Curve in production; the signing path
	// refuses keys on any other curve implementation.
	key := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: tecdsa.Curve, X: ethKey.X, Y: ethKey.Y},
		D:         ethKey.D,
	}
	return legacyWallet{
		key: key,
		id:  crypto.Keccak256Hash([]byte("legacy wallet"), []byte{seed}),
		pkh: bitcoin.PublicKeyHash(&key.PublicKey),
	}
}

type legacyWorld struct {
	t        *testing.T
	env      legacyEnvironment
	baseline *legacyBaseline
	anvilURL string
	admin    *ethclient.Client
	adminKey *ecdsa.PrivateKey
	operator common.Address
	chainID  *big.Int

	bridgeABI    abi.ABI
	validatorABI abi.ABI
	registryABI  abi.ABI
	relayABI     abi.ABI

	bridge common.Address
	bank   common.Address
	// reimbursementPool and maintainerProxy are real 40a11d1d contracts.
	reimbursementPool common.Address
	maintainerProxy   common.Address
	validator         common.Address
	registry          common.Address
	relay             common.Address
	legacy            *tbtcabi.Bridge
	fixture           *bind.BoundContract

	btc     *legacyRegtest
	source  legacyWallet
	target  legacyWallet
	main    *bitcoin.UnspentTransactionOutput
	keyFile string
}

func (w *legacyWorld) opts() *bind.TransactOpts {
	opts, err := bind.NewKeyedTransactorWithChainID(w.adminKey, w.chainID)
	if err != nil {
		w.t.Fatal(err)
	}
	opts.Context = context.Background()
	return opts
}

// legacyWaitMined waits for tx's receipt. bind.WaitMined polls once a second,
// which made every setup transaction cost a second although Anvil mines at
// once.
func legacyWaitMined(ctx context.Context, b bind.DeployBackend, tx *types.Transaction) (*types.Receipt, error) {
	for {
		receipt, err := b.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// send submits one admin transaction and requires it to succeed.
func (w *legacyWorld) send(what string, fn func(*bind.TransactOpts) (*types.Transaction, error)) {
	w.t.Helper()
	tx, err := fn(w.opts())
	if err != nil {
		w.t.Fatalf("%s: %v", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	receipt, err := legacyWaitMined(ctx, w.admin, tx)
	if err != nil {
		w.t.Fatalf("%s: %v", what, err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		w.t.Fatalf("%s: reverted", what)
	}
}

func (w *legacyWorld) deploy(what string, parsed abi.ABI, code []byte, args ...interface{}) common.Address {
	w.t.Helper()
	address, tx, _, err := bind.DeployContract(w.opts(), parsed, code, w.admin, args...)
	if err != nil {
		w.t.Fatalf("deploy %s: %v", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil {
		w.t.Fatalf("deploy %s: %v", what, err)
	} else if receipt.ContractAddress != address {
		w.t.Fatalf("deploy %s: receipt names contract %s", what, receipt.ContractAddress)
	}
	code, err = w.admin.CodeAt(context.Background(), address, nil)
	if err != nil || len(code) == 0 || len(code) > 24576 {
		w.t.Fatalf("deploy %s: code size %d (EIP-170 stays on)", what, len(code))
	}
	return address
}

// deployBaseline deploys a baseline artifact, linking its libraries.
func (w *legacyWorld) deployBaseline(libraries map[string]common.Address, source, name string, args ...interface{}) (common.Address, abi.ABI) {
	w.t.Helper()
	artifact := w.baseline.artifact(source, name)
	code := strings.TrimPrefix(artifact.Bytecode, "0x")
	for file, names := range artifact.LinkReferences {
		for library, positions := range names {
			key := file + ":" + library
			address, ok := libraries[key]
			if !ok {
				address, _ = w.deployBaseline(libraries, file, library)
				libraries[key] = address
			}
			for _, p := range positions {
				code = code[:2*p.Start] + hex.EncodeToString(address[:]) + code[2*(p.Start+p.Length):]
			}
		}
	}
	parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		w.t.Fatal(err)
	}
	return w.deploy(source+":"+name, parsed, common.FromHex(code), args...), parsed
}

// newLegacyWorld deploys the pre-FROST system and brings a funded legacy
// wallet into MovingFunds with one Live legacy target, through public calls.
func newLegacyWorld(t *testing.T) *legacyWorld {
	t.Helper()
	env := legacyEnv(t)
	return newLegacyWorldOn(t, env, openLegacyBaseline(t, env), nil)
}

// newLegacyWorldOn deploys the given tbtc-v2 build. beforeWallets, if set,
// runs after the Bridge is configured and before any wallet exists.
func newLegacyWorldOn(t *testing.T, env legacyEnvironment, build *legacyBaseline, beforeWallets func(*legacyWorld)) *legacyWorld {
	t.Helper()
	w := &legacyWorld{t: t, env: env, baseline: build}
	w.anvilURL = startLegacyAnvil(t, env)
	var err error
	w.admin, err = ethclient.Dial(w.anvilURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.admin.Close)
	w.chainID, err = w.admin.ChainID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	w.adminKey, err = crypto.HexToECDSA(legacyAdminKey)
	if err != nil {
		t.Fatal(err)
	}
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	w.operator = crypto.PubkeyToAddress(operatorKey.PublicKey)
	w.keyFile = legacyKeyFile(t, operatorKey)
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)

	// Labelled fixtures first: the registry and relay stand-ins.
	registryABI, registryCode := w.baseline.fixture("LegacyWalletRegistryFixture")
	relayABI, relayCode := w.baseline.fixture("LegacyRegtestRelayFixture")
	w.registryABI, w.relayABI = registryABI, relayABI
	w.registry = w.deploy("registry fixture", registryABI, registryCode)
	w.relay = w.deploy("relay fixture", relayABI, relayCode)
	w.fixture = bind.NewBoundContract(w.registry, registryABI, w.admin, w.admin, w.admin)

	// The real pre-FROST contracts, behind a real transparent proxy.
	libraries := map[string]common.Address{}
	var bankABI abi.ABI
	w.bank, bankABI = w.deployBaseline(libraries, "contracts/bank/Bank.sol", "Bank")
	implementation, bridgeABI := w.deployBaseline(libraries, "contracts/bridge/Bridge.sol", "Bridge")
	w.bridgeABI = bridgeABI
	proxyAdmin, _ := w.deployBaseline(libraries, "@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol", "ProxyAdmin")
	// The real ReimbursementPool the Bridge refunds commitment gas from; a
	// zero gas price cap makes every refund zero.
	reimbursementPool, reimbursementABI := w.deployBaseline(libraries,
		"@keep-network/random-beacon/contracts/ReimbursementPool.sol", "ReimbursementPool",
		big.NewInt(0), big.NewInt(0))
	initializer, err := bridgeABI.Pack("initialize", w.bank, w.relay, admin, w.registry, reimbursementPool, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	w.bridge, _ = w.deployBaseline(
		libraries,
		"@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol",
		"TransparentUpgradeableProxy",
		implementation, proxyAdmin, initializer,
	)
	w.validator, w.validatorABI = w.deployBaseline(libraries, "contracts/bridge/WalletProposalValidator.sol", "WalletProposalValidator", w.bridge)
	w.reimbursementPool = reimbursementPool
	// The real MaintainerProxy: the client attaches to it at startup.
	w.maintainerProxy, _ = w.deployBaseline(libraries, "contracts/maintainer/MaintainerProxy.sol", "MaintainerProxy", w.bridge, reimbursementPool)

	pool := bind.NewBoundContract(reimbursementPool, reimbursementABI, w.admin, w.admin, w.admin)
	w.send("reimbursement authorization", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return pool.Transact(o, "authorize", w.bridge)
	})
	bank := bind.NewBoundContract(w.bank, bankABI, w.admin, w.admin, w.admin)
	w.send("bank bridge", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return bank.Transact(o, "updateBridge", w.bridge)
	})
	w.send("fixture bind", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "bind", w.bridge, w.operator)
	})
	w.legacy, err = tbtcabi.NewBridge(w.bridge, w.admin)
	if err != nil {
		t.Fatal(err)
	}
	w.send("spv maintainer", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SetSpvMaintainerStatus(o, admin, true)
	})
	// Governance test values: any wallet may be created at any time.
	parameters, err := w.legacy.WalletParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.send("wallet parameters", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.UpdateWalletParameters(o, 0, 0,
			parameters.WalletCreationMaxBtcBalance, parameters.WalletClosureMinBtcBalance,
			parameters.WalletMaxAge, parameters.WalletMaxBtcTransfer, parameters.WalletClosingPeriod)
	})
	// Fund the node's operator for its own commitment transaction.
	if err := w.admin.Client().Call(nil, "anvil_setBalance", w.operator, "0x3635c9adc5dea00000"); err != nil {
		t.Fatal(err)
	}

	if beforeWallets != nil {
		beforeWallets(w)
	}
	w.btc = newLegacyRegtest(t, env)
	w.source = newLegacyWallet(t, 11)
	w.target = newLegacyWallet(t, 12)
	w.registerWallet(w.source)
	w.registerWallet(w.target)
	w.fundSource()
	w.send("heartbeat failure", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "heartbeatFailed", w.source.id,
			common.BytesToHash(w.source.key.PublicKey.X.FillBytes(make([]byte, 32))),
			common.BytesToHash(w.source.key.PublicKey.Y.FillBytes(make([]byte, 32))))
	})
	sourceWallet, err := w.legacy.Wallets(&bind.CallOpts{}, w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	if sourceWallet.State != 2 { // Wallets.WalletState.MovingFunds
		t.Fatalf("setup: source wallet state %d, want MovingFunds", sourceWallet.State)
	}
	return w
}

func (w *legacyWorld) registerWallet(wallet legacyWallet) {
	w.t.Helper()
	w.send("request wallet", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.RequestNewWallet(o, tbtcabi.BitcoinTxUTXO{})
	})
	w.send("publish wallet", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.fixture.Transact(o, "publish", wallet.id,
			common.BytesToHash(wallet.key.PublicKey.X.FillBytes(make([]byte, 32))),
			common.BytesToHash(wallet.key.PublicKey.Y.FillBytes(make([]byte, 32))))
	})
}

func legacyTxInfo(tx *bitcoin.Transaction) tbtcabi.BitcoinTxInfo {
	return tbtcabi.BitcoinTxInfo{
		Version:      tx.SerializeVersion(),
		InputVector:  tx.SerializeInputs(),
		OutputVector: tx.SerializeOutputs(),
		Locktime:     tx.SerializeLocktime(),
	}
}

// fundSource reveals a deposit to the source wallet and proves its sweep,
// signed with the source wallet's key, so the Bridge records a main UTXO.
func (w *legacyWorld) fundSource() {
	t := w.t
	t.Helper()
	admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
	header, err := w.admin.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	deposit := &tbtc.Deposit{
		Depositor:           chain.Address(admin.Hex()),
		BlindingFactor:      [8]byte{0x1e, 0x9a},
		WalletPublicKeyHash: w.source.pkh,
		RefundPublicKeyHash: [20]byte{9},
	}
	binary.LittleEndian.PutUint32(deposit.RefundLocktime[:], uint32(header.Time+30*24*3600))
	script, err := deposit.Script()
	if err != nil {
		t.Fatal(err)
	}
	locking, err := bitcoin.PayToWitnessScriptHash(bitcoin.WitnessScriptHash(script))
	if err != nil {
		t.Fatal(err)
	}
	scriptHash := sha256.Sum256(script)
	address, err := btcutil.NewAddressWitnessScriptHash(scriptHash[:], &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	funding := w.btc.pay(t, address.EncodeAddress(), legacyDepositValue)
	fundingIndex := -1
	for i, output := range funding.Outputs {
		if bytes.Equal(output.PublicKeyScript, locking) && output.Value == legacyDepositValue {
			fundingIndex = i
		}
	}
	if fundingIndex < 0 {
		t.Fatal("Core did not create the deposit output")
	}
	w.send("reveal deposit", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.RevealDeposit(o, legacyTxInfo(funding), tbtcabi.DepositDepositRevealInfo{
			FundingOutputIndex: uint32(fundingIndex),
			BlindingFactor:     deposit.BlindingFactor,
			WalletPubKeyHash:   deposit.WalletPublicKeyHash,
			RefundPubKeyHash:   deposit.RefundPublicKeyHash,
			RefundLocktime:     deposit.RefundLocktime,
		})
	})

	builder := bitcoin.NewTransactionBuilder(w.btc)
	if err := builder.AddScriptHashInput(&bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: uint32(fundingIndex)},
		Value:    legacyDepositValue,
	}, script); err != nil {
		t.Fatal(err)
	}
	walletScript, err := bitcoin.PayToWitnessPublicKeyHash(w.source.pkh)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddOutput(&bitcoin.TransactionOutput{Value: legacyDepositValue - legacySweepFee, PublicKeyScript: walletScript})
	sweep := legacySign(t, builder, w.source.key)
	w.btc.mine(t, sweep)
	w.send("sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.legacy.SubmitDepositSweepProof(o, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()), tbtcabi.BitcoinTxUTXO{}, common.Address{})
	})
	w.main = &bitcoin.UnspentTransactionOutput{
		Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: sweep.Hash(), OutputIndex: 0},
		Value:    legacyDepositValue - legacySweepFee,
	}
}

func legacySignatures(key *ecdsa.PrivateKey, messages []*big.Int) ([]*tecdsa.Signature, error) {
	signatures := make([]*tecdsa.Signature, len(messages))
	for i, message := range messages {
		r, s, err := ecdsa.Sign(rand.Reader, key, message.FillBytes(make([]byte, 32)))
		if err != nil {
			return nil, err
		}
		signatures[i] = &tecdsa.Signature{R: r, S: s}
	}
	return signatures, nil
}

func legacySign(t *testing.T, builder *bitcoin.TransactionBuilder, key *ecdsa.PrivateKey) *bitcoin.Transaction {
	t.Helper()
	hashes, err := builder.ComputeSignatureHashes()
	if err != nil {
		t.Fatal(err)
	}
	signatures, err := legacySignatures(key, hashes)
	if err != nil {
		t.Fatal(err)
	}
	containers := make([]*bitcoin.SignatureContainer, len(signatures))
	for i, signature := range signatures {
		containers[i] = &bitcoin.SignatureContainer{R: signature.R, S: signature.S, PublicKey: &key.PublicKey}
	}
	tx, err := builder.AddSignatures(containers)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func legacyKeyFile(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	store := keystore.NewKeyStore(t.TempDir(), keystore.LightScryptN, keystore.LightScryptP)
	account, err := store.ImportECDSA(key, "legacy-unchanged")
	if err != nil {
		t.Fatal(err)
	}
	return account.URL.Path
}

// clientConfig is a default, FROST-unconfigured client configuration: only
// the legacy contract addresses a production node is given.
func (w *legacyWorld) clientConfig(url string) commonethereum.Config {
	config := commonethereum.Config{
		Network:             commonethereum.Developer,
		URL:                 url,
		Account:             commonethereum.Account{KeyFile: w.keyFile, KeyFilePassword: "legacy-unchanged"},
		ContractAddresses:   map[string]string{},
		MiningCheckInterval: time.Second,
	}
	// FIXTURE: the registry fixture also answers the RandomBeacon and
	// TokenStaking startup reads (legacySurface models those roles).
	config.SetContractAddress(chainethereum.RandomBeaconContractName, w.registry.Hex())
	config.SetContractAddress(chainethereum.TokenStakingContractName, w.registry.Hex())
	config.SetContractAddress(chainethereum.MaintainerProxyContractName, w.maintainerProxy.Hex())
	config.SetContractAddress(chainethereum.BridgeContractName, w.bridge.Hex())
	config.SetContractAddress(chainethereum.WalletProposalValidatorContractName, w.validator.Hex())
	return config
}

// connect builds the production chain handles through the public Connect
// path, through the given recorder.
func (w *legacyWorld) connect(recorder *legacyRecorder) (*chainethereum.TbtcChain, error) {
	_, tbtcChain, _, _, _, err := chainethereum.Connect(context.Background(), w.clientConfig(recorder.URL()))
	return tbtcChain, err
}

// mineInBackground keeps Anvil producing blocks (the client waits on block
// confirmations) until the returned function is called.
func (w *legacyWorld) mineInBackground() func() {
	client, err := rpc.Dial(w.anvilURL)
	if err != nil {
		w.t.Fatal(err)
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = client.Call(nil, "evm_mine")
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-stopped
			client.Close()
		})
	}
}

// waitForBlock is the node's block waiter, bounded by ctx.
func (w *legacyWorld) waitForBlock(ctx context.Context, height uint64) error {
	for {
		current, err := w.admin.BlockNumber(ctx)
		if err == nil && current >= height {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// selectors returns the 4-byte method IDs of an ABI.
func legacySelectors(parsed abi.ABI) map[string]string {
	selectors := map[string]string{}
	for name, method := range parsed.Methods {
		selectors[hex.EncodeToString(method.ID)] = name
	}
	return selectors
}

// legacyRole is one client-visible production role and its pinned ABI.
type legacyRole struct {
	name string
	abi  abi.ABI
}

// legacySurface maps each deployed address to the production roles it plays.
// It is the D14 oracle (provisional reading: base functions only, any count
// and order). Permitted selectors come only from production ABIs pinned by
// the build (40a11d1d sources, or node_modules fixed by its yarn.lock), bound
// to the address that plays the role. A fixture's own ABI never counts: the
// registry FIXTURE address is permitted exactly the union of the production
// roles it stands in for. Fixture setup and admin calls go straight to Anvil
// and never appear in a client trace.
type legacySurface map[common.Address][]legacyRole

type legacyAddresses struct {
	bridge, validator, bank, reimbursementPool, relay, maintainerProxy, registry common.Address
}

// nodeModulesABI reads a dependency artifact pinned by the build's yarn.lock.
func (b *legacyBaseline) nodeModulesABI(path string) abi.ABI {
	b.t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.dir, "solidity", "node_modules", path))
	if err != nil {
		b.t.Fatalf("dependency artifact %s: %v", path, err)
	}
	var artifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		b.t.Fatal(err)
	}
	parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		b.t.Fatal(err)
	}
	return parsed
}

func (b *legacyBaseline) buildABI(source, name string) abi.ABI {
	b.t.Helper()
	parsed, err := abi.JSON(bytes.NewReader(b.artifact(source, name).ABI))
	if err != nil {
		b.t.Fatal(err)
	}
	return parsed
}

// newLegacySurface binds each production role to its address.
func newLegacySurface(b *legacyBaseline, a legacyAddresses) legacySurface {
	role := func(name string, parsed abi.ABI) legacyRole { return legacyRole{name: name, abi: parsed} }
	surface := legacySurface{
		a.bridge:            {role("Bridge", b.buildABI("contracts/bridge/Bridge.sol", "Bridge"))},
		a.validator:         {role("WalletProposalValidator", b.buildABI("contracts/bridge/WalletProposalValidator.sol", "WalletProposalValidator"))},
		a.bank:              {role("Bank", b.buildABI("contracts/bank/Bank.sol", "Bank"))},
		a.reimbursementPool: {role("ReimbursementPool", b.buildABI("@keep-network/random-beacon/contracts/ReimbursementPool.sol", "ReimbursementPool"))},
		a.relay:             {role("LightRelay", b.buildABI("contracts/relay/LightRelay.sol", "LightRelay"))},
		a.maintainerProxy:   {role("MaintainerProxy", b.buildABI("contracts/maintainer/MaintainerProxy.sol", "MaintainerProxy"))},
		// The registry FIXTURE stands in for four production roles: the
		// ECDSA WalletRegistry, the RandomBeacon, TokenStaking (both name it
		// as staking()) and the sortition pool both name (sortitionPool()).
		a.registry: {
			role("WalletRegistry", b.buildABI("@keep-network/ecdsa/contracts/WalletRegistry.sol", "WalletRegistry")),
			role("SortitionPool", b.buildABI("@keep-network/sortition-pools/contracts/SortitionPool.sol", "SortitionPool")),
			role("RandomBeacon", b.nodeModulesABI("@keep-network/random-beacon/artifacts/RandomBeacon.json")),
			role("TokenStaking", b.nodeModulesABI("@threshold-network/solidity-contracts/artifacts/TokenStaking.json")),
		},
	}
	return surface
}

func (w *legacyWorld) surface() legacySurface {
	return newLegacySurface(w.baseline, legacyAddresses{
		bridge: w.bridge, validator: w.validator, bank: w.bank,
		reimbursementPool: w.reimbursementPool, relay: w.relay,
		maintainerProxy: w.maintainerProxy, registry: w.registry,
	})
}

// violations lists every distinct call outside the surface, with its count.
func (s legacySurface) violations(calls []legacyRPCCall) []string {
	var violations []string
	counts := map[string]int{}
	for _, call := range calls {
		if call.Selector == "" {
			continue
		}
		roles, ok := s[call.To]
		var violation string
		if !ok {
			violation = fmt.Sprintf("%s to unknown %s selector 0x%s", call.Method, call.To.Hex(), call.Selector)
		} else {
			permitted := false
			var names []string
			for _, r := range roles {
				names = append(names, r.name)
				_, found := legacySelectors(r.abi)[call.Selector]
				permitted = permitted || found
			}
			if permitted {
				continue
			}
			violation = fmt.Sprintf("%s to %s@40a11d1d selector 0x%s is not in its ABI",
				call.Method, strings.Join(names, "+"), call.Selector)
		}
		if counts[violation] == 0 {
			violations = append(violations, violation)
		}
		counts[violation]++
	}
	for i, violation := range violations {
		violations[i] = fmt.Sprintf("%s (x%d)", violation, counts[violation])
	}
	return violations
}

// assertBaselineSurface is the RPC half of the invariant: every contract call
// the client made used a function of a production role played at that
// address (legacySurface).
func (w *legacyWorld) assertBaselineSurface(t *testing.T, what string, calls []legacyRPCCall) {
	t.Helper()
	violations := w.surface().violations(calls)
	if len(violations) > 0 {
		t.Errorf("%s: legacy RPC surface: the client called functions the pre-FROST system does not have:\n  %s",
			what, strings.Join(violations, "\n  "))
	}
}
