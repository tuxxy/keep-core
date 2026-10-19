package enforcement

import (
	"context"
	"encoding/binary"
	"errors"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

func runnerFixture(t *testing.T) (*Runner, *fakeChain, *memRecords) {
	t.Helper()
	w := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: big.NewInt(1), Registry: common.Address{1}, Epoch: 17, Members: []uint32{11, 12, 13}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{9}, OutputKey: [32]byte{0x42}}
	f := &fakeChain{head: 45}
	r := &memRecords{m: map[[32]byte][]byte{}}
	return &Runner{Chain: f, Schedule: redemption.Schedule{FinalityBlocks: 2, RoundBlocks: 10}, Wallet: w, Base: signing.HeartbeatConfig{Journal: recordsJournal{r: r}}}, f, r
}

// recordsJournal adapts memRecords to signing.Journal for the runner's base
// configuration; only the record methods are used.
type recordsJournal struct {
	signing.Journal
	r *memRecords
}

func (j recordsJournal) RetainSigningRecord(ctx context.Context, id [32]byte, p []byte) error {
	return j.r.RetainSigningRecord(ctx, id, p)
}
func (j recordsJournal) ReadSigningRecord(ctx context.Context, id [32]byte) ([]byte, error) {
	return j.r.ReadSigningRecord(ctx, id)
}

// Each heartbeat round runs once; its outcome and presence are durable, and
// an interrupted round is unknown, never a failure.
func TestHeartbeatRunnerOutcomes(t *testing.T) {
	ctx := context.Background()
	t.Run("the nonce is the current round anchor", func(t *testing.T) {
		r, _, _ := runnerFixture(t)
		round, err := r.Current(ctx)
		if err != nil || round.Anchor != 40 || round.StartBlock != 42 {
			t.Fatal(round, err)
		}
		wallet := dkg.WalletID(r.Wallet.OutputKey)
		if r.AuthorizeHeartbeat(ctx, wallet, 40) != nil || r.AuthorizeHeartbeat(ctx, wallet, 30) == nil || r.AuthorizeHeartbeat(ctx, [32]byte{1}, 40) == nil {
			t.Fatal("heartbeat nonce rule")
		}
	})
	t.Run("signed, recorded once", func(t *testing.T) {
		r, _, _ := runnerFixture(t)
		calls := 0
		r.sign = func(_ context.Context, c signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			calls++
			if c.Nonce != 40 || c.Authority != r {
				t.Fatal("heartbeat configuration")
			}
			return signing.HeartbeatResult{Present: []uint16{1, 2, 3}}, nil
		}
		round, _ := r.Current(ctx)
		o, err := r.Run(ctx, round)
		if err != nil || !o.Signed {
			t.Fatal(o, err)
		}
		if o, err = r.Run(ctx, round); err != nil || !o.Signed || calls != 1 {
			t.Fatal("a recorded round ran again", calls)
		}
	})
	t.Run("an interrupted round is unknown after restart", func(t *testing.T) {
		r, _, _ := runnerFixture(t)
		r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			return signing.HeartbeatResult{}, errors.New("process killed")
		}
		round, _ := r.Current(ctx)
		if _, err := r.Run(ctx, round); err == nil {
			t.Fatal("failure lost")
		}
		r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			t.Fatal("an interrupted round ran again")
			return signing.HeartbeatResult{}, nil
		}
		if _, err := r.Run(ctx, round); !errors.Is(err, ErrUnknownOutcome) {
			t.Fatal(err)
		}
	})
	t.Run("a refused heartbeat records no outcome", func(t *testing.T) {
		r, _, rec := runnerFixture(t)
		r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			return signing.HeartbeatResult{Present: []uint16{1}}, signing.ErrUnauthorized
		}
		round, _ := r.Current(ctx)
		r.Run(ctx, round)
		if _, err := ReadOutcome(ctx, rec, dkg.WalletID(r.Wallet.OutputKey), 40); !errors.Is(err, store.ErrMissing) {
			t.Fatal("refused heartbeat recorded an outcome")
		}
	})
}

func failRound(t *testing.T, r *Runner, f *fakeChain, head uint64, present []uint16, suspect uint16) {
	t.Helper()
	f.head = head
	r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
		return signing.HeartbeatResult{Present: present}, &frost.Error{Category: frost.ProtocolFailure, Suspect: suspect}
	}
	round, err := r.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o, err := r.Run(context.Background(), round); err == nil || o.Signed {
		t.Fatal("expected a failed heartbeat")
	}
}

