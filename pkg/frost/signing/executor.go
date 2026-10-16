package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

var ErrUnauthorized = errors.New("FROST transaction is not authorized")
var ErrLateSignature = errors.New("FROST signature retained in quarantine: reservation changed")

// ReservationView must be read at the current canonical head, from one
// internally consistent snapshot. The provider verifies the C-04 certificate
// and current request state; the executor cannot discover a hidden newer head. Matching
// an event alone is insufficient. The local test provider is not C-04 evidence.
type ReservationView struct {
	ID, Commitment, BlockHash, CanonicalBlockHash, HeadHash [32]byte
	State                                                   string
	Block, Head, Generation, StateVersion                   uint64
}
type Reservations interface {
	ReadReservation(context.Context, [32]byte) (ReservationView, error)
}

// Bitcoin reads by exact outpoint and complete script, never by HASH160 of Q.
// Transaction bytes are hashed and checked by the host. Confirmation/unspent
// answers remain an explicit chain-provider assumption.
type Bitcoin interface {
	Genesis(context.Context) ([32]byte, error)
	Transaction(context.Context, bitcoin.Hash) ([]byte, error)
	Confirmations(context.Context, bitcoin.Hash) (uint, error)
	Unspent(context.Context, bitcoin.TransactionOutpoint) (bool, error)
	Broadcast(context.Context, []byte) error
}

// Containment decides whether a Quarantined (contained) reservation may still
// be acted on. Hosts act on a contained reservation only under a governance
// directive (spec issue #3); the executor asks at every reservation read, so
// an admitted alternative cannot be granted or broadcast without one.
type Containment interface {
	AllowContained(ctx context.Context, plan *Plan, reservation [32]byte) error
}

// ConfirmedSpends answers from confirmed chain state only, ignoring the
// mempool. A cancellation exists to replace a payment that may sit
// unconfirmed in the mempool spending the same inputs, so its prevout check
// must not count that spend. Providers without it cannot sign cancellations.
type ConfirmedSpends interface {
	ConfirmedUnspent(context.Context, bitcoin.TransactionOutpoint) (bool, error)
}

type Journal interface {
	dkg.Journal
	RetainSigningRecord(context.Context, [32]byte, []byte) error
	ReadSigningRecord(context.Context, [32]byte) ([]byte, error)
}
type TransportFactory func(context.Context, frost.Attempt) (frost.Transport, func(), error)
type Config struct {
	Plan                       *Plan
	Reservation, Session       [32]byte
	StartBlock, FinalityBlocks uint64
	MinimumConfirmations       uint
	Journal                    Journal
	Reservations               Reservations
	Containment                Containment    // directive gate for Quarantined; nil refuses
	Sweeps                     SweepAuthority // K-06 sweep plans, in place of Reservations
	Guard                      GrantGuard     // K-06 main purpose lock; nil before K-06
	Bitcoin                    Bitcoin
	Worker                     snowfallengine.WorkerConfig
	Selected                   []uint16
	Transport                  TransportFactory
	// LocalRegtest is explicit. K-04 has no production C-04 adapter or dispatch.
	LocalRegtest bool
}
type Executor struct {
	config   Config
	engine   frost.Engine
	mu       sync.Mutex
	started  bool
	receipt  *ReservationReceipt
	boundary func(string) error // Test-only replay hook. Nil in normal use.
}
type Result struct {
	Plan        [32]byte
	Receipt     ReservationReceipt
	Transaction *bitcoin.Transaction
	Done        []Done
}

func NewLocalExecutor(c Config) (*Executor, error) {
	if !c.LocalRegtest || c.Plan == nil || c.Plan.Genesis() != [32]byte(*chaincfg.RegressionNetParams.GenesisHash) || c.Reservation == ([32]byte{}) || c.Session == ([32]byte{}) || c.StartBlock == 0 || c.FinalityBlocks == 0 || c.MinimumConfirmations == 0 || c.Journal == nil || (c.Plan.IsSweep() && c.Sweeps == nil) || (!c.Plan.IsSweep() && c.Reservations == nil) || c.Bitcoin == nil || c.Transport == nil {
		return nil, errors.New("incomplete local signing configuration")
	}
	c.Selected = append([]uint16(nil), c.Selected...)
	engine, err := snowfallengine.NewGuarded(c.Worker, c.Journal)
	if err != nil {
		return nil, err
	}
	return &Executor{config: c, engine: engine}, nil
}
func (e *Executor) step(name string) error {
	if e.boundary != nil {
		return e.boundary(name)
	}
	return nil
}
func (e *Executor) Plan() *Plan { return e.config.Plan }

