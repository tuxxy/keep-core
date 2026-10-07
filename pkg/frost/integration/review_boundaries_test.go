package integration

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

func (f *fixture) signedTestResult() dkg.Result {
	f.t.Helper()
	chain := f.executors[0].Chain
	p, err := chain.Parameters(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	selection, err := chain.Selection(f.ctx, f.deployment.Epoch)
	if err != nil {
		f.t.Fatal(err)
	}
	raw, _ := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	var q [32]byte
	copy(q[:], raw)
	descriptor, err := dkg.NewDescriptor(p, selection, frost.Candidate{Epoch: selection.Epoch, Roster: selection.Roster(), Threshold: p.Threshold, Profile: frost.ApprovedProfile, OutputKey: q, Descriptor: [32]byte{19}})
	if err != nil {
		f.t.Fatal(err)
	}
	result := dkg.NewResult(descriptor)
	digest, err := chain.ResultDigest(f.ctx, selection.Epoch, result)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, operator := range selection.Operators {
		found := false
		for _, node := range f.nodes {
			if crypto.PubkeyToAddress(node.key.PublicKey) == operator {
				sig, err := dkg.Sign(node.key, digest)
				if err != nil {
					f.t.Fatal(err)
				}
				result.Signatures = append(result.Signatures, sig...)
				found = true
				break
			}
		}
		if !found {
			f.t.Fatal("selected signer missing")
		}
	}
	valid, err := chain.Validate(f.ctx, selection.Epoch, result)
	if err != nil || !valid {
		f.t.Fatal("test result is not fully signed and valid", err)
	}
	return result
}

func TestSignedConflictingResultQuarantinesAttempt(t *testing.T) {
	f := setup(t, "1,2,3")
	result := f.signedTestResult()
	if err := f.executors[0].Chain.Submit(f.ctx, result); err != nil {
		t.Fatal(err)
	}
	_, errs := f.run()
	for i, err := range errs {
		if !errors.Is(err, dkg.ErrQuarantined) {
			t.Errorf("operator %d accepted a correctly signed conflicting candidate: %v", i, err)
		}
		if _, err := f.journals[i].LoadKey(f.ctx); !errors.Is(err, store.ErrMissing) {
			t.Errorf("operator %d installed a key: %v", i, err)
		}
		record, err := f.journals[i].LoadWallet(f.ctx)
		if err != nil || !record.Quarantined {
			t.Errorf("operator %d did not retain quarantine: %+v %v", i, record, err)
		}
	}
}

func TestApprovalRequiresChallengeWindow(t *testing.T) {
	f := setup(t, "1,2,3")
	f.stopMining()
	result := f.signedTestResult()
	chain := f.executors[0].Chain
	if err := chain.Submit(f.ctx, result); err != nil {
		t.Fatal(err)
	}
	before, err := chain.View(f.ctx, f.deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	p, err := chain.Parameters(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.State != 3 || before.Head >= before.SubmittedAt+p.ChallengeBlocks {
		t.Fatal("test missed the challenge window")
	}
	data, err := f.registryABI().Pack("approveDkgResult")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.client.CallContract(f.ctx, geth.CallMsg{To: &f.deployment.Registry, Data: data}, nil); err == nil {
		t.Fatal("early approval accepted")
	}
	after, err := chain.View(f.ctx, f.deployment.Epoch)
	if err != nil || after.State != before.State || after.SubmittedHash != before.SubmittedHash || after.ApprovedID != ([32]byte{}) {
		t.Fatal("early approval changed state", err)
	}
	f.mineTo(before.SubmittedAt + p.ChallengeBlocks)
	if err := chain.Approve(f.ctx); err != nil {
		t.Fatal("valid result rejected after challenge window", err)
	}
	after, err = chain.View(f.ctx, f.deployment.Epoch)
	if err != nil || after.ApprovedID == ([32]byte{}) || after.Approved.State != 1 {
		t.Fatal("mature result did not become pending", err)
	}
}

func TestBusRejectsSeatClaimFromAnotherSender(t *testing.T) {
	f := setup(t, "1,2,3")
	sender, receiver := f.executors[0], f.executors[2]
	digest := [32]byte{17}
	sig, err := dkg.Sign(f.executors[1].Signer, digest)
	if err != nil {
		t.Fatal(err)
	}
	forged := dkg.Attestation{Stage: "result", Epoch: f.deployment.Epoch, Seat: 2, Digest: digest, Signature: sig}
	if err := sender.Bus.Send(f.ctx, forged); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	_, err = receiver.Bus.Receive(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("authenticated sender forwarded another operator's seat", err)
	}
	sig, err = dkg.Sign(sender.Signer, digest)
	if err != nil {
		t.Fatal(err)
	}
	valid := forged
	valid.Seat = 1
	valid.Signature = sig
	if err := sender.Bus.Send(f.ctx, valid); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	got, err := receiver.Bus.Receive(ctx)
	if err != nil || got.Seat != 1 || got.Digest != digest {
		t.Fatalf("valid sender control failed: %+v %v", got, err)
	}
}

type localReloadTransport struct{ attempt frost.Attempt }

func (tr localReloadTransport) Binding() (frost.Attempt, [32]byte) {
	return tr.attempt, snowfallengine.AttemptID(tr.attempt)
}
func (localReloadTransport) Broadcast(context.Context, uint16, []byte) error {
	return errors.New("reload broadcast")
}
func (localReloadTransport) SendPrivate(context.Context, uint16, uint16, []byte) error {
	return errors.New("reload private send")
}
func (localReloadTransport) Receive(context.Context) (frost.Incoming, error) {
	return frost.Incoming{}, errors.New("reload receive")
}

func TestReloadRequiresEveryDurableLocalSeat(t *testing.T) {
	f := setup(t, "1,1,2")
	_, errs := f.run()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	e := f.executors[0]
	if len(e.Request.LocalSeats) != 2 {
		t.Fatal("fixture must own two local seats")
	}
	key, err := f.journals[0].LoadKey(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.journals[0].Domain().NewAttempt("sign", [32]byte{91}, f.deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	request := frost.SigningRequest{Group: e.Request.Group, LocalSeats: e.Request.LocalSeats[:1], Selected: e.Request.Participants, Attempt: attempt, Key: key}
	providers := e.Providers
	providers.Transport = localReloadTransport{attempt}
	if err = e.Engine.ReloadKey(f.ctx, request, providers); err == nil {
		t.Fatal("subset of durable seats established readiness")
	}
	// The rejected subset must not consume the claim. The exact same attempt
	// and providers can reload the complete set in a real fresh worker.
	request.LocalSeats = e.Request.LocalSeats
	if err = e.Engine.ReloadKey(f.ctx, request, providers); err != nil {
		t.Fatal("complete local set could not reload", err)
	}
	w, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, dkg.WalletID(key.Candidate.OutputKey))
	if err != nil || w.State != 2 {
		t.Fatal("reload changed certified state", err)
	}
}

func TestRegistryRejectsDuplicateOutputKeyAcrossEpochs(t *testing.T) {
	f := setup(t, "1,2,3")
	f.stopMining()
	approve := func() dkg.View {
		result := f.signedTestResult()
		chain := f.executors[0].Chain
		if err := chain.Submit(f.ctx, result); err != nil {
			t.Fatal(err)
		}
		view, err := chain.View(f.ctx, f.executors[0].Request.Group.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		params, err := chain.Parameters(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		f.mineTo(view.SubmittedAt + params.ChallengeBlocks)
		return view
	}
	approve()
	chain := f.executors[0].Chain
	if err := chain.Approve(f.ctx); err != nil {
		t.Fatal(err)
	}
	old, err := chain.View(f.ctx, f.deployment.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	f.mineTo(old.Approved.Deadline)
	if err := chain.Expire(f.ctx, old.ApprovedID); err != nil {
		t.Fatal(err)
	}
	f.nextEpoch()
	before := approve()
	chain = f.executors[0].Chain
	err = chain.Approve(f.ctx)
	if err == nil || !strings.Contains(err.Error(), "FROST duplicate key") {
		t.Fatal("registry did not reject reused Q at its own admission guard", err)
	}
	after, err := chain.View(f.ctx, f.executors[0].Request.Group.Epoch)
	if err != nil || after.State != 3 || after.SubmittedHash != before.SubmittedHash || after.ApprovedID != ([32]byte{}) {
		t.Fatal("duplicate approval changed the new epoch", err)
	}
	retained, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, old.ApprovedID)
	if err != nil || retained.State != 3 || retained.DescriptorHash != old.Approved.DescriptorHash {
		t.Fatal("duplicate approval changed the old tombstone", err)
	}
}
