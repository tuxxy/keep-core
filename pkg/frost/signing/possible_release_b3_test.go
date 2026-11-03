package signing

// Remediation batch 3 acceptance tests for tuxxy/keep-core#50 (K09-34).
//
// Invariant: once any input of an attempt has passed the executor's grant
// (BeforeSigning: the engine's durable claim, then the grant, then the worker
// may release this operator's share), the attempt is a possible release until
// chain evidence settles it. That holds whether or not this operator ever
// wrote its done record, with or without a main, and in a fresh process.
// KC-SF-V3 line 258: "A failed operator's unsupported assertion that no
// signature escaped is not evidence."
//
// The after-restart cases check in a fresh process (the test binary
// re-executed), so only durable state counts. Every subtest has its own plan.
//
// These tests use no build tag, so a default `go test ./pkg/frost/signing`
// runs them (tuxxy/keep-core#64, K09-49).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

// releaseJournal is the real encrypted store for every signing, claim and
// record call. FIXTURE: the wallet record, installed key and local seats come
// from journalStub (no DKG ran). Embedding *store.Scoped keeps every store
// method reachable, so a fix may read any durable record the store holds.
type releaseJournal struct {
	*store.Scoped
	stub *journalStub
}

func (j *releaseJournal) LoadWallet(ctx context.Context) (dkg.WalletRecord, error) {
	return j.stub.LoadWallet(ctx)
}
func (j *releaseJournal) LoadKey(ctx context.Context) (frost.KeyReady, error) {
	return j.stub.LoadKey(ctx)
}
func (j *releaseJournal) DKGStatus(ctx context.Context) (frost.DKGStatus, error) {
	return j.stub.DKGStatus(ctx)
}

// releasingEngine replaces only the worker and its peers (ADAPTER). It makes
// the real durable claim in the store, then calls the executor's real grant
// path, after which a real worker may send this operator's share. Then it
// fails the way an engine does when a peer aggregates but this operator's own
// completion never returns (peer timeout, local Wait error, or a kill before
// the done record).
type releasingEngine struct {
	frost.Engine
	journal *releaseJournal
	granted int
}

var errShareOutNoCompletion = errors.New("FIXTURE: share released, local completion never returned")

func (e *releasingEngine) Sign(ctx context.Context, r frost.SigningRequest, p frost.Providers) ([64]byte, error) {
	raw, _ := json.Marshal(r)
	release, err := e.journal.Claim(ctx, snowfallengine.AttemptID(r.Attempt), "sign", raw)
	if err != nil {
		return [64]byte{}, err
	}
	defer release()
	if err = p.SigningAuthorization.BeforeSigning(ctx, r); err != nil {
		return [64]byte{}, err
	}
	e.granted++
	return [64]byte{}, errShareOutNoCompletion
}

// incomingReleaseFixture is a target's incoming sweep (two incoming outputs,
// with or without the target's main) from one funding transaction.
func incomingReleaseFixture(t *testing.T, withMain bool, tag byte) (*Plan, dkg.Descriptor, *bitcoin.Transaction, *btcec.PrivateKey) {
	t.Helper()
	p, private, _ := planFixture(t)
	script, _ := bitcoin.PayToTaproot(p.Wallet.OutputKey)
	funding := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{47, tag}}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 50_000, PublicKeyScript: script}, {Value: 99_000, PublicKeyScript: script}, {Value: 99_000, PublicKeyScript: script}}}
	prev := func(i uint32) bitcoin.PreviousOutput {
		return bitcoin.PreviousOutput{Outpoint: bitcoin.TransactionOutpoint{TransactionHash: funding.Hash(), OutputIndex: i}, Value: funding.Outputs[i].Value, PublicKeyScript: script}
	}
	proposal := IncomingSweepProposal{Wallet: p.Wallet, BitcoinGenesis: p.BitcoinGenesis, Generation: 0, StateVersion: 1, Incoming: []bitcoin.PreviousOutput{prev(1), prev(2)}, MaximumFee: 1_000, MaximumSatPerVByte: 50}
	all, total := proposal.Incoming, int64(198_000)
	if withMain {
		main := prev(0)
		proposal.Main = &main
		all = append([]bitcoin.PreviousOutput{main}, all...)
		total += 50_000
	}
	tx := &bitcoin.Transaction{Version: 2}
	for _, in := range all {
		o := in.Outpoint
		tx.Inputs = append(tx.Inputs, &bitcoin.TransactionInput{Outpoint: &o, Sequence: 0xffffffff})
	}
	tx.Outputs = []*bitcoin.TransactionOutput{{Value: total - 500, PublicKeyScript: script}}
	proposal.Transaction = tx
	plan, err := NewIncomingSweepPlan(proposal)
	if err != nil {
		t.Fatal(err)
	}
	return plan, p.Wallet, funding, private
}

