package tbtc_test

// Remediation batch 11 acceptance tests: an Escalated FROST incoming output
// is proposed and accepted for a sweep exactly as a Pending one.
// Refs tuxxy/keep-core#133 (F-V08-1, P2) and DECISIONS.md D27.
//
// Invariant (P-K07-NO-STRANDED-MOVEMENT, recovery path; D27 (a)): an incoming
// moved output the Bridge holds for a FROST target as Pending or Escalated is
// sweepable. In every coordination window the leader's production proposal
// generator (pkg/tbtcpg) proposes its sweep, and every follower's validator
// (tbtc.ValidateMovedFundsSweepProposal) accepts that proposal under the same
// rules as a Pending one: the output is this wallet's, and the fee is in
// (0, on-chain cap]. An output that is not sweepable (Swept, unrecorded, or
// another wallet's) is never proposed and always refused.
//
// Why it matters: C-06 keeps an Escalated output sweepable and counts it in
// the target's pendingIncoming, so closure blocks on it, and
// notifyFrostMovedFundsSweepTimeout is permissionless. A host that sweeps
// only Pending outputs leaves the target unable to close after one call by
// anyone.
//
// World: batch 3's frostIncomingWorld on the real upgraded Bridge (tbtc-v2
// integration/frost-base, LEGACY_UPGRADED_DIR) with Bitcoin Core regtest.
// The output is escalated by a public call from an account that is neither
// an operator nor the deployer, after the real movedFundsSweepTimeout has
// passed on the chain clock (Anvil evm_increaseTime). The sweep is proven on
// the real Bridge with an SPV proof of a Core-mined transaction.
// FIXTURES: the FROST registry fixture behind the target and the second
// wallet (no FROST DKG), the source's test key (as in batch 3), and the
// target's test key (scalar 1000, as setUpFrostDrain derives it), which
// stands in for the FROST signers when the proposal is carried to Bitcoin.
// No build tag (tuxxy/keep-core#64).

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	chainethereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
)

// C-06 FrostMovementState.IncomingState.
const (
	b11IncomingPending   = uint8(1)
	b11IncomingEscalated = uint8(2)
	b11IncomingSwept     = uint8(3)
)

var b11StateNames = map[uint8]string{0: "Unknown", 1: "Pending", 2: "Escalated", 3: "Swept"}

// b11Prefix names the invariant in every assertion.
const b11Prefix = "P-K07-NO-STRANDED-MOVEMENT (D27, tuxxy/keep-core#133): "

// drainOutpoint is the target's incoming output: the proven drain's output 0.
func (w *frostIncomingWorld) drainOutpoint() bitcoin.TransactionOutpoint {
	return bitcoin.TransactionOutpoint{TransactionHash: w.drain.Hash(), OutputIndex: 0}
}

func (w *frostIncomingWorld) frostContract() *bind.BoundContract {
	return bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
}

// incoming reads the Bridge's C-06 record of the target's incoming output.
func (w *frostIncomingWorld) incoming(t *testing.T) frostIncomingRecord {
	t.Helper()
	var out []interface{}
	if err := w.frostContract().Call(&bind.CallOpts{}, &out, "frostIncoming", deposit.OutpointKey(w.drainOutpoint())); err != nil || len(out) != 1 {
		t.Fatalf("frostIncoming: %v", err)
	}
	return *abi.ConvertType(out[0], new(frostIncomingRecord)).(*frostIncomingRecord)
}

// pendingIncoming reads the target's C-06 pendingIncoming counter, which
// blocks its closure while non-zero.
func (w *frostIncomingWorld) pendingIncoming(t *testing.T) uint64 {
	t.Helper()
	var out []interface{}
	if err := w.frostContract().Call(&bind.CallOpts{}, &out, "frostMovementCounters", w.target.id); err != nil || len(out) != 4 {
		t.Fatalf("frostMovementCounters: %v", err)
	}
	return out[0].(uint64)
}

