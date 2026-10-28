package snowfallengine

import (
	"context"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
	snowfall "github.com/threshold-network/snowfall/clients/go"
)

type profileObserver struct{ calls int }

func (p *profileObserver) WaitCandidateAcceptance(_ context.Context, c frost.Candidate) (frost.Receipt, error) {
	p.calls++
	return frost.Receipt{Epoch: c.Epoch, Descriptor: c.Descriptor}, nil
}
func TestMutationAcceptanceRequiresApprovedProfile(t *testing.T) {
	for _, profile := range []string{frost.ApprovedProfile, "other-valid-profile", ""} {
		t.Run(profile, func(t *testing.T) {
			observer := &profileObserver{}
			bridge := acceptanceBridge{provider: observer}
			candidate := snowfall.Candidate{Epoch: 77, Descriptor: [32]byte{1}, Profile: profile, Roster: []uint16{1, 2, 3}, Threshold: 2}
			receipt, err := bridge.WaitCandidateAcceptance(context.Background(), candidate)
			_, called := bridge.result()
			if profile == frost.ApprovedProfile {
				if err != nil || observer.calls != 1 || !called || receipt.Epoch != 77 || receipt.Descriptor != candidate.Descriptor {
					t.Fatal("approved candidate was not delivered exactly once")
				}
			} else if err == nil || observer.calls != 0 || called {
				t.Fatal("unapproved profile reached host acceptance")
			}
		})
	}
}
