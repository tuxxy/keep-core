package signing

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// DoneContext is not only the message digest: Bitcoin signatures do not bind
// the host's attempt, reservation or purpose by themselves.
type DoneContext struct {
	Version                                                                       uint8
	Scheme                                                                        bitcoin.SignatureScheme
	WalletID, DescriptorHash, BitcoinGenesis, Plan, Reservation, Attempt, Message [32]byte
	Purpose                                                                       string
	Input                                                                         uint32
	StartBlock                                                                    uint64
}
type Done struct {
	Context   DoneContext
	Signature [64]byte
}

func (d Done) Verify(expected DoneContext, key [32]byte) (bitcoin.Signature, error) {
	if expected.Version != 1 || expected.Scheme != bitcoin.SignatureSchnorr || expected.WalletID == ([32]byte{}) || expected.DescriptorHash == ([32]byte{}) || expected.BitcoinGenesis == ([32]byte{}) || expected.Plan == ([32]byte{}) || expected.Reservation == ([32]byte{}) || expected.Attempt == ([32]byte{}) || expected.Purpose != Redemption || expected.StartBlock == 0 || d.Context != expected {
		return bitcoin.Signature{}, errors.New("signing done context mismatch")
	}
	pub, err := schnorr.ParsePubKey(key[:])
	if err != nil {
		return bitcoin.Signature{}, err
	}
	signature, err := schnorr.ParseSignature(d.Signature[:])
	if err != nil || !signature.Verify(expected.Message[:], pub) {
		return bitcoin.Signature{}, errors.New("invalid signing done signature")
	}
	return bitcoin.NewSchnorrSignature(d.Signature[:])
}
func (d Done) Marshal() ([]byte, error) { return json.Marshal(d) }
func (d *Done) Unmarshal(raw []byte) error {
	if len(raw) > 4096 {
		return errors.New("oversized signing done message")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(d); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("trailing signing done data")
	}
	return nil
}
