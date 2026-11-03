package tbtc_test

// Batch-6 Bitcoin views. Refs tuxxy/keep-core#26 (review B1, B2, B3, B7).
//
// FIXTURE: b6LiveBitcoin is a node process's Bitcoin chain, in place of the
// Electrum connection cmd/start.go builds (named deviation: no Electrum over
// regtest). It is the frozen legacyRegtest's block and mempool model, kept
// live: before every read it pulls the blocks Core added and replaces its
// mempool with Core's, so a node sees the drain, its confirmations and its
// own mined sweep. Errors are returned, never t.Fatal: it runs in a helper
// process. Like the production Electrum connection it also answers
// UnspentByScript (confirmed outputs paying exactly the script and not spent
// in a block or in the mempool), which the FROST planners use and which is
// not on bitcoin.Chain.

import (
	"bytes"
	"context"
	"encoding/hex"

	"github.com/btcsuite/btcd/wire"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

type b6LiveBitcoin struct {
	inner *legacyRegtest
}

var _ bitcoin.Chain = (*b6LiveBitcoin)(nil)

// b6CoreTransaction reads a transaction from Core (txindex is on).
func b6CoreTransaction(core *legacyCore, txid string) (*bitcoin.Transaction, error) {
	var rawHex string
	if err := core.call("getrawtransaction", []interface{}{txid}, &rawHex); err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(rawHex)
	if err != nil {
		return nil, err
	}
	tx := new(bitcoin.Transaction)
	if err := tx.Deserialize(raw); err != nil {
		return nil, err
	}
	return tx, nil
}

// refresh pulls Core's new blocks and current mempool into the model.
func (b *b6LiveBitcoin) refresh() error {
	c := b.inner
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var count int
	if err := c.core.call("getblockcount", nil, &count); err != nil {
		return err
	}
	for height := len(c.blocks); height <= count; height++ {
		var hash, rawHex string
		if err := c.core.call("getblockhash", []interface{}{height}, &hash); err != nil {
			return err
		}
		if err := c.core.call("getblock", []interface{}{hash, 0}, &rawHex); err != nil {
			return err
		}
		raw, err := hex.DecodeString(rawHex)
		if err != nil {
			return err
		}
		var message wire.MsgBlock
		if err := message.Deserialize(bytes.NewReader(raw)); err != nil {
			return err
		}
		h := message.Header
		block := &legacyBlock{header: bitcoin.BlockHeader{
			Version:                 h.Version,
			PreviousBlockHeaderHash: bitcoin.Hash(h.PrevBlock),
			MerkleRootHash:          bitcoin.Hash(h.MerkleRoot),
			Time:                    uint32(h.Timestamp.Unix()),
			Bits:                    h.Bits,
			Nonce:                   h.Nonce,
		}}
		for _, wireTx := range message.Transactions {
			var buffer bytes.Buffer
			if err := wireTx.Serialize(&buffer); err != nil {
				return err
			}
			tx := new(bitcoin.Transaction)
			if err := tx.Deserialize(buffer.Bytes()); err != nil {
				return err
			}
			block.txs = append(block.txs, tx)
		}
		c.blocks = append(c.blocks, block)
	}
	var txids []string
	if err := c.core.call("getrawmempool", nil, &txids); err != nil {
		return err
	}
	known := map[string]*bitcoin.Transaction{}
	for _, tx := range c.mempool {
		known[tx.Hash().Hex(bitcoin.ReversedByteOrder)] = tx
	}
	mempool := make([]*bitcoin.Transaction, 0, len(txids))
	for _, txid := range txids {
		tx, ok := known[txid]
		if !ok {
			var err error
			if tx, err = b6CoreTransaction(c.core, txid); err != nil {
				// Mined or evicted since getrawmempool; the next read sees it.
				continue
			}
		}
		mempool = append(mempool, tx)
	}
	c.mempool = mempool
	return nil
}

func (b *b6LiveBitcoin) GetTransaction(hash bitcoin.Hash) (*bitcoin.Transaction, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetTransaction(hash)
}

func (b *b6LiveBitcoin) GetTransactionConfirmations(ctx context.Context, hash bitcoin.Hash) (uint, error) {
	if err := b.refresh(); err != nil {
		return 0, err
	}
	return b.inner.GetTransactionConfirmations(ctx, hash)
}

func (b *b6LiveBitcoin) BroadcastTransaction(tx *bitcoin.Transaction) error {
	if err := b.refresh(); err != nil {
		return err
	}
	return b.inner.BroadcastTransaction(tx)
}

func (b *b6LiveBitcoin) GetLatestBlockHeight() (uint, error) {
	if err := b.refresh(); err != nil {
		return 0, err
	}
	return b.inner.GetLatestBlockHeight()
}

func (b *b6LiveBitcoin) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetBlockHeader(height)
}

