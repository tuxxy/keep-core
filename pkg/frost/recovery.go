package frost

import "context"

// RecoveryHandle contains public identity and opaque record IDs only.
type RecoveryHandle struct {
	Version   uint16
	Attempt   [32]byte
	Candidate Candidate
	Records   []CandidateRecordRef
}
type CandidateRecordRef struct {
	Seat uint16
	ID   [32]byte
}

// ReadinessEvidence records the transport's original operator authentication.
// It is host evidence, not an independently verifiable signature or core proof.
type ReadinessEvidence struct {
	Domain      Domain
	Attempt     [32]byte
	Sender      uint16
	OperatorKey []byte
	Message     []byte
}

// RecoveryJournal owns the queue for one deployment/epoch. AcquireRecovery
// reopens only the original claimed intent and excludes every live use of it.
type RecoveryJournal interface {
	Journal
	ReadLock(context.Context, uint16, [32]byte, string) (Write, error)
	SaveRecovery(context.Context, RecoveryHandle) error
	LoadRecovery(context.Context) (RecoveryHandle, error)
	RecoveryIntent(context.Context) ([]byte, error)
	PendingCandidates(context.Context) ([]CandidateRecordRef, error)
	AcquireRecovery(context.Context, [32]byte, []byte) (func(), error)
	SaveReadiness(context.Context, ReadinessEvidence) error
	ReadReadiness(context.Context, [32]byte) ([]ReadinessEvidence, error)
}
