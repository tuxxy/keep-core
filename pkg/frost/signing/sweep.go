package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// Sweep is the K-06 deposit sweep purpose. C-05 has no pre-signing
// certificate, so a sweep grant is authorized by the host itself from
// finalized reveal state, the refund margin and the main purpose lock
// (SweepAuthority, VD-07).
const Sweep = "sweep"

// MaxSweepInputs is C-05 FrostDepositSweep.MAX_SWEEP_INPUTS.
const MaxSweepInputs = 128

// SweepAuthority authorizes a sweep plan. It must reread finalized C-05
// reveal and custody state, the refund margin and the main purpose lock on
// every call, and return the same receipt (an anchoring block of that state)
// while the authorization holds. Any refusal is an error.
type SweepAuthority interface {
	AuthorizeSweep(ctx context.Context, plan *Plan) (ReservationReceipt, error)
}

// SweepDeposit is one revealed deposit input. Key is the C-05 deposit key of
// the funding outpoint; Spend is the wallet leaf and its control block.
type SweepDeposit struct {
	Key             [32]byte
	Prevout         bitcoin.PreviousOutput
	Spend           *bitcoin.TapscriptSpend
	TreasuryFee     uint64
	MaximumSweepFee uint64
	RefundLocktime  uint32
}

// SweepProposal is a sweep as the C-05 proof will account for it: the main
// output first (when included), then the deposits in proof order, one output
// to Q. Generation and StateVersion are the custody values read when
// planning. Resolution is set only for the C-04 sweep-resolution route.
type SweepProposal struct {
	Wallet                   dkg.Descriptor
	BitcoinGenesis           [32]byte
	Generation, StateVersion uint64
	Vault                    [20]byte
	Main                     *bitcoin.PreviousOutput
	Resolution               [32]byte
	Deposits                 []SweepDeposit
	MaximumSatPerVByte       int64
	Transaction              *bitcoin.Transaction
}

type sweepIntent struct {
	Version            uint8
	WalletID           [32]byte
	DescriptorHash     [32]byte
	BitcoinGenesis     [32]byte
	Snapshot           [32]byte
	TransactionHash    [32]byte
	Generation         uint64
	StateVersion       uint64
	Vault              [20]byte
	Main               *bitcoin.PreviousOutput `json:",omitempty"`
	Resolution         [32]byte
	Deposits           []SweepDeposit
	Fee                int64
	MaximumSatPerVByte int64
	Transaction        *bitcoin.Transaction
}

func depositKey(o bitcoin.TransactionOutpoint) [32]byte {
	b := append([]byte(nil), o.TransactionHash[:]...)
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint32(b, o.OutputIndex))
}

// sweepShares is C-05's split: fee / count to each deposit and the remainder
// to the last one.
func sweepShares(fee int64, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = fee / int64(n)
	}
	out[n-1] += fee % int64(n)
	return out
}