func (b *b6LiveBitcoin) GetTransactionMerkleProof(hash bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetTransactionMerkleProof(hash, height)
}

func (b *b6LiveBitcoin) GetTransactionsForPublicKeyHash(publicKeyHash [20]byte, limit int) ([]*bitcoin.Transaction, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetTransactionsForPublicKeyHash(publicKeyHash, limit)
}

func (b *b6LiveBitcoin) GetTxHashesForPublicKeyHash(publicKeyHash [20]byte) ([]bitcoin.Hash, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetTxHashesForPublicKeyHash(publicKeyHash)
}

func (b *b6LiveBitcoin) GetMempoolForPublicKeyHash(publicKeyHash [20]byte) ([]*bitcoin.Transaction, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetMempoolForPublicKeyHash(publicKeyHash)
}

func (b *b6LiveBitcoin) GetUtxosForPublicKeyHash(publicKeyHash [20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetUtxosForPublicKeyHash(publicKeyHash)
}

func (b *b6LiveBitcoin) GetMempoolUtxosForPublicKeyHash(publicKeyHash [20]byte) ([]*bitcoin.UnspentTransactionOutput, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b.inner.GetMempoolUtxosForPublicKeyHash(publicKeyHash)
}

func (b *b6LiveBitcoin) EstimateSatPerVByteFee(blocks uint32) (int64, error) {
	return b.inner.EstimateSatPerVByteFee(blocks)
}

func (b *b6LiveBitcoin) GetCoinbaseTxHash(height uint) (bitcoin.Hash, error) {
	if err := b.refresh(); err != nil {
		return bitcoin.Hash{}, err
	}
	return b.inner.GetCoinbaseTxHash(height)
}

// UnspentByScript is the Electrum connection's confirmed script query:
// confirmed outputs paying exactly script, oldest block first, without those
// spent in a block or in the mempool.
func (b *b6LiveBitcoin) UnspentByScript(_ context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	if err := b.refresh(); err != nil {
		return nil, err
	}
	c := b.inner
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var out []bitcoin.PreviousOutput
	for _, block := range c.blocks {
		for _, tx := range block.txs {
			for i, output := range tx.Outputs {
				outpoint := bitcoin.TransactionOutpoint{TransactionHash: tx.Hash(), OutputIndex: uint32(i)}
				if !bytes.Equal(output.PublicKeyScript, script) || c.spentLocked(&outpoint, true) {
					continue
				}
				out = append(out, bitcoin.PreviousOutput{Outpoint: outpoint, Value: output.Value, PublicKeyScript: append(bitcoin.Script(nil), script...)})
			}
		}
	}
	return out, nil
}

// --- The test process's reads of Core (review B1, B7) -----------------------

// coreTransaction reads a node's transaction from Core: the parent's own
// legacyRegtest only knows what it mined or sent itself.
func (w *frostNodeWorld) coreTransaction(txid string) (*bitcoin.Transaction, error) {
	return b6CoreTransaction(w.btc.core, txid)
}

// coreConfirmations is Core's confirmation count for a transaction (0 in the
// mempool).
func (w *frostNodeWorld) coreConfirmations(txid string) (uint, error) {
	var verbose struct {
		Confirmations uint `json:"confirmations"`
	}
	if err := w.btc.core.call("getrawtransaction", []interface{}{txid, true}, &verbose); err != nil {
		return 0, err
	}
	return verbose.Confirmations, nil
}

// syncParent brings the parent's view up to Core's blocks, for proofs of
// transactions another call mined.
func (w *frostNodeWorld) syncParent() {
	w.btc.mutex.Lock()
	defer w.btc.mutex.Unlock()
	w.btc.syncLocked(w.t)
}

// mineCore mines tx unless Core already confirmed it, then syncs the parent
// so it can prove it.
func (w *frostNodeWorld) mineCore(tx *bitcoin.Transaction) {
	t := w.t
	t.Helper()
	confirmations, err := w.coreConfirmations(tx.Hash().Hex(bitcoin.ReversedByteOrder))
	if err != nil {
		t.Fatalf("Core does not hold %s: %v", tx.Hash().Hex(bitcoin.ReversedByteOrder), err)
	}
	if confirmations == 0 {
		w.btc.mine(t, tx)
		return
	}
	w.syncParent()
}
