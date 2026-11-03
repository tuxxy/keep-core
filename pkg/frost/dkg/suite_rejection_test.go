package dkg

// K-03 review test gap G13 (review-2026-10-07-K03 REVIEW.md F6): the unknown
// suite rejection was detected only by a nil-pointer panic.
// Refs tuxxy/keep-core#74.

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost"
)

var errRecorded = errors.New("recorded external operation")

// FIXTURE: dependencies that record any use and then refuse it. Only the
// methods the acceptance callback can reach first are implemented; the rest
// are nil embeddings that are never called.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}
func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

type recordingJournal struct {
	Journal
	r *recorder
}

func (j recordingJournal) SaveWallet(context.Context, WalletRecord) error {
	j.r.record("journal.SaveWallet")
	return errRecorded
}

type recordingChain struct {
	Chain
	r *recorder
}

func (c recordingChain) ResultDigest(context.Context, uint64, Result) ([32]byte, error) {
	c.r.record("chain.ResultDigest")
	return [32]byte{}, errRecorded
}
func (c recordingChain) Submit(context.Context, Result) error {
	c.r.record("chain.Submit")
	return errRecorded
}
func (c recordingChain) View(context.Context, uint64) (View, error) {
	c.r.record("chain.View")
	return View{}, errRecorded
}

type recordingBus struct{ r *recorder }

func (b recordingBus) Send(context.Context, Attestation) error {
	b.r.record("bus.Send")
	return errRecorded
}
func (b recordingBus) Receive(ctx context.Context) (Attestation, error) {
	b.r.record("bus.Receive")
	return Attestation{}, errRecorded
}

// Invariant (KC-SF-V3 4.3: K-03 rejects an unknown suite string before
// publication; never truncate, hash or guess a byte value): a candidate whose
// profile is not exactly the approved suite string is refused by the
// acceptance callback before any journal, chain or bus operation, and
// NewDescriptor refuses it. The approved suite reaches the journal through
// the same recording dependencies (positive control).
func TestUnknownSuiteRefusedBeforeAnyExternalOperation(t *testing.T) {
	p := Parameters{ChainID: big.NewInt(31337), Registry: common.Address{1}, Pool: common.Address{2}, Bridge: common.Address{3}, GroupSize: 3, Threshold: 2, ReadySeats: 2, Profile: 1, ChallengeBlocks: 10}
	s := Selection{Epoch: 77, Members: []uint32{1, 1, 2}, Operators: []common.Address{{4}, {4}, {5}}}
	q, err := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	if err != nil {
		t.Fatal(err)
	}
	approved := frost.Candidate{Profile: frost.ApprovedProfile, Epoch: 77, Roster: []uint16{1, 2, 3}, Threshold: 2, Descriptor: [32]byte{6}}
	copy(approved.OutputKey[:], q)
	executor := func(r *recorder) *Executor {
		return &Executor{params: p, selection: s, Journal: recordingJournal{r: r}, Chain: recordingChain{r: r}, Bus: recordingBus{r}, Request: frost.DKGRequest{LocalSeats: []uint16{1, 2}}}
	}

	t.Run("approved-suite", func(t *testing.T) {
		r := &recorder{}
		if _, err := NewDescriptor(p, s, approved); err != nil {
			t.Fatalf("G13 positive control: the approved suite was refused: %v", err)
		}
		_, err := executor(r).WaitCandidateAcceptance(context.Background(), approved)
		if !errors.Is(err, errRecorded) || len(r.seen()) == 0 || r.seen()[0] != "journal.SaveWallet" {
			t.Fatalf("G13 positive control: the approved suite did not reach the journal: %v %v", r.seen(), err)
		}
	})
	for _, suite := range []string{"", frost.ApprovedProfile + " ", " " + frost.ApprovedProfile, frost.ApprovedProfile[:len(frost.ApprovedProfile)-1], "1", "FROST-secp256k1-SHA256-v1"} {
		t.Run("suite="+suite, func(t *testing.T) {
			if suite == frost.ApprovedProfile {
				t.Fatal("harness: suite vector equals the approved suite")
			}
			bad := approved
			bad.Profile = suite
			if _, err := NewDescriptor(p, s, bad); err == nil {
				t.Errorf("G13 %q: NewDescriptor accepted an unknown suite", suite)
			}
			r := &recorder{}
			var err error
			func() {
				defer func() {
					if v := recover(); v != nil {
						t.Fatalf("G13 %q: the acceptance callback panicked instead of refusing: %v", suite, v)
					}
				}()
				_, err = executor(r).WaitCandidateAcceptance(context.Background(), bad)
			}()
			if calls := r.seen(); len(calls) != 0 {
				t.Fatalf("G13 %q: an unknown suite reached %v before rejection", suite, calls)
			}
			if err == nil || errors.Is(err, errRecorded) {
				t.Fatalf("G13 %q: the acceptance callback did not refuse an unknown suite: %v", suite, err)
			}
		})
	}
}
