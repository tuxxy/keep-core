package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/movement"
	"github.com/keep-network/keep-core/pkg/maintainer/spv"
)

// A certified wallet interrupted before its local ReadyUnfunded save must
// recover its durable key and actually spend a subsequent incoming drain.
func TestFrostNodeCertifiedEpochInterruption(t *testing.T) {
	testCertifiedRecovery(t, "after-readiness-postcondition")
}
func TestFrostNodePrecertificationRecovery(t *testing.T) {
	t.Setenv("FROST_K09_PAUSE_ALL", "1")
	testCertifiedRecovery(t, "after-key-reload")
}
func TestFrostNodeMissingPreKeyIsLost(t *testing.T) {
	testFrostNodeEpochInterruption(t, "after-pending-wallet-save")
}
func TestFrostNodeCertificateSubmitInterruption(t *testing.T) {
	testCertifiedRecovery(t, "after-certificate-submit")
}
func testCertifiedRecovery(t *testing.T, boundary string) {
	for _, killed := range []bool{false, true} {
		name := "sigint"
		if killed {
			name = "sigkill"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("FROST_K09_PAUSE_BOUNDARY", boundary)
			var origin k09LegacyOrigin
			k6 := newK06WorldWith(t, k06Options{mode: "k09-public", remote: true, prepare: bootstrapPublicLegacy(t, &origin), beforeDKG: func(f *fixture) {
				node := f.remotes[1]
				paused := false
				for until := time.Now().Add(90 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
					if _, err := os.Stat(node.config.VerificationPause.Marker); err == nil {
						paused = true
						break
					}
				}
				if !paused {
					t.Fatal("real certified recovery boundary not reached")
				}
				if boundary == "after-key-reload" {
					for _, other := range f.remotes {
						for until := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
							if _, err := os.Stat(other.config.VerificationPause.Marker); err == nil {
								break
							}
							if time.Now().After(until) {
								t.Fatal("not all durable keys paused")
							}
						}
					}
					id, err := f.registry.ApprovedWallet(&bind.CallOpts{Context: f.ctx}, f.deployment.Epoch)
					if err != nil {
						t.Fatal(err)
					}
					public, err := f.registry.Wallet(&bind.CallOpts{Context: f.ctx}, id)
					if err != nil || public.State != 1 {
						t.Fatal("recovery must restart BEFORE certification", err)
					}
					for _, other := range f.remotes {
						other.config.VerificationPause = nil
						raw, err := json.Marshal(other.config)
						if err != nil {
							t.Fatal(err)
						}
						if err = os.WriteFile(other.command.Args[2], raw, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				restartRemoteNode(f, 1, killed)
				if boundary == "after-key-reload" {
					restartRemoteNode(f, 0, killed)
				}

			}})
			status, err := k6.f.remotes[1].status(k6.f.ctx)
			if err != nil || len(status.Wallets) != 1 || len(status.LostEpochs) != 0 {
				t.Fatal("durable certified seat was not recovered", err)
			}
			w := &k07World{k06World: k6}
			w.view = &movement.View{Chain: k6.chain, Policy: movement.Policy{FinalityBlocks: k06Finality}}
			w.proofs = spv.FrostMovementMaintainer{Chain: k6.chain, Bitcoin: k6.btc, Confirmations: 1}
			w.legacy, err = legacyabi.NewBridge(k6.f.deployment.Bridge, k6.f.client)
			if err != nil {
				t.Fatal(err)
			}
			var requests []frost.DKGRequest
			for _, executor := range k6.f.executors {
				requests = append(requests, executor.Request)
			}
			w.a = w.host(k6.wallet, k6.journals, requests, k6.locks)
			w.b = w.a // One eligible wallet: the drain must reach the recovered key.
			finishPublicLegacyFlow(t, w, origin, true)
			if w.custody(w.a.id).Value == 0 {
				t.Fatal("recovered certified key did not settle incoming")
			}
			t.Log("K09-RECOVERY:", boundary, name, "certified key recovered and incoming settled")
		})
	}
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
				node.config.VerificationPause = &verificationPause{Boundary: "after-incoming-sign", Marker: filepath.Join(t.TempDir(), "public-signed-boundary")}
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
		node.config.VerificationPause = &verificationPause{Boundary: "candidate-ready", Marker: filepath.Join(t.TempDir(), "public-candidate-boundary")}
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

// A leader cannot know whether an advertised recipe has produced a peer grant.
// Fee-policy changes after publication retain that recipe through settlement.
func TestFrostNodeAdvertisedIncomingStaysPinnedAfterRestart(t *testing.T) {
	testIncomingRecipeRestart(t, false)
}
func TestFrostNodeGrantedIncomingStaysPinned(t *testing.T) { testIncomingRecipeRestart(t, true) }
func testIncomingRecipeRestart(t *testing.T, granted bool) {
	var restarted bool
	var expected, walletID [32]byte
	w := runPublicLegacyFlow(t, true, func(w *k07World) {
		for i, node := range w.f.remotes {
			if granted && i != 0 {
				continue
			}
			node.config.VerificationPause = &verificationPause{Boundary: func() string {
				if granted {
					return "after-incoming-grant"
				}
				return "after-incoming-retain"
			}(), Marker: filepath.Join(t.TempDir(), "public-retained-boundary")}
			raw, err := json.Marshal(node.config)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
				t.Fatal(err)
			}
			restartRemoteNode(w.f, i)
		}
		w.observeIncoming = func() {
			if restarted {
				return
			}
			for i, node := range w.f.remotes {
				if granted && i != 0 {
					continue
				}
				if _, err := os.Stat(node.config.VerificationPause.Marker); err != nil {
					return
				}
			}
			for _, target := range []*walletHost{w.a, w.b} {
				p := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: target.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
				plan, err := p.IncomingSweep(w.f.ctx)
				if err == nil {
					expected, walletID = plan.BitcoinTransactionHash(), target.id
				}
			}
			if expected == ([32]byte{}) {
				t.Fatal("stale recipe has no public replacement")
			}
			for i, node := range w.f.remotes {
				node.config.FeeRate = 3
				node.config.VerificationPause = nil
				raw, err := json.Marshal(node.config)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
					t.Fatal(err)
				}
				restartRemoteNode(w.f, i, true)
			}
			restarted = true
		}
	})
	if !restarted || w.custody(walletID).MainOutpoint != movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(expected)}) {
		t.Fatal("advertised plan did not remain pinned after SIGKILL")
	}
	t.Log("K09-RECOVERY: advertised intent retained across fee change and SIGKILL; original settled once")
}

