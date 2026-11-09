package libp2p

import (
	"fmt"
	"reflect"
	"testing"

	addrutil "github.com/libp2p/go-addr-util"
	ma "github.com/multiformats/go-multiaddr"
)

// TestB17aEmptyListenAddressesKeepBaseListenSet (unit; opens no listener).
//
// Invariant (decision D35 (a): "empty means every interface, as base", so
// legacy is unchanged, shared rule 5): base's pure helper getListenAddrs(port)
// still returns every addrutil.InterfaceAddresses() address on that port, so
// a fix cannot make the base listen set loopback-only.
//
// That the provider uses this set when ListenAddresses is empty is not
// tested: checking it by listening would open LAN listeners, and a source
// check pins the fix's shape (recheck 2, B2). It is a named checklist item of
// the D35 fix review instead.
//
// Refs tuxxy/keep-core#37
func TestB17aEmptyListenAddressesKeepBaseListenSet(t *testing.T) {
	const port = 3919
	interfaces, err := addrutil.InterfaceAddresses()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, a := range interfaces {
		want = append(want, a.Encapsulate(ma.StringCast(fmt.Sprintf("/tcp/%d", port))).String())
	}
	got, err := getListenAddrs(port)
	if err != nil {
		t.Fatal(err)
	}
	var gotS []string
	for _, a := range got {
		gotS = append(gotS, a.String())
	}
	if !reflect.DeepEqual(gotS, want) {
		t.Errorf("K09-20: getListenAddrs(%d) = %v; base listens on every interface address %v", port, gotS, want)
	}
}
