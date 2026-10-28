// Package snowfallengine is the only keep-core adapter to the SNOWFALL client.
package snowfallengine

import (
	"context"
	"errors"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"path/filepath"
	"sync"

	"github.com/keep-network/keep-core/pkg/frost"
	snowfall "github.com/threshold-network/snowfall/clients/go"
)

// WorkerConfig pins a local immutable worker file. Env is an explicit
// allowlist: nil means empty. Never populate it with os.Environ().
type WorkerConfig struct {
	Path              string
	SHA256            [32]byte
	Env               []string
	CommandTimeoutMs  uint32
	ShutdownTimeoutMs uint32
}

type Engine struct{ worker snowfall.WorkerConfig }

var _ frost.Engine = (*Engine)(nil)

func New(config WorkerConfig) (*Engine, error) {
	if !filepath.IsAbs(config.Path) || config.SHA256 == ([32]byte{}) {
		return nil, invalid("absolute worker path and nonzero SHA-256 pin are required")
	}
	return &Engine{worker: snowfall.WorkerConfig{WorkerPath: config.Path, WorkerSHA256: config.SHA256,
		Env: append([]string{}, config.Env...), CommandTimeoutMs: config.CommandTimeoutMs, ShutdownTimeoutMs: config.ShutdownTimeoutMs}}, nil
}

func invalid(detail string) error {
	return &frost.Error{Category: frost.InvalidRequest, State: "starting", Detail: detail}
}
func classified(err error) error {
	if err == nil {
		return nil
	}
	var source *snowfall.Error
	if errors.As(err, &source) {
		return &frost.Error{Category: frost.Category(source.Category), State: source.State, Detail: source.Detail, Suspect: source.Suspect}
	}
	return &frost.Error{Category: frost.DependencyFailure, State: "starting", Detail: "FROST dependency failed"}
}

func (e *Engine) client(ctx context.Context, g frost.Group, seats []uint16, a frost.Attempt, p frost.Providers) (*snowfall.Client, *acceptanceBridge, error) {
	if ctx == nil {
		return nil, nil, invalid("operation context is required")
	}
	if ctx.Err() != nil {
		return nil, nil, &frost.Error{Category: frost.Cancelled, State: "starting", Detail: "operation cancelled"}
	}
	if len(a.Channel) == 0 || len(a.Session) == 0 {
		return nil, nil, invalid("attempt channel and session are required")
	}
	if p.Transport == nil || p.Store == nil || p.Acceptance == nil {
		return nil, nil, invalid("transport, storage and acceptance are required")
	}
	group := snowfall.NewGroupConfig(g.Roster, g.Threshold, g.Epoch)
	if g.Quorum != 0 {
		group = group.WithQuorum(g.Quorum)
	}
	chain := &acceptanceBridge{provider: p.Acceptance}
	c, err := snowfall.NewClient(group, seats, snowfall.Providers{Transport: transportBridge{p.Transport}, Store: storeBridge{p.Store}, Chain: chain}, &e.worker)
	return c, chain, classified(err)
}

func (e *Engine) DKG(ctx context.Context, r frost.DKGRequest, p frost.Providers) (frost.KeyReady, error) {
	c, chain, err := e.client(ctx, r.Group, r.LocalSeats, r.Attempt, p)
	if err != nil {
		return frost.KeyReady{}, err
	}
	op, err := c.BeginDKG(snowfall.DKGIntent{Attempt: snowfall.AttemptID(r.Attempt.Channel, r.Attempt.Session, r.Attempt.StartBlock), Participants: r.Participants})
	if err != nil {
		return frost.KeyReady{}, classified(err)
	}
	defer op.Cancel()
	key, err := op.Wait(ctx)
	if err != nil {
		return frost.KeyReady{}, classified(err)
	}
	candidate, ok := chain.result()
	if !ok || candidate.Descriptor != key.Descriptor || candidate.OutputKey != key.OutputKey {
		return frost.KeyReady{}, &frost.Error{Category: frost.ProtocolFailure, State: "key-ready", Detail: "candidate differs from final key"}
	}
	result := frost.KeyReady{Candidate: candidate}
	for _, ref := range key.LocalReferences {
		result.LocalReferences = append(result.LocalReferences, append(frost.KeyReference(nil), ref[:]...))
	}
	return result, nil
}

