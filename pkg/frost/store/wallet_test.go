package store

import (
	"context"
	"encoding/hex"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

func walletFixture(t *testing.T) dkg.WalletRecord {
	t.Helper()
	domain := testDomain()
	q, _ := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	var output [32]byte
	copy(output[:], q)
	d := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: new(big.Int).SetBytes(domain.Chain[:]), Registry: common.Address(domain.Registry), Epoch: domain.Epoch, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{4}, OutputKey: output}
	hash, e := dkg.DescriptorHash(d)
	must(t, e)
	return dkg.WalletRecord{Descriptor: d, ID: dkg.WalletID(output), DescriptorHash: hash, State: "Candidate"}
}
func TestWalletRecordRetainsIdentityApprovalAndExpiry(t *testing.T) {
	ctx := context.Background()
	config := testConfig(t)
	s, v := openTest(t, config)
	w := walletFixture(t)
	must(t, v.SaveWallet(ctx, w))
	must(t, v.SaveWallet(ctx, w))
	must(t, s.Close())
	_, v = openTest(t, config)
	loaded, e := v.LoadWallet(ctx)
	must(t, e)
	if !reflect.DeepEqual(loaded, w) {
		t.Fatal("wallet did not survive reopen")
	}
	w.ApprovalBlock = 20
	w.ApprovalHash = common.Hash{3}
	w.ResultHash = [32]byte{5}
	w.Deadline = 40
	w.State = "RegisteredPendingReady"
	must(t, v.SaveWallet(ctx, w))
	changed := w
	changed.ApprovalHash[0] ^= 1
	if e = v.SaveWallet(ctx, changed); e == nil {
		t.Fatal("approval receipt replaced")
	}
	changed = w
	changed.State = "ReadyUnfunded"
	if e = v.SaveWallet(ctx, changed); e == nil {
		t.Fatal("readiness saved without a durable key")
	}
	w.State = "Closed"
	must(t, v.SaveWallet(ctx, w))
	changed = w
	changed.State = "RegisteredPendingReady"
	if e = v.SaveWallet(ctx, changed); e == nil {
		t.Fatal("closed identity reopened")
	}
	loaded, e = v.LoadWallet(ctx)
	must(t, e)
	if !reflect.DeepEqual(loaded, w) {
		t.Fatal("failed update changed tombstone")
	}
}
func TestWalletRecordRejectsInvalidDomainAndMutableIdentity(t *testing.T) {
	_, v := openTest(t, testConfig(t))
	ctx := context.Background()
	w := walletFixture(t)
	must(t, v.SaveWallet(ctx, w))
	for _, field := range []string{"scheme", "profile", "chain", "registry", "epoch", "key", "members", "hash"} {
		t.Run(field, func(t *testing.T) {
			changed := walletFixture(t)
			switch field {
			case "scheme":
				changed.Descriptor.Scheme = 0
			case "profile":
				changed.Descriptor.Profile = 2
			case "chain":
				changed.Descriptor.ChainId = big.NewInt(1)
			case "registry":
				changed.Descriptor.Registry[0] ^= 1
			case "epoch":
				changed.Descriptor.Epoch++
			case "key":
				changed.Descriptor.OutputKey[0] ^= 1
			case "members":
				changed.Descriptor.Members[0] = 2
			case "hash":
				changed.DescriptorHash[0] ^= 1
			}
			if e := v.SaveWallet(ctx, changed); e == nil {
				t.Fatal("changed identity admitted")
			}
		})
	}
	w.Quarantined = true
	must(t, v.SaveWallet(ctx, w))
	w.Quarantined = false
	if e := v.SaveWallet(ctx, w); e == nil {
		t.Fatal("quarantine cleared without reconciliation")
	}
}
