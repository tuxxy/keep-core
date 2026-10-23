package spv

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

// reservationChain is the C-05 deposit record and C-10 reservation of one
// deposit. Other deposit.Chain methods are not used by ReserveDeposit.
type reservationChain struct {
	deposit.Chain
	record   deposit.Record
	reserved bool
	submits  int
	// submit runs on each submission and returns its error.
	submit func(c *reservationChain) error
}

func (c *reservationChain) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: 100}, nil
}

func (c *reservationChain) Deposit(context.Context, uint64, [32]byte) (deposit.Record, error) {
	return c.record, nil
}

func (c *reservationChain) Reserved(context.Context, uint64, [32]byte) (bool, error) {
	return c.reserved, nil
}

func (c *reservationChain) ReserveDeposit(context.Context, [32]byte, redemption.BitcoinInfo, redemption.BitcoinProof) error {
	c.submits++
	return c.submit(c)
}

type fundingState bool

func (f fundingState) Unspent(context.Context, bitcoin.TransactionOutpoint) (bool, error) {
	return bool(f), nil
}

type fixedClock uint64

func (c fixedClock) MedianTimePast(context.Context) (uint64, error) { return uint64(c), nil }

func TestReserveDeposit(t *testing.T) {
	funding := &bitcoin.Transaction{Version: 2, Outputs: []*bitcoin.TransactionOutput{{Value: 1000, PublicKeyScript: []byte{0x51}}}}
	hash := funding.Hash()
	const mtp, margin = 1_000_000, 3_600
	mined := func(c *reservationChain) error { c.reserved = true; return nil }
	lostReply := func(c *reservationChain) error { c.reserved = true; return errors.New("reply lost") }
	sweptFirst := func(c *reservationChain) error { c.record.SweptAt = 7; return errors.New("FROST deposit not pending") }
	refused := func(c *reservationChain) error { return errors.New("FROST custody budget") }
	tests := map[string]struct {
		unspent  bool
		locktime uint32
		submit   func(c *reservationChain) error
		kind     string
		err      error
		submits  int
		hooks    []string
	}{
		"reserves an eligible deposit":        {true, mtp + margin + 1, mined, "deposit-reservation", nil, 1, []string{"before-deposit-reservation", "deposit-reservation-submitted"}},
		"refuses a spent funding output":      {false, mtp + margin + 1, mined, "funding-spent", deposit.ErrFundingSpent, 0, nil},
		"refuses a deposit inside its margin": {true, mtp + margin, mined, "refund-margin", deposit.ErrRefundMargin, 0, nil},
		"settles a lost reply":                {true, mtp + margin + 1, lostReply, "already-reserved", nil, 1, []string{"before-deposit-reservation"}},
		"settles a race lost to a sweep":      {true, mtp + margin + 1, sweptFirst, "already-swept", nil, 1, []string{"before-deposit-reservation"}},
		"reports a refusal":                   {true, mtp + margin + 1, refused, "deposit-reservation", errors.New("deposit reservation"), 1, []string{"before-deposit-reservation"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			chain := &reservationChain{record: deposit.Record{FundingTransactionHash: hash, RevealedAt: 1, Terms: deposit.Terms{RefundLocktime: test.locktime}}, submit: test.submit}
			btc := newLocalBitcoinChain()
			if err := btc.addTransactionConfirmations(hash, 6); err != nil {
				t.Fatal(err)
			}
			var hooks []string
			m := FrostSweepMaintainer{
				Chain: chain, Bitcoin: btc, Confirmations: 6,
				Funding: fundingState(test.unspent), Clock: fixedClock(mtp), RefundMargin: margin,
				boundary: func(step string) error { hooks = append(hooks, step); return nil },
				assemble: func(h bitcoin.Hash, _ uint, _ bitcoin.Chain) (*bitcoin.Transaction, *bitcoin.SpvProof, error) {
					return funding, &bitcoin.SpvProof{}, nil
				},
			}
			action, err := m.ReserveDeposit(context.Background(), [32]byte{1})
			switch {
			case test.err == nil && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case test.err != nil && err == nil:
				t.Fatalf("expected error %v", test.err)
			case test.err != nil && errors.Is(test.err, deposit.ErrFundingSpent) && !errors.Is(err, test.err),
				test.err != nil && errors.Is(test.err, deposit.ErrRefundMargin) && !errors.Is(err, test.err):
				t.Fatalf("error %v, expected %v", err, test.err)
			}
			if action.Kind != test.kind || chain.submits != test.submits {
				t.Fatalf("kind %q submits %d, expected %q %d", action.Kind, chain.submits, test.kind, test.submits)
			}
			if len(hooks) != len(test.hooks) {
				t.Fatalf("hooks %v, expected %v", hooks, test.hooks)
			}
			for i := range hooks {
				if hooks[i] != test.hooks[i] {
					t.Fatalf("hooks %v, expected %v", hooks, test.hooks)
				}
			}
		})
	}
}
