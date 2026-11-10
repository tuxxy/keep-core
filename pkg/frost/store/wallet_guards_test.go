package store

// Mutation batch M2 (MUTATION-REPORT-2026-11-10, pkg/frost/store): wallet and
// signing-record guards, one case per clause. Kill table in the PR body.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// walletFor is walletFixture for an arbitrary domain d.
func walletFor(t *testing.T, d frost.Domain) dkg.WalletRecord {
	t.Helper()
	q, _ := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	var output [32]byte
	copy(output[:], q)
	desc := dkg.Descriptor{Scheme: 2, Profile: 1, ChainId: new(big.Int).SetBytes(d.Chain[:]), Registry: common.Address(d.Registry), Epoch: d.Epoch, Members: []uint32{1, 2, 3}, Operators: []common.Address{{1}, {2}, {3}}, Threshold: 2, SnowfallDescriptor: [32]byte{4}, OutputKey: output}
	hash, e := dkg.DescriptorHash(desc)
	must(t, e)
	return dkg.WalletRecord{Descriptor: desc, ID: dkg.WalletID(output), DescriptorHash: hash, State: "Candidate"}
}

func approved(w dkg.WalletRecord, state string) dkg.WalletRecord {
	w.ApprovalBlock, w.ApprovalHash, w.ResultHash, w.Deadline, w.State = 20, common.Hash{3}, [32]byte{5}, 40, state
	return w
}

// forgeWallet stores rec as the domain's wallet record without SaveWallet's
// checks. TEST FIXTURE: it installs an authenticated record that a correct
// store never writes, to test the checks applied when it is read back.
func forgeWallet(t *testing.T, s *Store, v *Scoped, rec dkg.WalletRecord) {
	t.Helper()
	plain, err := json.Marshal(rec)
	must(t, err)
	raw, err := s.seal(plain, []byte("keep-core/snowfall/wallet/v1/"+v.prefix()))
	must(t, err)
	next := clone(s.state)
	next.Wallets[v.prefix()] = raw
	commitFixture(t, s, next)
}

// Invariant (SaveWallet entry checks): a first wallet record is refused if
// any one of its descriptor hash, wallet ID, epoch, registry, chain or state
// is wrong for the record or the domain. Every other field stays valid, and
// a changed descriptor carries its recomputed hash and ID.
// Rows wallet.go:21 (5 LO), :30, :34.
func TestSaveWalletRefusesEachInvalidField(t *testing.T) {
	cases := map[string]func(*dkg.WalletRecord){
		"descriptor-hash": func(w *dkg.WalletRecord) { w.DescriptorHash[0] ^= 1 },
		"wallet-id":       func(w *dkg.WalletRecord) { w.ID[0] ^= 1 },
		"epoch":           func(w *dkg.WalletRecord) { w.Descriptor.Epoch++ },
		"registry":        func(w *dkg.WalletRecord) { w.Descriptor.Registry[0] ^= 1 },
		"chain":           func(w *dkg.WalletRecord) { w.Descriptor.ChainId = big.NewInt(2) },
		"state": func(w *dkg.WalletRecord) {
			*w = approved(*w, "Live")
		},
	}
	rebound := map[string]bool{"epoch": true, "registry": true, "chain": true}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			w := walletFixture(t)
			change(&w)
			if rebound[name] {
				w = rebind(t, w)
			}
			if err := v.SaveWallet(context.Background(), w); err == nil {
				t.Fatalf("SaveWallet accepted a record with a wrong %s", name)
			}
			if _, err := v.LoadWallet(context.Background()); !errors.Is(err, ErrMissing) {
				t.Fatalf("a refused record (%s) reached the store: %v", name, err)
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		_, v := openTest(t, testConfig(t))
		if err := v.SaveWallet(context.Background(), walletFixture(t)); err != nil {
			t.Fatalf("positive control: a valid record was refused: %v", err)
		}
	})
}