type releaseWorld struct {
	executor  *Executor
	engine    *releasingEngine
	authority *sweepAuthorityStub
	journal   *releaseJournal
	store     *store.Store
}

// fileFence is a TEST ONLY fence whose point lives in a file next to the
// keystore, so a child process reopening the store sees the same point.
type fileFence struct{ path string }

func (f fileFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) { return f, nil }
func (f fileFence) Head(context.Context) (store.Point, error) {
	var p store.Point
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	return p, err
}
func (f fileFence) Advance(ctx context.Context, before, after store.Point) error {
	current, err := f.Head(ctx)
	if err != nil {
		return err
	}
	if current != before {
		return store.ErrQuarantined
	}
	raw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, raw, 0600)
}
func (f fileFence) Close() error { return nil }

// newReleaseWorld builds an executor for a distinct incoming plan (tag) on the
// real store in dir (created if empty).
func newReleaseWorld(t *testing.T, withMain bool, tag byte, dir string) *releaseWorld {
	t.Helper()
	plan, desc, funding, _ := incomingReleaseFixture(t, withMain, tag)
	domain := frost.Domain{Network: "local", Chain: [32]byte{31: 1}, Registry: [20]byte(desc.Registry), Epoch: desc.Epoch}
	key := frost.KeyReady{Candidate: frost.Candidate{Epoch: desc.Epoch, Descriptor: desc.SnowfallDescriptor, OutputKey: desc.OutputKey, Threshold: 2, Roster: []uint16{1, 2, 3}, Profile: frost.ApprovedProfile}, LocalReferences: []frost.KeyReference{make([]byte, 32)}}
	stub := &journalStub{domain: domain, key: key, walletRecord: dkg.WalletRecord{Descriptor: desc, ID: plan.WalletID(), DescriptorHash: plan.DescriptorHash(), State: "ReadyUnfunded"}, records: map[[32]byte][]byte{}}
	c := store.Config{Keystore: filepath.Join(dir, "keystore"), StorageID: [32]byte{1}, Key: [32]byte{2}, Fence: fileFence{filepath.Join(dir, "fence.json")}}
	s, err := store.Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	scoped, err := s.Scope(domain)
	if err != nil {
		t.Fatal(err)
	}
	j := &releaseJournal{Scoped: scoped, stub: stub}
	authority := &sweepAuthorityStub{receipt: ReservationReceipt{Block: 20, Hash: [32]byte{3}}}
	btc := &bitcoinStub{raw: funding.Serialize(), genesis: plan.Genesis()}
	executor, err := NewLocalExecutor(Config{Plan: plan, Reservation: [32]byte{0x7e}, Session: [32]byte{9}, StartBlock: 23, FinalityBlocks: 2, MinimumConfirmations: 1, Journal: j, Sweeps: authority, Bitcoin: btc, Worker: snowfallengine.WorkerConfig{Path: "/unused-test-worker", SHA256: [32]byte{1}}, Selected: []uint16{1, 2}, Transport: func(context.Context, frost.Attempt) (frost.Transport, func(), error) {
		return transportStub{}, func() {}, nil
	}, LocalRegtest: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := &releasingEngine{journal: j}
	executor.engine = engine
	return &releaseWorld{executor: executor, engine: engine, authority: authority, journal: j, store: s}
}

// The child process of the after-restart cases: it opens the store the parent
// left, builds the executor from it alone and reports PossibleRelease.
const releaseChildEnv = "B3_RELEASE_CHILD"

func TestPossibleReleaseHelper(t *testing.T) {
	dir := os.Getenv(releaseChildEnv)
	if dir == "" {
		t.Skip("child process of TestPossibleReleaseAfterGrantWithoutDone only")
	}
	tag, _ := strconv.Atoi(os.Getenv("B3_RELEASE_TAG"))
	w := newReleaseWorld(t, os.Getenv("B3_RELEASE_MAIN") == "1", byte(tag), dir)
	released, err := w.executor.PossibleRelease(context.Background())
	fmt.Printf("B3_RELEASE_RESULT released=%v err=%v\n", released, err)
}

// releasedInFreshProcess closes the store and asks a re-executed test binary,
// a new process with no memory of this one, whether the attempt is a
// possible release.
func (w *releaseWorld) releasedInFreshProcess(t *testing.T, withMain bool, tag byte, dir string) bool {
	t.Helper()
	if err := w.store.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPossibleReleaseHelper$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), releaseChildEnv+"="+dir, "B3_RELEASE_TAG="+strconv.Itoa(int(tag)), "B3_RELEASE_MAIN="+map[bool]string{false: "0", true: "1"}[withMain])
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restarted process: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "B3_RELEASE_RESULT ") {
			if !strings.Contains(line, "err=<nil>") {
				t.Fatalf("restarted process: %s", line)
			}
			return strings.Contains(line, "released=true")
		}
	}
	t.Fatalf("restarted process reported nothing:\n%s", out)
	return false
}