type ReservationReceipt struct {
	Block uint64
	Hash  [32]byte
}

// Unresolved C-04 intent states. Only Reserved grants new signing; the others
// keep historical acts recoverable (K-05 rebroadcast and cancellation).
const (
	stateReserved       = "Reserved"
	stateTimeoutPending = "TimeoutPending"
	stateQuarantined    = "Quarantined"
)

func unresolved(state string) bool {
	return state == stateReserved || state == stateTimeoutPending || state == stateQuarantined
}

func (e *Executor) reservation(ctx context.Context, initialize bool) error {
	p := e.config.Plan
	if p.sweep != nil {
		receipt, err := e.config.Sweeps.AuthorizeSweep(ctx, p)
		if err != nil {
			return fmt.Errorf("%w: sweep authorization: %v", ErrUnauthorized, err)
		}
		if receipt.Block == 0 || receipt.Hash == ([32]byte{}) {
			return fmt.Errorf("%w: sweep authorization without an anchor", ErrUnauthorized)
		}
		return e.pinReceipt(ctx, initialize, receipt)
	}
	if p.cancellation != nil {
		return e.reservationCheck(ctx, initialize, unresolved)
	}
	return e.reservationCheck(ctx, initialize, func(state string) bool { return state == stateReserved })
}

func (e *Executor) reservationCheck(ctx context.Context, initialize bool, allowed func(string) bool) error {
	if err := e.step("before-reservation-read"); err != nil {
		return err
	}
	v, err := e.config.Reservations.ReadReservation(ctx, e.config.Reservation)
	if err != nil {
		return fmt.Errorf("%w: reservation read: %v", ErrUnauthorized, err)
	}
	p := e.config.Plan
	if v.ID != e.config.Reservation || v.Commitment != p.PaymentCommitment() || v.Generation != p.intent.Generation || v.StateVersion != p.intent.StateVersion || !allowed(v.State) || v.Block == 0 || v.BlockHash == ([32]byte{}) || v.BlockHash != v.CanonicalBlockHash || v.HeadHash == ([32]byte{}) || v.Head < v.Block || v.Head-v.Block < e.config.FinalityBlocks {
		return fmt.Errorf("%w: missing, unfinalized or changed reservation", ErrUnauthorized)
	}
	if v.State == stateQuarantined {
		if e.config.Containment == nil {
			return fmt.Errorf("%w: contained reservation without a governance directive", ErrUnauthorized)
		}
		if err := e.config.Containment.AllowContained(ctx, p, e.config.Reservation); err != nil {
			return fmt.Errorf("%w: contained reservation: %v", ErrUnauthorized, err)
		}
	}
	receipt := ReservationReceipt{Block: v.Block, Hash: v.BlockHash}
	if p.cancellation != nil {
		// A cancellation grant additionally requires its own finalized C-04
		// admission for the same reservation, snapshot and fee.
		reader, ok := e.config.Reservations.(Cancellations)
		if !ok {
			return fmt.Errorf("%w: provider cannot read cancellation admissions", ErrUnauthorized)
		}
		c, err := reader.ReadCancellation(ctx, e.config.Reservation, p.cancellation.TransactionHash)
		if err != nil {
			return fmt.Errorf("%w: cancellation read: %v", ErrUnauthorized, err)
		}
		if c.Reservation != e.config.Reservation || c.TransactionHash != p.cancellation.TransactionHash || c.Snapshot != p.cancellation.Snapshot || c.Fee != uint64(p.cancellation.Fee) || c.Block == 0 || c.BlockHash == ([32]byte{}) || c.BlockHash != c.CanonicalBlockHash || v.Head < c.Block || v.Head-c.Block < e.config.FinalityBlocks {
			return fmt.Errorf("%w: missing, unfinalized or changed cancellation admission", ErrUnauthorized)
		}
		receipt = ReservationReceipt{Block: c.Block, Hash: c.BlockHash}
	}
	if err := e.pinReceipt(ctx, initialize, receipt); err != nil {
		return err
	}
	return e.step("reservation-seen")
}

