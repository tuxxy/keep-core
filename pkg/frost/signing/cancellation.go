package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Cancellation is the K-05 conflicting spend of an admitted redemption's exact
// ordered inputs into one exact-Q output. It is a separate authorized act: the
// worker grant requires the C-04 cancellation alternative to be admitted and
// finalized for the same reservation, snapshot and fee.
const Cancellation = "cancellation"

// CancellationView is the authoritative chain record of one admitted
// cancellation alternative, read in the same consistent snapshot as the
// reservation. TransactionHash is the raw (internal-order) Bitcoin hash.
type CancellationView struct {
	Reservation, TransactionHash, Snapshot [32]byte
	Fee, Block                             uint64
	BlockHash, CanonicalBlockHash          [32]byte
}

// Cancellations is implemented by a reservation provider that can read C-04
// cancellation alternatives. The K-04 controlled provider does not.
type Cancellations interface {
	ReadCancellation(ctx context.Context, reservation [32]byte, transactionHash [32]byte) (CancellationView, error)
}

type cancellationIntent struct {
	Version         uint8
	Payment         [32]byte
	Snapshot        [32]byte
	TransactionHash [32]byte
	Fee             int64
	Transaction     *bitcoin.Transaction
	Prevouts        []bitcoin.PreviousOutput
}

// ImmediatelyFinal reports whether tx can be mined in the next block: no
// input enables a BIP68 relative lock (sequence bit 31 set), and the absolute
// locktime is zero or disabled because every input sequence is final. A
// certified transaction that is not immediately final can strand recovery:
// later alternatives must pay strictly more inside a fixed fee envelope.
func ImmediatelyFinal(tx *bitcoin.Transaction) error {
	if tx == nil || len(tx.Inputs) == 0 {
		return errors.New("transaction has no inputs")
	}
	allFinal := true
	for _, input := range tx.Inputs {
		if input == nil || input.Sequence&(1<<31) == 0 {
			return errors.New("input enables a relative locktime")
		}
		if input.Sequence != 0xffffffff {
			allFinal = false
		}
	}
	if tx.Locktime != 0 && !allFinal {
		return errors.New("absolute locktime is not immediately final")
	}
	return nil
}

// NewCancellationPlan freezes the cancellation of an admitted payment plan.
// Inputs must be the payment's exact ordered inputs and prevouts; the single
// output must be P2TR(Q); the fee must lie inside the payment's captured fee
// and fee-rate envelope. Fee escalation above previous alternatives is checked
// against chain state by the caller and again by the contract.
func NewCancellationPlan(payment *Plan, tx *bitcoin.Transaction) (*Plan, error) {
	if payment == nil || payment.cancellation != nil || tx == nil {
		return nil, errors.New("cancellation requires an admitted payment plan")
	}
	prevouts := payment.snapshot.PreviousOutputs()
	snapshot, err := bitcoin.NewTaprootTransaction(tx, prevouts, payment.snapshot.OutputKey())
	if err != nil {
		return nil, err
	}
	unsigned := snapshot.Unsigned()
	if err := ImmediatelyFinal(unsigned); err != nil {
		return nil, fmt.Errorf("cancellation must be immediately final: %w", err)
	}
	paid := payment.snapshot.Unsigned()
	if len(unsigned.Inputs) != len(paid.Inputs) {
		return nil, errors.New("cancellation must spend the exact reserved inputs")
	}
	for i := range unsigned.Inputs {
		if *unsigned.Inputs[i].Outpoint != *paid.Inputs[i].Outpoint {
			return nil, errors.New("cancellation input order or outpoint changed")
		}
	}
	change, _ := bitcoin.PayToTaproot(payment.snapshot.OutputKey())
	if len(unsigned.Outputs) != 1 || !bytes.Equal(unsigned.Outputs[0].PublicKeyScript, change) {
		return nil, errors.New("cancellation must pay one exact FROST output")
	}
	fees := payment.intent.Fees
	fee := snapshot.Fee()
	vsize := snapshot.VirtualSize()
	if fee < fees.Minimum || fee > fees.Maximum || fee/vsize > fees.MaximumSatPerVByte || (fee/vsize == fees.MaximumSatPerVByte && fee%vsize != 0) {
		return nil, errors.New("cancellation fee outside the captured envelope")
	}
	ci := &cancellationIntent{Version: 1, Payment: payment.hash, Snapshot: snapshot.Commitment(), TransactionHash: unsigned.Hash(), Fee: fee, Transaction: unsigned, Prevouts: snapshot.PreviousOutputs()}
	encoded, err := json.Marshal(ci)
	if err != nil {
		return nil, err
	}
	preimage := append([]byte("keep-core/frost/cancellation-intent/v1/"), ci.Payment[:]...)
	preimage = append(preimage, ci.Snapshot[:]...)
	preimage = append(preimage, ci.TransactionHash[:]...)
	preimage = binary.BigEndian.AppendUint64(preimage, uint64(fee))
	intent := payment.intent
	intent.Purpose = Cancellation
	intent.TransactionHash = snapshot.Commitment()
	return &Plan{snapshot: snapshot, intent: intent, encoded: encoded, preimage: preimage, hash: sha256.Sum256(preimage), cancellation: ci}, nil
}

// IsCancellation reports whether the plan is a cancellation alternative.
func (p *Plan) IsCancellation() bool { return p.cancellation != nil }

// PaymentCommitment is the admitted redemption intent commitment the
// reservation record carries. For a payment plan it equals Commitment.
func (p *Plan) PaymentCommitment() [32]byte {
	if p.cancellation != nil {
		return p.cancellation.Payment
	}
	return p.hash
}

// BitcoinTransactionHash is the raw (internal-order) hash of the unsigned
// transaction, which is the C-04 transaction identity.
func (p *Plan) BitcoinTransactionHash() [32]byte { return p.snapshot.Unsigned().Hash() }

// Requests returns a copy of the payment entitlements in output order.
func (p *Plan) Requests() []Request {
	out := make([]Request, len(p.intent.Requests))
	for i, r := range p.intent.Requests {
		out[i] = Request{ID: r.ID, Value: r.Value, Script: append(bitcoin.Script(nil), r.Script...)}
	}
	return out
}

// Fees, Generation, StateVersion and ConflictInput expose the captured fields.
func (p *Plan) Fees() FeePolicy                            { return p.intent.Fees }
func (p *Plan) Generation() uint64                         { return p.intent.Generation }
func (p *Plan) StateVersion() uint64                       { return p.intent.StateVersion }
func (p *Plan) ConflictInput() bitcoin.TransactionOutpoint { return p.intent.ConflictInput }
