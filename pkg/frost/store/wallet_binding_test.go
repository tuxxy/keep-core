package store

// K-03 review test gaps G08 and G11 (review-2026-10-07-K03 REVIEW.md F6).
// Refs tuxxy/keep-core#74.

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// readyFixture stores a completed key for the test domain through the real
// claim and SaveKey path, and returns a wallet record whose descriptor matches
// that key and carries a complete approval receipt. The key's references are
// opaque test bytes; nothing here decodes a key record.
func readyFixture(t *testing.T, v *Scoped) dkg.WalletRecord {
	t.Helper()
	request, key := dkgFixture(t, v.Domain())
	w := walletFixture(t)
	key.Candidate.Descriptor = w.Descriptor.SnowfallDescriptor
	key.Candidate.OutputKey = w.Descriptor.OutputKey
	claimDKG(t, v, [32]byte{31}, request)
	must(t, v.SaveKey(context.Background(), key))
	w.ApprovalBlock = 20
	w.ApprovalHash = common.Hash{3}
	w.ResultHash = [32]byte{5}
	w.Deadline = 40
	return w
}

// otherPoint is a valid x-only key (2G) that differs from the fixture's Q (G).
func otherPoint(t *testing.T) [32]byte {
	t.Helper()
	raw, err := hex.DecodeString("c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
	must(t, err)
	var q [32]byte
	copy(q[:], raw)
	return q
}

func rebind(t *testing.T, w dkg.WalletRecord) dkg.WalletRecord {
	t.Helper()
	hash, err := dkg.DescriptorHash(w.Descriptor)
	must(t, err)
	w.DescriptorHash = hash
	w.ID = dkg.WalletID(w.Descriptor.OutputKey)
	return w
}

// G08. Invariant (KC-SF-V3 4.2/4.3, SaveWallet contract): a wallet record is
// saved as ReadyUnfunded only if the durable KeyReady of its domain is the
// key of that wallet: same SNOWFALL descriptor and same output key Q. Each
// mismatch is otherwise a valid record, so only the key binding can refuse it.
func TestReadyWalletMustMatchDurableKey(t *testing.T) {
	for _, field := range []string{"snowfall-descriptor", "output-key"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			_, v := openTest(t, testConfig(t))
			w := readyFixture(t, v)
			changed := w
			changed.Descriptor.Members = append([]uint32(nil), w.Descriptor.Members...)
			changed.Descriptor.Operators = append([]common.Address(nil), w.Descriptor.Operators...)
			switch field {
			case "snowfall-descriptor":
				changed.Descriptor.SnowfallDescriptor[0] ^= 1
			case "output-key":
				changed.Descriptor.OutputKey = otherPoint(t)
			}
			changed = rebind(t, changed)
			// The same record is valid at the pending state: the refusal below
			// can only come from the ready-key binding.
			changed.State = "RegisteredPendingReady"
			must(t, v.SaveWallet(ctx, changed))
			changed.State = "ReadyUnfunded"
			if err := v.SaveWallet(ctx, changed); err == nil {
				t.Fatalf("G08 %s: ReadyUnfunded was saved against a durable key of another wallet", field)
			}
			loaded, err := v.LoadWallet(ctx)
			must(t, err)
			if loaded.State != "RegisteredPendingReady" {
				t.Fatalf("G08 %s: refused readiness changed the stored record to %q", field, loaded.State)
			}
		})
	}
	t.Run("matching-key", func(t *testing.T) {
		ctx := context.Background()
		_, v := openTest(t, testConfig(t))
		w := readyFixture(t, v)
		w.State = "ReadyUnfunded"
		if err := v.SaveWallet(ctx, w); err != nil {
			t.Fatalf("G08 positive control: the wallet of the durable key was refused: %v", err)
		}
	})
}

// G11. Invariant (KC-SF-V3 4.2, SaveWallet contract): every state after
// Candidate carries the full approval receipt: approval block, approval hash,
// result hash and a readiness deadline after the approval block. A record that
// lacks any one of them is refused in every later state; with all of them
// present (deadline one block after approval) it is accepted. For
// ReadyUnfunded a matching durable key exists, so only the receipt rule can
// refuse it.
func TestWalletApprovalReceiptRequired(t *testing.T) {
	missing := map[string]func(*dkg.WalletRecord){
		"approval-block":           func(w *dkg.WalletRecord) { w.ApprovalBlock = 0 },
		"approval-hash":            func(w *dkg.WalletRecord) { w.ApprovalHash = common.Hash{} },
		"result-hash":              func(w *dkg.WalletRecord) { w.ResultHash = [32]byte{} },
		"deadline-at-approval":     func(w *dkg.WalletRecord) { w.Deadline = w.ApprovalBlock },
		"deadline-before-approval": func(w *dkg.WalletRecord) { w.Deadline = w.ApprovalBlock - 1 },
		"no-deadline":              func(w *dkg.WalletRecord) { w.Deadline = 0 },
	}
	for _, state := range []string{"RegisteredPendingReady", "ReadyUnfunded", "Closed"} {
		for name, change := range missing {
			t.Run(state+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				_, v := openTest(t, testConfig(t))
				w := readyFixture(t, v)
				w.State = state
				change(&w)
				if err := v.SaveWallet(ctx, w); err == nil {
					t.Fatalf("G11 %s: a %s record without a complete approval receipt was saved", name, state)
				}
				if _, err := v.LoadWallet(ctx); !errors.Is(err, ErrMissing) {
					t.Fatalf("G11 %s: a refused %s record reached the store: %v", name, state, err)
				}
			})
		}
		t.Run(state+"/complete", func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			w := readyFixture(t, v)
			w.State = state
			w.Deadline = w.ApprovalBlock + 1
			if err := v.SaveWallet(context.Background(), w); err != nil {
				t.Fatalf("G11 positive control: a complete %s record was refused: %v", state, err)
			}
		})
	}
	t.Run("Candidate/no-receipt", func(t *testing.T) {
		_, v := openTest(t, testConfig(t))
		w := walletFixture(t)
		if err := v.SaveWallet(context.Background(), w); err != nil {
			t.Fatalf("G11 positive control: a Candidate record needs no receipt: %v", err)
		}
	})
}
