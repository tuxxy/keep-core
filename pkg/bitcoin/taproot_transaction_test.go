package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	btcec2 "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

func keyPathFixture(t *testing.T) (*Transaction, []PreviousOutput, [32]byte, *btcec2.PrivateKey) {
	t.Helper()
	secret := make([]byte, 32)
	secret[31] = 17
	private, pub := btcec2.PrivKeyFromBytes(secret)
	var key [32]byte
	copy(key[:], schnorr.SerializePubKey(pub))
	script, err := PayToTaproot(key)
	if err != nil {
		t.Fatal(err)
	}
	tx := &Transaction{Version: 2, Locktime: 19}
	var prevouts []PreviousOutput
	for i := 0; i < 2; i++ {
		point := TransactionOutpoint{TransactionHash: Hash{byte(i + 1)}, OutputIndex: uint32(i)}
		tx.Inputs = append(tx.Inputs, &TransactionInput{Outpoint: &point, Sequence: 0xfffffffd})
		prevouts = append(prevouts, PreviousOutput{Outpoint: point, Value: 100000, PublicKeyScript: append(Script(nil), script...)})
	}
	recipient, _ := PayToWitnessPublicKeyHash([20]byte{3})
	tx.Outputs = []*TransactionOutput{{Value: 120000, PublicKeyScript: recipient}, {Value: 79000, PublicKeyScript: script}}
	return tx, prevouts, key, private
}

