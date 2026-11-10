package dkg

// Mutation batch M10-dkg: the host attestation channel (network.go
// NewNetworkBus).
//
// Real code: NewNetworkBus and its receive filter, with the real
// group.MembershipValidator over local_v1 signing.
//
// FIXTURES (labelled, test-only): capProvider/capChannel stand in for the
// libp2p provider. The channel keeps the bus's receive handler, and the test
// calls it directly with capMessage values, so delivery is synchronous and
// ordered. Operator keys are fresh local_v1 test keys.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

type capProvider struct {
	net.Provider
	channel *capChannel
}

func (p capProvider) BroadcastChannelFor(string) (net.BroadcastChannel, error) {
	return p.channel, nil
}

type capChannel struct {
	net.BroadcastChannel
	mu      sync.Mutex
	handler func(net.Message)
}

func (c *capChannel) SetFilter(net.BroadcastChannelFilter) error  { return nil }
func (c *capChannel) SetUnmarshaler(func() net.TaggedUnmarshaler) {}
func (c *capChannel) Recv(_ context.Context, handler func(net.Message)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}
func (c *capChannel) deliver(m net.Message) {
	c.mu.Lock()
	h := c.handler
	c.mu.Unlock()
	h(m)
}

type capMessage struct {
	net.Message
	sender  []byte
	payload any
}

func (m capMessage) SenderPublicKey() []byte { return m.sender }
func (m capMessage) Payload() any            { return m.payload }

// busWorld is a two-seat group: seat 1 is the sender's, seat 2 another
// operator's.
type busWorld struct {
	membership *group.MembershipValidator
	sender     []byte
	channel    *capChannel
}

func newBusWorld(t *testing.T) *busWorld {
	t.Helper()
	signing := local_v1.Connect(2, 2).Signing()
	var keys [][]byte
	var addresses []chain.Address
	for i := 0; i < 2; i++ {
		_, pub, err := operator.GenerateKeyPair(local_v1.DefaultCurve)
		if err != nil {
			t.Fatal(err)
		}
		address, err := signing.PublicKeyToAddress(pub)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, operator.MarshalUncompressed(pub))
		addresses = append(addresses, address)
	}
	return &busWorld{membership: group.NewMembershipValidator(&testutils.MockLogger{}, addresses, signing), sender: keys[0], channel: &capChannel{}}
}

func newBusSafely(ctx context.Context, network net.Provider, membership *group.MembershipValidator, epoch uint64) (b *NetworkBus, err error, panicked any) {
	defer func() { panicked = recover() }()
	b, err = NewNetworkBus(ctx, network, membership, [32]byte{0xC4}, epoch)
	return b, err, nil
}

// Invariant: the attestation channel is opened only with a context, a
// network, a membership validator and a non-zero epoch; each missing on its
// own is refused without panicking. Rows network.go:56 (4).
func TestNetworkBusRefusesIncompleteSetup(t *testing.T) {
	t.Parallel()
	w := newBusWorld(t)
	provider := capProvider{channel: w.channel}
	var nilContext context.Context
	for name, tc := range map[string]struct {
		ctx        context.Context
		network    net.Provider
		membership *group.MembershipValidator
		epoch      uint64
	}{
		"nil-context":    {nilContext, provider, w.membership, 77},
		"nil-network":    {context.Background(), nil, w.membership, 77},
		"nil-membership": {context.Background(), provider, nil, 77},
		"zero-epoch":     {context.Background(), provider, w.membership, 0},
	} {
		t.Run(name, func(t *testing.T) {
			b, err, p := newBusSafely(tc.ctx, tc.network, tc.membership, tc.epoch)
			if p != nil {
				t.Fatalf("NewNetworkBus panicked instead of refusing a setup with %s: %v", name, p)
			}
			if err == nil || b != nil {
				t.Fatalf("NewNetworkBus opened a channel with %s", name)
			}
		})
	}
	t.Run("complete", func(t *testing.T) {
		b, err, p := newBusSafely(context.Background(), capProvider{channel: &capChannel{}}, w.membership, 77)
		if p != nil || err != nil || b == nil {
			t.Fatalf("NewNetworkBus refused a complete setup: %v (panic %v)", err, p)
		}
	})
}

func busAttestation(seat uint16, epoch uint64, digest byte) *Attestation {
	a := fixtureAttestation(seat)
	a.Epoch, a.Digest = epoch, [32]byte{digest}
	return &a
}

// drain returns every attestation the bus holds, waiting at most 50 ms for
// each.
func drain(b *NetworkBus) (digests []byte, err error) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		a, e := b.Receive(ctx)
		cancel()
		if errors.Is(e, context.DeadlineExceeded) {
			return digests, nil
		}
		if e != nil {
			return digests, e
		}
		digests = append(digests, a.Digest[0])
	}
}

// Invariant: the bus passes on only attestations of its epoch whose seat is
// the authenticated sender's seat in the group; other payloads are dropped.
// Row network.go:72.
func TestNetworkBusDeliversOnlyOwnEpochAndSeat(t *testing.T) {
	t.Parallel()
	w := newBusWorld(t)
	b, err := NewNetworkBus(context.Background(), capProvider{channel: w.channel}, w.membership, [32]byte{0xC4}, 77)
	if err != nil {
		t.Fatal(err)
	}
	w.channel.deliver(capMessage{sender: w.sender, payload: busAttestation(1, 78, 0xE1)}) // other epoch
	w.channel.deliver(capMessage{sender: w.sender, payload: busAttestation(2, 77, 0xE2)}) // not the sender's seat
	w.channel.deliver(capMessage{sender: w.sender, payload: "not an attestation"})        // other payload
	w.channel.deliver(capMessage{sender: w.sender, payload: busAttestation(1, 77, 0xE4)}) // valid
	digests, err := drain(b)
	if err != nil || len(digests) != 1 || digests[0] != 0xE4 {
		t.Fatalf("bus delivered attestations it must drop: %x %v", digests, err)
	}
}

// Invariant: the bus holds at most 400 distinct attestations per epoch; the
// 401st distinct one fails the channel rather than being delivered. Row
// network.go:85.
func TestNetworkBusFailsPastCapacity(t *testing.T) {
	t.Parallel()
	w := newBusWorld(t)
	b, err := NewNetworkBus(context.Background(), capProvider{channel: w.channel}, w.membership, [32]byte{0xC4}, 77)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 400; i++ {
		a := busAttestation(1, 77, 0)
		a.Digest[0], a.Digest[1] = byte(i), byte(i>>8)
		w.channel.deliver(capMessage{sender: w.sender, payload: a})
		if _, err := b.Receive(ctx); err != nil {
			t.Fatalf("attestation %d of 400 not delivered: %v", i+1, err)
		}
	}
	a := busAttestation(1, 77, 0)
	a.Digest[2] = 1
	w.channel.deliver(capMessage{sender: w.sender, payload: a})
	if got, err := b.Receive(ctx); err == nil {
		t.Fatalf("bus delivered a 401st distinct attestation: %x", got.Digest[:3])
	}
}
