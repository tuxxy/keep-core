package dkg

import (
	"context"
	"encoding/hex"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost"
	"math/big"
	"testing"
)

func TestUnknownSuiteRejectedBeforePublication(t *testing.T) {
	p := Parameters{ChainID: big.NewInt(31337), Registry: common.Address{1}, Pool: common.Address{2}, Bridge: common.Address{3}, GroupSize: 3, Threshold: 2, ReadySeats: 2, Profile: 1, ChallengeBlocks: 10}
	s := Selection{Epoch: 77, Members: []uint32{1, 1, 2}, Operators: []common.Address{{4}, {4}, {5}}}
	q, err := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	if err != nil {
		t.Fatal(err)
	}
	c := frost.Candidate{Profile: frost.ApprovedProfile, Epoch: 77, Roster: []uint16{1, 2, 3}, Threshold: 2, Descriptor: [32]byte{6}}
	copy(c.OutputKey[:], q)
	d, err := NewDescriptor(p, s, c)
	if err != nil || d.Profile != 1 {
		t.Fatalf("approved profile mapping: %+v %v", d, err)
	}
	for _, profile := range []string{"", frost.ApprovedProfile + " ", "1", "FROST-secp256k1-SHA256-v1"} {
		t.Run("suite="+profile, func(t *testing.T) {
			bad := c
			bad.Profile = profile
			// No chain, bus or journal exists: any publication or storage attempt
			// would panic. Suite rejection must precede all external operations.
			e := Executor{params: p, selection: s}
			if _, err := e.WaitCandidateAcceptance(context.Background(), bad); err == nil {
				t.Fatal("unknown suite reached publication")
			}
		})
	}
}
