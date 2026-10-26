package integration

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

func TestFrostNodeReservationDefersWithoutExit(t *testing.T) {
	w := runPublicLegacyFlow(t, true)
	ctx := w.f.ctx
	beforeFailures := make([]map[string]string, len(w.f.remotes))
	for i, node := range w.f.remotes {
		status, err := node.status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforeFailures[i] = status.FailureStages
	}
	target := w.a
	if w.custody(target.id).Value == 0 {
		target = w.b
	}
	w.wallet, w.walletID = target.desc, target.id
	script := append([]byte{0, 20}, make([]byte, 20)...)
	script[2] = 9
	request := w.request(20_000, script)
	w.mine(int(k06Finality))
	planner := redemption.Planner{Chain: w.chain, Bitcoin: w.btc, Wallet: target.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Fees: redemption.FeePolicy{FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate, Minimum: 1}}
	head := w.head()
	plan, _, err := planner.Build(ctx, head, [][32]byte{request})
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := redemption.CertificateFor(plan, head+100)
	if err != nil {
		t.Fatal(err)
	}
	proposal := redemption.ReservationProposal{Certificate: certificate, Transaction: spv.TransactionInfo(plan.Snapshot().Unsigned())}
	// Explicit certificate-authority adapter using the public local fixture
	// identities. This tests node response to a real admitted reservation;
	// it is not evidence of automatic node attestation or redemption dispatch.
	digest := redemption.ReservationDigest(w.chain.ChainID(), w.chain.Bridge(), certificate)
	for i, member := range target.desc.Members {
		key, err := crypto.ToECDSA(common.LeftPadBytes([]byte{byte(member)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		signature, err := redemption.SignDigest(key, digest)
		if err != nil {
			t.Fatal(err)
		}
		proposal.Seats = append(proposal.Seats, uint16(i+1))
		proposal.Signatures = append(proposal.Signatures, signature...)
	}
	for _, input := range plan.Snapshot().PreviousOutputs() {
		proposal.Inputs = append(proposal.Inputs, redemption.UTXO{TransactionHash: input.Outpoint.TransactionHash, OutputIndex: input.Outpoint.OutputIndex, Value: uint64(input.Value)})
	}
	for _, request := range plan.Requests() {
		proposal.Requests = append(proposal.Requests, redemption.ProposalRequest{ID: request.ID, Value: uint64(request.Value), Script: request.Script})
	}
	if err := w.chain.Reserve(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	w.mine(int(k06Finality))
	reserved := w.custody(target.id)
	if reserved.ActiveReservation == ([32]byte{}) {
		t.Fatal("real reservation not active")
	}
	duty := fmt.Sprintf("incoming/%d", target.desc.Epoch)
	for _, failures := range beforeFailures {
		if failures[duty] == "incoming-reservation" {
			t.Fatal("negative control: reservation reported before admission")
		}
	}
	incoming := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: target.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
	if _, err := incoming.IncomingSweep(ctx); !errors.Is(err, movement.ErrPlanMismatch) {
		t.Fatal("reservation did not reach the reviewed incoming deferral guard", err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		retried := true
		for _, node := range w.f.remotes {
			status, err := node.status(ctx)
			if err != nil {
				t.Fatal("active reservation stopped a node", err)
			}
			retried = retried && status.FailureStages[duty] == "incoming-reservation"
		}
		if retried {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("node did not exercise the reservation deferral")
		}
	}
	// The public legacy helper stops interval mining so proof checks can mine
	// exact finality depths. A new DKG needs its challenge blocks to advance.
	w.f.startMining()
	w.f.nextEpoch()
	_, failures := w.f.run()
	for _, err := range failures {
		if err != nil {
			t.Fatal("reserved wallet blocked a fresh DKG", err)
		}
	}
	if w.custody(target.id) != reserved {
		t.Fatal("unrelated DKG changed the reserved wallet's custody")
	}
	t.Log("K09-RECOVERY: real active reservation deferred; independent DKG completed")
}
