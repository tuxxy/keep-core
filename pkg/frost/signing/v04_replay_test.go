package signing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV04TraceProjection drives real executor seams from discovered model
// snapshots. Storage stages and crashes are not lockstep subprocess replay.
// Every report is partial or none; no abstract contract action counts as Go.
func TestV04TraceProjection(t *testing.T) {
	dir := "verification/evidence/V-04-2026-10-13/traces"
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(paths) != 23 {
		t.Fatalf("expected ten witnesses and thirteen controls, got %d: %v", len(paths), err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var trace struct {
				ID, Kind, Property, Weakening string
				Steps                         []struct {
					N           int
					Attempt     *uint8
					Diverges    bool
					ExpectState json.RawMessage `json:"expect_state"`
				}
			}
			if err = json.Unmarshal(raw, &trace); err != nil {
				t.Fatal(err)
			}
			if len(trace.Steps) == 0 {
				t.Fatal("empty discovered trace")
			}
			// The serialized exact trace is retained; contract-only paths have no seam.
			if strings.Contains(trace.ID, "payment-settles") || strings.Contains(trace.ID, "cancel-then-refund") || strings.Contains(trace.ID, "unreserved-refund") || trace.Weakening == "RefundOnElapsedTime" || trace.Weakening == "RefundOnCertificateExpiry" || trace.Weakening == "AllowConcurrentReservations" || strings.Contains(trace.ID, "next-reservation-after-confirmed-outcome") || strings.Contains(trace.ID, "sign-after-admission-certificate-expiry") {
				t.Logf("V04_REPLAY id=%s class=none reason=C04-ledger-absent", trace.ID)
				return
			}
			e, r, b, originalEngine := executorFixture(t)
			ctx := context.Background()
			switch trace.Weakening {
			case "None":
				if strings.Contains(trace.ID, "cancelled-return-retained") {
					TestV04CancelledReturnedSignatureRetained(t)
				} else if strings.Contains(trace.ID, "done-only-recovered") {
					TestV04DoneOnlyRecovery(t)
				} else if strings.Contains(trace.ID, "late-quarantine") {
					e.boundary = func(name string) error {
						if name == "after-signature" {
							r.view.State = "TimeoutPending"
						}
						return nil
					}
					if _, err = e.Run(ctx); !errors.Is(err, ErrLateSignature) {
						t.Fatal("late signature projection", err)
					}
				} else if strings.Contains(trace.ID, "fresh-after-failure") {
					e.boundary = func(name string) error {
						if name == "before-worker-start" {
							return errors.New("crash projection")
						}
						return nil
					}
					if _, err = e.Run(ctx); err == nil {
						t.Fatal("failure ignored")
					}
					if _, err = e.Run(ctx); err == nil {
						t.Fatal("executor restarted")
					}
					next := e.config
					next.Session[0]++
					next.StartBlock++
					fresh, err := NewLocalExecutor(next)
					if err != nil {
						t.Fatal(err)
					}
					fresh.engine = &engineStub{executor: fresh, private: originalEngine.private}
					oldAttempt, _ := e.attempt(0)
					newAttempt, _ := fresh.attempt(0)
					if oldAttempt.StartBlock == newAttempt.StartBlock || string(oldAttempt.Session) == string(newAttempt.Session) {
						t.Fatal("retry did not use fresh agreed identity")
					}
					if _, err = fresh.Run(ctx); err != nil {
						t.Fatal("fresh fixture attempt refused", err)
					}
				} else {
					result, err := e.Run(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if err = e.Broadcast(ctx, result); err != nil || len(b.sent) == 0 {
						t.Fatal("authorized success refused", err)
					}
				}
			case "ReleaseBeforeFinality":
				r.view.Head = r.view.Block
				if _, err = e.Run(ctx); !errors.Is(err, ErrUnauthorized) {
					t.Fatal("unfinalized release", err)
				}
			case "OmitExactPlan":
				r.view.Commitment[0] ^= 1
				if _, err = e.Run(ctx); !errors.Is(err, ErrUnauthorized) {
					t.Fatal("foreign plan release", err)
				}
			case "OmitFinalGuard":
				// Use the model's pre-grant snapshot to select the final-read fault.
				var snap struct {
					Requests []struct {
						Reservation     uint8
						Changed         bool
						Anchor, Receipt uint8
					}
					Attempts []struct {
						Released bool
						Request  uint8
					}
				}
				step := trace.Steps[len(trace.Steps)-1]
				if err = json.Unmarshal(step.ExpectState, &snap); err != nil {
					t.Fatal(err)
				}
				if step.Attempt == nil || int(*step.Attempt) >= len(snap.Attempts) {
					t.Fatal("trace grant attempt out of bound")
				}
				index := snap.Attempts[*step.Attempt].Request
				if int(index) >= len(snap.Requests) {
					t.Fatal("trace request out of bound")
				}
				q := snap.Requests[index]
				e.boundary = func(name string) error {
					if name == "before-worker-start" {
						if q.Changed {
							r.view.Commitment[0] ^= 1
						}
						// Preserve every non-Reserved model state, including a
						// confirmed cancellation/refund before the stale grant.
						switch q.Reservation {
						case 0:
							r.view.State = "Missing"
						case 1:
							r.view.State = "Pending"
						case 3:
							r.view.State = "TimeoutPending"
						case 4:
							r.view.State = "Paid"
						case 5:
							r.view.State = "Cancelled"
						case 6:
							r.view.State = "Refunded"
						}
						if q.Anchor != q.Receipt {
							r.view.BlockHash[0]++
							r.view.CanonicalBlockHash = r.view.BlockHash
						}
					}
					return nil
				}
				if _, err = e.Run(ctx); !errors.Is(err, ErrUnauthorized) {
					t.Fatal("stale final grant", err)
				}
			case "ForgetDurableReceipt":
				result, err := e.Run(ctx)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := NewLocalExecutor(e.config)
				if err != nil {
					t.Fatal(err)
				}
				r.view.BlockHash[0]++
				r.view.CanonicalBlockHash = r.view.BlockHash
				if err = fresh.Broadcast(ctx, result); !errors.Is(err, ErrUnauthorized) || len(b.sent) > 0 {
					t.Fatal("reanchored recovery", err)
				}
				delete(e.config.Journal.(*journalStub).records, e.receiptID())
				if _, err = fresh.LoadResult(ctx); !errors.Is(err, ErrUnauthorized) {
					t.Fatal("lost durable receipt", err)
				}
			case "AcceptLateSignature":
				result, err := e.Run(ctx)
				if err != nil {
					t.Fatal(err)
				}
				r.view.State = "TimeoutPending"
				if err = e.Broadcast(ctx, result); !errors.Is(err, ErrUnauthorized) || len(b.sent) > 0 {
					t.Fatal("late broadcast", err)
				}
			case "ForeignDoneContext":
				result, err := e.Run(ctx)
				if err != nil {
					t.Fatal(err)
				}
				result.Done[0].Context.Attempt[0] ^= 1
				if e.ValidateResult(result) == nil {
					t.Fatal("foreign done context")
				}
			case "CancelledRetention":
				TestV04CancelledReturnedSignatureRetained(t)
			case "AggregateOnlyRecovery":
				TestV04DoneOnlyRecovery(t)
			case "EmptyAuthorizationStartsWorker", "ReuseAttempt":
				// These exact discovered controls project to the real guarded engine/store
				// test, not this executor stub. Do not count this as a successful replay.
				t.Logf("V04_REPLAY id=%s class=none reason=separate-real-store-guard-test", trace.ID)
				return
			default:
				t.Fatal("unmapped weakening", trace.Weakening)
			}
			t.Logf("V04_REPLAY id=%s class=partial reason=executor-seam-projection-no-process-crash-or-storage-lockstep", trace.ID)
		})
	}
}
