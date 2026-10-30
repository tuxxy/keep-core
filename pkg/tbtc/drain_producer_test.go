package tbtc_test

// tuxxy/keep-core#34 (K09-15): the honest legacy moving-funds producer must
// build the ECDSA-to-FROST drain. Also tuxxy/keep-core#44 (K09-27: never pay a
// FROST target as P2WPKH) and tuxxy/keep-core#67 (K09-53: both empty-revert
// encodings), for drains the host produces itself.
//
// Invariant: when the upgraded Bridge admits a drain for a legacy MovingFunds
// source (drain enabled, eligible ready FROST targets, custody budget, not
// contained, main at or above dust), the production client commits the drain
// itself, in every creation mode and whether or not a legacy Live wallet
// exists (owner decision D16, 2026-10-29: "must go to FROST"): exactly the C-06 number of FROST targets,
// min(eligible, ceil(main / walletMaxBtcTransfer)), in ascending order, and no
// legacy target. It then signs and broadcasts the movement, paying each FROST
// target OP_1 PUSH32 Q from its descriptor; Bitcoin Core mines it and the
// Bridge accepts its proof, closing the source.
//
// Spec: KC-SF-V3 line 448 (K-07: "ECDSA-to-FROST for drain"; "After cutover,
// new movement commitments target ready FROST wallets"), line 616 (C-06),
// line 729 (SC-01: "New routing cannot assign new legacy obligations that
// defeat drain"). Review 5630879920 (K09-15), 5655962724 (K09-27).
//
//
// D6 (tentative: wait when the target index is short) applies in every mode
// after D16. The frost-read-faults cases test the unreadable half: while the
// FROST state cannot be read, the host makes no legacy commitment, and it
// drains once the reads succeed. The readable but short half is
// TestDrainProducer_ShortIndex (drain_short_index_test.go).

import (
	"bytes"
	"context"
	"math/big"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

var drainProducerCases = []struct {
	name      string
	setup     drainSetup
	transient string // "", "all" reads, or "frost" (FROST-extension reads only)
}{
	{"FrostOnly-live1-code3-0x", drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertCode3}, ""},
	{"FrostOnly-live1-geth-32000-nodata", drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertGeth}, ""},
	{"FrostOnly-live1-transient-then-success", drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertCode3, faults: true}, "all"},
	// Only FROST-extension reads fail (frostOnlyReads): the FROST state is
	// unreadable for a moment while every legacy read succeeds, so a host
	// that took the failure for "no FROST target" would make the legacy
	// commitment.
	{"FrostOnly-live1-frost-read-faults", drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertCode3, faults: true}, "frost"},
	{"FrostOnly-live0", drainSetup{mode: modeFrostOnly, live: false, route: routeOpen, encoding: revertCode3}, ""},
	{"LegacyOpen-live0", drainSetup{mode: modeLegacyOpen, live: false, route: routeOpen, encoding: revertCode3}, ""},
	{"LegacyDraining-live0", drainSetup{mode: modeLegacyDraining, live: false, route: routeOpen, encoding: revertCode3}, ""},
	// D16: before the cutover with a legacy Live wallet available, the
	// drain is still required; D6 applies there too.
	{"LegacyOpen-live1", drainSetup{mode: modeLegacyOpen, live: true, route: routeOpen, encoding: revertCode3}, ""},
	{"LegacyDraining-live1", drainSetup{mode: modeLegacyDraining, live: true, route: routeOpen, encoding: revertCode3}, ""},
	{"LegacyOpen-live1-frost-read-faults", drainSetup{mode: modeLegacyOpen, live: true, route: routeOpen, encoding: revertCode3, faults: true}, "frost"},
	{"LegacyDraining-live1-frost-read-faults", drainSetup{mode: modeLegacyDraining, live: true, route: routeOpen, encoding: revertCode3, faults: true}, "frost"},
	// C-06 target count: two eligible targets and a main above one
	// walletMaxBtcTransfer, so both must be named, ascending.
	{"FrostOnly-live1-two-targets", drainSetup{mode: modeFrostOnly, live: true, route: routeOpen, encoding: revertCode3,
		targets: 3, maxTransfer: drainMain/2 + 1}, ""},
}