// pinReceipt retains the first authorization anchor durably and refuses any
// later read whose anchor differs (a reorganization or a changed record).
func (e *Executor) pinReceipt(ctx context.Context, initialize bool, receipt ReservationReceipt) error {
	retained, err := e.loadReceipt(ctx)
	if errors.Is(err, store.ErrMissing) && initialize {
		raw, marshalErr := json.Marshal(receipt)
		if marshalErr != nil {
			return marshalErr
		}
		if err = e.config.Journal.RetainSigningRecord(ctx, e.receiptID(), raw); err != nil {
			return err
		}
		retained = receipt
	} else if err != nil {
		return fmt.Errorf("%w: missing or unreadable reservation receipt: %v", ErrUnauthorized, err)
	}
	if retained != receipt {
		return fmt.Errorf("%w: durable reservation receipt changed", ErrUnauthorized)
	}
	e.mu.Lock()
	if e.receipt != nil && *e.receipt != receipt {
		e.mu.Unlock()
		return fmt.Errorf("%w: reservation receipt changed", ErrUnauthorized)
	}
	if e.receipt == nil {
		e.receipt = &receipt
	}
	e.mu.Unlock()
	return nil
}
func (e *Executor) wallet(ctx context.Context) (frost.KeyReady, []uint16, error) {
	w, err := e.config.Journal.LoadWallet(ctx)
	if err != nil {
		return frost.KeyReady{}, nil, err
	}
	p := e.config.Plan
	if w.Quarantined || w.State != "ReadyUnfunded" || w.ID != p.intent.WalletID || w.DescriptorHash != p.intent.DescriptorHash || w.Descriptor.OutputKey != p.snapshot.OutputKey() {
		return frost.KeyReady{}, nil, fmt.Errorf("%w: wallet identity or readiness", ErrUnauthorized)
	}
	key, err := e.config.Journal.LoadKey(ctx)
	if err != nil {
		return key, nil, err
	}
	status, err := e.config.Journal.DKGStatus(ctx)
	if err != nil {
		return key, nil, err
	}
	if status.State != frost.DKGKeyStored || len(status.LocalSeats) == 0 || key.Candidate.OutputKey != w.Descriptor.OutputKey || key.Candidate.Descriptor != w.Descriptor.SnowfallDescriptor || key.Candidate.Epoch != w.Descriptor.Epoch || key.Candidate.Threshold != w.Descriptor.Threshold {
		return key, nil, fmt.Errorf("%w: durable key mismatch", ErrUnauthorized)
	}
	return key, append([]uint16(nil), status.LocalSeats...), nil
}
func (e *Executor) bitcoin(ctx context.Context) error {
	genesis, err := e.config.Bitcoin.Genesis(ctx)
	if err != nil || genesis != e.config.Plan.Genesis() {
		return fmt.Errorf("%w: Bitcoin network", ErrUnauthorized)
	}
	for _, p := range e.config.Plan.snapshot.PreviousOutputs() {
		raw, err := e.config.Bitcoin.Transaction(ctx, p.Outpoint.TransactionHash)
		if err != nil {
			return fmt.Errorf("%w: missing prevout transaction", ErrUnauthorized)
		}
		if len(raw) == 0 || len(raw) > 100000 {
			return fmt.Errorf("%w: prevout transaction size", ErrUnauthorized)
		}
		tx, decodeErr := decodePreviousTransaction(raw)
		if decodeErr != nil || tx.Hash() != p.Outpoint.TransactionHash || int(p.Outpoint.OutputIndex) >= len(tx.Outputs) {
			return fmt.Errorf("%w: prevout transaction hash/index", ErrUnauthorized)
		}
		out := tx.Outputs[p.Outpoint.OutputIndex]
		if out.Value != p.Value || !bytes.Equal(out.PublicKeyScript, p.PublicKeyScript) {
			return fmt.Errorf("%w: previous value/script changed", ErrUnauthorized)
		}
		confirmations, err := e.config.Bitcoin.Confirmations(ctx, p.Outpoint.TransactionHash)
		if err != nil || confirmations < e.config.MinimumConfirmations {
			return fmt.Errorf("%w: unconfirmed prevout", ErrUnauthorized)
		}
		unspent, err := e.unspent(ctx, p.Outpoint)
		if err != nil || !unspent {
			return fmt.Errorf("%w: spent prevout", ErrUnauthorized)
		}
	}
	return nil
}