// Invariant: a chain ID of exactly 256 bits is a valid chain. The bound on
// the chain ID is 256 bits inclusive. Row wallet.go:25 (CB >=).
func TestSaveWalletAcceptsFullWidthChain(t *testing.T) {
	s, _ := openTest(t, testConfig(t))
	d := testDomain()
	d.Chain = [32]byte{0x80}
	v, err := s.Scope(d)
	must(t, err)
	w := walletFor(t, d)
	if w.Descriptor.ChainId.BitLen() != 256 {
		t.Fatal("fixture chain ID is not 256 bits")
	}
	if err := v.SaveWallet(context.Background(), w); err != nil {
		t.Fatalf("a 256-bit chain ID was refused: %v", err)
	}
	loaded, err := v.LoadWallet(context.Background())
	must(t, err)
	if !reflect.DeepEqual(loaded, w) {
		t.Fatal("a 256-bit chain wallet did not load back unchanged")
	}
}

// Invariant: wallet records of different domains in one store are
// independent: saving one never removes another. Row wallet.go:89.
func TestWalletRecordsOfOtherDomainsSurvive(t *testing.T) {
	s, a := openTest(t, testConfig(t))
	d := testDomain()
	d.Epoch++
	b, err := s.Scope(d)
	must(t, err)
	wa, wb := walletFixture(t), walletFor(t, d)
	must(t, a.SaveWallet(context.Background(), wa))
	must(t, b.SaveWallet(context.Background(), wb))
	loaded, err := a.LoadWallet(context.Background())
	if err != nil || !reflect.DeepEqual(loaded, wa) {
		t.Fatalf("saving another domain's wallet removed this one: %v", err)
	}
}

// Invariant: a quarantined store neither saves nor returns wallet records.
// Rows wallet.go:47, :100.
func TestWalletRefusedOnQuarantinedStore(t *testing.T) {
	s, v := openTest(t, testConfig(t))
	must(t, v.SaveWallet(context.Background(), walletFixture(t)))
	poison(t, s, v)
	if err := v.SaveWallet(context.Background(), approved(walletFixture(t), "RegisteredPendingReady")); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("a quarantined store saved a wallet record: %v", err)
	}
	if _, err := v.LoadWallet(context.Background()); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("a quarantined store returned a wallet record: %v", err)
	}
}

// Invariant (custody monotonicity, SaveWallet): once stored, a wallet's
// descriptor, ID, descriptor hash and approval receipt (block, hash,
// deadline, result hash) never change; its state never moves back; quarantine
// is never cleared; and ReadyUnfunded never moves to Closed through this path.
// Each refused case changes exactly one of these. The stored record is
// honest except in the cases marked fixture: SaveWallet's entry checks bind
// descriptor, ID and hash together, so only an authenticated but
// inconsistent stored record (forgeWallet) can differ in one of them alone.
// Rows wallet.go:80 (6 mutants).
func TestSaveWalletRefusesEachIdentityOrLifecycleChange(t *testing.T) {
	type tc struct {
		ready  bool                                                        // stored record needs the domain's durable key
		stored func(t *testing.T, s *Store, v *Scoped, w dkg.WalletRecord) // installs the stored record
		update func(w dkg.WalletRecord) dkg.WalletRecord
	}
	save := func(states ...dkg.WalletRecord) func(*testing.T, *Store, *Scoped, dkg.WalletRecord) {
		return func(t *testing.T, _ *Store, v *Scoped, _ dkg.WalletRecord) {
			for _, w := range states {
				must(t, v.SaveWallet(context.Background(), w))
			}
		}
	}
	forged := func(change func(*dkg.WalletRecord)) func(*testing.T, *Store, *Scoped, dkg.WalletRecord) {
		return func(t *testing.T, s *Store, v *Scoped, w dkg.WalletRecord) {
			change(&w)
			forgeWallet(t, s, v, w)
		}
	}
	pending := approved(walletFixture(t), "RegisteredPendingReady")
	same := func(w dkg.WalletRecord) dkg.WalletRecord { return w }
	refused := map[string]tc{
		"descriptor (fixture)":      {stored: forged(func(w *dkg.WalletRecord) { w.Descriptor.SnowfallDescriptor[0] ^= 1 }), update: same},
		"wallet-id (fixture)":       {stored: forged(func(w *dkg.WalletRecord) { w.ID[0] ^= 1 }), update: same},
		"descriptor-hash (fixture)": {stored: forged(func(w *dkg.WalletRecord) { w.DescriptorHash[0] ^= 1 }), update: same},
		"rank-regression":           {stored: save(pending), update: func(w dkg.WalletRecord) dkg.WalletRecord { w.State = "Candidate"; return w }},
		"un-quarantine": {stored: save(func() dkg.WalletRecord { q := pending; q.Quarantined = true; return q }()),
			update: same},
		"ready-to-closed": {ready: true, stored: func(t *testing.T, _ *Store, v *Scoped, w dkg.WalletRecord) {
			must(t, v.SaveWallet(context.Background(), w))
		}, update: func(w dkg.WalletRecord) dkg.WalletRecord { w.State = "Closed"; return w }},
		"closed-to-pending": {stored: save(pending, approved(walletFixture(t), "Closed")), update: same},
		"approval-block":    {stored: save(pending), update: func(w dkg.WalletRecord) dkg.WalletRecord { w.ApprovalBlock++; return w }},
		"approval-hash":     {stored: save(pending), update: func(w dkg.WalletRecord) dkg.WalletRecord { w.ApprovalHash[0] ^= 1; return w }},
		"deadline":          {stored: save(pending), update: func(w dkg.WalletRecord) dkg.WalletRecord { w.Deadline++; return w }},
		"result-hash":       {stored: save(pending), update: func(w dkg.WalletRecord) dkg.WalletRecord { w.ResultHash[0] ^= 1; return w }},
	}
	for name, c := range refused {
		t.Run(name, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			w := pending
			if c.ready {
				w = readyFixture(t, v)
				w.State = "ReadyUnfunded"
			}
			c.stored(t, s, v, w)
			before := bytes.Clone(s.state.Wallets[v.prefix()])
			if err := v.SaveWallet(context.Background(), c.update(w)); err == nil {
				t.Fatalf("SaveWallet accepted a %s change of a stored wallet", name)
			}
			if !bytes.Equal(before, s.state.Wallets[v.prefix()]) {
				t.Fatalf("a refused %s change replaced the stored wallet", name)
			}
		})
	}
}