// NewSweepPlan freezes an exact sweep and checks every rule the C-05 proof
// applies to it: version 2, at most 128 inputs, the main first when included,
// one output paying exactly Q, each deposit's fee share within its captured
// cap and leaving a positive credit, immediately final, and the fee rate
// within the ceiling. Deposit inputs are signed through their wallet leaf
// (BIP342); the main input by key path.
func NewSweepPlan(p SweepProposal) (*Plan, error) {
	if dkg.ValidateDescriptor(p.Wallet) != nil || p.BitcoinGenesis == ([32]byte{}) || p.Transaction == nil || len(p.Deposits) == 0 || p.MaximumSatPerVByte <= 0 || p.MaximumSatPerVByte > bitcoin.MaxSatoshi {
		return nil, errors.New("invalid sweep proposal")
	}
	if p.Resolution != ([32]byte{}) && p.Main == nil {
		return nil, errors.New("a resolution sweep must spend the reserved main")
	}
	var prevouts []bitcoin.PreviousOutput
	var spends []*bitcoin.TapscriptSpend
	if p.Main != nil {
		prevouts = append(prevouts, *p.Main)
		spends = append(spends, nil)
	}
	seen := map[[32]byte]bool{}
	for _, d := range p.Deposits {
		if d.Spend == nil || d.Key != depositKey(d.Prevout.Outpoint) || seen[d.Key] || d.MaximumSweepFee > uint64(bitcoin.MaxSatoshi) || d.TreasuryFee > uint64(bitcoin.MaxSatoshi) {
			return nil, errors.New("invalid sweep deposit")
		}
		seen[d.Key] = true
		prevouts = append(prevouts, d.Prevout)
		spends = append(spends, d.Spend)
	}
	if len(prevouts) > MaxSweepInputs || len(p.Transaction.Inputs) != len(prevouts) {
		return nil, errors.New("sweep input count")
	}
	for i, input := range p.Transaction.Inputs {
		if input == nil || input.Outpoint == nil || *input.Outpoint != prevouts[i].Outpoint {
			return nil, fmt.Errorf("sweep input %d is not the planned main or deposit", i)
		}
	}
	snapshot, err := bitcoin.NewTaprootScriptTransaction(p.Transaction, prevouts, p.Wallet.OutputKey, spends)
	if err != nil {
		return nil, err
	}
	tx := snapshot.Unsigned()
	if err = ImmediatelyFinal(tx); err != nil {
		return nil, fmt.Errorf("sweep must be immediately final: %w", err)
	}
	wallet, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if len(tx.Outputs) != 1 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, wallet) {
		return nil, errors.New("sweep must pay one exact FROST output")
	}
	fee := snapshot.Fee()
	vsize := snapshot.VirtualSize()
	if fee/vsize > p.MaximumSatPerVByte || (fee/vsize == p.MaximumSatPerVByte && fee%vsize != 0) {
		return nil, errors.New("sweep fee rate above the ceiling")
	}
	var capTotal int64
	for i, share := range sweepShares(fee, len(p.Deposits)) {
		d := p.Deposits[i]
		if share > int64(d.MaximumSweepFee) || uint64(d.Prevout.Value) <= d.TreasuryFee+uint64(share) {
			return nil, fmt.Errorf("sweep fee share %d exceeds deposit %d capacity", share, i)
		}
		capTotal += int64(d.MaximumSweepFee)
	}
	descriptorHash, err := dkg.DescriptorHash(p.Wallet)
	if err != nil {
		return nil, err
	}
	si := &sweepIntent{Version: 1, WalletID: dkg.WalletID(p.Wallet.OutputKey), DescriptorHash: descriptorHash, BitcoinGenesis: p.BitcoinGenesis, Snapshot: snapshot.Commitment(), TransactionHash: tx.Hash(), Generation: p.Generation, StateVersion: p.StateVersion, Vault: p.Vault, Resolution: p.Resolution, Fee: fee, MaximumSatPerVByte: p.MaximumSatPerVByte, Transaction: tx}
	if p.Main != nil {
		main := snapshot.PreviousOutputs()[0]
		si.Main = &main
	}
	for i, d := range p.Deposits {
		d.Prevout = snapshot.PreviousOutputs()[len(prevouts)-len(p.Deposits)+i]
		d.Spend = &bitcoin.TapscriptSpend{Leaf: append(bitcoin.Script(nil), d.Spend.Leaf...), Control: append([]byte(nil), d.Spend.Control...)}
		si.Deposits = append(si.Deposits, d)
	}
	encoded, err := json.Marshal(si)
	if err != nil {
		return nil, err
	}
	preimage := sweepPreimage(si)
	conflict := si.Deposits[0].Prevout.Outpoint
	if si.Main != nil {
		conflict = si.Main.Outpoint
	}
	intent := Intent{Version: 2, WalletID: si.WalletID, DescriptorHash: si.DescriptorHash, BitcoinGenesis: si.BitcoinGenesis, TransactionHash: si.Snapshot, Purpose: Sweep, Generation: p.Generation, StateVersion: p.StateVersion, ConflictInput: conflict, Fees: FeePolicy{Minimum: 1, Maximum: capTotal, MaximumSatPerVByte: p.MaximumSatPerVByte}, Transaction: tx, Prevouts: snapshot.PreviousOutputs()}
	return &Plan{snapshot: snapshot, intent: intent, encoded: encoded, preimage: preimage, hash: sha256.Sum256(preimage), sweep: si}, nil
}

