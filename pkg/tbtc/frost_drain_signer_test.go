package tbtc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// drainRequest is the K-07 integration request file. With no Funding the
// helper only reports the legacy wallet key.
type drainRequest struct {
	Funding     string
	OutputIndex uint32
	Targets     []string
	Keys        []string
	Fee         int64
	Result      string
}

type drainResult struct {
	PublicKeyX, PublicKeyY string
	PublicKeyHash          string
	Transaction            string
}

// TestK07DrainSigner is a K-07 integration helper, not a unit test: it runs
// only when FROST_K07_DRAIN_REQUEST names a request file. It stands in for the
// legacy wallet's operators in the ECDSA-to-FROST drain test (V-08 reading
// 6): real tss-lib signing, in process, over the pre-generated key shares of
// pkg/internal/tecdsatest, of the moving funds transaction the legacy action
// assembles. The legacy wallet's DKG and sortition are out of scope.
func TestK07DrainSigner(t *testing.T) {
	path := os.Getenv("FROST_K07_DRAIN_REQUEST")
	if path == "" {
		t.Skip("K-07 integration helper; set FROST_K07_DRAIN_REQUEST")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var request drainRequest
	if err = json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	executor := setupSigningExecutor(t)
	public := executor.wallet().publicKey
	pkh := bitcoin.PublicKeyHash(public)
	result := drainResult{PublicKeyX: hex.EncodeToString(public.X.FillBytes(make([]byte, 32))), PublicKeyY: hex.EncodeToString(public.Y.FillBytes(make([]byte, 32))), PublicKeyHash: hex.EncodeToString(pkh[:])}
	if request.Funding != "" {
		fundingRaw, err := hex.DecodeString(request.Funding)
		if err != nil {
			t.Fatal(err)
		}
		funding := new(bitcoin.Transaction)
		if err = funding.Deserialize(fundingRaw); err != nil {
			t.Fatal(err)
		}
		btc := newLocalBitcoinChain()
		if err = btc.BroadcastTransaction(funding); err != nil {
			t.Fatal(err)
		}
		main := &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: request.OutputIndex}, Value: funding.Outputs[request.OutputIndex].Value}
		var targets [][20]byte
		keys := map[[20]byte][32]byte{}
		for i, label := range request.Targets {
			var l [20]byte
			var q [32]byte
			b, _ := hex.DecodeString(label)
			copy(l[:], b)
			b, _ = hex.DecodeString(request.Keys[i])
			copy(q[:], b)
			targets = append(targets, l)
			keys[l] = q
		}
		builder, err := assembleMovingFundsTransaction(btc, main, targets, request.Fee, keys)
		if err != nil {
			t.Fatal(err)
		}
		hashes, err := builder.ComputeSignatureHashes()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		signatures, err := executor.signBatch(ctx, hashes, 0)
		if err != nil {
			t.Fatal(err)
		}
		var containers []*bitcoin.SignatureContainer
		for _, s := range signatures {
			containers = append(containers, &bitcoin.SignatureContainer{R: s.R, S: s.S, PublicKey: public})
		}
		tx, err := builder.AddSignatures(containers)
		if err != nil {
			t.Fatal(err)
		}
		result.Transaction = hex.EncodeToString(tx.Serialize())
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(request.Result, out, 0600); err != nil {
		t.Fatal(err)
	}
}
