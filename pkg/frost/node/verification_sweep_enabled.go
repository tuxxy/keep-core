//go:build frosttest

package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/deposit"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

type verificationClock struct{ bitcoin.Chain }

func (c verificationClock) MedianTimePast(context.Context) (uint64, error) {
	height, err := c.GetLatestBlockHeight()
	if err != nil {
		return 0, err
	}
	if height < 10 {
		return 0, errors.New("Bitcoin clock too short")
	}
	times := make([]uint32, 0, 11)
	for i := uint(0); i < 11; i++ {
		header, err := c.GetBlockHeader(height - i)
		if err != nil {
			return 0, err
		}
		times = append(times, header.Time)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return uint64(times[5]), nil
}

// RegisterVerification exposes a single fixture-dispatched existing component
// only in an explicit frosttest executable with a paused pre-claim incoming
// duty. The node keeps ownership of its store and worker; the parent supplies
// public epoch/round values and receives a public Bitcoin transaction hash.
// This is evidence scaffolding, not autonomous deposit or all-duty dispatch.
func (n *Node) RegisterVerification(mux *http.ServeMux) {
	mux.HandleFunc("/verification/deposit-sweep", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != "POST" {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		stage := "precondition"
		fail := func() {
			response.Header().Set("X-Frost-Verification-Stage", stage)
			http.Error(response, "fixture component refused; inspect private diagnostics", http.StatusConflict)
		}
		pause := n.cfg.VerificationPause
		if pause == nil || pause.Boundary != "after-incoming-retain" {
			fail()
			return
		}
		if _, err := os.Stat(pause.Marker); err != nil {
			fail()
			return
		}
		var public struct{ Epoch, Round uint64 }
		if err := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1024)).Decode(&public); err != nil {
			fail()
			return
		}
		n.mu.RLock()
		w := n.wallets[public.Epoch]
		n.mu.RUnlock()
		if w == nil || pause.Epoch != public.Epoch || pause.Epoch == 0 {
			fail()
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), time.Duration(n.cfg.ProtocolSeconds)*time.Second)
		defer cancel()
		policy := deposit.Policy{FinalityBlocks: n.cfg.FinalityBlocks, RefundMargin: 60}
		clock := verificationClock{n.btc}
		view := &deposit.View{Chain: n.chain, Clock: clock, Policy: policy}
		base := w.mover.Base
		base.AfterGrant = nil
		sweeper := &deposit.Sweeper{Chain: n.chain, Planner: deposit.Planner{Chain: n.chain, Bitcoin: n.btc, Funding: n.btc, Lock: w.mover.Lock, Wallet: w.record.Descriptor, Genesis: [32]byte(*chaincfg.RegressionNetParams.GenesisHash), Policy: policy, FeeRate: n.cfg.FeeRate, MaximumSatPerVByte: n.cfg.MaximumSatPerVByte}, Authority: &deposit.Authority{View: view}, Lock: w.mover.Lock, Ledger: redemption.Ledger{Journal: w.journal}, Schedule: w.mover.Schedule, Base: base}
		stage = "plan"
		plan, err := sweeper.Plan(ctx, 0, deposit.Round{Anchor: public.Round * n.cfg.RoundBlocks})
		if err != nil {
			fail()
			return
		}
		stage = "sign"
		if _, err = sweeper.Sign(ctx, plan, public.Round); err != nil {
			fail()
			return
		}
		stage = "broadcast"
		if err = sweeper.Broadcast(ctx, plan); err != nil {
			fail()
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(struct{ TransactionHash [32]byte }{plan.BitcoinTransactionHash()})
	})
}
