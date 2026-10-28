package bitcoin

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
)

func tapKey(b byte) *btcec.PrivateKey {
	raw := make([]byte, 32)
	raw[31] = b
	k, _ := btcec.PrivKeyFromBytes(raw)
	return k
}

func xOnly(k *btcec.PrivateKey) (out [32]byte) {
	copy(out[:], schnorr.SerializePubKey(k.PubKey()))
	return out
}

func walletLeafFor(q [32]byte, tag byte) Script {
	leaf := append([]byte{txscript.OP_DATA_32}, bytes.Repeat([]byte{tag}, 32)...)
	leaf = append(leaf, txscript.OP_DROP, txscript.OP_DATA_32)
	leaf = append(leaf, q[:]...)
	return append(leaf, txscript.OP_CHECKSIG)
}

// scriptOutput commits leaf and a sibling under an unrelated internal key and
// returns the P2TR script and the control block for leaf.
func scriptOutput(t *testing.T, leaf, sibling Script) (Script, []byte) {
	t.Helper()
	tree := txscript.AssembleTaprootScriptTree(txscript.NewBaseTapLeaf(leaf), txscript.NewBaseTapLeaf(sibling))
	internal := tapKey(99).PubKey()
	root := tree.RootNode.TapHash()
	output := txscript.ComputeTaprootOutputKey(internal, root[:])
	proof := tree.LeafMerkleProofs[0]
	if !bytes.Equal(proof.TapLeaf.Script, leaf) {
		proof = tree.LeafMerkleProofs[1]
	}
	control := proof.ToControlBlock(internal)
	raw, err := control.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	script, err := txscript.PayToTaprootScript(output)
	if err != nil {
		t.Fatal(err)
	}
	return script, raw
}

type mixed struct {
	tx       *Transaction
	prevouts []PreviousOutput
	spends   []*TapscriptSpend
	wallet   *btcec.PrivateKey
}

// mixedSweep is one key-path main input and one script-path deposit input.
func mixedSweep(t *testing.T) mixed {
	t.Helper()
	wallet := tapKey(5)
	q := xOnly(wallet)
	main, _ := PayToTaproot(q)
	leaf := walletLeafFor(q, 0x11)
	deposit, control := scriptOutput(t, leaf, walletLeafFor(xOnly(tapKey(6)), 0x22))
	var a, b Hash
	a[0], b[0] = 1, 2
	tx := &Transaction{Version: 2, Inputs: []*TransactionInput{{Outpoint: &TransactionOutpoint{TransactionHash: a}, Sequence: 0xffffffff}, {Outpoint: &TransactionOutpoint{TransactionHash: b, OutputIndex: 3}, Sequence: 0xffffffff}}, Outputs: []*TransactionOutput{{Value: 149_000, PublicKeyScript: main}}}
	prevouts := []PreviousOutput{{Outpoint: *tx.Inputs[0].Outpoint, Value: 100_000, PublicKeyScript: main}, {Outpoint: *tx.Inputs[1].Outpoint, Value: 50_000, PublicKeyScript: deposit}}
	return mixed{tx: tx, prevouts: prevouts, spends: []*TapscriptSpend{nil, {Leaf: leaf, Control: control}}, wallet: wallet}
}

