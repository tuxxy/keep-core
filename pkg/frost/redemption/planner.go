package redemption

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/signing"
)

// Errors returned before any signature request. Each is a hard refusal.
var (
	ErrZeroNet       = errors.New("FROST redemption net payout is zero or negative")
	ErrDustOutput    = errors.New("FROST redemption output is below the dust floor")
	ErrFeeDomain     = errors.New("FROST redemption fee outside its domain")
	ErrAmountDomain  = errors.New("FROST redemption amount outside the satoshi domain")
	ErrNoCustody     = errors.New("FROST wallet main custody output not found by script")
	ErrInsufficient  = errors.New("FROST wallet custody cannot cover payouts, fee and change")
	ErrNotRedeemable = errors.New("FROST redemption request is not redeemable")
)

// ScriptUnspent discovers wallet outputs by exact output script (OP_1 PUSH32
// Q), never by a HASH160 or an ECDSA witness key.
type ScriptUnspent interface {
	UnspentByScript(ctx context.Context, script bitcoin.Script) ([]bitcoin.PreviousOutput, error)
}

// DustFloor is Bitcoin Core's default dust threshold per output type at the
// default 3 sat/vB dust relay fee. Outputs below it are non-standard.
func DustFloor(script bitcoin.Script) (int64, error) {
	switch bitcoin.GetScriptType(script) {
	case bitcoin.P2PKHScript:
		return 546, nil
	case bitcoin.P2SHScript:
		return 540, nil
	case bitcoin.P2WPKHScript:
		return 294, nil
	case bitcoin.P2WSHScript:
		return 330, nil
	}
	if len(script) == 34 && script[0] == 0x51 && script[1] == 0x20 {
		return 330, nil
	}
	return 0, errors.New("unsupported output script")
}

// taprootDust is Bitcoin Core's default dust threshold for P2TR outputs.
const taprootDust = 330

// OutpointKey is the C-04 custody key: keccak256(rawTxHash || uint32 BE index).
func OutpointKey(o bitcoin.TransactionOutpoint) [32]byte {
	b := append([]byte(nil), o.TransactionHash[:]...)
	b = append(b, byte(o.OutputIndex>>24), byte(o.OutputIndex>>16), byte(o.OutputIndex>>8), byte(o.OutputIndex))
	return crypto.Keccak256Hash(b)
}

// FeePolicy is the local signing envelope. These are D-09 inputs; the local
// fixture values are test values, not production policy.
type FeePolicy struct {
	// FeeRate is the target rate in sat/vB used to size the payment.
	FeeRate int64
	// MaximumSatPerVByte is the certificate's fee-rate ceiling.
	MaximumSatPerVByte int64
	// Minimum is the certificate's minimum absolute fee.
	Minimum int64
}

// Planner builds and independently validates redemption plans from funded
// C-04 requests at one finalized block.
type Planner struct {
	Chain   Reader
	Bitcoin ScriptUnspent
	Wallet  dkg.Descriptor
	Genesis [32]byte
	Fees    FeePolicy
	// DeadlineMargin is the minimum number of seconds left before a request
	// deadline for it to be planned or attested (a D-09 input).
	DeadlineMargin uint64
}

func checkedAmount(v uint64) (int64, error) {
	if v > uint64(bitcoin.MaxSatoshi) {
		return 0, ErrAmountDomain
	}
	return int64(v), nil
}

// shares splits fee evenly with the remainder on the last request, the legacy
// convention. Each share must stay within the request's captured cap.
func shares(fee int64, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = fee / int64(n)
	}
	out[n-1] += fee % int64(n)
	return out
}

