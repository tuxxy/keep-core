package movement

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"sort"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// fake is a controlled C-06/C-09 chain at one head. Unused seam methods are
// left to the embedded nil Reader and panic if reached.
type fake struct {
	Reader
	head        uint64
	hashes      map[uint64][32]byte
	wallets     map[[20]byte]Wallet
	lifecycles  map[[32]byte]Lifecycle
	commitments map[[32]byte]Commitment
	logs        map[[32]byte]CommitmentLog
	custody     map[[32]byte]redemption.Custody
	outputs     map[[32]byte]redemption.CustodyOutput
	incoming    map[[32]byte]Incoming
	incomingLog []IncomingLog
	counters    Counters
	obligations map[[32]byte]uint64
	params      Parameters
	ready       [][32]byte
	submitted   [][20]byte
	finalized   int
	// late, when set, is applied after the first obligations read.
	late func()
}

func (f *fake) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: f.head, Hash: f.hash(f.head)}, nil
}
func (f *fake) hash(n uint64) [32]byte {
	if h, ok := f.hashes[n]; ok {
		return h
	}
	return [32]byte{byte(n), byte(n >> 8), 0xbb}
}
func (f *fake) HeaderHash(_ context.Context, n uint64) ([32]byte, error) { return f.hash(n), nil }
func (f *fake) Wallet(_ context.Context, _ uint64, l [20]byte) (Wallet, error) {
	w, ok := f.wallets[l]
	if !ok {
		return Wallet{}, ErrNotFound
	}
	return w, nil
}
func (f *fake) ReadyWallets(context.Context, uint64, uint64) ([][32]byte, error) {
	return f.ready, nil
}
func (f *fake) Lifecycle(_ context.Context, _ uint64, w [32]byte) (Lifecycle, error) {
	return f.lifecycles[w], nil
}
func (f *fake) Commitment(_ context.Context, _ uint64, w [32]byte, _ uint32) (Commitment, error) {
	return f.commitments[w], nil
}
func (f *fake) CommitmentLog(_ context.Context, w [32]byte, _ uint32) (CommitmentLog, error) {
	l, ok := f.logs[w]
	if !ok {
		return CommitmentLog{}, ErrNotFound
	}
	return l, nil
}
func (f *fake) Custody(_ context.Context, _ uint64, w [32]byte) (redemption.Custody, error) {
	return f.custody[w], nil
}
func (f *fake) CustodyOutput(_ context.Context, _ uint64, k [32]byte) (redemption.CustodyOutput, error) {
	return f.outputs[k], nil
}
func (f *fake) MovementParameters(context.Context, uint64) (Parameters, error) {
	return f.params, nil
}
func (f *fake) Counters(context.Context, uint64, [32]byte) (Counters, error) {
	return f.counters, nil
}
func (f *fake) IncomingLogs(context.Context, [32]byte, uint64, uint64) ([]IncomingLog, error) {
	return f.incomingLog, nil
}
func (f *fake) Incoming(_ context.Context, _ uint64, k [32]byte) (Incoming, error) {
	return f.incoming[k], nil
}
func (f *fake) Obligations(_ context.Context, _ uint64, w [32]byte) (uint64, error) {
	open := f.obligations[w]
	if f.late != nil {
		late := f.late
		f.late = nil
		late()
	}
	return open, nil
}
func (f *fake) SubmitCommitment(_ context.Context, w [32]byte, _ uint16, targets [][20]byte) error {
	f.submitted = targets
	f.commitments[w] = Commitment{MainOutpoint: f.custody[w].MainOutpoint, MaxFee: f.params.MovingFundsTxMaxTotalFee, Targets: targets}
	f.logs[w] = CommitmentLog{Log: Log{Block: f.head, BlockHash: f.hash(f.head)}, Wallet: w}
	return nil
}
func (f *fake) FinalizeClosing(_ context.Context, w [32]byte) error {
	f.finalized++
	l := Label(w)
	wallet := f.wallets[l]
	wallet.State = StateClosed
	f.wallets[l] = wallet
	return nil
}
func (f *fake) SubmitMovementProof(context.Context, [32]byte, Proof) error      { return nil }
func (f *fake) SubmitIncomingSweepProof(context.Context, [32]byte, Proof) error { return nil }
func (f *fake) NotifyCloseable(context.Context, [32]byte) error                 { return nil }
func (f *fake) NotifyBelowDust(context.Context, [32]byte) error                 { return nil }
func (f *fake) ResetMovingFundsTimeout(context.Context, [32]byte) error         { return nil }

