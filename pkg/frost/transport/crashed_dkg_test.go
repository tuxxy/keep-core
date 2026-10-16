package transport

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/keep-network/keep-core/internal/testutils"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
)

// Pause only after the real journal reports a durable candidate write. The test
// stores a digest of its opaque bytes and never decodes or logs the record.
type candidateWritePause struct {
	frost.Journal
	written       chan [2][32]byte
	continueWrite chan struct{}
	released      chan struct{}
	paused        bool
}

func (j *candidateWritePause) Put(ctx context.Context, w frost.Write) (frost.PutResult, error) {
	r, err := j.Journal.Put(ctx, w)
	if err == nil && w.Kind == "candidate-record" && !j.paused && (r == frost.Durable || r == frost.Identical) {
		j.paused = true
		j.written <- [2][32]byte{w.ID, sha256.Sum256(w.Payload)}
		select {
		case <-j.continueWrite:
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
	return r, err
}

func (j *candidateWritePause) Claim(ctx context.Context, id [32]byte, purpose string, intent []byte) (func(), error) {
	release, err := j.Journal.Claim(ctx, id, purpose, intent)
	if err != nil {
		return nil, err
	}
	return func() {
		release()
		close(j.released)
	}, nil
}

func TestCrashedDKGNeedsFreshEpoch(t *testing.T) {
	cfg := realWorkerConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	roster := []uint16{1, 2, 3}
	domain := frost.Domain{Network: "K02/crashed-dkg", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: 77}
	storeConfig := store.Config{Keystore: filepath.Join(t.TempDir(), "keystore"), StorageID: [32]byte{1}, Key: [32]byte{42}, Fence: &memoryAuthority{}}
	open := func() *store.Store {
		t.Helper()
		root, err := store.Open(ctx, storeConfig)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := root.Close(); err != nil {
				t.Error(err)
			}
		})
		return root
	}
	root := open()
	scope := func(d frost.Domain) *store.Scoped {
		t.Helper()
		j, err := root.Scope(d)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	journal := scope(domain)
	_, public, err := operator.GenerateKeyPair(local_v1.DefaultCurve)
	if err != nil {
		t.Fatal(err)
	}
	signing := local_v1.Connect(3, 3).Signing()
	address, err := signing.PublicKeyToAddress(public)
	if err != nil {
		t.Fatal(err)
	}
	membership := group.NewMembershipValidator(&testutils.MockLogger{}, []chain.Address{address, address, address}, signing)
	// One node owns all three seats. The existing test-only authenticated channel
	// is sufficient here; the companion test covers real local libp2p peers.
	network := testProvider{hub: &testHub{names: map[string]bool{}}, key: operator.MarshalUncompressed(public)}
	transportFor := func(d frost.Domain, purpose string, session byte) (*Transport, frost.Attempt) {
		t.Helper()
		a, err := d.NewAttempt(purpose, [32]byte{session}, d.Epoch+uint64(session))
		if err != nil {
			t.Fatal(err)
		}
		tr, err := New(ctx, Config{Domain: d, Attempt: a, ID: snowfallengine.AttemptID(a), LocalSeats: roster, Roster: roster, LocalPublicKey: network.key, Membership: membership, Network: network})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tr.Close)
		return tr, a
	}
	pause := &candidateWritePause{Journal: journal, written: make(chan [2][32]byte, 1), continueWrite: make(chan struct{}), released: make(chan struct{})}
	engine, err := snowfallengine.NewGuarded(cfg, pause)
	if err != nil {
		t.Fatal(err)
	}
	tr, attempt := transportFor(domain, "dkg", 1)
	type result struct {
		key frost.KeyReady
		err error
	}
	done := make(chan result, 1)
	go func() {
		key, err := engine.DKG(ctx, frost.DKGRequest{Group: frost.Group{Roster: roster, Threshold: 2, Epoch: domain.Epoch}, LocalSeats: roster, Attempt: attempt}, frost.Providers{Transport: tr, Acceptance: testFinality{}})
		done <- result{key, err}
	}()
	var candidate [2][32]byte
	select {
	case candidate = <-pause.written:
	case r := <-done:
		t.Fatalf("DKG ended before a durable candidate: %v", r.err)
	case <-ctx.Done():
		t.Fatal("no durable candidate")
	}
	if err := root.Close(); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("live DKG lost its exclusive claim: %v", err)
	}
	killOwnedWorker(t, cfg.Path)
	close(pause.continueWrite)
	select {
	case r := <-done:
		if r.err == nil || len(r.key.LocalReferences) != 0 {
			t.Fatal("crashed DKG returned usable seats")
		}
	case <-ctx.Done():
		t.Fatal("killed DKG did not end")
	}
	select {
	case <-pause.released:
	default:
		t.Fatal("crashed DKG retained live ownership")
	}
	requireNoWorkers(t, cfg.Path)
	tr.Close()
	if _, err := journal.LoadKey(ctx); !errors.Is(err, store.ErrMissing) {
		t.Fatalf("crashed DKG installed a key: %v", err)
	}
	requireLostSeats := func() {
		t.Helper()
		status, err := journal.DKGStatus(ctx)
		if err != nil || status.Domain != domain || status.Attempt != snowfallengine.AttemptID(attempt) ||
			status.State != frost.DKGSeatsLost || !reflect.DeepEqual(status.LocalSeats, roster) || !reflect.DeepEqual(status.LostSeats, roster) {
			t.Fatalf("crashed worker did not report all local seats lost: %+v %v", status, err)
		}
	}
	requireLostSeats()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	root = open()
	journal = scope(domain)
	requireLostSeats()
	if release, err := journal.Claim(ctx, snowfallengine.AttemptID(attempt), "dkg", []byte("original attempt")); !errors.Is(err, store.ErrClaimed) {
		if release != nil {
			release()
		}
		t.Fatalf("original attempt tombstone was lost: %v", err)
	}
	retry, err := domain.NewAttempt("dkg", [32]byte{2}, domain.Epoch+2)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := journal.Claim(ctx, snowfallengine.AttemptID(retry), "dkg", []byte("same epoch retry")); !errors.Is(err, store.ErrDKGPending) {
		if release != nil {
			release()
		}
		t.Fatalf("same domain allowed another DKG: %v", err)
	}
	retained, err := journal.Read(ctx, candidate[0])
	if err != nil || sha256.Sum256(retained) != candidate[1] {
		t.Fatal("durable candidate changed after crash and release")
	}

	domain.Epoch++
	freshJournal := scope(domain)
	engine, err = snowfallengine.NewGuarded(cfg, freshJournal)
	if err != nil {
		t.Fatal(err)
	}
	tr, attempt = transportFor(domain, "dkg", 3)
	groupConfig := frost.Group{Roster: roster, Threshold: 2, Epoch: domain.Epoch}
	key, err := engine.DKG(ctx, frost.DKGRequest{Group: groupConfig, LocalSeats: roster, Attempt: attempt}, frost.Providers{Transport: tr, Acceptance: testFinality{}})
	if err != nil {
		t.Fatal(err)
	}
	if key.Candidate.Epoch != domain.Epoch || len(key.LocalReferences) != len(roster) {
		t.Fatal("fresh epoch did not complete every local seat")
	}
	tr.Close()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	root = open()
	freshJournal = scope(domain)
	loaded, err := freshJournal.LoadKey(ctx)
	if err != nil || !reflect.DeepEqual(loaded, key) {
		t.Fatal("fresh epoch KeyReady did not survive reopen")
	}
	counted := &countedJournal{Journal: freshJournal}
	engine, err = snowfallengine.NewGuarded(cfg, counted)
	if err != nil {
		t.Fatal(err)
	}
	tr, attempt = transportFor(domain, "sign", 4)
	message := sha256.Sum256([]byte("K02 fresh epoch after durable candidate crash"))
	signature, err := engine.Sign(ctx, frost.SigningRequest{Group: groupConfig, LocalSeats: roster, Selected: roster, Attempt: attempt, Key: loaded, Message: message}, frost.Providers{Transport: tr, Acceptance: testFinality{}})
	if err != nil {
		t.Fatal(err)
	}
	if counted.reads.Load() != int32(2*len(roster)) {
		t.Fatal("fresh signing worker did not reload every completed key")
	}
	pub, err := schnorr.ParsePubKey(key.Candidate.OutputKey[:])
	if err != nil {
		t.Fatal(err)
	}
	sig, err := schnorr.ParseSignature(signature[:])
	if err != nil || !sig.Verify(message[:], pub) {
		t.Fatal("independent BIP340 verification failed")
	}
	wrong := message
	wrong[0] ^= 1
	if sig.Verify(wrong[:], pub) {
		t.Fatal("signature accepted a changed message")
	}
	requireNoWorkers(t, cfg.Path)
	t.Logf("PUBLIC WITNESS: lost_epoch=77 fresh_epoch=%d lost_local_seats=%v tombstone_retained=true worker_sha256=%x descriptor=%x output_key=%x message=%x signature=%x", domain.Epoch, roster, cfg.SHA256, key.Candidate.Descriptor, key.Candidate.OutputKey, message, signature)
}
