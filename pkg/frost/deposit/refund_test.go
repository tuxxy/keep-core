package deposit

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

func sha3(b []byte) [32]byte { return crypto.Keccak256Hash(b) }

func TestRefundWithoutOperator(t *testing.T) {
	wallet := xonly(testKey(7))
	refundKey := testKey(9)
	terms := sampleTerms(refundKey, wallet)
	c, err := Build(terms, wallet)
	if err != nil {
		t.Fatal(err)
	}
	funding := bitcoin.TransactionOutpoint{OutputIndex: 1}
	funding.TransactionHash[0] = 0x42
	destination := append(bitcoin.Script{0x00, 0x14}, make([]byte, 20)...)
	tx, err := Refund(terms, c, funding, destination, 500, refundKey)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Locktime != terms.RefundLocktime || tx.Inputs[0].Sequence != refundSequence || tx.Outputs[0].Value != int64(terms.Amount)-500 {
		t.Fatalf("refund shape %+v", tx)
	}
	if len(tx.Inputs[0].Witness) != 3 || string(tx.Inputs[0].Witness[1]) != string(c.RefundLeaf) {
		t.Fatal("refund must spend the refund leaf")
	}
	// The script engine refuses the same signature under an earlier locktime:
	// CLTV binds the refund to the deposit's refund time.
	early := *tx
	early.Locktime = terms.RefundLocktime - 1
	prevouts := []bitcoin.PreviousOutput{{Outpoint: funding, Value: int64(terms.Amount), PublicKeyScript: c.Script()}}
	if err := execute(&early, prevouts); err == nil {
		t.Fatal("refund accepted before its locktime")
	}
}

func TestRefundRefusesWrongInputs(t *testing.T) {
	wallet := xonly(testKey(7))
	refundKey := testKey(9)
	terms := sampleTerms(refundKey, wallet)
	c, err := Build(terms, wallet)
	if err != nil {
		t.Fatal(err)
	}
	funding := bitcoin.TransactionOutpoint{}
	funding.TransactionHash[0] = 1
	destination := append(bitcoin.Script{0x00, 0x14}, make([]byte, 20)...)
	if _, err = Refund(terms, c, funding, destination, 500, testKey(8)); err == nil || !strings.Contains(err.Error(), "refund key") {
		t.Fatal("foreign key accepted:", err)
	}
	other := terms
	other.RefundLocktime++
	if _, err = Refund(other, c, funding, destination, 500, refundKey); err == nil {
		t.Fatal("construction of other terms accepted")
	}
	swapped := c
	swapped.RefundControl = c.WalletControl
	if _, err = Refund(terms, swapped, funding, destination, 500, refundKey); err == nil {
		t.Fatal("wrong control block accepted")
	}
	for _, fee := range []int64{0, int64(terms.Amount)} {
		if _, err = Refund(terms, c, funding, destination, fee, refundKey); err == nil {
			t.Fatalf("fee %d accepted", fee)
		}
	}
}
