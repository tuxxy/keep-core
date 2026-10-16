// Package deposit is the K-06 host for FROST deposits, sweeps and depositor
// refunds. It builds the proposed D-04 template, plans and authorizes sweeps
// from finalized C-05 reveal state, holds the VD-07 main purpose lock and
// drives the sweep through the K-04 executor. See README.md and
// verification/BOUNDARIES.md.
package deposit

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Terms mirrors C-05 FrostDepositProfile.Terms. The D-04 template is still a
// proposal (owner decision D-04 OPEN); these bytes follow the C-05 contract at
// the pinned tbtc-v2 revision.
type Terms struct {
	ChainID        *big.Int // uint256, as in C-05
	Bridge         [20]byte
	BitcoinGenesis [32]byte
	WalletID       [32]byte
	DescriptorHash [32]byte
	Depositor      [20]byte
	Vault          [20]byte
	Blinding       [8]byte
	Amount         uint64
	RefundKey      [32]byte
	RefundLocktime uint32
	ExtraData      [32]byte
}

// LeafVersion is the BIP342 tapscript leaf version.
const LeafVersion = 0xc0

// minimumLocktime is the first Unix-time CLTV value. The refund locktime is a
// timestamp, compared with Bitcoin median-time-past (BIP113).
const minimumLocktime = 500_000_000

var receiptDomain = crypto.Keccak256Hash([]byte("tbtc-v2/frost-deposit-receipt/v1"))

// InternalKey is BIP341's NUMS point H = lift_x(SHA256(uncompressed G)). The
// proposed profile uses H itself (no H+rG blinding) and assumes its discrete
// log is unknown.
var InternalKey = [32]byte{
	0x50, 0x92, 0x9b, 0x74, 0xc1, 0xa0, 0x49, 0x54, 0xb7, 0x8b, 0x4b, 0x60, 0x35, 0xe9, 0x7a, 0x5e,
	0x07, 0x8a, 0x5a, 0x0f, 0x28, 0xec, 0x96, 0xd5, 0x47, 0xbf, 0xee, 0x9a, 0xce, 0x80, 0x3a, 0xc0,
}

func word(v uint64) []byte {
	out := make([]byte, 32)
	new(big.Int).SetUint64(v).FillBytes(out)
	return out
}
func address32(a [20]byte) []byte { return append(make([]byte, 12), a[:]...) }

// Receipt is sha256(abi.encode(DOMAIN, terms...)), exactly as C-05 computes it.
func (t Terms) Receipt() ([32]byte, error) {
	if t.ChainID == nil || t.ChainID.Sign() <= 0 || t.ChainID.BitLen() > 256 || t.Bridge == ([20]byte{}) || t.BitcoinGenesis == ([32]byte{}) || t.WalletID == ([32]byte{}) || t.DescriptorHash == ([32]byte{}) || t.Depositor == ([20]byte{}) {
		return [32]byte{}, errors.New("FROST deposit domain")
	}
	if t.Amount == 0 || t.Amount > uint64(bitcoin.MaxSatoshi) {
		return [32]byte{}, errors.New("FROST deposit amount")
	}
	if t.RefundLocktime < minimumLocktime {
		return [32]byte{}, errors.New("FROST refund locktime must be a timestamp")
	}
	b := append([]byte(nil), receiptDomain[:]...)
	chain := make([]byte, 32)
	t.ChainID.FillBytes(chain)
	b = append(b, chain...)
	b = append(b, address32(t.Bridge)...)
	b = append(b, t.BitcoinGenesis[:]...)
	b = append(b, t.WalletID[:]...)
	b = append(b, t.DescriptorHash[:]...)
	b = append(b, address32(t.Depositor)...)
	b = append(b, address32(t.Vault)...)
	b = append(b, append(t.Blinding[:], make([]byte, 24)...)...)
	b = append(b, word(t.Amount)...)
	b = append(b, t.RefundKey[:]...)
	b = append(b, word(uint64(t.RefundLocktime))...)
	b = append(b, t.ExtraData[:]...)
	return sha256.Sum256(b), nil
}

// scriptNumber is the minimal CScriptNum push of a positive timestamp.
func scriptNumber(v uint32) []byte {
	var n []byte
	for v != 0 {
		n = append(n, byte(v))
		v >>= 8
	}
	if n[len(n)-1]&0x80 != 0 {
		n = append(n, 0)
	}
	return append([]byte{byte(len(n))}, n...)
}

