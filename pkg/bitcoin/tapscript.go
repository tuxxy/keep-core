package bitcoin

import (
	"bytes"
	"errors"

	"github.com/btcsuite/btcd/txscript"
)

// TapscriptSpend is one K-06 script-path input: a tapleaf (leaf version 0xc0)
// that ends in a single PUSH32 Q CHECKSIG, and the control block that commits
// it to the prevout's output key. The host computes the BIP342 digest itself;
// SNOWFALL signs that digest under Q without a per-deposit tweak (KC-SF-V3
// section 4.5).
type TapscriptSpend struct {
	Leaf    Script
	Control []byte
}

// maxWalletLeaf bounds a wallet leaf well above the D-04 template (69 bytes).
const maxWalletLeaf = 1000

// opSuccess reports BIP342 OP_SUCCESSx opcodes, which make any tapscript
// valid unconditionally.
func opSuccess(op byte) bool {
	return op == 80 || op == 98 || (op >= 126 && op <= 129) || (op >= 131 && op <= 134) || op == 137 || op == 138 || op == 141 || op == 142 || (op >= 149 && op <= 153) || (op >= 187 && op <= 254)
}

// checkWalletLeaf accepts a leaf whose only signature operation is the final
// PUSH32 Q CHECKSIG. It refuses OP_SUCCESSx, OP_CODESEPARATOR and every other
// signature opcode, and non-minimal or truncated pushes.
func checkWalletLeaf(leaf Script, key [32]byte) error {
	suffix := append(append([]byte{txscript.OP_DATA_32}, key[:]...), txscript.OP_CHECKSIG)
	if len(leaf) < len(suffix) || len(leaf) > maxWalletLeaf || !bytes.HasSuffix(leaf, suffix) {
		return errors.New("leaf must end in PUSH32 Q CHECKSIG")
	}
	tokens := txscript.MakeScriptTokenizer(0, leaf)
	for tokens.Next() {
		op := tokens.Opcode()
		if opSuccess(op) {
			return errors.New("leaf contains OP_SUCCESS")
		}
		switch op {
		case txscript.OP_CODESEPARATOR, txscript.OP_CHECKSIGVERIFY, txscript.OP_CHECKSIGADD, txscript.OP_CHECKMULTISIG, txscript.OP_CHECKMULTISIGVERIFY:
			return errors.New("leaf contains a forbidden signature opcode")
		case txscript.OP_CHECKSIG:
			if int(tokens.ByteIndex()) != len(leaf) {
				return errors.New("leaf has more than one CHECKSIG")
			}
		}
		if op <= txscript.OP_PUSHDATA4 && !minimalPush(op, tokens.Data()) {
			return errors.New("leaf has a non-minimal push")
		}
	}
	if tokens.Err() != nil {
		return tokens.Err()
	}
	return nil
}

// minimalPush is the BIP62 minimal-push rule for a data push opcode.
func minimalPush(op byte, data []byte) bool {
	switch n := len(data); {
	case n == 0:
		return op == txscript.OP_0
	case n == 1 && data[0] >= 1 && data[0] <= 16, n == 1 && data[0] == 0x81:
		return false
	case n <= 75:
		return int(op) == n
	case n <= 255:
		return op == txscript.OP_PUSHDATA1
	default:
		return op == txscript.OP_PUSHDATA2 && n <= 65535
	}
}

func (s *TapscriptSpend) check(prevout Script, key [32]byte) error {
	if !txscript.IsPayToTaproot(prevout) {
		return errors.New("script-path prevout is not P2TR")
	}
	if err := checkWalletLeaf(s.Leaf, key); err != nil {
		return err
	}
	control, err := txscript.ParseControlBlock(s.Control)
	if err != nil {
		return err
	}
	if control.LeafVersion != txscript.BaseLeafVersion {
		return errors.New("unsupported tapleaf version")
	}
	return txscript.VerifyTaprootLeafCommitment(control, prevout[2:], s.Leaf)
}

// TapscriptSignatureHash computes the BIP342 SIGHASH_DEFAULT digest of input
// index for the exact leaf (no annex, no code separator). Like
// TaprootSignatureHash it is not an authorization API.
func TapscriptSignatureHash(tx *Transaction, prevouts []PreviousOutput, index int, leaf Script) ([32]byte, error) {
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
	raw, err := txscript.CalcTapscriptSignaturehash(txscript.NewTxSigHashes(internal.MsgTx, fetcher), txscript.SigHashDefault, internal.MsgTx, index, fetcher, txscript.NewBaseTapLeaf(leaf))
	if err != nil {
		return [32]byte{}, err
	}
	var digest [32]byte
	copy(digest[:], raw)
	return digest, nil
}
