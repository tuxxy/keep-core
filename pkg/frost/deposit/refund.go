package deposit

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// refundSequence enables the absolute locktime and opts into replacement; bit
// 31 stays set, so no BIP68 relative lock applies.
const refundSequence = 0xfffffffd

// Refund builds and signs the depositor's refund of one funded deposit. It
// uses only public construction data, the funding outpoint and the refund
// key: no operator, Ethereum node or wallet signature is involved (P-K06-
// REFUND-RACE). The transaction's locktime is the refund timestamp, so Bitcoin
// accepts it only once median-time-past has passed that time. The construction
// must be the one for these terms (from Build or C-05 frostDepositRefundConstruction);
// the refund leaf, receipt and control block are checked before signing, and
// the signed transaction is run through the script engine.
func Refund(t Terms, c Construction, funding bitcoin.TransactionOutpoint, destination bitcoin.Script, fee int64, key *btcec.PrivateKey) (*bitcoin.Transaction, error) {
	if key == nil || len(destination) == 0 || funding.TransactionHash == (bitcoin.Hash{}) {
		return nil, errors.New("incomplete refund request")
	}
	receipt, err := t.Receipt()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(schnorr.SerializePubKey(key.PubKey()), t.RefundKey[:]) {
		return nil, errors.New("key is not the deposit refund key")
	}
	if c.Receipt != receipt || !bytes.Equal(c.RefundLeaf, RefundLeaf(receipt, t)) {
		return nil, errors.New("construction does not belong to these terms")
	}
	control, err := txscript.ParseControlBlock(c.RefundControl)
	if err != nil {
		return nil, err
	}
	if control.LeafVersion != txscript.BaseLeafVersion {
		return nil, errors.New("unsupported refund leaf version")
	}
	if err = txscript.VerifyTaprootLeafCommitment(control, c.OutputKey[:], c.RefundLeaf); err != nil {
		return nil, fmt.Errorf("refund leaf not committed to the deposit output: %w", err)
	}
	value := int64(t.Amount)
	if fee <= 0 || fee >= value {
		return nil, errors.New("refund fee outside the deposit value")
	}
	tx := &bitcoin.Transaction{Version: 2, Locktime: t.RefundLocktime, Inputs: []*bitcoin.TransactionInput{{Outpoint: &funding, Sequence: refundSequence}}, Outputs: []*bitcoin.TransactionOutput{{Value: value - fee, PublicKeyScript: append(bitcoin.Script(nil), destination...)}}}
	prevouts := []bitcoin.PreviousOutput{{Outpoint: funding, Value: value, PublicKeyScript: c.Script()}}
	digest, err := bitcoin.TapscriptSignatureHash(tx, prevouts, 0, c.RefundLeaf)
	if err != nil {
		return nil, err
	}
	signature, err := schnorr.Sign(key, digest[:])
	if err != nil {
		return nil, err
	}
	tx.Inputs[0].Witness = [][]byte{signature.Serialize(), append([]byte(nil), c.RefundLeaf...), append([]byte(nil), c.RefundControl...)}
	if err = execute(tx, prevouts); err != nil {
		return nil, fmt.Errorf("refund failed script validation: %w", err)
	}
	return tx, nil
}

// execute runs every input of a signed transaction through the script engine
// with standard flags (CLTV is checked against the transaction locktime;
// median-time-past is a block rule that only Bitcoin enforces).
func execute(tx *bitcoin.Transaction, prevouts []bitcoin.PreviousOutput) error {
	msg := wire.NewMsgTx(2)
	if err := msg.Deserialize(bytes.NewReader(tx.Serialize(bitcoin.Witness))); err != nil {
		return err
	}
	fetcher := txscript.NewMultiPrevOutFetcher(nil)
	for _, p := range prevouts {
		fetcher.AddPrevOut(wire.OutPoint{Hash: [32]byte(p.Outpoint.TransactionHash), Index: p.Outpoint.OutputIndex}, wire.NewTxOut(p.Value, p.PublicKeyScript))
	}
	hashes := txscript.NewTxSigHashes(msg, fetcher)
	for i, p := range prevouts {
		engine, err := txscript.NewEngine(p.PublicKeyScript, msg, i, txscript.StandardVerifyFlags, nil, hashes, p.Value, fetcher)
		if err != nil {
			return err
		}
		if err = engine.Execute(); err != nil {
			return err
		}
	}
	return nil
}
