package libp2p

import "testing"

func TestLoopbackOnlyListener(t *testing.T) {
	addresses, err := getListenAddrs(0, true)
	if err != nil || len(addresses) != 1 || addresses[0].String() != "/ip4/127.0.0.1/tcp/0" {
		t.Fatal("control LoopbackListener assertion: local listener exposed another interface", addresses, err)
	}
}
