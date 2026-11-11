package tbtcpg_test

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// Mutation batch M8 (MovingFundsTask, legacy source, the ECDSA-to-FROST
// drain branch).
//
// Invariant (KC-SF-V3 K-07 "ECDSA-to-FROST for drain", C-06; D16, D6;
// findFrostDrainTargetWallets doc): when the upgraded Bridge admits a drain
// for a legacy MovingFunds source, the leader commits, in every creation
// mode, exactly min(eligible, ceil(main / walletMaxBtcTransfer)) FROST
// targets, the newest eligible registrations first, in ascending order, and
// proposes them. After the cutover with no admissible drain, or with no
// Live wallet and no drain, it waits (no result). A recorded FROST
// commitment is proposed again, not recommitted. walletMaxBtcTransfer 0, an
// index with fewer eligible targets than required (ErrNotEnoughTargetWallets,
// the next window retries) and any failed read are errors with no
// commitment.
//
// Everything in this file named gDrain* is a test FIXTURE: a legacy host
// (the package's LocalChain, plus the three FrostDrainChain reads, and
// one-line overrides of the LocalChain methods that panic or need a
// registered filter) and a Bitcoin chain that knows the source's main
// transaction. Reads are counted by the shared gFaults injector.

// gDrainHost is a FIXTURE legacy host with the FROST drain reads.
type gDrainHost struct {
	*tbtcpg.LocalChain
	f *gFaults

	drain         *tbtc.FrostDrainState
	registrations [][20]byte // oldest first
	eligible      map[[20]byte]bool
	live          uint32
	events        []*tbtc.MovingFundsCommitmentSubmittedEvent

	commits [][][20]byte
	// commitRead is the read count when the commitment was submitted.
	commitRead int
	// mfReads counts moving-funds-parameters reads; firstMF records that
	// the injected fault hit the first one (base's safety margin check).
	mfReads int
	firstMF bool
}

func (h *gDrainHost) FrostDrainState() (*tbtc.FrostDrainState, error) {
	if err := h.f.read("drain-state"); err != nil {
		return nil, err
	}
	return h.drain, nil
}

func (h *gDrainHost) PastFrostWalletRegistrations() ([][20]byte, error) {
	if err := h.f.read("registrations"); err != nil {
		return nil, err
	}
	return h.registrations, nil
}

func (h *gDrainHost) IsFrostMovementTarget(label [20]byte) (bool, error) {
	if err := h.f.read("is-target"); err != nil {
		return false, err
	}
	return h.eligible[label], nil
}

func (h *gDrainHost) GetLiveWalletsCount() (uint32, error) {
	if err := h.f.read("live-count"); err != nil {
		return 0, err
	}
	return h.live, nil
}

func (h *gDrainHost) GetWalletParameters() (tbtc.WalletParameters, error) {
	if err := h.f.read("wallet-parameters"); err != nil {
		return tbtc.WalletParameters{}, err
	}
	return h.LocalChain.GetWalletParameters()
}

func (h *gDrainHost) GetMovingFundsParameters() (tbtc.MovingFundsParameters, error) {
	h.mfReads++
	if err := h.f.read("moving-funds-parameters"); err != nil {
		h.firstMF = h.mfReads == 1
		return tbtc.MovingFundsParameters{}, err
	}
	return h.LocalChain.GetMovingFundsParameters()
}

func (h *gDrainHost) ComputeMainUtxoHash(u *bitcoin.UnspentTransactionOutput) [32]byte {
	var raw [44]byte
	copy(raw[:32], u.Outpoint.TransactionHash[:])
	binary.BigEndian.PutUint32(raw[32:36], u.Outpoint.OutputIndex)
	binary.BigEndian.PutUint64(raw[36:], uint64(u.Value))
	return sha256.Sum256(raw[:])
}

func (h *gDrainHost) PastDepositRevealedEvents(*tbtc.DepositRevealedEventFilter) ([]*tbtc.DepositRevealedEvent, error) {
	return nil, nil
}