func TestScriptPathSnapshotSignsAndValidates(t *testing.T) {
	m := mixedSweep(t)
	q := xOnly(m.wallet)
	snapshot, err := NewTaprootScriptTransaction(m.tx, m.prevouts, q, m.spends)
	if err != nil {
		t.Fatal(err)
	}
	keyDigest, err := TaprootSignatureHash(m.tx, m.prevouts, 0)
	if err != nil {
		t.Fatal(err)
	}
	leafDigest, err := TapscriptSignatureHash(m.tx, m.prevouts, 1, m.spends[1].Leaf)
	if err != nil {
		t.Fatal(err)
	}
	if d := snapshot.Digests(); d[0] != keyDigest || d[1] != leafDigest || keyDigest == leafDigest {
		t.Fatal("per-input digests must be key-path and BIP342 respectively")
	}
	var signatures []Signature
	for _, digest := range snapshot.Digests() {
		sig, err := schnorr.Sign(m.wallet, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		signature, err := NewSchnorrSignature(sig.Serialize())
		if err != nil {
			t.Fatal(err)
		}
		signatures = append(signatures, signature)
	}
	signed, err := snapshot.AddSignatures(signatures)
	if err != nil {
		t.Fatal(err)
	}
	if len(signed.Inputs[0].Witness) != 1 || len(signed.Inputs[1].Witness) != 3 {
		t.Fatal("witness shapes")
	}
	// The exact witness vsize is what was estimated before signing.
	weight := 3*len(signed.Serialize(Standard)) + len(signed.Serialize(Witness))
	if int64((weight+3)/4) != snapshot.VirtualSize() {
		t.Fatal("virtual size estimate differs from the signed transaction")
	}
	// A signature valid for the key path cannot replace the leaf signature.
	swapped := snapshot.Unsigned()
	swapped.Inputs[0].Witness = signed.Inputs[0].Witness
	swapped.Inputs[1].Witness = [][]byte{signed.Inputs[0].Witness[0], m.spends[1].Leaf, m.spends[1].Control}
	if snapshot.ValidateSigned(swapped) == nil {
		t.Fatal("key-path signature accepted for the script-path input")
	}
	changed := snapshot.Unsigned()
	changed.Inputs[0].Witness = signed.Inputs[0].Witness
	changed.Inputs[1].Witness = [][]byte{signed.Inputs[1].Witness[0], m.spends[1].Leaf}
	if snapshot.ValidateSigned(changed) == nil {
		t.Fatal("missing control block accepted")
	}
	if err = snapshot.Matches(m.tx, m.prevouts); err != nil {
		t.Fatal(err)
	}
	keyOnly, err := NewTaprootTransaction(&Transaction{Version: 2, Inputs: m.tx.Inputs[:1], Outputs: []*TransactionOutput{{Value: 99_000, PublicKeyScript: m.prevouts[0].PublicKeyScript}}}, m.prevouts[:1], q)
	if err != nil || keyOnly.Spends() != nil {
		t.Fatal("key-path snapshot must keep no spends", err)
	}
}

func TestScriptPathSnapshotRefusals(t *testing.T) {
	m := mixedSweep(t)
	q := xOnly(m.wallet)
	leaf := m.spends[1].Leaf
	refuse := func(name string, spends []*TapscriptSpend, prevouts []PreviousOutput) {
		t.Helper()
		if _, err := NewTaprootScriptTransaction(m.tx, prevouts, q, spends); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	with := func(l Script, c []byte) []*TapscriptSpend { return []*TapscriptSpend{nil, {Leaf: l, Control: c}} }
	refuse("short spend list", []*TapscriptSpend{nil}, m.prevouts)
	refuse("foreign Q leaf", with(walletLeafFor(xOnly(tapKey(6)), 0x11), m.spends[1].Control), m.prevouts)
	refuse("leaf not committed", with(walletLeafFor(q, 0x12), m.spends[1].Control), m.prevouts)
	refuse("control from sibling", with(leaf, append([]byte{m.spends[1].Control[0] ^ 1}, m.spends[1].Control[1:]...)), m.prevouts)
	keyPath := []*TapscriptSpend{nil, nil}
	refuse("deposit treated as key path", keyPath, m.prevouts)
	wrongMain := append([]PreviousOutput(nil), m.prevouts...)
	wrongMain[1].PublicKeyScript = append(Script{0x00, 0x20}, make([]byte, 32)...)
	refuse("non-P2TR deposit", m.spends, wrongMain)
	for name, bad := range map[string]Script{
		"OP_SUCCESS":        append(Script{0x50}, leaf...),
		"code separator":    append(Script{txscript.OP_CODESEPARATOR}, leaf...),
		"second CHECKSIG":   append(append(Script{txscript.OP_DATA_32}, q[:]...), append(Script{txscript.OP_CHECKSIG, txscript.OP_DROP}, leaf...)...),
		"CHECKSIGADD":       append(Script{txscript.OP_CHECKSIGADD, txscript.OP_DROP}, leaf...),
		"non-minimal push":  append(Script{txscript.OP_PUSHDATA1, 1, 7, txscript.OP_DROP}, leaf...),
		"truncated push":    Script{txscript.OP_DATA_32, 1},
		"no trailing CHECK": leaf[:len(leaf)-1],
		"trailing junk":     append(append(Script(nil), leaf...), txscript.OP_1),
	} {
		if err := checkWalletLeaf(bad, q); err == nil {
			t.Errorf("leaf %s accepted", name)
		}
	}
	if err := checkWalletLeaf(leaf, q); err != nil {
		t.Fatal(err)
	}
}