// unspent is the prevout check: mempool-inclusive for every plan except a
// cancellation, which checks confirmed state only (see ConfirmedSpends).
func (e *Executor) unspent(ctx context.Context, o bitcoin.TransactionOutpoint) (bool, error) {
	if e.config.Plan.cancellation == nil {
		return e.config.Bitcoin.Unspent(ctx, o)
	}
	confirmed, ok := e.config.Bitcoin.(ConfirmedSpends)
	if !ok {
		return false, errors.New("Bitcoin provider cannot exclude mempool spends")
	}
	return confirmed.ConfirmedUnspent(ctx, o)
}

func (e *Executor) attempt(index int) (frost.Attempt, error) {
	c := e.config
	domain := "keep-core/frost/transaction-attempt/v1/"
	if c.Plan.cancellation != nil {
		domain = "keep-core/frost/cancellation-attempt/v1/"
	}
	if c.Plan.sweep != nil {
		domain = "keep-core/frost/sweep-attempt/v1/"
	}
	raw := append([]byte(domain), c.Plan.hash[:]...)
	raw = append(raw, c.Reservation[:]...)
	raw = append(raw, c.Session[:]...)
	raw = binary.BigEndian.AppendUint32(raw, uint32(index))
	return c.Journal.Domain().NewAttempt("sign", sha256.Sum256(raw), c.StartBlock)
}
func (e *Executor) doneContext(index int, attempt frost.Attempt) DoneContext {
	p := e.config.Plan
	return DoneContext{Version: 1, Scheme: bitcoin.SignatureSchnorr, WalletID: p.intent.WalletID, DescriptorHash: p.intent.DescriptorHash, BitcoinGenesis: p.intent.BitcoinGenesis, Plan: p.hash, Reservation: e.config.Reservation, Attempt: snowfallengine.AttemptID(attempt), Message: p.snapshot.Digests()[index], Purpose: p.intent.Purpose, Input: uint32(index), StartBlock: attempt.StartBlock}
}

type finalAuthorization struct {
	executor *Executor
	expected frost.SigningRequest
	input    int
}

func (g finalAuthorization) BeforeSigning(ctx context.Context, r frost.SigningRequest) error {
	e := g.executor
	if !reflect.DeepEqual(r, g.expected) {
		return fmt.Errorf("%w: worker grant changed", ErrUnauthorized)
	}
	if err := e.step("before-worker-start"); err != nil {
		return err
	}
	if _, _, err := e.wallet(ctx); err != nil {
		return err
	}
	if err := e.bitcoin(ctx); err != nil {
		return err
	}
	if err := e.reservation(ctx, false); err != nil {
		return err
	}
	if e.config.Guard != nil {
		if err := e.config.Guard.Grant(ctx, e.config.Plan, e.config.Reservation, g.input); err != nil {
			return fmt.Errorf("%w: main purpose lock: %v", ErrUnauthorized, err)
		}
	}
	return nil
}

type signingAcceptance struct{}

func (signingAcceptance) WaitCandidateAcceptance(context.Context, frost.Candidate) (frost.Receipt, error) {
	return frost.Receipt{}, errors.New("signing cannot admit a DKG candidate")
}

