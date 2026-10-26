package spv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

const retainedTaprootSHA = "6139d0175dbd00be7815050866f007ebd11ce9d4a66ad3a4eb0a140de5bc13bd"

type retainedMerkle struct {
	BlockHeight uint     `json:"block_height"`
	Merkle      []string `json:"merkle"`
	Pos         uint     `json:"pos"`
}
type retainedVector struct {
	Height                                               uint
	BlockHashes, Headers                                 []string
	RawTransaction, RawParent, RawCoinbase, CoinbaseHash string
	MerkleProof, CoinbaseMerkleProof                     retainedMerkle
}
type retainedBitcoin struct {
	bitcoin.Chain
	vector                        retainedVector
	transaction, parent, coinbase *bitcoin.Transaction
	headers                       []*bitcoin.BlockHeader
	available                     uint
}

func (c *retainedBitcoin) GetTransaction(hash bitcoin.Hash) (*bitcoin.Transaction, error) {
	for _, tx := range []*bitcoin.Transaction{c.transaction, c.parent, c.coinbase} {
		if tx.Hash() == hash {
			return tx, nil
		}
	}
	return nil, errors.New("transaction outside retained snapshot")
}
func (c *retainedBitcoin) GetTransactionConfirmations(context.Context, bitcoin.Hash) (uint, error) {
	return c.available, nil
}
func (c *retainedBitcoin) GetLatestBlockHeight() (uint, error) {
	return c.vector.Height + c.available - 1, nil
}
func (c *retainedBitcoin) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	if height < c.vector.Height || height >= c.vector.Height+c.available {
		return nil, errors.New("header outside retained snapshot")
	}
	return c.headers[height-c.vector.Height], nil
}
func (c *retainedBitcoin) GetCoinbaseTxHash(height uint) (bitcoin.Hash, error) {
	if height != c.vector.Height {
		return bitcoin.Hash{}, errors.New("coinbase outside retained block")
	}
	return c.coinbase.Hash(), nil
}
func (c *retainedBitcoin) GetTransactionMerkleProof(hash bitcoin.Hash, height uint) (*bitcoin.TransactionMerkleProof, error) {
	var path retainedMerkle
	if hash == c.transaction.Hash() {
		path = c.vector.MerkleProof
	} else if hash == c.coinbase.Hash() {
		path = c.vector.CoinbaseMerkleProof
	} else {
		return nil, errors.New("unretained inclusion")
	}
	if height != path.BlockHeight {
		return nil, errors.New("retained inclusion height mismatch")
	}
	return &bitcoin.TransactionMerkleProof{BlockHeight: height, Position: path.Pos, MerkleNodes: path.Merkle}, nil
}

type retainedEpochs struct{ current, previous *big.Int }

func (e retainedEpochs) GetCurrentAndPrevEpochDifficulty() (*big.Int, *big.Int, error) {
	return new(big.Int).Set(e.current), new(big.Int).Set(e.previous), nil
}

type proofHandoff struct {
	movement.Chain
	proof       movement.Proof
	wallet      [32]byte
	submissions int
	done        bool
}

func (c *proofHandoff) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 1}, nil
}
func (c *proofHandoff) ProvenTransaction(context.Context, uint64, [32]byte) ([32]byte, error) {
	if c.done {
		return c.wallet, nil
	}
	return [32]byte{}, nil
}
func (c *proofHandoff) SubmitMovementProof(_ context.Context, wallet [32]byte, proof movement.Proof) error {
	c.wallet = wallet
	c.proof = proof
	c.submissions++
	c.done = true
	return nil
}
func (c *proofHandoff) TxProofDifficultyFactor() (*big.Int, error) { return big.NewInt(6), nil }

