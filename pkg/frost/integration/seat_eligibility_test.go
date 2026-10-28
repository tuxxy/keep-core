package integration

// Seat eligibility for host attestations (deferral item A-READY-SEAT-ELIGIBILITY).
// Refs tuxxy/keep-core#86 (A-READY-SEAT-ELIGIBILITY).

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

// certifySeats holds every executor's readiness certificate and submits the
// one whose seat set is exactly want, through the real registry, before any
// executor submits its own. The executors then observe the certified wallet.
// Errors are returned on the channel; the test checks it after the DKG.
func certifySeats(f *fixture, want []uint16) <-chan error {
	pending, release := f.gateReadiness()
	result := make(chan error, 1)
	go func() {
		defer close(release)
		for range f.executors {
			select {
			case c := <-pending:
				if !slices.Equal(c.seats, want) {
					continue
				}
				data, err := f.registryABI().Pack("submitReadinessV1", c.id, c.seats, c.references, c.signatures)
				if err != nil {
					result <- err
					return
				}
				f.send(f.deployment.Registry, data)
				result <- nil
				return
			case <-f.ctx.Done():
				result <- f.ctx.Err()
				return
			}
		}
		result <- errors.New("no executor produced a certificate with the wanted seat set")
	}()
	return result
}

// Invariant (KC-SF-V3 4.2 and C-03; COMPOSITION A-READY-SEAT-ELIGIBILITY:
// "honest hosts attest only with seats in the frozen readiness-certified set
// and not reported lost; the certificate validator does not enforce these
// sets"): an honest host whose seat completed the DKG, holds a durable,
// reloadable key and saved the wallet as ReadyUnfunded, but whose seat is not
// in the readiness certificate the registry accepted, refuses to attest a C-04
// reservation certificate with that seat. Hosts whose seats are certified
// attest as before, and their signatures alone admit the reservation.
//
// Real: registry, Bridge, Bank and verifier (K-05 deployment), Bitcoin Core
// regtest, the pinned worker, real DKG over libp2p, the encrypted stores,
// FreshWorkerReload and the real Ethereum redemption adapter. Pattern 1,1,2:
// operator 1 holds seats 1 and 2, operator 2 holds seat 3. The test submits
// operator 1's own certificate (seats 1 and 2, the deployed 2-of-3 policy),
// so seat 3 stays uncertified while operator 2 is otherwise ready.
func TestUncertifiedSeatDoesNotAttest(t *testing.T) {
	t.Parallel()
	certified := []uint16{1, 2}
	var submitted <-chan error
	w := newK05WorldWith(t, func(f *fixture) { submitted = certifySeats(f, certified) })
	if err := <-submitted; err != nil {
		t.Fatal("harness: readiness certificate:", err)
	}
	ctx := w.f.ctx
	registry := w.f.registry
	events, err := registry.FilterReadinessAccepted(&bind.FilterOpts{Context: ctx, Start: 0}, [][32]byte{w.walletID})
	if err != nil {
		t.Fatal(err)
	}
	var accepted [][]uint16
	for events.Next() {
		accepted = append(accepted, events.Event.Seats)
	}
	if len(accepted) != 1 || !slices.Equal(accepted[0], certified) {
		t.Fatalf("harness: certified seat sets %v, want exactly %v", accepted, certified)
	}
	if wallet, err := registry.Wallet(&bind.CallOpts{Context: ctx}, w.walletID); err != nil || wallet.State != 2 {
		t.Fatal("harness: wallet not certified", err)
	}
	// Operator 2 is honest and otherwise ready: its own seat 3 completed,
	// its key is stored and its record is ReadyUnfunded.
	uncertified := w.attesters[1]
	if !slices.Equal(uncertified.Seats, []uint16{3}) {
		t.Fatalf("harness: operator 2 seats %v", uncertified.Seats)
	}
	saved, err := w.f.journals[1].LoadWallet(ctx)
	status, statusErr := w.f.journals[1].DKGStatus(ctx)
	if err != nil || saved.State != "ReadyUnfunded" || saved.Quarantined || saved.ID != w.walletID || statusErr != nil || status.State != frost.DKGKeyStored {
		t.Fatalf("harness: operator 2 is not locally ready: state=%q quarantined=%t status=%+v errors=%v %v", saved.State, saved.Quarantined, status, err, statusErr)
	}

	const gross = 100_000
	w.request(gross, p2wpkh(0x41))
	head := w.head()
	plan, _, err := w.proposal(head)
	if err != nil {
		t.Fatal(err)
	}
	sigs, err := uncertified.Attest(ctx, plan, head+100)
	if err == nil || len(sigs) != 0 {
		t.Errorf("#86: an honest host attested with seat 3, which is not in the readiness-certified set %v: signed seats %v, error %v", certified, slices.Sorted(maps.Keys(sigs)), err)
	}

	// Positive control: the certified seats attest, and their signatures
	// alone admit the reservation on the real Bridge.
	ready, err := w.attesters[0].Attest(ctx, plan, head+100)
	if err != nil || len(ready) != 2 || ready[1] == nil || ready[2] == nil {
		t.Fatalf("#86 positive control: the certified seats did not attest: %v", err)
	}
	seats, signatures, err := redemption.Collect(w.wallet.Threshold, ready)
	if err != nil || !slices.Equal(seats, certified) {
		t.Fatalf("#86 positive control: certified signatures %v do not meet the threshold: %v", seats, err)
	}
	certificate, err := redemption.CertificateFor(plan, head+100)
	if err != nil {
		t.Fatal(err)
	}
	proposal := redemption.ReservationProposal{Certificate: certificate, Transaction: spv.TransactionInfo(plan.Snapshot().Unsigned()), Seats: seats, Signatures: signatures}
	for _, p := range plan.Snapshot().PreviousOutputs() {
		proposal.Inputs = append(proposal.Inputs, redemption.UTXO{TransactionHash: p.Outpoint.TransactionHash, OutputIndex: p.Outpoint.OutputIndex, Value: uint64(p.Value)})
	}
	for _, r := range plan.Requests() {
		proposal.Requests = append(proposal.Requests, redemption.ProposalRequest{ID: r.ID, Value: uint64(r.Value), Script: r.Script})
	}
	if err = w.chain.Reserve(ctx, proposal); err != nil {
		t.Fatalf("#86 positive control: the Bridge refused a reservation signed by the certified seats: %v", err)
	}
}