// TestDrainProducer_Drains: see the file comment. Refs tuxxy/keep-core#34,
// tuxxy/keep-core#44, tuxxy/keep-core#67.
func TestDrainProducer_Drains(t *testing.T) {
	for _, c := range drainProducerCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			drainProducerRound(t, c.setup, c.transient)
		})
	}
}

func drainProducerRound(t *testing.T, setup drainSetup, transient string) {
	dw := newDrainWorld(t, setup)
	attempts := 1
	if transient != "" {
		// Every distinct contract read and log query (or every FROST
		// extension read) fails once (drainFaults): a failed read is not an
		// answer, so it must not turn into a legacy route.
		attempts = legacyTransientAttempts
		dw.armFaults(transient)
	}

	var proposal *tbtc.MovingFundsProposal
	var proposeErr error
	for attempt := 1; attempt <= attempts && proposal == nil; attempt++ {
		proposal, proposeErr = dw.window("moving-funds proposal")
		if legacy := dw.committedLegacy(); legacy != nil {
			t.Fatalf("drain producer (%s): the host committed legacy targets %s while an ECDSA-to-FROST drain was admissible (%s)",
				setup, dw.labels(legacy), map[bool]string{true: "after the cutover", false: "before the cutover; D16 requires the drain"}[setup.mode == modeFrostOnly])
		}
	}
	if transient != "" {
		// Non-vacuity: the proposal phase met failed reads.
		t.Logf("drain producer (%s): %d contract reads and %d log queries failed while proposing",
			setup, dw.faults.Failed("call"), dw.faults.Failed("logs"))
		if dw.faults.Failed("call")+dw.faults.Failed("logs") == 0 {
			t.Fatalf("drain producer (%s): precondition: no read failed while proposing", setup)
		}
		dw.armFaults(transient)
	}
	commitments := dw.commitments()
	if len(commitments) == 0 {
		t.Fatalf("drain producer (%s): the host made no drain commitment (last window: proposal %v, error %v)",
			setup, proposal != nil, proposeErr)
	}
	if len(commitments) != 1 {
		t.Fatalf("drain producer (%s): %d commitments, want 1", setup, len(commitments))
	}
	committed := commitments[0]

	// The C-06 count and order.
	want := len(dw.targets)
	if setup.maxTransfer != 0 {
		perTransfer := int((drainMain + setup.maxTransfer - 1) / setup.maxTransfer)
		if perTransfer < want {
			want = perTransfer
		}
	} else {
		want = 1
	}
	if len(committed) != want {
		t.Errorf("drain producer (%s): committed %d FROST targets %s, want the C-06 count %d",
			setup, len(committed), dw.labels(committed), want)
	}
	if !sort.SliceIsSorted(committed, func(i, j int) bool { return bytes.Compare(committed[i][:], committed[j][:]) < 0 }) {
		t.Errorf("drain producer (%s): targets %s are not in ascending order", setup, dw.labels(committed))
	}
	if proposal == nil {
		t.Fatalf("drain producer (%s): the host committed %s but made no moving-funds proposal: %v",
			setup, dw.labels(committed), proposeErr)
	}
	if len(proposal.TargetWallets) != len(committed) {
		t.Fatalf("drain producer (%s): proposal targets %s differ from the commitment %s",
			setup, dw.labels(proposal.TargetWallets), dw.labels(committed))
	}
	for i := range committed {
		if proposal.TargetWallets[i] != committed[i] {
			t.Fatalf("drain producer (%s): proposal targets %s differ from the commitment %s",
				setup, dw.labels(proposal.TargetWallets), dw.labels(committed))
		}
	}

	dw.executeDrain(proposal, attempts)
}

// committedLegacy returns the first committed target list that names a
// legacy wallet, or nil.
func (dw *drainWorld) committedLegacy() [][20]byte {
	for _, list := range dw.commitments() {
		for _, target := range list {
			if !dw.isFrostLabel(target) {
				return list
			}
		}
	}
	return nil
}

