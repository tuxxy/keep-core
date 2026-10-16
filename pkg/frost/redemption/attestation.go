package redemption

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// ErrConflictingAttestation is returned when a different intent is offered
// for an attested (wallet, generation, stateVersion) slot that may still be,
// or has been, admitted.
var ErrConflictingAttestation = errors.New("conflicting FROST intent attestation refused")

// ErrContained refuses a new cancellation for a Quarantined reservation
// without a governance directive to cancel.
var ErrContained = errors.New("FROST reservation is contained: governance directive required")

var (
	reservationDomain  = crypto.Keccak256Hash([]byte("tbtc-v2/frost-redemption-reservation/v2"))
	cancellationDomain = crypto.Keccak256Hash([]byte("tbtc-v2/frost-reservation-cancellation/v1"))
)

func word(v uint64) []byte { return common32(new(big.Int).SetUint64(v)) }
func common32(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}
func address32(a [20]byte) []byte { return append(make([]byte, 12), a[:]...) }

// RequestIDsHash is keccak256(abi.encode(bytes32[] ids)) in plan order.
func RequestIDsHash(ids [][32]byte) ([32]byte, error) {
	t, err := abi.NewType("bytes32[]", "", nil)
	if err != nil {
		return [32]byte{}, err
	}
	packed, err := abi.Arguments{{Type: t}}.Pack(ids)
	if err != nil {
		return [32]byte{}, err
	}
	return crypto.Keccak256Hash(packed), nil
}

// CertificateFor builds the reservation tuple for a plan.
func CertificateFor(plan *signing.Plan, validUntil uint64) (Certificate, error) {
	var ids [][32]byte
	for _, r := range plan.Requests() {
		ids = append(ids, r.ID)
	}
	idsHash, err := RequestIDsHash(ids)
	if err != nil {
		return Certificate{}, err
	}
	fees := plan.Fees()
	conflict := plan.ConflictInput()
	return Certificate{WalletID: plan.WalletID(), DescriptorHash: plan.DescriptorHash(), Generation: plan.Generation(), StateVersion: plan.StateVersion(), IntentCommitment: plan.Commitment(), RequestIDsHash: idsHash, ConflictTxHash: conflict.TransactionHash, ConflictOutputIndex: conflict.OutputIndex, MinimumFee: uint64(fees.Minimum), MaximumFee: uint64(fees.Maximum), MaximumSatPerVByte: uint64(fees.MaximumSatPerVByte), ValidUntilBlock: validUntil}, nil
}

// ReservationDigest is the C-04 certificate digest
// keccak256(abi.encode(DOMAIN, chainid, bridge, reservation)).
func ReservationDigest(chainID uint64, bridge [20]byte, c Certificate) [32]byte {
	b := append([]byte(nil), reservationDomain[:]...)
	b = append(b, word(chainID)...)
	b = append(b, address32(bridge)...)
	b = append(b, c.WalletID[:]...)
	b = append(b, c.DescriptorHash[:]...)
	b = append(b, word(c.Generation)...)
	b = append(b, word(c.StateVersion)...)
	b = append(b, c.IntentCommitment[:]...)
	b = append(b, c.RequestIDsHash[:]...)
	b = append(b, c.ConflictTxHash[:]...)
	b = append(b, word(uint64(c.ConflictOutputIndex))...)
	b = append(b, word(c.MinimumFee)...)
	b = append(b, word(c.MaximumFee)...)
	b = append(b, word(c.MaximumSatPerVByte)...)
	b = append(b, word(c.ValidUntilBlock)...)
	return crypto.Keccak256Hash(b)
}

// CancellationDigest is the C-04 cancellation certificate digest.
func CancellationDigest(chainID uint64, bridge [20]byte, a CancellationAuthorization) [32]byte {
	b := append([]byte(nil), cancellationDomain[:]...)
	b = append(b, word(chainID)...)
	b = append(b, address32(bridge)...)
	b = append(b, a.ReservationID[:]...)
	b = append(b, a.Snapshot[:]...)
	b = append(b, a.TransactionHash[:]...)
	b = append(b, word(a.Fee)...)
	b = append(b, word(a.ValidUntilBlock)...)
	return crypto.Keccak256Hash(b)
}

// SignDigest returns the 65-byte EIP-191 personal_sign signature (v = 27/28).
func SignDigest(key *ecdsa.PrivateKey, digest [32]byte) ([]byte, error) {
	prefixed := crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n32"), digest[:])
	sig, err := crypto.Sign(prefixed[:], key)
	if err != nil {
		return nil, err
	}
	sig[64] += 27
	return sig, nil
}

// Statement is one attested certificate for one slot.
type Statement struct {
	Certificate Certificate
	Plan        [32]byte
}

