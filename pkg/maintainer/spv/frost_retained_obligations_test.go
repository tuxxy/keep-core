package spv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

const retainedRedemptionSHA = "02131a742f4c32a2366781162c5b3e84f0a815864a6ff21d277803abba128f45"

func retainedObligationChain(t *testing.T, path, pin string) *retainedBitcoin {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		t.Fatal("retained obligation vector pin mismatch")
	}
	var vector retainedVector
	if err = json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	c := &retainedBitcoin{vector: vector, available: uint(len(vector.Headers))}
	for _, text := range vector.Headers {
		raw, err := hex.DecodeString(text)
		if err != nil || len(raw) != bitcoin.BlockHeaderByteLength {
			t.Fatal("retained header encoding")
		}
		h := new(bitcoin.BlockHeader)
		h.Deserialize([bitcoin.BlockHeaderByteLength]byte(raw))
		c.headers = append(c.headers, h)
	}
	for _, entry := range []struct {
		raw  string
		dest **bitcoin.Transaction
	}{{vector.RawTransaction, &c.transaction}, {vector.RawParent, &c.parent}, {vector.RawCoinbase, &c.coinbase}} {
		raw, err := hex.DecodeString(entry.raw)
		if err != nil {
			t.Fatal(err)
		}
		tx := new(bitcoin.Transaction)
		if err = tx.Deserialize(raw); err != nil {
			t.Fatal(err)
		}
		*entry.dest = tx
	}
	if len(c.headers) != 10 || len(c.transaction.Inputs) != 1 || c.transaction.Inputs[0].Outpoint.TransactionHash != c.parent.Hash() {
		t.Fatal("retained obligation parent/header correspondence")
	}
	return c
}

func writeObligationProof(t *testing.T, output, pin string, c *retainedBitcoin, wallet [32]byte, tx redemption.BitcoinInfo, proof redemption.BitcoinProof) {
	t.Helper()
	if !filepath.IsAbs(output) || len(proof.BitcoinHeaders) != 6*bitcoin.BlockHeaderByteLength {
		t.Fatal("private output path or six-header proof missing")
	}
	h := func(b []byte) string { return "0x" + hex.EncodeToString(b) }
	in := c.transaction.Inputs[0].Outpoint
	prev := c.parent.Outputs[in.OutputIndex]
	outputs := []map[string]any{}
	for _, out := range c.transaction.Outputs {
		outputs = append(outputs, map[string]any{"value": out.Value, "script": h(out.PublicKeyScript)})
	}
	record := map[string]any{"vectorSha256": pin, "factor": 6, "headerCount": 6, "walletId": h(wallet[:]), "inputTransactionHash": h(in.TransactionHash[:]), "inputIndex": in.OutputIndex, "inputValue": prev.Value, "inputScript": h(prev.PublicKeyScript), "outputs": outputs,
		"previousDifficulty": c.headers[0].Difficulty().String(), "currentDifficulty": c.headers[1].Difficulty().String(),
		"transaction": map[string]any{"version": h(tx.Version[:]), "inputVector": h(tx.InputVector), "outputVector": h(tx.OutputVector), "locktime": h(tx.Locktime[:])},
		"proof":       map[string]any{"merkleProof": h(proof.MerkleProof), "txIndexInBlock": proof.TxIndexInBlock, "bitcoinHeaders": h(proof.BitcoinHeaders), "coinbasePreimage": h(proof.CoinbasePreimage[:]), "coinbaseProof": h(proof.CoinbaseProof)}}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

// Only the pre-existing public obligation and L1 handoff are controlled.
// The native FROST maintainer and Bitcoin assembler are the actual producers.
type retainedSweepHandoff struct {
	deposit.Chain
	request deposit.SweepProof
	done    bool
}

func (*retainedSweepHandoff) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 1}, nil
}
func (*retainedSweepHandoff) Custody(context.Context, uint64, [32]byte) (redemption.Custody, error) {
	return redemption.Custody{}, nil
}
func (c *retainedSweepHandoff) Deposit(context.Context, uint64, [32]byte) (deposit.Record, error) {
	r := deposit.Record{}
	if c.done {
		r.SweptAt = 1
	}
	return r, nil
}
func (c *retainedSweepHandoff) SubmitSweepProof(_ context.Context, p deposit.SweepProof) error {
	c.request, c.done = p, true
	return nil
}
func (*retainedSweepHandoff) TxProofDifficultyFactor() (*big.Int, error) { return big.NewInt(6), nil }