// Independent, deliberately small BIP341 reference implementation for this
// profile. No txscript sighash helper is used in the reference side.
func referenceDefaultSighash(tx *Transaction, prev []PreviousOutput, index int) [32]byte {
	hashPart := func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
	var points, amounts, scripts, sequences, outputs []byte
	for i, input := range tx.Inputs {
		points = append(points, input.Outpoint.TransactionHash[:]...)
		points = binary.LittleEndian.AppendUint32(points, input.Outpoint.OutputIndex)
		amounts = binary.LittleEndian.AppendUint64(amounts, uint64(prev[i].Value))
		var b bytes.Buffer
		_ = wire.WriteVarBytes(&b, 0, prev[i].PublicKeyScript)
		scripts = append(scripts, b.Bytes()...)
		sequences = binary.LittleEndian.AppendUint32(sequences, input.Sequence)
	}
	for _, output := range tx.Outputs {
		outputs = binary.LittleEndian.AppendUint64(outputs, uint64(output.Value))
		var b bytes.Buffer
		_ = wire.WriteVarBytes(&b, 0, output.PublicKeyScript)
		outputs = append(outputs, b.Bytes()...)
	}
	data := []byte{0, 0} // epoch, SIGHASH_DEFAULT
	data = binary.LittleEndian.AppendUint32(data, uint32(tx.Version))
	data = binary.LittleEndian.AppendUint32(data, tx.Locktime)
	for _, part := range [][]byte{points, amounts, scripts, sequences, outputs} {
		data = append(data, hashPart(part)...)
	}
	data = append(data, 0) // ext_flag=0, annex_present=0
	data = binary.LittleEndian.AppendUint32(data, uint32(index))
	tag := sha256.Sum256([]byte("TapSighash"))
	pre := append(append([]byte{}, tag[:]...), tag[:]...)
	return sha256.Sum256(append(pre, data...))
}
func TestSighashVectorsAndMutation(t *testing.T) {
	t.Run("official default vector", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/bip341-wallet-test-vectors.json")
		if err != nil {
			t.Fatal(err)
		}
		var vectors struct {
			KeyPathSpending []struct {
				Given struct {
					RawUnsignedTx string
					UtxosSpent    []struct {
						ScriptPubKey string
						AmountSats   int64
					}
				}
				InputSpending []struct {
					Given        struct{ TxinIndex, HashType int }
					Intermediary struct{ SigHash string }
				}
			}
		}
		if err = json.Unmarshal(raw, &vectors); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, v := range vectors.KeyPathSpending {
			tx := new(Transaction)
			raw, err := hex.DecodeString(v.Given.RawUnsignedTx)
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Deserialize(raw); err != nil {
				t.Fatal(err)
			}
			var prev []PreviousOutput
			for i, p := range v.Given.UtxosSpent {
				script, err := hex.DecodeString(p.ScriptPubKey)
				if err != nil {
					t.Fatal(err)
				}
				prev = append(prev, PreviousOutput{Outpoint: *tx.Inputs[i].Outpoint, Value: p.AmountSats, PublicKeyScript: script})
			}
			for _, input := range v.InputSpending {
				if input.Given.HashType != 0 {
					continue
				}
				count++
				digest, err := TaprootSignatureHash(tx, prev, input.Given.TxinIndex)
				if err != nil {
					t.Fatal(err)
				}
				if hex.EncodeToString(digest[:]) != input.Intermediary.SigHash || digest != referenceDefaultSighash(tx, prev, input.Given.TxinIndex) {
					t.Fatal("official or independent digest differs")
				}
			}
		}
		if count != 1 {
			t.Fatalf("expected one default vector, got %d", count)
		}
	})
	tx, prev, key, _ := keyPathFixture(t)
	original, err := NewTaprootTransaction(tx, prev, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Transaction, []PreviousOutput)
	}{
		{"version", func(tx *Transaction, _ []PreviousOutput) { tx.Version++ }},
		{"locktime", func(tx *Transaction, _ []PreviousOutput) { tx.Locktime++ }},
		{"sequence", func(tx *Transaction, _ []PreviousOutput) { tx.Inputs[1].Sequence-- }},
		{"outpoint hash", func(tx *Transaction, p []PreviousOutput) {
			tx.Inputs[1].Outpoint.TransactionHash[0] ^= 4
			p[1].Outpoint = *tx.Inputs[1].Outpoint
		}},
		{"outpoint index", func(tx *Transaction, p []PreviousOutput) {
			tx.Inputs[1].Outpoint.OutputIndex++
			p[1].Outpoint = *tx.Inputs[1].Outpoint
		}},
		{"input order", func(tx *Transaction, p []PreviousOutput) {
			tx.Inputs[0], tx.Inputs[1] = tx.Inputs[1], tx.Inputs[0]
			p[0], p[1] = p[1], p[0]
		}},
		{"first amount", func(_ *Transaction, p []PreviousOutput) { p[0].Value++ }},
		{"last amount", func(_ *Transaction, p []PreviousOutput) { p[1].Value++ }},
		{"first prevout script", func(_ *Transaction, p []PreviousOutput) { p[0].PublicKeyScript[3] ^= 1 }},
		{"last prevout script", func(_ *Transaction, p []PreviousOutput) { p[1].PublicKeyScript[3] ^= 1 }},
		{"recipient", func(tx *Transaction, _ []PreviousOutput) { tx.Outputs[0].PublicKeyScript[2] ^= 1 }},
		{"recipient amount", func(tx *Transaction, _ []PreviousOutput) { tx.Outputs[0].Value++ }},
		{"change amount", func(tx *Transaction, _ []PreviousOutput) { tx.Outputs[1].Value++ }},
		{"change key", func(tx *Transaction, _ []PreviousOutput) { tx.Outputs[1].PublicKeyScript[2] ^= 1 }},
		{"output order", func(tx *Transaction, _ []PreviousOutput) { tx.Outputs[0], tx.Outputs[1] = tx.Outputs[1], tx.Outputs[0] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := original.Unsigned()
			prevouts := original.PreviousOutputs()
			tc.mutate(mutated, prevouts)
			if original.Matches(mutated, prevouts) == nil {
				t.Fatal("authorized field mutation accepted")
			}
			digest, err := TaprootSignatureHash(mutated, prevouts, 0)
			if err == nil && digest == original.Digests()[0] {
				t.Fatal("changed field did not bind digest")
			}
		})
	}
	t.Run("input index", func(t *testing.T) {
		digests := original.Digests()
		if digests[0] == digests[1] {
			t.Fatal("input index missing")
		}
		for i, d := range digests {
			if d != referenceDefaultSighash(tx, prev, i) {
				t.Fatal("reference mismatch")
			}
		}
	})
	t.Run("leading zero full width", func(t *testing.T) {
		for lock := uint32(0); lock < 65536; lock++ {
			tx.Locktime = lock
			digest, err := TaprootSignatureHash(tx, prev, 0)
			if err != nil {
				t.Fatal(err)
			}
			if digest[0] == 0 {
				if digest != referenceDefaultSighash(tx, prev, 0) {
					t.Fatal("leading zero changed")
				}
				t.Logf("locktime=%d digest=%x", lock, digest)
				return
			}
		}
		t.Fatal("no leading zero control")
	})
}

