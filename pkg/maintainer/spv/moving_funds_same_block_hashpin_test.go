package spv_test

import (
	"encoding/hex"
	"testing"

	"github.com/keep-network/keep-core/pkg/chain/ethereum"
)

// TestMovingFundsSameBlockCommitmentHashBinding pins the constants used by
// TestMovingFundsSameBlockCommitmentHashFixture to the production Ethereum
// binding, TbtcChain.ComputeMovingFundsCommitmentHash, live. Package spv
// cannot import the binding (pkg/chain/ethereum imports pkg/maintainer, which
// imports pkg/maintainer/spv); this external test package can. Together the
// two tests tie the in-package FIXTURE hash copy to the binding.
// Refs tuxxy/keep-core#171.
func TestMovingFundsSameBlockCommitmentHashBinding(t *testing.T) {
	label := func(s string) (out [20]byte) {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		copy(out[:], b)
		return
	}
	// Wallet A's live targets: recorded testnet FIXTURE data, as in
	// moving_funds_same_block_test.go.
	live := [][20]byte{
		label("3091d288521caec06ea912eacfd733edc5a36d6e"),
		label("92a6ec889a8fa34f731e639edede4c75e184307c"),
		label("c7302d75072d78be94eb8d36c4b77583c7abb06e"),
	}
	for name, test := range map[string]struct {
		targets  [][20]byte
		expected string
	}{
		"live targets":  {live, "904c4a6009cec78db41a4f8cb9fc5bddc425661fcd75c74edfcabd004a91ed6b"},
		"single target": {live[:1], "4feeb56181665f59da541b62040e7040b2ce97f093673909fee5dde774e957be"},
	} {
		hash := (&ethereum.TbtcChain{}).ComputeMovingFundsCommitmentHash(test.targets)
		if hex.EncodeToString(hash[:]) != test.expected {
			t.Fatalf("%s: binding computes %x, pinned constant %s", name, hash, test.expected)
		}
	}
}
