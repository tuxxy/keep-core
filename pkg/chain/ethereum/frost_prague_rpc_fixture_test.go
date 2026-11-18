package ethereum

// FIXTURE: an in-process Ethereum JSON-RPC backend for the Prague block hash
// tests in frost_prague_hash_test.go. It is not a chain: it serves a fixed set
// of block headers and the few contract reads and logs each test scripts. What
// it does model faithfully is the property under test: every block's `hash`
// field is the true hash, keccak256 of the RLP of the header including the
// Prague `requestsHash` field (EIP-7685), exactly as a Prague node returns it,
// and state is served only at a block hash the node has returned. A hash that
// go-ethereum v1.13.15 recomputes from a decoded Prague header names no block
// here, as on a real Prague node.
//
// Refs: chaosnet-terminal/FINDING-FROST-PRAGUE-HEADER-HASH.md (2026-11-18).

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// pragueHeaderRLP is the Prague (EIP-7685) header layout: go-ethereum
// v1.13.15's types.Header followed by requestsHash. Optional trailing fields
// are omitted when nil, as in go-ethereum, so a nil RequestsHash encodes a
// Cancun header byte for byte (checked by TestPragueFixtureHeaderHash).
type pragueHeaderRLP struct {
	ParentHash       common.Hash
	UncleHash        common.Hash
	Coinbase         common.Address
	Root             common.Hash
	TxHash           common.Hash
	ReceiptHash      common.Hash
	Bloom            types.Bloom
	Difficulty       *big.Int
	Number           *big.Int
	GasLimit         uint64
	GasUsed          uint64
	Time             uint64
	Extra            []byte
	MixDigest        common.Hash
	Nonce            types.BlockNonce
	BaseFee          *big.Int     `rlp:"optional"`
	WithdrawalsHash  *common.Hash `rlp:"optional"`
	BlobGasUsed      *uint64      `rlp:"optional"`
	ExcessBlobGas    *uint64      `rlp:"optional"`
	ParentBeaconRoot *common.Hash `rlp:"optional"`
	RequestsHash     *common.Hash `rlp:"optional"`
}

// trueBlockHash is the hash a node returns: keccak256(rlp(header)), with
// requestsHash appended when the header is Prague-shaped (requests != nil).
func trueBlockHash(h *types.Header, requests *common.Hash) common.Hash {
	enc, err := rlp.EncodeToBytes(&pragueHeaderRLP{
		ParentHash: h.ParentHash, UncleHash: h.UncleHash, Coinbase: h.Coinbase, Root: h.Root,
		TxHash: h.TxHash, ReceiptHash: h.ReceiptHash, Bloom: h.Bloom, Difficulty: h.Difficulty,
		Number: h.Number, GasLimit: h.GasLimit, GasUsed: h.GasUsed, Time: h.Time, Extra: h.Extra,
		MixDigest: h.MixDigest, Nonce: h.Nonce, BaseFee: h.BaseFee, WithdrawalsHash: h.WithdrawalsHash,
		BlobGasUsed: h.BlobGasUsed, ExcessBlobGas: h.ExcessBlobGas, ParentBeaconRoot: h.ParentBeaconRoot,
		RequestsHash: requests,
	})
	if err != nil {
		panic(err)
	}
	return crypto.Keccak256Hash(enc)
}

// fixtureBlock is one served block: its header as go-ethereum v1.13.15 can
// decode it, the Prague requestsHash (nil for a Cancun-shaped block), and the
// true hash the RPC returns.
type fixtureBlock struct {
	header   *types.Header
	requests *common.Hash
	hash     common.Hash
}

func sealFixtureBlock(h *types.Header, requests *common.Hash) fixtureBlock {
	return fixtureBlock{header: h, requests: requests, hash: trueBlockHash(h, requests)}
}

// emptyRequestsHash is the EIP-7685 commitment to an empty request list,
// sha256(""), which is what anvil and mainnet put in blocks without requests.
var emptyRequestsHash = common.Hash(sha256.Sum256(nil))