// executeDrain signs and broadcasts the movement through the production
// moving-funds action, has Bitcoin Core mine it, and submits its proof to the
// upgraded Bridge.
func (dw *drainWorld) executeDrain(proposal *tbtc.MovingFundsProposal, attempts int) {
	t := dw.t
	t.Helper()
	stop := dw.mineInBackground()
	operator := chain.Address(dw.operator.Hex())
	sign := tbtc.LegacySignBatchFn(func(_ context.Context, messages []*big.Int, _ uint64) ([]*tecdsa.Signature, error) {
		return legacySignatures(dw.source.key, messages)
	})
	var executeErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		start, err := dw.admin.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		executeErr = legacyBounded(t, "drain execution", func() error {
			return tbtc.ExecuteMovingFundsActionForTest(dw.client, dw.btc, &dw.source.key.PublicKey,
				[]chain.Address{operator}, sign, proposal, start, dw.waitForBlock)
		})
		if executeErr == nil {
			break
		}
	}
	stop()
	if executeErr != nil {
		t.Fatalf("drain producer (%s): the host committed the drain but its moving-funds action failed: %v", dw.setup, executeErr)
	}
	if rejected := dw.btc.Rejected(); len(rejected) > 0 {
		t.Fatalf("drain producer (%s): Bitcoin Core refused a broadcast: %v", dw.setup, rejected)
	}
	broadcasts := dw.btc.Broadcasts()
	if len(broadcasts) != 1 {
		t.Fatalf("drain producer (%s): %d transactions broadcast, want 1", dw.setup, len(broadcasts))
	}
	tx := broadcasts[0]
	if len(tx.Outputs) != len(proposal.TargetWallets) {
		t.Fatalf("drain producer (%s): outputs %v for %d targets", dw.setup, legacyOutputs(tx), len(proposal.TargetWallets))
	}
	for i, label := range proposal.TargetWallets {
		var q [32]byte
		for _, target := range dw.targets {
			if target.label == label {
				q = target.q
			}
		}
		script, err := bitcoin.PayToTaproot(q)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tx.Outputs[i].PublicKeyScript, script) {
			t.Errorf("drain producer (%s): output %d pays %x, want P2TR to the descriptor key of FROST target %x",
				dw.setup, i, tx.Outputs[i].PublicKeyScript, label)
		}
	}
	block := dw.btc.mine(t)
	if !legacyBlockHas(block, tx.Hash()) {
		t.Fatalf("drain producer (%s): Bitcoin Core did not mine the drain", dw.setup)
	}
	if err := dw.prove(tx); err != nil {
		t.Fatalf("drain producer (%s): the upgraded Bridge refuses the proof of the drain the host broadcast: %v", dw.setup, err)
	}
	if state, _, _ := dw.sourceWallet(); state != 3 { // Closing
		t.Errorf("drain producer (%s): source state %d after the drain proof, want Closing", dw.setup, state)
	}
	t.Logf("drain producer (%s): drain %s to %s mined by Core and accepted by the Bridge",
		dw.setup, tx.Hash().Hex(bitcoin.ReversedByteOrder), dw.labels(proposal.TargetWallets))
}

// prove submits the movement proof of a mined transaction and returns the
// Bridge's refusal, if any.
func (dw *drainWorld) prove(tx *bitcoin.Transaction) error {
	sent, err := dw.legacy.SubmitMovingFundsProof(dw.opts(), legacyTxInfo(tx), dw.btc.proof(dw.t, tx.Hash()),
		legacyUTXO(dw.main), dw.source.pkh)
	if err != nil {
		return err
	}
	receipt, err := legacyWaitMined(context.Background(), dw.admin, sent)
	if err != nil {
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return errReverted
	}
	return nil
}

type revertedError struct{}

func (revertedError) Error() string { return "reverted" }

var errReverted error = revertedError{}

// armFaults starts a fault phase: every read, or only FROST-extension reads.
func (dw *drainWorld) armFaults(kind string) {
	if kind == "frost" {
		dw.faults.ArmWhere(frostOnlyReads(dw.bridge,
			openLegacyBaseline(dw.t, dw.env).buildABI("contracts/bridge/Bridge.sol", "Bridge")))
		return
	}
	dw.faults.Arm()
}
