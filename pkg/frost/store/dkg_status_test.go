package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

func dkgFixture(t *testing.T, d frost.Domain) (frost.DKGRequest, frost.KeyReady) {
	t.Helper()
	attempt, err := d.NewAttempt("dkg", [32]byte{23}, d.Epoch+1)
	must(t, err)
	request := frost.DKGRequest{
		Group:      frost.Group{Roster: []uint16{1, 4, 7, 9, 12}, Threshold: 3, Epoch: d.Epoch},
		LocalSeats: []uint16{4, 12}, Attempt: attempt,
	}
	key := frost.KeyReady{Candidate: frost.Candidate{
		Epoch: d.Epoch, Roster: slices.Clone(request.Group.Roster), Threshold: 3,
		Profile: frost.ApprovedProfile, Descriptor: [32]byte{21}, OutputKey: [32]byte{22},
	}, LocalReferences: []frost.KeyReference{[]byte("opaque completed reference for seat 4"), []byte("opaque completed reference for seat 12")}}
	return request, key
}

func claimDKG(t *testing.T, v *Scoped, id [32]byte, request frost.DKGRequest) func() {
	t.Helper()
	intent, err := json.Marshal(request)
	must(t, err)
	release, err := v.Claim(context.Background(), id, "dkg", intent)
	must(t, err)
	t.Cleanup(release)
	return release
}

func requireDKGStatus(t *testing.T, v *Scoped, id [32]byte, state frost.DKGState) {
	t.Helper()
	status, err := v.DKGStatus(context.Background())
	must(t, err)
	want := frost.DKGStatus{Domain: v.Domain(), Attempt: id, State: state, LocalSeats: []uint16{4, 12}}
	if state == frost.DKGSeatsLost {
		want.LostSeats = []uint16{4, 12}
	}
	if !reflect.DeepEqual(status, want) {
		t.Fatalf("incorrect DKG status: got %+v, want %+v", status, want)
	}
}

func TestDKGLostSeatsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	config := testConfig(t)
	s, v := openTest(t, config)
	request, key := dkgFixture(t, v.Domain())
	id := [32]byte{31}
	if _, err := v.DKGStatus(ctx); !errors.Is(err, ErrMissing) {
		t.Fatalf("unclaimed domain reported a DKG: %v", err)
	}
	if err := v.SaveKey(ctx, key); !errors.Is(err, ErrDKGLost) {
		t.Fatalf("unclaimed domain installed a key: %v", err)
	}
	release := claimDKG(t, v, id, request)
	requireDKGStatus(t, v, id, frost.DKGInProgress)
	record := writeTest()
	_, err := v.Put(ctx, record)
	must(t, err)
	lock := frost.Write{Kind: "lock", Seat: 4, ID: [32]byte{18}, Payload: []byte("opaque lock"), Slot: &frost.LockSlot{Attempt: id, Kind: "ready"}}
	_, err = v.Put(ctx, lock)
	must(t, err)
	release()
	release() // release is idempotent and never deletes the claim
	requireDKGStatus(t, v, id, frost.DKGSeatsLost)
	if err = v.SaveKey(ctx, key); !errors.Is(err, ErrDKGLost) {
		t.Fatalf("released attempt installed a late key: %v", err)
	}
	must(t, s.Close())
	s, v = openTest(t, config)
	before := s.point
	requireDKGStatus(t, v, id, frost.DKGSeatsLost)
	status, err := v.DKGStatus(ctx)
	must(t, err)
	status.LocalSeats[0], status.LostSeats[0] = 99, 99
	requireDKGStatus(t, v, id, frost.DKGSeatsLost)
	if s.point != before {
		t.Fatal("reading a lost-seat marker changed durable state")
	}
	if err = v.SaveKey(ctx, key); !errors.Is(err, ErrDKGLost) {
		t.Fatalf("reopened attempt installed a late key: %v", err)
	}
	for _, w := range []frost.Write{record, lock} {
		got, err := v.Read(ctx, w.ID)
		must(t, err)
		if !slices.Equal(got, w.Payload) {
			t.Fatal("loss reporting changed an opaque record")
		}
	}
	conflicting := lock
	conflicting.ID = [32]byte{19}
	if result, err := v.Put(ctx, conflicting); err != nil || result != frost.Conflict {
		t.Fatalf("loss reporting removed a lock: %v %v", result, err)
	}
	if _, err = v.Claim(ctx, id, "dkg", []byte("reused")); !errors.Is(err, ErrClaimed) {
		t.Fatalf("terminal attempt was recycled: %v", err)
	}
	if _, err = v.Claim(ctx, [32]byte{32}, "dkg", []byte("same epoch")); !errors.Is(err, ErrDKGPending) {
		t.Fatalf("lost DKG domain was reused: %v", err)
	}
	freshDomain := v.Domain()
	freshDomain.Epoch++
	fresh, err := s.Scope(freshDomain)
	must(t, err)
	if _, err = fresh.DKGStatus(ctx); !errors.Is(err, ErrMissing) {
		t.Fatalf("loss leaked across epochs: %v", err)
	}
	request, key = dkgFixture(t, freshDomain)
	freshID := [32]byte{33}
	finish := claimDKG(t, fresh, freshID, request)
	must(t, fresh.SaveKey(ctx, key))
	finish()
	requireDKGStatus(t, fresh, freshID, frost.DKGKeyStored)
	must(t, s.Close())
	s, v = openTest(t, config)
	requireDKGStatus(t, v, id, frost.DKGSeatsLost)
	fresh, err = s.Scope(freshDomain)
	must(t, err)
	requireDKGStatus(t, fresh, freshID, frost.DKGKeyStored)
	must(t, fresh.SaveKey(ctx, key)) // identical saved key stays idempotent after restart
}

