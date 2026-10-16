package dkg

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// This is a projection of model reads onto the real executor seam, not a
// subprocess/lockstep replay of unsupported key, crash or chain transactions.
func TestV03ModelReadProjection(t *testing.T) {
	files, err := filepath.Glob("verification/evidence/traces/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("missing frozen model traces", err)
	}
	projected := 0
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var trace struct {
				Steps []struct {
					Action     json.RawMessage
					Diverges   bool
					ClaimState struct {
						Chain uint8
						Tick  uint64
					} `json:"claim_state"`
				}
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &trace); err != nil {
				t.Fatal(err)
			}
			for _, step := range trace.Steps {
				if step.Diverges {
					break
				} // H-06: never replay weakened expectations.
				var action map[string]uint8
				if json.Unmarshal(step.Action, &action) != nil {
					continue
				}
				if _, ok := action["Reread"]; !ok {
					continue
				}
				v := View{Head: 10 + step.ClaimState.Tick, ApprovedID: [32]byte{1}, ApprovalHash: common.Hash{2}, Approved: Wallet{State: 1, ApprovalBlock: 10, Deadline: 13, ResultHash: [32]byte{3}, DescriptorHash: [32]byte{4}}}
				record := WalletRecord{ID: v.ApprovedID, ApprovalHash: v.ApprovalHash, ApprovalBlock: 10, Deadline: 13, ResultHash: v.Approved.ResultHash, DescriptorHash: v.Approved.DescriptorHash}
				chain := &v03Chain{view: v}
				if step.ClaimState.Chain == 0 {
					chain.view.ApprovedID = [32]byte{}
				}
				if step.ClaimState.Chain == 2 {
					chain.view.ApprovalHash[0] ^= 1
				}
				e := Executor{Chain: chain, selection: Selection{Epoch: 1}, record: record}
				_, got := e.checkedApproval(context.Background())
				var want error
				if step.ClaimState.Chain != 1 {
					want = ErrQuarantined
				} else if step.ClaimState.Tick >= 3 {
					want = ErrExpired
				}
				if !errors.Is(got, want) {
					t.Fatalf("read projection: got %v want %v", got, want)
				}
				projected++
			}
		})
	}
	if projected == 0 {
		t.Fatal("vacuous projection")
	}
	t.Logf("trace files=%d projected approval reads=%d; full traces=0 (unsupported steps retained)", len(files), projected)
}

type v03Chain struct {
	Chain
	view    View
	expired int
}

func (c *v03Chain) View(context.Context, uint64) (View, error) { return c.view, nil }
func (c *v03Chain) Expire(context.Context, [32]byte) error {
	c.expired++
	c.view.Approved.State = 3
	return nil
}

func TestV03SubmissionObservationNonterminal(t *testing.T) {
	// A submission-only snapshot keeps watching; a changed chain epoch quarantines.
	for _, epoch := range []uint64{1, 2} {
		c := &v03Chain{view: View{State: 3, Epoch: epoch, Head: 10, ResultDeadline: 20}}
		e := Executor{Chain: c, selection: Selection{Epoch: 1}}
		var want error
		if epoch != 1 {
			want = ErrQuarantined
		}
		if err := e.inspectSubmission(context.Background()); !errors.Is(err, want) {
			t.Fatalf("epoch=%d got %v want %v", epoch, err, want)
		}
		if e.approvalSeen {
			t.Fatal("submission promoted to approval")
		}
	}
}

func TestV03ObservedApprovalRemovalAndDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		head    uint64
		removed bool
		want    error
	}{
		{"before deadline", 12, false, nil}, {"deadline equality", 13, false, ErrExpired}, {"after deadline", 14, false, ErrExpired}, {"removed", 12, true, ErrQuarantined},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &v03Chain{view: View{Epoch: 1, Head: tc.head, ApprovedID: [32]byte{1}, ApprovalHash: common.Hash{2}, Approved: Wallet{State: 1, Deadline: 13}}}
			if tc.removed {
				c.view.ApprovedID = [32]byte{}
			}
			e := Executor{Chain: c, selection: Selection{Epoch: 1}, approvalSeen: true, observedApprovalID: [32]byte{1}, observedApprovalHash: [32]byte{2}}
			err := e.inspectSubmission(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.head >= 13 && c.expired != 1 {
				t.Fatal("deadline did not expire exactly once")
			}
		})
	}
}
