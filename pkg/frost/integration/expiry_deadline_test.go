package integration

// K-03 review test gap R10 (review-2026-10-07-K03 REVIEW.md F5): expiry at
// exactly the readiness deadline was never exercised.
// Refs tuxxy/keep-core#74.

import (
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// sendReceipt submits a local transaction with a fixed gas limit (no estimate,
// so a revert is mined) and returns its receipt.
func (f *fixture) sendReceipt(to common.Address, data []byte) *types.Receipt {
	f.t.Helper()
	var accounts []common.Address
	if err := f.rpc.CallContext(f.ctx, &accounts, "eth_accounts"); err != nil {
		f.t.Fatal(err)
	}
	var hash common.Hash
	if err := f.rpc.CallContext(f.ctx, &hash, "eth_sendTransaction", map[string]interface{}{"from": accounts[0], "to": to, "data": "0x" + common.Bytes2Hex(data), "gas": "0x989680"}); err != nil {
		f.t.Fatal(err)
	}
	receipt, err := f.client.TransactionReceipt(f.ctx, hash)
	for errors.Is(err, geth.NotFound) {
		select {
		case <-f.ctx.Done():
			f.t.Fatal("transaction receipt deadline")
		case <-time.After(10 * time.Millisecond):
		}
		receipt, err = f.client.TransactionReceipt(f.ctx, hash)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return receipt
}

// Invariant (KC-SF-V3 4.2: on the readiness deadline RegisteredPendingReady
// transitions to Closed; certificate submission at or after the deadline is
// refused, so expiry and certification partition the blocks exactly): with
// real nodes holding a valid certificate, public expiry mined one block
// before the deadline is refused and leaves the wallet PendingReady, and
// expiry mined in exactly the deadline block closes it. The nodes then
// classify the outcome as expiry and retain a Closed record.
func TestFrostExpiryAtExactDeadline(t *testing.T) {
	t.Parallel()
	f := setup(t, "1,2,3")
	pending, release := f.gateReadiness()
	done := f.start()
	for range f.executors {
		f.certificate(pending)
	}
	w := f.pending()
	id := dkg.WalletID(w.Descriptor.OutputKey)
	f.stopMining()
	data, err := f.registryABI().Pack("expireWallet", id)
	if err != nil {
		t.Fatal(err)
	}
	// Hold node views while the test places each transaction in an exact block.
	f.chainMu.Lock()
	head, err := f.client.BlockNumber(f.ctx)
	if err != nil {
		f.chainMu.Unlock()
		t.Fatal(err)
	}
	if head+2 > w.Deadline {
		f.chainMu.Unlock()
		t.Fatalf("harness: head %d leaves no block before deadline %d", head, w.Deadline)
	}
	if head+2 < w.Deadline {
		var out interface{}
		if err = f.rpc.CallContext(f.ctx, &out, "anvil_mine", fmt.Sprintf("0x%x", w.Deadline-2-head)); err != nil {
			f.chainMu.Unlock()
			t.Fatal(err)
		}
	}
	early := f.sendReceipt(f.deployment.Registry, data)
	before, readErr := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
	late := f.sendReceipt(f.deployment.Registry, data)
	after, lateErr := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
	f.chainMu.Unlock()
	if early.BlockNumber.Cmp(new(big.Int).SetUint64(w.Deadline-1)) != 0 || late.BlockNumber.Cmp(new(big.Int).SetUint64(w.Deadline)) != 0 {
		t.Fatalf("harness: expiry transactions mined in blocks %v and %v, want %d and %d", early.BlockNumber, late.BlockNumber, w.Deadline-1, w.Deadline)
	}
	if early.Status != types.ReceiptStatusFailed || readErr != nil || before.State != 1 {
		t.Fatalf("R10: expiry one block before the deadline was not refused: status=%d state=%d error=%v", early.Status, before.State, readErr)
	}
	if late.Status != types.ReceiptStatusSuccessful || lateErr != nil || after.State != 3 {
		t.Fatalf("R10: expiry in exactly the deadline block did not close the pending wallet: status=%d state=%d error=%v", late.Status, after.State, lateErr)
	}
	close(release)
	r := f.await(done)
	for i, err := range r.errs {
		if !errors.Is(err, dkg.ErrExpired) {
			t.Fatalf("operator %d: expected classified expiry, got %v", i, err)
		}
		saved, loadErr := f.journals[i].LoadWallet(f.ctx)
		if loadErr != nil || saved.State != "Closed" || saved.Quarantined {
			t.Fatalf("operator %d: expired wallet record %+v, load error %v", i, saved, loadErr)
		}
	}
}
