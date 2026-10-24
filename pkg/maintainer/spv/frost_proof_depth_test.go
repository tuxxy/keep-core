package spv

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

func TestFrostDifficultyProofDepth(t *testing.T) {
	cases := []struct {
		name                     string
		current, previous        int64
		at                       func(uint) *big.Int
		available, minimum, want uint
	}{
		{"current epoch", 32, 16, func(uint) *big.Int { return big.NewInt(32) }, 12, 1, 6},
		{"retarget drops", 16, 32, func(h uint) *big.Int {
			if h < 1002 {
				return big.NewInt(32)
			}
			return big.NewInt(16)
		}, 12, 1, 10},
		{"retarget rises", 32, 16, func(h uint) *big.Int {
			if h < 1002 {
				return big.NewInt(16)
			}
			return big.NewInt(32)
		}, 12, 1, 4},
		{"minimum difficulty prefix", 32, 16, func(h uint) *big.Int {
			if h < 1002 {
				return big.NewInt(1)
			}
			return big.NewInt(32)
		}, 12, 1, 8},
		{"caller floor", 32, 16, func(uint) *big.Int { return big.NewInt(32) }, 12, 9, 9},
		{"insufficient work", 32, 16, func(uint) *big.Int { return big.NewInt(32) }, 5, 1, 0},
		{"outside relay", 32, 16, func(uint) *big.Int { return big.NewInt(8) }, 12, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			btc := newLocalBitcoinChain()
			settings := newLocalChain()
			if err := populateBlockHeaders(btc, 1000, 1000+tc.available-1, tc.at); err != nil {
				t.Fatal(err)
			}
			tx := &bitcoin.Transaction{Version: 2}
			hash := tx.Hash()
			btc.addTransactionConfirmations(hash, tc.available)
			settings.setTxProofDifficultyFactor(big.NewInt(6))
			settings.setCurrentAndPrevEpochDifficulty(big.NewInt(tc.current), big.NewInt(tc.previous))
			derive := DifficultyProofDepth(btc, settings, settings, tc.minimum)
			depth, err := derive(context.Background(), hash)
			if tc.want == 0 {
				if !errors.Is(err, ErrFrostProofNotReady) {
					t.Fatal("invalid work admitted", depth, err)
				}
				return
			}
			if err != nil || depth != tc.want {
				t.Fatal("wrong accumulated-work depth", depth, err)
			}
			// Exercise the actual FROST redemption proof producer with the derived
			// depth. Assembly is a test adapter here; no signature/SPV theorem is claimed.
			called := false
			maintainer := FrostRedemptionMaintainer{Bitcoin: btc, Confirmations: 1, ProofDepth: derive,
				assemble: func(got bitcoin.Hash, n uint, _ bitcoin.Chain) (*bitcoin.Transaction, *bitcoin.SpvProof, error) {
					if got != hash || n != tc.want {
						t.Fatal("producer bypassed derived proof depth", n)
					}
					called = true
					return tx, &bitcoin.SpvProof{}, nil
				},
			}
			if !maintainer.confirmed([32]byte(hash)) {
				t.Fatal("valid work was rejected")
			}
			if _, _, err := maintainer.proof([32]byte(hash)); err != nil || !called {
				t.Fatal("producer did not assemble derived proof", err)
			}
		})
	}
}

func TestFrostProofDepthRejectsZeroCallback(t *testing.T) {
	_, err := frostProofDepth(context.Background(), bitcoin.Hash{}, 1, func(context.Context, bitcoin.Hash) (uint, error) { return 0, nil })
	if !errors.Is(err, ErrFrostProofNotReady) {
		t.Fatal("zero callback admitted", err)
	}
}
