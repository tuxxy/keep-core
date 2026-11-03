package cmd

// Refs tuxxy/keep-core#42.

import (
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

// Invariant (KC-SF-V3 line 362, SC-01; D5): start refuses an enabled FROST
// switch with the FROST node gate's own error, before any Ethereum
// connection, account access or worker startup. The Ethereum URL here is
// unreachable, so a later refusal would be a connection error instead.
func TestStartRefusesFrostAtTheNodeGate(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	if gate == nil {
		t.Fatal("#42: the FROST node gate accepts an enabled configuration")
	}
	frostBefore, urlBefore := clientConfig.Frost, clientConfig.Ethereum.URL
	defer func() { clientConfig.Frost, clientConfig.Ethereum.URL = frostBefore, urlBefore }()
	clientConfig.Frost.Enabled = true
	clientConfig.Ethereum.URL = "ws://127.0.0.1:1"
	err := start(StartCommand)
	if err == nil || err.Error() != gate.Error() {
		t.Fatalf("#42: start did not refuse FROST at the node gate first: %v", err)
	}
}
