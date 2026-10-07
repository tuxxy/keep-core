package bitcoin

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

type SignatureScheme uint8

const (
	SignatureECDSA   SignatureScheme = 1
	SignatureSchnorr SignatureScheme = 2
)

// Signature is an explicit union. Inactive fields cannot be set by a caller;
// Schnorr bytes never pass through the legacy ECDSA R/S representation.
type Signature struct {
	scheme  SignatureScheme
	ecdsa   *SignatureContainer
	schnorr [64]byte
}

func NewECDSASignature(value *SignatureContainer) (Signature, error) {
	if value == nil || value.R == nil || value.S == nil || value.PublicKey == nil || value.PublicKey.Curve == nil || value.PublicKey.X == nil || value.PublicKey.Y == nil {
		return Signature{}, errors.New("missing ECDSA signature or key")
	}
	if value.R.Sign() <= 0 || value.S.Sign() <= 0 || !value.PublicKey.Curve.IsOnCurve(value.PublicKey.X, value.PublicKey.Y) {
		return Signature{}, errors.New("invalid ECDSA signature or key")
	}
	return Signature{scheme: SignatureECDSA, ecdsa: copyECDSASignature(value)}, nil
}
func copyECDSASignature(s *SignatureContainer) *SignatureContainer {
	return &SignatureContainer{R: new(big.Int).Set(s.R), S: new(big.Int).Set(s.S), PublicKey: &ecdsa.PublicKey{Curve: s.PublicKey.Curve, X: new(big.Int).Set(s.PublicKey.X), Y: new(big.Int).Set(s.PublicKey.Y)}}
}
func NewSchnorrSignature(raw []byte) (Signature, error) {
	if len(raw) != 64 {
		return Signature{}, errors.New("Schnorr signature must be exactly 64 bytes")
	}
	if _, err := schnorr.ParseSignature(raw); err != nil {
		return Signature{}, err
	}
	s := Signature{scheme: SignatureSchnorr}
	copy(s.schnorr[:], raw)
	return s, nil
}
func (s Signature) Scheme() SignatureScheme { return s.scheme }
func (s Signature) Schnorr() ([64]byte, error) {
	if s.scheme != SignatureSchnorr || s.ecdsa != nil {
		return [64]byte{}, errors.New("signature is not Schnorr")
	}
	return s.schnorr, nil
}
func (s Signature) ECDSA() (*SignatureContainer, error) {
	if s.scheme != SignatureECDSA || s.ecdsa == nil {
		return nil, errors.New("signature is not ECDSA")
	}
	return copyECDSASignature(s.ecdsa), nil
}
func (tb *TransactionBuilder) AddTypedSignatures(signatures []Signature) (*Transaction, error) {
	legacy := make([]*SignatureContainer, len(signatures))
	for i, s := range signatures {
		var err error
		legacy[i], err = s.ECDSA()
		if err != nil {
			return nil, err
		}
	}
	return tb.AddSignatures(legacy)
}