func (e *Engine) Sign(ctx context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	if r.Key.Candidate.Profile != frost.ApprovedProfile {
		return [64]byte{}, invalid("unapproved signing profile")
	}
	c, _, err := e.client(ctx, r.Group, r.LocalSeats, r.Attempt, p)
	if err != nil {
		return [64]byte{}, err
	}
	candidate := r.Key.Candidate
	key := snowfall.KeyReady{Roster: candidate.Roster, Threshold: candidate.Threshold, Epoch: candidate.Epoch, Descriptor: candidate.Descriptor, OutputKey: candidate.OutputKey}
	for _, raw := range r.Key.LocalReferences {
		var ref snowfall.KeyReference
		if len(raw) != len(ref) {
			return [64]byte{}, invalid("malformed key reference")
		}
		copy(ref[:], raw)
		key.LocalReferences = append(key.LocalReferences, ref)
	}
	op, err := c.BeginSigning(key, snowfall.SigningIntent{Selected: r.Selected, Attempt: snowfall.AttemptID(r.Attempt.Channel, r.Attempt.Session, r.Attempt.StartBlock), Message: r.Message})
	if err != nil {
		return [64]byte{}, classified(err)
	}
	defer op.Cancel()
	complete, err := op.Wait(ctx)
	if err != nil {
		return [64]byte{}, classified(err)
	}
	publicKey, keyErr := schnorr.ParsePubKey(candidate.OutputKey[:])
	signature, sigErr := schnorr.ParseSignature(complete.Signature[:])
	if keyErr != nil || sigErr != nil || !signature.Verify(r.Message[:], publicKey) {
		return [64]byte{}, &frost.Error{Category: frost.ProtocolFailure, State: "complete", Detail: "signature does not verify for the requested key and message"}
	}
	return complete.Signature, nil
}

type transportBridge struct{ frost.Transport }

func (b transportBridge) Receive(ctx context.Context) (snowfall.Incoming, error) {
	in, err := b.Transport.Receive(ctx)
	return snowfall.Incoming{AuthenticatedSender: in.AuthenticatedSender, Message: in.Message}, err
}

type storeBridge struct{ frost.Store }

func (b storeBridge) Put(ctx context.Context, w snowfall.Write) (snowfall.PutResult, error) {
	out := frost.Write{Kind: w.Kind, Seat: w.Seat, ID: w.ID, Payload: w.Payload}
	if w.Kind == "lock" {
		attempt, kind, err := snowfall.LockSlot(w.Payload)
		if err != nil {
			return 0, err
		}
		out.Slot = &frost.LockSlot{Attempt: attempt, Kind: kind}
	}
	result, err := b.Store.Put(ctx, out)
	if err != nil {
		return 0, err
	}
	switch result {
	case frost.Durable:
		return snowfall.Durable, nil
	case frost.Identical:
		return snowfall.Identical, nil
	case frost.Conflict:
		return snowfall.Conflict, nil
	}
	return 0, errors.New("invalid storage result")
}

type acceptanceBridge struct {
	provider  frost.Acceptance
	mu        sync.Mutex
	candidate frost.Candidate
	called    bool
}

func (b *acceptanceBridge) WaitAcceptance(context.Context, uint64, [32]byte) (snowfall.Receipt, error) {
	return snowfall.Receipt{}, errors.New("public candidate callback required")
}
func (b *acceptanceBridge) WaitCandidateAcceptance(ctx context.Context, c snowfall.Candidate) (snowfall.Receipt, error) {
	if c.Profile != frost.ApprovedProfile {
		return snowfall.Receipt{}, errors.New("unapproved candidate profile")
	}
	candidate := frost.Candidate{Epoch: c.Epoch, Descriptor: c.Descriptor, OutputKey: c.OutputKey, Roster: append([]uint16(nil), c.Roster...), Threshold: c.Threshold, Profile: c.Profile}
	b.mu.Lock()
	b.candidate = candidate
	b.candidate.Roster = append([]uint16(nil), candidate.Roster...)
	b.called = true
	b.mu.Unlock()
	receipt, err := b.provider.WaitCandidateAcceptance(ctx, candidate)
	return snowfall.Receipt{Epoch: receipt.Epoch, Descriptor: receipt.Descriptor}, err
}
func (b *acceptanceBridge) result() (frost.Candidate, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.candidate
	c.Roster = append([]uint16(nil), c.Roster...)
	return c, b.called
}
