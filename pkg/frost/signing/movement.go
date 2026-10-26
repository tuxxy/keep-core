package signing

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// Movement is the K-07 fund movement purpose: a FROST source pays its whole
// main to the targets of its C-06 commitment. IncomingSweep is a target's
// sweep of the moved outputs it received, together with its own main. Like
// K-06 sweeps, both are authorized by the host from chain state at every
// grant (the executor's SweepAuthority), not by a pre-signing certificate.
const (
	Movement      = "movement"
	IncomingSweep = "incoming-sweep"
)

// MovementTarget is one committed target: its label, the descriptor hash the
// commitment recorded, and the output key Q of that descriptor.
type MovementTarget struct {
	Label          [20]byte
	DescriptorHash [32]byte
	OutputKey      [32]byte
}

// MovementProposal is a movement as the C-06 proof will check it: one input
// (the committed main), one output per committed target in order, the equal
// split with the remainder on the last output, and the fee within the cap.
type MovementProposal struct {
	Wallet                   dkg.Descriptor
	BitcoinGenesis           [32]byte
	Generation, StateVersion uint64
	Period                   uint32
	Main                     bitcoin.PreviousOutput
	Targets                  []MovementTarget
	MaximumFee               int64
	MaximumSatPerVByte       int64
	Transaction              *bitcoin.Transaction
}

// IncomingSweepProposal is a target's sweep: its main first when it has one,
// then pending incoming outputs, one output paying its own Q.
type IncomingSweepProposal struct {
	Wallet                   dkg.Descriptor
	BitcoinGenesis           [32]byte
	Generation, StateVersion uint64
	Main                     *bitcoin.PreviousOutput
	Incoming                 []bitcoin.PreviousOutput
	MaximumFee               int64
	MaximumSatPerVByte       int64
	Transaction              *bitcoin.Transaction
}

type movementIntent struct {
	Version            uint8
	Kind               string
	WalletID           [32]byte
	DescriptorHash     [32]byte
	BitcoinGenesis     [32]byte
	Snapshot           [32]byte
	TransactionHash    [32]byte
	Generation         uint64
	StateVersion       uint64
	Period             uint32
	Main               *bitcoin.PreviousOutput  `json:",omitempty"`
	Targets            []MovementTarget         `json:",omitempty"`
	Incoming           []bitcoin.PreviousOutput `json:",omitempty"`
	Fee                int64
	MaximumFee         int64
	MaximumSatPerVByte int64
	Transaction        *bitcoin.Transaction
}

// MovementSplit is the C-06 and legacy equal split of total over n outputs.
func MovementSplit(total int64, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = total / int64(n)
	}
	out[n-1] += total % int64(n)
	return out
}

func feeRateWithin(fee, vsize, ceiling int64) bool {
	return fee/vsize < ceiling || (fee/vsize == ceiling && fee%vsize == 0)
}

// NewMovementPlan freezes an exact movement and checks every rule the C-06
// proof applies to it.
func NewMovementPlan(p MovementProposal) (*Plan, error) {
	if dkg.ValidateDescriptor(p.Wallet) != nil || p.BitcoinGenesis == ([32]byte{}) || p.Transaction == nil || len(p.Targets) == 0 || p.MaximumFee <= 0 || p.MaximumFee > bitcoin.MaxSatoshi || p.MaximumSatPerVByte <= 0 || p.MaximumSatPerVByte > bitcoin.MaxSatoshi {
		return nil, errors.New("invalid movement proposal")
	}
	if len(p.Transaction.Inputs) != 1 || p.Transaction.Inputs[0] == nil || p.Transaction.Inputs[0].Outpoint == nil || *p.Transaction.Inputs[0].Outpoint != p.Main.Outpoint {
		return nil, errors.New("movement must spend exactly the committed main")
	}
	snapshot, err := bitcoin.NewTaprootTransaction(p.Transaction, []bitcoin.PreviousOutput{p.Main}, p.Wallet.OutputKey)
	if err != nil {
		return nil, err
	}
	tx := snapshot.Unsigned()
	if err = ImmediatelyFinal(tx); err != nil {
		return nil, fmt.Errorf("movement must be immediately final: %w", err)
	}
	if len(tx.Outputs) != len(p.Targets) {
		return nil, errors.New("movement must pay exactly the committed targets")
	}
	var total int64
	for i, t := range p.Targets {
		script, err := bitcoin.PayToTaproot(t.OutputKey)
		if err != nil || t.Label == ([20]byte{}) || t.DescriptorHash == ([32]byte{}) || !bytes.Equal(tx.Outputs[i].PublicKeyScript, script) {
			return nil, fmt.Errorf("movement output %d is not committed target %d", i, i)
		}
		total += tx.Outputs[i].Value
	}
	for i, v := range MovementSplit(total, len(p.Targets)) {
		if tx.Outputs[i].Value != v {
			return nil, fmt.Errorf("movement output %d breaks the equal split", i)
		}
	}
	fee := snapshot.Fee()
	if fee <= 0 || fee > p.MaximumFee || !feeRateWithin(fee, snapshot.VirtualSize(), p.MaximumSatPerVByte) {
		return nil, errors.New("movement fee outside the committed cap or rate")
	}
	main := snapshot.PreviousOutputs()[0]
	mi := &movementIntent{Kind: Movement, Period: p.Period, Main: &main, Targets: append([]MovementTarget(nil), p.Targets...)}
	return movementPlan(p.Wallet, p.BitcoinGenesis, p.Generation, p.StateVersion, snapshot, mi, fee, p.MaximumFee, p.MaximumSatPerVByte, main.Outpoint)
}

