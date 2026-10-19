package deposit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// ErrMainBusy refuses an act on a main output that another purpose holds.
var ErrMainBusy = errors.New("FROST main output is held by another purpose")

// Purpose is who may release signatures that spend one main output (VD-07).
type Purpose string

const (
	Free       Purpose = "free"
	Redemption Purpose = "redemption"
	Resolution Purpose = "resolution"
	SweepMain  Purpose = "sweep"
	// MovementMain holds a main for a K-07 movement plan.
	MovementMain Purpose = "movement"
	// Closed is terminal: the main was spent by a finalized outcome.
	Closed Purpose = "closed"
)

// Entry is one append-only main lock record. A transition sets Purpose; a
// grant (Grant true) records one released signing grant under the current
// purpose and never changes it. Subject is the reservation (redemption and
// resolution) or the plan commitment (sweep); Transaction is the raw Bitcoin
// hash a resolution names. ValidUntil is a redemption certificate deadline.
type Entry struct {
	Purpose     Purpose
	Grant       bool `json:",omitempty"`
	Subject     [32]byte
	Transaction [32]byte `json:",omitempty"`
	ValidUntil  uint64   `json:",omitempty"`
	Input       int      `json:",omitempty"`
	Reason      string   `json:",omitempty"`
}

// MainLock is the host-local VD-07 main purpose lock: a durable compare-and-
// insert history per (wallet, main outpoint). Records are numbered; a writer
// inserts at the next number and the store refuses a different record at a
// taken number, so concurrent transitions and grants are totally ordered.
// History is never rewritten. Exclusion between quorums stays with the C-04
// sweep-resolution proof route.
type MainLock struct {
	Journal  redemption.RecordJournal
	boundary func(string) error
}

func (l *MainLock) step(name string) error {
	if l.boundary != nil {
		return l.boundary(name)
	}
	return nil
}