// fixtureChain builds blocks 0..head. Prague blocks carry requestsHash;
// Cancun blocks do not. All other post-London fields are set in both.
func fixtureChain(prague bool, head uint64) []fixtureBlock {
	blocks := make([]fixtureBlock, 0, head+1)
	for n := uint64(0); n <= head; n++ {
		zero := uint64(0)
		beacon := crypto.Keccak256Hash([]byte("beacon"), new(big.Int).SetUint64(n).Bytes())
		withdrawals := types.EmptyWithdrawalsHash
		h := &types.Header{
			UncleHash: types.EmptyUncleHash, Root: crypto.Keccak256Hash([]byte("state"), new(big.Int).SetUint64(n).Bytes()),
			TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: new(big.Int),
			Number: new(big.Int).SetUint64(n), GasLimit: 30_000_000, Time: 1_750_000_000 + 12*n, Extra: []byte{},
			MixDigest: crypto.Keccak256Hash([]byte("mix"), new(big.Int).SetUint64(n).Bytes()),
			BaseFee:   big.NewInt(1_000_000_000), WithdrawalsHash: &withdrawals,
			BlobGasUsed: &zero, ExcessBlobGas: &zero, ParentBeaconRoot: &beacon,
		}
		if n > 0 {
			h.ParentHash = blocks[n-1].hash
		}
		var requests *common.Hash
		if prague {
			r := emptyRequestsHash
			requests = &r
		}
		blocks = append(blocks, sealFixtureBlock(h, requests))
	}
	return blocks
}

// siblingBlock returns a different block at the same height. With
// requestsOnly set, it differs only in requestsHash, so go-ethereum v1.13.15
// recomputes the same hash for both while the RPC hashes differ (only
// meaningful for Prague blocks). Otherwise its state root differs too.
func siblingBlock(b fixtureBlock, requestsOnly bool) fixtureBlock {
	h := types.CopyHeader(b.header)
	requests := b.requests
	if requestsOnly {
		r := crypto.Keccak256Hash([]byte("sibling requests"))
		requests = &r
	} else {
		h.Root = crypto.Keccak256Hash([]byte("sibling state"), h.Root.Bytes())
	}
	return sealFixtureBlock(h, requests)
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// contractRead answers one eth_call. at is the block the call resolved to.
type contractRead func(input []byte, at fixtureBlock) ([]byte, error)

type ethCallRecord struct {
	to       common.Address
	selector [4]byte
	// byHash is set when the call named a block by hash; hash is that hash.
	// Otherwise block is the raw block parameter (tag, number or object).
	byHash bool
	hash   common.Hash
	block  string
}

// fixtureRPC is the JSON-RPC backend. All fields are guarded by mu.
type fixtureRPC struct {
	t         *testing.T
	mu        sync.Mutex
	canonical []fixtureBlock
	known     map[common.Hash]fixtureBlock
	reads     map[common.Address]map[[4]byte]contractRead
	logs      []types.Log
	calls     []ethCallRecord
	// afterCall runs, with mu held, after an eth_call with this selector has
	// been answered: it is how a test reorganizes the chain mid-read.
	afterCall map[[4]byte]func(f *fixtureRPC)
}

func newFixtureRPC(t *testing.T, blocks []fixtureBlock) *fixtureRPC {
	f := &fixtureRPC{t: t, canonical: append([]fixtureBlock(nil), blocks...), known: map[common.Hash]fixtureBlock{},
		reads: map[common.Address]map[[4]byte]contractRead{}, afterCall: map[[4]byte]func(*fixtureRPC){}}
	for _, b := range blocks {
		f.known[b.hash] = b
	}
	return f
}

// replace makes b canonical at its height (a reorg). The replaced block stays
// known by hash, as a node keeps recently orphaned blocks.
func (f *fixtureRPC) replace(b fixtureBlock) {
	f.canonical[b.header.Number.Uint64()] = b
	f.known[b.hash] = b
}

// advance appends a new canonical block on top of the head, of the same
// shape: the chain grows, nothing is reorganized. Callers hold mu (it is
// meant for afterCall hooks).
func (f *fixtureRPC) advance() fixtureBlock {
	parent := f.head()
	n := parent.header.Number.Uint64() + 1
	h := types.CopyHeader(parent.header)
	h.ParentHash = parent.hash
	h.Number = new(big.Int).SetUint64(n)
	h.Time = parent.header.Time + 12
	h.Root = crypto.Keccak256Hash([]byte("state"), new(big.Int).SetUint64(n).Bytes())
	b := sealFixtureBlock(h, parent.requests)
	f.canonical = append(f.canonical, b)
	f.known[b.hash] = b
	return b
}

func (f *fixtureRPC) serve(to common.Address, selector [4]byte, read contractRead) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads[to] == nil {
		f.reads[to] = map[[4]byte]contractRead{}
	}
	f.reads[to][selector] = read
}

func (f *fixtureRPC) addLog(l types.Log) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, l)
}

func (f *fixtureRPC) onCall(selector [4]byte, hook func(*fixtureRPC)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.afterCall[selector] = hook
}

func (f *fixtureRPC) callsTo(selector [4]byte) []ethCallRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ethCallRecord
	for _, c := range f.calls {
		if c.selector == selector {
			out = append(out, c)
		}
	}
	return out
}

