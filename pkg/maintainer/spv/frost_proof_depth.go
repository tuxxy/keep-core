package spv

import (
	"context"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// frostProofDepth returns the number of headers a proof of hash needs for the
// Bridge to accept it, never fewer than floor (the operator's Confirmations),
// derived as the base maintainer does from the Bridge factor and relay epochs
// the chain binding reports. ok is false while the transaction is unknown to
// Bitcoin, holds fewer than floor confirmations, or does not yet hold the
// derived depth; the transaction is then left for a later round. A failed
// Bridge, relay or header read is returned as an error.
func frostProofDepth(chain redemption.Reader, btc bitcoin.Chain, hash bitcoin.Hash, floor uint) (uint, bool, error) {
	if n, err := btc.GetTransactionConfirmations(context.Background(), hash); err != nil || n < floor {
		return 0, false, nil
	}
	within, available, required, err := getProofInfo(hash, btc, chain, chain)
	if err != nil {
		return 0, false, err
	}
	if !within || required == 0 {
		return 0, false, nil
	}
	if required < floor {
		required = floor
	}
	return required, available >= required, nil
}