// sweepPreimage is host-local: no contract consumes a sweep commitment. The
// snapshot commitment covers the transaction, prevouts, leaves and control
// blocks; the rest binds the C-05 accounting inputs.
func sweepPreimage(s *sweepIntent) []byte {
	b := []byte("keep-core/frost/sweep-intent/v1/")
	for _, h := range [][32]byte{s.Snapshot, s.WalletID, s.DescriptorHash, s.BitcoinGenesis} {
		b = append(b, h[:]...)
	}
	b = binary.BigEndian.AppendUint64(b, s.Generation)
	b = binary.BigEndian.AppendUint64(b, s.StateVersion)
	b = append(b, s.Vault[:]...)
	b = append(b, s.Resolution[:]...)
	if s.Main == nil {
		b = append(b, 0)
	} else {
		b = append(b, 1)
		b = append(b, s.Main.Outpoint.TransactionHash[:]...)
		b = binary.BigEndian.AppendUint32(b, s.Main.Outpoint.OutputIndex)
		b = binary.BigEndian.AppendUint64(b, uint64(s.Main.Value))
	}
	b = binary.BigEndian.AppendUint16(b, uint16(len(s.Deposits)))
	for _, d := range s.Deposits {
		b = append(b, d.Key[:]...)
		b = binary.BigEndian.AppendUint64(b, uint64(d.Prevout.Value))
		b = binary.BigEndian.AppendUint64(b, d.TreasuryFee)
		b = binary.BigEndian.AppendUint64(b, d.MaximumSweepFee)
		b = binary.BigEndian.AppendUint32(b, d.RefundLocktime)
	}
	return binary.BigEndian.AppendUint64(b, uint64(s.MaximumSatPerVByte))
}

// IsSweep reports whether the plan is a K-06 deposit sweep.
func (p *Plan) IsSweep() bool { return p.sweep != nil }

// SweepDeposits returns a copy of the deposit inputs in proof order.
func (p *Plan) SweepDeposits() []SweepDeposit {
	if p.sweep == nil {
		return nil
	}
	out := make([]SweepDeposit, len(p.sweep.Deposits))
	for i, d := range p.sweep.Deposits {
		d.Prevout.PublicKeyScript = append(bitcoin.Script(nil), d.Prevout.PublicKeyScript...)
		d.Spend = &bitcoin.TapscriptSpend{Leaf: append(bitcoin.Script(nil), d.Spend.Leaf...), Control: append([]byte(nil), d.Spend.Control...)}
		out[i] = d
	}
	return out
}

// SweepMain returns the main input of a sweep, or nil for a deposit-only sweep.
func (p *Plan) SweepMain() *bitcoin.PreviousOutput {
	if p.sweep == nil || p.sweep.Main == nil {
		return nil
	}
	main := *p.sweep.Main
	main.PublicKeyScript = append(bitcoin.Script(nil), main.PublicKeyScript...)
	return &main
}

// SweepResolution is the C-04 reservation a resolution sweep resolves.
func (p *Plan) SweepResolution() [32]byte {
	if p.sweep == nil {
		return [32]byte{}
	}
	return p.sweep.Resolution
}

// SweepVault is the vault every deposit of the sweep credits.
func (p *Plan) SweepVault() [20]byte {
	if p.sweep == nil {
		return [20]byte{}
	}
	return p.sweep.Vault
}