func (f *fixtureRPC) start() string {
	server := httptest.NewServer(http.HandlerFunc(f.handle))
	f.t.Cleanup(server.Close)
	return server.URL
}

type rpcRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (f *fixtureRPC) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []rpcRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := make([]rpcResponse, 0, len(batch))
		for _, req := range batch {
			out = append(out, f.answer(req))
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(f.answer(req))
}

func (f *fixtureRPC) answer(req rpcRequest) rpcResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	result, err := f.dispatch(req)
	out := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if err != nil {
		e, ok := err.(*rpcError)
		if !ok {
			e = &rpcError{Code: -32000, Message: err.Error()}
		}
		out.Error = e
		return out
	}
	if result == nil {
		result = json.RawMessage("null")
	}
	out.Result = result
	return out
}

func param(req rpcRequest, i int, v interface{}) error {
	if i >= len(req.Params) {
		return &rpcError{Code: -32602, Message: fmt.Sprintf("missing parameter %d", i)}
	}
	if err := json.Unmarshal(req.Params[i], v); err != nil {
		return &rpcError{Code: -32602, Message: err.Error()}
	}
	return nil
}

func (f *fixtureRPC) head() fixtureBlock { return f.canonical[len(f.canonical)-1] }

// byTag resolves "latest", "earliest" or a hex number to a canonical block.
func (f *fixtureRPC) byTag(tag string) (fixtureBlock, bool) {
	switch tag {
	case "latest", "safe", "finalized":
		return f.head(), true
	case "earliest":
		return f.canonical[0], true
	}
	n, err := hexutil.DecodeUint64(tag)
	if err != nil || n >= uint64(len(f.canonical)) {
		return fixtureBlock{}, false
	}
	return f.canonical[n], true
}

func blockJSON(b fixtureBlock) (json.RawMessage, error) {
	raw, err := json.Marshal(b.header)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	// go-ethereum's MarshalJSON writes its own recomputed hash; a node
	// returns the true one.
	m["hash"] = b.hash
	if b.requests != nil {
		m["requestsHash"] = *b.requests
	}
	m["totalDifficulty"] = "0x0"
	m["size"] = "0x267"
	m["transactions"] = []interface{}{}
	m["uncles"] = []interface{}{}
	m["withdrawals"] = []interface{}{}
	return json.Marshal(m)
}

func (f *fixtureRPC) dispatch(req rpcRequest) (interface{}, error) {
	switch req.Method {
	case "eth_chainId":
		return hexutil.Uint64(31337), nil
	case "eth_blockNumber":
		return hexutil.Uint64(f.head().header.Number.Uint64()), nil
	case "eth_getBlockByNumber":
		var tag string
		if err := param(req, 0, &tag); err != nil {
			return nil, err
		}
		b, ok := f.byTag(tag)
		if !ok {
			return nil, nil
		}
		return blockJSON(b)
	case "eth_getBlockByHash":
		var hash common.Hash
		if err := param(req, 0, &hash); err != nil {
			return nil, err
		}
		b, ok := f.known[hash]
		if !ok {
			return nil, nil
		}
		return blockJSON(b)
	case "eth_getCode":
		return hexutil.Bytes{0x60, 0x00}, nil
	case "eth_call":
		return f.ethCall(req)
	case "eth_getLogs":
		return f.getLogs(req)
	}
	return nil, &rpcError{Code: -32601, Message: "the method " + req.Method + " does not exist/is not available"}
}