// netValue derives a request's payout and refuses any zero, negative, dust or
// overflowing value before a signature request can exist.
func netValue(r Request, share int64) (int64, error) {
	gross, err := checkedAmount(r.Gross)
	if err != nil {
		return 0, err
	}
	treasury, err := checkedAmount(r.TreasuryFee)
	if err != nil {
		return 0, err
	}
	capped, err := checkedAmount(r.MaximumBitcoinFee)
	if err != nil {
		return 0, err
	}
	if share < 0 || share > capped {
		return 0, fmt.Errorf("%w: share %d exceeds captured cap %d", ErrFeeDomain, share, capped)
	}
	if treasury > gross || gross-treasury <= share {
		return 0, ErrZeroNet
	}
	net := gross - treasury - share
	floor, err := DustFloor(r.Script)
	if err != nil {
		return 0, err
	}
	if net < floor {
		return 0, ErrDustOutput
	}
	return net, nil
}

func (p Planner) redeemable(ctx context.Context, at uint64, id [32]byte) (Request, error) {
	r, err := p.Chain.Request(ctx, at, id)
	if err != nil {
		return r, err
	}
	walletID := dkg.WalletID(p.Wallet.OutputKey)
	descriptorHash, err := dkg.DescriptorHash(p.Wallet)
	if err != nil {
		return r, err
	}
	change, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	now, err := p.Chain.Timestamp(ctx, at)
	if err != nil {
		return r, err
	}
	if r.State != RequestRequested || r.ReservationID != ([32]byte{}) || r.WalletID != walletID || r.DescriptorHash != descriptorHash || bytes.Equal(r.Script, change) || now < r.NotBefore || r.Deadline <= now || r.Deadline-now < p.DeadlineMargin {
		return r, ErrNotRedeemable
	}
	return r, nil
}

// main discovers the recognized main custody output by script and checks it
// against the C-04 custody record.
func (p Planner) main(ctx context.Context, at uint64, custody Custody) (bitcoin.PreviousOutput, error) {
	script, err := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if err != nil {
		return bitcoin.PreviousOutput{}, err
	}
	candidates, err := p.Bitcoin.UnspentByScript(ctx, script)
	if err != nil {
		return bitcoin.PreviousOutput{}, err
	}
	for _, c := range candidates {
		if OutpointKey(c.Outpoint) != custody.MainOutpoint || !bytes.Equal(c.PublicKeyScript, script) {
			continue
		}
		out, err := p.Chain.CustodyOutput(ctx, at, custody.MainOutpoint)
		if err != nil {
			return bitcoin.PreviousOutput{}, err
		}
		if out.Spent || out.WalletID != dkg.WalletID(p.Wallet.OutputKey) || out.Value != uint64(c.Value) {
			return bitcoin.PreviousOutput{}, ErrNoCustody
		}
		return c, nil
	}
	return bitcoin.PreviousOutput{}, ErrNoCustody
}

