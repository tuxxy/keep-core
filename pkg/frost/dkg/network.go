package dkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

type Attestation struct {
	Stage     string
	Epoch     uint64
	Seat      uint16
	Digest    [32]byte
	Reference [32]byte
	Signature []byte
}

func (a *Attestation) Type() string { return "frost-chain-attestation-v1" }
func (a *Attestation) Marshal() ([]byte, error) {
	if a.Seat == 0 || a.Seat > 100 || len(a.Signature) != 65 || (a.Stage != "result" && a.Stage != "ready") {
		return nil, errors.New("invalid attestation")
	}
	return json.Marshal(a)
}
func (a *Attestation) Unmarshal(b []byte) error {
	if len(b) > 2048 {
		return errors.New("oversized attestation")
	}
	if e := json.Unmarshal(b, a); e != nil {
		return e
	}
	_, e := a.Marshal()
	return e
}

type Bus interface {
	Send(context.Context, Attestation) error
	Receive(context.Context) (Attestation, error)
}

// NetworkBus uses a dedicated authenticated channel. Worker envelopes and
// host attestations never share a receive queue.
type NetworkBus struct {
	channel  net.BroadcastChannel
	incoming chan Attestation
	failed   chan struct{}
	once     sync.Once
}

func NewNetworkBus(ctx context.Context, network net.Provider, membership *group.MembershipValidator, chainDomain [32]byte, epoch uint64) (*NetworkBus, error) {
	if ctx == nil || network == nil || membership == nil || epoch == 0 {
		return nil, errors.New("invalid attestation channel")
	}
	ch, e := network.BroadcastChannelFor(fmt.Sprintf("frost-chain-v1-%x", chainDomain))
	if e != nil {
		return nil, e
	}
	if e = ch.SetFilter(membership.IsInGroup); e != nil {
		return nil, e
	}
	b := &NetworkBus{channel: ch, incoming: make(chan Attestation, 400), failed: make(chan struct{})}
	ch.SetUnmarshaler(func() net.TaggedUnmarshaler { return &Attestation{} })
	var mu sync.Mutex
	seen := map[string]bool{}
	ch.Recv(ctx, func(m net.Message) {
		a, ok := m.Payload().(*Attestation)
		if !ok || a.Epoch != epoch || !membership.IsValidMembership(group.MemberIndex(a.Seat), m.SenderPublicKey()) {
			return
		}
		raw, e := a.Marshal()
		if e != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		k := string(raw)
		if seen[k] {
			return
		}
		if len(seen) >= 400 {
			b.once.Do(func() { close(b.failed) })
			return
		}
		seen[k] = true
		copy := *a
		copy.Signature = append([]byte(nil), a.Signature...)
		select {
		case b.incoming <- copy:
		default:
			b.once.Do(func() { close(b.failed) })
		}
	})
	return b, nil
}
func (b *NetworkBus) Send(ctx context.Context, a Attestation) error { return b.channel.Send(ctx, &a) }
func (b *NetworkBus) Receive(ctx context.Context) (Attestation, error) {
	select {
	case a := <-b.incoming:
		return a, nil
	case <-b.failed:
		return Attestation{}, errors.New("attestation channel capacity exceeded")
	case <-ctx.Done():
		return Attestation{}, ctx.Err()
	}
}