func (e *Executor) Run(ctx context.Context) (*Result, error) {
	if ctx == nil {
		return nil, errors.New("signing context required")
	}
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return nil, errors.New("signing executor is terminal; use a fresh agreed attempt")
	}
	e.started = true
	e.mu.Unlock()
	if err := e.step("transaction-frozen"); err != nil {
		return nil, err
	}
	key, locals, err := e.wallet(ctx)
	if err != nil {
		return nil, err
	}
	if err = e.bitcoin(ctx); err != nil {
		return nil, err
	}
	if err = e.reservation(ctx, true); err != nil {
		return nil, err
	}
	if e.config.Guard != nil {
		if err = e.config.Guard.Permit(ctx, e.config.Plan, e.config.Reservation); err != nil {
			return nil, fmt.Errorf("%w: main purpose lock: %v", ErrUnauthorized, err)
		}
	}
	if err = e.step("before-intent-record"); err != nil {
		return nil, err
	}
	p := e.config.Plan
	if err = e.config.Journal.RetainSigningRecord(ctx, p.hash, p.encoded); err != nil {
		return nil, err
	}
	e.mu.Lock()
	result := &Result{Plan: p.hash, Receipt: *e.receipt}
	e.mu.Unlock()
	var signatures []bitcoin.Signature
	for index, digest := range p.snapshot.Digests() {
		attempt, err := e.attempt(index)
		if err != nil {
			return nil, err
		}
		tr, closeTransport, err := e.config.Transport(ctx, attempt)
		if err != nil {
			return nil, err
		}
		if tr == nil || closeTransport == nil {
			return nil, errors.New("invalid signing transport factory")
		}
		request := frost.SigningRequest{Group: frost.Group{Roster: append([]uint16(nil), key.Candidate.Roster...), Threshold: key.Candidate.Threshold, Quorum: uint16(len(key.Candidate.Roster)), Epoch: key.Candidate.Epoch}, LocalSeats: locals, Selected: append([]uint16(nil), e.config.Selected...), Attempt: attempt, Key: key, Message: digest, Authorization: p.IntentBytes()}
		providers := frost.Providers{Transport: tr, Store: e.config.Journal, Acceptance: signingAcceptance{}, SigningAuthorization: finalAuthorization{e, request, index}}
		raw, err := e.engine.Sign(ctx, request, providers)
		if err != nil {
			closeTransport()
			return nil, err
		}
		done := Done{Context: e.doneContext(index, attempt), Signature: raw}
		signature, err := done.Verify(done.Context, p.snapshot.OutputKey())
		if err != nil {
			closeTransport()
			return nil, err
		}
		// Retain even a now-late signature before checking whether it may be used.
		encoded, err := done.Marshal()
		if err != nil {
			closeTransport()
			return nil, err
		}
		// A returned signature cannot be revoked by cancellation. Persist it
		// before cleanup, with a bounded context that keeps caller values but
		// does not inherit its cancellation. The store still enforces fencing.
		persistCtx, cancelPersistence := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err = e.config.Journal.RetainSigningRecord(persistCtx, signingDoneID(done.Context.Attempt), encoded)
		cancelPersistence()
		closeTransport()
		if err != nil {
			return nil, err
		}
		if err = e.step("after-signature"); err != nil {
			return nil, err
		}
		if err = e.step("before-done-accept"); err != nil {
			return nil, err
		}
		if err = e.reservation(ctx, false); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrLateSignature, err)
		}
		signatures = append(signatures, signature)
		result.Done = append(result.Done, done)
	}
	result.Transaction, err = p.snapshot.AddSignatures(signatures)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	// Session-bound result records permit recovery without generating a new signature.
	id := e.resultID()
	if err = e.config.Journal.RetainSigningRecord(ctx, id, encoded); err != nil {
		return nil, err
	}
	return result, nil
}
func (e *Executor) resultID() [32]byte {
	raw := append([]byte("keep-core/frost/signed-transaction/v1/"), e.config.Plan.hash[:]...)
	raw = append(raw, e.config.Reservation[:]...)
	raw = append(raw, e.config.Session[:]...)
	raw = binary.BigEndian.AppendUint64(raw, e.config.StartBlock)
	return sha256.Sum256(raw)
}
func (e *Executor) ValidateResult(result *Result) error {
	p := e.config.Plan
	if result == nil || result.Receipt.Block == 0 || result.Receipt.Hash == ([32]byte{}) || result.Plan != p.hash || len(result.Done) != len(p.snapshot.Digests()) {
		return errors.New("signed result context mismatch")
	}
	for i, done := range result.Done {
		attempt, err := e.attempt(i)
		if err != nil {
			return err
		}
		if _, err = done.Verify(e.doneContext(i, attempt), p.snapshot.OutputKey()); err != nil {
			return err
		}
		if result.Transaction == nil || i >= len(result.Transaction.Inputs) || result.Transaction.Inputs[i] == nil || len(result.Transaction.Inputs[i].Witness) == 0 || !bytes.Equal(result.Transaction.Inputs[i].Witness[0], done.Signature[:]) {
			return errors.New("done signature differs from transaction witness")
		}
	}
	return p.snapshot.ValidateSigned(result.Transaction)
}
func (e *Executor) LoadResult(ctx context.Context) (*Result, error) {
	raw, err := e.config.Journal.ReadSigningRecord(ctx, e.resultID())
	if errors.Is(err, store.ErrMissing) {
		return e.recoverCompletedResult(ctx)
	}
	if err != nil {
		return nil, err
	}
	result := new(Result)
	if err = json.Unmarshal(raw, result); err != nil {
		return nil, err
	}
	if err = e.ValidateResult(result); err != nil {
		return nil, err
	}
	if err = e.checkResultReceipt(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func signingDoneID(attempt [32]byte) [32]byte {
	return sha256.Sum256(append([]byte("keep-core/frost/signing-done/v1/"), attempt[:]...))
}

// recoverCompletedResult joins already durable completions. It never starts a
// worker or retries a claimed attempt. Incomplete or late data stays retained
// for K-05 resolution; it is not a grant to finish missing inputs.
func (e *Executor) recoverCompletedResult(ctx context.Context) (*Result, error) {
	p := e.config.Plan
	intent, err := e.config.Journal.ReadSigningRecord(ctx, p.hash)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(intent, p.encoded) {
		return nil, errors.New("durable signing intent mismatch")
	}
	receipt, err := e.loadReceipt(ctx)
	if err != nil {
		return nil, err
	}
	result := &Result{Plan: p.hash, Receipt: receipt}
	var signatures []bitcoin.Signature
	for index := range p.snapshot.Digests() {
		attempt, err := e.attempt(index)
		if err != nil {
			return nil, err
		}
		expected := e.doneContext(index, attempt)
		raw, err := e.config.Journal.ReadSigningRecord(ctx, signingDoneID(expected.Attempt))
		if err != nil {
			return nil, err
		}
		var done Done
		if err := done.Unmarshal(raw); err != nil {
			return nil, err
		}
		signature, err := done.Verify(expected, p.snapshot.OutputKey())
		if err != nil {
			return nil, err
		}
		result.Done = append(result.Done, done)
		signatures = append(signatures, signature)
	}
	if err := e.reservation(ctx, false); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLateSignature, err)
	}
	result.Transaction, err = p.snapshot.AddSignatures(signatures)
	if err != nil {
		return nil, err
	}
	if err := e.ValidateResult(result); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if err := e.config.Journal.RetainSigningRecord(ctx, e.resultID(), encoded); err != nil {
		return nil, err
	}
	return result, nil
}
func (e *Executor) Broadcast(ctx context.Context, result *Result) error {
	// Copy before calling an external provider: its callbacks must not change
	// the bytes that passed validation by mutating the caller's result.
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	frozen := new(Result)
	if err = json.Unmarshal(encoded, frozen); err != nil {
		return err
	}
	result = frozen

	if err := eValidateContext(ctx); err != nil {
		return err
	}
	if err := e.step("before-broadcast"); err != nil {
		return err
	}
	if err := e.ValidateResult(result); err != nil {
		return err
	}
	if err := e.checkResultReceipt(ctx, result); err != nil {
		return err
	}
	if err := e.reservation(ctx, false); err != nil {
		return err
	}
	// Serialize a validated private copy. Callers must not mutate result concurrently.
	raw := append([]byte(nil), result.Transaction.Serialize(bitcoin.Witness)...)
	return e.config.Bitcoin.Broadcast(ctx, raw)
}