// Build plans a redemption of the given requests, in order, spending only the
// current main custody output (which is also the reserved conflict input).
func (p Planner) Build(ctx context.Context, at uint64, ids [][32]byte) (*signing.Plan, []Request, error) {
	if len(ids) == 0 || len(ids) > signing.MaxRedemptionRequests {
		return nil, nil, ErrNotRedeemable
	}
	if p.Fees.FeeRate <= 0 || p.Fees.MaximumSatPerVByte < p.Fees.FeeRate || p.Fees.Minimum <= 0 {
		return nil, nil, ErrFeeDomain
	}
	cfg, err := p.Chain.Configuration(ctx, at)
	if err != nil {
		return nil, nil, err
	}
	if !cfg.Enabled || cfg.BitcoinGenesis != p.Genesis {
		return nil, nil, errors.New("FROST redemptions disabled or genesis mismatch")
	}
	custody, err := p.Chain.Custody(ctx, at, dkg.WalletID(p.Wallet.OutputKey))
	if err != nil {
		return nil, nil, err
	}
	if custody.ActiveReservation != ([32]byte{}) || custody.MainOutpoint == ([32]byte{}) {
		return nil, nil, errors.New("FROST wallet has an active reservation or no main output")
	}
	main, err := p.main(ctx, at, custody)
	if err != nil {
		return nil, nil, err
	}
	var requests []Request
	for _, id := range ids {
		r, err := p.redeemable(ctx, at, id)
		if err != nil {
			return nil, nil, fmt.Errorf("request %x: %w", id, err)
		}
		requests = append(requests, r)
	}
	change, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	build := func(values []int64, changeValue int64) *bitcoin.Transaction {
		tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: main.Outpoint.TransactionHash, OutputIndex: main.Outpoint.OutputIndex}, Sequence: 0xfffffffd}}}
		for i, r := range requests {
			tx.Outputs = append(tx.Outputs, &bitcoin.TransactionOutput{Value: values[i], PublicKeyScript: append(bitcoin.Script(nil), r.Script...)})
		}
		tx.Outputs = append(tx.Outputs, &bitcoin.TransactionOutput{Value: changeValue, PublicKeyScript: append(bitcoin.Script(nil), change...)})
		return tx
	}
	// Size first with placeholder values; virtual size does not depend on them.
	placeholder := make([]int64, len(requests))
	for i := range placeholder {
		placeholder[i] = 1
	}
	sizing, err := bitcoin.NewTaprootTransaction(build(placeholder, 1), []bitcoin.PreviousOutput{main}, p.Wallet.OutputKey)
	if err != nil {
		return nil, nil, err
	}
	vsize := sizing.VirtualSize()
	if p.Fees.FeeRate > bitcoin.MaxSatoshi/vsize {
		return nil, nil, ErrFeeDomain
	}
	fee := p.Fees.FeeRate * vsize
	maximum, err := p.Chain.MaximumTotalFee(ctx, at)
	if err != nil {
		return nil, nil, err
	}
	maxFee, err := checkedAmount(maximum)
	if err != nil {
		return nil, nil, err
	}
	if fee < p.Fees.Minimum || fee > maxFee {
		return nil, nil, fmt.Errorf("%w: fee %d outside [%d, %d]", ErrFeeDomain, fee, p.Fees.Minimum, maxFee)
	}
	values := make([]int64, len(requests))
	var paid int64
	for i, share := range shares(fee, len(requests)) {
		if values[i], err = netValue(requests[i], share); err != nil {
			return nil, nil, fmt.Errorf("request %x: %w", requests[i].ID, err)
		}
		paid += values[i]
	}
	changeValue := main.Value - paid - fee
	if changeValue < taprootDust {
		return nil, nil, ErrInsufficient
	}
	proposal := signing.Proposal{Wallet: p.Wallet, BitcoinGenesis: p.Genesis, Purpose: signing.Redemption, Generation: custody.Generation + 1, StateVersion: custody.StateVersion, ConflictInput: main.Outpoint, Fees: signing.FeePolicy{Minimum: p.Fees.Minimum, Maximum: maxFee, MaximumSatPerVByte: p.Fees.MaximumSatPerVByte}, Transaction: build(values, changeValue), Prevouts: []bitcoin.PreviousOutput{main}}
	for i, r := range requests {
		proposal.Requests = append(proposal.Requests, signing.Request{ID: r.ID, Value: values[i], Script: append(bitcoin.Script(nil), r.Script...)})
	}
	plan, err := signing.NewPlan(proposal)
	if err != nil {
		return nil, nil, err
	}
	return plan, requests, nil
}