type journal struct{ records map[[32]byte][]byte }

func (j *journal) RetainSigningRecord(_ context.Context, id [32]byte, raw []byte) error {
	if existing, ok := j.records[id]; ok && !bytes.Equal(existing, raw) {
		return errors.New("record conflict")
	}
	j.records[id] = raw
	return nil
}
func (j *journal) ReadSigningRecord(_ context.Context, id [32]byte) ([]byte, error) {
	raw, ok := j.records[id]
	if !ok {
		return nil, store.ErrMissing
	}
	return raw, nil
}

func descriptor(t *testing.T, secret byte) dkg.Descriptor {
	t.Helper()
	s := make([]byte, 32)
	s[31] = secret
	_, public := btcec.PrivKeyFromBytes(s)
	var q [32]byte
	copy(q[:], schnorr.SerializePubKey(public))
	d := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address{1}, Epoch: 17, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{secret}, OutputKey: q}
	if err := dkg.ValidateDescriptor(d); err != nil {
		t.Fatal(err)
	}
	return d
}

type world struct {
	f       *fake
	view    *View
	source  Wallet
	targets []Wallet
	main    bitcoin.PreviousOutput
}

// newWorld is a MovingFunds source with a 300,000 sat main and three
// eligible targets, at head 100 with finality 2.
func newWorld(t *testing.T) *world {
	t.Helper()
	f := &fake{head: 100, hashes: map[uint64][32]byte{}, wallets: map[[20]byte]Wallet{}, lifecycles: map[[32]byte]Lifecycle{}, commitments: map[[32]byte]Commitment{}, logs: map[[32]byte]CommitmentLog{}, custody: map[[32]byte]redemption.Custody{}, outputs: map[[32]byte]redemption.CustodyOutput{}, incoming: map[[32]byte]Incoming{}, obligations: map[[32]byte]uint64{}, params: Parameters{MovingFundsTxMaxTotalFee: 5_000, MovingFundsDustThreshold: 20_000, MovedFundsSweepTxMaxTotalFee: 5_000, WalletMaxBtcTransfer: 100_000}}
	w := &world{f: f, view: &View{Chain: f, Policy: Policy{FinalityBlocks: 2}}}
	add := func(secret byte, state State) Wallet {
		d := descriptor(t, secret)
		h, err := dkg.DescriptorHash(d)
		if err != nil {
			t.Fatal(err)
		}
		wallet := Wallet{Descriptor: d, ID: dkg.WalletID(d.OutputKey), DescriptorHash: h, State: state, CapabilityVersion: 1}
		f.wallets[Label(wallet.ID)] = wallet
		f.ready = append(f.ready, wallet.ID)
		return wallet
	}
	w.source = add(10, StateMovingFunds)
	for i := byte(0); i < 4; i++ {
		w.targets = append(w.targets, add(20+i, StateLive))
	}
	sort.Slice(w.targets, func(i, j int) bool {
		a, b := Label(w.targets[i].ID), Label(w.targets[j].ID)
		return bytes.Compare(a[:], b[:]) < 0
	})
	f.counters.EligibleTargets = 4
	script, _ := bitcoin.PayToTaproot(w.source.Descriptor.OutputKey)
	w.main = bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{77}, OutputIndex: 0}, Value: 300_000, PublicKeyScript: script}
	key := OutpointKey(w.main.Outpoint)
	f.custody[w.source.ID] = redemption.Custody{Generation: 0, StateVersion: 2, Value: 300_000, MainOutpoint: key}
	f.outputs[key] = redemption.CustodyOutput{WalletID: w.source.ID, Value: 300_000}
	return w
}

type unspent struct{ outs []bitcoin.PreviousOutput }

func (u unspent) UnspentByScript(context.Context, bitcoin.Script) ([]bitcoin.PreviousOutput, error) {
	return u.outs, nil
}

func (w *world) planner() Planner {
	return Planner{View: w.view, Bitcoin: unspent{[]bitcoin.PreviousOutput{w.main}}, Wallet: w.source.Descriptor, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: 2, MaximumSatPerVByte: 20}
}