// This is the ACTUAL maintainer and default Bitcoin proof assembler. The
// historical Bitcoin snapshot and public L1 submission handoff are controlled
// providers; the exact produced arguments are consumed by the full Bridge
// entrypoint in Frost.LifecycleRealisticSpv. No test assembler is installed.
func TestFrostRetainedMainnetMovementProof(t *testing.T) {
	path, output := os.Getenv("FROST_K09_REALISTIC_VECTOR"), os.Getenv("FROST_K09_REALISTIC_PROOF_PRIVATE")
	if path == "" || output == "" {
		t.Skip("explicit retained-vector producer/consumer integration only")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != retainedTaprootSHA {
		t.Fatal("retained vector pin mismatch")
	}
	var vector retainedVector
	if err = json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	chain := &retainedBitcoin{vector: vector, available: uint(len(vector.Headers))}
	for _, text := range vector.Headers {
		raw, err := hex.DecodeString(text)
		if err != nil || len(raw) != bitcoin.BlockHeaderByteLength {
			t.Fatal("retained header encoding")
		}
		header := new(bitcoin.BlockHeader)
		header.Deserialize([bitcoin.BlockHeaderByteLength]byte(raw))
		chain.headers = append(chain.headers, header)
	}
	if len(chain.headers) != 10 {
		t.Fatal("retained range missing")
	}
	for _, entry := range []struct {
		text string
		to   **bitcoin.Transaction
	}{{vector.RawTransaction, &chain.transaction}, {vector.RawParent, &chain.parent}, {vector.RawCoinbase, &chain.coinbase}} {
		raw, err := hex.DecodeString(entry.text)
		if err != nil {
			t.Fatal(err)
		}
		tx := new(bitcoin.Transaction)
		if err = tx.Deserialize(raw); err != nil {
			t.Fatal(err)
		}
		*entry.to = tx
	}
	tx := chain.transaction
	if tx.Version != 2 || len(tx.Inputs) != 1 || len(tx.Outputs) != 1 || len(tx.Inputs[0].SignatureScript) != 0 || chain.parent.Hash() != tx.Inputs[0].Outpoint.TransactionHash {
		t.Fatal("retained transaction/parent identity mismatch")
	}
	parent := chain.parent.Outputs[tx.Inputs[0].Outpoint.OutputIndex]
	target := tx.Outputs[0]
	if len(parent.PublicKeyScript) != 34 || len(target.PublicKeyScript) != 34 || !bytes.Equal(parent.PublicKeyScript[:2], []byte{0x51, 0x20}) || !bytes.Equal(target.PublicKeyScript[:2], []byte{0x51, 0x20}) {
		t.Fatal("retained shape is not a FROST key-path movement")
	}
	var sourceKey [32]byte
	copy(sourceKey[:], parent.PublicKeyScript[2:])
	wallet := dkg.WalletID(sourceKey)
	capture := &proofHandoff{}
	epochs := retainedEpochs{current: chain.headers[1].Difficulty(), previous: chain.headers[0].Difficulty()}
	if epochs.current.Cmp(epochs.previous) == 0 || epochs.previous.Sign() <= 0 {
		t.Fatal("retarget/nonzero work missing")
	}
	maintainer := FrostMovementMaintainer{Chain: capture, Bitcoin: chain, Confirmations: 1, ProofDepth: DifficultyProofDepth(chain, capture, epochs, 1)}
	chain.available = 5
	if _, err = maintainer.ProveMovement(context.Background(), wallet, [32]byte(tx.Hash())); !errors.Is(err, ErrFrostProofNotReady) || capture.submissions != 0 {
		t.Fatal("control RealisticFrostDepth assertion: inadequate accumulated work admitted", err)
	}
	chain.available = uint(len(chain.headers))
	if _, err = maintainer.ProveMovement(context.Background(), wallet, [32]byte(tx.Hash())); err != nil {
		t.Fatal("real maintainer failed retained movement", err)
	}
	p := capture.proof
	if capture.submissions != 1 || len(p.Proof.BitcoinHeaders) != 6*bitcoin.BlockHeaderByteLength {
		t.Fatal("control RealisticFrostDepth assertion: producer did not derive the six-header retarget proof")
	}
	if action, err := maintainer.ProveMovement(context.Background(), wallet, [32]byte(tx.Hash())); err != nil || action.Kind != "already-proven" || capture.submissions != 1 {
		t.Fatal("retained producer did not reconcile idempotently", err)
	}
	hexBytes := func(raw []byte) string { return "0x" + hex.EncodeToString(raw) }
	result := map[string]any{
		"vectorSha256": retainedTaprootSHA, "headerCount": len(p.Proof.BitcoinHeaders) / bitcoin.BlockHeaderByteLength, "factor": 6, "walletId": hexBytes(wallet[:]),
		"sourceKey": hexBytes(sourceKey[:]), "targetKey": hexBytes(target.PublicKeyScript[2:]), "inputTransactionHash": hexBytes(tx.Inputs[0].Outpoint.TransactionHash[:]), "inputIndex": tx.Inputs[0].Outpoint.OutputIndex, "inputValue": parent.Value, "outputValue": target.Value, "fee": parent.Value - target.Value,
		"currentDifficulty": epochs.current.String(), "previousDifficulty": epochs.previous.String(),
		"transaction": map[string]any{"version": hexBytes(p.Transaction.Version[:]), "inputVector": hexBytes(p.Transaction.InputVector), "outputVector": hexBytes(p.Transaction.OutputVector), "locktime": hexBytes(p.Transaction.Locktime[:])},
		"proof":       map[string]any{"merkleProof": hexBytes(p.Proof.MerkleProof), "txIndexInBlock": p.Proof.TxIndexInBlock, "bitcoinHeaders": hexBytes(p.Proof.BitcoinHeaders), "coinbasePreimage": hexBytes(p.Proof.CoinbasePreimage[:]), "coinbaseProof": hexBytes(p.Proof.CoinbaseProof)},
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(output) {
		t.Fatal("private absolute output path required")
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("K09-REALISTIC: actual maintainer derived six real retarget headers; five refused; exact public proof handoff written privately")
}