// Validate is the attester's independent check of a proposed plan against
// chain state at one block: every request is funded and redeemable, every net
// value equals gross minus treasury fee minus a capped share, the shares sum
// to the actual fee, inputs are recognized unspent custody of this wallet, the
// conflict input is the main output, and the counters are the next slot.
func (p Planner) Validate(ctx context.Context, at uint64, plan *signing.Plan) error {
	if plan == nil || plan.IsCancellation() || plan.Purpose() != signing.Redemption {
		return errors.New("not a redemption plan")
	}
	walletID := dkg.WalletID(p.Wallet.OutputKey)
	if plan.WalletID() != walletID || plan.Genesis() != p.Genesis || plan.Snapshot().OutputKey() != p.Wallet.OutputKey {
		return errors.New("plan wallet or network mismatch")
	}
	cfg, err := p.Chain.Configuration(ctx, at)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.BitcoinGenesis != p.Genesis {
		return errors.New("FROST redemptions disabled or genesis mismatch")
	}
	custody, err := p.Chain.Custody(ctx, at, walletID)
	if err != nil {
		return err
	}
	if custody.ActiveReservation != ([32]byte{}) || plan.Generation() != custody.Generation+1 || plan.StateVersion() != custody.StateVersion {
		return errors.New("plan does not target the next free reservation slot")
	}
	if OutpointKey(plan.ConflictInput()) != custody.MainOutpoint {
		return errors.New("conflict input is not the main custody output")
	}
	for _, prev := range plan.Snapshot().PreviousOutputs() {
		out, err := p.Chain.CustodyOutput(ctx, at, OutpointKey(prev.Outpoint))
		if err != nil {
			return err
		}
		if out.Spent || out.WalletID != walletID || out.Value != uint64(prev.Value) {
			return errors.New("input is not recognized unspent custody")
		}
	}
	fees := plan.Fees()
	maximum, err := p.Chain.MaximumTotalFee(ctx, at)
	if err != nil {
		return err
	}
	if fees.Maximum > int64(maximum) || fees.MaximumSatPerVByte > p.Fees.MaximumSatPerVByte || fees.Minimum < p.Fees.Minimum {
		return ErrFeeDomain
	}
	var shareTotal int64
	for _, entitlement := range plan.Requests() {
		r, err := p.redeemable(ctx, at, entitlement.ID)
		if err != nil {
			return fmt.Errorf("request %x: %w", entitlement.ID, err)
		}
		if !bytes.Equal(r.Script, entitlement.Script) {
			return errors.New("request script mismatch")
		}
		gross, _ := checkedAmount(r.Gross)
		treasury, _ := checkedAmount(r.TreasuryFee)
		share := gross - treasury - entitlement.Value
		net, err := netValue(r, share)
		if err != nil {
			return fmt.Errorf("request %x: %w", entitlement.ID, err)
		}
		if net != entitlement.Value {
			return ErrZeroNet
		}
		shareTotal += share
	}
	if shareTotal != plan.Snapshot().Fee() {
		return errors.New("fee shares do not equal the transaction fee")
	}
	// The builder's relay policy applies to any supplied plan as well: the
	// wallet change must be standard and the transaction minable now.
	unsigned := plan.Snapshot().Unsigned()
	if change := unsigned.Outputs[len(unsigned.Outputs)-1]; change.Value < taprootDust {
		return ErrDustOutput
	}
	if err := signing.ImmediatelyFinal(unsigned); err != nil {
		return fmt.Errorf("plan is not immediately final: %w", err)
	}
	return nil
}

// Pending returns redeemable request IDs of this wallet, oldest first, at most
// limit, discovered from FrostRedemptionRequested logs in [from, at] and then
// re-read from state at at. A log alone never makes a request redeemable.
func (p Planner) Pending(ctx context.Context, from, at uint64, limit int) ([][32]byte, error) {
	if limit <= 0 || limit > signing.MaxRedemptionRequests {
		limit = signing.MaxRedemptionRequests
	}
	events, err := p.Chain.RequestEvents(ctx, dkg.WalletID(p.Wallet.OutputKey), from, at)
	if err != nil {
		return nil, err
	}
	seen := map[[32]byte]bool{}
	var out [][32]byte
	for _, e := range events {
		if seen[e.RequestID] || len(out) == limit {
			continue
		}
		seen[e.RequestID] = true
		if _, err := p.redeemable(ctx, at, e.RequestID); err == nil {
			out = append(out, e.RequestID)
		} else if !errors.Is(err, ErrNotRedeemable) {
			return nil, err
		}
	}
	return out, nil
}
