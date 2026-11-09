package tbtc_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// TestB17aWorldNodeNetworkIsLoopbackOnly (unit).
//
// Invariant (decision D35 (a)): the K-09/b6 world's node network listens on
// loopback only, on the node's port. Every libp2p.Connect call in the world
// takes the configuration b17aLoopbackNetworkConfig builds, and that
// configuration lists only a loopback address on the given port. Whether
// libp2p honours the field is TestB17aListenAddressesBindLoopbackOnly in
// pkg/net/libp2p.
//
// Refs tuxxy/keep-core#37
func TestB17aWorldNodeNetworkIsLoopbackOnly(t *testing.T) {
	source, err := os.ReadFile("frost_node_b6_world_test.go")
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Count(string(source), "libp2p.Connect(")
	bound := strings.Count(string(source), "libp2p.Connect(ctx, networkConfig,")
	if calls == 0 || calls != bound || !strings.Contains(string(source), "networkConfig, err := b17aLoopbackNetworkConfig(config.Port, config.Peers)") {
		t.Errorf("K09-20: the b6 world calls libp2p.Connect %d times, %d of them with the loopback configuration from b17aLoopbackNetworkConfig", calls, bound)
	}

	const port = 41234
	config, err := b17aLoopbackNetworkConfig(port, []string{"/ip4/127.0.0.1/tcp/1/ipfs/peer"})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if config.Port != port || len(config.Peers) != 1 {
		t.Errorf("K09-20: the world's network configuration lost the base fields (port %d, peers %v)", config.Port, config.Peers)
	}
	listen := reflect.ValueOf(config).FieldByName("ListenAddresses").Interface().([]string)
	if len(listen) == 0 {
		t.Fatalf("K09-20: the world's network configuration sets no listen address, so the node listens on every interface")
	}
	for _, s := range listen {
		addr, err := ma.NewMultiaddr(s)
		if err != nil || !manet.IsIPLoopback(addr) {
			t.Errorf("K09-20: the world's node listens on %q, beyond loopback", s)
			continue
		}
		if p, err := addr.ValueForProtocol(ma.P_TCP); err != nil || p != "41234" {
			t.Errorf("K09-20: the world's node listens on %q, not on its port %d", s, port)
		}
	}
}
