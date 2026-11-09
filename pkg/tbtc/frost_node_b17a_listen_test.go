package tbtc_test

import (
	"fmt"
	"reflect"

	"github.com/keep-network/keep-core/pkg/net/libp2p"
)

// b17aLoopbackNetworkConfig is the K-09/b6 world's node network
// configuration: the base fields plus a loopback-only listen address on the
// node's port (decision D35, tuxxy/keep-core#37). The listen field is set by
// name, so that this file compiles against a libp2p.Config that does not
// have it yet. In that case it returns an error, and the node refuses to
// start rather than listen on every interface.
func b17aLoopbackNetworkConfig(port int, peers []string) (libp2p.Config, error) {
	config := libp2p.Config{Port: port, Peers: peers}
	field := reflect.ValueOf(&config).Elem().FieldByName("ListenAddresses")
	if !field.IsValid() || field.Type() != reflect.TypeOf([]string(nil)) {
		return config, fmt.Errorf("K09-20: libp2p.Config has no ListenAddresses []string field, so a K-09 node cannot be bound to loopback (D35)")
	}
	field.Set(reflect.ValueOf([]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)}))
	return config, nil
}
