// Package frost defines the inactive FROST integration boundary. The host must
// authorize each exact message before calling Sign. K-02 adds guarded local
// storage and transport components. Process death before the host durably saves
// KeyReady loses every local seat; retain the attempt tombstone and use a fresh
// epoch. Candidate recovery is rejected. Production finality, fencing deployment
// and SF-03 capacity qualification remain separate work.
package frost

import "context"

const ApprovedProfile = "FROST-secp256k1-SHA256-TR-v3/dkg-taptweak-none"

// Group is the frozen selected roster. Participants can narrow a DKG to its
// admitted subset. Seat IDs stay unchanged when one node holds several seats.
type Group struct {
	Roster    []uint16
	Threshold uint16
	Quorum    uint16
	Epoch     uint64
}

// Attempt contains facts agreed by all nodes. StartBlock is this attempt's
// start block, not the DKG epoch. Use fresh values for every signing retry.
// Use Domain.NewAttempt and a guarded engine for durable ownership.
type Attempt struct {
	Channel    []byte
	Session    []byte
	StartBlock uint64
}

type Candidate struct {
	Epoch      uint64
	Descriptor [32]byte
	OutputKey  [32]byte
	Roster     []uint16
	Threshold  uint16
	Profile    string
}

// KeyReference is an opaque client token. The host must preserve its exact
// bytes and the records it names. It is not a secret share.
type KeyReference []byte

type KeyReady struct {
	Candidate       Candidate
	LocalReferences []KeyReference
}

type DKGRequest struct {
	Group        Group
	LocalSeats   []uint16
	Participants []uint16
	Attempt      Attempt
}

type SigningRequest struct {
	Group      Group
	LocalSeats []uint16
	Selected   []uint16
	Attempt    Attempt
	Key        KeyReady
	Message    [32]byte
}

type Engine interface {
	DKG(context.Context, DKGRequest, Providers) (KeyReady, error)
	Sign(context.Context, SigningRequest, Providers) ([64]byte, error)
}

type Incoming struct {
	AuthenticatedSender uint16
	Message             []byte
}

type Transport interface {
	Broadcast(context.Context, uint16, []byte) error
	SendPrivate(context.Context, uint16, uint16, []byte) error
	Receive(context.Context) (Incoming, error)
}

// LockSlot is extracted by the adapter from an already checked client write.
// Storage must compare-or-insert using (Seat, Attempt, Kind), not Write.ID.
type LockSlot struct {
	Attempt [32]byte
	Kind    string
}

type Write struct {
	Kind    string
	Seat    uint16
	ID      [32]byte
	Payload []byte    // Opaque protected data. Never log or interpret it in the host.
	Slot    *LockSlot // Non-nil only for locks.
}

type PutResult uint8

const (
	Durable PutResult = iota + 1
	Identical
	Conflict
)

type Store interface {
	Put(context.Context, Write) (PutResult, error)
	Read(context.Context, [32]byte) ([]byte, error)
}

type Receipt struct {
	Epoch      uint64
	Descriptor [32]byte
}

// Acceptance must approve the same public candidate under the host policy.
// The local harness provider simulates this boundary; it proves no finality.
type Acceptance interface {
	WaitCandidateAcceptance(context.Context, Candidate) (Receipt, error)
}

type Providers struct {
	Transport  Transport
	Store      Store
	Acceptance Acceptance
}

type Category string

const (
	InvalidRequest    Category = "InvalidRequest"
	WorkerRefused     Category = "WorkerRefused"
	ProtocolFailure   Category = "ProtocolFailure"
	DependencyFailure Category = "DependencyFailure"
	Cancelled         Category = "Cancelled"
)

// Error contains only bounded public diagnostics. Suspect is an unproven
// retry hint; it is never evidence for punishment or roster changes.
type Error struct {
	Category Category
	State    string
	Detail   string
	Suspect  uint16
}

func (e *Error) Error() string {
	return "frost: " + string(e.Category) + " in " + e.State + ": " + e.Detail
}