func (w *world) committed(t *testing.T) *signing.Plan {
	t.Helper()
	c := Commitments{Chain: w.f, Planner: w.planner(), Seat: 0}
	targets, err := c.Commit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected three targets for 300,000 sat at a 100,000 cap, got %d", len(targets))
	}
	commitment := w.f.commitments[w.source.ID]
	for _, label := range targets {
		commitment.DescriptorHashes = append(commitment.DescriptorHashes, w.f.wallets[label].DescriptorHash)
	}
	w.f.commitments[w.source.ID] = commitment
	w.f.head += 2
	plan, err := w.planner().Movement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestCommitmentChoosesLowestEligibleLabels(t *testing.T) {
	w := newWorld(t)
	// The lowest label leaves eligibility before the commitment.
	lowest := w.targets[0]
	lowest.Quarantined = true
	w.f.wallets[Label(lowest.ID)] = lowest
	w.committed(t)
	want := [][20]byte{Label(w.targets[1].ID), Label(w.targets[2].ID), Label(w.targets[3].ID)}
	if !sameLabels(w.f.submitted, want) {
		t.Fatal("commitment must name the lowest eligible labels in order")
	}
	// A second Commit with the same targets recorded is success, not a resubmission.
	w.f.submitted = nil
	if _, err := (Commitments{Chain: w.f, Planner: w.planner()}).Commit(context.Background()); err != nil || w.f.submitted != nil {
		t.Fatal("recorded commitment must be reused", err)
	}
}

func TestNoEligibleTargetRefusesCommitment(t *testing.T) {
	w := newWorld(t)
	w.f.counters.EligibleTargets = 0
	if _, err := (Commitments{Chain: w.f, Planner: w.planner()}).Commit(context.Background()); !errors.Is(err, ErrNoTarget) {
		t.Fatal(err)
	}
}

func TestMovementAuthorityUsesTheCommitmentOnly(t *testing.T) {
	w := newWorld(t)
	plan := w.committed(t)
	a := &Authority{View: w.view}
	ctx := context.Background()
	if _, err := a.AuthorizeSweep(ctx, plan); err != nil {
		t.Fatal(err)
	}
	// Every committed target leaves eligibility: the plan stays authorized.
	for _, label := range w.f.commitments[w.source.ID].Targets {
		target := w.f.wallets[label]
		target.State, target.Quarantined = StateClosing, true
		w.f.wallets[label] = target
	}
	if _, err := a.AuthorizeSweep(ctx, plan); err != nil {
		t.Fatal("a target state change must not affect the movement", err)
	}
	for name, mutate := range map[string]func(w *world){
		"unfinal commitment": func(w *world) { w.f.head = w.f.logs[w.source.ID].Block + 1 },
		"proven":             func(w *world) { c := w.f.commitments[w.source.ID]; c.ProvenAt = 1; w.f.commitments[w.source.ID] = c },
		"new period":         func(w *world) { l := w.f.lifecycles[w.source.ID]; l.Period = 1; w.f.lifecycles[w.source.ID] = l },
		"source closed": func(w *world) {
			s := w.f.wallets[Label(w.source.ID)]
			s.State = StateClosed
			w.f.wallets[Label(w.source.ID)] = s
		},
		"main replaced": func(w *world) {
			c := w.f.custody[w.source.ID]
			c.MainOutpoint = [32]byte{9}
			w.f.custody[w.source.ID] = c
		},
		"reserved main": func(w *world) {
			c := w.f.custody[w.source.ID]
			c.ActiveReservation = [32]byte{9}
			w.f.custody[w.source.ID] = c
		},
		"other target recorded": func(w *world) {
			c := w.f.commitments[w.source.ID]
			c.Targets = append([][20]byte(nil), c.Targets...)
			c.Targets[0] = Label(w.targets[3].ID)
			w.f.commitments[w.source.ID] = c
		},
		"committed fee cap below the fee": func(w *world) {
			c := w.f.commitments[w.source.ID]
			c.MaxFee = 1
			w.f.commitments[w.source.ID] = c
		},
		"non-canonical log": func(w *world) {
			w.f.hashes[w.f.logs[w.source.ID].Block] = [32]byte{0xee}
		},
	} {
		w := newWorld(t)
		plan := w.committed(t)
		mutate(w)
		if _, err := (&Authority{View: w.view}).AuthorizeSweep(ctx, plan); err == nil {
			t.Errorf("%s: authorized", name)
		}
	}
}

