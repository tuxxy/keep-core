package tbtc

// Batch-7 regression tests for the ADV-startup findings S1 and S2
// (K-09-restart/ADV-startup.md), from the reviewer's probes. The registry
// below is a labelled TEST adapter, not a chain.

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// advFence is a TEST in-memory fencing authority.
type advFence struct {
	mu   sync.Mutex
	head store.Point
}
type advLease struct{ f *advFence }

func (f *advFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	return advLease{f}, nil
}
func (l advLease) Head(context.Context) (store.Point, error) {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	return l.f.head, nil
}
func (l advLease) Advance(_ context.Context, before, after store.Point) error {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	if l.f.head != before {
		return store.ErrQuarantined
	}
	l.f.head = after
	return nil
}
func (l advLease) Close() error { return nil }

// advRegistry is a TEST adapter of the FROST registry. Only the reads the
// startup and DKG-join paths make are implemented; anything else panics.
type advRegistry struct {
	FrostRegistryChain
	mu         sync.Mutex
	selection  dkg.Selection
	selections int
	parameters int
}

func (r *advRegistry) Selection(_ context.Context, epoch uint64) (dkg.Selection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections++
	s := r.selection
	s.Epoch = epoch
	return s, nil
}
func (r *advRegistry) Parameters(context.Context) (dkg.Parameters, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parameters++
	// executeDKG was reached; stop it here.
	return dkg.Parameters{}, errors.New("probe: executeDKG reached")
}
func (r *advRegistry) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selections, r.parameters
}