func TestFrostRetainedMainnetSweepProof(t *testing.T) {
	path, output := os.Getenv("FROST_K09_REALISTIC_VECTOR"), os.Getenv("FROST_K09_REALISTIC_SWEEP_PRIVATE")
	if path == "" || output == "" {
		t.Skip("explicit retained sweep producer/consumer integration only")
	}
	c := retainedObligationChain(t, path, retainedTaprootSHA)
	var q [32]byte
	copy(q[:], c.transaction.Outputs[0].PublicKeyScript[2:])
	wallet := dkg.WalletID(q)
	handoff := &retainedSweepHandoff{}
	epochs := retainedEpochs{current: c.headers[1].Difficulty(), previous: c.headers[0].Difficulty()}
	m := FrostSweepMaintainer{Chain: handoff, Bitcoin: c, Confirmations: 1, ProofDepth: DifficultyProofDepth(c, handoff, epochs, 1)}
	c.available = 5
	if _, err := m.ProveSweep(context.Background(), wallet, [32]byte(c.transaction.Hash()), [20]byte{}, nil); !errors.Is(err, ErrFrostProofNotReady) || handoff.done {
		t.Fatal("insufficient sweep work accepted", err)
	}
	c.available = 10
	if _, err := m.ProveSweep(context.Background(), wallet, [32]byte(c.transaction.Hash()), [20]byte{}, nil); err != nil || !handoff.done {
		t.Fatal("retained sweep proof failed", err)
	}
	writeObligationProof(t, output, retainedTaprootSHA, c, wallet, handoff.request.Transaction, handoff.request.Proof)
}

type retainedPaymentHandoff struct {
	redemption.Chain
	hash        [32]byte
	transaction redemption.BitcoinInfo
	proof       redemption.BitcoinProof
	done        bool
}

func (*retainedPaymentHandoff) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 1}, nil
}
func (*retainedPaymentHandoff) Admissions(context.Context, [32]byte, uint64, uint64) ([]redemption.Admission, error) {
	return []redemption.Admission{{ReservationID: [32]byte{1}}}, nil
}
func (c *retainedPaymentHandoff) Intent(context.Context, uint64, [32]byte) (redemption.Intent, error) {
	s := redemption.IntentReserved
	if c.done {
		s = redemption.IntentPaid
	}
	return redemption.Intent{State: s, TransactionHash: c.hash}, nil
}
func (*retainedPaymentHandoff) Alternatives(context.Context, uint64, [32]byte) ([]redemption.Alternative, error) {
	return nil, nil
}
func (c *retainedPaymentHandoff) SubmitPaymentProof(_ context.Context, _ [32]byte, tx redemption.BitcoinInfo, proof redemption.BitcoinProof) error {
	c.transaction, c.proof, c.done = tx, proof, true
	return nil
}
func (*retainedPaymentHandoff) TxProofDifficultyFactor() (*big.Int, error) { return big.NewInt(6), nil }

func TestFrostRetainedMainnetRedemptionProof(t *testing.T) {
	path, output := os.Getenv("FROST_K09_REALISTIC_REDEMPTION_VECTOR"), os.Getenv("FROST_K09_REALISTIC_REDEMPTION_PRIVATE")
	if path == "" || output == "" {
		t.Skip("explicit retained redemption producer/consumer integration only")
	}
	c := retainedObligationChain(t, path, retainedRedemptionSHA)
	var q [32]byte
	copy(q[:], c.parent.Outputs[c.transaction.Inputs[0].Outpoint.OutputIndex].PublicKeyScript[2:])
	wallet := dkg.WalletID(q)
	handoff := &retainedPaymentHandoff{hash: [32]byte(c.transaction.Hash())}
	epochs := retainedEpochs{current: c.headers[1].Difficulty(), previous: c.headers[0].Difficulty()}
	m := FrostRedemptionMaintainer{Chain: handoff, Bitcoin: c, Confirmations: 1, Wallet: wallet, ProofDepth: DifficultyProofDepth(c, handoff, epochs, 1)}
	c.available = 5
	if actions, err := m.ProveSettlements(context.Background()); err != nil || len(actions) != 0 || handoff.done {
		t.Fatal("insufficient redemption work accepted", err)
	}
	c.available = 10
	if actions, err := m.ProveSettlements(context.Background()); err != nil || len(actions) != 1 || !handoff.done {
		t.Fatal("retained redemption proof failed", err)
	}
	writeObligationProof(t, output, retainedRedemptionSHA, c, wallet, handoff.transaction, handoff.proof)
}