// NewIncomingSweepPlan freezes an exact incoming sweep and checks every rule
// the C-06 receiving sweep proof applies to it.
func NewIncomingSweepPlan(p IncomingSweepProposal) (*Plan, error) {
	if dkg.ValidateDescriptor(p.Wallet) != nil || p.BitcoinGenesis == ([32]byte{}) || p.Transaction == nil || len(p.Incoming) == 0 || p.MaximumFee <= 0 || p.MaximumFee > bitcoin.MaxSatoshi || p.MaximumSatPerVByte <= 0 || p.MaximumSatPerVByte > bitcoin.MaxSatoshi {
		return nil, errors.New("invalid incoming sweep proposal")
	}
	var prevouts []bitcoin.PreviousOutput
	if p.Main != nil {
		prevouts = append(prevouts, *p.Main)
	}
	seen := map[bitcoin.TransactionOutpoint]bool{}
	for _, in := range p.Incoming {
		if seen[in.Outpoint] || (p.Main != nil && in.Outpoint == p.Main.Outpoint) {
			return nil, errors.New("duplicate incoming sweep input")
		}
		seen[in.Outpoint] = true
		prevouts = append(prevouts, in)
	}
	if len(prevouts) > MaxSweepInputs || len(p.Transaction.Inputs) != len(prevouts) {
		return nil, errors.New("incoming sweep input count")
	}
	for i, input := range p.Transaction.Inputs {
		if input == nil || input.Outpoint == nil || *input.Outpoint != prevouts[i].Outpoint {
			return nil, fmt.Errorf("incoming sweep input %d is not planned", i)
		}
	}
	snapshot, err := bitcoin.NewTaprootTransaction(p.Transaction, prevouts, p.Wallet.OutputKey)
	if err != nil {
		return nil, err
	}
	tx := snapshot.Unsigned()
	if err = ImmediatelyFinal(tx); err != nil {
		return nil, fmt.Errorf("incoming sweep must be immediately final: %w", err)
	}
	wallet, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	if len(tx.Outputs) != 1 || !bytes.Equal(tx.Outputs[0].PublicKeyScript, wallet) {
		return nil, errors.New("incoming sweep must pay one exact FROST output")
	}
	fee := snapshot.Fee()
	if fee <= 0 || fee > p.MaximumFee || !feeRateWithin(fee, snapshot.VirtualSize(), p.MaximumSatPerVByte) {
		return nil, errors.New("incoming sweep fee outside the cap or rate")
	}
	all := snapshot.PreviousOutputs()
	mi := &movementIntent{Kind: IncomingSweep}
	if p.Main != nil {
		main := all[0]
		mi.Main = &main
		all = all[1:]
	}
	mi.Incoming = append([]bitcoin.PreviousOutput(nil), all...)
	return movementPlan(p.Wallet, p.BitcoinGenesis, p.Generation, p.StateVersion, snapshot, mi, fee, p.MaximumFee, p.MaximumSatPerVByte, snapshot.PreviousOutputs()[0].Outpoint)
}

// RestoreIncomingSweepPlan validates a retained public intent with the same
// constructor used before signing. It never treats decoded bytes as authority;
// the caller must still check the current chain before signing or broadcasting.
func RestoreIncomingSweepPlan(wallet dkg.Descriptor, raw []byte) (*Plan, error) {
	if len(raw) > 1<<20 {
		return nil, errors.New("incoming intent too large")
	}
	var intent movementIntent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || intent.Version != 1 || intent.Kind != IncomingSweep {
		return nil, errors.New("invalid retained incoming intent")
	}
	plan, err := NewIncomingSweepPlan(IncomingSweepProposal{Wallet: wallet, BitcoinGenesis: intent.BitcoinGenesis, Generation: intent.Generation, StateVersion: intent.StateVersion, Main: intent.Main, Incoming: intent.Incoming, MaximumFee: intent.MaximumFee, MaximumSatPerVByte: intent.MaximumSatPerVByte, Transaction: intent.Transaction})
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(plan.IntentBytes(), raw) {
		return nil, errors.New("retained incoming intent does not match validated plan")
	}
	return plan, nil
}

