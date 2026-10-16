package deposit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// ErrNothingToSweep reports that no revealed deposit is sweepable at the anchor.
var ErrNothingToSweep = errors.New("no sweepable FROST deposit")

// Planner builds sweeps from finalized C-05 state at one agreed anchor block,
// so every honest operator derives the same plan without coordination. The
// planning margin uses the anchor block's Ethereum timestamp, which every
// operator reads identically; the Authority separately enforces the real
// margin against Bitcoin median-time-past before every grant.
type Planner struct {
	Chain   Reader
	Bitcoin redemption.ScriptUnspent
	// Funding reports whether a deposit's funding output is still unspent; a
	// refunded deposit is never planned again.
	Funding Funding
	Lock    *MainLock
	Wallet  dkg.Descriptor
	Genesis [32]byte
	Policy  Policy
	// FeeRate sizes the sweep; MaximumSatPerVByte is its ceiling. Both are
	// D-09 inputs (local test values, OPEN).
	FeeRate, MaximumSatPerVByte int64
	boundary                    func(string) error
}

func (p Planner) step(name string) error {
	if p.boundary != nil {
		return p.boundary(name)
	}
	return nil
}

// Funding is the Bitcoin view of deposit funding outputs.
type Funding interface {
	Unspent(ctx context.Context, outpoint bitcoin.TransactionOutpoint) (bool, error)
}

func (p Planner) walletID() [32]byte { return dkg.WalletID(p.Wallet.OutputKey) }

// eligible applies the deterministic planning rules at block at.
func (p Planner) eligible(ctx context.Context, at uint64, key [32]byte) (Record, bool, error) {
	r, err := p.Chain.Deposit(ctx, at, key)
	if errors.Is(err, ErrNotFound) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	reveal, err := p.Chain.Reveal(ctx, key)
	if err != nil {
		return Record{}, false, err
	}
	timestamp, err := p.Chain.Timestamp(ctx, at)
	if err != nil {
		return Record{}, false, err
	}
	ok := r.RevealedAt != 0 && r.SweptAt == 0 && r.Terms.WalletID == p.walletID() && r.WalletKey == p.Wallet.OutputKey &&
		reveal.Block+p.Policy.FinalityBlocks <= at && timestamp+p.Policy.RefundMargin < uint64(r.Terms.RefundLocktime)
	if !ok {
		return r, false, nil
	}
	unspent, err := p.Funding.Unspent(ctx, bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(r.FundingTransactionHash), OutputIndex: r.FundingOutputIndex})
	return r, unspent, err
}