func (h *gDrainHost) PastMovingFundsCommitmentSubmittedEvents(filter *tbtc.MovingFundsCommitmentSubmittedEventFilter) ([]*tbtc.MovingFundsCommitmentSubmittedEvent, error) {
	var out []*tbtc.MovingFundsCommitmentSubmittedEvent
	for _, e := range h.events {
		if len(filter.WalletPublicKeyHash) == 0 || filter.WalletPublicKeyHash[0] == e.WalletPublicKeyHash {
			out = append(out, e)
		}
	}
	return out, nil
}

// SubmitMovingFundsCommitment records the targets and, as a mined
// commitment would, sets the wallet's commitment hash.
func (h *gDrainHost) SubmitMovingFundsCommitment(pkh [20]byte, _ bitcoin.UnspentTransactionOutput, _ []uint32, _ uint32, targets [][20]byte) error {
	h.commits = append(h.commits, targets)
	h.commitRead = h.f.reads
	data, err := h.LocalChain.GetWallet(pkh)
	if err != nil {
		return err
	}
	data.MovingFundsTargetWalletsCommitmentHash = h.ComputeMovingFundsCommitmentHash(targets)
	return nil
}

func (h *gDrainHost) ValidateMovingFundsProposal([20]byte, *bitcoin.UnspentTransactionOutput, *tbtc.MovingFundsProposal) error {
	return nil
}

// gDrainBitcoin is a FIXTURE Bitcoin chain whose address history of the
// source is its main transaction.
type gDrainBitcoin struct {
	*tbtcpg.LocalBitcoinChain
	history []bitcoin.Hash
}

func (c *gDrainBitcoin) GetTxHashesForPublicKeyHash([20]byte) ([]bitcoin.Hash, error) {
	return c.history, nil
}

type drainWorld struct {
	f       *gFaults
	host    *gDrainHost
	btc     *gDrainBitcoin
	source  [20]byte
	request *tbtc.CoordinationProposalRequest
	regs    [][20]byte // r0 (oldest) .. r3 (newest)
}

const drainMain = 1_200_000 // exactly two walletMaxBtcTransfer

// newDrainWorld: a legacy MovingFunds source past its margin with a FIXTURE
// P2WPKH main of drainMain, four FROST registrations all eligible, an open
// drain after the cutover with three eligible targets, one Live wallet.
func newDrainWorld(t *testing.T) *drainWorld {
	t.Helper()
	w := &drainWorld{f: &gFaults{}, source: [20]byte{0x5c, 0x01}}
	w.host = &gDrainHost{LocalChain: tbtcpg.NewLocalChain(), f: w.f, eligible: map[[20]byte]bool{}, live: 1}
	for i := byte(0); i < 4; i++ {
		r := [20]byte{0xf0 - i*0x20, i}
		w.regs = append(w.regs, r)
		w.host.registrations = append(w.host.registrations, r)
		w.host.eligible[r] = true
	}
	w.host.drain = &tbtc.FrostDrainState{FrostOnly: true, Enabled: true, EligibleTargets: 3, RemainingCustody: 10 * drainMain}
	lc := w.host.LocalChain
	lc.SetMovingFundsParameters(1_000_000, 1_000, 0, 30*24*3600, big.NewInt(0), 0, 0, 1_000_000, 0, big.NewInt(0), 0)
	lc.SetWalletParameters(0, 0, 0, 0, 0, drainMain/2, 0)
	counter := tbtcpg.NewMockBlockCounter()
	counter.SetCurrentBlock(1_000)
	lc.SetBlockCounter(counter)
	lc.SetAverageBlockTime(12 * time.Second)
	w.request = &tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: w.source,
		WalletOperators:     []chain.Address{"0xd1", "0xd2"},
		ExecutingOperator:   "0xd2",
	}
	for i, op := range w.request.WalletOperators {
		if err := lc.SetOperatorID(op, chain.OperatorID(i+1)); err != nil {
			t.Fatal(err)
		}
	}

	w.btc = &gDrainBitcoin{LocalBitcoinChain: tbtcpg.NewLocalBitcoinChain()}
	mainTx := &bitcoin.Transaction{
		Version: 1,
		Inputs:  []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{0xab}}, Sequence: 0xffffffff}},
		Outputs: []*bitcoin.TransactionOutput{{Value: drainMain, PublicKeyScript: append([]byte{0x00, 0x14}, w.source[:]...)}},
	}
	w.btc.SetTransaction(mainTx.Hash(), mainTx)
	w.btc.history = []bitcoin.Hash{mainTx.Hash()}
	for blocks := uint32(1); blocks <= 32; blocks++ {
		w.btc.SetEstimateSatPerVByteFee(blocks, 2)
	}
	main := &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: mainTx.Hash()}, Value: drainMain}
	lc.SetWallet(w.source, &tbtc.WalletChainData{
		State:                  tbtc.StateMovingFunds,
		MainUtxoHash:           w.host.ComputeMainUtxoHash(main),
		MovingFundsRequestedAt: time.Now().Add(-48 * time.Hour),
	})
	return w
}

