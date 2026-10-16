package tbtc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	frostsigning "github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

func TestMixedWalletRouting(t *testing.T) {
	legacy := generateWallet(big.NewInt(100))
	ecdsaID, err := NewECDSAWalletIdentity(legacy.publicKey)
	if err != nil {
		t.Fatal(err)
	}
	_, pub := btcec.PrivKeyFromBytes([]byte{100})
	var q [32]byte
	copy(q[:], schnorr.SerializePubKey(pub))
	descriptor := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address{1}, Epoch: 17, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{1}, OutputKey: q}
	frostID, err := NewFrostWalletIdentity(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if frostID.legacy != nil || ecdsaID.Scheme() != bitcoin.SignatureECDSA || frostID.Scheme() != bitcoin.SignatureSchnorr {
		t.Fatal("identity union lost scheme")
	}
	a, _ := ecdsaID.CacheKey()
	b, _ := frostID.CacheKey()
	if a == b || !strings.HasPrefix(a, "ecdsa:") || !strings.HasPrefix(b, "frost:") {
		t.Fatal("cache identities collide")
	}
	foreign := descriptor
	foreign.Registry[1] = 1
	other, err := NewFrostWalletIdentity(foreign)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := other.CacheKey()
	if c == b {
		t.Fatal("cache omits deployment")
	}
	fullID, _ := frostID.FrostWalletID()
	id := dkg.WalletID(q)
	if fullID != id {
		t.Fatal("FROST label used legacy lookup")
	}
	if _, err = frostID.ECDSAPublicKeyHash(); err == nil {
		t.Fatal("FROST supplied an ECDSA lookup key")
	}
	if _, err = ecdsaID.FrostWalletID(); err == nil {
		t.Fatal("ECDSA supplied a FROST lookup key")
	}
	first, _ := frostID.monitorIdentity()
	last := first
	last.frost[31] ^= 1
	if first.String() == last.String() {
		t.Fatal("monitor truncates FROST identity")
	}
	script, _ := frostID.OutputScript()
	want, _ := bitcoin.PayToTaproot(q)
	if !bytes.Equal(script, want) {
		t.Fatal("FROST change was not exact P2TR")
	}
	monitor := newTransactionMonitor(newLocalBitcoinChain())
	if err = monitor.trackWallet(bitcoin.Hash{1}, frostID); err != nil || monitor.tracked[bitcoin.Hash{1}].wallet.frost != id || monitor.tracked[bitcoin.Hash{1}].wallet.scheme != bitcoin.SignatureSchnorr {
		t.Fatal("FROST monitoring identity", err)
	}
	if err = monitor.trackWallet(bitcoin.Hash{2}, WalletIdentity{}); err == nil {
		t.Fatal("empty monitoring identity accepted")
	}
	if (&wallet{}).String() == "" || (WalletIdentity{}).String() == "" {
		t.Fatal("nil identity logging")
	}
	if _, err = NewECDSAWalletIdentity(nil); err == nil {
		t.Fatal("nil ECDSA identity")
	}
	bad := *legacy.publicKey
	bad.X = big.NewInt(1)
	bad.Y = big.NewInt(1)
	if _, err = NewECDSAWalletIdentity(&bad); err == nil {
		t.Fatal("off-curve ECDSA identity")
	}
	_, builder := buildSignTransactionFixture(t, legacy)
	_, original := buildSignTransactionFixture(t, legacy)
	hashes, err := builder.ComputeSignatureHashes()
	if err != nil {
		t.Fatal(err)
	}
	private := &ecdsa.PrivateKey{PublicKey: *legacy.publicKey, D: big.NewInt(100)}
	var batch []*tecdsa.Signature
	var untyped []*bitcoin.SignatureContainer
	for _, h := range hashes {
		r, s, err := ecdsa.Sign(rand.Reader, private, h.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, &tecdsa.Signature{R: r, S: s})
		untyped = append(untyped, &bitcoin.SignatureContainer{R: r, S: s, PublicKey: legacy.publicKey})
	}
	calls := 0
	signer := func(_ context.Context, h []*big.Int, start uint64) ([]*tecdsa.Signature, error) {
		calls++
		if start != 42 || len(h) != len(hashes) {
			t.Fatal("legacy signing context changed")
		}
		for i := range h {
			if h[i].Cmp(hashes[i]) != 0 {
				t.Fatal("legacy digest changed")
			}
		}
		return batch, nil
	}
	result, err := SignWalletTransaction(context.Background(), WalletTransactionRequest{Wallet: ecdsaID, LegacyBuilder: builder, LegacySigner: signer, LegacyStartBlock: 42})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = original.ComputeSignatureHashes(); err != nil {
		t.Fatal(err)
	}
	old, err := original.AddSignatures(untyped)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.Frost != nil || !bytes.Equal(result.Transaction.Serialize(), old.Serialize()) {
		t.Fatal("legacy serialization or route changed")
	}
	for name, request := range map[string]WalletTransactionRequest{
		"empty": {}, "ECDSA with FROST": {Wallet: ecdsaID, LegacyBuilder: builder, LegacySigner: signer, Frost: new(frostsigning.Executor)}, "FROST with legacy": {Wallet: frostID, LegacyBuilder: builder, LegacySigner: signer}, "FROST empty executor": {Wallet: frostID, Frost: new(frostsigning.Executor)},
		"FROST missing executor": {Wallet: frostID}, "ECDSA missing signer": {Wallet: ecdsaID, LegacyBuilder: builder},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := SignWalletTransaction(context.Background(), request); err == nil {
				t.Fatal("cross-scheme route accepted")
			}
		})
	}
	if calls != 1 {
		t.Fatal("invalid route reached legacy signer")
	}
}
