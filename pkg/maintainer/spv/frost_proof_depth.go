package spv

import (
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// frostProofDepth returns the number of headers a proof of hash needs for the
// Bridge to accept it, never fewer than floor (the operator's Confirmations),
// derived as the base maintainer does from the Bridge factor and relay epochs
// the chain binding reports. ok is false while the transaction does not yet
// hold that many confirmations or the relay cannot evaluate its proof range;
// like the confirmation count before it, a failed read leaves the
// transaction for a later round.
func frostProofDepth(chain redemption.Reader, btc bitcoin.Chain, hash bitcoin.Hash, floor uint) (uint, bool) {
	within, available, required, err := getProofInfo(hash, btc, chain, chain)
	if err != nil || !within || required == 0 {
		return 0, false
	}
	if required < floor {
		required = floor
	}
	return required, available >= required
}
