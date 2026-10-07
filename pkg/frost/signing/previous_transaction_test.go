package signing

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestPreviousTransactionLengthPreflight(t *testing.T) {
	_, _, tx := planFixture(t)
	for _, witness := range []bool{false, true} {
		if witness {
			tx.Inputs[0].Witness = [][]byte{make([]byte, 64)}
		}
		raw := tx.Serialize()
		got, err := decodePreviousTransaction(raw)
		if err != nil || !bytes.Equal(got.Serialize(), raw) {
			t.Fatal("valid prevout decode", err)
		}
		for n := 0; n < len(raw); n++ {
			if _, err = decodePreviousTransaction(raw[:n]); err == nil {
				t.Fatalf("truncation %d accepted", n)
			}
		}
		if _, err = decodePreviousTransaction(append(raw, 0)); err == nil {
			t.Fatal("trailing bytes accepted")
		}
	}
	for _, text := range []string{"02000000ffffffffffffffffff00000000", "020000000001ffffffffffffffffff00000000", "0200000000020100000000"} {
		raw, _ := hex.DecodeString(text)
		if _, err := decodePreviousTransaction(raw); err == nil {
			t.Fatal("hostile count accepted")
		}
	}
}