// TestPossibleReleaseAfterGrantWithoutDone (tuxxy/keep-core#50, K09-34): an
// attempt that passed the grant, with no done record, is a possible release.
func TestPossibleReleaseAfterGrantWithoutDone(t *testing.T) {
	for _, tc := range []struct {
		name              string
		withMain, restart bool
		tag               byte
	}{
		{"mainless", false, false, 1},
		{"with-main", true, false, 2},
		{"mainless-after-restart", false, true, 3},
		{"with-main-after-restart", true, true, 4},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			w := newReleaseWorld(t, tc.withMain, tc.tag, dir)
			if _, err := w.executor.Run(context.Background()); !errors.Is(err, errShareOutNoCompletion) || w.engine.granted != 1 {
				t.Fatalf("precondition: the attempt must pass the grant and then fail without completion (granted %d, err %v)", w.engine.granted, err)
			}
			if _, err := w.journal.ReadSigningRecord(context.Background(), doneID(w.executor)); !errors.Is(err, store.ErrMissing) {
				t.Fatalf("precondition: no done record may exist (%v)", err)
			}
			var released bool
			if tc.restart {
				// A kill: only what the store holds reaches the new process.
				released = w.releasedInFreshProcess(t, tc.withMain, tc.tag, dir)
			} else {
				var err error
				if released, err = w.executor.PossibleRelease(context.Background()); err != nil {
					t.Fatalf("PossibleRelease: %v", err)
				}
			}
			if !released {
				t.Fatal("possible release: the attempt passed the grant (its share may have been sent) and has no done record, but PossibleRelease reports false")
			}
		})
	}
}

// TestNoPossibleReleaseBeforeTheWorker (guard): an attempt refused before the
// worker was ever asked releases nothing, so it stays abandonable.
func TestNoPossibleReleaseBeforeTheWorker(t *testing.T) {
	for _, withMain := range []bool{false, true} {
		withMain := withMain
		t.Run(map[bool]string{false: "mainless", true: "with-main"}[withMain], func(t *testing.T) {
			t.Parallel()
			w := newReleaseWorld(t, withMain, map[bool]byte{false: 5, true: 6}[withMain], t.TempDir())
			w.authority.err = errors.New("incoming record changed")
			if _, err := w.executor.Run(context.Background()); !errors.Is(err, ErrUnauthorized) || w.engine.granted != 0 {
				t.Fatalf("precondition: the attempt must be refused before the worker (granted %d, err %v)", w.engine.granted, err)
			}
			released, err := w.executor.PossibleRelease(context.Background())
			if err != nil {
				t.Fatalf("PossibleRelease: %v", err)
			}
			if released {
				t.Fatal("no possible release: nothing reached the worker, but PossibleRelease reports true")
			}
		})
	}
}
