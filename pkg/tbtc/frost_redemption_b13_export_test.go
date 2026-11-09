package tbtc

// Batch-13 test exports (tuxxy/keep-core#142, FSM analysis R-1). They let the
// external batch-13 tests run a FROST member's own handling of a redemption
// proposal (frostWallet.redeem) against the real upgraded Bridge, and read
// the member's deadline margin. They add no behaviour.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ipfs/go-log/v2"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// B13FrostRedemptionDeadlineMargin is the minimum number of seconds a FROST
// member requires before a request's deadline to plan it.
const B13FrostRedemptionDeadlineMargin = uint64(frostRedemptionDeadlineMargin)

// errB13NotRetained is returned when the member returned without reaching
// the plan retention: neither an acceptance nor a refusal the tests know.
var errB13NotRetained = errors.New("b13: the member returned without retaining a plan")

// b13Fence is a TEST in-memory fencing authority for the member's store.
type b13Fence struct {
	mu   sync.Mutex
	head store.Point
}
type b13Lease struct{ f *b13Fence }

func (f *b13Fence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	return b13Lease{f}, nil
}
func (l b13Lease) Head(context.Context) (store.Point, error) {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	return l.f.head, nil
}
func (l b13Lease) Advance(_ context.Context, before, after store.Point) error {
	l.f.mu.Lock()
	defer l.f.mu.Unlock()
	if l.f.head != before {
		return store.ErrQuarantined
	}
	l.f.head = after
	return nil
}
func (l b13Lease) Close() error { return nil }

// B13FollowerRedeem runs a FROST member's handling of the window's
// redemption proposal, frostWallet.redeem, with the member's real Bridge
// binding, Bitcoin view, wallet descriptor and the window's coordination
// block, up to its first write: retaining the validated plan before it
// attests the reservation.
//
// FIXTURE: the member's FROST store is a real store opened in dir and closed
// before the call, so the retention fails (store.ErrQuarantined) and nothing
// is attested, sent or signed. The member has no signer, seats or network.
//
// It returns nil when the member accepted the proposal (it built its plan
// from chain state, found it to pay exactly the proposed requests with the
// proposed fee, and reached the retention), and the member's refusal
// otherwise.
func B13FollowerRedeem(
	ctx context.Context,
	dir string,
	bridge FrostBridgeChain,
	btc frostBitcoinChain,
	descriptor dkg.Descriptor,
	genesis [32]byte,
	proposal *RedemptionProposal,
	coordinationBlock uint64,
) error {
	root, err := store.Open(ctx, store.Config{
		Keystore:  filepath.Join(dir, "ks"),
		StorageID: [32]byte{0xb1, 0x3},
		Key:       [32]byte{0xb1, 0x3, 1},
		Fence:     &b13Fence{},
	})
	if err != nil {
		return fmt.Errorf("b13: member store: %w", err)
	}
	fn := &frostNode{
		config:  FrostConfig{Registry: [20]byte{0xb1, 0x3}},
		bridge:  bridge,
		bitcoin: btc,
		store:   root,
		network: "b13",
		chainID: [32]byte{31: 1},
		genesis: genesis,
		scopes:  map[uint64]*store.Scoped{},
		epochs:  map[uint64]bool{},
		wallets: map[[32]byte]*frostWallet{},
	}
	journal, err := root.Scope(fn.domain(descriptor.Epoch))
	if err != nil {
		return fmt.Errorf("b13: member store scope: %w", err)
	}
	if err := root.Close(); err != nil {
		return fmt.Errorf("b13: member store close: %w", err)
	}
	fw := &frostWallet{
		node:       fn,
		id:         dkg.WalletID(descriptor.OutputKey),
		epoch:      descriptor.Epoch,
		descriptor: descriptor,
		journal:    journal,
	}
	copy(fw.label[:], fw.id[:20])

	err = fw.redeem(ctx, log.Logger("b13-follower"), proposal, coordinationBlock)
	switch {
	case errors.Is(err, store.ErrQuarantined):
		return nil
	case err == nil:
		return errB13NotRetained
	default:
		return err
	}
}
