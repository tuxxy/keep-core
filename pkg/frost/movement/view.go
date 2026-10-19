package movement

import (
	"context"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// Refusals of a movement or incoming sweep. Each stops it before any grant.
var (
	ErrNotFinal     = errors.New("FROST movement record is not finalized")
	ErrPlanMismatch = errors.New("FROST movement plan differs from the C-06 record")
	ErrWalletState  = errors.New("FROST wallet state does not allow this movement")
	ErrSettled      = errors.New("FROST movement already settled")
)

// Policy is the K-07 timing input: the Ethereum depth at which a commitment
// or incoming record is final (D-09, local test value OPEN), and the first
// block to scan for logs.
type Policy struct {
	FinalityBlocks uint64
	From           uint64
}

// Source is one consistent read of a moving wallet.
type Source struct {
	Head       redemption.Head
	Wallet     Wallet
	Lifecycle  Lifecycle
	Commitment Commitment
	// Log is the commitment's event; nil when nothing is committed.
	Log        *CommitmentLog
	Custody    redemption.Custody
	MainValue  uint64
	Targets    []Wallet
	Parameters Parameters
}

// IncomingOutput is one recorded incoming output with its log.
type IncomingOutput struct {
	Log    IncomingLog
	Record Incoming
}

// Target is one consistent read of a receiving wallet.
type Target struct {
	Head       redemption.Head
	Wallet     Wallet
	Custody    redemption.Custody
	Incoming   []IncomingOutput
	Parameters Parameters
}

// View reads C-06 state for planning and authorization.
type View struct {
	Chain    Reader
	Policy   Policy
	boundary func(string) error
}

func (v *View) step(name string) error {
	if v.boundary != nil {
		return v.boundary(name)
	}
	return nil
}

func (v *View) canonical(ctx context.Context, head redemption.Head, log Log) error {
	if log.Block > head.Number {
		return redemption.ErrInconsistentRead
	}
	hash, err := v.Chain.HeaderHash(ctx, log.Block)
	if err != nil {
		return err
	}
	if hash != log.BlockHash {
		return fmt.Errorf("%w: log block %d is not canonical", ErrNotFinal, log.Block)
	}
	return nil
}

// Final reports whether a log is at or below the finality depth.
func (v *View) Final(head redemption.Head, log Log) bool {
	return head.Number >= log.Block && head.Number-log.Block >= v.Policy.FinalityBlocks
}

func (v *View) recheck(ctx context.Context, head redemption.Head) error {
	again, err := v.Chain.HeaderHash(ctx, head.Number)
	if err != nil {
		return err
	}
	if again != head.Hash {
		return redemption.ErrInconsistentRead
	}
	return nil
}

// Source reads a source wallet, its current commitment and every committed
// target by its committed label. Targets are read for their keys only; their
// current state never changes what the commitment pays.
func (v *View) Source(ctx context.Context, wallet [32]byte) (Source, error) {
	if v.Chain == nil || v.Policy.FinalityBlocks == 0 {
		return Source{}, errors.New("incomplete movement view")
	}
	if err := v.step("before-movement-read"); err != nil {
		return Source{}, err
	}
	head, err := v.Chain.Head(ctx)
	if err != nil {
		return Source{}, err
	}
	s := Source{Head: head}
	if s.Wallet, err = v.Chain.Wallet(ctx, head.Number, Label(wallet)); err != nil {
		return Source{}, err
	}
	if s.Lifecycle, err = v.Chain.Lifecycle(ctx, head.Number, wallet); err != nil {
		return Source{}, err
	}
	if s.Commitment, err = v.Chain.Commitment(ctx, head.Number, wallet, s.Lifecycle.Period); err != nil {
		return Source{}, err
	}
	if s.Commitment.MainOutpoint != ([32]byte{}) {
		log, err := v.Chain.CommitmentLog(ctx, wallet, s.Lifecycle.Period)
		if err != nil {
			return Source{}, err
		}
		if err = v.canonical(ctx, head, log.Log); err != nil {
			return Source{}, err
		}
		s.Log = &log
	}
	if s.Custody, err = v.Chain.Custody(ctx, head.Number, wallet); err != nil {
		return Source{}, err
	}
	if s.Custody.MainOutpoint != ([32]byte{}) {
		out, err := v.Chain.CustodyOutput(ctx, head.Number, s.Custody.MainOutpoint)
		if err != nil {
			return Source{}, err
		}
		s.MainValue = out.Value
	}
	if s.Parameters, err = v.Chain.MovementParameters(ctx, head.Number); err != nil {
		return Source{}, err
	}
	if err = v.step("movement-seen"); err != nil {
		return Source{}, err
	}
	for _, label := range s.Commitment.Targets {
		t, err := v.Chain.Wallet(ctx, head.Number, label)
		if err != nil {
			return Source{}, err
		}
		s.Targets = append(s.Targets, t)
	}
	if err = v.recheck(ctx, head); err != nil {
		return Source{}, err
	}
	return s, v.step("targets-seen")
}

// Target reads a receiving wallet and its recorded incoming outputs whose log
// is canonical. keys limits the read to those outpoints; nil reads every log
// since Policy.From.
func (v *View) Target(ctx context.Context, wallet [32]byte, keys [][32]byte) (Target, error) {
	if v.Chain == nil || v.Policy.FinalityBlocks == 0 {
		return Target{}, errors.New("incomplete movement view")
	}
	if err := v.step("before-movement-read"); err != nil {
		return Target{}, err
	}
	head, err := v.Chain.Head(ctx)
	if err != nil {
		return Target{}, err
	}
	t := Target{Head: head}
	if t.Wallet, err = v.Chain.Wallet(ctx, head.Number, Label(wallet)); err != nil {
		return Target{}, err
	}
	if t.Custody, err = v.Chain.Custody(ctx, head.Number, wallet); err != nil {
		return Target{}, err
	}
	if t.Parameters, err = v.Chain.MovementParameters(ctx, head.Number); err != nil {
		return Target{}, err
	}
	logs, err := v.Chain.IncomingLogs(ctx, wallet, v.Policy.From, head.Number)
	if err != nil {
		return Target{}, err
	}
	want := map[[32]byte]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for _, log := range logs {
		if keys != nil && !want[log.Outpoint] {
			continue
		}
		delete(want, log.Outpoint)
		if err = v.canonical(ctx, head, log.Log); err != nil {
			return Target{}, err
		}
		record, err := v.Chain.Incoming(ctx, head.Number, log.Outpoint)
		if err != nil {
			return Target{}, err
		}
		t.Incoming = append(t.Incoming, IncomingOutput{Log: log, Record: record})
	}
	if keys != nil && len(want) != 0 {
		return Target{}, fmt.Errorf("%w: %d incoming outputs have no log", ErrNotFound, len(want))
	}
	if err = v.step("incoming-seen"); err != nil {
		return Target{}, err
	}
	return t, v.recheck(ctx, head)
}

// OutpointKey is the C-06 incoming and C-05 custody key.
var OutpointKey = deposit.OutpointKey
