// Package signing binds the inactive FROST signer to exact Bitcoin transactions
// and finalized purpose reservations. It does not implement C-04 settlement.
package signing

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

const Redemption = "redemption"

// Request is a locally resolved, net recipient entitlement. C-04/K-05 must
// derive these values from funded requests and their captured fee policy.
type Request struct {
	ID     [32]byte
	Value  int64
	Script bitcoin.Script
}
type FeePolicy struct{ Minimum, Maximum, MaximumSatPerVByte int64 }
type Proposal struct {
	Wallet                   dkg.Descriptor
	BitcoinGenesis           [32]byte
	Purpose                  string
	Generation, StateVersion uint64
	Requests                 []Request
	ConflictInput            bitcoin.TransactionOutpoint
	Fees                     FeePolicy
	Transaction              *bitcoin.Transaction
	Prevouts                 []bitcoin.PreviousOutput
}

// Intent is public recovery data, not a secret worker record. Every field is
// covered by Commitment, including original transaction and prevout bytes.
type Intent struct {
	Version                                                   uint8
	WalletID, DescriptorHash, BitcoinGenesis, TransactionHash [32]byte
	Purpose                                                   string
	Generation, StateVersion                                  uint64
	Requests                                                  []Request
	ConflictInput                                             bitcoin.TransactionOutpoint
	Fees                                                      FeePolicy
	Transaction                                               *bitcoin.Transaction
	Prevouts                                                  []bitcoin.PreviousOutput
}

type Plan struct {
	snapshot *bitcoin.TaprootTransaction
	intent   Intent
	encoded  []byte
	preimage []byte
	hash     [32]byte
}