// Pending returns the deposits to sweep at anchor block at: revealed for this
// wallet in [from, at], final at at, unswept and before the planning margin.
// All selected deposits share one vault (C-05 credits one vault per sweep):
// the vault of the smallest eligible key. Keys are in ascending order.
func (p Planner) Pending(ctx context.Context, from, at uint64, limit int) ([][32]byte, error) {
	reveals, err := p.Chain.Reveals(ctx, p.walletID(), from, at)
	if err != nil {
		return nil, err
	}
	var keys [][32]byte
	vaults := map[[32]byte][20]byte{}
	for _, r := range reveals {
		record, ok, err := p.eligible(ctx, at, r.Outpoint)
		if err != nil {
			return nil, err
		}
		if ok {
			keys = append(keys, r.Outpoint)
			vaults[r.Outpoint] = record.Terms.Vault
		}
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
	var out [][32]byte
	for _, k := range keys {
		if vaults[k] != vaults[keys[0]] || (limit > 0 && len(out) == limit) || len(out) == signing.MaxSweepInputs-1 {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

// main discovers the recognized main output by exact script and checks it
// against the C-05/C-04 custody record at block at.
func (p Planner) main(ctx context.Context, at uint64, key [32]byte) (bitcoin.PreviousOutput, error) {
	script, err := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if err != nil {
		return bitcoin.PreviousOutput{}, err
	}
	candidates, err := p.Bitcoin.UnspentByScript(ctx, script)
	if err != nil {
		return bitcoin.PreviousOutput{}, err
	}
	for _, c := range candidates {
		if OutpointKey(c.Outpoint) != key {
			continue
		}
		out, err := p.Chain.CustodyOutput(ctx, at, key)
		if err != nil {
			return bitcoin.PreviousOutput{}, err
		}
		if out.Spent || out.WalletID != p.walletID() || out.Value != uint64(c.Value) {
			return bitcoin.PreviousOutput{}, redemption.ErrNoCustody
		}
		return c, nil
	}
	return bitcoin.PreviousOutput{}, redemption.ErrNoCustody
}

// Build plans a sweep of keys (in the given order) at anchor block at. The main
// follows the C-05 rule: an unreserved main is included, a reserved main is
// not, unless resolution names the active reservation (the C-04 route). A main
// held by another purpose under the host lock refuses the plan.
func (p Planner) Build(ctx context.Context, at uint64, keys [][32]byte, resolution [32]byte) (*signing.Plan, error) {
	if len(keys) == 0 {
		return nil, ErrNothingToSweep
	}
	if err := p.step("before-sweep-plan"); err != nil {
		return nil, err
	}
	if p.FeeRate <= 0 || p.MaximumSatPerVByte < p.FeeRate {
		return nil, errors.New("sweep fee policy")
	}
	wallet := p.walletID()
	custody, err := p.Chain.Custody(ctx, at, wallet)
	if err != nil {
		return nil, err
	}
	var main *bitcoin.PreviousOutput
	switch {
	case resolution != ([32]byte{}):
		if custody.ActiveReservation != resolution || custody.MainOutpoint == ([32]byte{}) {
			return nil, errors.New("resolution needs the active reservation's main")
		}
	case custody.ActiveReservation != ([32]byte{}) || custody.MainOutpoint == ([32]byte{}):
		// Deposit-only: a reserved main stays with its reservation.
	default:
		history, err := p.Lock.History(ctx, wallet, custody.MainOutpoint)
		if err != nil {
			return nil, err
		}
		if current := state(history); current.Purpose != Free && current.Purpose != SweepMain {
			return nil, fmt.Errorf("%w: %s", ErrMainBusy, current.Purpose)
		}
	}
	if resolution != ([32]byte{}) || (custody.ActiveReservation == ([32]byte{}) && custody.MainOutpoint != ([32]byte{})) {
		m, err := p.main(ctx, at, custody.MainOutpoint)
		if err != nil {
			return nil, err
		}
		main = &m
	}
	var deposits []signing.SweepDeposit
	var vault [20]byte
	var total int64
	tx := &bitcoin.Transaction{Version: 2}
	if main != nil {
		o := main.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
		total += main.Value
	}
	for i, key := range keys {
		r, ok, err := p.eligible(ctx, at, key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("deposit %x is not sweepable at block %d", key, at)
		}
		if i == 0 {
			vault = r.Terms.Vault
		} else if r.Terms.Vault != vault {
			return nil, errors.New("one sweep credits one vault")
		}
		c, err := Build(r.Terms, p.Wallet.OutputKey)
		if err != nil {
			return nil, err
		}
		if c.OutputKey != r.DepositOutputKey {
			return nil, fmt.Errorf("%w: C-05 output key differs from the D-04 template", ErrPlanMismatch)
		}
		funding := bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(r.FundingTransactionHash), OutputIndex: r.FundingOutputIndex}
		deposits = append(deposits, signing.SweepDeposit{Key: key, Prevout: bitcoin.PreviousOutput{Outpoint: funding, Value: int64(r.Terms.Amount), PublicKeyScript: c.Script()}, Spend: c.WalletSpend(), TreasuryFee: r.TreasuryFee, MaximumSweepFee: r.MaximumSweepFee, RefundLocktime: r.Terms.RefundLocktime})
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &funding, Sequence: 0xffffffff})
		total += int64(r.Terms.Amount)
	}
	script, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	tx.Outputs = []*bitcoin.TransactionOutput{{Value: 1, PublicKeyScript: script}}
	var prevouts []bitcoin.PreviousOutput
	var spends []*bitcoin.TapscriptSpend
	if main != nil {
		prevouts, spends = append(prevouts, *main), append(spends, nil)
	}
	for _, d := range deposits {
		prevouts, spends = append(prevouts, d.Prevout), append(spends, d.Spend)
	}
	// Size first; the virtual size does not depend on the output value.
	sizing, err := bitcoin.NewTaprootScriptTransaction(tx, prevouts, p.Wallet.OutputKey, spends)
	if err != nil {
		return nil, err
	}
	vsize := sizing.VirtualSize()
	if p.FeeRate > bitcoin.MaxSatoshi/vsize {
		return nil, errors.New("sweep fee overflow")
	}
	tx.Outputs[0].Value = total - p.FeeRate*vsize
	return signing.NewSweepPlan(signing.SweepProposal{Wallet: p.Wallet, BitcoinGenesis: p.Genesis, Generation: custody.Generation, StateVersion: custody.StateVersion, Vault: vault, Main: main, Resolution: resolution, Deposits: deposits, MaximumSatPerVByte: p.MaximumSatPerVByte, Transaction: tx})
}