// WalletLeaf is PUSH32(receipt) DROP PUSH32(Q) CHECKSIG.
func WalletLeaf(receipt, key [32]byte) bitcoin.Script {
	out := append([]byte{txscript.OP_DATA_32}, receipt[:]...)
	out = append(out, txscript.OP_DROP, txscript.OP_DATA_32)
	out = append(out, key[:]...)
	return append(out, txscript.OP_CHECKSIG)
}

// RefundLeaf is PUSH32(receipt) DROP <locktime> CLTV DROP PUSH32(refundKey) CHECKSIG.
func RefundLeaf(receipt [32]byte, t Terms) bitcoin.Script {
	out := append([]byte{txscript.OP_DATA_32}, receipt[:]...)
	out = append(out, txscript.OP_DROP)
	out = append(out, scriptNumber(t.RefundLocktime)...)
	out = append(out, txscript.OP_CHECKLOCKTIMEVERIFY, txscript.OP_DROP, txscript.OP_DATA_32)
	out = append(out, t.RefundKey[:]...)
	return append(out, txscript.OP_CHECKSIG)
}

// Construction is the complete Bitcoin data of one deposit, equal to C-05
// FrostDepositAdmission.Construction.
type Construction struct {
	OutputKey     [32]byte
	Root          [32]byte
	Parity        uint8
	Receipt       [32]byte
	WalletLeaf    bitcoin.Script
	RefundLeaf    bitcoin.Script
	WalletControl []byte
	RefundControl []byte
}

// Script is the funding output script OP_1 PUSH32 outputKey.
func (c Construction) Script() bitcoin.Script {
	return append(bitcoin.Script{txscript.OP_1, txscript.OP_DATA_32}, c.OutputKey[:]...)
}

// WalletSpend is the script-path spend the sweep signs under Q.
func (c Construction) WalletSpend() *bitcoin.TapscriptSpend {
	return &bitcoin.TapscriptSpend{Leaf: append(bitcoin.Script(nil), c.WalletLeaf...), Control: append([]byte(nil), c.WalletControl...)}
}

func leafHash(script []byte) [32]byte {
	return [32]byte(txscript.NewBaseTapLeaf(script).TapHash())
}

// Build constructs the deposit for wallet key Q. Both keys must be valid
// x-only curve points; the tweak is BIP341's, computed independently of C-07.
func Build(t Terms, key [32]byte) (Construction, error) {
	receipt, err := t.Receipt()
	if err != nil {
		return Construction{}, err
	}
	if _, err = schnorr.ParsePubKey(key[:]); err != nil {
		return Construction{}, errors.New("wallet key is not a curve point")
	}
	if _, err = schnorr.ParsePubKey(t.RefundKey[:]); err != nil {
		return Construction{}, errors.New("refund key is not a curve point")
	}
	internal, err := schnorr.ParsePubKey(InternalKey[:])
	if err != nil {
		return Construction{}, err
	}
	c := Construction{Receipt: receipt, WalletLeaf: WalletLeaf(receipt, key), RefundLeaf: RefundLeaf(receipt, t)}
	walletHash, refundHash := leafHash(c.WalletLeaf), leafHash(c.RefundLeaf)
	c.Root = branchHash(walletHash, refundHash)
	output := txscript.ComputeTaprootOutputKey(internal, c.Root[:])
	copy(c.OutputKey[:], schnorr.SerializePubKey(output))
	if output.SerializeCompressed()[0] == 0x03 {
		c.Parity = 1
	}
	c.WalletControl = controlBlock(refundHash, c.Parity)
	c.RefundControl = controlBlock(walletHash, c.Parity)
	return c, nil
}

func branchHash(a, b [32]byte) [32]byte {
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}
	return [32]byte(*chainhash.TaggedHash([]byte("TapBranch"), a[:], b[:]))
}

func controlBlock(sibling [32]byte, parity uint8) []byte {
	out := append([]byte{LeafVersion | parity}, InternalKey[:]...)
	return append(out, sibling[:]...)
}

// OutpointKey is the C-05 deposit and custody key: keccak256(rawTxHash ||
// uint32 BE index), the same key C-04 uses.
func OutpointKey(o bitcoin.TransactionOutpoint) [32]byte {
	b := append([]byte(nil), o.TransactionHash[:]...)
	b = binary.BigEndian.AppendUint32(b, o.OutputIndex)
	return crypto.Keccak256Hash(b)
}
