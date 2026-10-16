package tbtc

import (
	"context"
	"errors"
	"math/big"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	frostsigning "github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

type LegacySignBatch func(context.Context, []*big.Int, uint64) ([]*tecdsa.Signature, error)
type WalletTransactionRequest struct {
	Wallet           WalletIdentity
	LegacyBuilder    *bitcoin.TransactionBuilder
	LegacySigner     LegacySignBatch
	LegacyStartBlock uint64
	Frost            *frostsigning.Executor
}
type WalletTransactionResult struct {
	Transaction *bitcoin.Transaction
	Frost       *frostsigning.Result
}

// SignWalletTransaction is a strict tagged dispatch used by both the existing
// ECDSA transaction executor and the explicit local FROST node path.
func SignWalletTransaction(ctx context.Context, r WalletTransactionRequest) (*WalletTransactionResult, error) {
	if ctx == nil {
		return nil, errors.New("signing context required")
	}
	if _, err := r.Wallet.CacheKey(); err != nil {
		return nil, err
	}
	switch r.Wallet.scheme {
	case bitcoin.SignatureECDSA:
		if r.Frost != nil || r.LegacyBuilder == nil || r.LegacySigner == nil {
			return nil, errors.New("inconsistent ECDSA signing route")
		}
		digests, err := r.LegacyBuilder.ComputeSignatureHashes()
		if err != nil {
			return nil, err
		}
		signatures, err := r.LegacySigner(ctx, digests, r.LegacyStartBlock)
		if err != nil {
			return nil, err
		}
		typed := make([]bitcoin.Signature, len(signatures))
		for i, s := range signatures {
			if s == nil {
				return nil, errors.New("missing ECDSA signature")
			}
			typed[i], err = bitcoin.NewECDSASignature(&bitcoin.SignatureContainer{R: s.R, S: s.S, PublicKey: r.Wallet.legacy})
			if err != nil {
				return nil, err
			}
		}
		tx, err := r.LegacyBuilder.AddTypedSignatures(typed)
		if err != nil {
			return nil, err
		}
		return &WalletTransactionResult{Transaction: tx}, nil
	case bitcoin.SignatureSchnorr:
		if r.Frost == nil || r.Frost.Plan() == nil || r.LegacyBuilder != nil || r.LegacySigner != nil || r.LegacyStartBlock != 0 || r.Frost.Plan().WalletID() != r.Wallet.id || r.Frost.Plan().DescriptorHash() != r.Wallet.descriptor || r.Frost.Plan().Snapshot().OutputKey() != r.Wallet.outputKey {
			return nil, errors.New("inconsistent FROST signing route")
		}
		result, err := r.Frost.Run(ctx)
		if err != nil {
			return nil, err
		}
		if err = r.Frost.ValidateResult(result); err != nil {
			return nil, err
		}
		return &WalletTransactionResult{Transaction: result.Transaction, Frost: result}, nil
	default:
		return nil, errors.New("unknown signing scheme")
	}
}

// ExecuteFrostTransaction uses an explicitly configured local executor. No
// production node switch enables this path; C-04/K-05 supply the later adapter.
func ExecuteFrostTransaction(ctx context.Context, wallet WalletIdentity, executor *frostsigning.Executor) (*frostsigning.Result, error) {
	result, err := SignWalletTransaction(ctx, WalletTransactionRequest{Wallet: wallet, Frost: executor})
	if err != nil {
		return nil, err
	}
	if result.Frost == nil {
		return nil, errors.New("missing FROST result")
	}
	return result.Frost, nil
}
func (tm *transactionMonitor) trackWallet(hash bitcoin.Hash, wallet WalletIdentity) error {
	identity, err := wallet.monitorIdentity()
	if err != nil {
		return err
	}
	tm.trackIdentity(hash, identity)
	return nil
}
