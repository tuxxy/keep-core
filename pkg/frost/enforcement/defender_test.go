package enforcement

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
	"github.com/keep-network/keep-core/pkg/frost/signing"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

type memRecords struct {
	mu sync.Mutex
	m  map[[32]byte][]byte
}

func (r *memRecords) RetainSigningRecord(_ context.Context, id [32]byte, p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.m[id]; ok && string(old) != string(p) {
		return errors.New("conflicting signing application record")
	}
	r.m[id] = append([]byte(nil), p...)
	return nil
}
func (r *memRecords) ReadSigningRecord(_ context.Context, id [32]byte) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.m[id]
	if !ok {
		return nil, store.ErrMissing
	}
	return p, nil
}

type fakeChain struct {
	head       uint64
	now        uint64
	timeout    uint64
	logs       []ChallengeLog
	challenges map[[32]byte]Challenge
	proven     map[[32]byte][32]byte
	defenses   []Defense
	heartbeats []uint64
	timeouts   [][32]byte
	nonce      uint64
	claims     []Claim
	lostReply  bool
	submitErr  error
}

func (f *fakeChain) ChainID() uint64  { return 31337 }
func (f *fakeChain) Bridge() [20]byte { return [20]byte{0xb} }
func (f *fakeChain) Head(context.Context) (redemption.Head, error) {
	return redemption.Head{Number: f.head, Hash: [32]byte{byte(f.head)}}, nil
}
func (f *fakeChain) HeaderHash(_ context.Context, n uint64) ([32]byte, error) {
	return [32]byte{byte(n), 1}, nil
}
func (f *fakeChain) Timestamp(context.Context, uint64) (uint64, error) { return f.now, nil }
func (f *fakeChain) ChallengeLogs(_ context.Context, w [32]byte, from, to uint64) ([]ChallengeLog, error) {
	var out []ChallengeLog
	for _, l := range f.logs {
		if l.WalletID == w && l.Block >= from && l.Block <= to {
			out = append(out, l)
		}
	}
	return out, nil
}
func (f *fakeChain) Challenge(_ context.Context, w, d [32]byte) (Challenge, error) {
	return f.challenges[d], nil
}
func (f *fakeChain) FraudDefeatTimeout(context.Context) (uint64, error) { return f.timeout, nil }
func (f *fakeChain) ProvenTransaction(_ context.Context, _ uint64, h [32]byte) ([32]byte, error) {
	return f.proven[h], nil
}
func (f *fakeChain) resolve(d [32]byte) error {
	c := f.challenges[d]
	c.Resolved = true
	f.challenges[d] = c
	if f.lostReply {
		return errors.New("reply lost")
	}
	return nil
}
func (f *fakeChain) DefeatChallenge(_ context.Context, d Defense) error {
	f.defenses = append(f.defenses, d)
	if f.submitErr != nil && !f.lostReply {
		return f.submitErr
	}
	// The fake cannot rebuild the sighash; the tests defend digest 0x0d.
	return f.resolve([32]byte{0xd})
}
func (f *fakeChain) DefeatChallengeWithHeartbeat(_ context.Context, w [32]byte, nonce uint64) error {
	f.heartbeats = append(f.heartbeats, nonce)
	return f.resolve(signing.HeartbeatDigest(w, nonce))
}
func (f *fakeChain) NotifyChallengeTimeout(_ context.Context, w, d [32]byte) error {
	f.timeouts = append(f.timeouts, d)
	return f.resolve(d)
}
func (f *fakeChain) InactivityNonce(context.Context, [32]byte) (uint64, error) { return f.nonce, nil }
func (f *fakeChain) NotifyInactivity(_ context.Context, c Claim, seats []uint16, sigs []byte) error {
	f.claims = append(f.claims, c)
	f.nonce++
	if f.lostReply {
		return errors.New("reply lost")
	}
	return nil
}

var testWallet = [32]byte{0x77}