func TestTaprootSnapshotAndWitness(t *testing.T) {
	tx, prev, key, private := keyPathFixture(t)
	snapshot, err := NewTaprootTransaction(tx, prev, key)
	if err != nil {
		t.Fatal(err)
	}
	tx.Outputs[0].Value++
	prev[0].Value++
	exported := snapshot.Unsigned()
	exported.Outputs[0].Value++
	exportedPrev := snapshot.PreviousOutputs()
	exportedPrev[0].PublicKeyScript[0] ^= 1
	if snapshot.Fee() != 1000 || snapshot.Unsigned().Outputs[0].Value != 120000 {
		t.Fatal("snapshot aliases caller data")
	}
	var signatures []Signature
	for _, digest := range snapshot.Digests() {
		signature, err := schnorr.Sign(private, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		typed, err := NewSchnorrSignature(signature.Serialize())
		if err != nil {
			t.Fatal(err)
		}
		signatures = append(signatures, typed)
	}
	signed, err := snapshot.AddSignatures(signatures)
	if err != nil {
		t.Fatal(err)
	}
	if len(signed.Inputs[0].Witness) != 1 || len(signed.Inputs[0].Witness[0]) != 64 || snapshot.ValidateSigned(signed) != nil {
		t.Fatal("wrong witness")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Transaction)
	}{
		{"explicit default byte", func(tx *Transaction) { tx.Inputs[0].Witness[0] = append(tx.Inputs[0].Witness[0], 0) }},
		{"annex", func(tx *Transaction) { tx.Inputs[0].Witness = append(tx.Inputs[0].Witness, []byte{0x50}) }},
		{"sighash all", func(tx *Transaction) { tx.Inputs[0].Witness[0] = append(tx.Inputs[0].Witness[0], 1) }},
		{"scriptSig", func(tx *Transaction) { tx.Inputs[0].SignatureScript = []byte{1} }},
		{"bad signature", func(tx *Transaction) { tx.Inputs[0].Witness[0][8] ^= 1 }},
		{"post-sign amount", func(tx *Transaction) { tx.Outputs[0].Value++ }},
		{"wrong input signature", func(tx *Transaction) {
			tx.Inputs[0].Witness, tx.Inputs[1].Witness = tx.Inputs[1].Witness, tx.Inputs[0].Witness
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := cloneBitcoinTransaction(signed)
			tc.mutate(bad)
			if snapshot.ValidateSigned(bad) == nil {
				t.Fatal("invalid signed bytes accepted")
			}
		})
	}
	t.Run("wrong key and double tweak", func(t *testing.T) {
		tx, prev, key, private := keyPathFixture(t)
		other := txscript.ComputeTaprootKeyNoScript(private.PubKey())
		var twice [32]byte
		copy(twice[:], schnorr.SerializePubKey(other))
		if bytes.Equal(key[:], twice[:]) {
			t.Fatal("control key did not change")
		}
		if _, err = NewTaprootTransaction(tx, prev, twice); err == nil {
			t.Fatal("double-tweaked key accepted")
		}
		script, _ := PayToTaproot(key)
		if len(script) != 34 || script[0] != txscript.OP_1 || script[1] != 32 || !bytes.Equal(script[2:], key[:]) {
			t.Fatal("wrong output program")
		}
	})
	for _, length := range []int{0, 63, 65} {
		t.Run(fmt.Sprintf("signature length %d", length), func(t *testing.T) {
			if _, err := NewSchnorrSignature(make([]byte, length)); err == nil {
				t.Fatal("bad signature length")
			}
		})
	}
}

func TestSignatureSchemeBoundary(t *testing.T) {
	tx, prev, key, private := keyPathFixture(t)
	snapshot, err := NewTaprootTransaction(tx, prev, key)
	if err != nil {
		t.Fatal(err)
	}
	digest := snapshot.Digests()[0]
	signed, err := schnorr.Sign(private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature, err := NewSchnorrSignature(signed.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = signature.ECDSA(); err == nil {
		t.Fatal("Schnorr interpreted as ECDSA")
	}
	if _, err = NewTransactionBuilder(nil).AddTypedSignatures([]Signature{signature}); err == nil {
		t.Fatal("Schnorr entered legacy builder")
	}
	for _, invalid := range []Signature{{}, {scheme: SignatureECDSA}} {
		if _, err = invalid.Schnorr(); err == nil {
			t.Fatal("non-Schnorr interpreted as Schnorr")
		}
		if _, err = snapshot.AddSignatures([]Signature{invalid, invalid}); err == nil {
			t.Fatal("non-Schnorr entered Taproot witness")
		}
	}
}
