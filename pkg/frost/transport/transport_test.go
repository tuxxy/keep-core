package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// This hub is a test-only authenticated operator transport. It deliberately
// delivers synchronously and replays key announcements when a receiver joins.
type testMessage struct {
	net.Message
	p   *packet
	key []byte
}

func (m testMessage) Payload() interface{}    { return m.p }
func (m testMessage) Type() string            { return packetType }
func (m testMessage) SenderPublicKey() []byte { return m.key }

type testSubscription struct {
	ctx  context.Context
	name string
	fn   func(net.Message)
}
type publication struct {
	name string
	m    testMessage
	ctx  context.Context
}
type testHub struct {
	mu            sync.Mutex
	subscriptions []testSubscription
	publications  []publication
	names         map[string]bool
}
type testProvider struct {
	net.Provider
	hub *testHub
	key []byte
}

func (p testProvider) BroadcastChannelFor(name string) (net.BroadcastChannel, error) {
	p.hub.mu.Lock()
	p.hub.names[name] = true
	p.hub.mu.Unlock()
	return &testChannel{hub: p.hub, name: name, key: p.key}, nil
}

type testChannel struct {
	net.BroadcastChannel
	hub  *testHub
	name string
	key  []byte
}

func (c *testChannel) SetFilter(net.BroadcastChannelFilter) error  { return nil }
func (c *testChannel) SetUnmarshaler(func() net.TaggedUnmarshaler) {}
func (c *testChannel) Recv(ctx context.Context, fn func(net.Message)) {
	c.hub.mu.Lock()
	c.hub.subscriptions = append(c.hub.subscriptions, testSubscription{ctx, c.name, fn})
	old := append([]publication(nil), c.hub.publications...)
	c.hub.mu.Unlock()
	for _, p := range old {
		if p.name == c.name && p.m.p.Kind == "ephemeral-key" && p.ctx.Err() == nil {
			fn(p.m)
		}
	}
}
func (c *testChannel) Send(ctx context.Context, m net.TaggedMarshaler, _ ...net.RetransmissionStrategy) error {
	raw, e := m.Marshal()
	if e != nil {
		return e
	}
	p := new(packet)
	if e = p.Unmarshal(raw); e != nil {
		return e
	}
	msg := testMessage{p: p, key: append([]byte(nil), c.key...)}
	c.hub.mu.Lock()
	c.hub.publications = append(c.hub.publications, publication{c.name, msg, ctx})
	subs := append([]testSubscription(nil), c.hub.subscriptions...)
	c.hub.mu.Unlock()
	for _, sub := range subs {
		if sub.name == c.name && sub.ctx.Err() == nil {
			sub.fn(msg)
		}
	}
	return nil
}
func testEnvelope(kind string, id [32]byte, sender, recipient uint16, payload []byte) []byte {
	seat := func(n uint16) []byte { return binary.BigEndian.AppendUint16(nil, n) }
	var to []byte
	if recipient != 0 {
		to = seat(recipient)
	}
	fields := [][]byte{[]byte("snowfall-message-v1"), []byte(kind), id[:], seat(sender), to, payload}
	var out []byte
	for _, f := range fields {
		out = binary.BigEndian.AppendUint64(out, uint64(len(f)))
		out = append(out, f...)
	}
	return out
}

type fixture struct {
	hub     *testHub
	nodes   []*Transport
	configs []Config
	ctx     context.Context
}