func challenge(f *fakeChain, digest [32]byte, block, reportedAt uint64) {
	if f.challenges == nil {
		f.challenges = map[[32]byte]Challenge{}
	}
	f.logs = append(f.logs, ChallengeLog{WalletID: testWallet, Digest: digest})
	f.logs[len(f.logs)-1].Block = block
	f.challenges[digest] = Challenge{WalletID: testWallet, Digest: digest, ReportedAt: reportedAt}
}

func retainDefense(t *testing.T, r *memRecords, rec signing.DefenseRecord) {
	t.Helper()
	raw, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RetainSigningRecord(context.Background(), signing.DefenseID(rec.WalletID, rec.Digest), raw); err != nil {
		t.Fatal(err)
	}
}

func sweepTx() (*bitcoin.Transaction, []bitcoin.PreviousOutput) {
	tx := &bitcoin.Transaction{Version: 2, Inputs: []*bitcoin.TransactionInput{{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{1}, OutputIndex: 0}, Sequence: 0xffffffff}}, Outputs: []*bitcoin.TransactionOutput{{Value: 900, PublicKeyScript: bitcoin.Script{0x51, 0x20}}}}
	return tx, []bitcoin.PreviousOutput{{Outpoint: *tx.Inputs[0].Outpoint, Value: 1000, PublicKeyScript: bitcoin.Script{0x51, 0x20}}}
}

// P-K08-DEFENSE-BEFORE-DEADLINE: the defender needs only the chain and the
// defense records, picks the C-08 route per purpose and settles lost replies.
func TestDefenderRoutes(t *testing.T) {
	ctx := context.Background()
	tx, prevouts := sweepTx()
	reservation := [32]byte{0x5}
	cases := []struct {
		name   string
		record signing.DefenseRecord
		proven bool
		check  func(*fakeChain) bool
	}{
		{"heartbeat", signing.DefenseRecord{Kind: signing.DefenseHeartbeat, Nonce: 40}, false, func(f *fakeChain) bool { return len(f.heartbeats) == 1 && f.heartbeats[0] == 40 }},
		{"payment intent", signing.DefenseRecord{Kind: signing.DefenseIntent, Ref: reservation, Transaction: tx, Prevouts: prevouts}, false, func(f *fakeChain) bool {
			return len(f.defenses) == 1 && f.defenses[0].Authorization == AuthIntent && f.defenses[0].Ref == reservation
		}},
		{"proven sweep", signing.DefenseRecord{Kind: signing.DefenseSweep, Transaction: tx, Prevouts: prevouts}, true, func(f *fakeChain) bool { return f.defenses[0].Authorization == AuthProven }},
		{"unmined sweep", signing.DefenseRecord{Kind: signing.DefenseSweep, Transaction: tx, Prevouts: prevouts}, false, func(f *fakeChain) bool { return f.defenses[0].Authorization == AuthSelf }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeChain{head: 100, proven: map[[32]byte][32]byte{}}
			r := &memRecords{m: map[[32]byte][]byte{}}
			rec := tc.record
			rec.Version, rec.WalletID = 1, testWallet
			rec.Digest = [32]byte{0xd}
			if rec.Kind == signing.DefenseHeartbeat {
				rec.Digest = signing.HeartbeatDigest(testWallet, rec.Nonce)
			}
			retainDefense(t, r, rec)
			if tc.proven {
				f.proven[[32]byte(tx.Hash())] = testWallet
			}
			challenge(f, rec.Digest, 50, 1000)
			challenge(f, [32]byte{0xee}, 51, 1000) // not this host's digest
			d := &Defender{Chain: f, Wallet: testWallet, Records: r}
			actions, err := d.Defend(ctx)
			if err != nil || !tc.check(f) || len(actions) != 2 || actions[0].Kind != ActionDefended || actions[1].Kind != ActionNotOurs {
				t.Fatalf("%v %v %+v", actions, err, f)
			}
			again, err := d.Defend(ctx)
			if err != nil || again[0].Kind != ActionResolved {
				t.Fatal("a resolved challenge was defended twice", again, err)
			}
		})
	}
	t.Run("a lost reply is settled by rereading", func(t *testing.T) {
		f := &fakeChain{head: 100, lostReply: true}
		r := &memRecords{m: map[[32]byte][]byte{}}
		retainDefense(t, r, signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: signing.HeartbeatDigest(testWallet, 40), Kind: signing.DefenseHeartbeat, Nonce: 40})
		challenge(f, signing.HeartbeatDigest(testWallet, 40), 50, 1000)
		if actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx); err != nil || actions[0].Kind != ActionDefended {
			t.Fatal(actions, err)
		}
	})
	t.Run("a failed defense does not block a later one", func(t *testing.T) {
		f := &fakeChain{head: 100, submitErr: errors.New("FROST defense self fee")}
		r := &memRecords{m: map[[32]byte][]byte{}}
		retainDefense(t, r, signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: [32]byte{0xd}, Kind: signing.DefenseSweep, Transaction: tx, Prevouts: prevouts})
		heartbeat := signing.HeartbeatDigest(testWallet, 40)
		retainDefense(t, r, signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: heartbeat, Kind: signing.DefenseHeartbeat, Nonce: 40})
		challenge(f, [32]byte{0xd}, 50, 1000)
		challenge(f, heartbeat, 51, 1000)
		actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx)
		if len(f.heartbeats) != 1 || !f.challenges[heartbeat].Resolved {
			t.Fatal("an earlier failed defense blocked a later valid one")
		}
		if err == nil || kindOf(actions, [32]byte{0xd}) != ActionFailed || kindOf(actions, heartbeat) != ActionDefended {
			t.Fatalf("the failure must be reported beside the defense: %+v %v", actions, err)
		}
	})
	t.Run("a failed defense is reported, not assumed", func(t *testing.T) {
		f := &fakeChain{head: 100, submitErr: errors.New("FROST defense self fee")}
		r := &memRecords{m: map[[32]byte][]byte{}}
		retainDefense(t, r, signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: [32]byte{0xd}, Kind: signing.DefenseSweep, Transaction: tx, Prevouts: prevouts})
		challenge(f, [32]byte{0xd}, 50, 1000)
		if _, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).Defend(ctx); err == nil {
			t.Fatal("refused defense reported as success")
		}
	})
}

