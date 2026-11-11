package spv

import (
	"encoding/hex"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// Mutation batch M8 (legacy moving funds, the C-10 re-commitment rule).
//
// Invariant (KC-SF-V3 C-10; getUnprovenMovingFundsTransactions): when a
// wallet committed more than once (C-10 cleared an earlier drain commitment),
// only its most recent commitment can still be proven, so the unproven
// moving funds transactions are looked up against the latest commitment by
// block number, whatever order the events are listed in.
//
// FIXTURE: wallet 1 and its two testnet transactions are the recorded public
// testnet data of TestGetUnprovenMovingFundsTransactions (the main UTXO's
// funding transaction and the moving funds transaction paying three
// targets). The earlier commitment names a single target, which the moving
// funds transaction does not pay as committed.
func TestUnprovenMovingFundsUsesLatestCommitment(t *testing.T) {
	decode := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	label := func(s string) (out [20]byte) { copy(out[:], decode(s)); return }
	tx := func(s string) *bitcoin.Transaction {
		out := new(bitcoin.Transaction)
		if err := out.Deserialize(decode(s)); err != nil {
			t.Fatal(err)
		}
		return out
	}
	wallet := label("8db50eb52063ea9d98b3eac91489a90f738986f6")
	mainTx := tx("02000000000101cf063c32da06ff5044f38220c61ee57ac5f3c31368a665bddb109d225d8b4d1a0100000000fdffffff02409c0000000000001600148db50eb52063ea9d98b3eac91489a90f738986f6df66000000000000160014be8347707d02375d1bd0c8f21a59f44c62990d8f024730440220166fbabc5d144d639e350826a04f1d2d148702312ce532da962076eb3859b81c02203d640f23f84a3792451539616432ffb07dc8ddac72047da56a22ecb842165bb4012103399de99c1d409735b7d20f46f616cd485f6b0442db13dfe4cd1b3772ae675fe024572700")
	movingTx := tx("0100000000010144ae7a0899ce39c1afb27f8fd18a272386edf0402ca2cee5d08b59f54a7bd2110000000000ffffffff037a310000000000001600143091d288521caec06ea912eacfd733edc5a36d6e7a3100000000000016001492a6ec889a8fa34f731e639edede4c75e184307c7c31000000000000160014c7302d75072d78be94eb8d36c4b77583c7abb06e0247304402200be4c3706674d848b09bee2c72c23a71832b5da98d8c4fb8ba7febcc4464e5c102207a8fbaea6f253727add9baa5adc0d53ca7eafc7493d937180959c82d9b3d69c9012103989d253b17a6a0f41838b84ff0d20e8898f9d7b1a98f2564da4cc29dcf8581d900000000")
	targets := [][20]byte{
		label("3091d288521caec06ea912eacfd733edc5a36d6e"),
		label("92a6ec889a8fa34f731e639edede4c75e184307c"),
		label("c7302d75072d78be94eb8d36c4b77583c7abb06e"),
	}
	earlier := &tbtc.MovingFundsCommitmentSubmittedEvent{WalletPublicKeyHash: wallet, TargetWallets: targets[:1], BlockNumber: 50}
	latest := &tbtc.MovingFundsCommitmentSubmittedEvent{WalletPublicKeyHash: wallet, TargetWallets: targets, BlockNumber: 100}

	const historyDepth, currentBlock = uint64(5), uint64(1000)
	for name, events := range map[string][]*tbtc.MovingFundsCommitmentSubmittedEvent{
		"earlier listed first":  {earlier, latest},
		"earlier listed second": {latest, earlier},
	} {
		t.Run(name, func(t *testing.T) {
			btcChain := newLocalBitcoinChain()
			spvChain := newLocalChain()
			blockCounter := newMockBlockCounter()
			blockCounter.SetCurrentBlock(currentBlock)
			spvChain.setBlockCounter(blockCounter)
			spvChain.setWallet(wallet, &tbtc.WalletChainData{
				MainUtxoHash: spvChain.ComputeMainUtxoHash(&bitcoin.UnspentTransactionOutput{
					Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: mainTx.Hash(), OutputIndex: 0},
					Value:    40000,
				}),
				State: tbtc.StateMovingFunds,
			})
			for _, transaction := range []*bitcoin.Transaction{mainTx, movingTx} {
				if err := btcChain.BroadcastTransaction(transaction); err != nil {
					t.Fatal(err)
				}
			}
			for _, event := range events {
				if err := spvChain.addPastMovingFundsCommitmentSubmittedEvent(
					&tbtc.MovingFundsCommitmentSubmittedEventFilter{StartBlock: currentBlock - historyDepth},
					event,
				); err != nil {
					t.Fatal(err)
				}
			}
			transactions, err := getUnprovenMovingFundsTransactions(historyDepth, 10, btcChain, spvChain)
			if err != nil {
				t.Fatal(err)
			}
			if len(transactions) != 1 || transactions[0].Hash() != movingTx.Hash() {
				t.Fatalf("found %d unproven transactions, expected only the moving funds transaction of the latest commitment", len(transactions))
			}
		})
	}
}