func (w *drainWorld) run() (*tbtc.MovingFundsProposal, bool, error) {
	proposal, ok, err := recoverRun(tbtcpg.NewMovingFundsTask(w.host, w.btc), w.request)
	if proposal == nil {
		return nil, ok, err
	}
	p, isMoving := proposal.(*tbtc.MovingFundsProposal)
	if !isMoving {
		return nil, ok, errors.New("fixture: not a moving funds proposal")
	}
	return p, ok, err
}

func ascending(labels ...[20]byte) [][20]byte {
	out := append([][20]byte{}, labels...)
	sort.Slice(out, func(i, j int) bool { return new(big.Int).SetBytes(out[i][:]).Cmp(new(big.Int).SetBytes(out[j][:])) < 0 })
	return out
}

func TestFrostDrainCommitsTheC06Targets(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *drainWorld)
		want  func(w *drainWorld) [][20]byte
	}{
		{"after the cutover: the two newest eligible, ascending", func(w *drainWorld) {},
			func(w *drainWorld) [][20]byte { return ascending(w.regs[3], w.regs[2]) }},
		{"before the cutover with no Live wallet", func(w *drainWorld) { w.host.drain.FrostOnly, w.host.live = false, 0 },
			func(w *drainWorld) [][20]byte { return ascending(w.regs[3], w.regs[2]) }},
		{"before the cutover with a Live wallet (D16: must go to FROST)", func(w *drainWorld) { w.host.drain.FrostOnly = false },
			func(w *drainWorld) [][20]byte { return ascending(w.regs[3], w.regs[2]) }},
		{"the eligible count caps the target count", func(w *drainWorld) { w.host.drain.EligibleTargets = 1 },
			func(w *drainWorld) [][20]byte { return [][20]byte{w.regs[3]} }},
		{"ineligible registrations are skipped, the oldest is needed", func(w *drainWorld) {
			w.host.eligible[w.regs[1]], w.host.eligible[w.regs[2]] = false, false
		}, func(w *drainWorld) [][20]byte { return ascending(w.regs[3], w.regs[0]) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newDrainWorld(t)
			test.setup(w)
			proposal, ok, err := w.run()
			if err != nil || !ok || proposal == nil {
				t.Fatalf("%s: no proposal (ok %v): %v", test.name, ok, err)
			}
			want := test.want(w)
			if len(w.host.commits) != 1 || !sameLabels(w.host.commits[0], want) {
				t.Fatalf("%s: commitments %x, expected one of %x", test.name, w.host.commits, want)
			}
			if !sameLabels(proposal.TargetWallets, want) {
				t.Fatalf("%s: proposal names %x, expected the committed %x", test.name, proposal.TargetWallets, want)
			}
		})
	}
	t.Run("a recorded FROST commitment is proposed again", func(t *testing.T) {
		w := newDrainWorld(t)
		committed := ascending(w.regs[1], w.regs[3])
		data, _ := w.host.LocalChain.GetWallet(w.source)
		data.MovingFundsTargetWalletsCommitmentHash = w.host.ComputeMovingFundsCommitmentHash(committed)
		w.host.events = []*tbtc.MovingFundsCommitmentSubmittedEvent{{WalletPublicKeyHash: w.source, TargetWallets: committed, BlockNumber: 900}}
		w.host.live = 0
		proposal, ok, err := w.run()
		if err != nil || !ok || proposal == nil {
			t.Fatalf("no proposal (ok %v): %v", ok, err)
		}
		if len(w.host.commits) != 0 || !sameLabels(proposal.TargetWallets, committed) {
			t.Fatalf("%d commitments, proposal %x; expected none and the recorded %x", len(w.host.commits), proposal.TargetWallets, committed)
		}
	})
}