// Rebroadcast resends a durable, fully validated payment result after its
// reservation left Reserved (TimeoutPending or Quarantined). It never starts a
// worker, never joins partial or late completions, and requires the result
// record that Run writes only after every input was accepted while Reserved.
// Whether an honest host should do this is spec issue #3 (OPEN); callers gate it
// with an explicit policy.
func (e *Executor) Rebroadcast(ctx context.Context) error {
	if err := eValidateContext(ctx); err != nil {
		return err
	}
	if e.config.Plan.cancellation != nil || e.config.Plan.sweep != nil {
		return errors.New("rebroadcast applies to payment results only")
	}
	raw, err := e.config.Journal.ReadSigningRecord(ctx, e.resultID())
	if err != nil {
		return fmt.Errorf("%w: no durable accepted result: %v", ErrUnauthorized, err)
	}
	result := new(Result)
	if err = json.Unmarshal(raw, result); err != nil {
		return err
	}
	if err = e.step("before-rebroadcast"); err != nil {
		return err
	}
	if err = e.ValidateResult(result); err != nil {
		return err
	}
	if err = e.checkResultReceipt(ctx, result); err != nil {
		return err
	}
	if err = e.reservationCheck(ctx, false, unresolved); err != nil {
		return err
	}
	return e.config.Bitcoin.Broadcast(ctx, append([]byte(nil), result.Transaction.Serialize(bitcoin.Witness)...))
}