func TestDKGKeyMustMatchClaim(t *testing.T) {
	changes := map[string]func(*frost.KeyReady){
		"wrong roster":       func(k *frost.KeyReady) { k.Candidate.Roster[0] = 2 },
		"wrong threshold":    func(k *frost.KeyReady) { k.Candidate.Threshold++ },
		"wrong epoch":        func(k *frost.KeyReady) { k.Candidate.Epoch++ },
		"wrong profile":      func(k *frost.KeyReady) { k.Candidate.Profile = "unknown" },
		"missing local seat": func(k *frost.KeyReady) { k.LocalReferences = k.LocalReferences[:1] },
		"extra local seat":   func(k *frost.KeyReady) { k.LocalReferences = append(k.LocalReferences, []byte("extra")) },
		"empty reference":    func(k *frost.KeyReady) { k.LocalReferences[0] = nil },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			s, v := openTest(t, testConfig(t))
			request, bad := dkgFixture(t, v.Domain())
			release := claimDKG(t, v, [32]byte{31}, request)
			change(&bad)
			before := s.point
			if err := v.SaveKey(context.Background(), bad); err == nil || s.point != before {
				t.Fatal("mismatched key reached durable state")
			}
			requireDKGStatus(t, v, [32]byte{31}, frost.DKGInProgress)
			_, good := dkgFixture(t, v.Domain())
			must(t, v.SaveKey(context.Background(), good))
			release()
			requireDKGStatus(t, v, [32]byte{31}, frost.DKGKeyStored)
		})
	}
}

func TestDKGStatusRejectsInvalidDurableIntent(t *testing.T) {
	changes := map[string]func(*frost.DKGRequest){
		"wrong epoch":           func(r *frost.DKGRequest) { r.Group.Epoch++ },
		"wrong domain":          func(r *frost.DKGRequest) { r.Attempt.Channel[0] ^= 1 },
		"duplicate local seat":  func(r *frost.DKGRequest) { r.LocalSeats = []uint16{4, 4} },
		"foreign local seat":    func(r *frost.DKGRequest) { r.LocalSeats = []uint16{5} },
		"no local seats":        func(r *frost.DKGRequest) { r.LocalSeats = nil },
		"unordered roster":      func(r *frost.DKGRequest) { r.Group.Roster[0], r.Group.Roster[1] = 4, 1 },
		"nonmajority threshold": func(r *frost.DKGRequest) { r.Group.Threshold = 2 },
		"partial quorum":        func(r *frost.DKGRequest) { r.Group.Quorum = 4 },
		"partial admission":     func(r *frost.DKGRequest) { r.Participants = []uint16{1, 4, 7, 12} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			_, v := openTest(t, testConfig(t))
			request, _ := dkgFixture(t, v.Domain())
			change(&request)
			release := claimDKG(t, v, [32]byte{31}, request)
			release()
			status, err := v.DKGStatus(context.Background())
			if !errors.Is(err, ErrQuarantined) || status.State != "" {
				t.Fatalf("invalid intent produced a seat assertion: %+v %v", status, err)
			}
		})
	}
	t.Run("legacy opaque claim", func(t *testing.T) {
		_, v := openTest(t, testConfig(t))
		release, err := v.Claim(context.Background(), [32]byte{31}, "dkg", []byte("not a typed host DKG request"))
		must(t, err)
		release()
		if status, err := v.DKGStatus(context.Background()); !errors.Is(err, ErrQuarantined) || status.State != "" {
			t.Fatal("unreadable intent was treated as no lost seats")
		}
	})
}

