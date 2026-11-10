package enforcement

// Mutation batch M3 (MUTATION-REPORT-2026-11-10 sections 3-4, keep-core
// pkg/frost/enforcement heartbeat.go): oracles for the outcome record check,
// the round schedule and the runner's record faults. Faults come from the
// labelled fixtures in faults_test.go.

import (
	"context"
	"errors"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// Invariant: an outcome record is accepted only if it decodes strictly, has
// nothing after it, is version 1 and names the nonce it is keyed by; each
// condition alone refuses it.
// Kills KC-d3a49170cc (:63 err->nil), KC-ef5bf00274 and KC-8d53c3751f (:66
// ||->&&), KC-c3a0f57990 (:67 err->nil).
func TestReadOutcomeRejectsInvalidRecords(t *testing.T) {
	ctx := context.Background()
	wallet := [32]byte{0x42}
	for _, tc := range []struct {
		name string
		raw  string
		ok   bool
	}{
		{"valid", `{"Version":1,"Nonce":40,"Signed":true,"Present":[1,2]}`, true},
		{"not JSON", `not an outcome`, false},
		{"unknown field", `{"Version":1,"Nonce":40,"Extra":1}`, false},
		{"trailing data", `{"Version":1,"Nonce":40} {}`, false},
		{"version 2", `{"Version":2,"Nonce":40}`, false},
		{"version 0", `{"Version":0,"Nonce":40}`, false},
		{"another nonce", `{"Version":1,"Nonce":30}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &memRecords{m: map[[32]byte][]byte{outcomeID(wallet, 40): []byte(tc.raw)}} // fixture record
			o, err := ReadOutcome(ctx, r, wallet, 40)
			if tc.ok {
				if err != nil || o.Nonce != 40 || !o.Signed || len(o.Present) != 2 {
					t.Fatalf("a valid outcome was refused: %+v %v", o, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("an invalid outcome record was accepted: %+v", o)
			}
		})
	}
}

// Invariant: the current round is the latest whose anchor is final at head
// (anchor + FinalityBlocks <= head); a chain shorter than finality, round 0
// and an incomplete schedule have no round; chain read failures are returned;
// AuthorizeHeartbeat admits nothing when there is no current round.
// Kills KC-e524788f13 (:122 - -> +), KC-67022fa543 (:119 ||->&&),
// KC-0b1e8a366d (:120), KC-ae5fba956c (:98), KC-15d33c33be (:93 ||->&&),
// KC-828269b25b (:94), KC-1ba7448c60 (:117), KC-a36c2e0ce4 (:102) and
// KC-37fd8015aa (:132) err->nil.
func TestHeartbeatRoundSchedule(t *testing.T) {
	ctx := context.Background()
	t.Run("finality boundary", func(t *testing.T) {
		r, f, _ := runnerFixture(t) // finality 2, rounds of 10
		for _, tc := range []struct{ head, anchor uint64 }{{12, 10}, {41, 30}, {42, 40}, {49, 40}, {51, 40}, {52, 50}} {
			f.head = tc.head
			round, err := r.Current(ctx)
			if err != nil || round.Anchor != tc.anchor || round.Number != tc.anchor/10 || round.StartBlock != tc.anchor+2 {
				t.Fatalf("head %d: round %+v (err %v), want anchor %d", tc.head, round, err, tc.anchor)
			}
		}
	})
	t.Run("no round", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			head     uint64
			schedule redemption.Schedule
		}{
			{"empty chain", 0, redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}},
			{"chain shorter than finality", 1, redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}},
			{"chain equal to finality", 2, redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}},
			{"round zero", 11, redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}},
			{"no finality", 45, redemption.Schedule{FinalityBlocks: 0, RoundBlocks: 10}},
			{"no round length", 45, redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 0}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r, f, _ := runnerFixture(t)
				f.head, r.Schedule = tc.head, tc.schedule
				if round, err := r.Current(ctx); err == nil {
					t.Fatalf("a round was derived: %+v", round)
				}
				if err := r.AuthorizeHeartbeat(ctx, dkg.WalletID(r.Wallet.OutputKey), 0); err == nil {
					t.Fatal("a heartbeat was authorized without a current round")
				}
			})
		}
	})
	t.Run("chain read failures", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			fault func(*faultChain)
		}{
			{"head", func(f *faultChain) { f.headErr = errInjected }},
			{"anchor header", func(f *faultChain) { f.headerErr = errInjected }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r, base, _ := runnerFixture(t)
				f := &faultChain{fakeChain: base}
				tc.fault(f)
				r.Chain = f
				if round, err := r.Current(ctx); !errors.Is(err, errInjected) {
					t.Fatalf("a chain read failure was hidden: %+v %v", round, err)
				}
				if err := r.AuthorizeHeartbeat(ctx, dkg.WalletID(r.Wallet.OutputKey), 40); !errors.Is(err, errInjected) {
					t.Fatalf("a heartbeat was authorized past a chain read failure: %v", err)
				}
			})
		}
	})
}

// Invariant: Run never signs past a refused boundary, an unreadable outcome
// or start record, or a start record it could not retain; each is returned,
// and an incomplete signer configuration records no outcome.
// Kills KC-b669356ef4 (:148), KC-909caf9eb1 (:155), KC-734b1cc6bb (:160),
// KC-27fe44e3bb (:163) and KC-12b52ed3de (:172) err->nil.
func TestHeartbeatRunFaults(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		fault func(r *Runner, rec *faultRecords, wallet [32]byte)
	}{
		{"boundary before the heartbeat", func(r *Runner, _ *faultRecords, _ [32]byte) { r.boundary = failAt("before-heartbeat") }},
		{"corrupt outcome record", func(_ *Runner, rec *faultRecords, w [32]byte) {
			rec.m[outcomeID(w, 40)] = []byte("not an outcome") // fixture record
		}},
		{"start record unreadable", func(_ *Runner, rec *faultRecords, w [32]byte) { rec.readErr[startID(w, 40)] = errInjected }},
		{"start record not retained", func(_ *Runner, rec *faultRecords, w [32]byte) { rec.retainErr[startID(w, 40)] = errInjected }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, mem := runnerFixture(t)
			rec := newFaultRecords(mem)
			r.Base.Journal = faultJournal{r: rec}
			wallet := dkg.WalletID(r.Wallet.OutputKey)
			tc.fault(r, rec, wallet)
			r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
				t.Fatal("the heartbeat was signed past a failure")
				return signing.HeartbeatResult{}, nil
			}
			round, err := r.Current(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if o, err := r.Run(ctx, round); err == nil {
				t.Fatalf("a run failure was hidden: %+v", o)
			}
		})
	}
	t.Run("incomplete signer configuration", func(t *testing.T) {
		r, _, rec := runnerFixture(t) // r.sign is nil: the real HeartbeatSigner is built
		round, err := r.Current(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if o, err := r.Run(ctx, round); err == nil || o.Signed {
			t.Fatalf("an unbuildable signer was reported as %+v (err %v)", o, err)
		}
		if _, err := ReadOutcome(ctx, rec, dkg.WalletID(r.Wallet.OutputKey), round.Anchor); !errors.Is(err, store.ErrMissing) {
			t.Fatalf("an unbuildable signer recorded an outcome: %v", err)
		}
	})
}
