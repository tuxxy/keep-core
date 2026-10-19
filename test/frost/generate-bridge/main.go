// Command generate-bridge binds the tbtc-v2 IFrostBridge ABI together with the
// events its linked FROST libraries emit at the Bridge address. The interface
// artifact alone cannot decode those events.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

var eventSources = []string{
	"FrostRedemptionLedger", "FrostRedemptionRequests", "FrostRedemptionSettlement",
	"FrostCancellationAdmissions", "FrostDepositSweep", "FrostDepositAdmission",
	"FrostFundingConfiguration", "FrostWallets", "FrostMovement", "FrostMovementDispatch",
	"FrostFraud", "FrostLifecycle",
}

func read(root, name string) []map[string]any {
	raw, err := os.ReadFile(filepath.Join(root, name+".sol", name+".json"))
	if err != nil {
		panic(err)
	}
	var a struct {
		ABI []map[string]any `json:"abi"`
	}
	if err = json.Unmarshal(raw, &a); err != nil {
		panic(err)
	}
	return a.ABI
}

func signature(entry map[string]any) string {
	raw, _ := json.Marshal(entry["inputs"])
	return entry["name"].(string) + string(raw)
}

func main() {
	if len(os.Args) != 3 {
		panic("usage: generate-bridge <tbtc-v2 build/contracts/frost> <output.go>")
	}
	root, out := os.Args[1], os.Args[2]
	merged := read(root, "IFrostBridge")
	seen := map[string]bool{}
	for _, e := range merged {
		if e["type"] == "event" {
			seen[signature(e)] = true
		}
	}
	var events []map[string]any
	for _, source := range eventSources {
		for _, e := range read(root, source) {
			if e["type"] != "event" || seen[signature(e)] {
				continue
			}
			seen[signature(e)] = true
			events = append(events, e)
		}
	}
	sort.Slice(events, func(i, j int) bool { return signature(events[i]) < signature(events[j]) })
	merged = append(merged, events...)
	raw, err := json.Marshal(merged)
	if err != nil {
		panic(err)
	}
	s, err := bind.Bind([]string{"FrostBridge"}, []string{string(raw)}, []string{""}, nil, "frostbridgeabi", bind.LangGo, nil, nil)
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(out, []byte(s), 0644); err != nil {
		panic(err)
	}
}
