package replay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"github.com/keep-network/keep-core/pkg/frost/store"
)

type abstract struct {
	PreTick         uint8    `json:"pre_tick"`
	ResultClosed    bool     `json:"result_closed"`
	OldResultClosed bool     `json:"old_result_closed"`
	WorkerReady     uint8    `json:"worker_ready"`
	Collect         [2]uint8 `json:"collect"`
	Watched         uint8    `json:"watched"`
	KeyDesc         [2]uint8 `json:"key_desc"`
	WalletDesc      [2]uint8 `json:"wallet_desc"`
	WalletState     [2]uint8 `json:"wallet_state"`
	RecognizedDesc  [2]uint8 `json:"recognized_desc"`
	RecognizedView  [2]uint8 `json:"recognized_view"`
	RecognizedTick  [2]uint8 `json:"recognized_tick"`
	Receipt         uint8    `json:"receipt"`
	CertDesc        uint8    `json:"cert_desc"`
	CertSeats       uint8    `json:"cert_seats"`
	CertCount       uint8    `json:"cert_count"`
	CertTick        uint8    `json:"cert_tick"`
	Deadline        uint8    `json:"deadline"`
	Anchor          uint8    `json:"anchor"`
	Inconsistency   uint8    `json:"inconsistency"`
	Quarantine      uint8    `json:"quarantine"`
	Termination     [2]uint8 `json:"termination"`
	Tombstones      [4]uint8 `json:"tombstones"`
	OldTombstones   [4]uint8 `json:"old_tombstones"`
	Funded          bool     `json:"funded"`
	Epoch           uint8    `json:"epoch"`
	Tick            uint8    `json:"tick"`
	Chain           uint8    `json:"chain"`
	Submitted       uint8    `json:"submitted"`
	Phase           [2]uint8 `json:"phase"`
	Seen            uint8    `json:"seen"`
	Key             uint8    `json:"key"`
	Reloaded        uint8    `json:"reloaded"`
	Attest          uint8    `json:"attest"`
	Ready           uint8    `json:"ready"`
	Recognized      uint8    `json:"recognized"`
	Certified       bool     `json:"certified"`
	Closed          bool     `json:"closed"`
	Old             bool     `json:"old"`
	Identities      [4]uint8 `json:"identities"`
	OldIDs          [4]uint8 `json:"old_ids"`
}
type step struct {
	N              int             `json:"n"`
	Action         json.RawMessage `json:"action"`
	Proc           *int            `json:"proc"`
	Pause          *string         `json:"pause"`
	Before         *string         `json:"pause_before"`
	Expect         string          `json:"expect"`
	State          abstract        `json:"expect_state"`
	Claim          abstract        `json:"claim_state"`
	ClaimAvailable bool            `json:"claim_available"`
	Diverges       bool            `json:"diverges"`
}
type trace struct {
	ID, Kind, Property, Replay string
	Weakening                  uint8
	Steps                      []step
}
type event struct {
	Kind, Boundary, Outcome string
	Key, Reloaded           bool
	Wallet                  dkg.WalletRecord
	Status                  frost.DKGStatus
	Detail                  string
}
type rpcRequest struct {
	Node, Epoch int
	Method      string
	Arg         json.RawMessage
}
type rpcResponse struct {
	Error string
	Value any
}

var nodeOutput sync.Mutex

func emitNode(e event) error {
	nodeOutput.Lock()
	defer nodeOutput.Unlock()
	return json.NewEncoder(os.Stdout).Encode(e)
}

type remote struct {
	url         string
	node, epoch int
}

func (r remote) call(ctx context.Context, m string, arg, out any) error {
	raw, _ := json.Marshal(arg)
	body, _ := json.Marshal(rpcRequest{Node: r.node, Epoch: r.epoch, Method: m, Arg: raw})
	req, err := http.NewRequestWithContext(ctx, "POST", r.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var response struct {
		Error string
		Value json.RawMessage
	}
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		if strings.HasPrefix(response.Error, "quarantined") {
			return fmt.Errorf("%w: %s", dkg.ErrQuarantined, response.Error)
		}
		if response.Error == "expired" {
			return dkg.ErrExpired
		}
		return errors.New(response.Error)
	}
	if out != nil {
		return json.Unmarshal(response.Value, out)
	}
	return nil
}
func (r remote) Parameters(ctx context.Context) (dkg.Parameters, error) {
	var p dkg.Parameters
	err := r.call(ctx, "parameters", nil, &p)
	return p, err
}
func (r remote) Selection(ctx context.Context, _ uint64) (dkg.Selection, error) {
	var s dkg.Selection
	err := r.call(ctx, "selection", nil, &s)
	return s, err
}
func (r remote) View(ctx context.Context, _ uint64) (dkg.View, error) {
	// Hold the monitor's RPC until the model schedules that observer. Cancellation
	// always releases it. This represents an outstanding RPC, not a stale answer.
	pc := make([]uintptr, 20)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	method := "view"
	for {
		f, more := frames.Next()
		if strings.Contains(f.Function, "reconcileWallet") || strings.Contains(f.Function, "rereadOverlappingSubmission") {
			method = "view"
			break
		}
		if strings.Contains(f.Function, "collect") {
			method = "collection-view"
			break
		}
		if strings.Contains(f.Function, "WaitCandidateAcceptance") {
			method = "acceptance-view"
		}
		if strings.Contains(f.Function, "inspectSubmission") {
			method = "monitor-view"
			break
		}
		if !more {
			break
		}
	}
	var v dkg.View
	err := r.call(ctx, method, nil, &v)
	if method == "collection-view" && err == nil {
		emitNode(event{Kind: "collection-read"})
	}
	return v, err
}
func (r remote) ResultDigest(ctx context.Context, _ uint64, result dkg.Result) ([32]byte, error) {
	var v [32]byte
	err := r.call(ctx, "result-digest", result, &v)
	return v, err
}
func (r remote) ReadinessDigest(ctx context.Context, w dkg.Wallet, seat uint16, ref [32]byte) ([32]byte, error) {
	var v [32]byte
	err := r.call(ctx, "ready-digest", struct {
		Wallet dkg.Wallet
		Seat   uint16
		Ref    [32]byte
	}{w, seat, ref}, &v)
	return v, err
}
func (r remote) Validate(ctx context.Context, _ uint64, result dkg.Result) (bool, error) {
	var v bool
	err := r.call(ctx, "validate", result, &v)
	return v, err
}
func (r remote) Submit(ctx context.Context, result dkg.Result) error {
	return r.call(ctx, "submit", result, nil)
}
func (r remote) Challenge(ctx context.Context) error { return r.call(ctx, "challenge", nil, nil) }
func (r remote) Approve(ctx context.Context) error   { return r.call(ctx, "approve", nil, nil) }
func (r remote) Ready(ctx context.Context, id [32]byte, seats []uint16, refs [][32]byte, sigs []byte) error {
	return r.call(ctx, "ready", struct {
		ID    [32]byte
		Seats []uint16
		Refs  [][32]byte
		Sigs  []byte
	}{id, seats, refs, sigs}, nil)
}
func (r remote) Expire(ctx context.Context, id [32]byte) error { return r.call(ctx, "expire", id, nil) }
func (r remote) ExpireDKG(ctx context.Context) error           { return r.call(ctx, "expire-dkg", nil, nil) }
func (r remote) Send(ctx context.Context, a dkg.Attestation) error {
	return r.call(ctx, "bus-send", a, nil)
}
func (r remote) Receive(ctx context.Context) (dkg.Attestation, error) {
	var a dkg.Attestation
	err := r.call(ctx, "bus-receive", nil, &a)
	return a, err
}
func (r remote) Acquire(ctx context.Context, _ [32]byte) (store.FenceLease, error) {
	if err := r.call(ctx, "fence-acquire", nil, nil); err != nil {
		return nil, err
	}
	return remoteLease{r}, nil
}