func lockID(wallet [32]byte, generation, stateVersion uint64, index uint32) [32]byte {
	raw := append([]byte("keep-core/frost/attestation-lock/v1/"), wallet[:]...)
	raw = binary.BigEndian.AppendUint64(raw, generation)
	raw = binary.BigEndian.AppendUint64(raw, stateVersion)
	raw = binary.BigEndian.AppendUint32(raw, index)
	return sha256.Sum256(raw)
}

// Attester is one operator's honest attestation path. It holds the operator's
// Ethereum key, which signs only after the independent plan check and the
// durable slot lock.
type Attester struct {
	Planner Planner
	Journal RecordJournal
	Ledger  Ledger
	Key     *ecdsa.PrivateKey
	// Seats are this operator's seats in the frozen descriptor.
	Seats []uint16
	// Reload checks the completed key in a fresh worker immediately before
	// attesting (C-04 honest-host rule). Nil only in unit tests.
	Reload func(context.Context) error
	// Directives are governance instructions for Quarantined reservations.
	// Without a cancel directive no cancellation is attested for them.
	Directives Directives
	boundary   func(string) error
}

func (a *Attester) step(name string) error {
	if a.boundary != nil {
		return a.boundary(name)
	}
	return nil
}

// History returns every attested statement for the slot in order.
func (a *Attester) History(ctx context.Context, wallet [32]byte, generation, stateVersion uint64) ([]Statement, error) {
	var out []Statement
	for i := uint32(0); ; i++ {
		raw, err := a.Journal.ReadSigningRecord(ctx, lockID(wallet, generation, stateVersion, i))
		if errors.Is(err, store.ErrMissing) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		var s Statement
		if err = json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
}

// lock is the durable compare-and-insert rule of INTENT-V2 / ADMISSION-SEMANTICS:
// an identical statement is idempotent; a same-intent renewal with a later
// deadline is appended; a different intent is appended only after every prior
// inclusive deadline is finalized and finalized canonical state shows that the
// slot was never admitted. History is never rewritten.
func (a *Attester) lock(ctx context.Context, finalized Head, s Statement) error {
	c := s.Certificate
	history, err := a.History(ctx, c.WalletID, c.Generation, c.StateVersion)
	if err != nil {
		return err
	}
	for _, old := range history {
		if old == s {
			return nil
		}
	}
	if len(history) > 0 {
		last := history[len(history)-1]
		renewal := last.Plan == s.Plan && last.Certificate.IntentCommitment == c.IntentCommitment
		if renewal {
			prior := last.Certificate
			prior.ValidUntilBlock = c.ValidUntilBlock
			if prior != c || c.ValidUntilBlock <= last.Certificate.ValidUntilBlock {
				return ErrConflictingAttestation
			}
		} else {
			for _, old := range history {
				if old.Certificate.ValidUntilBlock > finalized.Number {
					return fmt.Errorf("%w: prior deadline %d not finalized", ErrConflictingAttestation, old.Certificate.ValidUntilBlock)
				}
			}
			custody, err := a.Planner.Chain.Custody(ctx, finalized.Number, c.WalletID)
			if err != nil {
				return err
			}
			if custody.Generation >= c.Generation || custody.StateVersion != c.StateVersion {
				return fmt.Errorf("%w: slot admitted or superseded", ErrConflictingAttestation)
			}
		}
	}
	if len(history) >= defaultCapacity {
		return fmt.Errorf("%w: attestation history capacity reached", ErrConflictingAttestation)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return a.Journal.RetainSigningRecord(ctx, lockID(c.WalletID, c.Generation, c.StateVersion, uint32(len(history))), raw)
}

// Attest validates the plan against finalized chain state, records the slot
// lock durably, then signs the certificate once per local seat in the given
// selection. It returns seat -> 65-byte signature.
func (a *Attester) Attest(ctx context.Context, plan *signing.Plan, validUntil uint64) (map[uint16][]byte, error) {
	head, err := a.Planner.Chain.Head(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := a.Planner.Chain.Configuration(ctx, head.Number)
	if err != nil {
		return nil, err
	}
	if head.Number < cfg.FinalityBlocks {
		return nil, errors.New("chain shorter than finality")
	}
	finalized := Head{Number: head.Number - cfg.FinalityBlocks}
	if finalized.Hash, err = a.Planner.Chain.HeaderHash(ctx, finalized.Number); err != nil {
		return nil, err
	}
	if validUntil < head.Number || validUntil > head.Number+cfg.MaximumCertificateBlocks {
		return nil, errors.New("certificate deadline outside the admissible window")
	}
	if err = a.Planner.Validate(ctx, head.Number, plan); err != nil {
		return nil, fmt.Errorf("plan refused: %w", err)
	}
	if a.Reload != nil {
		if err = a.Reload(ctx); err != nil {
			return nil, fmt.Errorf("completed key reload: %w", err)
		}
	}
	certificate, err := CertificateFor(plan, validUntil)
	if err != nil {
		return nil, err
	}
	if err = a.step("before-attest-lock"); err != nil {
		return nil, err
	}
	if err = a.lock(ctx, finalized, Statement{Certificate: certificate, Plan: plan.Commitment()}); err != nil {
		return nil, err
	}
	if err = a.step("after-attest-lock"); err != nil {
		return nil, err
	}
	digest := ReservationDigest(a.Planner.Chain.ChainID(), a.Planner.Chain.Bridge(), certificate)
	out := map[uint16][]byte{}
	for _, seat := range a.Seats {
		if out[seat], err = SignDigest(a.Key, digest); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AttestCancellation signs a cancellation alternative for an admitted,
// unresolved reservation. The transaction must spend the exact stored inputs
// to one exact-Q output (checked by signing.NewCancellationPlan) and its fee
// must exceed every previously admitted alternative. History is append-only
// on chain, so no local slot lock is needed: every admitted alternative
// conflicts with the payment through the shared inputs.
func (a *Attester) AttestCancellation(ctx context.Context, payment, cancellation *signing.Plan, reservation [32]byte, validUntil uint64) (map[uint16][]byte, CancellationAuthorization, error) {
	var none CancellationAuthorization
	if !cancellation.IsCancellation() || cancellation.PaymentCommitment() != payment.Commitment() {
		return nil, none, errors.New("not a cancellation of this payment")
	}
	head, err := a.Planner.Chain.Head(ctx)
	if err != nil {
		return nil, none, err
	}
	intent, err := a.Planner.Chain.Intent(ctx, head.Number, reservation)
	if err != nil {
		return nil, none, err
	}
	if !intent.State.Unresolved() || intent.Certificate.IntentCommitment != payment.Commitment() || intent.Certificate.WalletID != dkg.WalletID(a.Planner.Wallet.OutputKey) {
		return nil, none, errors.New("reservation is not an unresolved admission of this payment")
	}
	if intent.State == IntentQuarantined {
		d, ok, err := directive(ctx, a.Directives, reservation)
		if err != nil {
			return nil, none, err
		}
		if !ok || d.Action != DirectiveCancel {
			return nil, none, ErrContained
		}
	}
	alternatives, err := a.Planner.Chain.Alternatives(ctx, head.Number, reservation)
	if err != nil {
		return nil, none, err
	}
	if out := cancellation.Snapshot().Unsigned().Outputs[0]; out.Value < taprootDust {
		return nil, none, ErrDustOutput
	}
	fee := uint64(cancellation.Snapshot().Fee())
	for _, alt := range alternatives {
		if alt.Fee >= fee {
			return nil, none, errors.New("cancellation fee must exceed every admitted alternative")
		}
	}
	if validUntil < head.Number {
		return nil, none, errors.New("cancellation deadline passed")
	}
	if err = a.step("before-cancel-attest"); err != nil {
		return nil, none, err
	}
	auth := CancellationAuthorization{ReservationID: reservation, Snapshot: cancellation.Snapshot().Commitment(), TransactionHash: cancellation.BitcoinTransactionHash(), Fee: fee, ValidUntilBlock: validUntil}
	if err = a.Ledger.Append(ctx, Entry{Reservation: reservation, Kind: KindCancellationAttest, Block: head.Number, Transaction: auth.TransactionHash}); err != nil {
		return nil, none, err
	}
	digest := CancellationDigest(a.Planner.Chain.ChainID(), a.Planner.Chain.Bridge(), auth)
	out := map[uint16][]byte{}
	for _, seat := range a.Seats {
		if out[seat], err = SignDigest(a.Key, digest); err != nil {
			return nil, none, err
		}
	}
	return out, auth, nil
}

// Collect concatenates seat signatures in strictly increasing seat order and
// checks that at least threshold distinct seats signed.
func Collect(threshold uint16, parts ...map[uint16][]byte) ([]uint16, []byte, error) {
	all := map[uint16][]byte{}
	for _, p := range parts {
		for seat, sig := range p {
			all[seat] = sig
		}
	}
	var seats []uint16
	for seat := uint16(1); seat != 0 && len(seats) < len(all); seat++ {
		if _, ok := all[seat]; ok {
			seats = append(seats, seat)
		}
	}
	if len(seats) < int(threshold) {
		return nil, nil, errors.New("not enough attested seats")
	}
	var sigs []byte
	for _, seat := range seats {
		if len(all[seat]) != 65 {
			return nil, nil, errors.New("malformed seat signature")
		}
		sigs = append(sigs, all[seat]...)
	}
	return seats, sigs, nil
}
