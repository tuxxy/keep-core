// Package transport adapts authenticated keep-core channels to SNOWFALL. It
// preserves worker envelopes and encrypts remote private messages with the
// established ephemeral ECDH mechanism. No raw private bytes enter pubsub.
package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/keep-network/keep-core/pkg/crypto/ephemeral"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/net"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

var ErrCapacity = errors.New("FROST transport capacity exceeded")

type Config struct {
	// Required by guarded DKG. Retains operator attribution before delivery.
	RecoveryJournal frost.RecoveryJournal
	RecoveryOnly    bool
	Domain          frost.Domain
	Attempt         frost.Attempt
	// ID must come from snowfallengine.AttemptID. The guarded engine checks it.
	ID             [32]byte
	LocalSeats     []uint16
	Roster         []uint16
	LocalPublicKey []byte
	Membership     *group.MembershipValidator
	Network        net.Provider
	QueueCapacity  int
	MaxMessages    int
}

type Transport struct {
	journal      frost.RecoveryJournal
	scope        frost.Domain
	localKey     []byte
	recoveryOnly bool
	ctx          context.Context
	cancel       context.CancelFunc
	attempt      frost.Attempt
	id, domain   [32]byte
	channel      net.BroadcastChannel
	membership   *group.MembershipValidator
	local        map[uint16]*ephemeral.KeyPair
	roster       map[uint16]bool
	sendToken    chan struct{}
	mu           sync.Mutex
	keys         map[uint16][]byte
	changed      chan struct{}
	incoming     chan frost.Incoming
	seen         map[[32]byte]bool
	sent         map[[32]byte]bool
	max          int
	pending      map[[32]byte]packet
	pendingLimit int
	failure      error
}

var _ frost.Transport = (*Transport)(nil)

func New(ctx context.Context, c Config) (*Transport, error) {
	if ctx == nil || c.Network == nil || c.Membership == nil || c.ID == ([32]byte{}) {
		return nil, errors.New("incomplete FROST transport configuration")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("FROST transport requires an agreed deadline")
	}
	if e := c.Domain.Validate(); e != nil {
		return nil, e
	}
	if c.Domain.ValidateAttempt(c.Attempt, "dkg") != nil && c.Domain.ValidateAttempt(c.Attempt, "sign") != nil {
		return nil, errors.New("transport attempt domain mismatch")
	}
	if c.QueueCapacity == 0 {
		c.QueueCapacity = 256
	}
	if c.MaxMessages == 0 {
		c.MaxMessages = 4096
	}
	if c.QueueCapacity < 1 || c.QueueCapacity > 4096 || c.MaxMessages < 1 || c.MaxMessages > 65536 {
		return nil, ErrCapacity
	}
	t := &Transport{journal: c.RecoveryJournal, scope: c.Domain, localKey: append([]byte(nil), c.LocalPublicKey...), recoveryOnly: c.RecoveryOnly, id: c.ID, domain: c.Domain.ID(), membership: c.Membership, local: map[uint16]*ephemeral.KeyPair{}, roster: map[uint16]bool{}, keys: map[uint16][]byte{}, changed: make(chan struct{}), incoming: make(chan frost.Incoming, c.QueueCapacity), seen: map[[32]byte]bool{}, sent: map[[32]byte]bool{}, max: c.MaxMessages, pending: map[[32]byte]packet{}, pendingLimit: c.QueueCapacity, sendToken: make(chan struct{}, 1)}
	if c.RecoveryJournal != nil && c.RecoveryJournal.Domain() != c.Domain {
		return nil, errors.New("readiness journal domain mismatch")
	}
	t.attempt = frost.Attempt{Channel: append([]byte(nil), c.Attempt.Channel...), Session: append([]byte(nil), c.Attempt.Session...), StartBlock: c.Attempt.StartBlock}
	var prev uint16
	for _, seat := range c.Roster {
		if seat <= prev || seat > 100 {
			return nil, errors.New("invalid transport roster")
		}
		prev = seat
		t.roster[seat] = true
	}
	if len(t.roster) == 0 || len(c.LocalSeats) == 0 {
		return nil, errors.New("missing transport seats")
	}
	for _, seat := range c.LocalSeats {
		if !t.roster[seat] || t.local[seat] != nil || !c.Membership.IsValidMembership(group.MemberIndex(seat), c.LocalPublicKey) {
			return nil, errors.New("local seat does not belong to operator")
		}
		pair, e := ephemeral.GenerateKeyPair()
		if e != nil {
			return nil, e
		}
		t.local[seat] = pair
		t.keys[seat] = pair.PublicKey.Marshal()
	}
	// Reuse the domain/purpose channel across retries. Libp2p owns topics until
	// provider shutdown, so per-attempt topics would grow without bound.
	topic := sha256.Sum256(c.Attempt.Channel)
	name := fmt.Sprintf("frost-v1-%x", topic)
	ch, e := c.Network.BroadcastChannelFor(name)
	if e != nil {
		return nil, e
	}
	t.channel = ch
	if e = ch.SetFilter(c.Membership.IsInGroup); e != nil {
		return nil, e
	}
	t.ctx, t.cancel = context.WithCancel(ctx)
	ch.SetUnmarshaler(func() net.TaggedUnmarshaler { return &packet{} })
	if c.RecoveryOnly {
		if t.journal == nil {
			t.cancel()
			return nil, errors.New("recovery requires readiness journal")
		}
		evidence, e := t.journal.ReadReadiness(ctx, t.id)
		if e != nil {
			t.cancel()
			return nil, e
		}
		for _, saved := range evidence {
			kind, id, sender, recipient, err := inspect(saved.Message)
			if err != nil || kind != "ready-attestation" || id != t.id || sender != saved.Sender || recipient != 0 || saved.Domain != c.Domain || !t.roster[sender] || !t.membership.IsValidMembership(group.MemberIndex(sender), saved.OperatorKey) {
				t.cancel()
				return nil, errors.New("invalid saved readiness attribution")
			}
			t.enqueueLocked(sender, saved.Message)
		}
	}
	ch.Recv(t.ctx, t.receive)
	for seat, pair := range t.local {
		if c.RecoveryOnly {
			break
		}
		p := t.packet(seat, 0, "ephemeral-key", pair.PublicKey.Marshal())
		if e = ch.Send(t.ctx, &p); e != nil {
			t.cancel()
			return nil, e
		}
	}
	return t, nil
}
func (t *Transport) RecoveryBinding() frost.RecoveryJournal { return t.journal }

