package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// PreviousOutput includes the complete data committed by BIP341. The host must
// resolve it from the referenced transaction and check Bitcoin chain state.
type PreviousOutput struct {
	Outpoint        TransactionOutpoint
	Value           int64
	PublicKeyScript Script
}

const MaxSatoshi int64 = 21_000_000 * 100_000_000
const maxTaprootInputs = 256
const maxTaprootOutputs = 256

// TaprootTransaction is an immutable, single-wallet key-path snapshot. Q is
// already the output key; this type never applies a TapTweak. SIGHASH_DEFAULT,
// no annex, and exactly one 64-byte witness item per input are the only profile.
type TaprootTransaction struct {
	transaction *Transaction
	prevouts    []PreviousOutput
	key         [32]byte
	digests     [][32]byte
	commitment  [32]byte
	fee         int64
}

func cloneBitcoinTransaction(tx *Transaction) *Transaction {
	out := &Transaction{Version: tx.Version, Locktime: tx.Locktime}
	for _, input := range tx.Inputs {
		p := *input.Outpoint
		v := *input
		v.Outpoint = &p
		v.SignatureScript = append([]byte(nil), input.SignatureScript...)
		v.Witness = nil
		for _, item := range input.Witness {
			v.Witness = append(v.Witness, append([]byte(nil), item...))
		}
		out.Inputs = append(out.Inputs, &v)
	}
	for _, output := range tx.Outputs {
		v := *output
		v.PublicKeyScript = append(Script(nil), output.PublicKeyScript...)
		out.Outputs = append(out.Outputs, &v)
	}
	return out
}

// checkedTaprootInputs rejects malformed shapes before invoking btcd APIs.
// This helper also supports the mixed-prevout official BIP341 vectors.
func checkedTaprootInputs(tx *Transaction, prevouts []PreviousOutput) (*internalTransaction, *txscript.MultiPrevOutFetcher, int64, error) {
	if tx == nil || len(tx.Inputs) == 0 || len(tx.Inputs) > maxTaprootInputs || len(tx.Outputs) == 0 || len(tx.Outputs) > maxTaprootOutputs || len(prevouts) != len(tx.Inputs) {
		return nil, nil, 0, errors.New("invalid Taproot transaction shape")
	}
	fetcher := txscript.NewMultiPrevOutFetcher(nil)
	var inputTotal, outputTotal int64
	for i, input := range tx.Inputs {
		if input == nil || input.Outpoint == nil || len(input.SignatureScript) != 0 || len(input.Witness) != 0 {
			return nil, nil, 0, errors.New("Taproot snapshot must be unsigned")
		}
		p := prevouts[i]
		if p.Outpoint != *input.Outpoint || p.Outpoint.TransactionHash == (Hash{}) || p.Value <= 0 || p.Value > MaxSatoshi || len(p.PublicKeyScript) == 0 || len(p.PublicKeyScript) > 10000 || inputTotal > MaxSatoshi-p.Value {
			return nil, nil, 0, fmt.Errorf("invalid previous output %d", i)
		}
		point := wire.OutPoint{Hash: chainhash.Hash(p.Outpoint.TransactionHash), Index: p.Outpoint.OutputIndex}
		if fetcher.FetchPrevOutput(point) != nil {
			return nil, nil, 0, errors.New("duplicate transaction input")
		}
		fetcher.AddPrevOut(point, wire.NewTxOut(p.Value, append([]byte(nil), p.PublicKeyScript...)))
		inputTotal += p.Value
	}
	for _, output := range tx.Outputs {
		if output == nil || output.Value <= 0 || output.Value > MaxSatoshi || outputTotal > MaxSatoshi-output.Value || len(output.PublicKeyScript) == 0 || len(output.PublicKeyScript) > 10000 {
			return nil, nil, 0, errors.New("invalid Taproot output")
		}
		outputTotal += output.Value
	}
	if outputTotal >= inputTotal {
		return nil, nil, 0, errors.New("transaction must have a positive fee")
	}
	internal := newInternalTransaction()
	internal.fromTransaction(cloneBitcoinTransaction(tx))
	if internal.SerializeSizeStripped() > 100000 {
		return nil, nil, 0, errors.New("Taproot snapshot exceeds local size bound")
	}
	return internal, fetcher, inputTotal - outputTotal, nil
}

// TaprootSignatureHash computes a full-width default key-path digest. It is
// not an authorization API. The caller must validate purpose and reservation.
func TaprootSignatureHash(tx *Transaction, prevouts []PreviousOutput, index int) ([32]byte, error) {
	internal, fetcher, _, err := checkedTaprootInputs(tx, prevouts)
	if err != nil {
		return [32]byte{}, err
	}
	if index < 0 || index >= len(tx.Inputs) {
		return [32]byte{}, errors.New("invalid signing input index")
	}
	if !txscript.IsPayToTaproot(prevouts[index].PublicKeyScript) {
		return [32]byte{}, errors.New("signing input is not P2TR")
	}
	raw, err := txscript.CalcTaprootSignatureHash(txscript.NewTxSigHashes(internal.MsgTx, fetcher), txscript.SigHashDefault, internal.MsgTx, index, fetcher)
	if err != nil {
		return [32]byte{}, err
	}
	var digest [32]byte
	copy(digest[:], raw)
	return digest, nil
}

