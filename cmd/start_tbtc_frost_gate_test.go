package cmd

// Refs tuxxy/keep-core#26 (fix review B1), #42.

import (
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

// Invariant (D5 revised): start refuses the node's FROST settings
// (tbtc.Config.Frost) with the FROST node gate's own error, before any
// Ethereum connection, account access or worker startup, whatever the
// [frost] switch says. The Ethereum URL here is unreachable, so a later
// refusal would be a connection error instead.
func TestStartRefusesTbtcFrostAtTheNodeGate(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	tbtcBefore, urlBefore := clientConfig.Tbtc, clientConfig.Ethereum.URL
	defer func() { clientConfig.Tbtc, clientConfig.Ethereum.URL = tbtcBefore, urlBefore }()
	clientConfig.Tbtc.Frost.Registry = [20]byte{1}
	clientConfig.Ethereum.URL = "ws://127.0.0.1:1"
	err := start(StartCommand)
	if err == nil || err.Error() != gate.Error() {
		t.Fatalf("B1: start did not refuse the node's FROST settings at the node gate first: %v", err)
	}
}
