package ethereum

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// walletDescriptor(label) returns abi.encode(FrostWallets.Wallet); the
// decoder must read the nested descriptor tuple and every field after it.
func TestDecodeWalletRecord(t *testing.T) {
	want := walletABI{Descriptor: dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(31337), Registry: common.Address{7}, Epoch: 9, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{5}, OutputKey: [32]byte{6}}, Id: [32]byte{1}, DescriptorHash: [32]byte{2}, Deadline: 77, State: 5, Quarantined: true, Generation: 1, CapabilityVersion: 1}
	args, err := walletArguments()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := args.Pack(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWallet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != want.Id || got.State != 5 || !got.Quarantined || got.Descriptor.OutputKey != want.Descriptor.OutputKey || len(got.Descriptor.Operators) != 3 || got.Descriptor.ChainId.Cmp(want.Descriptor.ChainId) != 0 || got.CapabilityVersion != 1 {
		t.Fatalf("decoded %+v", got)
	}
}