// Invariant: the monotone updates are accepted: a Candidate gains its
// approval receipt, a pending wallet closes, and the separate quarantine flag
// (KC-SF-V3 4.2: "plus a separate quarantine flag") can be set in every
// state, Closed and ReadyUnfunded included. Rows wallet.go:80 (CN == and !=).
func TestSaveWalletAcceptsMonotoneUpdates(t *testing.T) {
	quarantine := func(w dkg.WalletRecord) dkg.WalletRecord { w.Quarantined = true; return w }
	cases := map[string]func(t *testing.T, v *Scoped) []dkg.WalletRecord{
		"candidate-to-approved": func(t *testing.T, _ *Scoped) []dkg.WalletRecord {
			return []dkg.WalletRecord{walletFixture(t), approved(walletFixture(t), "RegisteredPendingReady")}
		},
		"pending-to-closed": func(t *testing.T, _ *Scoped) []dkg.WalletRecord {
			p := approved(walletFixture(t), "RegisteredPendingReady")
			return []dkg.WalletRecord{p, approved(walletFixture(t), "Closed")}
		},
		"quarantine-candidate": func(t *testing.T, _ *Scoped) []dkg.WalletRecord {
			return []dkg.WalletRecord{walletFixture(t), quarantine(walletFixture(t))}
		},
		"quarantine-pending": func(t *testing.T, _ *Scoped) []dkg.WalletRecord {
			p := approved(walletFixture(t), "RegisteredPendingReady")
			return []dkg.WalletRecord{p, quarantine(p)}
		},
		"quarantine-ready": func(t *testing.T, v *Scoped) []dkg.WalletRecord {
			r := readyFixture(t, v)
			r.State = "ReadyUnfunded"
			return []dkg.WalletRecord{r, quarantine(r)}
		},
		"quarantine-closed": func(t *testing.T, _ *Scoped) []dkg.WalletRecord {
			c := approved(walletFixture(t), "Closed")
			return []dkg.WalletRecord{c, quarantine(c)}
		},
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			records := steps(t, v)
			for i, w := range records {
				if err := v.SaveWallet(context.Background(), w); err != nil {
					t.Fatalf("monotone update %s refused at step %d: %v", name, i, err)
				}
			}
			loaded, err := v.LoadWallet(context.Background())
			must(t, err)
			if !reflect.DeepEqual(loaded, records[len(records)-1]) {
				t.Fatalf("monotone update %s did not store the last record", name)
			}
		})
	}
}