func TestFrostNodeDifferentFeesAgree(t *testing.T) {
	runPublicLegacyFlow(t, true, func(w *k07World) {
		node := w.f.remotes[1]
		node.config.FeeRate = 3
		node.config.MaximumSatPerVByte = k06MaxRate + 1
		raw, err := json.Marshal(node.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
			t.Fatal(err)
		}
		restartRemoteNode(w.f, 1)
	})
	t.Log("K09-RECOVERY: differing preferred fees and ceilings agreed a validated incoming recipe")
}

// Real nodes: only the follower reaches its durable grant. The leader is
// interrupted before claim. An additional actual drain is finalized while both
// are stopped, then the old recipe must settle despite changed fee preferences.
// The second legacy source main is an explicit controlled fixture precursor;
// its Bitcoin signature, mining and incoming proof are real.
func TestFrostNodeFollowerGrantKeepsLeaderRecipe(t *testing.T) {
	var restarted bool
	var expected, walletID, newIncoming [32]byte
	w := runPublicLegacyFlow(t, true, func(w *k07World) {
		for i, node := range w.f.remotes {
			point := "after-incoming-retain"
			if i == 1 {
				point = "after-incoming-grant"
			}
			node.config.VerificationPause = &verificationPause{Boundary: point, Marker: filepath.Join(t.TempDir(), "public-split-boundary")}
			raw, _ := json.Marshal(node.config)
			if err := os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
				t.Fatal(err)
			}
			restartRemoteNode(w.f, i)
		}
		w.observeIncoming = func() {
			if restarted {
				return
			}
			for _, node := range w.f.remotes {
				if _, err := os.Stat(node.config.VerificationPause.Marker); err != nil {
					return
				}
			}
			var target *walletHost
			var old *signing.Plan
			for _, h := range []*walletHost{w.a, w.b} {
				p := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: h.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
				if plan, err := p.IncomingSweep(w.f.ctx); err == nil {
					target = h
					old = plan
				}
			}
			if target == nil {
				t.Fatal("missing original incoming recipe")
			}
			expected, walletID = old.BitcoinTransactionHash(), target.id
			// Keep the leader dead across the new finalized observation.
			leaderBefore, err := w.f.remotes[0].status(w.f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.f.remotes[0].command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			<-w.f.remotes[0].done
			controller := bind.NewBoundContract(w.f.deployment.Exposure, c10ABI, w.f.client, w.f.client, w.f.client)
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return controller.Transact(o, "lift", crypto.Keccak256Hash([]byte("second incoming fixture")))
			})
			key := w.drainSigner(map[string]any{})
			raw, _ := hex.DecodeString(key.PublicKeyHash)
			var pkh [20]byte
			copy(pkh[:], raw)
			source := w.controlledLegacyMain(pkh, k07DrainValue)
			w.commitDrain(source, []*walletHost{target})
			tx := w.drain(source, []*walletHost{target})
			newIncoming = movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: tx.Hash()})
			w.mine(int(k06Finality))
			pending, err := w.chain.Incoming(w.f.ctx, w.head(), newIncoming)
			if err != nil || pending.State != movement.IncomingPending {
				t.Fatal("new incoming did not finalize", err)
			}
			planner := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: target.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: 3, MaximumSatPerVByte: k06MaxRate}
			changed, err := planner.IncomingSweep(w.f.ctx)
			if err != nil || changed.Commitment() == old.Commitment() {
				t.Fatal("new incoming did not change fresh recipe", err)
			}
			w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
				return controller.Transact(o, "contain", crypto.Keccak256Hash([]byte("split recovery containment")))
			})
			for i, node := range w.f.remotes {
				node.config.FeeRate = 3
				node.config.VerificationPause = nil
				raw, _ := json.Marshal(node.config)
				if err := os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					restartStoppedRemoteNode(w.f, i, leaderBefore)
				} else {
					restartRemoteNode(w.f, i, true)
				}
			}
			restarted = true
		}
	})
	if !restarted || expected == ([32]byte{}) || newIncoming == ([32]byte{}) || w.custody(walletID).MainOutpoint != movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(expected)}) {
		t.Fatal("control FollowerGrant assertion: split recipes did not converge to original")
	}
	t.Log("K09-RECOVERY: follower-only grant, leader pre-claim kill and new finalized incoming retained original recipe")
}

func TestFrostNodeFollowerRefusesOverCeilingRecipe(t *testing.T) {
	refused := false
	runPublicLegacyFlow(t, true, func(w *k07World) {
		node := w.f.remotes[1]
		node.config.FeeRate = 1
		node.config.MaximumSatPerVByte = 1
		raw, _ := json.Marshal(node.config)
		if err := os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
			t.Fatal(err)
		}
		restartRemoteNode(w.f, 1)
		w.observeIncoming = func() {
			if refused {
				return
			}
			status, err := node.status(w.f.ctx)
			if err != nil {
				return
			}
			for _, stage := range status.FailureStages {
				if stage == "incoming-recipe" {
					refused = true
				}
			}
			if !refused {
				return
			}
			node.config.MaximumSatPerVByte = k06MaxRate
			raw, _ := json.Marshal(node.config)
			if err := os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
				t.Fatal(err)
			}
			restartRemoteNode(w.f, 1, true)
		}
	})
	if !refused {
		t.Fatal("control FollowerCeiling assertion: real follower never refused over-ceiling recipe")
	}
}