func NewTaprootTransaction(tx *Transaction, prevouts []PreviousOutput, key [32]byte) (*TaprootTransaction, error) {
	script, err := PayToTaproot(key)
	if err != nil {
		return nil, err
	}
	_, _, fee, err := checkedTaprootInputs(tx, prevouts)
	if err != nil {
		return nil, err
	}
	if tx.Version != 2 {
		return nil, errors.New("unsupported Taproot transaction version")
	}
	result := &TaprootTransaction{transaction: cloneBitcoinTransaction(tx), key: key, fee: fee}
	for _, p := range prevouts {
		if !bytes.Equal(p.PublicKeyScript, script) {
			return nil, errors.New("previous output does not match wallet output key")
		}
		p.PublicKeyScript = append(Script(nil), p.PublicKeyScript...)
		result.prevouts = append(result.prevouts, p)
	}
	for i := range tx.Inputs {
		digest, err := TaprootSignatureHash(result.transaction, result.prevouts, i)
		if err != nil {
			return nil, err
		}
		result.digests = append(result.digests, digest)
	}
	data := append([]byte("keep-core/taproot-snapshot/v1"), key[:]...)
	data = append(data, result.transaction.Serialize(Standard)...)
	for _, p := range result.prevouts {
		data = binary.LittleEndian.AppendUint64(data, uint64(p.Value))
		var buf bytes.Buffer
		_ = wire.WriteVarBytes(&buf, 0, p.PublicKeyScript)
		data = append(data, buf.Bytes()...)
	}
	result.commitment = sha256.Sum256(data)
	return result, nil
}
func (t *TaprootTransaction) Unsigned() *Transaction { return cloneBitcoinTransaction(t.transaction) }
func (t *TaprootTransaction) PreviousOutputs() []PreviousOutput {
	out := append([]PreviousOutput(nil), t.prevouts...)
	for i := range out {
		out[i].PublicKeyScript = append(Script(nil), out[i].PublicKeyScript...)
	}
	return out
}
func (t *TaprootTransaction) Digests() [][32]byte  { return append([][32]byte(nil), t.digests...) }
func (t *TaprootTransaction) Commitment() [32]byte { return t.commitment }
func (t *TaprootTransaction) OutputKey() [32]byte  { return t.key }
func (t *TaprootTransaction) Fee() int64           { return t.fee }

// VirtualSize includes the exact single-signature witness shape, before signing.
func (t *TaprootTransaction) VirtualSize() int64 {
	tx := t.Unsigned()
	for _, input := range tx.Inputs {
		input.Witness = [][]byte{make([]byte, 64)}
	}
	weight := 3*len(tx.Serialize(Standard)) + len(tx.Serialize(Witness))
	return int64((weight + 3) / 4)
}
func (t *TaprootTransaction) Matches(tx *Transaction, prevouts []PreviousOutput) error {
	other, err := NewTaprootTransaction(tx, prevouts, t.key)
	if err != nil {
		return err
	}
	if other.commitment != t.commitment {
		return errors.New("authorized transaction changed")
	}
	return nil
}

// AddSignatures verifies every signature before returning any signed bytes.
func (t *TaprootTransaction) AddSignatures(signatures []Signature) (*Transaction, error) {
	if len(signatures) != len(t.digests) {
		return nil, errors.New("wrong Taproot signatures count")
	}
	tx := t.Unsigned()
	key, err := schnorr.ParsePubKey(t.key[:])
	if err != nil {
		return nil, err
	}
	for i, s := range signatures {
		raw, err := s.Schnorr()
		if err != nil {
			return nil, err
		}
		signature, err := schnorr.ParseSignature(raw[:])
		if err != nil || !signature.Verify(t.digests[i][:], key) {
			return nil, fmt.Errorf("invalid Taproot signature for input %d", i)
		}
		tx.Inputs[i].Witness = [][]byte{append([]byte(nil), raw[:]...)}
	}
	if err = t.ValidateSigned(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// ValidateSigned checks immutable fields, the exact witness profile, and the
// Bitcoin script engine. A broadcaster must call it on the bytes it will send.
func (t *TaprootTransaction) ValidateSigned(tx *Transaction) error {
	if tx == nil || len(tx.Inputs) != len(t.transaction.Inputs) || len(tx.Outputs) != len(t.transaction.Outputs) {
		return errors.New("signed transaction shape changed")
	}
	for _, input := range tx.Inputs {
		if input == nil || input.Outpoint == nil || len(input.SignatureScript) != 0 || len(input.Witness) != 1 || len(input.Witness[0]) != 64 {
			return errors.New("invalid default key-path witness")
		}
	}
	for _, output := range tx.Outputs {
		if output == nil {
			return errors.New("missing signed output")
		}
	}
	if !bytes.Equal(tx.Serialize(Standard), t.transaction.Serialize(Standard)) {
		return errors.New("signed transaction fields changed")
	}
	internal, fetcher, _, err := checkedTaprootInputs(t.transaction, t.prevouts)
	if err != nil {
		return err
	}
	internal.fromTransaction(cloneBitcoinTransaction(tx))
	hashes := txscript.NewTxSigHashes(internal.MsgTx, fetcher)
	for i, p := range t.prevouts {
		engine, err := txscript.NewEngine(p.PublicKeyScript, internal.MsgTx, i, txscript.StandardVerifyFlags, nil, hashes, p.Value, fetcher)
		if err != nil {
			return err
		}
		if err = engine.Execute(); err != nil {
			return fmt.Errorf("Taproot input %d: %w", i, err)
		}
	}
	return nil
}
