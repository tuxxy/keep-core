package integration

// The restart signature witness for #46 (batch 4, Astra review B1): after
// startup, the recovered host takes part in a real FROST signature with the
// readiness-certified seats, so a startup that only updates metadata cannot
// stand in for a recovered signer. Refs tuxxy/keep-core#46.
//
// The signing request is test-only authorization, as K-05's signDigest: a
// fixed public digest, no Bitcoin transaction. Only the 64-byte signature and
// the public seat set leave the host.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/frost/transport"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// witnessDigest is the public message of the restart signature witness.
var witnessDigest = sha256.Sum256([]byte("keep-core/frost/b4-restart-witness/v1"))

// acceptedSeats returns the seat set of the registry's ReadinessAccepted event
// for wallet id; exactly one event is required.
func acceptedSeats(ctx context.Context, registry *binding.FrostWalletRegistry, id [32]byte) ([]uint16, error) {
	events, err := registry.FilterReadinessAccepted(&bind.FilterOpts{Context: ctx, Start: 0}, [][32]byte{id})
	if err != nil {
		return nil, err
	}
	var accepted [][]uint16
	for events.Next() {
		accepted = append(accepted, events.Event.Seats)
	}
	if len(accepted) != 1 {
		return nil, fmt.Errorf("%d readiness certificates accepted", len(accepted))
	}
	return accepted[0], nil
}

// signWitness runs one operator's side of the witness signature with selected
// seats, on its real store and worker, over its authenticated network.
func signWitness(ctx context.Context, worker snowfallengine.WorkerConfig, journal *store.Scoped, private *operator.PrivateKey, network net.Provider, membership *group.MembershipValidator, roster, selected []uint16) ([64]byte, error) {
	engine, err := snowfallengine.New(worker)
	if err != nil {
		return [64]byte{}, err
	}
	key, err := journal.LoadKey(ctx)
	if err != nil {
		return [64]byte{}, err
	}
	status, err := journal.DKGStatus(ctx)
	if err != nil {
		return [64]byte{}, err
	}
	attempt, err := journal.Domain().NewAttempt("sign", witnessDigest, 1)
	if err != nil {
		return [64]byte{}, err
	}
	tr, err := transport.New(ctx, transport.Config{Domain: journal.Domain(), Attempt: attempt, ID: snowfallengine.AttemptID(attempt), LocalSeats: status.LocalSeats, Roster: roster, LocalPublicKey: operator.MarshalUncompressed(&private.PublicKey), Membership: membership, Network: network})
	if err != nil {
		return [64]byte{}, err
	}
	defer tr.Close()
	c := key.Candidate
	return engine.Sign(ctx, frost.SigningRequest{Group: frost.Group{Roster: c.Roster, Threshold: c.Threshold, Quorum: uint16(len(c.Roster)), Epoch: c.Epoch}, LocalSeats: status.LocalSeats, Selected: selected, Attempt: attempt, Key: key, Message: witnessDigest}, frost.Providers{Transport: tr, Store: journal, Acceptance: noAcceptance{}})
}

// hostWitness is the recovered host's side: it reads the certified seat set
// from the registry, announces it to the parent and signs with it.
func hostWitness(ctx context.Context, registry *binding.FrostWalletRegistry, journal *store.Scoped, worker snowfallengine.WorkerConfig, private *operator.PrivateKey, network net.Provider, membership *group.MembershipValidator, roster []uint16) ([64]byte, []uint16, error) {
	wallet, err := journal.LoadWallet(ctx)
	if err != nil {
		return [64]byte{}, nil, err
	}
	selected, err := acceptedSeats(ctx, registry, wallet.ID)
	if err != nil {
		return [64]byte{}, nil, err
	}
	status, err := journal.DKGStatus(ctx)
	if err != nil {
		return [64]byte{}, selected, err
	}
	if !slices.ContainsFunc(status.LocalSeats, func(s uint16) bool { return slices.Contains(selected, s) }) {
		return [64]byte{}, selected, errors.New("no local seat is certified")
	}
	fmt.Fprintf(os.Stderr, "B4_HOST_SIGNING:%v\n", selected)
	signature, err := signWitness(ctx, worker, journal, private, network, membership, roster, selected)
	return signature, selected, err
}
