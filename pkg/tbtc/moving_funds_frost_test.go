package tbtc

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

type frostResolverStub map[[20]byte][32]byte

func (s frostResolverStub) FrostTargetOutputKey(label [20]byte) ([32]byte, bool, error) {
	q, ok := s[label]
	return q, ok, nil
}

// A drain pays OP_1 PUSH32 Q of each FROST target in commitment order with
// the legacy equal split (KC-SF-V3 K-07, C-06).
func TestAssembleMovingFundsTransactionPaysFrostTargets(t *testing.T) {
	bitcoinChain := newLocalBitcoinChain()
	private, err := ecdsa.GenerateKey(btcec.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	funding := &bitcoin.Transaction{Version: 1, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{7}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 400_000, PublicKeyScript: mustP2WPKH(t, bitcoin.PublicKeyHash(&private.PublicKey))}}}
	if err := bitcoinChain.BroadcastTransaction(funding); err != nil {
		t.Fatal(err)
	}
	main := &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: funding.Hash()}, Value: 400_000}
	targets := [][20]byte{{0x10}, {0x20}}
	keys := frostResolverStub{{0x10}: xOnly(t, 0xaa), {0x20}: xOnly(t, 0xbb)}
	resolved, err := FrostTargetKeys(keys, targets)
	if err != nil || len(resolved) != 2 {
		t.Fatal("FROST targets not resolved", err)
	}
	builder, err := assembleMovingFundsTransaction(bitcoinChain, main, targets, 3_001, resolved)
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := builder.ComputeSignatureHashes()
	if err != nil {
		t.Fatal(err)
	}
	r, sig, err := ecdsa.Sign(rand.Reader, private, hashes[0].Bytes())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := builder.AddSignatures([]*bitcoin.SignatureContainer{{R: r, S: sig, PublicKey: &private.PublicKey}})
	if err != nil {
		t.Fatal(err)
	}
	for i, label := range targets {
		q := resolved[label]
		want, _ := bitcoin.PayToTaproot(q)
		if !bytes.Equal(tx.Outputs[i].PublicKeyScript, want) {
			t.Fatalf("output %d is not P2TR to its target", i)
		}
	}
	if tx.Outputs[0].Value != 198_499 || tx.Outputs[1].Value != 198_500 {
		t.Fatal("equal split with the remainder on the last output", tx.Outputs[0].Value, tx.Outputs[1].Value)
	}
	if _, err = FrostTargetKeys(frostResolverStub{{0x10}: xOnly(t, 0xaa)}, targets); err == nil {
		t.Fatal("a mix of legacy and FROST targets must be refused")
	}
	if keys, err := FrostTargetKeys(struct{}{}, targets); err != nil || keys != nil {
		t.Fatal("a chain without the resolver knows legacy targets only")
	}
}

func mustP2WPKH(t *testing.T, pkh [20]byte) bitcoin.Script {
	t.Helper()
	s, err := bitcoin.PayToWitnessPublicKeyHash(pkh)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func xOnly(t *testing.T, secret byte) (q [32]byte) {
	t.Helper()
	s := make([]byte, 32)
	s[31] = secret
	_, public := btcec.PrivKeyFromBytes(s)
	copy(q[:], public.SerializeCompressed()[1:])
	return q
}
