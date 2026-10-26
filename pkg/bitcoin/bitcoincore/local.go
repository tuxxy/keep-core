// Package bitcoincore provides a local-regtest-only Bitcoin Core RPC adapter.
// It uses the actual transaction, UTXO, header and Merkle data from Core; it is
// not a production discovery backend or a controlled proof assembler.
package bitcoincore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type rpcClient struct {
	url, cookie string
	client      *http.Client
}
type Chain struct {
	core    *rpcClient
	ctx     context.Context
	feeRate int64
}

// NewLocal refuses external endpoints and non-regtest genesis before exposing
// signing or proof data. Cookie bytes stay inside this process.
func NewLocal(ctx context.Context, endpoint, cookieFile string, feeRate int64) (*Chain, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || feeRate <= 0 {
		return nil, errors.New("local regtest RPC configuration required")
	}
	cookie, err := os.ReadFile(cookieFile)
	if err != nil {
		return nil, errors.New("cannot read local Core cookie")
	}
	client := &rpcClient{url: endpoint, cookie: strings.TrimSpace(string(cookie)), client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("local Core redirect refused") }}}
	genesis, err := client.Genesis(ctx)
	if err != nil {
		return nil, err
	}
	if genesis != [32]byte(*chaincfg.RegressionNetParams.GenesisHash) {
		return nil, errors.New("Bitcoin Core is not regtest")
	}
	return &Chain{core: client, ctx: ctx, feeRate: feeRate}, nil
}
func (c *Chain) Close()                                        { c.core.client.CloseIdleConnections() }
func (c *Chain) Genesis(ctx context.Context) ([32]byte, error) { return c.core.Genesis(ctx) }
func (c *Chain) Transaction(ctx context.Context, h bitcoin.Hash) ([]byte, error) {
	return c.core.Transaction(ctx, h)
}
func (c *Chain) Confirmations(ctx context.Context, h bitcoin.Hash) (uint, error) {
	return c.core.Confirmations(ctx, h)
}
func (c *Chain) Unspent(ctx context.Context, p bitcoin.TransactionOutpoint) (bool, error) {
	return c.core.Unspent(ctx, p)
}
func (c *Chain) ConfirmedUnspent(ctx context.Context, p bitcoin.TransactionOutpoint) (bool, error) {
	return c.core.ConfirmedUnspent(ctx, p)
}
func (c *Chain) Broadcast(ctx context.Context, raw []byte) error { return c.core.Broadcast(ctx, raw) }

func (c *rpcClient) call(ctx context.Context, method string, args []any, result any) error {
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
		return fmt.Errorf("invalid Core RPC cookie")
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
func (c *rpcClient) Genesis(ctx context.Context) ([32]byte, error) {
	var text string
	if err := c.call(ctx, "getblockhash", []any{0}, &text); err != nil {
		return [32]byte{}, err
	}
	h, err := bitcoin.NewHashFromString(text, bitcoin.ReversedByteOrder)
	return [32]byte(h), err
}
func (c *rpcClient) Transaction(ctx context.Context, h bitcoin.Hash) ([]byte, error) {
	var text string
	if err := c.call(ctx, "getrawtransaction", []any{h.Hex(bitcoin.ReversedByteOrder)}, &text); err != nil {
		return nil, err
	}
	return hex.DecodeString(text)
}
func (c *rpcClient) Confirmations(ctx context.Context, h bitcoin.Hash) (uint, error) {
	var result struct{ Confirmations uint }
	err := c.call(ctx, "getrawtransaction", []any{h.Hex(bitcoin.ReversedByteOrder), true}, &result)
	return result.Confirmations, err
}
func (c *rpcClient) Unspent(ctx context.Context, p bitcoin.TransactionOutpoint) (bool, error) {
	var result json.RawMessage
	err := c.call(ctx, "gettxout", []any{p.TransactionHash.Hex(bitcoin.ReversedByteOrder), p.OutputIndex, true}, &result)
	return len(result) > 0 && string(result) != "null", err
}

// ConfirmedUnspent ignores the mempool (gettxout include_mempool=false).
func (c *rpcClient) ConfirmedUnspent(ctx context.Context, p bitcoin.TransactionOutpoint) (bool, error) {
	var result json.RawMessage
	err := c.call(ctx, "gettxout", []any{p.TransactionHash.Hex(bitcoin.ReversedByteOrder), p.OutputIndex, false}, &result)
	return len(result) > 0 && string(result) != "null", err
}
func (c *rpcClient) Broadcast(ctx context.Context, raw []byte) error {
	// Repeated dispatch and independently accepted threshold results may
	// rebroadcast the same transaction. Observe Core before trying to admit it.
	tx := new(bitcoin.Transaction)
	if err := tx.Deserialize(raw); err != nil {
		return err
	}
	if known, err := c.Transaction(ctx, tx.Hash()); err == nil {
		other := new(bitcoin.Transaction)
		if other.Deserialize(known) == nil && bytes.Equal(other.Serialize(bitcoin.Standard), tx.Serialize(bitcoin.Standard)) {
			return nil
		}
		return errors.New("Core transaction identity mismatch")
	}
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

func (c *Chain) GetTransaction(h bitcoin.Hash) (*bitcoin.Transaction, error) {
	raw, err := c.core.Transaction(c.ctx, h)
	if err != nil {
		return nil, err
	}
	tx := new(bitcoin.Transaction)
	return tx, tx.Deserialize(raw)
}
func (c *Chain) GetTransactionConfirmations(ctx context.Context, h bitcoin.Hash) (uint, error) {
	return c.core.Confirmations(ctx, h)
}
func (c *Chain) BroadcastTransaction(tx *bitcoin.Transaction) error {
	return c.core.Broadcast(c.ctx, tx.Serialize(bitcoin.Witness))
}
func (c *Chain) GetLatestBlockHeight() (uint, error) {
	var n uint
	err := c.core.call(c.ctx, "getblockcount", nil, &n)
	return n, err
}
func (c *Chain) blockHash(height uint) (string, error) {
	var h string
	err := c.core.call(c.ctx, "getblockhash", []any{height}, &h)
	return h, err
}
func (c *Chain) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
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
func (c *Chain) blockTxids(height uint) ([]string, error) {
	h, err := c.blockHash(height)
	if err != nil {
		return nil, err
	}
	var block struct{ Tx []string }
	err = c.core.call(c.ctx, "getblock", []any{h, 1}, &block)
	return block.Tx, err
}
func (c *Chain) GetTransactionMerkleProof(h bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
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
func (c *Chain) GetCoinbaseTxHash(height uint) (bitcoin.Hash, error) {
	txids, err := c.blockTxids(height)
	if err != nil || len(txids) == 0 {
		return bitcoin.Hash{}, fmt.Errorf("coinbase: %v", err)
	}
	return bitcoin.NewHashFromString(txids[0], bitcoin.ReversedByteOrder)
}
func (c *Chain) GetTransactionsForPublicKeyHash([20]byte, int) ([]*bitcoin.Transaction, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *Chain) GetTxHashesForPublicKeyHash([20]byte) ([]bitcoin.Hash, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *Chain) GetMempoolForPublicKeyHash([20]byte) ([]*bitcoin.Transaction, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *Chain) GetUtxosForPublicKeyHash([20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *Chain) GetMempoolUtxosForPublicKeyHash([20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	return nil, errors.New("HASH160 discovery is not used for FROST")
}
func (c *Chain) EstimateSatPerVByteFee(uint32) (int64, error) { return c.feeRate, nil }

// UnspentByScript uses Core's scantxoutset on the exact raw script.
func (c *Chain) UnspentByScript(ctx context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
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