// The fee cap is the one recorded at commitment. Raising the parameter
// afterwards must not raise the planned or authorized fee (the proof and the
// C-08 defense would reject it), and lowering it must not block the movement.
func TestMovementUsesCommittedFeeCap(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.f.params.MovingFundsTxMaxTotalFee = 300
	w.committed(t)
	w.f.params.MovingFundsTxMaxTotalFee = 5_000
	p := w.planner()
	p.FeeRate = 20
	plan, err := p.Movement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fee := plan.Snapshot().Fee(); fee != 300 {
		t.Fatalf("fee %d after a cap increase, want the committed 300", fee)
	}
	if _, err = (&Authority{View: w.view}).AuthorizeSweep(ctx, plan); err != nil {
		t.Fatal("committed-cap movement refused:", err)
	}
	w.f.params.MovingFundsTxMaxTotalFee = 1
	if _, err = (&Authority{View: w.view}).AuthorizeSweep(ctx, plan); err != nil {
		t.Fatal("a later cap decrease must not block the committed movement:", err)
	}
	// A plan built under the raised parameter is above the committed cap.
	c := w.f.commitments[w.source.ID]
	c.MaxFee = 5_000
	w.f.commitments[w.source.ID] = c
	high, err := p.Movement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.MaxFee = 300
	w.f.commitments[w.source.ID] = c
	if fee := high.Snapshot().Fee(); fee <= 300 {
		t.Fatalf("control plan fee %d is not above the committed cap", fee)
	}
	if _, err = (&Authority{View: w.view}).AuthorizeSweep(ctx, high); !errors.Is(err, ErrPlanMismatch) {
		t.Fatalf("fee above the committed cap authorized: %v", err)
	}
}

func TestIncomingAuthorityAndPlan(t *testing.T) {
	w := newWorld(t)
	target := w.targets[0]
	movementTx := [32]byte{0x55}
	key := OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(movementTx), OutputIndex: 1})
	w.f.incoming[key] = Incoming{Target: target.ID, Source: w.source.ID, Value: 99_000, State: IncomingPending}
	w.f.incomingLog = []IncomingLog{{Log: Log{Block: 90, BlockHash: w.f.hash(90)}, Outpoint: key, Target: target.ID, Source: w.source.ID, Value: 99_000, TransactionHash: movementTx, OutputIndex: 1}}
	p := w.planner()
	p.Wallet = target.Descriptor
	p.Bitcoin = unspent{}
	plan, err := p.IncomingSweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsIncomingSweep() || plan.MovementMain() != nil || len(plan.IncomingOutputs()) != 1 {
		t.Fatal("incoming plan shape")
	}
	a := &Authority{View: w.view}
	if _, err = a.AuthorizeSweep(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	// A Closing target still sweeps (V-08 reading 2).
	closing := w.f.wallets[Label(target.ID)]
	closing.State = StateClosing
	w.f.wallets[Label(target.ID)] = closing
	if _, err = a.AuthorizeSweep(context.Background(), plan); err != nil {
		t.Fatal("a Closing target must sweep its incoming", err)
	}
	// A quarantined target needs a directive.
	closing.Quarantined = true
	w.f.wallets[Label(target.ID)] = closing
	if _, err = a.AuthorizeSweep(context.Background(), plan); !errors.Is(err, redemption.ErrContained) {
		t.Fatal("quarantined target swept without a directive", err)
	}
	closing.Quarantined = false
	w.f.wallets[Label(target.ID)] = closing
	// Swept already: settled, never signed again.
	w.f.incoming[key] = Incoming{Target: target.ID, Value: 99_000, State: IncomingSwept}
	if _, err = a.AuthorizeSweep(context.Background(), plan); !errors.Is(err, ErrSettled) {
		t.Fatal(err)
	}
}

