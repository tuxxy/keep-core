package spv

import (
	"encoding/hex"
	"math/big"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// Same-block moving funds re-commitment (Refs tuxxy/keep-core#171).
//
// Invariant: for every wallet in MovingFunds, the SPV maintainer's moving
// funds proof task proves the moving funds transaction of the wallet's live
// commitment, the one whose target wallets hash to the wallet's stored
// MovingFundsTargetWalletsCommitmentHash, and never a transaction that only
// matches a commitment C-10 has already cleared. This holds when the cleared
// and the live commitment were emitted in the same Ethereum block.
//
// Why same-block events reach the maintainer as cleared-then-live:
// TbtcChain.PastMovingFundsCommitmentSubmittedEvents
// (pkg/chain/ethereum/tbtc.go) collects the abigen iterator in eth_getLogs
// order (block, then log index) and stable-sorts by block number only. On
// chain the live commitment can only be submitted after the earlier one was
// cleared (MovingFunds.submitMovingFundsCommitment requires a zero stored
// commitment hash; FrostMovement.releaseDrain clears it), so within one block
// the cleared commitment always has the lower log index and is listed first.
// The tests below therefore list same-block events cleared first. The
// reverse listing is not reachable through the binding and is not tested.
//
// Reachable shape (tbtc-v2 integration/frost-base): releaseDrain is the only
// place that clears the stored commitment hash, and it acts only on a drain
// commitment. So the cleared commitment names FROST drain labels, whose P2TR
// outputs the legacy target-0 P2PKH/P2WPKH lookup never matches, and the
// legacy proof that cleared it replaced the main UTXO that commitment would
// have spent. Sequence: drain commitment against main M0; a legacy deposit
// sweep proof spends M0, releases the drain and sets main M1; a legacy
// commitment against M1. All three can share a block. In the tests below
// the wallet's main is M1, the cleared commitment names a SYNTHETIC drain
// label, and the only confirmed transaction spending M1 is "live".
//
// The tests drive the maintainer's registered moving funds proof task,
// proofTypes[tbtc.ActionMovingFunds], through spvMaintainer.proveTransactions.
// They assert on which transactions reach the proof submitter, not on how the
// maintainer chooses a commitment, so either fix named in the issue (keep the
// last same-block event, or keep only events whose targets hash to the stored
// commitment hash) satisfies them.

// sameBlockSpvChain is a FIXTURE: the package's localChain plus
// ComputeMovingFundsCommitmentHash. It lets a fix that filters events by the
// stored commitment hash, as tbtcpg does, compile and run against these
// tests.
type sameBlockSpvChain struct {
	*localChain
}

func (c *sameBlockSpvChain) ComputeMovingFundsCommitmentHash(
	targetWallets [][20]byte,
) [32]byte {
	return sameBlockCommitmentHash(targetWallets)
}

// sameBlockCommitmentHash mirrors the Ethereum binding's
// computeMovingFundsCommitmentHash (pkg/chain/ethereum/tbtc.go), which is
// keccak256(abi.encodePacked(bytes20[])) as MovingFunds.sol computes it:
// each target padded to 32 bytes. The binding cannot be imported here
// (pkg/chain/ethereum imports pkg/maintainer, which imports this package), so
// TestMovingFundsSameBlockCommitmentHashFixture pins this mirror to values
// computed by TbtcChain.ComputeMovingFundsCommitmentHash.
func sameBlockCommitmentHash(targetWallets [][20]byte) [32]byte {
	packed := make([]byte, 0, 32*len(targetWallets))
	for _, wallet := range targetWallets {
		packed = append(packed, wallet[:]...)
		packed = append(packed, make([]byte, 12)...)
	}
	return crypto.Keccak256Hash(packed)
}

// sameBlockWallet is one source wallet of the FIXTURE world.
type sameBlockWallet struct {
	walletPublicKeyHash [20]byte
	mainTx              *bitcoin.Transaction
	mainOutputIndex     uint32
	// Moving funds transactions spending the wallet's main UTXO, keyed by
	// a label used in failure messages.
	movingTxs map[string]*bitcoin.Transaction
}

type sameBlockFixture struct {
	// Wallet A: FIXTURE taken from the recorded public testnet data of
	// TestGetUnprovenMovingFundsTransactions (main UTXO funding transaction
	// and the moving funds transaction "live" paying three targets).
	a            sameBlockWallet
	aLiveTargets [][20]byte
	// aDrainTargets: a SYNTHETIC FROST drain label standing in for the
	// cleared drain commitment's targets. No transaction pays it by
	// P2PKH/P2WPKH, as on chain a drain pays P2TR outputs.
	aDrainTargets [][20]byte
	// aSingleTargets and aSingleTargetTx: the first live target alone, and
	// a SYNTHETIC unsigned transaction spending wallet A's main to it. It
	// competes with "live" for the same main, so it is broadcast only in
	// the single-commitment control where it is the wallet's only moving
	// funds transaction.
	aSingleTargets  [][20]byte
	aSingleTargetTx *bitcoin.Transaction

	// Wallet B: SYNTHETIC unsigned transactions. "live-b" spends B's main
	// UTXO to B's single committed target.
	b            sameBlockWallet
	bLiveTargets [][20]byte
}

func newSameBlockFixture(t *testing.T) *sameBlockFixture {
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
	p2wpkh := func(pkh [20]byte) bitcoin.Script {
		script, err := bitcoin.PayToWitnessPublicKeyHash(pkh)
		if err != nil {
			t.Fatal(err)
		}
		return script
	}
	spend := func(funding *bitcoin.Transaction, index uint32, value int64, to ...[20]byte) *bitcoin.Transaction {
		outputs := make([]*bitcoin.TransactionOutput, 0, len(to))
		for _, pkh := range to {
			outputs = append(outputs, &bitcoin.TransactionOutput{
				Value:           value,
				PublicKeyScript: p2wpkh(pkh),
			})
		}
		return &bitcoin.Transaction{
			Version: 1,
			Inputs: []*bitcoin.TransactionInput{{
				Outpoint: &bitcoin.TransactionOutpoint{
					TransactionHash: funding.Hash(),
					OutputIndex:     index,
				},
				Sequence: 0xffffffff,
			}},
			Outputs: outputs,
		}
	}

	f := &sameBlockFixture{}

	walletA := label("8db50eb52063ea9d98b3eac91489a90f738986f6")
	mainTxA := tx("02000000000101cf063c32da06ff5044f38220c61ee57ac5f3c31368a665bddb109d225d8b4d1a0100000000fdffffff02409c0000000000001600148db50eb52063ea9d98b3eac91489a90f738986f6df66000000000000160014be8347707d02375d1bd0c8f21a59f44c62990d8f024730440220166fbabc5d144d639e350826a04f1d2d148702312ce532da962076eb3859b81c02203d640f23f84a3792451539616432ffb07dc8ddac72047da56a22ecb842165bb4012103399de99c1d409735b7d20f46f616cd485f6b0442db13dfe4cd1b3772ae675fe024572700")
	liveTxA := tx("0100000000010144ae7a0899ce39c1afb27f8fd18a272386edf0402ca2cee5d08b59f54a7bd2110000000000ffffffff037a310000000000001600143091d288521caec06ea912eacfd733edc5a36d6e7a3100000000000016001492a6ec889a8fa34f731e639edede4c75e184307c7c31000000000000160014c7302d75072d78be94eb8d36c4b77583c7abb06e0247304402200be4c3706674d848b09bee2c72c23a71832b5da98d8c4fb8ba7febcc4464e5c102207a8fbaea6f253727add9baa5adc0d53ca7eafc7493d937180959c82d9b3d69c9012103989d253b17a6a0f41838b84ff0d20e8898f9d7b1a98f2564da4cc29dcf8581d900000000")
	f.aLiveTargets = [][20]byte{
		label("3091d288521caec06ea912eacfd733edc5a36d6e"),
		label("92a6ec889a8fa34f731e639edede4c75e184307c"),
		label("c7302d75072d78be94eb8d36c4b77583c7abb06e"),
	}
	f.aDrainTargets = [][20]byte{
		label("4444444444444444444444444444444444444444"),
	}
	f.aSingleTargets = f.aLiveTargets[:1]
	f.aSingleTargetTx = spend(mainTxA, 0, 39000, f.aSingleTargets...)
	f.a = sameBlockWallet{
		walletPublicKeyHash: walletA,
		mainTx:              mainTxA,
		mainOutputIndex:     0,
		movingTxs: map[string]*bitcoin.Transaction{
			"live": liveTxA,
		},
	}

	walletB := label("1111111111111111111111111111111111111111")
	targetB := label("2222222222222222222222222222222222222222")
	f.bLiveTargets = [][20]byte{targetB}
	// SYNTHETIC funding transaction: one input from an arbitrary outpoint,
	// one P2WPKH output to wallet B.
	mainTxB := &bitcoin.Transaction{
		Version: 1,
		Inputs: []*bitcoin.TransactionInput{{
			Outpoint: &bitcoin.TransactionOutpoint{
				TransactionHash: bitcoin.Hash{0x33},
				OutputIndex:     0,
			},
			Sequence: 0xffffffff,
		}},
		Outputs: []*bitcoin.TransactionOutput{{
			Value:           50000,
			PublicKeyScript: p2wpkh(walletB),
		}},
	}
	f.b = sameBlockWallet{
		walletPublicKeyHash: walletB,
		mainTx:              mainTxB,
		mainOutputIndex:     0,
		movingTxs: map[string]*bitcoin.Transaction{
			"live-b": spend(mainTxB, 0, 49000, targetB),
		},
	}

	return f
}

// sameBlockCommitment is one MovingFundsCommitmentSubmitted event as the
// binding would deliver it.
type sameBlockCommitment struct {
	wallet  *sameBlockWallet
	targets [][20]byte
	block   uint64
}

// sameBlockWalletState is the on-chain state of one source wallet: it is in
// MovingFunds, its main UTXO is its main transaction's output, and its stored
// commitment hash is the hash of storedTargets.
type sameBlockWalletState struct {
	wallet        *sameBlockWallet
	storedTargets [][20]byte
	// extraTxs are further confirmed transactions of this wallet for this
	// case only, keyed by label.
	extraTxs map[string]*bitcoin.Transaction
}

// proveMovingFundsSameBlock runs one round of the maintainer's moving funds
// proof task over the FIXTURE world and returns the labels of the
// transactions handed to the proof submitter, sorted.
func proveMovingFundsSameBlock(
	t *testing.T,
	f *sameBlockFixture,
	wallets []sameBlockWalletState,
	commitments []sameBlockCommitment,
) []string {
	t.Helper()

	const (
		historyDepth     = uint64(5)
		currentBlock     = uint64(1000)
		latestBtcHeight  = uint(800000)
		txConfirmations  = uint(6)
		headerDifficulty = int64(32)
	)

	btcChain := newLocalBitcoinChain()
	spvChain := &sameBlockSpvChain{localChain: newLocalChain()}

	blockCounter := newMockBlockCounter()
	blockCounter.SetCurrentBlock(currentBlock)
	spvChain.setBlockCounter(blockCounter)

	// FIXTURE relay view: every proof header carries the relay's current
	// epoch difficulty and the difficulty factor is 1, so one confirmation
	// suffices and every candidate transaction (6 confirmations) is proven.
	spvChain.setTxProofDifficultyFactor(big.NewInt(1))
	spvChain.setCurrentAndPrevEpochDifficulty(
		big.NewInt(headerDifficulty),
		big.NewInt(headerDifficulty),
	)
	if err := populateBlockHeaders(
		btcChain,
		latestBtcHeight-txConfirmations+1,
		latestBtcHeight,
		func(uint) *big.Int { return big.NewInt(headerDifficulty) },
	); err != nil {
		t.Fatal(err)
	}

	labels := make(map[bitcoin.Hash]string)
	for _, state := range wallets {
		w := state.wallet
		mainTxOutput := w.mainTx.Outputs[w.mainOutputIndex]
		spvChain.setWallet(w.walletPublicKeyHash, &tbtc.WalletChainData{
			MainUtxoHash: spvChain.ComputeMainUtxoHash(&bitcoin.UnspentTransactionOutput{
				Outpoint: &bitcoin.TransactionOutpoint{
					TransactionHash: w.mainTx.Hash(),
					OutputIndex:     w.mainOutputIndex,
				},
				Value: mainTxOutput.Value,
			}),
			MovingFundsTargetWalletsCommitmentHash: spvChain.ComputeMovingFundsCommitmentHash(
				state.storedTargets,
			),
			State: tbtc.StateMovingFunds,
		})
	}
	extraTxs := make(map[*sameBlockWallet]map[string]*bitcoin.Transaction)
	for _, state := range wallets {
		extraTxs[state.wallet] = state.extraTxs
	}
	for _, w := range []*sameBlockWallet{&f.a, &f.b} {
		if err := btcChain.BroadcastTransaction(w.mainTx); err != nil {
			t.Fatal(err)
		}
		movingTxs := make(map[string]*bitcoin.Transaction)
		for label, movingTx := range w.movingTxs {
			movingTxs[label] = movingTx
		}
		for label, movingTx := range extraTxs[w] {
			movingTxs[label] = movingTx
		}
		for label, movingTx := range movingTxs {
			if err := btcChain.BroadcastTransaction(movingTx); err != nil {
				t.Fatal(err)
			}
			if err := btcChain.addTransactionConfirmations(
				movingTx.Hash(),
				txConfirmations,
			); err != nil {
				t.Fatal(err)
			}
			labels[movingTx.Hash()] = label
		}
	}

	filter := &tbtc.MovingFundsCommitmentSubmittedEventFilter{
		StartBlock: currentBlock - historyDepth,
	}
	for _, commitment := range commitments {
		if err := spvChain.addPastMovingFundsCommitmentSubmittedEvent(
			filter,
			&tbtc.MovingFundsCommitmentSubmittedEvent{
				WalletPublicKeyHash: commitment.wallet.walletPublicKeyHash,
				TargetWallets:       commitment.targets,
				BlockNumber:         commitment.block,
			},
		); err != nil {
			t.Fatal(err)
		}
	}

	maintainer := &spvMaintainer{
		config: Config{
			HistoryDepth:     historyDepth,
			TransactionLimit: 10,
		},
		spvChain:     spvChain,
		btcDiffChain: spvChain.localChain,
		btcChain:     btcChain,
	}

	// FIXTURE proof submitter: records which transaction the maintainer
	// decided to prove. Proof assembly and submission are out of scope.
	var proven []string
	submitter := func(
		transactionHash bitcoin.Hash,
		_ uint,
		_ bitcoin.Chain,
		_ Chain,
	) error {
		label, ok := labels[transactionHash]
		if !ok {
			label = "unknown:" + transactionHash.Hex(bitcoin.ReversedByteOrder)
		}
		proven = append(proven, label)
		return nil
	}

	if err := maintainer.proveTransactions(
		proofTypes[tbtc.ActionMovingFunds].unprovenTransactionsGetter,
		submitter,
	); err != nil {
		t.Fatalf("moving funds proof task failed: %v", err)
	}

	sort.Strings(proven)
	return proven
}

func assertProvenMovingFunds(t *testing.T, proven []string, expected ...string) {
	t.Helper()
	sort.Strings(expected)
	if strings.Join(proven, ",") != strings.Join(expected, ",") {
		t.Fatalf(
			"proven moving funds transactions [%s], expected [%s]",
			strings.Join(proven, ","),
			strings.Join(expected, ","),
		)
	}
}

// TestMovingFundsProofSameBlockRecommitment checks the invariant above for a
// wallet whose cleared and live commitments were emitted in the same block,
// listed in the binding's log order (cleared, then live), in the reachable
// shape described at the top of this file. The maintainer must prove the
// live commitment's transaction.
// Refs tuxxy/keep-core#171.
func TestMovingFundsProofSameBlockRecommitment(t *testing.T) {
	f := newSameBlockFixture(t)

	t.Run("one wallet, cleared then live in one block", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{{wallet: &f.a, storedTargets: f.aLiveTargets}},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aDrainTargets, block: 100},
				{wallet: &f.a, targets: f.aLiveTargets, block: 100},
			},
		)
		assertProvenMovingFunds(t, proven, "live")
	})

	// Another wallet's commitment lands in the same block, between the
	// two commitments of wallet A (log order).
	t.Run("cleared and live in one block, another wallet between", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{
				{wallet: &f.a, storedTargets: f.aLiveTargets},
				{wallet: &f.b, storedTargets: f.bLiveTargets},
			},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aDrainTargets, block: 100},
				{wallet: &f.b, targets: f.bLiveTargets, block: 100},
				{wallet: &f.a, targets: f.aLiveTargets, block: 100},
			},
		)
		assertProvenMovingFunds(t, proven, "live", "live-b")
	})
}

