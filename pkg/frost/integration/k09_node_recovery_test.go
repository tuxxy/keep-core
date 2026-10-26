package integration

import (
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

// Pause the actual DKG executor after chain certification, before its local
// wallet save. This is the permanently crash-looping state from K09-21.
func TestFrostNodeCertifiedEpochInterruption(t *testing.T) {
	for _, killed := range []bool{false, true} {
		name := "sigint"
		if killed {
			name = "sigkill"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("FROST_K09_PAUSE_BOUNDARY", "after-readiness-postcondition")
			var origin k09LegacyOrigin
			prepare := bootstrapPublicLegacy(t, &origin)
			f := setupWith(t, "1,1,2", setupOptions{remote: true, mode: "k09-public", timeout: 240 * time.Second, prepare: func(f *fixture, url, out string) { prepare(f, url, out) }})
			epoch := f.deployment.Epoch
			node := f.remotes[1]
			marker := node.config.VerificationPause.Marker
			paused := false
			for until := time.Now().Add(90 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
				if _, err := os.Stat(marker); err == nil {
					paused = true
					break
				}
				select {
				case <-node.done:
					t.Fatal("node exited before real boundary")
				default:
				}
			}
			if !paused {
				t.Fatal("actual DKG certification boundary not reached")
			}
			before, err := node.status(f.ctx)
			if err != nil || len(before.Wallets) != 0 {
				t.Fatal("interruption did not precede local ready save", err)
			}
			// A ready public wallet exists while this node lacks its ready save.
			certified := false
			for until := time.Now().Add(15 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
				s, err := f.remotes[0].status(f.ctx)
				if err != nil || len(s.Wallets) == 0 {
					continue
				}
				v, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, s.Wallets[0].ID)
				if err == nil && v.State == 2 {
					certified = true
					break
				}
			}
			if !certified {
				t.Fatal("wallet not publicly certified")
			}
			restartRemoteNode(f, 1, killed)
			lost := false
			for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
				s, err := node.status(f.ctx)
				if err != nil {
					continue
				}
				for _, v := range s.LostEpochs {
					if v.Epoch == epoch && len(v.Seats) == 1 {
						lost = true
					}
				}
				if lost {
					break
				}
			}
			if !lost {
				t.Fatal("interrupted epoch seats were not reported separately")
			}
			f.nextEpoch()
			records, failures := f.run()
			for _, err := range failures {
				if err != nil {
					t.Fatal("lost epoch blocked a fresh DKG", err)
				}
			}
			if len(records) != 2 || records[0].ID != records[1].ID || records[0].Descriptor.Epoch == epoch {
				t.Fatal("fresh shared epoch missing")
			}
			select {
			case <-node.done:
				t.Fatal("lost epoch still crash-loops after fresh DKG")
			default:
			}
			t.Log("K09-RECOVERY: certified-epoch interruption isolated; new epoch ready")
		})
	}
}
