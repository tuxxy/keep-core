package node

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"math/big"
	"testing"
)

type approvalChain struct {
	dkg.Chain
	view    dkg.View
	failure error
}

func (c *approvalChain) View(context.Context, uint64) (dkg.View, error) { return c.view, c.failure }
func TestNodeReloadRequiresOriginalApproval(t *testing.T) {
	p := dkg.Parameters{ChainID: big.NewInt(100123), Registry: common.Address{1}, Pool: common.Address{2}, Bridge: common.Address{3}, GroupSize: 3, Threshold: 2, ReadySeats: 3, Profile: 1, ChallengeBlocks: 2}
	s := dkg.Selection{Epoch: 9, Members: []uint32{1, 1, 2}, Operators: []common.Address{{1}, {1}, {2}}}
	keyBytes := make([]byte, 32)
	keyBytes[31] = 1
	key, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	var q [32]byte
	key.X.FillBytes(q[:])
	candidate := frost.Candidate{Profile: frost.ApprovedProfile, Roster: []uint16{1, 2, 3}, Threshold: 2, Epoch: 9, Descriptor: [32]byte{1}, OutputKey: q}
	descriptor, err := dkg.NewDescriptor(p, s, candidate)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := dkg.DescriptorHash(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	record := dkg.WalletRecord{Descriptor: descriptor, DescriptorHash: hash, ID: dkg.WalletID(q), ApprovalHash: common.Hash{3}, ApprovalBlock: 10, ResultHash: [32]byte{4}}
	view := dkg.View{ApprovedID: record.ID, ApprovalHash: record.ApprovalHash, Approved: dkg.Wallet{State: 2, Descriptor: descriptor, DescriptorHash: hash, ResultHash: record.ResultHash, ApprovalBlock: record.ApprovalBlock}}
	chain := &approvalChain{view: view}
	acceptance := readyAcceptance{chain: chain, parameters: p, selection: s, record: record}
	if receipt, err := acceptance.WaitCandidateAcceptance(context.Background(), candidate); err != nil || receipt.Descriptor != candidate.Descriptor {
		t.Fatal("positive certified reload refused", err)
	}
	changed := candidate
	changed.Descriptor[0] ^= 2 // Stay nonzero: reach binding guard, not candidate-shape refusal.
	if _, err := acceptance.WaitCandidateAcceptance(context.Background(), changed); err == nil || err.Error() != "reloaded candidate differs from durable approval" {
		t.Fatal("control NodeReloadCandidate assertion: changed candidate admitted")
	}
	cases := []struct {
		name   string
		change func(*dkg.View)
	}{
		{"approval fork", func(v *dkg.View) { v.ApprovalHash[0] ^= 1 }},
		{"identity", func(v *dkg.View) { v.ApprovedID[0] ^= 1 }},
		{"unready", func(v *dkg.View) { v.Approved.State = 1 }},
		{"descriptor", func(v *dkg.View) { v.Approved.DescriptorHash[0] ^= 1 }},
		{"result", func(v *dkg.View) { v.Approved.ResultHash[0] ^= 1 }},
		{"approval block", func(v *dkg.View) { v.Approved.ApprovalBlock++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain.view = view
			tc.change(&chain.view)
			if _, err := acceptance.WaitCandidateAcceptance(context.Background(), candidate); err == nil || err.Error() != "reloaded candidate approval is no longer canonical and ready" {
				t.Fatal("control NodeReloadApproval assertion: changed approval admitted")
			}
		})
	}
	chain.view = view
	chain.failure = errors.New("controlled RPC outage")
	if _, err := acceptance.WaitCandidateAcceptance(context.Background(), candidate); !errors.Is(err, chain.failure) {
		t.Fatal("dependency failure did not refuse reload")
	}
}
func TestNodeLocalGatePrecedesCredentials(t *testing.T) {
	cases := []Config{{}, {LocalRegtest: true, EthereumURL: "https://example.invalid"}, {LocalRegtest: true, EthereumURL: "http://127.0.0.1:1", BitcoinURL: "http://127.0.0.1:2", FenceURL: "http://127.0.0.1:3", Registry: common.Address{1}, Bridge: common.Address{2}, FinalityBlocks: 2, RoundBlocks: 10, FeeRate: 2, MaximumSatPerVByte: 10, ProtocolSeconds: 901}}
	for _, cfg := range cases {
		if _, err := New(context.Background(), cfg); err == nil || err.Error() != "explicit bounded local FROST configuration required" {
			t.Fatalf("control NodeLocalGate assertion: wrong gate before credentials: %v", err)
		}
	}
}

func TestNodeBootstrapGatePrecedesCredentials(t *testing.T) {
	c := Config{LocalRegtest: true, EthereumURL: "http://127.0.0.1:1", BitcoinURL: "http://127.0.0.1:2", FenceURL: "http://127.0.0.1:3", Registry: common.Address{1}, Bridge: common.Address{2}, FinalityBlocks: 2, RoundBlocks: 10, FeeRate: 2, MaximumSatPerVByte: 10, ProtocolSeconds: 120, Peers: []string{"/dns4/example.invalid/tcp/1/p2p-circuit/ip4/127.0.0.1/tcp/2"}}
	if _, err := New(context.Background(), c); err == nil || err.Error() != "local FROST bootstrap required" {
		t.Fatal("control NodeBootstrapGate assertion: wrong gate before credentials", err)
	}
}
