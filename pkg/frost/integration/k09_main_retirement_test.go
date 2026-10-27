package integration

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/movement"
)

func setIncomingPause(w *k07World, epoch uint64, boundary string) {
	w.t.Helper()
	for i, node := range w.f.remotes {
		node.config.VerificationPause = nil
		if boundary != "" {
			node.config.VerificationPause = &verificationPause{Epoch: epoch, Boundary: boundary, Marker: filepath.Join(w.t.TempDir(), "public-"+boundary)}
		}
		raw, err := json.Marshal(node.config)
		if err != nil {
			w.t.Fatal(err)
		}
		if err = os.WriteFile(node.command.Args[2], raw, 0600); err != nil {
			w.t.Fatal(err)
		}
		restartRemoteNode(w.f, i, true)
	}
}
func waitIncomingPauses(w *k07World) {
	w.t.Helper()
	for deadline := time.Now().Add(75 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		w.mine(1)
		all := true
		for _, node := range w.f.remotes {
			if node.config.VerificationPause == nil {
				w.t.Fatal("missing verification pause")
			}
			if _, err := os.Stat(node.config.VerificationPause.Marker); err != nil {
				all = false
			}
		}
		if all {
			return
		}
	}
	w.t.Fatal("incoming boundary not reached in real nodes")
}

// A second incoming recipe really includes the first incoming sweep's main.
// While both processes are paused before ANY claim, dispatch the existing K-06
// deposit component inside those same processes. It spends/proves the old main.
// Parent observation never opens node stores or invokes worker record APIs.
func TestFrostNodeRetiresRecipeAfterProvenMainSpend(t *testing.T) {
	w := runPublicLegacyFlow(t, true)
	var target *walletHost
	for _, candidate := range []*walletHost{w.a, w.b} {
		if w.custody(candidate.id).Value > 0 {
			target = candidate
		}
	}
	if target == nil {
		t.Fatal("initial autonomous incoming did not create main")
	}
	oldMain := w.custody(target.id).MainOutpoint
	controller := bind.NewBoundContract(w.f.deployment.Exposure, c10ABI, w.f.client, w.f.client, w.f.client)
	w.send(func(o *bind.TransactOpts) (*types.Transaction, error) {
		return controller.Transact(o, "lift", crypto.Keccak256Hash([]byte("retirement deposit precursor")))
	})
	// These are public depositor/maintainer actions; signing is dispatched below.
	w.wallet, w.walletID = target.desc, target.id
	deposit := w.deposit(200_000, w.locktime(86400), [20]byte{})
	w.mine(int(k06Finality))
	setIncomingPause(w, target.desc.Epoch, "after-incoming-retain")
	key := w.drainSigner(map[string]any{})
	raw, err := hex.DecodeString(key.PublicKeyHash)
	if err != nil {
		t.Fatal(err)
	}
	var pkh [20]byte
	copy(pkh[:], raw)
	source := w.controlledLegacyMain(pkh, k07DrainValue)
	w.commitDrain(source, []*walletHost{target})
	drain := w.drain(source, []*walletHost{target})
	incomingKey := movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: drain.Hash()})
	w.mine(int(k06Finality))
	waitIncomingPauses(w)
	planner := movement.Planner{View: w.view, Bitcoin: w.btc, Wallet: target.desc, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), FeeRate: k06FeeRate, MaximumSatPerVByte: k06MaxRate}
	old, err := planner.IncomingSweep(w.f.ctx)
	if err != nil || old.MovementMain() == nil || movement.OutpointKey(old.MovementMain().Outpoint) != oldMain {
		t.Fatal("retained recipe did not actually use old main", err)
	}
	// Select one common finalized signing anchor AFTER the deposit reservation.
	for (w.head()-k06Finality)/k09NodeRoundBlocks*k09NodeRoundBlocks < deposit.readyBlock+k06Finality {
		w.mine(1)
	}
	round := (w.head() - k06Finality) / k09NodeRoundBlocks
	payload, _ := json.Marshal(struct{ Epoch, Round uint64 }{target.desc.Epoch, round})
	type answer struct{ TransactionHash [32]byte }
	replies := make(chan answer, len(w.f.remotes))
	failures := make(chan error, len(w.f.remotes))
	for _, node := range w.f.remotes {
		go func(node *remoteNode) {
			response, err := (&http.Client{Timeout: 120 * time.Second}).Post(node.endpoint+"/verification/deposit-sweep", "application/json", bytes.NewReader(payload))
			if err != nil {
				failures <- err
				return
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				failures <- fmt.Errorf("fixture component HTTP status %d at %s", response.StatusCode, response.Header.Get("X-Frost-Verification-Stage"))
				return
			}
			var got answer
			if err = json.NewDecoder(response.Body).Decode(&got); err != nil {
				failures <- err
				return
			}
			replies <- got
		}(node)
		// Core scantxoutset admits one scan at a time. Stagger public planning;
		// the first worker still waits for the other operator before signing.
		time.Sleep(250 * time.Millisecond)
	}
	var spent [32]byte
	for range w.f.remotes {
		select {
		case err := <-failures:
			t.Fatal(err)
		case got := <-replies:
			if spent != ([32]byte{}) && spent != got.TransactionHash {
				t.Fatal("nodes signed different public deposit sweep")
			}
			spent = got.TransactionHash
		case <-time.After(130 * time.Second):
			t.Fatal("fixture-dispatched deposit component timed out")
		}
	}
	w.coreMine(1)
	if _, err = w.sweeps.ProveSweep(w.f.ctx, target.id, spent, [20]byte{}, nil); err != nil {
		t.Fatal("real deposit sweep proof", err)
	}
	w.mine(int(k06Finality))
	replaced := w.custody(target.id).MainOutpoint
	if replaced == oldMain || replaced != movement.OutpointKey(bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash(spent)}) {
		t.Fatal("main did not change through actual Bitcoin/proof path")
	}
	// Kill/reload once to observe durable retirement, then kill/reload AGAIN
	// after its tombstone, before any fresh signature can hide a lost cursor.
	setIncomingPause(w, target.desc.Epoch, "after-incoming-retire")
	waitIncomingPauses(w)
	setIncomingPause(w, target.desc.Epoch, "")
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		w.mine(1)
		var pool []string
		if err = w.core.call(w.f.ctx, "getrawmempool", nil, &pool); err != nil {
			t.Fatal(err)
		}
		if len(pool) > 0 {
			w.coreMine(1)
		}
		record, err := w.chain.Incoming(w.f.ctx, w.head(), incomingKey)
		if err != nil {
			t.Fatal(err)
		}
		if record.State != movement.IncomingSwept {
			continue
		}
		w.mine(int(k06Finality))
		custody := w.custody(target.id)
		if custody.MainOutpoint == replaced || custody.MainOutpoint == oldMain || custody.Value <= 200_000 {
			t.Fatal("fresh settlement did not replace proven deposit main")
		}
		// The old incoming transaction was never broadcast. A fresh result had to
		// include the newly proven main to pass the actual C-08 proof.
		if raw, err := w.core.Transaction(w.f.ctx, bitcoin.Hash(old.BitcoinTransactionHash())); err == nil && len(raw) > 0 {
			t.Fatal("old stale transaction was broadcast")
		}
		t.Log("K09-RECOVERY: finalized deposit sweep spent stale recipe main; both nodes durably retired, restarted and settled a fresh incoming sweep")
		return
	}
	t.Fatal("control SpentMainRecovery assertion: fresh incoming did not settle after durable retirement")
}