func (f *fixtureRPC) ethCall(req rpcRequest) (interface{}, error) {
	var msg struct {
		To    *common.Address `json:"to"`
		Data  hexutil.Bytes   `json:"data"`
		Input hexutil.Bytes   `json:"input"`
	}
	if err := param(req, 0, &msg); err != nil {
		return nil, err
	}
	input := msg.Input
	if len(input) == 0 {
		input = msg.Data
	}
	if msg.To == nil || len(input) < 4 {
		return nil, &rpcError{Code: -32602, Message: "malformed call"}
	}
	record := ethCallRecord{to: *msg.To}
	copy(record.selector[:], input[:4])
	var at fixtureBlock
	if len(req.Params) < 2 {
		at = f.head()
		record.block = "latest"
	} else {
		record.block = string(req.Params[1])
		var tag string
		if json.Unmarshal(req.Params[1], &tag) == nil {
			b, ok := f.byTag(tag)
			if !ok {
				f.calls = append(f.calls, record)
				return nil, &rpcError{Code: -32000, Message: "header not found"}
			}
			at = b
		} else {
			var sel struct {
				BlockHash   *common.Hash    `json:"blockHash"`
				BlockNumber *hexutil.Uint64 `json:"blockNumber"`
			}
			if err := json.Unmarshal(req.Params[1], &sel); err != nil {
				return nil, &rpcError{Code: -32602, Message: err.Error()}
			}
			switch {
			case sel.BlockHash != nil:
				record.byHash, record.hash = true, *sel.BlockHash
				b, ok := f.known[*sel.BlockHash]
				if !ok {
					// What geth answers for a block hash it does not have
					// (anvil answers -32001 "Resource not found"; the
					// adapter depends on neither).
					f.calls = append(f.calls, record)
					return nil, &rpcError{Code: -32000, Message: "header for hash not found"}
				}
				at = b
			case sel.BlockNumber != nil:
				b, ok := f.byTag(hexutil.EncodeUint64(uint64(*sel.BlockNumber)))
				if !ok {
					f.calls = append(f.calls, record)
					return nil, &rpcError{Code: -32000, Message: "header not found"}
				}
				at = b
			default:
				return nil, &rpcError{Code: -32602, Message: "empty block selector"}
			}
		}
	}
	f.calls = append(f.calls, record)
	read := f.reads[record.to][record.selector]
	if read == nil {
		// Empty return, the shape of a call to a function a contract lacks.
		return hexutil.Bytes{}, nil
	}
	out, err := read(input[4:], at)
	if err != nil {
		return nil, err
	}
	if hook := f.afterCall[record.selector]; hook != nil {
		delete(f.afterCall, record.selector)
		hook(f)
	}
	return hexutil.Bytes(out), nil
}

func (f *fixtureRPC) getLogs(req rpcRequest) (interface{}, error) {
	var q struct {
		Address   []common.Address  `json:"address"`
		Topics    []json.RawMessage `json:"topics"`
		FromBlock *string           `json:"fromBlock"`
		ToBlock   *string           `json:"toBlock"`
		BlockHash *common.Hash      `json:"blockHash"`
	}
	if err := param(req, 0, &q); err != nil {
		return nil, err
	}
	from, to := uint64(0), f.head().header.Number.Uint64()
	if q.FromBlock != nil {
		b, ok := f.byTag(*q.FromBlock)
		if !ok {
			return nil, &rpcError{Code: -32000, Message: "invalid fromBlock"}
		}
		from = b.header.Number.Uint64()
	}
	if q.ToBlock != nil {
		b, ok := f.byTag(*q.ToBlock)
		if !ok {
			return nil, &rpcError{Code: -32000, Message: "invalid toBlock"}
		}
		to = b.header.Number.Uint64()
	}
	topics := make([][]common.Hash, len(q.Topics))
	for i, raw := range q.Topics {
		if string(raw) == "null" {
			continue
		}
		var one common.Hash
		if json.Unmarshal(raw, &one) == nil {
			topics[i] = []common.Hash{one}
			continue
		}
		if err := json.Unmarshal(raw, &topics[i]); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
	}
	out := []types.Log{}
	for _, l := range f.logs {
		if q.BlockHash != nil {
			if l.BlockHash != *q.BlockHash {
				continue
			}
		} else if l.BlockNumber < from || l.BlockNumber > to {
			continue
		}
		if len(q.Address) > 0 && !containsAddress(q.Address, l.Address) {
			continue
		}
		if !topicsMatch(topics, l.Topics) {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func containsAddress(set []common.Address, a common.Address) bool {
	for _, x := range set {
		if x == a {
			return true
		}
	}
	return false
}

func topicsMatch(filter [][]common.Hash, topics []common.Hash) bool {
	if len(filter) > len(topics) {
		return false
	}
	for i, alternatives := range filter {
		if len(alternatives) == 0 {
			continue
		}
		match := false
		for _, a := range alternatives {
			if a == topics[i] {
				match = true
			}
		}
		if !match {
			return false
		}
	}
	return true
}

// selectorOf is the 4-byte selector of a method in a contract ABI.
func selectorOf(t *testing.T, a *abi.ABI, method string) [4]byte {
	t.Helper()
	m, ok := a.Methods[method]
	if !ok {
		t.Fatalf("fixture: no method %s", method)
	}
	var s [4]byte
	copy(s[:], m.ID)
	return s
}

// packOutputs ABI-encodes a method's return values. It runs on the server
// goroutine, so it reports errors instead of failing the test.
func packOutputs(a *abi.ABI, method string, values ...interface{}) ([]byte, error) {
	out, err := a.Methods[method].Outputs.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("fixture: packing %s outputs: %w", method, err)
	}
	return out, nil
}
