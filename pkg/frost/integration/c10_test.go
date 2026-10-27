package integration

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// c10ABI is the governance and reporting surface of the C-10 controller.
var c10ABI, _ = abi.JSON(strings.NewReader(`[
{"type":"function","name":"custodyReserved","stateMutability":"view","inputs":[],"outputs":[{"type":"uint64"}]},
{"type":"function","name":"issuanceReserved","stateMutability":"view","inputs":[],"outputs":[{"type":"uint64"}]},
{"type":"function","name":"openBudgetPeriod","stateMutability":"nonpayable","inputs":[{"type":"uint64"},{"type":"uint64"},{"type":"bytes32"}],"outputs":[]},
{"type":"function","name":"contain","stateMutability":"nonpayable","inputs":[{"type":"bytes32"}],"outputs":[]},
{"type":"function","name":"lift","stateMutability":"nonpayable","inputs":[{"type":"bytes32"}],"outputs":[]},
{"type":"function","name":"issued","stateMutability":"view","inputs":[],"outputs":[{"type":"uint64"}]}]`))

// TestFrostSweepWithinExposureBudget: with a one-deposit custody budget (D-10
// test values, OPEN) the host reserves the first deposit before planning and
// cannot reserve the second, so the plan leaves it out and the sweep proof
// is never refused. A deposit reserved before containment is swept and
// credited during it; reservations stay refused until governance lifts and
// opens a new period. The controller's issuance equals the Bank credits.
func TestFrostSweepWithinExposureBudget(t *testing.T) {
	w := newK06World(t)
	ctx := w.f.ctx
	controller := bind.NewBoundContract(w.f.deployment.Exposure, c10ABI, w.f.client, w.f.client, w.f.client)
	transact := func(method string, args ...any) error {
		_, err := w.trySend(func(o *bind.TransactOpts) (*types.Transaction, error) {
			return controller.Transact(o, method, args...)
		})
		return err
	}
	record := func(s string) [32]byte { return [32]byte(crypto.Keccak256([]byte(s))) }
	issued := func() uint64 {
		var out []any
		if err := controller.Call(&bind.CallOpts{Context: ctx}, &out, "issued"); err != nil {
			t.Fatal(err)
		}
		return out[0].(uint64)
	}
	reserved := func(f funded) bool {
		ok, err := w.chain.Reserved(ctx, w.head(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if err := transact("openBudgetPeriod", uint64(1_500_000), uint64(1_500_000), record("C-10 local one-deposit budget; D-10 OPEN")); err != nil {
		t.Fatal(err)
	}
	locktime := w.locktime(k06RefundDelay)
	a := w.deposit(1_000_000, locktime, [20]byte{})
	w.unreservedReveals = true
	b := w.deposit(1_000_000, locktime, [20]byte{})
	if action, err := w.sweeps.ReserveDeposit(ctx, b.key); err == nil || reserved(b) {
		t.Fatalf("a deposit beyond the custody budget was reserved: %+v %v", action, err)
	}
	r := w.round(b.readyBlock + k06Finality)
	plan := w.plan(r)
	if len(plan.SweepDeposits()) != 1 || plan.SweepDeposits()[0].Key != a.key {
		t.Fatal("the plan must hold the reserved deposit only")
	}
	w.sweepAndProve(plan, r.Number, w.sweeps)
	credit, treasury := expectedCredit(plan)
	total := credit + treasury

	// Reserved before containment, swept during it.
	w.unreservedReveals = false
	c := w.deposit(400_000, locktime, [20]byte{})
	if err := transact("contain", record("C-10 local containment")); err != nil {
		t.Fatal(err)
	}
	r = w.round(c.readyBlock + k06Finality)
	plan = w.plan(r)
	if len(plan.SweepDeposits()) != 1 || plan.SweepDeposits()[0].Key != c.key || plan.SweepMain() == nil {
		t.Fatal("under containment the plan must hold the reserved deposit and the main only")
	}
	w.sweepAndProve(plan, r.Number, w.sweeps)
	credit, treasury = expectedCredit(plan)
	total += credit + treasury
	if _, err := w.sweeps.ReserveDeposit(ctx, b.key); err == nil || reserved(b) {
		t.Fatal("a reservation was admitted under containment")
	}
	if err := transact("openBudgetPeriod", uint64(3_000_000), uint64(3_000_000), record("C-10 local top-up; D-10 OPEN")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.sweeps.ReserveDeposit(ctx, b.key); err == nil {
		t.Fatal("a top-up alone lifted containment")
	}

	// Lift, reserve once (a retry reuses it) and sweep the last deposit.
	if err := transact("lift", record("C-10 local lift")); err != nil {
		t.Fatal(err)
	}
	if action, err := w.sweeps.ReserveDeposit(ctx, b.key); err != nil || action.Kind != "deposit-reservation" {
		t.Fatalf("reservation after the lift: %+v %v", action, err)
	}
	if action, err := w.sweeps.ReserveDeposit(ctx, b.key); err != nil || action.Kind != "already-reserved" {
		t.Fatalf("second reservation: %+v %v", action, err)
	}
	r = w.round(w.head() + k06Finality)
	plan = w.plan(r)
	if len(plan.SweepDeposits()) != 1 || plan.SweepDeposits()[0].Key != b.key {
		t.Fatal("the plan must hold the newly reserved deposit")
	}
	w.sweepAndProve(plan, r.Number, w.sweeps)
	credit, treasury = expectedCredit(plan)
	total += credit + treasury
	if action, err := w.sweeps.ReserveDeposit(ctx, b.key); err != nil || action.Kind != "already-swept" {
		t.Fatalf("reservation of a swept deposit: %+v %v", action, err)
	}
	if issued() != total {
		t.Fatalf("controller issued %d, Bank credits %d", issued(), total)
	}
	t.Logf("C-10: three sweeps within the budget, %d sat issued", total)
}