func newFixture(t *testing.T, nodes [][]uint16, capacity int) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	f := &fixture{ctx: ctx, hub: &testHub{names: map[string]bool{}}}
	signing := local_v1.Connect(3, 3).Signing()
	pubs := make([]*operator.PublicKey, len(nodes))
	addresses := make([]chain.Address, 3)
	for i, seats := range nodes {
		_, pub, e := operator.GenerateKeyPair(local_v1.DefaultCurve)
		if e != nil {
			t.Fatal(e)
		}
		pubs[i] = pub
		addr, e := signing.PublicKeyToAddress(pub)
		if e != nil {
			t.Fatal(e)
		}
		for _, seat := range seats {
			addresses[seat-1] = addr
		}
	}
	membership := group.NewMembershipValidator(&testutils.MockLogger{}, addresses, signing)
	domain := frost.Domain{Network: t.Name(), Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 77}
	attempt, e := domain.NewAttempt("dkg", [32]byte{1}, 100)
	if e != nil {
		t.Fatal(e)
	}
	for i, seats := range nodes {
		pub := operator.MarshalUncompressed(pubs[i])
		c := Config{Domain: domain, Attempt: attempt, ID: snowfallengine.AttemptID(attempt), Roster: []uint16{1, 2, 3}, LocalSeats: seats, LocalPublicKey: pub, Membership: membership, Network: testProvider{hub: f.hub, key: pub}, QueueCapacity: capacity}
		tr, e := New(ctx, c)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(tr.Close)
		f.nodes = append(f.nodes, tr)
		f.configs = append(f.configs, c)
	}
	return f
}
func receive(t *testing.T, tr *Transport, want []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, e := tr.Receive(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(in.Message, want) {
		t.Fatal("worker envelope changed")
	}
}
func noReceive(t *testing.T, tr *Transport) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, e := tr.Receive(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("unexpected delivered packet/result: %v", e)
	}
}
func TestSeatAttributionAndPrivateRoundTwo(t *testing.T) {
	f := newFixture(t, [][]uint16{{1, 2}, {3}}, 16)
	a, b := f.nodes[0], f.nodes[1]
	broadcast := testEnvelope("dkg-round-one", a.id, 1, 0, []byte("public"))
	if e := a.Broadcast(f.ctx, 1, broadcast); e != nil {
		t.Fatal(e)
	}
	receive(t, a, broadcast)
	receive(t, b, broadcast)
	local := testEnvelope("dkg-round-two", a.id, 1, 2, []byte("LOCAL PRIVATE MARKER"))
	before := len(f.hub.publications)
	if e := a.SendPrivate(f.ctx, 1, 2, local); e != nil {
		t.Fatal(e)
	}
	receive(t, a, local)
	if len(f.hub.publications) != before {
		t.Fatal("local private packet went onto public channel")
	}
	noReceive(t, b)
	remote := testEnvelope("dkg-round-two", a.id, 2, 3, []byte("REMOTE PRIVATE MARKER"))
	if e := a.SendPrivate(f.ctx, 2, 3, remote); e != nil {
		t.Fatal(e)
	}
	receive(t, b, remote)
	p := f.hub.publications[len(f.hub.publications)-1].m
	raw, _ := p.p.Marshal()
	if bytes.Contains(raw, []byte("REMOTE PRIVATE MARKER")) {
		t.Fatal("private plaintext reached pubsub")
	}
	b.receive(p)
	noReceive(t, b) // exact retransmission is idempotent
	forged := a.packet(1, 0, "dkg-round-one", testEnvelope("dkg-round-one", a.id, 1, 0, []byte("forged")))
	b.receive(testMessage{p: &forged, key: f.configs[1].LocalPublicKey})
	noReceive(t, b)
	// A malicious member may alter outer metadata, but cannot retarget a ciphertext.
	altered := *p.p
	altered.Kind = "dkg-round-one"
	altered.Recipient = 0
	b.receive(testMessage{p: &altered, key: p.key})
	noReceive(t, b)
	if e := a.Broadcast(f.ctx, 1, local); e == nil {
		t.Fatal("private envelope accepted by broadcast API")
	}
}
func TestReorderedPrivatePacketWaitsForAuthenticatedKey(t *testing.T) {
	f := newFixture(t, [][]uint16{{1, 2}, {3}}, 8)
	a, b := f.nodes[0], f.nodes[1]
	a.mu.Lock()
	delete(a.keys, 3)
	a.mu.Unlock() // model delayed sender announcement
	body := testEnvelope("dkg-round-two", a.id, 3, 1, []byte("private before key"))
	if e := b.SendPrivate(f.ctx, 3, 1, body); e != nil {
		t.Fatal(e)
	}
	noReceive(t, a)
	p := b.packet(3, 0, "ephemeral-key", b.local[3].PublicKey.Marshal())
	a.receive(testMessage{p: &p, key: f.configs[1].LocalPublicKey})
	receive(t, a, body)
	a.mu.Lock()
	n := len(a.pending)
	a.mu.Unlock()
	if n != 0 {
		t.Fatal("pending ciphertext retained")
	}
}
func TestAttemptReplayAndQueueBounds(t *testing.T) {
	t.Run("replay-and-bounds", func(t *testing.T) {
		f := newFixture(t, [][]uint16{{1, 2}, {3}}, 1)
		a, b := f.nodes[0], f.nodes[1]
		stale := a.packet(1, 0, "dkg-round-one", nil)
		stale.Attempt = bytes.Repeat([]byte{8}, 32)
		stale.Body = testEnvelope("dkg-round-one", [32]byte{8}, 1, 0, nil)
		b.receive(testMessage{p: &stale, key: f.configs[0].LocalPublicKey})
		noReceive(t, b)
		for i := 0; i < 2; i++ {
			p := a.packet(1, 0, "dkg-round-one", testEnvelope("dkg-round-one", a.id, 1, 0, []byte{byte(i)}))
			b.receive(testMessage{p: &p, key: f.configs[0].LocalPublicKey})
		}
		if _, e := b.Receive(f.ctx); !errors.Is(e, ErrCapacity) {
			t.Fatal("queue overflow did not stop operation")
		}
	})
	t.Run("oversized", func(t *testing.T) {
		f := newFixture(t, [][]uint16{{1, 2}, {3}}, 8)
		a := f.nodes[0]
		body := testEnvelope("dkg-round-one", a.id, 1, 0, make([]byte, 8193))
		if e := a.Broadcast(f.ctx, 1, body); e == nil {
			t.Fatal("oversized payload accepted")
		}
	})
	t.Run("message-budget", func(t *testing.T) {
		f := newFixture(t, [][]uint16{{1, 2}, {3}}, 8)
		a := f.nodes[0]
		a.max = 1
		for i := 0; i < 2; i++ {
			body := testEnvelope("dkg-round-one", a.id, 1, 0, []byte{byte(i)})
			e := a.Broadcast(f.ctx, 1, body)
			if i == 0 && e != nil {
				t.Fatal(e)
			}
			if i == 1 && !errors.Is(e, ErrCapacity) {
				t.Fatal("dedup cache grew beyond budget")
			}
		}
	})
}
func TestPacketEncodingRejectsAlternateAndForeignFields(t *testing.T) {
	p := packet{Version: 1, Domain: make([]byte, 32), Attempt: make([]byte, 32), Sender: 1, Kind: "share", Body: []byte("exact")}
	raw, e := p.Marshal()
	if e != nil {
		t.Fatal(e)
	}
	var out packet
	if e = out.Unmarshal(raw); e != nil {
		t.Fatal(e)
	}
	for name, b := range map[string][]byte{"trailing": append(append([]byte(nil), raw...), 0), "oversized": make([]byte, maxPacket+1), "version": append([]byte{8, 2}, raw[2:]...), "overlong-varint": append([]byte{8, 0x81, 0}, raw[2:]...)} {
		t.Run(name, func(t *testing.T) {
			if new(packet).Unmarshal(b) == nil {
				t.Fatal("alternate packet accepted")
			}
		})
	}
}
func TestSendContextRetainsAgreedDeadline(t *testing.T) {
	f := newFixture(t, [][]uint16{{1, 2}, {3}}, 8)
	a := f.nodes[0]
	ctx, cancel := context.WithCancel(f.ctx)
	body := testEnvelope("share", a.id, 1, 0, []byte("public"))
	if e := a.Broadcast(ctx, 1, body); e != nil {
		t.Fatal(e)
	}
	cancel()
	p := f.hub.publications[len(f.hub.publications)-1]
	if p.ctx.Err() != nil {
		t.Fatal("per-send cancellation stopped retransmission")
	}
	a.Close()
	if p.ctx.Err() == nil {
		t.Fatal("operation close left retransmission alive")
	}
}
func TestRetriesReuseTopic(t *testing.T) {
	f := newFixture(t, [][]uint16{{1, 2}, {3}}, 8)
	for _, tr := range f.nodes {
		tr.Close()
	}
	for i := 2; i < 12; i++ {
		c := f.configs[0]
		a, e := c.Domain.NewAttempt("dkg", sha256.Sum256([]byte(fmt.Sprint(i))), uint64(100+i))
		if e != nil {
			t.Fatal(e)
		}
		c.Attempt = a
		c.ID = snowfallengine.AttemptID(a)
		tr, e := New(f.ctx, c)
		if e != nil {
			t.Fatal(e)
		}
		tr.Close()
	}
	if len(f.hub.names) != 1 {
		t.Fatal("each retry allocated a new permanent network topic")
	}
}

