package bitcoincore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

func TestLocalCoreDomainAndKnownTransaction(t *testing.T) {
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{1}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 1000, PublicKeyScript: []byte{0x51}}}}
	raw := hex.EncodeToString(tx.Serialize())
	var admissions atomic.Int32
	genesis := chaincfg.RegressionNetParams.GenesisHash.String()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Method string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "getblockhash":
			result = genesis
		case "getrawtransaction":
			result = raw
		default:
			admissions.Add(1)
			t.Errorf("known transaction unexpectedly reached admission RPC %s", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "error": nil, "id": "k04"})
	}))
	defer server.Close()
	cookie := filepath.Join(t.TempDir(), "cookie")
	if err := os.WriteFile(cookie, []byte("rpc:public-test-cookie"), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewLocal(context.Background(), server.URL, cookie, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = client.Broadcast(context.Background(), tx.Serialize()); err != nil || admissions.Load() != 0 {
		t.Fatal("control CoreKnownTransaction assertion: repeated dispatch was not idempotent", err)
	}
	genesis = chaincfg.MainNetParams.GenesisHash.String()
	if _, err = NewLocal(context.Background(), server.URL, cookie, 2); err == nil || err.Error() != "Bitcoin Core is not regtest" {
		t.Fatal("control CoreRegtestDomain assertion: non-regtest provider admitted", err)
	}
	if _, err = NewLocal(context.Background(), "https://example.invalid", cookie, 2); err == nil {
		t.Fatal("external Core endpoint admitted")
	}
}
func TestLocalCoreDoesNotFollowRedirect(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	cookie := filepath.Join(t.TempDir(), "cookie")
	if err := os.WriteFile(cookie, []byte("rpc:public-test-cookie"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal(context.Background(), redirect.URL, cookie, 2); err == nil || reached.Load() != 0 {
		t.Fatal("control CoreRedirect assertion: local RPC followed a redirect", err)
	}
}