// Invariant (LoadWallet): a stored wallet record is returned only if its
// descriptor is valid and its hash, ID, epoch and registry match the record
// and the domain; otherwise the store quarantines. Each fixture record breaks
// one of these, with any changed descriptor carrying its recomputed hash.
// Rows wallet.go:116 (5 LO).
func TestLoadWalletRefusesEachInconsistentField(t *testing.T) {
	cases := map[string]func(*dkg.WalletRecord){
		"invalid-descriptor": func(w *dkg.WalletRecord) { w.Descriptor.Scheme = 3 },
		"descriptor-hash":    func(w *dkg.WalletRecord) { w.DescriptorHash[0] ^= 1 },
		"wallet-id":          func(w *dkg.WalletRecord) { w.ID[0] ^= 1 },
		"epoch":              func(w *dkg.WalletRecord) { w.Descriptor.Epoch++ },
		"registry":           func(w *dkg.WalletRecord) { w.Descriptor.Registry[0] ^= 1 },
	}
	rehash := map[string]bool{"invalid-descriptor": true, "epoch": true, "registry": true}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			w := walletFixture(t)
			change(&w)
			if rehash[name] {
				hash, err := dkg.DescriptorHash(w.Descriptor)
				must(t, err)
				w.DescriptorHash = hash
			}
			forgeWallet(t, s, v, w)
			if _, err := v.LoadWallet(context.Background()); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("LoadWallet returned a stored record with a wrong %s: %v", name, err)
			}
		})
	}
	t.Run("consistent", func(t *testing.T) {
		s, v := openTest(t, testConfig(t))
		w := walletFixture(t)
		forgeWallet(t, s, v, w)
		loaded, err := v.LoadWallet(context.Background())
		if err != nil || !reflect.DeepEqual(loaded, w) {
			t.Fatalf("positive control: a consistent fixture record did not load: %v", err)
		}
	})
}

// Invariant: a signing application record needs a non-zero ID and a payload
// of 1 to maxRecord bytes; a refused record is not retained. A retained ID
// is not acknowledged again from another domain. Rows signing.go:18 (2 LO,
// CB), :19, :38.
func TestRetainSigningRecordBounds(t *testing.T) {
	refused := map[string]struct {
		id      [32]byte
		payload []byte
	}{
		"id-zero":          {[32]byte{}, []byte("intent")},
		"payload-empty":    {[32]byte{93}, nil},
		"payload-over-max": {[32]byte{93}, make([]byte, maxRecord+1)},
	}
	for name, c := range refused {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			if err := v.RetainSigningRecord(context.Background(), c.id, c.payload); err == nil {
				t.Fatalf("RetainSigningRecord accepted a record with %s", name)
			}
			if _, err := v.ReadSigningRecord(context.Background(), c.id); !errors.Is(err, ErrMissing) {
				t.Fatalf("a refused signing record (%s) was retained: %v", name, err)
			}
		})
	}
	t.Run("payload-at-max", func(t *testing.T) {
		_, v := openTest(t, testConfig(t))
		if err := v.RetainSigningRecord(context.Background(), [32]byte{93}, make([]byte, maxRecord)); err != nil {
			t.Fatalf("a signing record of exactly maxRecord bytes was refused: %v", err)
		}
	})
	t.Run("other-domain", func(t *testing.T) {
		s, v := openTest(t, testConfig(t))
		id, payload := [32]byte{94}, []byte("public intent")
		must(t, v.RetainSigningRecord(context.Background(), id, payload))
		d := testDomain()
		d.Epoch++
		other, err := s.Scope(d)
		must(t, err)
		if err := other.RetainSigningRecord(context.Background(), id, payload); err == nil {
			t.Fatal("a signing record retained in one domain was acknowledged from another")
		}
	})
}
