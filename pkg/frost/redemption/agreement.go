package redemption

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// Agreement is the executor configuration every operator must share
// (A-K04-AGREED-EXECUTOR-CONFIG). It is derived only from the finalized
// on-chain admission record, so honest operators compute the same values
// without an additional coordination protocol. A reorganization that changes
// the admission block hash changes every derived value, and the executor's
// canonical-hash check refuses the old one.
type Agreement struct {
	Session    [32]byte
	StartBlock uint64
	Selected   []uint16
	Round      uint64
}

// Schedule is the local retry schedule (a D-09 input; test values locally).
type Schedule struct {
	FinalityBlocks uint64
	// RoundBlocks is the length of one signing round. A failed or crashed
	// attempt is terminal; the next round derives a fresh session and attempt.
	RoundBlocks uint64
}

// Agree derives the agreed configuration for the round that contains head.
// purpose separates payment from cancellation sessions. Selected is the
// certificate's seat set, in its strictly increasing on-chain order.
func Agree(s Schedule, chainID uint64, bridge [20]byte, purpose string, reservation [32]byte, admission Admission, seats []uint16, head uint64) (Agreement, error) {
	if s.FinalityBlocks == 0 || s.RoundBlocks == 0 || admission.Block == 0 || admission.BlockHash == ([32]byte{}) || len(seats) == 0 {
		return Agreement{}, errors.New("incomplete agreement inputs")
	}
	base := admission.Block + s.FinalityBlocks
	if head < base {
		return Agreement{}, errors.New("admission not yet final")
	}
	return AgreeRound(s, chainID, bridge, purpose, reservation, admission, seats, (head-base)/s.RoundBlocks)
}

// AgreeRound derives the agreed configuration of one explicit round, used to
// reopen the session that produced a durable result.
func AgreeRound(s Schedule, chainID uint64, bridge [20]byte, purpose string, reservation [32]byte, admission Admission, seats []uint16, round uint64) (Agreement, error) {
	if s.FinalityBlocks == 0 || s.RoundBlocks == 0 || admission.Block == 0 || admission.BlockHash == ([32]byte{}) || len(seats) == 0 {
		return Agreement{}, errors.New("incomplete agreement inputs")
	}
	base := admission.Block + s.FinalityBlocks
	raw := append([]byte("keep-core/frost/redemption-session/v1/"), purpose...)
	raw = append(raw, 0)
	raw = binary.BigEndian.AppendUint64(raw, chainID)
	raw = append(raw, bridge[:]...)
	raw = append(raw, reservation[:]...)
	raw = append(raw, admission.TransactionHash[:]...)
	raw = append(raw, admission.BlockHash[:]...)
	raw = binary.BigEndian.AppendUint64(raw, round)
	return Agreement{Session: sha256.Sum256(raw), StartBlock: base + round*s.RoundBlocks, Selected: append([]uint16(nil), seats...), Round: round}, nil
}