func TestFrostDrainWaits(t *testing.T) {
	for name, setup := range map[string]func(w *drainWorld){
		"no admissible drain after the cutover":                 func(w *drainWorld) { w.host.drain.Enabled = false },
		"no admissible drain and no Live wallet before cutover": func(w *drainWorld) { w.host.drain.Enabled, w.host.drain.FrostOnly, w.host.live = false, false, 0 },
		"a pre-FROST Bridge and no Live wallet":                 func(w *drainWorld) { w.host.drain, w.host.live = nil, 0 },
	} {
		t.Run(name, func(t *testing.T) {
			w := newDrainWorld(t)
			setup(w)
			proposal, ok, err := w.run()
			if err != nil || ok || proposal != nil || len(w.host.commits) != 0 {
				t.Fatalf("%s: error %v, ok %v, proposal %v, %d commitments; expected no result", name, err, ok, proposal != nil, len(w.host.commits))
			}
		})
	}
}

func TestFrostDrainRefusals(t *testing.T) {
	for name, test := range map[string]struct {
		setup     func(w *drainWorld)
		notEnough bool
	}{
		"walletMaxBtcTransfer 0": {func(w *drainWorld) { w.host.LocalChain.SetWalletParameters(0, 0, 0, 0, 0, 0, 0) }, false},
		"an index shorter than the eligible count": {func(w *drainWorld) {
			w.host.eligible[w.regs[0]], w.host.eligible[w.regs[1]], w.host.eligible[w.regs[2]] = false, false, false
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newDrainWorld(t)
			test.setup(w)
			proposal, _, err := w.run()
			if err == nil || errors.Is(err, errGapsPanic) || proposal != nil || len(w.host.commits) != 0 {
				t.Fatalf("%s: error %v, proposal %v, %d commitments; expected a refusal and no commitment", name, err, proposal != nil, len(w.host.commits))
			}
			if errors.Is(err, tbtcpg.ErrNotEnoughTargetWallets) != test.notEnough {
				t.Fatalf("%s: error %v, expected not-enough-targets %v", name, err, test.notEnough)
			}
		})
	}
}

// TestFrostDrainReadFaults: every failed drain, wallet-parameters or
// moving-funds-parameters read gives no proposal and no commitment after the
// fault; it is an error, except the first moving-funds-parameters read, base's
// safety margin check, whose failure means no result.
func TestFrostDrainReadFaults(t *testing.T) {
	for n := 1; n < 100; n++ {
		w := newDrainWorld(t)
		w.f.failAt = n
		proposal, ok, err := w.run()
		if !w.f.fired {
			if n == 1 {
				t.Fatal("fixture: no read was made")
			}
			return
		}
		if proposal != nil || errors.Is(err, errGapsPanic) {
			t.Fatalf("read %d (%s) failed: proposal %v, error %v; expected no proposal", n, w.f.firedOp, proposal != nil, err)
		}
		if len(w.host.commits) != 0 && w.host.commitRead >= n {
			t.Fatalf("read %d (%s) failed, and a commitment was submitted after it", n, w.f.firedOp)
		}
		if w.host.firstMF {
			if ok {
				t.Fatalf("read %d (%s) failed: ok %v, expected no result", n, w.f.firedOp, ok)
			}
			continue
		}
		if err == nil {
			t.Fatalf("read %d (%s) failed, but Run reported no error", n, w.f.firedOp)
		}
	}
	t.Fatal("fixture: reads never ended")
}
