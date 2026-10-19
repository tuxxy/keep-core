package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"

	"github.com/btcsuite/btcd/txscript"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// Defense kinds name the C-08 route a challenged digest is defended by.
const (
	// DefenseIntent is a payment or cancellation alternative named by the
	// admitted C-04 intent of Ref (the reservation ID).
	DefenseIntent = "intent"
	// DefenseMovement is a movement matching the C-06 commitment of the
	// period in Ref.
	DefenseMovement = "movement"
	// DefenseSweep is a deposit or incoming sweep: proven if C-08 recorded
	// the transaction, otherwise the self-transfer route.
	DefenseSweep = "sweep"
	// DefenseHeartbeat is a heartbeat in the C-08 heartbeat domain.
	DefenseHeartbeat = "heartbeat"
)

// DefenseRecord holds everything a C-08 defense of one released digest needs.
// The executor retains it before the worker grant, so no local state other
// than this record is needed to defend the digest later.
type DefenseRecord struct {
	Version          uint8
	WalletID, Digest [32]byte
	Purpose, Kind    string
	// Ref is the reservation ID (intent) or the big-endian period (movement).
	Ref         [32]byte
	Transaction *bitcoin.Transaction      `json:",omitempty"`
	Prevouts    []bitcoin.PreviousOutput  `json:",omitempty"`
	Input       uint32
	// LeafHash is zero for a key-path input, else the BIP341 leaf hash.
	LeafHash [32]byte
	Nonce    uint64
}

// DefenseID keys a defense record by the challenge identity C-08 uses.
func DefenseID(wallet, digest [32]byte) [32]byte {
	raw := append([]byte("keep-core/frost/defense-record/v1/"), wallet[:]...)
	return sha256.Sum256(append(raw, digest[:]...))
}

func (r DefenseRecord) Marshal() ([]byte, error) { return json.Marshal(r) }

// RecordReader reads retained signing records.
type RecordReader interface {
	ReadSigningRecord(context.Context, [32]byte) ([]byte, error)
}

// ReadDefense loads the defense record of (wallet, digest), or store.ErrMissing.
func ReadDefense(ctx context.Context, journal RecordReader, wallet, digest [32]byte) (DefenseRecord, error) {
	raw, err := journal.ReadSigningRecord(ctx, DefenseID(wallet, digest))
	if err != nil {
		return DefenseRecord{}, err
	}
	var r DefenseRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&r); err != nil {
		return DefenseRecord{}, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return DefenseRecord{}, errors.New("trailing defense record data")
	}
	if r.Version != 1 || r.WalletID != wallet || r.Digest != digest {
		return DefenseRecord{}, errors.New("defense record does not match its key")
	}
	return r, nil
}

// defenses builds one record per input digest of the plan.
func (p *Plan) defenses(reservation [32]byte) ([]DefenseRecord, error) {
	kind, ref := DefenseIntent, reservation
	switch {
	case p.IsMovement():
		kind, ref = DefenseMovement, [32]byte{}
		binary.BigEndian.PutUint32(ref[28:], p.MovementPeriod())
	case p.IsSweep() || p.IsIncomingSweep():
		kind, ref = DefenseSweep, [32]byte{}
	}
	spends := p.snapshot.Spends()
	var out []DefenseRecord
	for i, digest := range p.snapshot.Digests() {
		r := DefenseRecord{Version: 1, WalletID: p.intent.WalletID, Digest: digest, Purpose: p.intent.Purpose, Kind: kind, Ref: ref, Transaction: p.snapshot.Unsigned(), Prevouts: p.snapshot.PreviousOutputs(), Input: uint32(i)}
		if spends != nil && spends[i] != nil {
			r.LeafHash = txscript.NewBaseTapLeaf(spends[i].Leaf).TapHash()
		}
		out = append(out, r)
	}
	return out, nil
}

// retainDefenses writes every defense record of the plan. A record already
// retained with the same bytes is accepted, so retries and recovery rewrite
// nothing.
func (e *Executor) retainDefenses(ctx context.Context) error {
	records, err := e.config.Plan.defenses(e.config.Reservation)
	if err != nil {
		return err
	}
	for _, r := range records {
		raw, err := r.Marshal()
		if err != nil {
			return err
		}
		if err = e.config.Journal.RetainSigningRecord(ctx, DefenseID(r.WalletID, r.Digest), raw); err != nil {
			return err
		}
	}
	return nil
}
