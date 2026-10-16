package redemption

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Kind is one signed-intent ledger record kind.
type Kind string

const (
	KindAttested              Kind = "attested"
	KindAdmitted              Kind = "admitted"
	KindSignAttempt           Kind = "sign-attempt"
	KindCancellationAttempt   Kind = "cancellation-attempt"
	KindPossibleRelease       Kind = "possible-release"
	KindSigned                Kind = "signed"
	KindBroadcast             Kind = "broadcast"
	KindLateQuarantined       Kind = "late-signature-quarantined"
	KindTimeoutSeen           Kind = "timeout-seen"
	KindCancellationAttest    Kind = "cancellation-attested"
	KindCancellationSigned    Kind = "cancellation-signed"
	KindCancellationBroadcast Kind = "cancellation-broadcast"
	KindEscalation            Kind = "escalation"
	KindOutcome               Kind = "outcome"

	// K-06 sweep records, keyed by the sweep ID instead of a reservation.
	KindSweepAttempt         Kind = "sweep-attempt"
	KindSweepSigned          Kind = "sweep-signed"
	KindSweepBroadcast       Kind = "sweep-broadcast"
	KindSweepLateQuarantined Kind = "sweep-late-quarantined"
	KindSweepOutcome         Kind = "sweep-outcome"
)

// Entry is one immutable ledger record. Detail is public recovery data only;
// no secret share or nonce is ever recorded here.
type Entry struct {
	Reservation [32]byte
	Kind        Kind
	Sequence    uint32
	Block       uint64
	Transaction [32]byte `json:",omitempty"`
	Detail      string   `json:",omitempty"`
}

// RecordJournal is the durable, compare-and-insert application record store
// (pkg/frost/store Scoped via signing.Journal).
type RecordJournal interface {
	RetainSigningRecord(context.Context, [32]byte, []byte) error
	ReadSigningRecord(context.Context, [32]byte) ([]byte, error)
}

// Ledger is the K-05 durable signed-intent ledger. Records are append-only
// and never erased: an unknown signed intent stays pending and recoverable.
type Ledger struct {
	Journal RecordJournal
	// Capacity and Reserve override the defaults (4,096 and 512); zero uses
	// the defaults. Tests scale them down to reach the boundary cheaply.
	Capacity, Reserve int
}

// Ledger capacity. Reads are never bounded: every durable record stays
// readable. Appends stop at the capacity. The last Reserve slots accept only
// recovery records, i.e. every write the timeout and cancellation path needs
// (timeout and escalation markers, cancellation attestation, attempt,
// signature and broadcast) plus the records that finish a payment (signed,
// broadcast, late quarantine, outcome). Ordinary progress records (payment
// sign attempts, possible-release notes, attestation and admission notes)
// stop at Capacity-Reserve, so retries can never consume the recovery space.
// Beyond the full capacity the ledger fails closed: no record is removed, the
// chain stays authoritative, and the operator escalation applies.
const (
	defaultCapacity = 4096
	defaultReserve  = 512
)

func recoveryKind(k Kind) bool {
	switch k {
	case KindTimeoutSeen, KindEscalation, KindCancellationAttest, KindCancellationAttempt, KindCancellationSigned, KindCancellationBroadcast,
		KindSigned, KindBroadcast, KindLateQuarantined, KindOutcome,
		KindSweepSigned, KindSweepBroadcast, KindSweepLateQuarantined, KindSweepOutcome:
		return true
	}
	return false
}

func (l Ledger) limits() (capacity, reserve int) {
	capacity, reserve = l.Capacity, l.Reserve
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	if reserve <= 0 || reserve > capacity {
		reserve = defaultReserve
		if reserve > capacity {
			reserve = capacity
		}
	}
	return capacity, reserve
}

func entryID(reservation [32]byte, sequence uint32) [32]byte {
	raw := append([]byte("keep-core/frost/redemption-ledger/v1/"), reservation[:]...)
	raw = binary.BigEndian.AppendUint32(raw, sequence)
	return sha256.Sum256(raw)
}

// Entries returns every record for a reservation in append order.
func (l Ledger) Entries(ctx context.Context, reservation [32]byte) ([]Entry, error) {
	var out []Entry
	for seq := uint32(0); ; seq++ {
		raw, err := l.Journal.ReadSigningRecord(ctx, entryID(reservation, seq))
		if errors.Is(err, store.ErrMissing) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		var e Entry
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		if e.Reservation != reservation || e.Sequence != seq {
			return nil, errors.New("ledger record identity mismatch")
		}
		out = append(out, e)
	}
}

// Has reports whether a record of the kind (and transaction, when nonzero) exists.
func (l Ledger) Has(ctx context.Context, reservation [32]byte, kind Kind, transaction [32]byte) (bool, error) {
	entries, err := l.Entries(ctx, reservation)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Kind == kind && (transaction == ([32]byte{}) || e.Transaction == transaction) {
			return true, nil
		}
	}
	return false, nil
}

// Append adds a record unless an identical kind/transaction/detail record is
// already present (idempotent across restarts).
func (l Ledger) Append(ctx context.Context, e Entry) error {
	entries, err := l.Entries(ctx, e.Reservation)
	if err != nil {
		return err
	}
	for _, old := range entries {
		if old.Kind == e.Kind && old.Transaction == e.Transaction && old.Detail == e.Detail {
			return nil
		}
	}
	capacity, reserve := l.limits()
	if len(entries) >= capacity || (len(entries) >= capacity-reserve && !recoveryKind(e.Kind)) {
		return fmt.Errorf("ledger capacity reached for %s record", e.Kind)
	}
	e.Sequence = uint32(len(entries))
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err = l.Journal.RetainSigningRecord(ctx, entryID(e.Reservation, e.Sequence), raw); err != nil {
		return fmt.Errorf("ledger append: %w", err)
	}
	return nil
}