// Reading 6: the host notifies a timeout only past the deadline and only for
// a digest it holds no defense record for.
func TestTimeoutNotifier(t *testing.T) {
	ctx := context.Background()
	f := &fakeChain{head: 100, now: 1000 + 600, timeout: 600}
	r := &memRecords{m: map[[32]byte][]byte{}}
	ours := signing.HeartbeatDigest(testWallet, 40)
	retainDefense(t, r, signing.DefenseRecord{Version: 1, WalletID: testWallet, Digest: ours, Kind: signing.DefenseHeartbeat, Nonce: 40})
	challenge(f, ours, 50, 1000)
	challenge(f, [32]byte{0xe1}, 51, 1000) // expired, unauthorized
	challenge(f, [32]byte{0xe2}, 52, 1001) // not yet expired
	actions, err := (&Defender{Chain: f, Wallet: testWallet, Records: r}).NotifyTimeouts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.timeouts {
		if d == ours {
			t.Fatal("the host timed out a digest it could defend")
		}
	}
	if len(f.timeouts) != 1 || f.timeouts[0] != [32]byte{0xe1} || len(actions) != 2 || actions[1].Kind != ActionPending {
		t.Fatalf("timeouts %+v %x", actions, f.timeouts)
	}
}


func kindOf(actions []Action, digest [32]byte) string {
	for _, a := range actions {
		if a.Digest == digest {
			return a.Kind
		}
	}
	return ""
}
