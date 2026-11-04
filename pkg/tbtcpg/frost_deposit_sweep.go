package tbtcpg

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ipfs/go-log/v2"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// runFrost proposes the sweep of a FROST wallet's revealed deposits (K-06,
// C-05) as base proposes a legacy wallet's: the deposits found from chain
// state that are old enough by the wall clock and whose funding has the
// required confirmations, with the fee the leader estimates within the
// deposits' fee caps (D19). The deposits are the ones the K-06 planner
// would sweep: reserved against the C-10 budget, final, unswept, before the
// refund margin and of one vault. While the wallet's main is reserved for a
// redemption, its deposits wait, as an ECDSA wallet's wait for its main
// (D13). The legacy on-chain proposal validator does not know FROST
// wallets; every member plans the sweep from chain state and checks it
// against the proposal.
func (dst *DepositSweepTask) runFrost(
	taskLogger log.StandardLogger,
	walletPublicKeyHash [20]byte,
	depositSweepMaxSize uint16,
) (tbtc.CoordinationProposal, bool, error) {
	frostChain, ok := dst.chain.(tbtc.FrostBridgeConnector)
	if !ok {
		return nil, false, fmt.Errorf("chain cannot read FROST deposits")
	}
	funding, ok := dst.btcChain.(redemption.ScriptUnspent)
	if !ok {
		return nil, false, fmt.Errorf("bitcoin chain has no script query")
	}
	outputKey, frost, err := frostChain.FrostTargetOutputKey(walletPublicKeyHash)
	if err != nil {
		return nil, false, fmt.Errorf("cannot resolve FROST wallet: [%w]", err)
	}
	if !frost {
		return nil, false, fmt.Errorf("wallet is not a FROST wallet")
	}

	ctx := context.Background()
	bridge, err := frostChain.FrostBridge(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("cannot bind the FROST Bridge: [%w]", err)
	}
	head, err := bridge.Head(ctx)
	if err != nil {
		return nil, false, err
	}
	walletID := dkg.WalletID(outputKey)

	custody, err := bridge.Custody(ctx, head.Number, walletID)
	if err != nil {
		return nil, false, fmt.Errorf("cannot get FROST wallet custody: [%w]", err)
	}
	if custody.ActiveReservation != ([32]byte{}) {
		taskLogger.Info("FROST wallet's main is reserved for a redemption; deposits wait")
		return nil, false, nil
	}

	filterStartBlock := uint64(0)
	if head.Number > DepositSweepLookBackBlocks {
		filterStartBlock = head.Number - DepositSweepLookBackBlocks
	}
	limit := int(depositSweepMaxSize)
	if limit <= 0 || limit > signing.MaxSweepInputs-1 {
		limit = signing.MaxSweepInputs - 1
	}
	planner := deposit.Planner{
		Chain:   bridge,
		Funding: frostFunding{chain: dst.btcChain, unspent: funding},
		Wallet:  dkg.Descriptor{OutputKey: outputKey},
		Policy:  tbtc.FrostDepositPolicy,
	}
	keys, err := planner.Pending(ctx, filterStartBlock, head.Number, 0)
	if err != nil {
		return nil, false, fmt.Errorf("cannot find FROST deposits: [%w]", err)
	}

	depositMinAgeSeconds, err := dst.chain.GetDepositMinAge()
	if err != nil {
		return nil, false, fmt.Errorf("failed to get deposit minimum age: [%w]", err)
	}
	depositMinAge := time.Duration(depositMinAgeSeconds) * time.Second
	timeNow := time.Now()

	proposal := &tbtc.DepositSweepProposal{}
	perDepositMaxFee := uint64(0)
	for _, key := range keys {
		if len(proposal.DepositsKeys) == limit {
			break
		}
		record, err := bridge.Deposit(ctx, head.Number, key)
		if err != nil {
			return nil, false, fmt.Errorf("cannot get FROST deposit: [%w]", err)
		}
		reveal, err := bridge.Reveal(ctx, key)
		if err != nil {
			return nil, false, fmt.Errorf("cannot get FROST deposit reveal: [%w]", err)
		}
		fundingTxHash := bitcoin.Hash(record.FundingTransactionHash)

		matureAt := time.Unix(int64(record.RevealedAt), 0).Add(depositMinAge)
		if !timeNow.After(matureAt) {
			taskLogger.Infof("FROST deposit [0x%x] is not old enough", key)
			continue
		}
		confirmations, err := dst.btcChain.GetTransactionConfirmations(
			ctx,
			fundingTxHash,
		)
		if err != nil || confirmations < tbtc.DepositSweepRequiredFundingTxConfirmations {
			taskLogger.Infof(
				"FROST deposit [0x%x] funding transaction doesn't have enough "+
					"confirmations: [%d/%d] [%v]",
				key,
				confirmations,
				tbtc.DepositSweepRequiredFundingTxConfirmations,
				err,
			)
			continue
		}

		proposal.DepositsKeys = append(proposal.DepositsKeys, struct {
			FundingTxHash      bitcoin.Hash
			FundingOutputIndex uint32
		}{
			FundingTxHash:      fundingTxHash,
			FundingOutputIndex: record.FundingOutputIndex,
		})
		proposal.DepositsRevealBlocks = append(
			proposal.DepositsRevealBlocks,
			new(big.Int).SetUint64(reveal.Block),
		)
		if perDepositMaxFee == 0 || record.MaximumSweepFee < perDepositMaxFee {
			perDepositMaxFee = record.MaximumSweepFee
		}
	}

	if len(proposal.DepositsKeys) == 0 {
		taskLogger.Info("no FROST deposits to sweep")
		return nil, false, nil
	}

	fee, _, err := estimateDepositsSweepFee(
		dst.btcChain,
		len(proposal.DepositsKeys),
		perDepositMaxFee,
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"cannot estimate FROST sweep transaction fee: [%w]",
			err,
		)
	}
	proposal.SweepTxFee = big.NewInt(fee)

	taskLogger.Infof(
		"proposing the sweep of [%d] FROST deposits with fee [%d]",
		len(proposal.DepositsKeys),
		fee,
	)

	return proposal, true, nil
}

// frostFunding reports whether a FROST deposit's funding output is still
// unspent, mempool included, from the outputs paying its script.
type frostFunding struct {
	chain   bitcoin.Chain
	unspent redemption.ScriptUnspent
}

func (ff frostFunding) Unspent(
	ctx context.Context,
	outpoint bitcoin.TransactionOutpoint,
) (bool, error) {
	transaction, err := ff.chain.GetTransaction(outpoint.TransactionHash)
	if err != nil {
		return false, err
	}
	if int(outpoint.OutputIndex) >= len(transaction.Outputs) {
		return false, nil
	}
	outputs, err := ff.unspent.UnspentByScript(
		ctx,
		transaction.Outputs[outpoint.OutputIndex].PublicKeyScript,
	)
	if err != nil {
		return false, err
	}
	for _, output := range outputs {
		if output.Outpoint == outpoint {
			return true, nil
		}
	}
	return false, nil
}

// fundingOutpoint is a FROST deposit's funding outpoint.
func fundingOutpoint(record deposit.Record) bitcoin.TransactionOutpoint {
	return bitcoin.TransactionOutpoint{
		TransactionHash: bitcoin.Hash(record.FundingTransactionHash),
		OutputIndex:     record.FundingOutputIndex,
	}
}
