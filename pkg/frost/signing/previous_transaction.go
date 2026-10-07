package signing

import (
	"bytes"
	"errors"
	"io"

	"github.com/btcsuite/btcd/wire"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Preflight count/length fields before the underlying wire decoder allocates.
// A byte cap alone does not constrain allocations from dishonest varints.
// This is the local signing profile limit, not a Bitcoin consensus limit.
func decodePreviousTransaction(raw []byte) (*bitcoin.Transaction, error) {
	bad := errors.New("malformed or oversized previous transaction")
	if len(raw) < 10 || len(raw) > 100000 {
		return nil, bad
	}
	r := bytes.NewReader(raw)
	take := func(n uint64) bool {
		if n > uint64(r.Len()) {
			return false
		}
		_, err := r.Seek(int64(n), io.SeekCurrent)
		return err == nil
	}
	variable := func() bool { n, err := wire.ReadVarInt(r, 0); return err == nil && take(n) }
	if !take(4) {
		return nil, bad
	}
	inputs, err := wire.ReadVarInt(r, 0)
	if err != nil {
		return nil, bad
	}
	witness := false
	if inputs == 0 {
		flag, err := r.ReadByte()
		if err != nil || flag != 1 {
			return nil, bad
		}
		witness = true
		inputs, err = wire.ReadVarInt(r, 0)
		if err != nil {
			return nil, bad
		}
	}
	if inputs == 0 || inputs > uint64(r.Len()/41) {
		return nil, bad
	}
	for i := uint64(0); i < inputs; i++ {
		if !take(36) || !variable() || !take(4) {
			return nil, bad
		}
	}
	outputs, err := wire.ReadVarInt(r, 0)
	if err != nil || outputs == 0 || outputs > uint64(r.Len()/9) {
		return nil, bad
	}
	for i := uint64(0); i < outputs; i++ {
		if !take(8) || !variable() {
			return nil, bad
		}
	}
	if witness {
		for i := uint64(0); i < inputs; i++ {
			items, err := wire.ReadVarInt(r, 0)
			if err != nil || items > uint64(r.Len()) {
				return nil, bad
			}
			for j := uint64(0); j < items; j++ {
				if !variable() {
					return nil, bad
				}
			}
		}
	}
	if !take(4) || r.Len() != 0 {
		return nil, bad
	}
	tx := new(bitcoin.Transaction)
	if err = tx.Deserialize(raw); err != nil {
		return nil, err
	}
	return tx, nil
}