func movementPlan(wallet dkg.Descriptor, genesis [32]byte, generation, stateVersion uint64, snapshot *bitcoin.TaprootTransaction, mi *movementIntent, fee, maxFee, maxRate int64, conflict bitcoin.TransactionOutpoint) (*Plan, error) {
	descriptorHash, err := dkg.DescriptorHash(wallet)
	if err != nil {
		return nil, err
	}
	tx := snapshot.Unsigned()
	mi.Version = 1
	mi.WalletID = dkg.WalletID(wallet.OutputKey)
	mi.DescriptorHash = descriptorHash
	mi.BitcoinGenesis = genesis
	mi.Snapshot = snapshot.Commitment()
	mi.TransactionHash = tx.Hash()
	mi.Generation, mi.StateVersion = generation, stateVersion
	mi.Fee, mi.MaximumFee, mi.MaximumSatPerVByte = fee, maxFee, maxRate
	mi.Transaction = tx
	encoded, err := json.Marshal(mi)
	if err != nil {
		return nil, err
	}
	preimage := movementPreimage(mi)
	intent := Intent{Version: 2, WalletID: mi.WalletID, DescriptorHash: descriptorHash, BitcoinGenesis: genesis, TransactionHash: mi.Snapshot, Purpose: mi.Kind, Generation: generation, StateVersion: stateVersion, ConflictInput: conflict, Fees: FeePolicy{Minimum: 1, Maximum: maxFee, MaximumSatPerVByte: maxRate}, Transaction: tx, Prevouts: snapshot.PreviousOutputs()}
	return &Plan{snapshot: snapshot, intent: intent, encoded: encoded, preimage: preimage, hash: sha256.Sum256(preimage), movement: mi}, nil
}

// movementPreimage is host-local; C-06 binds the commitment on chain. The
// snapshot commitment covers the transaction and prevouts; the rest binds the
// purpose, the period and the committed targets.
func movementPreimage(m *movementIntent) []byte {
	b := []byte("keep-core/frost/movement-intent/v1/")
	b = append(b, byte(len(m.Kind)))
	b = append(b, m.Kind...)
	for _, h := range [][32]byte{m.Snapshot, m.WalletID, m.DescriptorHash, m.BitcoinGenesis} {
		b = append(b, h[:]...)
	}
	b = binary.BigEndian.AppendUint64(b, m.Generation)
	b = binary.BigEndian.AppendUint64(b, m.StateVersion)
	b = binary.BigEndian.AppendUint32(b, m.Period)
	b = binary.BigEndian.AppendUint16(b, uint16(len(m.Targets)))
	for _, t := range m.Targets {
		b = append(b, t.Label[:]...)
		b = append(b, t.DescriptorHash[:]...)
		b = append(b, t.OutputKey[:]...)
	}
	b = binary.BigEndian.AppendUint64(b, uint64(m.MaximumFee))
	return binary.BigEndian.AppendUint64(b, uint64(m.MaximumSatPerVByte))
}

// IsMovement reports whether the plan is a K-07 movement.
func (p *Plan) IsMovement() bool { return p.movement != nil && p.movement.Kind == Movement }

// IsIncomingSweep reports whether the plan is a K-07 incoming sweep.
func (p *Plan) IsIncomingSweep() bool {
	return p.movement != nil && p.movement.Kind == IncomingSweep
}

// hostAuthorized plans are authorized by the host's SweepAuthority at every
// grant instead of a C-04 reservation: K-06 sweeps and K-07 movement plans.
func (p *Plan) hostAuthorized() bool { return p.sweep != nil || p.movement != nil }

// MovementMain is the main output a movement or incoming sweep spends, or nil.
func (p *Plan) MovementMain() *bitcoin.PreviousOutput {
	if p.movement == nil || p.movement.Main == nil {
		return nil
	}
	main := *p.movement.Main
	main.PublicKeyScript = append(bitcoin.Script(nil), main.PublicKeyScript...)
	return &main
}

// MovementTargets returns the committed targets of a movement.
func (p *Plan) MovementTargets() []MovementTarget {
	if p.movement == nil {
		return nil
	}
	return append([]MovementTarget(nil), p.movement.Targets...)
}

// MovementPeriod is the C-06 commitment period of a movement.
func (p *Plan) MovementPeriod() uint32 {
	if p.movement == nil {
		return 0
	}
	return p.movement.Period
}

// IncomingOutputs returns the incoming inputs of an incoming sweep.
func (p *Plan) IncomingOutputs() []bitcoin.PreviousOutput {
	if p.movement == nil {
		return nil
	}
	out := make([]bitcoin.PreviousOutput, len(p.movement.Incoming))
	for i, in := range p.movement.Incoming {
		in.PublicKeyScript = append(bitcoin.Script(nil), in.PublicKeyScript...)
		out[i] = in
	}
	return out
}
