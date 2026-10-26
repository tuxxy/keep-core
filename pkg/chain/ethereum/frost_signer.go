package ethereum

import (
	"crypto/ecdsa"
	"errors"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/chain"
)

// NewFrostOperatorSigner uses the same Ethereum address and signing rules as
// the normal node's signer. A local-chain signer has a different address
// encoding and must not be used to validate a real registry selection.
func NewFrostOperatorSigner(key *ecdsa.PrivateKey) (chain.Signing, error) {
	if key == nil || key.D == nil || key.D.Sign() <= 0 || key.Curve != crypto.S256() {
		return nil, errors.New("invalid FROST operator key")
	}
	return newSigner(&keystore.Key{PrivateKey: key}), nil
}