// The view returns incoming records in log order and the planner sorts inputs
// by key; a log order that is the reverse of the key order still authorizes.
func TestIncomingAuthorityMatchesRecordsByKey(t *testing.T) {
	w := newWorld(t)
	target := w.targets[0]
	type output struct {
		tx  [32]byte
		key [32]byte
	}
	var outs []output
	for _, b := range []byte{0x55, 0x66} {
		tx := [32]byte{b}
		outs = append(outs, output{tx, OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(tx), OutputIndex: 1})})
	}
	sort.Slice(outs, func(i, j int) bool { return bytes.Compare(outs[i].key[:], outs[j].key[:]) > 0 })
	for i, o := range outs {
		w.f.incoming[o.key] = Incoming{Target: target.ID, Source: w.source.ID, Value: 99_000, State: IncomingPending}
		block := uint64(90 + i)
		w.f.incomingLog = append(w.f.incomingLog, IncomingLog{Log: Log{Block: block, BlockHash: w.f.hash(block)}, Outpoint: o.key, Target: target.ID, Source: w.source.ID, Value: 99_000, TransactionHash: o.tx, OutputIndex: 1})
	}
	p := w.planner()
	p.Wallet = target.Descriptor
	p.Bitcoin = unspent{}
	plan, err := p.IncomingSweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inputs := plan.IncomingOutputs()
	if len(inputs) != 2 || OutpointKey(inputs[0].Outpoint) != outs[1].key {
		t.Fatal("plan must list incoming inputs in ascending key order")
	}
	if _, err = (&Authority{View: w.view}).AuthorizeSweep(context.Background(), plan); err != nil {
		t.Fatal("log order must not refuse a valid incoming sweep", err)
	}
}

// A K-06 sweep that aborted still holds the main C-06 committed. The
// authorized movement takes it over; a movement-held main refuses a sweep.
func TestCommittedMovementSupersedesAbortedSweep(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	plan := w.committed(t)
	lock := &deposit.MainLock{Journal: &journal{records: map[[32]byte][]byte{}}}
	if err := lock.AcquireSweep(ctx, w.source.ID, w.main.Outpoint, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := lock.AcquireMovement(ctx, deposit.CommittedMovement{}); err == nil {
		t.Fatal("movement took a sweep's main without evidence")
	}
	committed, err := deposit.AuthorizeMovement(ctx, &Authority{View: w.view}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.AcquireMovement(ctx, committed); err != nil {
		t.Fatal("committed movement must supersede an aborted sweep", err)
	}
	current, err := lock.Current(ctx, w.source.ID, OutpointKey(w.main.Outpoint))
	if err != nil || current.Purpose != deposit.MovementMain || current.Subject != plan.Commitment() || current.Reason != "committed movement supersedes sweep" {
		t.Fatal("movement record", current, err)
	}
	if err = (deposit.Guard{Lock: lock}).Permit(ctx, plan, [32]byte{}); err != nil {
		t.Fatal("grant guard refused the movement", err)
	}
	if err = lock.AcquireSweep(ctx, w.source.ID, w.main.Outpoint, [32]byte{2}); !errors.Is(err, deposit.ErrMainBusy) {
		t.Fatal("a sweep took a committed movement's main", err)
	}
	// A proven commitment gives no evidence.
	c := w.f.commitments[w.source.ID]
	c.ProvenAt = 1
	w.f.commitments[w.source.ID] = c
	if _, err = deposit.AuthorizeMovement(ctx, &Authority{View: w.view}, plan); !errors.Is(err, ErrSettled) {
		t.Fatal("evidence for a proven commitment", err)
	}
}

func TestClosureNeedsAnEmptyLedgerTwice(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	j := &journal{records: map[[32]byte][]byte{}}
	c := Closure{Chain: w.f, Policy: Policy{FinalityBlocks: 2}, Journal: j}
	w.f.obligations[w.source.ID] = OpenPendingIncoming | OpenChallenge
	if err := c.Close(ctx, w.source.ID); !errors.Is(err, ErrObligations) || w.f.finalized != 0 {
		t.Fatal("closed with obligations", err)
	}
	// Clear at the finalized head, then a late challenge before the notice.
	w.f.obligations[w.source.ID] = 0
	w.f.late = func() { w.f.obligations[w.source.ID] = OpenChallenge }
	if err := c.Close(ctx, w.source.ID); !errors.Is(err, ErrObligations) || w.f.finalized != 0 {
		t.Fatal("late arrival ignored", err)
	}
	w.f.obligations[w.source.ID] = 0
	if err := c.Close(ctx, w.source.ID); err != nil || w.f.finalized != 1 {
		t.Fatal(err)
	}
	if archived, err := IsArchived(ctx, j, w.source.ID); err != nil || !archived {
		t.Fatal("not archived")
	}
	// Idempotent after closure; the archive deletes nothing.
	if err := c.Close(ctx, w.source.ID); err != nil || w.f.finalized != 1 {
		t.Fatal(err)
	}
}