// P-K08-NO-SUSPECT-SANCTION: a claim names only seats absent from the
// authenticated presence of every failed heartbeat in its window.
func TestInactivityClaimFromPresenceOnly(t *testing.T) {
	ctx := context.Background()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	claimer := func(r *Runner, f *fakeChain, rec *memRecords) *Claimer {
		return &Claimer{Chain: f, Runner: r, Records: rec, LocalSeats: []uint16{1}, Key: key, Failures: 2}
	}
	t.Run("two failures with seat 3 absent", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 2)
		failRound(t, r, f, 45, []uint16{1, 2}, 2)
		c := claimer(r, f, rec)
		claim, through, err := c.Derive(ctx)
		if err != nil || !reflect.DeepEqual(claim.Inactive, []uint16{3}) || !claim.HeartbeatFailed || through != 40 {
			t.Fatal(claim, through, err)
		}
		sigs, err := c.Sign(ctx, claim)
		if err != nil || len(sigs) != 1 {
			t.Fatal(sigs, err)
		}
		digest, _ := ClaimDigest(f.ChainID(), f.Bridge(), claim)
		pub, err := crypto.SigToPub(crypto.Keccak256([]byte("\x19Ethereum Signed Message:\n32"), digest[:]), append(sigs[1][:64:64], sigs[1][64]-27))
		if err != nil || crypto.PubkeyToAddress(*pub) != crypto.PubkeyToAddress(key.PublicKey) {
			t.Fatal("claim signature does not recover to the operator")
		}
		seats, packed, err := redemption.Collect(1, sigs)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Submit(ctx, claim, seats, packed); err != nil || len(f.claims) != 1 {
			t.Fatal(err)
		}
		if err = c.Submit(ctx, claim, seats, packed); err != nil || len(f.claims) != 1 {
			t.Fatal("a submitted claim was submitted again")
		}
		if _, _, err = c.Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatal("the claimed window was claimed again", err)
		}
	})
	t.Run("a Suspect hint names no one", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2, 3}, 2)
		failRound(t, r, f, 45, []uint16{1, 2, 3}, 2)
		if claim, _, err := claimer(r, f, rec).Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatal("a suspect hint produced a claim", claim)
		}
	})
	t.Run("one failure is not enough", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		if _, _, err := claimer(r, f, rec).Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatal("one failure produced a claim", err)
		}
	})
	t.Run("a seat present in either failure is not claimed", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2, 3}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		if _, _, err := claimer(r, f, rec).Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatal(err)
		}
	})
	t.Run("a signed heartbeat ends the streak", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 25, []uint16{1, 2}, 0)
		f.head = 35
		r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			return signing.HeartbeatResult{Present: []uint16{1, 2, 3}}, nil
		}
		round, _ := r.Current(ctx)
		r.Run(ctx, round)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		if _, _, err := claimer(r, f, rec).Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatal(err)
		}
	})
	t.Run("a started round without an outcome ends the window", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 25, []uint16{1, 2}, 0)
		// Round 30 was signed, but the process stopped before its outcome
		// was written; after a restart it is unknown.
		f.head = 35
		r.sign = func(context.Context, signing.HeartbeatConfig) (signing.HeartbeatResult, error) {
			return signing.HeartbeatResult{}, errors.New("process killed after the signature")
		}
		round, _ := r.Current(ctx)
		r.Run(ctx, round)
		if _, err := r.Run(ctx, round); !errors.Is(err, ErrUnknownOutcome) {
			t.Fatal(err)
		}
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		if claim, _, err := claimer(r, f, rec).Derive(ctx); !errors.Is(err, ErrNoClaim) {
			t.Fatal("failures on both sides of an unknown round were claimed as consecutive", claim)
		}
	})
	t.Run("a retried claim reuses its retained window", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		c := claimer(r, f, rec)
		claim, _, err := c.Derive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		c.boundary = func(name string) error {
			if name == "claim-signed" {
				return errors.New("crash before the signatures left")
			}
			return nil
		}
		if _, err = c.Sign(ctx, claim); err == nil {
			t.Fatal("the crash hook did not run")
		}
		c.boundary = nil
		failRound(t, r, f, 55, []uint16{1, 2}, 0)
		sigs, err := c.Sign(ctx, claim)
		if err != nil || len(sigs) != 1 {
			t.Fatal("a retried claim conflicted with its own retained record:", err)
		}
		other := claim
		other.Inactive = []uint16{2, 3}
		if _, err = c.Sign(ctx, other); !errors.Is(err, ErrNoClaim) {
			t.Fatal("another claim was signed at a nonce this host already signed", err)
		}
	})
	t.Run("a different proposal is not signed", func(t *testing.T) {
		r, f, rec := runnerFixture(t)
		failRound(t, r, f, 35, []uint16{1, 2}, 0)
		failRound(t, r, f, 45, []uint16{1, 2}, 0)
		c := claimer(r, f, rec)
		claim, _, _ := c.Derive(ctx)
		claim.Inactive = []uint16{2, 3}
		if _, err := c.Sign(ctx, claim); !errors.Is(err, ErrNoClaim) {
			t.Fatal("signed a claim this host's records do not support", err)
		}
	})
}

func TestClaimDigestEncoding(t *testing.T) {
	c := Claim{WalletID: [32]byte{1}, DescriptorHash: [32]byte{2}, Nonce: 3, Inactive: []uint16{4, 5}, HeartbeatFailed: true}
	got, err := ClaimDigest(31337, [20]byte{0xb}, c)
	if err != nil {
		t.Fatal(err)
	}
	word := func(b []byte) []byte { return append(make([]byte, 32-len(b)), b...) }
	u := func(v uint64) []byte { return word(binary.BigEndian.AppendUint64(nil, v)) }
	domain := crypto.Keccak256Hash([]byte("tbtc-v2/frost-inactivity/v1"))
	var raw []byte
	raw = append(raw, domain[:]...)
	raw = append(raw, u(31337)...)
	raw = append(raw, word([]byte{0xb, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})...)
	raw = append(raw, c.WalletID[:]...)
	raw = append(raw, c.DescriptorHash[:]...)
	raw = append(raw, u(3)...)
	raw = append(raw, u(8*32)...) // offset of the dynamic array
	raw = append(raw, u(1)...)
	raw = append(raw, u(2)...)
	raw = append(raw, u(4)...)
	raw = append(raw, u(5)...)
	if want := crypto.Keccak256Hash(raw); got != [32]byte(want) {
		t.Fatalf("claim digest %x, want %x", got, want)
	}
}
