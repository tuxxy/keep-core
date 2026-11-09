package libp2p

import (
	"net"
	"reflect"
	"strconv"
	"testing"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/keep-network/keep-core/pkg/firewall"
	"github.com/keep-network/keep-core/pkg/operator"
)

// freeLoopbackPort is a TCP port free on 127.0.0.1 at the time of the call.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestB17aListenAddressesBindLoopbackOnly (unit; opens a loopback listener
// only).
//
// Invariant (decision D35 (a)): a provider configured with listen addresses
// listens on exactly those, so a test network whose operator keys are public
// (the K-09 world's test scalars) can be kept off every LAN interface. An
// empty setting keeps base behaviour (every interface); that half is not
// exercised here because it would open LAN listeners.
//
// The field is set by name: if libp2p.Config has no ListenAddresses field,
// the test fails before Connect runs, so nothing listens at all.
//
// Refs tuxxy/keep-core#37
func TestB17aListenAddressesBindLoopbackOnly(t *testing.T) {
	port := freeLoopbackPort(t)
	config := Config{Port: port}
	field := reflect.ValueOf(&config).Elem().FieldByName("ListenAddresses")
	if !field.IsValid() || field.Type() != reflect.TypeOf([]string(nil)) {
		t.Fatalf("K09-20: libp2p.Config has no optional ListenAddresses []string field, so a test network cannot be bound to loopback (D35)")
	}
	want := "/ip4/127.0.0.1/tcp/" + strconv.Itoa(port)
	field.Set(reflect.ValueOf([]string{want}))

	ctx, cancel := newTestContext()
	defer cancel()
	key, _, err := operator.GenerateKeyPair(DefaultCurve)
	if err != nil {
		t.Fatal(err)
	}
	connected, err := Connect(ctx, config, key, firewall.Disabled, idleTicker())
	if err != nil {
		t.Fatalf("K09-20: Connect with ListenAddresses [%s]: %v", want, err)
	}
	p := connected.(*provider)
	defer p.host.Close()

	listening := p.host.Network().ListenAddresses()
	if len(listening) == 0 {
		t.Fatalf("K09-20: with ListenAddresses [%s] the provider listens nowhere", want)
	}
	for _, addr := range listening {
		if !manet.IsIPLoopback(addr) {
			t.Errorf("K09-20: with ListenAddresses [%s] the provider listens on %s, beyond loopback", want, addr)
			continue
		}
		got, err := addr.ValueForProtocol(ma.P_TCP)
		if err != nil || got != strconv.Itoa(port) {
			t.Errorf("K09-20: with ListenAddresses [%s] the provider listens on %s, not on port %d", want, addr, port)
		}
	}
}