// escalate lets the real movedFundsSweepTimeout pass on the chain clock and
// escalates the target's incoming output with a public call from a fresh
// account that is neither an operator nor the deployer: anyone can.
func (w *frostIncomingWorld) escalate(t *testing.T) {
	t.Helper()
	params, err := w.legacy.MovingFundsParameters(&bind.CallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	w.elapse(int64(params.MovedFundsSweepTimeout) + 1)

	anyone, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(anyone.PublicKey)
	for _, operator := range w.operators {
		if common.HexToAddress(string(operator)) == address {
			t.Fatal("setup: the escalating account is an operator")
		}
	}
	if err := w.admin.Client().Call(nil, "anvil_setBalance", address, hexutil.EncodeBig(big.NewInt(1e18))); err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(anyone, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	opts.Context = context.Background()
	tx, err := w.frostContract().Transact(opts, "notifyFrostMovedFundsSweepTimeout", deposit.OutpointKey(w.drainOutpoint()))
	if err != nil {
		t.Fatalf("setup: notifyFrostMovedFundsSweepTimeout: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if receipt, err := legacyWaitMined(ctx, w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: notifyFrostMovedFundsSweepTimeout not mined: %v", err)
	}
	// Preconditions: the Bridge holds the output Escalated, still the
	// target's, and still counted against its closure.
	if record := w.incoming(t); record.State != b11IncomingEscalated || record.Target != w.target.id {
		t.Fatalf("precondition: the Bridge does not hold the target's incoming output Escalated: %+v", record)
	}
	if n := w.pendingIncoming(t); n != 1 {
		t.Fatalf("precondition: the escalated output no longer blocks the target's closure (pendingIncoming %d)", n)
	}
}

// clientWith is a production client started fresh, behind an RPC adapter
// with the given empty-revert encoding.
func (w *frostIncomingWorld) clientWith(t *testing.T, encoding legacyRevertEncoding) *chainethereum.TbtcChain {
	t.Helper()
	c, err := w.connect(newLegacyRecorder(t, w.anvilURL, encoding))
	if err != nil {
		t.Fatalf("client startup (%v): %v", encoding, err)
	}
	return c
}

// settle carries an accepted proposal to Bitcoin and the Bridge: the
// target's sweep of the proposed output with the proposed fee, signed by the
// FIXTURE target key, mined by Core and proven on the real Bridge.
func (w *frostIncomingWorld) settle(t *testing.T, p *tbtc.MovedFundsSweepProposal) {
	t.Helper()
	input := w.drainOutpoint()
	value := w.main.Value - frostDrainFee
	script, err := bitcoin.PayToTaproot(w.target.q)
	if err != nil {
		t.Fatal(err)
	}
	fee := p.SweepTxFee.Int64()
	unsigned := &bitcoin.Transaction{
		Version: 2,
		Inputs:  []*bitcoin.TransactionInput{{Outpoint: &input, Sequence: 0xffffffff}},
		Outputs: []*bitcoin.TransactionOutput{{Value: value - fee, PublicKeyScript: script}},
	}
	snapshot, err := bitcoin.NewTaprootTransaction(unsigned, []bitcoin.PreviousOutput{{Outpoint: input, Value: value, PublicKeyScript: script}}, w.target.q)
	if err != nil {
		t.Fatalf("setup: sweep snapshot: %v", err)
	}
	private, _ := btcec.PrivKeyFromBytes(common.LeftPadBytes(big.NewInt(1000).Bytes(), 32))
	var signatures []bitcoin.Signature
	for _, digest := range snapshot.Digests() {
		signature, err := schnorr.Sign(private, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		typed, err := bitcoin.NewSchnorrSignature(signature.Serialize())
		if err != nil {
			t.Fatal(err)
		}
		signatures = append(signatures, typed)
	}
	sweep, err := snapshot.AddSignatures(signatures)
	if err != nil {
		t.Fatalf("setup: sweep signatures: %v", err)
	}
	w.btc.mine(t, sweep)
	w.send("incoming sweep proof", func(o *bind.TransactOpts) (*types.Transaction, error) {
		return w.frostContract().Transact(o, "submitFrostMovedFundsSweepProof", w.target.id, legacyTxInfo(sweep), w.btc.proof(t, sweep.Hash()))
	})
	if record := w.incoming(t); record.State != b11IncomingSwept {
		t.Fatalf("setup: the Bridge did not record the proven sweep (state %s)", b11StateNames[record.State])
	}
	if n := w.pendingIncoming(t); n != 0 {
		t.Fatalf("setup: the proven sweep left pendingIncoming %d", n)
	}
}

// requireNoSweepOf: the window's proposal, if any, does not sweep the
// outpoint.
func requireNoSweepOf(t *testing.T, what string, outpoint bitcoin.TransactionOutpoint, proposal tbtc.CoordinationProposal, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s%s: proposal generation failed: %v", b11Prefix, what, err)
	}
	if p, ok := proposal.(*tbtc.MovedFundsSweepProposal); ok &&
		p.MovingFundsTxHash == outpoint.TransactionHash && p.MovingFundsTxOutputIndex == outpoint.OutputIndex {
		t.Fatalf("%s%s: the leader proposed a sweep of %x:%d, which is not sweepable", b11Prefix, what, p.MovingFundsTxHash, p.MovingFundsTxOutputIndex)
	}
}

// TestFrostIncomingSweepEscalatedProposed (tuxxy/keep-core#133, D27): for a
// target whose only incoming output is Pending (guard, base behaviour) or
// Escalated by anyone, every window's leader, whichever operator and on
// either RPC empty-revert encoding, proposes its sweep from chain state;
// each follower accepts the proposal; the proposal carried to Bitcoin is
// accepted by the real Bridge, which records the output Swept and clears the
// target's pendingIncoming. After that, no leader proposes the Swept output
// and every follower refuses it.
func TestFrostIncomingSweepEscalatedProposed(t *testing.T) {
	for _, escalated := range []bool{false, true} {
		escalated := escalated
		name := map[bool]string{false: "pending", true: "escalated"}[escalated]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newFrostIncomingWorld(t)
			if escalated {
				w.escalate(t)
			}
			state := b11StateNames[w.incoming(t).State]
			stop := w.mineInBackground()
			defer stop()

			var accepted *tbtc.MovedFundsSweepProposal
			for i, leader := range w.operators {
				encoding := legacyEncodings[i%len(legacyEncodings)]
				c := w.clientWith(t, encoding)
				proposal, err := w.propose(t, c, w.btc, leader)
				what := fmt.Sprintf("%swindow with leader %s (%v), incoming output %s", b11Prefix, leader, encoding, state)
				p := w.requireIncomingProposal(t, what, proposal, err)
				for _, follower := range legacyEncodings {
					if err := w.validate(w.clientWith(t, follower), p); err != nil {
						t.Fatalf("%sa follower (%v) refused the leader's sweep of the target's %s incoming output (fee %v, cap %d): %v", b11Prefix, follower, state, p.SweepTxFee, w.sweepCap, err)
					}
				}
				accepted = p
			}

			// The accepted proposal settles on the real Bridge.
			w.settle(t, accepted)

			// Swept is not sweepable: no window proposes it and every
			// follower refuses it.
			for i, leader := range w.operators {
				encoding := legacyEncodings[i%len(legacyEncodings)]
				c := w.clientWith(t, encoding)
				proposal, err := w.propose(t, c, w.btc, leader)
				requireNoSweepOf(t, fmt.Sprintf("window with leader %s (%v) after the %s output was swept", leader, encoding, name), w.drainOutpoint(), proposal, err)
			}
			for _, follower := range legacyEncodings {
				if err := w.validate(w.clientWith(t, follower), accepted); err == nil {
					t.Fatalf("%sa follower (%v) accepted a sweep of the target's incoming output after the Bridge recorded it Swept (from %s)", b11Prefix, follower, state)
				}
			}
		})
	}
}

