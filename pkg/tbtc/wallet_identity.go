package tbtc

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// WalletIdentity is the scheme-tagged routing boundary. FROST never receives a
// synthetic ECDSA public key. The legacy wallet struct stays ECDSA-only.
type WalletIdentity struct {
	scheme                    bitcoin.SignatureScheme
	legacy                    *ecdsa.PublicKey
	outputKey, id, descriptor [32]byte
}

func NewECDSAWalletIdentity(key *ecdsa.PublicKey) (WalletIdentity, error) {
	if _, err := marshalPublicKey(key); err != nil {
		return WalletIdentity{}, err
	}
	copy := &ecdsa.PublicKey{Curve: key.Curve, X: new(big.Int).Set(key.X), Y: new(big.Int).Set(key.Y)}
	return WalletIdentity{scheme: bitcoin.SignatureECDSA, legacy: copy}, nil
}
func NewFrostWalletIdentity(descriptor dkg.Descriptor) (WalletIdentity, error) {
	if err := dkg.ValidateDescriptor(descriptor); err != nil {
		return WalletIdentity{}, err
	}
	hash, err := dkg.DescriptorHash(descriptor)
	if err != nil {
		return WalletIdentity{}, err
	}
	return WalletIdentity{scheme: bitcoin.SignatureSchnorr, outputKey: descriptor.OutputKey, id: dkg.WalletID(descriptor.OutputKey), descriptor: hash}, nil
}
func (w WalletIdentity) Scheme() bitcoin.SignatureScheme { return w.scheme }
func (w WalletIdentity) ECDSAPublicKeyHash() ([20]byte, error) {
	if w.scheme != bitcoin.SignatureECDSA || w.legacy == nil {
		return [20]byte{}, errors.New("wallet is not ECDSA")
	}
	return bitcoin.PublicKeyHash(w.legacy), nil
}
func (w WalletIdentity) FrostWalletID() ([32]byte, error) {
	if w.scheme != bitcoin.SignatureSchnorr || w.descriptor == ([32]byte{}) {
		return [32]byte{}, errors.New("wallet is not FROST")
	}
	return w.id, nil
}
func (w WalletIdentity) monitorIdentity() (walletMonitorIdentity, error) {
	switch w.scheme {
	case bitcoin.SignatureECDSA:
		key, err := w.ECDSAPublicKeyHash()
		return walletMonitorIdentity{scheme: w.scheme, ecdsa: key}, err
	case bitcoin.SignatureSchnorr:
		id, err := w.FrostWalletID()
		return walletMonitorIdentity{scheme: w.scheme, frost: id}, err
	default:
		return walletMonitorIdentity{}, errors.New("unknown wallet scheme")
	}
}
func (w WalletIdentity) CacheKey() (string, error) {
	switch w.scheme {
	case bitcoin.SignatureECDSA:
		raw, err := marshalPublicKey(w.legacy)
		if err != nil {
			return "", err
		}
		return "ecdsa:" + hex.EncodeToString(raw), nil
	case bitcoin.SignatureSchnorr:
		if w.descriptor == ([32]byte{}) {
			return "", errors.New("missing FROST descriptor")
		}
		return fmt.Sprintf("frost:%x", w.descriptor), nil
	default:
		return "", errors.New("unknown wallet scheme")
	}
}
func (w WalletIdentity) OutputScript() (bitcoin.Script, error) {
	switch w.scheme {
	case bitcoin.SignatureECDSA:
		label, err := w.ECDSAPublicKeyHash()
		if err != nil {
			return nil, err
		}
		return bitcoin.PayToWitnessPublicKeyHash(label)
	case bitcoin.SignatureSchnorr:
		return bitcoin.PayToTaproot(w.outputKey)
	default:
		return nil, errors.New("unknown wallet scheme")
	}
}
func (w WalletIdentity) String() string {
	key, err := w.CacheKey()
	if err != nil {
		return "invalid wallet identity"
	}
	return key
}