type remoteLease struct{ remote }

func (r remoteLease) Head(ctx context.Context) (store.Point, error) {
	var p store.Point
	err := r.call(ctx, "fence-head", nil, &p)
	return p, err
}
func (r remoteLease) Advance(ctx context.Context, before, after store.Point) error {
	return r.call(ctx, "fence-advance", [2]store.Point{before, after}, nil)
}
func (r remoteLease) Close() error { return r.call(context.Background(), "fence-release", nil, nil) }

type journalTap struct {
	saved bool
	dkg.Journal
	emit func(event) error
}

func (j *journalTap) SaveWallet(ctx context.Context, w dkg.WalletRecord) error {
	if err := j.Journal.SaveWallet(ctx, w); err != nil {
		return err
	}
	if w.State == "Candidate" && !w.Quarantined && !j.saved {
		j.saved = true
		return j.emit(event{Kind: "checkpoint", Boundary: "candidate-saved", Wallet: w})
	}
	return nil
}

type replayEngine struct {
	pause      func(string) error
	controller remote
	frost.Engine
	journal   dkg.Journal
	candidate frost.Candidate
	reloaded  bool
}

func (e *replayEngine) DKG(ctx context.Context, request frost.DKGRequest, p frost.Providers) (frost.KeyReady, error) {
	intent, _ := json.Marshal(request)
	release, err := e.journal.Claim(ctx, snowfallengine.AttemptID(request.Attempt), "dkg", intent)
	if err != nil {
		return frost.KeyReady{}, err
	}
	defer release()
	receipt, err := p.Acceptance.WaitCandidateAcceptance(ctx, e.candidate)
	if err != nil {
		return frost.KeyReady{}, err
	}
	if receipt.Epoch != e.candidate.Epoch || receipt.Descriptor != e.candidate.Descriptor {
		return frost.KeyReady{}, errors.New("wrong receipt")
	}
	if err = e.controller.call(ctx, "worker-ready", nil, nil); err != nil {
		return frost.KeyReady{}, err
	}
	if err = e.pause("before-key-ready-save"); err != nil {
		return frost.KeyReady{}, err
	}
	if err = e.controller.call(ctx, "worker-complete", nil, nil); err != nil {
		return frost.KeyReady{}, err
	}
	key := frost.KeyReady{Candidate: e.candidate}
	for _, seat := range request.LocalSeats {
		key.LocalReferences = append(key.LocalReferences, []byte{byte(request.Group.Epoch), byte(seat), 42})
	}
	// No extra approval read here. Real guarded engine also saves after receipt.
	if err = e.journal.SaveKey(ctx, key); err != nil {
		return frost.KeyReady{}, err
	}
	return key, nil
}
func (e *replayEngine) ReloadKey(ctx context.Context, r frost.SigningRequest, _ frost.Providers) error {
	k, err := e.journal.LoadKey(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(k, r.Key) || len(k.LocalReferences) != len(r.LocalSeats) {
		return errors.New("completed key reload mismatch")
	}
	// This adapter joins the fresh-worker guarantee established by the pinned
	// actual fixture; it does not claim synthetic reference bytes are SNOWFALL.
	e.reloaded = true
	return e.controller.call(ctx, "reloaded", nil, nil)
}
func signer(n int) *ecdsa.PrivateKey {
	raw := make([]byte, 32)
	raw[31] = byte(n + 1)
	key, _ := crypto.ToECDSA(raw)
	return key
}
func domain(epoch int) frost.Domain {
	return frost.Domain{Network: "V03/controlled", Chain: [32]byte{1}, Registry: [20]byte{2}, Epoch: uint64(epoch + 1)}
}
func candidate(epoch int) frost.Candidate {
	q := signer(epoch).PublicKey.X.FillBytes(make([]byte, 32))
	var output [32]byte
	copy(output[:], q)
	return frost.Candidate{Epoch: uint64(epoch + 1), Descriptor: [32]byte{byte(epoch + 1), 3}, OutputKey: output, Roster: []uint16{1, 2, 3, 4}, Threshold: 3, Profile: frost.ApprovedProfile}
}
func parameters() dkg.Parameters {
	return dkg.Parameters{ChainID: new(big.Int).Lsh(big.NewInt(1), 248), Registry: common.Address{2}, Pool: common.Address{3}, Bridge: common.Address{4}, GroupSize: 4, Threshold: 3, ReadySeats: 3, Profile: 1, ChallengeBlocks: 1}
}
func selection(epoch int) dkg.Selection {
	return dkg.Selection{Epoch: uint64(epoch + 1), Members: []uint32{1, 1, 2, 2}, Operators: []common.Address{crypto.PubkeyToAddress(signer(0).PublicKey), crypto.PubkeyToAddress(signer(0).PublicKey), crypto.PubkeyToAddress(signer(1).PublicKey), crypto.PubkeyToAddress(signer(1).PublicKey)}}
}
func config(root string, node int, f store.Fence) store.Config {
	return store.Config{Keystore: filepath.Join(root, fmt.Sprint(node)), StorageID: [32]byte{byte(node + 1)}, Key: [32]byte{byte(node + 1), 4}, Fence: f}
}

func TestV03Node(t *testing.T) {
	if os.Getenv("V03_NODE") == "" {
		t.Skip("subprocess helper")
	}
	var opts struct {
		URL, Root   string
		Node, Epoch int
	}
	if err := json.Unmarshal([]byte(os.Getenv("V03_NODE")), &opts); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := remote{opts.URL, opts.Node, opts.Epoch}
	root, err := store.Open(ctx, config(opts.Root, opts.Node, r))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := root.Scope(domain(opts.Epoch))
	if err != nil {
		t.Fatal(err)
	}
	c := candidate(opts.Epoch)
	attempt, err := domain(opts.Epoch).NewAttempt("dkg", [32]byte{byte(opts.Epoch + 1)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	local := []uint16{uint16(opts.Node*2 + 1), uint16(opts.Node*2 + 2)}
	engine := &replayEngine{journal: journal, candidate: c, controller: r}
	e := &dkg.Executor{Chain: r, Bus: r, Journal: journal, Engine: engine, Signer: signer(opts.Node), Request: frost.DKGRequest{Group: frost.Group{Epoch: uint64(opts.Epoch + 1), Roster: c.Roster, Threshold: 3, Quorum: 4}, LocalSeats: local, Participants: c.Roster, Attempt: attempt}, FinalityBlocks: 2, PollInterval: time.Millisecond}
	scanner := bufio.NewScanner(os.Stdin)
	tap := &journalTap{Journal: journal, emit: func(e event) error { return emitNode(e) }}
	e.Journal = tap
	engine.journal = tap
	e.Boundary = func(name string) error {
		if name == "after-monitor-observation-snapshot" {
			return nil
		}
		if name == "after-monitor-inspect" {
			return emitNode(event{Kind: "monitor-complete"})
		}
		k, ke := journal.LoadKey(ctx)
		_ = k
		w, _ := journal.LoadWallet(ctx)
		status, _ := journal.DKGStatus(ctx)
		if err := emitNode(event{Kind: "boundary", Boundary: name, Key: ke == nil, Reloaded: engine.reloaded, Wallet: w, Status: status}); err != nil {
			return err
		}
		line := make(chan string, 1)
		go func() {
			if scanner.Scan() {
				line <- scanner.Text()
			} else {
				line <- "closed"
			}
		}()
		var command string
		select {
		case command = <-line:
		case <-ctx.Done():
			return ctx.Err()
		}
		if command == "closed" {
			return errors.New("driver closed")
		}
		if command == "fail" {
			return context.Canceled
		}
		return nil
	}
	engine.pause = e.Boundary
	w, err := e.Run(ctx)
	outcome := "ready-unfunded"
	if errors.Is(err, dkg.ErrQuarantined) {
		outcome = "quarantined"
	} else if errors.Is(err, dkg.ErrExpired) {
		outcome = "expired"
	} else if err != nil {
		outcome = "error"
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	_ = root.Close()
	_ = emitNode(event{Kind: "result", Outcome: outcome, Wallet: w, Detail: detail})
	os.Exit(0)
}

type authority struct {
	point  store.Point
	leased bool
}
type world struct {
	resultSent        [2]uint8
	resultVotes       [2]chan struct{}
	resultClosed      bool
	workerMask        uint8
	workerComplete    chan struct{}
	delivery          [2]chan struct{}
	deliveryReady     [2]bool
	deliveryMask      [2]uint8
	history           map[[32]byte]dkg.Wallet
	poll              [2]chan bool
	pollFree          [2]bool
	reloadMask        uint8
	readyMask         uint8
	certMask          uint8
	certCount         uint8
	certTick          uint8
	acceptance        [2]chan struct{}
	visible           [2]bool
	mu                sync.Mutex
	epoch             int
	head              uint64
	chain             uint8
	submitted         *dkg.Result
	foreign           bool
	approved          bool
	certified, closed bool
	wallet            dkg.Wallet
	id                [32]byte
	hash              common.Hash
	result            dkg.Result
	resultReady       chan struct{}
	approvedReady     chan struct{}
	buses             [2]chan dkg.Attestation
	watches           [2]chan struct{}
	monitorHold       [2]bool
	monitorCaptured   [2]chan struct{}
	monitorRelease    [2]chan struct{}
	attest            [2]chan struct{}
	sent              [2]uint8
	fences            [2]authority
	root              string
}

func newWorld(root string) *world { w := &world{root: root}; w.reset(0); return w }
func (w *world) reset(epoch int) {
	if w.history == nil {
		w.history = make(map[[32]byte]dkg.Wallet)
	}
	w.workerMask = 0
	w.workerComplete = make(chan struct{})
	w.reloadMask = 0
	w.readyMask = 0
	w.certMask = 0
	w.certCount = 0
	w.certTick = 0
	w.pollFree = [2]bool{}
	w.deliveryReady = [2]bool{}
	w.deliveryMask = [2]uint8{}
	w.epoch = epoch
	w.head = 10
	w.chain = 0
	w.submitted = nil
	w.foreign = false
	w.resultClosed = false
	w.approved = false
	w.certified = false
	w.closed = false
	w.wallet = dkg.Wallet{}
	w.id = [32]byte{}
	w.hash = common.Hash{}
	w.resultReady = make(chan struct{})
	w.approvedReady = make(chan struct{})
	w.sent = [2]uint8{}
	w.resultSent = [2]uint8{}
	w.visible = [2]bool{}
	for n := 0; n < 2; n++ {
		w.resultVotes[n] = make(chan struct{})
		w.delivery[n] = make(chan struct{})
		w.poll[n] = make(chan bool, 8)
		w.acceptance[n] = make(chan struct{})
		w.buses[n] = make(chan dkg.Attestation, 16)
		w.watches[n] = make(chan struct{}, 8)
		w.monitorHold[n] = false
		w.monitorCaptured[n] = make(chan struct{}, 1)
		w.monitorRelease[n] = make(chan struct{}, 1)
		w.attest[n] = make(chan struct{})
	}
}
func (w *world) view() dkg.View {
	v := dkg.View{Head: w.head, Epoch: uint64(w.epoch + 1), State: 2, ResultDeadline: 13}
	if w.submitted != nil {
		v.State = 3
		v.Submitted = w.submitted
		v.SubmittedAt = 9
		v.SubmittedHash, _ = dkg.ResultHash(*w.submitted)
	}
	if w.chain == 1 || w.chain == 3 {
		v.State = 0
		v.ApprovedID = w.id
		v.ApprovalHash = w.hash
		v.Approved = w.wallet
		if w.chain == 3 {
			v.ApprovalHash[0] ^= 1
			v.Approved.Descriptor.SnowfallDescriptor[0] ^= 1
		}
	}
	if w.resultClosed {
		v.State = 0
		v.Submitted = nil
	}
	return v
}
func (w *world) serve(res http.ResponseWriter, req *http.Request) {
	var r rpcRequest
	if err := json.NewDecoder(req.Body).Decode(&r); err != nil {
		http.Error(res, err.Error(), 400)
		return
	}
	if r.Node < 0 || r.Node > 1 {
		http.Error(res, "node", 400)
		return
	}
	if r.Method == "bus-receive" {
		w.mu.Lock()
		queue := w.buses[r.Node]
		gate := w.delivery[r.Node]
		w.mu.Unlock()
		for {
			select {
			case a := <-queue:
				if a.Stage == "ready" {
					select {
					case <-gate:
					case <-req.Context().Done():
						return
					}
					w.mu.Lock()
					mask := w.deliveryMask[r.Node]
					w.mu.Unlock()
					if mask&(1<<uint(a.Seat-1)) == 0 {
						continue
					}
				}
				json.NewEncoder(res).Encode(rpcResponse{Value: a})
				return
			case <-req.Context().Done():
				return
			}
		}
	}
	if r.Method == "collection-view" {
		w.mu.Lock()
		captured := w.view()
		permit := w.poll[r.Node]
		free := w.pollFree[r.Node]
		w.mu.Unlock()
		if !free {
			select {
			case current := <-permit:
				if !current {
					json.NewEncoder(res).Encode(rpcResponse{Value: captured})
					return
				}
			case <-req.Context().Done():
				return
			}
		}
	}
	if r.Method == "acceptance-view" {
		w.mu.Lock()
		wait := w.approved && !w.visible[r.Node]
		permit := w.acceptance[r.Node]
		w.mu.Unlock()
		if wait {
			select {
			case <-permit:
			case <-req.Context().Done():
				return
			}
		}
	}
	if r.Method == "monitor-view" {
		w.mu.Lock()
		permit := w.watches[r.Node]
		w.mu.Unlock()
		select {
		case <-permit:
		case <-req.Context().Done():
			return
		}
	}
	if r.Method == "monitor-view" {
		w.mu.Lock()
		held := w.monitorHold[r.Node]
		captured := w.view()
		ack, release := w.monitorCaptured[r.Node], w.monitorRelease[r.Node]
		w.mu.Unlock()
		if held {
			ack <- struct{}{}
			select {
			case <-release:
			case <-req.Context().Done():
				return
			}
			json.NewEncoder(res).Encode(rpcResponse{Value: captured})
			return
		}
	}
	if r.Method == "worker-complete" {
		w.mu.Lock()
		complete := w.workerComplete
		w.mu.Unlock()
		select {
		case <-complete:
		case <-req.Context().Done():
			return
		}
	}
	if r.Method == "approve" {
		w.mu.Lock()
		approved := w.approvedReady
		w.mu.Unlock()
		select {
		case <-approved:
		case <-req.Context().Done():
			return
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var val any
	var err error
	switch r.Method {
	case "worker-ready":
		w.workerMask |= 3 << uint(r.Node*2)
		if w.workerMask == 15 {
			close(w.workerComplete)
		}
	case "worker-complete":
	case "reloaded":
		w.reloadMask |= 3 << uint(r.Node*2)
	case "parameters":
		val = parameters()
	case "selection":
		val = selection(r.Epoch)
	case "view", "monitor-view", "acceptance-view", "collection-view":
		val = w.view()
	case "result-digest":
		var result dkg.Result
		json.Unmarshal(r.Arg, &result)
		val = [32]byte(crypto.Keccak256Hash(result.GroupPubKey))
	case "ready-digest":
		var a struct {
			Wallet dkg.Wallet
			Seat   uint16
			Ref    [32]byte
		}
		json.Unmarshal(r.Arg, &a)
		val = readyDigest(a.Wallet, a.Seat, a.Ref)
	case "validate":
		val = !w.foreign
	case "submit":
		if w.chain != 0 || w.submitted != nil {
			err = errors.New("submission closed")
		} else {
			json.Unmarshal(r.Arg, &w.result)
			w.submitted = &w.result
			close(w.resultReady)
		}
	case "challenge":
		w.foreign = false
		w.submitted = nil
	case "approve":
		if !w.approved {
			err = errors.New("approval unavailable")
		}
	case "ready":
		var a struct {
			ID    [32]byte
			Seats []uint16
			Refs  [][32]byte
			Sigs  []byte
		}
		json.Unmarshal(r.Arg, &a)
		err = w.ready(a.ID, a.Seats, a.Refs, a.Sigs)
	case "expire":
		if w.chain != 1 || w.certified || w.head < w.wallet.Deadline {
			err = errors.New("expiry denied")
		} else {
			w.closed = true
			w.wallet.State = 3
			w.history[w.id] = w.wallet
		}
	case "expire-dkg":
		if w.approved || w.head < 13 {
			err = errors.New("result expiry denied")
		} else {
			w.resultClosed = true
			w.submitted = nil
		}
	case "bus-send":
		var a dkg.Attestation
		json.Unmarshal(r.Arg, &a)
		if int(a.Seat-1)/2 != r.Node {
			err = errors.New("wrong sender")
			break
		}
		if !dkg.SignedBy(a.Signature, a.Digest, selection(r.Epoch).Operators[a.Seat-1]) {
			err = errors.New("invalid signature")
			break
		}
		for n := 0; n < 2; n++ {
			if n != r.Node {
				w.buses[n] <- a
			}
		}
		if a.Stage == "result" {
			w.resultSent[r.Node]++
			if w.resultSent[r.Node] == 2 {
				close(w.resultVotes[r.Node])
			}
		}
		if a.Stage == "ready" {
			w.readyMask |= 1 << uint(a.Seat-1)
			w.sent[r.Node]++
			if w.sent[r.Node] == 2 {
				close(w.attest[r.Node])
			}
		}
	case "fence-acquire":
		if w.fences[r.Node].leased {
			err = store.ErrBusy
		} else {
			w.fences[r.Node].leased = true
		}
	case "fence-head":
		val = w.fences[r.Node].point
	case "fence-advance":
		var points [2]store.Point
		json.Unmarshal(r.Arg, &points)
		if w.fences[r.Node].point != points[0] {
			err = store.ErrQuarantined
		} else {
			w.fences[r.Node].point = points[1]
		}
	case "fence-release":
		w.fences[r.Node].leased = false
	default:
		err = fmt.Errorf("unknown RPC %s", r.Method)
	}
	response := rpcResponse{Value: val}
	if err != nil {
		response.Error = err.Error()
	}
	json.NewEncoder(res).Encode(response)
}
func readyDigest(w dkg.Wallet, seat uint16, ref [32]byte) [32]byte {
	return crypto.Keccak256Hash(w.DescriptorHash[:], []byte{byte(seat)}, ref[:])
}
func (w *world) ready(id [32]byte, seats []uint16, refs [][32]byte, sigs []byte) error {
	if w.chain != 1 || w.closed || w.certified || w.head >= w.wallet.Deadline || id != w.id {
		return errors.New("readiness closed")
	}
	if len(seats) < 3 || len(refs) != len(seats) || len(sigs) != 65*len(seats) {
		return errors.New("count")
	}
	var prev uint16
	for i, seat := range seats {
		if seat <= prev || seat > 4 || refs[i] == ([32]byte{}) {
			return errors.New("seat")
		}
		prev = seat
		if !dkg.SignedBy(sigs[i*65:(i+1)*65], readyDigest(w.wallet, seat, refs[i]), selection(w.epoch).Operators[seat-1]) {
			return errors.New("signature")
		}
	}
	w.certified = true
	w.certCount = uint8(len(seats))
	for _, seat := range seats {
		w.certMask |= 1 << uint(seat-1)
	}
	w.certTick = uint8(w.head - w.wallet.ApprovalBlock)
	w.wallet.State = 2
	w.history[w.id] = w.wallet
	return nil
}
func (w *world) approve(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	d, err := dkg.NewDescriptor(parameters(), selection(w.epoch), candidate(w.epoch))
	if err != nil {
		t.Fatal(err)
	}
	h, err := dkg.DescriptorHash(d)
	if err != nil {
		t.Fatal(err)
	}
	resultHash, err := dkg.ResultHash(w.result)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.admission(d); err != nil {
		t.Fatal(err)
	}
	w.wallet = dkg.Wallet{Descriptor: d, DescriptorHash: h, ResultHash: resultHash, ApprovalBlock: w.head, Deadline: w.head + 3, State: 1}
	w.id = dkg.WalletID(d.OutputKey)
	w.hash = common.Hash{byte(w.epoch + 1), 8}
	w.history[w.id] = w.wallet
	w.chain = 1
	w.approved = true
	w.submitted = nil
	close(w.approvedReady)
}

type child struct {
	outcome  string
	terminal *event
	observed []event
	pending  []event
	cmd      *exec.Cmd
	input    io.WriteCloser
	events   chan event
	stderr   bytes.Buffer
	last     string
	done     bool
}

func start(t *testing.T, url, root string, node, epoch int) *child {
	t.Helper()
	opts, _ := json.Marshal(struct {
		URL, Root   string
		Node, Epoch int
	}{url, root, node, epoch})
	c := &child{events: make(chan event, 32)}
	life, stop := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(stop)
	c.cmd = exec.CommandContext(life, os.Args[0], "-test.run=^TestV03Node$")
	c.cmd.Env = append(os.Environ(), "V03_NODE="+string(opts))
	var err error
	c.input, err = c.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.cmd.Stderr = &c.stderr
	if err = c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var e event
			if json.Unmarshal(scanner.Bytes(), &e) == nil && e.Kind != "" {
				c.events <- e
			}
		}
		close(c.events)
	}()
	t.Cleanup(func() {
		if !c.done {
			c.cmd.Process.Kill()
			c.cmd.Wait()
		}
	})
	return c
}
func (c *child) wait(t *testing.T, kind, name string) event {
	t.Helper()
	for i, e := range c.pending {
		if e.Kind == kind && (kind != "boundary" || e.Boundary == name) {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			if kind == "boundary" {
				c.last = name
			}
			c.outcome = "paused"
			if kind == "result" {
				c.outcome = e.Outcome
			}
			return e
		}
	}
	for {
		select {
		case e, ok := <-c.events:
			if !ok {
				t.Fatalf("child ended waiting %s/%s: %s", kind, name, c.stderr.String())
			}
			c.observed = append(c.observed, e)
			if kind == "result" && e.Kind == "checkpoint" {
				continue
			}
			if e.Kind == "monitor-complete" && kind != "monitor-complete" {
				continue
			}
			if e.Kind == "collection-read" && kind != "collection-read" {
				continue
			}
			if kind == "monitor-complete" && e.Kind == "boundary" {
				c.pending = append(c.pending, e)
				c.last = e.Boundary
				continue
			}
			if e.Kind != kind || (kind == "boundary" && e.Boundary != name) {
				t.Fatalf("want %s/%s got %+v", kind, name, e)
			}
			if kind == "boundary" {
				c.last = name
			}
			c.outcome = "paused"
			if kind == "result" {
				c.outcome = e.Outcome
			}
			return e
		case <-time.After(10 * time.Second):
			t.Fatalf("child timeout waiting %s/%s; last=%s stderr=%s", kind, name, c.last, c.stderr.String())
		}
		return event{}
	}
}
func (c *child) goOn(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(c.input, "go\n"); err != nil {
		t.Fatal(err)
	}
	for i := len(c.pending) - 1; i >= 0; i-- {
		if c.pending[i].Kind == "boundary" && c.pending[i].Boundary == c.last {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
		}
	}
	c.last = ""
}
func (c *child) finish(t *testing.T, want string) event {
	t.Helper()
	e := c.wait(t, "result", "")
	if e.Outcome != want {
		t.Fatalf("outcome got %+v want %s", e, want)
	}
	if err := c.cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	c.done = true
	c.terminal = &e
	return e
}
func (c *child) kill(t *testing.T) {
	t.Helper()
	if err := c.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.cmd.Wait()
	c.done = true
	c.outcome = "dead"
}
func parseAction(raw json.RawMessage) (string, int) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name, 0
	}
	var a map[string]json.RawMessage
	json.Unmarshal(raw, &a)
	for k, v := range a {
		var n int
		if json.Unmarshal(v, &n) == nil {
			return k, n
		}
		var args struct {
			Proc  int
			Seats uint8
		}
		if json.Unmarshal(v, &args) == nil {
			return k, args.Proc
		}
	}
	panic("action")
}

// Parent fence view used only after a child has exited/killed; same independent
// authority as the remote child's lease, preserving history across epochs.
type localFence struct {
	w *world
	n int
}
type localLease struct{ localFence }

func (f localFence) Acquire(context.Context, [32]byte) (store.FenceLease, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	if f.w.fences[f.n].leased {
		return nil, store.ErrBusy
	}
	f.w.fences[f.n].leased = true
	return localLease{f}, nil
}
func (f localLease) Head(context.Context) (store.Point, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	return f.w.fences[f.n].point, nil
}
func (f localLease) Advance(_ context.Context, b, a store.Point) error {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	if f.w.fences[f.n].point != b {
		return store.ErrQuarantined
	}
	f.w.fences[f.n].point = a
	return nil
}
func (f localLease) Close() error {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	f.w.fences[f.n].leased = false
	return nil
}
func inspectKilled(t *testing.T, w *world, n int, s abstract) {
	t.Helper()
	w.mu.Lock()
	w.fences[n].leased = false
	w.mu.Unlock()
	ctx := context.Background()
	root, err := store.Open(ctx, config(w.root, n, localFence{w, n}))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	j, err := root.Scope(domain(int(s.Epoch)))
	if err != nil {
		t.Fatal(err)
	}
	key, err := j.LoadKey(ctx)
	has := err == nil
	if err != nil && !errors.Is(err, store.ErrMissing) {
		t.Fatal(err)
	}
	if has != (s.Key&(1<<n) != 0) {
		t.Fatalf("durable key after kill=%t model mask=%d", has, s.Key)
	}
	if has {
		compareKey(t, key, s, n)
	}
	record, walletErr := j.LoadWallet(ctx)
	if walletErr != nil && !(s.WalletState[n] == 0 && errors.Is(walletErr, store.ErrMissing)) {
		t.Fatal(walletErr)
	}
	compareWallet(t, record, s, n)
	compareResultIdentity(t, w, record)
	status, err := j.DKGStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := frost.DKGSeatsLost
	if has {
		want = frost.DKGKeyStored
	}
	if status.State != want {
		t.Fatalf("status after kill=%s want %s", status.State, want)
	}
}

func checkBoundary(t *testing.T, e event, s abstract, n int) {
	t.Helper()
	if e.Key != (s.Key&(1<<n) != 0) {
		t.Fatalf("key at %s concrete=%t abstract=%d", e.Boundary, e.Key, s.Key)
	}
	if e.Reloaded != (s.Reloaded&(3<<(n*2)) != 0) {
		t.Fatalf("reload at %s concrete=%t abstract=%d", e.Boundary, e.Reloaded, s.Reloaded)
	}
	compareWallet(t, e.Wallet, s, n)
	if e.Wallet.ID != ([32]byte{}) {
		d, err := dkg.NewDescriptor(parameters(), selection(int(s.Epoch)), candidate(int(s.Epoch)))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(e.Wallet.Descriptor, d) {
			t.Fatal("wallet descriptor differs from exact candidate")
		}
	}
}
func checkWorld(t *testing.T, w *world, s abstract) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.chain != s.Chain || w.certified != s.Certified || w.closed != s.Closed || w.head != 10+uint64(s.Tick)+uint64(s.PreTick) {
		t.Fatalf("chain state concrete=%d/%t/%t/%d abstract=%d/%t/%t/%d", w.chain, w.certified, w.closed, w.head, s.Chain, s.Certified, s.Closed, s.Tick)
	}
	submitted := uint8(0)
	if w.submitted != nil {
		submitted = 1
		if w.foreign {
			submitted = 2
		}
	}
	if submitted != s.Submitted {
		t.Fatalf("submitted concrete=%d model=%d", submitted, s.Submitted)
	}
	if w.workerMask != s.WorkerReady {
		t.Fatalf("worker-ready concrete=%d model=%d", w.workerMask, s.WorkerReady)
	}
	if w.reloadMask != s.Reloaded || w.readyMask != s.Attest {
		t.Fatalf("seats reload/attest concrete=%d/%d model=%d/%d", w.reloadMask, w.readyMask, s.Reloaded, s.Attest)
	}
	if w.certified && (w.certMask != s.CertSeats || w.certCount != s.CertCount || w.certTick != s.CertTick) {
		t.Fatalf("accepted certificate concrete=%d/%d/%d model=%d/%d/%d", w.certMask, w.certCount, w.certTick, s.CertSeats, s.CertCount, s.CertTick)
	}
	if s.Tombstones[1] != 0 {
		if _, ok := w.history[dkg.WalletID(candidate(w.epoch).OutputKey)]; !ok {
			t.Fatal("current tombstone missing")
		}
	}
	if s.Old && s.OldTombstones[1] != 0 {
		if _, ok := w.history[dkg.WalletID(candidate(0).OutputKey)]; !ok {
			t.Fatal("old tombstone missing")
		}
	}
	if w.resultClosed != s.ResultClosed {
		t.Fatal("result closure mismatch")
	}
	if w.chain == 1 && w.id != dkg.WalletID(candidate(w.epoch).OutputKey) {
		t.Fatal("chain ID correspondence")
	}
}
func play(t *testing.T, tr trace) map[string]any {
	t.Helper()
	w := newWorld(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(w.serve))
	defer server.Close()
	var children [2]*child
	defer func() {
		for _, c := range children {
			if c != nil && !c.done {
				c.cmd.Process.Kill()
				c.cmd.Wait()
				c.done = true
			}
		}
	}()
	for n := 0; n < 2; n++ {
		children[n] = start(t, server.URL, w.root, n, 0)
		checkBoundary(t, children[n].wait(t, "boundary", "candidate-ready"), abstract{}, n)
	}
	previous := abstract{Identities: [4]uint8{1, 1, 1, 1}}
	executed := 0
	cut := false
	var outcomes []map[string]any
	hooks := map[string]int{}
	boundaryWait := func(n int, name string, s abstract) {
		e := children[n].wait(t, "boundary", name)
		hooks[name]++
		checkBoundary(t, e, s, n)
	}
	complete := func(n int, want string) {
		c := children[n]
		if c.last != "" {
			c.goOn(t)
		}
		c.finish(t, want)
	}
	for _, st := range tr.Steps {
		if tr.Weakening != 0 && !st.ClaimAvailable {
			cut = true
			break
		}
		s := st.State
		if tr.Weakening != 0 {
			s = st.Claim
		}
		action, n := parseAction(st.Action)
		t.Logf("step=%d action=%s node=%d phase=%v tick=%d", st.N, action, n, s.Phase, s.Tick)
		switch action {
		case "Candidate":
			children[n].goOn(t)
			children[n].wait(t, "checkpoint", "")
			w.mu.Lock()
			votes := w.resultVotes[n]
			w.mu.Unlock()
			select {
			case <-votes:
			case <-time.After(10 * time.Second):
				t.Fatal("result votes not sent")
			}
		case "BeforePublish":
			boundaryWait(0, "before-publish", s)
		case "Publish":
			children[0].goOn(t)
			boundaryWait(0, "after-publish", s)
		case "AfterPublish":
			children[0].goOn(t)
		case "ResultTick":
			w.mu.Lock()
			w.head++
			w.mu.Unlock()
		case "ExpireDKG":
			w.mu.Lock()
			w.resultClosed = true
			w.submitted = nil
			w.mu.Unlock()
		case "Approve":
			w.approve(t)
		case "Tick":
			w.mu.Lock()
			w.head++
			w.mu.Unlock()
		case "Foreign":
			w.mu.Lock()
			result := dkg.NewResult(mustDescriptor(t, 1-int(s.Epoch)))
			w.submitted = &result
			w.foreign = true
			w.mu.Unlock()
		case "Challenge":
			w.mu.Lock()
			w.submitted = nil
			w.foreign = false
			w.mu.Unlock()
		case "Remove":
			w.mu.Lock()
			w.chain = 2
			w.mu.Unlock()
		case "Replace":
			w.mu.Lock()
			w.chain = 3
			w.mu.Unlock()
		case "Observe":
			w.mu.Lock()
			if !w.visible[n] {
				w.visible[n] = true
				close(w.acceptance[n])
			}
			w.mu.Unlock()
			if s.Phase[n] == 10 {
				complete(n, "quarantined")
			} else {
				boundaryWait(n, "approval-seen", s)
			}
		case "BeforeFinality":
			children[n].goOn(t)
			boundaryWait(n, "before-finality-reread", s)
		case "Reread":
			children[n].goOn(t)
			if s.Phase[n] == 10 {
				children[n].finish(t, "quarantined")
			} else if s.Phase[n] == 13 {
				children[n].finish(t, "expired")
			} else {
				boundaryWait(n, "after-finality-reread", s)
			}
		case "WorkerReady":
			children[n].goOn(t)
			boundaryWait(n, "before-key-ready-save", s)
		case "Key":
			children[n].goOn(t)
			boundaryWait(n, "after-key-ready", s)
		case "PostKeyRead":
			children[n].goOn(t)
			if s.Phase[n] == 10 {
				children[n].finish(t, "quarantined")
			} else if s.Phase[n] == 13 {
				children[n].finish(t, "expired")
			} else {
				boundaryWait(n, "after-post-key-read", s)
			}
		case "Reload":
			children[n].goOn(t)
			boundaryWait(n, "after-key-reload", s)
		case "BeginReadiness":
			children[n].goOn(t)
			boundaryWait(n, "before-readiness-collect", s)
		case "CollectionRead":
			w.mu.Lock()
			w.poll[n] <- true
			w.mu.Unlock()
			children[n].wait(t, "collection-read", "")
			if s.Phase[n] == 10 {
				complete(n, "quarantined")
			} else if s.Phase[n] == 13 {
				complete(n, "expired")
			}
		case "Attest":
			children[n].goOn(t)
			w.mu.Lock()
			sent := w.attest[n]
			w.mu.Unlock()
			select {
			case <-sent:
			case <-time.After(10 * time.Second):
				t.Fatal("local seats not sent")
			}
		case "BeforeCertificate":
			w.mu.Lock()
			w.pollFree[n] = true
			w.deliveryMask[n] = s.Collect[n]
			if !w.deliveryReady[n] {
				w.deliveryReady[n] = true
				close(w.delivery[n])
			}
			w.poll[n] <- false
			w.mu.Unlock()
			boundaryWait(n, "before-certificate-submit", s)
		case "Certify":
			children[n].goOn(t)
			want := "ready-unfunded"
			if s.Phase[n] == 10 {
				want = "quarantined"
			} else if s.Phase[n] == 13 {
				want = "expired"
			}
			if want == "ready-unfunded" {
				boundaryWait(n, "after-certificate-submit", s)
			} else {
				children[n].finish(t, want)
			}
		case "Postcondition":
			children[n].goOn(t)
			if s.Phase[n] == 10 {
				children[n].finish(t, "quarantined")
			} else if s.Phase[n] == 13 {
				children[n].finish(t, "expired")
			} else {
				boundaryWait(n, "after-readiness-postcondition", s)
			}
		case "PersistReady":
			children[n].goOn(t)
			boundaryWait(n, "after-wallet-ready-save", s)
		case "Complete":
			children[n].goOn(t)
			children[n].finish(t, "ready-unfunded")
		case "BeginWatch":
			w.mu.Lock()
			w.monitorHold[n] = true
			permit, captured := w.watches[n], w.monitorCaptured[n]
			w.mu.Unlock()
			permit <- struct{}{}
			select {
			case <-captured:
			case <-time.After(5 * time.Second):
				t.Fatal("monitor snapshot capture timeout")
			}
		case "FinishWatch":
			w.mu.Lock()
			w.monitorHold[n] = false
			release := w.monitorRelease[n]
			w.mu.Unlock()
			release <- struct{}{}
			children[n].wait(t, "monitor-complete", "")
			if s.Phase[n] == 10 {
				complete(n, "quarantined")
			} else if s.Phase[n] == 13 {
				complete(n, "expired")
			}
		case "Watch":
			w.mu.Lock()
			permit := w.watches[n]
			w.mu.Unlock()
			permit <- struct{}{}
			children[n].wait(t, "monitor-complete", "")
			if s.Phase[n] == 10 {
				complete(n, "quarantined")
			} else if s.Phase[n] == 13 {
				complete(n, "expired")
			}
		case "Retry": // Real retry seam is a fixed wallet read; no deadline reset or resubmit.
			w.mu.Lock()
			v := w.view()
			w.mu.Unlock()
			if v.Approved.Deadline != 13+uint64(s.PreTick) {
				t.Fatal("retry changed anchor")
			}
		case "Expire":
			w.mu.Lock()
			if w.certified || w.head < w.wallet.Deadline {
				t.Fatal("invalid expiry action")
			}
			w.closed = true
			w.wallet.State = 3
			w.history[w.id] = w.wallet
			w.mu.Unlock()
		case "ProcessCrash", "HostCrash":
			if st.Before != nil && children[n].last == "" {
				boundaryWait(n, *st.Before, previous)
			}
			children[n].kill(t)
			inspectKilled(t, w, n, s)
		case "Fresh":
			for node, c := range children {
				if !c.done {
					c.kill(t)
				}
				w.mu.Lock()
				w.fences[node].leased = false
				w.mu.Unlock()
			}
			oldCandidate := candidate(int(previous.Epoch))
			newCandidate := candidate(int(s.Epoch))
			oldAttempt, _ := domain(int(previous.Epoch)).NewAttempt("dkg", [32]byte{1}, 1)
			newAttempt, _ := domain(int(s.Epoch)).NewAttempt("dkg", [32]byte{2}, 1)
			oldID, newID := dkg.WalletID(oldCandidate.OutputKey), dkg.WalletID(newCandidate.OutputKey)
			if oldCandidate.Descriptor == newCandidate.Descriptor || oldCandidate.OutputKey == newCandidate.OutputKey || oldID == newID || bytes.Equal(oldID[:20], newID[:20]) || bytes.Equal(oldAttempt.Channel, newAttempt.Channel) {
				t.Fatal("fresh identities not disjoint")
			}
			// Reopen old domain to compare retained identity even when no local node
			// observed the public close. Closed chain tombstone is retained in report.
			for node := 0; node < 2; node++ {
				root, err := store.Open(context.Background(), config(w.root, node, localFence{w, node}))
				if err != nil {
					t.Fatal(err)
				}
				j, err := root.Scope(domain(int(previous.Epoch)))
				if err != nil {
					t.Fatal(err)
				}
				record, err := j.LoadWallet(context.Background())
				if previous.WalletState[node] != 0 && err != nil {
					t.Fatal("required old wallet missing", err)
				}
				if err == nil {
					compareWallet(t, record, previous, node)
				}
				fresh, err := root.Scope(domain(int(s.Epoch)))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = fresh.Claim(context.Background(), snowfallengine.AttemptID(oldAttempt), "dkg", []byte{1}); !errors.Is(err, store.ErrClaimed) {
					t.Fatal("old attempt reused", err)
				}
				if err == nil && record.Descriptor.SnowfallDescriptor != oldCandidate.Descriptor {
					t.Fatal("old wallet relabeled")
				}

				root.Close()
			}
			w.mu.Lock()
			oldID = dkg.WalletID(oldCandidate.OutputKey)
			oldWallet, exists := w.history[oldID]
			if previous.Tombstones[1] != 0 && !exists {
				t.Fatal("closed/quarantined identity tombstone absent")
			}
			if previous.Closed && oldWallet.State != 3 {
				t.Fatal("old chain tombstone not closed")
			}
			// Admission tests the same outputKeyUsed-style guard as approve.
			reused := mustDescriptor(t, int(s.Epoch))
			reused.OutputKey = oldCandidate.OutputKey
			before := len(w.history)
			if err := w.admission(reused); previous.Tombstones[1] != 0 && err == nil {
				t.Fatal("old Q reuse not rejected")
			}
			if len(w.history) != before {
				t.Fatal("rejected reuse changed tombstones")
			}
			w.reset(int(s.Epoch))
			w.mu.Unlock()
			for node := 0; node < 2; node++ {
				children[node] = start(t, server.URL, w.root, node, int(s.Epoch))
				checkBoundary(t, children[node].wait(t, "boundary", "candidate-ready"), s, node)
			}
		case "Fund":
			t.Fatal("claim funding has no effect and cannot be replayed as success")
		default:
			t.Fatalf("unsupported action %s", action)
		}
		checkWorld(t, w, s)
		if st.Proc != nil && children[*st.Proc].done && action != "ProcessCrash" && action != "HostCrash" {
			inspectTerminal(t, w, *st.Proc, s, children[*st.Proc].terminal)
		}
		actual := "environment"
		if st.Proc != nil {
			actual = children[*st.Proc].outcome
		}
		expected := st.Expect
		if tr.Weakening != 0 && st.Proc != nil {
			expected = "paused"
			switch s.Phase[*st.Proc] {
			case 8:
				expected = "ready-unfunded"
			case 9:
				expected = "dead"
			case 10:
				expected = "quarantined"
			case 13:
				expected = "expired"
			}
		}
		if actual != expected {
			t.Fatalf("step outcome concrete=%s model=%s", actual, expected)
		}
		outcomes = append(outcomes, map[string]any{"n": st.N, "action": action, "outcome": actual, "matched_state": s})
		previous = s
		executed++
	}
	for n, c := range children {
		if !c.done {
			c.kill(t)
			w.mu.Lock()
			w.fences[n].leased = false
			w.mu.Unlock()
		}
	}
	return map[string]any{"id": tr.ID, "kind": tr.Kind, "weakening": tr.Weakening, "steps_executed": executed, "cut_at_claim_divergence": cut, "replay": tr.Replay, "hooks": hooks, "matched": true, "outcomes": outcomes, "node_events": []any{children[0].observed, children[1].observed}}
}
func mustDescriptor(t *testing.T, epoch int) dkg.Descriptor {
	t.Helper()
	d, err := dkg.NewDescriptor(parameters(), selection(epoch), candidate(epoch))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestV03SubprocessReplay(t *testing.T) {
	files, err := filepath.Glob("../evidence/traces/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("missing current traces", err)
	}
	var reports []map[string]any
	for _, file := range files {
		var tr trace
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &tr); err != nil {
			t.Fatal(err)
		}
		t.Run(tr.ID, func(t *testing.T) { reports = append(reports, play(t, tr)) })
	}
	if os.Getenv("V03_REPLAY_REPORT") != "" {
		raw, _ := json.MarshalIndent(reports, "", "  ")
		if err = os.WriteFile(os.Getenv("V03_REPLAY_REPORT"), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("matched traces=%d", len(reports))
}

func compareWallet(t *testing.T, w dkg.WalletRecord, s abstract, n int) {
	t.Helper()
	rank := map[string]uint8{"": 0, "Candidate": 1, "RegisteredPendingReady": 2, "ReadyUnfunded": 3, "Closed": 4}
	if rank[w.State] != s.WalletState[n] {
		t.Fatalf("persisted wallet state node=%d concrete=%s model=%d phase=%v", n, w.State, s.WalletState[n], s.Phase)
	}
	if s.WalletState[n] == 0 {
		return
	}
	d := mustDescriptor(t, int(s.Epoch))
	h, err := dkg.DescriptorHash(d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.Descriptor, d) || w.DescriptorHash != h || w.ID != dkg.WalletID(candidate(int(s.Epoch)).OutputKey) {
		t.Fatal("persisted identity mismatch")
	}
	if w.Quarantined != (s.Quarantine&(1<<n) != 0) {
		t.Fatalf("persisted quarantine mismatch node=%d concrete=%t model=%d", n, w.Quarantined, s.Quarantine)
	}
	if s.WalletState[n] >= 2 && (w.ApprovalBlock != 10+uint64(s.PreTick) || w.Deadline != 13+uint64(s.PreTick) || w.ApprovalHash != (common.Hash{byte(s.Epoch + 1), 8})) {
		t.Fatal("persisted receipt/deadline changed")
	}
}
func inspectTerminal(t *testing.T, w *world, n int, s abstract, returned *event) {
	t.Helper()
	ctx := context.Background()
	root, err := store.Open(ctx, config(w.root, n, localFence{w, n}))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	j, err := root.Scope(domain(int(s.Epoch)))
	if err != nil {
		t.Fatal(err)
	}
	record, err := j.LoadWallet(ctx)
	if err != nil && !(s.WalletState[n] == 0 && errors.Is(err, store.ErrMissing)) {
		t.Fatal(err)
	}
	compareWallet(t, record, s, n)
	compareResultIdentity(t, w, record)
	if returned == nil || !reflect.DeepEqual(returned.Wallet, record) {
		t.Fatal("returned wallet differs from reopened durable record")
	}
	key, err := j.LoadKey(ctx)
	has := err == nil
	if err != nil && !errors.Is(err, store.ErrMissing) {
		t.Fatal(err)
	}
	if has != (s.Key&(1<<n) != 0) {
		t.Fatal("terminal key durability mismatch")
	}
	if has {
		compareKey(t, key, s, n)
	}
	status, err := j.DKGStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := frost.DKGSeatsLost
	if has {
		want = frost.DKGKeyStored
	}
	if status.State != want {
		t.Fatal("terminal DKG status mismatch")
	}
}

func (w *world) admission(d dkg.Descriptor) error {
	if _, used := w.history[dkg.WalletID(d.OutputKey)]; used {
		return errors.New("duplicate output key tombstone")
	}
	return nil
}

func compareKey(t *testing.T, key frost.KeyReady, s abstract, n int) {
	t.Helper()
	want := frost.KeyReady{Candidate: candidate(int(s.Epoch))}
	for seat := n*2 + 1; seat <= n*2+2; seat++ {
		want.LocalReferences = append(want.LocalReferences, []byte{byte(selection(int(s.Epoch)).Epoch), byte(seat), 42})
	}
	if !reflect.DeepEqual(key, want) {
		t.Fatal("durable KeyReady differs from exact candidate and local references")
	}
}

func compareResultIdentity(t *testing.T, w *world, record dkg.WalletRecord) {
	t.Helper()
	if record.ResultHash == ([32]byte{}) {
		if record.ApprovalBlock != 0 {
			t.Fatal("approved record missing result hash")
		}
		return
	}
	w.mu.Lock()
	expected, err := dkg.ResultHash(w.result)
	w.mu.Unlock()
	if err != nil || record.ResultHash != expected {
		t.Fatal("durable result hash differs from actual submitted signed result", err)
	}
}
