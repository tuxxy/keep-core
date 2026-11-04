package redemption

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/rand"
	"slices"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
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
	// CancelAfterBlocks is the RebroadcastThenCancel window: how long after
	// the timeout block (plus finality) a timely signed payment is
	// rebroadcast before the host moves to cancellation (a D-09 input).
	CancelAfterBlocks uint64
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

// SelectSeats is the signing seats of one payment round, chosen as base
// keep-core chooses a signing attempt's participants (pkg/tbtc
// signingRetryLoop, tecdsa/retry EvaluateRetryParticipantsForSigning) and as
// movement rounds do: the operators holding readiness-certified seats,
// sorted by address and shuffled with the wallet ID and the rotation cycle,
// then whole operators (their certified seats) taken from the round's start
// operator until the seats reach the threshold. Every operator derives the
// same seats from chain state and the round alone. It returns nil when the
// certified seats cannot reach the threshold.
func SelectSeats(d dkg.Descriptor, certified []uint16, round uint64) []uint16 {
	seatsOf := map[common.Address][]uint16{}
	var operators []common.Address
	for i, member := range d.Members {
		seat := uint16(i + 1)
		if member == 0 || i >= len(d.Operators) || !slices.Contains(certified, seat) {
			continue
		}
		operator := d.Operators[i]
		if _, ok := seatsOf[operator]; !ok {
			operators = append(operators, operator)
		}
		seatsOf[operator] = append(seatsOf[operator], seat)
	}
	total := 0
	for _, seats := range seatsOf {
		total += len(seats)
	}
	if len(operators) == 0 || total < int(d.Threshold) {
		return nil
	}
	sort.Slice(operators, func(i, j int) bool {
		return bytes.Compare(operators[i][:], operators[j][:]) < 0
	})
	id := dkg.WalletID(d.OutputKey)
	n := uint64(len(operators))
	// #nosec G404 (insecure random number source (rand))
	// As in base, shuffling operators for retries needs no secure randomness.
	rng := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(id[:8])) + int64(round/n)))
	rng.Shuffle(len(operators), func(i, j int) {
		operators[i], operators[j] = operators[j], operators[i]
	})
	var out []uint16
	for i := uint64(0); i < n && len(out) < int(d.Threshold); i++ {
		out = append(out, seatsOf[operators[(round%n+i)%n]]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