func mainLockID(wallet, main [32]byte, index uint32) [32]byte {
	raw := append([]byte("keep-core/frost/main-purpose-lock/v1/"), wallet[:]...)
	raw = append(raw, main[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint32(raw, index))
}

// History returns every record for the main in order.
func (l *MainLock) History(ctx context.Context, wallet, main [32]byte) ([]Entry, error) {
	var out []Entry
	for i := uint32(0); ; i++ {
		raw, err := l.Journal.ReadSigningRecord(ctx, mainLockID(wallet, main, i))
		if errors.Is(err, store.ErrMissing) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		var e Entry
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
}

// state is the last transition (grants excluded), or Free.
func state(history []Entry) Entry {
	for i := len(history) - 1; i >= 0; i-- {
		if !history[i].Grant {
			return history[i]
		}
	}
	return Entry{Purpose: Free}
}

// since returns the records after the last transition into a different purpose.
func since(history []Entry) []Entry {
	current := state(history)
	start := len(history)
	for start > 0 && (history[start-1].Grant || history[start-1].Purpose == current.Purpose) {
		start--
	}
	return history[start:]
}

func (l *MainLock) insert(ctx context.Context, wallet, main [32]byte, index int, e Entry) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err = l.Journal.RetainSigningRecord(ctx, mainLockID(wallet, main, uint32(index)), raw); err != nil {
		return fmt.Errorf("main lock insert: %w", err)
	}
	return nil
}

// Current returns the current purpose record of the main.
func (l *MainLock) Current(ctx context.Context, wallet, main [32]byte) (Entry, error) {
	history, err := l.History(ctx, wallet, main)
	if err != nil {
		return Entry{}, err
	}
	return state(history), nil
}

// AcquireRedemption moves a free main to redemption(r) at attestation. It is
// idempotent for the same reservation; a renewal with a later certificate
// deadline records that deadline, so the main is not released before it is
// final. It refuses any other holder.
func (l *MainLock) AcquireRedemption(ctx context.Context, wallet [32]byte, main bitcoin.TransactionOutpoint, reservation [32]byte, validUntil uint64) error {
	key := OutpointKey(main)
	if err := l.step("before-main-lock"); err != nil {
		return err
	}
	history, err := l.History(ctx, wallet, key)
	if err != nil {
		return err
	}
	current := state(history)
	switch {
	case current.Purpose == Redemption && current.Subject == reservation:
		if validUntil > current.ValidUntil {
			if err = l.insert(ctx, wallet, key, len(history), Entry{Purpose: Redemption, Subject: reservation, ValidUntil: validUntil, Reason: "renewal"}); err != nil {
				return err
			}
		}
		return l.step("main-lock-acquired")
	case current.Purpose != Free:
		return fmt.Errorf("%w: %s", ErrMainBusy, current.Purpose)
	}
	if err = l.insert(ctx, wallet, key, len(history), Entry{Purpose: Redemption, Subject: reservation, ValidUntil: validUntil}); err != nil {
		return err
	}
	return l.step("main-lock-acquired")
}

// AcquireSweep records a main-spending sweep plan. A free main moves to sweep;
// a main already held by sweep accepts further alternatives, which all spend
// the same main, so at most one can confirm. K-07 incoming sweeps use it too.
// A main a committed movement holds is refused.
func (l *MainLock) AcquireSweep(ctx context.Context, wallet [32]byte, main bitcoin.TransactionOutpoint, plan [32]byte) error {
	return l.acquirePlan(ctx, wallet, OutpointKey(main), plan, SweepMain, "")
}

// CommittedMovement is evidence that the movement authority accepted one
// movement plan: it read a finalized, unproven C-06 commitment naming exactly
// the plan's main and targets. Only AuthorizeMovement makes one.
type CommittedMovement struct {
	wallet, main, plan [32]byte
}

// AuthorizeMovement asks authority to authorize a movement plan and returns
// the evidence AcquireMovement needs.
func AuthorizeMovement(ctx context.Context, authority signing.SweepAuthority, plan *signing.Plan) (CommittedMovement, error) {
	if authority == nil || !plan.IsMovement() || plan.MovementMain() == nil {
		return CommittedMovement{}, errors.New("not a movement plan")
	}
	if _, err := authority.AuthorizeSweep(ctx, plan); err != nil {
		return CommittedMovement{}, err
	}
	return CommittedMovement{wallet: plan.WalletID(), main: OutpointKey(plan.MovementMain().Outpoint), plan: plan.Commitment()}, nil
}

// AcquireMovement records a K-07 movement plan on the source main. A free main
// moves to movement; a main already held by movement accepts further
// alternatives of the commitment, which spend the whole main and exclude each
// other on Bitcoin. A main still held by a K-06 sweep that aborted (its
// deposit later refunded, say) moves to movement too, recorded as "committed
// movement supersedes sweep": C-06 committed that unspent main, and the
// evidence shows the commitment is finalized, unproven and names exactly this
// main. A superseded sweep signature and the movement spend the same main, so
// Bitcoin admits at most one; a proven sweep retires the commitment on chain
// and opens a new period.
func (l *MainLock) AcquireMovement(ctx context.Context, evidence CommittedMovement) error {
	if evidence.plan == ([32]byte{}) {
		return errors.New("movement needs the movement authority's evidence")
	}
	return l.acquirePlan(ctx, evidence.wallet, evidence.main, evidence.plan, MovementMain, SweepMain)
}

// acquirePlan records plan under purpose on a free main, a main already held
// by purpose, or a main held by supersedes (none when empty).
func (l *MainLock) acquirePlan(ctx context.Context, wallet, key, plan [32]byte, purpose, supersedes Purpose) error {
	if err := l.step("before-main-lock"); err != nil {
		return err
	}
	history, err := l.History(ctx, wallet, key)
	if err != nil {
		return err
	}
	current := state(history)
	entry := Entry{Purpose: purpose, Subject: plan}
	switch {
	case current.Purpose == Free || current.Purpose == purpose:
	case supersedes != "" && current.Purpose == supersedes:
		entry.Reason = "committed movement supersedes " + string(supersedes)
	default:
		return fmt.Errorf("%w: %s", ErrMainBusy, current.Purpose)
	}
	for _, e := range since(history) {
		if !e.Grant && e.Purpose == purpose && e.Subject == plan {
			return l.step("main-lock-acquired")
		}
	}
	if err = l.insert(ctx, wallet, key, len(history), entry); err != nil {
		return err
	}
	return l.step("main-lock-acquired")
}

// Resolve moves redemption(r) to resolution(r, tx) before the host attests the
// C-04 sweep-resolution certificate. Afterwards no payment or cancellation
// grant for r is issued. A later resolution of the same r with another
// transaction is recorded as an alternative: every resolution sweep spends
// the reserved main, so at most one confirms. This lets a resolution whose
// deposit was refunded or passed its margin be replaced.
func (l *MainLock) Resolve(ctx context.Context, wallet [32]byte, main bitcoin.TransactionOutpoint, reservation, transaction [32]byte) error {
	return l.transfer(ctx, wallet, OutpointKey(main), func(current Entry) (Entry, bool, error) {
		if current.Purpose == Resolution && current.Subject == reservation {
			return Entry{Purpose: Resolution, Subject: reservation, Transaction: transaction}, current.Transaction != transaction, nil
		}
		if current.Purpose != Redemption || current.Subject != reservation {
			return Entry{}, false, fmt.Errorf("%w: %s", ErrMainBusy, current.Purpose)
		}
		return Entry{Purpose: Resolution, Subject: reservation, Transaction: transaction}, true, nil
	})
}

// ReleaseUnadmitted frees a main attested for r that C-04 never admitted. The
// caller passes the finalized head; the deadline must be finalized and the
// reservation absent at that head (VD-07 reading 1).
func (l *MainLock) ReleaseUnadmitted(ctx context.Context, chain redemption.Reader, finalized uint64, wallet [32]byte, main bitcoin.TransactionOutpoint, reservation [32]byte) error {
	return l.transfer(ctx, wallet, OutpointKey(main), func(current Entry) (Entry, bool, error) {
		if current.Purpose != Redemption || current.Subject != reservation {
			return Entry{}, false, fmt.Errorf("%w: %s", ErrMainBusy, current.Purpose)
		}
		if current.ValidUntil == 0 || finalized < current.ValidUntil {
			return Entry{}, false, errors.New("certificate deadline not finalized")
		}
		if _, err := chain.Intent(ctx, finalized, reservation); !errors.Is(err, redemption.ErrNotFound) {
			return Entry{}, false, fmt.Errorf("reservation admitted or unreadable: %v", err)
		}
		return Entry{Purpose: Free, Subject: reservation, Reason: "finalized non-admission"}, true, nil
	})
}

// Close ends a main that a finalized outcome spent. The caller passes the
// finalized head; the custody output must be recorded spent at it.
func (l *MainLock) Close(ctx context.Context, chain redemption.Reader, finalized uint64, wallet [32]byte, main bitcoin.TransactionOutpoint, reason string) error {
	output, err := chain.CustodyOutput(ctx, finalized, OutpointKey(main))
	if err != nil {
		return err
	}
	if !output.Spent {
		return errors.New("main output is not spent at the finalized head")
	}
	return l.transfer(ctx, wallet, OutpointKey(main), func(current Entry) (Entry, bool, error) {
		if current.Purpose == Closed {
			return Entry{}, false, nil
		}
		return Entry{Purpose: Closed, Reason: reason}, true, nil
	})
}

func (l *MainLock) transfer(ctx context.Context, wallet, key [32]byte, next func(Entry) (Entry, bool, error)) error {
	if err := l.step("before-main-lock-transfer"); err != nil {
		return err
	}
	history, err := l.History(ctx, wallet, key)
	if err != nil {
		return err
	}
	e, write, err := next(state(history))
	if err != nil {
		return err
	}
	if write {
		if err = l.insert(ctx, wallet, key, len(history), e); err != nil {
			return err
		}
	}
	return l.step("main-lock-transferred")
}

// permitted checks one grant against the current purpose of the main it
// spends. Payment and cancellation grants need redemption(r); an ordinary
// main-spending sweep needs sweep with this plan recorded; a resolution sweep
// needs resolution(r, its transaction). Deposit-only sweeps spend no main.
func permitted(history []Entry, plan *signing.Plan, reservation [32]byte) error {
	current := state(history)
	switch {
	case plan.IsMovement() || plan.IsIncomingSweep():
		want := MovementMain
		if plan.IsIncomingSweep() {
			want = SweepMain
		}
		if current.Purpose != want {
			return fmt.Errorf("%w: %s plan needs %s, main is %s", ErrMainBusy, plan.Purpose(), want, current.Purpose)
		}
		for _, e := range since(history) {
			if !e.Grant && e.Subject == plan.Commitment() {
				return nil
			}
		}
		return fmt.Errorf("%w: %s plan not recorded under the lock", ErrMainBusy, plan.Purpose())
	case !plan.IsSweep():
		if current.Purpose != Redemption || current.Subject != reservation {
			return fmt.Errorf("%w: payment or cancellation needs redemption(%x), main is %s", ErrMainBusy, reservation, current.Purpose)
		}
	case plan.SweepResolution() != ([32]byte{}):
		if current.Purpose != Resolution || current.Subject != plan.SweepResolution() {
			return fmt.Errorf("%w: resolution sweep needs its resolution, main is %s", ErrMainBusy, current.Purpose)
		}
		for _, e := range since(history) {
			if !e.Grant && e.Transaction == plan.BitcoinTransactionHash() {
				return nil
			}
		}
		return fmt.Errorf("%w: resolution sweep not recorded under the lock", ErrMainBusy)
	default:
		if current.Purpose != SweepMain {
			return fmt.Errorf("%w: sweep needs the main, main is %s", ErrMainBusy, current.Purpose)
		}
		for _, e := range since(history) {
			if !e.Grant && e.Subject == plan.Commitment() {
				return nil
			}
		}
		return fmt.Errorf("%w: sweep plan not recorded under the lock", ErrMainBusy)
	}
	return nil
}

// spentMain is the main output a grant would spend, if any.
func spentMain(plan *signing.Plan) (bitcoin.TransactionOutpoint, bool) {
	if plan.IsMovement() || plan.IsIncomingSweep() {
		main := plan.MovementMain()
		if main == nil {
			return bitcoin.TransactionOutpoint{}, false
		}
		return main.Outpoint, true
	}
	if plan.IsSweep() {
		main := plan.SweepMain()
		if main == nil {
			return bitcoin.TransactionOutpoint{}, false
		}
		return main.Outpoint, true
	}
	return plan.ConflictInput(), true
}

// Guard implements signing.GrantGuard over the main lock.
type Guard struct {
	Lock *MainLock
}

var _ signing.GrantGuard = Guard{}

// Permit is the read-only check at planning and claim time.
func (g Guard) Permit(ctx context.Context, plan *signing.Plan, reservation [32]byte) error {
	main, ok := spentMain(plan)
	if !ok {
		return nil
	}
	history, err := g.Lock.History(ctx, plan.WalletID(), OutpointKey(main))
	if err != nil {
		return err
	}
	return permitted(history, plan, reservation)
}

// Grant is the durable check at the release point (after the claim, right
// before the worker gets its grant). It inserts a grant record at the next
// number, so a concurrent resolution is ordered strictly before or after it.
func (g Guard) Grant(ctx context.Context, plan *signing.Plan, reservation [32]byte, input int) error {
	main, ok := spentMain(plan)
	if !ok {
		return nil
	}
	key := OutpointKey(main)
	history, err := g.Lock.History(ctx, plan.WalletID(), key)
	if err != nil {
		return err
	}
	if err = permitted(history, plan, reservation); err != nil {
		return err
	}
	current := state(history)
	return g.Lock.insert(ctx, plan.WalletID(), key, len(history), Entry{Purpose: current.Purpose, Grant: true, Subject: plan.Commitment(), Transaction: plan.BitcoinTransactionHash(), Input: input})
}