func TestPendingCiphertextBoundAndDeduplication(t *testing.T) {
	f := newFixture(t, [][]uint16{{1, 2}, {3}}, 1)
	a, b := f.nodes[0], f.nodes[1]
	a.mu.Lock()
	delete(a.keys, 3)
	a.mu.Unlock()
	body := testEnvelope("dkg-round-two", a.id, 3, 1, []byte("waiting"))
	if e := b.SendPrivate(f.ctx, 3, 1, body); e != nil {
		t.Fatal(e)
	}
	packet := f.hub.publications[len(f.hub.publications)-1].m
	a.receive(packet)
	a.receive(packet)
	a.mu.Lock()
	count := len(a.pending)
	failure := a.failure
	a.mu.Unlock()
	if count != 1 || failure != nil {
		t.Fatal("duplicate pending ciphertext consumed extra capacity")
	}
	body = testEnvelope("dkg-round-two", a.id, 3, 2, []byte("another"))
	if e := b.SendPrivate(f.ctx, 3, 2, body); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Receive(f.ctx); !errors.Is(e, ErrCapacity) {
		t.Fatal("pending ciphertext overflow accepted")
	}
}

func TestForeignDomainAndEnvelopeBinding(t *testing.T) {
	for _, change := range []string{"domain", "attempt", "sender", "recipient", "kind"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t, [][]uint16{{1, 2}, {3}}, 8)
			a, b := f.nodes[0], f.nodes[1]
			p := a.packet(1, 0, "dkg-round-one", testEnvelope("dkg-round-one", a.id, 1, 0, []byte("bound")))
			switch change {
			case "domain":
				p.Domain[0] ^= 1
			case "attempt":
				p.Attempt[0] ^= 1
			case "sender":
				p.Sender = 2
			case "recipient":
				p.Recipient = 3
			case "kind":
				p.Kind = "share"
			}
			b.receive(testMessage{p: &p, key: f.configs[0].LocalPublicKey})
			noReceive(t, b)
		})
	}
}
