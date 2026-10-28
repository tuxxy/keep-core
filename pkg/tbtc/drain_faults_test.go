package tbtc_test

// ADAPTER for the batch-2 transient case: a fault proxy in front of Anvil that
// fails the first occurrence of every distinct contract read since the last
// Arm with a transient error (HTTP 503, or a JSON-RPC internal error on the
// websocket). A read is an eth_call (target and full calldata) or an
// eth_getLogs (filter addresses and topics; the block range is ignored, so a
// re-scan of the same events counts as a repeat). Every repeat succeeds;
// transactions and every other method pass through.
//
// ArmWhere(frostOnlyReads) fails only the FROST-extension reads.
//
// The frozen batch-1 adapters fail eth_call only (legacyFaults) or key logs by
// address alone (legacyRecorder), so a first-time query for FROST events at
// the Bridge address could not fail there. A producer that took such a
// failure for "no FROST target" and fell back to a legacy target (control
// W-swallow-frost-read) would pass them.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

type drainFaults struct {
	upstream string
	server   *httptest.Server

	mutex  sync.Mutex
	armed  bool
	where  func(legacyRPCRequest) bool
	seen   map[string]bool
	failed map[string]int
}

func newDrainFaults(t *testing.T, upstream string) *drainFaults {
	f := &drainFaults{upstream: upstream, failed: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *drainFaults) URL() string { return f.server.URL }

// Arm starts a new phase: every distinct read fails once more.
func (f *drainFaults) Arm() { f.ArmWhere(nil) }

// ArmWhere starts a new phase in which only the reads where selects fail
// once; nil selects every read.
func (f *drainFaults) ArmWhere(where func(legacyRPCRequest) bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.armed = true
	f.where = where
	f.seen = map[string]bool{}
}

// frostOnlyReads selects the reads the pre-FROST Bridge cannot answer: an
// eth_call to the Bridge with a selector outside its 40a11d1d ABI, or an
// eth_getLogs at the Bridge for an event outside that ABI. Failing only
// these leaves every legacy read working while the FROST state is
// momentarily unreadable.
func frostOnlyReads(bridge common.Address, base abi.ABI) func(legacyRPCRequest) bool {
	selectors := legacySelectors(base)
	events := map[common.Hash]bool{}
	for _, event := range base.Events {
		events[event.ID] = true
	}
	return func(request legacyRPCRequest) bool {
		if len(request.Params) == 0 {
			return false
		}
		switch request.Method {
		case "eth_call":
			var message struct {
				To    *common.Address `json:"to"`
				Data  string          `json:"data"`
				Input string          `json:"input"`
			}
			if json.Unmarshal(request.Params[0], &message) != nil || message.To == nil || *message.To != bridge {
				return false
			}
			data := message.Input
			if data == "" {
				data = message.Data
			}
			raw := common.FromHex(data)
			if len(raw) < 4 {
				return false
			}
			_, base := selectors[hex.EncodeToString(raw[:4])]
			return !base
		case "eth_getLogs":
			var filter struct {
				Address json.RawMessage   `json:"address"`
				Topics  []json.RawMessage `json:"topics"`
			}
			if json.Unmarshal(request.Params[0], &filter) != nil || len(filter.Topics) == 0 ||
				!strings.Contains(strings.ToLower(string(filter.Address)), strings.ToLower(bridge.Hex())) {
				return false
			}
			var topics []common.Hash
			var one common.Hash
			if json.Unmarshal(filter.Topics[0], &one) == nil {
				topics = []common.Hash{one}
			} else if json.Unmarshal(filter.Topics[0], &topics) != nil {
				return false
			}
			for _, topic := range topics {
				if !events[topic] {
					return true
				}
			}
		}
		return false
	}
}

// Failed reports how many reads of each kind ("call", "logs") were failed.
func (f *drainFaults) Failed(kind string) int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	total := 0
	for key, n := range f.failed {
		if strings.HasPrefix(key, kind+":") {
			total += n
		}
	}
	return total
}

func drainReadKey(request legacyRPCRequest) string {
	if len(request.Params) == 0 {
		return ""
	}
	switch request.Method {
	case "eth_call":
		if key := legacyCallKey(request); key != "" {
			return "call:" + key
		}
	case "eth_getLogs":
		var filter struct {
			Address json.RawMessage `json:"address"`
			Topics  json.RawMessage `json:"topics"`
		}
		if json.Unmarshal(request.Params[0], &filter) != nil {
			return ""
		}
		return "logs:" + strings.ToLower(string(filter.Address)) + ":" + strings.ToLower(string(filter.Topics))
	}
	return ""
}

func (f *drainFaults) admit(requests []legacyRPCRequest) bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if !f.armed {
		return false
	}
	fail := false
	for _, request := range requests {
		key := drainReadKey(request)
		if f.where != nil && !f.where(request) {
			continue
		}
		if key != "" && !f.seen[key] {
			f.seen[key] = true
			f.failed[key]++
			fail = true
		}
	}
	return fail
}

func (f *drainFaults) serve(w http.ResponseWriter, req *http.Request) {
	if websocket.IsWebSocketUpgrade(req) {
		f.serveWebsocket(w, req)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	requests, _, err := legacyParseRequests(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.admit(requests) {
		http.Error(w, "transient: upstream unavailable", http.StatusServiceUnavailable)
		return
	}
	response, err := http.Post(f.upstream, "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	out, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(out)
}

func (f *drainFaults) serveWebsocket(w http.ResponseWriter, req *http.Request) {
	upstream, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.upstream, "http"), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, err := legacyUpgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	defer client.Close()
	var clientWrite sync.Mutex
	write := func(message []byte) error {
		clientWrite.Lock()
		defer clientWrite.Unlock()
		return client.WriteMessage(websocket.TextMessage, message)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := upstream.ReadMessage()
			if err != nil {
				return
			}
			if write(message) != nil {
				return
			}
		}
	}()
	for {
		_, message, err := client.ReadMessage()
		if err != nil {
			return
		}
		requests, _, err := legacyParseRequests(message)
		if err != nil {
			return
		}
		if f.admit(requests) {
			for _, request := range requests {
				reply, _ := json.Marshal(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      request.ID,
					"error":   map[string]interface{}{"code": -32603, "message": "transient: upstream unavailable"},
				})
				if write(reply) != nil {
					return
				}
			}
			continue
		}
		if upstream.WriteMessage(websocket.TextMessage, message) != nil {
			return
		}
		select {
		case <-done:
			return
		default:
		}
	}
}