// TestFrostIncomingSweepEscalatedFollowerValidation (tuxxy/keep-core#133,
// D27, D19): for an Escalated incoming output a follower applies the same
// rules as for a Pending one (batch 3's TestFrostIncomingSweepFollowerValidation):
// it accepts any leader fee in (0, on-chain cap] and refuses a fee above the
// cap, a zero fee, the wrong output index, an unrecorded output, and another
// FROST wallet's proposal naming the target's escalated output; that other
// wallet's leader never proposes it.
func TestFrostIncomingSweepEscalatedFollowerValidation(t *testing.T) {
	t.Parallel()
	w := newFrostIncomingWorld(t)
	w.other = w.registerSecondFrostWallet(t)
	w.escalate(t)
	stop := w.mineInBackground()
	defer stop()
	good := func(fee uint64) *tbtc.MovedFundsSweepProposal {
		return &tbtc.MovedFundsSweepProposal{MovingFundsTxHash: [32]byte(w.drain.Hash()), MovingFundsTxOutputIndex: 0, SweepTxFee: new(big.Int).SetUint64(fee)}
	}
	for _, encoding := range legacyEncodings {
		c := w.clientWith(t, encoding)
		for _, fee := range []uint64{1, w.sweepCap / 2, w.sweepCap} {
			if err := w.validate(c, good(fee)); err != nil {
				t.Errorf("%sa follower (%v) refused a sweep of the target's Escalated incoming output with fee %d within the on-chain cap %d: %v", b11Prefix, encoding, fee, w.sweepCap, err)
			}
		}
		refusals := []struct {
			name string
			p    *tbtc.MovedFundsSweepProposal
		}{
			{"fee above the on-chain cap", good(w.sweepCap + 1)},
			{"zero fee", good(0)},
			{"wrong output index", &tbtc.MovedFundsSweepProposal{MovingFundsTxHash: [32]byte(w.drain.Hash()), MovingFundsTxOutputIndex: 1, SweepTxFee: big.NewInt(1)}},
			{"unrecorded output", &tbtc.MovedFundsSweepProposal{MovingFundsTxHash: [32]byte{0xb1, 0x1}, MovingFundsTxOutputIndex: 0, SweepTxFee: big.NewInt(1)}},
		}
		for _, r := range refusals {
			if err := w.validate(c, r.p); err == nil {
				t.Errorf("%sa follower (%v) accepted a sweep of the target's Escalated incoming output with %s", b11Prefix, encoding, r.name)
			}
		}
		if err := w.validateFor(c, w.other, good(1)); err == nil {
			t.Errorf("%sa follower (%v) accepted another FROST wallet's proposal sweeping the target's Escalated incoming output", b11Prefix, encoding)
		}
	}
	// The other wallet's leader has nothing of its own to sweep.
	c := w.clientWith(t, revertCode3)
	for _, leader := range w.operators {
		var proposal tbtc.CoordinationProposal
		err := legacyBounded(t, "proposal generation", func() error {
			var err error
			proposal, err = generateFor(c, w.btc, w.other, w.operators, leader)
			return err
		})
		requireNoSweepOf(t, fmt.Sprintf("other wallet's window with leader %s", leader), w.drainOutpoint(), proposal, err)
	}
}

// generateFor is one window's leader routine for the given wallet label.
func generateFor(c *chainethereum.TbtcChain, btc bitcoin.Chain, label [20]byte, operators []chain.Address, leader chain.Address) (tbtc.CoordinationProposal, error) {
	return tbtcpg.NewProposalGenerator(c, btc).Generate(&tbtc.CoordinationProposalRequest{
		WalletPublicKeyHash: label,
		WalletOperators:     operators,
		ExecutingOperator:   leader,
		ActionsChecklist:    windowChecklist,
	})
}