func (t *Transport) Binding() (frost.Attempt, [32]byte) {
	return frost.Attempt{Channel: append([]byte(nil), t.attempt.Channel...), Session: append([]byte(nil), t.attempt.Session...), StartBlock: t.attempt.StartBlock}, t.id
}
func (t *Transport) Close() { t.cancel() }
func (t *Transport) packet(sender, recipient uint16, kind string, body []byte) packet {
	return packet{Version: 1, Domain: append([]byte(nil), t.domain[:]...), Attempt: append([]byte(nil), t.id[:]...), Sender: uint64(sender), Recipient: uint64(recipient), Kind: kind, Body: body}
}
func (t *Transport) failLocked(e error) {
	if t.failure == nil {
		t.failure = e
		t.cancel()
	}
}
func (t *Transport) receive(m net.Message) {
	p, ok := m.Payload().(*packet)
	if !ok || m.Type() != packetType {
		return
	}
	// Public entrypoints can also be fed by an in-process provider. Validate the
	// packet shape here as well as at the network unmarshal boundary.
	raw, e := p.Marshal()
	if e != nil {
		return
	}
	var checked packet
	if checked.Unmarshal(raw) != nil {
		return
	}
	p = &checked
	if !bytes.Equal(p.Domain, t.domain[:]) || !bytes.Equal(p.Attempt, t.id[:]) || !t.roster[uint16(p.Sender)] || !t.membership.IsValidMembership(group.MemberIndex(p.Sender), m.SenderPublicKey()) {
		return
	}
	if t.recoveryOnly && p.Kind != "ready-attestation" {
		return
	}
	sender, recipient := uint16(p.Sender), uint16(p.Recipient)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx.Err() != nil {
		return
	}
	// The local worker path performs local delivery, never reflected pubsub.
	if t.local[sender] != nil {
		return
	}
	if p.Kind == "ephemeral-key" {
		if recipient != 0 || len(p.Body) != 33 {
			return
		}
		if _, e := ephemeral.UnmarshalPublicKey(p.Body); e != nil {
			return
		}
		if old, ok := t.keys[sender]; ok {
			if !bytes.Equal(old, p.Body) {
				t.failLocked(errors.New("FROST ephemeral key equivocation"))
			}
			return
		}
		t.keys[sender] = append([]byte(nil), p.Body...)
		close(t.changed)
		t.changed = make(chan struct{})
		for hash, waiting := range t.pending {
			if uint16(waiting.Sender) == sender {
				delete(t.pending, hash)
				t.processLocked(&waiting, nil)
			}
		}
		return
	}
	t.processLocked(p, m.SenderPublicKey())
}
func (t *Transport) processLocked(p *packet, operatorKey []byte) {
	sender, recipient := uint16(p.Sender), uint16(p.Recipient)
	envelope := p.Body
	if p.Kind == "dkg-round-two" {
		pair := t.local[recipient]
		if pair == nil {
			return
		}
		if t.keys[sender] == nil {
			raw, _ := p.Marshal()
			hash := sha256.Sum256(raw)
			if _, ok := t.pending[hash]; ok {
				return
			}
			if len(t.pending) >= t.pendingLimit {
				t.failLocked(ErrCapacity)
				return
			}
			t.pending[hash] = *p
			return
		}
		pub, e := ephemeral.UnmarshalPublicKey(t.keys[sender])
		if e != nil {
			return
		}
		plain, e := pair.PrivateKey.Ecdh(pub).Decrypt(p.Body)
		if e != nil {
			return
		}
		header := p.header()
		if len(plain) < len(header) || !bytes.Equal(plain[:len(header)], header) {
			return
		}
		envelope = plain[len(header):]
	} else if recipient != 0 {
		return
	}
	kind, id, from, to, e := inspect(envelope)
	if e != nil || kind != p.Kind || id != t.id || from != sender || to != recipient {
		return
	}
	if kind == "ready-attestation" && !t.seen[sha256.Sum256(envelope)] {
		if e := t.saveReadiness(t.ctx, sender, envelope, operatorKey); e != nil {
			t.failLocked(e)
			return
		}
	}
	t.enqueueLocked(sender, envelope)
}
func (t *Transport) enqueueLocked(sender uint16, b []byte) {
	digest := sha256.Sum256(b)
	if t.seen[digest] {
		return
	}
	if len(t.seen) >= t.max {
		t.failLocked(ErrCapacity)
		return
	}
	select {
	case t.incoming <- frost.Incoming{AuthenticatedSender: sender, Message: append([]byte(nil), b...)}:
		t.seen[digest] = true
	default:
		t.failLocked(ErrCapacity)
	}
}
func (t *Transport) Broadcast(ctx context.Context, sender uint16, b []byte) error {
	return t.send(ctx, sender, 0, b)
}
func (t *Transport) SendPrivate(ctx context.Context, sender, recipient uint16, b []byte) error {
	if recipient == 0 {
		return errPacket
	}
	return t.send(ctx, sender, recipient, b)
}
func (t *Transport) send(ctx context.Context, sender, recipient uint16, b []byte) error {
	if ctx == nil {
		return errPacket
	}
	select {
	case t.sendToken <- struct{}{}:
		defer func() { <-t.sendToken }()
	case <-ctx.Done():
		return ctx.Err()
	case <-t.ctx.Done():
		return t.terminal()
	}
	kind, id, from, to, e := inspect(b)
	if e != nil || id != t.id || from != sender || to != recipient || t.local[sender] == nil || (recipient != 0 && !t.roster[recipient]) {
		return errPacket
	}
	if t.recoveryOnly && kind != "ready-attestation" {
		return errPacket
	}
	if ctx == nil {
		return errPacket
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if kind == "ready-attestation" {
		if e := t.saveReadiness(ctx, sender, b, t.localKey); e != nil {
			return e
		}
	}
	t.mu.Lock()
	for recipient != 0 && t.keys[recipient] == nil && t.ctx.Err() == nil {
		changed := t.changed
		t.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-t.ctx.Done():
			return t.terminal()
		}
		t.mu.Lock()
	}
	defer t.mu.Unlock()
	if t.ctx.Err() != nil {
		if t.failure != nil {
			return t.failure
		}
		return t.ctx.Err()
	}
	digest := sha256.Sum256(b)
	if t.sent[digest] {
		return nil
	}
	if len(t.sent) >= t.max {
		t.failLocked(ErrCapacity)
		return ErrCapacity
	}
	if recipient == 0 || t.local[recipient] != nil {
		t.enqueueLocked(sender, b)
		if t.failure != nil {
			return t.failure
		}
	}
	if recipient != 0 && t.local[recipient] != nil {
		t.sent[digest] = true
		return nil
	}
	p := t.packet(sender, recipient, kind, append([]byte(nil), b...))
	if recipient != 0 {
		pub, e := ephemeral.UnmarshalPublicKey(t.keys[recipient])
		if e != nil {
			return e
		}
		plain := append(p.header(), b...)
		p.Body, e = t.local[sender].PrivateKey.Ecdh(pub).Encrypt(plain)
		clear(plain)
		if e != nil {
			t.failLocked(e)
			return e
		}
	}
	// Keep retransmission alive until the transport's agreed operation deadline,
	// rather than the short-lived per-send context supplied by the client.
	t.sent[digest] = true
	t.mu.Unlock()
	e = t.channel.Send(t.ctx, &p)
	t.mu.Lock()
	if e != nil {
		t.failLocked(e)
		return e
	}
	return nil
}
func (t *Transport) terminal() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failure != nil {
		return t.failure
	}
	return t.ctx.Err()
}
func (t *Transport) Receive(ctx context.Context) (frost.Incoming, error) {
	if ctx == nil {
		return frost.Incoming{}, errPacket
	}
	if t.ctx.Err() != nil {
		return frost.Incoming{}, t.terminal()
	}
	select {
	case <-ctx.Done():
		return frost.Incoming{}, ctx.Err()
	case <-t.ctx.Done():
		return frost.Incoming{}, t.terminal()
	case v := <-t.incoming:
		if t.ctx.Err() != nil {
			return frost.Incoming{}, t.terminal()
		}
		return v, nil
	}
}

func (t *Transport) saveReadiness(ctx context.Context, sender uint16, b, key []byte) error {
	if t.journal == nil {
		return nil
	}
	return t.journal.SaveReadiness(ctx, frost.ReadinessEvidence{Domain: t.scope, Attempt: t.id, Sender: sender, OperatorKey: append([]byte(nil), key...), Message: append([]byte(nil), b...)})
}
