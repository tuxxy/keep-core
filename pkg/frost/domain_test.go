package frost_test

import (
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/snowfallengine"
	"testing"
)

func TestAttemptDomainBinding(t *testing.T) {
	base := frost.Domain{Network: "local", Chain: [32]byte{1}, Registry: [20]byte{2}, Wallet: [32]byte{3}, Epoch: 4}
	a, e := base.NewAttempt("sign", [32]byte{5}, 6)
	if e != nil {
		t.Fatal(e)
	}
	want := snowfallengine.AttemptID(a)
	for _, field := range []string{"network", "chain", "registry", "wallet", "epoch", "purpose", "session", "start"} {
		t.Run(field, func(t *testing.T) {
			d := base
			purpose := "sign"
			session := [32]byte{5}
			start := uint64(6)
			switch field {
			case "network":
				d.Network = "other"
			case "chain":
				d.Chain[0]++
			case "registry":
				d.Registry[0]++
			case "wallet":
				d.Wallet[0]++
			case "epoch":
				d.Epoch++
			case "purpose":
				purpose = "dkg"
			case "session":
				session[0]++
			case "start":
				start++
			}
			changed, e := d.NewAttempt(purpose, session, start)
			if e != nil {
				t.Fatal(e)
			}
			if snowfallengine.AttemptID(changed) == want {
				t.Fatal("domain component not bound")
			}
		})
	}
	if e = base.ValidateAttempt(a, "dkg"); e == nil {
		t.Fatal("purpose mismatch accepted")
	}
	a.Channel[0] ^= 1
	if e = base.ValidateAttempt(a, "sign"); e == nil {
		t.Fatal("noncanonical channel accepted")
	}
}