// TestMovingFundsProofRecommitmentControls checks the same invariant where
// the integration base already holds it, so that a fix for #171 cannot break
// it: commitments in different blocks resolve to the later one, a single
// commitment is proven whatever its targets, and wallets do not affect each
// other. Refs tuxxy/keep-core#171.
func TestMovingFundsProofRecommitmentControls(t *testing.T) {
	f := newSameBlockFixture(t)

	t.Run("cleared and live in different blocks", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{{wallet: &f.a, storedTargets: f.aLiveTargets}},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aDrainTargets, block: 99},
				{wallet: &f.a, targets: f.aLiveTargets, block: 100},
			},
		)
		assertProvenMovingFunds(t, proven, "live")
	})

	t.Run("single commitment", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{{wallet: &f.a, storedTargets: f.aLiveTargets}},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aLiveTargets, block: 100},
			},
		)
		assertProvenMovingFunds(t, proven, "live")
	})

	// The single-target commitment is live here, so its transaction is the
	// one to prove and the three-target transaction "live" is not. This is
	// the only case where two confirmed transactions spend A's main.
	t.Run("single commitment with the single target", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{{
				wallet:        &f.a,
				storedTargets: f.aSingleTargets,
				extraTxs:      map[string]*bitcoin.Transaction{"single-target": f.aSingleTargetTx},
			}},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aSingleTargets, block: 100},
			},
		)
		assertProvenMovingFunds(t, proven, "single-target")
	})

	t.Run("two wallets in one block", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{
				{wallet: &f.a, storedTargets: f.aLiveTargets},
				{wallet: &f.b, storedTargets: f.bLiveTargets},
			},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aLiveTargets, block: 100},
				{wallet: &f.b, targets: f.bLiveTargets, block: 100},
			},
		)
		assertProvenMovingFunds(t, proven, "live", "live-b")
	})

	// Wallet B's single commitment is unaffected by wallet A's two
	// commitments in an earlier and a later block.
	t.Run("other wallet between two commitments in different blocks", func(t *testing.T) {
		t.Parallel()
		proven := proveMovingFundsSameBlock(
			t,
			f,
			[]sameBlockWalletState{
				{wallet: &f.a, storedTargets: f.aLiveTargets},
				{wallet: &f.b, storedTargets: f.bLiveTargets},
			},
			[]sameBlockCommitment{
				{wallet: &f.a, targets: f.aDrainTargets, block: 99},
				{wallet: &f.b, targets: f.bLiveTargets, block: 100},
				{wallet: &f.a, targets: f.aLiveTargets, block: 101},
			},
		)
		assertProvenMovingFunds(t, proven, "live", "live-b")
	})
}

// TestMovingFundsSameBlockCommitmentHashFixture pins the FIXTURE commitment
// hash to the production binding: the expected values were computed by
// TbtcChain.ComputeMovingFundsCommitmentHash (pkg/chain/ethereum/tbtc.go) at
// integration/snowfall-base d42fb7f17 for wallet A's live and
// single targets. TestMovingFundsSameBlockCommitmentHashBinding (external
// package) pins the same constants to the binding live. Refs tuxxy/keep-core#171.
func TestMovingFundsSameBlockCommitmentHashFixture(t *testing.T) {
	f := newSameBlockFixture(t)
	for name, test := range map[string]struct {
		targets  [][20]byte
		expected string
	}{
		"live targets":  {f.aLiveTargets, "904c4a6009cec78db41a4f8cb9fc5bddc425661fcd75c74edfcabd004a91ed6b"},
		"single target": {f.aSingleTargets, "4feeb56181665f59da541b62040e7040b2ce97f093673909fee5dde774e957be"},
	} {
		hash := sameBlockCommitmentHash(test.targets)
		if hex.EncodeToString(hash[:]) != test.expected {
			t.Fatalf("%s: fixture commitment hash %x, binding computes %s", name, hash, test.expected)
		}
	}
}
