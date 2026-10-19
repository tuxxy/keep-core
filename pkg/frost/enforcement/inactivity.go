package enforcement

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// ErrNoClaim reports that this host's records support no inactivity claim.
var ErrNoClaim = errors.New("no inactivity claim")

// Claimer derives, signs and submits one wallet host's inactivity claims.
// A claim comes only from durable heartbeat outcomes and their authenticated
// presence; worker errors and their Suspect hints are never an input
// (P-K08-NO-SUSPECT-SANCTION).
type Claimer struct {
	Chain   Chain
	Runner  *Runner
	Records Records
	// LocalSeats are this operator's seats; Key is its operator key, the
	// address the descriptor names for those seats.
	LocalSeats []uint16
	Key        *ecdsa.PrivateKey
	// Failures is how many consecutive failed heartbeats a claim needs
	// (2 in tests, OPEN; legacy uses 3). Lookback bounds the rounds scanned.
	Failures int
	Lookback uint64
	boundary func(string) error
}

func (c *Claimer) step(name string) error {
	if c.boundary != nil {
		return c.boundary(name)
	}
	return nil
}

func claimRecordID(wallet [32]byte, nonce uint64) [32]byte {
	raw := append([]byte("keep-core/frost/inactivity-claim/v1/"), wallet[:]...)
	return sha256.Sum256(binary.BigEndian.AppendUint64(raw, nonce))
}

type claimRecord struct {
	Claim Claim
	// Through is the newest heartbeat nonce the claim's window covered; a
	// later claim counts only heartbeats after it.
	Through uint64
}

// Derive returns the claim this host's records support at head, the newest
// heartbeat nonce it covers, or ErrNoClaim.
func (c *Claimer) Derive(ctx context.Context) (Claim, uint64, error) {
	if err := c.step("before-claim-read"); err != nil {
		return Claim{}, 0, err
	}
	if c.Failures < 1 || c.Runner == nil {
		return Claim{}, 0, errors.New("incomplete inactivity configuration")
	}
	w := c.Runner.Wallet
	wallet := dkg.WalletID(w.OutputKey)
	nonce, err := c.Chain.InactivityNonce(ctx, wallet)
	if err != nil {
		return Claim{}, 0, err
	}
	var after uint64
	if nonce > 0 {
		raw, err := c.Records.ReadSigningRecord(ctx, claimRecordID(wallet, nonce-1))
		if err == nil {
			var previous claimRecord
			if err = json.Unmarshal(raw, &previous); err != nil {
				return Claim{}, 0, err
			}
			after = previous.Through
		} else if !errors.Is(err, store.ErrMissing) {
			return Claim{}, 0, err
		}
	}
	current, err := c.Runner.Current(ctx)
	if err != nil {
		return Claim{}, 0, err
	}
	lookback := c.Lookback
	if lookback == 0 {
		lookback = 64
	}
	var failed []Outcome
	for k := uint64(0); k < lookback && k < current.Number && len(failed) < c.Failures; k++ {
		anchor := (current.Number - k) * c.Runner.Schedule.RoundBlocks
		if anchor <= after {
			break
		}
		o, err := ReadOutcome(ctx, c.Records, wallet, anchor)
		if errors.Is(err, store.ErrMissing) {
			// A round that never started is skipped. A round that started
			// and lost its outcome may have been signed: it ends the window.
			if _, serr := c.Records.ReadSigningRecord(ctx, startID(wallet, anchor)); serr == nil {
				break
			} else if !errors.Is(serr, store.ErrMissing) {
				return Claim{}, 0, serr
			}
			continue
		}
		if err != nil {
			return Claim{}, 0, err
		}
		if o.Signed {
			break // a signed heartbeat ends the failure streak
		}
		failed = append(failed, o)
	}
	if len(failed) < c.Failures {
		return Claim{}, 0, ErrNoClaim
	}
	var inactive []uint16
	for i, member := range w.Members {
		seat := uint16(i + 1)
		if member == 0 {
			continue
		}
		absent := true
		for _, o := range failed {
			for _, p := range o.Present {
				if p == seat {
					absent = false
				}
			}
		}
		if absent {
			inactive = append(inactive, seat)
		}
	}
	if len(inactive) == 0 {
		return Claim{}, 0, ErrNoClaim
	}
	descriptorHash, err := dkg.DescriptorHash(w)
	if err != nil {
		return Claim{}, 0, err
	}
	claim := Claim{WalletID: wallet, DescriptorHash: descriptorHash, Nonce: nonce, Inactive: inactive, HeartbeatFailed: true}
	if err = c.step("claim-derived"); err != nil {
		return Claim{}, 0, err
	}
	return claim, failed[0].Nonce, nil
}

