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
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gorilla/websocket"
)

type drainFaults struct {
	upstream string
	server   *httptest.Server

	mutex sync.Mutex
	armed bool
	where func(legacyRPCRequest) bool
	seen  map[string]bool
	// horizon, if set, is the highest block whose logs eth_getLogs returns
	// (SetLogHorizon).
	horizon uint64
	hidden  int
	failed  map[string]int
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

// SetLogHorizon makes the endpoint's log index lag: eth_getLogs succeeds but
// omits every log from a block above height, while every other method sees
// the head. 0 removes the lag. This is a readable but short index for a host
// that learns targets from logs (review R6); a host that does not sees a
// complete index.
func (f *drainFaults) SetLogHorizon(height uint64) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.horizon = height
}

// Hidden reports how many logs the horizon withheld.
func (f *drainFaults) Hidden() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.hidden
}

// trimLogs removes the logs above the horizon from eth_getLogs responses in
// message (a single response or a batch); logsIDs holds the request ids of
// the eth_getLogs requests.
func (f *drainFaults) trimLogs(message []byte, logsIDs map[string]bool) []byte {
	f.mutex.Lock()
	horizon := f.horizon
	f.mutex.Unlock()
	if horizon == 0 || len(logsIDs) == 0 {
		return message
	}
	type response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   json.RawMessage `json:"error,omitempty"`
	}
	// Only responses to eth_getLogs are rewritten; anything else
	// (subscription notifications included) passes through byte for byte.
	trim := func(r *response) bool {
		if !logsIDs[string(r.ID)] || r.Result == nil {
			return false
		}
		var logs []map[string]interface{}
		if json.Unmarshal(r.Result, &logs) != nil {
			return false
		}
		kept := make([]map[string]interface{}, 0, len(logs))
		for _, l := range logs {
			number, _ := l["blockNumber"].(string)
			if n, err := hexutil.DecodeUint64(number); err == nil && n > horizon {
				f.mutex.Lock()
				f.hidden++
				f.mutex.Unlock()
				continue
			}
			kept = append(kept, l)
		}
		r.Result, _ = json.Marshal(kept)
		return true
	}
	trimmed := bytes.TrimSpace(message)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []response
		if json.Unmarshal(message, &batch) != nil {
			return message
		}
		changed := false
		for i := range batch {
			changed = trim(&batch[i]) || changed
		}
		if !changed {
			return message
		}
		out, _ := json.Marshal(batch)
		return out
	}
	var single response
	if json.Unmarshal(message, &single) != nil || !trim(&single) {
		return message
	}
	out, _ := json.Marshal(single)
	return out
}

func drainLogsIDs(requests []legacyRPCRequest) map[string]bool {
	ids := map[string]bool{}
	for _, request := range requests {
		if request.Method == "eth_getLogs" {
			ids[string(request.ID)] = true
		}
	}
	return ids
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
	_, _ = w.Write(f.trimLogs(out, drainLogsIDs(requests)))
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
	// The ids of eth_getLogs requests in flight on this connection.
	var logsMutex sync.Mutex
	logsIDs := map[string]bool{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := upstream.ReadMessage()
			if err != nil {
				return
			}
			logsMutex.Lock()
			ids := logsIDs
			logsIDs = map[string]bool{}
			for id := range ids {
				logsIDs[id] = true
			}
			logsMutex.Unlock()
			if write(f.trimLogs(message, ids)) != nil {
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
		logsMutex.Lock()
		for id := range drainLogsIDs(requests) {
			logsIDs[id] = true
		}
		logsMutex.Unlock()
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
