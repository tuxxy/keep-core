package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
)

// Heartbeat is the K-08 heartbeat purpose. Its message is never supplied by
// a caller: the host computes it from (wallet, nonce) in the C-08 domain.
const Heartbeat = "heartbeat"

var heartbeatTag = sha256.Sum256([]byte("tbtc-v2/frost-heartbeat/v1"))

// HeartbeatDigest is C-08's heartbeatDigest: the BIP340-style tagged hash
// sha256(T ‖ T ‖ walletId ‖ uint64 nonce) with T = sha256(domain).
func HeartbeatDigest(wallet [32]byte, nonce uint64) [32]byte {
	raw := append(append(heartbeatTag[:], heartbeatTag[:]...), wallet[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint64(raw, nonce))
}

// HeartbeatAuthority admits a heartbeat nonce: the host signs only the
// current one. It is asked before the intent record and again after the
// durable claim.
type HeartbeatAuthority interface {
	AuthorizeHeartbeat(ctx context.Context, wallet [32]byte, nonce uint64) error
}

// Presence reports the seats whose authenticated ephemeral keys arrived in
// an attempt, including local seats.
type Presence interface{ Present() []uint16 }

type heartbeatIntent struct {
	Version                  uint8
	WalletID, DescriptorHash [32]byte
	Nonce                    uint64
}

func (i heartbeatIntent) hash() [32]byte {
	raw := append([]byte("keep-core/frost/heartbeat-intent/v1/"), i.WalletID[:]...)
	raw = append(raw, i.DescriptorHash[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint64(raw, i.Nonce))
}

func decodeHeartbeatIntent(raw []byte) (heartbeatIntent, error) {
	var i heartbeatIntent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&i); err != nil {
		return i, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || i.Version != 1 {
		return i, errors.New("invalid heartbeat intent")
	}
	return i, nil
}

// HeartbeatID is the Done reservation and outcome subject of one heartbeat.
func HeartbeatID(wallet [32]byte, nonce uint64) [32]byte {
	raw := append([]byte("keep-core/frost/heartbeat-id/v1/"), wallet[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint64(raw, nonce))
}

type HeartbeatConfig struct {
	Wallet     dkg.Descriptor
	Nonce      uint64
	Session    [32]byte
	StartBlock uint64
	Journal    Journal
	Authority  HeartbeatAuthority
	Worker     snowfallengine.WorkerConfig
	Selected   []uint16
	Transport  TransportFactory
	// LocalRegtest is the same deployment gate as the executor's.
	LocalRegtest bool
}

// HeartbeatSigner signs one heartbeat through the guarded engine.
type HeartbeatSigner struct {
	config   HeartbeatConfig
	intent   heartbeatIntent
	encoded  []byte
	engine   frost.Engine
	boundary func(string) error // Test-only replay hook. Nil in normal use.
}

type HeartbeatResult struct {
	Done Done
	// Present is the authenticated presence of the attempt, sorted; empty if
	// the transport does not report it.
	Present []uint16
}

func NewHeartbeatSigner(c HeartbeatConfig) (*HeartbeatSigner, error) {
	if !c.LocalRegtest || dkg.ValidateDescriptor(c.Wallet) != nil || c.Nonce == 0 || c.Session == ([32]byte{}) || c.StartBlock == 0 || c.Journal == nil || c.Authority == nil || c.Transport == nil || len(c.Selected) == 0 {
		return nil, errors.New("incomplete heartbeat configuration")
	}
	descriptorHash, err := dkg.DescriptorHash(c.Wallet)
	if err != nil {
		return nil, err
	}
	c.Selected = append([]uint16(nil), c.Selected...)
	intent := heartbeatIntent{Version: 1, WalletID: dkg.WalletID(c.Wallet.OutputKey), DescriptorHash: descriptorHash, Nonce: c.Nonce}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	engine, err := snowfallengine.NewGuarded(c.Worker, c.Journal)
	if err != nil {
		return nil, err
	}
	return &HeartbeatSigner{config: c, intent: intent, encoded: encoded, engine: engine}, nil
}

func (h *HeartbeatSigner) step(name string) error {
	if h.boundary != nil {
		return h.boundary(name)
	}
	return nil
}

// Digest is the only message this signer can release.
func (h *HeartbeatSigner) Digest() [32]byte { return HeartbeatDigest(h.intent.WalletID, h.intent.Nonce) }

func (h *HeartbeatSigner) attempt() (frost.Attempt, error) {
	id := h.intent.hash()
	raw := append([]byte("keep-core/frost/heartbeat-attempt/v1/"), id[:]...)
	raw = append(raw, h.config.Session[:]...)
	raw = binary.BigEndian.AppendUint32(raw, 0)
	return h.config.Journal.Domain().NewAttempt("sign", sha256.Sum256(raw), h.config.StartBlock)
}

func (h *HeartbeatSigner) authorize(ctx context.Context) (frost.KeyReady, []uint16, error) {
	key, locals, err := localWallet(ctx, h.config.Journal, h.intent.WalletID, h.intent.DescriptorHash, h.config.Wallet.OutputKey)
	if err != nil {
		return key, nil, err
	}
	if err = h.config.Authority.AuthorizeHeartbeat(ctx, h.intent.WalletID, h.intent.Nonce); err != nil {
		return key, nil, fmt.Errorf("%w: heartbeat nonce: %v", ErrUnauthorized, err)
	}
	return key, locals, nil
}

type heartbeatAuthorization struct {
	signer   *HeartbeatSigner
	expected frost.SigningRequest
}

// BeforeSigning runs after the durable claim. The message is recomputed
// from the intent the grant carries; a raw digest cannot pass.
func (g heartbeatAuthorization) BeforeSigning(ctx context.Context, r frost.SigningRequest) error {
	h := g.signer
	if !reflect.DeepEqual(r, g.expected) {
		return fmt.Errorf("%w: worker grant changed", ErrUnauthorized)
	}
	if err := h.step("before-worker-start"); err != nil {
		return err
	}
	intent, err := decodeHeartbeatIntent(r.Authorization)
	if err != nil || intent != h.intent || HeartbeatDigest(intent.WalletID, intent.Nonce) != r.Message {
		return fmt.Errorf("%w: heartbeat message is not in the heartbeat domain", ErrUnauthorized)
	}
	_, _, err = h.authorize(ctx)
	return err
}

// Run signs the heartbeat. The result carries the attempt's presence even
// when signing fails.
func (h *HeartbeatSigner) Run(ctx context.Context) (HeartbeatResult, error) {
	if ctx == nil {
		return HeartbeatResult{}, errors.New("heartbeat context required")
	}
	if err := h.step("heartbeat-nonce-seen"); err != nil {
		return HeartbeatResult{}, err
	}
	key, locals, err := h.authorize(ctx)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err = h.step("before-heartbeat-record"); err != nil {
		return HeartbeatResult{}, err
	}
	id := h.intent.hash()
	if err = h.config.Journal.RetainSigningRecord(ctx, id, h.encoded); err != nil {
		return HeartbeatResult{}, err
	}
	if err = h.step("before-defense-record"); err != nil {
		return HeartbeatResult{}, err
	}
	digest := h.Digest()
	defense, err := DefenseRecord{Version: 1, WalletID: h.intent.WalletID, Digest: digest, Purpose: Heartbeat, Kind: DefenseHeartbeat, Nonce: h.intent.Nonce}.Marshal()
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err = h.config.Journal.RetainSigningRecord(ctx, DefenseID(h.intent.WalletID, digest), defense); err != nil {
		return HeartbeatResult{}, err
	}
	attempt, err := h.attempt()
	if err != nil {
		return HeartbeatResult{}, err
	}
	tr, closeTransport, err := h.config.Transport(ctx, attempt)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if tr == nil || closeTransport == nil {
		return HeartbeatResult{}, errors.New("invalid signing transport factory")
	}
	defer closeTransport()
	present := func() []uint16 {
		p, ok := tr.(Presence)
		if !ok {
			return nil
		}
		seats := p.Present()
		sort.Slice(seats, func(i, j int) bool { return seats[i] < seats[j] })
		return seats
	}
	request := frost.SigningRequest{Group: frost.Group{Roster: append([]uint16(nil), key.Candidate.Roster...), Threshold: key.Candidate.Threshold, Quorum: uint16(len(key.Candidate.Roster)), Epoch: key.Candidate.Epoch}, LocalSeats: locals, Selected: append([]uint16(nil), h.config.Selected...), Attempt: attempt, Key: key, Message: digest, Authorization: append([]byte(nil), h.encoded...)}
	providers := frost.Providers{Transport: tr, Store: h.config.Journal, Acceptance: signingAcceptance{}, SigningAuthorization: heartbeatAuthorization{h, request}}
	raw, err := h.engine.Sign(ctx, request, providers)
	if err != nil {
		return HeartbeatResult{Present: present()}, err
	}
	done := Done{Context: DoneContext{Version: 1, Scheme: bitcoin.SignatureSchnorr, WalletID: h.intent.WalletID, DescriptorHash: h.intent.DescriptorHash, BitcoinGenesis: heartbeatGenesis, Plan: id, Reservation: HeartbeatID(h.intent.WalletID, h.intent.Nonce), Attempt: snowfallengine.AttemptID(attempt), Message: digest, Purpose: Heartbeat, StartBlock: attempt.StartBlock}, Signature: raw}
	if _, err = done.Verify(done.Context, h.config.Wallet.OutputKey); err != nil {
		return HeartbeatResult{Present: present()}, err
	}
	encoded, err := done.Marshal()
	if err != nil {
		return HeartbeatResult{Present: present()}, err
	}
	persistCtx, cancelPersistence := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	err = h.config.Journal.RetainSigningRecord(persistCtx, signingDoneID(done.Context.Attempt), encoded)
	cancelPersistence()
	if err != nil {
		return HeartbeatResult{Present: present()}, err
	}
	if err = h.step("after-signature"); err != nil {
		return HeartbeatResult{Present: present()}, err
	}
	return HeartbeatResult{Done: done, Present: present()}, nil
}

// heartbeatGenesis fills DoneContext.BitcoinGenesis, which a heartbeat does
// not bind: it marks the field as the heartbeat domain.
var heartbeatGenesis = heartbeatTag