func NewPlan(p Proposal) (*Plan, error) {
	if dkg.ValidateDescriptor(p.Wallet) != nil || p.BitcoinGenesis == ([32]byte{}) || p.Generation == 0 || p.StateVersion == 0 || p.Purpose != Redemption || len(p.Requests) == 0 || len(p.Requests) > 255 {
		return nil, errors.New("invalid signing purpose or wallet")
	}
	snapshot, err := bitcoin.NewTaprootTransaction(p.Transaction, p.Prevouts, p.Wallet.OutputKey)
	if err != nil {
		return nil, err
	}
	if p.Fees.Minimum <= 0 || p.Fees.Maximum < p.Fees.Minimum || p.Fees.Maximum > bitcoin.MaxSatoshi || p.Fees.MaximumSatPerVByte <= 0 || p.Fees.MaximumSatPerVByte > bitcoin.MaxSatoshi || snapshot.Fee() < p.Fees.Minimum || snapshot.Fee() > p.Fees.Maximum {
		return nil, errors.New("transaction fee outside policy")
	}
	// Division avoids multiplication overflow and rejects fractional excess.
	vsize := snapshot.VirtualSize()
	if snapshot.Fee()/vsize > p.Fees.MaximumSatPerVByte || (snapshot.Fee()/vsize == p.Fees.MaximumSatPerVByte && snapshot.Fee()%vsize != 0) {
		return nil, errors.New("transaction feerate outside policy")
	}
	found := false
	for _, prev := range p.Prevouts {
		if prev.Outpoint == p.ConflictInput {
			found = true
		}
	}
	if !found {
		return nil, errors.New("reserved conflict input is absent")
	}
	tx := snapshot.Unsigned()
	change, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if len(tx.Outputs) != len(p.Requests)+1 {
		return nil, errors.New("redemption requires exact recipients and one wallet change output")
	}
	seen := map[[32]byte]bool{}
	for i, request := range p.Requests {
		kind := bitcoin.GetScriptType(request.Script)
		if request.ID == ([32]byte{}) || seen[request.ID] || request.Value <= 0 || (kind != bitcoin.P2PKHScript && kind != bitcoin.P2WPKHScript && kind != bitcoin.P2SHScript && kind != bitcoin.P2WSHScript) || bytes.Equal(request.Script, change) {
			return nil, errors.New("invalid redemption entitlement")
		}
		seen[request.ID] = true
		if tx.Outputs[i].Value != request.Value || !bytes.Equal(tx.Outputs[i].PublicKeyScript, request.Script) {
			return nil, fmt.Errorf("recipient output %d does not match request", i)
		}
	}
	if !bytes.Equal(tx.Outputs[len(p.Requests)].PublicKeyScript, change) {
		return nil, errors.New("change is not the exact FROST output key")
	}
	descriptorHash, err := dkg.DescriptorHash(p.Wallet)
	if err != nil {
		return nil, err
	}
	intent := Intent{Version: 2, WalletID: dkg.WalletID(p.Wallet.OutputKey), DescriptorHash: descriptorHash, BitcoinGenesis: p.BitcoinGenesis, TransactionHash: snapshot.Commitment(), Purpose: p.Purpose, Generation: p.Generation, StateVersion: p.StateVersion, ConflictInput: p.ConflictInput, Fees: p.Fees, Transaction: tx, Prevouts: snapshot.PreviousOutputs()}
	for _, request := range p.Requests {
		request.Script = append(bitcoin.Script(nil), request.Script...)
		intent.Requests = append(intent.Requests, request)
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	preimage := canonicalIntentPreimage(intent)
	h := sha256.Sum256(preimage)
	return &Plan{snapshot: snapshot, intent: intent, encoded: encoded, preimage: preimage, hash: h}, nil
}
func (p *Plan) Commitment() [32]byte                  { return p.hash }
func (p *Plan) IntentBytes() []byte                   { return append([]byte(nil), p.encoded...) }
func (p *Plan) Snapshot() *bitcoin.TaprootTransaction { return p.snapshot }
func (p *Plan) WalletID() [32]byte                    { return p.intent.WalletID }
func (p *Plan) DescriptorHash() [32]byte              { return p.intent.DescriptorHash }
func (p *Plan) Genesis() [32]byte                     { return p.intent.BitcoinGenesis }
func (p *Plan) Purpose() string                       { return p.intent.Purpose }

// CommitmentPreimage is the versioned binary contract/client boundary. JSON is
// used only for local recovery records, never for the reservation commitment.
func (p *Plan) CommitmentPreimage() []byte { return append([]byte(nil), p.preimage...) }

// canonicalIntentPreimage accepts only the validated intent built by NewPlan.
// The byte contract and independent TypeScript vector are in INTENT-V2.md.
func canonicalIntentPreimage(i Intent) []byte {
	b := []byte("keep-core/frost/signing-intent/v2/")
	b = append(b, i.Version)
	for _, h := range [][32]byte{i.TransactionHash, i.WalletID, i.DescriptorHash, i.BitcoinGenesis} {
		b = append(b, h[:]...)
	}
	b = append(b, 1) // Purpose byte 1 = redemption. NewPlan refuses every other purpose.
	b = binary.BigEndian.AppendUint64(b, i.Generation)
	b = binary.BigEndian.AppendUint64(b, i.StateVersion)
	b = binary.BigEndian.AppendUint16(b, uint16(len(i.Requests)))
	for _, request := range i.Requests {
		b = append(b, request.ID[:]...)
		b = binary.BigEndian.AppendUint64(b, uint64(request.Value))
		b = binary.BigEndian.AppendUint16(b, uint16(len(request.Script)))
		b = append(b, request.Script...)
	}
	b = append(b, i.ConflictInput.TransactionHash[:]...)
	b = binary.BigEndian.AppendUint32(b, i.ConflictInput.OutputIndex)
	for _, v := range []int64{i.Fees.Minimum, i.Fees.Maximum, i.Fees.MaximumSatPerVByte} {
		b = binary.BigEndian.AppendUint64(b, uint64(v))
	}
	return b
}