// Result returns the durable accepted result record without recovery.
func (e *Executor) Result(ctx context.Context) (*Result, error) {
	raw, err := e.config.Journal.ReadSigningRecord(ctx, e.resultID())
	if err != nil {
		return nil, err
	}
	result := new(Result)
	if err = json.Unmarshal(raw, result); err != nil {
		return nil, err
	}
	if err = e.ValidateResult(result); err != nil {
		return nil, err
	}
	return result, e.checkResultReceipt(ctx, result)
}

// PossibleRelease reports whether any input's signature completion is durable
// for this plan and session, i.e. a share may have been released.
func (e *Executor) PossibleRelease(ctx context.Context) (bool, error) {
	for index := range e.config.Plan.snapshot.Digests() {
		attempt, err := e.attempt(index)
		if err != nil {
			return false, err
		}
		_, err = e.config.Journal.ReadSigningRecord(ctx, signingDoneID(snowfallengine.AttemptID(attempt)))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, store.ErrMissing) {
			return true, err
		}
	}
	return false, nil
}

func eValidateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context required")
	}
	return ctx.Err()
}

func (e *Executor) receiptID() [32]byte {
	raw := append([]byte("keep-core/frost/reservation-receipt/v1/"), e.config.Plan.hash[:]...)
	raw = append(raw, e.config.Reservation[:]...)
	return sha256.Sum256(raw)
}
func (e *Executor) loadReceipt(ctx context.Context) (ReservationReceipt, error) {
	raw, err := e.config.Journal.ReadSigningRecord(ctx, e.receiptID())
	if err != nil {
		return ReservationReceipt{}, err
	}
	var receipt ReservationReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return receipt, err
	}
	if receipt.Block == 0 || receipt.Hash == ([32]byte{}) {
		return receipt, errors.New("invalid durable receipt")
	}
	return receipt, nil
}
func (e *Executor) checkResultReceipt(ctx context.Context, result *Result) error {
	receipt, err := e.loadReceipt(ctx)
	if err != nil || result.Receipt != receipt {
		return fmt.Errorf("%w: result does not match durable reservation receipt", ErrUnauthorized)
	}
	return nil
}