func advNode(t *testing.T, registry *advRegistry, signer *ecdsa.PrivateKey) *frostNode {
	root, err := store.Open(context.Background(), store.Config{
		Keystore:  filepath.Join(t.TempDir(), "ks"),
		StorageID: [32]byte{1},
		Key:       [32]byte{2},
		Fence:     &advFence{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &frostNode{
		config:   FrostConfig{Registry: [20]byte{9}},
		registry: registry,
		store:    root,
		signer:   signer,
		self:     crypto.PubkeyToAddress(signer.PublicKey),
		network:  "probe",
		chainID:  [32]byte{31: 1},
		scopes:   map[uint64]*store.Scoped{},
		epochs:   map[uint64]bool{},
		wallets:  map[[32]byte]*frostWallet{},
	}
}

func advEventually(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestFrostNodeB7Startup_RestartDuringAwaitingSeedJoinsDKG: a node that starts while
// the current FROST epoch awaits its beacon seed (selection still empty) must
// join that epoch's DKG once the seed selects its seats. The FROST DKG needs
// every selected seat (Quorum = GroupSize), so a node that never joins fails
// the whole wallet's DKG.
func TestFrostNodeB7Startup_RestartDuringAwaitingSeedJoinsDKG(t *testing.T) {
	const epoch = 100
	signer, _ := crypto.GenerateKey()
	other, _ := crypto.GenerateKey()

	for _, tc := range []struct {
		name    string
		startup bool // node (re)started during AwaitingSeed: newFrostNode ran restoreEpoch
	}{
		{"control: node running before the epoch", false},
		{"node restarted during AwaitingSeed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := &advRegistry{}
			fn := advNode(t, registry, signer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if tc.startup {
				// newFrostNode: the current epoch (AwaitingSeed) is restored.
				fn.restoreEpoch(ctx, epoch)
			} else {
				// DKG loop block before the seed.
				fn.joinDKGIfEligible(ctx, epoch)
			}

			// The beacon seed arrives: the selection holds a local seat.
			registry.mu.Lock()
			registry.selection = dkg.Selection{
				Members:   []uint32{1, 2},
				Operators: []common.Address{fn.self, crypto.PubkeyToAddress(other.PublicKey)},
			}
			registry.mu.Unlock()

			// Next blocks of the DKG loop.
			fn.joinDKGIfEligible(ctx, epoch)
			fn.joinDKGIfEligible(ctx, epoch)

			if !advEventually(func() bool { _, p := registry.counts(); return p > 0 }) {
				s, _ := registry.counts()
				t.Errorf("ADV-S1: the node never joined the DKG of epoch %d after its seed selected local seat 1 (selection reads %d; executeDKG not reached)", epoch, s)
			}
		})
	}
}

// advCertifiedRegistry adds the reads restoreEpoch makes for a certified
// wallet. Every failOn-th selection read fails (a flaky RPC): with failOn 2,
// the read inside restore fails each time, so a retried restore stops there.
type advCertifiedRegistry struct {
	advRegistry
	failOn int
}

func (r *advCertifiedRegistry) Selection(ctx context.Context, epoch uint64) (dkg.Selection, error) {
	r.mu.Lock()
	n := r.selections + 1
	r.mu.Unlock()
	if r.failOn > 0 && n%r.failOn == 0 {
		r.mu.Lock()
		r.selections++
		r.mu.Unlock()
		return dkg.Selection{}, errors.New("probe: transient RPC error")
	}
	return r.advRegistry.Selection(ctx, epoch)
}
func (r *advCertifiedRegistry) ApprovedWallet(context.Context, uint64) ([32]byte, error) {
	return [32]byte{7}, nil
}
func (r *advCertifiedRegistry) Wallet(context.Context, [32]byte) (dkg.Wallet, error) {
	return dkg.Wallet{State: frostWalletReadyUnfunded}, nil
}

// TestFrostNodeB7Startup_CertifiedWalletRestoreSurvivesOneRPCError: base loads every
// stored signer from disk at startup, so no RPC error can drop one. Here one
// transient read error during the startup restore of a certified wallet must
// not leave the operator's seats out of coordination for the process life:
// the restore has to be attempted again.
func TestFrostNodeB7Startup_CertifiedWalletRestoreSurvivesOneRPCError(t *testing.T) {
	const epoch = 100
	signer, _ := crypto.GenerateKey()
	other, _ := crypto.GenerateKey()
	registry := &advCertifiedRegistry{failOn: 2}
	fn := advNode(t, &registry.advRegistry, signer)
	fn.registry = registry
	registry.selection = dkg.Selection{
		Members:   []uint32{1, 2},
		Operators: []common.Address{fn.self, crypto.PubkeyToAddress(other.PublicKey)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A completed key, as Run leaves it (claim, SaveKey, release).
	journal, err := fn.scope(epoch)
	if err != nil {
		t.Fatal(err)
	}
	domain := journal.Domain()
	attempt, err := domain.NewAttempt("dkg", domain.ID(), epoch)
	if err != nil {
		t.Fatal(err)
	}
	request := frost.DKGRequest{Group: frost.Group{Roster: []uint16{1, 2}, Threshold: 2, Quorum: 2, Epoch: epoch}, LocalSeats: []uint16{1}, Participants: []uint16{1, 2}, Attempt: attempt}
	intent, _ := json.Marshal(request)
	release, err := journal.Claim(ctx, [32]byte{5}, "dkg", intent)
	if err != nil {
		t.Fatal(err)
	}
	key := frost.KeyReady{Candidate: frost.Candidate{Epoch: epoch, Roster: []uint16{1, 2}, Threshold: 2, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{{1}}}
	if err = journal.SaveKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	release()
	if status, err := journal.DKGStatus(ctx); err != nil || status.State != frost.DKGKeyStored {
		t.Fatalf("precondition: key not stored: %v %v", status.State, err)
	}

	// newFrostNode's startup restore; the read inside restore fails once.
	fn.restoreEpoch(ctx, epoch)
	// Later blocks of the DKG loop for the same (current) epoch.
	for i := 0; i < 3; i++ {
		fn.joinDKGIfEligible(ctx, epoch)
	}
	if !advEventually(func() bool { s, _ := registry.counts(); return s > 2 }) {
		s, _ := registry.counts()
		t.Errorf("ADV-S2: after one transient read error the certified wallet's restore was never attempted again (selection reads %d); its seats stay out of coordination until the process restarts", s)
	}
	// A retried restore never reruns the DKG of an epoch with a stored key.
	if _, p := registry.counts(); p != 0 {
		t.Errorf("ADV-S2: a retried restore reran the DKG (executeDKG reached %d times)", p)
	}
}
