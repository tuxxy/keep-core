package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	frostnode "github.com/keep-network/keep-core/pkg/frost/node"
)

// Pause the actual DKG executor after chain certification, before its local
// wallet save. This is the permanently crash-looping state from K09-21.
func TestFrostNodeCertifiedEpochInterruption(t *testing.T) {
	testFrostNodeEpochInterruption(t, "after-readiness-postcondition")
}

func TestFrostNodeSignedIncomingInterruption(t *testing.T) {
	for _, killed := range []bool{false, true} {
		name := "sigint"
		if killed {
			name = "sigkill"
		}
		t.Run(name, func(t *testing.T) {
			var interrupted bool
			var expected, walletID [32]byte
			var expectedWitness [32]byte
			w := runPublicLegacyFlow(t, true, func(w *k07World) {
				node := w.f.remotes[0] // Seat one owns broadcast and proof.
				node.config.VerificationPause = &frostnode.VerificationPause{Boundary: "after-incoming-sign", Marker: filepath.Join(t.TempDir(), "public-signed-boundary")}
				data, err := json.Marshal(node.config)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(node.command.Args[2], data, 0600); err != nil {
					t.Fatal(err)
				}
				restartRemoteNode(w.f, 0)
				w.observeIncoming = func() {
					if interrupted {
						return
					}
					if _, err := os.Stat(node.config.VerificationPause.Marker); err != nil {
						return
					}
					marker, err := os.ReadFile(node.config.VerificationPause.Marker)
					if err != nil {
						t.Fatal(err)
					}
					for _, field := range strings.Fields(string(marker)) {
						if strings.HasPrefix(field, "witness=") {
							raw, err := hex.DecodeString(strings.TrimPrefix(field, "witness="))
							if err != nil || len(raw) != 32 {
								t.Fatal("invalid public witness hash")
							}
							copy(expectedWitness[:], raw)
						}
					}
					if expectedWitness == ([32]byte{}) {
						return // The writer has not completed the public marker yet.
					}
					// Independently derive the public transaction before it can be
					// broadcast. No worker/store material enters this observation.
					for _, target := range []*walletHost{w.a, w.b} {
						planner := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: target.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
						plan, err := planner.IncomingSweep(w.f.ctx)
						if err == nil {
							expected, walletID = plan.BitcoinTransactionHash(), target.id
						}
					}
					if expected == ([32]byte{}) {
						t.Fatal("signed boundary has no public incoming plan")
					}
					restartRemoteNode(w.f, 0, killed)
					interrupted = true
				}
			})
			if !interrupted || w.custody(walletID).MainOutpoint != movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(expected)}) {
				t.Fatal("signed incoming restart did not settle the exact original transaction")
			}
			raw, err := w.core.Transaction(w.f.ctx, bitcoin.Hash(expected))
			if err != nil || expectedWitness == ([32]byte{}) || sha256.Sum256(raw) != expectedWitness {
				t.Fatal("restart changed the originally accepted transaction witness", err)
			}
			t.Log("K09-RECOVERY: signed incoming plan survived", name, "and settled unchanged")
		})
	}
}

func TestFrostNodeIncomingDuringStalledDKG(t *testing.T) {
	w := runPublicLegacyFlow(t, true, func(w *k07World) {
		node := w.f.remotes[1]
		node.config.VerificationPause = &frostnode.VerificationPause{Boundary: "candidate-ready", Marker: filepath.Join(t.TempDir(), "public-candidate-boundary")}
		data, err := json.Marshal(node.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(node.command.Args[2], data, 0600); err != nil {
			t.Fatal(err)
		}
		restartRemoteNode(w.f, 1)
		w.f.nextEpoch()
		paused := false
		for until := time.Now().Add(60 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
			if _, err := os.Stat(node.config.VerificationPause.Marker); err == nil {
				paused = true
				break
			}
		}
		if !paused {
			t.Fatal("latest DKG did not reach its real candidate boundary")
		}
	})
	// runPublicLegacyFlow proved the drain and observed both old-wallet nodes
	// settle their real incoming sweep while the newest DKG stayed paused.
	state, err := w.f.remotes[1].status(w.f.ctx)
	if err != nil || len(state.Wallets) != 2 {
		t.Fatal("stalled latest DKG blocked previous-wallet service", err)
	}
	t.Log("K09-RECOVERY: old-wallet incoming settled while newest DKG stayed paused")
}
func TestFrostNodeCandidateEpochInterruption(t *testing.T) {
	testFrostNodeEpochInterruption(t, "candidate-ready")
}
func TestFrostNodeApprovedEpochInterruption(t *testing.T) {
	testFrostNodeEpochInterruption(t, "approval-seen")
}
func testFrostNodeEpochInterruption(t *testing.T, boundary string) {
	for _, killed := range []bool{false, true} {
		name := "sigint"
		if killed {
			name = "sigkill"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("FROST_K09_PAUSE_BOUNDARY", boundary)
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
			if !certified && boundary == "after-readiness-postcondition" {
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
			// If interruption preceded publication, move the public clock past
			// the deadline. The surviving executor must reconcile/expire it.
			if state, err := f.registry.State(&bind.CallOpts{Context: f.ctx}); err != nil {
				t.Fatal(err)
			} else if state != 0 {
				head, err := f.client.BlockNumber(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				f.mineTo(head + 1100)
				idle := false
				for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
					state, err = f.registry.State(&bind.CallOpts{Context: f.ctx})
					if err == nil && state == 0 {
						idle = true
						break
					}
				}
				if !idle {
					t.Fatal("interrupted epoch did not expire through the surviving executor")
				}
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
			t.Log("K09-RECOVERY:", boundary, "interruption isolated; new epoch ready")
		})
	}
}
