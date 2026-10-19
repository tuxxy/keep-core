package signing

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

type nonceAuthority struct {
	current uint64
	calls   int
}

func (a *nonceAuthority) AuthorizeHeartbeat(_ context.Context, _ [32]byte, nonce uint64) error {
	a.calls++
	if nonce != a.current {
		return errors.New("stale heartbeat nonce")
	}
	return nil
}

type heartbeatEngine struct {
	frost.Engine
	signer  *HeartbeatSigner
	private *btcec.PrivateKey
	calls   int
	err     error
	grant   func(frost.SigningRequest) frost.SigningRequest
}

func (e *heartbeatEngine) Sign(ctx context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	e.calls++
	j := e.signer.config.Journal.(*journalStub)
	if _, ok := j.records[e.signer.intent.hash()]; !ok {
		return [64]byte{}, errors.New("heartbeat intent not durable before sign")
	}
	if _, ok := j.records[DefenseID(e.signer.intent.WalletID, r.Message)]; !ok {
		return [64]byte{}, errors.New("heartbeat defense not durable before sign")
	}
	if e.grant != nil {
		r = e.grant(r)
	}
	if err := p.SigningAuthorization.BeforeSigning(ctx, r); err != nil {
		return [64]byte{}, err
	}
	if e.err != nil {
		return [64]byte{}, e.err
	}
	s, err := schnorr.Sign(e.private, r.Message[:])
	var raw [64]byte
	if err == nil {
		copy(raw[:], s.Serialize())
	}
	return raw, err
}

type presentTransport struct {
	frost.Transport
	seats []uint16
}

func (p presentTransport) Present() []uint16 { return append([]uint16(nil), p.seats...) }

func heartbeatFixture(t *testing.T) (*HeartbeatSigner, *nonceAuthority, *heartbeatEngine) {
	t.Helper()
	proposal, private, _ := planFixture(t)
	w := proposal.Wallet
	descriptorHash, err := dkg.DescriptorHash(w)
	if err != nil {
		t.Fatal(err)
	}
	domain := frost.Domain{Network: "local", Chain: [32]byte{31: 1}, Registry: [20]byte(w.Registry), Epoch: w.Epoch}
	key := frost.KeyReady{Candidate: frost.Candidate{Epoch: w.Epoch, Descriptor: w.SnowfallDescriptor, OutputKey: w.OutputKey, Threshold: 2, Roster: []uint16{1, 2, 3}, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{make([]byte, 32)}}
	j := &journalStub{domain: domain, key: key, walletRecord: dkg.WalletRecord{Descriptor: w, ID: dkg.WalletID(w.OutputKey), DescriptorHash: descriptorHash, State: "ReadyUnfunded"}, records: map[[32]byte][]byte{}}
	authority := &nonceAuthority{current: 40}
	h, err := NewHeartbeatSigner(HeartbeatConfig{Wallet: w, Nonce: 40, Session: [32]byte{7}, StartBlock: 42, Journal: j, Authority: authority, Worker: snowfallengine.WorkerConfig{Path: "/unused-test-worker", SHA256: [32]byte{1}}, Selected: []uint16{1, 2, 3}, Transport: func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
		return presentTransport{seats: []uint16{3, 1}}, func() {}, nil
	}, LocalRegtest: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := &heartbeatEngine{signer: h, private: private}
	h.engine = engine
	return h, authority, engine
}

// P-K08-HEARTBEAT-DOMAIN: the host releases only the C-08 heartbeat digest
// of the current nonce, under its own attempt domain.
func TestHeartbeatSignerDomain(t *testing.T) {
	ctx := context.Background()
	t.Run("signs the C-08 heartbeat digest and keeps its defense", func(t *testing.T) {
		h, _, _ := heartbeatFixture(t)
		result, err := h.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wallet := h.intent.WalletID
		tag := sha256.Sum256([]byte("tbtc-v2/frost-heartbeat/v1"))
		raw := append(append(append(tag[:], tag[:]...), wallet[:]...), binary.BigEndian.AppendUint64(nil, 40)...)
		if want := sha256.Sum256(raw); result.Done.Context.Message != want || h.Digest() != want {
			t.Fatal("heartbeat message is not the C-08 digest")
		}
		if _, err = result.Done.Verify(result.Done.Context, h.config.Wallet.OutputKey); err != nil || result.Done.Context.Purpose != Heartbeat {
			t.Fatal("heartbeat done", err)
		}
		r, err := ReadDefense(ctx, h.config.Journal, wallet, h.Digest())
		if err != nil || r.Kind != DefenseHeartbeat || r.Nonce != 40 || r.Transaction != nil {
			t.Fatalf("heartbeat defense %+v %v", r, err)
		}
		if len(result.Present) != 2 || result.Present[0] != 1 || result.Present[1] != 3 {
			t.Fatalf("presence %v", result.Present)
		}
	})
	t.Run("a raw digest in the grant is refused", func(t *testing.T) {
		h, _, engine := heartbeatFixture(t)
		transactionDigest := [32]byte{0x7a}
		engine.grant = func(r frost.SigningRequest) frost.SigningRequest {
			// The grant keeps the heartbeat intent but carries another message.
			r.Message = transactionDigest
			return r
		}
		if _, err := h.Run(ctx); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("a non-heartbeat message passed the heartbeat grant", err)
		}
		g := heartbeatAuthorization{signer: h}
		attempt, _ := h.attempt()
		g.expected = frost.SigningRequest{Attempt: attempt, Message: transactionDigest, Authorization: h.encoded}
		if err := g.BeforeSigning(ctx, g.expected); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("the guard accepted a raw digest", err)
		}
	})
	t.Run("a stale nonce is refused before the intent record", func(t *testing.T) {
		h, authority, engine := heartbeatFixture(t)
		authority.current = 50
		if _, err := h.Run(ctx); !errors.Is(err, ErrUnauthorized) || engine.calls != 0 {
			t.Fatal("stale heartbeat reached the worker", err)
		}
		if _, ok := h.config.Journal.(*journalStub).records[h.intent.hash()]; ok {
			t.Fatal("stale heartbeat recorded an intent")
		}
	})
	t.Run("the nonce is rechecked after the claim", func(t *testing.T) {
		h, authority, _ := heartbeatFixture(t)
		h.boundary = func(name string) error {
			if name == "before-worker-start" {
				authority.current = 50
			}
			return nil
		}
		if _, err := h.Run(ctx); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("heartbeat released after its round passed", err)
		}
	})
	t.Run("its own attempt domain", func(t *testing.T) {
		h, _, _ := heartbeatFixture(t)
		attempt, err := h.attempt()
		if err != nil {
			t.Fatal(err)
		}
		id := h.intent.hash()
		raw := append([]byte("keep-core/frost/heartbeat-attempt/v1/"), id[:]...)
		raw = append(raw, h.config.Session[:]...)
		want, err := h.config.Journal.Domain().NewAttempt("sign", sha256.Sum256(binary.BigEndian.AppendUint32(raw, 0)), 42)
		if err != nil || snowfallengine.AttemptID(attempt) != snowfallengine.AttemptID(want) {
			t.Fatal("heartbeat attempt is not in the heartbeat domain")
		}
	})
	t.Run("presence survives a failed heartbeat", func(t *testing.T) {
		h, _, engine := heartbeatFixture(t)
		engine.err = errors.New("worker failed")
		result, err := h.Run(ctx)
		if err == nil || len(result.Present) != 2 {
			t.Fatal("failed heartbeat lost its presence", result, err)
		}
	})
}
