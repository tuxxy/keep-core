package enforcement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Round is one agreed heartbeat round, derived from the canonical chain alone.
// Its anchor block is the heartbeat nonce (reading 1).
type Round struct {
	Number, Anchor, StartBlock uint64
	AnchorHash, Session        [32]byte
}

// Outcome is the durable record of one heartbeat: whether it was signed and
// which seats were authenticated present on its attempt.
type Outcome struct {
	Version uint8
	Nonce   uint64
	Signed  bool
	Present []uint16
	// Signature is the heartbeat's BIP340 signature when signed.
	Signature [64]byte
}

func outcomeID(wallet [32]byte, nonce uint64) [32]byte {
	raw := append([]byte("keep-core/frost/heartbeat-outcome/v1/"), wallet[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint64(raw, nonce))
}

func startID(wallet [32]byte, nonce uint64) [32]byte {
	raw := append([]byte("keep-core/frost/heartbeat-started/v1/"), wallet[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint64(raw, nonce))
}

// Records is the durable signing-record store of one wallet host.
type Records interface {
	RetainSigningRecord(context.Context, [32]byte, []byte) error
	ReadSigningRecord(context.Context, [32]byte) ([]byte, error)
}

// ReadOutcome loads the outcome of nonce, or store.ErrMissing.
func ReadOutcome(ctx context.Context, r Records, wallet [32]byte, nonce uint64) (Outcome, error) {
	raw, err := r.ReadSigningRecord(ctx, outcomeID(wallet, nonce))
	if err != nil {
		return Outcome{}, err
	}
	var o Outcome
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&o); err != nil {
		return Outcome{}, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || o.Version != 1 || o.Nonce != nonce {
		return Outcome{}, errors.New("invalid heartbeat outcome record")
	}
	return o, nil
}

// Runner runs one wallet host's heartbeats.
type Runner struct {
	Chain    Chain
	Schedule redemption.Schedule
	Wallet   dkg.Descriptor
	// Base carries the journal, worker, transport, selected seats and
	// LocalRegtest; the nonce, session and start block are set per round.
	Base     signing.HeartbeatConfig
	boundary func(string) error
	// sign is a test seam; nil runs the HeartbeatSigner.
	sign func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error)
}

func (r *Runner) step(name string) error {
	if r.boundary != nil {
		return r.boundary(name)
	}
	return nil
}

func (r *Runner) roundAt(ctx context.Context, number uint64) (Round, error) {
	if r.Schedule.FinalityBlocks == 0 || r.Schedule.RoundBlocks == 0 {
		return Round{}, errors.New("incomplete heartbeat schedule")
	}
	anchor := number * r.Schedule.RoundBlocks
	if anchor == 0 {
		return Round{}, errors.New("no heartbeat round at genesis")
	}
	hash, err := r.Chain.HeaderHash(ctx, anchor)
	if err != nil {
		return Round{}, err
	}
	wallet, bridge := dkg.WalletID(r.Wallet.OutputKey), r.Chain.Bridge()
	raw := append([]byte("keep-core/frost/heartbeat-session/v1/"), wallet[:]...)
	raw = binary.BigEndian.AppendUint64(raw, r.Chain.ChainID())
	raw = append(raw, bridge[:]...)
	raw = append(raw, hash[:]...)
	raw = binary.BigEndian.AppendUint64(raw, number)
	return Round{Number: number, Anchor: anchor, AnchorHash: hash, StartBlock: anchor + r.Schedule.FinalityBlocks, Session: sha256.Sum256(raw)}, nil
}

// Current returns the round whose final anchor is the latest one at head.
func (r *Runner) Current(ctx context.Context) (Round, error) {
	head, err := r.Chain.Head(ctx)
	if err != nil {
		return Round{}, err
	}
	if r.Schedule.RoundBlocks == 0 || head.Number < r.Schedule.FinalityBlocks {
		return Round{}, errors.New("chain shorter than finality")
	}
	return r.roundAt(ctx, (head.Number-r.Schedule.FinalityBlocks)/r.Schedule.RoundBlocks)
}

// AuthorizeHeartbeat admits only the current round's anchor as the nonce.
func (r *Runner) AuthorizeHeartbeat(ctx context.Context, wallet [32]byte, nonce uint64) error {
	if wallet != dkg.WalletID(r.Wallet.OutputKey) {
		return errors.New("heartbeat for another wallet")
	}
	current, err := r.Current(ctx)
	if err != nil {
		return err
	}
	if nonce != current.Anchor {
		return fmt.Errorf("heartbeat nonce %d is not the current round anchor %d", nonce, current.Anchor)
	}
	return nil
}

// ErrUnknownOutcome reports a round that started before a crash: it is never
// rerun and never counted as a failure.
var ErrUnknownOutcome = errors.New("heartbeat round started before a restart; outcome unknown")

// Run signs the round's heartbeat once and records its outcome.
func (r *Runner) Run(ctx context.Context, round Round) (Outcome, error) {
	wallet := dkg.WalletID(r.Wallet.OutputKey)
	if err := r.step("before-heartbeat"); err != nil {
		return Outcome{}, err
	}
	o, err := ReadOutcome(ctx, r.Base.Journal, wallet, round.Anchor)
	if err == nil {
		return o, nil
	}
	if !errors.Is(err, store.ErrMissing) {
		return Outcome{}, err
	}
	if _, err = r.Base.Journal.ReadSigningRecord(ctx, startID(wallet, round.Anchor)); err == nil {
		return Outcome{}, ErrUnknownOutcome
	} else if !errors.Is(err, store.ErrMissing) {
		return Outcome{}, err
	}
	if err = r.Base.Journal.RetainSigningRecord(ctx, startID(wallet, round.Anchor), []byte{1}); err != nil {
		return Outcome{}, err
	}
	c := r.Base
	c.Wallet, c.Nonce, c.Session, c.StartBlock, c.Authority = r.Wallet, round.Anchor, round.Session, round.StartBlock, r
	sign := r.sign
	if sign == nil {
		sign = func(ctx context.Context, c signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			s, err := signing.NewHeartbeatSigner(c)
			if err != nil {
				return signing.HeartbeatResult{}, err
			}
			return s.Run(ctx)
		}
	}
	result, err := sign(ctx, c)
	if errors.Is(err, signing.ErrUnauthorized) || (err != nil && len(result.Present) == 0) {
		// Refused, or failed before the attempt's transport ran: no presence
		// was observed, so no outcome is recorded.
		return Outcome{}, err
	}
	o = Outcome{Version: 1, Nonce: round.Anchor, Signed: err == nil, Present: result.Present, Signature: result.Done.Signature}
	raw, merr := json.Marshal(o)
	if merr != nil {
		return Outcome{}, merr
	}
	if merr = r.Base.Journal.RetainSigningRecord(context.WithoutCancel(ctx), outcomeID(wallet, round.Anchor), raw); merr != nil {
		return Outcome{}, merr
	}
	if merr = r.step("heartbeat-outcome-recorded"); merr != nil {
		return o, merr
	}
	return o, err
}