// Sign signs a proposed claim for this operator's seats outside the claimed
// set, only if this host's own records derive exactly the same claim.
//
// A claim retained at this nonce is reused as it was retained: a retry signs
// the same claim and window again, and any other proposal at that nonce is
// refused, even if later heartbeats would derive a different window.
func (c *Claimer) Sign(ctx context.Context, proposed Claim) (map[uint16][]byte, error) {
	own, err := c.retained(ctx, proposed)
	if errors.Is(err, store.ErrMissing) {
		var through uint64
		if own, through, err = c.Derive(ctx); err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(own, proposed) {
			return nil, fmt.Errorf("%w: proposed claim differs from this host's records", ErrNoClaim)
		}
		raw, merr := json.Marshal(claimRecord{Claim: own, Through: through})
		if merr != nil {
			return nil, merr
		}
		err = c.Records.RetainSigningRecord(ctx, claimRecordID(own.WalletID, own.Nonce), raw)
	}
	if err != nil {
		return nil, err
	}
	digest, err := ClaimDigest(c.Chain.ChainID(), c.Chain.Bridge(), own)
	if err != nil {
		return nil, err
	}
	out := map[uint16][]byte{}
	for _, seat := range c.LocalSeats {
		claimed := false
		for _, s := range own.Inactive {
			claimed = claimed || s == seat
		}
		if claimed {
			continue
		}
		sig, err := redemption.SignDigest(c.Key, digest)
		if err != nil {
			return nil, err
		}
		out[seat] = sig
	}
	if err = c.step("claim-signed"); err != nil {
		return nil, err
	}
	return out, nil
}

// retained returns the claim this host retained at the proposal's nonce, or
// store.ErrMissing. A retained claim that differs from the proposal refuses it.
func (c *Claimer) retained(ctx context.Context, proposed Claim) (Claim, error) {
	raw, err := c.Records.ReadSigningRecord(ctx, claimRecordID(proposed.WalletID, proposed.Nonce))
	if err != nil {
		return Claim{}, err
	}
	var r claimRecord
	if err = json.Unmarshal(raw, &r); err != nil {
		return Claim{}, err
	}
	if !reflect.DeepEqual(r.Claim, proposed) {
		return Claim{}, fmt.Errorf("%w: this host signed another claim at nonce %d", ErrNoClaim, proposed.Nonce)
	}
	return r.Claim, nil
}

// Submit submits a collected claim once. A claim whose nonce the chain has
// already passed was submitted by another operator or by a lost reply.
func (c *Claimer) Submit(ctx context.Context, claim Claim, seats []uint16, signatures []byte) error {
	if err := c.step("before-claim-submit"); err != nil {
		return err
	}
	nonce, err := c.Chain.InactivityNonce(ctx, claim.WalletID)
	if err != nil {
		return err
	}
	if nonce > claim.Nonce {
		return c.step("claim-submitted")
	}
	if err = c.Chain.NotifyInactivity(ctx, claim, seats, signatures); err != nil {
		again, rerr := c.Chain.InactivityNonce(ctx, claim.WalletID)
		if rerr != nil || again <= claim.Nonce {
			return err
		}
	}
	return c.step("claim-submitted")
}