func TestDKGStatusConcurrentRelease(t *testing.T) {
	_, v := openTest(t, testConfig(t))
	request, _ := dkgFixture(t, v.Domain())
	release := claimDKG(t, v, [32]byte{31}, request)
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 20; i++ {
				status, err := v.DKGStatus(context.Background())
				if err != nil || (status.State != frost.DKGInProgress && status.State != frost.DKGSeatsLost) {
					t.Errorf("inconsistent concurrent status: %+v %v", status, err)
					return
				}
			}
		}()
	}
	release()
	readers.Wait()
	requireDKGStatus(t, v, [32]byte{31}, frost.DKGSeatsLost)
}

func TestDKGKeyWriteUncertainty(t *testing.T) {
	for _, point := range []string{"before-fence", "after-fence"} {
		t.Run(point, func(t *testing.T) {
			config := testConfig(t)
			s, v := openTest(t, config)
			request, key := dkgFixture(t, v.Domain())
			release := claimDKG(t, v, [32]byte{31}, request)
			s.boundary = func(p string) error {
				if p == point {
					return errors.New("injected key-write uncertainty")
				}
				return nil
			}
			if err := v.SaveKey(context.Background(), key); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("uncertain key write succeeded: %v", err)
			}
			release()
			if status, err := v.DKGStatus(context.Background()); !errors.Is(err, ErrQuarantined) || status.State != "" {
				t.Fatal("uncertain handle asserted a seat outcome")
			}
			must(t, s.Close())
			_, v = openTest(t, config)
			state := frost.DKGSeatsLost
			if point == "after-fence" {
				state = frost.DKGKeyStored
			}
			requireDKGStatus(t, v, [32]byte{31}, state)
		})
	}
}

// Kill the host process with no deferred cleanup. The authority is on its own
// path and the production store makes actual file, link and directory calls.
// This checks local process death, not production storage power-loss behavior.
func TestDKGHostCrashReportsSeats(t *testing.T) {
	points := []string{"claim-only", "candidate-only", "before-write", "before-file-sync", "after-file-sync", "before-install", "after-install", "before-directory-sync", "after-directory-sync", "before-fence", "after-fence", "before-acknowledgement", "after-return"}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			config := testConfig(t)
			cmd := helper(t, config, "dkg-crash", point)
			cmd.Args[1] = "-test.run=^TestDKGCrashProcessHelper$"
			output, err := cmd.CombinedOutput()
			if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != -1 ||
				!strings.Contains(string(output), "DKG_HOST_KILL:"+point+"\n") {
				t.Fatalf("host was not killed at %s: %v %s", point, err, output)
			}
			_, v := openTest(t, config)
			state := frost.DKGSeatsLost
			if point == "after-fence" || point == "before-acknowledgement" || point == "after-return" {
				state = frost.DKGKeyStored
			}
			requireDKGStatus(t, v, [32]byte{31}, state)
			if state == frost.DKGSeatsLost {
				_, key := dkgFixture(t, v.Domain())
				if err := v.SaveKey(context.Background(), key); !errors.Is(err, ErrDKGLost) {
					t.Fatalf("crashed attempt accepted a key: %v", err)
				}
			}
			if point != "claim-only" {
				raw, err := v.Read(context.Background(), writeTest().ID)
				must(t, err)
				if !slices.Equal(raw, writeTest().Payload) {
					t.Fatal("crash changed candidate bytes")
				}
			}
		})
	}
}

func TestDKGCrashProcessHelper(t *testing.T) {
	if os.Getenv("FROST_STORE_HELPER") != "dkg-crash" {
		return
	}
	kill := func() {
		_, err := os.Stderr.WriteString("DKG_HOST_KILL:" + os.Getenv("FROST_STORE_BOUNDARY") + "\n")
		must(t, err)
		process, err := os.FindProcess(os.Getpid())
		must(t, err)
		must(t, process.Kill())
		select {} // cannot reach normal teardown after the kill signal
	}
	config := configAt(os.Getenv("FROST_STORE_ROOT"), os.Getenv("FROST_STORE_FENCE"))
	s, v := openTest(t, config)
	request, key := dkgFixture(t, v.Domain())
	_ = claimDKG(t, v, [32]byte{31}, request)
	point := os.Getenv("FROST_STORE_BOUNDARY")
	if point == "claim-only" {
		kill()
	}
	_, err := v.Put(context.Background(), writeTest())
	must(t, err)
	if point == "candidate-only" {
		kill()
	}
	s.boundary = func(p string) error {
		if p == point {
			kill()
		}
		return nil
	}
	must(t, v.SaveKey(context.Background(), key))
	if point == "after-return" {
		kill()
	}
	t.Fatal("requested crash point was not reached")
}
